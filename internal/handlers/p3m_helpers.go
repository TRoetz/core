package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/logger"
	"windshift/internal/models"
	"windshift/internal/utils"
)

// p3mAudit records one committed P3M mutation in the audit log (BRD TEC-004).
// Decisions and approvals are audited first, then register mutations. details
// should carry previous/new values for changed fields; the audit query API
// renders them as-is. Failures are logged but never break the request — the
// mutation itself already committed.
func (h *P3MHandler) p3mAudit(r *http.Request, actor *models.User, actionType, resourceType string, resourceID *int, resourceName string, details map[string]any) {
	if h == nil || h.db == nil || actor == nil {
		return
	}
	_ = logger.LogAudit(h.db, logger.AuditEvent{
		UserID:       actor.ID,
		Username:     actor.Username,
		IPAddress:    utils.GetClientIP(r),
		UserAgent:    r.UserAgent(),
		ActionType:   actionType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceName: resourceName,
		Details:      details,
		Success:      true,
	})
}

// p3mAuditDiff builds an audit details payload containing only the fields
// whose values changed. Each entry is [name, oldValue, newValue]; unchanged
// entries are dropped so the log stays compact.
func p3mAuditDiff(fields ...[3]any) map[string]any {
	details := make(map[string]any, len(fields))
	for _, f := range fields {
		name, oldV, newV := fmt.Sprint(f[0]), fmt.Sprint(f[1]), fmt.Sprint(f[2])
		if name != "" && oldV != newV {
			details[name] = map[string]any{"old": f[1], "new": f[2]}
		}
	}
	return details
}

// isNotFound reports whether a database row lookup came back empty.
func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// parseQueryInt parses an optional integer query parameter.
func parseQueryInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// nullInt renders a nullable int column argument: nil pointer binds NULL.
func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullIntStr renders a nullable int for display in audit resource names; nil
// becomes "-".
func nullIntStr(v *int) string {
	if v == nil {
		return "-"
	}
	return strconv.Itoa(*v)
}

// p3mNullStr dereferences a nullable string for audit diffs: fmt.Sprint of a
// pointer compares addresses, so the value must be unwrapped before p3mAuditDiff.
func p3mNullStr(v *string) any {
	if v == nil {
		return ""
	}
	return *v
}

// nullTimeStr formats a nullable time for audit diffs; nil becomes "".
func nullTimeStr(v *time.Time) any {
	if v == nil {
		return ""
	}
	return v.Format("2006-01-02")
}

// nullFloatPtr renders a nullable float64 value (nil stays nil); used for SQL
// arguments and audit details where fmt.Sprint of a pointer would print an address.
func nullFloatPtr(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullTime converts an optional time.Time request field to a nullable column
// value; the zero time means "not provided".
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// nullTimePtr renders a *time.Time as an SQL argument (nil stays NULL).
func nullTimePtr(v *time.Time) any {
	return v
}

// joinPlaceholders joins IN-list placeholders with commas.
func joinPlaceholders(parts []string) string {
	return strings.Join(parts, ", ")
}

// questionPlaceholders returns a comma-separated list of n "?" placeholders
// for IN (...) clauses. (joinPlaceholders(make([]string, n)) would join n
// EMPTY strings and produce a syntax error.)
func questionPlaceholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// qualifyColumns prefixes every column in an unqualified comma-separated
// list with alias so the list stays safe inside multi-table SELECTs where
// shared names (created_at, updated_at) would otherwise be ambiguous.
func qualifyColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// p3mValidRag reports whether s is one of the three RAG states.
func p3mValidRag(s string) bool {
	switch s {
	case models.P3MRagRed, models.P3MRagAmber, models.P3MRagGreen:
		return true
	default:
		return false
	}
}

// boolToInt renders a boolean as the 0/1 integer SQLite stores in flag columns.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// p3mStageRow is the raw scan shape for p3m_lifecycle_stages (integer flags).
type p3mStageRow struct {
	ID           int
	StageKey     string
	Name         string
	Description  string
	SortOrder    int
	IsEntry      int
	IsExit       int
	GateRequired int
	Skippable    int
	Enabled      int
}

func (s p3mStageRow) model() models.P3MLifecycleStage {
	return models.P3MLifecycleStage{
		ID: s.ID, StageKey: s.StageKey, Name: s.Name, Description: s.Description, SortOrder: s.SortOrder,
		IsEntry: s.IsEntry == 1, IsExit: s.IsExit == 1, GateRequired: s.GateRequired == 1, Skippable: s.Skippable == 1, Enabled: s.Enabled == 1,
	}
}

// loadP3MLifecycleStages returns every lifecycle stage in path order.
func (h *P3MHandler) loadP3MLifecycleStages(ctx context.Context) ([]models.P3MLifecycleStage, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled FROM p3m_lifecycle_stages ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stages := make([]models.P3MLifecycleStage, 0)
	for rows.Next() {
		var s p3mStageRow
		if err := rows.Scan(&s.ID, &s.StageKey, &s.Name, &s.Description, &s.SortOrder, &s.IsEntry, &s.IsExit, &s.GateRequired, &s.Skippable, &s.Enabled); err != nil {
			return nil, err
		}
		stages = append(stages, s.model())
	}
	return stages, rows.Err()
}

// loadEnabledLifecycleStages returns only the enabled stages in path order.
func (h *P3MHandler) loadEnabledLifecycleStages(ctx context.Context) ([]models.P3MLifecycleStage, error) {
	all, err := h.loadP3MLifecycleStages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.P3MLifecycleStage, 0, len(all))
	for _, s := range all {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

// p3mStagePosition returns the index of key in an ordered stage list, or -1.
func p3mStagePosition(stages []models.P3MLifecycleStage, key string) int {
	for i, s := range stages {
		if s.StageKey == key {
			return i
		}
	}
	return -1
}

// validateEnabledStageKey checks that key names an existing enabled lifecycle
// stage. On failure it writes a 400 listing the valid keys and returns false.
func (h *P3MHandler) validateEnabledStageKey(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) bool {
	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return false
	}
	if p3mStagePosition(stages, key) < 0 {
		keys := make([]string, len(stages))
		for i, s := range stages {
			keys[i] = s.StageKey
		}
		respondValidationError(w, r, "unknown stage "+key+" (enabled stages: "+strings.Join(keys, ", ")+")")
		return false
	}
	return true
}

// p3mMandatoryTemplatesFor returns the enabled mandatory gate templates
// targeting toKey that apply to workType (a NULL template work type applies to
// every project; a typed template only to matching projects).
func (h *P3MHandler) p3mMandatoryTemplatesFor(ctx context.Context, toKey string, workType *string) ([]models.P3MStageGateTemplate, error) {
	query := `SELECT ` + p3mStageGateTemplateColumns + ` WHERE enabled = 1 AND mandatory = 1 AND to_stage_key = ?`
	args := []any{toKey}
	if workType != nil {
		query += ` AND (work_type IS NULL OR work_type = ?)`
		args = append(args, *workType)
	} else {
		query += ` AND work_type IS NULL`
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]models.P3MStageGateTemplate, 0)
	for rows.Next() {
		t, err := scanP3MStageGateTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// p3mSkippedStages returns the set of stage keys with a recorded skip for P.
func (h *P3MHandler) p3mSkippedStages(ctx context.Context, projectID int) (map[string]bool, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT stage_key FROM p3m_project_stage_skips WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// p3mEntrySatisfied reports whether a gate from fromKey into toKey satisfies
// the mandatory entry requirements for project P: an applicable mandatory
// template matches the transition (from anywhere or exactly fromKey), or every
// enabled stage strictly between the two is skippable and carries a recorded
// skip for P. Backward moves are always free, as are stages with no applicable
// mandatory templates.
func (h *P3MHandler) p3mEntrySatisfied(ctx context.Context, projectID int, stages []models.P3MLifecycleStage, templates []models.P3MStageGateTemplate, fromKey, toKey string) bool {
	fromPos := p3mStagePosition(stages, fromKey)
	toPos := p3mStagePosition(stages, toKey)
	if fromPos < 0 || toPos < 0 {
		return false
	}
	if fromPos >= toPos {
		return true // backward or same stage: no entry requirement
	}
	if len(templates) == 0 {
		return true // nothing mandatory targets this stage
	}
	for _, t := range templates {
		if t.FromStageKey == nil || *t.FromStageKey == fromKey {
			return true
		}
	}
	intermediates := stages[fromPos+1 : toPos]
	if len(intermediates) == 0 {
		return false // adjacent stages: the skip shortcut does not apply vacuously
	}
	skipped, err := h.p3mSkippedStages(ctx, projectID)
	if err != nil {
		return false
	}
	for _, s := range intermediates {
		if !s.Skippable || !skipped[s.StageKey] {
			return false
		}
	}
	return true
}

// p3mGateDecisionEntryError checks that a passing gate decision from
// fromKey into toStage is allowed for project P (template match or recorded
// skips of all intermediate stages). It returns nil when the decision may
// proceed.
func (h *P3MHandler) p3mGateDecisionEntryError(ctx context.Context, projectID int, workType *string, fromKey, toKey string) error {
	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil {
		return err
	}
	if p3mStagePosition(stages, fromKey) < 0 {
		return fmt.Errorf("the project's current stage %q is not an enabled lifecycle stage; re-route the project first", fromKey)
	}
	templates, err := h.p3mMandatoryTemplatesFor(ctx, toKey, workType)
	if err != nil {
		return err
	}
	if !h.p3mEntrySatisfied(ctx, projectID, stages, templates, fromKey, toKey) {
		return fmt.Errorf("entering %q requires a gate from an applicable stage or recorded skips of all intermediate stages", toKey)
	}
	return nil
}

// p3mStageEntryError checks direct entry into toKey (project creation or a
// direct stage update): at least one already-passed review into toKey must
// satisfy the mandatory templates, or every intermediate stage must be
// skippable with recorded skips. New projects may only start at an entry
// stage. It returns nil when entry is allowed.
func (h *P3MHandler) p3mStageEntryError(ctx context.Context, projectID int, fromKey string, workType *string, toKey string) error {
	stages, err := h.loadEnabledLifecycleStages(ctx)
	if err != nil {
		return err
	}
	toPos := p3mStagePosition(stages, toKey)
	if toPos < 0 {
		return fmt.Errorf("stage %q is not an enabled lifecycle stage", toKey)
	}
	if fromKey == "" { // project creation: no reviews exist yet.
		if stages[toPos].IsEntry {
			return nil
		}
		entryKeys := make([]string, 0)
		for _, s := range stages {
			if s.IsEntry {
				entryKeys = append(entryKeys, s.StageKey)
			}
		}
		return fmt.Errorf("new projects must start at an entry stage (%s); pass the mandatory gates to advance", strings.Join(entryKeys, ", "))
	}
	fromPos := p3mStagePosition(stages, fromKey)
	if fromPos < 0 {
		return fmt.Errorf("stage %q is not an enabled lifecycle stage", fromKey)
	}
	if fromPos >= toPos {
		return nil // backward moves are free
	}
	templates, err := h.p3mMandatoryTemplatesFor(ctx, toKey, workType)
	if err != nil {
		return err
	}
	if len(templates) == 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT from_stage FROM p3m_stage_gate_reviews WHERE project_id = ? AND to_stage = ? AND passed = 1`, projectID, toKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	froms := make([]string, 0)
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return err
		}
		froms = append(froms, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range froms {
		if h.p3mEntrySatisfied(ctx, projectID, stages, templates, f, toKey) {
			return nil
		}
	}
	return fmt.Errorf("entering %q requires a passed gate review from an applicable stage or recorded skips of all intermediate stages", toKey)
}

// p3mApplyComputedRaid fills the derived RAID fields: risk score (impact x
// probability), priority bucket and the escalation flag for project-level
// items scoring 15 or above.
func p3mApplyComputedRaid(item *models.P3MRaidItem) {
	item.RiskScore = item.ImpactScore * item.ProbabilityScore
	switch {
	case item.RiskScore >= 15:
		item.PriorityBucket = "high"
	case item.RiskScore >= 8:
		item.PriorityBucket = "medium"
	default:
		item.PriorityBucket = "low"
	}
	item.RequiresEscalation = item.EntityType == models.P3MEntityProject && item.RiskScore >= 15
}

// p3mStakeholderQuadrant maps power/interest scores (1-5) to the classic
// power-interest matrix quadrant; a score of 4 or more counts as high.
func p3mStakeholderQuadrant(power, interest int) string {
	switch {
	case power >= 4 && interest >= 4:
		return "manage_closely"
	case power >= 4:
		return "keep_satisfied"
	case interest >= 4:
		return "keep_informed"
	default:
		return "monitor"
	}
}
