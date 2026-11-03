package handlers

// Full-register restore (WI-5.1 / WI-5.2 exit guardrail): accepts the flat
// JSON produced by GET /sms/export/full and re-inserts every row with its
// original ID, in FK-safe order, inside one transaction. With "wipe": true the
// register is cleared first so export -> wipe -> import round-trips exactly.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"windshift/internal/database"
)

// smsRestoreTableSpec describes one table of the full export: its name, the
// ordered column list used for insertion and which columns hold timestamps.
type smsRestoreTableSpec struct {
	table    string
	columns  []string
	timeCols map[string]bool
}

func smsTimeCols(cols ...string) map[string]bool {
	m := make(map[string]bool, len(cols))
	for _, c := range cols {
		m[c] = true
	}
	return m
}

// smsRestoreSpecs lists every exported table in FK-safe insertion order.
// Column lists mirror the export JSON keys (model JSON tags == column names).
func smsRestoreSpecs() []smsRestoreTableSpec {
	return []smsRestoreTableSpec{
		{table: "sms_stakeholders", columns: strings.Split(smsStakeholderColumns, ", "), timeCols: smsTimeCols("last_engagement_at", "next_engagement_at", "last_reviewed_at", "created_at", "updated_at")},
		{table: "sms_communications", columns: strings.Split(smsCommunicationColumns, ", "), timeCols: smsTimeCols("created_at")},
		{table: "sms_engagement_plans", columns: strings.Split(smsPlanColumns, ", "), timeCols: smsTimeCols("start_date", "end_date", "created_at", "updated_at")},
		{table: "sms_engagement_plan_stakeholders", columns: []string{"id", "plan_id", "stakeholder_id"}, timeCols: nil},
		{table: "sms_issues", columns: strings.Split(smsIssueColumns, ", "), timeCols: smsTimeCols("resolved_at", "created_at", "updated_at")},
		{table: "sms_tasks", columns: strings.Split(smsTaskColumns, ", "), timeCols: smsTimeCols("completed_at", "created_at", "updated_at")},
		{table: "sms_feedback", columns: strings.Split(smsFeedbackColumns, ", "), timeCols: smsTimeCols("created_at", "updated_at")},
		{table: "sms_relationship_goals", columns: strings.Split(smsGoalColumns, ", "), timeCols: smsTimeCols("created_at", "updated_at")},
		{table: "sms_commitments", columns: strings.Split(smsCommitmentColumns, ", "), timeCols: smsTimeCols("due_date", "created_at", "updated_at")},
		{table: "sms_grievances", columns: strings.Split(smsGrievanceColumns, ", "), timeCols: smsTimeCols("acknowledged_at", "response_due_at", "created_at", "updated_at")},
		{table: "sms_decisions", columns: strings.Split(smsDecisionColumns, ", "), timeCols: smsTimeCols("decided_at", "created_at")},
		{table: "sms_stakeholder_groups", columns: []string{"id", "name", "description", "created_at"}, timeCols: smsTimeCols("created_at")},
		{table: "sms_stakeholder_group_members", columns: []string{"id", "group_id", "stakeholder_id"}, timeCols: nil},
		{table: "sms_stakeholder_links", columns: strings.Split(smsLinkColumns, ", "), timeCols: smsTimeCols("created_at")},
		{table: "sms_ownership_transfers", columns: strings.Split(smsTransferColumns, ", "), timeCols: smsTimeCols("completed_at", "created_at")},
		{table: "sms_stakeholder_entities", columns: []string{"id", "stakeholder_id", "entity_type", "entity_id", "created_at"}, timeCols: smsTimeCols("created_at")},
	}
}

// smsRestoreValue normalises one decoded JSON value for insertion into the
// given column. Whole-number floats become int64 (JSON has no integer type);
// timestamp columns accept RFC3339 strings and empty/missing values as NULL.
func smsRestoreValue(col string, v any, isTime bool) (any, error) {
	switch val := v.(type) {
	case nil:
		return nil, nil
	case float64:
		if val == math.Trunc(val) && val >= math.MinInt32 && val <= math.MaxInt32 {
			return int64(val), nil
		}
		return val, nil
	case string:
		if !isTime {
			return val, nil
		}
		if val == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp %q", val)
		}
		return t, nil
	default:
		return v, nil
	}
}

// smsRestoreTable inserts one exported table's rows with explicit IDs.
func smsRestoreTable(ctx context.Context, tx database.Tx, spec smsRestoreTableSpec, rows []map[string]any) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	query := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		spec.table, strings.Join(spec.columns, ", "),
		strings.TrimSuffix(strings.Repeat("?,", len(spec.columns)), ","))
	n := 0
	for _, row := range rows {
		args := make([]any, len(spec.columns))
		for i, col := range spec.columns {
			v, ok := row[col]
			if !ok || v == nil {
				continue // NULL
			}
			nv, err := smsRestoreValue(col, v, spec.timeCols != nil && spec.timeCols[col])
			if err != nil {
				return n, fmt.Errorf("%s.%s: %w", spec.table, col, err)
			}
			args[i] = nv
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return n, fmt.Errorf("%s row id=%v: %w", spec.table, row["id"], err)
		}
		n++
	}
	return n, nil
}

// ImportFull restores a full JSON export (GET /sms/export/full) preserving IDs
// and FK relationships. Body is the exported file itself; optional "wipe":
// true clears every SMS table first so the restore reproduces the source
// register exactly (Doc 8 exit guardrail test). Audit-logged with counts.
func (h *SMSHandler) ImportFull(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Module            string         `json:"module"`
		SchemaVersion     int            `json:"schema_version"`
		Wipe              bool           `json:"wipe"`
		Stakeholders      []map[string]any `json:"stakeholders"`
		Communications    []map[string]any `json:"communications"`
		EngagementPlans   []map[string]any `json:"engagement_plans"`
		PlanStakeholders  []map[string]any `json:"plan_stakeholders"`
		Issues            []map[string]any `json:"issues"`
		Tasks             []map[string]any `json:"tasks"`
		Feedback          []map[string]any `json:"feedback"`
		RelationshipGoals []map[string]any `json:"relationship_goals"`
		Commitments       []map[string]any `json:"commitments"`
		Grievances        []map[string]any `json:"grievances"`
		Decisions         []map[string]any `json:"decisions"`
		Groups            []map[string]any `json:"groups"`
		GroupMembers      []map[string]any `json:"group_members"`
		Links             []map[string]any `json:"links"`
		Transfers         []map[string]any `json:"transfers"`
		EntityLinks       []map[string]any `json:"entity_links"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Module != "" && in.Module != "sms" {
		respondBadRequest(w, r, `module must be "sms"`)
		return
	}
	if in.SchemaVersion != smsExportSchemaVersion {
		respondBadRequest(w, r, fmt.Sprintf("unsupported schema_version %d (want %d)", in.SchemaVersion, smsExportSchemaVersion))
		return
	}

	collections := map[string][]map[string]any{
		"sms_stakeholders":                in.Stakeholders,
		"sms_communications":              in.Communications,
		"sms_engagement_plans":            in.EngagementPlans,
		"sms_engagement_plan_stakeholders": in.PlanStakeholders,
		"sms_issues":                      in.Issues,
		"sms_tasks":                       in.Tasks,
		"sms_feedback":                    in.Feedback,
		"sms_relationship_goals":          in.RelationshipGoals,
		"sms_commitments":                 in.Commitments,
		"sms_grievances":                  in.Grievances,
		"sms_decisions":                   in.Decisions,
		"sms_stakeholder_groups":          in.Groups,
		"sms_stakeholder_group_members":   in.GroupMembers,
		"sms_stakeholder_links":           in.Links,
		"sms_ownership_transfers":         in.Transfers,
		"sms_stakeholder_entities":        in.EntityLinks,
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if in.Wipe {
		specs := smsRestoreSpecs()
		for i := len(specs) - 1; i >= 0; i-- { // children first
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+specs[i].table); err != nil {
				respondInternalError(w, r, err)
				return
			}
		}
	}

	counts := make(map[string]int, len(collections))
	total := 0
	for _, spec := range smsRestoreSpecs() {
		n, err := smsRestoreTable(ctx, tx, spec, collections[spec.table])
		if err != nil {
			respondBadRequest(w, r, err.Error())
			return
		}
		counts[spec.table] = n
		total += n
	}

	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Explicit IDs do not advance Postgres sequences; realign them so future
	// inserts continue after the restored maximum.
	if database.IsPostgresDriver(h.db.GetDriverName()) {
		for _, spec := range smsRestoreSpecs() {
			if counts[spec.table] == 0 {
				continue
			}
			_, _ = h.db.ExecContext(ctx, fmt.Sprintf(
				`SELECT setval(pg_get_serial_sequence('%s', 'id'), (SELECT MAX(id) FROM %s))`, spec.table, spec.table))
		}
	}

	smsAudit(h, r, user, "sms_stakeholders_restored", "sms_stakeholder", nil, fmt.Sprintf("%d rows", total), map[string]any{
		"wipe":   in.Wipe,
		"counts": counts,
	})
	respondJSONOK(w, map[string]any{
		"wiped":    in.Wipe,
		"restored": counts,
		"total":    total,
	})
}
