package service

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
)

type ListSyncEventsByChangeService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewListSyncEventsByChangeService(Context context.Context, RequestContext *app.RequestContext) *ListSyncEventsByChangeService {
	return &ListSyncEventsByChangeService{RequestContext: RequestContext, Context: Context}
}

func (h *ListSyncEventsByChangeService) Run(req *agent_memory.ListSyncEventsByChangeRequest) (*agent_memory.ListSyncEventsByChangeResponse, error) {
	if err := validateUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	if err := validateID("change_id", req.GetChangeId()); err != nil {
		return nil, err
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}
	events, err := model.NewMemoryOutboxQuery(h.Context, db).ListByChangeID(req.GetChangeId(), req.GetUserId())
	if err != nil {
		return nil, err
	}
	resp := &agent_memory.ListSyncEventsByChangeResponse{SyncEvents: make([]*agent_memory.SyncEvent, 0, len(events))}
	for _, event := range events {
		resp.SyncEvents = append(resp.SyncEvents, syncResponse(event.MemoryOutbox, event.GraphID))
	}
	return resp, nil
}
