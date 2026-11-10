package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Scheduler-facing P3M passes, driven by the server's hourly governance tick.
// Both are idempotent: alerts are guarded by an open-RAID check plus a per-day
// notification delivery_key; reminders by their delivery_key alone.
// ---------------------------------------------------------------------------

// RaiseProjectFinanceAlerts evaluates every enabled project-scoped finance
// alert rule for one project against its current financial position (FIN-009).
// Each breached rule raises an issue RAID draft owned by the project manager
// and a per-day deduplicated warning notification. It returns the number of
// new alerts raised this pass; projects without a manager cannot receive a
// RAID owner, so no alert is raised for them.
func (h *P3MHandler) RaiseProjectFinanceAlerts(ctx context.Context, projectID int) (int, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, name, metric, comparator, threshold FROM p3m_finance_alert_rules WHERE scope = 'project' AND project_id = ? AND enabled = 1`, projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type ruleRef struct {
		ID         int
		Name       string
		Metric     string
		Comparator string
		Threshold  float64
	}
	var rules []ruleRef
	for rows.Next() {
		var r ruleRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Metric, &r.Comparator, &r.Threshold); err != nil {
			return 0, err
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, nil
	}

	var manager sql.NullInt64
	if err := h.db.QueryRowContext(ctx, `SELECT manager_id FROM p3m_projects WHERE id = ?`, projectID).Scan(&manager); err != nil || !manager.Valid {
		return 0, nil // no owner to attach the RAID draft to; skip silently
	}

	metrics := h.p3mFinanceMetrics(ctx, `id = ?`, projectID)
	today := time.Now().UTC().Format("2006-01-02")
	raised := 0
	for _, rule := range rules {
		v, okV := metrics[rule.Metric]
		if !okV || v == nil {
			continue
		}
		if !p3mCompare(*v, rule.Comparator, rule.Threshold) {
			continue
		}

		title := "Finance alert: " + rule.Name
		var existing int
		errX := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_raid_items WHERE entity_type = 'project' AND entity_id = ? AND title = ? AND status IN ('open', 'mitigating', 'escalated')`, projectID, title).Scan(&existing)
		if errX == nil {
			continue // guard 1: an open alert for this rule already exists
		}

		desc := fmt.Sprintf("Rule %q triggered: metric %s is %.2f (threshold %s %g).", rule.Name, rule.Metric, *v, rule.Comparator, rule.Threshold)
		if _, errI := h.db.ExecContext(ctx, `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, owner_id, status) VALUES ('project', ?, 'issue', ?, ?, ?, 'open')`, projectID, title, desc, manager.Int64); errI != nil {
			return raised, errI
		}
		raised++

		key := fmt.Sprintf("p3m_finance_alert:%d:%s", rule.ID, today)
		if _, errN := h.db.ExecContext(ctx, `INSERT INTO notifications (user_id, title, message, type, authorization_scope, source_type, source_id, referenced_entity_type, referenced_entity_id, delivery_key) VALUES (?, ?, ?, 'warning', 'legacy', 'p3m_finance_alert', ?, 'p3m_project', ?, ?) ON CONFLICT DO NOTHING`, manager.Int64, "P3M finance alert", desc, rule.ID, projectID, key); errN != nil {
			return raised, errN // guard 2: per-day dedupe; the RAID above already exists and will be skipped next pass
		}
	}
	return raised, nil
}

// P3MDueBenefit identifies a benefit whose measurement period has lapsed.
type P3MDueBenefit struct {
	ID                   int
	OwnerID              int
	Title                string
	MeasurementFrequency *string
}

// DueBenefitReminders returns benefits that are due for their next benefit
// measurement (BEN-008). A benefit is due when its status is planned or
// measuring and its next expected measurement date has passed: for periodic
// frequencies the anchor is the latest completed measurement (falling back to
// the target realization date), advanced by one frequency interval; for
// one-off benefits it is the target realization date itself. Recording a new
// completion pushes the due date out, which makes repeated passes idempotent.
func (h *P3MHandler) DueBenefitReminders(ctx context.Context) ([]P3MDueBenefit, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, owner_id, title, measurement_frequency, target_realization_date FROM p3m_benefits WHERE status IN ('planned', 'measuring')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		P3MDueBenefit
		target time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var freq sql.NullString
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Title, &freq, &c.target); err != nil {
			return nil, err
		}
		if freq.Valid && freq.String != "" {
			c.MeasurementFrequency = &freq.String
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lastCompleted := map[int]time.Time{}
	mrows, err := h.db.QueryContext(ctx, `SELECT benefit_id, MAX(completed_at) FROM p3m_benefit_measurements WHERE completed_at IS NOT NULL GROUP BY benefit_id`)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var id int
		var at time.Time
		if err := mrows.Scan(&id, &at); err != nil {
			mrows.Close()
			return nil, err
		}
		lastCompleted[id] = at
	}
	if err := mrows.Err(); err != nil {
		mrows.Close()
		return nil, err
	}
	mrows.Close()

	today := time.Now().UTC()
	due := make([]P3MDueBenefit, 0)
	for _, c := range candidates {
		nextDue := c.target
		if freq := c.MeasurementFrequency; freq != nil && *freq != "one_off" {
			anchor := c.target
			if last, okL := lastCompleted[c.ID]; okL && last.After(anchor) {
				anchor = last
			}
			switch *freq {
			case "monthly":
				nextDue = anchor.AddDate(0, 1, 0)
			case "quarterly":
				nextDue = anchor.AddDate(0, 3, 0)
			case "annual":
				nextDue = anchor.AddDate(0, 0, 1)
			}
		}
		if nextDue.Before(today) {
			due = append(due, c.P3MDueBenefit)
		}
	}
	return due, nil
}
