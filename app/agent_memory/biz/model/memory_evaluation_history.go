package model

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemoryEvaluationHistory struct {
	gorm.Model

	EvaluationID         string  `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uk_eval_id;index:uk_eval_node_id,unique,priority:3"`
	GraphID              string  `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_eval_node_id,unique,priority:1;index:uk_eval_node_version,unique,priority:1;index:uk_eval_predecessor,unique,priority:1"`
	NodeID               string  `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_eval_node_id,unique,priority:2;index:uk_eval_node_version,unique,priority:2;index:uk_eval_predecessor,unique,priority:2"`
	PreviousEvaluationID *string `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;index:uk_eval_predecessor,unique,priority:3"`
	NodeVersion          uint64  `gorm:"type:bigint unsigned;not null;index:uk_eval_node_version,unique,priority:3"`
	Confidence           float64 `gorm:"type:double;not null"`
	Reason               string  `gorm:"type:text;not null"`

	Node               *MemoryNode              `gorm:"belongsTo:MemoryNode;foreignKey:GraphID,NodeID;references:GraphID,NodeID;constraint:fk_eval_node,"`
	PreviousEvaluation *MemoryEvaluationHistory `gorm:"belongsTo:MemoryEvaluationHistory;foreignKey:GraphID,NodeID,PreviousEvaluationID;references:GraphID,NodeID,EvaluationID;constraint:fk_eval_previous,"`
}

func (MemoryEvaluationHistory) TableName() string {
	return "memory_evaluation_history"
}

type MemoryEvaluationHistoryQuery struct {
	db *gorm.DB
}

func NewMemoryEvaluationHistoryQuery(ctx context.Context, db *gorm.DB) *MemoryEvaluationHistoryQuery {
	return &MemoryEvaluationHistoryQuery{db: db.WithContext(ctx)}
}

func (q *MemoryEvaluationHistoryQuery) Create(evaluation *MemoryEvaluationHistory) error {
	if evaluation.NodeVersion == 0 {
		return fmt.Errorf("evaluation version must be positive")
	}
	if (evaluation.PreviousEvaluationID == nil) != (evaluation.NodeVersion == 1) {
		return fmt.Errorf("only the first evaluation has no predecessor")
	}
	return q.db.Omit(clause.Associations).Create(evaluation).Error
}

// ListByNode resolves the cursor within the node, including deleted cursors, and
// returns visible records older than it. Callers can request limit plus one for pagination.
func (q *MemoryEvaluationHistoryQuery) ListByNode(graphID, nodeID, beforeID string, limit int) ([]MemoryEvaluationHistory, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("evaluation history limit must be positive")
	}
	query := q.db.Where("graph_id = ? AND node_id = ?", graphID, nodeID)
	if beforeID != "" {
		var cursor MemoryEvaluationHistory
		err := q.db.Unscoped().Select("node_version").
			Where("graph_id = ? AND node_id = ? AND evaluation_id = ?", graphID, nodeID, beforeID).
			Take(&cursor).Error
		if err != nil {
			return nil, err
		}
		query = query.Where("node_version < ?", cursor.NodeVersion)
	}
	var evaluations []MemoryEvaluationHistory
	err := query.Order("node_version DESC").Limit(limit).Find(&evaluations).Error
	return evaluations, err
}
