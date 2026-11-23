package mysql

import (
	"fmt"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"gorm.io/gorm"
)

// Migrate creates missing tables and constraints for the initial schema.
func Migrate(db *gorm.DB) error {
	schema := db.Session(&gorm.Session{NewDB: true})
	schema.Config.DisableForeignKeyConstraintWhenMigrating = true
	schema = schema.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	migrator := schema.Migrator()
	tables := []any{
		&model.MemoryGraph{}, &model.MemoryNode{}, &model.MemoryEdge{},
		&model.MemoryEvaluationHistory{}, &model.MemoryOutbox{},
	}
	for _, table := range tables {
		if !migrator.HasTable(table) {
			if err := migrator.CreateTable(table); err != nil {
				return fmt.Errorf("create table for %T: %w", table, err)
			}
		}
	}
	if migrator.HasColumn(&model.MemoryEdge{}, "from_node_id") && migrator.HasColumn(&model.MemoryEdge{}, "to_node_id") {
		if err := schema.Exec(`ALTER TABLE memory_edge
			RENAME COLUMN from_node_id TO previous_node_id,
			RENAME COLUMN to_node_id TO node_id,
			RENAME INDEX idx_edge_reverse TO idx_edge_forward`).Error; err != nil {
			return fmt.Errorf("migrate memory edge direction: %w", err)
		}
	}
	constraints := []struct {
		table any
		name  string
	}{
		{&model.MemoryNode{}, "fk_node_graph"},
		{&model.MemoryEdge{}, "fk_edge_from"},
		{&model.MemoryEdge{}, "fk_edge_to"},
		{&model.MemoryEvaluationHistory{}, "fk_eval_node"},
		{&model.MemoryEvaluationHistory{}, "fk_eval_previous"},
		{&model.MemoryOutbox{}, "fk_outbox_node"},
		{&model.MemoryNode{}, "fk_node_latest_eval"},
	}
	for _, constraint := range constraints {
		if !migrator.HasConstraint(constraint.table, constraint.name) {
			if err := migrator.CreateConstraint(constraint.table, constraint.name); err != nil {
				return fmt.Errorf("create constraint %s: %w", constraint.name, err)
			}
		}
	}
	return nil
}
