package handlers

import (
	"fmt"
	"net/http"
	"strings"
)

// smsStakeholderOwnedTables lists every table that carries a stakeholder_id
// foreign key into sms_stakeholders. Merge re-points these rows from the
// source to the target record; extend this list when new sub-resources land.
var smsStakeholderOwnedTables = []string{
	"sms_communications",
	"sms_issues",
	"sms_tasks",
	"sms_feedback",
	"sms_relationship_goals",
	"sms_engagement_plans",
	"sms_stakeholder_entities",
	"sms_commitments",
	"sms_grievances",
	"sms_decisions",
	"sms_ownership_transfers",
}

// smsDuplicateKey derives the identity fingerprint used to group likely
// duplicate register entries: normalized name, disambiguated by email when
// present, otherwise by the digits of the phone number.
func smsDuplicateKey(name, email, phone string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if e := strings.ToLower(strings.TrimSpace(email)); e != "" {
		return key + "|" + e
	}
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		return key + "|p" + b.String()
	}
	return key
}

type smsDuplicateEntry struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Tier         string `json:"tier"`
}

type smsDuplicateGroup struct {
	Key          string               `json:"key"`
	Stakeholders []smsDuplicateEntry  `json:"stakeholders"`
}

// ListDuplicateStakeholders returns groups of register entries that share an
// identity fingerprint (name, plus email or phone when available). Groups of
// one are omitted.
func (h *SMSHandler) ListDuplicateStakeholders(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	rows, err := h.db.QueryContext(ctx, `SELECT id, name, title, organization, email, phone, tier FROM sms_stakeholders ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	groups := make(map[string]*smsDuplicateGroup)
	order := make([]string, 0)
	for rows.Next() {
		var e smsDuplicateEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Title, &e.Organization, &e.Email, &e.Phone, &e.Tier); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		key := smsDuplicateKey(e.Name, e.Email, e.Phone)
		g, ok := groups[key]
		if !ok {
			g = &smsDuplicateGroup{Key: key, Stakeholders: make([]smsDuplicateEntry, 0)}
			groups[key] = g
			order = append(order, key)
		}
		g.Stakeholders = append(g.Stakeholders, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}

	out := make([]smsDuplicateGroup, 0)
	for _, key := range order {
		if len(groups[key].Stakeholders) > 1 {
			out = append(out, *groups[key])
		}
	}
	respondJSONOK(w, map[string]any{"groups": out})
}

// MergeStakeholder folds the source stakeholder (body.source_id) into the
// target stakeholder (path id): every sub-resource row is re-pointed at the
// target, P3M/SRM link columns are updated, and the source row is deleted.
func (h *SMSHandler) MergeStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	targetID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		SourceID int `json:"source_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.SourceID == targetID {
		respondBadRequest(w, r, "source_id must differ from the target id")
		return
	}

	ctx := r.Context()
	for _, id := range []int{targetID, in.SourceID} {
		exists, err := h.smsStakeholderExists(ctx, id)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "Stakeholder not found")
			return
		}
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer tx.Rollback()

	for _, table := range smsStakeholderOwnedTables {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET stakeholder_id = ? WHERE stakeholder_id = ?`, targetID, in.SourceID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	// If both records sat on the same engagement plan, the membership table
	// now holds two rows for that plan; keep the lowest id of each pair.
	dups, err := tx.QueryContext(ctx, `SELECT plan_id FROM sms_engagement_plan_stakeholders GROUP BY plan_id HAVING COUNT(*) > 1`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var dupPlans []int
	for dups.Next() {
		var pid int
		if err := dups.Scan(&pid); err != nil {
			dups.Close()
			respondInternalError(w, r, err)
			return
		}
		dupPlans = append(dupPlans, pid)
	}
	dups.Close()
	for _, pid := range dupPlans {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sms_engagement_plan_stakeholders WHERE plan_id = ? AND id NOT IN (SELECT MIN(id) FROM sms_engagement_plan_stakeholders WHERE plan_id = ?)`, pid, pid); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	// Group memberships follow the same pattern: keep the lowest id of each pair.
	dupsG, err := tx.QueryContext(ctx, `SELECT group_id FROM sms_stakeholder_group_members GROUP BY group_id HAVING COUNT(*) > 1`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	var dupGroups []int
	for dupsG.Next() {
		var gid int
		if err := dupsG.Scan(&gid); err != nil {
			dupsG.Close()
			respondInternalError(w, r, err)
			return
		}
		dupGroups = append(dupGroups, gid)
	}
	dupsG.Close()
	for _, gid := range dupGroups {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sms_stakeholder_group_members WHERE group_id = ? AND id NOT IN (SELECT MIN(id) FROM sms_stakeholder_group_members WHERE group_id = ?)`, gid, gid); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	// Stakeholder links re-point on either end; a link that becomes
	// self-referential violates the source < target invariant and is dropped.
	if _, err := tx.ExecContext(ctx, `UPDATE sms_stakeholder_links SET source_id = ? WHERE source_id = ?`, targetID, in.SourceID); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sms_stakeholder_links SET target_id = ? WHERE target_id = ?`, targetID, in.SourceID); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sms_stakeholder_links WHERE source_id = target_id`); err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Re-point cross-store links that reference the source record.
	for _, table := range []string{"p3m_stakeholders", "srm_stakeholders"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET sms_stakeholder_id = ? WHERE sms_stakeholder_id = ?`, targetID, in.SourceID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM sms_stakeholders WHERE id = ?`, in.SourceID); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	s, err := h.smsGetStakeholder(ctx, targetID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_stakeholder_merged", "sms_stakeholder", &s.ID, s.Name, map[string]any{"source_id": in.SourceID})
	respondJSONOK(w, s)
}

// crossStoreDuplicateRow is one identity match between the SMS register and a
// parallel store (p3m by name, srm by email). Rows whose link is missing or
// points at a different record are the actionable duplicates.
type crossStoreDuplicateRow struct {
	Store          string `json:"store"` // p3m | srm
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email,omitempty"`
	LinkedSMSID    *int   `json:"linked_sms_id,omitempty"`
	CandidateSMSID int    `json:"candidate_sms_id"`
	CandidateName  string `json:"candidate_name"`
	MatchOn        string `json:"match_on"` // name | email
}

// smsCrossStoreDuplicateQuery is the identity-match report between the SMS
// register and the parallel p3m/srm stakeholder stores (WI-0.2): p3m matches
// by name, srm by email. Shared by ListCrossStoreDuplicates and the
// data-quality engine.
const smsCrossStoreDuplicateQuery = `
SELECT 'p3m' AS store, ps.id, ps.name, '' AS email, ps.sms_stakeholder_id, s.id, s.name, 'name' AS match_on
FROM p3m_stakeholders ps JOIN sms_stakeholders s ON lower(ps.name) = lower(s.name) WHERE ps.name <> ''
UNION ALL
SELECT 'srm', ss.id, ss.full_name, ss.email, ss.sms_stakeholder_id, s.id, s.name, 'email'
FROM srm_stakeholders ss JOIN sms_stakeholders s ON lower(ss.email) = lower(s.email) WHERE ss.email <> ''
ORDER BY 1, 2`

// ListCrossStoreDuplicates reports identity matches between the SMS register
// and the parallel p3m/srm stakeholder stores (WI-0.2).
func (h *SMSHandler) ListCrossStoreDuplicates(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), smsCrossStoreDuplicateQuery)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]crossStoreDuplicateRow, 0)
	for rows.Next() {
		var row crossStoreDuplicateRow
		if err := rows.Scan(&row.Store, &row.ID, &row.Name, &row.Email, &row.LinkedSMSID, &row.CandidateSMSID, &row.CandidateName, &row.MatchOn); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// LinkCrossStoreStakeholder sets the sms_stakeholder_id link on one p3m/srm
// stakeholder row after review (null/0 clears it).
func (h *SMSHandler) LinkCrossStoreStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Store           string `json:"store"`
		ID              int    `json:"id"`
		SMSTakeholderID *int   `json:"sms_stakeholder_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	var table string
	switch in.Store {
	case "p3m":
		table = "p3m_stakeholders"
	case "srm":
		table = "srm_stakeholders"
	default:
		respondBadRequest(w, r, "store must be 'p3m' or 'srm'")
		return
	}
	if in.ID <= 0 {
		respondBadRequest(w, r, "id is required")
		return
	}

	ctx := r.Context()
	var link *int
	if in.SMSTakeholderID != nil && *in.SMSTakeholderID > 0 {
		exists, err := h.smsStakeholderExists(ctx, *in.SMSTakeholderID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !exists {
			respondNotFound(w, r, "SMS stakeholder not found")
			return
		}
		link = in.SMSTakeholderID
	}

	res, err := h.db.ExecContext(ctx, `UPDATE `+table+` SET sms_stakeholder_id = ? WHERE id = ?`, link, in.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	details := map[string]any{"store": in.Store}
	if link != nil {
		details["sms_stakeholder_id"] = *link
	} else {
		details["cleared"] = true
	}
	smsAudit(h, r, user, "sms_cross_store_linked", "sms_cross_store_link", &in.ID, fmt.Sprintf("%s:%d", in.Store, in.ID), details)
	w.WriteHeader(http.StatusNoContent)
}
