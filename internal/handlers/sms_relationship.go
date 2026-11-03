package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Commitments (SRM-1004, Std §21 template 7).
// ---------------------------------------------------------------------------

const smsCommitmentColumns = `id, stakeholder_id, direction, title, description, owner_user_id, due_date, status, escalated, escalation_level, source_communication_id, linked_issue_id, notes, created_at, updated_at`

func scanSMSCommitment(row interface{ Scan(dest ...any) error }) (*models.SMSCommitment, error) {
	c := &models.SMSCommitment{}
	err := row.Scan(&c.ID, &c.StakeholderID, &c.Direction, &c.Title, &c.Description, &c.OwnerUserID, &c.DueDate, &c.Status, &c.Escalated, &c.EscalationLevel, &c.SourceCommunicationID, &c.LinkedIssueID, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListStakeholderCommitments returns the commitment register for one stakeholder.
func (h *SMSHandler) ListStakeholderCommitments(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT c.id, c.stakeholder_id, c.direction, c.title, c.description, c.owner_user_id, c.due_date, c.status, c.escalated, c.escalation_level, c.source_communication_id, c.linked_issue_id, c.notes, c.created_at, c.updated_at, s.name FROM sms_commitments c JOIN sms_stakeholders s ON s.id = c.stakeholder_id WHERE c.stakeholder_id = ? ORDER BY c.due_date IS NULL, c.due_date ASC, c.id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSCommitment, 0)
	for rows.Next() {
		var c models.SMSCommitment
		err := rows.Scan(&c.ID, &c.StakeholderID, &c.Direction, &c.Title, &c.Description, &c.OwnerUserID, &c.DueDate, &c.Status, &c.Escalated, &c.EscalationLevel, &c.SourceCommunicationID, &c.LinkedIssueID, &c.Notes, &c.CreatedAt, &c.UpdatedAt, &c.StakeholderName)
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

// CreateCommitment adds a commitment to the register.
func (h *SMSHandler) CreateCommitment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Direction             string     `json:"direction"`
		Title                 string     `json:"title"`
		Description           string     `json:"description"`
		OwnerUserID           *int       `json:"owner_user_id,omitempty"`
		DueDate               *time.Time `json:"due_date,omitempty"`
		Status                string     `json:"status"`
		SourceCommunicationID *int       `json:"source_communication_id,omitempty"`
		LinkedIssueID         *int       `json:"linked_issue_id,omitempty"`
		Notes                 string     `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		respondBadRequest(w, r, "title is required")
		return
	}
	direction := in.Direction
	if direction == "" {
		direction = models.SMSCommitmentFromThem
	}
	if !smsValidEnum(direction, smsCommitmentDirections...) {
		respondBadRequest(w, r, "invalid direction")
		return
	}
	if in.Status != "" && !smsValidEnum(in.Status, smsCommitmentStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_commitments (stakeholder_id, direction, title, description, owner_user_id, due_date, status, source_communication_id, linked_issue_id, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, direction, in.Title, in.Description, in.OwnerUserID, in.DueDate,
		orKeepStr(in.Status, models.SMSCommitmentOpen), in.SourceCommunicationID, in.LinkedIssueID, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	cid, _ := res.LastInsertId()

	var c models.SMSCommitment
	err = h.db.QueryRowContext(ctx, `SELECT `+smsCommitmentColumns+` FROM sms_commitments WHERE id = ?`, cid).
		Scan(&c.ID, &c.StakeholderID, &c.Direction, &c.Title, &c.Description, &c.OwnerUserID, &c.DueDate, &c.Status, &c.Escalated, &c.EscalationLevel, &c.SourceCommunicationID, &c.LinkedIssueID, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c.StakeholderName = smsStakeholderName(ctx, h.db, id)
	smsAudit(h, r, user, "sms_commitment_created", "sms_commitment", &c.ID, c.Title, nil)
	respondJSONCreated(w, c)
}

// UpdateCommitment applies a partial update to one commitment.
func (h *SMSHandler) UpdateCommitment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "cid")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := scanSMSCommitment(h.db.QueryRowContext(ctx, `SELECT `+smsCommitmentColumns+` FROM sms_commitments WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Commitment not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Direction             *string    `json:"direction,omitempty"`
		Title                 *string    `json:"title,omitempty"`
		Description           *string    `json:"description,omitempty"`
		OwnerUserID           *int       `json:"owner_user_id,omitempty"`
		DueDate               *time.Time `json:"due_date,omitempty"`
		Status                *string    `json:"status,omitempty"`
		Escalated             *bool      `json:"escalated,omitempty"`
		SourceCommunicationID *int       `json:"source_communication_id,omitempty"`
		LinkedIssueID         *int       `json:"linked_issue_id,omitempty"`
		Notes                 *string    `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	title := orKeep(in.Title, existing.Title)
	if title == "" {
		respondBadRequest(w, r, "title cannot be empty")
		return
	}
	if in.Direction != nil && !smsValidEnum(*in.Direction, smsCommitmentDirections...) {
		respondBadRequest(w, r, "invalid direction")
		return
	}
	if in.Status != nil && !smsValidEnum(*in.Status, smsCommitmentStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_commitments SET direction = ?, title = ?, description = ?, owner_user_id = ?, due_date = ?, status = ?, escalated = ?, source_communication_id = ?, linked_issue_id = ?, notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Direction, existing.Direction), title, orKeep(in.Description, existing.Description), intPtrOr(in.OwnerUserID, existing.OwnerUserID),
		timePtrOr(in.DueDate, existing.DueDate), orKeep(in.Status, existing.Status), boolOr(in.Escalated, existing.Escalated),
		intPtrOr(in.SourceCommunicationID, existing.SourceCommunicationID), intPtrOr(in.LinkedIssueID, existing.LinkedIssueID),
		orKeep(in.Notes, existing.Notes), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	c, err := scanSMSCommitment(h.db.QueryRowContext(ctx, `SELECT `+smsCommitmentColumns+` FROM sms_commitments WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c.StakeholderName = smsStakeholderName(ctx, h.db, c.StakeholderID)
	smsAudit(h, r, user, "sms_commitment_updated", "sms_commitment", &c.ID, c.Title, map[string]any{"status": c.Status})
	respondJSONOK(w, c)
}

// DeleteCommitment removes one commitment.
func (h *SMSHandler) DeleteCommitment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "cid")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT title FROM sms_commitments WHERE id = ?`, id).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Commitment not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_commitments WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_commitment_deleted", "sms_commitment", &id, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// EscalateCommitment marks a commitment as escalated and bumps its level.
func (h *SMSHandler) EscalateCommitment(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "cid")
	if !ok {
		return
	}
	ctx := r.Context()

	res, err := h.db.ExecContext(ctx, `UPDATE sms_commitments SET escalated = 1, escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Commitment not found")
		return
	}

	c, err := scanSMSCommitment(h.db.QueryRowContext(ctx, `SELECT `+smsCommitmentColumns+` FROM sms_commitments WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	c.StakeholderName = smsStakeholderName(ctx, h.db, c.StakeholderID)
	smsAudit(h, r, user, "sms_commitment_escalated", "sms_commitment", &c.ID, c.Title, map[string]any{"escalation_level": c.EscalationLevel})
	respondJSONOK(w, c)
}

// intPtrOr resolves a nullable integer update: explicit value wins; nil keeps the stored value.
func intPtrOr(v *int, fallback *int) any {
	if v != nil {
		return *v
	}
	return fallback
}

// timePtrOr resolves a nullable timestamp update: explicit value wins; nil keeps the stored value (which may itself be NULL).
func timePtrOr(v *time.Time, fallback *time.Time) any {
	if v != nil {
		return *v
	}
	return fallback
}

// smsStakeholderName resolves a display name for joined list responses.
func smsStakeholderName(ctx context.Context, db database.Database, id int) string {
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM sms_stakeholders WHERE id = ?`, id).Scan(&name); err != nil {
		return ""
	}
	return name
}

// ---------------------------------------------------------------------------
// Grievances (R3; distinct from general feedback).
// ---------------------------------------------------------------------------

const smsGrievanceColumns = `id, stakeholder_id, title, description, category, severity, status, escalated, escalation_level, acknowledged_at, response_due_at, response, resolution, created_by, created_at, updated_at`

func scanSMSGrievance(row interface{ Scan(dest ...any) error }) (*models.SMSGrievance, error) {
	g := &models.SMSGrievance{}
	err := row.Scan(&g.ID, &g.StakeholderID, &g.Title, &g.Description, &g.Category, &g.Severity, &g.Status, &g.Escalated, &g.EscalationLevel, &g.AcknowledgedAt, &g.ResponseDueAt, &g.Response, &g.Resolution, &g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

// ListStakeholderGrievances returns the grievance log for one stakeholder.
func (h *SMSHandler) ListStakeholderGrievances(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT g.id, g.stakeholder_id, g.title, g.description, g.category, g.severity, g.status, g.escalated, g.escalation_level, g.acknowledged_at, g.response_due_at, g.response, g.resolution, g.created_by, g.created_at, g.updated_at, s.name FROM sms_grievances g JOIN sms_stakeholders s ON s.id = g.stakeholder_id WHERE g.stakeholder_id = ? ORDER BY g.id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSGrievance, 0)
	for rows.Next() {
		var g models.SMSGrievance
		err := rows.Scan(&g.ID, &g.StakeholderID, &g.Title, &g.Description, &g.Category, &g.Severity, &g.Status, &g.Escalated, &g.EscalationLevel, &g.AcknowledgedAt, &g.ResponseDueAt, &g.Response, &g.Resolution, &g.CreatedBy, &g.CreatedAt, &g.UpdatedAt, &g.StakeholderName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateGrievance records a formal grievance.
func (h *SMSHandler) CreateGrievance(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title         string     `json:"title"`
		Description   string     `json:"description"`
		Category      string     `json:"category"`
		Severity      string     `json:"severity"`
		Status        string     `json:"status"`
		ResponseDueAt *time.Time `json:"response_due_at,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		respondBadRequest(w, r, "title is required")
		return
	}
	severity := in.Severity
	if severity == "" {
		severity = models.SMSGrievanceMedium
	}
	if !smsValidEnum(severity, smsGrievanceSeverities...) {
		respondBadRequest(w, r, "invalid severity")
		return
	}
	if in.Status != "" && !smsValidEnum(in.Status, smsGrievanceStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_grievances (stakeholder_id, title, description, category, severity, status, response_due_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Title, in.Description, in.Category, severity,
		orKeepStr(in.Status, models.SMSGrievanceReceived), in.ResponseDueAt, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	gid, _ := res.LastInsertId()

	var g models.SMSGrievance
	err = h.db.QueryRowContext(ctx, `SELECT `+smsGrievanceColumns+` FROM sms_grievances WHERE id = ?`, gid).
		Scan(&g.ID, &g.StakeholderID, &g.Title, &g.Description, &g.Category, &g.Severity, &g.Status, &g.Escalated, &g.EscalationLevel, &g.AcknowledgedAt, &g.ResponseDueAt, &g.Response, &g.Resolution, &g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	g.StakeholderName = smsStakeholderName(ctx, h.db, id)
	smsAudit(h, r, user, "sms_grievance_created", "sms_grievance", &g.ID, g.Title, nil)
	respondJSONCreated(w, g)
}

// UpdateGrievance applies a partial update; moving to acknowledged stamps
// acknowledged_at automatically.
func (h *SMSHandler) UpdateGrievance(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := scanSMSGrievance(h.db.QueryRowContext(ctx, `SELECT `+smsGrievanceColumns+` FROM sms_grievances WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Grievance not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Title         *string    `json:"title,omitempty"`
		Description   *string    `json:"description,omitempty"`
		Category      *string    `json:"category,omitempty"`
		Severity      *string    `json:"severity,omitempty"`
		Status        *string    `json:"status,omitempty"`
		Escalated     *bool      `json:"escalated,omitempty"`
		ResponseDueAt *time.Time `json:"response_due_at,omitempty"`
		Response      *string    `json:"response,omitempty"`
		Resolution    *string    `json:"resolution,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	title := orKeep(in.Title, existing.Title)
	if title == "" {
		respondBadRequest(w, r, "title cannot be empty")
		return
	}
	if in.Severity != nil && !smsValidEnum(*in.Severity, smsGrievanceSeverities...) {
		respondBadRequest(w, r, "invalid severity")
		return
	}
	newStatus := existing.Status
	if in.Status != nil {
		newStatus = *in.Status
	}
	if !smsValidEnum(newStatus, smsGrievanceStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	var ackAt *time.Time
	if newStatus == models.SMSGrievanceAcknowledged && existing.AcknowledgedAt == nil {
		now := time.Now().UTC()
		ackAt = &now
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_grievances SET title = ?, description = ?, category = ?, severity = ?, status = ?, escalated = ?, response_due_at = ?, response = ?, resolution = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		title, orKeep(in.Description, existing.Description), orKeep(in.Category, existing.Category),
		orKeep(in.Severity, existing.Severity), newStatus, boolOr(in.Escalated, existing.Escalated),
		timePtrOr(in.ResponseDueAt, existing.ResponseDueAt), orKeep(in.Response, existing.Response),
		orKeep(in.Resolution, existing.Resolution), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if ackAt != nil {
		if _, err := h.db.ExecContext(ctx, `UPDATE sms_grievances SET acknowledged_at = ? WHERE id = ?`, *ackAt, id); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	g, err := scanSMSGrievance(h.db.QueryRowContext(ctx, `SELECT `+smsGrievanceColumns+` FROM sms_grievances WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	g.StakeholderName = smsStakeholderName(ctx, h.db, g.StakeholderID)
	smsAudit(h, r, user, "sms_grievance_updated", "sms_grievance", &g.ID, g.Title, map[string]any{"status": g.Status})
	respondJSONOK(w, g)
}

// DeleteGrievance removes one grievance record.
func (h *SMSHandler) DeleteGrievance(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT title FROM sms_grievances WHERE id = ?`, id).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Grievance not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_grievances WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_grievance_deleted", "sms_grievance", &id, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// EscalateGrievance marks a grievance as escalated and bumps its level.
func (h *SMSHandler) EscalateGrievance(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	ctx := r.Context()

	res, err := h.db.ExecContext(ctx, `UPDATE sms_grievances SET escalated = 1, escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Grievance not found")
		return
	}

	g, err := scanSMSGrievance(h.db.QueryRowContext(ctx, `SELECT `+smsGrievanceColumns+` FROM sms_grievances WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	g.StakeholderName = smsStakeholderName(ctx, h.db, g.StakeholderID)
	smsAudit(h, r, user, "sms_grievance_escalated", "sms_grievance", &g.ID, g.Title, map[string]any{"escalation_level": g.EscalationLevel})
	respondJSONOK(w, g)
}

// ---------------------------------------------------------------------------
// Decisions (Appendix A: decisions belong in relationship history).
// ---------------------------------------------------------------------------

const smsDecisionColumns = `id, stakeholder_id, title, decision, context, decided_at, decided_by_user_id, entity_type, entity_id, created_at`

func scanSMSDecision(row interface{ Scan(dest ...any) error }) (*models.SMSDecision, error) {
	d := &models.SMSDecision{}
	err := row.Scan(&d.ID, &d.StakeholderID, &d.Title, &d.Decision, &d.Context, &d.DecidedAt, &d.DecidedByUserID, &d.EntityType, &d.EntityID, &d.CreatedAt)
	return d, err
}

// ListStakeholderDecisions returns the decision log for one stakeholder.
func (h *SMSHandler) ListStakeholderDecisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT d.id, d.stakeholder_id, d.title, d.decision, d.context, d.decided_at, d.decided_by_user_id, d.entity_type, d.entity_id, d.created_at, s.name FROM sms_decisions d JOIN sms_stakeholders s ON s.id = d.stakeholder_id WHERE d.stakeholder_id = ? ORDER BY COALESCE(d.decided_at, d.created_at) DESC, d.id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSDecision, 0)
	for rows.Next() {
		var d models.SMSDecision
		err := rows.Scan(&d.ID, &d.StakeholderID, &d.Title, &d.Decision, &d.Context, &d.DecidedAt, &d.DecidedByUserID, &d.EntityType, &d.EntityID, &d.CreatedAt, &d.StakeholderName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateDecision appends an immutable entry to the decision log.
func (h *SMSHandler) CreateDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title      string     `json:"title"`
		Decision   string     `json:"decision"`
		Context    string     `json:"context"`
		DecidedAt  *time.Time `json:"decided_at,omitempty"`
		EntityType *string    `json:"entity_type,omitempty"`
		EntityID   *int       `json:"entity_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		respondBadRequest(w, r, "title is required")
		return
	}
	if in.EntityType != nil && !smsValidEnum(*in.EntityType, models.SMSEntityPortfolio, models.SMSEntityProgramme, models.SMSEntityProject) {
		respondBadRequest(w, r, "invalid entity_type")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_decisions (stakeholder_id, title, decision, context, decided_at, decided_by_user_id, entity_type, entity_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Title, in.Decision, in.Context, in.DecidedAt, user.ID, in.EntityType, in.EntityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	did, _ := res.LastInsertId()

	var d models.SMSDecision
	err = h.db.QueryRowContext(ctx, `SELECT `+smsDecisionColumns+` FROM sms_decisions WHERE id = ?`, did).
		Scan(&d.ID, &d.StakeholderID, &d.Title, &d.Decision, &d.Context, &d.DecidedAt, &d.DecidedByUserID, &d.EntityType, &d.EntityID, &d.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	d.StakeholderName = smsStakeholderName(ctx, h.db, id)
	smsAudit(h, r, user, "sms_decision_created", "sms_decision", &d.ID, d.Title, nil)
	respondJSONCreated(w, d)
}

// DeleteDecision removes one decision log entry.
func (h *SMSHandler) DeleteDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "did")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT title FROM sms_decisions WHERE id = ?`, id).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Decision not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_decisions WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_decision_deleted", "sms_decision", &id, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Stakeholder groups.
// ---------------------------------------------------------------------------

const smsGroupColumns = `id, name, description, created_at`

func scanSMSGroup(row interface{ Scan(dest ...any) error }) (*models.SMSStakeholderGroup, error) {
	g := &models.SMSStakeholderGroup{}
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt)
	return g, err
}

// ListStakeholderGroups returns all groups with member counts.
func (h *SMSHandler) ListStakeholderGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT g.id, g.name, g.description, g.created_at, COUNT(m.stakeholder_id) FROM sms_stakeholder_groups g LEFT JOIN sms_stakeholder_group_members m ON m.group_id = g.id GROUP BY g.id ORDER BY g.name COLLATE NOCASE`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSStakeholderGroup, 0)
	for rows.Next() {
		var g models.SMSStakeholderGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.MemberCount); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	ids := make([]any, 0, len(out))
	ph := make([]string, len(out))
	for i := range out {
		ids = append(ids, out[i].ID)
		ph[i] = "?"
	}
	if len(ids) > 0 {
		members, err := h.db.QueryContext(ctx, `SELECT m.group_id, m.stakeholder_id, s.name FROM sms_stakeholder_group_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.group_id IN (`+joinPlaceholders(ph)+`) ORDER BY s.name COLLATE NOCASE`, ids...)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer members.Close()
		byGroup := make(map[int][]models.SMSStakeholderRef)
		for members.Next() {
			var gid, sid int
			var name string
			if err := members.Scan(&gid, &sid, &name); err != nil {
				respondInternalError(w, r, err)
				return
			}
			byGroup[gid] = append(byGroup[gid], models.SMSStakeholderRef{ID: sid, Name: name})
		}
		if err := members.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}
		for i := range out {
			out[i].Members = byGroup[out[i].ID]
		}
	}
	respondJSONOK(w, out)
}

// CreateGroup creates a group and seeds its membership.
func (h *SMSHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name           string `json:"name"`
		Description    string `json:"description"`
		StakeholderIDs []int  `json:"stakeholder_ids,omitempty"`
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

	ctx := r.Context()
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_stakeholder_groups (name, description) VALUES (?, ?)`, in.Name, in.Description)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			respondConflict(w, r, "A group with this name already exists")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	gid, _ := res.LastInsertId()

	for _, sid := range in.StakeholderIDs {
		if sid <= 0 {
			continue
		}
		if _, err := h.db.ExecContext(ctx, `INSERT OR IGNORE INTO sms_stakeholder_group_members (group_id, stakeholder_id) VALUES (?, ?)`, gid, sid); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	g, err := scanSMSGroup(h.db.QueryRowContext(ctx, `SELECT `+smsGroupColumns+` FROM sms_stakeholder_groups WHERE id = ?`, gid))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var count int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_group_members WHERE group_id = ?`, gid).Scan(&count); err != nil {
		respondInternalError(w, r, err)
		return
	}
	g.MemberCount = count
	smsAudit(h, r, user, "sms_group_created", "sms_stakeholder_group", &g.ID, g.Name, nil)
	respondJSONCreated(w, g)
}

// UpdateGroup renames or re-describes a group.
func (h *SMSHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	var in struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	existing, err := scanSMSGroup(h.db.QueryRowContext(ctx, `SELECT `+smsGroupColumns+` FROM sms_stakeholder_groups WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Group not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	name := orKeep(in.Name, existing.Name)
	if name == "" {
		respondBadRequest(w, r, "name cannot be empty")
		return
	}
	_, err = h.db.ExecContext(ctx, `UPDATE sms_stakeholder_groups SET name = ?, description = ? WHERE id = ?`, name, orKeep(in.Description, existing.Description), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			respondConflict(w, r, "A group with this name already exists")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	g, err := scanSMSGroup(h.db.QueryRowContext(ctx, `SELECT `+smsGroupColumns+` FROM sms_stakeholder_groups WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var count int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_group_members WHERE group_id = ?`, id).Scan(&count); err != nil {
		respondInternalError(w, r, err)
		return
	}
	g.MemberCount = count
	smsAudit(h, r, user, "sms_group_updated", "sms_stakeholder_group", &g.ID, g.Name, nil)
	respondJSONOK(w, g)
}

// DeleteGroup removes a group and its membership rows.
func (h *SMSHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT name FROM sms_stakeholder_groups WHERE id = ?`, id).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Group not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_stakeholder_groups WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_group_deleted", "sms_stakeholder_group", &id, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// AddGroupMember adds a stakeholder to a group.
func (h *SMSHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	gid, ok := requireIDParam(w, r, "gid")
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
	if in.StakeholderID <= 0 {
		respondBadRequest(w, r, "stakeholder_id is required")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE id = ?`, in.StakeholderID).Scan(&exists); err != nil || exists == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_stakeholder_group_members (group_id, stakeholder_id) VALUES (?, ?)`, gid, in.StakeholderID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			respondConflict(w, r, "Stakeholder is already a member of this group")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	mid, _ := res.LastInsertId()

	var m models.SMSStakeholderGroupMember
	if err := h.db.QueryRowContext(ctx, `SELECT m.id, m.group_id, m.stakeholder_id, s.name FROM sms_stakeholder_group_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.id = ?`, mid).Scan(&m.ID, &m.GroupID, &m.StakeholderID, &m.Name); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_member_added", "sms_stakeholder_group", &m.GroupID, m.Name, map[string]any{"stakeholder_id": m.StakeholderID})
	respondJSONCreated(w, m)
}

// RemoveGroupMember removes a stakeholder from a group.
func (h *SMSHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	gid, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	sid, ok := requireIDParam(w, r, "sid")
	if !ok {
		return
	}
	var name string
	err := h.db.QueryRowContext(r.Context(), `SELECT s.name FROM sms_stakeholders s WHERE s.id = ?`, sid).Scan(&name)
	if isNotFound(err) {
		respondNotFound(w, r, "Membership not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_stakeholder_group_members WHERE group_id = ? AND stakeholder_id = ?`, gid, sid)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_member_removed", "sms_stakeholder_group", &sid, name, map[string]any{"group_id": gid})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Stakeholder network links (undirected; stored once with source_id < target_id).
// ---------------------------------------------------------------------------

const smsLinkColumns = `id, source_id, target_id, relation_type, notes, created_at`

func scanSMSLink(row interface{ Scan(dest ...any) error }) (*models.SMSStakeholderLink, error) {
	l := &models.SMSStakeholderLink{}
	err := row.Scan(&l.ID, &l.SourceID, &l.TargetID, &l.RelationType, &l.Notes, &l.CreatedAt)
	return l, err
}

// ListStakeholderLinks returns every link touching one stakeholder.
func (h *SMSHandler) ListStakeholderLinks(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT l.id, l.source_id, l.target_id, l.relation_type, l.notes, l.created_at, s.name, t.name FROM sms_stakeholder_links l JOIN sms_stakeholders s ON s.id = l.source_id JOIN sms_stakeholders t ON t.id = l.target_id WHERE l.source_id = ? OR l.target_id = ? ORDER BY l.id`, id, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSStakeholderLink, 0)
	for rows.Next() {
		var l models.SMSStakeholderLink
		err := rows.Scan(&l.ID, &l.SourceID, &l.TargetID, &l.RelationType, &l.Notes, &l.CreatedAt, &l.SourceName, &l.TargetName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateLink adds a link between two stakeholders. The pair is canonicalised
// so source_id < target_id; duplicate pairs are rejected.
func (h *SMSHandler) CreateLink(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		SourceID     int    `json:"source_id"`
		TargetID     int    `json:"target_id"`
		RelationType string `json:"relation_type"`
		Notes        string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.SourceID <= 0 || in.TargetID <= 0 {
		respondBadRequest(w, r, "source_id and target_id are required")
		return
	}
	if in.SourceID == in.TargetID {
		respondBadRequest(w, r, "a stakeholder cannot be linked to itself")
		return
	}

	ctx := r.Context()
	for _, sid := range []int{in.SourceID, in.TargetID} {
		exists, err := h.smsStakeholderExists(ctx, sid)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "Stakeholder not found")
			return
		}
	}

	src, dst := in.SourceID, in.TargetID
	if src > dst {
		src, dst = dst, src
	}
	var dup int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_links WHERE source_id = ? AND target_id = ?`, src, dst).Scan(&dup); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if dup > 0 {
		respondConflict(w, r, "These stakeholders are already linked")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_stakeholder_links (source_id, target_id, relation_type, notes) VALUES (?, ?, ?, ?)`, src, dst, in.RelationType, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	lid, _ := res.LastInsertId()

	var l models.SMSStakeholderLink
	err = h.db.QueryRowContext(ctx, `SELECT l.id, l.source_id, l.target_id, l.relation_type, l.notes, l.created_at, s.name, t.name FROM sms_stakeholder_links l JOIN sms_stakeholders s ON s.id = l.source_id JOIN sms_stakeholders t ON t.id = l.target_id WHERE l.id = ?`, lid).
		Scan(&l.ID, &l.SourceID, &l.TargetID, &l.RelationType, &l.Notes, &l.CreatedAt, &l.SourceName, &l.TargetName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_link_created", "sms_stakeholder_link", &l.ID, "", map[string]any{"source_id": l.SourceID, "target_id": l.TargetID, "relation_type": l.RelationType})
	respondJSONCreated(w, l)
}

// DeleteLink removes one stakeholder link.
func (h *SMSHandler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "lid")
	if !ok {
		return
	}
	var src, tgt int
	err := h.db.QueryRowContext(r.Context(), `SELECT source_id, target_id FROM sms_stakeholder_links WHERE id = ?`, id).Scan(&src, &tgt)
	if isNotFound(err) {
		respondNotFound(w, r, "Link not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(r.Context(), `DELETE FROM sms_stakeholder_links WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_link_deleted", "sms_stakeholder_link", &id, "", map[string]any{"source_id": src, "target_id": tgt})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Ownership transfers / handover (Std §16).
// ---------------------------------------------------------------------------

const smsTransferColumns = `id, stakeholder_id, from_user_id, to_user_id, reason, history_reviewed, risks_reviewed, commitments_reviewed, notes, completed_at, created_by, created_at`

func scanSMSTransfer(row interface{ Scan(dest ...any) error }) (*models.SMSOwnershipTransfer, error) {
	t := &models.SMSOwnershipTransfer{}
	err := row.Scan(&t.ID, &t.StakeholderID, &t.FromUserID, &t.ToUserID, &t.Reason, &t.HistoryReviewed, &t.RisksReviewed, &t.CommitmentsReviewed, &t.Notes, &t.CompletedAt, &t.CreatedBy, &t.CreatedAt)
	return t, err
}

// smsTransferComplete stamps the transfer as completed and re-points the
// stakeholder's owner to the receiving user.
func (h *SMSHandler) smsTransferComplete(ctx context.Context, db database.Database, t models.SMSOwnershipTransfer) error {
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `UPDATE sms_ownership_transfers SET completed_at = ? WHERE id = ?`, now, t.ID); err != nil {
		return err
	}
	if t.ToUserID != nil {
		if _, err := db.ExecContext(ctx, `UPDATE sms_stakeholders SET owner_user_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, *t.ToUserID, t.StakeholderID); err != nil {
			return err
		}
	}
	return nil
}

// ListStakeholderTransfers returns the handover history for one stakeholder.
func (h *SMSHandler) ListStakeholderTransfers(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT t.id, t.stakeholder_id, t.from_user_id, t.to_user_id, t.reason, t.history_reviewed, t.risks_reviewed, t.commitments_reviewed, t.notes, t.completed_at, t.created_by, t.created_at, s.name FROM sms_ownership_transfers t JOIN sms_stakeholders s ON s.id = t.stakeholder_id WHERE t.stakeholder_id = ? ORDER BY t.id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSOwnershipTransfer, 0)
	for rows.Next() {
		var t models.SMSOwnershipTransfer
		err := rows.Scan(&t.ID, &t.StakeholderID, &t.FromUserID, &t.ToUserID, &t.Reason, &t.HistoryReviewed, &t.RisksReviewed, &t.CommitmentsReviewed, &t.Notes, &t.CompletedAt, &t.CreatedBy, &t.CreatedAt, &t.StakeholderName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateTransfer records a handover. When all three review flags are set the
// transfer completes immediately and ownership moves to the receiving user.
func (h *SMSHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		FromUserID          *int   `json:"from_user_id,omitempty"`
		ToUserID            *int   `json:"to_user_id,omitempty"`
		Reason              string `json:"reason"`
		HistoryReviewed     bool   `json:"history_reviewed"`
		RisksReviewed       bool   `json:"risks_reviewed"`
		CommitmentsReviewed bool   `json:"commitments_reviewed"`
		Notes               string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	var existing models.SMSStakeholder
	err := h.db.QueryRowContext(ctx, `SELECT id, owner_user_id FROM sms_stakeholders WHERE id = ?`, id).Scan(&existing.ID, &existing.OwnerUserID)
	if isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	from := in.FromUserID
	if from == nil {
		from = existing.OwnerUserID
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_ownership_transfers (stakeholder_id, from_user_id, to_user_id, reason, history_reviewed, risks_reviewed, commitments_reviewed, notes, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, from, in.ToUserID, in.Reason, in.HistoryReviewed, in.RisksReviewed, in.CommitmentsReviewed, in.Notes, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	tid, _ := res.LastInsertId()

	var t models.SMSOwnershipTransfer
	err = h.db.QueryRowContext(ctx, `SELECT `+smsTransferColumns+` FROM sms_ownership_transfers WHERE id = ?`, tid).
		Scan(&t.ID, &t.StakeholderID, &t.FromUserID, &t.ToUserID, &t.Reason, &t.HistoryReviewed, &t.RisksReviewed, &t.CommitmentsReviewed, &t.Notes, &t.CompletedAt, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	if t.HistoryReviewed && t.RisksReviewed && t.CommitmentsReviewed {
		if err := h.smsTransferComplete(ctx, h.db, t); err != nil {
			respondInternalError(w, r, err)
			return
		}
		var done *time.Time
		if err := h.db.QueryRowContext(ctx, `SELECT completed_at FROM sms_ownership_transfers WHERE id = ?`, tid).Scan(&done); err == nil {
			t.CompletedAt = done
		}
	}

	t.StakeholderName = smsStakeholderName(ctx, h.db, id)
	smsAudit(h, r, user, "sms_transfer_created", "sms_ownership_transfer", &t.ID, "", map[string]any{"from_user_id": t.FromUserID, "to_user_id": t.ToUserID})
	respondJSONCreated(w, t)
}

// UpdateTransfer toggles the review checklist; completing all three flags
// finalises the handover and moves ownership.
func (h *SMSHandler) UpdateTransfer(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "tid")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := scanSMSTransfer(h.db.QueryRowContext(ctx, `SELECT `+smsTransferColumns+` FROM sms_ownership_transfers WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Transfer not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Reason              *string `json:"reason,omitempty"`
		HistoryReviewed     *bool   `json:"history_reviewed,omitempty"`
		RisksReviewed       *bool   `json:"risks_reviewed,omitempty"`
		CommitmentsReviewed *bool   `json:"commitments_reviewed,omitempty"`
		Notes               *string `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	hist := boolOr(in.HistoryReviewed, existing.HistoryReviewed)
	risk := boolOr(in.RisksReviewed, existing.RisksReviewed)
	comm := boolOr(in.CommitmentsReviewed, existing.CommitmentsReviewed)

	_, err = h.db.ExecContext(ctx, `UPDATE sms_ownership_transfers SET reason = ?, history_reviewed = ?, risks_reviewed = ?, commitments_reviewed = ?, notes = ? WHERE id = ?`,
		orKeep(in.Reason, existing.Reason), hist, risk, comm, orKeep(in.Notes, existing.Notes), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	t, err := scanSMSTransfer(h.db.QueryRowContext(ctx, `SELECT `+smsTransferColumns+` FROM sms_ownership_transfers WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if t.HistoryReviewed && t.RisksReviewed && t.CommitmentsReviewed && t.CompletedAt == nil {
		if err := h.smsTransferComplete(ctx, h.db, *t); err != nil {
			respondInternalError(w, r, err)
			return
		}
		var done time.Time
		if err := h.db.QueryRowContext(ctx, `SELECT completed_at FROM sms_ownership_transfers WHERE id = ?`, id).Scan(&done); err == nil {
			t.CompletedAt = &done
		}
	}

	t.StakeholderName = smsStakeholderName(ctx, h.db, t.StakeholderID)
	smsAudit(h, r, user, "sms_transfer_updated", "sms_ownership_transfer", &t.ID, "", map[string]any{"completed": t.CompletedAt != nil})
	respondJSONOK(w, t)
}

// ---------------------------------------------------------------------------
// Pilot success-measure baselines (Doc 7).
// ---------------------------------------------------------------------------

// ListPilotBaselines returns the configured baseline/target pairs.
func (h *SMSHandler) ListPilotBaselines(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT metric_key, baseline_value, target_value, set_at FROM sms_pilot_baselines ORDER BY metric_key`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSPilotBaseline, 0)
	for rows.Next() {
		var b models.SMSPilotBaseline
		if err := rows.Scan(&b.MetricKey, &b.BaselineValue, &b.TargetValue, &b.SetAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// UpsertPilotBaseline sets or replaces one baseline/target pair.
func (h *SMSHandler) UpsertPilotBaseline(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		MetricKey     string   `json:"metric_key"`
		BaselineValue *float64 `json:"baseline_value,omitempty"`
		TargetValue   *float64 `json:"target_value,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.MetricKey = strings.TrimSpace(in.MetricKey)
	if in.MetricKey == "" {
		respondBadRequest(w, r, "metric_key is required")
		return
	}

	ctx := r.Context()
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_pilot_baselines (metric_key, baseline_value, target_value) VALUES (?, ?, ?) ON CONFLICT(metric_key) DO UPDATE SET baseline_value = excluded.baseline_value, target_value = excluded.target_value`,
		in.MetricKey, in.BaselineValue, in.TargetValue)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	_ = res

	var b models.SMSPilotBaseline
	err = h.db.QueryRowContext(ctx, `SELECT metric_key, baseline_value, target_value, set_at FROM sms_pilot_baselines WHERE metric_key = ?`, in.MetricKey).
		Scan(&b.MetricKey, &b.BaselineValue, &b.TargetValue, &b.SetAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_baseline_upserted", "sms_pilot_baseline", nil, b.MetricKey, map[string]any{"baseline_value": b.BaselineValue, "target_value": b.TargetValue})
	respondJSONOK(w, b)
}
