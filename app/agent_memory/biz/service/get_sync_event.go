package service

import (
	"context"
	"strconv"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
)

type GetSyncEventService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewGetSyncEventService(Context context.Context, RequestContext *app.RequestContext) *GetSyncEventService {
	return &GetSyncEventService{RequestContext: RequestContext, Context: Context}
}

func (h *GetSyncEventService) Run(req *agent_memory.GetSyncEventRequest) (*agent_memory.GetSyncEventResponse, error) {
	if err := validateUserID(req.GetUserId()); err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(req.GetEventId(), 10, strconv.IntSize)
	if err != nil || id == 0 {
		return nil, badRequest("event_id must be a positive decimal ID")
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}
	event, err := model.NewMemoryOutboxQuery(h.Context, db).GetByID(uint(id), req.GetUserId())
	if err != nil {
		return nil, queryError(err, "sync event")
	}
	return &agent_memory.GetSyncEventResponse{SyncEvent: syncResponse(event.MemoryOutbox, event.GraphID)}, nil
}
