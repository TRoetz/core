package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Financial exception alert rules (FIN-009). Rules are evaluated on demand via
// POST /api/p3m/evaluate-finance-alerts and by the nightly scheduler tick; a
// triggered rule is audit-emitted so the alert history is reconstructable.
// ---------------------------------------------------------------------------

const p3mFinanceAlertRuleColumns = `id, name, scope, portfolio_id, project_id, metric, comparator, threshold, notify_email, enabled, created_at, updated_at`

func scanP3MFinanceAlertRule(row interface{ Scan(...any) error }) (models.P3MFinanceAlertRule, error) {
	var rl models.P3MFinanceAlertRule
	err := row.Scan(&rl.ID, &rl.Name, &rl.Scope, &rl.PortfolioID, &rl.ProjectID, &rl.Metric, &rl.Comparator, &rl.Threshold, &rl.NotifyEmail, &rl.Enabled, &rl.CreatedAt, &rl.UpdatedAt)
	return rl, err
}

func p3mValidAlertMetric(m string) bool {
	switch m {
	case "forecast_overrun_pct", "variance_vs_budget", "commitment_vs_remaining":
		return true
	default:
		return false
	}
}

func p3mValidComparator(c string) bool {
	switch c {
	case ">", "<", ">=", "<=":
		return true
	default:
		return false
	}
}

// validateP3MFinanceAlertRuleInput checks the rule payload and writes a 422 on
// failure. Scope must match its entity reference.
func (h *P3MHandler) validateP3MFinanceAlertRuleInput(w http.ResponseWriter, r *http.Request, in models.P3MFinanceAlertRule) bool {
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return false
	}
	switch in.Scope {
	case "portfolio":
		if in.PortfolioID == nil || *in.PortfolioID <= 0 {
			respondValidationError(w, r, "portfolio_id is required for portfolio scope")
			return false
		}
	case "project":
		if in.ProjectID == nil || *in.ProjectID <= 0 {
			respondValidationError(w, r, "project_id is required for project scope")
			return false
		}
	default:
		respondValidationError(w, r, "scope must be portfolio or project")
		return false
	}
	if !p3mValidAlertMetric(in.Metric) {
		respondValidationError(w, r, "metric must be forecast_overrun_pct, variance_vs_budget or commitment_vs_remaining")
		return false
	}
	if !p3mValidComparator(in.Comparator) {
		respondValidationError(w, r, "comparator must be one of >, <, >=, <=")
		return false
	}
	return true
}

// ListFinanceAlertRules handles GET /api/p3m/finance-alert-rules. Optional
// filters: scope, portfolio_id and project_id.
func (h *P3MHandler) ListFinanceAlertRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mFinanceAlertRuleColumns + ` FROM p3m_finance_alert_rules WHERE 1=1`
	args := []any{}
	q := r.URL.Query()
	if v := q.Get("scope"); v != "" {
		query += ` AND scope = ?`
		args = append(args, v)
	}
	if v := q.Get("portfolio_id"); v != "" {
		id, okID := parseQueryInt(v)
		if !okID {
			respondValidationError(w, r, "portfolio_id must be an integer")
			return
		}
		query += ` AND portfolio_id = ?`
		args = append(args, id)
	}
	if v := q.Get("project_id"); v != "" {
		id, okID := parseQueryInt(v)
		if !okID {
			respondValidationError(w, r, "project_id must be an integer")
			return
		}
		query += ` AND project_id = ?`
		args = append(args, id)
	}
	query += ` ORDER BY name`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MFinanceAlertRule, 0)
	for rows.Next() {
		rl, err := scanP3MFinanceAlertRule(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, rl)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateFinanceAlertRule handles POST /api/p3m/finance-alert-rules.
func (h *P3MHandler) CreateFinanceAlertRule(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in models.P3MFinanceAlertRule
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MFinanceAlertRuleInput(w, r, in) {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_finance_alert_rules (name, scope, portfolio_id, project_id, metric, comparator, threshold, notify_email, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Scope, in.PortfolioID, in.ProjectID, in.Metric, in.Comparator, in.Threshold, in.NotifyEmail, true)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	rl, err := scanP3MFinanceAlertRule(h.db.QueryRowContext(r.Context(), `SELECT `+p3mFinanceAlertRuleColumns+` FROM p3m_finance_alert_rules WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_finance_alert_rule_created", "p3m_finance_alert_rule", &rl.ID, in.Name)
	respondJSONCreated(w, rl)
}

// UpdateFinanceAlertRule handles PUT /api/p3m/finance-alert-rules/{id}.
func (h *P3MHandler) UpdateFinanceAlertRule(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in models.P3MFinanceAlertRule
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.validateP3MFinanceAlertRuleInput(w, r, in) {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_finance_alert_rules SET name = ?, scope = ?, portfolio_id = ?, project_id = ?, metric = ?, comparator = ?, threshold = ?, notify_email = ?, enabled = ? WHERE id = ?`,
		in.Name, in.Scope, in.PortfolioID, in.ProjectID, in.Metric, in.Comparator, in.Threshold, in.NotifyEmail, in.Enabled, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_finance_alert_rule")
		return
	}
	rl, err := scanP3MFinanceAlertRule(h.db.QueryRowContext(r.Context(), `SELECT `+p3mFinanceAlertRuleColumns+` FROM p3m_finance_alert_rules WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_finance_alert_rule_updated", "p3m_finance_alert_rule", &id, in.Name)
	respondJSONOK(w, rl)
}

// DeleteFinanceAlertRule handles DELETE /api/p3m/finance-alert-rules/{id}.
func (h *P3MHandler) DeleteFinanceAlertRule(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_finance_alert_rules WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_finance_alert_rule")
		return
	}
	logAudit(h.db, r, user, "p3m_finance_alert_rule_deleted", "p3m_finance_alert_rule", &id, "")
	respondJSONOK(w, map[string]any{"deleted": true})
}

// p3mFinanceMetrics computes the three alert metrics for one project:
// variance_vs_budget (actual vs approved), forecast_overrun_pct (forecast at
// completion = actual + outstanding tranche commitments, vs approved) and
// commitment_vs_remaining (outstanding commitments as a percentage of the
// remaining budget; undefined when no budget remains). A nil value means the
// metric is not computable for that project.
func (h *P3MHandler) p3mFinanceMetrics(ctx context.Context, query string, args ...any) map[string]*float64 {
	out := map[string]*float64{}

	var approved, actual float64
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(budget_approved), 0), COALESCE(SUM(budget_actual), 0) FROM p3m_projects WHERE `+query, args...).Scan(&approved, &actual); err != nil {
		return out
	}

	var committed float64
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(t.amount), 0) FROM p3m_budget_tranches t JOIN p3m_projects p ON p.id = t.entity_id AND t.entity_type = 'project' WHERE `+query, args...).Scan(&committed); err != nil {
		return out
	}

	if approved > 0 {
		variance := (actual - approved) / approved * 100
		out["variance_vs_budget"] = &variance
		forecast := (actual + committed - approved) / approved * 100
		out["forecast_overrun_pct"] = &forecast
	}
	if remaining := approved - actual; remaining > 0 {
		ratio := committed / remaining * 100
		out["commitment_vs_remaining"] = &ratio
	}
	return out
}

// p3mCompare applies a rule comparator to a metric value.
func p3mCompare(value float64, comparator string, threshold float64) bool {
	switch comparator {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	default:
		return false
	}
}

// p3mEvaluateEnabledRules evaluates the enabled finance alert rules matching
// where against the current financial position of each rule's scope and
// returns the triggered alerts. It is read-only: callers decide on auditing,
// notifications or RAID drafts.
func (h *P3MHandler) p3mEvaluateEnabledRules(ctx context.Context, where string, args ...any) ([]models.P3MFinanceAlertRule, []map[string]any, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mFinanceAlertRuleColumns+` FROM p3m_finance_alert_rules WHERE enabled = 1 AND (`+where+`) ORDER BY id`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	rules := make([]models.P3MFinanceAlertRule, 0)
	for rows.Next() {
		rl, err := scanP3MFinanceAlertRule(rows)
		if err != nil {
			return nil, nil, err
		}
		rules = append(rules, rl)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	alerts := make([]map[string]any, 0)
	for _, rule := range rules {
		var metrics map[string]*float64
		switch rule.Scope {
		case "portfolio":
			metrics = h.p3mFinanceMetrics(ctx, `p.portfolio_id = ?`, *rule.PortfolioID)
		default:
			metrics = h.p3mFinanceMetrics(ctx, `p.id = ?`, *rule.ProjectID)
		}

		value, defined := metrics[rule.Metric]
		if !defined || value == nil {
			continue
		}
		if !p3mCompare(*value, rule.Comparator, rule.Threshold) {
			continue
		}

		alert := map[string]any{
			"rule_id":    rule.ID,
			"name":       rule.Name,
			"scope":      rule.Scope,
			"metric":     rule.Metric,
			"value":      round2(value),
			"comparator": rule.Comparator,
			"threshold":  rule.Threshold,
		}
		if rule.PortfolioID != nil {
			alert["portfolio_id"] = *rule.PortfolioID
		}
		if rule.ProjectID != nil {
			alert["project_id"] = *rule.ProjectID
		}
		if rule.NotifyEmail != nil && *rule.NotifyEmail != "" {
			alert["notify_email"] = *rule.NotifyEmail
		}
		alerts = append(alerts, alert)
	}
	return rules, alerts, nil
}

// p3mAuditFinanceAlerts emits one audit event per triggered rule.
func (h *P3MHandler) p3mAuditFinanceAlerts(r *http.Request, user *models.User, alerts []map[string]any) {
	for _, alert := range alerts {
		ruleID, _ := alert["rule_id"].(int)
		id := ruleID
		logAudit(h.db, r, user, "p3m_finance_alert_triggered", "p3m_finance_alert_rule", &id,
			fmt.Sprintf("%v=%v %v %v", alert["metric"], alert["value"], alert["comparator"], alert["threshold"]))
	}
}

// EvaluateFinanceAlerts handles POST /api/p3m/evaluate-finance-alerts. Every
// enabled rule is evaluated against the current financial position of its
// scope; triggered rules are returned and audit-emitted (FIN-009). The body is
// ignored, so the endpoint can be wired to a scheduler unchanged.
func (h *P3MHandler) EvaluateFinanceAlerts(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	rules, alerts, err := h.p3mEvaluateEnabledRules(r.Context(), `1=1`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAuditFinanceAlerts(r, user, alerts)

	respondJSONOK(w, map[string]any{
		"evaluated_at":    time.Now().UTC().Format(time.RFC3339),
		"rules_evaluated": len(rules),
		"alerts":          alerts,
	})
}

// EvaluateProjectFinanceAlerts handles POST /api/p3m/projects/{id}/evaluate-finance-alerts:
// the read-only evaluation of every rule that applies to one project - its own
// project-scoped rules plus the rules of its parent portfolio (FIN-009).
func (h *P3MHandler) EvaluateProjectFinanceAlerts(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var portfolioID sql.NullInt64
	if err := h.db.QueryRowContext(ctx, `SELECT portfolio_id FROM p3m_projects WHERE id = ?`, id).Scan(&portfolioID); err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	where := `scope = 'project' AND project_id = ?`
	args := []any{id}
	if portfolioID.Valid {
		where += ` OR (scope = 'portfolio' AND portfolio_id = ?)`
		args = append(args, int(portfolioID.Int64))
	}

	rules, alerts, err := h.p3mEvaluateEnabledRules(ctx, where, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.p3mAuditFinanceAlerts(r, user, alerts)

	respondJSONOK(w, map[string]any{
		"project_id":      id,
		"evaluated_at":    time.Now().UTC().Format(time.RFC3339),
		"rules_evaluated": len(rules),
		"alerts":          alerts,
	})
}
