package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"windshift/internal/database"
	"windshift/internal/handlers"
)

// p3mGovernanceTickInterval is how often the P3M governance tick evaluates
// financial exception alerts and posts benefit-measurement reminders. An hour
// keeps alert latency bounded without hammering the database; each pass is a
// handful of indexed queries plus idempotent inserts (delivery_key dedupe).
const p3mGovernanceTickInterval = time.Hour

// p3mGovernanceTick runs one process-wide goroutine that, on an hourly pass:
//  1. evaluates the enabled P3M financial exception rules for every project
//     and raises alerts (RAID draft) on breach —
//     (*handlers.P3MHandler).RaiseProjectFinanceAlerts,
//  2. posts a reminder notification for each benefit whose measurement period
//     has lapsed without a recorded realization or completed measurement —
//     (*handlers.P3MHandler).DueBenefitReminders.
//
// Both passes are idempotent: alerts are guarded by the open-RAID check and a
// per-day delivery_key, reminders by their delivery_key alone.
type p3mGovernanceTick struct {
	db              database.Database
	p3m             *handlers.P3MHandler
	mu              sync.Mutex
	running         bool
	stopChan        chan struct{}
	wg              sync.WaitGroup
	alertsRaised    int64
	remindersPosted int64
}

func newP3MGovernanceTick(db database.Database, p3m *handlers.P3MHandler) *p3mGovernanceTick {
	return &p3mGovernanceTick{db: db, p3m: p3m}
}

func (t *p3mGovernanceTick) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return
	}
	t.stopChan = make(chan struct{})
	t.running = true
	t.wg.Add(1)
	go t.run()
}

func (t *p3mGovernanceTick) Stop() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	t.running = false
	close(t.stopChan)
	t.mu.Unlock()
	t.wg.Wait()
}

func (t *p3mGovernanceTick) run() {
	defer t.wg.Done()
	ctx := context.Background()
	t.sweep(ctx) // one pass at startup so alerts/reminders are not an hour late
	ticker := time.NewTicker(p3mGovernanceTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.sweep(ctx)
		}
	}
}

func (t *p3mGovernanceTick) sweep(ctx context.Context) {
	rows, err := t.db.QueryContext(ctx, `SELECT id FROM p3m_projects`)
	if err != nil {
		slog.Warn("p3m governance tick: project list query failed", "error", err)
	} else {
		var ids []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		var raised int
		for _, id := range ids {
			n, err := t.p3m.RaiseProjectFinanceAlerts(ctx, id)
			if err != nil {
				slog.Warn("p3m governance tick: finance alert evaluation failed", "project_id", id, "error", err)
			} else if n > 0 {
				raised += n
			}
		}
		if raised > 0 {
			t.alertsRaised += int64(raised)
			slog.Info("p3m governance tick: finance alerts raised", "count", raised)
		}
	}
	if posted := t.postBenefitReminders(ctx); posted > 0 {
		t.remindersPosted += posted
		slog.Info("p3m governance tick: benefit measurement reminders posted", "count", posted)
	}
}

// postBenefitReminders posts one reminder notification per due benefit, deduped
// by delivery_key so each benefit notifies at most once per due date.
func (t *p3mGovernanceTick) postBenefitReminders(ctx context.Context) int64 {
	due, err := t.p3m.DueBenefitReminders(ctx)
	if err != nil {
		slog.Warn("p3m governance tick: benefit reminder query failed", "error", err)
		return 0
	}
	today := time.Now().UTC().Format("2006-01-02")
	var posted int64
	for _, b := range due {
		key := fmt.Sprintf("p3m_benefit_measurement_due:%d:%s", b.ID, today)
		freq := "periodic"
		if b.MeasurementFrequency != nil {
			freq = *b.MeasurementFrequency
		}
		res, err := t.db.ExecContext(ctx, `
			INSERT INTO notifications (user_id, title, message, type, authorization_scope, source_type, source_id, referenced_entity_type, referenced_entity_id, delivery_key)
			VALUES (?, ?, ?, 'reminder', 'legacy', 'p3m_benefit_reminder', ?, 'p3m_benefit', ?, ?)
			ON CONFLICT DO NOTHING`,
			b.OwnerID, "Benefit measurement due",
			fmt.Sprintf("%q is due for its %s benefit measurement.", b.Title, freq),
			b.ID, b.ID, key)
		if err != nil {
			slog.Warn("p3m governance tick: reminder insert failed", "error", err.Error(), "key", key)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			posted++
		}
	}
	return posted
}
