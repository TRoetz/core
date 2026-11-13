// P3M risk/RAID items, stakeholder engagement, communications, resources and
// resource allocations: the risk, stakeholder and resource-management
// perspectives of the portfolio, programme and project management module.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// RAID items (risk / assumption / issue / dependency).
// ---------------------------------------------------------------------------

const p3mRaidColumns = `id, entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, due_date, item_subtype, decision_subtype, srm_contract_id, item_id, created_at, updated_at`

// scanP3MRaidItem reads one p3m_raid_items row.
func scanP3MRaidItem(row interface{ Scan(...any) error }) (models.P3MRaidItem, error) {
	var item models.P3MRaidItem
	err := row.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.ItemType, &item.Title, &item.Description, &item.ImpactScore, &item.ProbabilityScore, &item.MitigationPlan, &item.OwnerID, &item.Status, &item.DueDate, &item.ItemSubtype, &item.DecisionSubtype, &item.SRMContractID, &item.ItemID, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

// getRaidItemRow loads one RAID item by id.
func (h *P3MHandler) getRaidItemRow(ctx context.Context, id int) (models.P3MRaidItem, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mRaidColumns+` FROM p3m_raid_items WHERE id = ?`, id)
	return scanP3MRaidItem(row)
}

// p3mValidRaidItemType reports whether itemType is one of the RAID item types.
func p3mValidRaidItemType(itemType string) bool {
	switch itemType {
	case "risk", "assumption", "issue", "dependency", models.P3MRAIDItemTypeDecision:
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

// p3mValidRaidItemSubtype reports whether subtype is one of the BRD change and
// adoption risk subtypes (RAID-001); empty means untyped.
func p3mValidRaidItemSubtype(subtype string) bool {
	switch subtype {
	case "", "change_risk", "adoption_risk":
		return true
	default:
		return false
	}
}

// p3mValidDecisionSubtype reports whether subtype is one of the decision and
// approval categories (GOV-007); empty means untyped.
func p3mValidDecisionSubtype(subtype string) bool {
	switch subtype {
	case "", "governance", "funding", "procurement", "plan", "closure":
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
	if v := r.URL.Query().Get("item_subtype"); v != "" {
		conds = append(conds, "item_subtype = ?")
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

// p3mRaidSubtypeArg binds an optional RAID subtype column argument: the API
// accepts an omitted (empty) subtype, which must be stored as NULL to satisfy
// the CHECK constraint - matching rows created by the escalation path.
func p3mRaidSubtypeArg(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// CreateRaidItem handles POST /api/p3m/raid-items.
func (h *P3MHandler) CreateRaidItem(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		EntityType       string     `json:"entity_type"`
		EntityID         int        `json:"entity_id"`
		ItemType         string     `json:"item_type"`
		Title            string     `json:"title"`
		Description      string     `json:"description"`
		ImpactScore      int        `json:"impact_score"`
		ProbabilityScore int        `json:"probability_score"`
		MitigationPlan   string     `json:"mitigation_plan"`
		OwnerID          int        `json:"owner_id"`
		Status           string     `json:"status"`
		DueDate          *time.Time `json:"due_date,omitempty"`
		ItemSubtype      string     `json:"item_subtype,omitempty"`
		DecisionSubtype  string     `json:"decision_subtype,omitempty"`
		SRMContractID    *int       `json:"srm_contract_id,omitempty"`
		ItemID           *int       `json:"item_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if !p3mValidRaidItemType(in.ItemType) {
		respondValidationError(w, r, "item_type must be one of risk, assumption, issue, dependency or decision")
		return
	}
	if !p3mValidRaidItemSubtype(in.ItemSubtype) {
		respondValidationError(w, r, "item_subtype must be change_risk or adoption_risk")
		return
	}
	if !p3mValidDecisionSubtype(in.DecisionSubtype) {
		respondValidationError(w, r, "decision_subtype must be one of governance, funding, procurement, plan or closure")
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
	if in.SRMContractID != nil {
		var contractCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_contracts WHERE id = ?`, *in.SRMContractID).Scan(&contractCount); err != nil || contractCount == 0 {
			respondNotFound(w, r, "srm_contract")
			return
		}
	}
	if in.ItemID != nil {
		var itemCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM items WHERE id = ?`, *in.ItemID).Scan(&itemCount); err != nil || itemCount == 0 {
			respondNotFound(w, r, "item")
			return
		}
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if !p3mValidRaidStatus(in.Status) {
		respondValidationError(w, r, "status must be one of open, mitigating, closed or escalated")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, due_date, item_subtype, decision_subtype, srm_contract_id, item_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, in.ItemType, in.Title, in.Description, in.ImpactScore, in.ProbabilityScore, in.MitigationPlan, in.OwnerID, in.Status, nullTimePtr(in.DueDate), p3mRaidSubtypeArg(in.ItemSubtype), p3mRaidSubtypeArg(in.DecisionSubtype), in.SRMContractID, in.ItemID, now, now)
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
	h.p3mAudit(r, user, "p3m.raid.create", "p3m_raid_item", &item.ID, item.Title, map[string]any{
		"entity":    in.EntityType + "#" + strconv.Itoa(in.EntityID),
		"item_type": in.ItemType,
		"status":    in.Status,
	})
	respondJSONCreated(w, item)
}

// UpdateRaidItem handles PUT /api/p3m/raid-items/{id}. The full mutable state
// is replaced; the entity reference may move between tiers but must point at
// an existing entity.
func (h *P3MHandler) UpdateRaidItem(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldItem, err := h.getRaidItemRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_raid_item")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var in struct {
		EntityType       string     `json:"entity_type"`
		EntityID         int        `json:"entity_id"`
		ItemType         string     `json:"item_type"`
		Title            string     `json:"title"`
		Description      string     `json:"description"`
		ImpactScore      int        `json:"impact_score"`
		ProbabilityScore int        `json:"probability_score"`
		MitigationPlan   string     `json:"mitigation_plan"`
		OwnerID          int        `json:"owner_id"`
		Status           string     `json:"status"`
		DueDate          *time.Time `json:"due_date,omitempty"`
		ItemSubtype      string     `json:"item_subtype,omitempty"`
		DecisionSubtype  string     `json:"decision_subtype,omitempty"`
		SRMContractID    *int       `json:"srm_contract_id,omitempty"`
		ItemID           *int       `json:"item_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !h.p3mValidatePolymorphicEntity(r.Context(), w, r, in.EntityType, in.EntityID) {
		return
	}
	if !p3mValidRaidItemType(in.ItemType) {
		respondValidationError(w, r, "item_type must be one of risk, assumption, issue, dependency or decision")
		return
	}
	if !p3mValidRaidItemSubtype(in.ItemSubtype) {
		respondValidationError(w, r, "item_subtype must be change_risk or adoption_risk")
		return
	}
	if !p3mValidDecisionSubtype(in.DecisionSubtype) {
		respondValidationError(w, r, "decision_subtype must be one of governance, funding, procurement, plan or closure")
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
	if in.SRMContractID != nil {
		var contractCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_contracts WHERE id = ?`, *in.SRMContractID).Scan(&contractCount); err != nil || contractCount == 0 {
			respondNotFound(w, r, "srm_contract")
			return
		}
	}
	if in.ItemID != nil {
		var itemCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM items WHERE id = ?`, *in.ItemID).Scan(&itemCount); err != nil || itemCount == 0 {
			respondNotFound(w, r, "item")
			return
		}
	}
	if !p3mValidRaidStatus(in.Status) {
		respondValidationError(w, r, "status must be one of open, mitigating, closed or escalated")
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_raid_items SET entity_type = ?, entity_id = ?, item_type = ?, title = ?, description = ?, impact_score = ?, probability_score = ?, mitigation_plan = ?, owner_id = ?, status = ?, due_date = ?, item_subtype = ?, decision_subtype = ?, srm_contract_id = ?, item_id = ?, updated_at = ? WHERE id = ?`,
		in.EntityType, in.EntityID, in.ItemType, in.Title, in.Description, in.ImpactScore, in.ProbabilityScore, in.MitigationPlan, in.OwnerID, in.Status, nullTimePtr(in.DueDate), p3mRaidSubtypeArg(in.ItemSubtype), p3mRaidSubtypeArg(in.DecisionSubtype), in.SRMContractID, in.ItemID, now, id)
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
	h.p3mAudit(r, user, "p3m.raid.update", "p3m_raid_item", &item.ID, item.Title, p3mAuditDiff(
		[3]any{"entity", oldItem.EntityType+"#"+strconv.Itoa(oldItem.EntityID), in.EntityType+"#"+strconv.Itoa(in.EntityID)},
		[3]any{"item_type", oldItem.ItemType, in.ItemType},
		[3]any{"title", oldItem.Title, in.Title},
		[3]any{"description", oldItem.Description, in.Description},
		[3]any{"impact_score", oldItem.ImpactScore, in.ImpactScore},
		[3]any{"probability_score", oldItem.ProbabilityScore, in.ProbabilityScore},
		[3]any{"mitigation_plan", oldItem.MitigationPlan, in.MitigationPlan},
		[3]any{"owner_id", oldItem.OwnerID, in.OwnerID},
		[3]any{"status", oldItem.Status, in.Status},
		[3]any{"due_date", nullTimeStr(oldItem.DueDate), nullTimeStr(in.DueDate)},
		[3]any{"item_subtype", oldItem.ItemSubtype, in.ItemSubtype},
		[3]any{"decision_subtype", oldItem.DecisionSubtype, in.DecisionSubtype},
		[3]any{"srm_contract_id", nullInt(oldItem.SRMContractID), nullInt(in.SRMContractID)},
		[3]any{"item_id", nullInt(oldItem.ItemID), nullInt(in.ItemID)},
	))
	respondJSONOK(w, item)
}

// DeleteRaidItem handles DELETE /api/p3m/raid-items/{id}.
func (h *P3MHandler) DeleteRaidItem(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldItem, err := h.getRaidItemRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_raid_item")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	h.p3mAudit(r, user, "p3m.raid.delete", "p3m_raid_item", &id, oldItem.Title, map[string]any{
		"entity":    oldItem.EntityType + "#" + strconv.Itoa(oldItem.EntityID),
		"item_type": oldItem.ItemType,
	})
	w.WriteHeader(http.StatusNoContent)
}

// EscalateRaidItem handles POST /api/p3m/raid-items/{id}/escalate. Only
// project-level items can be escalated: a copy is created on the target tier
// (an explicit programme or portfolio, or the item's own parent when none is
// given) and both rows are marked escalated.
func (h *P3MHandler) EscalateRaidItem(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
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
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, item_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'escalated', ?, ?, ?)`,
		targetType, targetID, item.ItemType, item.Title, item.Description, item.ImpactScore, item.ProbabilityScore, item.MitigationPlan, item.OwnerID, item.ItemID, now, now)
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
	h.p3mAudit(r, user, "p3m.raid.escalate", "p3m_raid_item", &created.ID, created.Title, map[string]any{
		"source_entity": item.EntityType + "#" + strconv.Itoa(item.EntityID),
		"target_entity": targetType + "#" + strconv.Itoa(targetID),
	})
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
	user, ok := RequireAuth(w, r)
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
	h.p3mAudit(r, user, "p3m.stakeholder.create", "p3m_stakeholder", &s.ID, s.Name, map[string]any{
		"entity":     in.EntityType + "#" + strconv.Itoa(in.EntityID),
		"role_title": in.RoleTitle,
	})
	respondJSONCreated(w, s)
}

// UpdateStakeholder handles PUT /api/p3m/stakeholders/{id}. The full mutable
// state is replaced.
func (h *P3MHandler) UpdateStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldS, err := h.getStakeholderRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_stakeholder")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	h.p3mAudit(r, user, "p3m.stakeholder.update", "p3m_stakeholder", &s.ID, s.Name, p3mAuditDiff(
		[3]any{"entity", oldS.EntityType+"#"+strconv.Itoa(oldS.EntityID), in.EntityType+"#"+strconv.Itoa(in.EntityID)},
		[3]any{"name", oldS.Name, in.Name},
		[3]any{"role_title", oldS.RoleTitle, in.RoleTitle},
		[3]any{"power_score", oldS.PowerScore, in.PowerScore},
		[3]any{"interest_score", oldS.InterestScore, in.InterestScore},
		[3]any{"notes", oldS.Notes, in.Notes},
	))
	respondJSONOK(w, s)
}

// DeleteStakeholder handles DELETE /api/p3m/stakeholders/{id}.
func (h *P3MHandler) DeleteStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldS, err := h.getStakeholderRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_stakeholder")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	h.p3mAudit(r, user, "p3m.stakeholder.delete", "p3m_stakeholder", &id, oldS.Name, map[string]any{
		"entity":     oldS.EntityType + "#" + strconv.Itoa(oldS.EntityID),
		"role_title": oldS.RoleTitle,
	})
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
	name := strings.TrimSpace(in.Summary)
	if name == "" {
		name = in.Channel + " communication"
	}
	h.p3mAudit(r, user, "p3m.communication.create", "p3m_communication", &c.ID, name, map[string]any{
		"entity":  in.EntityType + "#" + strconv.Itoa(in.EntityID),
		"date":    c.Date.Format("2006-01-02"),
		"channel": in.Channel,
	})
	respondJSONCreated(w, c)
}

// DeleteCommunication handles DELETE /api/p3m/communications/{id}.
func (h *P3MHandler) DeleteCommunication(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldC, err := h.getCommunicationRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_communication")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	name := strings.TrimSpace(oldC.Summary)
	if name == "" {
		name = oldC.Channel + " communication"
	}
	h.p3mAudit(r, user, "p3m.communication.delete", "p3m_communication", &id, name, map[string]any{
		"entity":  oldC.EntityType + "#" + strconv.Itoa(oldC.EntityID),
		"date":    oldC.Date.Format("2006-01-02"),
		"channel": oldC.Channel,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Resources.
// ---------------------------------------------------------------------------

const p3mResourceColumns = `id, name, user_id, skills, cost_rate, capacity_hours_per_week, pool, created_at, updated_at`

// scanP3MResource reads one p3m_resources row; the skills column is stored as
// a JSON array in TEXT.
func scanP3MResource(row interface{ Scan(...any) error }) (models.P3MResource, error) {
	var res models.P3MResource
	var skills string
	if err := row.Scan(&res.ID, &res.Name, &res.UserID, &skills, &res.CostRate, &res.CapacityHoursPerWeek, &res.Pool, &res.CreatedAt, &res.UpdatedAt); err != nil {
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
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name                 string   `json:"name"`
		UserID               *int     `json:"user_id,omitempty"`
		Skills               []string `json:"skills"`
		CostRate             float64  `json:"cost_rate"`
		CapacityHoursPerWeek int      `json:"capacity_hours_per_week"`
		Pool                 string   `json:"pool,omitempty"`
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
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_resources (name, user_id, skills, cost_rate, capacity_hours_per_week, pool, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, nullInt(in.UserID), p3mMarshalSkills(in.Skills), in.CostRate, in.CapacityHoursPerWeek, in.Pool, now, now)
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
	h.p3mAudit(r, user, "p3m.resource.create", "p3m_resource", &resource.ID, resource.Name, map[string]any{
		"cost_rate":               in.CostRate,
		"capacity_hours_per_week": in.CapacityHoursPerWeek,
		"pool":                    in.Pool,
	})
	respondJSONCreated(w, resource)
}

// UpdateResource handles PUT /api/p3m/resources/{id}. The full mutable state
// is replaced.
func (h *P3MHandler) UpdateResource(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldRes, err := h.getResourceRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var in struct {
		Name                 string   `json:"name"`
		UserID               *int     `json:"user_id,omitempty"`
		Skills               []string `json:"skills"`
		CostRate             float64  `json:"cost_rate"`
		CapacityHoursPerWeek int      `json:"capacity_hours_per_week"`
		Pool                 string   `json:"pool,omitempty"`
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
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_resources SET name = ?, user_id = ?, skills = ?, cost_rate = ?, capacity_hours_per_week = ?, pool = ?, updated_at = ? WHERE id = ?`,
		in.Name, nullInt(in.UserID), p3mMarshalSkills(in.Skills), in.CostRate, in.CapacityHoursPerWeek, in.Pool, now, id)
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
	h.p3mAudit(r, user, "p3m.resource.update", "p3m_resource", &resource.ID, resource.Name, p3mAuditDiff(
		[3]any{"name", oldRes.Name, in.Name},
		[3]any{"user_id", nullInt(oldRes.UserID), nullInt(in.UserID)},
		[3]any{"skills", p3mMarshalSkills(oldRes.Skills), p3mMarshalSkills(in.Skills)},
		[3]any{"cost_rate", oldRes.CostRate, in.CostRate},
		[3]any{"capacity_hours_per_week", oldRes.CapacityHoursPerWeek, in.CapacityHoursPerWeek},
		[3]any{"pool", oldRes.Pool, in.Pool},
	))
	respondJSONOK(w, resource)
}

// DeleteResource handles DELETE /api/p3m/resources/{id}. Allocations cascade.
func (h *P3MHandler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldRes, err := h.getResourceRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	h.p3mAudit(r, user, "p3m.resource.delete", "p3m_resource", &id, oldRes.Name, map[string]any{
		"cost_rate":               oldRes.CostRate,
		"capacity_hours_per_week": oldRes.CapacityHoursPerWeek,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Resource allocations and capacity.
// ---------------------------------------------------------------------------

const p3mResourceAllocationColumns = `id, resource_id, project_id, start_date, end_date, hours_per_week, booking_type, allocation_class, created_at, updated_at`

// scanP3MResourceAllocation reads one p3m_resource_allocations row.
func scanP3MResourceAllocation(row interface{ Scan(...any) error }) (models.P3MResourceAllocation, error) {
	var a models.P3MResourceAllocation
	err := row.Scan(&a.ID, &a.ResourceID, &a.ProjectID, &a.StartDate, &a.EndDate, &a.HoursPerWeek, &a.BookingType, &a.AllocationClass, &a.CreatedAt, &a.UpdatedAt)
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

// p3mValidAllocationClass reports whether allocationClass is project or bau;
// bau rows count against capacity but not any project (RES-004).
func p3mValidAllocationClass(allocClass string) bool {
	switch allocClass {
	case "project", "bau":
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
	if v := r.URL.Query().Get("allocation_class"); v != "" {
		conds = append(conds, "allocation_class = ?")
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
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		ResourceID      int       `json:"resource_id"`
		ProjectID       *int      `json:"project_id,omitempty"`
		StartDate       time.Time `json:"start_date"`
		EndDate         time.Time `json:"end_date"`
		HoursPerWeek    int       `json:"hours_per_week"`
		BookingType     string    `json:"booking_type"`
		AllocationClass string    `json:"allocation_class,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.AllocationClass == "" {
		in.AllocationClass = "project"
	}
	if !p3mValidAllocationClass(in.AllocationClass) {
		respondValidationError(w, r, "allocation_class must be project or bau")
		return
	}
	var resourceCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_resources WHERE id = ?`, in.ResourceID).Scan(&resourceCount); err != nil || resourceCount == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if in.AllocationClass == "bau" && in.ProjectID != nil {
		respondValidationError(w, r, "project_id must not be set for bau allocations")
		return
	}
	if in.AllocationClass == "project" {
		if in.ProjectID == nil {
			respondValidationError(w, r, "project_id is required")
			return
		}
		var projectCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *in.ProjectID).Scan(&projectCount); err != nil || projectCount == 0 {
			respondNotFound(w, r, "p3m_project")
			return
		}
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
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_resource_allocations (resource_id, project_id, start_date, end_date, hours_per_week, booking_type, allocation_class, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ResourceID, nullInt(in.ProjectID), in.StartDate, in.EndDate, in.HoursPerWeek, in.BookingType, in.AllocationClass, now, now)
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
	target := "BAU"
	if in.ProjectID != nil {
		target = "project " + strconv.Itoa(*in.ProjectID)
	}
	h.p3mAudit(r, user, "p3m.resource_allocation.create", "p3m_resource_allocation", &a.ID,
		"resource "+strconv.Itoa(in.ResourceID)+" -> "+target, map[string]any{
			"start_date":       a.StartDate.Format("2006-01-02"),
			"end_date":         a.EndDate.Format("2006-01-02"),
			"hours_per_week":   in.HoursPerWeek,
			"booking_type":     in.BookingType,
			"allocation_class": in.AllocationClass,
		})
	respondJSONCreated(w, a)
}

// UpdateResourceAllocation handles PUT /api/p3m/resource-allocations/{id}. The
// full mutable state is replaced.
func (h *P3MHandler) UpdateResourceAllocation(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldA, err := h.getResourceAllocationRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_resource_allocation")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var in struct {
		ResourceID      int       `json:"resource_id"`
		ProjectID       *int      `json:"project_id,omitempty"`
		StartDate       time.Time `json:"start_date"`
		EndDate         time.Time `json:"end_date"`
		HoursPerWeek    int       `json:"hours_per_week"`
		BookingType     string    `json:"booking_type"`
		AllocationClass string    `json:"allocation_class,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.AllocationClass == "" {
		in.AllocationClass = "project"
	}
	if !p3mValidAllocationClass(in.AllocationClass) {
		respondValidationError(w, r, "allocation_class must be project or bau")
		return
	}
	var resourceCount int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_resources WHERE id = ?`, in.ResourceID).Scan(&resourceCount); err != nil || resourceCount == 0 {
		respondNotFound(w, r, "p3m_resource")
		return
	}
	if in.AllocationClass == "bau" && in.ProjectID != nil {
		respondValidationError(w, r, "project_id must not be set for bau allocations")
		return
	}
	if in.AllocationClass == "project" {
		if in.ProjectID == nil {
			respondValidationError(w, r, "project_id is required")
			return
		}
		var projectCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, *in.ProjectID).Scan(&projectCount); err != nil || projectCount == 0 {
			respondNotFound(w, r, "p3m_project")
			return
		}
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
	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_resource_allocations SET resource_id = ?, project_id = ?, start_date = ?, end_date = ?, hours_per_week = ?, booking_type = ?, allocation_class = ?, updated_at = ? WHERE id = ?`,
		in.ResourceID, nullInt(in.ProjectID), in.StartDate, in.EndDate, in.HoursPerWeek, in.BookingType, in.AllocationClass, now, id)
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
	target := "BAU"
	if a.ProjectID != nil {
		target = "project " + strconv.Itoa(*a.ProjectID)
	}
	h.p3mAudit(r, user, "p3m.resource_allocation.update", "p3m_resource_allocation", &a.ID,
		"resource "+strconv.Itoa(a.ResourceID)+" -> "+target, p3mAuditDiff(
			[3]any{"resource_id", oldA.ResourceID, in.ResourceID},
			[3]any{"project_id", nullInt(oldA.ProjectID), nullInt(in.ProjectID)},
			[3]any{"start_date", oldA.StartDate.Format("2006-01-02"), in.StartDate.Format("2006-01-02")},
			[3]any{"end_date", oldA.EndDate.Format("2006-01-02"), in.EndDate.Format("2006-01-02")},
			[3]any{"hours_per_week", oldA.HoursPerWeek, in.HoursPerWeek},
			[3]any{"booking_type", oldA.BookingType, in.BookingType},
			[3]any{"allocation_class", oldA.AllocationClass, in.AllocationClass},
		))
	respondJSONOK(w, a)
}

// DeleteResourceAllocation handles DELETE /api/p3m/resource-allocations/{id}.
func (h *P3MHandler) DeleteResourceAllocation(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	oldA, err := h.getResourceAllocationRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_resource_allocation")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
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
	h.p3mAudit(r, user, "p3m.resource_allocation.delete", "p3m_resource_allocation", &id,
		"resource "+strconv.Itoa(oldA.ResourceID)+" -> project "+nullIntStr(oldA.ProjectID), map[string]any{
			"start_date":     oldA.StartDate.Format("2006-01-02"),
			"end_date":       oldA.EndDate.Format("2006-01-02"),
			"hours_per_week": oldA.HoursPerWeek,
			"booking_type":   oldA.BookingType,
		})
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
	allocations, err := h.db.QueryContext(r.Context(), `SELECT resource_id, start_date, end_date, hours_per_week, allocation_class FROM p3m_resource_allocations`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer allocations.Close()
	byResource := make(map[int][]allocWindow)
	bauByResource := make(map[int][]allocWindow)
	for allocations.Next() {
		var rid int
		var a allocWindow
		var allocClass string
		if err := allocations.Scan(&rid, &a.start, &a.end, &a.hours, &allocClass); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if allocClass == "bau" {
			bauByResource[rid] = append(bauByResource[rid], a)
		} else {
			byResource[rid] = append(byResource[rid], a)
		}
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
			bauDemand := 0.0
			for _, a := range byResource[ref.id] {
				if overlap := p3mOverlapDays(a.start, a.end, ms, me); overlap > 0 {
					demand += float64(a.hours) * float64(overlap) / 7.0
				}
			}
			for _, a := range bauByResource[ref.id] {
				if overlap := p3mOverlapDays(a.start, a.end, ms, me); overlap > 0 {
					bauDemand += float64(a.hours) * float64(overlap) / 7.0
				}
			}
			demand += bauDemand // BAU consumes capacity but no project (RES-004)
			capacity := float64(ref.capacity) * (me.Sub(ms).Hours() / 24) / 7.0
			rows = append(rows, models.P3MResourceCapacityRow{
				ResourceID:    ref.id,
				Name:          ref.name,
				Month:         ms.Format("2006-01"),
				DemandHours:   demand,
				BauHours:      bauDemand,
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
