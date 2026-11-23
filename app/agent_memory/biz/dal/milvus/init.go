package milvus

import (
	"context"
	"fmt"
	"slices"

	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
	milvusindexer "github.com/cloudwego/eino-ext/components/indexer/milvus2"
	milvusretriever "github.com/cloudwego/eino-ext/components/retriever/milvus2"
	"github.com/cloudwego/eino-ext/components/retriever/milvus2/search_mode"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

const (
	nodeIDField     = "node_id"
	graphIDField    = "graph_id"
	contentField    = "content"
	denseField      = "vector"
	sparseField     = "sparse_vector"
	confidenceField = "confidence"
	isValidField    = "is_valid"
	validFromField  = "valid_from"
	fixTimeField    = "fix_time"
	expiresAtField  = "expires_at"
)

var timeFields = [...]string{validFromField, fixTimeField, expiresAtField}

func Init(ctx context.Context, cfg conf.Milvus, dimensions int, embedder embedding.Embedder) (*NodeIndex, error) {
	if cfg.Address == "" || cfg.Database == "" || cfg.Collection == "" || dimensions <= 0 || embedder == nil {
		return nil, fmt.Errorf("Milvus requires an address, database, collection and embedder dimensions")
	}
	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: cfg.Address,
		DBName:  cfg.Database,
		APIKey:  cfg.Token,
	})
	if err != nil {
		return nil, fmt.Errorf("connect Milvus: %w", err)
	}
	nodeIndex, err := initNodeIndex(ctx, client, cfg.Collection, dimensions, embedder)
	if err != nil {
		_ = client.Close(ctx)
		return nil, err
	}
	return nodeIndex, nil
}

func initNodeIndex(ctx context.Context, client *milvusclient.Client, collection string, dimensions int, embedder embedding.Embedder) (*NodeIndex, error) {
	if err := ensureCollection(ctx, client, collection, dimensions); err != nil {
		return nil, err
	}

	writer, err := milvusindexer.NewIndexer(ctx, &milvusindexer.IndexerConfig{
		Client:            client,
		Collection:        collection,
		Embedding:         embedder,
		DocumentConverter: documentColumns(dimensions),
		Vector: &milvusindexer.VectorConfig{
			Dimension: int64(dimensions), VectorField: denseField, MetricType: milvusindexer.COSINE,
		},
		Sparse: &milvusindexer.SparseVectorConfig{
			VectorField: sparseField, MetricType: milvusindexer.BM25, Method: milvusindexer.SparseMethodAuto,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Milvus indexer: %w", err)
	}
	readerConfig := milvusretriever.RetrieverConfig{
		Client:            client,
		Collection:        collection,
		Embedding:         embedder,
		SearchMode:        hybridSearch{},
		ConsistencyLevel:  milvusretriever.ConsistencyLevelStrong,
		DocumentConverter: searchDocuments,
		OutputFields:      []string{contentField, graphIDField, confidenceField, isValidField, validFromField, fixTimeField, expiresAtField},
	}
	semanticConfig := readerConfig
	semanticConfig.SearchMode = search_mode.NewApproximate(milvusretriever.COSINE)
	reader, err := milvusretriever.NewRetriever(ctx, &readerConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize Milvus retriever: %w", err)
	}
	semantic, err := milvusretriever.NewRetriever(ctx, &semanticConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize Milvus semantic retriever: %w", err)
	}
	return &NodeIndex{client: client, writer: writer, reader: reader, semantic: semantic}, nil
}

func nodeSchema(collection string, dimensions int) *entity.Schema {
	schema := entity.NewSchema().WithName(collection).
		WithField(entity.NewField().WithName(nodeIDField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(36).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(graphIDField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(36)).
		WithField(entity.NewField().WithName(contentField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535).
			WithEnableAnalyzer(true).WithAnalyzerParams(map[string]any{"tokenizer": "jieba"})).
		WithField(entity.NewField().WithName(denseField).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dimensions))).
		WithField(entity.NewField().WithName(sparseField).WithDataType(entity.FieldTypeSparseVector)).
		WithField(entity.NewField().WithName(confidenceField).WithDataType(entity.FieldTypeDouble)).
		WithField(entity.NewField().WithName(isValidField).WithDataType(entity.FieldTypeBool)).
		WithFunction(entity.NewFunction().WithName("memory_bm25").WithType(entity.FunctionTypeBM25).
			WithInputFields(contentField).WithOutputFields(sparseField))
	for _, name := range timeFields {
		schema.WithField(entity.NewField().WithName(name).WithDataType(entity.FieldTypeInt64).WithNullable(true))
	}
	return schema
}

func ensureCollection(ctx context.Context, client *milvusclient.Client, name string, dimensions int) error {
	want := nodeSchema(name, dimensions)
	exists, err := client.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
	if err != nil {
		return fmt.Errorf("check Milvus collection %q: %w", name, err)
	}
	if !exists {
		if err := client.CreateCollection(ctx, milvusclient.NewCreateCollectionOption(name, want)); err != nil {
			exists, _ = client.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
			if !exists {
				return fmt.Errorf("create Milvus collection %q: %w", name, err)
			}
		}
	}
	collection, err := client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(name))
	if err != nil {
		return fmt.Errorf("describe Milvus collection %q: %w", name, err)
	}
	if err := validateSchema(collection.Schema, want); err != nil {
		return fmt.Errorf("incompatible Milvus collection %q: %w", name, err)
	}
	return nil
}

func validateSchema(got, want *entity.Schema) error {
	if got.AutoID || got.PKFieldName() != nodeIDField {
		return fmt.Errorf("expected a manually assigned %s primary key", nodeIDField)
	}
	for _, field := range want.Fields {
		pos := slices.IndexFunc(got.Fields, func(existing *entity.Field) bool { return existing.Name == field.Name })
		if pos < 0 || got.Fields[pos].DataType != field.DataType || got.Fields[pos].Nullable != field.Nullable {
			return fmt.Errorf("expected schema for field %s", field.Name)
		}
		for key, value := range field.TypeParams {
			if got.Fields[pos].TypeParams[key] != value {
				return fmt.Errorf("expected %s=%s for field %s", key, value, field.Name)
			}
		}
	}
	for _, fn := range got.Functions {
		if fn.Type == entity.FunctionTypeBM25 && slices.Equal(fn.InputFieldNames, []string{contentField}) &&
			slices.Equal(fn.OutputFieldNames, []string{sparseField}) {
			return nil
		}
	}
	return fmt.Errorf("expected a BM25 function from %s to %s", contentField, sparseField)
}
