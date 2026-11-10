package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Closure & handover records (CLS-001): one per project, captured at the close
// stage. Outstanding items auto-populate from open RAID entries at capture;
// approval stamps the acceptance signoff with the acting user and time.
// ---------------------------------------------------------------------------

const p3mClosureRecordColumns = `id, project_id, closure_report_page_id, acceptance_signoff_by, acceptance_signoff_at, outstanding_items, lessons_learned, cost_ownership, handover_to, status, created_at, updated_at`

func scanP3MClosureRecord(row interface{ Scan(...any) error }) (models.P3MClosureRecord, error) {
	var c models.P3MClosureRecord
	err := row.Scan(&c.ID, &c.ProjectID, &c.ClosureReportPageID, &c.AcceptanceSignoffBy, &c.AcceptanceSignoffAt,
		&c.OutstandingItems, &c.LessonsLearned, &c.CostOwnership, &c.HandoverTo, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// p3mOpenRaidTitles returns the open RAID entries for a project as display
// strings ("type: title"), used to auto-populate outstanding items at capture.
func (h *P3MHandler) p3mOpenRaidTitles(ctx context.Context, projectID int) ([]string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT item_type, title FROM p3m_raid_items WHERE entity_type = 'project' AND entity_id = ? AND status IN ('open', 'mitigating', 'escalated') ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var itemType, title string
		if err := rows.Scan(&itemType, &title); err != nil {
			return nil, err
		}
		out = append(out, itemType+": "+title)
	}
	return out, rows.Err()
}

// GetClosureRecord handles GET /api/p3m/projects/{id}/closure-record.
func (h *P3MHandler) GetClosureRecord(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	c, err := scanP3MClosureRecord(h.db.QueryRowContext(r.Context(), `SELECT `+p3mClosureRecordColumns+` FROM p3m_closure_records WHERE project_id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_closure_record")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, c)
}

// CreateClosureRecord handles POST /api/p3m/projects/{id}/closure-record. The
// outstanding items list is captured from the project's open RAID entries at
// this moment; a second capture for the same project returns 409 (use PUT to
// amend).
func (h *P3MHandler) CreateClosureRecord(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ClosureReportPageID *string `json:"closure_report_page_id"`
		LessonsLearned      *string `json:"lessons_learned"`
		CostOwnership       *string `json:"cost_ownership"`
		HandoverTo          *string `json:"handover_to"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	var existing int
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM p3m_closure_records WHERE project_id = ?`, id).Scan(&existing); err == nil {
		respondConflict(w, r, "A closure record already exists for this project")
		return
	}

	openItems, err := h.p3mOpenRaidTitles(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	itemsJSON, _ := json.Marshal(openItems)

	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_closure_records (project_id, closure_report_page_id, outstanding_items, lessons_learned, cost_ownership, handover_to) VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.ClosureReportPageID, string(itemsJSON), in.LessonsLearned, in.CostOwnership, in.HandoverTo)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c, err := scanP3MClosureRecord(h.db.QueryRowContext(ctx, `SELECT `+p3mClosureRecordColumns+` FROM p3m_closure_records WHERE project_id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_closure_record_created", "p3m_closure_record", &c.ID, strconv.Itoa(len(openItems)))
	_ = res
	respondJSONCreated(w, c)
}

// UpdateClosureRecord handles PUT /api/p3m/closure-records/{id}. All fields
// except outstanding items (frozen at capture) are mutable; setting the status
// to approved stamps the acceptance signoff with the acting user and the
// current time when no signoff exists yet.
func (h *P3MHandler) UpdateClosureRecord(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ClosureReportPageID *string `json:"closure_report_page_id"`
		LessonsLearned      *string `json:"lessons_learned"`
		CostOwnership       *string `json:"cost_ownership"`
		HandoverTo          *string `json:"handover_to"`
		Status              string  `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Status {
	case "", "recommended", "approved":
	default:
		respondValidationError(w, r, "status must be recommended or approved")
		return
	}

	row := h.db.QueryRowContext(ctx, `SELECT `+p3mClosureRecordColumns+` FROM p3m_closure_records WHERE id = ?`, id)
	existing, err := scanP3MClosureRecord(row)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_closure_record")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	status := existing.Status
	if in.Status != "" {
		status = in.Status
	}
	signoffBy := existing.AcceptanceSignoffBy
	signoffAt := existing.AcceptanceSignoffAt
	if status == "approved" && signoffBy == nil {
		now := time.Now().UTC()
		signoffBy = &user.ID
		signoffAt = &now
	}

	pageID := in.ClosureReportPageID
	if pageID == nil {
		pageID = existing.ClosureReportPageID
	}
	lessons := in.LessonsLearned
	if lessons == nil {
		lessons = existing.LessonsLearned
	}
	cost := in.CostOwnership
	if cost == nil {
		cost = existing.CostOwnership
	}
	handover := in.HandoverTo
	if handover == nil {
		handover = existing.HandoverTo
	}

	res, err := h.db.ExecContext(ctx, `UPDATE p3m_closure_records SET closure_report_page_id = ?, acceptance_signoff_by = ?, acceptance_signoff_at = ?, lessons_learned = ?, cost_ownership = ?, handover_to = ?, status = ? WHERE id = ?`,
		pageID, signoffBy, signoffAt, lessons, cost, handover, status, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_closure_record")
		return
	}

	c, err := scanP3MClosureRecord(h.db.QueryRowContext(ctx, `SELECT `+p3mClosureRecordColumns+` FROM p3m_closure_records WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	action := "p3m_closure_record_updated"
	if status == "approved" && existing.Status != "approved" {
		action = "p3m_closure_record_approved"
	}
	logAudit(h.db, r, user, action, "p3m_closure_record", &c.ID, status)
	respondJSONOK(w, c)
}
