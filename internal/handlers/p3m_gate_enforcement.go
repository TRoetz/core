package handlers

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Stage-gate enforcement hooks. DecideStageGate calls these before advancing a
// project: the first three return an error that blocks the gate with 409 when
// the condition is not met; ProposeChangeCalendarEntry records the transition
// on the change calendar as a local entry for the decision week (CHG-008).
// ---------------------------------------------------------------------------

// ChangeAssessmentBlockError (CHG-002) blocks entry into implement_deliver
// while the project's most recent change impact assessment rates overall
// impact at 4 or higher: high-impact changes must be resolved or re-assessed
// before implementation starts. A project with no recorded assessment is not
// blocked - there is nothing assessed to act on.
func (h *P3MHandler) ChangeAssessmentBlockError(ctx context.Context, projectID int) error {
	var rating int
	err := h.db.QueryRowContext(ctx, `SELECT overall_rating FROM p3m_change_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, projectID).Scan(&rating)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if rating >= 4 {
		return fmt.Errorf("the latest change impact assessment rates overall impact %d/5; resolve or re-assess before entering implement_deliver", rating)
	}
	return nil
}

// ReadinessNoGoError (CHG-006) blocks entry into implement_deliver while the
// project's most recent readiness assessment is NO-GO. A REASSESS outcome does
// not block: the gate decision itself carries that judgement.
func (h *P3MHandler) ReadinessNoGoError(ctx context.Context, projectID int) error {
	var outcome string
	err := h.db.QueryRowContext(ctx, `SELECT outcome FROM p3m_readiness_assessments WHERE entity_type = 'project' AND entity_id = ? ORDER BY assessed_at DESC, id DESC LIMIT 1`, projectID).Scan(&outcome)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if outcome == "no_go" {
		return fmt.Errorf("the latest readiness assessment is NO-GO; resolve the gaps and re-assess before entering implement_deliver")
	}
	return nil
}

// ClosureRecordBlockError (CLS-001) blocks entry into close until a closure &
// handover record has been captured for the project.
func (h *P3MHandler) ClosureRecordBlockError(ctx context.Context, projectID int) error {
	var id int
	err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_closure_records WHERE project_id = ?`, projectID).Scan(&id)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("a closure record must be captured before closing the project")
		}
		return err
	}
	return nil
}

// ProposeChangeCalendarEntry records a stage transition on the change calendar
// as a local entry covering the week of the decision. It is advisory: callers
// log and continue when it fails, so a calendar write can never block a gate.
func (h *P3MHandler) ProposeChangeCalendarEntry(ctx context.Context, entityType string, entityID int, activity string, at time.Time) (int64, error) {
	week := isoWeekString(at)
	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_change_calendar_entries (entity_type, entity_id, activity, start_week, end_week, impact_level, source, notes) VALUES (?, ?, ?, ?, ?, 3, 'local', ?)`,
		entityType, entityID, activity, week, week, "Proposed automatically from a stage-gate decision")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
