// SMS RACI matrices and work packages (roadworks scenario): versioned R/A/C/I
// activity matrices scoped to P3M entities, plus a work-package register tying
// performing contractors to impacted nodes. Same conventions as sms.go /
// sms_subresources.go: global sms.view gate at route level, authenticated user
// for every write, audit trail on mutations.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

const smsRaciColumns = `id, entity_type, entity_id, version, status, created_by, agreed_at, last_reviewed_at, created_at`

func scanSMSRaci(row interface{ Scan(dest ...any) error }) (*models.SMSCRACI, error) {
	m := &models.SMSCRACI{}
	err := row.Scan(&m.ID, &m.EntityType, &m.EntityID, &m.Version, &m.Status, &m.CreatedBy, &m.AgreedAt, &m.LastReviewedAt, &m.CreatedAt)
	return m, err
}

// smsWorkPackageColumns is qualified with the wp alias: every usage selects
// from sms_work_packages AS wp joined to sms_stakeholders, where unqualified
// column names (id) are ambiguous.
const smsWorkPackageColumns = `wp.id, wp.title, wp.description, wp.entity_type, wp.entity_id, wp.contractor_stakeholder_id, wp.status, wp.planned_start, wp.planned_end, wp.actual_start, wp.completed_at, wp.notes, wp.created_at, wp.updated_at`

func scanSMSWorkPackage(row interface{ Scan(dest ...any) error }) (*models.SMSWorkPackage, error) {
	wp := &models.SMSWorkPackage{}
	err := row.Scan(&wp.ID, &wp.Title, &wp.Description, &wp.EntityType, &wp.EntityID, &wp.ContractorStakeholderID, &wp.Status, &wp.PlannedStart, &wp.PlannedEnd, &wp.ActualStart, &wp.CompletedAt, &wp.Notes, &wp.CreatedAt, &wp.UpdatedAt)
	return wp, err
}

// smsRACIEntityTitle resolves the P3M entity title for RACI / work-package rows.
func (h *SMSHandler) smsRACIEntityTitle(ctx context.Context, entityType string, entityID int) (string, error) {
	table, ok := smsP3MEntityTables[entityType]
	if !ok || entityID <= 0 {
		return "", nil
	}
	var title string
	err := h.db.QueryRowContext(ctx, `SELECT title FROM `+table+` WHERE id = ?`, entityID).Scan(&title)
	if isNotFound(err) {
		return "", nil
	}
	return title, err
}

// smsValidateP3MEntity checks that the referenced P3M row exists and writes an
// error response otherwise. Returns false once a response has been written.
func (h *SMSHandler) smsValidateP3MEntity(ctx context.Context, w http.ResponseWriter, r *http.Request, entityType string, entityID int) bool {
	table, ok := smsP3MEntityTables[entityType]
	if !ok || entityID <= 0 {
		respondBadRequest(w, r, "entity_type must be portfolio, programme or project and entity_id is required")
		return false
	}
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, entityID).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "P3M "+entityType+" not found")
		return false
	}
	if err != nil {
		respondInternalError(w, r, err)
		return false
	}
	return true
}

// smsRaciAssignments loads a matrix's assignments with resolved assignee names.
func (h *SMSHandler) smsRaciAssignments(ctx context.Context, raciID int) ([]models.SMSCRACIAssignment, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT a.id, a.raci_id, a.activity_code, a.description, a.stakeholder_id, a.user_id, a.value,
		COALESCE(s.name, TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), u.username, '')
		FROM sms_raci_assignments AS a
		LEFT JOIN sms_stakeholders AS s ON s.id = a.stakeholder_id
		LEFT JOIN users AS u ON u.id = a.user_id
		WHERE a.raci_id = ?
		ORDER BY a.activity_code COLLATE NOCASE, CASE a.value WHEN 'a' THEN 0 WHEN 'r' THEN 1 WHEN 'c' THEN 2 ELSE 3 END, a.id`, raciID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.SMSCRACIAssignment, 0)
	for rows.Next() {
		var a models.SMSCRACIAssignment
		if err := rows.Scan(&a.ID, &a.RaciID, &a.ActivityCode, &a.Description, &a.StakeholderID, &a.UserID, &a.Value, &a.AssigneeName); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListRacis lists RACI matrices, optionally filtered by P3M entity.
func (h *SMSHandler) ListRacis(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	where, args := "", []any{}
	if et := r.URL.Query().Get("entity_type"); et != "" {
		if _, ok := smsP3MEntityTables[et]; !ok {
			respondBadRequest(w, r, "entity_type must be portfolio, programme or project")
			return
		}
		where += ` AND m.entity_type = ?`
		args = append(args, et)
	}
	if eid := r.URL.Query().Get("entity_id"); eid != "" {
		id, err := strconv.Atoi(eid)
		if err != nil || id <= 0 {
			respondBadRequest(w, r, "entity_id must be a positive integer")
			return
		}
		where += ` AND m.entity_id = ?`
		args = append(args, id)
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis AS m WHERE 1=1`+where+` ORDER BY m.entity_type, m.entity_id, m.version DESC`, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSCRACI, 0)
	for rows.Next() {
		m, err := scanSMSRaci(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		title, terr := h.smsRACIEntityTitle(ctx, m.EntityType, m.EntityID)
		if terr != nil {
			respondInternalError(w, r, terr)
			return
		}
		m.EntityTitle = title
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetRaci returns a RACI matrix with its assignments.
func (h *SMSHandler) GetRaci(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	m, err := scanSMSRaci(h.db.QueryRowContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.EntityTitle, _ = h.smsRACIEntityTitle(ctx, m.EntityType, m.EntityID)

	assignments, err := h.smsRaciAssignments(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	out := struct {
		models.SMSCRACI
		Assignments []models.SMSCRACIAssignment `json:"assignments"`
	}{*m, assignments}
	respondJSONOK(w, out)
}

// CreateRaci creates a new draft version of the RACI matrix for a P3M entity.
func (h *SMSHandler) CreateRaci(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
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
	in.EntityType = strings.TrimSpace(in.EntityType)

	ctx := r.Context()
	if !h.smsValidateP3MEntity(ctx, w, r, in.EntityType, in.EntityID) {
		return
	}

	var version int
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM sms_racis WHERE entity_type = ? AND entity_id = ?`, in.EntityType, in.EntityID).Scan(&version)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_racis (entity_type, entity_id, version, status, created_by) VALUES (?, ?, ?, 'draft', ?)`, in.EntityType, in.EntityID, version, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	m, err := scanSMSRaci(h.db.QueryRowContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis WHERE id = ?`, int(id64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.EntityTitle, _ = h.smsRACIEntityTitle(ctx, m.EntityType, m.EntityID)

	resourceName := "RACI matrix" + fmt.Sprintf(" v%d", m.Version)
	if m.EntityTitle != "" {
		resourceName = m.EntityTitle + resourceName
	}
	smsAudit(h, r, user, "sms_raci_create", "sms_raci", &m.ID, resourceName, map[string]any{"entity_type": m.EntityType, "version": m.Version})
	respondJSONCreated(w, *m)
}

// UpdateRaci updates a matrix's status and review date. Transitioning to
// 'agreed' stamps agreed_at; leaving it clears the stamp.
func (h *SMSHandler) UpdateRaci(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status         *string `json:"status,omitempty"`
		LastReviewedAt *string `json:"last_reviewed_at,omitempty"` // YYYY-MM-DD
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	m, err := scanSMSRaci(h.db.QueryRowContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	newStatus := m.Status
	if in.Status != nil {
		s := strings.TrimSpace(*in.Status)
		if !smsValidEnum(s, models.SMSCRACIStatusDraft, models.SMSCRACIStatusPreliminary, models.SMSCRACIStatusUnderDiscussion, models.SMSCRACIStatusAgreed) {
			respondBadRequest(w, r, "status must be draft, preliminary, under_discussion or agreed")
			return
		}
		newStatus = s
	}

	var lastReviewed any
	if in.LastReviewedAt != nil && strings.TrimSpace(*in.LastReviewedAt) != "" {
		d := strings.TrimSpace(*in.LastReviewedAt)
		if !validSMSDate(d) {
			respondBadRequest(w, r, "last_reviewed_at must be a date like 2026-11-30")
			return
		}
		lastReviewed = d
	} else if m.LastReviewedAt != nil {
		lastReviewed = m.LastReviewedAt.Format("2006-01-02")
	}

	var agreedAt any
	switch {
	case newStatus == models.SMSCRACIStatusAgreed:
		agreedAt = time.Now().UTC()
	case m.Status == models.SMSCRACIStatusAgreed && newStatus != models.SMSCRACIStatusAgreed:
		agreedAt = nil // leaving 'agreed' clears the stamp
	default:
		if m.AgreedAt != nil {
			agreedAt = *m.AgreedAt
		}
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE sms_racis SET status = ?, last_reviewed_at = ?, agreed_at = ? WHERE id = ?`, newStatus, lastReviewed, agreedAt, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out, err := scanSMSRaci(h.db.QueryRowContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	out.EntityTitle, _ = h.smsRACIEntityTitle(ctx, out.EntityType, out.EntityID)

	resourceName := "RACI matrix" + fmt.Sprintf(" v%d", out.Version)
	if out.EntityTitle != "" {
		resourceName = out.EntityTitle + resourceName
	}
	smsAudit(h, r, user, "sms_raci_update", "sms_raci", &out.ID, resourceName, map[string]any{"status": newStatus})
	respondJSONOK(w, *out)
}

// DeleteRaci removes a RACI matrix and its assignments.
func (h *SMSHandler) DeleteRaci(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	ctx := r.Context()
	m, err := scanSMSRaci(h.db.QueryRowContext(ctx, `SELECT `+smsRaciColumns+` FROM sms_racis WHERE id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_racis WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	resourceName := "RACI matrix" + fmt.Sprintf(" v%d", m.Version)
	if title, _ := h.smsRACIEntityTitle(ctx, m.EntityType, m.EntityID); title != "" {
		resourceName = title + resourceName
	}
	smsAudit(h, r, user, "sms_raci_delete", "sms_raci", &m.ID, resourceName, map[string]any{"entity_type": m.EntityType})
	w.WriteHeader(http.StatusNoContent)
}

// SaveRaciAssignments replaces a matrix's assignments in one transaction. Each
// activity_code must carry exactly one accountable ('a'); every row references
// exactly one of stakeholder_id (external party) or user_id (internal role).
func (h *SMSHandler) SaveRaciAssignments(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Assignments []struct {
			ActivityCode  string `json:"activity_code"`
			Description   string `json:"description"`
			StakeholderID *int   `json:"stakeholder_id,omitempty"`
			UserID        *int   `json:"user_id,omitempty"`
			Value         string `json:"value"`
		} `json:"assignments"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	var raciID int
	err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_racis WHERE id = ?`, id).Scan(&raciID)
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	type raciRow struct {
		ActivityCode  string
		Description   string
		StakeholderID *int
		UserID        *int
		Value         string
	}
	rows := make([]raciRow, 0, len(in.Assignments))
	for i, a := range in.Assignments {
		code := strings.TrimSpace(a.ActivityCode)
		value := strings.ToLower(strings.TrimSpace(a.Value))
		if code == "" {
			respondBadRequest(w, r, fmt.Sprintf("assignments[%d].activity_code is required", i))
			return
		}
		if !smsValidEnum(value, models.SMSCRACIValueResponsible, models.SMSCRACIValueAccountable, models.SMSCRACIValueConsulted, models.SMSCRACIValueInformed) {
			respondBadRequest(w, r, fmt.Sprintf("assignments[%d].value must be one of r, a, c, i", i))
			return
		}
		if (a.StakeholderID == nil) == (a.UserID == nil) {
			respondBadRequest(w, r, fmt.Sprintf("assignments[%d] must reference exactly one of stakeholder_id or user_id", i))
			return
		}
		rows = append(rows, raciRow{ActivityCode: code, Description: a.Description, StakeholderID: a.StakeholderID, UserID: a.UserID, Value: value})
	}

	// Exactly one accountable per activity: zero or multiple is invalid.
	accountable := map[string]int{} // A count for every activity in the payload
	for _, a := range rows {
		if _, ok := accountable[a.ActivityCode]; !ok {
			accountable[a.ActivityCode] = 0
		}
		if a.Value == models.SMSCRACIValueAccountable {
			accountable[a.ActivityCode]++
		}
	}
	var bad []string
	for code, n := range accountable {
		if n != 1 {
			bad = append(bad, fmt.Sprintf("%q (%d)", code, n))
		}
	}
	if len(bad) > 0 {
		respondBadRequest(w, r, "each activity must have exactly one accountable (a): "+strings.Join(bad, ", "))
		return
	}

	for _, a := range rows {
		if a.StakeholderID != nil {
			exists, err := h.smsStakeholderExists(ctx, *a.StakeholderID)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			if !exists {
				respondNotFound(w, r, "stakeholder not found")
				return
			}
		} else if a.UserID != nil {
			var n int
			err := h.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, *a.UserID).Scan(&n)
			if isNotFound(err) {
				respondNotFound(w, r, "user not found")
				return
			}
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
		}
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM sms_raci_assignments WHERE raci_id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range rows {
		a := &rows[i]
		if _, err := tx.ExecContext(ctx, `INSERT INTO sms_raci_assignments (raci_id, activity_code, description, stakeholder_id, user_id, value) VALUES (?, ?, ?, ?, ?, ?)`, id, a.ActivityCode, a.Description, nullInt(a.StakeholderID), nullInt(a.UserID), a.Value); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	smsAudit(h, r, user, "sms_raci_assignments_saved", "sms_raci", &id, "", map[string]any{"count": len(rows)})

	out, err := h.smsRaciAssignments(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// smsWorkPackageByID loads one work package with contractor name and node count.
func (h *SMSHandler) smsWorkPackageByID(ctx context.Context, id int) (*models.SMSWorkPackage, error) {
	wp := &models.SMSWorkPackage{}
	err := h.db.QueryRowContext(ctx, `SELECT `+smsWorkPackageColumns+`, COALESCE(s.name, ''), (SELECT COUNT(*) FROM sms_work_package_nodes AS n WHERE n.work_package_id = wp.id)
		FROM sms_work_packages AS wp
		LEFT JOIN sms_stakeholders AS s ON s.id = wp.contractor_stakeholder_id
		WHERE wp.id = ?`, id).
		Scan(&wp.ID, &wp.Title, &wp.Description, &wp.EntityType, &wp.EntityID, &wp.ContractorStakeholderID, &wp.Status, &wp.PlannedStart, &wp.PlannedEnd, &wp.ActualStart, &wp.CompletedAt, &wp.Notes, &wp.CreatedAt, &wp.UpdatedAt, &wp.ContractorName, &wp.NodeCount)
	return wp, err
}

// ListWorkPackages lists work packages, optionally filtered by P3M entity.
func (h *SMSHandler) ListWorkPackages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	where, args := "", []any{}
	if et := r.URL.Query().Get("entity_type"); et != "" {
		if _, ok := smsP3MEntityTables[et]; !ok {
			respondBadRequest(w, r, "entity_type must be portfolio, programme or project")
			return
		}
		where += ` AND wp.entity_type = ?`
		args = append(args, et)
	}
	if eid := r.URL.Query().Get("entity_id"); eid != "" {
		id, err := strconv.Atoi(eid)
		if err != nil || id <= 0 {
			respondBadRequest(w, r, "entity_id must be a positive integer")
			return
		}
		where += ` AND wp.entity_id = ?`
		args = append(args, id)
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+smsWorkPackageColumns+`, COALESCE(s.name, ''), (SELECT COUNT(*) FROM sms_work_package_nodes AS n WHERE n.work_package_id = wp.id)
		FROM sms_work_packages AS wp
		LEFT JOIN sms_stakeholders AS s ON s.id = wp.contractor_stakeholder_id
		WHERE 1=1`+where+` ORDER BY wp.title COLLATE NOCASE`, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.SMSWorkPackage, 0)
	for rows.Next() {
		var wp models.SMSWorkPackage
		if err := rows.Scan(&wp.ID, &wp.Title, &wp.Description, &wp.EntityType, &wp.EntityID, &wp.ContractorStakeholderID, &wp.Status, &wp.PlannedStart, &wp.PlannedEnd, &wp.ActualStart, &wp.CompletedAt, &wp.Notes, &wp.CreatedAt, &wp.UpdatedAt, &wp.ContractorName, &wp.NodeCount); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, wp)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetWorkPackage returns a work package with its impacted nodes.
func (h *SMSHandler) GetWorkPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	wp, err := h.smsWorkPackageByID(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Work package not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT n.id, n.work_package_id, n.node_id, n.impact_notes, COALESCE(nd.name, '')
		FROM sms_work_package_nodes AS n
		LEFT JOIN sms_nodes AS nd ON nd.id = n.node_id
		WHERE n.work_package_id = ?
		ORDER BY nd.name COLLATE NOCASE`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	nodes := make([]models.SMSWorkPackageNode, 0)
	for rows.Next() {
		var n models.SMSWorkPackageNode
		if err := rows.Scan(&n.ID, &n.WorkPackageID, &n.NodeID, &n.ImpactNotes, &n.NodeName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out := struct {
		models.SMSWorkPackage
		Nodes []models.SMSWorkPackageNode `json:"nodes"`
	}{*wp, nodes}
	respondJSONOK(w, out)
}

// CreateWorkPackage registers a physical activity with an optional performing
// contractor and P3M scope.
func (h *SMSHandler) CreateWorkPackage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Title                   string  `json:"title"`
		Description             string  `json:"description"`
		EntityType              *string `json:"entity_type,omitempty"`
		EntityID                *int    `json:"entity_id,omitempty"`
		ContractorStakeholderID *int    `json:"contractor_stakeholder_id,omitempty"`
		Status                  string  `json:"status"`
		PlannedStart            *string `json:"planned_start,omitempty"` // YYYY-MM-DD
		PlannedEnd              *string `json:"planned_end,omitempty"`   // YYYY-MM-DD
		Notes                   string  `json:"notes"`
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

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = models.SMSWorkPackageStatusPlanned
	}
	if !smsValidEnum(status, models.SMSWorkPackageStatusPlanned, models.SMSWorkPackageStatusInProgress, models.SMSWorkPackageStatusCompleted, models.SMSWorkPackageStatusCancelled) {
		respondBadRequest(w, r, "status must be planned, in_progress, completed or cancelled")
		return
	}

	ctx := r.Context()

	var entityType any
	if (in.EntityType == nil) != (in.EntityID == nil) {
		respondBadRequest(w, r, "entity_type and entity_id must be provided together")
		return
	}
	if in.EntityType != nil {
		et := strings.TrimSpace(smsDerefStr(in.EntityType))
		eid := smsDerefInt(in.EntityID)
		if !h.smsValidateP3MEntity(ctx, w, r, et, eid) {
			return
		}
		entityType = et
	}

	if in.ContractorStakeholderID != nil {
		exists, err := h.smsStakeholderExists(ctx, *in.ContractorStakeholderID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "contractor stakeholder not found")
			return
		}
	}

	var plannedStart, plannedEnd any
	if in.PlannedStart != nil && strings.TrimSpace(*in.PlannedStart) != "" {
		d := strings.TrimSpace(*in.PlannedStart)
		if !validSMSDate(d) {
			respondBadRequest(w, r, "planned_start must be a date like 2026-11-30")
			return
		}
		plannedStart = d
	}
	if in.PlannedEnd != nil && strings.TrimSpace(*in.PlannedEnd) != "" {
		d := strings.TrimSpace(*in.PlannedEnd)
		if !validSMSDate(d) {
			respondBadRequest(w, r, "planned_end must be a date like 2026-11-30")
			return
		}
		plannedEnd = d
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_work_packages (title, description, entity_type, entity_id, contractor_stakeholder_id, status, planned_start, planned_end, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Description, entityType, smsEntityIDArg(smsDerefInt(in.EntityID)), nullInt(in.ContractorStakeholderID), status, plannedStart, plannedEnd, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	wp, err := h.smsWorkPackageByID(ctx, int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_work_package_create", "sms_work_package", &wp.ID, wp.Title, map[string]any{"status": status})
	respondJSONCreated(w, *wp)
}

// UpdateWorkPackage partially updates a work package. Entering in_progress
// stamps actual_start; entering completed/cancelled stamps completed_at.
func (h *SMSHandler) UpdateWorkPackage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title                   *string `json:"title,omitempty"`
		Description             *string `json:"description,omitempty"`
		EntityType              *string `json:"entity_type,omitempty"`
		EntityID                *int    `json:"entity_id,omitempty"`
		ContractorStakeholderID *int    `json:"contractor_stakeholder_id,omitempty"`
		Status                  *string `json:"status,omitempty"`
		PlannedStart            *string `json:"planned_start,omitempty"` // YYYY-MM-DD
		PlannedEnd              *string `json:"planned_end,omitempty"`   // YYYY-MM-DD
		Notes                   *string `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	wp, err := h.smsWorkPackageByID(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Work package not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	title := wp.Title
	if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
		title = strings.TrimSpace(*in.Title)
	}

	newStatus := wp.Status
	if in.Status != nil {
		s := strings.TrimSpace(*in.Status)
		if !smsValidEnum(s, models.SMSWorkPackageStatusPlanned, models.SMSWorkPackageStatusInProgress, models.SMSWorkPackageStatusCompleted, models.SMSWorkPackageStatusCancelled) {
			respondBadRequest(w, r, "status must be planned, in_progress, completed or cancelled")
			return
		}
		newStatus = s
	}

	var entityType any
	if wp.EntityType != nil {
		entityType = *wp.EntityType
	}
	entityIDArg := smsEntityIDArg(smsDerefInt(wp.EntityID))
	if in.EntityType != nil || in.EntityID != nil {
		if in.EntityType == nil || in.EntityID == nil {
			respondBadRequest(w, r, "entity_type and entity_id must be provided together")
			return
		}
		et := strings.TrimSpace(smsDerefStr(in.EntityType))
		eid := smsDerefInt(in.EntityID)
		if !h.smsValidateP3MEntity(ctx, w, r, et, eid) {
			return
		}
		entityType = et
		entityIDArg = eid
	}

	contractor := orPtrInt(in.ContractorStakeholderID, wp.ContractorStakeholderID)
	if contractor != nil && in.ContractorStakeholderID != nil {
		exists, err := h.smsStakeholderExists(ctx, *contractor)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "contractor stakeholder not found")
			return
		}
	}

	var plannedStart, plannedEnd any
	if in.PlannedStart != nil && strings.TrimSpace(*in.PlannedStart) != "" {
		d := strings.TrimSpace(*in.PlannedStart)
		if !validSMSDate(d) {
			respondBadRequest(w, r, "planned_start must be a date like 2026-11-30")
			return
		}
		plannedStart = d
	} else if wp.PlannedStart != nil {
		plannedStart = wp.PlannedStart.Format("2006-01-02")
	}
	if in.PlannedEnd != nil && strings.TrimSpace(*in.PlannedEnd) != "" {
		d := strings.TrimSpace(*in.PlannedEnd)
		if !validSMSDate(d) {
			respondBadRequest(w, r, "planned_end must be a date like 2026-11-30")
			return
		}
		plannedEnd = d
	} else if wp.PlannedEnd != nil {
		plannedEnd = wp.PlannedEnd.Format("2006-01-02")
	}

	actualStart := wp.ActualStart
	completedAt := wp.CompletedAt
	if newStatus != wp.Status {
		now := time.Now().UTC()
		switch newStatus {
		case models.SMSWorkPackageStatusInProgress:
			if actualStart == nil {
				actualStart = &now
			}
		case models.SMSWorkPackageStatusCompleted, models.SMSWorkPackageStatusCancelled:
			if completedAt == nil {
				completedAt = &now
			}
		}
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE sms_work_packages SET title = ?, description = ?, entity_type = ?, entity_id = ?, contractor_stakeholder_id = ?, status = ?, planned_start = ?, planned_end = ?, actual_start = ?, completed_at = ?, notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		title, orKeep(in.Description, wp.Description), entityType, entityIDArg, nullInt(contractor), newStatus, plannedStart, plannedEnd, actualStart, completedAt, orKeep(in.Notes, wp.Notes), id); err != nil {
		respondInternalError(w, r, err)
		return
	}

	out, err := h.smsWorkPackageByID(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_work_package_update", "sms_work_package", &out.ID, out.Title, map[string]any{"status": newStatus})
	respondJSONOK(w, *out)
}

// DeleteWorkPackage removes a work package and its node links.
func (h *SMSHandler) DeleteWorkPackage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	ctx := r.Context()
	wp, err := h.smsWorkPackageByID(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Work package not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_work_packages WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_work_package_delete", "sms_work_package", &wp.ID, wp.Title, map[string]any{})
	w.WriteHeader(http.StatusNoContent)
}

// AddWorkPackageNode links an impacted node to a work package.
func (h *SMSHandler) AddWorkPackageNode(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		NodeID      int    `json:"node_id"`
		ImpactNotes string `json:"impact_notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.NodeID <= 0 {
		respondBadRequest(w, r, "node_id is required")
		return
	}

	ctx := r.Context()
	var wpID int
	err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_work_packages WHERE id = ?`, id).Scan(&wpID)
	if isNotFound(err) {
		respondNotFound(w, r, "Work package not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_nodes WHERE id = ?`, in.NodeID).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var dup int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_work_package_nodes WHERE work_package_id = ? AND node_id = ?`, id, in.NodeID).Scan(&dup)
	if isNotFound(err) {
		// not linked yet — proceed
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	} else {
		respondConflict(w, r, "Node is already linked to this work package")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_work_package_nodes (work_package_id, node_id, impact_notes) VALUES (?, ?, ?)`, id, in.NodeID, in.ImpactNotes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	linkID64, _ := res.LastInsertId()

	var out models.SMSWorkPackageNode
	err = h.db.QueryRowContext(ctx, `SELECT n.id, n.work_package_id, n.node_id, n.impact_notes, COALESCE(nd.name, '')
		FROM sms_work_package_nodes AS n LEFT JOIN sms_nodes AS nd ON nd.id = n.node_id WHERE n.id = ?`, int(linkID64)).
		Scan(&out.ID, &out.WorkPackageID, &out.NodeID, &out.ImpactNotes, &out.NodeName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	smsAudit(h, r, user, "sms_work_package_node_add", "sms_work_package", &wpID, "", map[string]any{"node_id": in.NodeID})
	respondJSONCreated(w, out)
}

// RemoveWorkPackageNode unlinks a node from its work package.
func (h *SMSHandler) RemoveWorkPackageNode(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	linkID, ok := requireIDParam(w, r, "lid")
	if !ok {
		return
	}

	ctx := r.Context()
	var wpID int
	err := h.db.QueryRowContext(ctx, `SELECT work_package_id FROM sms_work_package_nodes WHERE id = ?`, linkID).Scan(&wpID)
	if isNotFound(err) {
		respondNotFound(w, r, "Work package node link not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_work_package_nodes WHERE id = ?`, linkID); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_work_package_node_remove", "sms_work_package", &wpID, "", map[string]any{"link_id": linkID})
	w.WriteHeader(http.StatusNoContent)
}
