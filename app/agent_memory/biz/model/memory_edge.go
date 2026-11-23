package model

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RelationType uint8

const (
	RelationTypeCorrection RelationType = 1
	RelationTypeUpdate     RelationType = 2
)

type MemoryEdge struct {
	gorm.Model

	EdgeID         string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uk_edge_id"`
	GraphID        string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_edge_pair,unique,priority:1;index:idx_edge_forward,priority:1"`
	NodeID         string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_edge_pair,unique,priority:3;index:idx_edge_forward,priority:2"`
	PreviousNodeID string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:uk_edge_pair,unique,priority:2;index:idx_edge_forward,priority:3"`
	RelationType   RelationType `gorm:"type:tinyint unsigned;not null"`
	Reason         string       `gorm:"type:text;not null"`
	ChangeID       string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:idx_edge_change"`
	GraphVersion   uint64       `gorm:"type:bigint unsigned;not null"`

	Node         *MemoryNode `gorm:"belongsTo:MemoryNode;foreignKey:GraphID,NodeID;references:GraphID,NodeID;constraint:fk_edge_to,"`
	PreviousNode *MemoryNode `gorm:"belongsTo:MemoryNode;foreignKey:GraphID,PreviousNodeID;references:GraphID,NodeID;constraint:fk_edge_from,"`
}

func (MemoryEdge) TableName() string {
	return "memory_edge"
}

type MemoryEdgeQuery struct {
	db *gorm.DB
}

func NewMemoryEdgeQuery(ctx context.Context, db *gorm.DB) *MemoryEdgeQuery {
	return &MemoryEdgeQuery{db: db.WithContext(ctx)}
}

func (q *MemoryEdgeQuery) CreateBatch(edges []MemoryEdge) error {
	if len(edges) == 0 {
		return nil
	}
	return q.db.Omit(clause.Associations).Create(&edges).Error
}

func (q *MemoryEdgeQuery) ListOutgoing(graphID string, nodeIDs []string, window *TimeWindow) ([]MemoryEdge, error) {
	var edges []MemoryEdge
	query := q.db.Where("memory_edge.graph_id = ? AND memory_edge.node_id IN ?", graphID, nodeIDs).
		Joins("JOIN memory_node AS current_node ON current_node.graph_id = memory_edge.graph_id AND current_node.node_id = memory_edge.node_id AND current_node.deleted_at IS NULL").
		Joins("JOIN memory_node AS previous_node ON previous_node.graph_id = memory_edge.graph_id AND previous_node.node_id = memory_edge.previous_node_id AND previous_node.deleted_at IS NULL")
	if window != nil {
		query = window.apply(query, "previous_node")
	}
	err := query.Order("memory_edge.node_id, memory_edge.previous_node_id").Find(&edges).Error
	return edges, err
}

// ListByChangeID includes deleted edges to return the original change group.
func (q *MemoryEdgeQuery) ListByChangeID(graphID, changeID string) ([]MemoryEdge, error) {
	var edges []MemoryEdge
	err := q.db.Unscoped().Where("graph_id = ? AND change_id = ?", graphID, changeID).Order("id").Find(&edges).Error
	return edges, err
}
