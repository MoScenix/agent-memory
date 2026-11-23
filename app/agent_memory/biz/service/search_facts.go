package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

const searchCandidateBudget = 4096

type SearchFactsService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewSearchFactsService(Context context.Context, RequestContext *app.RequestContext) *SearchFactsService {
	return &SearchFactsService{RequestContext: RequestContext, Context: Context}
}

func (h *SearchFactsService) Run(req *agent_memory.SearchFactsRequest) (*agent_memory.SearchFactsResponse, error) {
	body, graphID := req.GetBody(), req.GetGraphId()
	userID, question := body.GetUserId(), body.GetQuestion()
	if err := validateGraphRef(userID, graphID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(question) == "" {
		return nil, badRequest("question is required")
	}
	if body.GetTopK() > 100 {
		return nil, badRequest("top_k must be at most 100")
	}
	topK := int(body.GetTopK())
	if topK == 0 {
		topK = 10
	}
	mode := body.GetMode()
	switch mode {
	case agent_memory.SearchMode_SEARCH_MODE_UNSPECIFIED, agent_memory.SearchMode_SEARCH_MODE_HYBRID, agent_memory.SearchMode_SEARCH_MODE_SEMANTIC:
	default:
		return nil, badRequest("unsupported search mode")
	}
	filter := model.NodeCandidateFilter{At: time.Now().UTC(), MinimumConfidence: body.MinimumConfidence, SyncedOnly: true}
	if filter.MinimumConfidence != nil {
		if err := validateConfidence(*filter.MinimumConfidence); err != nil {
			return nil, err
		}
	}
	if err := applySearchTimeFilter(&filter, body.GetTimeFilter()); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(h.Context, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		version, candidates, err := searchCandidates(ctx, graphID, userID, filter)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return &agent_memory.SearchFactsResponse{GraphVersion: version}, nil
		}
		index := dependencies(ctx).Index
		if index == nil {
			return nil, &utils.HTTPError{Status: http.StatusServiceUnavailable, Message: "search index is unavailable"}
		}
		search := index.Search
		if mode == agent_memory.SearchMode_SEARCH_MODE_SEMANTIC {
			search = index.SearchSemantic
		}
		docs, err := search(ctx, graphID, question, candidates, topK)
		if err != nil {
			slog.ErrorContext(ctx, "memory search failed", "error", err)
			return nil, &utils.HTTPError{Status: http.StatusBadGateway, Message: "search index request failed"}
		}
		hits, err := hydrateSearchHits(ctx, graphID, userID, version, docs)
		if errors.Is(err, model.ErrVersionConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &agent_memory.SearchFactsResponse{GraphVersion: version, Hits: hits}, nil
	}
	return nil, versionConflict()
}

func applySearchTimeFilter(filter *model.NodeCandidateFilter, timeFilter *agent_memory.TimeFilter) error {
	if interval := timeFilter.GetValidRange(); interval != nil {
		if timeFilter.GetValidAt() != nil {
			return badRequest("valid_at and valid_range are mutually exclusive")
		}
		start, err := businessTime("valid_range.start", interval.GetStart())
		if err != nil {
			return err
		}
		end, err := businessTime("valid_range.end", interval.GetEnd())
		if err != nil {
			return err
		}
		if start == nil || end == nil || !start.Before(*end) {
			return badRequest("valid_range requires start before end")
		}
		filter.At, filter.Until, filter.Historical = *start, *end, true
		return nil
	}
	at, err := businessTime("valid_at", timeFilter.GetValidAt())
	if err != nil {
		return err
	}
	if at != nil {
		filter.At, filter.Historical = *at, true
	}
	return nil
}

func searchCandidates(ctx context.Context, graphID, userID string, filter model.NodeCandidateFilter) (uint64, []string, error) {
	var graphVersion uint64
	var ids []string
	err := readSnapshot(ctx, func(tx *gorm.DB) error {
		graph, err := model.NewMemoryGraphQuery(ctx, tx).GetByID(graphID, userID)
		if err != nil {
			return queryError(err, "graph")
		}
		graphVersion = graph.Version
		ids, err = model.NewMemoryNodeQuery(ctx, tx).ListCandidateIDs(graphID, filter, searchCandidateBudget+1)
		return err
	})
	if err == nil && len(ids) > searchCandidateBudget {
		err = &utils.HTTPError{Status: http.StatusUnprocessableEntity, Message: "too many search candidates; narrow the filters"}
	}
	return graphVersion, ids, err
}

func hydrateSearchHits(ctx context.Context, graphID, userID string, expectedVersion uint64, docs []*schema.Document) ([]*agent_memory.SearchHit, error) {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	var hits []*agent_memory.SearchHit
	err := readSnapshot(ctx, func(tx *gorm.DB) error {
		graph, err := model.NewMemoryGraphQuery(ctx, tx).GetByID(graphID, userID)
		if err != nil {
			return queryError(err, "graph")
		}
		if graph.Version != expectedVersion {
			return model.ErrVersionConflict
		}
		if len(ids) == 0 {
			return nil
		}
		nodes, err := model.NewMemoryNodeQuery(ctx, tx).GetByIDs(graphID, ids)
		if err != nil {
			return err
		}
		byID := make(map[string]model.MemoryNode, len(nodes))
		for _, node := range nodes {
			byID[node.NodeID] = node
		}
		for i, doc := range docs {
			node, found := byID[doc.ID]
			if !found {
				return model.ErrVersionConflict
			}
			hits = append(hits, &agent_memory.SearchHit{Node: nodeResponse(node), RelevanceScore: doc.Score(), Rank: uint32(i + 1)})
		}
		return nil
	})
	return hits, err
}
