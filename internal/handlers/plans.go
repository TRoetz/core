// Package handlers: Plans module.
//
// A Plan is a first-class planning container (in the spirit of Jira Plans)
// that groups multiple workspaces/projects together and can additionally
// reference P3M entities (portfolio/programme/project) and SMS stakeholders.
// The module is gated by the global permission models.PermissionPlansView at
// the route level.
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// PlanHandler serves the /api/plans/* endpoints for the Plans module.
type PlanHandler struct {
	db database.Database
}

func NewPlanHandler(db database.Database) *PlanHandler {
	return &PlanHandler{db: db}
}

const planColumns = `id, name, description, status, color, owner_id, start_date, end_date, created_at, updated_at`

var planStatuses = map[string]bool{
	models.PlanStatusDraft:     true,
	models.PlanStatusActive:    true,
	models.PlanStatusCompleted: true,
	models.PlanStatusArchived:  true,
}

var planP3MEntityTypes = map[string]bool{
	models.PlanEntityPortfolio: true,
	models.PlanEntityProgramme: true,
	models.PlanEntityProject:   true,
}

func (h *PlanHandler) scanPlan(row interface{ Scan(...any) error }) (models.Plan, error) {
	var p models.Plan
	var ownerID sql.NullInt64
	var startDate, endDate sql.NullTime
	var createdAt, updatedAt time.Time
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color,
		&ownerID, &startDate, &endDate, &createdAt, &updatedAt)
	if err != nil {
		return p, err
	}
	if ownerID.Valid {
		v := int(ownerID.Int64)
		p.OwnerID = &v
	}
	if startDate.Valid {
		v := startDate.Time
		p.StartDate = &v
	}
	if endDate.Valid {
		v := endDate.Time
		p.EndDate = &v
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return p, nil
}

func (h *PlanHandler) scanPlanWithCounts(rows *sql.Rows, wsCount, p3mCount, shCount *int) (models.Plan, error) {
	var p models.Plan
	var ownerID sql.NullInt64
	var startDate, endDate sql.NullTime
	var createdAt, updatedAt time.Time
	err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color,
		&ownerID, &startDate, &endDate, &createdAt, &updatedAt, wsCount, p3mCount, shCount)
	if err != nil {
		return p, err
	}
	if ownerID.Valid {
		v := int(ownerID.Int64)
		p.OwnerID = &v
	}
	if startDate.Valid {
		v := startDate.Time
		p.StartDate = &v
	}
	if endDate.Valid {
		v := endDate.Time
		p.EndDate = &v
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return p, nil
}

func (h *PlanHandler) listPlansQuery(ctx context.Context) (*sql.Rows, error) {
	return h.db.QueryContext(ctx, `
		SELECT `+planColumns+`,
			(SELECT COUNT(*) FROM plan_workspaces pw WHERE pw.plan_id = plans.id),
			(SELECT COUNT(*) FROM plan_p3m_entities pe WHERE pe.plan_id = plans.id),
			(SELECT COUNT(*) FROM plan_stakeholders ps WHERE ps.plan_id = plans.id)
		FROM plans
		ORDER BY name COLLATE NOCASE`)
}

// ListPlans handles GET /api/plans.
func (h *PlanHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.listPlansQuery(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	plans := make([]models.Plan, 0)
	for rows.Next() {
		var wsCount, p3mCount, shCount int
		p, err := h.scanPlanWithCounts(rows, &wsCount, &p3mCount, &shCount)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		p.WorkspaceCount = wsCount
		p.P3MEntityCount = p3mCount
		p.StakeholderCount = shCount
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, plans)
}

func (h *PlanHandler) getPlanRow(ctx context.Context, id int) (*models.Plan, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+planColumns+` FROM plans WHERE id = ?`, id)
	p, err := h.scanPlan(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlan handles GET /api/plans/{id}.
func (h *PlanHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := h.getPlanRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "plan")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, p)
}

// CreatePlan handles POST /api/plans.
func (h *PlanHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Status      string     `json:"status"`
		Color       string     `json:"color"`
		OwnerID     *int       `json:"owner_id,omitempty"`
		StartDate   *time.Time `json:"start_date,omitempty"`
		EndDate     *time.Time `json:"end_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.Status == "" {
		in.Status = models.PlanStatusActive
	}
	if !planStatuses[in.Status] {
		respondValidationError(w, r, "status must be draft, active, completed or archived")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO plans (name, description, status, color, owner_id, start_date, end_date, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, in.Status, in.Color, nullInt(in.OwnerID),
		nullTimePtr(in.StartDate), nullTimePtr(in.EndDate), now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	p, err := h.getPlanRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, p)
}

// UpdatePlan handles PUT /api/plans/{id}. The body carries the full mutable
// state of the plan.
func (h *PlanHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Status      string     `json:"status"`
		Color       string     `json:"color"`
		OwnerID     *int       `json:"owner_id,omitempty"`
		StartDate   *time.Time `json:"start_date,omitempty"`
		EndDate     *time.Time `json:"end_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if !planStatuses[in.Status] {
		respondValidationError(w, r, "status must be draft, active, completed or archived")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE plans SET name = ?, description = ?, status = ?, color = ?, owner_id = ?, start_date = ?, end_date = ?, updated_at = ?
		WHERE id = ?`,
		in.Name, in.Description, in.Status, in.Color, nullInt(in.OwnerID),
		nullTimePtr(in.StartDate), nullTimePtr(in.EndDate), now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan")
		return
	}
	p, err := h.getPlanRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, p)
}

// DeletePlan handles DELETE /api/plans/{id}.
func (h *PlanHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM plans WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Plan detail (aggregate of a plan and its links)
// ---------------------------------------------------------------------------

func (h *PlanHandler) listPlanWorkspacesRows(ctx context.Context, planID int) (*sql.Rows, error) {
	return h.db.QueryContext(ctx, `
		SELECT pw.plan_id, pw.workspace_id, pw.created_at,
			w.name, w.key,
			COALESCE(i.c, 0)
		FROM plan_workspaces pw
		JOIN workspaces w ON w.id = pw.workspace_id
		LEFT JOIN (SELECT workspace_id, COUNT(*) AS c FROM items GROUP BY workspace_id) i
			ON i.workspace_id = pw.workspace_id
		WHERE pw.plan_id = ?
		ORDER BY w.name COLLATE NOCASE`, planID)
}

func (h *PlanHandler) scanPlanWorkspace(row interface{ Scan(...any) error }) (models.PlanWorkspace, error) {
	var l models.PlanWorkspace
	var createdAt time.Time
	err := row.Scan(&l.PlanID, &l.WorkspaceID, &createdAt, &l.WorkspaceName, &l.WorkspaceKey, &l.ItemCount)
	l.CreatedAt = createdAt
	return l, err
}

func (h *PlanHandler) listPlanP3MEntityRows(ctx context.Context, planID int) (*sql.Rows, error) {
	return h.db.QueryContext(ctx, `
		SELECT pe.plan_id, pe.entity_type, pe.entity_id, pe.created_at,
			CASE pe.entity_type
				WHEN 'portfolio' THEN pf.title
				WHEN 'programme' THEN pg.title
				ELSE pj.title
			END
		FROM plan_p3m_entities pe
		LEFT JOIN p3m_portfolios pf ON pe.entity_type = 'portfolio' AND pf.id = pe.entity_id
		LEFT JOIN p3m_programmes pg ON pe.entity_type = 'programme' AND pg.id = pe.entity_id
		LEFT JOIN p3m_projects pj ON pe.entity_type = 'project' AND pj.id = pe.entity_id
		WHERE pe.plan_id = ?
		ORDER BY pe.entity_type, pe.entity_id`, planID)
}

func (h *PlanHandler) scanPlanP3MEntity(row interface{ Scan(...any) error }) (models.PlanP3MEntity, error) {
	var l models.PlanP3MEntity
	var createdAt time.Time
	var title sql.NullString
	err := row.Scan(&l.PlanID, &l.EntityType, &l.EntityID, &createdAt, &title)
	l.CreatedAt = createdAt
	l.EntityTitle = title.String
	return l, err
}

func (h *PlanHandler) listPlanStakeholderRows(ctx context.Context, planID int) (*sql.Rows, error) {
	return h.db.QueryContext(ctx, `
		SELECT ps.plan_id, ps.stakeholder_id, ps.created_at,
			s.name, s.role_classification, s.organization
		FROM plan_stakeholders ps
		JOIN sms_stakeholders s ON s.id = ps.stakeholder_id
		WHERE ps.plan_id = ?
		ORDER BY s.name COLLATE NOCASE`, planID)
}

func (h *PlanHandler) scanPlanStakeholder(row interface{ Scan(...any) error }) (models.PlanStakeholder, error) {
	var l models.PlanStakeholder
	var createdAt time.Time
	err := row.Scan(&l.PlanID, &l.StakeholderID, &createdAt, &l.Name, &l.RoleClassification, &l.Organization)
	l.CreatedAt = createdAt
	return l, err
}

func scanLinkList[T any](rows *sql.Rows, scan func(interface{ Scan(...any) error }) (T, error)) ([]T, error) {
	out := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetPlanDetail handles GET /api/plans/{id}/detail.
func (h *PlanHandler) GetPlanDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := h.getPlanRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "plan")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	detail := models.PlanDetail{Plan: *p, Workspaces: []models.PlanWorkspace{}, P3MEntities: []models.PlanP3MEntity{}, Stakeholders: []models.PlanStakeholder{}}

	wsRows, err := h.listPlanWorkspacesRows(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	ws, err := scanLinkList(wsRows, h.scanPlanWorkspace)
	wsRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	detail.Workspaces = ws

	p3mRows, err := h.listPlanP3MEntityRows(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	p3m, err := scanLinkList(p3mRows, h.scanPlanP3MEntity)
	p3mRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	detail.P3MEntities = p3m

	shRows, err := h.listPlanStakeholderRows(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	sh, err := scanLinkList(shRows, h.scanPlanStakeholder)
	shRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	detail.Stakeholders = sh

	fRows, err := h.db.QueryContext(r.Context(), `SELECT id, plan_id, name, cql_query, created_at FROM plan_filters WHERE plan_id = ? ORDER BY name COLLATE NOCASE`, id)
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("list plan filters: %w", err))
		return
	}
	filters := make([]models.PlanFilter, 0)
	for fRows.Next() {
		var f models.PlanFilter
		if err := fRows.Scan(&f.ID, &f.PlanID, &f.Name, &f.CQLQuery, &f.CreatedAt); err != nil {
			fRows.Close()
			respondInternalError(w, r, fmt.Errorf("scan plan filter: %w", err))
			return
		}
		filters = append(filters, f)
	}
	err = fRows.Err()
	fRows.Close()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	detail.Filters = filters

	respondJSONOK(w, detail)
}

// ---------------------------------------------------------------------------
// Plan <-> workspaces
// ---------------------------------------------------------------------------

// SetPlanWorkspaces handles PUT /api/plans/{id}/workspaces. The body is
// { "workspace_ids": [ ... ] } — the complete desired set (full replace).
func (h *PlanHandler) SetPlanWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		WorkspaceIDs []int `json:"workspace_ids"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	seen := make(map[int]bool, len(in.WorkspaceIDs))
	for _, wsID := range in.WorkspaceIDs {
		if seen[wsID] {
			continue
		}
		seen[wsID] = true
		var exists bool
		err := h.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = ?)`, wsID).Scan(&exists)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "workspace")
			return
		}
	}

	tx, err := h.db.Begin()
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM plan_workspaces WHERE plan_id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for wsID := range seen {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO plan_workspaces (plan_id, workspace_id, created_at) VALUES (?, ?, ?)`, id, wsID, time.Now().UTC()); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddPlanWorkspace handles POST /api/plans/{id}/workspaces/{workspace_id}.
func (h *PlanHandler) AddPlanWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	wsID, ok := requireIDParam(w, r, "workspace_id")
	if !ok {
		return
	}
	var exists bool
	err := h.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = ?)`, wsID).Scan(&exists)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "workspace")
		return
	}
	res, err := h.db.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO plan_workspaces (plan_id, workspace_id, created_at) VALUES (?, ?, ?)`,
		id, wsID, time.Now().UTC())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemovePlanWorkspace handles DELETE /api/plans/{id}/workspaces/{workspace_id}.
func (h *PlanHandler) RemovePlanWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	wsID, ok := requireIDParam(w, r, "workspace_id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM plan_workspaces WHERE plan_id = ? AND workspace_id = ?`, id, wsID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan_workspace")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Plan <-> P3M entities
// ---------------------------------------------------------------------------

func (h *PlanHandler) validateP3MEntity(ctx context.Context, entityType string, entityID int) (bool, error) {
	table, ok := map[string]string{
		models.PlanEntityPortfolio: "p3m_portfolios",
		models.PlanEntityProgramme: "p3m_programmes",
		models.PlanEntityProject:   "p3m_projects",
	}[entityType]
	if !ok {
		return false, nil
	}
	var exists bool
	err := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = ?)`, entityID).Scan(&exists)
	return exists, err
}

// AddPlanP3MEntity handles POST /api/plans/{id}/p3m-entities.
func (h *PlanHandler) AddPlanP3MEntity(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EntityType string `json:"entity_type"`
		EntityID   int    `json:"entity_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !planP3MEntityTypes[in.EntityType] {
		respondValidationError(w, r, "entity_type must be portfolio, programme or project")
		return
	}
	exists, err := h.validateP3MEntity(r.Context(), in.EntityType, in.EntityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "p3m entity")
		return
	}
	res, err := h.db.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO plan_p3m_entities (plan_id, entity_type, entity_id, created_at) VALUES (?, ?, ?, ?)`,
		id, in.EntityType, in.EntityID, time.Now().UTC())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemovePlanP3MEntity handles DELETE /api/plans/{id}/p3m-entities/{entity_type}/{entity_id}.
func (h *PlanHandler) RemovePlanP3MEntity(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	entityType := r.PathValue("entity_type")
	entityID, ok := requireIDParam(w, r, "entity_id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(),
		`DELETE FROM plan_p3m_entities WHERE plan_id = ? AND entity_type = ? AND entity_id = ?`,
		id, entityType, entityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan_p3m_entity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Plan <-> SMS stakeholders
// ---------------------------------------------------------------------------

// AddPlanStakeholder handles POST /api/plans/{id}/stakeholders.
func (h *PlanHandler) AddPlanStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StakeholderID int `json:"stakeholder_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	var exists bool
	err := h.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM sms_stakeholders WHERE id = ?)`, in.StakeholderID).Scan(&exists)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "stakeholder")
		return
	}
	res, err := h.db.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO plan_stakeholders (plan_id, stakeholder_id, created_at) VALUES (?, ?, ?)`,
		id, in.StakeholderID, time.Now().UTC())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemovePlanStakeholder handles DELETE /api/plans/{id}/stakeholders/{stakeholder_id}.
func (h *PlanHandler) RemovePlanStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	shID, ok := requireIDParam(w, r, "stakeholder_id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM plan_stakeholders WHERE plan_id = ? AND stakeholder_id = ?`, id, shID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "plan_stakeholder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Workspace map (used by Big Gantt for plan grouping)
// ---------------------------------------------------------------------------

// GetWorkspaceMap handles GET /api/plans/workspace-map. Returns the flat
// plan/workspace pairs so the frontend can group items by plan without one
// request per plan.
func (h *PlanHandler) GetWorkspaceMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT pw.plan_id, p.name, pw.workspace_id
		FROM plan_workspaces pw
		JOIN plans p ON p.id = pw.plan_id
		ORDER BY p.name COLLATE NOCASE, pw.workspace_id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type pair struct {
		PlanID      int    `json:"plan_id"`
		PlanName    string `json:"plan_name"`
		WorkspaceID int    `json:"workspace_id"`
	}
	out := make([]pair, 0)
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.PlanID, &p.PlanName, &p.WorkspaceID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetP3MMap handles GET /api/plans/p3m-map. Returns the flat
// plan/P3M-entity pairs so the P3M views can show which plans are linked to
// each portfolio/programme/project.
func (h *PlanHandler) GetP3MMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT pe.plan_id, p.name, p.color, pe.entity_type, pe.entity_id
		FROM plan_p3m_entities pe
		JOIN plans p ON p.id = pe.plan_id
		ORDER BY p.name COLLATE NOCASE, pe.entity_type, pe.entity_id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type pair struct {
		PlanID      int    `json:"plan_id"`
		PlanName    string `json:"plan_name"`
		PlanColor   string `json:"plan_color"`
		EntityType  string `json:"entity_type"`
		EntityID    int    `json:"entity_id"`
	}
	out := make([]pair, 0)
	for rows.Next() {
		var p pair
		var color sql.NullString
		if err := rows.Scan(&p.PlanID, &p.PlanName, &color, &p.EntityType, &p.EntityID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		p.PlanColor = color.String
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}
