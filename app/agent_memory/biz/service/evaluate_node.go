package service

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

type EvaluateNodeService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewEvaluateNodeService(Context context.Context, RequestContext *app.RequestContext) *EvaluateNodeService {
	return &EvaluateNodeService{RequestContext: RequestContext, Context: Context}
}

func (h *EvaluateNodeService) Run(req *agent_memory.EvaluateNodeRequest) (*agent_memory.EvaluateNodeResponse, error) {
	body, graphID, nodeID := req.GetBody(), req.GetGraphId(), req.GetNodeId()
	if err := validateGraphRef(body.GetUserId(), graphID); err != nil {
		return nil, err
	}
	if err := validateID("node_id", nodeID); err != nil {
		return nil, err
	}
	version := body.GetExpectedGraphVersion()
	if version == 0 || body.GetExpectedNodeVersion() == 0 {
		return nil, badRequest("expected_graph_version and expected_node_version are required")
	}
	if err := validateConfidence(body.GetConfidence()); err != nil {
		return nil, err
	}
	if err := validateReason(body.GetReason()); err != nil {
		return nil, err
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}

	resp := &agent_memory.EvaluateNodeResponse{}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGraph(h.Context, tx, graphID, body.GetUserId(), version); err != nil {
			return err
		}
		node, err := model.NewMemoryNodeQuery(h.Context, tx).GetByID(graphID, nodeID)
		if err != nil {
			return queryError(err, "node")
		}
		if node.Version != body.GetExpectedNodeVersion() {
			return versionConflict()
		}
		evaluation, err := reviseFact(h.Context, tx, &node, body.GetConfidence(), body.GetReason(), nil)
		if err != nil {
			return err
		}
		if err := model.NewMemoryGraphQuery(h.Context, tx).AdvanceVersion(graphID, version); err != nil {
			return queryError(err, "graph")
		}
		resp.Node = nodeResponse(node)
		resp.Evaluation = evaluationResponse(evaluation)
		resp.GraphVersion = version + 1
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
