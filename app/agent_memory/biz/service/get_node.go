package service

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

type GetNodeService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewGetNodeService(Context context.Context, RequestContext *app.RequestContext) *GetNodeService {
	return &GetNodeService{RequestContext: RequestContext, Context: Context}
}

func (h *GetNodeService) Run(req *agent_memory.GetNodeRequest) (*agent_memory.GetNodeResponse, error) {
	graphID, userID, nodeID := req.GetGraphId(), req.GetUserId(), req.GetNodeId()
	if err := validateGraphRef(userID, graphID); err != nil {
		return nil, err
	}
	if err := validateID("node_id", nodeID); err != nil {
		return nil, err
	}
	if req.EvaluationBeforeId != nil {
		if err := validateID("evaluation_before_id", req.GetEvaluationBeforeId()); err != nil {
			return nil, err
		}
	}
	if req.GetEvaluationLimit() > 100 {
		return nil, badRequest("evaluation_limit must be at most 100")
	}
	limit := int(req.GetEvaluationLimit())
	if limit == 0 {
		limit = 20
	}

	resp := &agent_memory.GetNodeResponse{}
	err := readSnapshot(h.Context, func(tx *gorm.DB) error {
		if _, err := model.NewMemoryGraphQuery(h.Context, tx).GetByID(graphID, userID); err != nil {
			return queryError(err, "graph")
		}
		node, err := model.NewMemoryNodeQuery(h.Context, tx).GetByID(graphID, nodeID)
		if err != nil {
			return queryError(err, "node")
		}
		evaluations, err := model.NewMemoryEvaluationHistoryQuery(h.Context, tx).ListByNode(graphID, nodeID, req.GetEvaluationBeforeId(), limit+1)
		if err != nil {
			return queryError(err, "evaluation cursor")
		}
		resp.Node = nodeResponse(node)
		if len(evaluations) > limit {
			evaluations = evaluations[:limit]
			resp.NextEvaluationBeforeId = &evaluations[len(evaluations)-1].EvaluationID
		}
		resp.Evaluations = make([]*agent_memory.Evaluation, 0, len(evaluations))
		for _, evaluation := range evaluations {
			resp.Evaluations = append(resp.Evaluations, evaluationResponse(evaluation))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
