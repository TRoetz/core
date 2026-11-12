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
	"strconv"
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
// Site & occupancy CSV import (roadworks structure capture). One row per site,
// optionally carrying one occupant (stakeholder matched by email). Sites are
// deduped by external_id or name+location_text; occupants upsert into
// sms_node_members with kind-validated roles.
// ---------------------------------------------------------------------------

type smsSiteImportRow struct {
	Line             int      `json:"line"`
	Name             string   `json:"name"`
	ExternalID       string   `json:"external_id,omitempty"`
	KindKey          string   `json:"kind,omitempty"` // defaults to "building"
	ParentExternalID string   `json:"parent_external_id,omitempty"`
	Description      string   `json:"description,omitempty"`
	LocationLat      *float64 `json:"location_lat,omitempty"`
	LocationLon      *float64 `json:"location_lon,omitempty"`
	LocationText     string   `json:"location_text,omitempty"` // street address
	CustomFields     string   `json:"custom_fields,omitempty"` // JSON object (access constraints…)
	EntityType       string   `json:"entity_type,omitempty"`
	EntityID         int      `json:"entity_id,omitempty"`
	// Occupant (optional per row).
	StakeholderEmail string  `json:"stakeholder_email,omitempty"`
	OccupancyRole    string  `json:"occupancy_role,omitempty"`
	TenancyStart     *string `json:"tenancy_start,omitempty"` // YYYY-MM-DD
	TenancyEnd       *string `json:"tenancy_end,omitempty"`   // YYYY-MM-DD
	MemberNotes      string  `json:"member_notes,omitempty"`
}

type smsSiteImportUnmatched struct {
	Row      int    `json:"row"`
	NodeName string `json:"node_name"`
	Email    string `json:"email"`
}

// smsParseSitesCSV parses a site & occupancy CSV with a required header row.
// Unknown columns are ignored; missing optional columns stay empty.
func smsParseSitesCSV(csvText string) ([]smsSiteImportRow, []smsImportError, error) {
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

	rows := make([]smsSiteImportRow, 0, len(records)-1)
	var errors []smsImportError
	for _, record := range records[1:] {
		line := len(rows) + len(errors) + 2 // header is line 1; count consumed data lines
		row := smsSiteImportRow{Line: line}
		if allEmpty(record) {
			continue // skip blank lines
		}
		row.Name = get(record, "name")
		row.ExternalID = get(record, "external_id")
		row.KindKey = get(record, "kind")
		row.ParentExternalID = get(record, "parent_external_id")
		row.Description = get(record, "description")
		if v := get(record, "location_lat"); v != "" {
			f, perr := strconv.ParseFloat(v, 64)
			if perr != nil || f < -90 || f > 90 {
				errors = append(errors, smsImportError{Row: line, Field: "location_lat", Message: "invalid latitude"})
				continue
			}
			row.LocationLat = &f
		}
		if v := get(record, "location_lon"); v != "" {
			f, perr := strconv.ParseFloat(v, 64)
			if perr != nil || f < -180 || f > 180 {
				errors = append(errors, smsImportError{Row: line, Field: "location_lon", Message: "invalid longitude"})
				continue
			}
			row.LocationLon = &f
		}
		row.LocationText = get(record, "location_text")
		row.CustomFields = get(record, "custom_fields")
		if row.CustomFields != "" && !json.Valid([]byte(row.CustomFields)) {
			errors = append(errors, smsImportError{Row: line, Field: "custom_fields", Message: "must be valid JSON"})
			continue
		}
		row.EntityType = get(record, "entity_type")
		if row.EntityType != "" && !smsValidEnum(row.EntityType, "portfolio", "programme", "project") {
			errors = append(errors, smsImportError{Row: line, Field: "entity_type", Message: "invalid entity_type"})
			continue
		}
		if v := get(record, "entity_id"); v != "" {
			n, aerr := strconv.Atoi(v)
			if aerr != nil || n <= 0 {
				errors = append(errors, smsImportError{Row: line, Field: "entity_id", Message: "invalid entity_id"})
				continue
			}
			row.EntityID = n
		}
		row.StakeholderEmail = get(record, "stakeholder_email")
		row.OccupancyRole = get(record, "occupancy_role")
		if v := get(record, "tenancy_start"); v != "" {
			if !validSMSDate(v) {
				errors = append(errors, smsImportError{Row: line, Field: "tenancy_start", Message: "must be YYYY-MM-DD"})
				continue
			}
			row.TenancyStart = &v
		}
		if v := get(record, "tenancy_end"); v != "" {
			if !validSMSDate(v) {
				errors = append(errors, smsImportError{Row: line, Field: "tenancy_end", Message: "must be YYYY-MM-DD"})
				continue
			}
			row.TenancyEnd = &v
		}
		row.MemberNotes = get(record, "member_notes")

		if row.Name == "" {
			errors = append(errors, smsImportError{Row: line, Field: "name", Message: "name is required"})
			continue
		}
		rows = append(rows, row)
	}
	return rows, errors, nil
}

// smsSiteImportIndex maps existing nodes for duplicate detection: by
// external_id and by lower-cased name+location_text.
func (h *SMSHandler) smsSiteImportIndex(ctx context.Context) (map[string]int, map[string]int, error) {
	byExternal := make(map[string]int)
	byNameLoc := make(map[string]int)
	rows, err := h.db.QueryContext(ctx, `SELECT id, COALESCE(external_id, ''), lower(name), lower(location_text) FROM sms_nodes`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id           int
			external     string
			name         string
			locationText string
		)
		if err := rows.Scan(&id, &external, &name, &locationText); err != nil {
			return nil, nil, err
		}
		if external != "" {
			if _, exists := byExternal[external]; !exists {
				byExternal[external] = id
			}
		}
		key := name + "\x00" + locationText
		if _, exists := byNameLoc[key]; !exists {
			byNameLoc[key] = id
		}
	}
	return byExternal, byNameLoc, rows.Err()
}

func smsFindSiteDuplicate(row smsSiteImportRow, byExternal, byNameLoc map[string]int) *smsImportDuplicate {
	if row.ExternalID != "" {
		if id, ok := byExternal[row.ExternalID]; ok {
			return &smsImportDuplicate{Row: row.Line, Name: row.Name, ExistingID: id, MatchOn: "external_id"}
		}
	}
	key := strings.ToLower(row.Name) + "\x00" + strings.ToLower(row.LocationText)
	if id, ok := byNameLoc[key]; ok {
		return &smsImportDuplicate{Row: row.Line, Name: row.Name, ExistingID: id, MatchOn: "name_location"}
	}
	return nil
}

// ImportSitesPreview parses and validates a site & occupancy CSV without
// writing anything. Body: {"csv": "<raw csv text>"}. Reports per-row errors,
// duplicates against existing nodes, and occupant emails with no matching
// stakeholder in the register.
func (h *SMSHandler) ImportSitesPreview(w http.ResponseWriter, r *http.Request) {
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
	rows, errors, err := smsParseSitesCSV(in.CSV)
	if err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}
	ctx := r.Context()
	byExternal, byNameLoc, err := h.smsSiteImportIndex(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var duplicates []smsImportDuplicate
	for _, row := range rows {
		if dup := smsFindSiteDuplicate(row, byExternal, byNameLoc); dup != nil {
			duplicates = append(duplicates, *dup)
		}
	}
	shByEmail, _, err := h.smsImportDedupeIndex(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var unmatched []smsSiteImportUnmatched
	for _, row := range rows {
		if row.StakeholderEmail == "" {
			continue
		}
		if _, ok := shByEmail[strings.ToLower(row.StakeholderEmail)]; !ok {
			unmatched = append(unmatched, smsSiteImportUnmatched{Row: row.Line, NodeName: row.Name, Email: row.StakeholderEmail})
		}
	}
	if duplicates == nil {
		duplicates = []smsImportDuplicate{}
	}
	if unmatched == nil {
		unmatched = []smsSiteImportUnmatched{}
	}
	if errors == nil {
		errors = []smsImportError{}
	}
	respondJSONOK(w, map[string]any{
		"total_rows":          len(rows) + len(errors),
		"valid_rows":          len(rows),
		"errors":              errors,
		"duplicates":          duplicates,
		"unmatched_occupants": unmatched,
	})
}

// ImportSitesExecute imports a site & occupancy CSV. Body: {"csv": "<raw csv
// text>", "mode": "create"|"upsert"}. create skips sites that already exist;
// upsert updates the matched site with the provided fields. Occupant rows
// upsert sms_node_members (kind-validated role); occupants whose email matches
// no stakeholder are skipped and reported. Invalid rows are reported and
// skipped.
func (h *SMSHandler) ImportSitesExecute(w http.ResponseWriter, r *http.Request) {
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
	rows, errors, err := smsParseSitesCSV(in.CSV)
	if err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}
	ctx := r.Context()

	// Kind + role vocabulary (admin-configurable), loaded once.
	kindIDs := make(map[string]int)    // key -> id
	kindRoles := make(map[int][]string) // kind_id -> allowed roles
	krows, err := h.db.QueryContext(ctx, `SELECT id, key FROM sms_node_kinds`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for krows.Next() {
		var (
			id  int
			key string
		)
		if err := krows.Scan(&id, &key); err != nil {
			krows.Close()
			respondInternalError(w, r, err)
			return
		}
		kindIDs[key] = id
	}
	krows.Close()
	if err := krows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	rrows, err := h.db.QueryContext(ctx, `SELECT kind_id, role_key FROM sms_node_kind_roles ORDER BY sort_order`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rrows.Next() {
		var (
			kindID int
			role   string
		)
		if err := rrows.Scan(&kindID, &role); err != nil {
			rrows.Close()
			respondInternalError(w, r, err)
			return
		}
		kindRoles[kindID] = append(kindRoles[kindID], role)
	}
	rrows.Close()
	if err := rrows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	byExternal, byNameLoc, err := h.smsSiteImportIndex(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	shByEmail, _, err := h.smsImportDedupeIndex(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var sitesCreated, sitesUpdated, membersAdded, membersUpdated int64
	skipped := len(errors)
	for _, row := range rows {
		kindKey := row.KindKey
		if kindKey == "" {
			kindKey = "building"
		}
		kindID, okKind := kindIDs[kindKey]
		if !okKind {
			errors = append(errors, smsImportError{Row: row.Line, Field: "kind", Message: fmt.Sprintf("unknown kind %q", kindKey)})
			skipped++
			continue
		}

		dup := smsFindSiteDuplicate(row, byExternal, byNameLoc)
		var nodeID int
		switch {
		case dup == nil:
			nodeID, err = h.smsImportInsertNode(ctx, row, kindID)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			sitesCreated++
			if row.ExternalID != "" {
				byExternal[row.ExternalID] = nodeID
			}
			byNameLoc[strings.ToLower(row.Name)+"\x00"+strings.ToLower(row.LocationText)] = nodeID
		case mode == "upsert":
			nodeID = dup.ExistingID
			if n, uerr := h.smsImportUpsertNode(ctx, nodeID, row); uerr != nil {
				respondInternalError(w, r, uerr)
				return
			} else if n > 0 {
				sitesUpdated++
			}
		default: // existing site in create mode
			skipped++
			continue
		}

		if row.StakeholderEmail == "" {
			continue
		}
		shID, okSH := shByEmail[strings.ToLower(row.StakeholderEmail)]
		if !okSH {
			errors = append(errors, smsImportError{Row: row.Line, Field: "stakeholder_email", Message: "no stakeholder with this email"})
			skipped++
			continue
		}
		role := strings.TrimSpace(row.OccupancyRole)
		if allowed := kindRoles[kindID]; len(allowed) > 0 {
			okRole := false
			for _, a := range allowed {
				if a == role {
					okRole = true
					break
				}
			}
			if !okRole {
				errors = append(errors, smsImportError{Row: row.Line, Field: "occupancy_role", Message: fmt.Sprintf("role must be one of: %s", strings.Join(allowed, ", "))})
				skipped++
				continue
			}
		} else {
			role = ""
		}

		var existing int
		err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_members WHERE node_id = ? AND stakeholder_id = ? AND role = ?`, nodeID, shID, role).Scan(&existing)
		switch {
		case err == nil && existing > 0:
			if mode == "upsert" {
				if _, uerr := h.db.ExecContext(ctx, `UPDATE sms_node_members SET start_date = ?, end_date = ?, notes = ? WHERE node_id = ? AND stakeholder_id = ? AND role = ?`, row.TenancyStart, row.TenancyEnd, row.MemberNotes, nodeID, shID, role); uerr != nil {
					respondInternalError(w, r, uerr)
					return
				}
				membersUpdated++
			} else {
				skipped++
			}
		case isNotFound(err):
			if _, ierr := h.db.ExecContext(ctx, `INSERT INTO sms_node_members (node_id, stakeholder_id, role, start_date, end_date, notes) VALUES (?, ?, ?, ?, ?, ?)`, nodeID, shID, role, row.TenancyStart, row.TenancyEnd, row.MemberNotes); ierr != nil {
				respondInternalError(w, r, ierr)
				return
			}
			membersAdded++
		default:
			respondInternalError(w, r, err)
			return
		}
	}

	smsAudit(h, r, user, "sms_sites_imported", "sms_node", nil, fmt.Sprintf("%d sites / %d members", sitesCreated+sitesUpdated, membersAdded+membersUpdated), map[string]any{"mode": mode, "skipped_rows": skipped})
	respondJSONOK(w, map[string]any{
		"sites_created":   sitesCreated,
		"sites_updated":   sitesUpdated,
		"members_added":   membersAdded,
		"members_updated": membersUpdated,
		"skipped_rows":    skipped,
		"errors":          errors,
	})
}

// smsImportInsertNode inserts one site import row using the same column
// defaults as CreateNode. parent_external_id must already resolve (existing or
// created earlier in this file).
func (h *SMSHandler) smsImportInsertNode(ctx context.Context, row smsSiteImportRow, kindID int) (int, error) {
	var parentID *int
	if row.ParentExternalID != "" {
		var pid int
		err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_nodes WHERE external_id = ?`, row.ParentExternalID).Scan(&pid)
		if err != nil {
			return 0, fmt.Errorf("parent_external_id %q not found", row.ParentExternalID)
		}
		parentID = &pid
	}
	cf := row.CustomFields
	if cf == "" {
		cf = "{}"
	}
	var externalID, entityType *string
	if row.ExternalID != "" {
		externalID = &row.ExternalID
	}
	if row.EntityType != "" {
		entityType = &row.EntityType
	}
	var entityID *int
	if row.EntityID > 0 {
		entityID = &row.EntityID
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_nodes (external_id, kind_id, parent_id, name, description, location_lat, location_lon, location_text, custom_fields, entity_type, entity_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		externalID, kindID, parentID, row.Name, row.Description, row.LocationLat, row.LocationLon, row.LocationText, cf, entityType, entityID)
	if err != nil {
		return 0, err
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

// smsImportUpsertNode updates the matched node with the non-empty fields of
// one import row. Returns rows affected (0 when nothing was provided).
func (h *SMSHandler) smsImportUpsertNode(ctx context.Context, id int, row smsSiteImportRow) (int64, error) {
	sets := make([]string, 0, 8)
	args := make([]any, 0, 9)
	if row.Description != "" {
		sets = append(sets, "description = ?")
		args = append(args, row.Description)
	}
	if row.LocationLat != nil {
		sets = append(sets, "location_lat = ?")
		args = append(args, *row.LocationLat)
	}
	if row.LocationLon != nil {
		sets = append(sets, "location_lon = ?")
		args = append(args, *row.LocationLon)
	}
	if row.LocationText != "" {
		sets = append(sets, "location_text = ?")
		args = append(args, row.LocationText)
	}
	if row.CustomFields != "" {
		sets = append(sets, "custom_fields = ?")
		args = append(args, row.CustomFields)
	}
	if row.EntityType != "" {
		sets = append(sets, "entity_type = ?, entity_id = ?")
		var entityID *int
		if row.EntityID > 0 {
			entityID = &row.EntityID
		}
		args = append(args, row.EntityType, entityID)
	}
	if len(sets) == 0 {
		return 0, nil
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)
	res, err := h.db.ExecContext(ctx, `UPDATE sms_nodes SET `+strings.Join(sets, ", ") + ` WHERE id = ?`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
		"stakeholders":          "sms_stakeholders",
		"communications":        "sms_communications",
		"commitments":           "sms_commitments",
		"grievances":            "sms_grievances",
		"decisions":             "sms_decisions",
		"issues":                "sms_issues",
		"engagement_plans":      "sms_engagement_plans",
		"nodes":                 "sms_nodes",
		"node_members":          "sms_node_members",
		"notification_campaigns": "sms_notification_campaigns",
		"notifications":         "sms_notifications",
		"racis":                 "sms_racis",
		"raci_assignments":      "sms_raci_assignments",
		"work_packages":         "sms_work_packages",
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
	var nodeGeoCount int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_nodes WHERE location_lat IS NOT NULL AND location_lon IS NOT NULL`).Scan(&nodeGeoCount); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"module":       "sms",
		"exports": []map[string]any{
			{"scope": "stakeholders", "format": "csv", "endpoint": "/api/sms/export?scope=stakeholders&format=csv", "row_count": counts["stakeholders"]},
			{"scope": "communications", "format": "csv", "endpoint": "/api/sms/export?scope=communications&format=csv", "row_count": counts["communications"]},
			{"scope": "nodes", "format": "csv", "endpoint": "/api/sms/export?scope=nodes&format=csv", "row_count": counts["nodes"]},
			{"scope": "occupants", "format": "csv", "endpoint": "/api/sms/export?scope=occupants&format=csv", "row_count": counts["node_members"]},
			{"scope": "full", "format": "json", "endpoint": "/api/sms/export/full", "row_count": counts["stakeholders"], "note": "all sub-resources nested per stakeholder"},
			{"scope": "geojson", "format": "geojson", "endpoint": "/api/sms/export/geojson", "row_count": geoCount, "note": "stakeholders with location coordinates only"},
			{"scope": "geojson-nodes", "format": "geojson", "endpoint": "/api/sms/export/geojson?layer=nodes", "row_count": nodeGeoCount, "note": "sites (buildings, road segments) with coordinates only"},
		},
	})
}

// ---------------------------------------------------------------------------
// GeoJSON export (WI-6.4)
// ---------------------------------------------------------------------------

// ExportGeoJSON returns location-bearing records as a GeoJSON FeatureCollection
// (WI-6.4). ?layer=stakeholders (default) exports stakeholders with
// coordinates; ?layer=nodes exports sites (buildings, road segments) with
// coordinates for the affected-area map.
func (h *SMSHandler) ExportGeoJSON(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	layer := r.URL.Query().Get("layer")
	switch layer {
	case "", "stakeholders":
		layer = "stakeholders"
	case "nodes":
	default:
		respondBadRequest(w, r, `layer must be "stakeholders" or "nodes"`)
		return
	}

	features := make([]map[string]any, 0)
	if layer == "stakeholders" {
		rows, err := h.db.QueryContext(ctx, `SELECT id, name, organization, tier, relationship_status, engagement_status, location_lat, location_lon FROM sms_stakeholders WHERE location_lat IS NOT NULL AND location_lon IS NOT NULL ORDER BY name COLLATE NOCASE`)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer rows.Close()

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
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT n.id, k.key AS kind_key, n.name, COALESCE(n.location_text, ''), COALESCE(n.description, ''), (SELECT COUNT(*) FROM sms_node_members m WHERE m.node_id = n.id) AS occupant_count, n.location_lat, n.location_lon FROM sms_nodes n JOIN sms_node_kinds k ON k.id = n.kind_id WHERE n.location_lat IS NOT NULL AND n.location_lon IS NOT NULL ORDER BY n.name COLLATE NOCASE`)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id         int
				kindKey    string
				name       string
				location   string
				description string
				occupants  int
				lat, lon   float64
			)
			if err := rows.Scan(&id, &kindKey, &name, &location, &description, &occupants, &lat, &lon); err != nil {
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
					"id":             id,
					"kind":           kindKey,
					"name":           name,
					"location_text":  location,
					"description":    description,
					"occupant_count": occupants,
				},
			})
		}
		if err := rows.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	filename := "sms-stakeholders.geojson"
	if layer == "nodes" {
		filename = "sms-nodes.geojson"
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{
		"type":     "FeatureCollection",
		"features": features,
	})
}
