package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"

	agent_memory "github.com/MoScenix/agent-memory/app/agent_memory/hertz_gen/agent_memory"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type CreateGraphService struct {
	RequestContext *app.RequestContext
	Context        context.Context
}

func NewCreateGraphService(Context context.Context, RequestContext *app.RequestContext) *CreateGraphService {
	return &CreateGraphService{RequestContext: RequestContext, Context: Context}
}

func (h *CreateGraphService) Run(req *agent_memory.CreateGraphRequest) (*agent_memory.CreateGraphResponse, error) {
	body := req.GetBody()
	userID, key := body.GetUserId(), body.GetIdempotencyKey()
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, badRequest("idempotency_key must contain 1 to 128 UTF-8 bytes")
	}
	db, err := database(h.Context)
	if err != nil {
		return nil, err
	}
	graphs := model.NewMemoryGraphQuery(h.Context, db)
	graph, err := graphs.GetByIdempotencyKey(userID, []byte(key))
	if err == nil {
		return createdGraphResponse(graph)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	graphID, err := newID()
	if err != nil {
		return nil, err
	}
	graph = model.MemoryGraph{GraphID: graphID, UserID: userID, CreateIdempotencyKey: []byte(key), Version: 1}
	if err := graphs.Create(&graph); err != nil {
		var duplicate *mysql.MySQLError
		if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
			return nil, err
		}
		existing, lookupErr := graphs.GetByIdempotencyKey(userID, []byte(key))
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		return createdGraphResponse(existing)
	}
	return createdGraphResponse(graph)
}

func createdGraphResponse(graph model.MemoryGraph) (*agent_memory.CreateGraphResponse, error) {
	if graph.DeletedAt.Valid {
		return nil, &utils.HTTPError{Status: http.StatusConflict, Message: "idempotency_key belongs to a deleted graph"}
	}
	graph.Version = 1
	return &agent_memory.CreateGraphResponse{Graph: graphResponse(graph)}, nil
}
