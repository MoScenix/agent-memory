package service

import (
	"context"
	"net/http"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

const maxTraceNodes = 100

type TraceRelationsService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewTraceRelationsService(Context context.Context, RequestContext *app.RequestContext) *TraceRelationsService {
	return &TraceRelationsService{RequestContext: RequestContext, Context: Context}
}

func (h *TraceRelationsService) Run(req *agent_memory.TraceRelationsRequest) (*agent_memory.TraceRelationsResponse, error) {
	body, graphID := req.GetBody(), req.GetGraphId()
	if err := validateGraphRef(body.GetUserId(), graphID); err != nil {
		return nil, err
	}
	if body.GetLimit() == 0 || body.GetLimit() > maxTraceNodes {
		return nil, badRequest("limit must be between 1 and 100")
	}
	roots, err := traceRoots(body.GetNodeIds(), int(body.GetLimit()))
	if err != nil {
		return nil, err
	}

	var cutoff *time.Time
	upper, err := businessTime("start_time", body.GetStartTime())
	if err != nil {
		return nil, err
	}
	switch condition := body.GetStopCondition().(type) {
	case *agent_memory.TraceRelationsRequest_Body_EndTime:
		cutoff, err = businessTime("end_time", condition.EndTime)
		if err != nil {
			return nil, err
		}
		if cutoff == nil {
			return nil, badRequest("end_time is required")
		}
		if upper != nil && !cutoff.Before(*upper) {
			return nil, badRequest("start_time must be later than end_time")
		}
	case *agent_memory.TraceRelationsRequest_Body_MaxDepth:
		if upper != nil {
			return nil, badRequest("start_time requires end_time")
		}
	default:
		return nil, badRequest("end_time or max_depth is required")
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	resp := &agent_memory.TraceRelationsResponse{}
	err = readSnapshot(h.Context, func(tx *gorm.DB) error {
		graph, err := model.NewMemoryGraphQuery(h.Context, tx).GetByID(graphID, body.GetUserId())
		if err != nil {
			return queryError(err, "graph")
		}
		nodeQuery := model.NewMemoryNodeQuery(h.Context, tx)
		nodes, err := nodeQuery.GetByIDs(graphID, roots)
		if err != nil {
			return err
		}
		if len(nodes) != len(roots) {
			return &utils.HTTPError{Status: http.StatusNotFound, Message: "trace start node not found"}
		}

		resp.GraphVersion = graph.Version
		window := traceWindow(cutoff, upper, nodes, now)
		edges := model.NewMemoryEdgeQuery(h.Context, tx)
		selected, err := traceOutgoing(resp, roots, int(body.GetLimit()), body.GetMaxDepth(), window,
			func(ids []string, window *model.TimeWindow) ([]model.MemoryEdge, error) {
				return edges.ListOutgoing(graphID, ids, window)
			})
		if err != nil {
			return err
		}
		if len(selected) > len(roots) {
			previous, err := nodeQuery.GetByIDs(graphID, selected[len(roots):])
			if err != nil {
				return err
			}
			nodes = append(nodes, previous...)
		}
		byID := make(map[string]model.MemoryNode, len(nodes))
		for _, node := range nodes {
			byID[node.NodeID] = node
		}
		for _, id := range selected {
			resp.Nodes = append(resp.Nodes, nodeResponse(byID[id]))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func traceWindow(cutoff, upper *time.Time, roots []model.MemoryNode, now time.Time) *model.TimeWindow {
	if cutoff == nil {
		return nil
	}
	var end time.Time
	if upper != nil {
		end = *upper
	} else {
		for _, node := range roots {
			if node.ValidFrom != nil && node.ValidFrom.After(end) {
				end = *node.ValidFrom
			}
		}
		if end.IsZero() {
			end = now
		}
	}
	if !end.After(*cutoff) {
		return nil
	}
	return &model.TimeWindow{Start: *cutoff, End: end}
}

func traceRoots(ids []string, limit int) ([]string, error) {
	if len(ids) == 0 {
		return nil, badRequest("node_ids must contain at least one node")
	}
	roots := make([]string, 0, min(len(ids), limit))
	seen := make(map[string]struct{}, cap(roots))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		if err := validateID("node_ids", id); err != nil {
			return nil, err
		}
		if len(roots) == limit {
			return nil, badRequest("distinct start nodes exceed limit")
		}
		roots = append(roots, id)
		seen[id] = struct{}{}
	}
	return roots, nil
}

func traceOutgoing(
	resp *agent_memory.TraceRelationsResponse,
	roots []string,
	limit int,
	maxDepth uint32,
	window *model.TimeWindow,
	outgoing func([]string, *model.TimeWindow) ([]model.MemoryEdge, error),
) ([]string, error) {
	selected := append(make([]string, 0, limit), roots...)
	selectedSet := make(map[string]struct{}, limit)
	for _, id := range roots {
		selectedSet[id] = struct{}{}
	}
	boundaries := make(map[string]struct{})
	frontier := roots
	for depth := uint32(0); len(frontier) > 0 && (window != nil || depth < maxDepth); depth++ {
		edges, err := outgoing(frontier, window)
		if err != nil {
			return nil, err
		}
		next := make([]string, 0, limit-len(selected))
		for _, edge := range edges {
			id := edge.PreviousNodeID
			if _, exists := selectedSet[id]; !exists {
				if len(selected) == limit {
					if _, exists := boundaries[edge.NodeID]; !exists {
						resp.UnexpandedBoundaries = append(resp.UnexpandedBoundaries, &agent_memory.TraceBoundary{NodeId: edge.NodeID, Depth: depth})
						boundaries[edge.NodeID] = struct{}{}
					}
					resp.Truncated = true
					continue
				}
				selectedSet[id] = struct{}{}
				selected = append(selected, id)
				next = append(next, id)
			}
			resp.Edges = append(resp.Edges, edgeResponse(edge))
		}
		frontier = next
	}
	return selected, nil
}
