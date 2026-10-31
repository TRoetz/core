package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GetCommunications returns the cross-stakeholder communication feed with
// stakeholder names joined in. Supports ?limit= (default 200, max 500).
func (h *SMSHandler) GetCommunications(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	limit := 200
	if v, ok := parseQueryInt(r.URL.Query().Get("limit")); ok && v > 0 {
		limit = v
		if limit > 500 {
			limit = 500
		}
	}

	query := `SELECT c.id, c.stakeholder_id, s.name, c.channel, c.direction, c.subject, c.body, c.sentiment, c.created_at FROM sms_communications c JOIN sms_stakeholders s ON s.id = c.stakeholder_id`
	args := make([]any, 0, 2)
	if sid, ok := parseQueryInt(r.URL.Query().Get("stakeholder_id")); ok && sid > 0 {
		query += ` WHERE c.stakeholder_id = ?`
		args = append(args, sid)
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type commRow struct {
		ID              int       `json:"id"`
		StakeholderID   int       `json:"stakeholder_id"`
		StakeholderName string    `json:"stakeholder_name"`
		Channel         string    `json:"channel"`
		Direction       string    `json:"direction"`
		Subject         string    `json:"subject"`
		Body            string    `json:"body"`
		Sentiment       *string   `json:"sentiment,omitempty"`
		CreatedAt       time.Time `json:"created_at"`
	}

	out := make([]commRow, 0)
	for rows.Next() {
		var c commRow
		if err := rows.Scan(&c.ID, &c.StakeholderID, &c.StakeholderName, &c.Channel, &c.Direction, &c.Subject, &c.Body, &c.Sentiment, &c.CreatedAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// ListEngagementPlans returns the cross-stakeholder engagement plan list for
// the Engagement tab. Supports ?status= (validated against the plan status
// enum); rows are ordered by stakeholder name then start date and include the
// joined stakeholder_name.
func (h *SMSHandler) ListEngagementPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !smsValidEnum(status, smsPlanStatuses...) {
		respondBadRequest(w, r, "invalid status")
		return
	}

	query := `SELECT p.id, p.stakeholder_id, s.name, p.strategy, p.frequency, p.channel, p.status, p.start_date, p.end_date, p.notes FROM sms_engagement_plans p JOIN sms_stakeholders s ON s.id = p.stakeholder_id`
	args := make([]any, 0, 1)
	if status != "" {
		query += ` WHERE p.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY s.name COLLATE NOCASE, CASE WHEN p.start_date IS NULL THEN 1 ELSE 0 END, p.start_date`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	type planRow struct {
		ID              int        `json:"id"`
		StakeholderID   int        `json:"stakeholder_id"`
		StakeholderName string     `json:"stakeholder_name"`
		Strategy        string     `json:"strategy"`
		Frequency       string     `json:"frequency"`
		Channel         string     `json:"channel"`
		Status          string     `json:"status"`
		StartDate       *time.Time `json:"start_date,omitempty"`
		EndDate         *time.Time `json:"end_date,omitempty"`
		Notes           string     `json:"notes"`
	}

	out := make([]planRow, 0)
	for rows.Next() {
		var p planRow
		if err := rows.Scan(&p.ID, &p.StakeholderID, &p.StakeholderName, &p.Strategy, &p.Frequency, &p.Channel, &p.Status, &p.StartDate, &p.EndDate, &p.Notes); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// GetTimeline returns a merged activity feed for one stakeholder covering
// communications, issues, tasks, feedback, and goals. Supports ?limit=
// (default 50, max 200).
func (h *SMSHandler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	limit := 50
	if v, ok := parseQueryInt(r.URL.Query().Get("limit")); ok && v > 0 {
		limit = v
		if limit > 200 {
			limit = 200
		}
	}

	ctx := r.Context()
	type timelineEntry struct {
		Type    string    `json:"type"`
		ID      int       `json:"id"`
		Title   string    `json:"title"`
		Summary string    `json:"summary,omitempty"`
		At      time.Time `json:"at"`
	}

	var entries []timelineEntry

	rows, err := h.db.QueryContext(ctx, `SELECT id, subject, channel, direction, sentiment, created_at FROM sms_communications WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, id, limit)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var (
			cid       int
			subject   string
			channel   string
			direction string
			sentiment *string
			createdAt time.Time
		)
		if err := rows.Scan(&cid, &subject, &channel, &direction, &sentiment, &createdAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		title := subject
		if title == "" {
			title = channel
		}
		parts := make([]string, 0, 2)
		if direction != "" {
			parts = append(parts, direction)
		}
		if sentiment != nil && *sentiment != "" {
			parts = append(parts, *sentiment)
		}
		entries = append(entries, timelineEntry{Type: "communication", ID: cid, Title: title, Summary: strings.Join(parts, " · "), At: createdAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `SELECT id, title, status, priority, created_at FROM sms_issues WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, id, limit)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var (
			iid       int
			title     string
			status    string
			priority  string
			createdAt time.Time
		)
		if err := rows.Scan(&iid, &title, &status, &priority, &createdAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		entries = append(entries, timelineEntry{Type: "issue", ID: iid, Title: title, Summary: status + " / " + priority, At: createdAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `SELECT id, title, status, completed_at, created_at FROM sms_tasks WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, id, limit)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var (
			tid       int
			title     string
			status    string
			completed *time.Time
			createdAt time.Time
		)
		if err := rows.Scan(&tid, &title, &status, &completed, &createdAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		at := createdAt
		if completed != nil {
			at = *completed
		}
		entries = append(entries, timelineEntry{Type: "task", ID: tid, Title: title, Summary: status, At: at})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `SELECT id, category, source, resolution_status, created_at FROM sms_feedback WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, id, limit)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var (
			fid              int
			category         string
			source           string
			resolutionStatus string
			createdAt        time.Time
		)
		if err := rows.Scan(&fid, &category, &source, &resolutionStatus, &createdAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		title := category
		if title == "" {
			title = source
		}
		entries = append(entries, timelineEntry{Type: "feedback", ID: fid, Title: title, Summary: resolutionStatus, At: createdAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `SELECT id, goal, status, updated_at FROM sms_relationship_goals WHERE stakeholder_id = ? ORDER BY updated_at DESC LIMIT ?`, id, limit)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var (
			gid       int
			goal      string
			status    string
			updatedAt time.Time
		)
		if err := rows.Scan(&gid, &goal, &status, &updatedAt); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		entries = append(entries, timelineEntry{Type: "goal", ID: gid, Title: goal, Summary: status, At: updatedAt})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	rows.Close()

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	if entries == nil {
		entries = []timelineEntry{}
	}
	respondJSONOK(w, entries)
}

// ExportCSV streams a CSV export of either all stakeholders (with computed
// health scores) or the full communication log. Only ?scope=stakeholders and
// ?scope=communications are supported; format must be empty or "csv".
func (h *SMSHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	scope := r.URL.Query().Get("scope")
	format := r.URL.Query().Get("format")
	if format != "" && format != "csv" {
		respondBadRequest(w, r, "unsupported export format")
		return
	}

	ctx := r.Context()
	var filename string
	switch scope {
	case "stakeholders":
		filename = "sms-stakeholders.csv"
		rows, err := h.db.QueryContext(ctx, `SELECT id, name, title, organization, email, phone, role_classification, influence_level, interest_level, interests, engagement_status, lifecycle_stage, relationship_status, communication_preferences, custom_fields, notes, created_at, updated_at FROM sms_stakeholders ORDER BY name COLLATE NOCASE`)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer rows.Close()

		critMap, escMap, err := h.smsIssuePressure(ctx)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"id", "name", "title", "organization", "email", "phone", "role_classification", "influence_level", "interest_level", "interests", "engagement_status", "lifecycle_stage", "relationship_status", "communication_preferences", "custom_fields", "notes", "health_score", "created_at", "updated_at"}); err != nil {
			return
		}
		for rows.Next() {
			var (
				id, influence, interest int
				name                    string
				title                   string
				org                     string
				email                   string
				phone                   string
				classification          string
				interests               string
				engagement              string
				lifecycle               string
				relationship            string
				prefs                   string
				customFields            string
				notes                   string
				createdAt, updatedAt    time.Time
			)
			if err := rows.Scan(&id, &name, &title, &org, &email, &phone, &classification, &influence, &interest, &interests, &engagement, &lifecycle, &relationship, &prefs, &customFields, &notes, &createdAt, &updatedAt); err != nil {
				return
			}
			health := smsHealthScore(relationship, engagement, critMap[id], escMap[id])
			record := []string{strconv.Itoa(id), name, title, org, email, phone, classification, strconv.Itoa(influence), strconv.Itoa(interest), interests, engagement, lifecycle, relationship, prefs, customFields, notes, strconv.Itoa(health), createdAt.Format(time.RFC3339), updatedAt.Format(time.RFC3339)}
			if err := cw.Write(record); err != nil {
				return
			}
		}
		cw.Flush()

	case "communications":
		filename = "sms-communications.csv"
		rows, err := h.db.QueryContext(ctx, `SELECT c.id, s.name, c.channel, c.direction, c.subject, c.body, c.sentiment, c.created_at FROM sms_communications c JOIN sms_stakeholders s ON s.id = c.stakeholder_id ORDER BY c.created_at DESC`)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		defer rows.Close()

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"id", "stakeholder_name", "channel", "direction", "subject", "body", "sentiment", "created_at"}); err != nil {
			return
		}
		for rows.Next() {
			var (
				cid       int
				name      string
				channel   string
				direction string
				subject   string
				body      string
				sentiment *string
				createdAt time.Time
			)
			if err := rows.Scan(&cid, &name, &channel, &direction, &subject, &body, &sentiment, &createdAt); err != nil {
				return
			}
			record := []string{strconv.Itoa(cid), name, channel, direction, subject, body, smsNullableString(sentiment), createdAt.Format(time.RFC3339)}
			if err := cw.Write(record); err != nil {
				return
			}
		}
		cw.Flush()

	default:
		respondBadRequest(w, r, "scope must be 'stakeholders' or 'communications'")
	}
}

// smsNullableString renders a nullable string column for CSV output.
func smsNullableString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
