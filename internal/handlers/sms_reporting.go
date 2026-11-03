package handlers

import (
	"context"
	"net/http"
	"sort"
	"time"
)

// smsRollup computes the shared dashboard rollup for an optional stakeholder
// scope. scopeCond is a SQL fragment applied to sms_stakeholders.id ("" = all);
// args are its bind parameters. Used by both the executive and programme
// dashboards so the two views stay consistent (WI-4.1/WI-4.2).
func (h *SMSHandler) smsRollup(ctx context.Context, scopeCond string, args []any) (map[string]any, error) {
	where := ""
	if scopeCond != "" {
		where = " WHERE " + scopeCond
	}

	var total int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders`+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	counts := func(query string, cargs ...any) ([]smsCountRow, error) {
		return h.smsGroupCounts(ctx, query, cargs...)
	}

	// scopedIn restricts sub-resource counts to the same stakeholder scope via an
	// IN subquery carrying the scope's bind args once.
	scopedIn := ""
	if where != "" {
		scopedIn = " AND stakeholder_id IN (SELECT id FROM sms_stakeholders" + where + ")"
	}
	byTier, err := counts(`SELECT tier, COUNT(*) FROM sms_stakeholders`+where+` GROUP BY tier ORDER BY tier`, args...)
	if err != nil {
		return nil, err
	}
	relationshipDistribution, err := counts(`SELECT relationship_status, COUNT(*) FROM sms_stakeholders`+where+` GROUP BY relationship_status ORDER BY COUNT(*) DESC, relationship_status`, args...)
	if err != nil {
		return nil, err
	}

	critical, escalated, err := h.smsIssuePressure(ctx)
	if err != nil {
		return nil, err
	}

	buckets := []smsCountRow{{Value: "0-25"}, {Value: "26-50"}, {Value: "51-75"}, {Value: "76-100"}}
	atRisk := make([]map[string]any, 0)
	rows, err := h.db.QueryContext(ctx, `SELECT id, name, organization, tier, relationship_status, engagement_status FROM sms_stakeholders`+where+` ORDER BY name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id           int
			name         string
			org          string
			tier         string
			relationship string
			engagement   string
		)
		if err := rows.Scan(&id, &name, &org, &tier, &relationship, &engagement); err != nil {
			rows.Close()
			return nil, err
		}
		score := smsHealthScore(relationship, engagement, critical[id], escalated[id])
		idx := 0
		if score > 0 {
			idx = (score - 1) / 25
		}
		if idx > 3 {
			idx = 3
		}
		buckets[idx].Count++
		if score < 50 || critical[id] > 0 {
			atRisk = append(atRisk, map[string]any{
				"stakeholder_id":        id,
				"name":                  name,
				"organization":          org,
				"tier":                  tier,
				"relationship_status":   relationship,
				"health_score":          score,
				"open_critical_issues":  critical[id],
			})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Cap the at-risk list for display; ordering is by ascending health score.
	sort.Slice(atRisk, func(i, j int) bool {
		a, _ := atRisk[i]["health_score"].(int)
		b, _ := atRisk[j]["health_score"].(int)
		return a < b
	})
	if len(atRisk) > 10 {
		atRisk = atRisk[:10]
	}

	now := time.Now().UTC()
	dueSoon, overdue := 0, 0
	crows, err := h.db.QueryContext(ctx, `SELECT c.due_date FROM sms_commitments c JOIN sms_stakeholders s ON s.id = c.stakeholder_id WHERE c.status IN ('open','in_progress','at_risk') AND c.due_date IS NOT NULL`+scopedIn, args...)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var d time.Time
		if err := crows.Scan(&d); err != nil {
			crows.Close()
			return nil, err
		}
		if d.Before(now) {
			overdue++
		} else if !d.After(now.AddDate(0, 0, 30)) {
			dueSoon++
		}
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		return nil, err
	}
	crows.Close()

	var commitmentsOpen, grievancesEscalated int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_commitments WHERE status IN ('open','in_progress','at_risk')`+scopedIn, args...).Scan(&commitmentsOpen); err != nil {
		return nil, err
	}
	grievanceSeverity, err := counts(`SELECT severity, COUNT(*) FROM sms_grievances WHERE status <> 'closed'`+scopedIn+` GROUP BY severity ORDER BY severity`, args...)
	if err != nil {
		return nil, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_grievances WHERE escalated = 1 AND status <> 'closed'`+scopedIn, args...).Scan(&grievancesEscalated); err != nil {
		return nil, err
	}

	var issuesEscalated, issuesCriticalOpen int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE status = 'escalated'`+scopedIn, args...).Scan(&issuesEscalated); err != nil {
		return nil, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE priority = 'critical' AND status IN ('open','in_progress','escalated')`+scopedIn, args...).Scan(&issuesCriticalOpen); err != nil {
		return nil, err
	}

	var activePlans int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT stakeholder_id) FROM sms_engagement_plans WHERE status = 'active'`+scopedIn, args...).Scan(&activePlans); err != nil {
		return nil, err
	}
	coveragePct := 0.0
	if total > 0 {
		coveragePct = float64(activePlans) / float64(total) * 100
	}

	return map[string]any{
		"totals": map[string]any{
			"stakeholders":             total,
			"by_tier":                  byTier,
			"relationship_distribution": relationshipDistribution,
		},
		"health_buckets":          buckets,
		"at_risk":                 atRisk,
		"commitments": map[string]any{
			"open":           commitmentsOpen,
			"due_within_30d": dueSoon,
			"overdue":        overdue,
		},
		"grievances": map[string]any{
			"open_by_severity": grievanceSeverity,
			"escalated_open":   grievancesEscalated,
		},
		"issues": map[string]any{
			"escalated":       issuesEscalated,
			"critical_open":   issuesCriticalOpen,
		},
		"engagement_coverage_pct": coveragePct,
	}, nil
}

// GetExecutiveDashboard returns the portfolio-wide stakeholder rollup for the
// executive view (WI-4.1, Doc 5 §3).
func (h *SMSHandler) GetExecutiveDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	out, err := h.smsRollup(r.Context(), "", nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetProgrammeDashboard returns the rollup scoped to stakeholders linked to one
// P3M portfolio/programme/project (WI-4.2). Query params: entity_type
// (portfolio|programme|project), entity_id.
func (h *SMSHandler) GetProgrammeDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	q := r.URL.Query()
	entityType := q.Get("entity_type")
	table, valid := smsP3MEntityTables[entityType]
	if !valid {
		respondBadRequest(w, r, "entity_type must be portfolio, programme or project")
		return
	}
	entityID, ok := parseQueryInt(q.Get("entity_id"))
	if !ok || entityID <= 0 {
		respondBadRequest(w, r, "entity_id is required")
		return
	}

	out, err := h.smsRollup(r.Context(), `id IN (SELECT stakeholder_id FROM sms_stakeholder_entities WHERE entity_type = ? AND entity_id = ?)`, []any{entityType, entityID})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	out["scope"] = map[string]any{"table": table, "entity_type": entityType, "entity_id": entityID}
	respondJSONOK(w, out)
}

// smsPilotMetricCurrents computes the current value for every known pilot
// success-measure key (WI-4.5). Unknown keys are simply absent from the map.
func (h *SMSHandler) smsPilotMetricCurrents(ctx context.Context) (map[string]float64, error) {
	out := map[string]float64{}

	var stakeholderCount int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders`).Scan(&stakeholderCount); err != nil {
		return nil, err
	}
	out["stakeholder_count"] = float64(stakeholderCount)

	var activePlans int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_engagement_plans WHERE status = 'active'`).Scan(&activePlans); err != nil {
		return nil, err
	}
	out["active_engagement_plans"] = float64(activePlans)

	var stakeholdersWithPlan int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT stakeholder_id) FROM sms_engagement_plans WHERE status = 'active'`).Scan(&stakeholdersWithPlan); err != nil {
		return nil, err
	}
	if stakeholderCount > 0 {
		out["engagement_coverage_pct"] = float64(stakeholdersWithPlan) / float64(stakeholderCount) * 100
	}

	var completed, nonCancelled int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_commitments WHERE status = 'completed'`).Scan(&completed); err != nil {
		return nil, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_commitments WHERE status <> 'cancelled'`).Scan(&nonCancelled); err != nil {
		return nil, err
	}
	if nonCancelled > 0 {
		out["commitments_completion_rate_pct"] = float64(completed) / float64(nonCancelled) * 100
	}

	var grievancesClosed, grievancesTotal int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_grievances WHERE status = 'closed'`).Scan(&grievancesClosed); err != nil {
		return nil, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_grievances`).Scan(&grievancesTotal); err != nil {
		return nil, err
	}
	if grievancesTotal > 0 {
		out["grievances_closed_rate_pct"] = float64(grievancesClosed) / float64(grievancesTotal) * 100
	}

	var issuesResolved, issuesTotal int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE status IN ('resolved','closed')`).Scan(&issuesResolved); err != nil {
		return nil, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues`).Scan(&issuesTotal); err != nil {
		return nil, err
	}
	if issuesTotal > 0 {
		out["issues_resolved_rate_pct"] = float64(issuesResolved) / float64(issuesTotal) * 100
	}

	return out, nil
}

// GetPilotMetrics compares each configured pilot success measure against its
// baseline and target (WI-4.5, Doc 8 Go/No-Go evidence).
func (h *SMSHandler) GetPilotMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	currents, err := h.smsPilotMetricCurrents(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT metric_key, baseline_value, target_value FROM sms_pilot_baselines ORDER BY metric_key`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	metrics := make([]map[string]any, 0)
	for rows.Next() {
		var key string
		var baseline, target *float64
		if err := rows.Scan(&key, &baseline, &target); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		m := map[string]any{"metric_key": key, "baseline_value": baseline, "target_value": target}
		if current, ok := currents[key]; ok {
			m["current_value"] = current
			if baseline != nil {
				m["delta"] = current - *baseline
			}
			if target == nil {
				m["status"] = "no_target"
			} else if current >= *target {
				m["status"] = "on_track"
			} else {
				m["status"] = "behind"
			}
		} else {
			m["current_value"] = nil
			m["status"] = "unknown_metric"
		}
		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"generated_at": time.Now().UTC(),
		"metrics":      metrics,
	})
}
