package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"windshift/internal/models"
)

const p3mChangeCalendarColumns = `id, entity_type, entity_id, activity, business_unit, start_week, end_week, impact_level, source, notes, created_by, created_at, updated_at`

var isoWeekPattern = regexp.MustCompile(`^(\d{4})-W([0-9]{2})$`)

func scanP3MChangeCalendarEntry(row interface{ Scan(...any) error }) (models.P3MChangeCalendarEntry, error) {
	var e models.P3MChangeCalendarEntry
	var entityType sql.NullString
	var entityID sql.NullInt64
	var businessUnit, notes sql.NullString
	var createdBy sql.NullInt64
	err := row.Scan(&e.ID, &entityType, &entityID, &e.Activity, &businessUnit, &e.StartWeek, &e.EndWeek,
		&e.ImpactLevel, &e.Source, &notes, &createdBy, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	if entityType.Valid {
		e.EntityType = &entityType.String
	}
	if entityID.Valid {
		v := int(entityID.Int64)
		e.EntityID = &v
	}
	if businessUnit.Valid {
		e.BusinessUnit = &businessUnit.String
	}
	if notes.Valid {
		e.Notes = &notes.String
	}
	if createdBy.Valid {
		v := int(createdBy.Int64)
		e.CreatedBy = &v
	}
	return e, nil
}

// isoWeekString renders a date as its ISO week label (e.g. "2026-W45").
func isoWeekString(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// parseISOWeek converts an ISO week label to the Monday of that week. The
// result is validated by round-tripping through ISOWeek so labels like
// "2026-W53" (a 52-week year) are rejected rather than silently rolling over.
func parseISOWeek(label string) (time.Time, bool) {
	m := isoWeekPattern.FindStringSubmatch(label)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	week, _ := strconv.Atoi(m[2])
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	mondayOfWeek1 := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7))
	monday := mondayOfWeek1.AddDate(0, 0, (week-1)*7)
	y, w := monday.ISOWeek()
	if y != year || w != week {
		return time.Time{}, false
	}
	return monday, true
}

// p3mChangeCalendarInput is the create/update payload for a calendar entry.
type p3mChangeCalendarInput struct {
	Activity     string  `json:"activity"`
	BusinessUnit *string `json:"business_unit"`
	EntityType   *string `json:"entity_type"`
	EntityID     *int    `json:"entity_id"`
	StartWeek    string  `json:"start_week"`
	EndWeek      string  `json:"end_week"`
	ImpactLevel  int     `json:"impact_level"`
	Source       string  `json:"source"`
	Notes        *string `json:"notes"`
}

// validateP3MChangeCalendarInput checks the shared invariants of a calendar
// entry and returns normalised values. ISO week labels compare correctly as
// strings, so start<=end is checked on the parsed Mondays.
func (h *P3MHandler) validateP3MChangeCalendarInput(w http.ResponseWriter, r *http.Request, in p3mChangeCalendarInput) (p3mChangeCalendarInput, bool) {
	if in.Activity == "" {
		respondValidationError(w, r, "activity is required")
		return in, false
	}
	startMonday, okStart := parseISOWeek(in.StartWeek)
	endMonday, okEnd := parseISOWeek(in.EndWeek)
	if !okStart || !okEnd {
		respondValidationError(w, r, "start_week and end_week must be ISO week labels like 2026-W45")
		return in, false
	}
	if startMonday.After(endMonday) {
		respondValidationError(w, r, "start_week must not be after end_week")
		return in, false
	}
	if in.ImpactLevel == 0 {
		in.ImpactLevel = 3
	}
	if in.ImpactLevel < 1 || in.ImpactLevel > 5 {
		respondValidationError(w, r, "impact_level must be between 1 and 5")
		return in, false
	}
	if in.Source == "" {
		in.Source = "local"
	}
	switch in.Source {
	case "corporate_project", "corporate_activity", "bau", "local":
	default:
		respondValidationError(w, r, "source must be corporate_project, corporate_activity, bau or local")
		return in, false
	}
	if (in.EntityType == nil) != (in.EntityID == nil) {
		respondValidationError(w, r, "entity_type and entity_id must both be set or both omitted")
		return in, false
	}
	if in.EntityType != nil {
		switch *in.EntityType {
		case "portfolio", "programme", "project":
		default:
			respondValidationError(w, r, "entity_type must be portfolio, programme or project")
			return in, false
		}
	}
	return in, true
}

// GetChangeCalendarWeek handles GET /api/p3m/change-calendar/week. It returns
// the weekly load grid (CHG-009): for each ISO week from `from` (a date,
// default today) over `weeks` weeks (default 8), the entries active in that
// week and their summed impact as the load figure.
func (h *P3MHandler) GetChangeCalendarWeek(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	fromDate := time.Now().UTC()
	if v := r.URL.Query().Get("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondValidationError(w, r, "from must be a date like 2026-11-10")
			return
		}
		fromDate = parsed.UTC()
	}
	count := 8
	if v := r.URL.Query().Get("weeks"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 52 {
			respondValidationError(w, r, "weeks must be between 1 and 52")
			return
		}
		count = n
	}

	baseMonday := fromDate.AddDate(0, 0, -((int(fromDate.Weekday())+6)%7))
	type weekEntry struct {
		ID          int     `json:"id"`
		Activity    string  `json:"activity"`
		ImpactLevel int     `json:"impact_level"`
	}
	type weekRow struct {
		Week    string      `json:"week"`
		Start   string      `json:"start"`
		End     string      `json:"end"`
		Load    int         `json:"load"`
		Entries []weekEntry `json:"entries"`
	}
	weeks := make([]weekRow, 0, count)
	for i := 0; i < count; i++ {
		monday := baseMonday.AddDate(0, 0, i*7)
		sunday := monday.AddDate(0, 0, 6)
		label := isoWeekString(monday)

		rows, err := h.db.QueryContext(ctx, `SELECT id, activity, impact_level FROM p3m_change_calendar_entries WHERE start_week <= ? AND end_week >= ? ORDER BY impact_level DESC, id`, label, label)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		row := weekRow{Week: label, Start: monday.Format("2006-01-02"), End: sunday.Format("2006-01-02"), Entries: []weekEntry{}}
		for rows.Next() {
			var e weekEntry
			if err := rows.Scan(&e.ID, &e.Activity, &e.ImpactLevel); err != nil {
				rows.Close()
				respondInternalError(w, r, err)
				return
			}
			row.Load += e.ImpactLevel
			row.Entries = append(row.Entries, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		rows.Close()
		weeks = append(weeks, row)
	}
	respondJSONOK(w, map[string]any{"weeks": weeks})
}

// ListChangeCalendarEntries handles GET /api/p3m/change-calendar. Optional
// filters: from_week/to_week (ISO labels; entries overlapping the range),
// entity_type and source.
func (h *P3MHandler) ListChangeCalendarEntries(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mChangeCalendarColumns + ` FROM p3m_change_calendar_entries WHERE 1=1`
	args := []any{}
	q := r.URL.Query()
	if v := q.Get("from_week"); v != "" {
		if _, ok := parseISOWeek(v); !ok {
			respondValidationError(w, r, "from_week must be an ISO week label like 2026-W45")
			return
		}
		query += ` AND end_week >= ?`
		args = append(args, v)
	}
	if v := q.Get("to_week"); v != "" {
		if _, ok := parseISOWeek(v); !ok {
			respondValidationError(w, r, "to_week must be an ISO week label like 2026-W45")
			return
		}
		query += ` AND start_week <= ?`
		args = append(args, v)
	}
	if v := q.Get("entity_type"); v != "" {
		query += ` AND entity_type = ?`
		args = append(args, v)
	}
	if v := q.Get("source"); v != "" {
		query += ` AND source = ?`
		args = append(args, v)
	}
	query += ` ORDER BY start_week, id`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]models.P3MChangeCalendarEntry, 0)
	for rows.Next() {
		e, err := scanP3MChangeCalendarEntry(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// CreateChangeCalendarEntry handles POST /api/p3m/change-calendar.
func (h *P3MHandler) CreateChangeCalendarEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in p3mChangeCalendarInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in, ok = h.validateP3MChangeCalendarInput(w, r, in)
	if !ok {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `INSERT INTO p3m_change_calendar_entries (entity_type, entity_id, activity, business_unit, start_week, end_week, impact_level, source, notes, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.EntityType, in.EntityID, in.Activity, in.BusinessUnit, in.StartWeek, in.EndWeek, in.ImpactLevel, in.Source, in.Notes, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	e, err := scanP3MChangeCalendarEntry(h.db.QueryRowContext(r.Context(), `SELECT `+p3mChangeCalendarColumns+` FROM p3m_change_calendar_entries WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	newID := int(id)
	logAudit(h.db, r, user, "p3m_change_calendar_created", "p3m_change_calendar_entry", &newID, in.Activity)
	respondJSONCreated(w, e)
}

// GetChangeCalendarEntry handles GET /api/p3m/change-calendar/{id}.
func (h *P3MHandler) GetChangeCalendarEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	e, err := scanP3MChangeCalendarEntry(h.db.QueryRowContext(r.Context(), `SELECT `+p3mChangeCalendarColumns+` FROM p3m_change_calendar_entries WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_change_calendar_entry")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, e)
}

// UpdateChangeCalendarEntry handles PUT /api/p3m/change-calendar/{id}. The
// payload is a full record write (same shape as create).
func (h *P3MHandler) UpdateChangeCalendarEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in p3mChangeCalendarInput
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in, ok = h.validateP3MChangeCalendarInput(w, r, in)
	if !ok {
		return
	}

	res, err := h.db.ExecContext(r.Context(), `UPDATE p3m_change_calendar_entries SET entity_type = ?, entity_id = ?, activity = ?, business_unit = ?, start_week = ?, end_week = ?, impact_level = ?, source = ?, notes = ? WHERE id = ?`,
		in.EntityType, in.EntityID, in.Activity, in.BusinessUnit, in.StartWeek, in.EndWeek, in.ImpactLevel, in.Source, in.Notes, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_change_calendar_entry")
		return
	}
	e, err := scanP3MChangeCalendarEntry(h.db.QueryRowContext(r.Context(), `SELECT `+p3mChangeCalendarColumns+` FROM p3m_change_calendar_entries WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_change_calendar_updated", "p3m_change_calendar_entry", &id, in.Activity)
	respondJSONOK(w, e)
}

// DeleteChangeCalendarEntry handles DELETE /api/p3m/change-calendar/{id}.
func (h *P3MHandler) DeleteChangeCalendarEntry(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_change_calendar_entries WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_change_calendar_entry")
		return
	}
	logAudit(h.db, r, user, "p3m_change_calendar_deleted", "p3m_change_calendar_entry", &id, "")
	respondJSONOK(w, map[string]any{"deleted": true})
}
