package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
)

type FactIndex interface {
	Search(ctx context.Context, graphID, query string, candidateIDs []string, topK int) ([]*schema.Document, error)
	SearchSemantic(ctx context.Context, graphID, query string, candidateIDs []string, topK int) ([]*schema.Document, error)
}

type Dependencies struct {
	DB    *gorm.DB
	Index FactIndex
}

type dependenciesKey struct{}

func (deps Dependencies) Middleware(ctx context.Context, c *app.RequestContext) {
	c.Next(context.WithValue(ctx, dependenciesKey{}, deps))
}

func dependencies(ctx context.Context) Dependencies {
	deps, _ := ctx.Value(dependenciesKey{}).(Dependencies)
	return deps
}

func database(ctx context.Context) (*gorm.DB, error) {
	db := dependencies(ctx).DB
	if db == nil {
		return nil, &utils.HTTPError{Status: http.StatusServiceUnavailable, Message: "MySQL is unavailable"}
	}
	return db.WithContext(ctx), nil
}

func readSnapshot(ctx context.Context, read func(*gorm.DB) error) error {
	db, err := database(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(read, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

func lockGraph(ctx context.Context, tx *gorm.DB, graphID, userID string, expected uint64) error {
	graph, err := model.NewMemoryGraphQuery(ctx, tx).GetForUpdate(graphID, userID)
	if err != nil {
		return queryError(err, "graph")
	}
	if graph.Version != expected {
		return versionConflict()
	}
	return nil
}

func queryError(err error, resource string) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return &utils.HTTPError{Status: http.StatusNotFound, Message: resource + " not found"}
	case errors.Is(err, model.ErrVersionConflict):
		return versionConflict()
	default:
		return err
	}
}

func versionConflict() error {
	return &utils.HTTPError{Status: http.StatusConflict, Message: "version conflict"}
}
