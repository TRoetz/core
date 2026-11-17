// SMS notification campaign handlers: the notice to be delivered ("have they
// been informed?"), its node targets, and the per-stakeholder delivery ledger.
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

const smsCampaignColumns = `c.id, c.title, c.description, c.entity_type, c.entity_id, c.channel, c.scheduled_at, c.due_by, c.status, c.created_by, c.created_at, c.updated_at`

func scanSMSCampaign(row interface{ Scan(dest ...any) error }) (*models.SMSNotificationCampaign, error) {
	c := &models.SMSNotificationCampaign{}
	err := row.Scan(&c.ID, &c.Title, &c.Description, &c.EntityType, &c.EntityID, &c.Channel, &c.ScheduledAt, &c.DueBy, &c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

const smsCampaignTargetColumns = `t.id, t.campaign_id, t.node_id, t.include_descendants`

func scanSMSCampaignTarget(row interface{ Scan(dest ...any) error }) (*models.SMSCampaignTarget, error) {
	t := &models.SMSCampaignTarget{}
	err := row.Scan(&t.ID, &t.CampaignID, &t.NodeID, &t.IncludeDescendants)
	return t, err
}

const smsNotificationColumns = `n.id, n.campaign_id, n.stakeholder_id, n.node_id, n.required_by, n.status, n.channel, n.sent_at, n.acknowledged_at, n.communication_id, n.notes, n.created_at, n.updated_at`

func scanSMSNotification(row interface{ Scan(dest ...any) error }) (*models.SMSNotification, error) {
	n := &models.SMSNotification{}
	err := row.Scan(&n.ID, &n.CampaignID, &n.StakeholderID, &n.NodeID, &n.RequiredBy, &n.Status, &n.Channel, &n.SentAt, &n.AcknowledgedAt, &n.CommunicationID, &n.Notes, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

var smsCampaignStatuses = []string{models.SMSCampaignStatusDraft, models.SMSCampaignStatusScheduled, models.SMSCampaignStatusInProgress, models.SMSCampaignStatusCompleted, models.SMSCampaignStatusCancelled}

var smsNotificationStatuses = []string{models.SMSNotificationStatusPending, models.SMSNotificationStatusSent, models.SMSNotificationStatusRead, models.SMSNotificationStatusAcknowledged, models.SMSNotificationStatusFailed, models.SMSNotificationStatusExempt}

// smsCampaignCoverage loads one campaign with its ledger coverage counters.
func (h *SMSHandler) smsCampaignCoverage(ctx context.Context, id int) (*models.SMSNotificationCampaign, error) {
	c := &models.SMSNotificationCampaign{}
	err := h.db.QueryRowContext(ctx, `SELECT `+smsCampaignColumns+`, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id) AS total, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id AND n.status IN ('sent','read','acknowledged')) AS informed, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id AND n.required_by < date('now') AND n.status = 'pending') AS overdue FROM sms_notification_campaigns c WHERE c.id = ?`, id).
		Scan(&c.ID, &c.Title, &c.Description, &c.EntityType, &c.EntityID, &c.Channel, &c.ScheduledAt, &c.DueBy, &c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.TotalNotifications, &c.InformedCount, &c.OverdueCount)
	if err != nil {
		return nil, err
	}
	if c.TotalNotifications > 0 {
		c.InformedPct = 100.0 * float64(c.InformedCount) / float64(c.TotalNotifications)
	}
	return c, nil
}

// smsGetNotification loads one ledger row joined to its stakeholder and node.
func (h *SMSHandler) smsGetNotification(ctx context.Context, id int) (*models.SMSNotification, error) {
	n := &models.SMSNotification{}
	err := h.db.QueryRowContext(ctx, `SELECT `+smsNotificationColumns+`, s.name AS name, s.organization AS organization, COALESCE(nd.name, '') AS node_name FROM sms_notifications n JOIN sms_stakeholders s ON s.id = n.stakeholder_id LEFT JOIN sms_nodes nd ON nd.id = n.node_id WHERE n.id = ?`, id).
		Scan(&n.ID, &n.CampaignID, &n.StakeholderID, &n.NodeID, &n.RequiredBy, &n.Status, &n.Channel, &n.SentAt, &n.AcknowledgedAt, &n.CommunicationID, &n.Notes, &n.CreatedAt, &n.UpdatedAt, &n.Name, &n.Organization, &n.NodeName)
	return n, err
}

// smsTxn is the subset of database.Database/Tx used by notification updates so
// one ledger row can be transitioned standalone or inside a bulk transaction.
type smsTxn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// smsApplyNotificationUpdate moves one ledger row to desiredStatus (already
// resolved to the stored value when the client omitted it), applying channel/
// notes overrides and the sent/acknowledged side effects. It reports whether
// the row was updated or that it was missing.
func smsApplyNotificationUpdate(ctx context.Context, db smsTxn, user *models.User, id int, desiredStatus string, inChannel, inNotes *string) (bool, bool, error) {
	n := &models.SMSNotification{}
	err := db.QueryRowContext(ctx, `SELECT `+smsNotificationColumns+` FROM sms_notifications n WHERE n.id = ?`, id).Scan(&n.ID, &n.CampaignID, &n.StakeholderID, &n.NodeID, &n.RequiredBy, &n.Status, &n.Channel, &n.SentAt, &n.AcknowledgedAt, &n.CommunicationID, &n.Notes, &n.CreatedAt, &n.UpdatedAt)
	if isNotFound(err) {
		return false, true, nil
	} else if err != nil {
		return false, false, err
	}

	var campaignTitle, campaignDescription, campaignChannel string
	err = db.QueryRowContext(ctx, `SELECT title, description, channel FROM sms_notification_campaigns WHERE id = ?`, n.CampaignID).Scan(&campaignTitle, &campaignDescription, &campaignChannel)
	if isNotFound(err) {
		return false, true, nil
	} else if err != nil {
		return false, false, err
	}

	channel := n.Channel
	if inChannel != nil {
		channel = *inChannel
	}
	notes := n.Notes
	if inNotes != nil {
		notes = *inNotes
	}
	sentAt, ackAt, commID := n.SentAt, n.AcknowledgedAt, n.CommunicationID
	if desiredStatus != n.Status {
		switch desiredStatus {
		case models.SMSNotificationStatusSent:
			now := time.Now()
			sentAt = &now
			if inChannel == nil {
				channel = campaignChannel
			}
			res, err := db.ExecContext(ctx, `INSERT INTO sms_communications (stakeholder_id, channel, direction, subject, body, created_by) VALUES (?, ?, 'outbound', ?, ?, ?)`, n.StakeholderID, channel, campaignTitle, campaignDescription, user.ID)
			if err != nil {
				return false, false, err
			}
			cid64, _ := res.LastInsertId()
			cid := int(cid64)
			commID = &cid
		case models.SMSNotificationStatusAcknowledged:
			now := time.Now()
			ackAt = &now
		}
	}

	if _, err := db.ExecContext(ctx, `UPDATE sms_notifications SET status = ?, channel = ?, notes = ?, sent_at = ?, acknowledged_at = ?, communication_id = ? WHERE id = ?`,
		desiredStatus, channel, notes, nullTimePtr(sentAt), nullTimePtr(ackAt), nullInt(commID), id); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// ---------------------------------------------------------------------------
// Campaigns.
// ---------------------------------------------------------------------------

// ListNotificationCampaigns returns campaigns with per-campaign ledger coverage
// (total/informed/overdue counts and informed percentage). Optional filters:
// ?status, ?entity_type. Ordered by created_at DESC.
func (h *SMSHandler) ListNotificationCampaigns(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	query := `SELECT ` + smsCampaignColumns + `, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id) AS total, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id AND n.status IN ('sent','read','acknowledged')) AS informed, (SELECT COUNT(*) FROM sms_notifications n WHERE n.campaign_id = c.id AND n.required_by < date('now') AND n.status = 'pending') AS overdue FROM sms_notification_campaigns c`
	conds := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if v := r.URL.Query().Get("status"); v != "" {
		conds = append(conds, "c.status = ?")
		args = append(args, v)
	}
	if v := r.URL.Query().Get("entity_type"); v != "" {
		conds = append(conds, "c.entity_type = ?")
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY c.created_at DESC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNotificationCampaign, 0)
	for rows.Next() {
		c := &models.SMSNotificationCampaign{}
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.EntityType, &c.EntityID, &c.Channel, &c.ScheduledAt, &c.DueBy, &c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.TotalNotifications, &c.InformedCount, &c.OverdueCount); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		if c.TotalNotifications > 0 {
			c.InformedPct = 100.0 * float64(c.InformedCount) / float64(c.TotalNotifications)
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

// CreateNotificationCampaign registers a new campaign. channel defaults to
// email; status defaults to draft; due_by is a YYYY-MM-DD date.
func (h *SMSHandler) CreateNotificationCampaign(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Title       string     `json:"title"`
		Description string     `json:"description"`
		EntityType  *string    `json:"entity_type,omitempty"`
		EntityID    *int       `json:"entity_id,omitempty"`
		Channel     string     `json:"channel"`
		ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
		DueBy       *string    `json:"due_by,omitempty"` // YYYY-MM-DD
		Status      string     `json:"status"`
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
	channel := models.SMSChannelEmail
	if in.Channel != "" {
		if !smsValidEnum(in.Channel, smsChannels...) {
			respondBadRequest(w, r, "invalid channel")
			return
		}
		channel = in.Channel
	}
	status := models.SMSCampaignStatusDraft
	if in.Status != "" {
		if !smsValidEnum(in.Status, smsCampaignStatuses...) {
			respondBadRequest(w, r, "invalid status")
			return
		}
		status = in.Status
	}
	dueBy := smsDerefStr(in.DueBy)
	if dueBy != "" && !validSMSDate(dueBy) {
		respondBadRequest(w, r, "due_by must be YYYY-MM-DD")
		return
	}

	ctx := r.Context()
	entityType := smsDerefStr(in.EntityType)
	entityID := smsDerefInt(in.EntityID)
	if entityType != "" || entityID > 0 {
		if !h.smsValidateEntityLink(w, r, ctx, entityType, entityID) {
			return
		}
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_notification_campaigns (title, description, entity_type, entity_id, channel, scheduled_at, due_by, status, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Description, nullStringOrEmpty(entityType), smsEntityIDArg(entityID), channel, in.ScheduledAt, nullStringOrEmpty(dueBy), status, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	c, err := h.smsCampaignCoverage(ctx, int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_campaign_created", "sms_notification_campaign", &c.ID, c.Title, map[string]any{"status": status})
	respondJSONCreated(w, c)
}

// GetNotificationCampaign returns one campaign with coverage counters, its node
// targets and the ledger count per notification status.
func (h *SMSHandler) GetNotificationCampaign(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	campaign, err := h.smsCampaignCoverage(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+smsCampaignTargetColumns+`, n.name AS node_name, k.label AS kind_label FROM sms_campaign_targets t JOIN sms_nodes n ON n.id = t.node_id JOIN sms_node_kinds k ON k.id = n.kind_id WHERE t.campaign_id = ? ORDER BY n.name COLLATE NOCASE`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	targets := make([]models.SMSCampaignTarget, 0)
	for rows.Next() {
		t := &models.SMSCampaignTarget{}
		if err := rows.Scan(&t.ID, &t.CampaignID, &t.NodeID, &t.IncludeDescendants, &t.NodeName, &t.KindLabel); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		targets = append(targets, *t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}

	byStatus := map[string]int{
		models.SMSNotificationStatusPending:      0,
		models.SMSNotificationStatusSent:         0,
		models.SMSNotificationStatusRead:         0,
		models.SMSNotificationStatusAcknowledged: 0,
		models.SMSNotificationStatusFailed:       0,
		models.SMSNotificationStatusExempt:       0,
	}
	statusRows, err := h.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM sms_notifications WHERE campaign_id = ? GROUP BY status`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for statusRows.Next() {
		var s string
		var cnt int
		if err := statusRows.Scan(&s, &cnt); err != nil {
			statusRows.Close()
			respondInternalError(w, r, err)
			return
		}
		byStatus[s] = cnt
	}
	statusRows.Close()
	if err := statusRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{"campaign": campaign, "targets": targets, "notifications_by_status": byStatus})
}

// UpdateNotificationCampaign applies a partial update to a campaign. Fields
// keep their stored value when omitted or null; send an empty string (or zero
// id) for entity_type/entity_id/due_by to clear them.
func (h *SMSHandler) UpdateNotificationCampaign(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title       *string    `json:"title,omitempty"`
		Description *string    `json:"description,omitempty"`
		EntityType  *string    `json:"entity_type"` // "" clears the link; omitted/null keeps
		EntityID    *int       `json:"entity_id"`   // 0 clears the link; omitted/null keeps
		Channel     *string    `json:"channel,omitempty"`
		ScheduledAt *time.Time `json:"scheduled_at"` // zero time clears; omitted/null keeps
		DueBy       *string    `json:"due_by"`       // YYYY-MM-DD; "" clears; omitted/null keeps
		Status      *string    `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := scanSMSCampaign(h.db.QueryRowContext(ctx, `SELECT `+smsCampaignColumns+` FROM sms_notification_campaigns c WHERE c.id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	title := existing.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if title == "" {
		respondBadRequest(w, r, "title must not be empty")
		return
	}
	description := existing.Description
	if in.Description != nil {
		description = *in.Description
	}
	channel := existing.Channel
	if in.Channel != nil {
		if !smsValidEnum(*in.Channel, smsChannels...) {
			respondBadRequest(w, r, "invalid channel")
			return
		}
		channel = *in.Channel
	}
	status := existing.Status
	if in.Status != nil {
		if !smsValidEnum(*in.Status, smsCampaignStatuses...) {
			respondBadRequest(w, r, "invalid status")
			return
		}
		status = *in.Status
	}
	scheduledAt := existing.ScheduledAt
	if in.ScheduledAt != nil {
		if in.ScheduledAt.IsZero() {
			scheduledAt = nil
		} else {
			v := *in.ScheduledAt
			scheduledAt = &v
		}
	}
	dueBy := ""
	if existing.DueBy != nil {
		dueBy = existing.DueBy.Format("2006-01-02")
	}
	if in.DueBy != nil {
		dueBy = *in.DueBy
	}
	if dueBy != "" && !validSMSDate(dueBy) {
		respondBadRequest(w, r, "due_by must be YYYY-MM-DD")
		return
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

	if _, err := h.db.ExecContext(ctx, `UPDATE sms_notification_campaigns SET title = ?, description = ?, entity_type = ?, entity_id = ?, channel = ?, scheduled_at = ?, due_by = ?, status = ? WHERE id = ?`,
		title, description, nullStringOrEmpty(entityType), smsEntityIDArg(entityID), channel, scheduledAt, nullStringOrEmpty(dueBy), status, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	updated, err := h.smsCampaignCoverage(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_campaign_updated", "sms_notification_campaign", &updated.ID, updated.Title, map[string]any{"status": status})
	respondJSONOK(w, updated)
}

// DeleteNotificationCampaign removes a campaign; its targets and ledger rows
// cascade via foreign keys.
func (h *SMSHandler) DeleteNotificationCampaign(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var title string
	err := h.db.QueryRowContext(ctx, `SELECT title FROM sms_notification_campaigns WHERE id = ?`, id).Scan(&title)
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_notification_campaigns WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_campaign_deleted", "sms_notification_campaign", nil, title, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Campaign targets.
// ---------------------------------------------------------------------------

// ListCampaignTargets returns the node targets of one campaign with resolved
// node names and kind labels.
func (h *SMSHandler) ListCampaignTargets(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_notification_campaigns WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+smsCampaignTargetColumns+`, nd.name AS node_name, k.label AS kind_label FROM sms_campaign_targets t JOIN sms_nodes nd ON nd.id = t.node_id JOIN sms_node_kinds k ON k.id = nd.kind_id WHERE t.campaign_id = ? ORDER BY nd.name COLLATE NOCASE`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSCampaignTarget, 0)
	for rows.Next() {
		t := &models.SMSCampaignTarget{}
		if err := rows.Scan(&t.ID, &t.CampaignID, &t.NodeID, &t.IncludeDescendants, &t.NodeName, &t.KindLabel); err != nil {
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

// AddCampaignTarget adds a node target to a campaign. include_descendants
// (default false) widens the audience generation to the whole subtree.
func (h *SMSHandler) AddCampaignTarget(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		NodeID             int  `json:"node_id"`
		IncludeDescendants bool `json:"include_descendants"`
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
	var nodeName string
	err := h.db.QueryRowContext(ctx, `SELECT name FROM sms_nodes WHERE id = ?`, in.NodeID).Scan(&nodeName)
	if isNotFound(err) {
		respondNotFound(w, r, "Node not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_campaign_targets WHERE campaign_id = ? AND node_id = ?`, id, in.NodeID).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "node is already a target of this campaign")
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_campaign_targets (campaign_id, node_id, include_descendants) VALUES (?, ?, ?)`, id, in.NodeID, in.IncludeDescendants)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	tid64, _ := res.LastInsertId()
	t, err := scanSMSCampaignTarget(h.db.QueryRowContext(ctx, `SELECT `+smsCampaignTargetColumns+` FROM sms_campaign_targets t WHERE t.id = ?`, int(tid64)))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	t.NodeName = nodeName
	smsAudit(h, r, user, "sms_campaign_target_added", "sms_campaign_target", &t.ID, nodeName, map[string]any{"campaign_id": id})
	respondJSONCreated(w, t)
}

// RemoveCampaignTarget deletes one target row by its own id (route param tid).
func (h *SMSHandler) RemoveCampaignTarget(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	tid, ok := requireIDParam(w, r, "tid")
	if !ok {
		return
	}
	ctx := r.Context()
	var nodeName string
	err := h.db.QueryRowContext(ctx, `SELECT nd.name FROM sms_campaign_targets t JOIN sms_nodes nd ON nd.id = t.node_id WHERE t.id = ?`, tid).Scan(&nodeName)
	if isNotFound(err) {
		respondNotFound(w, r, "Campaign target not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sms_campaign_targets WHERE id = ?`, tid); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_campaign_target_removed", "sms_campaign_target", nil, nodeName, map[string]any{"target_id": tid})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Campaign audience + ledger.
// ---------------------------------------------------------------------------

// GenerateCampaignAudience materializes the per-stakeholder delivery ledger for
// a campaign: every distinct member of each target node (plus descendants when
// requested) gets one pending notification row. Existing rows are skipped.
func (h *SMSHandler) GenerateCampaignAudience(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var title, channel string
	var dueBy *time.Time
	err := h.db.QueryRowContext(ctx, `SELECT title, channel, due_by FROM sms_notification_campaigns WHERE id = ?`, id).Scan(&title, &channel, &dueBy)
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	targetRows, err := tx.QueryContext(ctx, `SELECT node_id, include_descendants FROM sms_campaign_targets WHERE campaign_id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	nodeIDs := make(map[int]bool)
	type walk struct {
		root        int
		descendants bool
	}
	walks := make([]walk, 0)
	for targetRows.Next() {
		var nodeID int
		var descendants bool
		if err := targetRows.Scan(&nodeID, &descendants); err != nil {
			targetRows.Close()
			respondInternalError(w, r, err)
			return
		}
		nodeIDs[nodeID] = true
		if descendants {
			walks = append(walks, walk{root: nodeID, descendants: true})
		}
	}
	targetRows.Close()
	if err := targetRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Iterative breadth-first descent for each include_descendants target.
	for _, wk := range walks {
		queue := []int{wk.root}
		seen := map[int]bool{wk.root: true}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			childRows, err := tx.QueryContext(ctx, `SELECT id FROM sms_nodes WHERE parent_id = ?`, cur)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			children := make([]int, 0)
			for childRows.Next() {
				var child int
				if err := childRows.Scan(&child); err != nil {
					childRows.Close()
					respondInternalError(w, r, err)
					return
				}
				children = append(children, child)
			}
			childRows.Close()
			if err := childRows.Err(); err != nil {
				respondInternalError(w, r, err)
				return
			}
			for _, child := range children {
				if seen[child] {
					continue
				}
				seen[child] = true
				nodeIDs[child] = true
				queue = append(queue, child)
			}
		}
	}

	var created, skipped int
	if len(nodeIDs) > 0 {
		ids := make([]int, 0, len(nodeIDs))
		for nid := range nodeIDs {
			ids = append(ids, nid)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		idArgs := make([]any, len(ids))
		for i, nid := range ids {
			idArgs[i] = nid
		}
		memberRows, err := tx.QueryContext(ctx, `SELECT DISTINCT node_id, stakeholder_id FROM sms_node_members WHERE node_id IN (`+placeholders+`) ORDER BY stakeholder_id`, idArgs...)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		members := make([][2]int, 0)
		for memberRows.Next() {
			var nodeID, shID int
			if err := memberRows.Scan(&nodeID, &shID); err != nil {
				memberRows.Close()
				respondInternalError(w, r, err)
				return
			}
			members = append(members, [2]int{nodeID, shID})
		}
		memberRows.Close()
		if err := memberRows.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}

		var dueByArg any
		if dueBy != nil {
			dueByArg = dueBy.Format("2006-01-02")
		}
		for _, m := range members {
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO sms_notifications (campaign_id, stakeholder_id, node_id, required_by, status, channel) VALUES (?, ?, ?, ?, 'pending', ?)`, id, m[1], m[0], dueByArg, channel)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			affected, _ := res.RowsAffected()
			if affected > 0 {
				created++
			} else {
				skipped++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_campaign_audience_generated", "sms_notification_campaign", &id, title, map[string]any{"created": created, "skipped": skipped})
	respondJSONOK(w, map[string]any{"created": created, "skipped": skipped})
}

// ListCampaignNotifications returns the delivery ledger of one campaign joined
// to stakeholder and node names. Optional filter: ?status.
func (h *SMSHandler) ListCampaignNotifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_notification_campaigns WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		respondNotFound(w, r, "Notification campaign not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	query := `SELECT ` + smsNotificationColumns + `, s.name AS name, s.organization AS organization, COALESCE(nd.name, '') AS node_name FROM sms_notifications n JOIN sms_stakeholders s ON s.id = n.stakeholder_id LEFT JOIN sms_nodes nd ON nd.id = n.node_id WHERE n.campaign_id = ?`
	args := []any{id}
	if v := r.URL.Query().Get("status"); v != "" {
		query += ` AND n.status = ?`
		args = append(args, v)
	}
	query += ` ORDER BY s.name COLLATE NOCASE, s.organization COLLATE NOCASE`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]models.SMSNotification, 0)
	for rows.Next() {
		nr := &models.SMSNotification{}
		if err := rows.Scan(&nr.ID, &nr.CampaignID, &nr.StakeholderID, &nr.NodeID, &nr.RequiredBy, &nr.Status, &nr.Channel, &nr.SentAt, &nr.AcknowledgedAt, &nr.CommunicationID, &nr.Notes, &nr.CreatedAt, &nr.UpdatedAt, &nr.Name, &nr.Organization, &nr.NodeName); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, *nr)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// UpdateNotification patches one ledger row. Transitioning to 'sent' stamps
// sent_at, falls back to the campaign channel and logs an outbound
// sms_communications entry; transitioning to 'acknowledged' stamps acknowledged_at.
func (h *SMSHandler) UpdateNotification(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status  *string `json:"status,omitempty"`
		Channel *string `json:"channel,omitempty"`
		Notes   *string `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := scanSMSNotification(h.db.QueryRowContext(ctx, `SELECT `+smsNotificationColumns+` FROM sms_notifications n WHERE n.id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Notification not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}
	desiredStatus := existing.Status
	if in.Status != nil {
		if !smsValidEnum(*in.Status, smsNotificationStatuses...) {
			respondBadRequest(w, r, "invalid status")
			return
		}
		desiredStatus = *in.Status
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, notFound, err := smsApplyNotificationUpdate(ctx, tx, user, id, desiredStatus, in.Channel, in.Notes); err != nil {
		respondInternalError(w, r, err)
		return
	} else if notFound {
		respondNotFound(w, r, "Notification not found")
		return
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	updated, err := h.smsGetNotification(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_notification_updated", "sms_notification", &updated.ID, updated.Name, map[string]any{"status": desiredStatus})
	respondJSONOK(w, updated)
}

// BulkUpdateNotifications moves many ledger rows to one status in a single
// transaction, applying the same side effects as UpdateNotification per row.
// Missing ids are skipped.
func (h *SMSHandler) BulkUpdateNotifications(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		IDs    []int  `json:"ids"`
		Status string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if len(in.IDs) == 0 {
		respondBadRequest(w, r, "ids is required")
		return
	}
	if !smsValidEnum(in.Status, smsNotificationStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	updated := 0
	for _, nid := range in.IDs {
		okRow, notFound, err := smsApplyNotificationUpdate(ctx, tx, user, nid, in.Status, nil, nil)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if okRow {
			updated++
		} else if notFound {
			// Missing rows are skipped silently.
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	smsAudit(h, r, user, "sms_notifications_bulk_updated", "sms_notification", nil, fmt.Sprintf("%d notifications", updated), map[string]any{"status": in.Status, "updated": updated})
	respondJSONOK(w, map[string]any{"updated": updated})
}
