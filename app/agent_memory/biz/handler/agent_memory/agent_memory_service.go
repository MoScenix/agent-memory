package agent_memory

import (
	"context"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/service"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"
	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"google.golang.org/protobuf/encoding/protojson"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// Health .
// @router /healthz [GET]
func Health(ctx context.Context, c *app.RequestContext) {
	var req emptypb.Empty
	if err := c.BindAndValidate(&req); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewHealthService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// CreateGraph .
// @router /graphs [POST]
func CreateGraph(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.CreateGraphRequest{Body: &agent_memory.CreateGraphRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewCreateGraphService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// GetGraph .
// @router /graphs/:graph_id [GET]
func GetGraph(ctx context.Context, c *app.RequestContext) {
	var req agent_memory.GetGraphRequest
	if err := c.BindAndValidate(&req); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewGetGraphService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// AddFact .
// @router /graphs/:graph_id/facts [POST]
func AddFact(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.AddFactRequest{GraphId: c.Param("graph_id"), Body: &agent_memory.AddFactRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewAddFactService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// ChangeFact .
// @router /graphs/:graph_id/fact-changes [POST]
func ChangeFact(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.ChangeFactRequest{GraphId: c.Param("graph_id"), Body: &agent_memory.ChangeFactRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewChangeFactService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// GetNode .
// @router /graphs/:graph_id/nodes/:node_id [GET]
func GetNode(ctx context.Context, c *app.RequestContext) {
	var req agent_memory.GetNodeRequest
	if err := c.BindAndValidate(&req); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewGetNodeService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// EvaluateNode .
// @router /graphs/:graph_id/nodes/:node_id/evaluations [POST]
func EvaluateNode(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.EvaluateNodeRequest{GraphId: c.Param("graph_id"), NodeId: c.Param("node_id"), Body: &agent_memory.EvaluateNodeRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewEvaluateNodeService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// TraceRelations .
// @router /graphs/:graph_id/relations:trace [POST]
func TraceRelations(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.TraceRelationsRequest{GraphId: c.Param("graph_id"), Body: &agent_memory.TraceRelationsRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewTraceRelationsService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// SearchFacts .
// @router /graphs/:graph_id/facts:search [POST]
func SearchFacts(ctx context.Context, c *app.RequestContext) {
	req := agent_memory.SearchFactsRequest{GraphId: c.Param("graph_id"), Body: &agent_memory.SearchFactsRequest_Body{}}
	if err := protojson.Unmarshal(c.Request.Body(), req.Body); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewSearchFactsService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// GetSyncEvent .
// @router /sync-events/:event_id [GET]
func GetSyncEvent(ctx context.Context, c *app.RequestContext) {
	var req agent_memory.GetSyncEventRequest
	if err := c.BindAndValidate(&req); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewGetSyncEventService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}

// ListSyncEventsByChange .
// @router /changes/:change_id/sync-events [GET]
func ListSyncEventsByChange(ctx context.Context, c *app.RequestContext) {
	var req agent_memory.ListSyncEventsByChangeRequest
	if err := c.BindAndValidate(&req); err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusBadRequest, err)
		return
	}

	resp, err := service.NewListSyncEventsByChangeService(ctx, c).Run(&req)
	if err != nil {
		utils.SendErrResponse(ctx, c, consts.StatusInternalServerError, err)
		return
	}

	utils.SendSuccessResponse(ctx, c, consts.StatusOK, resp)
}
