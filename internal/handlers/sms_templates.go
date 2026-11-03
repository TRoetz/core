package handlers

// Template artifacts (WI-5.3): first-class export/form templates served as
// downloadable files instead of living in documentation. Each template is a
// deterministic rendering of live register data (or the import contract), so
// what the pilot team fills in and hands over matches what the system holds.

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"windshift/internal/database"
)

type smsTemplateSpec struct {
	name        string
	title       string
	format      string // "csv" or "json"
	description string
	generate    func(h *SMSHandler, ctx context.Context) (string, []byte, error)
}

func smsTemplates() []smsTemplateSpec {
	return []smsTemplateSpec{
		{"stakeholder_register", "Stakeholder register", "csv", "Full register export: every stakeholder with all profile fields.", smsTplStakeholderRegister},
		{"engagement_plan", "Engagement plan", "csv", "One row per engagement-plan / stakeholder pair with strategy, objective and approach.", smsTplEngagementPlan},
		{"communication_plan", "Communication plan", "csv", "Channel, frequency and key messages per engagement plan and stakeholder.", smsTplCommunicationPlan},
		{"risk_assessment", "Risk assessment", "csv", "Open and resolved issues with category, priority, impact, likelihood and mitigation.", smsTplRiskAssessment},
		{"commitment_register", "Commitment register", "csv", "Every commitment made to or by a stakeholder with owner, due date and status.", smsTplCommitmentRegister},
		{"health_assessment", "Health assessment", "csv", "Relationship status, trust level and computed health score per stakeholder.", smsTplHealthAssessment},
		{"executive_briefing", "Executive briefing", "json", "Rollup of totals, distributions and open items for the executive tier (Std §17).", smsTplExecutiveBriefing},
		{"handover", "Relationship transition / handover", "csv", "Ownership transfers with review checklists and completion state.", smsTplHandover},
		{"mapping_canvas", "Mapping canvas", "csv", "Stakeholder-to-stakeholder links (relation type + notes) for the mapping exercise.", smsTplMappingCanvas},
		{"data_dictionary", "Data dictionary", "csv", "Generated from the live schema: every SMS table and column with its declared type.", smsTplDataDictionary},
		{"import_template", "CSV import template", "csv", "Header + sample rows matching POST /sms/import/preview expectations.", smsTplImportTemplate},
		{"pilot_baselines", "Pilot baselines", "csv", "Recorded baseline vs target values for the pilot success measures (Doc 7).", smsTplPilotBaselines},
	}
}

// ListTemplates returns the catalog of downloadable template artifacts.
func (h *SMSHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	out := make([]map[string]any, 0, len(smsTemplates()))
	for _, t := range smsTemplates() {
		out = append(out, map[string]any{
			"name":        t.name,
			"title":       t.title,
			"format":      t.format,
			"description": t.description,
		})
	}
	respondJSONOK(w, out)
}

// DownloadTemplate renders one template artifact as a file download.
func (h *SMSHandler) DownloadTemplate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	var spec *smsTemplateSpec
	for i := range smsTemplates() {
		if smsTemplates()[i].name == name {
			spec = &smsTemplates()[i]
			break
		}
	}
	if spec == nil {
		respondNotFound(w, r, "template")
		return
	}
	filename, body, err := spec.generate(h, r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_template_exported", "sms_template", nil, name, map[string]any{"format": spec.format})
	w.Header().Set("Content-Type", mimeForTemplate(spec.format))
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write(body)
}

func mimeForTemplate(format string) string {
	if format == "json" {
		return "application/json"
	}
	return "text/csv"
}

// --- rendering helpers ------------------------------------------------------

func smsTemplateCSV(rows [][]string) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.WriteAll(rows)
	w.Flush()
	return buf.Bytes()
}

func smsTplCell(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case time.Time:
		if val.IsZero() {
			return ""
		}
		return val.UTC().Format(time.RFC3339)
	case float64:
		if val == math.Trunc(val) && val >= -1e15 && val <= 1e15 {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// smsTplRows runs a query and returns rows as string cells in column order.
func (h *SMSHandler) smsTplRows(ctx context.Context, query string, args ...any) ([][]string, error) {
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := [][]string{make([]string, len(cols))}
	for i, c := range cols {
		out[0][i] = c
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = smsTplCell(v)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// mustInt parses a template cell holding an integer column.
func mustInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// --- generators -------------------------------------------------------------

func smsTplStakeholderRegister(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT `+smsStakeholderColumns+` FROM sms_stakeholders ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return "", nil, err
	}
	return "stakeholder_register.csv", smsTemplateCSV(rows), nil
}

func smsTplEngagementPlan(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT p.id AS plan_id, s.name AS stakeholder, p.strategy, p.objective, p.approach, p.status, p.start_date, p.end_date FROM sms_engagement_plans p LEFT JOIN sms_engagement_plan_stakeholders ps ON ps.plan_id = p.id LEFT JOIN sms_stakeholders s ON s.id = ps.stakeholder_id ORDER BY p.id, s.name`)
	if err != nil {
		return "", nil, err
	}
	for _, row := range rows[1:] {
		if row[1] == "" { // plan without stakeholder rows: keep readable
			row[1] = fmt.Sprintf("(plan %s)", row[0])
		}
	}
	return "engagement_plan.csv", smsTemplateCSV(rows), nil
}

func smsTplCommunicationPlan(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT p.id AS plan_id, s.name AS stakeholder, p.channel, p.frequency, p.key_messages, p.success_measures FROM sms_engagement_plans p LEFT JOIN sms_engagement_plan_stakeholders ps ON ps.plan_id = p.id LEFT JOIN sms_stakeholders s ON s.id = ps.stakeholder_id ORDER BY p.id, s.name`)
	if err != nil {
		return "", nil, err
	}
	return "communication_plan.csv", smsTemplateCSV(rows), nil
}

func smsTplRiskAssessment(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT i.id, s.name AS stakeholder, i.category, i.title, i.priority, i.impact, i.likelihood, i.status, i.escalation_level, i.mitigation, i.resolution_notes, i.resolved_at FROM sms_issues i LEFT JOIN sms_stakeholders s ON s.id = i.stakeholder_id ORDER BY CASE WHEN i.status = 'open' THEN 0 ELSE 1 END, i.priority DESC, i.id`)
	if err != nil {
		return "", nil, err
	}
	return "risk_assessment.csv", smsTemplateCSV(rows), nil
}

func smsTplCommitmentRegister(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT c.id, s.name AS stakeholder, c.direction, c.title, c.owner_user_id, c.due_date, c.status, c.escalated, c.notes FROM sms_commitments c LEFT JOIN sms_stakeholders s ON s.id = c.stakeholder_id ORDER BY CASE WHEN c.status = 'open' THEN 0 ELSE 1 END, c.due_date, c.id`)
	if err != nil {
		return "", nil, err
	}
	return "commitment_register.csv", smsTemplateCSV(rows), nil
}

func smsTplHealthAssessment(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT s.id, s.name, s.tier, s.relationship_status, COALESCE(s.trust_level, '') AS trust_level, s.engagement_status, (SELECT COUNT(*) FROM sms_issues i WHERE i.stakeholder_id = s.id AND i.status = 'open' AND i.priority = 'critical') AS open_critical, (SELECT COUNT(*) FROM sms_issues i WHERE i.stakeholder_id = s.id AND i.status = 'open' AND i.escalation_level > 0) AS escalated_open FROM sms_stakeholders s ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return "", nil, err
	}
	out := [][]string{{"id", "name", "tier", "relationship_status", "trust_level", "engagement_status", "open_critical_issues", "escalated_open_issues", "health_score"}}
	for _, row := range rows[1:] {
		score := smsHealthScore(row[3], row[5], mustInt(row[6]), mustInt(row[7]))
		out = append(out, append(append([]string{}, row...), strconv.Itoa(score)))
	}
	return "health_assessment.csv", smsTemplateCSV(out), nil
}

func smsTplExecutiveBriefing(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rollup, err := h.smsRollup(ctx, "", nil)
	if err != nil {
		return "", nil, err
	}
	body, err := json.MarshalIndent(map[string]any{
		"module":       "sms",
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"data":         rollup,
	}, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return "executive_briefing.json", body, nil
}

func smsTplHandover(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT t.id, s.name AS stakeholder, t.from_user_id, t.to_user_id, t.reason, t.history_reviewed, t.risks_reviewed, t.commitments_reviewed, t.notes, t.completed_at FROM sms_ownership_transfers t LEFT JOIN sms_stakeholders s ON s.id = t.stakeholder_id ORDER BY t.created_at DESC, t.id`)
	if err != nil {
		return "", nil, err
	}
	return "handover.csv", smsTemplateCSV(rows), nil
}

func smsTplMappingCanvas(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT l.id, s1.name AS source, l.source_id, l.relation_type, l.target_id, s2.name AS target, l.notes FROM sms_stakeholder_links l LEFT JOIN sms_stakeholders s1 ON s1.id = l.source_id LEFT JOIN sms_stakeholders s2 ON s2.id = l.target_id ORDER BY l.id`)
	if err != nil {
		return "", nil, err
	}
	return "mapping_canvas.csv", smsTemplateCSV(rows), nil
}

func smsTplDataDictionary(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	var rows [][]string
	var err error
	if database.IsPostgresDriver(h.db.GetDriverName()) {
		rows, err = h.smsTplRows(ctx, `SELECT table_name, column_name, data_type, is_nullable, COALESCE(column_default, '') FROM information_schema.columns WHERE table_name LIKE 'sms\_%' ORDER BY table_name, ordinal_position`)
	} else {
		tables, err := h.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'sms\_%' ESCAPE '\' ORDER BY name`)
		if err != nil {
			return "", nil, err
		}
		var names []string
		for tables.Next() {
			var n string
			if err := tables.Scan(&n); err != nil {
				tables.Close()
				return "", nil, err
			}
			names = append(names, n)
		}
		err = tables.Err()
		tables.Close()
		if err != nil {
			return "", nil, err
		}
		rows = [][]string{{"table_name", "column_name", "data_type", "notnull", "default"}}
		for _, n := range names {
			info, err := h.db.QueryContext(ctx, `PRAGMA table_info(`+n+`)`)
			if err != nil {
				return "", nil, err
			}
			for info.Next() {
				var cid int
				var col, typ string
				var notnull, pk int
				var dflt any
				if err := info.Scan(&cid, &col, &typ, &notnull, &dflt, &pk); err != nil {
					info.Close()
					return "", nil, err
				}
				rows = append(rows, []string{n, col, typ, strconv.Itoa(notnull), smsTplCell(dflt)})
			}
			err = info.Err()
			info.Close()
			if err != nil {
				return "", nil, err
			}
		}
	}
	if err != nil {
		return "", nil, err
	}
	return "data_dictionary.csv", smsTemplateCSV(rows), nil
}

func smsTplImportTemplate(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows := [][]string{
		{"name", "title", "organization", "email", "phone", "tier", "relationship_status", "engagement_status", "interests", "notes"},
		{"Jane Example", "Community Lead", "Example Org", "jane@example.org", "+64 21 000 0000", "tier_1", "advocate", "active", "funding, governance", "sample row - replace with real data"},
		{"John Sample", "Project Manager", "Sample Co", "john@sample.co", "", "tier_2", "supportive", "engaged", "timeline", "second sample row"},
	}
	return "sms_import_template.csv", smsTemplateCSV(rows), nil
}

func smsTplPilotBaselines(h *SMSHandler, ctx context.Context) (string, []byte, error) {
	rows, err := h.smsTplRows(ctx, `SELECT metric_key, baseline_value, target_value, set_at FROM sms_pilot_baselines ORDER BY metric_key`)
	if err != nil {
		return "", nil, err
	}
	return "pilot_baselines.csv", smsTemplateCSV(rows), nil
}
