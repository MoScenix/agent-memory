package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OutboxEventType uint8

const (
	OutboxEventAdd    OutboxEventType = 1
	OutboxEventUpdate OutboxEventType = 2
)

type OutboxStatus uint8

const (
	OutboxStatusPending    OutboxStatus = 1
	OutboxStatusProcessing OutboxStatus = 2
	OutboxStatusRetryWait  OutboxStatus = 3
	OutboxStatusSucceeded  OutboxStatus = 4
	OutboxStatusFailed     OutboxStatus = 5
)

// MemoryOutbox tracks one node synchronization event. InnoDB appends id to the
// queue indexes, so workers can order by the scheduled time and then id.
type MemoryOutbox struct {
	gorm.Model

	NodeID    string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:idx_outbox_node"`
	ChangeID  string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uk_outbox_change"`
	EventType OutboxEventType `gorm:"type:tinyint unsigned;not null"`
	Status    OutboxStatus    `gorm:"type:tinyint unsigned;not null;default:1;index:idx_outbox_ready,priority:1;index:idx_outbox_lease,priority:1"`
	Attempts  uint32          `gorm:"type:int unsigned;not null;default:0"`
	// NextAttemptAt is set when an event is queued or scheduled for retry.
	NextAttemptAt *time.Time `gorm:"type:datetime(6);index:idx_outbox_ready,priority:2"`
	LeaseUntil    *time.Time `gorm:"type:datetime(6);index:idx_outbox_lease,priority:2"`
	LeaseToken    *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LastError     *string    `gorm:"type:text"`
	ProcessedAt   *time.Time `gorm:"type:datetime(6)"`

	Node *MemoryNode `gorm:"belongsTo:MemoryNode;foreignKey:NodeID;references:NodeID;constraint:fk_outbox_node,"`
}

func (MemoryOutbox) TableName() string {
	return "memory_outbox"
}

var ErrOutboxLeaseLost = errors.New("outbox lease lost")

type MemoryOutboxQuery struct {
	db *gorm.DB
}

func NewMemoryOutboxQuery(ctx context.Context, db *gorm.DB) *MemoryOutboxQuery {
	return &MemoryOutboxQuery{db: db.WithContext(ctx)}
}

func (q *MemoryOutboxQuery) Enqueue(nodeID, changeID string, eventType OutboxEventType) (MemoryOutbox, error) {
	var now time.Time
	if err := q.db.Raw("SELECT UTC_TIMESTAMP(6)").Row().Scan(&now); err != nil {
		return MemoryOutbox{}, err
	}
	event := MemoryOutbox{
		NodeID:        nodeID,
		ChangeID:      changeID,
		EventType:     eventType,
		Status:        OutboxStatusPending,
		NextAttemptAt: &now,
	}
	err := q.db.Create(&event).Error
	return event, err
}

type OutboxEventWithGraph struct {
	MemoryOutbox
	GraphID string
}

func (q *MemoryOutboxQuery) GetByID(id uint, userID string) (OutboxEventWithGraph, error) {
	var event OutboxEventWithGraph
	err := q.ownedEvents(userID).Where("memory_outbox.id = ?", id).Take(&event).Error
	return event, err
}

func (q *MemoryOutboxQuery) ListByChangeID(changeID, userID string) ([]OutboxEventWithGraph, error) {
	var events []OutboxEventWithGraph
	err := q.ownedEvents(userID).Where("memory_outbox.change_id = ?", changeID).
		Order("memory_outbox.id").Find(&events).Error
	return events, err
}

func (q *MemoryOutboxQuery) ownedEvents(userID string) *gorm.DB {
	return q.db.Model(&MemoryOutbox{}).Select("memory_outbox.*, node.graph_id").
		Joins("JOIN memory_node AS node ON node.node_id = memory_outbox.node_id AND node.deleted_at IS NULL").
		Joins("JOIN memory_graph AS graph ON graph.graph_id = node.graph_id AND graph.deleted_at IS NULL").
		Where("graph.user_id = ?", userID)
}

// ClaimNext uses a short transaction on the shared client. Pass one status at
// a time so the queue index can provide the ordering. The token is unique to the claim.
func (q *MemoryOutboxQuery) ClaimNext(
	status OutboxStatus, token string, lease time.Duration,
) (MemoryOutbox, error) {
	if status != OutboxStatusPending && status != OutboxStatusRetryWait {
		return MemoryOutbox{}, fmt.Errorf("invalid outbox claim status %d", status)
	}
	if token == "" || lease.Microseconds() <= 0 {
		return MemoryOutbox{}, fmt.Errorf("outbox claim requires a token and a positive lease")
	}

	var event MemoryOutbox
	err := q.db.Transaction(func(tx *gorm.DB) error {
		var candidate MemoryOutbox
		result := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_attempt_at <= UTC_TIMESTAMP(6)", status).
			Order("next_attempt_at, id").Limit(1).Find(&candidate)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		result = tx.Model(&MemoryOutbox{}).Where("id = ?", candidate.ID).Updates(map[string]any{
			"status":          OutboxStatusProcessing,
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": nil,
			"lease_token":     token,
			"lease_until":     afterDBTime(lease),
		})
		if err := leaseUpdateError(result); err != nil {
			return err
		}
		return tx.Take(&event, candidate.ID).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return MemoryOutbox{}, err
	}
	return event, nil
}

func (q *MemoryOutboxQuery) ListExpired(limit int) ([]MemoryOutbox, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("expired outbox query limit must be positive")
	}
	var events []MemoryOutbox
	err := q.db.Where("status = ? AND lease_until <= UTC_TIMESTAMP(6)", OutboxStatusProcessing).
		Order("lease_until, id").Limit(limit).Find(&events).Error
	return events, err
}

func (q *MemoryOutboxQuery) Complete(id uint, token string) error {
	return finishOutbox(q.validLease(id, token), OutboxStatusSucceeded, nil, nil)
}

func (q *MemoryOutboxQuery) Retry(id uint, token string, delay time.Duration, reason string) error {
	if delay < 0 {
		return fmt.Errorf("outbox retry delay must be non-negative")
	}
	return finishOutbox(q.validLease(id, token), OutboxStatusRetryWait, afterDBTime(delay), reason)
}

func (q *MemoryOutboxQuery) Fail(id uint, token, reason string) error {
	return finishOutbox(q.validLease(id, token), OutboxStatusFailed, nil, reason)
}

// RecoverExpired releases an expired claim. A nil delay marks the event failed.
func (q *MemoryOutboxQuery) RecoverExpired(id uint, token string, delay *time.Duration, reason string) error {
	query := q.lease(id, token).Where("lease_until <= UTC_TIMESTAMP(6)")
	if delay == nil {
		return finishOutbox(query, OutboxStatusFailed, nil, reason)
	}
	if *delay < 0 {
		return fmt.Errorf("outbox recovery delay must be non-negative")
	}
	return finishOutbox(query, OutboxStatusRetryWait, afterDBTime(*delay), reason)
}

func (q *MemoryOutboxQuery) lease(id uint, token string) *gorm.DB {
	return q.db.Model(&MemoryOutbox{}).
		Where("id = ? AND status = ? AND lease_token = ?", id, OutboxStatusProcessing, token)
}

func (q *MemoryOutboxQuery) validLease(id uint, token string) *gorm.DB {
	return q.lease(id, token).Where("lease_until > UTC_TIMESTAMP(6)")
}

func finishOutbox(query *gorm.DB, status OutboxStatus, retryAt, lastError any) error {
	var processedAt any
	if status != OutboxStatusRetryWait {
		processedAt = gorm.Expr("UTC_TIMESTAMP(6)")
	}
	return leaseUpdateError(query.Updates(map[string]any{
		"status":          status,
		"last_error":      lastError,
		"next_attempt_at": retryAt,
		"processed_at":    processedAt,
		"lease_token":     nil,
		"lease_until":     nil,
	}))
}

func afterDBTime(duration time.Duration) clause.Expr {
	return gorm.Expr("TIMESTAMPADD(MICROSECOND, ?, UTC_TIMESTAMP(6))", duration.Microseconds())
}

func leaseUpdateError(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOutboxLeaseLost
	}
	return nil
}
