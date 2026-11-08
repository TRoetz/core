package handlers

// CSV exports for the P3M registers, mirroring the SMS ExportCSV contract:
// one endpoint, ?scope selects the register, format must be empty or "csv",
// output streams as text/csv with an attachment filename.

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
)

var p3mExportScopeNames = []string{"portfolios", "programmes", "projects", "benefits", "raid", "stakeholders", "resources", "investment_cases"}

// ExportCSV streams a CSV export of one P3M register. Supported scopes:
// portfolios, programmes, projects, benefits, raid, stakeholders, resources,
// investment_cases.
func (h *P3MHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	scope := r.URL.Query().Get("scope")
	format := r.URL.Query().Get("format")
	if format != "" && format != "csv" {
		respondBadRequest(w, r, "unsupported export format")
		return
	}
	validScope := false
	for _, name := range p3mExportScopeNames {
		if scope == name {
			validScope = true
			break
		}
	}
	if !validScope {
		respondBadRequest(w, r, fmt.Sprintf("scope must be one of %v", p3mExportScopeNames))
		return
	}

	ctx := r.Context()
	filename, err := h.p3mExportScope(ctx, w, scope)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
}

// p3mExportScope writes the CSV body for one register and returns the
// attachment filename. Response headers are set by the caller only after a
// successful query, so errors never leak a half-written CSV header.
func (h *P3MHandler) p3mExportScope(ctx context.Context, w http.ResponseWriter, scope string) (string, error) {
	switch scope {
	case "portfolios":
		return h.p3mExportPortfolios(ctx, w)
	case "programmes":
		return h.p3mExportProgrammes(ctx, w)
	case "projects":
		return h.p3mExportProjects(ctx, w)
	case "benefits":
		return h.p3mExportBenefits(ctx, w)
	case "raid":
		return h.p3mExportRaid(ctx, w)
	case "stakeholders":
		return h.p3mExportStakeholders(ctx, w)
	case "resources":
		return h.p3mExportResources(ctx, w)
	case "investment_cases":
		return h.p3mExportInvestmentCases(ctx, w)
	default: // unreachable: ExportCSV validates the scope first
		return "", fmt.Errorf("unknown export scope %q", scope)
	}
}

// p3mCSVHeader writes the header row and returns the writer for rows.
func p3mCSVHeader(w http.ResponseWriter, columns []string) *csv.Writer {
	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return nil
	}
	return cw
}

// p3mFlush terminates a CSV stream; write errors surface to the client as a
// truncated download, which is acceptable for an export endpoint.
func p3mFlush(cw *csv.Writer) {
	cw.Flush()
}

func (h *P3MHandler) p3mExportPortfolios(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT p.id, p.title, p.description, p.strategic_objective, p.budget_allocated, p.rag_status, COALESCE(u.first_name || ' ' || u.last_name, ''), p.created_at, p.updated_at FROM p3m_portfolios p LEFT JOIN users u ON u.id = p.owner_id ORDER BY p.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "title", "description", "strategic_objective", "budget_allocated", "rag_status", "owner_name", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id                   int
			title, desc          string
			objective            string
			budget               float64
			rag                  string
			owner                string
			createdAt, updatedAt sql.NullString
		)
		if err := rows.Scan(&id, &title, &desc, &objective, &budget, &rag, &owner, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), title, desc, objective, strconv.FormatFloat(budget, 'f', -1, 64), rag, owner, createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-portfolios.csv", rows.Err()
}

func (h *P3MHandler) p3mExportProgrammes(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT pr.id, pf.title, pr.title, pr.description, COALESCE(u.first_name || ' ' || u.last_name, ''), pr.budget_allocated, pr.target_start_date, pr.target_end_date, pr.rag_status, pr.created_at, pr.updated_at FROM p3m_programmes pr JOIN p3m_portfolios pf ON pf.id = pr.portfolio_id LEFT JOIN users u ON u.id = pr.manager_id ORDER BY pf.title COLLATE NOCASE, pr.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "portfolio_title", "title", "description", "manager_name", "budget_allocated", "target_start_date", "target_end_date", "rag_status", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id        int
			pfTitle   string
			title     string
			desc      string
			manager   string
			budget    float64
			startDate sql.NullString
			endDate   sql.NullString
			rag       string
			createdAt sql.NullString
			updatedAt sql.NullString
		)
		if err := rows.Scan(&id, &pfTitle, &title, &desc, &manager, &budget, &startDate, &endDate, &rag, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), pfTitle, title, desc, manager, strconv.FormatFloat(budget, 'f', -1, 64), startDate.String, endDate.String, rag, createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-programmes.csv", rows.Err()
}

func (h *P3MHandler) p3mExportProjects(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT px.id, COALESCE(pf.title, pfv.title), COALESCE(prg.title, ''), px.title, px.current_stage, px.budget_approved, px.budget_actual, px.progress_percent, px.start_date, px.target_completion_date, px.rag_status, COALESCE(u.first_name || ' ' || u.last_name, ''), px.created_at, px.updated_at FROM p3m_projects px LEFT JOIN p3m_portfolios pf ON pf.id = px.portfolio_id LEFT JOIN p3m_programmes prg ON prg.id = px.programme_id LEFT JOIN p3m_portfolios pfv ON pfv.id = prg.portfolio_id LEFT JOIN users u ON u.id = px.manager_id ORDER BY COALESCE(pf.title, pfv.title) COLLATE NOCASE, prg.title COLLATE NOCASE, px.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "portfolio_title", "programme_title", "title", "current_stage", "budget_approved", "budget_actual", "progress_percent", "start_date", "target_completion_date", "rag_status", "manager_name", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id             int
			pfTitle        string
			prgTitle       string
			title          string
			stage          string
			budgetApproved float64
			budgetActual   float64
			progress       int
			startDate      sql.NullString
			targetDate     sql.NullString
			rag            string
			manager        string
			createdAt      sql.NullString
			updatedAt      sql.NullString
		)
		if err := rows.Scan(&id, &pfTitle, &prgTitle, &title, &stage, &budgetApproved, &budgetActual, &progress, &startDate, &targetDate, &rag, &manager, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), pfTitle, prgTitle, title, stage, strconv.FormatFloat(budgetApproved, 'f', -1, 64), strconv.FormatFloat(budgetActual, 'f', -1, 64), strconv.Itoa(progress), startDate.String, targetDate.String, rag, manager, createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-projects.csv", rows.Err()
}

func (h *P3MHandler) p3mExportBenefits(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT b.id, b.title, CASE WHEN b.project_id IS NOT NULL THEN 'project' ELSE 'programme' END, COALESCE(px.title, prg.title), b.benefit_type, b.target_value, b.unit_of_measure, b.baseline_value, b.target_realization_date, COALESCE(u.first_name || ' ' || u.last_name, ''), COALESCE(rz.realized, 0), b.created_at FROM p3m_benefits b LEFT JOIN p3m_projects px ON px.id = b.project_id LEFT JOIN p3m_programmes prg ON prg.id = b.programme_id LEFT JOIN users u ON u.id = b.owner_id LEFT JOIN (SELECT benefit_id, SUM(realized_value) AS realized FROM p3m_benefit_realizations GROUP BY benefit_id) rz ON rz.benefit_id = b.id ORDER BY COALESCE(px.title, prg.title) COLLATE NOCASE, b.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "title", "entity_type", "entity_title", "benefit_type", "target_value", "unit_of_measure", "baseline_value", "target_realization_date", "owner_name", "realized_value", "created_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id          int
			title       string
			entityType  string
			entityTitle string
			benefitType string
			targetValue float64
			unit        string
			baseline    float64
			targetDate  sql.NullString
			owner       string
			realized    float64
			createdAt   sql.NullString
		)
		if err := rows.Scan(&id, &title, &entityType, &entityTitle, &benefitType, &targetValue, &unit, &baseline, &targetDate, &owner, &realized, &createdAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), title, entityType, entityTitle, benefitType, strconv.FormatFloat(targetValue, 'f', -1, 64), unit, strconv.FormatFloat(baseline, 'f', -1, 64), targetDate.String, owner, strconv.FormatFloat(realized, 'f', -1, 64), createdAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-benefits.csv", rows.Err()
}

func (h *P3MHandler) p3mExportRaid(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT ri.id, ri.item_type, ri.title, ri.entity_type, COALESCE(epf.title, epr.title, epx.title), ri.impact_score, ri.probability_score, ri.impact_score * ri.probability_score, ri.mitigation_plan, COALESCE(u.first_name || ' ' || u.last_name, ''), ri.status, ri.created_at, ri.updated_at FROM p3m_raid_items ri LEFT JOIN p3m_portfolios epf ON ri.entity_type = 'portfolio' AND epf.id = ri.entity_id LEFT JOIN p3m_programmes epr ON ri.entity_type = 'programme' AND epr.id = ri.entity_id LEFT JOIN p3m_projects epx ON ri.entity_type = 'project' AND epx.id = ri.entity_id LEFT JOIN users u ON u.id = ri.owner_id ORDER BY COALESCE(epf.title, epr.title, epx.title) COLLATE NOCASE, ri.item_type, ri.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "item_type", "title", "entity_type", "entity_title", "impact_score", "probability_score", "score", "mitigation_plan", "owner_name", "status", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id          int
			itemType    string
			title       string
			entityType  string
			entityTitle string
			impact      int
			probability int
			score       int
			mitigation  string
			owner       string
			status      string
			createdAt   sql.NullString
			updatedAt   sql.NullString
		)
		if err := rows.Scan(&id, &itemType, &title, &entityType, &entityTitle, &impact, &probability, &score, &mitigation, &owner, &status, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), itemType, title, entityType, entityTitle, strconv.Itoa(impact), strconv.Itoa(probability), strconv.Itoa(score), mitigation, owner, status, createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-raid.csv", rows.Err()
}

func (h *P3MHandler) p3mExportStakeholders(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT s.id, s.name, s.role_title, s.entity_type, COALESCE(epf.title, epr.title, epx.title), s.power_score, s.interest_score, s.notes, s.created_at, s.updated_at FROM p3m_stakeholders s LEFT JOIN p3m_portfolios epf ON s.entity_type = 'portfolio' AND epf.id = s.entity_id LEFT JOIN p3m_programmes epr ON s.entity_type = 'programme' AND epr.id = s.entity_id LEFT JOIN p3m_projects epx ON s.entity_type = 'project' AND epx.id = s.entity_id ORDER BY COALESCE(epf.title, epr.title, epx.title) COLLATE NOCASE, s.name COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "name", "role_title", "entity_type", "entity_title", "power_score", "interest_score", "notes", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id          int
			name        string
			roleTitle   string
			entityType  string
			entityTitle string
			power       int
			interest    int
			notes       string
			createdAt   sql.NullString
			updatedAt   sql.NullString
		)
		if err := rows.Scan(&id, &name, &roleTitle, &entityType, &entityTitle, &power, &interest, &notes, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), name, roleTitle, entityType, entityTitle, strconv.Itoa(power), strconv.Itoa(interest), notes, createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-stakeholders.csv", rows.Err()
}

func (h *P3MHandler) p3mExportResources(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT r.id, r.name, COALESCE(u.username, ''), r.skills, r.cost_rate, r.capacity_hours_per_week, COALESCE(al.allocated, 0), r.created_at, r.updated_at FROM p3m_resources r LEFT JOIN users u ON u.id = r.user_id LEFT JOIN (SELECT resource_id, SUM(hours_per_week) AS allocated FROM p3m_resource_allocations GROUP BY resource_id) al ON al.resource_id = r.id ORDER BY r.name COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"id", "name", "linked_user", "skills", "cost_rate", "capacity_hours_per_week", "allocated_hours_per_week", "created_at", "updated_at"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			id         int
			name       string
			linkedUser string
			skills     string
			costRate   float64
			capacity   int
			allocated  int
			createdAt  sql.NullString
			updatedAt  sql.NullString
		)
		if err := rows.Scan(&id, &name, &linkedUser, &skills, &costRate, &capacity, &allocated, &createdAt, &updatedAt); err != nil {
			return "", err
		}
		if err := cw.Write([]string{strconv.Itoa(id), name, linkedUser, skills, strconv.FormatFloat(costRate, 'f', -1, 64), strconv.Itoa(capacity), strconv.Itoa(allocated), createdAt.String, updatedAt.String}); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-resources.csv", rows.Err()
}

func (h *P3MHandler) p3mExportInvestmentCases(ctx context.Context, w http.ResponseWriter) (string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT p.title, COALESCE(pf.title, ''), ic.status, ic.benefit_score, ic.risk_score, ic.roi_percent, ic.payback_months, ic.business_case, COALESCE(u.first_name || ' ' || u.last_name, ''), ic.decision_date
		FROM p3m_investment_cases ic
		JOIN p3m_projects p ON p.id = ic.project_id
		LEFT JOIN p3m_portfolios pf ON pf.id = COALESCE(p.portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = p.programme_id))
		LEFT JOIN users u ON u.id = ic.decided_by
		ORDER BY pf.title COLLATE NOCASE, p.title COLLATE NOCASE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cw := p3mCSVHeader(w, []string{"project_title", "portfolio_title", "status", "benefit_score", "risk_score", "roi_percent", "payback_months", "business_case", "decided_by", "decision_date"})
	if cw == nil {
		return "", fmt.Errorf("csv header write failed")
	}
	for rows.Next() {
		var (
			projectTitle, portfolioTitle, status, businessCase, decidedBy string
			benefitScore, riskScore                                       int
			roiPercent                                                    sql.NullFloat64
			paybackMonths                                                 sql.NullInt64
			decisionDate                                                  sql.NullString
		)
		if err := rows.Scan(&projectTitle, &portfolioTitle, &status, &benefitScore, &riskScore, &roiPercent, &paybackMonths, &businessCase, &decidedBy, &decisionDate); err != nil {
			return "", err
		}
		row := []string{projectTitle, portfolioTitle, status, strconv.Itoa(benefitScore), strconv.Itoa(riskScore)}
		if roiPercent.Valid {
			row = append(row, strconv.FormatFloat(roiPercent.Float64, 'f', -1, 64))
		} else {
			row = append(row, "")
		}
		if paybackMonths.Valid {
			row = append(row, strconv.Itoa(int(paybackMonths.Int64)))
		} else {
			row = append(row, "")
		}
		row = append(row, businessCase, decidedBy, decisionDate.String)
		if err := cw.Write(row); err != nil {
			return "", err
		}
	}
	p3mFlush(cw)
	return "p3m-investment-cases.csv", rows.Err()
}
