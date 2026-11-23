package service

import (
	"context"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type HealthService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewHealthService(Context context.Context, RequestContext *app.RequestContext) *HealthService {
	return &HealthService{RequestContext: RequestContext, Context: Context}
}

func (h *HealthService) Run(req *emptypb.Empty) (*agent_memory.HealthResponse, error) {
	return &agent_memory.HealthResponse{Status: "ok"}, nil
}
