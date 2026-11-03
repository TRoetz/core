package handlers

// Public grievance intake (WI-5.3): an unauthenticated webform lets external
// stakeholders lodge a grievance against the register. A submission resolves
// or creates the stakeholder, then records an sms_grievances row in 'received'
// status. The SMS grievance workflow is the single system of truth for the
// complaint lifecycle — unlike portal form channels (which route submissions
// into workspace items), no duplicate intake record is created here.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"windshift/internal/logger"
	"windshift/internal/models"
	"windshift/internal/utils"
)

type smsIntakeRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
	Severity     string `json:"severity"`
	Description  string `json:"description"`
}

// IntakeGrievance handles POST /sms/grievances/intake (public, rate-limited).
func (h *SMSHandler) IntakeGrievance(w http.ResponseWriter, r *http.Request) {
	var in smsIntakeRequest
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Organization = strings.TrimSpace(in.Organization)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	if len(in.Description) < 10 {
		respondBadRequest(w, r, "description must be at least 10 characters")
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

	ctx := r.Context()
	stakeholderID, matched, err := h.smsIntakeResolveStakeholder(ctx, in)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	title := smsIntakeTitle(in.Description)
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_grievances (stakeholder_id, title, description, severity, status) VALUES (?, ?, ?, ?, ?)`,
		stakeholderID, title, in.Description, severity, models.SMSGrievanceReceived)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	gid64, _ := res.LastInsertId()
	gid := int(gid64)

	// Anonymous actor: user_id stays NULL (the SCIM/system convention); the
	// client IP and user agent are retained for traceability.
	_ = logger.LogAudit(h.db, logger.AuditEvent{
		Username:     "anonymous",
		IPAddress:    utils.GetClientIP(r),
		UserAgent:    r.UserAgent(),
		ActionType:   "sms_grievance_intake",
		ResourceType: "sms_grievance",
		ResourceID:   &gid,
		ResourceName: title,
		Success:      true,
		Details: map[string]any{
			"stakeholder_id":      stakeholderID,
			"matched_stakeholder": matched,
			"name":                in.Name,
			"email":               in.Email,
			"organization":        in.Organization,
		},
	})

	respondJSONCreated(w, map[string]any{
		"grievance_id":        gid,
		"stakeholder_id":      stakeholderID,
		"matched_stakeholder": matched,
	})
}

// smsIntakeResolveStakeholder matches the submitter against the register by
// email first, then name+organization. Unknown submitters get a minimal
// register row (tier_3 defaults) so every grievance attaches to an identity in
// the system of record; operators can enrich or merge it afterwards.
func (h *SMSHandler) smsIntakeResolveStakeholder(ctx context.Context, in smsIntakeRequest) (int, bool, error) {
	if in.Email != "" {
		var id int
		err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_stakeholders WHERE lower(email) = ? ORDER BY id LIMIT 1`, in.Email).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
	}
	if in.Organization != "" {
		var id int
		err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_stakeholders WHERE lower(name) = ? AND lower(organization) = ? ORDER BY id LIMIT 1`,
			strings.ToLower(in.Name), strings.ToLower(in.Organization)).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_stakeholders (name, email, organization, notes) VALUES (?, ?, ?, ?)`,
		in.Name, in.Email, in.Organization, "Created via public grievance intake")
	if err != nil {
		return 0, false, err
	}
	id, _ := res.LastInsertId()
	return int(id), false, nil
}

// smsIntakeTitle derives the grievance title from the first line of the
// description (capped at 120 chars).
func smsIntakeTitle(description string) string {
	line := strings.TrimSpace(strings.SplitN(description, "\n", 2)[0])
	if line == "" {
		return "Grievance"
	}
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}
