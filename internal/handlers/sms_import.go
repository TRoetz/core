package handlers

// Workbook import for the Stakeholder Management System module. The request
// carries one or more sheets of the data-collection workbook as raw CSV text;
// preview validates everything without writing, execute writes in dependency
// order inside a single transaction (stakeholders first - every other sheet
// references them by email). Sheet names and headers match
// templates/windshift-data-collection-template.xlsx exactly.
//
// Matching semantics:
//   - SMS Stakeholders upserts by explicit id (export dialect), then
//     external_id, then lower-cased email; updates only set non-empty fields.
//   - Sites & Occupancy upserts nodes by external_id; rows without one are
//     always created. Occupant columns add sms_node_members rows (idempotent).
//   - Groups upsert by name; memberships insert idempotently.
//   - Issues, commitments, grievances, tasks and relationship goals upsert on
//     the natural key (stakeholder, lower(title|goal)).
//   - Engagement plans, decisions, feedback and communications are append-only:
//     every valid row creates a record (a sheet-level warning says so).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// smsImportSheetNames lists every sheet the importer accepts, in processing
// (dependency) order. Unknown sheet names are rejected up front.
var smsImportSheetNames = []string{
	"SMS Stakeholders",
	"SMS Sites & Occupancy",
	"SMS Groups",
	"SMS Engagement Plans",
	"SMS Issues & Risks",
	"SMS Commitments",
	"SMS Grievances",
	"SMS Decisions Log",
	"SMS Tasks",
	"SMS Feedback",
	"SMS Relationship Goals",
	"SMS Communications Log",
	"SMS Network Links",
}

func smsSheetKnown(name string) bool {
	for _, n := range smsImportSheetNames {
		if n == name {
			return true
		}
	}
	return false
}

// smsImportContext carries the shared indexes every sheet needs: person and
// stakeholder lookups, node taxonomy, groups, and P3M scope titles. The maps
// grow as rows are processed so later sheets can reference entities created
// earlier in the same payload.
type smsImportContext struct {
	ctx       context.Context
	exec      importExecer
	mode      string // "create" | "upsert"
	actorID   int    // importing user (created_by columns)
	executing bool

	// Person resolution: email first, then "first last".
	usersByEmail map[string]int
	usersByName  map[string][]int

	// Stakeholder register index. shSeen marks emails present in the DB or
	// created earlier in this payload (preview has no real ids to hand out).
	shByEmail map[string]int // lower(email) -> id
	shByExtID map[string]int // lower(external_id) -> id
	shSeen    map[string]bool

	// Node taxonomy for sites & occupancy.
	nodeKindsByKey map[string]int          // key -> id (admin-configurable kinds)
	kindRoles      map[int]map[string]bool // kind id -> allowed role keys
	nodesByExtID   map[string]int          // lower(external_id) -> id
	nodesSeen      map[string]bool         // external ids in DB or this payload

	groupsByName map[string]int // lower(name) -> id (DB + batch)

	// P3M scope links for engagement plans and decisions.
	portfoliosByTitle map[string]int
	programmesByKey   map[string]int // lower(pf)\x00lower(title) -> id
	projectsByKey     map[string]int // lower(pf)\x00lower(prg)\x00lower(title) -> id
}

// newSMSImportContext loads the shared indexes. exec is h.db for preview and
// the import transaction for execute.
func (h *SMSHandler) newSMSImportContext(ctx context.Context, exec importExecer, mode string, actorID int, executing bool) (*smsImportContext, error) {
	c := &smsImportContext{
		ctx:               ctx,
		exec:              exec,
		mode:              mode,
		actorID:           actorID,
		executing:         executing,
		usersByEmail:      make(map[string]int),
		usersByName:       make(map[string][]int),
		shByEmail:         make(map[string]int),
		shByExtID:         make(map[string]int),
		shSeen:            make(map[string]bool),
		nodeKindsByKey:    make(map[string]int),
		kindRoles:         make(map[int]map[string]bool),
		nodesByExtID:      make(map[string]int),
		nodesSeen:         make(map[string]bool),
		groupsByName:      make(map[string]int),
		portfoliosByTitle: make(map[string]int),
		programmesByKey:   make(map[string]int),
		projectsByKey:     make(map[string]int),
	}

	rows, err := exec.Query(`SELECT id, lower(email), lower(first_name || ' ' || last_name) FROM users`)
	if err != nil {
		return nil, fmt.Errorf("load user index for import: %w", err)
	}
	for rows.Next() {
		var id int
		var email, name string
		if err := rows.Scan(&id, &email, &name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan user index row: %w", err)
		}
		name = strings.TrimSpace(name)
		if email != "" {
			c.usersByEmail[email] = id
		}
		if name != "" && name != "  " {
			c.usersByName[name] = append(c.usersByName[name], id)
		}
	}
	rows.Close()

	shRows, err := exec.Query(`SELECT id, lower(email), external_id FROM sms_stakeholders`)
	if err != nil {
		return nil, fmt.Errorf("load stakeholder index for import: %w", err)
	}
	for shRows.Next() {
		var id int
		var email string
		var extID sql.NullString
		if err := shRows.Scan(&id, &email, &extID); err != nil {
			shRows.Close()
			return nil, fmt.Errorf("scan stakeholder index row: %w", err)
		}
		if email != "" {
			c.shByEmail[email] = id
			c.shSeen[email] = true
		}
		if extID.Valid && strings.TrimSpace(extID.String) != "" {
			c.shByExtID[strings.ToLower(strings.TrimSpace(extID.String))] = id
		}
	}
	shRows.Close()

	kindRows, err := exec.Query(`SELECT id, key FROM sms_node_kinds`)
	if err != nil {
		return nil, fmt.Errorf("load node kind index for import: %w", err)
	}
	for kindRows.Next() {
		var id int
		var key string
		if err := kindRows.Scan(&id, &key); err == nil {
			c.nodeKindsByKey[key] = id
		}
	}
	kindRows.Close()

	roleRows, err := exec.Query(`SELECT kind_id, role_key FROM sms_node_kind_roles`)
	if err != nil {
		return nil, fmt.Errorf("load node kind roles for import: %w", err)
	}
	for roleRows.Next() {
		var kindID int
		var roleKey string
		if err := roleRows.Scan(&kindID, &roleKey); err == nil {
			if c.kindRoles[kindID] == nil {
				c.kindRoles[kindID] = make(map[string]bool)
			}
			c.kindRoles[kindID][roleKey] = true
		}
	}
	roleRows.Close()

	nodeRows, err := exec.Query(`SELECT id, external_id FROM sms_nodes WHERE external_id IS NOT NULL AND external_id != ''`)
	if err != nil {
		return nil, fmt.Errorf("load node index for import: %w", err)
	}
	for nodeRows.Next() {
		var id int
		var extID string
		if err := nodeRows.Scan(&id, &extID); err == nil {
			key := strings.ToLower(strings.TrimSpace(extID))
			c.nodesByExtID[key] = id
			c.nodesSeen[key] = true
		}
	}
	nodeRows.Close()

	groupRows, err := exec.Query(`SELECT lower(name), id FROM sms_stakeholder_groups`)
	if err != nil {
		return nil, fmt.Errorf("load group index for import: %w", err)
	}
	for groupRows.Next() {
		var name string
		var id int
		if err := groupRows.Scan(&name, &id); err == nil {
			c.groupsByName[name] = id
		}
	}
	groupRows.Close()

	pfRows, err := exec.Query(`SELECT lower(title), id FROM p3m_portfolios`)
	if err != nil {
		return nil, fmt.Errorf("load portfolio index for import: %w", err)
	}
	for pfRows.Next() {
		var title string
		var id int
		if err := pfRows.Scan(&title, &id); err == nil {
			c.portfoliosByTitle[title] = id
		}
	}
	pfRows.Close()

	prgRows, err := exec.Query(`SELECT COALESCE(p.title,''), COALESCE(pg.title,''), pg.id FROM p3m_programmes pg JOIN p3m_portfolios p ON p.id = pg.portfolio_id`)
	if err != nil {
		return nil, fmt.Errorf("load programme index for import: %w", err)
	}
	for prgRows.Next() {
		var pfTitle, prgTitle string
		var id int
		if err := prgRows.Scan(&pfTitle, &prgTitle, &id); err == nil {
			c.programmesByKey[strings.ToLower(pfTitle)+"\x00"+strings.ToLower(prgTitle)] = id
		}
	}
	prgRows.Close()

	prjRows, err := exec.Query(`
		SELECT COALESCE(p.title,''), COALESCE(pg.title,''), prj.title, prj.id
		FROM p3m_projects prj
		LEFT JOIN p3m_portfolios p ON p.id = prj.portfolio_id
		LEFT JOIN p3m_programmes pg ON pg.id = prj.programme_id`)
	if err != nil {
		return nil, fmt.Errorf("load project index for import: %w", err)
	}
	for prjRows.Next() {
		var pfTitle, prgTitle, prjTitle string
		var id int
		if err := prjRows.Scan(&pfTitle, &prgTitle, &prjTitle, &id); err == nil {
			c.projectsByKey[strings.ToLower(pfTitle)+"\x00"+strings.ToLower(prgTitle)+"\x00"+strings.ToLower(prjTitle)] = id
		}
	}
	prjRows.Close()

	return c, nil
}

// ---------------------------------------------------------------------------
// Field helpers (shared with the other workbook importers where possible)
// ---------------------------------------------------------------------------

// importNullStr renders an optional string as SQL NULL when empty.
func importNullStr(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

// importBoolField parses an optional true/false field; nil when empty or invalid.
func importBoolField(rep *importSheetReport, row int, rec map[string]string, field string) *bool {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		return nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		rep.err(row, field, fmt.Sprintf("invalid boolean %q (expected true or false)", value))
		return nil
	}
	return &b
}

// importFloatRangeField validates an optional number within [lo, hi]; returns
// nil when empty and records an error when invalid.
func importFloatRangeField(rep *importSheetReport, row int, rec map[string]string, field string, lo, hi float64) *float64 {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		return nil
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f < lo || f > hi {
		rep.err(row, field, fmt.Sprintf("invalid number %q (expected %g to %g)", value, lo, hi))
		return nil
	}
	return &f
}

// importPersonField resolves an optional person reference to a user id: email
// first, then "first last" display name. Ambiguous names are rejected - use an
// email instead.
func importPersonField(rep *importSheetReport, row int, rec map[string]string, byEmail map[string]int, byName map[string][]int, required bool, headers ...string) *int {
	value := recField(rec, headers...)
	if value == "" {
		if required {
			rep.err(row, headers[0], fmt.Sprintf("%s is required", headers[0]))
		}
		return nil
	}
	key := strings.ToLower(value)
	if id, ok := byEmail[key]; ok {
		return &id
	}
	ids := byName[key]
	switch len(ids) {
	case 1:
		return &ids[0]
	case 0:
		rep.err(row, headers[0], fmt.Sprintf("unknown person %q (no user with that email or name)", value))
	default:
		rep.err(row, headers[0], fmt.Sprintf("ambiguous person %q (%d users match); use an email", value, len(ids)))
	}
	return nil
}

// stakeholderField resolves a required stakeholder email to a register id. In
// preview mode emails that will exist once the SMS Stakeholders sheet has run
// resolve as (nil, true) - there is no real id yet; execute mode always finds
// real ids because sheets process in dependency order.
func (c *smsImportContext) stakeholderField(rep *importSheetReport, row int, rec map[string]string, field string) (*int, bool) {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		rep.err(row, field, fmt.Sprintf("%s is required", field))
		return nil, false
	}
	key := strings.ToLower(value)
	if id, ok := c.shByEmail[key]; ok {
		return &id, true
	}
	if !c.executing && c.shSeen[key] {
		return nil, true // resolvable at execute time
	}
	rep.err(row, field, fmt.Sprintf("unknown stakeholder email %q (must match an SMS Stakeholders row or existing register entry)", value))
	return nil, false
}

// smsResolveScope resolves the optional P3M scope columns (entity_type plus
// portfolio/programme/project titles) to a (type, id) pair. All-empty returns
// NULLs; anything else must resolve exactly.
func (c *smsImportContext) smsResolveScope(rep *importSheetReport, row int, rec map[string]string) (sql.NullString, sql.NullInt64) {
	et := strings.ToLower(strings.TrimSpace(rec["entity_type"]))
	pf := strings.ToLower(strings.TrimSpace(rec["portfolio_title"]))
	prg := strings.ToLower(strings.TrimSpace(rec["programme_title"]))
	prj := strings.ToLower(strings.TrimSpace(rec["project_title"]))
	if et == "" && pf == "" && prg == "" && prj == "" {
		return sql.NullString{}, sql.NullInt64{}
	}
	var id int
	var ok bool
	switch et {
	case "portfolio":
		id, ok = c.portfoliosByTitle[pf]
		if !ok {
			rep.err(row, "portfolio_title", fmt.Sprintf("unknown portfolio %q", rec["portfolio_title"]))
			return sql.NullString{}, sql.NullInt64{}
		}
	case "programme":
		id, ok = c.programmesByKey[pf+"\x00"+prg]
		if !ok {
			rep.err(row, "programme_title", fmt.Sprintf("unknown programme %q in portfolio %q", rec["programme_title"], rec["portfolio_title"]))
			return sql.NullString{}, sql.NullInt64{}
		}
	case "project":
		id, ok = c.projectsByKey[pf+"\x00"+prg+"\x00"+prj]
		if !ok {
			rep.err(row, "project_title", fmt.Sprintf("unknown project %q", rec["project_title"]))
			return sql.NullString{}, sql.NullInt64{}
		}
	default:
		rep.err(row, "entity_type", "entity_type must be portfolio, programme or project")
		return sql.NullString{}, sql.NullInt64{}
	}
	return sql.NullString{String: et, Valid: true}, sql.NullInt64{Int64: int64(id), Valid: true}
}

// smsNaturalKeyID looks up an existing row by (stakeholder_id, lower(title)) -
// the natural key for issues, commitments, grievances, tasks and goals. A nil
// stakeholder id (preview rows referencing batch-created stakeholders) can
// never match, so it reports no existing row.
func (c *smsImportContext) smsNaturalKeyID(rep *importSheetReport, row int, table string, shID *int, titleField, titleValue string) (int, bool) {
	if shID == nil {
		return 0, false
	}
	var existingID int
	err := c.exec.QueryRow(
		fmt.Sprintf(`SELECT id FROM %s WHERE stakeholder_id = ? AND lower(title) = ? LIMIT 1`, table),
		*shID, strings.ToLower(titleValue)).Scan(&existingID)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		rep.err(row, titleField, fmt.Sprintf("lookup failed: %v", err))
		return 0, false
	}
	return existingID, true
}

// smsGoalKeyID is the goal-titled variant of smsNaturalKeyID.
func (c *smsImportContext) smsGoalKeyID(rep *importSheetReport, row int, shID *int, goal string) (int, bool) {
	if shID == nil {
		return 0, false
	}
	var existingID int
	err := c.exec.QueryRow(`SELECT id FROM sms_relationship_goals WHERE stakeholder_id = ? AND lower(goal) = ? LIMIT 1`, *shID, strings.ToLower(goal)).Scan(&existingID)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		rep.err(row, "goal", fmt.Sprintf("lookup failed: %v", err))
		return 0, false
	}
	return existingID, true
}

// smsAppendOnlyNote flags sheets whose rows always create records.
func (rep *importSheetReport) appendOnlyNote(msg string) {
	if rep.Rows > 0 {
		rep.warn(0, "", msg)
	}
}

// ---------------------------------------------------------------------------
// Sheet processors. Each returns a report; when ctx.executing is set the rows
// are written through ctx.exec (the import transaction). A row that produced
// any field error is not written.
// ---------------------------------------------------------------------------

func (h *SMSHandler) smsImportStakeholders(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		name := strings.TrimSpace(rec["name"])
		if name == "" {
			rep.err(row, "name", "name is required")
			continue
		}
		emailKey := strings.ToLower(strings.TrimSpace(rec["email"]))
		extID := strings.TrimSpace(rec["external_id"])

		tier := importEnum(&rep, row, rec, "tier", []string{"tier_1", "tier_2", "tier_3", "tier_4"}, false)
		if tier == "" {
			tier = "tier_3"
		}
		relStatus := importEnum(&rep, row, rec, "relationship_status", []string{"advocate", "supportive", "neutral", "concerned", "opposed"}, false)
		if relStatus == "" {
			relStatus = "neutral"
		}
		engStatus := importEnum(&rep, row, rec, "engagement_status", []string{"passive", "engaged", "active", "disengaged"}, false)
		if engStatus == "" {
			engStatus = "passive"
		}
		shType := importEnum(&rep, row, rec, "stakeholder_type", []string{
			"mana_whenua", "elected_member", "government_agency", "regional_authority",
			"regulatory_partner", "infrastructure_partner", "business", "developer",
			"business_association", "strategic_supplier", "utility_provider",
			"community_organisation", "industry_group", "advocacy_group",
			"educational_institution", "major_landowner", "project_stakeholder", "other"}, false)
		roleClass := importEnum(&rep, row, rec, "role_classification", []string{"key", "decision_maker", "support", "influencer", "observer"}, false)
		if roleClass == "" {
			roleClass = "support"
		}
		influence := importIntField(&rep, row, rec, "influence_level", 1, 5)
		if influence == 0 {
			influence = 3
		}
		interestLvl := importIntField(&rep, row, rec, "interest_level", 1, 5)
		if interestLvl == 0 {
			interestLvl = 3
		}
		trustLevel := importEnum(&rep, row, rec, "trust_level", []string{"very_high", "high", "moderate", "low", "critical"}, false)
		riskRating := importEnum(&rep, row, rec, "risk_rating", []string{"low", "medium", "high", "critical"}, false)
		infoClass := importEnum(&rep, row, rec, "info_classification", []string{"public", "internal", "confidential", "restricted"}, false)
		if infoClass == "" {
			infoClass = "internal"
		}
		consent := importEnum(&rep, row, rec, "consent_status", []string{"not_required", "granted", "declined", "withdrawn", "unknown"}, false)
		if consent == "" {
			consent = "not_required"
		}
		reviewCadence := importEnum(&rep, row, rec, "review_cadence", []string{"monthly", "quarterly", "semi_annual", "annual"}, false)
		if reviewCadence == "" {
			reviewCadence = "quarterly"
		}
		lastEngagement := importDateField(&rep, row, rec, "last_engagement_at", false)
		nextEngagement := importDateField(&rep, row, rec, "next_engagement_at", false)
		locLat := importFloatRangeField(&rep, row, rec, "location_lat", -90, 90)
		locLon := importFloatRangeField(&rep, row, rec, "location_lon", -180, 180)
		ownerID := importPersonField(&rep, row, rec, c.usersByEmail, c.usersByName, false, "owner_user")
		backupOwnerID := importPersonField(&rep, row, rec, c.usersByEmail, c.usersByName, false, "backup_owner_user")
		sponsorID := importPersonField(&rep, row, rec, c.usersByEmail, c.usersByName, false, "executive_sponsor_user")
		idStr := strings.TrimSpace(rec["id"])

		// Match order: explicit id (export dialect) > external_id > email.
		existingID, exists := 0, false
		if extID != "" {
			existingID, exists = c.shByExtID[strings.ToLower(extID)]
		}
		if !exists && emailKey != "" {
			existingID, exists = c.shByEmail[emailKey]
		}
		if idStr != "" {
			n, err := strconv.Atoi(idStr)
			if err != nil || n <= 0 {
				rep.err(row, "id", fmt.Sprintf("invalid id %q", idStr))
				continue
			}
			existingID, exists = n, true
		}
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, idStr) {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				if emailKey != "" {
					c.shSeen[emailKey] = true
				}
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO sms_stakeholders (
					external_id, name, title, organization, email, phone, stakeholder_type, category, tier,
					business_unit, role_classification, influence_level, interest_level, interests, engagement_status,
					relationship_status, trust_level, risk_rating, communication_preferences, owner_user_id,
					backup_owner_user_id, executive_sponsor_user_id, last_engagement_at, next_engagement_at, info_classification,
					consent_status, retention_category, review_cadence, location_lat, location_lon, location_text, notes
				) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				importNullStr(extID), name, rec["title"], rec["organization"], rec["email"], rec["phone"],
				importNullStr(shType), rec["category"], tier, rec["business_unit"], roleClass, influence, interestLvl,
				rec["interests"], engStatus, relStatus, importNullStr(trustLevel), importNullStr(riskRating),
				rec["communication_preferences"], nullInt(ownerID), nullInt(backupOwnerID), nullInt(sponsorID),
				importNullStr(lastEngagement), importNullStr(nextEngagement), infoClass, consent, rec["retention_category"],
				reviewCadence, nullFloatPtr(locLat), nullFloatPtr(locLon), rec["location_text"], rec["notes"])
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			newID := int(id64)
			if emailKey != "" {
				c.shByEmail[emailKey] = newID
				c.shSeen[emailKey] = true
			}
			if extID != "" {
				c.shByExtID[strings.ToLower(extID)] = newID
			}
			rep.Created++
		default: // update - only non-empty fields are set, so partial sheets keep current values
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addText("name", "name")
			addText("title", "title")
			addText("organization", "organization")
			addText("email", "email")
			addText("phone", "phone")
			if shType != "" {
				sets = append(sets, "stakeholder_type=?")
				args = append(args, shType)
			}
			addText("category", "category")
			if strings.TrimSpace(rec["tier"]) != "" {
				sets = append(sets, "tier=?")
				args = append(args, tier)
			}
			addText("business_unit", "business_unit")
			if strings.TrimSpace(rec["role_classification"]) != "" {
				sets = append(sets, "role_classification=?")
				args = append(args, roleClass)
			}
			if strings.TrimSpace(rec["influence_level"]) != "" {
				sets = append(sets, "influence_level=?")
				args = append(args, influence)
			}
			if strings.TrimSpace(rec["interest_level"]) != "" {
				sets = append(sets, "interest_level=?")
				args = append(args, interestLvl)
			}
			addText("interests", "interests")
			if strings.TrimSpace(rec["engagement_status"]) != "" {
				sets = append(sets, "engagement_status=?")
				args = append(args, engStatus)
			}
			if strings.TrimSpace(rec["relationship_status"]) != "" {
				sets = append(sets, "relationship_status=?")
				args = append(args, relStatus)
			}
			if trustLevel != "" {
				sets = append(sets, "trust_level=?")
				args = append(args, trustLevel)
			}
			if riskRating != "" {
				sets = append(sets, "risk_rating=?")
				args = append(args, riskRating)
			}
			addText("communication_preferences", "communication_preferences")
			if ownerID != nil {
				sets = append(sets, "owner_user_id=?")
				args = append(args, *ownerID)
			}
			if backupOwnerID != nil {
				sets = append(sets, "backup_owner_user_id=?")
				args = append(args, *backupOwnerID)
			}
			if sponsorID != nil {
				sets = append(sets, "executive_sponsor_user_id=?")
				args = append(args, *sponsorID)
			}
			addText("last_engagement_at", "last_engagement_at")
			addText("next_engagement_at", "next_engagement_at")
			if strings.TrimSpace(rec["info_classification"]) != "" {
				sets = append(sets, "info_classification=?")
				args = append(args, infoClass)
			}
			if strings.TrimSpace(rec["consent_status"]) != "" {
				sets = append(sets, "consent_status=?")
				args = append(args, consent)
			}
			addText("retention_category", "retention_category")
			if strings.TrimSpace(rec["review_cadence"]) != "" {
				sets = append(sets, "review_cadence=?")
				args = append(args, reviewCadence)
			}
			if locLat != nil {
				sets = append(sets, "location_lat=?")
				args = append(args, *locLat)
			}
			if locLon != nil {
				sets = append(sets, "location_lon=?")
				args = append(args, *locLon)
			}
			addText("location_text", "location_text")
			addText("notes", "notes")
			if extID != "" {
				sets = append(sets, "external_id=?")
				args = append(args, extID)
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_stakeholders WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "id", fmt.Sprintf("stakeholder %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_stakeholders SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// sitePlan is one validated row of the sites & occupancy sheet, ready to write.
type sitePlan struct {
	rec          map[string]string
	row          int
	name         string
	extID        string
	kindKey      string
	kindID       int
	parentExt    string // "" = root node
	customFields string // validated JSON object (defaults to "{}")
	locLat       *float64
	locLon       *float64
	entityType   string // "" or portfolio/programme/project
	entityID     *int
	nodeID       int // execute mode: id of the upserted node

	hasOccupant bool
	shEmail     string
	role        string
	startDate   string
	endDate     string
	memberNotes string
}

func (h *SMSHandler) smsImportSites(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}

	// Pre-scan batch external ids so parent references may point at rows that
	// appear later in the sheet.
	for _, rec := range recs {
		if e := strings.TrimSpace(rec["external_id"]); e != "" {
			c.nodesSeen[strings.ToLower(e)] = true
		}
	}

	var plans []sitePlan
	noExtCount := 0
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		name := strings.TrimSpace(rec["name"])
		if name == "" {
			rep.err(row, "name", "name is required")
			continue
		}
		extID := strings.TrimSpace(rec["external_id"])
		if extID == "" {
			noExtCount++
		}

		kindKey := strings.ToLower(strings.TrimSpace(rec["kind"]))
		if kindKey == "" {
			kindKey = "building"
		}
		kindID, ok := c.nodeKindsByKey[kindKey]
		if !ok {
			var available []string
			for k := range c.nodeKindsByKey {
				available = append(available, k)
			}
			rep.err(row, "kind", fmt.Sprintf("unknown node kind %q (configured kinds: %s)", kindKey, strings.Join(available, ", ")))
			continue
		}

		parentExt := strings.TrimSpace(rec["parent_external_id"])
		if parentExt != "" {
			pkey := strings.ToLower(parentExt)
			if _, ok := c.nodesByExtID[pkey]; !ok && !c.nodesSeen[pkey] {
				rep.err(row, "parent_external_id", fmt.Sprintf("unknown parent site external_id %q", parentExt))
				continue
			}
		}

		cf := strings.TrimSpace(rec["custom_fields"])
		if cf == "" {
			cf = "{}"
		} else if !strings.HasPrefix(cf, "{") {
			rep.err(row, "custom_fields", "must be a JSON object, e.g. {\"access\":\"rear only during works\"}")
			continue
		} else {
			var obj map[string]any
			if err := json.Unmarshal([]byte(cf), &obj); err != nil {
				rep.err(row, "custom_fields", fmt.Sprintf("invalid JSON object: %v", err))
				continue
			}
		}

		locLat := importFloatRangeField(&rep, row, rec, "location_lat", -90, 90)
		locLon := importFloatRangeField(&rep, row, rec, "location_lon", -180, 180)

		et := strings.ToLower(strings.TrimSpace(rec["entity_type"]))
		eidStr := strings.TrimSpace(rec["entity_id"])
		var entityID *int
		if et != "" || eidStr != "" {
			switch et {
			case "portfolio", "programme", "project":
			default:
				rep.err(row, "entity_type", "entity_type must be portfolio, programme or project (or leave both scope columns blank)")
				continue
			}
			n, err := strconv.Atoi(eidStr)
			if err != nil || n <= 0 {
				rep.err(row, "entity_id", fmt.Sprintf("invalid P3M entity id %q", eidStr))
				continue
			}
			entityID = &n
		}

		shEmail := strings.TrimSpace(rec["stakeholder_email"])
		if shEmail != "" {
			var ok bool
			if _, ok = c.stakeholderField(&rep, row, rec, "stakeholder_email"); !ok {
				continue
			}
		} else if strings.TrimSpace(rec["occupancy_role"]) != "" || strings.TrimSpace(rec["member_notes"]) != "" {
			rep.warn(row, "stakeholder_email", "occupant columns ignored without stakeholder_email")
		}

		var role string
		if shEmail != "" {
			role = strings.TrimSpace(rec["occupancy_role"])
			if role != "" && len(c.kindRoles[kindID]) > 0 && !c.kindRoles[kindID][role] {
				var allowed []string
				for r := range c.kindRoles[kindID] {
					allowed = append(allowed, r)
				}
				rep.err(row, "occupancy_role", fmt.Sprintf("invalid role %q for kind %q (allowed: %s)", role, kindKey, strings.Join(allowed, ", ")))
				continue
			}
		}

		startDate := importDateField(&rep, row, rec, "tenancy_start", false)
		endDate := importDateField(&rep, row, rec, "tenancy_end", false)

		if len(rep.Errors) > errCount {
			continue
		}

		plans = append(plans, sitePlan{
			rec: rec, row: row, name: name, extID: extID, kindKey: kindKey, kindID: kindID,
			parentExt: parentExt, customFields: cf, locLat: locLat, locLon: locLon,
			entityType: et, entityID: entityID, hasOccupant: shEmail != "", shEmail: shEmail,
			role: role, startDate: startDate, endDate: endDate, memberNotes: rec["member_notes"],
		})
	}

	if noExtCount > 0 {
		rep.warn(0, "", fmt.Sprintf("%d rows have no external_id and will be created again on re-import", noExtCount))
	}

	// Phase 1: upsert the nodes. A parent that appears later in the sheet is
	// resolved after all inserts (phase 2). batchNodeIDs dedupes repeated rows
	// for the same external_id within this payload; it holds 0 until a node is
	// actually created, so preview counts stay correct too.
	batchNodeIDs := make(map[string]int, len(plans)) // lower(extID) -> node id
	for i := range plans {
		p := &plans[i]

		existingID, exists := 0, false
		reused := false
		if p.extID != "" {
			key := strings.ToLower(p.extID)
			if id, ok := batchNodeIDs[key]; ok {
				// Same node already upserted earlier in this payload (repeated
				// occupancy rows): reuse it without touching the counters.
				p.nodeID = id
				reused = true
			} else {
				existingID, exists = c.nodesByExtID[key]
				batchNodeIDs[key] = existingID // 0 when absent; create overwrites below
			}
		}

		if !reused {
			switch importDecide(c.mode, exists, "") {
			case "skip":
				rep.Skipped++
			case "create":
				if !c.executing {
					rep.ToCreate++
					continue
				}
				var parentID any
				if p.parentExt != "" {
					if pid, ok := c.nodesByExtID[strings.ToLower(p.parentExt)]; ok {
						parentID = pid
					} else if pid, ok := batchNodeIDs[strings.ToLower(p.parentExt)]; ok && pid > 0 {
						parentID = pid
					}
				}
				res, err := c.exec.Exec(`
					INSERT INTO sms_nodes (external_id, kind_id, parent_id, name, description, location_lat, location_lon, location_text, custom_fields, entity_type, entity_id)
					VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
					importNullStr(p.extID), p.kindID, parentID, p.name, p.rec["description"],
					nullFloatPtr(p.locLat), nullFloatPtr(p.locLon), p.rec["location_text"],
					p.customFields, importNullStr(p.entityType), nullInt(p.entityID))
				if err != nil {
					rep.err(p.row, "", fmt.Sprintf("insert failed: %v", err))
					continue
				}
				id64, _ := res.LastInsertId()
				p.nodeID = int(id64)
				if p.extID != "" {
					batchNodeIDs[strings.ToLower(p.extID)] = p.nodeID
					c.nodesByExtID[strings.ToLower(p.extID)] = p.nodeID
				}
				rep.Created++
			default: // update - only non-empty fields are set
				var sets []string
				var args []any
				addText := func(col, field string) {
					if v := p.rec[field]; v != "" {
						sets = append(sets, col+"=?")
						args = append(args, v)
					}
				}
				addText("name", "name")
				addText("description", "description")
				if strings.TrimSpace(p.rec["kind"]) != "" {
					sets = append(sets, "kind_id=?")
					args = append(args, p.kindID)
				}
				if p.parentExt != "" {
					if pid, ok := c.nodesByExtID[strings.ToLower(p.parentExt)]; ok {
						sets = append(sets, "parent_id=?")
						args = append(args, pid)
					} else if pid, ok := batchNodeIDs[strings.ToLower(p.parentExt)]; ok && pid > 0 {
						sets = append(sets, "parent_id=?")
						args = append(args, pid)
					}
				}
				addText("location_text", "location_text")
				if v := p.rec["custom_fields"]; v != "" {
					sets = append(sets, "custom_fields=?")
					args = append(args, v)
				}
				p.nodeID = existingID // occupancy below applies even on no-op updates
				if len(sets) == 0 {
					rep.Skipped++ // nothing to change
					continue
				}
				var curID int
				if err := c.exec.QueryRow(`SELECT id FROM sms_nodes WHERE id = ?`, existingID).Scan(&curID); err != nil {
					rep.err(p.row, "external_id", fmt.Sprintf("node %d not found: %v", existingID, err))
					continue
				}
				if !c.executing {
					rep.ToUpdate++
					continue
				}
				if _, err := c.exec.Exec(`UPDATE sms_nodes SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
					rep.err(p.row, "", fmt.Sprintf("update failed: %v", err))
					continue
				}
				if c.executing {
					rep.Updated++
				} else {
					rep.ToUpdate++
				}
			}
		}

		// Occupancy membership (idempotent via OR IGNORE).
		if p.hasOccupant && c.executing {
			shID := c.shByEmail[strings.ToLower(p.shEmail)]
			if shID == 0 {
				rep.err(p.row, "stakeholder_email", fmt.Sprintf("stakeholder %q not found at write time", p.shEmail))
				continue
			}
			if _, err := c.exec.Exec(`
				INSERT OR IGNORE INTO sms_node_members (node_id, stakeholder_id, role, start_date, end_date, notes)
				VALUES (?,?,?,?,?,?)`,
				p.nodeID, shID, p.role, importNullStr(p.startDate), importNullStr(p.endDate), p.memberNotes); err != nil {
				rep.err(p.row, "", fmt.Sprintf("insert membership failed: %v", err))
			}
		}
	}

	// Phase 3: resolve parents that were created later in the same payload.
	for i := range plans {
		p := &plans[i]
		if p.parentExt == "" || !c.executing || p.nodeID == 0 {
			continue
		}
		pid, ok := c.nodesByExtID[strings.ToLower(p.parentExt)]
		if !ok {
			rep.err(p.row, "parent_external_id", fmt.Sprintf("parent site %q was not created (its row may have failed validation)", p.parentExt))
			continue
		}
		var curParent sql.NullInt64
		if err := c.exec.QueryRow(`SELECT parent_id FROM sms_nodes WHERE id = ?`, p.nodeID).Scan(&curParent); err != nil {
			rep.err(p.row, "", fmt.Sprintf("read node failed: %v", err))
			continue
		}
		if curParent.Valid && int(curParent.Int64) == pid {
			continue // already set at insert time
		}
		if _, err := c.exec.Exec(`UPDATE sms_nodes SET parent_id = ? WHERE id = ?`, pid, p.nodeID); err != nil {
			rep.err(p.row, "", fmt.Sprintf("set parent failed: %v", err))
		}
	}
	return rep
}

func (h *SMSHandler) smsImportGroups(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	batchGroups := make(map[string]bool) // names created earlier in this payload (preview has no ids)
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		groupName := strings.TrimSpace(rec["group_name"])
		if groupName == "" {
			rep.err(row, "group_name", "group_name is required")
			continue
		}
		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		if len(rep.Errors) > errCount {
			continue
		}

		gkey := strings.ToLower(groupName)
		groupID, inDB := c.groupsByName[gkey]
		switch {
		case !inDB && batchGroups[gkey]:
			// Group already handled earlier in this payload; membership only.
		case !inDB:
			if !c.executing {
				rep.ToCreate++
				batchGroups[gkey] = true
			} else {
				res, err := c.exec.Exec(`INSERT INTO sms_stakeholder_groups (name, description) VALUES (?,?)`, groupName, rec["description"])
				if err != nil {
					rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
					continue
				}
				id64, _ := res.LastInsertId()
				groupID = int(id64)
				c.groupsByName[gkey] = groupID
				batchGroups[gkey] = true
				rep.Created++
			}
		default: // exists in DB
			if c.mode == "upsert" && strings.TrimSpace(rec["description"]) != "" {
				var curID int
				if err := c.exec.QueryRow(`SELECT id FROM sms_stakeholder_groups WHERE id = ?`, groupID).Scan(&curID); err != nil {
					rep.err(row, "group_name", fmt.Sprintf("group %d not found: %v", groupID, err))
					continue
				}
				if !c.executing {
					rep.ToUpdate++
					continue
				}
				if _, err := c.exec.Exec(`UPDATE sms_stakeholder_groups SET description = ? WHERE id = ?`, rec["description"], groupID); err != nil {
					rep.err(row, "", fmt.Sprintf("update failed: %v", err))
					continue
				}
				if c.executing {
					rep.Updated++
				} else {
					rep.ToUpdate++
				}
			}
		}

		if c.executing && shID != nil {
			if _, err := c.exec.Exec(`INSERT OR IGNORE INTO sms_stakeholder_group_members (group_id, stakeholder_id) VALUES (?,?)`, groupID, *shID); err != nil {
				rep.err(row, "", fmt.Sprintf("insert membership failed: %v", err))
			}
		}
	}
	return rep
}

func (h *SMSHandler) smsImportEngagementPlans(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "lead_stakeholder_email")
		if !ok {
			continue
		}
		approach := importEnum(&rep, row, rec, "approach", []string{"inform", "consult", "involve", "collaborate", "partner"}, false)
		frequency := importEnum(&rep, row, rec, "frequency", []string{"daily", "weekly", "biweekly", "monthly", "quarterly", "annual", "as_needed"}, false)
		if frequency == "" {
			frequency = "monthly"
		}
		channel := importEnum(&rep, row, rec, "channel", []string{"email", "phone", "meeting", "in_person", "slack", "teams", "letter", "sms_text", "other"}, false)
		if channel == "" {
			channel = "email"
		}
		status := importEnum(&rep, row, rec, "status", []string{"draft", "active", "paused", "completed"}, false)
		if status == "" {
			status = "active"
		}
		entityType, entityID := c.smsResolveScope(&rep, row, rec)
		startDate := importDateField(&rep, row, rec, "start_date", false)
		endDate := importDateField(&rep, row, rec, "end_date", false)

		var memberIDs []int
		if extra := strings.TrimSpace(rec["additional_stakeholder_emails"]); extra != "" {
			for _, part := range strings.Split(extra, ",") {
				email := strings.ToLower(strings.TrimSpace(part))
				if email == "" {
					continue
				}
				id, ok := c.shByEmail[email]
				if !ok && !c.executing && c.shSeen[email] {
					continue // resolvable at execute time; re-resolved below is not possible in preview - nothing to write anyway
				}
				if !ok {
					rep.warn(row, "additional_stakeholder_emails", fmt.Sprintf("unknown stakeholder email %q skipped (must match an SMS Stakeholders row or existing register entry)", part))
					continue
				}
				memberIDs = append(memberIDs, id)
			}
		}

		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing {
			rep.ToCreate++
			continue
		}
		res, err := c.exec.Exec(`
			INSERT INTO sms_engagement_plans (stakeholder_id, strategy, objective, approach, key_messages, success_measures, frequency, channel, status, entity_type, entity_id, start_date, end_date)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			*shID, rec["strategy"], rec["objective"], importNullStr(approach), rec["key_messages"], rec["success_measures"],
			frequency, channel, status, entityType, entityID, importNullStr(startDate), importNullStr(endDate))
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		planID64, _ := res.LastInsertId()
		for _, mid := range memberIDs {
			if _, err := c.exec.Exec(`INSERT OR IGNORE INTO sms_engagement_plan_stakeholders (plan_id, stakeholder_id) VALUES (?,?)`, planID64, mid); err != nil {
				rep.err(row, "", fmt.Sprintf("insert plan membership failed: %v", err))
			}
		}
		rep.Created++
	}
	if rep.Rows > 0 {
		rep.warn(0, "", "engagement plans have no stable id in the workbook; re-importing this sheet creates duplicate plans")
	}
	return rep
}

func (h *SMSHandler) smsImportIssues(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		kind := importEnum(&rep, row, rec, "kind", []string{"issue", "risk"}, false)
		if kind == "" {
			kind = "issue"
		}
		category := importEnum(&rep, row, rec, "category", []string{"relationship", "reputation", "delivery", "communication", "dependency", "knowledge_loss", "commitment", "other"}, false)
		if category == "" {
			category = "other"
		}
		priority := importEnum(&rep, row, rec, "priority", []string{"low", "medium", "high", "critical"}, false)
		if priority == "" {
			priority = "medium"
		}
		impact := importIntField(&rep, row, rec, "impact", 1, 5)
		if impact == 0 {
			impact = 3
		}
		likelihood := importIntField(&rep, row, rec, "likelihood", 1, 5)
		if likelihood == 0 {
			likelihood = 3
		}
		status := importEnum(&rep, row, rec, "status", []string{"open", "in_progress", "escalated", "resolved", "closed"}, false)
		if status == "" {
			status = "open"
		}
		escalationLevel := importIntField(&rep, row, rec, "escalation_level", 0, 24)

		existingID, exists := c.smsNaturalKeyID(&rep, row, "sms_issues", shID, "title", title)
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing || shID == nil {
				rep.ToCreate++
				continue
			}
			_, err = c.exec.Exec(`
				INSERT INTO sms_issues (stakeholder_id, kind, category, title, description, priority, impact, likelihood, status, escalation_level, escalation_path, mitigation, resolution_notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				*shID, kind, category, title, rec["description"], priority, impact, likelihood, status,
				escalationLevel, rec["escalation_path"], rec["mitigation"], rec["resolution_notes"])
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			if strings.TrimSpace(rec["kind"]) != "" {
				sets = append(sets, "kind=?")
				args = append(args, kind)
			}
			if strings.TrimSpace(rec["category"]) != "" {
				sets = append(sets, "category=?")
				args = append(args, category)
			}
			addText("title", "title")
			addText("description", "description")
			if strings.TrimSpace(rec["priority"]) != "" {
				sets = append(sets, "priority=?")
				args = append(args, priority)
			}
			if strings.TrimSpace(rec["impact"]) != "" {
				sets = append(sets, "impact=?")
				args = append(args, impact)
			}
			if strings.TrimSpace(rec["likelihood"]) != "" {
				sets = append(sets, "likelihood=?")
				args = append(args, likelihood)
			}
			if strings.TrimSpace(rec["status"]) != "" {
				sets = append(sets, "status=?")
				args = append(args, status)
			}
			if strings.TrimSpace(rec["escalation_level"]) != "" {
				sets = append(sets, "escalation_level=?")
				args = append(args, escalationLevel)
			}
			addText("escalation_path", "escalation_path")
			addText("mitigation", "mitigation")
			addText("resolution_notes", "resolution_notes")
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_issues WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "title", fmt.Sprintf("issue %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_issues SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *SMSHandler) smsImportCommitments(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		direction := importEnum(&rep, row, rec, "direction", []string{"from_us", "from_them", "mutual"}, false)
		if direction == "" {
			direction = "from_them"
		}
		status := importEnum(&rep, row, rec, "status", []string{"open", "in_progress", "at_risk", "completed", "cancelled"}, false)
		if status == "" {
			status = "open"
		}
		ownerID := importPersonField(&rep, row, rec, c.usersByEmail, c.usersByName, false, "owner_user")
		dueDate := importDateField(&rep, row, rec, "due_date", false)
		escalated := importBoolField(&rep, row, rec, "escalated")

		existingID, exists := c.smsNaturalKeyID(&rep, row, "sms_commitments", shID, "title", title)
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing || shID == nil {
				rep.ToCreate++
				continue
			}
			esc := false
			if escalated != nil {
				esc = *escalated
			}
			_, err = c.exec.Exec(`
				INSERT INTO sms_commitments (stakeholder_id, direction, title, description, owner_user_id, due_date, status, escalated)
				VALUES (?,?,?,?,?,?,?,?)`,
				*shID, direction, title, rec["description"], nullInt(ownerID), importNullStr(dueDate), status, esc)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			if strings.TrimSpace(rec["direction"]) != "" {
				sets = append(sets, "direction=?")
				args = append(args, direction)
			}
			addText("title", "title")
			addText("description", "description")
			if ownerID != nil {
				sets = append(sets, "owner_user_id=?")
				args = append(args, *ownerID)
			}
			addText("due_date", "due_date")
			if strings.TrimSpace(rec["status"]) != "" {
				sets = append(sets, "status=?")
				args = append(args, status)
			}
			if escalated != nil {
				sets = append(sets, "escalated=?")
				args = append(args, *escalated)
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_commitments WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "title", fmt.Sprintf("commitment %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_commitments SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *SMSHandler) smsImportGrievances(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		severity := importEnum(&rep, row, rec, "severity", []string{"low", "medium", "high", "critical"}, false)
		if severity == "" {
			severity = "medium"
		}
		status := importEnum(&rep, row, rec, "status", []string{"received", "acknowledged", "investigating", "responded", "closed"}, false)
		if status == "" {
			status = "received"
		}
		responseDueAt := importDateField(&rep, row, rec, "response_due_at", false)

		existingID, exists := c.smsNaturalKeyID(&rep, row, "sms_grievances", shID, "title", title)
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing || shID == nil {
				rep.ToCreate++
				continue
			}
			_, err = c.exec.Exec(`
				INSERT INTO sms_grievances (stakeholder_id, title, description, category, severity, status, response_due_at, response, resolution)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				*shID, title, rec["description"], rec["category"], severity, status, importNullStr(responseDueAt), rec["response"], rec["resolution"])
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addText("title", "title")
			addText("description", "description")
			addText("category", "category")
			if strings.TrimSpace(rec["severity"]) != "" {
				sets = append(sets, "severity=?")
				args = append(args, severity)
			}
			if strings.TrimSpace(rec["status"]) != "" {
				sets = append(sets, "status=?")
				args = append(args, status)
			}
			addText("response_due_at", "response_due_at")
			addText("response", "response")
			addText("resolution", "resolution")
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_grievances WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "title", fmt.Sprintf("grievance %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_grievances SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *SMSHandler) smsImportDecisions(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		decision := strings.TrimSpace(rec["decision"])
		if title == "" {
			rep.err(row, "title", "title is required")
		}
		if decision == "" {
			rep.err(row, "decision", "decision is required")
		}
		entityType, entityID := c.smsResolveScope(&rep, row, rec)
		decidedAt := importDateField(&rep, row, rec, "decided_at", false)
		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing || shID == nil {
			rep.ToCreate++
			continue
		}
		_, err = c.exec.Exec(`
			INSERT INTO sms_decisions (stakeholder_id, title, decision, context, decided_at, entity_type, entity_id)
			VALUES (?,?,?,?,?,?,?)`,
			*shID, title, decision, rec["context"], importNullStr(decidedAt), entityType, entityID)
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		rep.Created++
	}
	rep.appendOnlyNote("the decisions log is append-only; re-importing this sheet creates duplicate entries")
	return rep
}

func (h *SMSHandler) smsImportTasks(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		status := importEnum(&rep, row, rec, "status", []string{"open", "in_progress", "done", "cancelled"}, false)
		if status == "" {
			status = "open"
		}
		assignedToID := importPersonField(&rep, row, rec, c.usersByEmail, c.usersByName, false, "assigned_to_user")
		dueDate := importDateField(&rep, row, rec, "due_date", false)

		existingID, exists := c.smsNaturalKeyID(&rep, row, "sms_tasks", shID, "title", title)
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing || shID == nil {
				rep.ToCreate++
				continue
			}
			_, err = c.exec.Exec(`
				INSERT INTO sms_tasks (stakeholder_id, title, description, assigned_to_user_id, due_date, status)
				VALUES (?,?,?,?,?,?)`,
				*shID, title, rec["description"], nullInt(assignedToID), importNullStr(dueDate), status)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addText("title", "title")
			addText("description", "description")
			if assignedToID != nil {
				sets = append(sets, "assigned_to_user_id=?")
				args = append(args, *assignedToID)
			}
			addText("due_date", "due_date")
			if strings.TrimSpace(rec["status"]) != "" {
				sets = append(sets, "status=?")
				args = append(args, status)
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_tasks WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "title", fmt.Sprintf("task %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_tasks SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *SMSHandler) smsImportFeedback(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		source := importEnum(&rep, row, rec, "source", []string{"form", "survey", "email", "meeting", "other"}, false)
		if source == "" {
			source = "form"
		}
		rating := importIntField(&rep, row, rec, "rating", 1, 5) // 0 = not provided (NULL)
		resolutionStatus := importEnum(&rep, row, rec, "resolution_status", []string{"open", "addressed", "resolved"}, false)
		if resolutionStatus == "" {
			resolutionStatus = "open"
		}
		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing || shID == nil {
			rep.ToCreate++
			continue
		}
		var ratingArg any
		if rating > 0 {
			ratingArg = rating
		}
		_, err = c.exec.Exec(`
			INSERT INTO sms_feedback (stakeholder_id, source, category, rating, body, resolution_status, resolution_notes)
			VALUES (?,?,?,?,?,?,?)`,
			*shID, source, rec["category"], ratingArg, rec["body"], resolutionStatus, rec["resolution_notes"])
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		rep.Created++
	}
	rep.appendOnlyNote("feedback is append-only; re-importing this sheet creates duplicate entries")
	return rep
}

func (h *SMSHandler) smsImportGoals(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		goal := strings.TrimSpace(rec["goal"])
		if goal == "" {
			rep.err(row, "goal", "goal is required")
			continue
		}
		status := importEnum(&rep, row, rec, "status", []string{"on_track", "at_risk", "missed", "achieved"}, false)
		if status == "" {
			status = "on_track"
		}
		targetValue := importFloatRangeField(&rep, row, rec, "target_value", -1e9, 1e9)
		currentValue := importFloatRangeField(&rep, row, rec, "current_value", -1e9, 1e9)

		existingID, exists := c.smsGoalKeyID(&rep, row, shID, goal)
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing || shID == nil {
				rep.ToCreate++
				continue
			}
			_, err = c.exec.Exec(`
				INSERT INTO sms_relationship_goals (stakeholder_id, goal, metric, target_value, current_value, status)
				VALUES (?,?,?,?,?,?)`,
				*shID, goal, rec["metric"], nullFloatPtr(targetValue), nullFloatPtr(currentValue), status)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var sets []string
			var args []any
			addText := func(col, field string) {
				if v := rec[field]; v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addText("goal", "goal")
			addText("metric", "metric")
			if targetValue != nil {
				sets = append(sets, "target_value=?")
				args = append(args, *targetValue)
			}
			if currentValue != nil {
				sets = append(sets, "current_value=?")
				args = append(args, *currentValue)
			}
			if strings.TrimSpace(rec["status"]) != "" {
				sets = append(sets, "status=?")
				args = append(args, status)
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			var curID int
			if err := c.exec.QueryRow(`SELECT id FROM sms_relationship_goals WHERE id = ?`, existingID).Scan(&curID); err != nil {
				rep.err(row, "goal", fmt.Sprintf("goal %d not found: %v", existingID, err))
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE sms_relationship_goals SET `+strings.Join(sets, ", ")+" WHERE id = ?", append(args, existingID)...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *SMSHandler) smsImportCommunications(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		shID, ok := c.stakeholderField(&rep, row, rec, "stakeholder_email")
		if !ok {
			continue
		}
		channel := importEnum(&rep, row, rec, "channel", []string{"email", "phone", "meeting", "in_person", "slack", "teams", "letter", "sms_text", "other"}, false)
		if channel == "" {
			channel = "email"
		}
		direction := importEnum(&rep, row, rec, "direction", []string{"outbound", "inbound"}, false)
		if direction == "" {
			direction = "outbound"
		}
		sentiment := importEnum(&rep, row, rec, "sentiment", []string{"positive", "neutral", "negative"}, false)
		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing || shID == nil {
			rep.ToCreate++
			continue
		}
		_, err = c.exec.Exec(`
			INSERT INTO sms_communications (stakeholder_id, channel, direction, subject, body, sentiment, created_by)
			VALUES (?,?,?,?,?,?,?)`,
			*shID, channel, direction, rec["subject"], rec["body"], importNullStr(sentiment), nullInt(&c.actorID))
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		rep.Created++
	}
	rep.appendOnlyNote("the communications log is append-only; re-importing this sheet creates duplicate entries")
	return rep
}

func (h *SMSHandler) smsImportNetworkLinks(c *smsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		srcEmail := strings.TrimSpace(rec["source_stakeholder_email"])
		tgtEmail := strings.TrimSpace(rec["target_stakeholder_email"])
		if srcEmail != "" && tgtEmail != "" && strings.EqualFold(srcEmail, tgtEmail) {
			rep.err(row, "target_stakeholder_email", "source and target must be different stakeholders")
			continue
		}

		srcID, okSrc := c.stakeholderField(&rep, row, rec, "source_stakeholder_email")
		tgtID, okTgt := c.stakeholderField(&rep, row, rec, "target_stakeholder_email")
		if !okSrc || !okTgt {
			continue
		}
		if len(rep.Errors) > errCount {
			continue
		}

		// Links are undirected: store once with source_id < target_id. The
		// existence check runs in both modes so preview counts match execute.
		if srcID != nil && tgtID != nil {
			src, tgt := *srcID, *tgtID
			if src > tgt {
				src, tgt = tgt, src
			}
			var existingID int
			err := c.exec.QueryRow(`SELECT id FROM sms_stakeholder_links WHERE source_id = ? AND target_id = ? LIMIT 1`, src, tgt).Scan(&existingID)
			if err == nil {
				rep.Skipped++ // link already exists (idempotent re-import)
				continue
			}
			if err != sql.ErrNoRows {
				rep.err(row, "", fmt.Sprintf("lookup failed: %v", err))
				continue
			}
		}

		if !c.executing || srcID == nil || tgtID == nil {
			rep.ToCreate++
			continue
		}
		src, tgt := *srcID, *tgtID
		if src > tgt {
			src, tgt = tgt, src
		}
		_, err = c.exec.Exec(`INSERT INTO sms_stakeholder_links (source_id, target_id, relation_type, notes) VALUES (?,?,?,?)`, src, tgt, rec["relation_type"], rec["notes"])
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		rep.Created++
	}
	return rep
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

func (h *SMSHandler) smsProcessSheet(c *smsImportContext, name, csvText string) importSheetReport {
	switch name {
	case "SMS Stakeholders":
		return h.smsImportStakeholders(c, csvText)
	case "SMS Sites & Occupancy":
		return h.smsImportSites(c, csvText)
	case "SMS Groups":
		return h.smsImportGroups(c, csvText)
	case "SMS Engagement Plans":
		return h.smsImportEngagementPlans(c, csvText)
	case "SMS Issues & Risks":
		return h.smsImportIssues(c, csvText)
	case "SMS Commitments":
		return h.smsImportCommitments(c, csvText)
	case "SMS Grievances":
		return h.smsImportGrievances(c, csvText)
	case "SMS Decisions Log":
		return h.smsImportDecisions(c, csvText)
	case "SMS Tasks":
		return h.smsImportTasks(c, csvText)
	case "SMS Feedback":
		return h.smsImportFeedback(c, csvText)
	case "SMS Relationship Goals":
		return h.smsImportGoals(c, csvText)
	case "SMS Communications Log":
		return h.smsImportCommunications(c, csvText)
	case "SMS Network Links":
		return h.smsImportNetworkLinks(c, csvText)
	}
	return importSheetReport{Errors: []importRowError{{Row: 1, Field: "header", Message: fmt.Sprintf("unknown sheet %q", name)}}}
}

func (h *SMSHandler) smsParseWorkbookRequest(w http.ResponseWriter, r *http.Request) (*bulkImportRequest, string, bool) {
	var req bulkImportRequest
	if err := newJSONDecoder(w, r).Decode(&req); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return nil, "", false
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "upsert"
	}
	if mode != "create" && mode != "upsert" {
		respondValidationError(w, r, "mode must be create or upsert")
		return nil, "", false
	}
	for name := range req.Sheets {
		if !smsSheetKnown(name) {
			respondValidationError(w, r, fmt.Sprintf("unknown sheet %q (expected one of: %s)", name, strings.Join(smsImportSheetNames, ", ")))
			return nil, "", false
		}
	}
	return &req, mode, true
}

// WorkbookPreview handles POST /api/sms/workbook/preview. Validates every sheet
// against the current database state without writing anything.
func (h *SMSHandler) WorkbookPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	req, mode, okReq := h.smsParseWorkbookRequest(w, r)
	if !okReq {
		return
	}
	c, err := h.newSMSImportContext(r.Context(), h.db, mode, 0, false)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	for _, name := range smsImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.smsProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
	}
	respondJSONOK(w, resp)
}

// WorkbookExecute handles POST /api/sms/workbook/execute. Writes all valid rows
// in dependency order inside a single transaction; invalid rows are skipped and
// reported. On any database failure the whole transaction rolls back.
func (h *SMSHandler) WorkbookExecute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	req, mode, okReq := h.smsParseWorkbookRequest(w, r)
	if !okReq {
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("begin import transaction: %w", err))
		return
	}
	c, err := h.newSMSImportContext(r.Context(), tx, mode, user.ID, true)
	if err != nil {
		tx.Rollback()
		respondInternalError(w, r, err)
		return
	}

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	sheetCounts := make(map[string]int64)
	for _, name := range smsImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.smsProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
		sheetCounts[name] = rep.Created + rep.Updated
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		respondInternalError(w, r, fmt.Errorf("commit import (rolled back): %w", err))
		return
	}

	smsAudit(h, r, user, "sms.workbook.import.execute", "sms_workbook_import", nil, mode, map[string]any{
		"mode":   mode,
		"sheets": sheetCounts,
	})
	respondJSONOK(w, resp)
}
