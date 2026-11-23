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

type ChangeFactService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewChangeFactService(Context context.Context, RequestContext *app.RequestContext) *ChangeFactService {
	return &ChangeFactService{RequestContext: RequestContext, Context: Context}
}

func (h *ChangeFactService) Run(req *agent_memory.ChangeFactRequest) (*agent_memory.ChangeFactResponse, error) {
	body, graphID := req.GetBody(), req.GetGraphId()
	if err := validateGraphRef(body.GetUserId(), graphID); err != nil {
		return nil, err
	}
	version := body.GetExpectedGraphVersion()
	if version == 0 {
		return nil, badRequest("expected_graph_version is required")
	}
	newNode, err := prepareFact(body.GetNewFact())
	if err != nil {
		return nil, err
	}
	previous, previousIDs, err := preparePreviousChanges(body.GetPreviousNodes())
	if err != nil {
		return nil, err
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}
	changeID, err := newID()
	if err != nil {
		return nil, err
	}

	resp := &agent_memory.ChangeFactResponse{ChangeId: changeID, GraphVersion: version + 1}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGraph(h.Context, tx, graphID, body.GetUserId(), version); err != nil {
			return err
		}
		nodes, err := model.NewMemoryNodeQuery(h.Context, tx).GetByIDs(graphID, previousIDs)
		if err != nil {
			return err
		}
		if len(nodes) != len(previousIDs) {
			return &utils.HTTPError{Status: http.StatusNotFound, Message: "previous node not found"}
		}
		byID := make(map[string]model.MemoryNode, len(nodes))
		for _, node := range nodes {
			byID[node.NodeID] = node
		}
		initial, err := createFact(h.Context, tx, graphID, &newNode, body.GetNewFact().GetReason())
		if err != nil {
			return err
		}
		resp.NewNode, resp.InitialEvaluation = nodeResponse(newNode), evaluationResponse(initial)
		edges := make([]model.MemoryEdge, 0, len(previous))
		for _, change := range previous {
			node := byID[change.input.GetNodeId()]
			result, edge, err := applyPreviousChange(h.Context, tx, node, newNode.NodeID, changeID, version+1, change)
			if err != nil {
				return err
			}
			resp.PreviousNodes = append(resp.PreviousNodes, result)
			edges = append(edges, edge)
		}
		if err := model.NewMemoryEdgeQuery(h.Context, tx).CreateBatch(edges); err != nil {
			return err
		}
		event, err := enqueueFactChange(h.Context, tx, newNode, changeID, version, model.OutboxEventUpdate)
		if err != nil {
			return err
		}
		resp.SyncEvent = syncResponse(event, graphID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

type previousChange struct {
	input   *agent_memory.PreviousNodeChange
	fixTime *time.Time
}

func preparePreviousChanges(inputs []*agent_memory.PreviousNodeChange) ([]previousChange, []string, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, nil, badRequest("previous_nodes must contain 1 to 100 nodes")
	}
	changes := make([]previousChange, 0, len(inputs))
	ids := make([]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		id := input.GetNodeId()
		if err := validateID("previous_nodes.node_id", id); err != nil {
			return nil, nil, err
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, nil, badRequest("previous_nodes contains duplicate node_id")
		}
		if input.GetExpectedNodeVersion() == 0 {
			return nil, nil, badRequest("previous_nodes.expected_node_version is required")
		}
		if err := validateReason(input.GetReason()); err != nil {
			return nil, nil, err
		}
		change := previousChange{input: input}
		switch input.GetRelationType() {
		case agent_memory.RelationType_RELATION_TYPE_CORRECTION:
			if input.CorrectedConfidence == nil || input.FixTime != nil {
				return nil, nil, badRequest("CORRECTION requires corrected_confidence and accepts no fix_time")
			}
			if err := validateConfidence(input.GetCorrectedConfidence()); err != nil {
				return nil, nil, err
			}
		case agent_memory.RelationType_RELATION_TYPE_UPDATE:
			if input.FixTime == nil || input.CorrectedConfidence != nil {
				return nil, nil, badRequest("UPDATE requires fix_time and accepts no corrected_confidence")
			}
			fixTime, err := businessTime("fix_time", input.GetFixTime())
			if err != nil {
				return nil, nil, err
			}
			change.fixTime = fixTime
		default:
			return nil, nil, badRequest("previous_nodes.relation_type must be CORRECTION or UPDATE")
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		changes = append(changes, change)
	}
	return changes, ids, nil
}

func applyPreviousChange(ctx context.Context, tx *gorm.DB, node model.MemoryNode, targetID, changeID string, graphVersion uint64, change previousChange) (*agent_memory.ChangedPreviousNode, model.MemoryEdge, error) {
	input := change.input
	if node.Version != input.GetExpectedNodeVersion() {
		return nil, model.MemoryEdge{}, versionConflict()
	}
	confidence := node.Confidence
	validity := &model.NodeValidity{IsValid: false, FixTime: node.FixTime, ExpiresAt: node.ExpiresAt}
	if input.GetRelationType() == agent_memory.RelationType_RELATION_TYPE_CORRECTION {
		confidence = input.GetCorrectedConfidence()
		if confidence >= node.Confidence {
			return nil, model.MemoryEdge{}, badRequest("corrected_confidence must be lower than the current confidence")
		}
	} else {
		if node.ValidFrom != nil && !change.fixTime.After(*node.ValidFrom) {
			return nil, model.MemoryEdge{}, badRequest("fix_time must be after the previous node's valid_from")
		}
		validity.FixTime = change.fixTime
	}
	edgeID, err := newID()
	if err != nil {
		return nil, model.MemoryEdge{}, err
	}
	evaluation, err := reviseFact(ctx, tx, &node, confidence, input.GetReason(), validity)
	if err != nil {
		return nil, model.MemoryEdge{}, err
	}
	edge := model.MemoryEdge{
		EdgeID: edgeID, GraphID: node.GraphID, NodeID: targetID, PreviousNodeID: node.NodeID,
		RelationType: model.RelationType(input.GetRelationType()), Reason: input.GetReason(),
		ChangeID: changeID, GraphVersion: graphVersion,
	}
	return &agent_memory.ChangedPreviousNode{Node: nodeResponse(node), Evaluation: evaluationResponse(evaluation), Edge: edgeResponse(edge)}, edge, nil
}
