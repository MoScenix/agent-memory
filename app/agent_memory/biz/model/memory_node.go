package model

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemoryNode struct {
	gorm.Model

	// NodeID is also the primary key of the corresponding Milvus entity.
	NodeID             string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uk_node_id;index:uk_node_graph_node,unique,priority:2;index:idx_node_latest_eval,priority:2"`
	GraphID            string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_node_graph_node,unique,priority:1;index:idx_node_latest_eval,priority:1;index:idx_node_current_confidence,priority:1"`
	Content            string     `gorm:"type:mediumtext;not null"`
	Confidence         float64    `gorm:"type:double;not null;index:idx_node_current_confidence,priority:3"`
	IsValid            bool       `gorm:"type:boolean;not null;default:true;index:idx_node_current_confidence,priority:2"`
	ValidFrom          *time.Time `gorm:"type:datetime(6)"`
	FixTime            *time.Time `gorm:"type:datetime(6)"`
	ExpiresAt          *time.Time `gorm:"type:datetime(6)"`
	LatestEvaluationID *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;index:idx_node_latest_eval,priority:3"`
	Version            uint64     `gorm:"type:bigint unsigned;not null;default:1"`

	Graph            *MemoryGraph             `gorm:"belongsTo:MemoryGraph;foreignKey:GraphID;references:GraphID;constraint:fk_node_graph,"`
	LatestEvaluation *MemoryEvaluationHistory `gorm:"belongsTo:MemoryEvaluationHistory;foreignKey:GraphID,NodeID,LatestEvaluationID;references:GraphID,NodeID,EvaluationID;constraint:fk_node_latest_eval,"`
}

func (MemoryNode) TableName() string {
	return "memory_node"
}

type MemoryNodeQuery struct {
	db *gorm.DB
}

func NewMemoryNodeQuery(ctx context.Context, db *gorm.DB) *MemoryNodeQuery {
	return &MemoryNodeQuery{db: db.WithContext(ctx)}
}

func (q *MemoryNodeQuery) Create(node *MemoryNode) error {
	return q.db.Omit(clause.Associations).Create(node).Error
}

func (q *MemoryNodeQuery) GetByID(graphID, nodeID string) (MemoryNode, error) {
	var node MemoryNode
	err := q.db.Where("graph_id = ? AND node_id = ?", graphID, nodeID).Take(&node).Error
	return node, err
}

// GetForSync resolves the globally unique node ID carried by an outbox event.
func (q *MemoryNodeQuery) GetForSync(nodeID string) (MemoryNode, error) {
	var node MemoryNode
	err := q.db.Select("memory_node.*").
		Joins("JOIN memory_graph AS graph ON graph.graph_id = memory_node.graph_id AND graph.deleted_at IS NULL").
		Where("memory_node.node_id = ?", nodeID).Take(&node).Error
	return node, err
}

func (q *MemoryNodeQuery) GetByIDs(graphID string, nodeIDs []string) ([]MemoryNode, error) {
	var nodes []MemoryNode
	err := q.db.Where("graph_id = ? AND node_id IN ?", graphID, nodeIDs).Find(&nodes).Error
	return nodes, err
}

// SetInitialEvaluation links the first evaluation in the same transaction as Create.
func (q *MemoryNodeQuery) SetInitialEvaluation(graphID, nodeID, evaluationID string) error {
	result := q.db.Model(&MemoryNode{}).
		Where("graph_id = ? AND node_id = ? AND version = 1 AND latest_evaluation_id IS NULL", graphID, nodeID).
		Update("latest_evaluation_id", evaluationID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

type NodeValidity struct {
	IsValid   bool
	FixTime   *time.Time
	ExpiresAt *time.Time
}

// ApplyEvaluation advances the score, version and chain head in the same
// transaction as the evaluation insert. Pass nil validity for a score-only update.
func (q *MemoryNodeQuery) ApplyEvaluation(evaluation MemoryEvaluationHistory, validity *NodeValidity) error {
	if evaluation.NodeVersion <= 1 || evaluation.PreviousEvaluationID == nil {
		return fmt.Errorf("applying a node evaluation requires a successor version and predecessor")
	}
	updates := map[string]any{
		"confidence":           evaluation.Confidence,
		"latest_evaluation_id": evaluation.EvaluationID,
		"version":              evaluation.NodeVersion,
	}
	if validity != nil {
		updates["is_valid"] = validity.IsValid
		updates["fix_time"] = validity.FixTime
		updates["expires_at"] = validity.ExpiresAt
	}
	result := q.db.Model(&MemoryNode{}).
		Where("graph_id = ? AND node_id = ? AND version = ? AND latest_evaluation_id = ?",
			evaluation.GraphID, evaluation.NodeID, evaluation.NodeVersion-1, *evaluation.PreviousEvaluationID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

type NodeCandidateFilter struct {
	At                time.Time
	Until             time.Time
	MinimumConfidence *float64
	Historical        bool
	SyncedOnly        bool
}

// ListCandidateIDs returns up to limit candidates. Pass the search budget plus
// one as limit to detect when the eligible set exceeds the search budget.
func (q *MemoryNodeQuery) ListCandidateIDs(graphID string, filter NodeCandidateFilter, limit int) ([]string, error) {
	if filter.At.IsZero() || limit <= 0 || (!filter.Until.IsZero() && !filter.Until.After(filter.At)) {
		return nil, fmt.Errorf("candidate query requires a valid time window and positive limit")
	}
	window := TimeWindow{Start: filter.At, End: filter.Until}
	query := window.apply(q.db.Model(&MemoryNode{}).Where("graph_id = ?", graphID), "memory_node")
	if filter.Historical {
		// A correction invalidates the original fact even for historical searches.
		corrections := q.db.Unscoped().Model(&MemoryEdge{}).Select("1").
			Where("memory_edge.graph_id = memory_node.graph_id").
			Where("memory_edge.previous_node_id = memory_node.node_id").
			Where("relation_type = ?", RelationTypeCorrection)
		query = query.Where("(is_valid = ? OR fix_time IS NOT NULL)", true).
			Where("NOT EXISTS (?)", corrections)
	} else {
		query = query.Where("is_valid = ?", true)
	}
	if filter.MinimumConfidence != nil {
		query = query.Where("confidence >= ?", *filter.MinimumConfidence)
	}
	if filter.SyncedOnly {
		synced := q.db.Unscoped().Model(&MemoryOutbox{}).Select("1").
			Where("memory_outbox.node_id = memory_node.node_id AND memory_outbox.status = ?", OutboxStatusSucceeded)
		query = query.Where("EXISTS (?)", synced)
	}
	var ids []string
	err := query.Limit(limit).Pluck("node_id", &ids).Error
	return ids, err
}

// TimeWindow intersects the node's effective interval. A zero End selects a
// single point in time; a nonzero End is the exclusive upper bound.
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

func (w TimeWindow) apply(query *gorm.DB, table string) *gorm.DB {
	start := w.Start.UTC().Truncate(time.Microsecond)
	validFrom := table + ".valid_from"
	if w.End.IsZero() {
		query = query.Where("("+validFrom+" IS NULL OR "+validFrom+" <= ?)", start)
	} else {
		query = query.Where("("+validFrom+" IS NULL OR "+validFrom+" < ?)", w.End.UTC().Truncate(time.Microsecond))
	}
	fixTime, expiresAt := table+".fix_time", table+".expires_at"
	return query.Where("("+fixTime+" IS NULL OR "+fixTime+" > ?)", start).
		Where("("+expiresAt+" IS NULL OR "+expiresAt+" > ?)", start)
}
