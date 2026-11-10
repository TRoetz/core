package models

import "time"

// P3M (Portfolio, Programme, Project Management) domain models. The module is
// gated by the global permission PermissionP3MView and spans three tiers of
// control plus seven process perspectives: governance, management control,
// benefits, financials, risk/RAID, stakeholder engagement, and resource
// management.

// P3MRagStatus values shared by all three tiers.
const (
	P3MRagRed   = "red"
	P3MRagAmber = "amber"
	P3MRagGreen = "green"
)

// P3MEntityType is the polymorphic owner discriminator used by RAID items,
// stakeholders, communications, RACI assignments, and budget tranches.
const (
	P3MEntityPortfolio  = "portfolio"
	P3MEntityProgramme  = "programme"
	P3MEntityProject    = "project"
)

// P3MRAIDItemTypeDecision is the decision-register item type: decisions reuse
// p3m_raid_items with impact/probability scores defaulting to 1.
const P3MRAIDItemTypeDecision = "decision"

// P3M architecture review board (ARB) lifecycle: a review starts as work in
// progress, is submitted for ARB, and ends approved or needs_work.
const (
	P3MARBStatusWIP       = "wip"
	P3MARBStatusInReview  = "in_review"
	P3MARBStatusNeedsWork = "needs_work"
	P3MARBStatusApproved  = "approved"
)

// P3MPortfolio is the top tier: a collection of investments aligned to
// corporate strategy.
type P3MPortfolio struct {
	ID                 int       `json:"id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	StrategicObjective string    `json:"strategic_objective"`
	BudgetAllocated    float64   `json:"budget_allocated"`
	RagStatus          string    `json:"rag_status"`
	OwnerID            *int      `json:"owner_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`

	// Rollup is computed server-side on list/get: aggregate budget and the
	// worst RAG across all contained programmes and projects.
	Rollup *P3MPortfolioRollup `json:"rollup,omitempty" db:"-"`

	// Hierarchy nesting, populated by GET /p3m/hierarchy only.
	Programmes         []P3MProgramme `json:"programmes,omitempty" db:"-"`
	StandaloneProjects []P3MProject   `json:"standalone_projects,omitempty" db:"-"`
}

type P3MPortfolioRollup struct {
	ProgrammeCount int     `json:"programme_count"`
	ProjectCount   int     `json:"project_count"`
	BudgetApproved float64 `json:"budget_approved"`
	BudgetActual   float64 `json:"budget_actual"`
	RagStatus      string  `json:"rag_status"`
	FundingSources map[string]float64 `json:"funding_sources,omitempty"` // approved budget by funding source (FIN-002/006)
}

// P3MProgramme is the middle tier: related projects coordinated to realize a
// business outcome.
type P3MProgramme struct {
	ID              int        `json:"id"`
	PortfolioID     int        `json:"portfolio_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	ManagerID       *int       `json:"manager_id,omitempty"`
	BudgetAllocated float64    `json:"budget_allocated"`
	TargetStartDate *time.Time `json:"target_start_date,omitempty"`
	TargetEndDate   *time.Time `json:"target_end_date,omitempty"`
	RagStatus       string     `json:"rag_status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	Rollup *P3MProgrammeRollup `json:"rollup,omitempty" db:"-"`

	// Populated by GET /p3m/hierarchy only.
	Projects []P3MProject `json:"projects,omitempty" db:"-"`
}

type P3MProgrammeRollup struct {
	ProjectCount   int     `json:"project_count"`
	BudgetApproved float64 `json:"budget_approved"`
	BudgetActual   float64 `json:"budget_actual"`
	RagStatus      string  `json:"rag_status"`
}

// P3MProject is the execution tier. It may belong to a programme, or sit
// directly in a portfolio as a standalone initiative (ProgrammeID nil).
type P3MProject struct {
	ID                   int        `json:"id"`
	PortfolioID          *int       `json:"portfolio_id,omitempty"`
	ProgrammeID          *int       `json:"programme_id,omitempty"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	ManagerID            *int       `json:"manager_id,omitempty"`
	TimeProjectID        *int       `json:"time_project_id,omitempty"`
	WorkType             *string    `json:"work_type,omitempty"` // programme | project | continuous_improvement | digital_product | operational
	ExternalRef          *string    `json:"external_ref,omitempty"` // unique reference in the external finance system (FIN-007 matching)
	FundingSource        *string    `json:"funding_source,omitempty"` // e.g. core_budget, capital_fund, grant (FIN-002/006)
	Contingency          float64    `json:"contingency"`              // contingency reserve held against the budget (FIN-010)
	CurrentStage         string     `json:"current_stage"`
	BudgetApproved       float64    `json:"budget_approved"`
	BudgetActual         float64    `json:"budget_actual"`
	ProgressPercent      int        `json:"progress_percent"`
	StartDate            *time.Time `json:"start_date,omitempty"`
	TargetCompletionDate *time.Time `json:"target_completion_date,omitempty"`
	RagStatus            string     `json:"rag_status"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// EVM is computed server-side on list/get (earned value management).
	EVM *P3MEVM `json:"evm,omitempty" db:"-"`
}

// P3MEVM holds earned-value metrics for a project. SPI/CPI are 0 when the
// denominator is zero (not yet computable).
type P3MEVM struct {
	PlannedValue float64 `json:"planned_value"` // PV: budget_approved x time-based schedule progress
	EarnedValue  float64 `json:"earned_value"`  // EV: budget_approved x progress_percent/100
	ActualCost   float64 `json:"actual_cost"`   // AC: budget_actual
	SPI          float64 `json:"spi"`           // EV/PV, schedule performance index
	CPI          float64 `json:"cpi"`           // EV/AC, cost performance index
}

// P3MBenefit is a value outcome bound to exactly one project or programme.
type P3MBenefit struct {
	ID                  int        `json:"id"`
	ProjectID           *int       `json:"project_id,omitempty"`
	ProgrammeID         *int       `json:"programme_id,omitempty"`
	Title               string     `json:"title"`
	BenefitType         string     `json:"benefit_type"` // financial | non_financial | strategic | compliance
	TargetValue         float64    `json:"target_value"`
	UnitOfMeasure       string     `json:"unit_of_measure"`
	BaselineValue       float64    `json:"baseline_value"`
	TargetRealizationDate time.Time `json:"target_realization_date"`
	OwnerID             int        `json:"owner_id"`

	// Benefit profile completion (BEN-003/007/008).
	MeasurementMethod    *string  `json:"measurement_method,omitempty"`
	MeasurementFrequency *string  `json:"measurement_frequency,omitempty"` // monthly | quarterly | annual | one_off
	Status               string   `json:"status"`                          // planned | measuring | realised | shortfall | retired
	Evidence             *string  `json:"evidence,omitempty"`
	FinanceValidatorID   *int     `json:"finance_validator_id,omitempty"`
	ValidationStatus     string   `json:"validation_status"` // not_required | pending | validated
	CapabilityRef        *string  `json:"capability_ref,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	// Computed from the latest realization row (nil when none recorded).
	LatestRealizedValue *float64 `json:"latest_realized_value,omitempty" db:"-"`
}

// P3MBenefitRealization is one point on a benefit's value burn-up series.
type P3MBenefitRealization struct {
	ID            int        `json:"id"`
	BenefitID     int        `json:"benefit_id"`
	RecordedDate  time.Time  `json:"recorded_date"`
	RealizedValue float64    `json:"realized_value"`
	Notes         string     `json:"notes"`
	RecordedBy    int        `json:"recorded_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

// P3MStageGateReview is a governance gate between two project stages. A
// passing review requires every required artifact completed and signs the
// stage transition with the reviewer's identity.
type P3MStageGateReview struct {
	ID         int                `json:"id"`
	ProjectID  int                `json:"project_id"`
	FromStage  string             `json:"from_stage"`
	ToStage    string             `json:"to_stage"`
	Passed     bool               `json:"passed"`
	ReviewerID int                `json:"reviewer_id"`
	ReviewDate *time.Time         `json:"review_date,omitempty"`
	Comments   string             `json:"comments"`
	TemplateID *int               `json:"template_id,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`

	Artifacts []P3MGateArtifact `json:"artifacts,omitempty" db:"-"`
}

// P3MGateArtifact is one mandatory deliverable on a stage-gate checklist.
type P3MGateArtifact struct {
	ID         int    `json:"id"`
	ReviewID   int    `json:"review_id"`
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Completed  bool   `json:"completed"`
	LinkOrNote string `json:"link_or_note"`
}

// P3MRaciAssignment is one RACI role for a user on a tier entity.
type P3MRaciAssignment struct {
	ID         int    `json:"id"`
	EntityType string `json:"entity_type"` // portfolio | programme | project
	EntityID   int    `json:"entity_id"`
	UserID     int    `json:"user_id"`
	Role       string `json:"role"` // responsible | accountable | consulted | informed
}

// P3MAuthorizationThreshold caps the budget a tier may approve on its own;
// larger increases route up one level (see P3MBudgetApproval).
type P3MAuthorizationThreshold struct {
	ID              int     `json:"id"`
	Level           string  `json:"level"` // portfolio | programme | project
	ThresholdAmount float64 `json:"threshold_amount"`
}

// P3MBudgetApproval is a spend-authorization request routed by the tier
// thresholds. RequestedAmount is the proposed new total budget for the
// project; approval applies it, rejection leaves CurrentBudget in force.
type P3MBudgetApproval struct {
	ID              int        `json:"id"`
	ProjectID       int        `json:"project_id"`
	CurrentBudget   float64    `json:"current_budget"`
	RequestAmount   float64    `json:"requested_amount"`
	RoutedToLevel   string     `json:"routed_to_level"` // project | programme | portfolio
	Status          string     `json:"status"`          // pending | approved | rejected
	RequestedBy     int        `json:"requested_by"`
	DecidedBy       *int       `json:"decided_by,omitempty"`
	DecisionNotes   string     `json:"decision_notes"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// P3MBaseline is an immutable scope/schedule/cost snapshot of a project at an
// approval point. Snapshot is JSON captured at creation and never mutated.
type P3MBaseline struct {
	ID        int            `json:"id"`
	ProjectID int            `json:"project_id"`
	Label     string         `json:"label"`
	Snapshot  map[string]any `json:"snapshot"`
	CreatedBy int            `json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
}

// P3MRaidItem is a polymorphic risk/assumption/issue/dependency entry that
// can be logged at any tier and escalated upward.
type P3MRaidItem struct {
	ID                int        `json:"id"`
	EntityType        string     `json:"entity_type"` // portfolio | programme | project
	EntityID          int        `json:"entity_id"`
	ItemType          string     `json:"item_type"`   // risk | assumption | issue | dependency | decision
	ItemID            *int       `json:"item_id,omitempty"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	ImpactScore       int        `json:"impact_score"`      // 1-5
	ProbabilityScore  int        `json:"probability_score"` // 1-5
	MitigationPlan    string     `json:"mitigation_plan"`
	OwnerID           int        `json:"owner_id"`
	Status            string     `json:"status"` // open | mitigating | closed | escalated

	// RAID due dates + subtypes (RAID-001/006, Appendix B).
	DueDate      *time.Time `json:"due_date,omitempty"`
	ItemSubtype  *string    `json:"item_subtype,omitempty"` // change_risk | adoption_risk
	// Decision register extensions (GOV-007): set when item_type = 'decision'.
	DecisionSubtype *string `json:"decision_subtype,omitempty"` // governance | funding | procurement | plan | closure
	SRMContractID   *int    `json:"srm_contract_id,omitempty"`  // srm_contracts.id for procurement decisions

	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Computed: probability x impact (1-25) and the resulting priority bucket.
	RiskScore        int    `json:"risk_score" db:"-"`
	PriorityBucket   string `json:"priority_bucket" db:"-"` // high | medium | low
	RequiresEscalation bool `json:"requires_escalation" db:"-"` // project-level, score >= 15
}

// P3MStakeholder is a stakeholder in the power/interest matrix for a tier.
type P3MStakeholder struct {
	ID           int        `json:"id"`
	EntityType   string     `json:"entity_type"` // portfolio | programme | project
	EntityID     int        `json:"entity_id"`
	Name         string     `json:"name"`
	RoleTitle    string     `json:"role_title"`
	PowerScore   int        `json:"power_score"`   // 1-5
	InterestScore int       `json:"interest_score"` // 1-5
	Notes        string     `json:"notes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Computed quadrant: manage_closely | keep_satisfied | monitor | keep_informed.
	Quadrant string `json:"quadrant" db:"-"`
}

// P3MCommunication is one entry in the stakeholder communication log.
type P3MCommunication struct {
	ID         int        `json:"id"`
	EntityType string     `json:"entity_type"` // portfolio | programme | project
	EntityID   int        `json:"entity_id"`
	Date       time.Time  `json:"date"`
	Audience   string     `json:"audience"`
	Channel    string     `json:"channel"` // meeting | workshop | consultation | report | email | other
	Summary    string     `json:"summary"`
	CreatedBy  *int       `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// P3MResource is one entry in the global resource inventory with a skill tag
// matrix. UserID optionally links to an internal user account.
type P3MResource struct {
	ID                   int        `json:"id"`
	Name                 string     `json:"name"`
	UserID               *int       `json:"user_id,omitempty"`
	Skills               []string   `json:"skills"`
	CostRate             float64    `json:"cost_rate"` // per hour
	CapacityHoursPerWeek int        `json:"capacity_hours_per_week"`
	Pool                 *string    `json:"pool,omitempty"` // specialist pool, e.g. digital_services | change | general (RES-005)
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// P3MResourceAllocation books a resource against a project for a period.
// Soft bookings are provisional; hard bookings become mandatory on approval.
type P3MResourceAllocation struct {
	ID           int        `json:"id"`
	ResourceID   int        `json:"resource_id"`
	ProjectID    *int       `json:"project_id,omitempty"`
	StartDate    time.Time  `json:"start_date"`
	EndDate      time.Time  `json:"end_date"`
	HoursPerWeek int        `json:"hours_per_week"`
	BookingType  string     `json:"booking_type"` // soft | hard
	// BAU rows count against capacity but not against any project (RES-004).
	AllocationClass string    `json:"allocation_class"` // project | bau
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// P3MBudgetTranche is a funding tranche (CapEx or OpEx) with a planned date,
// used for multi-year cash-flow forecasting.
type P3MBudgetTranche struct {
	ID              int        `json:"id"`
	EntityType      string     `json:"entity_type"` // portfolio | programme | project
	EntityID        int        `json:"entity_id"`
	Title           string     `json:"title"`
	Amount          float64   `json:"amount"`
	ExpenditureType string    `json:"expenditure_type"` // capex | opex
	ExpenditureClass *string  `json:"expenditure_class,omitempty"` // one_off | ongoing (FIN-002)
	PlannedDate     time.Time `json:"planned_date"`
	Status          string     `json:"status"` // proposed | approved | released
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// P3MFinancialCloseOut is the finance sign-off record for a project at its
// close stage (FIN-010): final budget position, outstanding commitments and
// ongoing obligations carried into BAU. Status moves open -> validated by the
// finance reviewer; validation stamps closed_at.
type P3MFinancialCloseOut struct {
	ID                      int        `json:"id"`
	ProjectID               int        `json:"project_id"`
	FinalBudget             float64    `json:"final_budget"`
	ActualToDate            float64    `json:"actual_to_date"`
	OutstandingCommitments  float64    `json:"outstanding_commitments"`
	OngoingObligations      string     `json:"ongoing_obligations"` // free-text BAU obligations carried forward
	FinanceReviewerID       *int       `json:"finance_reviewer_id,omitempty"`
	Status                  string     `json:"status"` // open | validated
	Notes                   string     `json:"notes"`
	ClosedAt                *time.Time `json:"closed_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// P3MForecastPoint is one month of aggregated planned spend.
type P3MForecastPoint struct {
	Month      string  `json:"month"` // YYYY-MM
	CapexTotal float64 `json:"capex_total"`
	OpexTotal  float64 `json:"opex_total"`
}

// P3MResourceCapacityRow is one resource's demand vs capacity for a month.
type P3MResourceCapacityRow struct {
	ResourceID    int     `json:"resource_id"`
	Name          string  `json:"name"`
	Month         string  `json:"month"` // YYYY-MM
	DemandHours   float64 `json:"demand_hours"`
	BauHours      float64 `json:"bau_hours"`
	CapacityHours float64 `json:"capacity_hours"`
	OverAllocated bool    `json:"over_allocated"`
}

// P3M investment case decision pipeline: cases start proposed, move to
// under_review, and end in a terminal status set only by the decision endpoint.
const (
	P3MICStatusProposed    = "proposed"
	P3MICStatusUnderReview = "under_review"
	P3MICStatusApproved    = "approved"
	P3MICStatusRejected    = "rejected"
	P3MICStatusDeferred    = "deferred"
)

// P3MInvestmentCase is a project's investment case: the prioritisation decision
// pipeline plus risk/benefit/return scores used to balance portfolios on timing,
// strategic coverage, benefit, risk and return. One per project; it travels with
// the project through its lifecycle.
type P3MInvestmentCase struct {
	ID            int        `json:"id"`
	ProjectID     int        `json:"project_id"`
	Status        string     `json:"status"` // proposed | under_review | approved | rejected | deferred
	BenefitScore  int        `json:"benefit_score"` // 1-5
	RiskScore     int        `json:"risk_score"`    // 1-5
	ROIPercent    *float64   `json:"roi_percent,omitempty"`
	PaybackMonths *int       `json:"payback_months,omitempty"`
	BusinessCase  string     `json:"business_case"`
	PageID        *int       `json:"page_id,omitempty"`
	DecidedBy     *int       `json:"decided_by,omitempty"`
	DecisionDate  *time.Time `json:"decision_date,omitempty"` // "YYYY-MM-DD" or null
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Display fields joined in by list responses.
	ProjectTitle   string `json:"project_title"`
	PortfolioID    *int   `json:"portfolio_id,omitempty"` // project may sit under a programme only
	PortfolioTitle string `json:"portfolio_title,omitempty"`
	DecidedByName  string `json:"decided_by_name,omitempty"`
}

// P3MPipelineCounts is the per-portfolio investment-case pipeline rollup in the
// executive summary: how many cases sit at each decision stage.
type P3MPipelineCounts struct {
	Proposed    int `json:"proposed"`
	UnderReview int `json:"under_review"`
	Approved    int `json:"approved"`
	Rejected    int `json:"rejected"`
	Deferred    int `json:"deferred"`
}

// P3MExecutiveSummary is the auto-generated one-page status rollup for a
// portfolio: RAG, budget health, upcoming gates, and top open risks.
type P3MExecutiveSummary struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Portfolios  []P3MPortfolioSummary   `json:"portfolios"`
}

type P3MPortfolioSummary struct {
	PortfolioID     int                `json:"portfolio_id"`
	Title           string             `json:"title"`
	RagStatus       string             `json:"rag_status"`
	BudgetAllocated float64            `json:"budget_allocated"`
	BudgetApproved  float64            `json:"budget_approved"`
	BudgetActual    float64            `json:"budget_actual"`
	UpcomingGates   []P3MUpcomingGate  `json:"upcoming_gates"`
	TopRisks        []P3MRaidItem      `json:"top_risks"`
	InvestmentPipeline P3MPipelineCounts `json:"investment_pipeline"`
}

type P3MUpcomingGate struct {
	ProjectID int    `json:"project_id"`
	Title     string `json:"title"`
	Stage     string `json:"stage"` // current stage awaiting its next gate
}

// P3MCriticalPathTask is one dated work item in a CPM schedule analysis.
// Dates are whole days (UTC midnight); *_finish fields are inclusive last
// working days, so duration_days = finish - start + 1 for the planned window.
type P3MCriticalPathTask struct {
	ItemID         int       `json:"item_id"`
	Title          string    `json:"title"`
	WorkspaceID    int       `json:"workspace_id"`
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	DurationDays   float64   `json:"duration_days"`
	EarliestStart  time.Time `json:"earliest_start"`
	EarliestFinish time.Time `json:"earliest_finish"`
	LatestStart    time.Time `json:"latest_start"`
	LatestFinish   time.Time `json:"latest_finish"`
	TotalFloatDays float64   `json:"total_float_days"`
	IsCritical     bool      `json:"is_critical"`

	// Register exposure: open risks and pending decisions linked to this work
	// item via p3m_raid_items.item_id (populated by the CPM endpoints).
	OpenRisks        int `json:"open_risks"`
	PendingDecisions int `json:"pending_decisions"`
}

// P3MCriticalPath is the CPM result for a project or programme: every dated
// work item in its linked workspaces, scheduled with forward and backward
// passes over "blocks" dependencies. A task is critical when its total float
// is zero; critical_task_ids lists the critical tasks in topological order so
// they read as the path that determines the entity's completion date.
type P3MCriticalPath struct {
	GeneratedAt        time.Time               `json:"generated_at"`
	EntityType         string                  `json:"entity_type"` // project | programme
	EntityID           int                     `json:"entity_id"`
	Title              string                  `json:"title"`
	ItemCount          int                     `json:"item_count"`
	DependencyCount    int                     `json:"dependency_count"`
	ProjectStart       *time.Time              `json:"project_start,omitempty"`
	ProjectEnd         *time.Time              `json:"project_end,omitempty"`
	CriticalPathDays   float64                 `json:"critical_path_days"`
	Tasks              []P3MCriticalPathTask   `json:"tasks"`
	CriticalTaskIDs    []int                   `json:"critical_task_ids"`
}

// P3MCPMCycleError is returned by the CPM scheduler when "blocks" dependencies
// form a cycle. ItemIDs holds one concrete cycle in edge order, starting from
// its lowest item id.
type P3MCPMCycleError struct {
	ItemIDs []int `json:"item_ids"`
}

func (e *P3MCPMCycleError) Error() string {
	return "dependency cycle detected among work items"
}

// P3MArchitectureReview is one Architecture Review Board entry for a project.
// Status follows the ARB lifecycle; an approved review must reference its
// architecture page, and a needs_work decision stores the id of the
// decision-register entry written on rejection.
type P3MArchitectureReview struct {
	ID                 int        `json:"id"`
	ProjectID          int        `json:"project_id"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Status             string     `json:"status"` // wip | in_review | needs_work | approved
	ReviewerID         *int       `json:"reviewer_id,omitempty"`
	DecidedAt          *time.Time `json:"decided_at,omitempty"`
	Comments           string     `json:"comments"`
	ArchitecturePageID *int       `json:"architecture_page_id,omitempty"`
	DecisionItemID     *int       `json:"decision_item_id,omitempty"`
	CreatedBy          int        `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// P3MLeavePeriodRef is one active leave period overlapping a task or resource
// allocation window, with the substitute's name when one was named.
type P3MLeavePeriodRef struct {
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	SubstituteName string    `json:"substitute_name,omitempty"`
}

// P3MLeaveResourceRow is a user-backed resource allocation whose window
// overlaps active leave; LostHours is the allocation's weekly hours scaled by
// the overlap fraction of the week.
type P3MLeaveResourceRow struct {
	ResourceID          int                 `json:"resource_id"`
	Name                string              `json:"name"`
	UserID              *int                `json:"user_id,omitempty"`
	UserName            string              `json:"user_name,omitempty"`
	AllocationStart     time.Time           `json:"allocation_start"`
	AllocationEnd       time.Time           `json:"allocation_end"`
	HoursPerWeek        int                 `json:"hours_per_week"`
	LeavePeriods        []P3MLeavePeriodRef `json:"leave_periods"`
	OverlapDays         float64             `json:"overlap_days"`
	LostHours           float64             `json:"lost_hours"` // hours_per_week * overlap_days / 7
	CoveredBySubstitute bool                `json:"covered_by_substitute"`
}

// P3MLeaveTaskRow is a CPM task whose assignee has active leave overlapping the
// task's planned window.
type P3MLeaveTaskRow struct {
	ItemID       int                 `json:"item_id"`
	Title        string              `json:"title"`
	StartDate    time.Time           `json:"start_date"` // CPM planned window (inclusive)
	EndDate      time.Time           `json:"end_date"`
	IsCritical   bool                `json:"is_critical"`
	AssigneeID   *int                `json:"assignee_id,omitempty"`
	AssigneeName string              `json:"assignee_name,omitempty"`
	LeavePeriods []P3MLeavePeriodRef `json:"leave_periods"`
	OverlapDays  float64             `json:"overlap_days"`
}

// P3MLeaveImpact is the leave-impact analysis for a project or programme: which
// resource allocations and critical-path tasks are hit by active user leaves.
type P3MLeaveImpact struct {
	GeneratedAt time.Time             `json:"generated_at"`
	EntityType  string                `json:"entity_type"` // project | programme
	EntityID    int                   `json:"entity_id"`
	Title       string                `json:"title"`
	Resources   []P3MLeaveResourceRow `json:"resources"`
	Tasks       []P3MLeaveTaskRow     `json:"tasks"`
}

// P3MIntake is a Front Door intake record (FD-001..005, FD-008): the single
// entry point for new work into the P3M lifecycle. A "proceed" decision
// creates/links a project pre-filled from this record so the idea is captured
// once and never re-entered downstream (FD-010).
type P3MIntake struct {
	ID                   int        `json:"id"`
	Title                string     `json:"title"`
	Source               string     `json:"source"` // standard | exceptional_path (FD-011 retrospective registration)
	Description          string     `json:"description"`
	ProblemOpportunity   string     `json:"problem_opportunity"`
	StrategicAlignment   string     `json:"strategic_alignment"`
	IntendedOutcomes     string     `json:"intended_outcomes"`
	IndicativeCost       *float64   `json:"indicative_cost,omitempty"`
	ResourceRequirements string     `json:"resource_requirements"`
	RiskNotes            string     `json:"risk_notes"`
	ChangeImpactNotes    string     `json:"change_impact_notes"`
	BusinessGroup        string     `json:"business_group"`
	WorkType             *string    `json:"work_type,omitempty"` // programme | project | continuous_improvement | digital_product | operational
	Complexity           string     `json:"complexity"`          // low | med | high (FD-005)
	Status               string     `json:"status"`              // captured | assessing | decided
	Decision             *string    `json:"decision,omitempty"`  // proceed | redirect | defer | decline | return_info (FD-008)
	DecidedBy            *int       `json:"decided_by,omitempty"`
	DecisionDate         *time.Time `json:"decision_date,omitempty"`
	Conditions           string     `json:"conditions"` // decision conditions; copied to the first gate on proceed
	Rationale            string     `json:"rationale"`  // mandatory with every decision
	TargetProjectID      *int       `json:"target_project_id,omitempty"`
	LifecyclePosition    *string    `json:"lifecycle_position,omitempty"` // FD-011: where an exceptional-path item sits in the lifecycle
	CreatedBy            *int       `json:"created_by,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// P3MFrontDoorScreen is a per-complexity required-field rule for Front Door
// assessment (FD-003/005): every listed field must be completed before an
// intake of that complexity can be decided. Admin-configurable.
type P3MFrontDoorScreen struct {
	ID         int    `json:"id"`
	Complexity string `json:"complexity"` // low | med | high
	FieldKey   string `json:"field_key"`
	Required   bool   `json:"required"`
}

// P3MFinanceSyncConfig is a scheduled source of financial data from the
// external finance system (FIN-007). Typically one row per deployment;
// multiple rows are allowed.
type P3MFinanceSyncConfig struct {
	ID              int        `json:"id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	SourceType      string     `json:"source_type"` // file_path | s3
	SourceRef       string     `json:"source_ref"`  // absolute path (allowlisted) or s3://bucket/key
	ScheduleMinutes int        `json:"schedule_minutes"`
	PeriodFormat    string     `json:"period_format"`
	DefaultCurrency string     `json:"default_currency"`
	MatchMode       string     `json:"match_mode"` // ref_only | ref_then_title
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	LastRunStatus   *string    `json:"last_run_status,omitempty"` // success | partial | failed | skipped
	LastFileHash    *string    `json:"last_file_hash,omitempty"`
	CreatedBy       *int       `json:"created_by,omitempty"`
	UpdatedBy       *int       `json:"updated_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// P3MFinanceActual is one immutable fact row imported from the finance system.
// Runs are append-only; the effective value for a (project, period, line_type)
// key comes from the latest run that matched it. No row is ever deleted.
type P3MFinanceActual struct {
	ID          int        `json:"id"`
	ConfigID    int        `json:"config_id"`
	RunID       *int       `json:"run_id,omitempty"`
	ProjectID   *int       `json:"project_id,omitempty"`
	ExternalRef string     `json:"external_ref"`
	CostCenter  *string    `json:"cost_center,omitempty"`
	WorkOrder   *string    `json:"work_order,omitempty"`
	Period      string     `json:"period"` // YYYY-MM (quarters expanded to months)
	LineType    string     `json:"line_type"`
	Amount      float64    `json:"amount"`
	Currency    string     `json:"currency"`
	Description *string    `json:"description,omitempty"`
	MatchStatus string     `json:"match_status"` // matched | unmatched | rejected
	RawRow      *string    `json:"raw_row,omitempty"`
	ImportedAt  time.Time  `json:"imported_at"`

	// Candidates is populated only in the unmatched review queue: projects
	// whose normalised title matches this row.
	Candidates []P3MFinanceMatchCandidate `json:"candidates,omitempty" db:"-"`
}

// P3MFinanceMatchCandidate is a project suggested for an unmatched fact row.
type P3MFinanceMatchCandidate struct {
	ID    int     `json:"id"`
	Title string  `json:"title"`
	Ref   *string `json:"external_ref,omitempty"`
}

// P3MFinanceSyncRun is the business-level record of one sync execution;
// tick-level health is recorded separately in scheduler_runs.
type P3MFinanceSyncRun struct {
	ID            int        `json:"id"`
	ConfigID      int        `json:"config_id"`
	Trigger       string     `json:"trigger"` // schedule | manual | upload
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Status        string     `json:"status"` // running | success | partial | failed
	FileHash      *string    `json:"file_hash,omitempty"`
	RowsTotal     int        `json:"rows_total"`
	RowsMatched   int        `json:"rows_matched"`
	RowsUnmatched int        `json:"rows_unmatched"`
	RowsFailed    int        `json:"rows_failed"`
	ErrorSummary  *string    `json:"error_summary,omitempty"` // JSON [{row, reason}], last 200 entries
}

// P3MFinanceSyncRejection is an operator dismissal of a (ref, period,
// line_type) row from the review queue; later runs store such rows as
// rejected instead of re-queueing them.
type P3MFinanceSyncRejection struct {
	ID          int       `json:"id"`
	ExternalRef string    `json:"external_ref"`
	Period      string    `json:"period"`
	LineType    string    `json:"line_type"`
	Reason      string    `json:"reason"`
	RejectedBy  int       `json:"rejected_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// --- Phase 2 gap-closure models (BRD OHP F6-F15) ---------------------------

// P3MChangeCalendarEntry is one row of the organisation-wide change calendar
// (CHG-008/009). Weeks are ISO week strings such as "2026-W45".
type P3MChangeCalendarEntry struct {
	ID           int       `json:"id"`
	EntityType   *string   `json:"entity_type,omitempty"` // project | programme | portfolio; nil = standalone entry
	EntityID     *int      `json:"entity_id,omitempty"`
	Activity     string    `json:"activity"`
	BusinessUnit *string   `json:"business_unit,omitempty"`
	StartWeek    string    `json:"start_week"`
	EndWeek      string    `json:"end_week"`
	ImpactLevel  int       `json:"impact_level"` // 1 (low) .. 5 (high)
	Source       string    `json:"source"`       // corporate_project | corporate_activity | bau | local
	Notes        *string   `json:"notes,omitempty"`
	CreatedBy    *int      `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// P3MChangeAssessment is the single active change impact assessment for an
// entity (CHG-002/003); SupportLevel carries the CHG-003 triage outcome.
type P3MChangeAssessment struct {
	ID                     int       `json:"id"`
	EntityType             string    `json:"entity_type"` // project | programme
	EntityID               int       `json:"entity_id"`
	ImpactedGroups         *string   `json:"impacted_groups,omitempty"`          // JSON [string]
	ImpactByCharacteristic *string   `json:"impact_by_characteristic,omitempty"` // JSON [{characteristic, nature, severity}]
	OverallRating          int       `json:"overall_rating"`                     // 1..5
	SupportLevel           string    `json:"support_level"`                      // none | light | full
	Approach               *string   `json:"approach,omitempty"`
	Deliverables           *string   `json:"deliverables,omitempty"`             // JSON [string]
	CreatedBy              *int      `json:"created_by,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// P3MReadinessAssessment is one readiness assessment (CHG-006); the latest
// per entity gates entry to implement_deliver.
type P3MReadinessAssessment struct {
	ID             int       `json:"id"`
	EntityType     string    `json:"entity_type"` // project | programme
	EntityID       int       `json:"entity_id"`
	Questions      string    `json:"questions"`   // JSON [{id, text, required}]
	Answers        *string   `json:"answers,omitempty"` // JSON [{question_id, answer: yes|no, gap_action}]
	AssessedAt     time.Time `json:"assessed_at"`
	Outcome        string    `json:"outcome"`     // go | no_go | reassess
	ReassessmentOf *int      `json:"reassessment_of,omitempty"`
	CreatedBy      *int      `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// P3MBenefitMeasurement is one scheduled or completed measurement point for a
// benefit (BEN-007); the scheduler reminds on DueDate while incomplete.
type P3MBenefitMeasurement struct {
	ID          int        `json:"id"`
	BenefitID   int        `json:"benefit_id"`
	DueDate     string     `json:"due_date"` // YYYY-MM-DD
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Value       *float64   `json:"value,omitempty"`
	Evidence    *string    `json:"evidence,omitempty"`
	RecordedBy  *int       `json:"recorded_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// P3MReportSnapshot is one point-in-time RAG report capture (RPT-005/006);
// Metrics and SuggestedCategoryRags are computed server-side at capture.
type P3MReportSnapshot struct {
	ID                    int       `json:"id"`
	EntityType            string    `json:"entity_type"` // project | programme | portfolio
	EntityID              int       `json:"entity_id"`
	Period                string    `json:"period"`      // e.g. "2026-11" or a date
	OverallRAG            string    `json:"overall_rag"` // green | amber | red
	CategoryRags          *string   `json:"category_rags,omitempty"`           // JSON {schedule,budget,scope,risk,benefits,change}
	SuggestedCategoryRags *string   `json:"suggested_category_rags,omitempty"` // tolerance-computed (RPT-007)
	RAGOverrideReasons    *string   `json:"rag_override_reasons,omitempty"`    // JSON {category: reason} where manual differs from suggested
	Commentary            *string   `json:"commentary,omitempty"`
	Escalations           *string   `json:"escalations,omitempty"` // JSON [string]
	Metrics               *string   `json:"metrics,omitempty"`     // auto-captured at capture time
	CapturedBy            *int      `json:"captured_by,omitempty"`
	CapturedAt            time.Time `json:"captured_at"`
}

// P3MToleranceProfile defines the RAG tolerances for a portfolio or project
// (RPT-007); EntityID is the portfolio or project id per Scope.
type P3MToleranceProfile struct {
	ID           int       `json:"id"`
	Scope        string    `json:"scope"` // portfolio | project
	EntityID     int       `json:"entity_id"`
	ScheduleDays int       `json:"schedule_days"`
	BudgetPct    float64   `json:"budget_pct"`
	RiskScore    int       `json:"risk_score"` // 1..5: amber at this score or above
	UpdatedAt    time.Time `json:"updated_at"`
}

// P3MScoringCriterion is one portfolio prioritisation criterion (PO-003).
type P3MScoringCriterion struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Weight      float64   `json:"weight"` // >= 0; composite = sum(weight*score)/sum(weight)
	Direction   string    `json:"direction"` // higher_better | lower_better
	Description *string   `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// P3MInvestmentCaseScore is one 1-5 score of an investment case against a
// criterion (PO-003); changes are audit-emitted for PO-008 history.
type P3MInvestmentCaseScore struct {
	ID          int       `json:"id"`
	ICID        int       `json:"ic_id"`
	CriterionID int       `json:"criterion_id"`
	Score       float64   `json:"score"` // 1..5
	Rationale   *string   `json:"rationale,omitempty"`
	ScoredBy    *int      `json:"scored_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// P3MFinanceAlertRule is one financial exception rule (FIN-009) evaluated on
// each finance sync run and the nightly tick.
type P3MFinanceAlertRule struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Scope       string    `json:"scope"` // portfolio | project
	PortfolioID *int      `json:"portfolio_id,omitempty"`
	ProjectID   *int      `json:"project_id,omitempty"`
	Metric      string    `json:"metric"`     // forecast_overrun_pct | variance_vs_budget | commitment_vs_remaining
	Comparator  string    `json:"comparator"` // > | < | >= | <=
	Threshold   float64   `json:"threshold"`
	NotifyEmail *string   `json:"notify_email,omitempty"` // comma list; empty = no email
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// P3MClosureRecord is the closure & handover record for a project (CLS-001);
// OutstandingItems auto-populates from open RAID items at capture.
type P3MClosureRecord struct {
	ID                  int        `json:"id"`
	ProjectID           int        `json:"project_id"`
	ClosureReportPageID *string    `json:"closure_report_page_id,omitempty"`
	AcceptanceSignoffBy *int       `json:"acceptance_signoff_by,omitempty"`
	AcceptanceSignoffAt *time.Time `json:"acceptance_signoff_at,omitempty"`
	OutstandingItems    *string    `json:"outstanding_items,omitempty"` // JSON [string]
	LessonsLearned      *string    `json:"lessons_learned,omitempty"`
	CostOwnership       *string    `json:"cost_ownership,omitempty"` // owner ref + ongoing cost ref
	HandoverTo          *string    `json:"handover_to,omitempty"`   // user/team ref
	Status              string     `json:"status"`                  // recommended | approved
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
