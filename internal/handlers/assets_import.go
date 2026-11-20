package handlers

// Workbook import for the Asset Management module (preview + execute), mirroring
// the P3M/SMS/workspace importers: one JSON request carrying per-sheet CSV text,
// validated against the live database in preview mode and applied inside a single
// transaction in execute mode.
//
// Sheets (processed in dependency order):
//   "Asset Sets"       - create/update asset_management_sets; new sets are initialized exactly like the app does (Administrator role for the importer + four default statuses)
//   "Asset Types"      - upsert by (set, lower(name))
//   "Asset Categories" - upsert by (set, lower(name)); parent_category_name is resolved in a second pass so rows may reference parents listed later in the sheet; parent counts are maintained like repository.updateCategoryParentCounts
//   "Asset Statuses"   - upsert by (set, lower(name)); is_default promotes within the set
//   "Asset Set Access" - upsert user_asset_set_roles by (user_email, set_name, role_name); users and roles must already exist
//   "Assets"           - upsert by asset_tag when present (rows without a tag are always created); type required, category/status optional with fallback to the set's default status
//
// Preview mode never writes: rows that would update existing data count as
// to_update. References to entities created earlier in the same payload resolve
// through the live transaction at execute time; preview validates them against a
// pre-scan of the child sheets instead, so typos inside brand-new sets surface
// only at execute time (documented limitation).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
	"windshift/internal/restapi"
)

const (
	assetSheetSets     = "Asset Sets"
	assetSheetTypes    = "Asset Types"
	assetSheetCats     = "Asset Categories"
	assetSheetStatuses = "Asset Statuses"
	assetSheetAccess   = "Asset Set Access"
	assetSheetAssets   = "Assets"
)

var assetImportSheetNames = []string{assetSheetSets, assetSheetTypes, assetSheetCats, assetSheetStatuses, assetSheetAccess, assetSheetAssets}

func assetSheetKnown(name string) bool {
	for _, n := range assetImportSheetNames {
		if name == n {
			return true
		}
	}
	return false
}

type assetImportContext struct {
	ctx       context.Context
	exec      importExecer
	mode      string // "create" | "upsert"
	actorID   int    // importing user (created_by / granted_by columns)
	executing bool

	usersByEmail map[string]int // lower(email) -> id
	rolesByName  map[string]int // lower(role name) -> id (asset_roles: system + custom)

	batchSetNames map[string]bool // preview only: set names created earlier in this payload

	// batchChildNames[lower(set_name)][lower(child name)] = true, pre-scanned
	// from the Types/Categories/Statuses sheets so that PREVIEW can validate
	// references to children created earlier in this payload (execute mode
	// resolves them through the live transaction).
	batchChildNames map[string]map[string]bool

	defaultStatusBySet map[int]int // set_id -> default status id (DB at build time; execute: also batch-created sets)
}

func newAssetImportContext(ctx context.Context, exec importExecer, mode string, actorID int, executing bool) (*assetImportContext, error) {
	c := &assetImportContext{
		ctx:                ctx,
		exec:               exec,
		mode:               mode,
		actorID:            actorID,
		executing:          executing,
		usersByEmail:       make(map[string]int),
		rolesByName:        make(map[string]int),
		batchSetNames:      make(map[string]bool),
		batchChildNames:    make(map[string]map[string]bool),
		defaultStatusBySet: make(map[int]int),
	}

	rows, err := exec.Query(`SELECT id, lower(email) FROM users WHERE email IS NOT NULL AND email != ''`)
	if err != nil {
		return nil, fmt.Errorf("load user index for import: %w", err)
	}
	for rows.Next() {
		var id int
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan user index row: %w", err)
		}
		c.usersByEmail[email] = id
	}
	rows.Close()

	roleRows, err := exec.Query(`SELECT id, lower(name) FROM asset_roles`)
	if err != nil {
		return nil, fmt.Errorf("load role index for import: %w", err)
	}
	for roleRows.Next() {
		var id int
		var name string
		if err := roleRows.Scan(&id, &name); err != nil {
			roleRows.Close()
			return nil, fmt.Errorf("scan role index row: %w", err)
		}
		c.rolesByName[name] = id
	}
	roleRows.Close()

	stRows, err := exec.Query(`SELECT set_id, id FROM asset_statuses WHERE is_default = 1`)
	if err != nil {
		return nil, fmt.Errorf("load default status index for import: %w", err)
	}
	for stRows.Next() {
		var setID, id int
		if err := stRows.Scan(&setID, &id); err != nil {
			stRows.Close()
			return nil, fmt.Errorf("scan default status row: %w", err)
		}
		c.defaultStatusBySet[setID] = id
	}
	stRows.Close()

	return c, nil
}

// assetPrescan records child names per set from the Types/Categories/Statuses
// sheets so preview mode can validate references to entities created earlier in
// this payload. Execute mode does not need it (the live transaction resolves
// them); parsing is cheap enough that one code path serves both modes.
func (c *assetImportContext) assetPrescan(sheets map[string]string) {
	for _, sheet := range []string{assetSheetTypes, assetSheetCats, assetSheetStatuses} {
		csvText, ok := sheets[sheet]
		if !ok || strings.TrimSpace(csvText) == "" {
			continue
		}
		recs, err := importRecords(csvText)
		if err != nil {
			continue // the sheet processor reports parse errors itself
		}
		for _, rec := range recs {
			setKey := strings.ToLower(strings.TrimSpace(rec["set_name"]))
			name := strings.ToLower(strings.TrimSpace(rec["name"]))
			if setKey == "" || name == "" {
				continue
			}
			m := c.batchChildNames[setKey]
			if m == nil {
				m = make(map[string]bool)
				c.batchChildNames[setKey] = m
			}
			m[name] = true
		}
	}
}

// assetResolveSet resolves the required set_name column to a set id. In preview
// mode, names created earlier in this payload resolve as "new" (id 0): their
// children are necessarily creates and reference checks against them are
// deferred to execute time.
func (c *assetImportContext) assetResolveSet(rep *importSheetReport, row int, rec map[string]string, field string) (int, bool) {
	name := strings.TrimSpace(rec[field])
	if name == "" {
		rep.err(row, field, fmt.Sprintf("%s is required", field))
		return 0, false
	}
	key := strings.ToLower(name)
	var id int
	err := c.exec.QueryRow(`SELECT id FROM asset_management_sets WHERE lower(name) = ? LIMIT 1`, key).Scan(&id)
	if err == nil {
		return id, true
	}
	if err != sql.ErrNoRows {
		rep.err(row, field, fmt.Sprintf("asset set lookup failed: %v", err))
		return 0, false
	}
	if !c.executing && c.batchSetNames[key] {
		return 0, true // new set in this payload; resolvable at execute time
	}
	rep.err(row, field, fmt.Sprintf("unknown asset set %q (must match an Asset Sets row or existing set)", name))
	return 0, false
}

// assetChildInPayload reports whether a child entity with the given name is
// created earlier in this payload for the named set (preview tolerance).
func (c *assetImportContext) assetChildInPayload(setNameLower, name string) bool {
	m := c.batchChildNames[setNameLower]
	return m != nil && m[strings.ToLower(strings.TrimSpace(name))]
}

// assetFindCategoryID looks up a category id within a set without reporting an
// error; the caller decides how to react. In preview mode it also accepts names
// created earlier in this payload (id 0, resolvable at execute time).
func (c *assetImportContext) assetFindCategoryID(setID int, setNameLower, name string) (int, bool) {
	var id int
	err := c.exec.QueryRow(`SELECT id FROM asset_categories WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(strings.TrimSpace(name))).Scan(&id)
	if err == nil {
		return id, true
	}
	if !c.executing && (setID == 0 || c.assetChildInPayload(setNameLower, name)) {
		return 0, true // resolvable at execute time
	}
	return 0, false
}

// assetResolveType resolves the required asset_type_name column within a set.
func (c *assetImportContext) assetResolveType(rep *importSheetReport, row int, rec map[string]string, setID int, setNameLower string) (*int, bool) {
	name := strings.TrimSpace(rec["asset_type_name"])
	if name == "" {
		rep.err(row, "asset_type_name", "asset_type_name is required")
		return nil, false
	}
	var id int
	err := c.exec.QueryRow(`SELECT id FROM asset_types WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&id)
	if err == nil {
		return &id, true
	}
	if err != sql.ErrNoRows {
		rep.err(row, "asset_type_name", fmt.Sprintf("asset type lookup failed: %v", err))
		return nil, false
	}
	if !c.executing && (setID == 0 || c.assetChildInPayload(setNameLower, name)) {
		return nil, true // resolvable at execute time
	}
	rep.err(row, "asset_type_name", fmt.Sprintf("unknown asset type %q in set (must match an Asset Types row or existing type)", name))
	return nil, false
}

// assetResolveCategory resolves the optional category_name column within a set.
// Returns (nil, true) when the column is empty.
func (c *assetImportContext) assetResolveCategory(rep *importSheetReport, row int, rec map[string]string, setID int, setNameLower string) (*int, bool) {
	name := strings.TrimSpace(rec["category_name"])
	if name == "" {
		return nil, true
	}
	var id int
	err := c.exec.QueryRow(`SELECT id FROM asset_categories WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&id)
	if err == nil {
		return &id, true
	}
	if err != sql.ErrNoRows {
		rep.err(row, "category_name", fmt.Sprintf("asset category lookup failed: %v", err))
		return nil, false
	}
	if !c.executing && (setID == 0 || c.assetChildInPayload(setNameLower, name)) {
		return nil, true // resolvable at execute time
	}
	rep.err(row, "category_name", fmt.Sprintf("unknown asset category %q in set (must match an Asset Categories row or existing category)", name))
	return nil, false
}

// assetResolveStatus resolves the optional status_name column within a set,
// falling back to the set's default status when empty. New sets are seeded with
// an Active default at execute time, so preview treats them as resolvable.
func (c *assetImportContext) assetResolveStatus(rep *importSheetReport, row int, rec map[string]string, setID int, setNameLower string) (*int, bool) {
	name := strings.TrimSpace(rec["status_name"])
	if name == "" {
		if !c.executing && setID == 0 {
			return nil, true // new sets are seeded with an Active default at execute time
		}
		id, ok := c.defaultStatusBySet[setID]
		if !ok {
			rep.err(row, "status_name", "no status specified and asset set has no default status")
			return nil, false
		}
		return &id, true
	}
	var id int
	err := c.exec.QueryRow(`SELECT id FROM asset_statuses WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&id)
	if err == nil {
		return &id, true
	}
	if err != sql.ErrNoRows {
		rep.err(row, "status_name", fmt.Sprintf("asset status lookup failed: %v", err))
		return nil, false
	}
	if !c.executing && (setID == 0 || c.assetChildInPayload(setNameLower, name)) {
		return nil, true // resolvable at execute time
	}
	rep.err(row, "status_name", fmt.Sprintf("unknown asset status %q in set (must match an Asset Statuses row or existing status)", name))
	return nil, false
}

// ---------------------------------------------------------------------------
// Field helpers
// ---------------------------------------------------------------------------

func assetIntField(rep *importSheetReport, row int, rec map[string]string, field string, def int) int {
	v := strings.TrimSpace(rec[field])
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		rep.err(row, field, fmt.Sprintf("invalid integer %q", rec[field]))
		return def
	}
	return n
}

// assetBoolField parses an optional true/false column; nil when empty.
func assetBoolField(rep *importSheetReport, row int, rec map[string]string, field string) *bool {
	v := strings.ToLower(strings.TrimSpace(rec[field]))
	if v == "" {
		return nil
	}
	switch v {
	case "true", "1", "yes":
		t := true
		return &t
	case "false", "0", "no":
		f := false
		return &f
	}
	rep.err(row, field, fmt.Sprintf("invalid boolean %q (use true or false)", rec[field]))
	return nil
}

func assetBoolValue(rep *importSheetReport, row int, rec map[string]string, field string, def bool) bool {
	if v := assetBoolField(rep, row, rec, field); v != nil {
		return *v
	}
	return def
}

func orDefaultStr(rec map[string]string, field, def string) string {
	if v := strings.TrimSpace(rec[field]); v != "" {
		return v
	}
	return def
}

// assetCustomFields validates the optional custom_field_values column as a JSON
// object and returns it for storage (nil when empty). Per-type field schema
// validation is left to the app's own CSV import; workbook rows store the raw
// values.
func assetCustomFields(rep *importSheetReport, row int, rec map[string]string) *string {
	v := strings.TrimSpace(rec["custom_field_values"])
	if v == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(v), &obj); err != nil {
		rep.err(row, "custom_field_values", fmt.Sprintf("must be a JSON object: %v", err))
		return nil
	}
	return &v
}

// ---------------------------------------------------------------------------
// Sheet 1: Asset Sets
// ---------------------------------------------------------------------------

func (h *AssetHandler) assetImportSets(c *assetImportContext, csvText string) importSheetReport {
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
		key := strings.ToLower(name)
		var existing int
		err := c.exec.QueryRow(`SELECT id FROM asset_management_sets WHERE lower(name) = ? LIMIT 1`, key).Scan(&existing)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			rep.err(row, "name", fmt.Sprintf("asset set lookup failed: %v", err))
			continue
		}

		isDefaultPtr := assetBoolField(&rep, row, rec, "is_default")
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				c.batchSetNames[key] = true
				continue
			}
			now := time.Now()
			isDefault := isDefaultPtr != nil && *isDefaultPtr
			if isDefault {
				if _, err := c.exec.Exec(`UPDATE asset_management_sets SET is_default = false`); err != nil {
					rep.err(row, "is_default", fmt.Sprintf("clear default set failed: %v", err))
					continue
				}
			}
			res, err := c.exec.Exec(
				`INSERT INTO asset_management_sets (name, description, is_default, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				name, strings.TrimSpace(rec["description"]), isDefault, c.actorID, now, now)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			setID64, _ := res.LastInsertId()
			setID := int(setID64)

			// Initialize like the app does (CreateSetAndInitialize): Administrator
			// role for the importer + four default statuses.
			adminRoleID, ok := c.rolesByName["administrator"]
			if !ok {
				rep.err(row, "", "built-in Administrator asset role missing; cannot initialize new set")
				continue
			}
			if _, err := c.exec.Exec(
				`INSERT INTO user_asset_set_roles (set_id, user_id, role_id, granted_by, granted_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT (set_id, user_id) DO UPDATE SET role_id = ?, granted_by = ?, granted_at = ?`,
				setID, c.actorID, adminRoleID, c.actorID, now, adminRoleID, c.actorID, now); err != nil {
				rep.err(row, "", fmt.Sprintf("grant administrator role failed: %v", err))
				continue
			}
			defaultStatuses := []struct {
				name         string
				color        string
				isDefault    bool
				displayOrder int
			}{
				{"Active", "#22c55e", true, 0},
				{"Inactive", "#6b7280", false, 1},
				{"Maintenance", "#f59e0b", false, 2},
				{"Retired", "#ef4444", false, 3},
			}
			for _, s := range defaultStatuses {
				res, err := c.exec.Exec(
					`INSERT INTO asset_statuses (set_id, name, color, is_default, display_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					setID, s.name, s.color, s.isDefault, s.displayOrder, now, now)
				if err != nil {
					rep.err(row, "", fmt.Sprintf("seed default status %q failed: %v", s.name, err))
					continue
				}
				if s.isDefault {
					id64, _ := res.LastInsertId()
					c.defaultStatusBySet[setID] = int(id64)
				}
			}
			rep.Created++
		default: // update - only provided fields are set
			var curID int
			var curName string
			if err := c.exec.QueryRow(`SELECT id, name FROM asset_management_sets WHERE id = ?`, existing).Scan(&curID, &curName); err != nil {
				rep.err(row, "name", fmt.Sprintf("asset set %d not found: %v", existing, err))
				continue
			}
			var sets []string
			var args []any
			if !strings.EqualFold(curName, name) {
				sets = append(sets, "name=?")
				args = append(args, name)
			}
			if v := strings.TrimSpace(rec["description"]); v != "" {
				sets = append(sets, "description=?")
				args = append(args, v)
			}
			if isDefaultPtr != nil {
				sets = append(sets, "is_default=?")
				args = append(args, *isDefaultPtr)
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if isDefaultPtr != nil && *isDefaultPtr {
				if _, err := c.exec.Exec(`UPDATE asset_management_sets SET is_default = false WHERE is_default = true AND id != ?`, existing); err != nil {
					rep.err(row, "is_default", fmt.Sprintf("clear other default sets failed: %v", err))
					continue
				}
			}
			args = append(args, time.Now(), existing)
			if _, err := c.exec.Exec(`UPDATE asset_management_sets SET `+strings.Join(sets, ", ")+`, updated_at = ? WHERE id = ?`, args...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// Sheet 2: Asset Types
// ---------------------------------------------------------------------------

func (h *AssetHandler) assetImportTypes(c *assetImportContext, csvText string) importSheetReport {
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

		setID, okSet := c.assetResolveSet(&rep, row, rec, "set_name")
		if !okSet {
			continue
		}
		name := strings.TrimSpace(rec["name"])
		if name == "" {
			rep.err(row, "name", "name is required")
			continue
		}

		var existing int
		err := c.exec.QueryRow(`SELECT id FROM asset_types WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&existing)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			rep.err(row, "name", fmt.Sprintf("asset type lookup failed: %v", err))
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			if _, err := c.exec.Exec(
				`INSERT INTO asset_types (set_id, name, description, icon, color, display_order, is_active) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				setID, name, strings.TrimSpace(rec["description"]), orDefaultStr(rec, "icon", "Box"), orDefaultStr(rec, "color", "#6b7280"), assetIntField(&rep, row, rec, "display_order", 0), assetBoolValue(&rep, row, rec, "is_active", true)); err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			if len(rep.Errors) > errCount {
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var curID int
			var curName string
			if err := c.exec.QueryRow(`SELECT id, name FROM asset_types WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&curID, &curName); err != nil {
				rep.err(row, "name", fmt.Sprintf("asset type lookup failed: %v", err))
				continue
			}
			var sets []string
			var args []any
			if !strings.EqualFold(curName, name) {
				sets = append(sets, "name=?")
				args = append(args, name)
			}
			addStr := func(col, field string) {
				if v := strings.TrimSpace(rec[field]); v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addStr("description", "description")
			addStr("icon", "icon")
			addStr("color", "color")
			if v := strings.TrimSpace(rec["display_order"]); v != "" {
				sets = append(sets, "display_order=?")
				args = append(args, assetIntField(&rep, row, rec, "display_order", 0))
			}
			if b := assetBoolField(&rep, row, rec, "is_active"); b != nil {
				sets = append(sets, "is_active=?")
				args = append(args, *b)
			}
			if len(rep.Errors) > errCount {
				continue
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			args = append(args, time.Now(), curID)
			if _, err := c.exec.Exec(`UPDATE asset_types SET `+strings.Join(sets, ", ")+`, updated_at = ? WHERE id = ?`, args...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// Sheet 3: Asset Categories (two-phase parent resolution)
// ---------------------------------------------------------------------------

type assetPendingParent struct {
	row        int
	setID      int
	categoryID int
	parentName string
}

func (h *AssetHandler) assetImportCategories(c *assetImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}

	var pending []assetPendingParent // parent assignments applied after all rows exist (execute only)

	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++

		setID, okSet := c.assetResolveSet(&rep, row, rec, "set_name")
		if !okSet {
			continue
		}
		name := strings.TrimSpace(rec["name"])
		if name == "" {
			rep.err(row, "name", "name is required")
			continue
		}
		parentName := strings.TrimSpace(rec["parent_category_name"])
		setKeyLower := strings.ToLower(strings.TrimSpace(rec["set_name"]))
		if parentName != "" && strings.EqualFold(parentName, name) {
			rep.err(row, "parent_category_name", "a category cannot be its own parent")
			continue
		}

		var existing int
		err := c.exec.QueryRow(`SELECT id FROM asset_categories WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&existing)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			rep.err(row, "name", fmt.Sprintf("asset category lookup failed: %v", err))
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			now := time.Now()
			res, err := c.exec.Exec(
				`INSERT INTO asset_categories (set_id, name, description, path, created_at, updated_at) VALUES (?, ?, ?, '/', ?, ?)`,
				setID, name, strings.TrimSpace(rec["description"]), now, now)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			id64, _ := res.LastInsertId()
			if parentName != "" {
				pending = append(pending, assetPendingParent{row: row, setID: setID, categoryID: int(id64), parentName: parentName})
			}
			rep.Created++
		default: // update - name/description now; parent change deferred to phase 2
			var curID int
			var curName string
			if err := c.exec.QueryRow(`SELECT id, name FROM asset_categories WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&curID, &curName); err != nil {
				rep.err(row, "name", fmt.Sprintf("asset category lookup failed: %v", err))
				continue
			}

			parentChange := false
			if parentName != "" {
				if _, okParent := c.assetFindCategoryID(setID, setKeyLower, parentName); !okParent {
					rep.err(row, "parent_category_name", fmt.Sprintf("unknown asset category %q in set (must match an Asset Categories row or existing category)", parentName))
					continue
				}
				parentChange = true // phase 2 assigns it (no-op when already correct)
			}

			var sets []string
			var args []any
			if !strings.EqualFold(curName, name) {
				sets = append(sets, "name=?")
				args = append(args, name)
			}
			if v := strings.TrimSpace(rec["description"]); v != "" {
				sets = append(sets, "description=?")
				args = append(args, v)
			}
			if len(sets) == 0 && !parentChange {
				rep.Skipped++ // nothing to change
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if parentChange {
				pending = append(pending, assetPendingParent{row: row, setID: setID, categoryID: curID, parentName: parentName})
			}
			if len(sets) > 0 {
				args = append(args, time.Now(), curID)
				if _, err := c.exec.Exec(`UPDATE asset_categories SET `+strings.Join(sets, ", ")+`, updated_at = ? WHERE id = ?`, args...); err != nil {
					rep.err(row, "", fmt.Sprintf("update failed: %v", err))
					continue
				}
			}
			rep.Updated++
		}
	}

	// Phase 2 (execute only): every category in this payload now exists in the
	// transaction, so parent names resolve even when listed after their children.
	if c.executing && len(pending) > 0 {
		touched := make(map[int]bool) // parents whose counts need recomputing
		for _, p := range pending {
			pid, okParent := c.assetFindCategoryID(p.setID, "", p.parentName)
			if !okParent || pid == 0 {
				rep.err(p.row, "parent_category_name", fmt.Sprintf("unknown asset category %q (still unresolved after all rows were created)", p.parentName))
				continue
			}
			if c.assetIsDescendant(p.categoryID, pid) {
				rep.err(p.row, "parent_category_name", fmt.Sprintf("setting parent %q would create a cycle in the category tree", p.parentName))
				continue
			}
			var curParent sql.NullInt64
			if err := c.exec.QueryRow(`SELECT parent_id FROM asset_categories WHERE id = ?`, p.categoryID).Scan(&curParent); err != nil {
				rep.err(p.row, "parent_category_name", fmt.Sprintf("category %d not found: %v", p.categoryID, err))
				continue
			}
			if curParent.Valid && int(curParent.Int64) == pid {
				continue // already correct
			}
			if _, err := c.exec.Exec(`UPDATE asset_categories SET parent_id = ?, updated_at = ? WHERE id = ?`, pid, time.Now(), p.categoryID); err != nil {
				rep.err(p.row, "parent_category_name", fmt.Sprintf("assign parent failed: %v", err))
				continue
			}
			if curParent.Valid {
				touched[int(curParent.Int64)] = true
			}
			touched[pid] = true
		}
		for pid := range touched {
			if err := c.assetUpdateCategoryCounts(pid); err != nil {
				rep.Warnings = append(rep.Warnings, importRowWarning{Row: 0, Field: "parent_category_name", Message: fmt.Sprintf("category count refresh failed for parent %d: %v", pid, err)})
			}
		}
	}

	return rep
}

// assetIsDescendant reports whether categoryID is a descendant of ancestorID
// (recursive walk up the parent chain).
func (c *assetImportContext) assetIsDescendant(categoryID, ancestorID int) bool {
	var found int
	err := c.exec.QueryRow(`
		WITH RECURSIVE ancestors AS (
			SELECT parent_id AS id FROM asset_categories WHERE id = ? AND parent_id IS NOT NULL
			UNION ALL
			SELECT ac.parent_id AS id FROM asset_categories ac INNER JOIN ancestors a ON ac.id = a.id WHERE ac.parent_id IS NOT NULL
		) SELECT COUNT(*) FROM ancestors WHERE id = ?`, categoryID, ancestorID).Scan(&found)
	return err == nil && found > 0
}

// assetUpdateCategoryCounts recomputes children_count/has_children on parentID
// and descendants_count on all of its ancestors, mirroring
// repository.updateCategoryParentCounts.
func (c *assetImportContext) assetUpdateCategoryCounts(parentID int) error {
	var childrenCount int
	if err := c.exec.QueryRow(`SELECT COUNT(*) FROM asset_categories WHERE parent_id = ?`, parentID).Scan(&childrenCount); err != nil {
		return fmt.Errorf("count category children: %w", err)
	}
	now := time.Now()
	if _, err := c.exec.Exec(
		`UPDATE asset_categories SET children_count = ?, has_children = ?, updated_at = ? WHERE id = ?`,
		childrenCount, childrenCount > 0, now, parentID); err != nil {
		return fmt.Errorf("update category counts: %w", err)
	}
	if _, err := c.exec.Exec(`
		WITH RECURSIVE ancestors AS (
			SELECT parent_id AS id FROM asset_categories WHERE id = ? AND parent_id IS NOT NULL
			UNION ALL
			SELECT ac.parent_id AS id FROM asset_categories ac INNER JOIN ancestors a ON ac.id = a.id WHERE ac.parent_id IS NOT NULL
		)
		UPDATE asset_categories
		SET descendants_count = (
			WITH RECURSIVE descendants AS (
				SELECT id FROM asset_categories WHERE parent_id = asset_categories.id
				UNION ALL
				SELECT ac.id FROM asset_categories ac INNER JOIN descendants d ON ac.parent_id = d.id
			)
			SELECT COUNT(*) FROM descendants
		)
		WHERE id IN (SELECT id FROM ancestors)`, parentID); err != nil {
		return fmt.Errorf("update ancestor descendant counts: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sheet 4: Asset Statuses
// ---------------------------------------------------------------------------

func (h *AssetHandler) assetImportStatuses(c *assetImportContext, csvText string) importSheetReport {
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

		setID, okSet := c.assetResolveSet(&rep, row, rec, "set_name")
		if !okSet {
			continue
		}
		name := strings.TrimSpace(rec["name"])
		if name == "" {
			rep.err(row, "name", "name is required")
			continue
		}

		var existing int
		err := c.exec.QueryRow(`SELECT id FROM asset_statuses WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&existing)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			rep.err(row, "name", fmt.Sprintf("asset status lookup failed: %v", err))
			continue
		}

		isDefaultPtr := assetBoolField(&rep, row, rec, "is_default")
		if len(rep.Errors) > errCount {
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			now := time.Now()
			isDefault := isDefaultPtr != nil && *isDefaultPtr
			res, err := c.exec.Exec(
				`INSERT INTO asset_statuses (set_id, name, color, description, is_default, display_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				setID, name, orDefaultStr(rec, "color", "#6b7280"), strings.TrimSpace(rec["description"]), isDefault, assetIntField(&rep, row, rec, "display_order", 0), now, now)
			if err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			if len(rep.Errors) > errCount {
				continue
			}
			if isDefault {
				id64, _ := res.LastInsertId()
				c.defaultStatusBySet[setID] = int(id64)
			}
			rep.Created++
		default: // update - only non-empty fields are set
			var curID int
			var curName string
			if err := c.exec.QueryRow(`SELECT id, name FROM asset_statuses WHERE set_id = ? AND lower(name) = ? LIMIT 1`, setID, strings.ToLower(name)).Scan(&curID, &curName); err != nil {
				rep.err(row, "name", fmt.Sprintf("asset status lookup failed: %v", err))
				continue
			}
			var sets []string
			var args []any
			if !strings.EqualFold(curName, name) {
				sets = append(sets, "name=?")
				args = append(args, name)
			}
			addStr := func(col, field string) {
				if v := strings.TrimSpace(rec[field]); v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addStr("color", "color")
			addStr("description", "description")
			if v := strings.TrimSpace(rec["display_order"]); v != "" {
				sets = append(sets, "display_order=?")
				args = append(args, assetIntField(&rep, row, rec, "display_order", 0))
			}
			if isDefaultPtr != nil {
				sets = append(sets, "is_default=?")
				args = append(args, *isDefaultPtr)
			}
			if len(rep.Errors) > errCount {
				continue
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			if isDefaultPtr != nil && *isDefaultPtr {
				if _, err := c.exec.Exec(`UPDATE asset_statuses SET is_default = false WHERE set_id = ? AND id != ?`, setID, curID); err != nil {
					rep.err(row, "is_default", fmt.Sprintf("clear other default statuses failed: %v", err))
					continue
				}
			}
			args = append(args, time.Now(), curID)
			if _, err := c.exec.Exec(`UPDATE asset_statuses SET `+strings.Join(sets, ", ")+`, updated_at = ? WHERE id = ?`, args...); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			if isDefaultPtr != nil && *isDefaultPtr {
				c.defaultStatusBySet[setID] = curID
			}
			rep.Updated++
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// Sheet 5: Asset Set Access
// ---------------------------------------------------------------------------

func (h *AssetHandler) assetImportAccess(c *assetImportContext, csvText string) importSheetReport {
	rep := importSheetReport{Errors: []importRowError{}}
	recs, err := importRecords(csvText)
	if err != nil {
		rep.err(1, "header", err.Error())
		return rep
	}
	for _, rec := range recs {
		row := recordLine(rec)
		rep.Rows++

		email := strings.TrimSpace(rec["user_email"])
		if email == "" {
			rep.err(row, "user_email", "user_email is required")
			continue
		}
		userID, okUser := c.usersByEmail[strings.ToLower(email)]
		if !okUser {
			rep.err(row, "user_email", fmt.Sprintf("no user with email %q (users are not created by this import)", email))
			continue
		}

		setID, okSet := c.assetResolveSet(&rep, row, rec, "set_name")
		if !okSet {
			continue
		}
		roleName := strings.TrimSpace(rec["role_name"])
		if roleName == "" {
			rep.err(row, "role_name", "role_name is required")
			continue
		}
		roleID, okRole := c.rolesByName[strings.ToLower(roleName)]
		if !okRole {
			rep.err(row, "role_name", fmt.Sprintf("unknown asset role %q (must match an existing asset_roles name)", roleName))
			continue
		}

		var existing int
		err := c.exec.QueryRow(`SELECT id FROM user_asset_set_roles WHERE user_id = ? AND set_id = ?`, userID, setID).Scan(&existing)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			rep.err(row, "", fmt.Sprintf("access lookup failed: %v", err))
			continue
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			now := time.Now()
			if _, err := c.exec.Exec(
				`INSERT INTO user_asset_set_roles (user_id, set_id, role_id, granted_by, granted_at) VALUES (?, ?, ?, ?, ?)`,
				userID, setID, roleID, c.actorID, now); err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - change the granted role
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			now := time.Now()
			if _, err := c.exec.Exec(
				`UPDATE user_asset_set_roles SET role_id = ?, granted_by = ?, granted_at = ? WHERE id = ?`,
				roleID, c.actorID, now, existing); err != nil {
				rep.err(row, "", fmt.Sprintf("update failed: %v", err))
				continue
			}
			rep.Updated++
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// Sheet 6: Assets
// ---------------------------------------------------------------------------

func (h *AssetHandler) assetImportAssets(c *assetImportContext, csvText string) importSheetReport {
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

		setNameLower := strings.ToLower(strings.TrimSpace(rec["set_name"]))
		setID, okSet := c.assetResolveSet(&rep, row, rec, "set_name")
		if !okSet {
			continue
		}
		title := strings.TrimSpace(rec["title"])
		if title == "" {
			rep.err(row, "title", "title is required")
			continue
		}

		typeID, okType := c.assetResolveType(&rep, row, rec, setID, setNameLower)
		if !okType {
			continue
		}
		categoryID, okCat := c.assetResolveCategory(&rep, row, rec, setID, setNameLower)
		if !okCat {
			continue
		}
		statusID, okStatus := c.assetResolveStatus(&rep, row, rec, setID, setNameLower)
		if !okStatus {
			continue
		}

		tag := strings.TrimSpace(rec["asset_tag"])
		var existing int
		exists := false
		if tag != "" {
			err := c.exec.QueryRow(`SELECT id FROM assets WHERE set_id = ? AND lower(asset_tag) = ? LIMIT 1`, setID, strings.ToLower(tag)).Scan(&existing)
			exists = err == nil
			if err != nil && err != sql.ErrNoRows {
				rep.err(row, "asset_tag", fmt.Sprintf("asset lookup failed: %v", err))
				continue
			}
		}

		switch importDecide(c.mode, exists, "") {
		case "skip":
			rep.Skipped++
		case "create":
			if !c.executing {
				rep.ToCreate++
				continue
			}
			cfJSON := assetCustomFields(&rep, row, rec)
			if len(rep.Errors) > errCount {
				continue
			}
			now := time.Now()
			if _, err := c.exec.Exec(
				`INSERT INTO assets (set_id, asset_type_id, category_id, status_id, title, description, asset_tag, custom_field_values, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				setID, *typeID, nullInt(categoryID), nullInt(statusID), title, strings.TrimSpace(rec["description"]), tag, cfJSON, c.actorID, now, now); err != nil {
				rep.err(row, "", fmt.Sprintf("insert failed: %v", err))
				continue
			}
			rep.Created++
		default: // update - only non-empty fields are set; references re-resolved above
			var curID int
			var curTitle string
			if err := c.exec.QueryRow(`SELECT id, title FROM assets WHERE set_id = ? AND lower(asset_tag) = ? LIMIT 1`, setID, strings.ToLower(tag)).Scan(&curID, &curTitle); err != nil {
				rep.err(row, "asset_tag", fmt.Sprintf("asset lookup failed: %v", err))
				continue
			}
			var sets []string
			var args []any
			if !strings.EqualFold(curTitle, title) {
				sets = append(sets, "title=?")
				args = append(args, title)
			}
			addStr := func(col, field string) {
				if v := strings.TrimSpace(rec[field]); v != "" {
					sets = append(sets, col+"=?")
					args = append(args, v)
				}
			}
			addStr("description", "description")
			if typeID != nil && *typeID > 0 {
				sets = append(sets, "asset_type_id=?")
				args = append(args, *typeID)
			}
			if categoryID != nil {
				sets = append(sets, "category_id=?")
				args = append(args, *categoryID)
			}
			if statusID != nil && *statusID > 0 {
				sets = append(sets, "status_id=?")
				args = append(args, *statusID)
			}
			cfJSON := assetCustomFields(&rep, row, rec)
			if cfJSON != nil {
				sets = append(sets, "custom_field_values=?")
				args = append(args, *cfJSON)
			}
			if len(rep.Errors) > errCount {
				continue
			}
			if len(sets) == 0 {
				rep.Skipped++ // nothing to change
				continue
			}
			if !c.executing {
				rep.ToUpdate++
				continue
			}
			args = append(args, time.Now(), curID)
			if _, err := c.exec.Exec(`UPDATE assets SET `+strings.Join(sets, ", ")+`, updated_at = ? WHERE id = ?`, args...); err != nil {
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

func (h *AssetHandler) assetParseImportRequest(w http.ResponseWriter, r *http.Request) (*bulkImportRequest, string, bool) {
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
		if !assetSheetKnown(name) {
			respondValidationError(w, r, fmt.Sprintf("unknown sheet %q (expected one of: %s)", name, strings.Join(assetImportSheetNames, ", ")))
			return nil, "", false
		}
	}
	return &req, mode, true
}

// assetRequireImportPermission enforces the same global gate as set creation
// (system.admin or asset.manage): the import routes are not set-scoped, so the
// route-level per-set permission middleware cannot apply here.
func (h *AssetHandler) assetRequireImportPermission(w http.ResponseWriter, r *http.Request, user *models.User) bool {
	admin, err := h.permissionService.HasGlobalPermission(user.ID, models.PermissionSystemAdmin)
	if err == nil && admin {
		return true
	}
	manage, err := h.permissionService.HasGlobalPermission(user.ID, "asset.manage")
	if err != nil || !manage {
		respondError(w, r, restapi.NewAPIError(http.StatusForbidden, restapi.ErrCodeInsufficientPermission, "system.admin or asset.manage permission required"))
		return false
	}
	return true
}

// ImportWorkbookPreview handles POST /api/assets/import/preview. It validates
// every sheet against the current database without writing anything.
func (h *AssetHandler) ImportWorkbookPreview(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.assetRequireImportPermission(w, r, user) {
		return
	}
	req, mode, okReq := h.assetParseImportRequest(w, r)
	if !okReq {
		return
	}
	c, err := newAssetImportContext(r.Context(), h.db, mode, 0, false)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c.assetPrescan(req.Sheets)

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	for _, name := range assetImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.assetProcessSheet(c, name, csvText)
		resp.Sheets[name] = rep
		resp.TotalRows += int64(rep.Rows)
		resp.ErrorCount += len(rep.Errors)
	}
	respondJSONOK(w, resp)
}

// ImportWorkbookExecute handles POST /api/assets/import/execute. It writes all
// valid rows in dependency order inside a single transaction; invalid rows are
// skipped and reported. On any database failure the whole transaction rolls back.
func (h *AssetHandler) ImportWorkbookExecute(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	if !h.assetRequireImportPermission(w, r, user) {
		return
	}
	req, mode, okReq := h.assetParseImportRequest(w, r)
	if !okReq {
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("begin import transaction: %w", err))
		return
	}
	c, err := newAssetImportContext(r.Context(), tx, mode, user.ID, true)
	if err != nil {
		tx.Rollback()
		respondInternalError(w, r, err)
		return
	}

	resp := importResponse{Mode: mode, Sheets: make(map[string]importSheetReport)}
	sheetCounts := make(map[string]int64)
	for _, name := range assetImportSheetNames {
		csvText, ok := req.Sheets[name]
		if !ok {
			continue
		}
		rep := h.assetProcessSheet(c, name, csvText)
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

	logAuditWithDetails(h.db, r, user, "asset.import.execute", "asset_import", nil, mode, map[string]any{
		"mode":   mode,
		"sheets": sheetCounts,
	})
	respondJSONOK(w, resp)
}

func (h *AssetHandler) assetProcessSheet(c *assetImportContext, name, csvText string) importSheetReport {
	switch name {
	case assetSheetSets:
		return h.assetImportSets(c, csvText)
	case assetSheetTypes:
		return h.assetImportTypes(c, csvText)
	case assetSheetCats:
		return h.assetImportCategories(c, csvText)
	case assetSheetStatuses:
		return h.assetImportStatuses(c, csvText)
	case assetSheetAccess:
		return h.assetImportAccess(c, csvText)
	case assetSheetAssets:
		return h.assetImportAssets(c, csvText)
	}
	return importSheetReport{Errors: []importRowError{{Row: 1, Field: "header", Message: fmt.Sprintf("unknown sheet %q", name)}}}
}
