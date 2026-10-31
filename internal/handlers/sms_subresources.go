package handlers

import (
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Communications.
// ---------------------------------------------------------------------------

const smsCommunicationColumns = `id, stakeholder_id, channel, direction, subject, body, sentiment, created_by, created_at`

func scanSMSCommunication(row interface{ Scan(dest ...any) error }) (*models.SMSCommunication, error) {
	c := &models.SMSCommunication{}
	err := row.Scan(&c.ID, &c.StakeholderID, &c.Channel, &c.Direction, &c.Subject, &c.Body, &c.Sentiment, &c.CreatedBy, &c.CreatedAt)
	return c, err
}

// ListStakeholderCommunications returns the interaction log for one stakeholder.
func (h *SMSHandler) ListStakeholderCommunications(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsCommunicationColumns+` FROM sms_communications WHERE stakeholder_id = ? ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSCommunication, 0)
	for rows.Next() {
		c, err := scanSMSCommunication(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateCommunication logs one interaction with a stakeholder.
func (h *SMSHandler) CreateCommunication(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Channel   string  `json:"channel"`
		Direction string  `json:"direction"`
		Subject   string  `json:"subject"`
		Body      string  `json:"body"`
		Sentiment *string `json:"sentiment,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Channel != "" && !smsValidEnum(in.Channel, smsChannels...) {
		respondBadRequest(w, r, "invalid channel")
		return
	}
	if in.Direction != "" && !smsValidEnum(in.Direction, smsDirections...) {
		respondBadRequest(w, r, "invalid direction")
		return
	}
	if in.Sentiment != nil && !smsValidEnum(*in.Sentiment, smsSentiments...) {
		respondBadRequest(w, r, "invalid sentiment")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	var sentiment any
	if in.Sentiment != nil {
		sentiment = *in.Sentiment
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_communications (stakeholder_id, channel, direction, subject, body, sentiment) VALUES (?, ?, ?, ?, ?, ?)`,
		id, orKeepStr(in.Channel, models.SMSChannelEmail), orKeepStr(in.Direction, models.SMSDirectionOutbound), in.Subject, in.Body, sentiment)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	cid, _ := res.LastInsertId()

	var c models.SMSCommunication
	err = h.db.QueryRowContext(ctx, `SELECT `+smsCommunicationColumns+` FROM sms_communications WHERE id = ?`, cid).
		Scan(&c.ID, &c.StakeholderID, &c.Channel, &c.Direction, &c.Subject, &c.Body, &c.Sentiment, &c.CreatedBy, &c.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, c)
}

// DeleteCommunication removes one logged interaction.
func (h *SMSHandler) DeleteCommunication(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "cid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_communications WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Communication not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Engagement plans.
// ---------------------------------------------------------------------------

const smsPlanColumns = `id, stakeholder_id, strategy, frequency, channel, status, start_date, end_date, notes, created_at, updated_at`

func scanSMSPlan(row interface{ Scan(dest ...any) error }) (*models.SMSEngagementPlan, error) {
	p := &models.SMSEngagementPlan{}
	err := row.Scan(&p.ID, &p.StakeholderID, &p.Strategy, &p.Frequency, &p.Channel, &p.Status, &p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// ListStakeholderPlans returns the engagement plans for one stakeholder.
func (h *SMSHandler) ListStakeholderPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsPlanColumns+` FROM sms_engagement_plans WHERE stakeholder_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSEngagementPlan, 0)
	for rows.Next() {
		p, err := scanSMSPlan(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreatePlan adds an engagement plan for one stakeholder.
func (h *SMSHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Strategy  string     `json:"strategy"`
		Frequency string     `json:"frequency"`
		Channel   string     `json:"channel"`
		Status    string     `json:"status"`
		StartDate *time.Time `json:"start_date,omitempty"`
		EndDate   *time.Time `json:"end_date,omitempty"`
		Notes     string     `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Frequency != "" && !smsValidEnum(in.Frequency, smsPlanFrequencies...) {
		respondBadRequest(w, r, "invalid frequency")
		return
	}
	if in.Channel != "" && !smsValidEnum(in.Channel, smsChannels...) {
		respondBadRequest(w, r, "invalid channel")
		return
	}
	if in.Status != "" && !smsValidEnum(in.Status, smsPlanStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_engagement_plans (stakeholder_id, strategy, frequency, channel, status, start_date, end_date, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Strategy, orKeepStr(in.Frequency, models.SMSFrequencyMonthly), orKeepStr(in.Channel, models.SMSChannelEmail), orKeepStr(in.Status, models.SMSPlanStatusActive), in.StartDate, in.EndDate, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	pid, _ := res.LastInsertId()

	var p models.SMSEngagementPlan
	err = h.db.QueryRowContext(ctx, `SELECT `+smsPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, pid).
		Scan(&p.ID, &p.StakeholderID, &p.Strategy, &p.Frequency, &p.Channel, &p.Status, &p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, p)
}

// UpdatePlan applies a partial update to one engagement plan.
func (h *SMSHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "pid")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SMSEngagementPlan
	err := h.db.QueryRowContext(ctx, `SELECT `+smsPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.Strategy, &existing.Frequency, &existing.Channel, &existing.Status, &existing.StartDate, &existing.EndDate, &existing.Notes, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Engagement plan not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Strategy  *string    `json:"strategy,omitempty"`
		Frequency *string    `json:"frequency,omitempty"`
		Channel   *string    `json:"channel,omitempty"`
		Status    *string    `json:"status,omitempty"`
		StartDate *time.Time `json:"start_date,omitempty"`
		EndDate   *time.Time `json:"end_date,omitempty"`
		Notes     *string    `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Frequency != nil && !smsValidEnum(*in.Frequency, smsPlanFrequencies...) {
		respondBadRequest(w, r, "invalid frequency")
		return
	}
	if in.Channel != nil && !smsValidEnum(*in.Channel, smsChannels...) {
		respondBadRequest(w, r, "invalid channel")
		return
	}
	if in.Status != nil && !smsValidEnum(*in.Status, smsPlanStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	startDate := existing.StartDate
	if in.StartDate != nil {
		startDate = in.StartDate
	}
	endDate := existing.EndDate
	if in.EndDate != nil {
		endDate = in.EndDate
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_engagement_plans SET strategy = ?, frequency = ?, channel = ?, status = ?, start_date = ?, end_date = ?, notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Strategy, existing.Strategy), orKeep(in.Frequency, existing.Frequency), orKeep(in.Channel, existing.Channel), orKeep(in.Status, existing.Status), startDate, endDate, orKeep(in.Notes, existing.Notes), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var p models.SMSEngagementPlan
	err = h.db.QueryRowContext(ctx, `SELECT `+smsPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, id).
		Scan(&p.ID, &p.StakeholderID, &p.Strategy, &p.Frequency, &p.Channel, &p.Status, &p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, p)
}

// DeletePlan removes one engagement plan.
func (h *SMSHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "pid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_engagement_plans WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Engagement plan not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Issues & risks.
// ---------------------------------------------------------------------------

const smsIssueColumns = `id, stakeholder_id, kind, title, description, priority, impact, likelihood, status, escalation_level, mitigation, resolution_notes, resolved_at, created_at, updated_at`

func scanSMSIssue(row interface{ Scan(dest ...any) error }) (*models.SMSIssue, error) {
	i := &models.SMSIssue{}
	err := row.Scan(&i.ID, &i.StakeholderID, &i.Kind, &i.Title, &i.Description, &i.Priority, &i.Impact, &i.Likelihood, &i.Status, &i.EscalationLevel, &i.Mitigation, &i.ResolutionNotes, &i.ResolvedAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

// ListStakeholderIssues returns the issue/risk register for one stakeholder.
func (h *SMSHandler) ListStakeholderIssues(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsIssueColumns+` FROM sms_issues WHERE stakeholder_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSIssue, 0)
	for rows.Next() {
		i, err := scanSMSIssue(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *i)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateIssue adds one issue or risk to a stakeholder's register.
func (h *SMSHandler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Kind        string `json:"kind"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Impact      int    `json:"impact"`
		Likelihood  int    `json:"likelihood"`
		Status      string `json:"status"`
		Mitigation  string `json:"mitigation"`
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
	if in.Kind != "" && !smsValidEnum(in.Kind, smsIssueKinds...) {
		respondBadRequest(w, r, "invalid kind")
		return
	}
	if in.Priority != "" && !smsValidEnum(in.Priority, smsPriorities...) {
		respondBadRequest(w, r, "invalid priority")
		return
	}
	if in.Status != "" && !smsValidEnum(in.Status, smsIssueStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}
	for _, lvl := range []int{in.Impact, in.Likelihood} {
		if lvl != 0 && (lvl < 1 || lvl > 5) {
			respondBadRequest(w, r, "impact and likelihood must be between 1 and 5")
			return
		}
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_issues (stakeholder_id, kind, title, description, priority, impact, likelihood, status, mitigation) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, orKeepStr(in.Kind, models.SMSIssueKindIssue), in.Title, in.Description,
		orKeepStr(in.Priority, models.SMSPriorityMedium), smsLevelOrDefault(in.Impact), smsLevelOrDefault(in.Likelihood),
		orKeepStr(in.Status, models.SMSIssueStatusOpen), in.Mitigation)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	iid, _ := res.LastInsertId()

	var i models.SMSIssue
	err = h.db.QueryRowContext(ctx, `SELECT `+smsIssueColumns+` FROM sms_issues WHERE id = ?`, iid).
		Scan(&i.ID, &i.StakeholderID, &i.Kind, &i.Title, &i.Description, &i.Priority, &i.Impact, &i.Likelihood, &i.Status, &i.EscalationLevel, &i.Mitigation, &i.ResolutionNotes, &i.ResolvedAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, i)
}

// UpdateIssue applies a partial update to one issue/risk. Moving the status to
// 'resolved' stamps resolved_at; leaving it clears the stamp.
func (h *SMSHandler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "iid")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SMSIssue
	err := h.db.QueryRowContext(ctx, `SELECT `+smsIssueColumns+` FROM sms_issues WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.Kind, &existing.Title, &existing.Description, &existing.Priority, &existing.Impact, &existing.Likelihood, &existing.Status, &existing.EscalationLevel, &existing.Mitigation, &existing.ResolutionNotes, &existing.ResolvedAt, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Issue not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Kind            *string `json:"kind,omitempty"`
		Title           *string `json:"title,omitempty"`
		Description     *string `json:"description,omitempty"`
		Priority        *string `json:"priority,omitempty"`
		Impact          *int    `json:"impact,omitempty"`
		Likelihood      *int    `json:"likelihood,omitempty"`
		Status          *string `json:"status,omitempty"`
		Mitigation      *string `json:"mitigation,omitempty"`
		ResolutionNotes *string `json:"resolution_notes,omitempty"`
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
	if in.Kind != nil && !smsValidEnum(*in.Kind, smsIssueKinds...) {
		respondBadRequest(w, r, "invalid kind")
		return
	}
	if in.Priority != nil && !smsValidEnum(*in.Priority, smsPriorities...) {
		respondBadRequest(w, r, "invalid priority")
		return
	}
	if in.Status != nil && !smsValidEnum(*in.Status, smsIssueStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}
	for _, lvl := range []*int{in.Impact, in.Likelihood} {
		if lvl != nil && (*lvl < 1 || *lvl > 5) {
			respondBadRequest(w, r, "impact and likelihood must be between 1 and 5")
			return
		}
	}

	newStatus := orKeep(in.Status, existing.Status)
	resolvedAt := existing.ResolvedAt
	if newStatus == models.SMSIssueStatusResolved && existing.Status != models.SMSIssueStatusResolved {
		now := time.Now().UTC()
		resolvedAt = &now
	} else if newStatus != models.SMSIssueStatusResolved && existing.Status == models.SMSIssueStatusResolved {
		resolvedAt = nil
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_issues SET kind = ?, title = ?, description = ?, priority = ?, impact = ?, likelihood = ?, status = ?, mitigation = ?, resolution_notes = ?, resolved_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Kind, existing.Kind), title, orKeep(in.Description, existing.Description), orKeep(in.Priority, existing.Priority),
		intOr(in.Impact, existing.Impact), intOr(in.Likelihood, existing.Likelihood), newStatus,
		orKeep(in.Mitigation, existing.Mitigation), orKeep(in.ResolutionNotes, existing.ResolutionNotes), resolvedAt, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var i models.SMSIssue
	err = h.db.QueryRowContext(ctx, `SELECT `+smsIssueColumns+` FROM sms_issues WHERE id = ?`, id).
		Scan(&i.ID, &i.StakeholderID, &i.Kind, &i.Title, &i.Description, &i.Priority, &i.Impact, &i.Likelihood, &i.Status, &i.EscalationLevel, &i.Mitigation, &i.ResolutionNotes, &i.ResolvedAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, i)
}

// EscalateIssue marks an issue as escalated and bumps its escalation level.
func (h *SMSHandler) EscalateIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "iid")
	if !ok {
		return
	}
	ctx := r.Context()

	res, err := h.db.ExecContext(ctx, `UPDATE sms_issues SET status = 'escalated', escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Issue not found")
		return
	}

	var i models.SMSIssue
	err = h.db.QueryRowContext(ctx, `SELECT `+smsIssueColumns+` FROM sms_issues WHERE id = ?`, id).
		Scan(&i.ID, &i.StakeholderID, &i.Kind, &i.Title, &i.Description, &i.Priority, &i.Impact, &i.Likelihood, &i.Status, &i.EscalationLevel, &i.Mitigation, &i.ResolutionNotes, &i.ResolvedAt, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, i)
}

// DeleteIssue removes one issue/risk entry.
func (h *SMSHandler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "iid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_issues WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Issue not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Tasks.
// ---------------------------------------------------------------------------

const smsTaskColumns = `id, stakeholder_id, title, description, assigned_to_user_id, due_date, status, completed_at, created_at, updated_at`

func scanSMSTask(row interface{ Scan(dest ...any) error }) (*models.SMSTask, error) {
	t := &models.SMSTask{}
	err := row.Scan(&t.ID, &t.StakeholderID, &t.Title, &t.Description, &t.AssignedToUserID, &t.DueDate, &t.Status, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// ListStakeholderTasks returns the task list for one stakeholder.
func (h *SMSHandler) ListStakeholderTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsTaskColumns+` FROM sms_tasks WHERE stakeholder_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSTask, 0)
	for rows.Next() {
		t, err := scanSMSTask(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateTask adds one task for a stakeholder.
func (h *SMSHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title              string     `json:"title"`
		Description        string     `json:"description"`
		AssignedToUserID   *int       `json:"assigned_to_user_id,omitempty"`
		DueDate            *time.Time `json:"due_date,omitempty"`
		Status             string     `json:"status"`
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
	if in.Status != "" && !smsValidEnum(in.Status, smsTaskStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	var completedAt any
	status := orKeepStr(in.Status, models.SMSTaskStatusOpen)
	if status == models.SMSTaskStatusDone {
		now := time.Now().UTC()
		completedAt = &now
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_tasks (stakeholder_id, title, description, assigned_to_user_id, due_date, status, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, in.Title, in.Description, nullInt(in.AssignedToUserID), in.DueDate, status, completedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	tid, _ := res.LastInsertId()

	var t models.SMSTask
	err = h.db.QueryRowContext(ctx, `SELECT `+smsTaskColumns+` FROM sms_tasks WHERE id = ?`, tid).
		Scan(&t.ID, &t.StakeholderID, &t.Title, &t.Description, &t.AssignedToUserID, &t.DueDate, &t.Status, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, t)
}

// UpdateTask applies a partial update to one task. Setting status 'done'
// stamps completed_at; any other status clears it.
func (h *SMSHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "tid")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SMSTask
	err := h.db.QueryRowContext(ctx, `SELECT `+smsTaskColumns+` FROM sms_tasks WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.Title, &existing.Description, &existing.AssignedToUserID, &existing.DueDate, &existing.Status, &existing.CompletedAt, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Task not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Title              *string    `json:"title,omitempty"`
		Description        *string    `json:"description,omitempty"`
		AssignedToUserID   *int       `json:"assigned_to_user_id,omitempty"`
		DueDate            *time.Time `json:"due_date,omitempty"`
		Status             *string    `json:"status,omitempty"`
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
	if in.Status != nil && !smsValidEnum(*in.Status, smsTaskStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	newStatus := orKeep(in.Status, existing.Status)
	completedAt := existing.CompletedAt
	if newStatus == models.SMSTaskStatusDone && existing.Status != models.SMSTaskStatusDone {
		now := time.Now().UTC()
		completedAt = &now
	} else if newStatus != models.SMSTaskStatusDone && existing.Status == models.SMSTaskStatusDone {
		completedAt = nil
	}

	dueDate := existing.DueDate
	if in.DueDate != nil {
		dueDate = in.DueDate
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_tasks SET title = ?, description = ?, assigned_to_user_id = ?, due_date = ?, status = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		title, orKeep(in.Description, existing.Description), nullInt(orPtrInt(in.AssignedToUserID, existing.AssignedToUserID)), dueDate, newStatus, completedAt, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var t models.SMSTask
	err = h.db.QueryRowContext(ctx, `SELECT `+smsTaskColumns+` FROM sms_tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.StakeholderID, &t.Title, &t.Description, &t.AssignedToUserID, &t.DueDate, &t.Status, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, t)
}

// DeleteTask removes one task.
func (h *SMSHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "tid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_tasks WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Feedback.
// ---------------------------------------------------------------------------

const smsFeedbackColumns = `id, stakeholder_id, source, category, rating, body, resolution_status, resolution_notes, created_at, updated_at`

func scanSMSFeedback(row interface{ Scan(dest ...any) error }) (*models.SMSFeedback, error) {
	f := &models.SMSFeedback{}
	err := row.Scan(&f.ID, &f.StakeholderID, &f.Source, &f.Category, &f.Rating, &f.Body, &f.ResolutionStatus, &f.ResolutionNotes, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

// ListStakeholderFeedback returns the feedback log for one stakeholder.
func (h *SMSHandler) ListStakeholderFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsFeedbackColumns+` FROM sms_feedback WHERE stakeholder_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSFeedback, 0)
	for rows.Next() {
		f, err := scanSMSFeedback(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateFeedback records one feedback entry for a stakeholder.
func (h *SMSHandler) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Source           string `json:"source"`
		Category         string `json:"category"`
		Rating           *int   `json:"rating,omitempty"`
		Body             string `json:"body"`
		ResolutionStatus string `json:"resolution_status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Source != "" && !smsValidEnum(in.Source, smsFeedbackSources...) {
		respondBadRequest(w, r, "invalid source")
		return
	}
	if in.ResolutionStatus != "" && !smsValidEnum(in.ResolutionStatus, smsFeedbackResolutions...) {
		respondBadRequest(w, r, "invalid resolution_status")
		return
	}
	if in.Rating != nil && (*in.Rating < 1 || *in.Rating > 5) {
		respondBadRequest(w, r, "rating must be between 1 and 5")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_feedback (stakeholder_id, source, category, rating, body, resolution_status) VALUES (?, ?, ?, ?, ?, ?)`,
		id, orKeepStr(in.Source, models.SMSFeedbackSourceForm), in.Category, nullInt(in.Rating), in.Body, orKeepStr(in.ResolutionStatus, models.SMSFeedbackOpen))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	fid, _ := res.LastInsertId()

	var f models.SMSFeedback
	err = h.db.QueryRowContext(ctx, `SELECT `+smsFeedbackColumns+` FROM sms_feedback WHERE id = ?`, fid).
		Scan(&f.ID, &f.StakeholderID, &f.Source, &f.Category, &f.Rating, &f.Body, &f.ResolutionStatus, &f.ResolutionNotes, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, f)
}

// UpdateFeedback applies a partial update to one feedback entry.
func (h *SMSHandler) UpdateFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "fid")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SMSFeedback
	err := h.db.QueryRowContext(ctx, `SELECT `+smsFeedbackColumns+` FROM sms_feedback WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.Source, &existing.Category, &existing.Rating, &existing.Body, &existing.ResolutionStatus, &existing.ResolutionNotes, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Feedback not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Source           *string `json:"source,omitempty"`
		Category         *string `json:"category,omitempty"`
		Rating           *int    `json:"rating,omitempty"`
		Body             *string `json:"body,omitempty"`
		ResolutionStatus *string `json:"resolution_status,omitempty"`
		ResolutionNotes  *string `json:"resolution_notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Source != nil && !smsValidEnum(*in.Source, smsFeedbackSources...) {
		respondBadRequest(w, r, "invalid source")
		return
	}
	if in.ResolutionStatus != nil && !smsValidEnum(*in.ResolutionStatus, smsFeedbackResolutions...) {
		respondBadRequest(w, r, "invalid resolution_status")
		return
	}
	rating := orPtrInt(in.Rating, existing.Rating)
	if rating != nil && (*rating < 1 || *rating > 5) {
		respondBadRequest(w, r, "rating must be between 1 and 5")
		return
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_feedback SET source = ?, category = ?, rating = ?, body = ?, resolution_status = ?, resolution_notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		orKeep(in.Source, existing.Source), orKeep(in.Category, existing.Category), nullInt(rating), orKeep(in.Body, existing.Body), orKeep(in.ResolutionStatus, existing.ResolutionStatus), orKeep(in.ResolutionNotes, existing.ResolutionNotes), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var f models.SMSFeedback
	err = h.db.QueryRowContext(ctx, `SELECT `+smsFeedbackColumns+` FROM sms_feedback WHERE id = ?`, id).
		Scan(&f.ID, &f.StakeholderID, &f.Source, &f.Category, &f.Rating, &f.Body, &f.ResolutionStatus, &f.ResolutionNotes, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, f)
}

// DeleteFeedback removes one feedback entry.
func (h *SMSHandler) DeleteFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "fid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_feedback WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Feedback not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Relationship goals.
// ---------------------------------------------------------------------------

const smsGoalColumns = `id, stakeholder_id, goal, metric, target_value, current_value, status, created_at, updated_at`

func scanSMSGoal(row interface{ Scan(dest ...any) error }) (*models.SMSRelationshipGoal, error) {
	g := &models.SMSRelationshipGoal{}
	err := row.Scan(&g.ID, &g.StakeholderID, &g.Goal, &g.Metric, &g.TargetValue, &g.CurrentValue, &g.Status, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

// ListStakeholderGoals returns the relationship goals for one stakeholder.
func (h *SMSHandler) ListStakeholderGoals(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+smsGoalColumns+` FROM sms_relationship_goals WHERE stakeholder_id = ? ORDER BY id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSRelationshipGoal, 0)
	for rows.Next() {
		g, err := scanSMSGoal(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateGoal adds one relationship goal/KPI for a stakeholder.
func (h *SMSHandler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Goal         string   `json:"goal"`
		Metric       string   `json:"metric"`
		TargetValue  *float64 `json:"target_value,omitempty"`
		CurrentValue *float64 `json:"current_value,omitempty"`
		Status       string   `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Goal = strings.TrimSpace(in.Goal)
	if in.Goal == "" {
		respondBadRequest(w, r, "goal is required")
		return
	}
	if in.Status != "" && !smsValidEnum(in.Status, smsGoalStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_relationship_goals (stakeholder_id, goal, metric, target_value, current_value, status) VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.Goal, in.Metric, nullFloat(in.TargetValue), nullFloat(in.CurrentValue), orKeepStr(in.Status, models.SMSGoalOnTrack))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	gid, _ := res.LastInsertId()

	var g models.SMSRelationshipGoal
	err = h.db.QueryRowContext(ctx, `SELECT `+smsGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, gid).
		Scan(&g.ID, &g.StakeholderID, &g.Goal, &g.Metric, &g.TargetValue, &g.CurrentValue, &g.Status, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, g)
}

// UpdateGoal applies a partial update to one relationship goal.
func (h *SMSHandler) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SMSRelationshipGoal
	err := h.db.QueryRowContext(ctx, `SELECT `+smsGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.Goal, &existing.Metric, &existing.TargetValue, &existing.CurrentValue, &existing.Status, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Goal not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Goal         *string  `json:"goal,omitempty"`
		Metric       *string  `json:"metric,omitempty"`
		TargetValue  *float64 `json:"target_value,omitempty"`
		CurrentValue *float64 `json:"current_value,omitempty"`
		Status       *string  `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	goal := orKeep(in.Goal, existing.Goal)
	if goal == "" {
		respondBadRequest(w, r, "goal cannot be empty")
		return
	}
	if in.Status != nil && !smsValidEnum(*in.Status, smsGoalStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	_, err = h.db.ExecContext(ctx, `UPDATE sms_relationship_goals SET goal = ?, metric = ?, target_value = ?, current_value = ?, status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		goal, orKeep(in.Metric, existing.Metric), nullFloat(orPtrF(in.TargetValue, existing.TargetValue)), nullFloat(orPtrF(in.CurrentValue, existing.CurrentValue)), orKeep(in.Status, existing.Status), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var g models.SMSRelationshipGoal
	err = h.db.QueryRowContext(ctx, `SELECT `+smsGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, id).
		Scan(&g.ID, &g.StakeholderID, &g.Goal, &g.Metric, &g.TargetValue, &g.CurrentValue, &g.Status, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, g)
}

// DeleteGoal removes one relationship goal.
func (h *SMSHandler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "gid")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM sms_relationship_goals WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Goal not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
