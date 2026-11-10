package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"windshift/internal/models"
)

const p3mBenefitMeasurementColumns = `id, benefit_id, due_date, completed_at, value, evidence, recorded_by, created_at`

func scanP3MBenefitMeasurement(row interface{ Scan(...any) error }) (models.P3MBenefitMeasurement, error) {
	var m models.P3MBenefitMeasurement
	var completedAt sql.NullTime
	var value sql.NullFloat64
	var evidence sql.NullString
	var recordedBy sql.NullInt64
	err := row.Scan(&m.ID, &m.BenefitID, &m.DueDate, &completedAt, &value, &evidence, &recordedBy, &m.CreatedAt)
	if err != nil {
		return m, err
	}
	if completedAt.Valid {
		m.CompletedAt = &completedAt.Time
	}
	if value.Valid {
		m.Value = &value.Float64
	}
	if evidence.Valid {
		m.Evidence = &evidence.String
	}
	if recordedBy.Valid {
		v := int(recordedBy.Int64)
		m.RecordedBy = &v
	}
	return m, nil
}

// p3mBenefitScopeClause builds the WHERE fragment selecting benefits in scope
// for a rollup: an explicit project, a programme (own + child projects), or a
// portfolio (all programmes and projects beneath it). Empty filters select all.
func p3mBenefitScopeClause(q map[string]string) (string, []any, bool) {
	var clauses []string
	args := []any{}
	if v := q["project_id"]; v != "" {
		clauses = append(clauses, "b.project_id = ?")
		args = append(args, v)
	}
	if v := q["programme_id"]; v != "" {
		clauses = append(clauses, "(b.programme_id = ? OR b.project_id IN (SELECT id FROM p3m_projects WHERE programme_id = ?))")
		args = append(args, v, v)
	}
	if v := q["portfolio_id"]; v != "" {
		clauses = append(clauses, `(b.programme_id IN (SELECT id FROM p3m_programmes WHERE portfolio_id = ?) OR b.project_id IN (SELECT id FROM p3m_projects WHERE portfolio_id = ?))`)
		args = append(args, v, v)
	}
	if len(clauses) == 0 {
		return "", nil, true
	}
	where := ""
	for i, c := range clauses {
		if i > 0 {
			where += " AND "
		}
		where += c
	}
	return where, args, true
}

// ListBenefitMeasurements handles GET /api/p3m/benefits/{id}/measurements. It
// returns the benefit's profile alongside its measurement schedule so clients
// can render progress without a second request (BEN-007).
func (h *P3MHandler) ListBenefitMeasurements(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var title, benefitType, unit string
	var targetValue float64
	var status string
	err := h.db.QueryRowContext(ctx, `SELECT title, benefit_type, unit_of_measure, target_value, status FROM p3m_benefits WHERE id = ?`, id).Scan(&title, &benefitType, &unit, &targetValue, &status)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_benefit")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+p3mBenefitMeasurementColumns+` FROM p3m_benefit_measurements WHERE benefit_id = ? ORDER BY due_date`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	measurements := make([]models.P3MBenefitMeasurement, 0)
	for rows.Next() {
		m, err := scanP3MBenefitMeasurement(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		measurements = append(measurements, m)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"benefit_id":       id,
		"title":            title,
		"benefit_type":     benefitType,
		"unit_of_measure":  unit,
		"target_value":     targetValue,
		"status":           status,
		"measurements":     measurements,
	})
}

// CreateBenefitMeasurement handles POST /api/p3m/benefits/{id}/measurements.
// A measurement created with a value is completed immediately; one without a
// value is a scheduled reminder point (BEN-007).
func (h *P3MHandler) CreateBenefitMeasurement(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		DueDate  string          `json:"due_date"`
		Value    json.RawMessage `json:"value"`
		Evidence *string         `json:"evidence"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if _, err := time.Parse("2006-01-02", in.DueDate); err != nil {
		respondValidationError(w, r, "due_date must be a date like 2026-11-30")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_benefits WHERE id = ?`, id).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists == 0 {
		respondNotFound(w, r, "p3m_benefit")
		return
	}

	var value *float64
	var completedAt *time.Time
	if len(in.Value) > 0 && string(in.Value) != "null" {
		var f float64
		if err := json.Unmarshal(in.Value, &f); err != nil {
			respondValidationError(w, r, "value must be a number")
			return
		}
		value = &f
		now := time.Now().UTC()
		completedAt = &now
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO p3m_benefit_measurements (benefit_id, due_date, completed_at, value, evidence, recorded_by) VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.DueDate, completedAt, value, in.Evidence, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rowID, _ := res.LastInsertId()
	m, err := scanP3MBenefitMeasurement(h.db.QueryRowContext(ctx, `SELECT `+p3mBenefitMeasurementColumns+` FROM p3m_benefit_measurements WHERE id = ?`, rowID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	newID := int(rowID)
	logAudit(h.db, r, user, "p3m_benefit_measurement_created", "p3m_benefit_measurement", &newID, in.DueDate)
	respondJSONCreated(w, m)
}

// UpdateBenefitMeasurement handles PUT /api/p3m/benefit-measurements/{id}.
// value semantics: absent keeps the current state; a number completes (or
// re-completes) the measurement; explicit null reopens it.
func (h *P3MHandler) UpdateBenefitMeasurement(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		DueDate  string          `json:"due_date"`
		Value    json.RawMessage `json:"value"`
		Evidence *string         `json:"evidence"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	current, err := scanP3MBenefitMeasurement(h.db.QueryRowContext(ctx, `SELECT `+p3mBenefitMeasurementColumns+` FROM p3m_benefit_measurements WHERE id = ?`, id))
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_benefit_measurement")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	dueDate := current.DueDate
	if in.DueDate != "" {
		if _, err := time.Parse("2006-01-02", in.DueDate); err != nil {
			respondValidationError(w, r, "due_date must be a date like 2026-11-30")
			return
		}
		dueDate = in.DueDate
	}

	value := current.Value
	completedAt := current.CompletedAt
	recordedBy := current.RecordedBy
	switch {
	case len(in.Value) == 0: // keep existing completion state
	case string(in.Value) == "null": // explicit null reopens the measurement
		value = nil
		completedAt = nil
		recordedBy = nil
	default:
		var f float64
		if err := json.Unmarshal(in.Value, &f); err != nil {
			respondValidationError(w, r, "value must be a number")
			return
		}
		value = &f
		now := time.Now().UTC()
		completedAt = &now
		recordedBy = &user.ID
	}

	evidence := current.Evidence
	if in.Evidence != nil {
		evidence = in.Evidence
	}

	res, err := h.db.ExecContext(ctx, `UPDATE p3m_benefit_measurements SET due_date = ?, completed_at = ?, value = ?, evidence = ?, recorded_by = ? WHERE id = ?`,
		dueDate, completedAt, value, evidence, recordedBy, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "p3m_benefit_measurement")
		return
	}
	m, err := scanP3MBenefitMeasurement(h.db.QueryRowContext(ctx, `SELECT `+p3mBenefitMeasurementColumns+` FROM p3m_benefit_measurements WHERE id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAudit(h.db, r, user, "p3m_benefit_measurement_updated", "p3m_benefit_measurement", &id, dueDate)
	respondJSONOK(w, m)
}

// GetBenefitsRollup handles GET /api/p3m/benefits/rollup. It aggregates the
// benefit register (BEN-009) for a portfolio, programme or project scope:
// counts by status, the financial target total, overdue measurement points and
// the next upcoming ones.
func (h *P3MHandler) GetBenefitsRollup(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	filters := map[string]string{
		"project_id":   r.URL.Query().Get("project_id"),
		"programme_id": r.URL.Query().Get("programme_id"),
		"portfolio_id": r.URL.Query().Get("portfolio_id"),
	}
	where, args, _ := p3mBenefitScopeClause(filters)

	countQuery := `SELECT COUNT(*), COALESCE(SUM(CASE WHEN benefit_type = 'financial' AND status != 'retired' THEN target_value ELSE 0 END), 0) FROM p3m_benefits b`
	statusQuery := `SELECT status, COUNT(*) FROM p3m_benefits b`
	if where != "" {
		countQuery += " WHERE " + where
		statusQuery += " WHERE " + where
	}

	var total int
	var financialTarget float64
	if err := h.db.QueryRowContext(ctx, countQuery, args...).Scan(&total, &financialTarget); err != nil {
		respondInternalError(w, r, err)
		return
	}

	statusRows, err := h.db.QueryContext(ctx, statusQuery+` GROUP BY status`, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer statusRows.Close()
	byStatus := map[string]int{"planned": 0, "measuring": 0, "realised": 0, "shortfall": 0, "retired": 0}
	for statusRows.Next() {
		var s string
		var n int
		if err := statusRows.Scan(&s, &n); err != nil {
			respondInternalError(w, r, err)
			return
		}
		byStatus[s] = n
	}
	if err := statusRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	today := time.Now().UTC().Format("2006-01-02")
	var overdue int
	err = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM p3m_benefit_measurements m JOIN p3m_benefits b ON b.id = m.benefit_id`+func() string {
		if where != "" {
			return " WHERE " + where
		}
		return ""
	}() + ` AND m.completed_at IS NULL AND m.due_date < ?`, append(append([]any{}, args...), today)...).Scan(&overdue)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	upcomingQuery := `SELECT m.benefit_id, b.title, m.due_date FROM p3m_benefit_measurements m JOIN p3m_benefits b ON b.id = m.benefit_id`
	if where != "" {
		upcomingQuery += " WHERE " + where
	}
	upcomingQuery += ` AND m.completed_at IS NULL AND m.due_date >= ? ORDER BY m.due_date LIMIT 5`
	upRows, err := h.db.QueryContext(ctx, upcomingQuery, append(append([]any{}, args...), today)...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer upRows.Close()
	upcoming := make([]map[string]any, 0, 5)
	for upRows.Next() {
		var benefitID int
		var title, dueDate string
		if err := upRows.Scan(&benefitID, &title, &dueDate); err != nil {
			respondInternalError(w, r, err)
			return
		}
		upcoming = append(upcoming, map[string]any{"benefit_id": benefitID, "title": title, "due_date": dueDate})
	}
	if err := upRows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"total_benefits":         total,
		"by_status":              byStatus,
		"financial_target_total": financialTarget,
		"overdue_measurements":   overdue,
		"upcoming":               upcoming,
	})
}
