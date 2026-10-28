// P3M risk/RAID items, stakeholder engagement, communications, resources and
// resource allocations: the risk, stakeholder and resource-management
// perspectives of the portfolio, programme and project management module.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// RAID items (risk / assumption / issue / dependency).
// ---------------------------------------------------------------------------

const p3mRaidColumns = `id, entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, created_at, updated_at`

// scanP3MRaidItem reads one p3m_raid_items row.
func scanP3MRaidItem(row interface{ Scan(...any) error }) (models.P3MRaidItem, error) {
	var item models.P3MRaidItem
	err := row.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.ItemType, &item.Title, &item.Description, &item.ImpactScore, &item.ProbabilityScore, &item.MitigationPlan, &item.OwnerID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

// getRaidItemRow loads one RAID item by id.
func (h *P3MHandler) getRaidItemRow(ctx context.Context, id int) (models.P3MRaidItem, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mRaidColumns+` FROM p3m_raid_items WHERE id = ?`, id)
	return scanP3MRaidItem(row)
}

// p3mValidRaidItemType reports whether itemType is one of the four RAID item types.
func p3mValidRaidItemType(itemType string) bool {
	switch itemType {
	case "risk", "assumption", "issue", "dependency":
		return true
	default:
		return false
	}
}

// p3mValidRaidStatus reports whether status is one of the four RAID item states.
func p3mValidRaidStatus(status string) bool {
	switch status {
	case "open", "mitigating", "closed", "escalated":
		return true
	default:
		return false
	}
}

// p3mValidatePolymorphicEntity checks that a polymorphic owner reference is
// valid and writes an error response when it is not. It returns false after
// writing the response.
func (h *P3MHandler) p3mValidatePolymorphicEntity(ctx context.Context, w http.ResponseWriter, r *http.Request, entityType string, entityID int) bool {
	var resource string
	switch entityType {
	case models.P3MEntityPortfolio:
		resource = "p3m_portfolio"
	case models.P3MEntityProgramme:
		resource = "p3m_programme"
	case models.P3MEntityProject:
		resource = "p3m_project"
	default:
		respondValidationError(w, r, "entity_type must be portfolio, programme or project")
		return false
	}
	if !h.p3mEntityExists(ctx, entityType, entityID) {
		respondNotFound(w, r, resource)
		return false
	}
	return true
}

// ListRaidItems handles GET /api/p3m/raid-items?entity_type=&entity_id=&item_type=&status=.
func (h *P3MHandler) ListRaidItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mRaidColumns + ` FROM p3m_raid_items`
	var conds []string
	var args []any
	if v := r.URL.Query().Get("entity_type"); v != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, v)
	}
	if raw := r.URL.Query().Get("entity_id"); raw != "" {
		v, ok := parseQueryInt(raw)
		if !ok {
			respondBadRequest(w, r, "invalid entity_id")
			return
		}
		conds = append(conds, "entity_id = ?")
		args = append(args, v)
	}
	if v := r.URL.Query().Get("item_type"); v != "" {
		conds = append(conds, "item_type = ?")
		args = append(args, v)
	}
	if v := r.URL.Query().Get("status"); v != "" {
		conds = append(conds, "status = ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY id DESC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]models.P3MRaidItem, 0)
	for rows.Next() {
		item, err := scanP3MRaidItem(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		p3mApplyComputedRaid(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, items)
}

// CreateRaidItem handles POST /api/p3m/raid-items.
func (h *P3MHandler) CreateRaidItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		EntityType       string `json:"entity_type"`
		EntityID         int    `json:"entity_id"`
		ItemType         string `json:"item_type"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		ImpactScore      int    `json:"impact_score"`
		ProbabilityScore int    `json:"probability_score"`
		MitigationPlan   string `json:"mitigation_plan"`
		OwnerID          int    `json:"owner_id"`
		Status           string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if !p3mValidRaidItemType(in.ItemType) {
		respondValidationError(w, r, "item_type must be one of risk, assumption, issue or dependency")
		return
	}
	if in.ImpactScore < 1 || in.ImpactScore > 5 {
		respondValidationError(w, r, "impact_score must be between 1 and 5")
		return
	}
	if in.ProbabilityScore < 1 || in.ProbabilityScore > 5 {
		respondValidationError(w, r, "probability_score must be between 1 and 5")
		return
	}
	var ownerCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, in.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
		respondNotFound(w, r, "user")
		return
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if !p3mValidRaidStatus(in.Status) {
		respondValidationError(w, r, "status must be one of open, mitigating, closed or escalated")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, in.ItemType, in.Title, in.Description, in.ImpactScore, in.ProbabilityScore, in.MitigationPlan, in.OwnerID, in.Status, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	item, err := h.getRaidItemRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	p3mApplyComputedRaid(&item)
	respondJSONCreated(w, item)
}

// UpdateRaidItem handles PUT /api/p3m/raid-items/{id}. The full mutable state
// is replaced; the entity reference may move between tiers but must point at
// an existing entity.
func (h *P3MHandler) UpdateRaidItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EntityType       string `json:"entity_type"`
		EntityID         int    `json:"entity_id"`
		ItemType         string `json:"item_type"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		ImpactScore      int    `json:"impact_score"`
		ProbabilityScore int    `json:"probability_score"`
		MitigationPlan   string `json:"mitigation_plan"`
		OwnerID          int    `json:"owner_id"`
		Status           string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if !p3mValidRaidItemType(in.ItemType) {
		respondValidationError(w, r, "item_type must be one of risk, assumption, issue or dependency")
		return
	}
	if in.ImpactScore < 1 || in.ImpactScore > 5 {
		respondValidationError(w, r, "impact_score must be between 1 and 5")
		return
	}
	if in.ProbabilityScore < 1 || in.ProbabilityScore > 5 {
		respondValidationError(w, r, "probability_score must be between 1 and 5")
		return
	}
	var ownerCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, in.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
		respondNotFound(w, r, "user")
		return
	}
	if !p3mValidRaidStatus(in.Status) {
		respondValidationError(w, r, "status must be one of open, mitigating, closed or escalated")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_raid_items SET entity_type = ?, entity_id = ?, item_type = ?, title = ?, description = ?, impact_score = ?, probability_score = ?, mitigation_plan = ?, owner_id = ?, status = ?, updated_at = ? WHERE id = ?`,
		in.EntityType, in.EntityID, in.ItemType, in.Title, in.Description, in.ImpactScore, in.ProbabilityScore, in.MitigationPlan, in.OwnerID, in.Status, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_raid_item")
		return
	}
	item, err := h.getRaidItemRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	p3mApplyComputedRaid(&item)
	respondJSONOK(w, item)
}

// DeleteRaidItem handles DELETE /api/p3m/raid-items/{id}.
func (h *P3MHandler) DeleteRaidItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_raid_items WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_raid_item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EscalateRaidItem handles POST /api/p3m/raid-items/{id}/escalate. Only
// project-level items can be escalated: a copy is created on the target tier
// (an explicit programme or portfolio, or the item's own parent when none is
// given) and both rows are marked escalated.
func (h *P3MHandler) EscalateRaidItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		TargetEntityID int `json:"target_entity_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	item, err := h.getRaidItemRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_raid_item")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if item.EntityType != models.P3MEntityProject {
		respondBadRequest(w, r, "only project-level items can be escalated")
		return
	}

	var targetType string
	var targetID int
	switch {
	case in.TargetEntityID > 0:
		var n int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_programmes WHERE id = ?`, in.TargetEntityID).Scan(&n); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if n > 0 {
			targetType = models.P3MEntityProgramme
			targetID = in.TargetEntityID
		} else if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_portfolios WHERE id = ?`, in.TargetEntityID).Scan(&n); err != nil {
			respondInternalError(w, r, err)
			return
		} else if n > 0 {
			targetType = models.P3MEntityPortfolio
			targetID = in.TargetEntityID
		} else {
			respondNotFound(w, r, "p3m_programme_or_portfolio")
			return
		}
	default:
		proj, err := h.getProjectRow(r.Context(), item.EntityID)
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		switch {
		case proj.ProgrammeID != nil:
			targetType = models.P3MEntityProgramme
			targetID = *proj.ProgrammeID
		case proj.PortfolioID != nil:
			targetType = models.P3MEntityPortfolio
			targetID = *proj.PortfolioID
		default:
			respondConflict(w, r, "project has no parent to escalate to")
			return
		}
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'escalated', ?, ?)`,
		targetType, targetID, item.ItemType, item.Title, item.Description, item.ImpactScore, item.ProbabilityScore, item.MitigationPlan, item.OwnerID, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	newID64, _ := res.LastInsertId()

	if _, err := h.db.ExecContext(r.Context(), `UPDATE p3m_raid_items SET status = 'escalated', updated_at = ? WHERE id = ?`, now, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	created, err := h.getRaidItemRow(r.Context(), int(newID64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	p3mApplyComputedRaid(&created)
	respondJSONCreated(w, created)
}

// ---------------------------------------------------------------------------
// Stakeholders (power/interest matrix).
// ---------------------------------------------------------------------------

const p3mStakeholderColumns = `id, entity_type, entity_id, name, role_title, power_score, interest_score, notes, created_at, updated_at`

// scanP3MStakeholder reads one p3m_stakeholders row.
func scanP3MStakeholder(row interface{ Scan(...any) error }) (models.P3MStakeholder, error) {
	var s models.P3MStakeholder
	err := row.Scan(&s.ID, &s.EntityType, &s.EntityID, &s.Name, &s.RoleTitle, &s.PowerScore, &s.InterestScore, &s.Notes, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// getStakeholderRow loads one stakeholder by id.
func (h *P3MHandler) getStakeholderRow(ctx context.Context, id int) (models.P3MStakeholder, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mStakeholderColumns+` FROM p3m_stakeholders WHERE id = ?`, id)
	return scanP3MStakeholder(row)
}

// ListStakeholders handles GET /api/p3m/stakeholders?entity_type=&entity_id=.
func (h *P3MHandler) ListStakeholders(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mStakeholderColumns + ` FROM p3m_stakeholders`
	var conds []string
	var args []any
	if v := r.URL.Query().Get("entity_type"); v != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, v)
	}
	if raw := r.URL.Query().Get("entity_id"); raw != "" {
		v, ok := parseQueryInt(raw)
		if !ok {
			respondBadRequest(w, r, "invalid entity_id")
			return
		}
		conds = append(conds, "entity_id = ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY name`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MStakeholder, 0)
	for rows.Next() {
		s, err := scanP3MStakeholder(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		s.Quadrant = p3mStakeholderQuadrant(s.PowerScore, s.InterestScore)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateStakeholder handles POST /api/p3m/stakeholders.
func (h *P3MHandler) CreateStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		EntityType    string `json:"entity_type"`
		EntityID      int    `json:"entity_id"`
		Name          string `json:"name"`
		RoleTitle     string `json:"role_title"`
		PowerScore    int    `json:"power_score"`
		InterestScore int    `json:"interest_score"`
		Notes         string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.PowerScore < 1 || in.PowerScore > 5 {
		respondValidationError(w, r, "power_score must be between 1 and 5")
		return
	}
	if in.InterestScore < 1 || in.InterestScore > 5 {
		respondValidationError(w, r, "interest_score must be between 1 and 5")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_stakeholders (entity_type, entity_id, name, role_title, power_score, interest_score, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, in.Name, in.RoleTitle, in.PowerScore, in.InterestScore, in.Notes, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	s, err := h.getStakeholderRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	s.Quadrant = p3mStakeholderQuadrant(s.PowerScore, s.InterestScore)
	respondJSONCreated(w, s)
}

// UpdateStakeholder handles PUT /api/p3m/stakeholders/{id}. The full mutable
// state is replaced.
func (h *P3MHandler) UpdateStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EntityType    string `json:"entity_type"`
		EntityID      int    `json:"entity_id"`
		Name          string `json:"name"`
		RoleTitle     string `json:"role_title"`
		PowerScore    int    `json:"power_score"`
		InterestScore int    `json:"interest_score"`
		Notes         string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.PowerScore < 1 || in.PowerScore > 5 {
		respondValidationError(w, r, "power_score must be between 1 and 5")
		return
	}
	if in.InterestScore < 1 || in.InterestScore > 5 {
		respondValidationError(w, r, "interest_score must be between 1 and 5")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_stakeholders SET entity_type = ?, entity_id = ?, name = ?, role_title = ?, power_score = ?, interest_score = ?, notes = ?, updated_at = ? WHERE id = ?`,
		in.EntityType, in.EntityID, in.Name, in.RoleTitle, in.PowerScore, in.InterestScore, in.Notes, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_stakeholder")
		return
	}
	s, err := h.getStakeholderRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	s.Quadrant = p3mStakeholderQuadrant(s.PowerScore, s.InterestScore)
	respondJSONOK(w, s)
}

// DeleteStakeholder handles DELETE /api/p3m/stakeholders/{id}.
func (h *P3MHandler) DeleteStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_stakeholders WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_stakeholder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Communications (stakeholder communication log).
// ---------------------------------------------------------------------------

const p3mCommunicationColumns = `id, entity_type, entity_id, date, audience, channel, summary, created_by, created_at`

// scanP3MCommunication reads one p3m_communications row.
func scanP3MCommunication(row interface{ Scan(...any) error }) (models.P3MCommunication, error) {
	var c models.P3MCommunication
	err := row.Scan(&c.ID, &c.EntityType, &c.EntityID, &c.Date, &c.Audience, &c.Channel, &c.Summary, &c.CreatedBy, &c.CreatedAt)
	return c, err
}

// getCommunicationRow loads one communication by id.
func (h *P3MHandler) getCommunicationRow(ctx context.Context, id int) (models.P3MCommunication, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mCommunicationColumns+` FROM p3m_communications WHERE id = ?`, id)
	return scanP3MCommunication(row)
}

// p3mValidChannel reports whether channel is one of the communication channels.
func p3mValidChannel(channel string) bool {
	switch channel {
	case "meeting", "workshop", "consultation", "report", "email", "other":
		return true
	default:
		return false
	}
}

// ListCommunications handles GET /api/p3m/communications?entity_type=&entity_id=.
func (h *P3MHandler) ListCommunications(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mCommunicationColumns + ` FROM p3m_communications`
	var conds []string
	var args []any
	if v := r.URL.Query().Get("entity_type"); v != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, v)
	}
	if raw := r.URL.Query().Get("entity_id"); raw != "" {
		v, ok := parseQueryInt(raw)
		if !ok {
			respondBadRequest(w, r, "invalid entity_id")
			return
		}
		conds = append(conds, "entity_id = ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY date DESC, id DESC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MCommunication, 0)
	for rows.Next() {
		c, err := scanP3MCommunication(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateCommunication handles POST /api/p3m/communications.
func (h *P3MHandler) CreateCommunication(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		EntityType string    `json:"entity_type"`
		EntityID   int       `json:"entity_id"`
		Date       time.Time `json:"date"`
		Audience   string    `json:"audience"`
		Channel    string    `json:"channel"`
		Summary    string    `json:"summary"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if in.Channel == "" {
		in.Channel = "other"
	}
	if !p3mValidChannel(in.Channel) {
		respondValidationError(w, r, "channel must be one of meeting, workshop, consultation, report, email or other")
		return
	}

	date := in.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_communications (entity_type, entity_id, date, audience, channel, summary, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, date, in.Audience, in.Channel, in.Summary, user.ID, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	c, err := h.getCommunicationRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, c)
}

// DeleteCommunication handles DELETE /api/p3m/communications/{id}.
func (h *P3MHandler) DeleteCommunication(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_communications WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_communication")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Resources.
// ---------------------------------------------------------------------------

const p3mResourceColumns = `id, name, user_id, skills, cost_rate, capacity_hours_per_week, created_at, updated_at`

// scanP3MResource reads one p3m_resources row; the skills column is stored as
// a JSON array in TEXT.
func scanP3MResource(row interface{ Scan(...any) error }) (models.P3MResource, error) {
	var res models.P3MResource
	var skills string
	if err := row.Scan(&res.ID, &res.Name, &res.UserID, &skills, &res.CostRate, &res.CapacityHoursPerWeek, &res.CreatedAt, &res.UpdatedAt); err != nil {
		return res, err
	}
	if skills != "" {
		if err := json.Unmarshal([]byte(skills), &res.Skills); err != nil {
			return res, err
		}
	}
	return res, nil
}

// getResourceRow loads one resource by id.
func (h *P3MHandler) getResourceRow(ctx context.Context, id int) (models.P3MResource, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mResourceColumns+` FROM p3m_resources WHERE id = ?`, id)
	return scanP3MResource(row)
}

// p3mMarshalSkills renders a skills list as the JSON TEXT stored in
// p3m_resources.skills; an empty list is stored as "[]".
func p3mMarshalSkills(skills []string) string {
	if len(skills) == 0 {
		return "[]"
	}
	b, err := json.Marshal(skills)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ListResources handles GET /api/p3m/resources.
func (h *P3MHandler) ListResources(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mResourceColumns+` FROM p3m_resources ORDER BY name`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MResource, 0)
	for rows.Next() {
		res, err := scanP3MResource(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateResource handles POST /api/p3m/resources.
func (h *P3MHandler) CreateResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		Name                 string   `json:"name"`
		UserID               *int     `json:"user_id,omitempty"`
		Skills               []string `json:"skills"`
		CostRate             float64  `json:"cost_rate"`
		CapacityHoursPerWeek int      `json:"capacity_hours_per_week"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.UserID != nil {
		var userCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, *in.UserID).Scan(&userCount); err != nil || userCount == 0 {
			respondNotFound(w, r, "user")
			return
		}
	}
	if in.CostRate < 0 {
		respondValidationError(w, r, "cost_rate must be zero or greater")
		return
	}
	if in.CapacityHoursPerWeek < 0 {
		respondValidationError(w, r, "capacity_hours_per_week must be zero or greater")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_resources (name, user_id, skills, cost_rate, capacity_hours_per_week, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Name, nullInt(in.UserID), p3mMarshalSkills(in.Skills), in.CostRate, in.CapacityHoursPerWeek, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	resource, err := h.getResourceRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, resource)
}

// UpdateResource handles PUT /api/p3m/resources/{id}. The full mutable state
// is replaced.
func (h *P3MHandler) UpdateResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name                 string   `json:"name"`
		UserID               *int     `json:"user_id,omitempty"`
		Skills               []string `json:"skills"`
		CostRate             float64  `json:"cost_rate"`
		CapacityHoursPerWeek int      `json:"capacity_hours_per_week"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Name == "" {
		respondValidationError(w, r, "name is required")
		return
	}
	if in.UserID != nil {
		var userCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE id = ?`, *in.UserID).Scan(&userCount); err != nil || userCount == 0 {
			respondNotFound(w, r, "user")
			return
		}
	}
	if in.CostRate < 0 {
		respondValidationError(w, r, "cost_rate must be zero or greater")
		return
	}
	if in.CapacityHoursPerWeek < 0 {
		respondValidationError(w, r, "capacity_hours_per_week must be zero or greater")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_resources SET name = ?, user_id = ?, skills = ?, cost_rate = ?, capacity_hours_per_week = ?, updated_at = ? WHERE id = ?`,
		in.Name, nullInt(in.UserID), p3mMarshalSkills(in.Skills), in.CostRate, in.CapacityHoursPerWeek, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	resource, err := h.getResourceRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, resource)
}

// DeleteResource handles DELETE /api/p3m/resources/{id}. Allocations cascade.
func (h *P3MHandler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_resources WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Resource allocations and capacity.
// ---------------------------------------------------------------------------

const p3mResourceAllocationColumns = `id, resource_id, project_id, start_date, end_date, hours_per_week, booking_type, created_at, updated_at`

// scanP3MResourceAllocation reads one p3m_resource_allocations row.
func scanP3MResourceAllocation(row interface{ Scan(...any) error }) (models.P3MResourceAllocation, error) {
	var a models.P3MResourceAllocation
	err := row.Scan(&a.ID, &a.ResourceID, &a.ProjectID, &a.StartDate, &a.EndDate, &a.HoursPerWeek, &a.BookingType, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// getResourceAllocationRow loads one allocation by id.
func (h *P3MHandler) getResourceAllocationRow(ctx context.Context, id int) (models.P3MResourceAllocation, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mResourceAllocationColumns+` FROM p3m_resource_allocations WHERE id = ?`, id)
	return scanP3MResourceAllocation(row)
}

// p3mValidBookingType reports whether bookingType is soft or hard.
func p3mValidBookingType(bookingType string) bool {
	switch bookingType {
	case "soft", "hard":
		return true
	default:
		return false
	}
}

// ListResourceAllocations handles GET /api/p3m/resource-allocations?resource_id=&project_id=.
func (h *P3MHandler) ListResourceAllocations(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mResourceAllocationColumns + ` FROM p3m_resource_allocations`
	var conds []string
	var args []any
	if raw := r.URL.Query().Get("resource_id"); raw != "" {
		v, ok := parseQueryInt(raw)
		if !ok {
			respondBadRequest(w, r, "invalid resource_id")
			return
		}
		conds = append(conds, "resource_id = ?")
		args = append(args, v)
	}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		v, ok := parseQueryInt(raw)
		if !ok {
			respondBadRequest(w, r, "invalid project_id")
			return
		}
		conds = append(conds, "project_id = ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY start_date ASC, id ASC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MResourceAllocation, 0)
	for rows.Next() {
		a, err := scanP3MResourceAllocation(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateResourceAllocation handles POST /api/p3m/resource-allocations.
func (h *P3MHandler) CreateResourceAllocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		ResourceID   int       `json:"resource_id"`
		ProjectID    *int      `json:"project_id,omitempty"`
		StartDate    time.Time `json:"start_date"`
		EndDate      time.Time `json:"end_date"`
		HoursPerWeek int       `json:"hours_per_week"`
		BookingType  string    `json:"booking_type"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	var resourceCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_resources WHERE id = ?`, in.ResourceID).Scan(&resourceCount); err != nil || resourceCount == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if in.ProjectID == nil {
		respondValidationError(w, r, "project_id is required")
		return
	}
	var projectCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *in.ProjectID).Scan(&projectCount); err != nil || projectCount == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	if in.StartDate.IsZero() {
		respondValidationError(w, r, "start_date is required")
		return
	}
	if in.EndDate.IsZero() {
		respondValidationError(w, r, "end_date is required")
		return
	}
	if !in.EndDate.After(in.StartDate) {
		respondValidationError(w, r, "end_date must be after start_date")
		return
	}
	if in.HoursPerWeek < 0 {
		respondValidationError(w, r, "hours_per_week must be zero or greater")
		return
	}
	if in.BookingType == "" {
		in.BookingType = "soft"
	}
	if !p3mValidBookingType(in.BookingType) {
		respondValidationError(w, r, "booking_type must be soft or hard")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_resource_allocations (resource_id, project_id, start_date, end_date, hours_per_week, booking_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ResourceID, *in.ProjectID, in.StartDate, in.EndDate, in.HoursPerWeek, in.BookingType, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	a, err := h.getResourceAllocationRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, a)
}

// UpdateResourceAllocation handles PUT /api/p3m/resource-allocations/{id}. The
// full mutable state is replaced.
func (h *P3MHandler) UpdateResourceAllocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ResourceID   int       `json:"resource_id"`
		ProjectID    *int      `json:"project_id,omitempty"`
		StartDate    time.Time `json:"start_date"`
		EndDate      time.Time `json:"end_date"`
		HoursPerWeek int       `json:"hours_per_week"`
		BookingType  string    `json:"booking_type"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	var resourceCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_resources WHERE id = ?`, in.ResourceID).Scan(&resourceCount); err != nil || resourceCount == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if in.ProjectID == nil {
		respondValidationError(w, r, "project_id is required")
		return
	}
	var projectCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *in.ProjectID).Scan(&projectCount); err != nil || projectCount == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	if in.StartDate.IsZero() {
		respondValidationError(w, r, "start_date is required")
		return
	}
	if in.EndDate.IsZero() {
		respondValidationError(w, r, "end_date is required")
		return
	}
	if !in.EndDate.After(in.StartDate) {
		respondValidationError(w, r, "end_date must be after start_date")
		return
	}
	if in.HoursPerWeek < 0 {
		respondValidationError(w, r, "hours_per_week must be zero or greater")
		return
	}
	if !p3mValidBookingType(in.BookingType) {
		respondValidationError(w, r, "booking_type must be soft or hard")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_resource_allocations SET resource_id = ?, project_id = ?, start_date = ?, end_date = ?, hours_per_week = ?, booking_type = ?, updated_at = ? WHERE id = ?`,
		in.ResourceID, *in.ProjectID, in.StartDate, in.EndDate, in.HoursPerWeek, in.BookingType, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_resource_allocation")
		return
	}
	a, err := h.getResourceAllocationRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, a)
}

// DeleteResourceAllocation handles DELETE /api/p3m/resource-allocations/{id}.
func (h *P3MHandler) DeleteResourceAllocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_resource_allocations WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_resource_allocation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetResourceCapacity handles GET /api/p3m/resource-capacity?from=YYYY-MM-DD&to=YYYY-MM-DD:
// per-resource, per-calendar-month demand (allocation hours weighted by the
// overlap of each booking with the month) versus capacity. Defaults cover the
// current month through six months ahead.
func (h *P3MHandler) GetResourceCapacity(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	toExclusive := time.Date(now.Year(), now.Month()+7, 1, 0, 0, 0, 0, time.UTC)

	q := r.URL.Query()
	if v := q.Get("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.UTC)
		if err != nil {
			respondBadRequest(w, r, "invalid date")
			return
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, time.UTC)
		if err != nil {
			respondBadRequest(w, r, "invalid date")
			return
		}
		toExclusive = t.AddDate(0, 0, 1) // inclusive day -> exclusive bound
	}

	resources, err := h.db.QueryContext(r.Context(), `SELECT id, name, capacity_hours_per_week FROM p3m_resources ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer resources.Close()
	type resourceRef struct {
		id       int
		name     string
		capacity int
	}
	var refs []resourceRef
	for resources.Next() {
		var ref resourceRef
		if err := resources.Scan(&ref.id, &ref.name, &ref.capacity); err != nil {
			respondInternalError(w, r, err)
			return
		}
		refs = append(refs, ref)
	}
	if err := resources.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	type allocWindow struct {
		start time.Time
		end   time.Time
		hours int
	}
	allocations, err := h.db.QueryContext(r.Context(), `SELECT resource_id, start_date, end_date, hours_per_week FROM p3m_resource_allocations`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer allocations.Close()
	byResource := make(map[int][]allocWindow)
	for allocations.Next() {
		var rid int
		var a allocWindow
		if err := allocations.Scan(&rid, &a.start, &a.end, &a.hours); err != nil {
			respondInternalError(w, r, err)
			return
		}
		byResource[rid] = append(byResource[rid], a)
	}
	if err := allocations.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows := make([]models.P3MResourceCapacityRow, 0)
	for _, ref := range refs {
		ms := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
		for ms.Before(toExclusive) {
			me := time.Date(ms.Year(), ms.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			demand := 0.0
			for _, a := range byResource[ref.id] {
				if overlap := p3mOverlapDays(a.start, a.end, ms, me); overlap > 0 {
					demand += float64(a.hours) * float64(overlap) / 7.0
				}
			}
			capacity := float64(ref.capacity) * (me.Sub(ms).Hours() / 24) / 7.0
			rows = append(rows, models.P3MResourceCapacityRow{
				ResourceID:    ref.id,
				Name:          ref.name,
				Month:         ms.Format("2006-01"),
				DemandHours:   demand,
				CapacityHours: capacity,
				OverAllocated: demand > capacity+1e-9,
			})
			ms = me
		}
	}
	respondJSONOK(w, rows)
}

// p3mOverlapDays returns the number of whole days in the intersection of the
// half-open day ranges [aStart, aEnd) and [bStart, bEnd); all arguments are
// UTC midnight boundaries.
func p3mOverlapDays(aStart, aEnd, bStart, bEnd time.Time) int {
	lo := aStart
	if bStart.After(lo) {
		lo = bStart
	}
	hi := aEnd
	if bEnd.Before(hi) {
		hi = bEnd
	}
	if !hi.After(lo) {
		return 0
	}
	return int(hi.Sub(lo).Hours() / 24)
}
