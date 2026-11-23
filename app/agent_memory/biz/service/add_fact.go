package service

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

type AddFactService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewAddFactService(Context context.Context, RequestContext *app.RequestContext) *AddFactService {
	return &AddFactService{RequestContext: RequestContext, Context: Context}
}

func (h *AddFactService) Run(req *agent_memory.AddFactRequest) (*agent_memory.AddFactResponse, error) {
	body, graphID := req.GetBody(), req.GetGraphId()
	if err := validateGraphRef(body.GetUserId(), graphID); err != nil {
		return nil, err
	}
	version := body.GetExpectedGraphVersion()
	if version == 0 {
		return nil, badRequest("expected_graph_version is required")
	}
	node, err := prepareFact(body.GetFact())
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

	resp := &agent_memory.AddFactResponse{}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGraph(h.Context, tx, graphID, body.GetUserId(), version); err != nil {
			return err
		}
		evaluation, err := createFact(h.Context, tx, graphID, &node, body.GetFact().GetReason())
		if err != nil {
			return err
		}
		event, err := enqueueFactChange(h.Context, tx, node, changeID, version, model.OutboxEventAdd)
		if err != nil {
			return err
		}
		resp.Node = nodeResponse(node)
		resp.InitialEvaluation = evaluationResponse(evaluation)
		resp.GraphVersion = version + 1
		resp.SyncEvent = syncResponse(event, graphID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
