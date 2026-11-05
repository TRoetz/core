package aitools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/database"
	"windshift/internal/models"
)

// This file adds MCP/AI tools for the P3M (Portfolio/Programme/Project
// Management) governance module: the portfolio→programme→project hierarchy,
// RAID register, budget approvals, benefits, and stage-gate reviews. Access
// mirrors the HTTP surface: the global "p3m.view" permission gates every tool.

const (
	p3mPortfolioColumns = `id, title, description, strategic_objective, budget_allocated, rag_status, owner_id, created_at, updated_at`
	p3mProgrammeColumns = `id, portfolio_id, title, description, manager_id, budget_allocated, target_start_date, target_end_date, rag_status, created_at, updated_at`
	p3mProjectColumns   = `id, portfolio_id, programme_id, title, description, manager_id, time_project_id, current_stage, budget_approved, budget_actual, progress_percent, start_date, target_completion_date, rag_status, created_at, updated_at`
	p3mRaidColumns      = `id, entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, item_id, created_at, updated_at`
	p3mStageGateColumns = `id, project_id, from_stage, to_stage, passed, reviewer_id, review_date, comments, created_at`
	p3mBenefitColumns   = `id, project_id, programme_id, title, benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, created_at`
)

func aiScanP3MPortfolio(row interface{ Scan(...any) error }) (models.P3MPortfolio, error) {
	var pf models.P3MPortfolio
	err := row.Scan(&pf.ID, &pf.Title, &pf.Description, &pf.StrategicObjective,
		&pf.BudgetAllocated, &pf.RagStatus, &pf.OwnerID, &pf.CreatedAt, &pf.UpdatedAt)
	return pf, err
}

func aiScanP3MProgramme(row interface{ Scan(...any) error }) (models.P3MProgramme, error) {
	var pg models.P3MProgramme
	err := row.Scan(&pg.ID, &pg.PortfolioID, &pg.Title, &pg.Description, &pg.ManagerID,
		&pg.BudgetAllocated, &pg.TargetStartDate, &pg.TargetEndDate, &pg.RagStatus, &pg.CreatedAt, &pg.UpdatedAt)
	return pg, err
}

func aiScanP3MProject(row interface{ Scan(...any) error }) (models.P3MProject, error) {
	var p models.P3MProject
	err := row.Scan(&p.ID, &p.PortfolioID, &p.ProgrammeID, &p.Title, &p.Description, &p.ManagerID, &p.TimeProjectID,
		&p.CurrentStage, &p.BudgetApproved, &p.BudgetActual, &p.ProgressPercent,
		&p.StartDate, &p.TargetCompletionDate, &p.RagStatus, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func aiScanP3MRaidItem(row interface{ Scan(...any) error }) (models.P3MRaidItem, error) {
	var item models.P3MRaidItem
	err := row.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.ItemType, &item.Title, &item.Description,
		&item.ImpactScore, &item.ProbabilityScore, &item.MitigationPlan, &item.OwnerID, &item.Status, &item.ItemID,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func aiScanP3MStageGate(row interface{ Scan(...any) error }) (models.P3MStageGateReview, error) {
	var g models.P3MStageGateReview
	err := row.Scan(&g.ID, &g.ProjectID, &g.FromStage, &g.ToStage, &g.Passed, &g.ReviewerID, &g.ReviewDate, &g.Comments, &g.CreatedAt)
	return g, err
}

// --- RAG + EVM computation (mirrors handlers.p3mComputeEVM / p3mComputeProjectRAG) ---

func aiP3MRagRank(status string) int {
	switch status {
	case models.P3MRagRed:
		return 0
	case models.P3MRagAmber:
		return 1
	default:
		return 2
	}
}

func aiP3MRankToRag(rank int) string {
	switch rank {
	case 0:
		return models.P3MRagRed
	case 1:
		return models.P3MRagAmber
	default:
		return models.P3MRagGreen
	}
}

func aiP3MComputeEVM(p *models.P3MProject, now time.Time) *models.P3MEVM {
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

func aiP3MComputeProjectRAG(p *models.P3MProject, evm *models.P3MEVM) string {
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

func aiP3MApplyComputed(p *models.P3MProject) {
	evm := aiP3MComputeEVM(p, time.Now().UTC())
	p.EVM = evm
	p.RagStatus = aiP3MComputeProjectRAG(p, evm)
}

func aiP3MApplyComputedRaid(item *models.P3MRaidItem) {
	item.RiskScore = item.ImpactScore * item.ProbabilityScore
	switch {
	case item.RiskScore >= 15:
		item.PriorityBucket = "high"
	case item.RiskScore >= 8:
		item.PriorityBucket = "medium"
	default:
		item.PriorityBucket = "low"
	}
	item.RequiresEscalation = item.EntityType == models.P3MEntityProject && item.RiskScore >= 15
}

// --- validation helpers ---

func aiP3MValidStage(s string) bool {
	switch s {
	case models.P3MStageInitiation, models.P3MStagePlanning, models.P3MStageExecution, models.P3MStageClosure:
		return true
	default:
		return false
	}
}

func aiP3MValidRaidItemType(itemType string) bool {
	switch itemType {
	case "risk", "assumption", "issue", "dependency", models.P3MRAIDItemTypeDecision:
		return true
	default:
		return false
	}
}

func aiP3MValidRaidStatus(status string) bool {
	switch status {
	case "open", "mitigating", "closed", "escalated":
		return true
	default:
		return false
	}
}

func aiP3MValidBenefitType(t string) bool {
	switch t {
	case "financial", "non_financial", "strategic", "compliance":
		return true
	default:
		return false
	}
}

func aiP3MEntityExists(db database.Database, entityType string, entityID int) bool {
	switch entityType {
	case models.P3MEntityPortfolio:
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM p3m_portfolios WHERE id = ?`, entityID).Scan(&n); err != nil {
			return false
		}
		return n > 0
	case models.P3MEntityProgramme:
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM p3m_programmes WHERE id = ?`, entityID).Scan(&n); err != nil {
			return false
		}
		return n > 0
	case models.P3MEntityProject:
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM p3m_projects WHERE id = ?`, entityID).Scan(&n); err != nil {
			return false
		}
		return n > 0
	default:
		return false
	}
}

func aiGetP3MProjectRow(db database.Database, id int) (models.P3MProject, error) {
	var p models.P3MProject
	err := db.QueryRow(`SELECT `+p3mProjectColumns+` FROM p3m_projects WHERE id = ?`, id).Scan(
		&p.ID, &p.PortfolioID, &p.ProgrammeID, &p.Title, &p.Description, &p.ManagerID, &p.TimeProjectID,
		&p.CurrentStage, &p.BudgetApproved, &p.BudgetActual, &p.ProgressPercent,
		&p.StartDate, &p.TargetCompletionDate, &p.RagStatus, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func aiRequireP3MView(env *Env) (bool, error) {
	ok, err := env.PermService.HasGlobalPermission(env.UserID, models.PermissionP3MView)
	return ok, err
}

func aiP3MLoadGateArtifacts(db database.Database, reviewID int) ([]models.P3MGateArtifact, error) {
	rows, err := db.Query(`SELECT id, review_id, name, required, completed, link_or_note FROM p3m_gate_artifacts WHERE review_id = ? ORDER BY id`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.P3MGateArtifact
	for rows.Next() {
		var a models.P3MGateArtifact
		if err := rows.Scan(&a.ID, &a.ReviewID, &a.Name, &a.Required, &a.Completed, &a.LinkOrNote); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func aiP3MThresholdMap(db database.Database) (map[string]float64, error) {
	rows, err := db.Query(`SELECT level, threshold_amount FROM p3m_authorization_thresholds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var level string
		var amount float64
		if err := rows.Scan(&level, &amount); err != nil {
			return nil, err
		}
		out[level] = amount
	}
	return out, rows.Err()
}

// aiP3MApplyProjectBudget persists a new approved budget and recomputes RAG.
func aiP3MApplyProjectBudget(db database.Database, projectID int, requestedAmount float64) error {
	p, err := aiGetP3MProjectRow(db, projectID)
	if err != nil {
		return err
	}
	p.BudgetApproved = requestedAmount
	aiP3MApplyComputed(&p)
	now := time.Now().UTC()
	_, err = db.Exec(`UPDATE p3m_projects SET budget_approved = ?, rag_status = ?, updated_at = ? WHERE id = ?`,
		requestedAmount, p.RagStatus, now, projectID)
	return err
}

func aiScanP3MBudgetApproval(row interface{ Scan(...any) error }) (models.P3MBudgetApproval, error) {
	var a models.P3MBudgetApproval
	err := row.Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status,
		&a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

const p3mBudgetApprovalColumns = `id, project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at`

func aiScanP3MBenefit(row interface{ Scan(...any) error }) (models.P3MBenefit, error) {
	var b models.P3MBenefit
	err := row.Scan(&b.ID, &b.ProjectID, &b.ProgrammeID, &b.Title, &b.BenefitType, &b.TargetValue,
		&b.UnitOfMeasure, &b.BaselineValue, &b.TargetRealizationDate, &b.OwnerID, &b.CreatedAt)
	return b, err
}

// ----------------------------------------------------------------------------
// Tools
// ----------------------------------------------------------------------------

type getP3MHierarchyArgs struct{}

type listP3MProjectsArgs struct {
	PortfolioID *int   `json:"portfolio_id,omitempty" jsonschema:"Filter to projects in this portfolio (including via programmes)."`
	ProgrammeID *int   `json:"programme_id,omitempty" jsonschema:"Filter to projects in this programme."`
	Stage       string `json:"stage,omitempty" jsonschema:"Filter by current stage: initiation, planning, execution, closure."`
}

type getP3MProjectArgs struct {
	ProjectID int `json:"project_id" jsonschema:"The P3M project's numeric ID."`
}

type listP3MRaidArgs struct {
	EntityType string `json:"entity_type,omitempty" jsonschema:"Filter by owner tier: portfolio, programme, or project."`
	EntityID   *int   `json:"entity_id,omitempty" jsonschema:"Filter by owner entity ID."`
	ItemType   string `json:"item_type,omitempty" jsonschema:"Filter by type: risk, assumption, issue, dependency, or decision."`
	Status     string `json:"status,omitempty" jsonschema:"Filter by status: open, mitigating, closed, escalated."`
}

type createP3MRaidArgs struct {
	EntityType       string `json:"entity_type" jsonschema:"Owner tier: portfolio, programme, or project."`
	EntityID         int    `json:"entity_id" jsonschema:"Owner entity ID."`
	ItemType         string `json:"item_type" jsonschema:"Type: risk, assumption, issue, dependency, or decision."`
	Title            string `json:"title" jsonschema:"Short title."`
	Description      string `json:"description,omitempty" jsonschema:"Detail."`
	ImpactScore      int    `json:"impact_score" jsonschema:"Impact 1-5."`
	ProbabilityScore int    `json:"probability_score" jsonschema:"Probability 1-5."`
	MitigationPlan   string `json:"mitigation_plan,omitempty" jsonschema:"Mitigation / response plan."`
	OwnerID          int    `json:"owner_id" jsonschema:"Owning user ID."`
	Status           string `json:"status,omitempty" jsonschema:"Status: open (default), mitigating, closed, escalated."`
	ItemID           *int   `json:"item_id,omitempty" jsonschema:"Optional linked work item ID."`
}

type updateP3MRaidArgs struct {
	ID               int    `json:"id" jsonschema:"The RAID item's numeric ID."`
	EntityType       string `json:"entity_type" jsonschema:"Owner tier: portfolio, programme, or project."`
	EntityID         int    `json:"entity_id" jsonschema:"Owner entity ID."`
	ItemType         string `json:"item_type" jsonschema:"Type: risk, assumption, issue, dependency, or decision."`
	Title            string `json:"title" jsonschema:"Short title."`
	Description      string `json:"description,omitempty" jsonschema:"Detail."`
	ImpactScore      int    `json:"impact_score" jsonschema:"Impact 1-5."`
	ProbabilityScore int    `json:"probability_score" jsonschema:"Probability 1-5."`
	MitigationPlan   string `json:"mitigation_plan,omitempty" jsonschema:"Mitigation / response plan."`
	OwnerID          int    `json:"owner_id" jsonschema:"Owning user ID."`
	Status           string `json:"status" jsonschema:"Status: open, mitigating, closed, escalated."`
	ItemID           *int   `json:"item_id,omitempty" jsonschema:"Optional linked work item ID."`
}

type listP3MBudgetApprovalsArgs struct {
	Status    string `json:"status,omitempty" jsonschema:"Filter by status: pending, approved, rejected."`
	ProjectID *int   `json:"project_id,omitempty" jsonschema:"Filter by project ID."`
}

type createP3MBudgetApprovalArgs struct {
	ProjectID     int     `json:"project_id" jsonschema:"The project requesting the budget increase."`
	RequestAmount float64 `json:"requested_amount" jsonschema:"Proposed new total budget (must exceed the current approved budget)."`
}

type decideP3MBudgetApprovalArgs struct {
	ApprovalID int    `json:"approval_id" jsonschema:"The budget approval's numeric ID."`
	Passed     bool   `json:"passed" jsonschema:"true to approve (applies the amount), false to reject."`
	Notes      string `json:"notes,omitempty" jsonschema:"Decision notes."`
}

type listP3MBenefitsArgs struct {
	ProjectID   *int `json:"project_id,omitempty" jsonschema:"Filter by project ID."`
	ProgrammeID *int `json:"programme_id,omitempty" jsonschema:"Filter by programme ID."`
}

type createP3MBenefitArgs struct {
	ProjectID               *int     `json:"project_id,omitempty" jsonschema:"Bound project ID (exactly one of project_id/programme_id)."`
	ProgrammeID             *int     `json:"programme_id,omitempty" jsonschema:"Bound programme ID (exactly one of project_id/programme_id)."`
	Title                   string   `json:"title" jsonschema:"Benefit title."`
	BenefitType             string   `json:"benefit_type" jsonschema:"Type: financial, non_financial, strategic, compliance."`
	TargetValue             float64  `json:"target_value" jsonschema:"Target value."`
	UnitOfMeasure           string   `json:"unit_of_measure,omitempty" jsonschema:"Unit of measure (e.g. $, FTE, %)."`
	BaselineValue           float64  `json:"baseline_value" jsonschema:"Baseline (current) value."`
	TargetRealizationDate   string   `json:"target_realization_date" jsonschema:"Target realization date (YYYY-MM-DD or RFC 3339)."`
	OwnerID                 int      `json:"owner_id" jsonschema:"Benefit owner user ID."`
}

type listP3MStageGatesArgs struct {
	ProjectID *int `json:"project_id,omitempty" jsonschema:"Filter by project ID."`
}

type createP3MStageGateArgs struct {
	ProjectID int    `json:"project_id" jsonschema:"The project being gated."`
	ToStage   string `json:"to_stage" jsonschema:"Target stage: initiation, planning, execution, closure (must differ from current)."`
	Comments  string `json:"comments,omitempty" jsonschema:"Reviewer comments."`
	Artifacts []struct {
		Name       string `json:"name" jsonschema:"Artifact name."`
		Required   bool   `json:"required,omitempty" jsonschema:"Whether this artifact is mandatory to pass."`
		LinkOrNote string `json:"link_or_note,omitempty" jsonschema:"Link or note."`
	} `json:"artifacts,omitempty" jsonschema:"Gate checklist artifacts."`
}

type decideP3MStageGateArgs struct {
	GateID int  `json:"gate_id" jsonschema:"The stage-gate review's numeric ID."`
	Passed bool `json:"passed" jsonschema:"true to pass (advances the project), false to fail."`
}

func init() {
	Register(Default, Tool[getP3MHierarchyArgs]{
		Name:        "get_p3m_hierarchy",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get the full portfolio→programme→project hierarchy with per-tier rollups (counts, budgets, worst RAG).",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, _ getP3MHierarchyArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			pfRows, err := env.DB.Query(`SELECT ` + p3mPortfolioColumns + ` FROM p3m_portfolios ORDER BY title`)
			if err != nil {
				return nil, err
			}
			portfolios := make([]models.P3MPortfolio, 0)
			for pfRows.Next() {
				pf, err := aiScanP3MPortfolio(pfRows)
				if err != nil {
					pfRows.Close()
					return nil, err
				}
				portfolios = append(portfolios, pf)
			}
			pfRows.Close()

			pgRows, err := env.DB.Query(`SELECT ` + p3mProgrammeColumns + ` FROM p3m_programmes ORDER BY title`)
			if err != nil {
				return nil, err
			}
			programmesByPortfolio := make(map[int][]models.P3MProgramme)
			for pgRows.Next() {
				pg, err := aiScanP3MProgramme(pgRows)
				if err != nil {
					pgRows.Close()
					return nil, err
				}
				programmesByPortfolio[pg.PortfolioID] = append(programmesByPortfolio[pg.PortfolioID], pg)
			}
			pgRows.Close()

			prRows, err := env.DB.Query(`SELECT ` + p3mProjectColumns + ` FROM p3m_projects ORDER BY title`)
			if err != nil {
				return nil, err
			}
			projectsByProgramme := make(map[int][]models.P3MProject)
			standaloneByPortfolio := make(map[int][]models.P3MProject)
			for prRows.Next() {
				p, err := aiScanP3MProject(prRows)
				if err != nil {
					prRows.Close()
					return nil, err
				}
				aiP3MApplyComputed(&p)
				switch {
				case p.ProgrammeID != nil:
					projectsByProgramme[*p.ProgrammeID] = append(projectsByProgramme[*p.ProgrammeID], p)
				case p.PortfolioID != nil:
					standaloneByPortfolio[*p.PortfolioID] = append(standaloneByPortfolio[*p.PortfolioID], p)
				}
			}
			prRows.Close()

			// Compute rollups in memory from the loaded projects.
			type acc struct {
				programmeCount int
				projectCount   int
				budgetApproved float64
				budgetActual   float64
				worstRank      int
			}
			pgAcc := make(map[int]*acc)
			pfAcc := make(map[int]*acc)
			add := func(a *acc, p models.P3MProject) {
				a.projectCount++
				a.budgetApproved += p.BudgetApproved
				a.budgetActual += p.BudgetActual
				if r := aiP3MRagRank(p.RagStatus); r < a.worstRank {
					a.worstRank = r
				}
			}
			for pgID, pgs := range projectsByProgramme {
				a := &acc{worstRank: 2}
				for _, p := range pgs {
					add(a, p)
				}
				pgAcc[pgID] = a
			}
			for _, pf := range portfolios {
				pfa := &acc{worstRank: aiP3MRagRank(pf.RagStatus)}
				pfAcc[pf.ID] = pfa
				pfa.programmeCount = len(programmesByPortfolio[pf.ID])
				for _, p := range standaloneByPortfolio[pf.ID] {
					add(pfa, p)
				}
				for _, pg := range programmesByPortfolio[pf.ID] {
					if a, ok2 := pgAcc[pg.ID]; ok2 {
						pfa.projectCount += a.projectCount
						pfa.budgetApproved += a.budgetApproved
						pfa.budgetActual += a.budgetActual
						if a.worstRank < pfa.worstRank {
							pfa.worstRank = a.worstRank
						}
					}
				}
			}

			for i := range portfolios {
				pfa := pfAcc[portfolios[i].ID]
				portfolios[i].Rollup = &models.P3MPortfolioRollup{
					ProgrammeCount: pfa.programmeCount,
					ProjectCount:   pfa.projectCount,
					BudgetApproved: pfa.budgetApproved,
					BudgetActual:   pfa.budgetActual,
					RagStatus:      aiP3MRankToRag(pfa.worstRank),
				}
				portfolios[i].StandaloneProjects = standaloneByPortfolio[portfolios[i].ID]
				pgs := programmesByPortfolio[portfolios[i].ID]
				for j := range pgs {
					rollup := &models.P3MProgrammeRollup{RagStatus: models.P3MRagGreen}
					if paga, ok2 := pgAcc[pgs[j].ID]; ok2 {
						rollup.ProjectCount = paga.projectCount
						rollup.BudgetApproved = paga.budgetApproved
						rollup.BudgetActual = paga.budgetActual
						rollup.RagStatus = aiP3MRankToRag(paga.worstRank)
					}
					pgs[j].Rollup = rollup
					pgs[j].Projects = projectsByProgramme[pgs[j].ID]
				}
				portfolios[i].Programmes = pgs
			}
			return map[string]any{"portfolios": portfolios}, nil
		},
	})

	Register(Default, Tool[listP3MProjectsArgs]{
		Name:        "list_p3m_projects",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M projects with computed EVM (SPI/CPI) and automatic RAG. Filter by portfolio, programme, or stage.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args listP3MProjectsArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + p3mProjectColumns + ` FROM p3m_projects`
			var conds []string
			var qArgs []any
			if args.PortfolioID != nil {
				conds = append(conds, `COALESCE(portfolio_id, (SELECT portfolio_id FROM p3m_programmes WHERE id = programme_id)) = ?`)
				qArgs = append(qArgs, *args.PortfolioID)
			}
			if args.ProgrammeID != nil {
				conds = append(conds, `programme_id = ?`)
				qArgs = append(qArgs, *args.ProgrammeID)
			}
			if args.Stage != "" {
				conds = append(conds, `current_stage = ?`)
				qArgs = append(qArgs, args.Stage)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY title`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			projects := make([]models.P3MProject, 0)
			for rows.Next() {
				p, err := aiScanP3MProject(rows)
				if err != nil {
					return nil, err
				}
				aiP3MApplyComputed(&p)
				projects = append(projects, p)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return projects, nil
		},
	})

	Register(Default, Tool[getP3MProjectArgs]{
		Name:        "get_p3m_project",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get a single P3M project with its computed EVM block and automatic RAG.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args getP3MProjectArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.ProjectID <= 0 {
				return map[string]string{"error": "project_id is required"}, nil
			}
			p, err := aiGetP3MProjectRow(env.DB, args.ProjectID)
			if err != nil {
				return map[string]string{"error": "project not found"}, nil
			}
			aiP3MApplyComputed(&p)
			return p, nil
		},
	})

	Register(Default, Tool[listP3MRaidArgs]{
		Name:        "list_p3m_raid",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M RAID register items (risk/assumption/issue/dependency/decision) with computed risk score and priority bucket.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args listP3MRaidArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + p3mRaidColumns + ` FROM p3m_raid_items`
			var conds []string
			var qArgs []any
			if args.EntityType != "" {
				conds = append(conds, "entity_type = ?")
				qArgs = append(qArgs, args.EntityType)
			}
			if args.EntityID != nil {
				conds = append(conds, "entity_id = ?")
				qArgs = append(qArgs, *args.EntityID)
			}
			if args.ItemType != "" {
				conds = append(conds, "item_type = ?")
				qArgs = append(qArgs, args.ItemType)
			}
			if args.Status != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, args.Status)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			items := make([]models.P3MRaidItem, 0)
			for rows.Next() {
				item, err := aiScanP3MRaidItem(rows)
				if err != nil {
					return nil, err
				}
				aiP3MApplyComputedRaid(&item)
				items = append(items, item)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return items, nil
		},
	})

	Register(Default, Tool[createP3MRaidArgs]{
		Name:        "create_p3m_raid",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M RAID register item (risk, assumption, issue, dependency, or decision) on a portfolio, programme, or project.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MRaidArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if !aiP3MEntityExists(env.DB, args.EntityType, args.EntityID) {
				return map[string]string{"error": "entity not found"}, nil
			}
			if !aiP3MValidRaidItemType(args.ItemType) {
				return map[string]string{"error": "item_type must be one of risk, assumption, issue, dependency or decision"}, nil
			}
			if args.ImpactScore < 1 || args.ImpactScore > 5 {
				return map[string]string{"error": "impact_score must be between 1 and 5"}, nil
			}
			if args.ProbabilityScore < 1 || args.ProbabilityScore > 5 {
				return map[string]string{"error": "probability_score must be between 1 and 5"}, nil
			}
			var ownerCount int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, args.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
				return map[string]string{"error": "owner user not found"}, nil
			}
			if args.ItemID != nil {
				var itemCount int
				if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, *args.ItemID).Scan(&itemCount); err != nil || itemCount == 0 {
					return map[string]string{"error": "linked item not found"}, nil
				}
			}
			status := args.Status
			if status == "" {
				status = "open"
			}
			if !aiP3MValidRaidStatus(status) {
				return map[string]string{"error": "status must be one of open, mitigating, closed or escalated"}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, mitigation_plan, owner_id, status, item_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.EntityType, args.EntityID, args.ItemType, args.Title, args.Description, args.ImpactScore, args.ProbabilityScore, args.MitigationPlan, args.OwnerID, status, nullIntPtr(args.ItemID), now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			var item models.P3MRaidItem
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mRaidColumns+` FROM p3m_raid_items WHERE id = ?`, int(id64)).Scan(
				&item.ID, &item.EntityType, &item.EntityID, &item.ItemType, &item.Title, &item.Description,
				&item.ImpactScore, &item.ProbabilityScore, &item.MitigationPlan, &item.OwnerID, &item.Status, &item.ItemID,
				&item.CreatedAt, &item.UpdatedAt); err != nil {
				return nil, err
			}
			aiP3MApplyComputedRaid(&item)
			env.AuditWrite("p3m_raid_item", int(id64), "create_p3m_raid", args.Title)
			return item, nil
		},
	})

	Register(Default, Tool[updateP3MRaidArgs]{
		Name:        "update_p3m_raid",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Update a P3M RAID register item. The full mutable state is replaced.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args updateP3MRaidArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.ID <= 0 {
				return map[string]string{"error": "id is required"}, nil
			}
			if !aiP3MEntityExists(env.DB, args.EntityType, args.EntityID) {
				return map[string]string{"error": "entity not found"}, nil
			}
			if !aiP3MValidRaidItemType(args.ItemType) {
				return map[string]string{"error": "item_type must be one of risk, assumption, issue, dependency or decision"}, nil
			}
			if args.ImpactScore < 1 || args.ImpactScore > 5 {
				return map[string]string{"error": "impact_score must be between 1 and 5"}, nil
			}
			if args.ProbabilityScore < 1 || args.ProbabilityScore > 5 {
				return map[string]string{"error": "probability_score must be between 1 and 5"}, nil
			}
			var ownerCount int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, args.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
				return map[string]string{"error": "owner user not found"}, nil
			}
			if args.ItemID != nil {
				var itemCount int
				if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, *args.ItemID).Scan(&itemCount); err != nil || itemCount == 0 {
					return map[string]string{"error": "linked item not found"}, nil
				}
			}
			if !aiP3MValidRaidStatus(args.Status) {
				return map[string]string{"error": "status must be one of open, mitigating, closed or escalated"}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `UPDATE p3m_raid_items SET entity_type = ?, entity_id = ?, item_type = ?, title = ?, description = ?, impact_score = ?, probability_score = ?, mitigation_plan = ?, owner_id = ?, status = ?, item_id = ?, updated_at = ? WHERE id = ?`,
				args.EntityType, args.EntityID, args.ItemType, args.Title, args.Description, args.ImpactScore, args.ProbabilityScore, args.MitigationPlan, args.OwnerID, args.Status, nullIntPtr(args.ItemID), now, args.ID)
			if err != nil {
				return nil, err
			}
			if affected, _ := res.RowsAffected(); affected == 0 {
				return map[string]string{"error": "raid item not found"}, nil
			}
			var item models.P3MRaidItem
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mRaidColumns+` FROM p3m_raid_items WHERE id = ?`, args.ID).Scan(
				&item.ID, &item.EntityType, &item.EntityID, &item.ItemType, &item.Title, &item.Description,
				&item.ImpactScore, &item.ProbabilityScore, &item.MitigationPlan, &item.OwnerID, &item.Status, &item.ItemID,
				&item.CreatedAt, &item.UpdatedAt); err != nil {
				return nil, err
			}
			aiP3MApplyComputedRaid(&item)
			env.AuditWrite("p3m_raid_item", args.ID, "update_p3m_raid", item.Title)
			return item, nil
		},
	})

	Register(Default, Tool[listP3MBudgetApprovalsArgs]{
		Name:        "list_p3m_budget_approvals",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M budget (spend-authorization) requests with their routing level and status.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args listP3MBudgetApprovalsArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + p3mBudgetApprovalColumns + ` FROM p3m_budget_approvals`
			var conds []string
			var qArgs []any
			if args.Status != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, args.Status)
			}
			if args.ProjectID != nil {
				conds = append(conds, "project_id = ?")
				qArgs = append(qArgs, *args.ProjectID)
			}
			if len(conds) > 0 {
				query += ` WHERE ` + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.P3MBudgetApproval, 0)
			for rows.Next() {
				a, err := aiScanP3MBudgetApproval(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, a)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[createP3MBudgetApprovalArgs]{
		Name:        "create_p3m_budget_approval",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Request a P3M project budget increase. Increases within the project's authorization threshold auto-approve and apply; larger ones route up for a decision.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MBudgetApprovalArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			project, err := aiGetP3MProjectRow(env.DB, args.ProjectID)
			if err != nil {
				return map[string]string{"error": "project not found"}, nil
			}
			if args.RequestAmount <= project.BudgetApproved {
				return map[string]string{"error": "requested_amount must exceed the current approved budget"}, nil
			}
			thresholds, err := aiP3MThresholdMap(env.DB)
			if err != nil {
				return nil, err
			}
			delta := args.RequestAmount - project.BudgetApproved
			routedToLevel := models.P3MEntityPortfolio
			switch {
			case delta <= thresholds[models.P3MEntityProject]:
				routedToLevel = models.P3MEntityProject
			case delta <= thresholds[models.P3MEntityProgramme]:
				routedToLevel = models.P3MEntityProgramme
			}
			status := "pending"
			var decidedBy *int
			decisionNotes := ""
			if routedToLevel == models.P3MEntityProject {
				status = "approved"
				decidedBy = &env.UserID
				decisionNotes = "auto-approved within project threshold"
				if err := aiP3MApplyProjectBudget(env.DB, args.ProjectID, args.RequestAmount); err != nil {
					return nil, err
				}
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `INSERT INTO p3m_budget_approvals (project_id, current_budget, requested_amount, routed_to_level, status, requested_by, decided_by, decision_notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.ProjectID, project.BudgetApproved, args.RequestAmount, routedToLevel, status, env.UserID, nullIntPtr(decidedBy), decisionNotes, now, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			var a models.P3MBudgetApproval
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mBudgetApprovalColumns+` FROM p3m_budget_approvals WHERE id = ?`, int(id64)).Scan(
				&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status,
				&a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt); err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_budget_approval", int(id64), "create_p3m_budget_approval", fmt.Sprintf("project %d -> %v", args.ProjectID, args.RequestAmount))
			return a, nil
		},
	})

	Register(Default, Tool[decideP3MBudgetApprovalArgs]{
		Name:        "decide_p3m_budget_approval",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Approve or reject a pending P3M budget request. Approval applies the requested amount to the project budget.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args decideP3MBudgetApprovalArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.ApprovalID <= 0 {
				return map[string]string{"error": "approval_id is required"}, nil
			}
			var a models.P3MBudgetApproval
			err = env.DB.QueryRowContext(ctx, `SELECT `+p3mBudgetApprovalColumns+` FROM p3m_budget_approvals WHERE id = ?`, args.ApprovalID).
				Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
			if err != nil {
				return map[string]string{"error": "budget approval not found"}, nil
			}
			if a.Status != "pending" {
				return map[string]string{"error": "approval already decided"}, nil
			}
			status := "rejected"
			if args.Passed {
				if err := aiP3MApplyProjectBudget(env.DB, a.ProjectID, a.RequestAmount); err != nil {
					return nil, err
				}
				status = "approved"
			}
			now := time.Now().UTC()
			if _, err := env.DB.ExecContext(ctx, `UPDATE p3m_budget_approvals SET status = ?, decided_by = ?, decision_notes = ?, updated_at = ? WHERE id = ?`,
				status, env.UserID, args.Notes, now, args.ApprovalID); err != nil {
				return nil, err
			}
			env.DB.QueryRowContext(ctx, `SELECT `+p3mBudgetApprovalColumns+` FROM p3m_budget_approvals WHERE id = ?`, args.ApprovalID).
				Scan(&a.ID, &a.ProjectID, &a.CurrentBudget, &a.RequestAmount, &a.RoutedToLevel, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecisionNotes, &a.CreatedAt, &a.UpdatedAt)
			env.AuditWrite("p3m_budget_approval", args.ApprovalID, "decide_p3m_budget_approval", status)
			return a, nil
		},
	})

	Register(Default, Tool[listP3MBenefitsArgs]{
		Name:        "list_p3m_benefits",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M benefits (value outcomes) bound to projects or programmes, with the latest realized value.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(_ context.Context, env *Env, args listP3MBenefitsArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + p3mBenefitColumns + ` FROM p3m_benefits`
			var conds []string
			var qArgs []any
			if args.ProjectID != nil {
				conds = append(conds, "project_id = ?")
				qArgs = append(qArgs, *args.ProjectID)
			}
			if args.ProgrammeID != nil {
				conds = append(conds, "programme_id = ?")
				qArgs = append(qArgs, *args.ProgrammeID)
			}
			if len(conds) > 0 {
				query += ` WHERE ` + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.P3MBenefit, 0)
			for rows.Next() {
				b, err := aiScanP3MBenefit(rows)
				if err != nil {
					return nil, err
				}
				// Latest realized value.
				var latest sql.NullFloat64
				if err := env.DB.QueryRow(`SELECT realized_value FROM p3m_benefit_realizations WHERE benefit_id = ? ORDER BY recorded_date DESC, id DESC LIMIT 1`, b.ID).Scan(&latest); err == nil && latest.Valid {
					b.LatestRealizedValue = new(latest.Float64)
				}
				out = append(out, b)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[createP3MBenefitArgs]{
		Name:        "create_p3m_benefit",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M benefit bound to exactly one project or programme.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MBenefitArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if (args.ProjectID == nil) == (args.ProgrammeID == nil) {
				return map[string]string{"error": "exactly one of project_id or programme_id is required"}, nil
			}
			if args.ProjectID != nil && !aiP3MEntityExists(env.DB, models.P3MEntityProject, *args.ProjectID) {
				return map[string]string{"error": "project not found"}, nil
			}
			if args.ProgrammeID != nil && !aiP3MEntityExists(env.DB, models.P3MEntityProgramme, *args.ProgrammeID) {
				return map[string]string{"error": "programme not found"}, nil
			}
			if !aiP3MValidBenefitType(args.BenefitType) {
				return map[string]string{"error": "benefit_type must be financial, non_financial, strategic, or compliance"}, nil
			}
			var ownerCount int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, args.OwnerID).Scan(&ownerCount); err != nil || ownerCount == 0 {
				return map[string]string{"error": "owner user not found"}, nil
			}
			date, err := aiParsePlanDate(args.TargetRealizationDate)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			if date == nil {
				return map[string]string{"error": "target_realization_date is required"}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `INSERT INTO p3m_benefits (project_id, programme_id, title, benefit_type, target_value, unit_of_measure, baseline_value, target_realization_date, owner_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nullIntPtr(args.ProjectID), nullIntPtr(args.ProgrammeID), args.Title, args.BenefitType, args.TargetValue, args.UnitOfMeasure, args.BaselineValue, *date, args.OwnerID, now)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			var b models.P3MBenefit
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mBenefitColumns+` FROM p3m_benefits WHERE id = ?`, int(id64)).Scan(
				&b.ID, &b.ProjectID, &b.ProgrammeID, &b.Title, &b.BenefitType, &b.TargetValue,
				&b.UnitOfMeasure, &b.BaselineValue, &b.TargetRealizationDate, &b.OwnerID, &b.CreatedAt); err != nil {
				return nil, err
			}
			env.AuditWrite("p3m_benefit", int(id64), "create_p3m_benefit", args.Title)
			return b, nil
		},
	})

	Register(Default, Tool[listP3MStageGatesArgs]{
		Name:        "list_p3m_stage_gates",
		Group:       CapabilityP3M,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List P3M stage-gate reviews with their artifact checklists. Filter by project.",
		Scopes:      []string{auth.ScopeP3MRead},
		Run: func(ctx context.Context, env *Env, args listP3MStageGatesArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + p3mStageGateColumns + ` FROM p3m_stage_gate_reviews`
			var qArgs []any
			if args.ProjectID != nil {
				query += ` WHERE project_id = ?`
				qArgs = append(qArgs, *args.ProjectID)
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			gates := make([]models.P3MStageGateReview, 0)
			for rows.Next() {
				g, err := aiScanP3MStageGate(rows)
				if err != nil {
					return nil, err
				}
				gates = append(gates, g)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			for i := range gates {
				arts, err := aiP3MLoadGateArtifacts(env.DB, gates[i].ID)
				if err != nil {
					return nil, err
				}
				gates[i].Artifacts = arts
			}
			return gates, nil
		},
	})

	Register(Default, Tool[createP3MStageGateArgs]{
		Name:        "create_p3m_stage_gate",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a P3M stage-gate review for a project, optionally with an artifact checklist.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args createP3MStageGateArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			project, err := aiGetP3MProjectRow(env.DB, args.ProjectID)
			if err != nil {
				return map[string]string{"error": "project not found"}, nil
			}
			if !aiP3MValidStage(args.ToStage) {
				return map[string]string{"error": "to_stage must be one of initiation, planning, execution or closure"}, nil
			}
			if args.ToStage == project.CurrentStage {
				return map[string]string{"error": "to_stage must differ from the current stage"}, nil
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx, `INSERT INTO p3m_stage_gate_reviews (project_id, from_stage, to_stage, passed, reviewer_id, review_date, comments, created_at) VALUES (?, ?, ?, false, ?, NULL, ?, ?)`,
				args.ProjectID, project.CurrentStage, args.ToStage, env.UserID, args.Comments, now)
			if err != nil {
				return nil, err
			}
			reviewID64, _ := res.LastInsertId()
			reviewID := int(reviewID64)
			for _, a := range args.Artifacts {
				if a.Name == "" {
					continue
				}
				if _, err := env.DB.ExecContext(ctx, `INSERT INTO p3m_gate_artifacts (review_id, name, required, completed, link_or_note, created_at) VALUES (?, ?, ?, false, ?, ?)`, reviewID, a.Name, a.Required, a.LinkOrNote, now); err != nil {
					return nil, err
				}
			}
			var g models.P3MStageGateReview
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mStageGateColumns+` FROM p3m_stage_gate_reviews WHERE id = ?`, reviewID).
				Scan(&g.ID, &g.ProjectID, &g.FromStage, &g.ToStage, &g.Passed, &g.ReviewerID, &g.ReviewDate, &g.Comments, &g.CreatedAt); err != nil {
				return nil, err
			}
			g.Artifacts, _ = aiP3MLoadGateArtifacts(env.DB, reviewID)
			env.AuditWrite("p3m_stage_gate", reviewID, "create_p3m_stage_gate", fmt.Sprintf("project %d -> %s", args.ProjectID, args.ToStage))
			return g, nil
		},
	})

	Register(Default, Tool[decideP3MStageGateArgs]{
		Name:        "decide_p3m_stage_gate",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Pass or fail a P3M stage-gate review. A passing decision requires every required artifact completed and the project still in the gate's from_stage; it then advances the project to to_stage.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args decideP3MStageGateArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.GateID <= 0 {
				return map[string]string{"error": "gate_id is required"}, nil
			}
			var gate models.P3MStageGateReview
			err = env.DB.QueryRowContext(ctx, `SELECT `+p3mStageGateColumns+` FROM p3m_stage_gate_reviews WHERE id = ?`, args.GateID).
				Scan(&gate.ID, &gate.ProjectID, &gate.FromStage, &gate.ToStage, &gate.Passed, &gate.ReviewerID, &gate.ReviewDate, &gate.Comments, &gate.CreatedAt)
			if err != nil {
				return map[string]string{"error": "stage gate not found"}, nil
			}
			gate.Artifacts, err = aiP3MLoadGateArtifacts(env.DB, args.GateID)
			if err != nil {
				return nil, err
			}
			project, err := aiGetP3MProjectRow(env.DB, gate.ProjectID)
			if err != nil {
				return map[string]string{"error": "project not found"}, nil
			}
			now := time.Now().UTC()
			if args.Passed {
				for _, a := range gate.Artifacts {
					if a.Required && !a.Completed {
						return map[string]string{"error": "required artifacts are not all completed"}, nil
					}
				}
				if project.CurrentStage != gate.FromStage {
					return map[string]string{"error": "project stage changed since the gate was created"}, nil
				}
				if _, err := env.DB.ExecContext(ctx, `UPDATE p3m_projects SET current_stage = ?, updated_at = ? WHERE id = ?`, gate.ToStage, now, project.ID); err != nil {
					return nil, err
				}
			}
			if _, err := env.DB.ExecContext(ctx, `UPDATE p3m_stage_gate_reviews SET passed = ?, review_date = ? WHERE id = ?`, args.Passed, now, args.GateID); err != nil {
				return nil, err
			}
			// Reload.
			gate = models.P3MStageGateReview{}
			if err := env.DB.QueryRowContext(ctx, `SELECT `+p3mStageGateColumns+` FROM p3m_stage_gate_reviews WHERE id = ?`, args.GateID).
				Scan(&gate.ID, &gate.ProjectID, &gate.FromStage, &gate.ToStage, &gate.Passed, &gate.ReviewerID, &gate.ReviewDate, &gate.Comments, &gate.CreatedAt); err != nil {
				return nil, err
			}
			gate.Artifacts, _ = aiP3MLoadGateArtifacts(env.DB, args.GateID)
			env.AuditWrite("p3m_stage_gate", args.GateID, "decide_p3m_stage_gate", fmt.Sprintf("project %d passed=%v", gate.ProjectID, args.Passed))
			return gate, nil
		},
	})
}
