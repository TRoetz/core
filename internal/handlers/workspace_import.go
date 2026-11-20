package handlers

// Bulk CSV import for workspaces and their item hierarchy (workbook round-trip).
// Sheets match both templates/windshift-data-collection-template.xlsx and the
// focused curation workbook templates/windshift-workspaces-template.xlsx:
//   "Work Plan (Workspaces)" - one row per project workspace
//   "Items (Work Hierarchy)" - initiative/epic/task/subtask rows inside workspaces
//
// Work plan rows create or update workspaces through the shared creation
// service; workbook assignees receive the built-in Viewer role. Item rows
// build their parent chain on demand and are written through the canonical
// item pipeline as historical imports (no notifications, no automation). Each
// row commits independently; a failed row is reported without aborting the
// rest of the sheet.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/logger"
	"windshift/internal/models"
	"windshift/internal/restapi"
	"windshift/internal/services"
)

const (
	wsSheetWorkPlan = "Work Plan (Workspaces)"
	wsSheetItems    = "Items (Work Hierarchy)"
)

var workspaceImportSheetNames = []string{wsSheetWorkPlan, wsSheetItems}

func workspaceSheetKnown(name string) bool {
	for _, n := range workspaceImportSheetNames {
		if n == name {
			return true
		}
	}
	return false
}

// workPlanUnpersisted lists work-plan columns that carry planning context but
// have no workspace column to store in. They are reported once per sheet so
// users know the data was seen, not silently dropped.
var workPlanUnpersisted = []string{
	"priority", "project_type", "where_lives", "phase", "rag_status", "why", "quarters", "web_team_care",
}

type wsImportContext struct {
	ctx       context.Context
	mode      string // "create" | "upsert"
	actorID   int
	executing bool

	itemSvc    *services.ItemCreationService
	actorName  string
	db         database.Database
	changedWS  bool // any workspace created or updated (drives cache invalidation)

	usersByEmail map[string]int
	usersByName  map[string][]int

	viewerRoleID *int // nil when the built-in Viewer role is missing

	// workspacesByKey: lower(key) -> id, seeded from the database and grown as
	// rows are created. wsSeen marks keys that will exist after the work plan
	// sheet has run (preview has no real ids to hand out).
	workspacesByKey map[string]int
	wsSeen          map[string]bool

	// itemChains: workspaceID\x00lower(title)\x00... -> id of the deepest item
	// created or matched for that chain so far in this payload.
	itemChains map[string]int

	statusesByName map[string]int // lower(name) -> id (statuses are global)
}

func newWSImportContext(ctx context.Context, db database.Database, mode string, actorID int, executing bool) (*wsImportContext, error) {
	c := &wsImportContext{
		ctx:             ctx,
		mode:            mode,
		actorID:         actorID,
		executing:       executing,
		db:              db,
		usersByEmail:    make(map[string]int),
		usersByName:     make(map[string][]int),
		workspacesByKey: make(map[string]int),
		wsSeen:          make(map[string]bool),
		itemChains:      make(map[string]int),
		statusesByName:  make(map[string]int),
	}

	rows, err := db.Query(`SELECT id, lower(email), lower(first_name || ' ' || last_name) FROM users`)
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

	var viewerID int
	if err := db.QueryRow(`SELECT id FROM workspace_roles WHERE builtin_key = 'viewer'`).Scan(&viewerID); err == nil {
		c.viewerRoleID = &viewerID
	} else if !isNotFound(err) {
		return nil, fmt.Errorf("load Viewer role for import: %w", err)
	}

	wsRows, err := db.Query(`SELECT lower(key), id FROM workspaces`)
	if err != nil {
		return nil, fmt.Errorf("load workspace index for import: %w", err)
	}
	for wsRows.Next() {
		var key string
		var id int
		if err := wsRows.Scan(&key, &id); err == nil {
			c.workspacesByKey[key] = id
		}
	}
	wsRows.Close()

	stRows, err := db.Query(`SELECT lower(name), id FROM statuses`)
	if err != nil {
		return nil, fmt.Errorf("load status index for import: %w", err)
	}
	for stRows.Next() {
		var name string
		var id int
		if err := stRows.Scan(&name, &id); err == nil {
			c.statusesByName[name] = id
		}
	}
	stRows.Close()

	return c, nil
}

// resolvePerson maps an email or "first last" display name to a user id.
func (c *wsImportContext) resolvePerson(value string) (*int, error) {
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

// wsBoolField parses an optional true/false field.
func (c *wsImportContext) wsBoolField(rep *importSheetReport, row int, rec map[string]string, field string) *bool {
	v := strings.ToLower(strings.TrimSpace(rec[field]))
	if v == "" {
		return nil
	}
	if v == "true" || v == "1" || v == "yes" {
		t := true
		return &t
	}
	if v == "false" || v == "0" || v == "no" {
		f := false
		return &f
	}
	rep.err(row, field, fmt.Sprintf("invalid boolean %q (use true or false)", rec[field]))
	return nil
}

// wsDateField parses an optional YYYY-MM-DD date.
func (c *wsImportContext) wsDateField(rep *importSheetReport, row int, rec map[string]string, field string) *time.Time {
	v := strings.TrimSpace(rec[field])
	if v == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		rep.err(row, field, fmt.Sprintf("invalid date %q (use YYYY-MM-DD)", v))
		return nil
	}
	return &t
}

// wsPersonField resolves an optional person reference; unresolved values warn.
func (c *wsImportContext) wsPersonField(rep *importSheetReport, row int, rec map[string]string, field string) *int {
	v := strings.TrimSpace(rec[field])
	if v == "" {
		return nil
	}
	id, err := c.resolvePerson(v)
	if err != nil {
		rep.warn(row, field, err.Error())
		return nil
	}
	return id
}

// wsGrantViewer gives each resolved assignee the built-in Viewer role in the
// workspace. Idempotent: existing grants are ignored.
func (c *wsImportContext) wsGrantViewer(rep *importSheetReport, row int, wsID int, refs []string) {
	if !c.executing || c.viewerRoleID == nil {
		return
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		id, err := c.resolvePerson(ref)
		if err != nil {
			rep.warn(row, "assignees", fmt.Sprintf("assignee %q: %v", ref, err))
			continue
		}
		if _, err := c.db.Exec(
			`INSERT OR IGNORE INTO user_workspace_roles (user_id, workspace_id, role_id, granted_by) VALUES (?, ?, ?, ?)`,
			*id, wsID, *c.viewerRoleID, c.actorID,
		); err != nil {
			rep.warn(row, "assignees", fmt.Sprintf("grant Viewer to %q: %v", ref, err))
		}
	}
}

// ---------------------------------------------------------------------------
// Sheet 1: Work Plan (Workspaces)
// ---------------------------------------------------------------------------

func (h *WorkspaceHandler) wsImportWorkPlan(c *wsImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}

	unpersistedSeen := false
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++
		errCount := len(rep.Errors)

		name := strings.TrimSpace(rec["name"])
		key := strings.ToUpper(strings.TrimSpace(rec["key"]))
		if name == "" {
			rep.err(row, "name", "workspace name is required")
		}
		if key == "" {
			rep.err(row, "key", "workspace key is required")
		} else if !services.ValidWorkspaceKey(key) {
			rep.err(row, "key", fmt.Sprintf("invalid workspace key %q (2-10 uppercase letters or digits)", rec["key"]))
		}

		defaultView := strings.TrimSpace(rec["default_view"])
		if defaultView != "" && !wsDefaultViewKnown(defaultView) {
			rep.err(row, "default_view", fmt.Sprintf("invalid default view %q (board, backlog, list, tree or map)", rec["default_view"]))
		}

		isTemplate := c.wsBoolField(&rep, row, rec, "is_template")
		active := c.wsBoolField(&rep, row, rec, "active")
		ownerID := c.wsPersonField(&rep, row, rec, "delivery_owner")
		var assignees []string
		if raw := strings.TrimSpace(rec["assignees"]); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				if t := strings.TrimSpace(part); t != "" {
					assignees = append(assignees, t)
				}
			}
		}

		if !unpersistedSeen {
			for _, col := range workPlanUnpersisted {
				if rec[col] != "" {
					rep.Warnings = append(rep.Warnings, importRowWarning{
						Row:     1,
						Field:   "sheet",
						Message: fmt.Sprintf("column %q carries planning context but has no workspace field to store in; it is not persisted by import", col),
					})
					break
				}
			}
			unpersistedSeen = true
		}

		if len(rep.Errors) > errCount {
			continue
		}

		lk := strings.ToLower(key)
		existingID, exists := c.workspacesByKey[lk]

		switch {
		case exists && c.mode == "create":
			rep.Skipped++
		case exists:
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if err := h.wsUpdateWorkspace(c, existingID, rec, ownerID, isTemplate, active); err != nil {
				rep.err(row, "key", fmt.Sprintf("update workspace %q: %v", key, err))
				continue
			}
			c.wsGrantViewer(&rep, row, existingID, assignees)
			c.changedWS = true
			rep.Updated++
		default:
			if !c.executing {
				rep.ToCreate++
				c.wsSeen[lk] = true
				continue
			}
			wsID, err := h.wsCreateWorkspace(c, name, key, rec, ownerID, isTemplate, active)
			if err != nil {
				rep.err(row, "key", fmt.Sprintf("create workspace %q: %v", key, err))
				continue
			}
			c.workspacesByKey[lk] = wsID
			c.wsSeen[lk] = true
			c.wsGrantViewer(&rep, row, wsID, assignees)
			c.changedWS = true
			rep.Created++
		}
	}
	return rep
}

func wsDefaultViewKnown(v string) bool {
	switch strings.ToLower(v) {
	case "board", "backlog", "list", "tree", "map":
		return true
	}
	return false
}

// wsCreateWorkspace creates a workspace through the shared creation service
// (which grants the creator Administrator in the same transaction) and applies
// is_template, which the params struct does not carry.
func (h *WorkspaceHandler) wsCreateWorkspace(c *wsImportContext, name, key string, rec map[string]string, ownerID *int, isTemplate, active *bool) (int, error) {
	params := services.CreateWorkspaceParams{
		Name:        name,
		Key:         key,
		Description: strings.TrimSpace(rec["description"]),
		Icon:        strings.TrimSpace(rec["icon"]),
		Color:       strings.TrimSpace(rec["color"]),
		DefaultView: strings.ToLower(strings.TrimSpace(rec["default_view"])),
		CreatorID:   c.actorID,
	}
	if ownerID != nil {
		params.OwnerID = ownerID
	}
	params.Active = active

	res, err := h.workspaceService.Create(c.ctx, params)
	if err != nil {
		return 0, err
	}
	wsID := res.Workspace.ID

	if isTemplate != nil && *isTemplate {
		if _, err := c.db.Exec(`UPDATE workspaces SET is_template = TRUE WHERE id = ?`, wsID); err != nil {
			return 0, fmt.Errorf("mark workspace as template: %w", err)
		}
	}
	return wsID, nil
}

// wsUpdateWorkspace updates only the non-empty workbook fields so partial
// sheets never wipe existing values.
func (h *WorkspaceHandler) wsUpdateWorkspace(c *wsImportContext, wsID int, rec map[string]string, ownerID *int, isTemplate, active *bool) error {
	sets := make([]string, 0, 7)
	args := make([]any, 0, 8)

	addStr := func(col, field string) {
		if v := strings.TrimSpace(rec[field]); v != "" {
			sets = append(sets, col+"=?")
			args = append(args, v)
		}
	}
	addStr("description", "description")
	addStr("icon", "icon")
	addStr("color", "color")

	if dv := strings.ToLower(strings.TrimSpace(rec["default_view"])); dv != "" {
		sets = append(sets, "default_view=?")
		args = append(args, dv)
	}
	if isTemplate != nil {
		sets = append(sets, "is_template=?")
		args = append(args, *isTemplate)
	}
	if active != nil {
		sets = append(sets, "active=?")
		args = append(args, *active)
	}
	if ownerID != nil {
		sets = append(sets, "owner_id=?")
		args = append(args, *ownerID)
	}

	if len(sets) == 0 {
		return nil // nothing to change; role grants still apply
	}
	args = append(args, wsID)
	if _, err := c.db.Exec(`UPDATE workspaces SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sheet 2: Items (Work Hierarchy)
// ---------------------------------------------------------------------------

var wsItemLevelColumns = []string{"initiative", "epic", "task", "subtask"}

func (h *WorkspaceHandler) wsImportItems(c *wsImportContext, csvText string) importSheetReport {
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

		wsKey := strings.TrimSpace(rec["workspace_key"])
		if wsKey == "" {
			rep.err(row, "workspace_key", "workspace key is required")
			continue
		}
		lk := strings.ToLower(wsKey)
		wsID, known := c.workspacesByKey[lk]
		if !known && !c.wsSeen[lk] {
			rep.err(row, "workspace_key", fmt.Sprintf("unknown workspace key %q (create it in the Work Plan sheet first)", wsKey))
			continue
		}

		var levels []string
		for _, col := range wsItemLevelColumns {
			if v := strings.TrimSpace(rec[col]); v != "" {
				levels = append(levels, v)
			}
		}
		if len(levels) == 0 {
			rep.err(row, "initiative", "provide at least one of initiative, epic, task or subtask")
			continue
		}

		description := strings.TrimSpace(rec["description"])
		assigneeID := c.wsPersonField(&rep, row, rec, "assignee")
		dueDate := c.wsDateField(&rep, row, rec, "due_date")
		var statusID *int
		if sn := strings.TrimSpace(rec["status"]); sn != "" {
			if id, ok := c.statusesByName[strings.ToLower(sn)]; ok {
				statusID = &id
			} else {
				rep.warn(row, "status", fmt.Sprintf("status %q not found; the item keeps its default status", sn))
			}
		}

		if len(rep.Errors) > errCount {
			continue
		}

		if !c.executing {
			rep.ToCreate++
			continue
		}
		if !known {
			rep.err(row, "workspace_key", fmt.Sprintf("workspace %q was not created; cannot add items", wsKey))
			continue
		}

		h.wsEnsureItemChain(c, &rep, row, wsID, levels, description, assigneeID, dueDate, statusID)
	}
	return rep
}

// wsEnsureItemChain walks the workbook's level path for one row, creating any
// missing items. Intermediate levels are created bare; the leaf receives the
// row's attributes. An existing leaf is updated in upsert mode and skipped in
// create mode. A failure aborts the rest of the chain for that row.
func (h *WorkspaceHandler) wsEnsureItemChain(
	c *wsImportContext, rep *importSheetReport, row, wsID int, levels []string,
	description string, assigneeID *int, dueDate *time.Time, statusID *int,
) {
	parentID := 0 // 0 = workspace root
	chainParts := []string{strconv.Itoa(wsID)}

	for i, title := range levels {
		chainParts = append(chainParts, strings.ToLower(title))
		chainKey := strings.Join(chainParts, "\x00")
		if id, ok := c.itemChains[chainKey]; ok {
			parentID = id
			continue
		}

		isLeaf := i == len(levels)-1
		existingID, found, err := h.wsFindItem(c, wsID, title, parentID)
		if err != nil {
			rep.err(row, wsItemLevelColumns[i], fmt.Sprintf("look up item %q: %v", title, err))
			return
		}

		switch {
		case found && isLeaf && c.mode == "upsert":
			c.itemChains[chainKey] = existingID
			if n := h.wsUpdateItem(c, rep, row, existingID, description, assigneeID, dueDate, statusID); n > 0 {
				rep.Updated++
			} else {
				rep.Skipped++ // no workbook fields to apply
			}
			parentID = existingID
		case found:
			c.itemChains[chainKey] = existingID
			if isLeaf {
				rep.Skipped++ // duplicate in create mode, or intermediate level
			}
			parentID = existingID
		default:
			input := services.ItemCreateInput{
				WorkspaceID: wsID,
				Title:       title,
			}
			if parentID != 0 {
				input.ParentID = &parentID
			}
			if isLeaf {
				input.Description = description
				input.AssigneeID = assigneeID
				input.DueDate = dueDate
				input.StatusID = statusID
			}
			res, err := c.itemSvc.CreateImported(c.actorID, c.actorName, input, "workbook-import")
			if err != nil {
				rep.err(row, wsItemLevelColumns[i], fmt.Sprintf("create item %q: %v", title, err))
				return
			}
			c.itemChains[chainKey] = res.Item.ID
			parentID = res.Item.ID
			if isLeaf {
				rep.Created++
			}
		}
	}
}

// wsFindItem looks for an item with the given title directly under parentID
// (0 = workspace root).
func (h *WorkspaceHandler) wsFindItem(c *wsImportContext, wsID int, title string, parentID int) (int, bool, error) {
	var id int
	var err error
	if parentID == 0 {
		err = c.db.QueryRow(
			`SELECT id FROM items WHERE workspace_id = ? AND lower(title) = lower(?) AND parent_id IS NULL LIMIT 1`,
			wsID, title,
		).Scan(&id)
	} else {
		err = c.db.QueryRow(
			`SELECT id FROM items WHERE workspace_id = ? AND lower(title) = lower(?) AND parent_id = ? LIMIT 1`,
			wsID, title, parentID,
		).Scan(&id)
	}
	if err != nil {
		if isNotFound(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

// wsUpdateItem applies the non-empty workbook fields to an existing item and
// returns how many were applied.
func (h *WorkspaceHandler) wsUpdateItem(c *wsImportContext, rep *importSheetReport, row, itemID int, description string, assigneeID *int, dueDate *time.Time, statusID *int) int {
	sets := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if description != "" {
		sets = append(sets, "description=?")
		args = append(args, description)
	}
	if assigneeID != nil {
		sets = append(sets, "assignee_id=?")
		args = append(args, *assigneeID)
	}
	if dueDate != nil {
		sets = append(sets, "due_date=?")
		args = append(args, dueDate.Format("2006-01-02"))
	}
	if statusID != nil {
		sets = append(sets, "status_id=?")
		args = append(args, *statusID)
	}
	if len(sets) == 0 {
		return 0
	}
	args = append(args, itemID)
	if _, err := c.db.Exec(`UPDATE items SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		rep.err(row, "item", fmt.Sprintf("update item %d: %v", itemID, err))
		return 0
	}
	return len(sets)
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

func (h *WorkspaceHandler) wsProcessSheet(c *wsImportContext, name, csvText string) importSheetReport {
	switch name {
	case wsSheetWorkPlan:
		return h.wsImportWorkPlan(c, csvText)
	case wsSheetItems:
		return h.wsImportItems(c, csvText)
	}
	return importSheetReport{Errors: []importRowError{{Row: 1, Field: "header", Message: fmt.Sprintf("unknown sheet %q", name)}}}
}

func (h *WorkspaceHandler) wsParseImportRequest(w http.ResponseWriter, r *http.Request) (*bulkImportRequest, string, bool) {
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
		if !workspaceSheetKnown(name) {
			respondValidationError(w, r, fmt.Sprintf("unknown sheet %q (expected: %s)", name, strings.Join(workspaceImportSheetNames, ", ")))
			return nil, "", false
		}
	}
	return &req, mode, true
}

// wsRequireImportPermission enforces the global workspace.create permission;
// the import routes are not workspace-scoped so the route-level workspace
// permission middleware cannot apply here. Item writes inside workspaces stay
// gated by per-workspace permissions in the item creation pipeline.
func (h *WorkspaceHandler) wsRequireImportPermission(w http.ResponseWriter, r *http.Request, user *models.User) bool {
	allowed, err := h.permissionService.HasGlobalPermission(user.ID, models.PermissionWorkspaceCreate)
	if err != nil || !allowed {
		respondError(w, r, restapi.NewAPIError(http.StatusForbidden, restapi.ErrCodeInsufficientPermission, "workspace.create permission required"))
		return false
	}
	return true
}

// ImportPreview handles POST /api/workspaces/import/preview. It validates every
// row against the current database without writing anything.
func (h *WorkspaceHandler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.wsRequireImportPermission(w, r, user) {
		return
	}
	req, mode, ok := h.wsParseImportRequest(w, r)
	if !ok {
		return
	}

	c, err := newWSImportContext(r.Context(), h.db, mode, user.ID, false)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	for _, name := range workspaceImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.wsProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
	}
	respondJSONOK(w, resp)
}

// ImportExecute handles POST /api/workspaces/import/execute. Work plan rows are
// processed first so item rows can reference workspaces created in the same
// payload; each row commits independently and failures are reported per row.
func (h *WorkspaceHandler) ImportExecute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.wsRequireImportPermission(w, r, user) {
		return
	}
	req, mode, ok := h.wsParseImportRequest(w, r)
	if !ok {
		return
	}

	c, err := newWSImportContext(r.Context(), h.db, mode, user.ID, true)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c.itemSvc = services.NewItemCreationService(h.db, h.permissionService)
	c.actorName = user.Username

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	sheetCounts := make(map[string]int64)
	for _, name := range workspaceImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.wsProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
		sheetCounts[name] = int64(rep.Rows)
	}

	if c.changedWS && h.cacheInvalidator != nil {
		if err := h.cacheInvalidator.Apply(services.AuthorizationInvalidation{
			ResetPermissions:        true,
			ActiveWorkspacesChanged: true,
			WorkspaceKeysChanged:    true,
		}); err != nil {
			logger.Get().Warn("workspace import: authorization cache invalidation failed", "error", err)
		}
	}

	logAuditWithDetails(h.db, r, user, "workspace.import.execute", "workspace_import", nil, mode, map[string]any{
		"mode":   mode,
		"sheets": sheetCounts,
	})
	respondJSONOK(w, resp)
}
