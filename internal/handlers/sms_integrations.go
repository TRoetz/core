package handlers

// External integrations (WI-6.1, WI-6.3): email-forward ingestion into the
// communications log and a flat Power BI feed of register health.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// smsExtractEmail pulls an address out of a mail header value:
// "Jane Example <jane@example.org>" -> "jane@example.org".
func smsExtractEmail(header string) string {
	s := strings.TrimSpace(header)
	if lt, gt := strings.IndexByte(s, '<'), strings.IndexByte(s, '>'); lt >= 0 && gt > lt {
		return strings.TrimSpace(s[lt+1 : gt])
	}
	return s
}

// IngestEmail records one forwarded email in the communications log (WI-6.1).
// The sender or recipient address is matched against sms_stakeholders.email;
// a sender match logs an inbound communication, otherwise a recipient match
// logs outbound. Unmatched mail gets 404 so gateways can alert on it.
func (h *SMSHandler) IngestEmail(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		From       string     `json:"from"`
		To         string     `json:"to"`
		Subject    string     `json:"subject"`
		Body       string     `json:"body"`
		ReceivedAt *time.Time `json:"received_at,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()

	matchStakeholder := func(addr string) (int, error) {
		addr = strings.ToLower(smsExtractEmail(addr))
		if addr == "" {
			return 0, nil
		}
		var id int
		err := h.db.QueryRowContext(ctx, `SELECT id FROM sms_stakeholders WHERE lower(email) = ? ORDER BY id LIMIT 1`, addr).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return id, err
	}

	stakeholderID, direction := 0, ""
	if sid, err := matchStakeholder(in.From); err != nil {
		respondInternalError(w, r, err)
		return
	} else if sid > 0 {
		stakeholderID, direction = sid, "inbound"
	} else {
		for _, part := range strings.FieldsFunc(in.To, func(c rune) bool { return c == ',' || c == ';' }) {
			sid, err := matchStakeholder(part)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			if sid > 0 {
				stakeholderID, direction = sid, "outbound"
				break
			}
		}
	}
	if stakeholderID == 0 {
		respondNotFound(w, r, "stakeholder")
		return
	}

	createdAt := time.Now().UTC()
	if in.ReceivedAt != nil {
		createdAt = in.ReceivedAt.UTC()
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_communications (stakeholder_id, channel, direction, subject, body, sentiment, created_by, created_at) VALUES (?, 'email', ?, ?, ?, NULL, NULL, ?)`,
		stakeholderID, direction, in.Subject, in.Body, createdAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	newID64, _ := res.LastInsertId()
	newID := int(newID64)

	smsAudit(h, r, user, "sms_email_ingested", "sms_communication", &newID, fmt.Sprintf("%s -> stakeholder %d", smsExtractEmail(in.From), stakeholderID), map[string]any{
		"direction": direction,
	})
	respondJSONCreated(w, map[string]any{
		"id":             newID,
		"stakeholder_id": stakeholderID,
		"direction":      direction,
	})
}

// PowerBIFeed returns a flat, denormalized snapshot of register health for BI
// tooling (WI-6.3): one row per stakeholder with computed health score and
// open-item counts. The response is a bare JSON array so Power BI's web data
// source can bind it directly; refresh on schedule from the BI side.
func (h *SMSHandler) PowerBIFeed(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT s.id, s.name, s.organization, COALESCE(s.tier, ''), s.relationship_status, COALESCE(s.trust_level, ''), s.engagement_status, s.last_engagement_at, (SELECT COUNT(*) FROM sms_issues i WHERE i.stakeholder_id = s.id AND i.status = 'open') AS open_issues, (SELECT COUNT(*) FROM sms_issues i WHERE i.stakeholder_id = s.id AND i.status = 'open' AND i.priority = 'critical') AS open_critical, (SELECT COUNT(*) FROM sms_issues i WHERE i.stakeholder_id = s.id AND i.status = 'open' AND i.escalation_level > 0) AS escalated_open, (SELECT COUNT(*) FROM sms_commitments c WHERE c.stakeholder_id = s.id AND c.status = 'open') AS open_commitments, (SELECT COUNT(*) FROM sms_grievances g WHERE g.stakeholder_id = s.id AND g.status <> 'closed') AS open_grievances FROM sms_stakeholders s ORDER BY s.name COLLATE NOCASE`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0, 64)
	for rows.Next() {
		var (
			id                  int
			name, org, tier     string
			relStatus           string
			trustLevel          string
			engStatus           string
			lastEngagement      sql.NullTime
			openIssues          int
			openCritical        int
			escalatedOpen       int
			openCommitments     int
			openGrievances      int
		)
		if err := rows.Scan(&id, &name, &org, &tier, &relStatus, &trustLevel, &engStatus, &lastEngagement, &openIssues, &openCritical, &escalatedOpen, &openCommitments, &openGrievances); err != nil {
			respondInternalError(w, r, err)
			return
		}
		row := map[string]any{
			"stakeholder_id":    id,
			"name":              name,
			"organization":      org,
			"tier":              tier,
			"relationship_status": relStatus,
			"trust_level":       trustLevel,
			"engagement_status": engStatus,
			"open_issues":       openIssues,
			"open_critical_issues": openCritical,
			"escalated_open_issues": escalatedOpen,
			"open_commitments":  openCommitments,
			"open_grievances":   openGrievances,
			"health_score":      smsHealthScore(relStatus, engStatus, openCritical, escalatedOpen),
		}
		if lastEngagement.Valid {
			row["last_engagement_at"] = lastEngagement.Time.UTC().Format(time.RFC3339)
		} else {
			row["last_engagement_at"] = nil
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}
