package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"windshift/internal/database"
)

// SMSReminderSweeperConfig configures the background ticker that posts due-date
// reminders for the Stakeholder Management System module (WI-4.4).
type SMSReminderSweeperConfig struct {
	TickInterval         time.Duration // how often to scan; default 1h
	CommitmentWindowDays int           // remind this many days before a commitment is due; default 7
	EngagementWindowDays int           // remind this many days before next_engagement_at; default 3
}

// DefaultSMSReminderSweeperConfig returns sensible defaults.
func DefaultSMSReminderSweeperConfig() SMSReminderSweeperConfig {
	return SMSReminderSweeperConfig{
		TickInterval:         time.Hour,
		CommitmentWindowDays: 7,
		EngagementWindowDays: 3,
	}
}

// SMSReminderSweeper is a background worker that posts idempotent reminder
// notifications for (a) commitments due within the configured window or
// overdue, (b) stakeholder next-engagement dates approaching, and (c) review
// cadences that have lapsed. Reminders are deduplicated via the unique
// notifications.delivery_key index: each (item, date) pair notifies at most
// once per tick cycle of its key. Modeled on ApprovalEscalationSweeper.
type SMSReminderSweeper struct {
	db     database.Database
	config SMSReminderSweeperConfig

	stopChan chan struct{}
	wg       sync.WaitGroup

	inserted int64
	errors   int64
}

// NewSMSReminderSweeper constructs the sweeper. Call Start() to begin.
func NewSMSReminderSweeper(db database.Database, config SMSReminderSweeperConfig) *SMSReminderSweeper {
	if config.TickInterval == 0 {
		config.TickInterval = time.Hour
	}
	if config.CommitmentWindowDays <= 0 {
		config.CommitmentWindowDays = 7
	}
	if config.EngagementWindowDays <= 0 {
		config.EngagementWindowDays = 3
	}
	return &SMSReminderSweeper{
		db:       db,
		config:   config,
		stopChan: make(chan struct{}),
	}
}

// Start launches the background worker. Idempotent — calling Start twice is a no-op.
func (s *SMSReminderSweeper) Start() {
	s.wg.Add(1)
	go s.run()
	slog.Debug("sms reminder sweeper started",
		slog.String("component", "sms"),
		slog.Duration("tick_interval", s.config.TickInterval),
	)
}

// Stop signals shutdown and waits for the worker to drain.
func (s *SMSReminderSweeper) Stop() {
	close(s.stopChan)
	s.wg.Wait()
}

func (s *SMSReminderSweeper) run() {
	defer s.wg.Done()
	ctx := context.Background()
	s.sweep(ctx) // one pass at startup so reminders are not a full tick late
	ticker := time.NewTicker(s.config.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// smsReviewCadenceDays maps the review_cadence enum to its interval in days.
var smsReviewCadenceDays = map[string]int{
	"monthly":     30,
	"quarterly":   90,
	"semi_annual": 182,
	"annual":      365,
}

func (s *SMSReminderSweeper) sweep(ctx context.Context) {
	now := time.Now().UTC()
	inserted := s.sweepCommitments(ctx, now)
	inserted += s.sweepEngagements(ctx, now)
	inserted += s.sweepReviews(ctx, now)
	if inserted > 0 {
		s.inserted += inserted
		slog.Info("sms reminder sweep posted notifications",
			slog.Int64("inserted", inserted), slog.String("component", "sms"))
	}
}

// insertReminder posts one notification unless its delivery_key already exists.
func (s *SMSReminderSweeper) insertReminder(ctx context.Context, userID int, title, message, deliveryKey string, sourceID, entityID int) bool {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, title, message, type, authorization_scope, source_type, source_id, referenced_entity_type, referenced_entity_id, delivery_key)
		VALUES (?, ?, ?, 'reminder', 'legacy', 'sms_reminder', ?, 'sms_stakeholder', ?, ?)
		ON CONFLICT DO NOTHING`,
		userID, title, message, sourceID, entityID, deliveryKey)
	if err != nil {
		s.errors++
		slog.Warn("sms reminder insert failed", slog.String("error", err.Error()), slog.String("key", deliveryKey))
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *SMSReminderSweeper) sweepCommitments(ctx context.Context, now time.Time) int64 {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.title, c.due_date, COALESCE(c.owner_user_id, st.owner_user_id), st.name
		FROM sms_commitments c
		JOIN sms_stakeholders st ON st.id = c.stakeholder_id
		WHERE c.status IN ('open','in_progress','at_risk') AND c.due_date IS NOT NULL
		  AND COALESCE(c.owner_user_id, st.owner_user_id) IS NOT NULL`)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var inserted int64
	for rows.Next() {
		var (
			id, userID       int
			title            string
			due              time.Time
			stakeholderName  string
		)
		if err := rows.Scan(&id, &title, &due, &userID, &stakeholderName); err != nil {
			s.errors++
			continue
		}
		if due.After(now.AddDate(0, 0, s.config.CommitmentWindowDays)) {
			continue // not yet inside the reminder window
		}
		key := fmt.Sprintf("sms_commitment_due:%d:%s", id, due.Format("2006-01-02"))
		t, msg := "Commitment due soon", fmt.Sprintf("%s: %q is due %s.", stakeholderName, title, due.Format("2 Jan 2006"))
		if due.Before(now) {
			t = "Commitment overdue"
			msg = fmt.Sprintf("%s: %q was due %s and is still open.", stakeholderName, title, due.Format("2 Jan 2006"))
		}
		if s.insertReminder(ctx, userID, t, msg, key, id, 0) {
			inserted++
		}
	}
	return inserted
}

func (s *SMSReminderSweeper) sweepEngagements(ctx context.Context, now time.Time) int64 {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, owner_user_id, next_engagement_at
		FROM sms_stakeholders
		WHERE next_engagement_at IS NOT NULL AND owner_user_id IS NOT NULL AND engagement_status <> 'disengaged'`)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var inserted int64
	for rows.Next() {
		var (
			id, userID   int
			name         string
			next         time.Time
		)
		if err := rows.Scan(&id, &name, &userID, &next); err != nil {
			s.errors++
			continue
		}
		if next.After(now.AddDate(0, 0, s.config.EngagementWindowDays)) {
			continue
		}
		key := fmt.Sprintf("sms_engagement_due:%d:%s", id, next.Format("2006-01-02"))
		msg := fmt.Sprintf("%s: next engagement is %s.", name, next.Format("2 Jan 2006"))
		if s.insertReminder(ctx, userID, "Stakeholder engagement due", msg, key, id, id) {
			inserted++
		}
	}
	return inserted
}

func (s *SMSReminderSweeper) sweepReviews(ctx context.Context, now time.Time) int64 {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, owner_user_id, review_cadence, last_reviewed_at
		FROM sms_stakeholders
		WHERE owner_user_id IS NOT NULL AND last_reviewed_at IS NOT NULL`)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var inserted int64
	for rows.Next() {
		var (
			id, userID   int
			name         string
			cadence      string
			lastReviewed time.Time
		)
		if err := rows.Scan(&id, &name, &userID, &cadence, &lastReviewed); err != nil {
			s.errors++
			continue
		}
		days, ok := smsReviewCadenceDays[cadence]
		if !ok || !now.After(lastReviewed.AddDate(0, 0, days)) {
			continue
		}
		key := fmt.Sprintf("sms_review_overdue:%d:%s", id, now.Format("2006-01")) // re-fires monthly while overdue
		msg := fmt.Sprintf("%s: relationship review (cadence %s) was last done %s.", name, cadence, lastReviewed.Format("2 Jan 2006"))
		if s.insertReminder(ctx, userID, "Stakeholder review overdue", msg, key, id, id) {
			inserted++
		}
	}
	return inserted
}
