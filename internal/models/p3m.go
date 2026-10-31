package models

import "time"

// P3M (Portfolio, Programme, Project Management) domain models. The module is
// gated by the global permission PermissionP3MView and spans three tiers of
// control plus seven process perspectives: governance, management control,
// benefits, financials, risk/RAID, stakeholder engagement, and resource
// management.

// P3M stage lifecycle shared by projects and stage-gate reviews.
const (
	P3MStageInitiation = "initiation"
	P3MStagePlanning   = "planning"
	P3MStageExecution  = "execution"
	P3MStageClosure    = "closure"
)

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
	CreatedAt           time.Time  `json:"created_at"`

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
	ItemType          string     `json:"item_type"`   // risk | assumption | issue | dependency
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	ImpactScore       int        `json:"impact_score"`      // 1-5
	ProbabilityScore  int        `json:"probability_score"` // 1-5
	MitigationPlan    string     `json:"mitigation_plan"`
	OwnerID           int        `json:"owner_id"`
	Status            string     `json:"status"` // open | mitigating | closed | escalated
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
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// P3MBudgetTranche is a funding tranche (CapEx or OpEx) with a planned date,
// used for multi-year cash-flow forecasting.
type P3MBudgetTranche struct {
	ID              int        `json:"id"`
	EntityType      string     `json:"entity_type"` // portfolio | programme | project
	EntityID        int        `json:"entity_id"`
	Title           string     `json:"title"`
	Amount          float64    `json:"amount"`
	ExpenditureType string     `json:"expenditure_type"` // capex | opex
	PlannedDate     time.Time  `json:"planned_date"`
	Status          string     `json:"status"` // proposed | approved | released
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// P3MForecastPoint is one month of aggregated planned spend.
type P3MForecastPoint struct {
	Month      string  `json:"month"` // YYYY-MM
	CapexTotal float64 `json:"capex_total"`
	OpexTotal  float64 `json:"opex_total"`
}

// P3MResourceCapacityRow is one resource's demand vs capacity for a month.
type P3MResourceCapacityRow struct {
	ResourceID   int     `json:"resource_id"`
	Name         string  `json:"name"`
	Month        string  `json:"month"` // YYYY-MM
	DemandHours  float64 `json:"demand_hours"`
	CapacityHours float64 `json:"capacity_hours"`
	OverAllocated bool   `json:"over_allocated"`
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
}

type P3MUpcomingGate struct {
	ProjectID int    `json:"project_id"`
	Title     string `json:"title"`
	Stage     string `json:"stage"` // current stage awaiting its next gate
}
