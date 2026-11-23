package milvus

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	milvusretriever "github.com/cloudwego/eino-ext/components/retriever/milvus2"
	"github.com/cloudwego/eino-ext/components/retriever/milvus2/search_mode"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

type NodeIndex struct {
	client   *milvusclient.Client
	writer   indexer.Indexer
	reader   retriever.Retriever
	semantic retriever.Retriever
}

func (idx *NodeIndex) Close(ctx context.Context) error {
	return idx.client.Close(ctx)
}

func (idx *NodeIndex) Sync(ctx context.Context, node model.MemoryNode) error {
	doc := &schema.Document{
		ID:      node.NodeID,
		Content: node.Content,
		MetaData: map[string]any{
			graphIDField:    node.GraphID,
			confidenceField: node.Confidence,
			isValidField:    node.IsValid,
			validFromField:  unixMicros(node.ValidFrom),
			fixTimeField:    unixMicros(node.FixTime),
			expiresAtField:  unixMicros(node.ExpiresAt),
		},
	}
	ids, err := idx.writer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		return err
	}
	if len(ids) != 1 || ids[0] != node.NodeID {
		return fmt.Errorf("Milvus did not confirm the upsert for node %q", node.NodeID)
	}
	return nil
}

// Search applies the MySQL candidate IDs to both the dense and BM25 branches.
func (idx *NodeIndex) Search(ctx context.Context, graphID, query string, candidateIDs []string, topK int) ([]*schema.Document, error) {
	return searchNodes(ctx, idx.reader, graphID, query, candidateIDs, topK)
}

func (idx *NodeIndex) SearchSemantic(ctx context.Context, graphID, query string, candidateIDs []string, topK int) ([]*schema.Document, error) {
	return searchNodes(ctx, idx.semantic, graphID, query, candidateIDs, topK)
}

func searchNodes(ctx context.Context, reader retriever.Retriever, graphID, query string, candidateIDs []string, topK int) ([]*schema.Document, error) {
	if graphID == "" || strings.TrimSpace(query) == "" || topK <= 0 {
		return nil, fmt.Errorf("search requires a graph ID, a query and a positive topK")
	}
	if len(candidateIDs) == 0 {
		return nil, nil
	}
	ids, err := json.Marshal(candidateIDs)
	if err != nil {
		return nil, fmt.Errorf("encode search candidates: %w", err)
	}
	filter := graphIDField + " == " + strconv.Quote(graphID) + " and " + nodeIDField + " in " + string(ids)
	return reader.Retrieve(ctx, query, retriever.WithTopK(topK), milvusretriever.WithFilter(filter))
}

type hybridSearch struct{}

func (hybridSearch) Retrieve(ctx context.Context, client *milvusclient.Client, cfg *milvusretriever.RetrieverConfig, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	settings := retriever.GetCommonOptions(&retriever.Options{TopK: &cfg.TopK}, opts...)
	branchLimit := max(*settings.TopK, 32)
	mode := search_mode.NewHybrid(milvusclient.NewRRFReranker(),
		&search_mode.SubRequest{VectorField: denseField, VectorType: milvusretriever.DenseVector, MetricType: milvusretriever.COSINE, TopK: branchLimit},
		&search_mode.SubRequest{VectorField: sparseField, VectorType: milvusretriever.SparseVector, MetricType: milvusretriever.BM25, TopK: branchLimit},
	)
	return mode.Retrieve(ctx, client, cfg, query, opts...)
}

func documentColumns(dimensions int) func(context.Context, []*schema.Document, [][]float64) ([]column.Column, error) {
	return func(_ context.Context, docs []*schema.Document, vectors [][]float64) ([]column.Column, error) {
		ids := column.NewColumnVarChar(nodeIDField, nil)
		graphIDs := column.NewColumnVarChar(graphIDField, nil)
		contents := column.NewColumnVarChar(contentField, nil)
		confidences := column.NewColumnDouble(confidenceField, nil)
		validities := column.NewColumnBool(isValidField, nil)
		dense := column.NewColumnFloatVector(denseField, dimensions, nil)
		columns := []column.Column{ids, graphIDs, contents, confidences, validities, dense}
		for _, name := range timeFields {
			timestamps := column.NewColumnInt64(name, nil)
			timestamps.SetNullable(true)
			columns = append(columns, timestamps)
		}

		for i, doc := range docs {
			if len(vectors[i]) != dimensions {
				return nil, fmt.Errorf("Qwen returned %d dimensions, expected %d", len(vectors[i]), dimensions)
			}
			vector := make([]float32, dimensions)
			for j, value := range vectors[i] {
				vector[j] = float32(value)
			}
			values := []any{doc.ID, doc.MetaData[graphIDField], doc.Content, doc.MetaData[confidenceField], doc.MetaData[isValidField], vector}
			for _, name := range timeFields {
				values = append(values, doc.MetaData[name])
			}
			for j, col := range columns {
				if err := col.AppendValue(values[j]); err != nil {
					return nil, fmt.Errorf("set Milvus field %s: %w", col.Name(), err)
				}
			}
		}
		return columns, nil
	}
}

func searchDocuments(_ context.Context, result milvusclient.ResultSet) ([]*schema.Document, error) {
	docs := make([]*schema.Document, 0, result.ResultCount)
	for i := 0; i < result.ResultCount; i++ {
		id, err := result.IDs.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("read Milvus node ID: %w", err)
		}
		doc := (&schema.Document{ID: id, MetaData: make(map[string]any)}).WithScore(float64(result.Scores[i]))
		for _, field := range result.Fields {
			isNull, err := field.IsNull(i)
			if err != nil {
				return nil, fmt.Errorf("check Milvus field %s: %w", field.Name(), err)
			}
			if isNull {
				doc.MetaData[field.Name()] = nil
				continue
			}
			value, err := field.Get(i)
			if err != nil {
				return nil, fmt.Errorf("read Milvus field %s: %w", field.Name(), err)
			}
			if field.Name() == contentField {
				doc.Content, _ = value.(string)
			} else {
				doc.MetaData[field.Name()] = value
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func unixMicros(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMicro()
}
