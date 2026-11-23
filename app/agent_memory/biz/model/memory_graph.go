package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemoryGraph struct {
	gorm.Model

	GraphID              string `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uk_graph_id"`
	UserID               string `gorm:"type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;not null;index:uk_graph_create_request,unique,priority:1"`
	CreateIdempotencyKey []byte `gorm:"type:varbinary(128);not null;index:uk_graph_create_request,unique,priority:2"`
	Version              uint64 `gorm:"type:bigint unsigned;not null;default:1"`
}

func (MemoryGraph) TableName() string {
	return "memory_graph"
}

var ErrVersionConflict = errors.New("memory version conflict")

type MemoryGraphQuery struct {
	db *gorm.DB
}

func NewMemoryGraphQuery(ctx context.Context, db *gorm.DB) *MemoryGraphQuery {
	return &MemoryGraphQuery{db: db.WithContext(ctx)}
}

func (q *MemoryGraphQuery) Create(graph *MemoryGraph) error {
	return q.db.Create(graph).Error
}

func (q *MemoryGraphQuery) GetByID(graphID, userID string) (MemoryGraph, error) {
	var graph MemoryGraph
	err := q.db.Where("graph_id = ? AND user_id = ?", graphID, userID).Take(&graph).Error
	return graph, err
}

// GetForUpdate holds the graph row until the caller's transaction finishes.
func (q *MemoryGraphQuery) GetForUpdate(graphID, userID string) (MemoryGraph, error) {
	var graph MemoryGraph
	err := q.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("graph_id = ? AND user_id = ?", graphID, userID).Take(&graph).Error
	return graph, err
}

// GetByIdempotencyKey includes deleted rows because their keys remain reserved.
func (q *MemoryGraphQuery) GetByIdempotencyKey(userID string, key []byte) (MemoryGraph, error) {
	var graph MemoryGraph
	err := q.db.Unscoped().Where("user_id = ? AND create_idempotency_key = ?", userID, key).
		Take(&graph).Error
	return graph, err
}

func (q *MemoryGraphQuery) AdvanceVersion(graphID string, expectedVersion uint64) error {
	result := q.db.Model(&MemoryGraph{}).
		Where("graph_id = ? AND version = ?", graphID, expectedVersion).
		Update("version", gorm.Expr("version + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}
