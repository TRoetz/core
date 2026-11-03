package handlers

// Data movement for the Stakeholder Management System module: CSV import with
// preview and dedupe (WI-5.1), migration-ready full export + manifest
// (WI-5.2) and GeoJSON location export (WI-6.4).

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// CSV import (preview + execute with dedupe)
// ---------------------------------------------------------------------------

type smsImportRow struct {
	Line               int    // 1-based line number in the submitted CSV (header = 1)
	Name               string `json:"name"`
	Title              string `json:"title,omitempty"`
	Organization       string `json:"organization,omitempty"`
	Email              string `json:"email,omitempty"`
	Phone              string `json:"phone,omitempty"`
	Tier               string `json:"tier,omitempty"`
	RelationshipStatus string `json:"relationship_status,omitempty"`
	EngagementStatus   string `json:"engagement_status,omitempty"`
	Interests          string `json:"interests,omitempty"`
	Notes              string `json:"notes,omitempty"`
}

type smsImportError struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type smsImportDuplicate struct {
	Row        int    `json:"row"`
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	ExistingID int    `json:"existing_id"`
	MatchOn    string `json:"match_on"` // "email" or "name_organization"
}

// smsParseImportCSV parses a stakeholder register CSV with a required header
// row. Unknown columns are ignored; missing optional columns stay empty.
func smsParseImportCSV(csvText string) ([]smsImportRow, []smsImportError, error) {
	reader := csv.NewReader(strings.NewReader(csvText))
	reader.FieldsPerRecord = -1 // validated per row below
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, []smsImportError{{Row: 0, Field: "", Message: "CSV is empty"}}, nil
	}

	header := make(map[string]int, len(records[0]))
	for i, col := range records[0] {
		header[strings.ToLower(strings.TrimSpace(col))] = i
	}
	if _, ok := header["name"]; !ok {
		return nil, []smsImportError{{Row: 1, Field: "header", Message: `missing required column "name"`}}, nil
	}

	get := func(record []string, col string) string {
		idx, ok := header[col]
		if !ok || idx >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[idx])
	}

	rows := make([]smsImportRow, 0, len(records)-1)
	var errors []smsImportError
	for _, record := range records[1:] {
		line := len(rows) + len(errors) + 2 // header is line 1; count consumed data lines
		row := smsImportRow{Line: line}
		if allEmpty(record) {
			continue // skip blank lines
		}
		row.Name = get(record, "name")
		row.Title = get(record, "title")
		row.Organization = get(record, "organization")
		row.Email = get(record, "email")
		row.Phone = get(record, "phone")
		row.Tier = get(record, "tier")
		row.RelationshipStatus = get(record, "relationship_status")
		row.EngagementStatus = get(record, "engagement_status")
		row.Interests = get(record, "interests")
		row.Notes = get(record, "notes")

		if row.Name == "" {
			errors = append(errors, smsImportError{Row: line, Field: "name", Message: "name is required"})
			continue
		}
		if row.Tier != "" && !smsValidEnum(row.Tier, smsTiers...) {
			errors = append(errors, smsImportError{Row: line, Field: "tier", Message: "invalid tier"})
			continue
		}
		if row.RelationshipStatus != "" && !smsValidEnum(row.RelationshipStatus, smsRelationshipStatuses...) {
			errors = append(errors, smsImportError{Row: line, Field: "relationship_status", Message: "invalid relationship_status"})
			continue
		}
		if row.EngagementStatus != "" && !smsValidEnum(row.EngagementStatus, smsEngagementStatuses...) {
			errors = append(errors, smsImportError{Row: line, Field: "engagement_status", Message: "invalid engagement_status"})
			continue
		}
		rows = append(rows, row)
	}
	return rows, errors, nil
}

func allEmpty(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// smsImportDedupeIndex maps existing stakeholders for duplicate detection: by
// lower-cased email and by lower-cased name+organization.
func (h *SMSHandler) smsImportDedupeIndex(ctx context.Context) (map[string]int, map[string]int, error) {
	byEmail := make(map[string]int)
	byNameOrg := make(map[string]int)
	rows, err := h.db.QueryContext(ctx, `SELECT id, lower(name), lower(organization), lower(email) FROM sms_stakeholders`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    int
			name  string
			org   string
			email string
		)
		if err := rows.Scan(&id, &name, &org, &email); err != nil {
			return nil, nil, err
		}
		if email != "" {
			if _, exists := byEmail[email]; !exists {
				byEmail[email] = id
			}
		}
		key := name + "\x00" + org
		if _, exists := byNameOrg[key]; !exists {
			byNameOrg[key] = id
		}
	}
	return byEmail, byNameOrg, rows.Err()
}

func smsFindDuplicate(row smsImportRow, byEmail, byNameOrg map[string]int) *smsImportDuplicate {
	if row.Email != "" {
		if id, ok := byEmail[strings.ToLower(row.Email)]; ok {
			return &smsImportDuplicate{Row: row.Line, Name: row.Name, Email: row.Email, ExistingID: id, MatchOn: "email"}
		}
	}
	key := strings.ToLower(row.Name) + "\x00" + strings.ToLower(row.Organization)
	if id, ok := byNameOrg[key]; ok {
		return &smsImportDuplicate{Row: row.Line, Name: row.Name, Email: row.Email, ExistingID: id, MatchOn: "name_organization"}
	}
	return nil
}

// ImportPreview parses and validates a stakeholder register CSV without
// writing anything (WI-5.1). Body: {"csv": "<raw csv text>"}.
func (h *SMSHandler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		CSV string `json:"csv"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	rows, errors, err := smsParseImportCSV(in.CSV)
	if err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}

	byEmail, byNameOrg, err := h.smsImportDedupeIndex(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var duplicates []smsImportDuplicate
	for _, row := range rows {
		if dup := smsFindDuplicate(row, byEmail, byNameOrg); dup != nil {
			duplicates = append(duplicates, *dup)
		}
	}
	if duplicates == nil {
		duplicates = []smsImportDuplicate{}
	}
	if errors == nil {
		errors = []smsImportError{}
	}
	respondJSONOK(w, map[string]any{
		"total_rows": len(rows) + len(errors),
		"valid_rows": len(rows),
		"errors":     errors,
		"duplicates": duplicates,
	})
}

// ImportExecute imports a stakeholder register CSV (WI-5.1). Body: {"csv":
// "<raw csv text>", "mode": "create"|"upsert"}. create skips rows that match
// an existing stakeholder; upsert updates the matched row with the provided
// fields. Invalid rows are reported and skipped.
func (h *SMSHandler) ImportExecute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		CSV  string `json:"csv"`
		Mode string `json:"mode"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	mode := in.Mode
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "upsert" {
		respondBadRequest(w, r, `mode must be "create" or "upsert"`)
		return
	}

	rows, errors, err := smsParseImportCSV(in.CSV)
	if err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}
	ctx := r.Context()
	byEmail, byNameOrg, err := h.smsImportDedupeIndex(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var created, updated, skipped int64
	for _, row := range rows {
		dup := smsFindDuplicate(row, byEmail, byNameOrg)
		switch {
		case dup == nil:
			id, err := h.smsImportInsertStakeholder(ctx, row)
			if err != nil {
				errors = append(errors, smsImportError{Row: row.Line, Field: "", Message: err.Error()})
				continue
			}
			created++
			if row.Email != "" {
				byEmail[strings.ToLower(row.Email)] = id // dedupe within the batch too
			}
			byNameOrg[strings.ToLower(row.Name)+"\x00"+strings.ToLower(row.Organization)] = id
		case mode == "create":
			skipped++
		default:
			n, err := h.smsImportUpsertStakeholder(ctx, dup.ExistingID, row)
			if err != nil {
				errors = append(errors, smsImportError{Row: row.Line, Field: "", Message: err.Error()})
				continue
			}
			if n > 0 {
				updated++
			} else {
				skipped++
			}
		}
	}

	smsAudit(h, r, user, "sms_stakeholders_imported", "sms_stakeholder", nil, fmt.Sprintf("%d rows", len(rows)), map[string]any{
		"mode":    mode,
		"created": created,
		"updated": updated,
		"skipped": skipped,
	})
	respondJSONOK(w, map[string]any{
		"created": created,
		"updated": updated,
		"skipped": skipped,
		"errors":  errors,
	})
}

// smsImportInsertStakeholder inserts one import row using the same column
// defaults as CreateStakeholder.
func (h *SMSHandler) smsImportInsertStakeholder(ctx context.Context, row smsImportRow) (int, error) {
	res, err := h.db.ExecContext(ctx, `
		INSERT INTO sms_stakeholders (name, title, organization, email, phone, tier, interests, engagement_status, relationship_status, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Name, row.Title, row.Organization, row.Email, row.Phone,
		orKeepStr(row.Tier, models.SMSTier3), row.Interests,
		orKeepStr(row.EngagementStatus, models.SMSEngagementPassive),
		orKeepStr(row.RelationshipStatus, models.SMSRelationshipNeutral), row.Notes)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// smsImportUpsertStakeholder updates the matched stakeholder with the non-empty
// fields of one import row. Returns the number of rows affected (0 when the
// row carried nothing updatable).
func (h *SMSHandler) smsImportUpsertStakeholder(ctx context.Context, id int, row smsImportRow) (int, error) {
	sets := make([]string, 0, 10)
	args := make([]any, 0, 11)
	add := func(col, value string) {
		if value == "" {
			return
		}
		sets = append(sets, col+"=?")
		args = append(args, value)
	}
	add("name", row.Name)
	add("title", row.Title)
	add("organization", row.Organization)
	add("email", row.Email)
	add("phone", row.Phone)
	add("tier", row.Tier)
	add("relationship_status", row.RelationshipStatus)
	add("engagement_status", row.EngagementStatus)
	add("interests", row.Interests)
	add("notes", row.Notes)
	if len(sets) == 0 {
		return 0, nil
	}
	args = append(args, id)
	query := "UPDATE sms_stakeholders SET " + strings.Join(sets, ", ") + " WHERE id=?"
	res, err := h.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---------------------------------------------------------------------------
// Migration-ready full export (WI-5.2)
// ---------------------------------------------------------------------------

// smsExportSchemaVersion is bumped whenever the full-export shape changes so
// importers can reject unknown files (WI-5.2 exit guardrail).
const smsExportSchemaVersion = 2

// smsExportModels loads one table as a flat slice of model rows for the full
// export; JSON tags mirror column names, which is what ImportFull relies on.
func smsExportModels[T any](ctx context.Context, db database.Database, query string, scan func(interface{ Scan(dest ...any) error }) (T, error)) ([]T, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			continue // skip unscannable row rather than fail the whole export
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// smsExportRaw loads a join table without a model as flat column maps.
func smsExportRaw(ctx context.Context, db database.Database, query string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ExportFullJSON returns the entire stakeholder register as flat per-entity
// arrays preserving IDs and FK relationships, for migration to another system
// (WI-5.2). Communication bodies are masked unless the caller may view
// sensitive data. The file round-trips through POST /sms/import/full.
func (h *SMSHandler) ExportFullJSON(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	canSensitive := h.smsCanViewSensitive(ctx, user.ID)

	fail := func(err error) { respondInternalError(w, r, err) }

	stakeholders, err := smsExportModels[*models.SMSStakeholder](ctx, h.db, `SELECT `+smsStakeholderColumns+` FROM sms_stakeholders ORDER BY id`, scanSMSStakeholder)
	if err != nil { fail(err); return }

	comms, err := smsExportModels[*models.SMSCommunication](ctx, h.db, `SELECT `+smsCommunicationColumns+` FROM sms_communications ORDER BY stakeholder_id, created_at DESC, id`, scanSMSCommunication)
	if err != nil { fail(err); return }
	communications := make([]*models.SMSCommunication, 0, len(comms))
	for _, c := range comms {
		if !canSensitive && c.Body != "" {
			masked := *c
			masked.Body = ""
			c = &masked
		}
		communications = append(communications, c)
	}

	plans, err := smsExportModels[*models.SMSEngagementPlan](ctx, h.db, `SELECT `+smsPlanColumns+` FROM sms_engagement_plans ORDER BY id`, scanSMSPlan)
	if err != nil { fail(err); return }
	planStakeholders, err := smsExportRaw(ctx, h.db, `SELECT id, plan_id, stakeholder_id FROM sms_engagement_plan_stakeholders ORDER BY id`)
	if err != nil { fail(err); return }

	issues, err := smsExportModels[*models.SMSIssue](ctx, h.db, `SELECT `+smsIssueColumns+` FROM sms_issues ORDER BY id`, scanSMSIssue)
	if err != nil { fail(err); return }
	tasks, err := smsExportModels[*models.SMSTask](ctx, h.db, `SELECT `+smsTaskColumns+` FROM sms_tasks ORDER BY id`, scanSMSTask)
	if err != nil { fail(err); return }
	feedback, err := smsExportModels[*models.SMSFeedback](ctx, h.db, `SELECT `+smsFeedbackColumns+` FROM sms_feedback ORDER BY id`, scanSMSFeedback)
	if err != nil { fail(err); return }
	goals, err := smsExportModels[*models.SMSRelationshipGoal](ctx, h.db, `SELECT `+smsGoalColumns+` FROM sms_relationship_goals ORDER BY id`, scanSMSGoal)
	if err != nil { fail(err); return }

	commitments, err := smsExportModels[*models.SMSCommitment](ctx, h.db, `SELECT `+smsCommitmentColumns+` FROM sms_commitments ORDER BY id`, scanSMSCommitment)
	if err != nil { fail(err); return }
	grievances, err := smsExportModels[*models.SMSGrievance](ctx, h.db, `SELECT `+smsGrievanceColumns+` FROM sms_grievances ORDER BY id`, scanSMSGrievance)
	if err != nil { fail(err); return }
	decisions, err := smsExportModels[*models.SMSDecision](ctx, h.db, `SELECT `+smsDecisionColumns+` FROM sms_decisions ORDER BY id`, scanSMSDecision)
	if err != nil { fail(err); return }

	groups, err := smsExportRaw(ctx, h.db, `SELECT id, name, description, created_at FROM sms_stakeholder_groups ORDER BY id`)
	if err != nil { fail(err); return }
	groupMembers, err := smsExportRaw(ctx, h.db, `SELECT id, group_id, stakeholder_id FROM sms_stakeholder_group_members ORDER BY id`)
	if err != nil { fail(err); return }

	links, err := smsExportModels[*models.SMSStakeholderLink](ctx, h.db, `SELECT `+smsLinkColumns+` FROM sms_stakeholder_links ORDER BY id`, scanSMSLink)
	if err != nil { fail(err); return }
	transfers, err := smsExportModels[*models.SMSOwnershipTransfer](ctx, h.db, `SELECT `+smsTransferColumns+` FROM sms_ownership_transfers ORDER BY id`, scanSMSTransfer)
	if err != nil { fail(err); return }
	entityLinks, err := smsExportRaw(ctx, h.db, `SELECT id, stakeholder_id, entity_type, entity_id, created_at FROM sms_stakeholder_entities ORDER BY id`)
	if err != nil { fail(err); return }

	smsAudit(h, r, user, "sms_export", "sms_stakeholder", nil, "full export (json)", map[string]any{
		"scope": "full", "rows": len(stakeholders),
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="sms-full-export.json"`)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{
		"module":             "sms",
		"schema_version":     smsExportSchemaVersion,
		"generated_at":       time.Now().UTC().Format(time.RFC3339),
		"stakeholders":       stakeholders,
		"communications":     communications,
		"engagement_plans":   plans,
		"plan_stakeholders":  planStakeholders,
		"issues":             issues,
		"tasks":              tasks,
		"feedback":           feedback,
		"relationship_goals": goals,
		"commitments":        commitments,
		"grievances":         grievances,
		"decisions":          decisions,
		"groups":             groups,
		"group_members":      groupMembers,
		"links":              links,
		"transfers":          transfers,
		"entity_links":       entityLinks,
	})
}

// ---------------------------------------------------------------------------
// Export manifest (WI-5.2)
// ---------------------------------------------------------------------------

// ExportManifest reports every available SMS export scope with its current row
// count, so a migration plan can be built from one call (WI-5.2).
func (h *SMSHandler) ExportManifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	counts := make(map[string]int64)
	for scope, table := range map[string]string{
		"stakeholders":     "sms_stakeholders",
		"communications":   "sms_communications",
		"commitments":      "sms_commitments",
		"grievances":       "sms_grievances",
		"decisions":        "sms_decisions",
		"issues":           "sms_issues",
		"engagement_plans": "sms_engagement_plans",
	} {
		var n int64
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			respondInternalError(w, r, err)
			return
		}
		counts[scope] = n
	}

	var geoCount int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE location_lat IS NOT NULL AND location_lon IS NOT NULL`).Scan(&geoCount); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"module":       "sms",
		"exports": []map[string]any{
			{"scope": "stakeholders", "format": "csv", "endpoint": "/api/sms/export?scope=stakeholders&format=csv", "row_count": counts["stakeholders"]},
			{"scope": "communications", "format": "csv", "endpoint": "/api/sms/export?scope=communications&format=csv", "row_count": counts["communications"]},
			{"scope": "full", "format": "json", "endpoint": "/api/sms/export/full", "row_count": counts["stakeholders"], "note": "all sub-resources nested per stakeholder"},
			{"scope": "geojson", "format": "geojson", "endpoint": "/api/sms/export/geojson", "row_count": geoCount, "note": "stakeholders with location coordinates only"},
		},
	})
}

// ---------------------------------------------------------------------------
// GeoJSON export (WI-6.4)
// ---------------------------------------------------------------------------

// ExportGeoJSON returns stakeholders that carry location coordinates as a
// GeoJSON FeatureCollection (WI-6.4).
func (h *SMSHandler) ExportGeoJSON(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT id, name, organization, tier, relationship_status, engagement_status, location_lat, location_lon FROM sms_stakeholders WHERE location_lat IS NOT NULL AND location_lon IS NOT NULL ORDER BY name COLLATE NOCASE`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	features := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id           int
			name         string
			org          string
			tier         string
			relationship string
			engagement   string
			lat, lon     float64
		)
		if err := rows.Scan(&id, &name, &org, &tier, &relationship, &engagement, &lat, &lon); err != nil {
			respondInternalError(w, r, err)
			return
		}
		features = append(features, map[string]any{
			"type": "Feature",
			"geometry": map[string]any{
				"type":        "Point",
				"coordinates": []float64{lon, lat}, // GeoJSON order is [lon, lat]
			},
			"properties": map[string]any{
				"id":                  id,
				"name":                name,
				"organization":        org,
				"tier":                tier,
				"relationship_status": relationship,
				"engagement_status":   engagement,
			},
		})
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Content-Disposition", `attachment; filename="sms-stakeholders.geojson"`)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{
		"type":     "FeatureCollection",
		"features": features,
	})
}
