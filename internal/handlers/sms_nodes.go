// SMS node taxonomy handlers: admin-configurable node kinds and their role
// vocabulary, the generic node hierarchy (buildings/sites with owner+tenant
// occupants, road segments, areas…), and stakeholder↔node membership with
// kind-validated roles.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// smsDerefStr returns the pointed value or "" for a nil pointer.
func smsDerefStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// smsDerefInt returns the pointed value or 0 for a nil pointer.
func smsDerefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// smsEntityIDArg renders an entity_id column argument: non-positive binds NULL.
func smsEntityIDArg(entityID int) any {
	if entityID <= 0 {
		return nil
	}
	return entityID
}

const smsNodeKindColumns = `id, key, label, description, allows_children, child_kinds, is_system, created_at, updated_at`

func scanSMSNodeKind(row interface{ Scan(dest ...any) error }) (*models.SMSNodeKind, error) {
	k := &models.SMSNodeKind{}
	err := row.Scan(&k.ID, &k.Key, &k.Label, &k.Description, &k.AllowsChildren, &k.ChildKinds, &k.IsSystem, &k.CreatedAt, &k.UpdatedAt)
	return k, err
}

const smsNodeKindRoleColumns = `id, kind_id, role_key, label, sort_order`

func scanSMSNodeKindRole(row interface{ Scan(dest ...any) error }) (*models.SMSNodeKindRole, error) {
	r := &models.SMSNodeKindRole{}
	err := row.Scan(&r.ID, &r.KindID, &r.RoleKey, &r.Label, &r.SortOrder)
	return r, err
}

const smsNodeColumns = `n.id, n.external_id, n.kind_id, n.parent_id, n.name, n.description, n.location_lat, n.location_lon, n.location_text, n.custom_fields, n.entity_type, n.entity_id, n.notes, n.created_at, n.updated_at`

func scanSMSNode(row interface{ Scan(dest ...any) error }) (*models.SMSNode, error) {
	n := &models.SMSNode{}
	err := row.Scan(&n.ID, &n.ExternalID, &n.KindID, &n.ParentID, &n.Name, &n.Description, &n.LocationLat, &n.LocationLon, &n.LocationText, &n.CustomFields, &n.EntityType, &n.EntityID, &n.Notes, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

const smsNodeMemberColumns = `m.id, m.node_id, m.stakeholder_id, m.role, m.start_date, m.end_date, m.notes, m.created_at`

// scanSMSNodeMemberJoined scans a member row joined to its stakeholder.
func scanSMSNodeMemberJoined(row interface{ Scan(dest ...any) error }) (*models.SMSNodeMember, error) {
	m := &models.SMSNodeMember{}
	err := row.Scan(&m.ID, &m.NodeID, &m.StakeholderID, &m.Role, &m.StartDate, &m.EndDate, &m.Notes, &m.CreatedAt, &m.Name, &m.Organization)
	return m, err
}

// smsSplitChildKinds normalizes a comma-separated list of kind keys: trims
// whitespace, drops empties and de-duplicates while preserving order.
func smsSplitChildKinds(raw string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// smsChildKindsContains reports whether the stored child_kinds list permits key.
func smsChildKindsContains(list, key string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == key {
			return true
		}
	}
	return false
}

// validSMSDate reports whether s is a calendar date in YYYY-MM-DD form.
func validSMSDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// smsValidateNodeParent checks that parentID may host a child of kindKey: the
// parent exists, its kind allows children and (when restricted) permits the
// child's kind. Returns a 400 message or an internal error.
func (h *SMSHandler) smsValidateNodeParent(ctx context.Context, parentID int, kindKey string) (string, error) {
	var allowsChildren bool
	var childKinds string
	err := h.db.QueryRowContext(ctx, `SELECT k.allows_children, k.child_kinds FROM sms_nodes n JOIN sms_node_kinds k ON k.id = n.kind_id WHERE n.id = ?`, parentID).Scan(&allowsChildren, &childKinds)
	if isNotFound(err) {
		return "parent node not found", nil
	} else if err != nil {
		return "", err
	}
	if !allowsChildren {
		return "the parent's kind does not allow children", nil
	}
	if childKinds != "" && !smsChildKindsContains(childKinds, kindKey) {
		return fmt.Sprintf("kind %q is not an allowed child of the parent's kind", kindKey), nil
	}
	return "", nil
}

// smsValidateEntityLink mirrors AddStakeholderEntity's P3M link checks and
// writes the 400/404 response itself. Returns false when a response was written.
func (h *SMSHandler) smsValidateEntityLink(w http.ResponseWriter, r *http.Request, ctx context.Context, entityType string, entityID int) bool {
	table, valid := smsP3MEntityTables[entityType]
	if !valid || entityID <= 0 {
		respondBadRequest(w, r, "entity_type must be portfolio, programme or project and entity_id is required")
		return false
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, entityID).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "P3M "+entityType+" not found")
		return false
	} else if err != nil {
		respondInternalError(w, r, err)
		return false
	}
	return true
}

// smsNodeMemberView is one member row with its kind role label resolved.
type smsNodeMemberView struct {
	models.SMSNodeMember
	RoleLabel string `json:"role_label,omitempty"`
}

// smsNodeDetail is GetNode's payload: the node plus its member roster.
type smsNodeDetail struct {
	models.SMSNode
	Members []smsNodeMemberView `json:"members"`
}

// smsGetNode loads one node with kind labels, parent name and members.
func (h *SMSHandler) smsGetNode(ctx context.Context, id int) (*smsNodeDetail, error) {
	n := &models.SMSNode{}
	err := h.db.QueryRowContext(ctx, `SELECT `+smsNodeColumns+`, k.key AS kind_key, k.label AS kind_label, COALESCE(p.name, '') AS parent_name FROM sms_nodes n JOIN sms_node_kinds k ON k.id = n.kind_id LEFT JOIN sms_nodes p ON p.id = n.parent_id WHERE n.id = ?`, id).
		Scan(&n.ID, &n.ExternalID, &n.KindID, &n.ParentID, &n.Name, &n.Description, &n.LocationLat, &n.LocationLon, &n.LocationText, &n.CustomFields, &n.EntityType, &n.EntityID, &n.Notes, &n.CreatedAt, &n.UpdatedAt, &n.KindKey, &n.KindLabel, &n.ParentName)
	if err != nil {
		return nil, err
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+smsNodeMemberColumns+`, s.name AS name, s.organization AS organization, COALESCE(r.label, '') AS role_label FROM sms_node_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id LEFT JOIN sms_node_kind_roles r ON r.kind_id = ? AND r.role_key = m.role WHERE m.node_id = ? ORDER BY m.role COLLATE NOCASE, s.name COLLATE NOCASE`, n.KindID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]smsNodeMemberView, 0)
	for rows.Next() {
		var v smsNodeMemberView
		if err := rows.Scan(&v.ID, &v.NodeID, &v.StakeholderID, &v.Role, &v.StartDate, &v.EndDate, &v.Notes, &v.CreatedAt, &v.Name, &v.Organization, &v.RoleLabel); err != nil {
			return nil, err
		}
		members = append(members, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &smsNodeDetail{SMSNode: *n, Members: members}, nil
}

// ---------------------------------------------------------------------------
// Node kinds.
// ---------------------------------------------------------------------------

// ListNodeKinds returns every node kind ordered by key, each annotated with its
// current node count.
func (h *SMSHandler) ListNodeKinds(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT `+smsNodeKindColumns+`, (SELECT COUNT(*) FROM sms_nodes WHERE kind_id = k.id) AS node_count FROM sms_node_kinds k ORDER BY k.key`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNodeKind, 0)
	for rows.Next() {
		k := &models.SMSNodeKind{}
		if err := rows.Scan(&k.ID, &k.Key, &k.Label, &k.Description, &k.AllowsChildren, &k.ChildKinds, &k.IsSystem, &k.CreatedAt, &k.UpdatedAt, &k.NodeCount); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateNodeKind registers a new node kind. child_kinds is a comma-separated
// list of existing kind keys; an empty list means any kind may nest below it.
func (h *SMSHandler) CreateNodeKind(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Key            string `json:"key"`
		Label          string `json:"label"`
		Description    string `json:"description"`
		AllowsChildren *bool  `json:"allows_children,omitempty"`
		ChildKinds     string `json:"child_kinds"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	in.Label = strings.TrimSpace(in.Label)
	if in.Key == "" {
		respondBadRequest(w, r, "key is required")
		return
	}
	if in.Label == "" {
		respondBadRequest(w, r, "label is required")
		return
	}
	keys := smsSplitChildKinds(in.ChildKinds)
	ctx := r.Context()
	for _, key := range keys {
		var n int
		err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kinds WHERE key = ?`, key).Scan(&n)
		if isNotFound(err) {
			respondBadRequest(w, r, fmt.Sprintf("child_kinds references unknown kind %q", key))
			return
		} else if err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kinds WHERE key = ?`, in.Key).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "node kind key already exists")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	allowsChildren := true
	if in.AllowsChildren != nil {
		allowsChildren = *in.AllowsChildren
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_node_kinds (key, label, description, allows_children, child_kinds) VALUES (?, ?, ?, ?, ?)`,
		in.Key, in.Label, in.Description, allowsChildren, strings.Join(keys, ","))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	k, err := scanSMSNodeKind(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindColumns+` FROM sms_node_kinds WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_created", "sms_node_kind", &k.ID, k.Label, map[string]any{"key": k.Key})
	respondJSONCreated(w, k)
}

// UpdateNodeKind applies a partial update to a node kind; the key is immutable.
func (h *SMSHandler) UpdateNodeKind(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Label          *string `json:"label,omitempty"`
		Description    *string `json:"description,omitempty"`
		AllowsChildren *bool   `json:"allows_children,omitempty"`
		ChildKinds     *string `json:"child_kinds,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNodeKind(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindColumns+` FROM sms_node_kinds WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	label := existing.Label
	if in.Label != nil {
		label = *in.Label
	}
	if label == "" {
		respondBadRequest(w, r, "label must not be empty")
		return
	}
	description := existing.Description
	if in.Description != nil {
		description = *in.Description
	}
	allowsChildren := existing.AllowsChildren
	if in.AllowsChildren != nil {
		allowsChildren = *in.AllowsChildren
	}
	childKinds := existing.ChildKinds
	if in.ChildKinds != nil {
		keys := smsSplitChildKinds(*in.ChildKinds)
		for _, key := range keys {
			var n int
			err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kinds WHERE key = ?`, key).Scan(&n)
			if isNotFound(err) {
				respondBadRequest(w, r, fmt.Sprintf("child_kinds references unknown kind %q", key))
				return
			} else if err != nil {
				respondInternalError(w, r, err)
				return
			}
		}
		childKinds = strings.Join(keys, ",")
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE sms_node_kinds SET label = ?, description = ?, allows_children = ?, child_kinds = ? WHERE id = ?`,
		label, description, allowsChildren, childKinds, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	updated, err := scanSMSNodeKind(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindColumns+` FROM sms_node_kinds WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_updated", "sms_node_kind", &updated.ID, updated.Label, map[string]any{"key": updated.Key})
	respondJSONOK(w, updated)
}

// DeleteNodeKind removes a node kind. System kinds, kinds still in use by nodes
// and kinds referenced from another kind's child_kinds are protected (409).
func (h *SMSHandler) DeleteNodeKind(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNodeKind(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindColumns+` FROM sms_node_kinds WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if existing.IsSystem {
		respondConflict(w, r, "system node kinds cannot be deleted")
		return
	}
	var nodeCount int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_nodes WHERE kind_id = ?`, id).Scan(&nodeCount); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if nodeCount > 0 {
		respondConflict(w, r, "node kind still has nodes")
		return
	}
	var refCount int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_node_kinds WHERE id <> ? AND child_kinds LIKE ?`, id, "%"+existing.Key+"%").Scan(&refCount); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if refCount > 0 {
		respondConflict(w, r, "node kind is referenced by another kind's child_kinds")
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_node_kinds WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_deleted", "sms_node_kind", nil, existing.Label, map[string]any{"key": existing.Key})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Node kind roles.
// ---------------------------------------------------------------------------

// ListNodeKindRoles returns the allowed membership roles for one node kind.
func (h *SMSHandler) ListNodeKindRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kinds WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+smsNodeKindRoleColumns+` FROM sms_node_kind_roles WHERE kind_id = ? ORDER BY sort_order, id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNodeKindRole, 0)
	for rows.Next() {
		role, err := scanSMSNodeKindRole(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *role)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// AddNodeKindRole registers one allowed membership role for a node kind.
func (h *SMSHandler) AddNodeKindRole(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		RoleKey   string `json:"role_key"`
		Label     string `json:"label"`
		SortOrder *int   `json:"sort_order,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.RoleKey = strings.TrimSpace(in.RoleKey)
	if in.RoleKey == "" {
		respondBadRequest(w, r, "role_key is required")
		return
	}
	ctx := r.Context()
	var kindLabel string
	err := h.db.QueryRowContext(ctx, `SELECT label FROM sms_node_kinds WHERE id = ?`, id).Scan(&kindLabel)
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kind_roles WHERE kind_id = ? AND role_key = ?`, id, in.RoleKey).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "role already exists for this kind")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	sortOrder := intOr(in.SortOrder, 0).(int)
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) VALUES (?, ?, ?, ?)`,
		id, in.RoleKey, in.Label, sortOrder)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rid64, _ := res.LastInsertId()
	role, err := scanSMSNodeKindRole(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindRoleColumns+` FROM sms_node_kind_roles WHERE id = ?`, int(rid64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_role_added", "sms_node_kind_role", &role.ID, role.RoleKey, map[string]any{"kind_id": id, "kind_label": kindLabel})
	respondJSONCreated(w, role)
}

// UpdateNodeKindRole applies a partial update to one node kind role.
func (h *SMSHandler) UpdateNodeKindRole(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Label     *string `json:"label,omitempty"`
		SortOrder *int    `json:"sort_order,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNodeKindRole(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindRoleColumns+` FROM sms_node_kind_roles WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind role not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	label := existing.Label
	if in.Label != nil {
		label = *in.Label
	}
	sortOrder := intOr(in.SortOrder, existing.SortOrder).(int)
	if _, err := h.db.ExecContext(ctx, `UPDATE sms_node_kind_roles SET label = ?, sort_order = ? WHERE id = ?`, label, sortOrder, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	updated, err := scanSMSNodeKindRole(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindRoleColumns+` FROM sms_node_kind_roles WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_role_updated", "sms_node_kind_role", &updated.ID, updated.RoleKey, map[string]any{"kind_id": existing.KindID})
	respondJSONOK(w, updated)
}

// DeleteNodeKindRole removes a role vocabulary entry. Roles still assigned to
// node members are protected (409).
func (h *SMSHandler) DeleteNodeKindRole(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNodeKindRole(h.db.QueryRowContext(ctx, `SELECT `+smsNodeKindRoleColumns+` FROM sms_node_kind_roles WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind role not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var used int
	err = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_node_members m JOIN sms_nodes n ON n.id = m.node_id WHERE n.kind_id = ? AND m.role = ?`, existing.KindID, existing.RoleKey).Scan(&used)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if used > 0 {
		respondConflict(w, r, "role is still assigned to node members")
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_node_kind_roles WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_kind_role_deleted", "sms_node_kind_role", nil, existing.RoleKey, map[string]any{"kind_id": existing.KindID})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Nodes.
// ---------------------------------------------------------------------------

// ListNodes returns nodes with kind labels and child/member counts. Optional
// filters: ?kind_id, ?parent_id, ?q (name substring), ?entity_type, ?entity_id.
func (h *SMSHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT ` + smsNodeColumns + `, k.key AS kind_key, k.label AS kind_label, (SELECT COUNT(*) FROM sms_nodes c WHERE c.parent_id = n.id) AS child_count, (SELECT COUNT(*) FROM sms_node_members m WHERE m.node_id = n.id) AS member_count FROM sms_nodes n JOIN sms_node_kinds k ON k.id = n.kind_id`
	conds := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if v := r.URL.Query().Get("kind_id"); v != "" {
		conds = append(conds, "n.kind_id = ?")
		args = append(args, v)
	}
	if v := r.URL.Query().Get("parent_id"); v != "" {
		conds = append(conds, "n.parent_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("q")); v != "" {
		conds = append(conds, "n.name LIKE ?")
		args = append(args, "%"+v+"%")
	}
	if et := r.URL.Query().Get("entity_type"); et != "" {
		conds = append(conds, "n.entity_type = ?")
		args = append(args, et)
	}
	if eid := r.URL.Query().Get("entity_id"); eid != "" {
		conds = append(conds, "n.entity_id = ?")
		args = append(args, eid)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY n.name COLLATE NOCASE`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNode, 0)
	for rows.Next() {
		n := &models.SMSNode{}
		if err := rows.Scan(&n.ID, &n.ExternalID, &n.KindID, &n.ParentID, &n.Name, &n.Description, &n.LocationLat, &n.LocationLon, &n.LocationText, &n.CustomFields, &n.EntityType, &n.EntityID, &n.Notes, &n.CreatedAt, &n.UpdatedAt, &n.KindKey, &n.KindLabel, &n.ChildCount, &n.MemberCount); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateNode adds a node to the hierarchy. The parent (when given) must exist,
// allow children and permit this node's kind; custom_fields must be a JSON object.
func (h *SMSHandler) CreateNode(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name         string   `json:"name"`
		KindID       int      `json:"kind_id"`
		ParentID     *int     `json:"parent_id,omitempty"`
		Description  string   `json:"description"`
		LocationLat  *float64 `json:"location_lat,omitempty"`
		LocationLon  *float64 `json:"location_lon,omitempty"`
		LocationText string   `json:"location_text"`
		CustomFields string   `json:"custom_fields"`
		EntityType   *string  `json:"entity_type,omitempty"`
		EntityID     *int     `json:"entity_id,omitempty"`
		Notes        string   `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	if in.KindID <= 0 {
		respondBadRequest(w, r, "kind_id is required")
		return
	}
	customFields := in.CustomFields
	if customFields == "" {
		customFields = "{}"
	}
	if !json.Valid([]byte(customFields)) || !strings.HasPrefix(strings.TrimSpace(customFields), "{") {
		respondBadRequest(w, r, "custom_fields must be a JSON object")
		return
	}

	ctx := r.Context()
	var kindKey string
	err := h.db.QueryRowContext(ctx, `SELECT key FROM sms_node_kinds WHERE id = ?`, in.KindID).Scan(&kindKey)
	if isNotFound(err) {
		respondNotFound(w, r, "Node kind not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if in.ParentID != nil && *in.ParentID > 0 {
		msg, err := h.smsValidateNodeParent(ctx, *in.ParentID, kindKey)
		if msg != "" {
			respondBadRequest(w, r, msg)
			return
		} else if err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	entityType := ""
	if in.EntityType != nil {
		entityType = *in.EntityType
	}
	entityID := 0
	if in.EntityID != nil {
		entityID = *in.EntityID
	}
	if entityType != "" || entityID > 0 {
		if !h.smsValidateEntityLink(w, r, ctx, entityType, entityID) {
			return
		}
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_nodes (name, kind_id, parent_id, description, location_lat, location_lon, location_text, custom_fields, entity_type, entity_id, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.KindID, nullInt(in.ParentID), in.Description, in.LocationLat, in.LocationLon, in.LocationText, customFields,
		nullStringOrEmpty(entityType), smsEntityIDArg(entityID), in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	node, err := h.smsGetNode(ctx, int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_created", "sms_node", &node.ID, node.Name, map[string]any{"kind_id": in.KindID, "parent_id": nullIntStr(in.ParentID)})
	respondJSONCreated(w, node)
}

// GetNode returns one node with kind labels, parent name and its member roster.
func (h *SMSHandler) GetNode(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	node, err := h.smsGetNode(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, node)
}

// UpdateNode applies a partial update to a node. An explicit parent_id of 0
// moves the node to root; any other value re-runs the child_kinds validation
// against the new parent and rejects cycles (409); omitted or null keeps it.
func (h *SMSHandler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name         *string  `json:"name,omitempty"`
		KindID       *int     `json:"kind_id,omitempty"`
		ParentID     *int     `json:"parent_id"` // 0 moves to root; omitted/null keeps
		Description  *string  `json:"description,omitempty"`
		LocationLat  *float64 `json:"location_lat,omitempty"`
		LocationLon  *float64 `json:"location_lon,omitempty"`
		LocationText *string  `json:"location_text,omitempty"`
		CustomFields *string  `json:"custom_fields,omitempty"`
		EntityType   *string  `json:"entity_type"` // "" clears the link; omitted/null keeps
		EntityID     *int     `json:"entity_id"`   // 0 clears the link; omitted/null keeps
		Notes        *string  `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNode(h.db.QueryRowContext(ctx, `SELECT `+smsNodeColumns+` FROM sms_nodes n WHERE n.id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	name := existing.Name
	if in.Name != nil {
		name = *in.Name
	}
	if name == "" {
		respondBadRequest(w, r, "name must not be empty")
		return
	}

	newKindID := existing.KindID
	if in.KindID != nil && *in.KindID != existing.KindID {
		newKindID = *in.KindID
		var n int
		err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_kinds WHERE id = ?`, newKindID).Scan(&n)
		if isNotFound(err) {
			respondNotFound(w, r, "Node kind not found")
			return
		} else if err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	var kindKey string
	if err := h.db.QueryRowContext(ctx, `SELECT key FROM sms_node_kinds WHERE id = ?`, newKindID).Scan(&kindKey); err != nil {
		respondInternalError(w, r, err)
		return
	}

	newParent := existing.ParentID
	if in.ParentID != nil {
		if *in.ParentID == 0 {
			newParent = nil
		} else {
			v := *in.ParentID
			newParent = &v
		}
	}
	if newParent != nil && smsDerefInt(newParent) != smsDerefInt(existing.ParentID) {
		msg, err := h.smsValidateNodeParent(ctx, *newParent, kindKey)
		if msg != "" {
			respondBadRequest(w, r, msg)
			return
		} else if err != nil {
			respondInternalError(w, r, err)
			return
		}
		// Cycle prevention: walk up from the new parent; reaching this node means
		// the move would make it its own ancestor.
		cur := *newParent
		for i := 0; i < 1000 && cur > 0; i++ {
			var next sql.NullInt64
			err := h.db.QueryRowContext(ctx, `SELECT parent_id FROM sms_nodes WHERE id = ?`, cur).Scan(&next)
			if isNotFound(err) || !next.Valid {
				break
			} else if err != nil {
				respondInternalError(w, r, err)
				return
			}
			if int(next.Int64) == id {
				respondConflict(w, r, "cycle detected in node hierarchy")
				return
			}
			cur = int(next.Int64)
		}
	}

	description := existing.Description
	if in.Description != nil {
		description = *in.Description
	}
	locText := existing.LocationText
	if in.LocationText != nil {
		locText = *in.LocationText
	}
	customFields := existing.CustomFields
	if in.CustomFields != nil {
		if !json.Valid([]byte(*in.CustomFields)) || !strings.HasPrefix(strings.TrimSpace(*in.CustomFields), "{") {
			respondBadRequest(w, r, "custom_fields must be a JSON object")
			return
		}
		customFields = *in.CustomFields
	}
	// location_lat/lon: an explicit zero (including JSON null) keeps the stored
	// value; only non-zero values replace it.
	locLat := existing.LocationLat
	if in.LocationLat != nil && *in.LocationLat != 0 {
		v := *in.LocationLat
		locLat = &v
	}
	locLon := existing.LocationLon
	if in.LocationLon != nil && *in.LocationLon != 0 {
		v := *in.LocationLon
		locLon = &v
	}

	entityType := smsDerefStr(existing.EntityType)
	if in.EntityType != nil {
		entityType = *in.EntityType
	}
	entityID := smsDerefInt(existing.EntityID)
	if in.EntityID != nil {
		entityID = *in.EntityID
	}
	if entityType != "" || entityID > 0 {
		if !h.smsValidateEntityLink(w, r, ctx, entityType, entityID) {
			return
		}
	}

	notes := existing.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE sms_nodes SET name = ?, kind_id = ?, parent_id = ?, description = ?, location_lat = ?, location_lon = ?, location_text = ?, custom_fields = ?, entity_type = ?, entity_id = ?, notes = ? WHERE id = ?`,
		name, newKindID, nullInt(newParent), description, locLat, locLon, locText, customFields,
		nullStringOrEmpty(entityType), smsEntityIDArg(entityID), notes, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	updated, err := h.smsGetNode(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_updated", "sms_node", &updated.ID, updated.Name, map[string]any{"kind_id": newKindID})
	respondJSONOK(w, updated)
}

// DeleteNode removes a node; its children and members cascade via foreign keys.
func (h *SMSHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var name string
	err := h.db.QueryRowContext(ctx, `SELECT name FROM sms_nodes WHERE id = ?`, id).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_nodes WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_deleted", "sms_node", nil, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Node members.
// ---------------------------------------------------------------------------

// ListNodeMembers returns the stakeholder roster for one node with resolved
// names and organizations.
func (h *SMSHandler) ListNodeMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_nodes WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+smsNodeMemberColumns+`, s.name AS name, s.organization AS organization FROM sms_node_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.node_id = ? ORDER BY m.role COLLATE NOCASE, s.name COLLATE NOCASE`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNodeMember, 0)
	for rows.Next() {
		m, err := scanSMSNodeMemberJoined(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// AddNodeMember maps a stakeholder to a node in a kind-validated role. When the
// node's kind defines roles, role must be one of them; otherwise it is stored empty.
func (h *SMSHandler) AddNodeMember(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StakeholderID int     `json:"stakeholder_id"`
		Role          string  `json:"role"`
		StartDate     *string `json:"start_date,omitempty"` // YYYY-MM-DD
		EndDate       *string `json:"end_date,omitempty"`   // YYYY-MM-DD
		Notes         string  `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	var sName string
	err := h.db.QueryRowContext(ctx, `SELECT name FROM sms_stakeholders WHERE id = ?`, in.StakeholderID).Scan(&sName)
	if isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var kindID int
	err = h.db.QueryRowContext(ctx, `SELECT kind_id FROM sms_nodes WHERE id = ?`, id).Scan(&kindID)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT role_key FROM sms_node_kind_roles WHERE kind_id = ? ORDER BY sort_order`, kindID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	allowed := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		allowed = append(allowed, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	role := strings.TrimSpace(in.Role)
	if len(allowed) > 0 {
		okRole := false
		for _, a := range allowed {
			if a == role {
				okRole = true
				break
			}
		}
		if !okRole {
			respondBadRequest(w, r, fmt.Sprintf("role must be one of: %s", strings.Join(allowed, ", ")))
			return
		}
	} else {
		role = ""
	}

	for _, d := range []*string{in.StartDate, in.EndDate} {
		if d != nil && *d != "" && !validSMSDate(*d) {
			respondBadRequest(w, r, "start_date and end_date must be YYYY-MM-DD")
			return
		}
	}

	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_node_members WHERE node_id = ? AND stakeholder_id = ? AND role = ?`, id, in.StakeholderID, role).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "stakeholder is already a member of this node in that role")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_node_members (node_id, stakeholder_id, role, start_date, end_date, notes) VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.StakeholderID, role, in.StartDate, in.EndDate, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	mid64, _ := res.LastInsertId()
	m, err := scanSMSNodeMemberJoined(h.db.QueryRowContext(ctx, `SELECT `+smsNodeMemberColumns+`, s.name AS name, s.organization AS organization FROM sms_node_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.id = ?`, int(mid64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_member_added", "sms_node_member", &m.ID, sName, map[string]any{"node_id": id, "role": role})
	respondJSONCreated(w, m)
}

// RemoveNodeMember deletes one membership row by its own id (route param mid).
func (h *SMSHandler) RemoveNodeMember(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	mid, ok := requireIDParam(w, r, "mid")
	if !ok {
		return
	}
	ctx := r.Context()
	var sName string
	err := h.db.QueryRowContext(ctx, `SELECT s.name FROM sms_node_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.id = ?`, mid).Scan(&sName)
	if isNotFound(err) {
		respondNotFound(w, r, "Node member not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_node_members WHERE id = ?`, mid); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_node_member_removed", "sms_node_member", nil, sName, map[string]any{"member_id": mid})
	w.WriteHeader(http.StatusNoContent)
}
