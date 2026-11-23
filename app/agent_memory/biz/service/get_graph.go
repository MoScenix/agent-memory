package service

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
)

type GetGraphService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewGetGraphService(Context context.Context, RequestContext *app.RequestContext) *GetGraphService {
	return &GetGraphService{RequestContext: RequestContext, Context: Context}
}

func (h *GetGraphService) Run(req *agent_memory.GetGraphRequest) (*agent_memory.GetGraphResponse, error) {
	if err := validateGraphRef(req.GetUserId(), req.GetGraphId()); err != nil {
		return nil, err
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}
	graph, err := model.NewMemoryGraphQuery(h.Context, db).GetByID(req.GetGraphId(), req.GetUserId())
	if err != nil {
		return nil, queryError(err, "graph")
	}
	return &agent_memory.GetGraphResponse{Graph: graphResponse(graph)}, nil
}
