// Package handlers: P3M (Portfolio, Programme, Project Management) module.
//
// The module is gated by the global permission models.PermissionP3MView at the
// route level; every handler here additionally requires an authenticated user
// for attribution fields. All three tiers share one rollup convention: a
// programme or portfolio reports the worst RAG of its descendant projects when
// it has any, otherwise its own stored rag_status (default green). Project RAG
// is always recomputed server-side from earned-value metrics and can never be
// set manually.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/services"
)

// P3MHandler serves the /api/p3m/* endpoints for the portfolio, programme and
// project management module.
type P3MHandler struct {
	db    database.Database
	perms *services.PermissionService
}

func NewP3MHandler(db database.Database, permissionService *services.PermissionService) *P3MHandler {
	return &P3MHandler{db: db, perms: permissionService}
}

// ---------------------------------------------------------------------------
// Shared RAG / earned-value helpers (used by the delegated P3M handler files).
// ---------------------------------------------------------------------------

func p3mRagRank(status string) int {
	switch status {
	case models.P3MRagRed:
		return 0
	case models.P3MRagAmber:
		return 1
	default:
		return 2
	}
}

func p3mRankToRag(rank int) string {
	switch rank {
	case 0:
		return models.P3MRagRed
	case 1:
		return models.P3MRagAmber
	default:
		return models.P3MRagGreen
	}
}

// p3mWorstRag returns the most severe status among its arguments.
func p3mWorstRag(rags ...string) string {
	worst := 2
	for _, r := range rags {
		if rank := p3mRagRank(r); rank < worst {
			worst = rank
		}
	}
	return p3mRankToRag(worst)
}

// p3mComputeEVM derives earned-value metrics for a project. PV is time-based:
// budget_approved spread linearly over the planned window. When dates are
// missing, PV falls back to EV so the schedule metrics read as on-plan rather
// than undefined (SPI = 1).
func p3mComputeEVM(p *models.P3MProject, now time.Time) *models.P3MEVM {
	ev := p.BudgetApproved * float64(p.ProgressPercent) / 100.0
	pv := ev
	if p.StartDate != nil && p.TargetCompletionDate != nil && p.TargetCompletionDate.After(*p.StartDate) {
		total := p.TargetCompletionDate.Sub(*p.StartDate).Seconds()
		elapsed := now.Sub(*p.StartDate).Seconds()
		frac := 0.0
		switch {
		case elapsed <= 0:
			frac = 0
		case elapsed >= total:
			frac = 1
		default:
			frac = elapsed / total
		}
		pv = p.BudgetApproved * frac
	}
	evm := &models.P3MEVM{PlannedValue: pv, EarnedValue: ev, ActualCost: p.BudgetActual}
	if pv > 0 {
		evm.SPI = ev / pv
	}
	if evm.ActualCost > 0 {
		evm.CPI = ev / evm.ActualCost
	}
	return evm
}

// p3mComputeProjectRAG applies the automatic RAG rules: red on a cost overrun
// above 25% or SPI below 0.7, amber on any overrun or SPI below 0.85.
func p3mComputeProjectRAG(p *models.P3MProject, evm *models.P3MEVM) string {
	if p.BudgetApproved > 0 && p.BudgetActual > p.BudgetApproved*1.25 {
		return models.P3MRagRed
	}
	if evm.SPI > 0 && evm.SPI < 0.7 {
		return models.P3MRagRed
	}
	if p.BudgetApproved > 0 && p.BudgetActual > p.BudgetApproved {
		return models.P3MRagAmber
	}
	if evm.SPI > 0 && evm.SPI < 0.85 {
		return models.P3MRagAmber
	}
	return models.P3MRagGreen
}

// p3mApplyComputed fills the in-memory EVM and rag fields of a project from its
// persisted numbers without touching the database.
func p3mApplyComputed(p *models.P3MProject) {
	evm := p3mComputeEVM(p, time.Now().UTC())
	p.EVM = evm
	p.RagStatus = p3mComputeProjectRAG(p, evm)
}

// ---------------------------------------------------------------------------
// Portfolios
// ---------------------------------------------------------------------------

const p3mPortfolioColumns = `id, title, description, strategic_objective, budget_allocated, rag_status, owner_id, created_at, updated_at`

func scanP3MPortfolio(row interface{ Scan(...any) error }) (models.P3MPortfolio, error) {
	var pf models.P3MPortfolio
	err := row.Scan(&pf.ID, &pf.Title, &pf.Description, &pf.StrategicObjective,
		&pf.BudgetAllocated, &pf.RagStatus, &pf.OwnerID, &pf.CreatedAt, &pf.UpdatedAt)
	return pf, err
}

// ListPortfolios handles GET /api/p3m/portfolios.
func (h *P3MHandler) ListPortfolios(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+p3mPortfolioColumns+` FROM p3m_portfolios ORDER BY title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	portfolios := make([]models.P3MPortfolio, 0)
	for rows.Next() {
		pf, err := scanP3MPortfolio(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		portfolios = append(portfolios, pf)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	rollups, err := h.portfolioRollups(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range portfolios {
		portfolios[i].Rollup = rollups[portfolios[i].ID]
	}
	respondJSONOK(w, portfolios)
}

// GetPortfolio handles GET /api/p3m/portfolios/{id}.
func (h *P3MHandler) GetPortfolio(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	pf, err := h.getPortfolioRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_portfolio")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	rollups, _ := h.portfolioRollups(r.Context())
	pf.Rollup = rollups[pf.ID]
	respondJSONOK(w, pf)
}

// CreatePortfolio handles POST /api/p3m/portfolios.
func (h *P3MHandler) CreatePortfolio(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		Title              string  `json:"title"`
		Description        string  `json:"description"`
		StrategicObjective string  `json:"strategic_objective"`
		BudgetAllocated    float64 `json:"budget_allocated"`
		RagStatus          string  `json:"rag_status"`
		OwnerID            *int    `json:"owner_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if in.RagStatus == "" {
		in.RagStatus = models.P3MRagGreen
	}
	if !p3mValidRag(in.RagStatus) {
		respondValidationError(w, r, "rag_status must be green, amber or red")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_portfolios (title, description, strategic_objective, budget_allocated, rag_status, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Title, in.Description, in.StrategicObjective, in.BudgetAllocated, in.RagStatus, nullInt(in.OwnerID), now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	pf, err := h.getPortfolioRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, pf)
}

// UpdatePortfolio handles PUT /api/p3m/portfolios/{id}. The body carries the
// full mutable state of the portfolio.
func (h *P3MHandler) UpdatePortfolio(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title              string  `json:"title"`
		Description        string  `json:"description"`
		StrategicObjective string  `json:"strategic_objective"`
		BudgetAllocated    float64 `json:"budget_allocated"`
		RagStatus          string  `json:"rag_status"`
		OwnerID            *int    `json:"owner_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !p3mValidRag(in.RagStatus) {
		respondValidationError(w, r, "rag_status must be green, amber or red")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_portfolios SET title = ?, description = ?, strategic_objective = ?, budget_allocated = ?, rag_status = ?, owner_id = ?, updated_at = ? WHERE id = ?`,
		in.Title, in.Description, in.StrategicObjective, in.BudgetAllocated, in.RagStatus, nullInt(in.OwnerID), now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_portfolio")
		return
	}
	pf, err := h.getPortfolioRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rollups, _ := h.portfolioRollups(r.Context())
	pf.Rollup = rollups[pf.ID]
	respondJSONOK(w, pf)
}

// DeletePortfolio handles DELETE /api/p3m/portfolios/{id}. Programmes and
// projects inside the portfolio cascade.
func (h *P3MHandler) DeletePortfolio(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_portfolios WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_portfolio")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *P3MHandler) getPortfolioRow(ctx context.Context, id int) (models.P3MPortfolio, error) {
	return scanP3MPortfolio(h.db.QueryRowContext(ctx, `SELECT `+p3mPortfolioColumns+` FROM p3m_portfolios WHERE id = ?`, id))
}

// portfolioRollups computes the aggregate rollup of every portfolio in one
// pass: contained programme/project counts, budget sums and the effective RAG
// (worst descendant project status, or the stored value when no projects).
func (h *P3MHandler) portfolioRollups(ctx context.Context) (map[int]*models.P3MPortfolioRollup, error) {
	out := make(map[int]*models.P3MPortfolioRollup)

	rows, err := h.db.QueryContext(ctx, `SELECT id, rag_status FROM p3m_portfolios`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		var rag string
		if err := rows.Scan(&id, &rag); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = &models.P3MPortfolioRollup{RagStatus: rag}
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `SELECT portfolio_id, COUNT(*) FROM p3m_programmes GROUP BY portfolio_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var fid int
		var count int
		if err := rows.Scan(&fid, &count); err != nil {
			rows.Close()
			return nil, err
		}
		r := out[fid]
		if r == nil {
			r = &models.P3MPortfolioRollup{}
			out[fid] = r
		}
		r.ProgrammeCount += count
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `
		SELECT COALESCE(p.portfolio_id, pg.portfolio_id) AS fid, COUNT(*), SUM(budget_approved), SUM(budget_actual),
			MIN(CASE p.rag_status WHEN 'red' THEN 0 WHEN 'amber' THEN 1 ELSE 2 END)
		FROM p3m_projects p LEFT JOIN p3m_programmes pg ON pg.id = p.programme_id
		WHERE COALESCE(p.portfolio_id, pg.portfolio_id) IS NOT NULL
		GROUP BY fid`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			fid       int
			count     int
			bAppr     float64
			bAct      float64
			worstRank int
		)
		if err := rows.Scan(&fid, &count, &bAppr, &bAct, &worstRank); err != nil {
			rows.Close()
			return nil, err
		}
		r := out[fid]
		if r == nil {
			r = &models.P3MPortfolioRollup{}
			out[fid] = r
		}
		r.ProjectCount += count
		r.BudgetApproved += bAppr
		r.BudgetActual += bAct
		r.RagStatus = p3mRankToRag(worstRank)
	}
	rows.Close()

	return out, nil
}

// ---------------------------------------------------------------------------
// Programmes
// ---------------------------------------------------------------------------

const p3mProgrammeColumns = `id, portfolio_id, title, description, manager_id, budget_allocated, target_start_date, target_end_date, rag_status, created_at, updated_at`

func scanP3MProgramme(row interface{ Scan(...any) error }) (models.P3MProgramme, error) {
	var pg models.P3MProgramme
	err := row.Scan(&pg.ID, &pg.PortfolioID, &pg.Title, &pg.Description, &pg.ManagerID,
		&pg.BudgetAllocated, &pg.TargetStartDate, &pg.TargetEndDate, &pg.RagStatus, &pg.CreatedAt, &pg.UpdatedAt)
	return pg, err
}

// ListProgrammes handles GET /api/p3m/programmes?portfolio_id=.
func (h *P3MHandler) ListProgrammes(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mProgrammeColumns + ` FROM p3m_programmes`
	var args []any
	if fid := r.URL.Query().Get("portfolio_id"); fid != "" {
		id, ok := parseQueryInt(fid)
		if !ok {
			respondBadRequest(w, r, "invalid portfolio_id")
			return
		}
		query += ` WHERE portfolio_id = ?`
		args = append(args, id)
	}
	query += ` ORDER BY title`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	programmes := make([]models.P3MProgramme, 0)
	for rows.Next() {
		pg, err := scanP3MProgramme(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		programmes = append(programmes, pg)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	rollups, err := h.programmeRollups(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range programmes {
		programmes[i].Rollup = rollups[programmes[i].ID]
	}
	respondJSONOK(w, programmes)
}

// GetProgramme handles GET /api/p3m/programmes/{id}.
func (h *P3MHandler) GetProgramme(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	pg, err := h.getProgrammeRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_programme")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	rollups, _ := h.programmeRollups(r.Context())
	pg.Rollup = rollups[pg.ID]
	respondJSONOK(w, pg)
}

// CreateProgramme handles POST /api/p3m/programmes. portfolio_id is required;
// the referenced portfolio must exist.
func (h *P3MHandler) CreateProgramme(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		PortfolioID     int       `json:"portfolio_id"`
		Title           string    `json:"title"`
		Description     string    `json:"description"`
		ManagerID       *int      `json:"manager_id,omitempty"`
		BudgetAllocated float64   `json:"budget_allocated"`
		TargetStartDate time.Time `json:"target_start_date,omitempty"`
		TargetEndDate   time.Time `json:"target_end_date,omitempty"`
		RagStatus       string    `json:"rag_status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.portfolioExists(r.Context(), in.PortfolioID) {
		respondNotFound(w, r, "p3m_portfolio")
		return
	}
	if in.RagStatus == "" {
		in.RagStatus = models.P3MRagGreen
	}
	if !p3mValidRag(in.RagStatus) {
		respondValidationError(w, r, "rag_status must be green, amber or red")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_programmes (portfolio_id, title, description, manager_id, budget_allocated, target_start_date, target_end_date, rag_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.PortfolioID, in.Title, in.Description, nullInt(in.ManagerID), in.BudgetAllocated,
		nullTime(in.TargetStartDate), nullTime(in.TargetEndDate), in.RagStatus, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	pg, err := h.getProgrammeRow(r.Context(), int(id64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, pg)
}

// UpdateProgramme handles PUT /api/p3m/programmes/{id}.
func (h *P3MHandler) UpdateProgramme(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		PortfolioID     int       `json:"portfolio_id"`
		Title           string    `json:"title"`
		Description     string    `json:"description"`
		ManagerID       *int      `json:"manager_id,omitempty"`
		BudgetAllocated float64   `json:"budget_allocated"`
		TargetStartDate time.Time `json:"target_start_date,omitempty"`
		TargetEndDate   time.Time `json:"target_end_date,omitempty"`
		RagStatus       string    `json:"rag_status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if !h.portfolioExists(r.Context(), in.PortfolioID) {
		respondNotFound(w, r, "p3m_portfolio")
		return
	}
	if !p3mValidRag(in.RagStatus) {
		respondValidationError(w, r, "rag_status must be green, amber or red")
		return
	}
	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE p3m_programmes SET portfolio_id = ?, title = ?, description = ?, manager_id = ?, budget_allocated = ?, target_start_date = ?, target_end_date = ?, rag_status = ?, updated_at = ? WHERE id = ?`,
		in.PortfolioID, in.Title, in.Description, nullInt(in.ManagerID), in.BudgetAllocated,
		nullTime(in.TargetStartDate), nullTime(in.TargetEndDate), in.RagStatus, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_programme")
		return
	}
	pg, err := h.getProgrammeRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	rollups, _ := h.programmeRollups(r.Context())
	pg.Rollup = rollups[pg.ID]
	respondJSONOK(w, pg)
}

// DeleteProgramme handles DELETE /api/p3m/programmes/{id}. Projects inside the
// programme cascade.
func (h *P3MHandler) DeleteProgramme(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_programmes WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_programme")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *P3MHandler) getProgrammeRow(ctx context.Context, id int) (models.P3MProgramme, error) {
	return scanP3MProgramme(h.db.QueryRowContext(ctx, `SELECT `+p3mProgrammeColumns+` FROM p3m_programmes WHERE id = ?`, id))
}

// programmeRollups computes per-programme rollups: project count, budget sums
// and effective RAG (worst child status, or the stored value when empty).
func (h *P3MHandler) programmeRollups(ctx context.Context) (map[int]*models.P3MProgrammeRollup, error) {
	out := make(map[int]*models.P3MProgrammeRollup)

	rows, err := h.db.QueryContext(ctx, `SELECT id, rag_status FROM p3m_programmes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		var rag string
		if err := rows.Scan(&id, &rag); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = &models.P3MProgrammeRollup{RagStatus: rag}
	}
	rows.Close()

	rows, err = h.db.QueryContext(ctx, `
		SELECT programme_id, COUNT(*), SUM(budget_approved), SUM(budget_actual),
			MIN(CASE rag_status WHEN 'red' THEN 0 WHEN 'amber' THEN 1 ELSE 2 END)
		FROM p3m_projects WHERE programme_id IS NOT NULL GROUP BY programme_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			pid       int
			count     int
			bAppr     float64
			bAct      float64
			worstRank int
		)
		if err := rows.Scan(&pid, &count, &bAppr, &bAct, &worstRank); err != nil {
			rows.Close()
			return nil, err
		}
		r := out[pid]
		if r == nil {
			r = &models.P3MProgrammeRollup{}
			out[pid] = r
		}
		r.ProjectCount += count
		r.BudgetApproved += bAppr
		r.BudgetActual += bAct
		r.RagStatus = p3mRankToRag(worstRank)
	}
	rows.Close()

	return out, nil
}

// ---------------------------------------------------------------------------
// Projects (leaf tier; EVM + automatic RAG live here)
// ---------------------------------------------------------------------------

const p3mProjectColumns = `id, portfolio_id, programme_id, title, description, manager_id, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date, rag_status, created_at, updated_at`

func scanP3MProject(row interface{ Scan(...any) error }) (models.P3MProject, error) {
	var p models.P3MProject
	err := row.Scan(&p.ID, &p.PortfolioID, &p.ProgrammeID, &p.Title, &p.Description, &p.ManagerID,
		&p.CurrentStage, &p.BudgetApproved, &p.BudgetActual, &p.ProgressPercent,
		&p.StartDate, &p.TargetCompletionDate, &p.RagStatus, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// ListProjects handles GET /api/p3m/projects?portfolio_id=&programme_id=&stage=.
func (h *P3MHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	query := `SELECT ` + p3mProjectColumns + ` FROM p3m_projects`
	var conds []string
	var args []any
	q := r.URL.Query()
	if v := q.Get("portfolio_id"); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondBadRequest(w, r, "invalid portfolio_id")
			return
		}
		conds = append(conds, `COALESCE(portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = programme_id)) = ?`)
		args = append(args, id)
	}
	if v := q.Get("programme_id"); v != "" {
		id, ok := parseQueryInt(v)
		if !ok {
			respondBadRequest(w, r, "invalid programme_id")
			return
		}
		conds = append(conds, `programme_id = ?`)
		args = append(args, id)
	}
	if v := q.Get("stage"); v != "" {
		conds = append(conds, `current_stage = ?`)
		args = append(args, v)
	}
	if len(conds) > 0 {
		query += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			query += " AND " + c
		}
	}
	query += ` ORDER BY title`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	projects := make([]models.P3MProject, 0)
	for rows.Next() {
		p, err := scanP3MProject(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		p3mApplyComputed(&p)
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, projects)
}

// GetProject handles GET /api/p3m/projects/{id}. The response carries the
// computed EVM block and the current automatic RAG.
func (h *P3MHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := h.getProjectRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}
	p3mApplyComputed(&p)
	respondJSONOK(w, p)
}

// CreateProject handles POST /api/p3m/projects. A project belongs to a
// portfolio directly or through a programme; when both are given they must
// agree. The RAG is computed server-side and stored with the row.
func (h *P3MHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		PortfolioID          *int      `json:"portfolio_id,omitempty"`
		ProgrammeID          *int      `json:"programme_id,omitempty"`
		Title                string    `json:"title"`
		Description          string    `json:"description"`
		ManagerID            *int      `json:"manager_id,omitempty"`
		CurrentStage         string    `json:"current_stage"`
		BudgetApproved       float64   `json:"budget_approved"`
		BudgetActual         float64   `json:"budget_actual"`
		ProgressPercent      int       `json:"progress_percent"`
		StartDate            time.Time `json:"start_date,omitempty"`
		TargetCompletionDate time.Time `json:"target_completion_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	fid, pid, ok := h.resolveProjectParents(r.Context(), w, r, in.PortfolioID, in.ProgrammeID)
	if !ok {
		return
	}
	if in.CurrentStage == "" {
		in.CurrentStage = models.P3MStageInitiation
	}
	if !p3mValidStage(in.CurrentStage) {
		respondValidationError(w, r, "current_stage must be one of initiation, planning, execution, closure")
		return
	}

	p := models.P3MProject{
		PortfolioID: fid, ProgrammeID: pid, Title: in.Title, Description: in.Description, ManagerID: in.ManagerID,
		CurrentStage: in.CurrentStage, BudgetApproved: in.BudgetApproved, BudgetActual: in.BudgetActual,
		ProgressPercent: in.ProgressPercent, StartDate: nullTime(in.StartDate), TargetCompletionDate: nullTime(in.TargetCompletionDate),
	}
	p3mApplyComputed(&p)

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_projects (portfolio_id, programme_id, title, description, manager_id, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date, rag_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullInt(p.PortfolioID), nullInt(p.ProgrammeID), p.Title, p.Description, nullInt(p.ManagerID),
		p.CurrentStage, p.BudgetApproved, p.BudgetActual, p.ProgressPercent,
		nullTimePtr(p.StartDate), nullTimePtr(p.TargetCompletionDate), p.RagStatus, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	p.ID = int(id64)
	p.CreatedAt, p.UpdatedAt = now, now
	respondJSONCreated(w, p)
}

// UpdateProject handles PUT /api/p3m/projects/{id}. Budget increases are
// rejected here: they must go through the authorization workflow at
// POST /p3m/budget-approvals so threshold routing applies. Decreases and all
// other field changes apply directly; RAG is recomputed on every write.
func (h *P3MHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	current, err := h.getProjectRow(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		PortfolioID          *int      `json:"portfolio_id,omitempty"`
		ProgrammeID          *int      `json:"programme_id,omitempty"`
		Title                string    `json:"title"`
		Description          string    `json:"description"`
		ManagerID            *int      `json:"manager_id,omitempty"`
		CurrentStage         string    `json:"current_stage"`
		BudgetApproved       float64   `json:"budget_approved"`
		BudgetActual         float64   `json:"budget_actual"`
		ProgressPercent      int       `json:"progress_percent"`
		StartDate            time.Time `json:"start_date,omitempty"`
		TargetCompletionDate time.Time `json:"target_completion_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Title == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	fid, pid, ok := h.resolveProjectParents(r.Context(), w, r, in.PortfolioID, in.ProgrammeID)
	if !ok {
		return
	}
	if in.CurrentStage == "" {
		in.CurrentStage = current.CurrentStage
	}
	if !p3mValidStage(in.CurrentStage) {
		respondValidationError(w, r, "current_stage must be one of initiation, planning, execution, closure")
		return
	}
	if in.BudgetApproved > current.BudgetApproved+1e-9 {
		respondConflict(w, r, "budget increases require POST /p3m/budget-approvals so authorization thresholds apply")
		return
	}

	p := models.P3MProject{
		ID: id, PortfolioID: fid, ProgrammeID: pid, Title: in.Title, Description: in.Description, ManagerID: in.ManagerID,
		CurrentStage: in.CurrentStage, BudgetApproved: in.BudgetApproved, BudgetActual: in.BudgetActual,
		ProgressPercent: in.ProgressPercent, StartDate: nullTime(in.StartDate), TargetCompletionDate: nullTime(in.TargetCompletionDate),
	}
	p3mApplyComputed(&p)

	now := time.Now().UTC()
	_, err = h.db.ExecContext(r.Context(), `
		UPDATE p3m_projects SET portfolio_id = ?, programme_id = ?, title = ?, description = ?, manager_id = ?, current_stage = ?, budget_approved = ?, budget_actual = ?, progress_percent = ?, start_date = ?, target_completion_date = ?, rag_status = ?, updated_at = ? WHERE id = ?`,
		nullInt(p.PortfolioID), nullInt(p.ProgrammeID), p.Title, p.Description, nullInt(p.ManagerID),
		p.CurrentStage, p.BudgetApproved, p.BudgetActual, p.ProgressPercent,
		nullTimePtr(p.StartDate), nullTimePtr(p.TargetCompletionDate), p.RagStatus, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	p.CreatedAt, p.UpdatedAt = current.CreatedAt, now
	respondJSONOK(w, p)
}

// DeleteProject handles DELETE /api/p3m/projects/{id}. Child rows (benefits,
// RAID items, gates, baselines, ...) cascade.
func (h *P3MHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_projects WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *P3MHandler) getProjectRow(ctx context.Context, id int) (models.P3MProject, error) {
	return scanP3MProject(h.db.QueryRowContext(ctx, `SELECT `+p3mProjectColumns+` FROM p3m_projects WHERE id = ?`, id))
}

// resolveProjectParents validates the portfolio/programme pair of a project
// write and returns the normalized (portfolio_id, programme_id) pointers. When
// only a programme is given its portfolio is implied; when both are given they
// must agree. It writes the error response itself on failure.
func (h *P3MHandler) resolveProjectParents(ctx context.Context, w http.ResponseWriter, r *http.Request, fid, pid *int) (*int, *int, bool) {
	if pid != nil {
		var pfID int
		err := h.db.QueryRowContext(ctx, `SELECT portfolio_id FROM p3m_programmes WHERE id = ?`, *pid).Scan(&pfID)
		if err != nil {
			if isNotFound(err) {
				respondNotFound(w, r, "p3m_programme")
			} else {
				respondInternalError(w, r, err)
			}
			return nil, nil, false
		}
		if fid != nil && *fid != pfID {
			respondValidationError(w, r, "portfolio_id does not match the programme's portfolio")
			return nil, nil, false
		}
		return &pfID, pid, true
	}
	if fid == nil {
		respondValidationError(w, r, "either portfolio_id or programme_id is required")
		return nil, nil, false
	}
	if !h.portfolioExists(ctx, *fid) {
		respondNotFound(w, r, "p3m_portfolio")
		return nil, nil, false
	}
	return fid, nil, true
}

func (h *P3MHandler) portfolioExists(ctx context.Context, id int) bool {
	var exists bool
	err := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM p3m_portfolios WHERE id = ?)`, id).Scan(&exists)
	return err == nil && exists
}

// ---------------------------------------------------------------------------
// Hierarchy + executive summary (the P3M view's aggregate endpoints)
// ---------------------------------------------------------------------------

// GetHierarchy handles GET /api/p3m/hierarchy: the full portfolio → programme
// → project tree in one call, with rollups and computed EVM on every node.
func (h *P3MHandler) GetHierarchy(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	pfRows, err := h.db.QueryContext(ctx, `SELECT `+p3mPortfolioColumns+` FROM p3m_portfolios ORDER BY title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	portfolios := make([]models.P3MPortfolio, 0)
	for pfRows.Next() {
		pf, err := scanP3MPortfolio(pfRows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		portfolios = append(portfolios, pf)
	}
	pfRows.Close()

	pgRows, err := h.db.QueryContext(ctx, `SELECT `+p3mProgrammeColumns+` FROM p3m_programmes ORDER BY title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	programmesByPortfolio := make(map[int][]models.P3MProgramme)
	for pgRows.Next() {
		pg, err := scanP3MProgramme(pgRows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		programmesByPortfolio[pg.PortfolioID] = append(programmesByPortfolio[pg.PortfolioID], pg)
	}
	pgRows.Close()

	prRows, err := h.db.QueryContext(ctx, `SELECT `+p3mProjectColumns+` FROM p3m_projects ORDER BY title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	projectsByProgramme := make(map[int][]models.P3MProject)
	for prRows.Next() {
		p, err := scanP3MProject(prRows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		p3mApplyComputed(&p)
		switch {
		case p.ProgrammeID != nil:
			projectsByProgramme[*p.ProgrammeID] = append(projectsByProgramme[*p.ProgrammeID], p)
		case p.PortfolioID != nil:
			for i := range portfolios {
				if portfolios[i].ID == *p.PortfolioID {
					portfolios[i].StandaloneProjects = append(portfolios[i].StandaloneProjects, p)
				}
			}
		}
	}
	prRows.Close()

	pfRollups, err := h.portfolioRollups(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	pgRollups, err := h.programmeRollups(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	for i := range portfolios {
		portfolios[i].Rollup = pfRollups[portfolios[i].ID]
		pgs := programmesByPortfolio[portfolios[i].ID]
		for j := range pgs {
			pgs[j].Rollup = pgRollups[pgs[j].ID]
			pgs[j].Projects = projectsByProgramme[pgs[j].ID]
		}
		portfolios[i].Programmes = pgs
	}

	respondJSONOK(w, map[string]any{"portfolios": portfolios})
}

// GetExecutiveSummary handles GET /api/p3m/executive-summary: per-portfolio
// budget health, projects awaiting their next stage gate and the top open
// risks across each portfolio tree.
func (h *P3MHandler) GetExecutiveSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	pfRows, err := h.db.QueryContext(ctx, `SELECT id, title FROM p3m_portfolios ORDER BY title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	type pfRef struct{ ID int; Title string }
	refs := make([]pfRef, 0)
	for pfRows.Next() {
		var ref pfRef
		if err := pfRows.Scan(&ref.ID, &ref.Title); err != nil {
			respondInternalError(w, r, err)
			return
		}
		refs = append(refs, ref)
	}
	pfRows.Close()

	rollups, err := h.portfolioRollups(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Collect the project ids of each portfolio tree for gate/risk lookups.
	treeProjects := make(map[int][]int) // fid -> project ids
	prRows, err := h.db.QueryContext(ctx, `SELECT COALESCE(portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = programme_id)), id FROM p3m_projects`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for prRows.Next() {
		var fid sql.NullInt64
		var pid int
		if err := prRows.Scan(&fid, &pid); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if !fid.Valid {
			continue
		}
		fidInt := int(fid.Int64)
		treeProjects[fidInt] = append(treeProjects[fidInt], pid)
	}
	prRows.Close()

	summary := models.P3MExecutiveSummary{GeneratedAt: time.Now().UTC()}
	for _, ref := range refs {
		ps := models.P3MPortfolioSummary{PortfolioID: ref.ID, Title: ref.Title, RagStatus: models.P3MRagGreen}
		if ru := rollups[ref.ID]; ru != nil {
			ps.RagStatus = ru.RagStatus
			ps.BudgetApproved = ru.BudgetApproved
			ps.BudgetActual = ru.BudgetActual
		}

		err := h.db.QueryRowContext(ctx, `SELECT budget_allocated FROM p3m_portfolios WHERE id = ?`, ref.ID).Scan(&ps.BudgetAllocated)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}

		// Upcoming gates: every project in the tree that has not reached closure.
		for _, pid := range treeProjects[ref.ID] {
			var title, stage string
			err := h.db.QueryRowContext(ctx, `SELECT title, current_stage FROM p3m_projects WHERE id = ?`, pid).Scan(&title, &stage)
			if err != nil {
				respondInternalError(w, r, err)
				return
			}
			if stage != models.P3MStageClosure {
				ps.UpcomingGates = append(ps.UpcomingGates, models.P3MUpcomingGate{ProjectID: pid, Title: title, Stage: stage})
			}
		}

		// Top risks across the tree.
		if ids := treeProjects[ref.ID]; len(ids) > 0 {
			ps.TopRisks = h.topTreeRisks(ctx, w, r, ref.ID, ids)
			if ps.TopRisks == nil {
				return // error already written
			}
		}

		summary.Portfolios = append(summary.Portfolios, ps)
	}
	respondJSONOK(w, summary)
}

// topTreeRisks returns the five highest-scored open RAID items attached to a
// portfolio tree (portfolio/programme/project level). It returns nil after
// writing an error response.
func (h *P3MHandler) topTreeRisks(ctx context.Context, w http.ResponseWriter, r *http.Request, fid int, projectIDs []int) []models.P3MRaidItem {
	placeholders := make([]string, len(projectIDs))
	args := make([]any, 0, len(projectIDs)+2)
	for i, id := range projectIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `
		SELECT entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, created_at, updated_at
		FROM p3m_raid_items
		WHERE ((entity_type = 'portfolio' AND entity_id = ?)
			OR (entity_type = 'project' AND entity_id IN (`+joinPlaceholders(placeholders)+`))
			OR (entity_type = 'programme' AND entity_id IN (SELECT id FROM p3m_programmes WHERE portfolio_id = ?)))
		AND status IN ('open', 'mitigating', 'escalated')
		ORDER BY impact_score * probability_score DESC, id LIMIT 5`
	args = append([]any{fid}, args...)
	args = append(args, fid)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return nil
	}
	defer rows.Close()

	out := make([]models.P3MRaidItem, 0, 5)
	for rows.Next() {
		var s models.P3MRaidItem
		if err := rows.Scan(&s.EntityType, &s.EntityID, &s.ItemType, &s.Title, &s.Description,
			&s.ImpactScore, &s.ProbabilityScore, &s.MitigationPlan, &s.OwnerID, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			respondInternalError(w, r, err)
			return nil
		}
		p3mApplyComputedRaid(&s)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return nil
	}
	return out
}
