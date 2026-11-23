package worker

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/model"
	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
	"gorm.io/gorm"
)

const (
	dbTimeout          = 5 * time.Second
	recoveryBatchSize  = 32
	expiredLeaseReason = "worker lease expired"
)

type Outbox struct {
	db   *gorm.DB
	sync func(context.Context, model.MemoryNode) error
	cfg  conf.Outbox
}

func NewOutbox(db *gorm.DB, cfg conf.Outbox, sync func(context.Context, model.MemoryNode) error) (*Outbox, error) {
	if cfg.PollInterval <= 0 || cfg.ProcessTimeout <= 0 || cfg.LeaseDuration <= 2*dbTimeout ||
		cfg.ProcessTimeout >= cfg.LeaseDuration-2*dbTimeout {
		return nil, errors.New("outbox needs positive poll and process durations and a lease longer than process timeout plus 10s")
	}
	if cfg.MaxAttempts == 0 || cfg.RetryBase < time.Microsecond || cfg.RetryMax < cfg.RetryBase {
		return nil, errors.New("outbox needs a positive attempt limit and retry durations with 1µs <= base <= max")
	}
	return &Outbox{db: db, sync: sync, cfg: cfg}, nil
}

// Run stops claiming on cancellation and finishes the current claim within its time limits.
func (w *Outbox) Run(ctx context.Context) {
	ticks := time.NewTicker(w.cfg.PollInterval)
	defer ticks.Stop()

	var nextRecovery time.Time
	status := model.OutboxStatusPending
	for ctx.Err() == nil {
		if now := time.Now(); !now.Before(nextRecovery) {
			w.recoverExpired(ctx)
			nextRecovery = now.Add(w.cfg.PollInterval)
		}

		event, err := w.claim(ctx, status)
		if err == nil {
			status = otherReadyStatus(status)
			w.process(context.WithoutCancel(ctx), event)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) && ctx.Err() == nil {
			slog.Error("claim outbox event", "error", err)
		}
		select {
		case <-ticks.C:
		case <-ctx.Done():
			return
		}
	}
}

func otherReadyStatus(status model.OutboxStatus) model.OutboxStatus {
	if status == model.OutboxStatusPending {
		return model.OutboxStatusRetryWait
	}
	return model.OutboxStatusPending
}

func (w *Outbox) claim(parent context.Context, status model.OutboxStatus) (model.MemoryOutbox, error) {
	token, err := newLeaseToken()
	if err != nil {
		return model.MemoryOutbox{}, err
	}
	ctx, cancel := context.WithTimeout(parent, dbTimeout)
	defer cancel()
	queue := model.NewMemoryOutboxQuery(ctx, w.db)
	event, err := queue.ClaimNext(status, token, w.cfg.LeaseDuration)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queue.ClaimNext(otherReadyStatus(status), token, w.cfg.LeaseDuration)
	}
	return event, err
}

func (w *Outbox) process(parent context.Context, event model.MemoryOutbox) {
	ctx, cancel := context.WithTimeout(parent, w.cfg.ProcessTimeout)
	permanent, processErr := w.syncEvent(ctx, event)
	cancel()

	// Use a separate deadline so a processing timeout can still be recorded.
	ctx, cancel = context.WithTimeout(parent, dbTimeout)
	defer cancel()
	queue := model.NewMemoryOutboxQuery(ctx, w.db)
	token := *event.LeaseToken
	var err error
	switch {
	case processErr == nil:
		err = queue.Complete(event.ID, token)
	case permanent || event.Attempts >= w.cfg.MaxAttempts:
		err = queue.Fail(event.ID, token, processErr.Error())
		if err == nil {
			slog.Warn("outbox event failed", "event_id", event.ID, "attempts", event.Attempts, "error", processErr)
		}
	default:
		err = queue.Retry(event.ID, token, w.retryDelay(event.Attempts), processErr.Error())
	}
	if err != nil && !errors.Is(err, model.ErrOutboxLeaseLost) {
		slog.Error("record outbox result", "event_id", event.ID, "error", err)
	}
}

func (w *Outbox) syncEvent(ctx context.Context, event model.MemoryOutbox) (permanent bool, err error) {
	switch event.EventType {
	case model.OutboxEventAdd, model.OutboxEventUpdate:
	default:
		return true, fmt.Errorf("unsupported outbox event type %d", event.EventType)
	}
	node, err := model.NewMemoryNodeQuery(ctx, w.db).GetForSync(event.NodeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, errors.New("node or graph unavailable for sync")
	}
	if err != nil {
		return false, fmt.Errorf("load node for sync: %w", err)
	}
	return false, w.sync(ctx, node)
}

func (w *Outbox) recoverExpired(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, dbTimeout)
	defer cancel()
	queue := model.NewMemoryOutboxQuery(ctx, w.db)
	events, err := queue.ListExpired(recoveryBatchSize)
	if err != nil {
		if parent.Err() == nil {
			slog.Error("list expired outbox leases", "error", err)
		}
		return
	}
	for _, event := range events {
		if ctx.Err() != nil {
			return
		}
		var retryAfter *time.Duration
		if event.Attempts < w.cfg.MaxAttempts {
			delay := w.retryDelay(event.Attempts)
			retryAfter = &delay
		}
		err := queue.RecoverExpired(event.ID, *event.LeaseToken, retryAfter, expiredLeaseReason)
		switch {
		case err != nil && !errors.Is(err, model.ErrOutboxLeaseLost):
			slog.Error("recover expired outbox lease", "event_id", event.ID, "error", err)
		case err == nil && retryAfter == nil:
			slog.Warn("outbox event failed", "event_id", event.ID, "attempts", event.Attempts, "error", expiredLeaseReason)
		}
	}
}

func (w *Outbox) retryDelay(attempts uint32) time.Duration {
	delay, maxDelay := w.cfg.RetryBase, w.cfg.RetryMax
	for i := uint32(1); i < attempts && delay < maxDelay; i++ {
		delay += min(delay, maxDelay-delay)
	}
	// Jitter within the upper half of the backoff window.
	floor := delay / 2
	return floor + time.Duration(randv2.Int64N(int64(delay-floor)+1))
}

func newLeaseToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create outbox lease token: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
