package handlers

// Bulk CSV import for the P3M registers (workbook round-trip). The request
// carries one or more sheets of the data-collection workbook as raw CSV text;
// preview validates everything without writing, execute writes in dependency
// order inside a single transaction. Sheet names and headers match
// templates/windshift-data-collection-template.xlsx exactly; the P3M export
// dialect (GET /p3m/export) is accepted too, so an exported register can be
// edited and re-imported - rows carrying an explicit id always upsert by id
// regardless of mode. Updates only set non-empty fields, so partial sheets do
// not wipe existing values.

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// p3mImportSheetNames lists every sheet the importer accepts, in processing
// (dependency) order. Unknown sheet names are rejected up front.
var p3mImportSheetNames = []string{
	"P3M Portfolios",
	"P3M Programmes",
	"P3M Projects",
	"P3M Intakes (Front Door)",
	"P3M Investment Cases",
	"P3M Benefits",
	"P3M Benefit Realizations",
	"P3M Budget Tranches",
	"P3M RAID Items",
	"P3M Stakeholders",
	"P3M Communications Log",
	"P3M Resources",
	"P3M Resource Allocations",
}

func p3mSheetKnown(name string) bool {
	for _, n := range p3mImportSheetNames {
		if n == name {
			return true
		}
	}
	return false
}

type bulkImportRequest struct {
	Mode   string            `json:"mode"` // "create" | "upsert"; default upsert
	Sheets map[string]string `json:"sheets"`
}

// importRowError is one invalid or unresolvable row. Row is the 1-based CSV
// line (header = 1), matching the SMS import contract.
type importRowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// importRowWarning is a non-fatal problem: the row imports, but an optional
// reference could not be resolved and was left empty.
type importRowWarning struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type importSheetReport struct {
	Rows     int                `json:"rows"`
	ToCreate int64              `json:"to_create,omitempty"` // preview only
	ToUpdate int64              `json:"to_update,omitempty"` // preview only
	Created  int64              `json:"created,omitempty"`   // execute only
	Updated  int64              `json:"updated,omitempty"`   // execute only
	Skipped  int64              `json:"skipped,omitempty"`   // duplicates (create mode) / no-op updates
	Errors   []importRowError   `json:"errors"`
	Warnings []importRowWarning `json:"warnings,omitempty"`
}

type importResponse struct {
	Mode       string                       `json:"mode"`
	Sheets     map[string]importSheetReport `json:"sheets"`
	TotalRows  int64                        `json:"total_rows"`
	ErrorCount int                          `json:"error_count"`
}

// importExecer is satisfied by both database.Database (preview reads) and
// database.Tx (execute writes), so sheet processors share one code path.
type importExecer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// p3mImportContext carries the shared indexes every sheet needs: person and
// SMS-register lookups, lifecycle stages, and per-entity maps seeded from the
// database that grow as rows are processed, so later sheets can reference
// entities created earlier in the same payload.
type p3mImportContext struct {
	ctx       context.Context
	exec      importExecer
	mode      string // "create" | "upsert"
	actorID   int    // importing user (created_by columns)
	executing bool

	// Person resolution: email first, then "first last".
	usersByEmail map[string]int
	usersByName  map[string][]int
	smsByEmail   map[string]int

	enabledStages map[string]bool

	// Existing-entity indexes.
	portfoliosByTitle map[string]int // lower(title) -> id
	programmesByKey   map[string]int // lower(pf)\x00lower(title) -> id
	programmesByTitle map[string][]int
	projectsByExtRef  map[string]int // lower(external_ref) -> id
	projectsByKey     map[string]int // lower(pf)\x00lower(prg)\x00lower(title) -> id
	projectsByTitle   map[string][]int
	intakesByTitle    map[string]int
	casesByProject    map[int]int
	benefitsByKey     map[string]int // entityType\x00entityID\x00lower(title) -> id
	benefitTitles     map[string][]int
	benefitProject    map[int]*int   // benefit id -> project_id (nil = programme scope)
	realizationsKey   map[string]int // benefitID\x00date -> id
	tranchesByKey     map[string]int // entityType\x00entityID\x00lower(title) -> id
	raidByKey         map[string]int
	stakeholdersByKey map[string]int
	resourcesByName   map[string]int
	allocationsByKey  map[string]int // resourceID\x00projectID(0=bau)\x00start_date -> id

	// willExist sets: entities present in the DB or created earlier in this
	// payload. Used for cross-sheet reference validation (preview has no real
	// ids to hand out).
	pfSeen     map[string]bool
	prgSeen    map[string]bool
	prjSeen    map[string]bool // "ref\x00<ext>" OR pf\x00prg\x00title
	intakeSeen map[string]bool
	benSeen    map[string]bool // lower(title)
	resSeen    map[string]bool // lower(name)

	// Title-only willExist sets for export-dialect references (entity_title).
	prgSeenTitles map[string]bool
	prjSeenTitles map[string]bool
}

func p3mImportKey(parts ...string) string { return strings.Join(parts, "\x00") }

// derefInt returns 0 for a nil pointer (BAU allocation key component).
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// nullInt64Val converts an optional project id to its SQL NULL form.
func nullInt64Val(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

// newP3MImportContext loads the shared indexes. exec is h.db for preview and
// the import transaction for execute.
func (h *P3MHandler) newP3MImportContext(ctx context.Context, exec importExecer, mode string, actorID int, executing bool) (*p3mImportContext, error) {
	c := &p3mImportContext{
		ctx:               ctx,
		exec:              exec,
		mode:              mode,
		actorID:           actorID,
		executing:         executing,
		usersByEmail:      make(map[string]int),
		usersByName:       make(map[string][]int),
		smsByEmail:        make(map[string]int),
		enabledStages:     make(map[string]bool),
		portfoliosByTitle: make(map[string]int),
		programmesByKey:   make(map[string]int),
		programmesByTitle: make(map[string][]int),
		projectsByExtRef:  make(map[string]int),
		projectsByKey:     make(map[string]int),
		projectsByTitle:   make(map[string][]int),
		intakesByTitle:    make(map[string]int),
		casesByProject:    make(map[int]int),
		benefitsByKey:     make(map[string]int),
		benefitTitles:     make(map[string][]int),
		benefitProject:    make(map[int]*int),
		realizationsKey:   make(map[string]int),
		tranchesByKey:     make(map[string]int),
		raidByKey:         make(map[string]int),
		stakeholdersByKey: make(map[string]int),
		resourcesByName:   make(map[string]int),
		allocationsByKey:  make(map[string]int),
		pfSeen:            make(map[string]bool),
		prgSeen:           make(map[string]bool),
		prjSeen:           make(map[string]bool),
		intakeSeen:        make(map[string]bool),
		benSeen:           make(map[string]bool),
		resSeen:           make(map[string]bool),
		prgSeenTitles:     make(map[string]bool),
		prjSeenTitles:     make(map[string]bool),
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

	smsRows, err := exec.Query(`SELECT id, lower(email) FROM sms_stakeholders WHERE email IS NOT NULL AND email != ''`)
	if err != nil {
		return nil, fmt.Errorf("load SMS register index for import: %w", err)
	}
	for smsRows.Next() {
		var id int
		var email string
		if err := smsRows.Scan(&id, &email); err == nil && email != "" {
			c.smsByEmail[email] = id
		}
	}
	smsRows.Close()

	stageRows, err := exec.Query(`SELECT stage_key FROM p3m_lifecycle_stages WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("load lifecycle stages for import: %w", err)
	}
	for stageRows.Next() {
		var key string
		if err := stageRows.Scan(&key); err == nil {
			c.enabledStages[key] = true
		}
	}
	stageRows.Close()

	loadIndex := func(query string, into map[string]int) error {
		r, err := exec.Query(query)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var key string
			var id int
			if err := r.Scan(&key, &id); err != nil {
				return err
			}
			into[key] = id
		}
		return nil
	}

	if err := loadIndex(`SELECT lower(title), id FROM p3m_portfolios`, c.portfoliosByTitle); err != nil {
		return nil, fmt.Errorf("load portfolio index: %w", err)
	}
	pgRows, err := exec.Query(`SELECT COALESCE(p.title,''), COALESCE(pg.title,''), pg.id FROM p3m_programmes pg JOIN p3m_portfolios p ON p.id = pg.portfolio_id`)
	if err != nil {
		return nil, fmt.Errorf("load programme index: %w", err)
	}
	for pgRows.Next() {
		var pfTitle, prgTitle string
		var id int
		if err := pgRows.Scan(&pfTitle, &prgTitle, &id); err == nil {
			c.programmesByKey[strings.ToLower(pfTitle)+"\x00"+strings.ToLower(prgTitle)] = id
			c.programmesByTitle[strings.ToLower(prgTitle)] = append(c.programmesByTitle[strings.ToLower(prgTitle)], id)
		}
	}
	pgRows.Close()
	if err := loadIndex(`SELECT lower(external_ref), id FROM p3m_projects WHERE external_ref IS NOT NULL AND external_ref != ''`, c.projectsByExtRef); err != nil {
		return nil, fmt.Errorf("load project ext-ref index: %w", err)
	}
	prjRows, err := exec.Query(`
		SELECT COALESCE(p.title,''), COALESCE(pg.title,''), prj.title, prj.id
		FROM p3m_projects prj
		LEFT JOIN p3m_portfolios p ON p.id = prj.portfolio_id
		LEFT JOIN p3m_programmes pg ON pg.id = prj.programme_id`)
	if err != nil {
		return nil, fmt.Errorf("load project index: %w", err)
	}
	for prjRows.Next() {
		var pfTitle, prgTitle, prjTitle string
		var id int
		if err := prjRows.Scan(&pfTitle, &prgTitle, &prjTitle, &id); err == nil {
			c.projectsByKey[strings.ToLower(pfTitle)+"\x00"+strings.ToLower(prgTitle)+"\x00"+strings.ToLower(prjTitle)] = id
		}
	}
	prjRows.Close()

	titleRows, err := exec.Query(`SELECT lower(title), id FROM p3m_projects`)
	if err != nil {
		return nil, fmt.Errorf("load project title index: %w", err)
	}
	for titleRows.Next() {
		var key string
		var id int
		if err := titleRows.Scan(&key, &id); err == nil {
			c.projectsByTitle[key] = append(c.projectsByTitle[key], id)
		}
	}
	titleRows.Close()

	if err := loadIndex(`SELECT lower(title), id FROM p3m_intakes`, c.intakesByTitle); err != nil {
		return nil, fmt.Errorf("load intake index: %w", err)
	}

	caseRows, err := exec.Query(`SELECT project_id, id FROM p3m_investment_cases`)
	if err != nil {
		return nil, fmt.Errorf("load investment case index: %w", err)
	}
	for caseRows.Next() {
		var pid, cid int
		if err := caseRows.Scan(&pid, &cid); err == nil {
			c.casesByProject[pid] = cid
		}
	}
	caseRows.Close()

	benRows, err := exec.Query(`
		SELECT CASE WHEN project_id IS NOT NULL THEN 'project' ELSE 'programme' END,
		       COALESCE(project_id, programme_id), lower(title), id, project_id
		FROM p3m_benefits`)
	if err != nil {
		return nil, fmt.Errorf("load benefit index: %w", err)
	}
	for benRows.Next() {
		var et string
		var eid int
		var title string
		var id int
		var projectID sql.NullInt64
		if err := benRows.Scan(&et, &eid, &title, &id, &projectID); err == nil {
			c.benefitsByKey[et+"\x00"+strconv.Itoa(eid)+"\x00"+title] = id
			c.benefitTitles[title] = append(c.benefitTitles[title], id)
			var pid *int
			if projectID.Valid {
				v := int(projectID.Int64)
				pid = &v
			}
			c.benefitProject[id] = pid
		}
	}
	benRows.Close()

	realRows, err := exec.Query(`SELECT benefit_id, COALESCE(CAST(recorded_date AS TEXT),''), id FROM p3m_benefit_realizations`)
	if err != nil {
		return nil, fmt.Errorf("load realization index: %w", err)
	}
	for realRows.Next() {
		var bid int
		var dateStr string
		var id int
		if err := realRows.Scan(&bid, &dateStr, &id); err == nil {
			c.realizationsKey[strconv.Itoa(bid)+"\x00"+dateStr] = id
		}
	}
	realRows.Close()

	loadEntityIndex := func(query string, into map[string]int) error {
		r, err := exec.Query(query)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var et string
			var eid int
			var title string
			var id int
			if err := r.Scan(&et, &eid, &title, &id); err == nil {
				into[et+"\x00"+strconv.Itoa(eid)+"\x00"+strings.ToLower(title)] = id
			}
		}
		return nil
	}
	if err := loadEntityIndex(`SELECT entity_type, entity_id, lower(title), id FROM p3m_budget_tranches`, c.tranchesByKey); err != nil {
		return nil, fmt.Errorf("load tranche index: %w", err)
	}
	if err := loadEntityIndex(`SELECT entity_type, entity_id, lower(title), id FROM p3m_raid_items`, c.raidByKey); err != nil {
		return nil, fmt.Errorf("load RAID index: %w", err)
	}
	if err := loadEntityIndex(`SELECT entity_type, entity_id, lower(name), id FROM p3m_stakeholders`, c.stakeholdersByKey); err != nil {
		return nil, fmt.Errorf("load stakeholder index: %w", err)
	}
	if err := loadIndex(`SELECT lower(name), id FROM p3m_resources`, c.resourcesByName); err != nil {
		return nil, fmt.Errorf("load resource index: %w", err)
	}

	allocRows, err := exec.Query(`SELECT resource_id, COALESCE(project_id, 0), COALESCE(CAST(start_date AS TEXT),''), id FROM p3m_resource_allocations`)
	if err != nil {
		return nil, fmt.Errorf("load allocation index: %w", err)
	}
	for allocRows.Next() {
		var rid, pid int
		var dateStr string
		var id int
		if err := allocRows.Scan(&rid, &pid, &dateStr, &id); err == nil {
			c.allocationsByKey[strconv.Itoa(rid)+"\x00"+strconv.Itoa(pid)+"\x00"+dateStr] = id
		}
	}
	allocRows.Close()

	for k := range c.portfoliosByTitle {
		c.pfSeen[k] = true
	}
	for k := range c.programmesByKey {
		c.prgSeen[k] = true
	}
	for k := range c.projectsByExtRef {
		c.prjSeen["ref\x00"+k] = true
	}
	for k := range c.projectsByKey {
		c.prjSeen[k] = true
	}
	for k := range c.intakesByTitle {
		c.intakeSeen[k] = true
	}
	for k := range c.benefitTitles {
		c.benSeen[k] = true
	}
	for k := range c.resourcesByName {
		c.resSeen[k] = true
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Field helpers (append to the active sheet report)
// ---------------------------------------------------------------------------

func (rep *importSheetReport) err(row int, field, msg string) {
	rep.Errors = append(rep.Errors, importRowError{Row: row, Field: field, Message: msg})
}

func (rep *importSheetReport) warn(row int, field, msg string) {
	rep.Warnings = append(rep.Warnings, importRowWarning{Row: row, Field: field, Message: msg})
}

// firstNonEmpty returns the value of the first non-empty accepted header.
func recField(rec map[string]string, headers ...string) string {
	for _, h := range headers {
		if v := strings.TrimSpace(rec[h]); v != "" {
			return v
		}
	}
	return ""
}

// Field helpers (append to the active sheet report). Package-level so every
// workbook importer (P3M, workspaces, SMS) shares one validation contract.

// importEnum validates an optional enum field; empty is allowed unless required.
func importEnum(rep *importSheetReport, row int, rec map[string]string, field string, allowed []string, required bool) string {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		if required {
			rep.err(row, field, fmt.Sprintf("%s is required", field))
		}
		return ""
	}
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			return a
		}
	}
	rep.err(row, field, fmt.Sprintf("invalid %s %q (allowed: %s)", field, value, strings.Join(allowed, ", ")))
	return ""
}

func importIntField(rep *importSheetReport, row int, rec map[string]string, field string, lo, hi int) int {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < lo || n > hi {
		rep.err(row, field, fmt.Sprintf("invalid integer %q (expected %d-%d)", value, lo, hi))
		return 0
	}
	return n
}

func importFloatField(rep *importSheetReport, row int, rec map[string]string, field string) float64 {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		rep.err(row, field, fmt.Sprintf("invalid number %q", value))
		return 0
	}
	return f
}

// importDateField validates YYYY-MM-DD and returns the normalized string.
func importDateField(rep *importSheetReport, row int, rec map[string]string, field string, required bool) string {
	value := strings.TrimSpace(rec[field])
	if value == "" {
		if required {
			rep.err(row, field, fmt.Sprintf("%s is required", field))
		}
		return ""
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		// Accept full timestamps too (export round-trips and values pasted
		// from other tools); store the date part only.
		t, err = time.Parse(time.RFC3339, value)
		if err != nil {
			rep.err(row, field, fmt.Sprintf("invalid date %q (expected YYYY-MM-DD)", value))
			return ""
		}
	}
	return t.Format("2006-01-02")
}

// personField resolves an optional person reference to a user id.
func (c *p3mImportContext) personField(rep *importSheetReport, row int, rec map[string]string, required bool, headers ...string) *int {
	value := recField(rec, headers...)
	if value == "" {
		if required {
			rep.err(row, headers[0], fmt.Sprintf("%s is required", headers[0]))
		}
		return nil
	}
	id, err := c.resolvePerson(value)
	if err != nil {
		rep.err(row, headers[0], err.Error())
		return nil
	}
	return id
}

// resolvePerson maps an email or "first last" display name to a user id.
func (c *p3mImportContext) resolvePerson(value string) (*int, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, nil
	}
	if id, ok := c.usersByEmail[strings.ToLower(v)]; ok {
		return &id, nil
	}
	ids := c.usersByName[strings.ToLower(v)]
	switch len(ids) {
	case 0:
		return nil, fmt.Errorf("no user with email or name %q", v)
	case 1:
		return &ids[0], nil
	default:
		return nil, fmt.Errorf("ambiguous person reference %q (matches %d users); use an email address", v, len(ids))
	}
}

// resolveEntity resolves a portfolio/programme/project reference given by the
// workbook's title columns. In preview it only checks resolvability and
// returns 0; in execute it returns the real id (entities must exist from an
// earlier sheet or the database).
func (c *p3mImportContext) resolveEntity(rep *importSheetReport, row int, entityType, pfTitle, prgTitle, projTitle, entityTitle string) (int, bool) {
	// Export-dialect fallback: rows carrying only entity_type + entity_title
	// (no parent titles) resolve by the entity's own title.
	if strings.TrimSpace(pfTitle) == "" && strings.TrimSpace(prgTitle) == "" && strings.TrimSpace(projTitle) == "" {
		if t := strings.TrimSpace(entityTitle); t != "" {
			return c.resolveEntityByTitle(rep, row, entityType, t)
		}
	}
	switch strings.ToLower(entityType) {
	case "portfolio":
		key := strings.ToLower(strings.TrimSpace(pfTitle))
		if id, ok := c.portfoliosByTitle[key]; ok {
			return id, true
		}
		if !c.pfSeen[key] {
			rep.err(row, "portfolio_title", fmt.Sprintf("unknown portfolio %q", pfTitle))
			return 0, false
		}
	case "programme":
		key := p3mImportKey(strings.ToLower(strings.TrimSpace(pfTitle)), strings.ToLower(strings.TrimSpace(prgTitle)))
		if id, ok := c.programmesByKey[key]; ok {
			return id, true
		}
		if !c.prgSeen[key] {
			rep.err(row, "programme_title", fmt.Sprintf("unknown programme %q in portfolio %q", prgTitle, pfTitle))
			return 0, false
		}
	case "project":
		pf := strings.ToLower(strings.TrimSpace(pfTitle))
		prg := strings.ToLower(strings.TrimSpace(prgTitle))
		prj := strings.ToLower(strings.TrimSpace(projTitle))
		if id, ok := c.projectsByKey[p3mImportKey(pf, prg, prj)]; ok {
			return id, true
		}
		if !c.prjSeen[p3mImportKey(pf, prg, prj)] {
			rep.err(row, "project_title", fmt.Sprintf("unknown project %q", projTitle))
			return 0, false
		}
	default:
		rep.err(row, "entity_type", "entity_type must be portfolio, programme or project")
		return 0, false
	}
	if !c.executing {
		return 0, true // preview: resolvable at execute time
	}
	switch strings.ToLower(entityType) {
	case "portfolio":
		return c.portfoliosByTitle[strings.ToLower(strings.TrimSpace(pfTitle))], true
	case "programme":
		return c.programmesByKey[p3mImportKey(strings.ToLower(strings.TrimSpace(pfTitle)), strings.ToLower(strings.TrimSpace(prgTitle)))], true
	default:
		return c.projectsByKey[p3mImportKey(strings.ToLower(strings.TrimSpace(pfTitle)), strings.ToLower(strings.TrimSpace(prgTitle)), strings.ToLower(strings.TrimSpace(projTitle)))], true
	}
}

// resolveEntityByTitle resolves an entity from its own title alone (the export
// dialect). Ambiguous titles are reported instead of guessed; entities created
// earlier in this payload resolve through the seen-title sets in preview and
// the live indexes in execute mode.
func (c *p3mImportContext) resolveEntityByTitle(rep *importSheetReport, row int, entityType, title string) (int, bool) {
	key := strings.ToLower(strings.TrimSpace(title))
	switch strings.ToLower(entityType) {
	case "portfolio":
		if id, ok := c.portfoliosByTitle[key]; ok {
			return id, true
		}
		if !c.pfSeen[key] {
			rep.err(row, "entity_title", fmt.Sprintf("unknown portfolio %q", title))
			return 0, false
		}
	case "programme":
		ids := c.programmesByTitle[key]
		if len(ids) == 1 {
			return ids[0], true
		}
		if len(ids) > 1 {
			rep.err(row, "entity_title", fmt.Sprintf("ambiguous programme title %q (%d matches); add portfolio_title to disambiguate", title, len(ids)))
			return 0, false
		}
		if !c.prgSeenTitles[key] {
			rep.err(row, "entity_title", fmt.Sprintf("unknown programme %q", title))
			return 0, false
		}
	case "project":
		ids := c.projectsByTitle[key]
		if len(ids) == 1 {
			return ids[0], true
		}
		if len(ids) > 1 {
			rep.err(row, "entity_title", fmt.Sprintf("ambiguous project title %q (%d matches); add portfolio_title/programme_title to disambiguate", title, len(ids)))
			return 0, false
		}
		if !c.prjSeenTitles[key] {
			rep.err(row, "entity_title", fmt.Sprintf("unknown project %q", title))
			return 0, false
		}
	default:
		rep.err(row, "entity_type", "entity_type must be portfolio, programme or project")
		return 0, false
	}
	if !c.executing {
		return 0, true // preview: created earlier in this payload
	}
	// Execute mode: creation paths update the live indexes above, so an empty
	// index here is unreachable; fail closed rather than hand out id 0.
	rep.err(row, "entity_title", fmt.Sprintf("internal: %s %q lost its resolved id", entityType, title))
	return 0, false
}

// importDecide returns the action for a row: "create", "update" or "skip". Rows
// with an explicit id (export dialect) always update; natural-key matches
// update in upsert mode and skip in create mode. Shared by every workbook
// importer.
func importDecide(mode string, existing bool, explicitID string) string {
	if explicitID != "" || existing && mode == "upsert" {
		return "update"
	}
	if existing {
		return "skip"
	}
	return "create"
}

// ---------------------------------------------------------------------------
// CSV record plumbing (shared by every sheet)
// ---------------------------------------------------------------------------

// p3mImportRecords parses one sheet's CSV text into per-row field maps. The
// header row is normalized (trimmed, lower-cased, trailing "*" stripped);
// unknown columns are ignored and blank rows skipped.
func importRecords(csvText string) ([]map[string]string, error) {
	if strings.TrimSpace(csvText) == "" {
		return nil, fmt.Errorf("CSV is empty")
	}
	r := csv.NewReader(strings.NewReader(csvText))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("CSV is empty")
	}

	headerIdx := make(map[string]int)
	for i, h := range records[0] {
		name := strings.ToLower(strings.TrimSpace(h))
		name = strings.TrimSuffix(name, "*")
		if name == "" {
			continue
		}
		if _, exists := headerIdx[name]; !exists {
			headerIdx[name] = i
		}
	}

	var out []map[string]string
	for lineNo, rec := range records[1:] {
		allEmpty := true
		recMap := make(map[string]string, len(headerIdx))
		for name, col := range headerIdx {
			v := ""
			if col < len(rec) {
				v = strings.TrimSpace(rec[col])
			}
			if v != "" {
				allEmpty = false
			}
			recMap[name] = v
		}
		if allEmpty {
			continue
		}
		recMap["\x00line"] = strconv.Itoa(lineNo + 1) // header is line 1
		out = append(out, recMap)
	}
	return out, nil
}

func recordLine(rec map[string]string) int {
	n, _ := strconv.Atoi(rec["\x00line"])
	return n
}

// normalizeSkills accepts a JSON array or a comma list and returns the JSON
// array form stored in p3m_resources.skills.
func p3mNormalizeSkills(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "[]"
	}
	if strings.HasPrefix(v, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			b, _ := json.Marshal(arr)
			return string(b)
		}
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// ---------------------------------------------------------------------------
// Sheet processors. Each returns a report; when ctx.executing is set the rows
// are written through ctx.exec (the import transaction). A row that produced
// any field error is not written.
// ---------------------------------------------------------------------------

func (h *P3MHandler) p3mImportPortfolios(c *p3mImportContext, csvText string) importSheetReport {
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

		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		budget := importFloatField(&rep, row, rec, "budget_allocated")
		rag := importEnum(&rep, row, rec, "rag_status", []string{"red", "amber", "green"}, false)
		if rag == "" {
			rag = "green"
		}
		ownerID := c.personField(&rep, row, rec, false, "owner", "owner_name")
		idStr := strings.TrimSpace(rec["id"])

		key := strings.ToLower(title)
		existingID, exists := c.portfoliosByTitle[key]
		if idStr != "" {
			n, err := strconv.Atoi(idStr)
			if err != nil || n <= 0 {
				rep.err(row, "id", fmt.Sprintf("invalid id %q", idStr))
				continue
			}
			existingID, exists = n, true // explicit export id always wins
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
				c.pfSeen[key] = true
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_portfolios (title, description, strategic_objective, budget_allocated, rag_status, owner_id)
				VALUES (?, ?, ?, ?, ?, ?)`,
				title, rec["description"], rec["strategic_objective"], budget, rag, nullInt(ownerID))
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.portfoliosByTitle[key] = int(id64)
			c.pfSeen[key] = true
			rep.Created++
		default: // update (read-modify-write; empty fields keep current values)
			var curDesc, curObj string
			var curBudget float64
			var curRag string
			var curOwner sql.NullInt64
			if err := c.exec.QueryRow(`
				SELECT description, strategic_objective, budget_allocated, rag_status, owner_id FROM p3m_portfolios WHERE id = ?`, existingID).
				Scan(&curDesc, &curObj, &curBudget, &curRag, &curOwner); err != nil {
				rep.err(row, "id", fmt.Sprintf("portfolio %d not found: %v", existingID, err))
				continue
			}
			if v := rec["description"]; v != "" {
				curDesc = v
			}
			if v := rec["strategic_objective"]; v != "" {
				curObj = v
			}
			if strings.TrimSpace(rec["budget_allocated"]) != "" {
				curBudget = budget
			}
			if ownerID != nil {
				curOwner = sql.NullInt64{Int64: int64(*ownerID), Valid: true}
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_portfolios SET description=?, strategic_objective=?, budget_allocated=?, rag_status=?, owner_id=? WHERE id=?`,
				curDesc, curObj, curBudget, rag, curOwner, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportProgrammes(c *p3mImportContext, csvText string) importSheetReport {
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

		pfTitle := strings.TrimSpace(rec["portfolio_title"])
		title := strings.TrimSpace(rec["title"])
		if pfTitle == "" || title == "" {
			if pfTitle == "" {
				rep.err(row, "portfolio_title", "portfolio_title is required")
			}
			if title == "" {
				rep.err(row, "title", "title is required")
			}
			continue
		}
		pfID, ok := c.resolveEntity(&rep, row, "portfolio", pfTitle, "", "", "")
		if !ok {
			continue
		}
		managerID := c.personField(&rep, row, rec, false, "manager", "manager_name")
		budget := importFloatField(&rep, row, rec, "budget_allocated")
		startDate := importDateField(&rep, row, rec, "target_start_date", false)
		endDate := importDateField(&rep, row, rec, "target_end_date", false)
		rag := importEnum(&rep, row, rec, "rag_status", []string{"red", "amber", "green"}, false)
		if rag == "" {
			rag = "green"
		}
		idStr := strings.TrimSpace(rec["id"])

		key := p3mImportKey(strings.ToLower(pfTitle), strings.ToLower(title))
		existingID, exists := c.programmesByKey[key]
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
				c.prgSeen[key] = true
				c.prgSeenTitles[strings.ToLower(title)] = true
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_programmes (portfolio_id, title, description, manager_id, budget_allocated, target_start_date, target_end_date, rag_status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				pfID, title, rec["description"], nullInt(managerID), budget, nullStr(startDate), nullStr(endDate), rag)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.programmesByKey[key] = int(id64)
			c.prgSeen[key] = true
			c.programmesByTitle[strings.ToLower(title)] = append(c.programmesByTitle[strings.ToLower(title)], int(id64))
			c.prgSeenTitles[strings.ToLower(title)] = true
			rep.Created++
		default: // update
			var curDesc string
			var curManager sql.NullInt64
			var curBudget float64
			var curStart, curEnd sql.NullString
			if err := c.exec.QueryRow(`
				SELECT description, manager_id, budget_allocated, target_start_date, target_end_date FROM p3m_programmes WHERE id = ?`, existingID).
				Scan(&curDesc, &curManager, &curBudget, &curStart, &curEnd); err != nil {
				rep.err(row, "id", fmt.Sprintf("programme %d not found: %v", existingID, err))
				continue
			}
			if v := rec["description"]; v != "" {
				curDesc = v
			}
			if managerID != nil {
				curManager = sql.NullInt64{Int64: int64(*managerID), Valid: true}
			}
			if strings.TrimSpace(rec["budget_allocated"]) != "" {
				curBudget = budget
			}
			if startDate != "" {
				curStart = nullStr(startDate)
			}
			if endDate != "" {
				curEnd = nullStr(endDate)
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_programmes SET description=?, manager_id=?, budget_allocated=?, target_start_date=?, target_end_date=?, rag_status=? WHERE id=?`,
				curDesc, curManager, curBudget, curStart, curEnd, rag, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportProjects(c *p3mImportContext, csvText string) importSheetReport {
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

		pfTitle := strings.TrimSpace(rec["portfolio_title"])
		prgTitle := strings.TrimSpace(rec["programme_title"])
		title := strings.TrimSpace(rec["title"])
		if pfTitle == "" || title == "" {
			if pfTitle == "" {
				rep.err(row, "portfolio_title", "portfolio_title is required")
			}
			if title == "" {
				rep.err(row, "title", "title is required")
			}
			continue
		}
		pfID, ok := c.resolveEntity(&rep, row, "portfolio", pfTitle, "", "", "")
		if !ok {
			continue
		}
		var prgID *int
		if prgTitle != "" {
			id, ok := c.resolveEntity(&rep, row, "programme", pfTitle, prgTitle, "", "")
			if !ok {
				continue
			}
			prgID = &id
		}
		managerID := c.personField(&rep, row, rec, false, "manager", "manager_name")
		extRef := strings.TrimSpace(rec["external_ref"])
		contingency := importFloatField(&rep, row, rec, "contingency")
		stage := strings.ToLower(strings.TrimSpace(rec["current_stage"]))
		if stage == "" {
			stage = "front_door"
		} else if !c.enabledStages[stage] {
			rep.err(row, "current_stage", fmt.Sprintf("unknown lifecycle stage %q (must be an enabled stage)", stage))
			continue
		}
		budgetApproved := importFloatField(&rep, row, rec, "budget_approved")
		budgetActual := importFloatField(&rep, row, rec, "budget_actual")
		progress := importIntField(&rep, row, rec, "progress_percent", 0, 100)
		startDate := importDateField(&rep, row, rec, "start_date", false)
		completionDate := importDateField(&rep, row, rec, "target_completion_date", false)
		rag := importEnum(&rep, row, rec, "rag_status", []string{"red", "amber", "green"}, false)
		if rag == "" {
			rag = "green"
		}
		wsKey := strings.TrimSpace(rec["workspace_key"])
		idStr := strings.TrimSpace(rec["id"])

		key := p3mImportKey(strings.ToLower(pfTitle), strings.ToLower(prgTitle), strings.ToLower(title))
		existingID, exists := c.projectsByKey[key]
		if extRef != "" {
			if id, ok := c.projectsByExtRef[strings.ToLower(extRef)]; ok && !exists {
				existingID = id // match by unique finance reference
			} else if id, ok := c.projectsByExtRef[strings.ToLower(extRef)]; ok && id != existingID {
				rep.err(row, "external_ref", fmt.Sprintf("external_ref %q already belongs to another project", extRef))
				continue
			}
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
				c.prjSeen[key] = true
				c.prjSeenTitles[strings.ToLower(title)] = true
				if extRef != "" {
					c.prjSeen["ref\x00"+strings.ToLower(extRef)] = true
				}
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_projects (portfolio_id, programme_id, title, description, manager_id, work_type, external_ref, funding_source, contingency, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date, rag_status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				pfID, nullInt(prgID), title, rec["description"], nullInt(managerID), nullStr(rec["work_type"]), nullStr(extRef), nullStr(rec["funding_source"]), contingency, stage, budgetApproved, budgetActual, progress, nullStr(startDate), nullStr(completionDate), rag)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			pid := int(id64)
			c.projectsByKey[key] = pid
			c.prjSeen[key] = true
			if extRef != "" {
				c.projectsByExtRef[strings.ToLower(extRef)] = pid
				c.prjSeen["ref\x00"+strings.ToLower(extRef)] = true
			}
			c.projectsByTitle[strings.ToLower(title)] = append(c.projectsByTitle[strings.ToLower(title)], pid)
			c.prjSeenTitles[strings.ToLower(title)] = true
			if wsKey != "" {
				h.p3mLinkProjectWorkspace(c, &rep, row, pid, wsKey)
			}
			rep.Created++
		default: // update
			var curDesc string
			var curManager sql.NullInt64
			var curWorkType, curExtRef, curFunding sql.NullString
			var curContingency float64
			var curStage string
			var curApproved, curActual float64
			var curProgress int
			var curStart, curEnd sql.NullString
			if err := c.exec.QueryRow(`
				SELECT description, manager_id, work_type, external_ref, funding_source, contingency, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date FROM p3m_projects WHERE id = ?`, existingID).
				Scan(&curDesc, &curManager, &curWorkType, &curExtRef, &curFunding, &curContingency, &curStage, &curApproved, &curActual, &curProgress, &curStart, &curEnd); err != nil {
				rep.err(row, "id", fmt.Sprintf("project %d not found: %v", existingID, err))
				continue
			}
			if v := rec["description"]; v != "" {
				curDesc = v
			}
			if managerID != nil {
				curManager = sql.NullInt64{Int64: int64(*managerID), Valid: true}
			}
			if v := strings.TrimSpace(rec["work_type"]); v != "" {
				curWorkType = nullStr(v)
			}
			if extRef != "" {
				curExtRef = nullStr(extRef)
			}
			if v := strings.TrimSpace(rec["funding_source"]); v != "" {
				curFunding = nullStr(v)
			}
			if strings.TrimSpace(rec["contingency"]) != "" {
				curContingency = contingency
			}
			if strings.TrimSpace(rec["budget_approved"]) != "" {
				curApproved = budgetApproved
			}
			if strings.TrimSpace(rec["budget_actual"]) != "" {
				curActual = budgetActual
			}
			if strings.TrimSpace(rec["progress_percent"]) != "" {
				curProgress = progress
			}
			if startDate != "" {
				curStart = nullStr(startDate)
			}
			if completionDate != "" {
				curEnd = nullStr(completionDate)
			}
			if !c.executing {
				rep.ToUpdate++
				if extRef != "" {
					c.projectsByExtRef[strings.ToLower(extRef)] = existingID
				}
				if wsKey != "" {
					h.p3mLinkProjectWorkspace(c, &rep, row, existingID, wsKey) // preview-safe: warns only
				}
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_projects SET description=?, manager_id=?, work_type=?, external_ref=?, funding_source=?, contingency=?, current_stage=?, budget_approved=?, budget_actual=?, progress_percent=?, start_date=?, target_completion_date=?, rag_status=? WHERE id=?`,
				curDesc, curManager, curWorkType, curExtRef, curFunding, curContingency, stage, curApproved, curActual, curProgress, curStart, curEnd, rag, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			if extRef != "" {
				c.projectsByExtRef[strings.ToLower(extRef)] = existingID
			}
			if wsKey != "" {
				h.p3mLinkProjectWorkspace(c, &rep, row, existingID, wsKey)
			}
			rep.Updated++
		}
	}
	return rep
}

// p3mLinkProjectWorkspace links a P3M project to its delivery workspace by key
// (workbook "workspace_key" column). Unknown keys produce a warning, not an
// error: the project still imports and can be linked later.
func (h *P3MHandler) p3mLinkProjectWorkspace(c *p3mImportContext, rep *importSheetReport, row, projectID int, wsKey string) {
	var wsID int
	err := c.exec.QueryRow(`SELECT id FROM workspaces WHERE lower(key) = lower(?)`, wsKey).Scan(&wsID)
	if err != nil {
		rep.warn(row, "workspace_key", fmt.Sprintf("no workspace with key %q; project imported without the link", wsKey))
		return
	}
	if !c.executing {
		return // preview: report only, no link write
	}
	if _, err := c.exec.Exec(`INSERT OR IGNORE INTO workspace_p3m_projects (workspace_id, p3m_project_id) VALUES (?, ?)`, wsID, projectID); err != nil {
		rep.warn(row, "workspace_key", fmt.Sprintf("could not link workspace %q: %v", wsKey, err))
	}
}

func (h *P3MHandler) p3mImportIntakes(c *p3mImportContext, csvText string) importSheetReport {
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

		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}
		source := importEnum(&rep, row, rec, "source", []string{"standard", "exceptional_path"}, false)
		if source == "" {
			source = "standard"
		}
		var costPtr *float64
		if strings.TrimSpace(rec["indicative_cost"]) != "" {
			v := importFloatField(&rep, row, rec, "indicative_cost")
			costPtr = &v
		}
		workType := strings.TrimSpace(rec["work_type"])
		complexity := importEnum(&rep, row, rec, "complexity", []string{"low", "med", "high"}, false)
		if complexity == "" {
			complexity = "med"
		}
		status := importEnum(&rep, row, rec, "status", []string{"captured", "assessing", "decided"}, false)
		if status == "" {
			status = "captured"
		}
		decision := importEnum(&rep, row, rec, "decision", []string{"proceed", "redirect", "defer", "decline", "return_info"}, false)
		idStr := strings.TrimSpace(rec["id"])

		key := strings.ToLower(title)
		existingID, exists := c.intakesByTitle[key]
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
				c.intakeSeen[key] = true
				continue
			}
			now := time.Now().UTC()
			res, err := c.exec.Exec(`
				INSERT INTO p3m_intakes (title, source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, decision, conditions, rationale, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				title, source, rec["description"], rec["problem_opportunity"], rec["strategic_alignment"], rec["intended_outcomes"], nullFloat(costPtr), rec["resource_requirements"], rec["risk_notes"], rec["change_impact_notes"], rec["business_group"], nullStr(workType), complexity, status, nullStr(decision), rec["conditions"], rec["rationale"], c.actorID, now, now)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.intakesByTitle[key] = int(id64)
			c.intakeSeen[key] = true
			rep.Created++
		default: // update
			var curSource string
			var curDesc, curProblem, curAlign, curOutcomes string
			var curCost sql.NullFloat64
			var curReq, curRisk, curChange, curGroup string
			var curWorkType sql.NullString
			var curComplexity, curStatus string
			var curDecision sql.NullString
			if err := c.exec.QueryRow(`
				SELECT source, description, problem_opportunity, strategic_alignment, intended_outcomes, indicative_cost, resource_requirements, risk_notes, change_impact_notes, business_group, work_type, complexity, status, decision FROM p3m_intakes WHERE id = ?`, existingID).
				Scan(&curSource, &curDesc, &curProblem, &curAlign, &curOutcomes, &curCost, &curReq, &curRisk, &curChange, &curGroup, &curWorkType, &curComplexity, &curStatus, &curDecision); err != nil {
				rep.err(row, "id", fmt.Sprintf("intake %d not found: %v", existingID, err))
				continue
			}
			if v := rec["description"]; v != "" {
				curDesc = v
			}
			if v := rec["problem_opportunity"]; v != "" {
				curProblem = v
			}
			if v := rec["strategic_alignment"]; v != "" {
				curAlign = v
			}
			if v := rec["intended_outcomes"]; v != "" {
				curOutcomes = v
			}
			if costPtr != nil {
				curCost = sql.NullFloat64{Float64: *costPtr, Valid: true}
			}
			if v := rec["resource_requirements"]; v != "" {
				curReq = v
			}
			if v := rec["risk_notes"]; v != "" {
				curRisk = v
			}
			if v := rec["change_impact_notes"]; v != "" {
				curChange = v
			}
			if v := rec["business_group"]; v != "" {
				curGroup = v
			}
			if workType != "" {
				curWorkType = nullStr(workType)
			}
			if decision != "" {
				curDecision = nullStr(decision)
			}
			now := time.Now().UTC()
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_intakes SET source=?, description=?, problem_opportunity=?, strategic_alignment=?, intended_outcomes=?, indicative_cost=?, resource_requirements=?, risk_notes=?, change_impact_notes=?, business_group=?, work_type=?, complexity=?, status=?, decision=?, conditions=?, rationale=?, updated_at=? WHERE id=?`,
				source, curDesc, curProblem, curAlign, curOutcomes, curCost, curReq, curRisk, curChange, curGroup, curWorkType, complexity, status, curDecision, rec["conditions"], rec["rationale"], now, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportInvestmentCases(c *p3mImportContext, csvText string) importSheetReport {
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

		pfTitle := strings.TrimSpace(rec["portfolio_title"])
		prgTitle := strings.TrimSpace(rec["programme_title"])
		projTitle := strings.TrimSpace(rec["project_title"])
		if projTitle == "" {
			rep.err(row, "project_title", "project_title is required")
			continue
		}
		var projectID int
		switch {
		case pfTitle != "" && prgTitle != "":
			id, ok := c.resolveEntity(&rep, row, "project", pfTitle, prgTitle, projTitle, "")
			if !ok {
				continue
			}
			projectID = id
		case len(c.projectsByTitle[strings.ToLower(projTitle)]) == 1:
			projectID = c.projectsByTitle[strings.ToLower(projTitle)][0]
		default:
			rep.err(row, "project_title", fmt.Sprintf("unknown or ambiguous project %q; add portfolio_title and programme_title to disambiguate", projTitle))
			continue
		}

		status := importEnum(&rep, row, rec, "status", []string{"proposed", "under_review", "approved", "rejected", "deferred"}, false)
		if status == "" {
			status = "proposed"
		}
		benefitScore := importIntField(&rep, row, rec, "benefit_score", 1, 5)
		if benefitScore == 0 {
			benefitScore = 3
		}
		riskScore := importIntField(&rep, row, rec, "risk_score", 1, 5)
		if riskScore == 0 {
			riskScore = 3
		}
		var roi *float64
		if strings.TrimSpace(rec["roi_percent"]) != "" {
			v := importFloatField(&rep, row, rec, "roi_percent")
			roi = &v
		}
		var payback *int
		if strings.TrimSpace(rec["payback_months"]) != "" {
			n := importIntField(&rep, row, rec, "payback_months", 0, 10000)
			payback = &n
		}
		decidedBy := c.personField(&rep, row, rec, false, "decided_by")
		decisionDate := importDateField(&rep, row, rec, "decision_date", false)
		if len(rep.Errors) > errCount {
			continue
		}

		existingID, exists := c.casesByProject[projectID]
		switch {
		case !exists:
			if !c.executing {
				rep.ToCreate++
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_investment_cases (project_id, status, benefit_score, risk_score, roi_percent, payback_months, business_case, decided_by, decision_date)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				projectID, status, benefitScore, riskScore, nullFloat(roi), nullInt(payback), rec["business_case"], nullInt(decidedBy), nullStr(decisionDate))
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.casesByProject[projectID] = int(id64)
			rep.Created++
		default: // one case per project (UNIQUE): always update
			var curStatus string
			var curBenefit, curRisk int
			var curRoi sql.NullFloat64
			var curPayback sql.NullInt64
			var curCaseText string
			var curDecidedBy sql.NullInt64
			var curDecisionDate sql.NullString
			if err := c.exec.QueryRow(`
				SELECT status, benefit_score, risk_score, roi_percent, payback_months, business_case, decided_by, decision_date FROM p3m_investment_cases WHERE id = ?`, existingID).
				Scan(&curStatus, &curBenefit, &curRisk, &curRoi, &curPayback, &curCaseText, &curDecidedBy, &curDecisionDate); err != nil {
				rep.err(row, "", fmt.Sprintf("investment case %d not found: %v", existingID, err))
				continue
			}
			if v := strings.TrimSpace(rec["business_case"]); v != "" {
				curCaseText = v
			}
			if roi != nil {
				curRoi = sql.NullFloat64{Float64: *roi, Valid: true}
			}
			if payback != nil {
				curPayback = sql.NullInt64{Int64: int64(*payback), Valid: true}
			}
			if decidedBy != nil {
				curDecidedBy = sql.NullInt64{Int64: int64(*decidedBy), Valid: true}
			}
			if decisionDate != "" {
				curDecisionDate = nullStr(decisionDate)
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_investment_cases SET status=?, benefit_score=?, risk_score=?, roi_percent=?, payback_months=?, business_case=?, decided_by=?, decision_date=? WHERE id=?`,
				status, benefitScore, riskScore, curRoi, curPayback, curCaseText, curDecidedBy, curDecisionDate, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportBenefits(c *p3mImportContext, csvText string) importSheetReport {
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

		scopeType := strings.ToLower(recField(rec, "scope_type", "entity_type"))
		title := strings.TrimSpace(rec["title"])
		if scopeType == "" || title == "" {
			if scopeType == "" {
				rep.err(row, "scope_type", "scope_type is required (project or programme)")
			}
			if title == "" {
				rep.err(row, "title", "title is required")
			}
			continue
		}
		var entityID int
		switch scopeType {
		case "project":
			id, ok := c.resolveEntity(&rep, row, "project", rec["portfolio_title"], rec["programme_title"], rec["project_title"], rec["entity_title"])
			if !ok {
				continue
			}
			entityID = id
		case "programme":
			id, ok := c.resolveEntity(&rep, row, "programme", rec["portfolio_title"], rec["programme_title"], "", rec["entity_title"])
			if !ok {
				continue
			}
			entityID = id
		default:
			rep.err(row, "scope_type", fmt.Sprintf("invalid scope_type %q (allowed: project, programme)", scopeType))
			continue
		}

		benefitType := importEnum(&rep, row, rec, "benefit_type", []string{"financial", "non_financial", "strategic", "compliance"}, false)
		if benefitType == "" {
			benefitType = "financial"
		}
		targetValue := importFloatField(&rep, row, rec, "target_value")
		baselineValue := importFloatField(&rep, row, rec, "baseline_value")
		realizationDate := importDateField(&rep, row, rec, "target_realization_date", true)
		ownerID := c.personField(&rep, row, rec, true, "owner", "owner_name")
		frequency := importEnum(&rep, row, rec, "measurement_frequency", []string{"monthly", "quarterly", "annual", "one_off"}, false)
		status := importEnum(&rep, row, rec, "status", []string{"planned", "measuring", "realised", "shortfall", "retired"}, false)
		if status == "" {
			status = "planned"
		}
		validatorID := c.personField(&rep, row, rec, false, "finance_validator")
		validationStatus := importEnum(&rep, row, rec, "validation_status", []string{"not_required", "pending", "validated"}, false)
		if validationStatus == "" {
			validationStatus = "not_required"
		}
		idStr := strings.TrimSpace(rec["id"])

		key := scopeType + "\x00" + strconv.Itoa(entityID) + "\x00" + strings.ToLower(title)
		existingID, exists := c.benefitsByKey[key]
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
				c.benSeen[strings.ToLower(title)] = true
				continue
			}
			var projectID, programmeID *int
			if scopeType == "project" {
				projectID = &entityID
			} else {
				programmeID = &entityID
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_benefits (project_id, programme_id, title, benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, measurement_method, measurement_frequency, status, evidence, finance_validator_id, validation_status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nullInt(projectID), nullInt(programmeID), title, benefitType, targetValue, rec["unit_of_measure"], baselineValue, realizationDate, ownerID, nullStr(rec["measurement_method"]), nullStr(frequency), status, nullStr(rec["evidence"]), nullInt(validatorID), validationStatus)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			bid := int(id64)
			c.benefitsByKey[key] = bid
			c.benefitTitles[strings.ToLower(title)] = append(c.benefitTitles[strings.ToLower(title)], bid)
			var pid *int
			if scopeType == "project" {
				pid = &entityID
			}
			c.benefitProject[bid] = pid
			c.benSeen[strings.ToLower(title)] = true
			rep.Created++
		default: // update
			var curType string
			var curTarget, curBaseline float64
			var curUnit string
			var curDate string
			var curOwner int
			var curMethod, curFreq sql.NullString
			var curStatus string
			var curEvidence sql.NullString
			var curValidator sql.NullInt64
			var curValidation string
			if err := c.exec.QueryRow(`
				SELECT benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, measurement_method, measurement_frequency, status, evidence, finance_validator_id, validation_status FROM p3m_benefits WHERE id = ?`, existingID).
				Scan(&curType, &curTarget, &curUnit, &curBaseline, &curDate, &curOwner, &curMethod, &curFreq, &curStatus, &curEvidence, &curValidator, &curValidation); err != nil {
				rep.err(row, "id", fmt.Sprintf("benefit %d not found: %v", existingID, err))
				continue
			}
			if v := strings.TrimSpace(rec["unit_of_measure"]); v != "" {
				curUnit = v
			}
			if strings.TrimSpace(rec["target_value"]) != "" {
				curTarget = targetValue
			}
			if strings.TrimSpace(rec["baseline_value"]) != "" {
				curBaseline = baselineValue
			}
			if realizationDate != "" {
				curDate = realizationDate
			}
			if ownerID != nil {
				curOwner = *ownerID
			}
			if v := strings.TrimSpace(rec["measurement_method"]); v != "" {
				curMethod = nullStr(v)
			}
			if frequency != "" {
				curFreq = nullStr(frequency)
			}
			if v := strings.TrimSpace(rec["evidence"]); v != "" {
				curEvidence = nullStr(v)
			}
			if validatorID != nil {
				curValidator = sql.NullInt64{Int64: int64(*validatorID), Valid: true}
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_benefits SET benefit_type=?, target_value=?, unit_of_measure=?, baseline_value=?, target_realization_date=?, owner_id=?, measurement_method=?, measurement_frequency=?, status=?, evidence=?, finance_validator_id=?, validation_status=? WHERE id=?`,
				benefitType, curTarget, curUnit, curBaseline, curDate, curOwner, curMethod, curFreq, status, curEvidence, curValidator, validationStatus, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportRealizations(c *p3mImportContext, csvText string) importSheetReport {
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

		benefitTitle := strings.TrimSpace(rec["benefit_title"])
		if benefitTitle == "" {
			rep.err(row, "benefit_title", "benefit_title is required")
			continue
		}
		recordedDate := importDateField(&rep, row, rec, "recorded_date", true)
		realizedValue := importFloatField(&rep, row, rec, "realized_value")
		recordedBy := c.personField(&rep, row, rec, true, "recorded_by")

		bKey := strings.ToLower(benefitTitle)
		candidates := c.benefitTitles[bKey]
		if len(candidates) == 0 {
			if !c.benSeen[bKey] || c.executing {
				rep.err(row, "benefit_title", fmt.Sprintf("unknown benefit %q (import the Benefits sheet first)", benefitTitle))
				continue
			}
		} else if len(candidates) > 1 && strings.TrimSpace(rec["project_title"]) != "" {
			// Narrow to the candidate scoped to this project.
			var narrowed []int
			for _, id := range candidates {
				if pid, ok := c.benefitProject[id]; ok && pid != nil {
					for _, prjID := range c.projectsByTitle[strings.ToLower(strings.TrimSpace(rec["project_title"]))] {
						if *pid == prjID {
							narrowed = append(narrowed, id)
						}
					}
				}
			}
			candidates = narrowed
		}
		var benefitID int
		switch len(candidates) {
		case 0:
			if !c.executing {
				benefitID = 0 // preview only; existence already checked above
			} else {
				continue // error recorded above
			}
		case 1:
			benefitID = candidates[0]
		default:
			rep.err(row, "benefit_title", fmt.Sprintf("ambiguous benefit title %q (%d matches); add project_title to disambiguate", benefitTitle, len(candidates)))
			continue
		}
		if len(rep.Errors) > errCount {
			continue
		}

		key := strconv.Itoa(benefitID) + "\x00" + recordedDate
		existingID, exists := c.realizationsKey[key]
		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_benefit_realizations (benefit_id, recorded_date, realized_value, notes, recorded_by)
				VALUES (?, ?, ?, ?, ?)`, benefitID, recordedDate, realizedValue, rec["notes"], recordedBy)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.realizationsKey[key] = int(id64)
			rep.Created++
		default: // update
			var curValue float64
			var curNotes string
			if err := c.exec.QueryRow(`SELECT realized_value, notes FROM p3m_benefit_realizations WHERE id = ?`, existingID).Scan(&curValue, &curNotes); err != nil {
				rep.err(row, "", fmt.Sprintf("realization %d not found: %v", existingID, err))
				continue
			}
			if strings.TrimSpace(rec["realized_value"]) != "" {
				curValue = realizedValue
			}
			if v := rec["notes"]; v != "" {
				curNotes = v
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`UPDATE p3m_benefit_realizations SET realized_value=?, notes=? WHERE id=?`, curValue, curNotes, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportTranches(c *p3mImportContext, csvText string) importSheetReport {
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

		entityType := strings.ToLower(recField(rec, "entity_type"))
		title := strings.TrimSpace(rec["title"])
		if entityType == "" || title == "" {
			if entityType == "" {
				rep.err(row, "entity_type", "entity_type is required (portfolio, programme or project)")
			}
			if title == "" {
				rep.err(row, "title", "title is required")
			}
			continue
		}
		entityID, ok := c.resolveEntity(&rep, row, entityType, rec["portfolio_title"], rec["programme_title"], rec["project_title"], rec["entity_title"])
		if !ok {
			continue
		}
		amountStr := strings.TrimSpace(rec["amount"])
		if amountStr == "" {
			rep.err(row, "amount", "amount is required")
		}
		amount := importFloatField(&rep, row, rec, "amount")
		expType := importEnum(&rep, row, rec, "expenditure_type", []string{"capex", "opex"}, false)
		if expType == "" {
			expType = "opex"
		}
		expClass := importEnum(&rep, row, rec, "expenditure_class", []string{"one_off", "ongoing"}, false)
		plannedDate := importDateField(&rep, row, rec, "planned_date", true)
		status := importEnum(&rep, row, rec, "status", []string{"proposed", "approved", "released"}, false)
		if status == "" {
			status = "approved"
		}
		idStr := strings.TrimSpace(rec["id"])

		key := entityType + "\x00" + strconv.Itoa(entityID) + "\x00" + strings.ToLower(title)
		existingID, exists := c.tranchesByKey[key]
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
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_budget_tranches (entity_type, entity_id, title, amount, expenditure_type, expenditure_class, planned_date, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				entityType, entityID, title, amount, expType, nullStr(expClass), plannedDate, status)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.tranchesByKey[key] = int(id64)
			rep.Created++
		default: // update
			var curAmount float64
			var curType string
			var curClass sql.NullString
			var curDate string
			if err := c.exec.QueryRow(`SELECT amount, expenditure_type, expenditure_class, planned_date FROM p3m_budget_tranches WHERE id = ?`, existingID).Scan(&curAmount, &curType, &curClass, &curDate); err != nil {
				rep.err(row, "", fmt.Sprintf("tranche %d not found: %v", existingID, err))
				continue
			}
			if amountStr != "" {
				curAmount = amount
			}
			if expClass != "" {
				curClass = nullStr(expClass)
			}
			if plannedDate != "" {
				curDate = plannedDate
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_budget_tranches SET amount=?, expenditure_type=?, expenditure_class=?, planned_date=?, status=? WHERE id=?`,
				curAmount, expType, curClass, curDate, status, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportRAID(c *p3mImportContext, csvText string) importSheetReport {
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

		entityType := strings.ToLower(recField(rec, "entity_type"))
		itemType := importEnum(&rep, row, rec, "item_type", []string{"risk", "assumption", "issue", "dependency", "decision"}, true)
		title := strings.TrimSpace(rec["title"])
		if entityType == "" || title == "" {
			if entityType == "" {
				rep.err(row, "entity_type", "entity_type is required (portfolio, programme or project)")
			}
			if title == "" {
				rep.err(row, "title", "title is required")
			}
			continue
		}
		entityID, ok := c.resolveEntity(&rep, row, entityType, rec["portfolio_title"], rec["programme_title"], rec["project_title"], rec["entity_title"])
		if !ok {
			continue
		}
		impact := importIntField(&rep, row, rec, "impact_score", 1, 5)
		if impact == 0 {
			impact = 1
		}
		probability := importIntField(&rep, row, rec, "probability_score", 1, 5)
		if probability == 0 {
			probability = 1
		}
		ownerID := c.personField(&rep, row, rec, true, "owner", "owner_name")
		status := importEnum(&rep, row, rec, "status", []string{"open", "mitigating", "closed", "escalated"}, false)
		if status == "" {
			status = "open"
		}
		dueDate := importDateField(&rep, row, rec, "due_date", false)
		itemSubtype := importEnum(&rep, row, rec, "item_subtype", []string{"change_risk", "adoption_risk"}, false)
		decisionSubtype := importEnum(&rep, row, rec, "decision_subtype", []string{"governance", "funding", "procurement", "plan", "closure"}, false)
		idStr := strings.TrimSpace(rec["id"])

		key := entityType + "\x00" + strconv.Itoa(entityID) + "\x00" + strings.ToLower(title)
		existingID, exists := c.raidByKey[key]
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
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, due_date, item_subtype, decision_subtype)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				entityType, entityID, itemType, title, rec["description"], impact, probability, rec["mitigation_plan"], ownerID, status, nullStr(dueDate), nullStr(itemSubtype), nullStr(decisionSubtype))
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.raidByKey[key] = int(id64)
			rep.Created++
		default: // update
			var curDesc string
			var curImpact, curProb int
			var curMitigation string
			var curOwner int
			var curDue sql.NullString
			var curItemSub, curDecisionSub sql.NullString
			if err := c.exec.QueryRow(`
				SELECT description, impact_score, probability_score, mitigation_plan, owner_id, due_date, item_subtype, decision_subtype FROM p3m_raid_items WHERE id = ?`, existingID).
				Scan(&curDesc, &curImpact, &curProb, &curMitigation, &curOwner, &curDue, &curItemSub, &curDecisionSub); err != nil {
				rep.err(row, "", fmt.Sprintf("RAID item %d not found: %v", existingID, err))
				continue
			}
			if v := rec["description"]; v != "" {
				curDesc = v
			}
			if strings.TrimSpace(rec["impact_score"]) != "" {
				curImpact = impact
			}
			if strings.TrimSpace(rec["probability_score"]) != "" {
				curProb = probability
			}
			if v := rec["mitigation_plan"]; v != "" {
				curMitigation = v
			}
			if ownerID != nil {
				curOwner = *ownerID
			}
			if dueDate != "" {
				curDue = nullStr(dueDate)
			}
			if itemSubtype != "" {
				curItemSub = nullStr(itemSubtype)
			}
			if decisionSubtype != "" {
				curDecisionSub = nullStr(decisionSubtype)
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_raid_items SET item_type=?, description=?, impact_score=?, probability_score=?, mitigation_plan=?, owner_id=?, status=?, due_date=?, item_subtype=?, decision_subtype=? WHERE id=?`,
				itemType, curDesc, curImpact, curProb, curMitigation, curOwner, status, curDue, curItemSub, curDecisionSub, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportStakeholders(c *p3mImportContext, csvText string) importSheetReport {
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

		entityType := strings.ToLower(recField(rec, "entity_type"))
		name := strings.TrimSpace(rec["name"])
		if entityType == "" || name == "" {
			if entityType == "" {
				rep.err(row, "entity_type", "entity_type is required (portfolio, programme or project)")
			}
			if name == "" {
				rep.err(row, "name", "name is required")
			}
			continue
		}
		entityID, ok := c.resolveEntity(&rep, row, entityType, rec["portfolio_title"], rec["programme_title"], rec["project_title"], rec["entity_title"])
		if !ok {
			continue
		}
		power := importIntField(&rep, row, rec, "power_score", 1, 5)
		if power == 0 {
			power = 3
		}
		interest := importIntField(&rep, row, rec, "interest_score", 1, 5)
		if interest == 0 {
			interest = 3
		}
		var smsID *int
		if email := strings.TrimSpace(rec["sms_stakeholder_email"]); email != "" {
			if id, found := c.smsByEmail[strings.ToLower(email)]; found {
				smsID = &id
			} else {
				rep.warn(row, "sms_stakeholder_email", fmt.Sprintf("no SMS register stakeholder with email %q; imported without the link", email))
			}
		}
		idStr := strings.TrimSpace(rec["id"])

		key := entityType + "\x00" + strconv.Itoa(entityID) + "\x00" + strings.ToLower(name)
		existingID, exists := c.stakeholdersByKey[key]
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
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_stakeholders (entity_type, entity_id, name, role_title, power_score, interest_score, sms_stakeholder_id, notes)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				entityType, entityID, name, rec["role_title"], power, interest, nullInt(smsID), rec["notes"])
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.stakeholdersByKey[key] = int(id64)
			rep.Created++
		default: // update
			var curRole string
			var curPower, curInterest int
			var curSMS sql.NullInt64
			var curNotes string
			if err := c.exec.QueryRow(`SELECT role_title, power_score, interest_score, sms_stakeholder_id, notes FROM p3m_stakeholders WHERE id = ?`, existingID).Scan(&curRole, &curPower, &curInterest, &curSMS, &curNotes); err != nil {
				rep.err(row, "", fmt.Sprintf("stakeholder %d not found: %v", existingID, err))
				continue
			}
			if v := rec["role_title"]; v != "" {
				curRole = v
			}
			if strings.TrimSpace(rec["power_score"]) != "" {
				curPower = power
			}
			if strings.TrimSpace(rec["interest_score"]) != "" {
				curInterest = interest
			}
			if smsID != nil {
				curSMS = sql.NullInt64{Int64: int64(*smsID), Valid: true}
			}
			if v := rec["notes"]; v != "" {
				curNotes = v
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_stakeholders SET role_title=?, power_score=?, interest_score=?, sms_stakeholder_id=?, notes=? WHERE id=?`,
				curRole, curPower, curInterest, curSMS, curNotes, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// p3mImportCommunications is append-only in both modes: the log has no natural
// key, so re-importing an export would duplicate rows - callers should not
// include this sheet when round-tripping.
func (h *P3MHandler) p3mImportCommunications(c *p3mImportContext, csvText string) importSheetReport {
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

		entityType := strings.ToLower(recField(rec, "entity_type"))
		dateStr := importDateField(&rep, row, rec, "date", true)
		audience := strings.TrimSpace(rec["audience"])
		summary := strings.TrimSpace(rec["summary"])
		if entityType == "" {
			rep.err(row, "entity_type", "entity_type is required (portfolio, programme or project)")
		}
		if audience == "" {
			rep.err(row, "audience", "audience is required")
		}
		if summary == "" {
			rep.err(row, "summary", "summary is required")
		}
		var entityID int
		if entityType != "" {
			id, ok := c.resolveEntity(&rep, row, entityType, rec["portfolio_title"], rec["programme_title"], rec["project_title"], rec["entity_title"])
			if !ok {
				continue
			}
			entityID = id
		}
		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing {
			rep.ToCreate++
			continue
		}
		res, err := c.exec.Exec(`
			INSERT INTO p3m_communications (entity_type, entity_id, date, audience, channel, summary, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			entityType, entityID, dateStr, audience, rec["channel"], summary, c.actorID)
		if err != nil {
			rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
			continue
		}
		id64, _ := res.LastInsertId()
		_ = id64
		rep.Created++
	}
	return rep
}

func (h *P3MHandler) p3mImportResources(c *p3mImportContext, csvText string) importSheetReport {
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
		userID := c.personField(&rep, row, rec, false, "user", "linked_user")
		skillsRaw := strings.TrimSpace(rec["skills"])
		costRate := importFloatField(&rep, row, rec, "cost_rate")
		capacity := importIntField(&rep, row, rec, "capacity_hours_per_week", 0, 24*7)
		if capacity == 0 {
			capacity = 40
		}
		pool := strings.TrimSpace(rec["pool"])
		idStr := strings.TrimSpace(rec["id"])

		key := strings.ToLower(name)
		existingID, exists := c.resourcesByName[key]
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
				c.resSeen[key] = true
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_resources (name, user_id, skills, cost_rate, capacity_hours_per_week, pool)
				VALUES (?, ?, ?, ?, ?, ?)`,
				name, nullInt(userID), p3mNormalizeSkills(skillsRaw), costRate, capacity, nullStr(pool))
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.resourcesByName[key] = int(id64)
			c.resSeen[key] = true
			rep.Created++
		default: // update
			var curUser sql.NullInt64
			var curSkills string
			var curCost float64
			var curCapacity int
			var curPool sql.NullString
			if err := c.exec.QueryRow(`SELECT user_id, skills, cost_rate, capacity_hours_per_week, pool FROM p3m_resources WHERE id = ?`, existingID).Scan(&curUser, &curSkills, &curCost, &curCapacity, &curPool); err != nil {
				rep.err(row, "", fmt.Sprintf("resource %d not found: %v", existingID, err))
				continue
			}
			if userID != nil {
				curUser = sql.NullInt64{Int64: int64(*userID), Valid: true}
			}
			if skillsRaw != "" {
				curSkills = p3mNormalizeSkills(skillsRaw)
			}
			if strings.TrimSpace(rec["cost_rate"]) != "" {
				curCost = costRate
			}
			if strings.TrimSpace(rec["capacity_hours_per_week"]) != "" {
				curCapacity = capacity
			}
			if pool != "" {
				curPool = nullStr(pool)
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_resources SET user_id=?, skills=?, cost_rate=?, capacity_hours_per_week=?, pool=? WHERE id=?`,
				curUser, curSkills, curCost, curCapacity, curPool, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

func (h *P3MHandler) p3mImportAllocations(c *p3mImportContext, csvText string) importSheetReport {
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

		resourceName := strings.TrimSpace(rec["resource_name"])
		if resourceName == "" {
			rep.err(row, "resource_name", "resource_name is required")
			continue
		}
		rKey := strings.ToLower(resourceName)
		var resourceID int
		if id, ok := c.resourcesByName[rKey]; ok {
			resourceID = id
		} else if !c.resSeen[rKey] || c.executing {
			rep.err(row, "resource_name", fmt.Sprintf("unknown resource %q (import the Resources sheet first)", resourceName))
			continue
		}

		class := importEnum(&rep, row, rec, "allocation_class", []string{"project", "bau"}, false)
		if class == "" {
			class = "project"
		}
		var projectID *int
		if projTitle := strings.TrimSpace(rec["project_title"]); projTitle != "" {
			switch len(c.projectsByTitle[strings.ToLower(projTitle)]) {
			case 0:
				rep.err(row, "project_title", fmt.Sprintf("unknown project %q", projTitle))
				continue
			case 1:
				pid := c.projectsByTitle[strings.ToLower(projTitle)][0]
				projectID = &pid
			default:
				rep.err(row, "project_title", fmt.Sprintf("ambiguous project title %q; use the Projects sheet's portfolio/programme columns to disambiguate", projTitle))
				continue
			}
			if class == "bau" {
				rep.warn(row, "allocation_class", "allocation_class is bau but project_title is set; the project link is kept")
			}
		} else if class == "project" && c.executing {
			rep.err(row, "project_title", "project_title is required when allocation_class is project (leave it empty for BAU)")
			continue
		}

		startDate := importDateField(&rep, row, rec, "start_date", true)
		endDate := importDateField(&rep, row, rec, "end_date", true)
		hoursPerWeek := importIntField(&rep, row, rec, "hours_per_week", 0, 24*7)
		bookingType := importEnum(&rep, row, rec, "booking_type", []string{"soft", "hard"}, false)
		if bookingType == "" {
			bookingType = "soft"
		}
		idStr := strings.TrimSpace(rec["id"])

		key := strconv.Itoa(resourceID) + "\x00" + strconv.Itoa(derefInt(projectID)) + "\x00" + startDate
		existingID, exists := c.allocationsByKey[key]
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
				continue
			}
			res, err := c.exec.Exec(`
				INSERT INTO p3m_resource_allocations (resource_id, project_id, start_date, end_date, hours_per_week, booking_type, allocation_class)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				resourceID, nullInt(projectID), startDate, endDate, hoursPerWeek, bookingType, class)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			c.allocationsByKey[key] = int(id64)
			rep.Created++
		default: // update
			var curProject sql.NullInt64
			var curStart, curEnd string
			var curHours int
			if err := c.exec.QueryRow(`SELECT project_id, start_date, end_date, hours_per_week FROM p3m_resource_allocations WHERE id = ?`, existingID).Scan(&curProject, &curStart, &curEnd, &curHours); err != nil {
				rep.err(row, "", fmt.Sprintf("allocation %d not found: %v", existingID, err))
				continue
			}
			if projectID != nil || idStr != "" {
				curProject = nullInt64Val(projectID)
			}
			if startDate != "" {
				curStart = startDate
			}
			if endDate != "" {
				curEnd = endDate
			}
			if strings.TrimSpace(rec["hours_per_week"]) != "" {
				curHours = hoursPerWeek
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if _, err := c.exec.Exec(`
				UPDATE p3m_resource_allocations SET project_id=?, start_date=?, end_date=?, hours_per_week=?, booking_type=?, allocation_class=? WHERE id=?`,
				curProject, curStart, curEnd, curHours, bookingType, class, existingID); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

func (h *P3MHandler) p3mProcessSheet(c *p3mImportContext, name, csvText string) importSheetReport {
	switch name {
	case "P3M Portfolios":
		return h.p3mImportPortfolios(c, csvText)
	case "P3M Programmes":
		return h.p3mImportProgrammes(c, csvText)
	case "P3M Projects":
		return h.p3mImportProjects(c, csvText)
	case "P3M Intakes (Front Door)":
		return h.p3mImportIntakes(c, csvText)
	case "P3M Investment Cases":
		return h.p3mImportInvestmentCases(c, csvText)
	case "P3M Benefits":
		return h.p3mImportBenefits(c, csvText)
	case "P3M Benefit Realizations":
		return h.p3mImportRealizations(c, csvText)
	case "P3M Budget Tranches":
		return h.p3mImportTranches(c, csvText)
	case "P3M RAID Items":
		return h.p3mImportRAID(c, csvText)
	case "P3M Stakeholders":
		return h.p3mImportStakeholders(c, csvText)
	case "P3M Communications Log":
		return h.p3mImportCommunications(c, csvText)
	case "P3M Resources":
		return h.p3mImportResources(c, csvText)
	case "P3M Resource Allocations":
		return h.p3mImportAllocations(c, csvText)
	}
	return importSheetReport{Errors: []importRowError{{Row: 1, Field: "header", Message: fmt.Sprintf("unknown sheet %q", name)}}}
}

func (h *P3MHandler) p3mParseImportRequest(w http.ResponseWriter, r *http.Request) (*bulkImportRequest, string, bool) {
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
		if !p3mSheetKnown(name) {
			respondValidationError(w, r, fmt.Sprintf("unknown sheet %q (expected one of: %s)", name, strings.Join(p3mImportSheetNames, ", ")))
			return nil, "", false
		}
	}
	return &req, mode, true
}

// ImportPreview handles POST /api/p3m/import/preview. Validates every sheet
// against the current database state without writing anything.
func (h *P3MHandler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	req, mode, okReq := h.p3mParseImportRequest(w, r)
	if !okReq {
		return
	}
	c, err := h.newP3MImportContext(r.Context(), h.db, mode, 0, false)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	for _, name := range p3mImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.p3mProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
	}
	respondJSONOK(w, resp)
}

// ImportExecute handles POST /api/p3m/import/execute. Writes all valid rows in
// dependency order inside a single transaction; invalid rows are skipped and
// reported. On any database failure the whole transaction rolls back.
func (h *P3MHandler) ImportExecute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	req, mode, okReq := h.p3mParseImportRequest(w, r)
	if !okReq {
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("begin import transaction: %w", err))
		return
	}
	c, err := h.newP3MImportContext(r.Context(), tx, mode, user.ID, true)
	if err != nil {
		tx.Rollback()
		respondInternalError(w, r, err)
		return
	}

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	sheetCounts := make(map[string]int64)
	for _, name := range p3mImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.p3mProcessSheet(c, name, csvText)
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

	h.p3mAudit(r, user, "p3m.import.execute", "p3m_import", nil, mode, map[string]any{
		"mode":   mode,
		"sheets": sheetCounts,
	})
	respondJSONOK(w, resp)
}
