// SRM monitoring phase handlers (Phase 4): performance metrics with period
// results, collaborative balanced scorecards over the five official summary
// dimensions, 360-degree pulse surveys, the innovation register (capture,
// evaluation, implementation), service improvement plans and disputes with
// the official escalation ladder plus corrective remedy plans.

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Column lists and validation helpers
// ---------------------------------------------------------------------------

const srmMetricColumns = `id, contract_id, name, type, unit, target_value, direction, frequency, data_source, owner_id, is_active, created_at`

const srmScorecardColumns = `id, contract_id, supplier_id, period_start, period_end, completed_collaboratively, overall_score, overall_reasoning, agency_completed_by, supplier_completed_by_name, status, created_at`

const srmSurveyColumns = `id, supplier_id, direction, template_version, sent_at, due_date, responses_count`

const srmInnovationColumns = `id, idea_code, contract_id, supplier_id, name, description, idea_date, supplier_team, agency_team, source, status, potential_financial_value, realised_financial_value, created_at, updated_at`

const srmSipColumns = `id, contract_id, supplier_id, title, sip_owner_id, problem_statement, subtype, success_measure_smart, exit_criteria, status, source_ref, created_at, updated_at`

const srmDisputeColumns = `id, contract_id, supplier_id, description, stage, opened_at, resolved_at, outcome_note`

func srmValidMetricType(s string) bool {
	switch s {
	case "sla", "kpi":
		return true
	default:
		return false
	}
}

func srmValidDirection(s string) bool {
	switch s {
	case "above", "below":
		return true
	default:
		return false
	}
}

func srmValidFrequency(s string) bool {
	switch s {
	case "monthly", "quarterly":
		return true
	default:
		return false
	}
}

func srmValidDataSource(s string) bool {
	switch s {
	case "manual", "erp_api", "supplier_reported":
		return true
	default:
		return false
	}
}

func srmValidRag(s string) bool {
	switch s {
	case models.SRMRagGreen, models.SRMRagAmber, models.SRMRagRed:
		return true
	default:
		return false
	}
}

// srmValidScorecardDimension accepts the five official balanced scorecard
// summary dimensions (spec §5.4).
func srmValidScorecardDimension(s string) bool {
	switch s {
	case "risk_summary", "initiatives_summary", "performance_summary", "benefits_delivered", "governance_summary":
		return true
	default:
		return false
	}
}

func srmValidSurveyDirection(s string) bool {
	switch s {
	case "internal_rates_supplier", "supplier_rates_agency":
		return true
	default:
		return false
	}
}

func srmValidInnovationSource(s string) bool {
	switch s {
	case "supplier_submission", "agency_observation", "jbp_workshop", "scorecard_review", "other":
		return true
	default:
		return false
	}
}

func srmValidInnovationStatus(s string) bool {
	switch s {
	case "submitted", "under_review", "proceed", "reject", "further_investigate", "implementing", "implemented":
		return true
	default:
		return false
	}
}

func srmValidRecommendationDecision(s string) bool {
	switch s {
	case "proceed", "reject", "further_investigate":
		return true
	default:
		return false
	}
}

func srmValidPriority(s string) bool {
	switch s {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

func srmValidTaskStatus(s string) bool {
	switch s {
	case "in_progress", "awaiting_approval", "sign_off_stage", "complete":
		return true
	default:
		return false
	}
}

func srmValidSipSubtype(s string) bool {
	switch s {
	case "breach_driven", "continuous_improvement":
		return true
	default:
		return false
	}
}

func srmValidSipStatus(s string) bool {
	switch s {
	case "draft", "active", "on_track_review", "resolved", "closed_unresolved":
		return true
	default:
		return false
	}
}

// srmValidDisputeStage accepts the official escalation ladder stages (spec §5.6).
func srmValidDisputeStage(s string) bool {
	switch s {
	case "l1_working_level", "l2_director", "l3_executive", "mediation", "litigation", "closed":
		return true
	default:
		return false
	}
}

func srmValidRemedyStatus(s string) bool {
	switch s {
	case "active", "resolved", "breached", "escalated":
		return true
	default:
		return false
	}
}

var srmSmartDateRe = regexp.MustCompile(`\d{1,4}[./-]\d{1,2}[./-]\d{2,4}`)

// srmSmartMeasureValid implements the FR-7.3.1 SMART check: a success measure
// must contain both a number and a date to be measurable and time-bound.
func srmSmartMeasureValid(s string) bool {
	hasNumber := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasNumber = true
			break
		}
	}
	return hasNumber && srmSmartDateRe.MatchString(s)
}

// srmUserExists reports whether users.id holds an account (attribution FKs).
func srmUserExists(ctx context.Context, db database.Database, userID int) bool {
	var n int
	return db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&n) == nil
}

// srmNextIdeaCode returns the next capture-stage idea ID (FR-7.2.1: auto, INN###).
func srmNextIdeaCode(ctx context.Context, db database.Database) string {
	var maxCode *string
	if err := db.QueryRowContext(ctx, `SELECT MAX(idea_code) FROM srm_innovations`).Scan(&maxCode); err != nil || maxCode == nil || *maxCode == "" {
		return "INN001"
	}
	n, err := strconv.Atoi(strings.TrimPrefix(*maxCode, "INN"))
	if err != nil {
		return "INN001"
	}
	return fmt.Sprintf("INN%03d", n+1)
}

// ---------------------------------------------------------------------------
// Performance metrics (Phase 4 / Step 4.1)
// ---------------------------------------------------------------------------

func (h *SRMHandler) ListMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT m.` + srmMetricColumns + ` FROM srm_performance_metrics m WHERE 1=1`
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("contract_id")); ok {
		query += ` AND m.contract_id = ?`
		args = append(args, v)
	}
	if active := r.URL.Query().Get("is_active"); active != "" {
		query += ` AND m.is_active = ?`
		args = append(args, active == "true")
	}
	query += ` ORDER BY m.created_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMPerformanceMetric{}
	for rows.Next() {
		var m models.SRMPerformanceMetric
		if err := rows.Scan(&m.ID, &m.ContractID, &m.Name, &m.Type, &m.Unit, &m.TargetValue, &m.Direction, &m.Frequency, &m.DataSource, &m.OwnerID, &m.IsActive, &m.CreatedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) getSRMMetric(ctx context.Context, id int) (*models.SRMPerformanceMetric, error) {
	var m models.SRMPerformanceMetric
	err := h.db.QueryRowContext(ctx, `SELECT `+srmMetricColumns+` FROM srm_performance_metrics WHERE id = ?`, id).Scan(
		&m.ID, &m.ContractID, &m.Name, &m.Type, &m.Unit, &m.TargetValue, &m.Direction, &m.Frequency, &m.DataSource, &m.OwnerID, &m.IsActive, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (h *SRMHandler) CreateMetric(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		ContractID  int     `json:"contract_id"`
		Name        string  `json:"name"`
		Type        string  `json:"type"`
		Unit        string  `json:"unit"`
		TargetValue float64 `json:"target_value"`
		Direction   string  `json:"direction"`
		Frequency   string  `json:"frequency"`
		DataSource  string  `json:"data_source"`
		OwnerID     *int    `json:"owner_id,omitempty"`
		IsActive    bool    `json:"is_active"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	if !srmValidMetricType(in.Type) {
		respondBadRequest(w, r, `type must be one of: sla, kpi`)
		return
	}
	if !srmValidDirection(in.Direction) {
		respondBadRequest(w, r, `direction must be one of: above, below`)
		return
	}
	if in.Frequency == "" {
		in.Frequency = "monthly"
	} else if !srmValidFrequency(in.Frequency) {
		respondBadRequest(w, r, `frequency must be one of: monthly, quarterly`)
		return
	}
	if in.DataSource == "" {
		in.DataSource = "manual"
	} else if !srmValidDataSource(in.DataSource) {
		respondBadRequest(w, r, `data_source must be one of: manual, erp_api, supplier_reported`)
		return
	}
	if _, err := h.srmContractSupplier(ctx, in.ContractID); isNotFound(err) {
		respondNotFound(w, r, "contract not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.OwnerID != nil && !srmUserExists(ctx, h.db, *in.OwnerID) {
		respondBadRequest(w, r, "owner_id does not match a known user")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_performance_metrics (contract_id, name, type, unit, target_value, direction, frequency, data_source, owner_id, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ContractID, in.Name, in.Type, in.Unit, in.TargetValue, in.Direction, in.Frequency, in.DataSource, nullInt(in.OwnerID), in.IsActive)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	m, err := h.getSRMMetric(ctx, int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, m)
}

func (h *SRMHandler) UpdateMetric(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name        *string  `json:"name,omitempty"`
		Type        *string  `json:"type,omitempty"`
		Unit        *string  `json:"unit,omitempty"`
		TargetValue *float64 `json:"target_value,omitempty"`
		Direction   *string  `json:"direction,omitempty"`
		Frequency   *string  `json:"frequency,omitempty"`
		DataSource  *string  `json:"data_source,omitempty"`
		OwnerID     *int     `json:"owner_id,omitempty"`
		IsActive    *bool    `json:"is_active,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	existing, err := h.getSRMMetric(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "metric not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Type != nil && !srmValidMetricType(*in.Type) {
		respondBadRequest(w, r, `type must be one of: sla, kpi`)
		return
	}
	if in.Direction != nil && !srmValidDirection(*in.Direction) {
		respondBadRequest(w, r, `direction must be one of: above, below`)
		return
	}
	if in.Frequency != nil && !srmValidFrequency(*in.Frequency) {
		respondBadRequest(w, r, `frequency must be one of: monthly, quarterly`)
		return
	}
	if in.DataSource != nil && !srmValidDataSource(*in.DataSource) {
		respondBadRequest(w, r, `data_source must be one of: manual, erp_api, supplier_reported`)
		return
	}
	if in.OwnerID != nil && !srmUserExists(ctx, h.db, *in.OwnerID) {
		respondBadRequest(w, r, "owner_id does not match a known user")
		return
	}
	targetValue := existing.TargetValue
	if in.TargetValue != nil {
		targetValue = *in.TargetValue
	}
	isActive := existing.IsActive
	if in.IsActive != nil {
		isActive = *in.IsActive
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_performance_metrics SET name = ?, type = ?, unit = ?, target_value = ?, direction = ?, frequency = ?, data_source = ?, owner_id = ?, is_active = ? WHERE id = ?`,
		orKeep(in.Name, existing.Name), orKeep(in.Type, existing.Type), orKeep(in.Unit, existing.Unit), targetValue, orKeep(in.Direction, existing.Direction), orKeep(in.Frequency, existing.Frequency), orKeep(in.DataSource, existing.DataSource), nullInt(orPtrInt(in.OwnerID, existing.OwnerID)), isActive, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	m, err := h.getSRMMetric(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, m)
}

func (h *SRMHandler) DeleteMetric(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM srm_performance_metrics WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "metric not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SRMHandler) ListMetricResults(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	metric, err := h.getSRMMetric(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "metric not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, metric_id, period_start, actual_value, achieved, recorded_by, recorded_at FROM srm_metric_results WHERE metric_id = ? ORDER BY period_start DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMMetricResult{}
	for rows.Next() {
		var res models.SRMMetricResult
		res.MetricName = metric.Name
		if err := rows.Scan(&res.ID, &res.MetricID, &res.PeriodStart, &res.ActualValue, &res.Achieved, &res.RecordedBy, &res.RecordedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) RecordMetricResult(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		PeriodStart time.Time `json:"period_start"`
		ActualValue float64   `json:"actual_value"`
		Achieved    bool      `json:"achieved"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	metric, err := h.getSRMMetric(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "metric not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_metric_results WHERE metric_id = ? AND period_start = ?`, id, in.PeriodStart).Scan(&n)
	if err == nil {
		respondConflict(w, r, "a result is already recorded for this period")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_metric_results (metric_id, period_start, actual_value, achieved, recorded_by) VALUES (?, ?, ?, ?, ?)`,
		id, in.PeriodStart, in.ActualValue, in.Achieved, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	resultID64, _ := res.LastInsertId()
	var out models.SRMMetricResult
	out.MetricName = metric.Name
	err = h.db.QueryRowContext(ctx, `SELECT id, metric_id, period_start, actual_value, achieved, recorded_by, recorded_at FROM srm_metric_results WHERE id = ?`, resultID64).Scan(
		&out.ID, &out.MetricID, &out.PeriodStart, &out.ActualValue, &out.Achieved, &out.RecordedBy, &out.RecordedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

// ---------------------------------------------------------------------------
// Collaborative balanced scorecards (Phase 4 / Step 4.3)
// ---------------------------------------------------------------------------

func (h *SRMHandler) getSRMScorecard(ctx context.Context, id int) (*models.SRMScorecard, error) {
	var s models.SRMScorecard
	err := h.db.QueryRowContext(ctx, `SELECT `+srmScorecardColumns+` FROM srm_scorecards WHERE id = ?`, id).Scan(
		&s.ID, &s.ContractID, &s.SupplierID, &s.PeriodStart, &s.PeriodEnd, &s.CompletedCollaboratively, &s.OverallScore, &s.OverallReasoning, &s.AgencyCompletedBy, &s.SupplierCompletedByName, &s.Status, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (h *SRMHandler) ListScorecards(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := "SELECT " + qualifyColumns("sc", srmScorecardColumns) + ", s.company_name AS supplier_name FROM srm_scorecards sc JOIN srm_suppliers s ON s.id = sc.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("contract_id")); ok {
		query += ` AND sc.contract_id = ?`
		args = append(args, v)
	}
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND sc.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY sc.created_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMScorecard{}
	for rows.Next() {
		var s models.SRMScorecard
		if err := rows.Scan(&s.ID, &s.ContractID, &s.SupplierID, &s.PeriodStart, &s.PeriodEnd, &s.CompletedCollaboratively, &s.OverallScore, &s.OverallReasoning, &s.AgencyCompletedBy, &s.SupplierCompletedByName, &s.Status, &s.CreatedAt, &s.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) CreateScorecard(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		ContractID               int       `json:"contract_id"`
		SupplierID               int       `json:"supplier_id"`
		PeriodStart              time.Time `json:"period_start"`
		PeriodEnd                time.Time `json:"period_end"`
		CompletedCollaboratively bool      `json:"completed_collaboratively"`
		OverallReasoning         string    `json:"overall_reasoning"`
		Status                   string    `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if in.Status == "" {
		in.Status = "open"
	} else if in.Status != "open" && in.Status != "closed" {
		respondBadRequest(w, r, `status must be one of: open, closed`)
		return
	}
	if !in.PeriodEnd.After(in.PeriodStart) {
		respondBadRequest(w, r, "period_end must be after period_start")
		return
	}
	contractSupplier, err := h.srmContractSupplier(ctx, in.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "contract not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.SupplierID != contractSupplier {
		respondBadRequest(w, r, "supplier_id does not match the contract's supplier")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_scorecards (contract_id, supplier_id, period_start, period_end, completed_collaboratively, overall_reasoning, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.ContractID, in.SupplierID, in.PeriodStart, in.PeriodEnd, in.CompletedCollaboratively, in.OverallReasoning, in.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	createdID := int(id64)
	logAuditWithDetails(h.db, r, user, "srm_scorecard_created", "srm_scorecard", &createdID, fmt.Sprintf("%d", createdID), map[string]any{"contract_id": in.ContractID})
	s, err := h.getSRMScorecard(ctx, createdID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, s)
}

// srmScorecardDimensions loads dimensions grouped by scorecard for the given ids.
func (h *SRMHandler) srmScorecardDimensions(ctx context.Context, scorecardIDs []int) (map[int][]models.SRMScorecardDimension, error) {
	out := map[int][]models.SRMScorecardDimension{}
	if len(scorecardIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(scorecardIDs))
	for i, id := range scorecardIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, scorecard_id, dimension, main_highlights, key_concerns, rag, supplier_rag, next_steps, joint_resolution_note FROM srm_scorecard_dimensions WHERE scorecard_id IN (`+joinPlaceholders(make([]string, len(scorecardIDs)))+`) ORDER BY id`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d models.SRMScorecardDimension
		if err := rows.Scan(&d.ID, &d.ScorecardID, &d.Dimension, &d.MainHighlights, &d.KeyConcerns, &d.Rag, &d.SupplierRag, &d.NextSteps, &d.JointResolutionNote); err != nil {
			return out, err
		}
		out[d.ScorecardID] = append(out[d.ScorecardID], d)
	}
	return out, rows.Err()
}

func (h *SRMHandler) GetScorecard(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	s, err := h.getSRMScorecard(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "scorecard not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	dims, err := h.srmScorecardDimensions(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	s.Dimensions = dims[id]
	respondJSONOK(w, s)
}

func (h *SRMHandler) UpdateScorecard(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		PeriodStart              *time.Time `json:"period_start,omitempty"`
		PeriodEnd                *time.Time `json:"period_end,omitempty"`
		CompletedCollaboratively *bool      `json:"completed_collaboratively,omitempty"`
		OverallScore             *int       `json:"overall_score,omitempty"`
		OverallReasoning         *string    `json:"overall_reasoning,omitempty"`
		SupplierCompletedByName  *string    `json:"supplier_completed_by_name,omitempty"`
		Status                   *string    `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	existing, err := h.getSRMScorecard(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "scorecard not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.OverallScore != nil && (*in.OverallScore < 1 || *in.OverallScore > 5) {
		respondBadRequest(w, r, "overall_score must be between 1 and 5")
		return
	}
	if in.Status != nil && *in.Status != "open" && *in.Status != "closed" {
		respondBadRequest(w, r, `status must be one of: open, closed`)
		return
	}
	start, end := existing.PeriodStart, existing.PeriodEnd
	if in.PeriodStart != nil {
		start = *in.PeriodStart
	}
	if in.PeriodEnd != nil {
		end = *in.PeriodEnd
	}
	if !end.After(start) {
		respondBadRequest(w, r, "period_end must be after period_start")
		return
	}
	closing := in.Status != nil && *in.Status == "closed" && existing.Status != "closed"
	if closing {
		var n int
		err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_scorecard_dimensions WHERE scorecard_id = ? AND supplier_rag IS NOT NULL AND supplier_rag <> rag AND joint_resolution_note = ''`, id).Scan(&n)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if n > 0 {
			respondConflict(w, r, "scorecard cannot be closed while a dimension RAG mismatch lacks a joint resolution note")
			return
		}
	}
	agencyCompletedBy := existing.AgencyCompletedBy
	if closing && agencyCompletedBy == nil {
		agencyCompletedBy = &user.ID
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_scorecards SET period_start = ?, period_end = ?, completed_collaboratively = ?, overall_score = ?, overall_reasoning = ?, supplier_completed_by_name = ?, status = ?, agency_completed_by = ? WHERE id = ?`,
		start, end, boolOr(in.CompletedCollaboratively, existing.CompletedCollaboratively), nullInt(orPtrInt(in.OverallScore, existing.OverallScore)), orKeep(in.OverallReasoning, existing.OverallReasoning), orKeep(in.SupplierCompletedByName, existing.SupplierCompletedByName), orKeep(in.Status, existing.Status), agencyCompletedBy, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAuditWithDetails(h.db, r, user, "srm_scorecard_updated", "srm_scorecard", &id, fmt.Sprintf("%d", id), map[string]any{"status": orKeep(in.Status, existing.Status)})
	s, err := h.getSRMScorecard(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	dims, err := h.srmScorecardDimensions(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	s.Dimensions = dims[id]
	respondJSONOK(w, s)
}

func (h *SRMHandler) UpsertScorecardDimension(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Dimension           string  `json:"dimension"`
		MainHighlights      string  `json:"main_highlights"`
		KeyConcerns         string  `json:"key_concerns"`
		Rag                 string  `json:"rag"`
		NextSteps           string  `json:"next_steps"`
		SupplierRag         *string `json:"supplier_rag,omitempty"`
		JointResolutionNote string  `json:"joint_resolution_note"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if !srmValidScorecardDimension(in.Dimension) {
		respondBadRequest(w, r, `dimension must be one of: risk_summary, initiatives_summary, performance_summary, benefits_delivered, governance_summary`)
		return
	}
	if in.Rag == "" {
		in.Rag = models.SRMRagGreen
	} else if !srmValidRag(in.Rag) {
		respondBadRequest(w, r, `rag must be one of: green, amber, red`)
		return
	}
	supplierRag := ""
	if in.SupplierRag != nil && *in.SupplierRag != "" {
		if !srmValidRag(*in.SupplierRag) {
			respondBadRequest(w, r, `supplier_rag must be one of: green, amber, red`)
			return
		}
		supplierRag = *in.SupplierRag
	}
	if supplierRag != "" && supplierRag != in.Rag && strings.TrimSpace(in.JointResolutionNote) == "" {
		respondBadRequest(w, r, "joint_resolution_note is required when the supplier RAG differs from the agency RAG")
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_scorecards WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "scorecard not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var dimID int
	err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_scorecard_dimensions WHERE scorecard_id = ? AND dimension = ?`, id, in.Dimension).Scan(&dimID)
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_scorecard_dimensions (scorecard_id, dimension, main_highlights, key_concerns, rag, next_steps, supplier_rag, joint_resolution_note) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.Dimension, in.MainHighlights, in.KeyConcerns, in.Rag, in.NextSteps, nullString(supplierRag), in.JointResolutionNote)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		dimID64, _ := res.LastInsertId()
		dimID = int(dimID64)
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_scorecard_dimensions SET main_highlights = ?, key_concerns = ?, rag = ?, next_steps = ?, supplier_rag = ?, joint_resolution_note = ? WHERE id = ?`,
			in.MainHighlights, in.KeyConcerns, in.Rag, in.NextSteps, nullString(supplierRag), in.JointResolutionNote, dimID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	var out models.SRMScorecardDimension
	err = h.db.QueryRowContext(ctx, `SELECT id, scorecard_id, dimension, main_highlights, key_concerns, rag, next_steps, supplier_rag, joint_resolution_note FROM srm_scorecard_dimensions WHERE id = ?`, dimID).Scan(
		&out.ID, &out.ScorecardID, &out.Dimension, &out.MainHighlights, &out.KeyConcerns, &out.Rag, &out.NextSteps, &out.SupplierRag, &out.JointResolutionNote)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ---------------------------------------------------------------------------
// 360-degree pulse surveys (Phase 4 / Step 4.3)
// ---------------------------------------------------------------------------

func (h *SRMHandler) ListPulseSurveys(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := "SELECT " + qualifyColumns("sv", srmSurveyColumns) + ", s.company_name AS supplier_name FROM srm_pulse_surveys sv JOIN srm_suppliers s ON s.id = sv.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("supplier_id")); ok {
		query += ` AND sv.supplier_id = ?`
		args = append(args, v)
	}
	query += ` ORDER BY sv.sent_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMPulseSurvey{}
	for rows.Next() {
		var s models.SRMPulseSurvey
		if err := rows.Scan(&s.ID, &s.SupplierID, &s.Direction, &s.TemplateVersion, &s.SentAt, &s.DueDate, &s.ResponsesCount, &s.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) CreatePulseSurvey(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		SupplierID      int        `json:"supplier_id"`
		Direction       string     `json:"direction"`
		TemplateVersion int        `json:"template_version"`
		DueDate         *time.Time `json:"due_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if !srmValidSurveyDirection(in.Direction) {
		respondBadRequest(w, r, `direction must be one of: internal_rates_supplier, supplier_rates_agency`)
		return
	}
	if in.TemplateVersion <= 0 {
		in.TemplateVersion = 1
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_suppliers WHERE id = ?`, in.SupplierID).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "supplier not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_pulse_surveys (supplier_id, direction, template_version, sent_at, due_date) VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		in.SupplierID, in.Direction, in.TemplateVersion, nullTimePtr(in.DueDate))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	var out models.SRMPulseSurvey
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("sv", srmSurveyColumns)+`, s.company_name AS supplier_name FROM srm_pulse_surveys sv JOIN srm_suppliers s ON s.id = sv.supplier_id WHERE sv.id = ?`, id64).Scan(
		&out.ID, &out.SupplierID, &out.Direction, &out.TemplateVersion, &out.SentAt, &out.DueDate, &out.ResponsesCount, &out.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

// srmSurveyResponses loads responses grouped by survey with resolved stakeholder names.
func (h *SRMHandler) srmSurveyResponses(ctx context.Context, surveyIDs []int) (map[int][]models.SRMSurveyResponse, error) {
	out := map[int][]models.SRMSurveyResponse{}
	if len(surveyIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(surveyIDs))
	for i, id := range surveyIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT r.id, r.survey_id, r.stakeholder_id, r.is_anonymous, r.answers_json, r.overall_score, r.responded_at, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_survey_responses r LEFT JOIN srm_stakeholders st ON st.id = r.stakeholder_id WHERE r.survey_id IN (`+joinPlaceholders(make([]string, len(surveyIDs)))+`) ORDER BY r.id`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var resp models.SRMSurveyResponse
		var storedAnswers string
		if err := rows.Scan(&resp.ID, &resp.SurveyID, &resp.StakeholderID, &resp.IsAnonymous, &storedAnswers, &resp.OverallScore, &resp.RespondedAt, &resp.StakeholderName); err != nil {
			return out, err
		}
		resp.Answers = map[string]any{}
		if strings.TrimSpace(storedAnswers) != "" {
			if uerr := json.Unmarshal([]byte(storedAnswers), &resp.Answers); uerr != nil || resp.Answers == nil {
				resp.Answers = map[string]any{}
			}
		}
		out[resp.SurveyID] = append(out[resp.SurveyID], resp)
	}
	return out, rows.Err()
}

func (h *SRMHandler) GetPulseSurvey(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var s models.SRMPulseSurvey
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("sv", srmSurveyColumns)+`, s.company_name AS supplier_name FROM srm_pulse_surveys sv JOIN srm_suppliers s ON s.id = sv.supplier_id WHERE sv.id = ?`, id).Scan(
		&s.ID, &s.SupplierID, &s.Direction, &s.TemplateVersion, &s.SentAt, &s.DueDate, &s.ResponsesCount, &s.SupplierName)
	if isNotFound(err) {
		respondNotFound(w, r, "survey not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	resps, err := h.srmSurveyResponses(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	s.Responses = resps[id]
	respondJSONOK(w, s)
}

func (h *SRMHandler) RecordSurveyResponse(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StakeholderID *int           `json:"stakeholder_id,omitempty"`
		IsAnonymous   bool           `json:"is_anonymous"`
		Answers       map[string]any `json:"answers"`
		OverallScore  *int           `json:"overall_score,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_pulse_surveys WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "survey not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.OverallScore != nil && (*in.OverallScore < 1 || *in.OverallScore > 5) {
		respondBadRequest(w, r, "overall_score must be between 1 and 5")
		return
	}
	answersJSON := []byte(`{}`)
	if len(in.Answers) > 0 {
		b, err := json.Marshal(in.Answers)
		if err != nil {
			respondBadRequest(w, r, "invalid answers payload")
			return
		}
		answersJSON = b
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_survey_responses (survey_id, stakeholder_id, is_anonymous, answers_json, overall_score) VALUES (?, ?, ?, ?, ?)`,
		id, nullInt(in.StakeholderID), in.IsAnonymous, string(answersJSON), nullInt(in.OverallScore))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respID64, _ := res.LastInsertId()
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_pulse_surveys SET responses_count = responses_count + 1 WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var out models.SRMSurveyResponse
	var storedAnswers string
	err = h.db.QueryRowContext(ctx, `SELECT r.id, r.survey_id, r.stakeholder_id, r.is_anonymous, r.answers_json, r.overall_score, r.responded_at, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_survey_responses r LEFT JOIN srm_stakeholders st ON st.id = r.stakeholder_id WHERE r.id = ?`, respID64).Scan(
		&out.ID, &out.SurveyID, &out.StakeholderID, &out.IsAnonymous, &storedAnswers, &out.OverallScore, &out.RespondedAt, &out.StakeholderName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	out.Answers = map[string]any{}
	if strings.TrimSpace(storedAnswers) != "" {
		if err := json.Unmarshal([]byte(storedAnswers), &out.Answers); err != nil || out.Answers == nil {
			out.Answers = map[string]any{}
		}
	}
	respondJSONCreated(w, out)
}

// ---------------------------------------------------------------------------
// Innovation register (Phase 4 / Step 4.2)
// ---------------------------------------------------------------------------

func (h *SRMHandler) srmInnovationFactors(ctx context.Context, innovationIDs []int) (map[int][]models.SRMInnovationFactor, error) {
	out := map[int][]models.SRMInnovationFactor{}
	if len(innovationIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(innovationIDs))
	for i, id := range innovationIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, innovation_id, evaluator_name, factor_name, is_custom, rating FROM srm_innovation_factors WHERE innovation_id IN (`+joinPlaceholders(make([]string, len(innovationIDs)))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f models.SRMInnovationFactor
		if err := rows.Scan(&f.ID, &f.InnovationID, &f.EvaluatorName, &f.FactorName, &f.IsCustom, &f.Rating); err != nil {
			return nil, err
		}
		out[f.InnovationID] = append(out[f.InnovationID], f)
	}
	return out, rows.Err()
}

func (h *SRMHandler) srmInnovationRecommendations(ctx context.Context, innovationIDs []int) (map[int][]models.SRMInnovationRecommendation, error) {
	out := map[int][]models.SRMInnovationRecommendation{}
	if len(innovationIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(innovationIDs))
	for i, id := range innovationIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT r.id, r.innovation_id, r.decision, r.justification, r.priority, r.timing, r.agency_champion_id, r.supplier_champion_name, r.decided_at, COALESCE(u.username, '') AS user_name FROM srm_innovation_recommendations r LEFT JOIN users u ON u.id = r.agency_champion_id WHERE r.innovation_id IN (`+joinPlaceholders(make([]string, len(innovationIDs)))+`) ORDER BY r.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec models.SRMInnovationRecommendation
		if err := rows.Scan(&rec.ID, &rec.InnovationID, &rec.Decision, &rec.Justification, &rec.Priority, &rec.Timing, &rec.AgencyChampionID, &rec.SupplierChampionName, &rec.DecidedAt, &rec.UserName); err != nil {
			return nil, err
		}
		out[rec.InnovationID] = append(out[rec.InnovationID], rec)
	}
	return out, rows.Err()
}

func (h *SRMHandler) srmInnovationObjectives(ctx context.Context, innovationIDs []int) (map[int][]models.SRMInnovationObjective, error) {
	out := map[int][]models.SRMInnovationObjective{}
	if len(innovationIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(innovationIDs))
	for i, id := range innovationIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, innovation_id, objective_text, actual_outcome, realised_value_nzd FROM srm_innovation_objectives WHERE innovation_id IN (`+joinPlaceholders(make([]string, len(innovationIDs)))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o models.SRMInnovationObjective
		if err := rows.Scan(&o.ID, &o.InnovationID, &o.ObjectiveText, &o.ActualOutcome, &o.RealisedValueNzd); err != nil {
			return nil, err
		}
		out[o.InnovationID] = append(out[o.InnovationID], o)
	}
	return out, rows.Err()
}

func (h *SRMHandler) srmInnovationTasks(ctx context.Context, innovationIDs []int) (map[int][]models.SRMInnovationTask, error) {
	out := map[int][]models.SRMInnovationTask{}
	if len(innovationIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(innovationIDs))
	for i, id := range innovationIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, innovation_id, task_no, task_name, description, deliverable, owner_name, side, start_date, end_date, status FROM srm_innovation_tasks WHERE innovation_id IN (`+joinPlaceholders(make([]string, len(innovationIDs)))+`) ORDER BY task_no`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t models.SRMInnovationTask
		if err := rows.Scan(&t.ID, &t.InnovationID, &t.TaskNo, &t.TaskName, &t.Description, &t.Deliverable, &t.OwnerName, &t.Side, &t.StartDate, &t.EndDate, &t.Status); err != nil {
			return nil, err
		}
		out[t.InnovationID] = append(out[t.InnovationID], t)
	}
	return out, rows.Err()
}

func (h *SRMHandler) getSRMInnovation(ctx context.Context, id int) (*models.SRMInnovation, error) {
	var i models.SRMInnovation
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("i", srmInnovationColumns)+`, s.company_name AS supplier_name FROM srm_innovations i JOIN srm_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id).Scan(
		&i.ID, &i.IdeaCode, &i.ContractID, &i.SupplierID, &i.Name, &i.Description, &i.IdeaDate, &i.SupplierTeam, &i.AgencyTeam, &i.Source, &i.Status, &i.PotentialFinancialValue, &i.RealisedFinancialValue, &i.CreatedAt, &i.UpdatedAt, &i.SupplierName)
	if err != nil {
		return nil, err
	}
	factors, err := h.srmInnovationFactors(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	i.Factors = factors[id]
	recs, err := h.srmInnovationRecommendations(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	if len(recs[id]) > 0 {
		i.Recommendation = &recs[id][0]
	}
	objectives, err := h.srmInnovationObjectives(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	i.Objectives = objectives[id]
	tasks, err := h.srmInnovationTasks(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	i.Tasks = tasks[id]
	return &i, nil
}

func (h *SRMHandler) ListInnovations(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := "SELECT " + qualifyColumns("i", srmInnovationColumns) + ", s.company_name AS supplier_name FROM srm_innovations i JOIN srm_suppliers s ON s.id = i.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("contract_id")); ok {
		query += ` AND i.contract_id = ?`
		args = append(args, v)
	}
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND i.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY i.created_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMInnovation{}
	for rows.Next() {
		var i models.SRMInnovation
		if err := rows.Scan(&i.ID, &i.IdeaCode, &i.ContractID, &i.SupplierID, &i.Name, &i.Description, &i.IdeaDate, &i.SupplierTeam, &i.AgencyTeam, &i.Source, &i.Status, &i.PotentialFinancialValue, &i.RealisedFinancialValue, &i.CreatedAt, &i.UpdatedAt, &i.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) CreateInnovation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		ContractID              int        `json:"contract_id"`
		SupplierID              int        `json:"supplier_id"`
		Name                    string     `json:"name"`
		Description             string     `json:"description"`
		IdeaDate                *time.Time `json:"idea_date,omitempty"`
		SupplierTeam            string     `json:"supplier_team"`
		AgencyTeam              string     `json:"agency_team"`
		Source                  string     `json:"source"`
		Status                  string     `json:"status"`
		PotentialFinancialValue float64    `json:"potential_financial_value"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	if !srmValidInnovationSource(in.Source) {
		respondBadRequest(w, r, `source must be one of: supplier_submission, agency_observation, jbp_workshop, scorecard_review, other`)
		return
	}
	if in.Status == "" {
		in.Status = "submitted"
	} else if !srmValidInnovationStatus(in.Status) {
		respondBadRequest(w, r, `status must be one of: submitted, under_review, proceed, reject, further_investigate, implementing, implemented`)
		return
	}
	contractSupplier, err := h.srmContractSupplier(ctx, in.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "contract not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.SupplierID != contractSupplier {
		respondBadRequest(w, r, "supplier_id does not match the contract's supplier")
		return
	}
	ideaDate := time.Now().UTC()
	if in.IdeaDate != nil {
		ideaDate = *in.IdeaDate
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_innovations (idea_code, contract_id, supplier_id, name, description, idea_date, supplier_team, agency_team, source, status, potential_financial_value) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srmNextIdeaCode(ctx, h.db), in.ContractID, in.SupplierID, in.Name, in.Description, ideaDate, in.SupplierTeam, in.AgencyTeam, in.Source, in.Status, in.PotentialFinancialValue)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	i, err := h.getSRMInnovation(ctx, int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, i)
}

func (h *SRMHandler) UpdateInnovation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name                    *string  `json:"name,omitempty"`
		Description             *string  `json:"description,omitempty"`
		SupplierTeam            *string  `json:"supplier_team,omitempty"`
		AgencyTeam              *string  `json:"agency_team,omitempty"`
		Source                  *string  `json:"source,omitempty"`
		Status                  *string  `json:"status,omitempty"`
		PotentialFinancialValue *float64 `json:"potential_financial_value,omitempty"`
		RealisedFinancialValue  *float64 `json:"realised_financial_value,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	existing, err := h.getSRMInnovation(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "innovation not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Source != nil && !srmValidInnovationSource(*in.Source) {
		respondBadRequest(w, r, `source must be one of: supplier_submission, agency_observation, jbp_workshop, scorecard_review, other`)
		return
	}
	if in.Status != nil && !srmValidInnovationStatus(*in.Status) {
		respondBadRequest(w, r, `status must be one of: submitted, under_review, proceed, reject, further_investigate, implementing, implemented`)
		return
	}
	potential := existing.PotentialFinancialValue
	if in.PotentialFinancialValue != nil {
		potential = *in.PotentialFinancialValue
	}
	realised := nullFloat(orPtrF(in.RealisedFinancialValue, existing.RealisedFinancialValue))
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_innovations SET name = ?, description = ?, supplier_team = ?, agency_team = ?, source = ?, status = ?, potential_financial_value = ?, realised_financial_value = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Name, existing.Name), orKeep(in.Description, existing.Description), orKeep(in.SupplierTeam, existing.SupplierTeam), orKeep(in.AgencyTeam, existing.AgencyTeam), orKeep(in.Source, existing.Source), orKeep(in.Status, existing.Status), potential, realised, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	i, err := h.getSRMInnovation(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, i)
}

func (h *SRMHandler) DeleteInnovation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM srm_innovations WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "innovation not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SRMHandler) UpsertInnovationFactor(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EvaluatorName string `json:"evaluator_name"`
		FactorName    string `json:"factor_name"`
		IsCustom      bool   `json:"is_custom"`
		Rating        int    `json:"rating"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.FactorName) == "" {
		respondBadRequest(w, r, "factor_name is required")
		return
	}
	if in.Rating < 1 || in.Rating > 5 {
		respondBadRequest(w, r, "rating must be between 1 and 5 (1 = optimal)")
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_innovations WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "innovation not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var factorID int
	err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_innovation_factors WHERE innovation_id = ? AND factor_name = ?`, id, in.FactorName).Scan(&factorID)
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_innovation_factors (innovation_id, evaluator_name, factor_name, is_custom, rating) VALUES (?, ?, ?, ?, ?)`,
			id, in.EvaluatorName, in.FactorName, in.IsCustom, in.Rating)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		factorID64, _ := res.LastInsertId()
		factorID = int(factorID64)
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_innovation_factors SET evaluator_name = ?, is_custom = ?, rating = ? WHERE id = ?`,
			in.EvaluatorName, in.IsCustom, in.Rating, factorID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	var out models.SRMInnovationFactor
	if err := h.db.QueryRowContext(ctx, `SELECT id, innovation_id, evaluator_name, factor_name, is_custom, rating FROM srm_innovation_factors WHERE id = ?`, factorID).Scan(
		&out.ID, &out.InnovationID, &out.EvaluatorName, &out.FactorName, &out.IsCustom, &out.Rating); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) GetInnovationRecommendation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var rec models.SRMInnovationRecommendation
	err := h.db.QueryRowContext(ctx, `SELECT r.id, r.innovation_id, r.decision, r.justification, r.priority, r.timing, r.agency_champion_id, r.supplier_champion_name, r.decided_at, COALESCE(u.username, '') AS user_name FROM srm_innovation_recommendations r LEFT JOIN users u ON u.id = r.agency_champion_id WHERE r.innovation_id = ?`, id).Scan(
		&rec.ID, &rec.InnovationID, &rec.Decision, &rec.Justification, &rec.Priority, &rec.Timing, &rec.AgencyChampionID, &rec.SupplierChampionName, &rec.DecidedAt, &rec.UserName)
	if isNotFound(err) {
		respondNotFound(w, r, "recommendation not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, rec)
}

func (h *SRMHandler) CreateInnovationRecommendation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Decision             string  `json:"decision"`
		Justification        string  `json:"justification"`
		Priority             *string `json:"priority,omitempty"`
		Timing               string  `json:"timing"`
		AgencyChampionID     *int    `json:"agency_champion_id,omitempty"`
		SupplierChampionName string  `json:"supplier_champion_name"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if !srmValidRecommendationDecision(in.Decision) {
		respondBadRequest(w, r, `decision must be one of: proceed, reject, further_investigate`)
		return
	}
	if strings.TrimSpace(in.Justification) == "" {
		respondBadRequest(w, r, "justification is required")
		return
	}
	if in.Priority != nil && !srmValidPriority(*in.Priority) {
		respondBadRequest(w, r, `priority must be one of: high, medium, low`)
		return
	}
	if in.AgencyChampionID != nil && !srmUserExists(ctx, h.db, *in.AgencyChampionID) {
		respondBadRequest(w, r, "agency_champion_id does not match a known user")
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_innovations WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "innovation not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var recID int
	err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_innovation_recommendations WHERE innovation_id = ?`, id).Scan(&recID)
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_innovation_recommendations (innovation_id, decision, justification, priority, timing, agency_champion_id, supplier_champion_name, decided_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			id, in.Decision, in.Justification, nullStringPtr(in.Priority), in.Timing, nullInt(in.AgencyChampionID), in.SupplierChampionName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		recID64, _ := res.LastInsertId()
		recID = int(recID64)
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_innovation_recommendations SET decision = ?, justification = ?, priority = ?, timing = ?, agency_champion_id = ?, supplier_champion_name = ?, decided_at = CURRENT_TIMESTAMP WHERE id = ?`,
			in.Decision, in.Justification, nullStringPtr(in.Priority), in.Timing, nullInt(in.AgencyChampionID), in.SupplierChampionName, recID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	var out models.SRMInnovationRecommendation
	err = h.db.QueryRowContext(ctx, `SELECT r.id, r.innovation_id, r.decision, r.justification, r.priority, r.timing, r.agency_champion_id, r.supplier_champion_name, r.decided_at, COALESCE(u.username, '') AS user_name FROM srm_innovation_recommendations r LEFT JOIN users u ON u.id = r.agency_champion_id WHERE r.id = ?`, recID).Scan(
		&out.ID, &out.InnovationID, &out.Decision, &out.Justification, &out.Priority, &out.Timing, &out.AgencyChampionID, &out.SupplierChampionName, &out.DecidedAt, &out.UserName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) AddInnovationObjective(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ObjectiveText string `json:"objective_text"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.ObjectiveText) == "" {
		respondBadRequest(w, r, "objective_text is required")
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_innovation_objectives WHERE innovation_id = ?`, id).Scan(&n)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n >= 5 {
		respondConflict(w, r, "maximum of five objectives per innovation")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_innovation_objectives (innovation_id, objective_text) VALUES (?, ?)`, id, in.ObjectiveText)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	objID64, _ := res.LastInsertId()
	var out models.SRMInnovationObjective
	err = h.db.QueryRowContext(ctx, `SELECT id, innovation_id, objective_text, actual_outcome, realised_value_nzd FROM srm_innovation_objectives WHERE id = ?`, objID64).Scan(
		&out.ID, &out.InnovationID, &out.ObjectiveText, &out.ActualOutcome, &out.RealisedValueNzd)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

func (h *SRMHandler) UpdateInnovationObjective(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ObjectiveText    *string  `json:"objective_text,omitempty"`
		ActualOutcome    *string  `json:"actual_outcome,omitempty"`
		RealisedValueNzd *float64 `json:"realised_value_nzd,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var existing models.SRMInnovationObjective
	err := h.db.QueryRowContext(ctx, `SELECT id, innovation_id, objective_text, actual_outcome, realised_value_nzd FROM srm_innovation_objectives WHERE id = ?`, id).Scan(
		&existing.ID, &existing.InnovationID, &existing.ObjectiveText, &existing.ActualOutcome, &existing.RealisedValueNzd)
	if isNotFound(err) {
		respondNotFound(w, r, "objective not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_innovation_objectives SET objective_text = ?, actual_outcome = ?, realised_value_nzd = ? WHERE id = ?`,
		orKeep(in.ObjectiveText, existing.ObjectiveText), orKeep(in.ActualOutcome, existing.ActualOutcome), nullFloat(orPtrF(in.RealisedValueNzd, existing.RealisedValueNzd)), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var out models.SRMInnovationObjective
	err = h.db.QueryRowContext(ctx, `SELECT id, innovation_id, objective_text, actual_outcome, realised_value_nzd FROM srm_innovation_objectives WHERE id = ?`, id).Scan(
		&out.ID, &out.InnovationID, &out.ObjectiveText, &out.ActualOutcome, &out.RealisedValueNzd)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) DeleteInnovationObjective(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM srm_innovation_objectives WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		respondNotFound(w, r, "objective not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SRMHandler) AddInnovationTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		TaskNo      int        `json:"task_no"`
		TaskName    string     `json:"task_name"`
		Description string     `json:"description"`
		Deliverable string     `json:"deliverable"`
		OwnerName   string     `json:"owner_name"`
		Side        string     `json:"side"`
		StartDate   *time.Time `json:"start_date,omitempty"`
		EndDate     *time.Time `json:"end_date,omitempty"`
		Status      string     `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if in.TaskNo <= 0 {
		respondBadRequest(w, r, "task_no must be a positive integer")
		return
	}
	if strings.TrimSpace(in.TaskName) == "" {
		respondBadRequest(w, r, "task_name is required")
		return
	}
	if in.Side != models.SRMSideAgency && in.Side != models.SRMSideSupplier {
		respondBadRequest(w, r, `side must be one of: agency, supplier`)
		return
	}
	if in.Status == "" {
		in.Status = "in_progress"
	} else if !srmValidTaskStatus(in.Status) {
		respondBadRequest(w, r, `status must be one of: in_progress, awaiting_approval, sign_off_stage, complete`)
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_innovation_tasks WHERE innovation_id = ? AND task_no = ?`, id, in.TaskNo).Scan(&n)
	if err == nil {
		respondConflict(w, r, "a task with this number already exists")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_innovation_tasks (innovation_id, task_no, task_name, description, deliverable, owner_name, side, start_date, end_date, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.TaskNo, in.TaskName, in.Description, in.Deliverable, in.OwnerName, in.Side, nullTimePtr(in.StartDate), nullTimePtr(in.EndDate), in.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	taskID64, _ := res.LastInsertId()
	var out models.SRMInnovationTask
	err = h.db.QueryRowContext(ctx, `SELECT id, innovation_id, task_no, task_name, description, deliverable, owner_name, side, start_date, end_date, status FROM srm_innovation_tasks WHERE id = ?`, taskID64).Scan(
		&out.ID, &out.InnovationID, &out.TaskNo, &out.TaskName, &out.Description, &out.Deliverable, &out.OwnerName, &out.Side, &out.StartDate, &out.EndDate, &out.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

func (h *SRMHandler) UpdateInnovationTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		TaskName    *string    `json:"task_name,omitempty"`
		Description *string    `json:"description,omitempty"`
		Deliverable *string    `json:"deliverable,omitempty"`
		OwnerName   *string    `json:"owner_name,omitempty"`
		Side        *string    `json:"side,omitempty"`
		StartDate   *time.Time `json:"start_date,omitempty"`
		EndDate     *time.Time `json:"end_date,omitempty"`
		Status      *string    `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var existing models.SRMInnovationTask
	err := h.db.QueryRowContext(ctx, `SELECT id, innovation_id, task_no, task_name, description, deliverable, owner_name, side, start_date, end_date, status FROM srm_innovation_tasks WHERE id = ?`, id).Scan(
		&existing.ID, &existing.InnovationID, &existing.TaskNo, &existing.TaskName, &existing.Description, &existing.Deliverable, &existing.OwnerName, &existing.Side, &existing.StartDate, &existing.EndDate, &existing.Status)
	if isNotFound(err) {
		respondNotFound(w, r, "task not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Side != nil && *in.Side != models.SRMSideAgency && *in.Side != models.SRMSideSupplier {
		respondBadRequest(w, r, `side must be one of: agency, supplier`)
		return
	}
	if in.Status != nil && !srmValidTaskStatus(*in.Status) {
		respondBadRequest(w, r, `status must be one of: in_progress, awaiting_approval, sign_off_stage, complete`)
		return
	}
	startDate := nullTimePtr(orPtrTime(in.StartDate, existing.StartDate))
	endDate := nullTimePtr(orPtrTime(in.EndDate, existing.EndDate))
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_innovation_tasks SET task_name = ?, description = ?, deliverable = ?, owner_name = ?, side = ?, start_date = ?, end_date = ?, status = ? WHERE id = ?`,
		orKeep(in.TaskName, existing.TaskName), orKeep(in.Description, existing.Description), orKeep(in.Deliverable, existing.Deliverable), orKeep(in.OwnerName, existing.OwnerName), orKeep(in.Side, existing.Side), startDate, endDate, orKeep(in.Status, existing.Status), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var out models.SRMInnovationTask
	err = h.db.QueryRowContext(ctx, `SELECT id, innovation_id, task_no, task_name, description, deliverable, owner_name, side, start_date, end_date, status FROM srm_innovation_tasks WHERE id = ?`, id).Scan(
		&out.ID, &out.InnovationID, &out.TaskNo, &out.TaskName, &out.Description, &out.Deliverable, &out.OwnerName, &out.Side, &out.StartDate, &out.EndDate, &out.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ---------------------------------------------------------------------------
// Service improvement plans (Phase 4 / Step 4.3)
// ---------------------------------------------------------------------------

func (h *SRMHandler) srmSipDeliverables(ctx context.Context, sipIDs []int) (map[int][]models.SRMSIPDeliverable, error) {
	out := map[int][]models.SRMSIPDeliverable{}
	if len(sipIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(sipIDs))
	for i, id := range sipIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, sip_id, seq, key_deliverable, completion_date, rag, owner_name, comments_risks_issues FROM srm_sip_deliverables WHERE sip_id IN (`+joinPlaceholders(make([]string, len(sipIDs)))+`) ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d models.SRMSIPDeliverable
		if err := rows.Scan(&d.ID, &d.SipID, &d.Seq, &d.KeyDeliverable, &d.CompletionDate, &d.Rag, &d.OwnerName, &d.CommentsRisksIssues); err != nil {
			return nil, err
		}
		out[d.SipID] = append(out[d.SipID], d)
	}
	return out, rows.Err()
}

func (h *SRMHandler) getSRMSip(ctx context.Context, id int) (*models.SRMServiceImprovementPlan, error) {
	var s models.SRMServiceImprovementPlan
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("p", srmSipColumns)+`, sup.company_name AS supplier_name FROM srm_service_improvement_plans p JOIN srm_suppliers sup ON sup.id = p.supplier_id WHERE p.id = ?`, id).Scan(
		&s.ID, &s.ContractID, &s.SupplierID, &s.Title, &s.SipOwnerID, &s.ProblemStatement, &s.Subtype, &s.SuccessMeasureSmart, &s.ExitCriteria, &s.Status, &s.SourceRef, &s.CreatedAt, &s.UpdatedAt, &s.SupplierName)
	if err != nil {
		return nil, err
	}
	deliverables, err := h.srmSipDeliverables(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	s.Deliverables = deliverables[id]
	return &s, nil
}

func (h *SRMHandler) ListServiceImprovementPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := "SELECT " + qualifyColumns("p", srmSipColumns) + ", sup.company_name AS supplier_name FROM srm_service_improvement_plans p JOIN srm_suppliers sup ON sup.id = p.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("contract_id")); ok {
		query += ` AND p.contract_id = ?`
		args = append(args, v)
	}
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND p.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY p.created_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMServiceImprovementPlan{}
	for rows.Next() {
		var s models.SRMServiceImprovementPlan
		if err := rows.Scan(&s.ID, &s.ContractID, &s.SupplierID, &s.Title, &s.SipOwnerID, &s.ProblemStatement, &s.Subtype, &s.SuccessMeasureSmart, &s.ExitCriteria, &s.Status, &s.SourceRef, &s.CreatedAt, &s.UpdatedAt, &s.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) CreateServiceImprovementPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		ContractID          int    `json:"contract_id"`
		SupplierID          int    `json:"supplier_id"`
		Title               string `json:"title"`
		SipOwnerID          int    `json:"sip_owner_id"`
		ProblemStatement    string `json:"problem_statement"`
		Subtype             string `json:"subtype"`
		SuccessMeasureSmart string `json:"success_measure_smart"`
		ExitCriteria        string `json:"exit_criteria"`
		Status              string `json:"status"`
		SourceRef           string `json:"source_ref"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		respondBadRequest(w, r, "title is required")
		return
	}
	if strings.TrimSpace(in.ProblemStatement) == "" {
		respondBadRequest(w, r, "problem_statement is required")
		return
	}
	if !srmSmartMeasureValid(in.SuccessMeasureSmart) {
		respondBadRequest(w, r, "success_measure_smart must be SMART: include a number and a date (e.g. 30/6/2026)")
		return
	}
	if strings.TrimSpace(in.ExitCriteria) == "" {
		respondBadRequest(w, r, "exit_criteria is required")
		return
	}
	if in.Subtype == "" {
		in.Subtype = "breach_driven"
	} else if !srmValidSipSubtype(in.Subtype) {
		respondBadRequest(w, r, `subtype must be one of: breach_driven, continuous_improvement`)
		return
	}
	if in.Status == "" {
		in.Status = "draft"
	} else if !srmValidSipStatus(in.Status) {
		respondBadRequest(w, r, `status must be one of: draft, active, on_track_review, resolved, closed_unresolved`)
		return
	}
	if !srmUserExists(ctx, h.db, in.SipOwnerID) {
		respondBadRequest(w, r, "sip_owner_id does not match a known user")
		return
	}
	contractSupplier, err := h.srmContractSupplier(ctx, in.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "contract not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.SupplierID != contractSupplier {
		respondBadRequest(w, r, "supplier_id does not match the contract's supplier")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_service_improvement_plans (contract_id, supplier_id, title, sip_owner_id, problem_statement, subtype, success_measure_smart, exit_criteria, status, source_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ContractID, in.SupplierID, in.Title, in.SipOwnerID, in.ProblemStatement, in.Subtype, in.SuccessMeasureSmart, in.ExitCriteria, in.Status, in.SourceRef)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	createdID := int(id64)
	logAuditWithDetails(h.db, r, user, "srm_sip_created", "srm_service_improvement_plan", &createdID, in.Title, map[string]any{"contract_id": in.ContractID})
	s, err := h.getSRMSip(ctx, createdID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, s)
}

func (h *SRMHandler) GetServiceImprovementPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	s, err := h.getSRMSip(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "service improvement plan not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

func (h *SRMHandler) UpdateServiceImprovementPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title               *string `json:"title,omitempty"`
		SipOwnerID          *int    `json:"sip_owner_id,omitempty"`
		ProblemStatement    *string `json:"problem_statement,omitempty"`
		Subtype             *string `json:"subtype,omitempty"`
		SuccessMeasureSmart *string `json:"success_measure_smart,omitempty"`
		ExitCriteria        *string `json:"exit_criteria,omitempty"`
		Status              *string `json:"status,omitempty"`
		SourceRef           *string `json:"source_ref,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	existing, err := h.getSRMSip(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "service improvement plan not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Subtype != nil && !srmValidSipSubtype(*in.Subtype) {
		respondBadRequest(w, r, `subtype must be one of: breach_driven, continuous_improvement`)
		return
	}
	if in.Status != nil && !srmValidSipStatus(*in.Status) {
		respondBadRequest(w, r, `status must be one of: draft, active, on_track_review, resolved, closed_unresolved`)
		return
	}
	if in.SuccessMeasureSmart != nil && !srmSmartMeasureValid(*in.SuccessMeasureSmart) {
		respondBadRequest(w, r, "success_measure_smart must be SMART: include a number and a date (e.g. 30/6/2026)")
		return
	}
	if in.SipOwnerID != nil && !srmUserExists(ctx, h.db, *in.SipOwnerID) {
		respondBadRequest(w, r, "sip_owner_id does not match a known user")
		return
	}
	sipOwner := existing.SipOwnerID
	if in.SipOwnerID != nil {
		sipOwner = *in.SipOwnerID
	}
	newStatus := orKeep(in.Status, existing.Status)
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_service_improvement_plans SET title = ?, sip_owner_id = ?, problem_statement = ?, subtype = ?, success_measure_smart = ?, exit_criteria = ?, status = ?, source_ref = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Title, existing.Title), sipOwner, orKeep(in.ProblemStatement, existing.ProblemStatement), orKeep(in.Subtype, existing.Subtype), orKeep(in.SuccessMeasureSmart, existing.SuccessMeasureSmart), orKeep(in.ExitCriteria, existing.ExitCriteria), newStatus, orKeep(in.SourceRef, existing.SourceRef), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Status != nil && *in.Status != existing.Status {
		logAuditWithDetails(h.db, r, user, "srm_sip_status_changed", "srm_service_improvement_plan", &id, fmt.Sprintf("%d", id), map[string]any{"from": existing.Status, "to": *in.Status})
	}
	s, err := h.getSRMSip(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

func (h *SRMHandler) AddSipDeliverable(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Seq                 int        `json:"seq"`
		KeyDeliverable      string     `json:"key_deliverable"`
		CompletionDate      *time.Time `json:"completion_date,omitempty"`
		Rag                 string     `json:"rag"`
		OwnerName           string     `json:"owner_name"`
		CommentsRisksIssues string     `json:"comments_risks_issues"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if in.Seq <= 0 {
		respondBadRequest(w, r, "seq must be a positive integer")
		return
	}
	if strings.TrimSpace(in.KeyDeliverable) == "" {
		respondBadRequest(w, r, "key_deliverable is required")
		return
	}
	if in.Rag == "" {
		in.Rag = models.SRMRagGreen
	} else if !srmValidRag(in.Rag) {
		respondBadRequest(w, r, `rag must be one of: green, amber, red`)
		return
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_sip_deliverables WHERE sip_id = ? AND seq = ?`, id, in.Seq).Scan(&n)
	if err == nil {
		respondConflict(w, r, "a deliverable with this sequence already exists")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_sip_deliverables (sip_id, seq, key_deliverable, completion_date, rag, owner_name, comments_risks_issues) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, in.Seq, in.KeyDeliverable, nullTimePtr(in.CompletionDate), in.Rag, in.OwnerName, in.CommentsRisksIssues)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	delivID64, _ := res.LastInsertId()
	var out models.SRMSIPDeliverable
	err = h.db.QueryRowContext(ctx, `SELECT id, sip_id, seq, key_deliverable, completion_date, rag, owner_name, comments_risks_issues FROM srm_sip_deliverables WHERE id = ?`, delivID64).Scan(
		&out.ID, &out.SipID, &out.Seq, &out.KeyDeliverable, &out.CompletionDate, &out.Rag, &out.OwnerName, &out.CommentsRisksIssues)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

func (h *SRMHandler) UpdateSipDeliverable(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		KeyDeliverable      *string    `json:"key_deliverable,omitempty"`
		CompletionDate      *time.Time `json:"completion_date,omitempty"`
		Rag                 *string    `json:"rag,omitempty"`
		OwnerName           *string    `json:"owner_name,omitempty"`
		CommentsRisksIssues *string    `json:"comments_risks_issues,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var existing models.SRMSIPDeliverable
	err := h.db.QueryRowContext(ctx, `SELECT id, sip_id, seq, key_deliverable, completion_date, rag, owner_name, comments_risks_issues FROM srm_sip_deliverables WHERE id = ?`, id).Scan(
		&existing.ID, &existing.SipID, &existing.Seq, &existing.KeyDeliverable, &existing.CompletionDate, &existing.Rag, &existing.OwnerName, &existing.CommentsRisksIssues)
	if isNotFound(err) {
		respondNotFound(w, r, "deliverable not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.Rag != nil && !srmValidRag(*in.Rag) {
		respondBadRequest(w, r, `rag must be one of: green, amber, red`)
		return
	}
	completionDate := nullTimePtr(orPtrTime(in.CompletionDate, existing.CompletionDate))
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_sip_deliverables SET key_deliverable = ?, completion_date = ?, rag = ?, owner_name = ?, comments_risks_issues = ? WHERE id = ?`,
		orKeep(in.KeyDeliverable, existing.KeyDeliverable), completionDate, orKeep(in.Rag, existing.Rag), orKeep(in.OwnerName, existing.OwnerName), orKeep(in.CommentsRisksIssues, existing.CommentsRisksIssues), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var out models.SRMSIPDeliverable
	err = h.db.QueryRowContext(ctx, `SELECT id, sip_id, seq, key_deliverable, completion_date, rag, owner_name, comments_risks_issues FROM srm_sip_deliverables WHERE id = ?`, id).Scan(
		&out.ID, &out.SipID, &out.Seq, &out.KeyDeliverable, &out.CompletionDate, &out.Rag, &out.OwnerName, &out.CommentsRisksIssues)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ---------------------------------------------------------------------------
// Disputes and remedy plans (Phase 4 / Step 4.3)
// ---------------------------------------------------------------------------

func (h *SRMHandler) srmDisputeEscalations(ctx context.Context, disputeIDs []int) (map[int][]models.SRMDisputeEscalation, error) {
	out := map[int][]models.SRMDisputeEscalation{}
	if len(disputeIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(disputeIDs))
	for i, id := range disputeIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, dispute_id, from_stage, to_stage, escalated_at, escalated_to, pause_reason, notes FROM srm_dispute_escalations WHERE dispute_id IN (`+joinPlaceholders(make([]string, len(disputeIDs)))+`) ORDER BY escalated_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e models.SRMDisputeEscalation
		if err := rows.Scan(&e.ID, &e.DisputeID, &e.FromStage, &e.ToStage, &e.EscalatedAt, &e.EscalatedTo, &e.PauseReason, &e.Notes); err != nil {
			return nil, err
		}
		out[e.DisputeID] = append(out[e.DisputeID], e)
	}
	return out, rows.Err()
}

func (h *SRMHandler) getSRMDispute(ctx context.Context, id int) (*models.SRMDispute, error) {
	var d models.SRMDispute
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("dp", srmDisputeColumns)+`, s.company_name AS supplier_name FROM srm_disputes dp JOIN srm_suppliers s ON s.id = dp.supplier_id WHERE dp.id = ?`, id).Scan(
		&d.ID, &d.ContractID, &d.SupplierID, &d.Description, &d.Stage, &d.OpenedAt, &d.ResolvedAt, &d.OutcomeNote, &d.SupplierName)
	if err != nil {
		return nil, err
	}
	escalations, err := h.srmDisputeEscalations(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	d.Escalations = escalations[id]

	var rp models.SRMRemedyPlan
	var timetableJSON string
	err = h.db.QueryRowContext(ctx, `SELECT id, dispute_id, sip_id, contract_id, non_performance_issue, root_cause, corrective_action_required, timetable_json, target_resolution_date, status, created_at FROM srm_remedy_plans WHERE dispute_id = ? ORDER BY created_at DESC LIMIT 1`, id).Scan(
		&rp.ID, &rp.DisputeID, &rp.SipID, &rp.ContractID, &rp.NonPerformanceIssue, &rp.RootCause, &rp.CorrectiveActionRequired, &timetableJSON, &rp.TargetResolutionDate, &rp.Status, &rp.CreatedAt)
	if err == nil {
		rp.SupplierName = d.SupplierName
		rp.Timetable = []models.SRMTimetableEntry{}
		if strings.TrimSpace(timetableJSON) != "" {
			if uerr := json.Unmarshal([]byte(timetableJSON), &rp.Timetable); uerr != nil || rp.Timetable == nil {
				rp.Timetable = []models.SRMTimetableEntry{}
			}
		}
		d.RemedyPlan = &rp
	} else if !isNotFound(err) {
		return nil, err
	}
	return &d, nil
}

func (h *SRMHandler) ListDisputes(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := "SELECT " + qualifyColumns("dp", srmDisputeColumns) + ", s.company_name AS supplier_name FROM srm_disputes dp JOIN srm_suppliers s ON s.id = dp.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("contract_id")); ok {
		query += ` AND dp.contract_id = ?`
		args = append(args, v)
	}
	if stage := r.URL.Query().Get("stage"); stage != "" {
		query += ` AND dp.stage = ?`
		args = append(args, stage)
	}
	query += ` ORDER BY dp.opened_at DESC`
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.SRMDispute{}
	for rows.Next() {
		var d models.SRMDispute
		if err := rows.Scan(&d.ID, &d.ContractID, &d.SupplierID, &d.Description, &d.Stage, &d.OpenedAt, &d.ResolvedAt, &d.OutcomeNote, &d.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

func (h *SRMHandler) CreateDispute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		ContractID  int    `json:"contract_id"`
		SupplierID  int    `json:"supplier_id"`
		Description string `json:"description"`
		Stage       string `json:"stage"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Description) == "" {
		respondBadRequest(w, r, "description is required")
		return
	}
	if in.Stage == "" {
		in.Stage = "l1_working_level"
	} else if !srmValidDisputeStage(in.Stage) {
		respondBadRequest(w, r, `stage must be one of: l1_working_level, l2_director, l3_executive, mediation, litigation, closed`)
		return
	}
	contractSupplier, err := h.srmContractSupplier(ctx, in.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "contract not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.SupplierID != contractSupplier {
		respondBadRequest(w, r, "supplier_id does not match the contract's supplier")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_disputes (contract_id, supplier_id, description, stage) VALUES (?, ?, ?, ?)`,
		in.ContractID, in.SupplierID, in.Description, in.Stage)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	createdID := int(id64)
	logAuditWithDetails(h.db, r, user, "srm_dispute_created", "srm_dispute", &createdID, fmt.Sprintf("%d", createdID), map[string]any{"contract_id": in.ContractID, "stage": in.Stage})
	d, err := h.getSRMDispute(ctx, createdID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, d)
}

func (h *SRMHandler) GetDispute(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	d, err := h.getSRMDispute(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "dispute not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, d)
}

func (h *SRMHandler) UpdateDispute(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Description *string `json:"description,omitempty"`
		OutcomeNote *string `json:"outcome_note,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var existing models.SRMDispute
	err := h.db.QueryRowContext(ctx, `SELECT id, contract_id, supplier_id, description, stage, opened_at, resolved_at, outcome_note FROM srm_disputes WHERE id = ?`, id).Scan(
		&existing.ID, &existing.ContractID, &existing.SupplierID, &existing.Description, &existing.Stage, &existing.OpenedAt, &existing.ResolvedAt, &existing.OutcomeNote)
	if isNotFound(err) {
		respondNotFound(w, r, "dispute not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_disputes SET description = ?, outcome_note = ? WHERE id = ?`,
		orKeep(in.Description, existing.Description), orKeep(in.OutcomeNote, existing.OutcomeNote), id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	d, err := h.getSRMDispute(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, d)
}

func (h *SRMHandler) AddDisputeEscalation(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ToStage     string `json:"to_stage"`
		EscalatedTo string `json:"escalated_to"`
		PauseReason string `json:"pause_reason"`
		Notes       string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if !srmValidDisputeStage(in.ToStage) {
		respondBadRequest(w, r, `to_stage must be one of: l1_working_level, l2_director, l3_executive, mediation, litigation, closed`)
		return
	}
	var existing models.SRMDispute
	err := h.db.QueryRowContext(ctx, `SELECT id, contract_id, supplier_id, description, stage, opened_at, resolved_at, outcome_note FROM srm_disputes WHERE id = ?`, id).Scan(
		&existing.ID, &existing.ContractID, &existing.SupplierID, &existing.Description, &existing.Stage, &existing.OpenedAt, &existing.ResolvedAt, &existing.OutcomeNote)
	if isNotFound(err) {
		respondNotFound(w, r, "dispute not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.ToStage == existing.Stage {
		respondBadRequest(w, r, "to_stage must differ from the current stage")
		return
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_dispute_escalations (dispute_id, from_stage, to_stage, escalated_to, pause_reason, notes) VALUES (?, ?, ?, ?, ?, ?)`,
		id, existing.Stage, in.ToStage, in.EscalatedTo, in.PauseReason, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	escID64, _ := res.LastInsertId()
	if in.ToStage == "closed" {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_disputes SET stage = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?`, in.ToStage, id); err != nil {
			respondInternalError(w, r, err)
			return
		}
	} else if _, err := h.db.ExecContext(ctx, `UPDATE srm_disputes SET stage = ? WHERE id = ?`, in.ToStage, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAuditWithDetails(h.db, r, user, "srm_dispute_escalated", "srm_dispute", &id, fmt.Sprintf("%d", id), map[string]any{"from": existing.Stage, "to": in.ToStage})
	var out models.SRMDisputeEscalation
	err = h.db.QueryRowContext(ctx, `SELECT id, dispute_id, from_stage, to_stage, escalated_at, escalated_to, pause_reason, notes FROM srm_dispute_escalations WHERE id = ?`, escID64).Scan(
		&out.ID, &out.DisputeID, &out.FromStage, &out.ToStage, &out.EscalatedAt, &out.EscalatedTo, &out.PauseReason, &out.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, out)
}

func (h *SRMHandler) UpsertRemedyPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		NonPerformanceIssue      string                     `json:"non_performance_issue"`
		RootCause                string                     `json:"root_cause"`
		CorrectiveActionRequired string                     `json:"corrective_action_required"`
		Timetable                []models.SRMTimetableEntry `json:"timetable"`
		TargetResolutionDate     time.Time                  `json:"target_resolution_date"`
		Status                   string                     `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.NonPerformanceIssue) == "" {
		respondBadRequest(w, r, "non_performance_issue is required")
		return
	}
	if strings.TrimSpace(in.CorrectiveActionRequired) == "" {
		respondBadRequest(w, r, "corrective_action_required is required")
		return
	}
	if in.TargetResolutionDate.IsZero() {
		respondBadRequest(w, r, "target_resolution_date is required")
		return
	}
	for _, entry := range in.Timetable {
		if entry.OwnerSide != models.SRMSideAgency && entry.OwnerSide != models.SRMSideSupplier {
			respondBadRequest(w, r, `timetable owner_side must be one of: agency, supplier`)
			return
		}
	}
	if in.Status == "" {
		in.Status = "active"
	} else if !srmValidRemedyStatus(in.Status) {
		respondBadRequest(w, r, `status must be one of: active, resolved, breached, escalated`)
		return
	}
	var existing models.SRMDispute
	err := h.db.QueryRowContext(ctx, `SELECT id, contract_id FROM srm_disputes WHERE id = ?`, id).Scan(&existing.ID, &existing.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "dispute not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	timetableJSON := []byte(`[]`)
	if len(in.Timetable) > 0 {
		b, err := json.Marshal(in.Timetable)
		if err != nil {
			respondBadRequest(w, r, "invalid timetable payload")
			return
		}
		timetableJSON = b
	}
	var planID int
	err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_remedy_plans WHERE dispute_id = ?`, id).Scan(&planID)
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_remedy_plans (dispute_id, contract_id, non_performance_issue, root_cause, corrective_action_required, timetable_json, target_resolution_date, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, existing.ContractID, in.NonPerformanceIssue, in.RootCause, in.CorrectiveActionRequired, string(timetableJSON), in.TargetResolutionDate, in.Status)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		planID64, _ := res.LastInsertId()
		planID = int(planID64)
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_remedy_plans SET non_performance_issue = ?, root_cause = ?, corrective_action_required = ?, timetable_json = ?, target_resolution_date = ?, status = ? WHERE id = ?`,
			in.NonPerformanceIssue, in.RootCause, in.CorrectiveActionRequired, string(timetableJSON), in.TargetResolutionDate, in.Status, planID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	logAuditWithDetails(h.db, r, user, "srm_remedy_plan_saved", "srm_remedy_plan", &planID, fmt.Sprintf("%d", planID), map[string]any{"dispute_id": id, "status": in.Status})
	var out models.SRMRemedyPlan
	var storedTimetable string
	err = h.db.QueryRowContext(ctx, `SELECT rp.id, rp.dispute_id, rp.sip_id, rp.contract_id, rp.non_performance_issue, rp.root_cause, rp.corrective_action_required, rp.timetable_json, rp.target_resolution_date, rp.status, rp.created_at FROM srm_remedy_plans rp WHERE rp.id = ?`, planID).Scan(
		&out.ID, &out.DisputeID, &out.SipID, &out.ContractID, &out.NonPerformanceIssue, &out.RootCause, &out.CorrectiveActionRequired, &storedTimetable, &out.TargetResolutionDate, &out.Status, &out.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	out.Timetable = []models.SRMTimetableEntry{}
	if strings.TrimSpace(storedTimetable) != "" {
		if uerr := json.Unmarshal([]byte(storedTimetable), &out.Timetable); uerr != nil || out.Timetable == nil {
			out.Timetable = []models.SRMTimetableEntry{}
		}
	}
	d, err := h.getSRMDispute(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if d.RemedyPlan != nil {
		out.SupplierName = d.RemedyPlan.SupplierName
	}
	respondJSONOK(w, out)
}
