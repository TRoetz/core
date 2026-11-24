package models

import "time"

// --- OHP portfolio report registers (RPT-001..RPT-009) ---------------------
//
// These are the row-level models for the report registers added by
// migration 20261124_p3m_portfolio_report. The report-document structs at
// the bottom of this file are the assembled GET /p3m/portfolios/{id}/
// portfolio-report response shapes.

// P3MRagHistory is one row of the RAG status history for a portfolio,
// programme or project: one row per change, used to render the trend
// arrows on the portfolio rollup.
type P3MRagHistory struct {
	ID         int       `json:"id"`
	EntityType string    `json:"entity_type"` // portfolio | programme | project
	EntityID   int       `json:"entity_id"`
	RagStatus  string    `json:"rag_status"` // red | amber | green
	ChangedAt  time.Time `json:"changed_at"`
	ChangedBy  *int      `json:"changed_by,omitempty"`
}

// P3MProjectStatusReport is one monthly project status report
// (issue / impact / next steps).
type P3MProjectStatusReport struct {
	ID         int       `json:"id"`
	ProjectID  int       `json:"project_id"`
	Period     string    `json:"period"` // YYYY-MM
	Issue      string    `json:"issue"`
	Impact     string    `json:"impact"`
	NextSteps  string    `json:"next_steps"`
	ReportedBy *int      `json:"reported_by,omitempty"`
	ReportedAt time.Time `json:"reported_at"`
}

// P3MChangeCalendarGroupImpact is the per-business-group impact of one
// change calendar entry.
type P3MChangeCalendarGroupImpact struct {
	ID            int    `json:"id"`
	EntryID       int    `json:"entry_id"`
	BusinessGroup string `json:"business_group"`
	ImpactLevel   int    `json:"impact_level"` // 0 (unknown) .. 5 (severe)
}

// P3MChangeCapacity is one business-group × month capacity posture cell.
type P3MChangeCapacity struct {
	ID            int     `json:"id"`
	BusinessGroup string  `json:"business_group"`
	Month         string  `json:"month"`   // YYYY-MM
	Posture       string  `json:"posture"` // proceed | caution | pause | reduce
	Notes         *string `json:"notes,omitempty"`
}

// P3MELTDecision is one Exec/L&T decision coversheet (ELT-001/002/003).
type P3MELTDecision struct {
	ID                      int        `json:"id"`
	PortfolioID             int        `json:"portfolio_id"`
	ProjectID               *int       `json:"project_id,omitempty"`
	Title                   string     `json:"title"`
	Why                     string     `json:"why"`
	DecisionRequested       string     `json:"decision_requested"`
	ConditionsUntilApproved string     `json:"conditions_until_approved"`
	Status                  string     `json:"status"` // pending | approved | rejected | withdrawn
	DecidedBy               *int       `json:"decided_by,omitempty"`
	DecidedAt               *time.Time `json:"decided_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// P3MPortfolioReport is the curated monthly portfolio report row: exec
// commentary, key pressures, delivery confidence, lenses, programme
// highlights and change key messages. KeyPressures is a JSON object with the
// keys delivery_outlook / amber_red_projects / business_readiness_change;
// ConfidenceLenses is a JSON array of {key, position, rag};
// ProgrammeHighlights is a JSON array of {programme_id, commentary}.
type P3MPortfolioReport struct {
	ID                    int        `json:"id"`
	PortfolioID           int        `json:"portfolio_id"`
	Period                string     `json:"period"` // YYYY-MM
	ExecutiveCommentary   string     `json:"executive_commentary"`
	KeyPressures          *string    `json:"key_pressures,omitempty"`
	DeliveryConfidencePct *float64   `json:"delivery_confidence_pct,omitempty"`
	ConfidenceLenses      *string    `json:"confidence_lenses,omitempty"`
	ProgrammeHighlights   *string    `json:"programme_highlights,omitempty"`
	ChangeKeyMessages     string     `json:"change_key_messages"`
	PublishedBy           *int       `json:"published_by,omitempty"`
	PublishedAt           *time.Time `json:"published_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// P3MReferenceValue is one curated free-text value for a report dimension
// category (business_group | theme | cso | benefit_category).
type P3MReferenceValue struct {
	ID       int    `json:"id"`
	Category string `json:"category"`
	Value    string `json:"value"`
}

// --- Assembled portfolio report document ------------------------------------

// P3MPortfolioReportDocument is the full GET /p3m/portfolios/{id}/
// portfolio-report response. Empty collections marshal as []; curated-only
// fields marshal as null when the curated row is absent.
type P3MPortfolioReportDocument struct {
	Portfolio       P3MReportPortfolioRef         `json:"portfolio"`
	AsOf            string                        `json:"as_of"`
	GeneratedAt     time.Time                     `json:"generated_at"`
	Position        P3MReportPosition             `json:"position"`
	Highlights      []P3MReportProgrammeHighlight `json:"highlights"`
	Risks           []P3MReportRisk               `json:"risks"`
	Benefits        P3MReportBenefits             `json:"benefits"`
	Dashboard       P3MReportDashboard            `json:"dashboard"`
	AtRisk          []P3MReportAtRiskProject      `json:"at_risk"`
	ChangeDashboard P3MReportChangeDashboard      `json:"change_dashboard"`
	ChangeCalendar  P3MReportChangeCalendar       `json:"change_calendar"`
	Ideate          []P3MReportIdeateRow          `json:"ideate"`
	Proposals       []P3MReportProposalRow        `json:"proposals"`
	Investments     []P3MReportInvestment         `json:"investments"`
	Closed          []P3MReportClosedRow          `json:"closed"`
	ELTDecisions    []P3MReportELTDecision        `json:"elt_decisions"`
}

type P3MReportPortfolioRef struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type P3MReportPosition struct {
	Counts               P3MReportPositionCounts   `json:"counts"`
	KeyPressures         *P3MReportKeyPressures    `json:"key_pressures"`
	ELTDecisionsRequired []P3MReportELTDecisionRef `json:"elt_decisions_required"`
}

type P3MReportPositionCounts struct {
	Programmes    int            `json:"programmes"`
	Projects      int            `json:"projects"`
	ApprovedTotal int            `json:"approved_total"`
	ByStage       map[string]int `json:"by_stage"`
	OnHold        int            `json:"on_hold"`
	ByDisplayRag  map[string]int `json:"by_display_rag"`
}

type P3MReportKeyPressures struct {
	DeliveryOutlook         string `json:"delivery_outlook"`
	AmberRedProjects        string `json:"amber_red_projects"`
	BusinessReadinessChange string `json:"business_readiness_change"`
}

type P3MReportELTDecisionRef struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	ProjectRef   *string `json:"project_ref,omitempty"`
	ProjectTitle *string `json:"project_title,omitempty"`
}

type P3MReportProgrammeHighlight struct {
	ProgrammeID     int             `json:"programme_id"`
	Title           string          `json:"title"`
	Rag             string          `json:"rag"`
	ProgressPercent int             `json:"progress_percent"`
	Milestones      P3MReportCounts `json:"milestones"`
	Tasks           P3MReportCounts `json:"tasks"`
	Commentary      string          `json:"commentary"`
}

// P3MReportCounts is a done/total pair.
type P3MReportCounts struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type P3MReportRisk struct {
	ID             int     `json:"id"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	MitigationPlan *string `json:"mitigation_plan,omitempty"`
	RiskScore      float64 `json:"risk_score"`
}

type P3MReportBenefits struct {
	Total                   int                        `json:"total"`
	InvestmentCount         int                        `json:"investment_count"`
	ByType                  map[string]int             `json:"by_type"`
	ByCSO                   []P3MReportBenefitCSORow   `json:"by_cso"`
	ByOwnerGroupAndCategory []P3MReportBenefitOwnerRow `json:"by_owner_group_and_category"`
}

type P3MReportBenefitCSORow struct {
	CSO       string `json:"cso"`
	Total     int    `json:"total"`
	Planning  int    `json:"planning"`
	Implement int    `json:"implement"`
	Closed    int    `json:"closed"`
}

type P3MReportBenefitOwnerRow struct {
	OwnerGroup string         `json:"owner_group"`
	Categories map[string]int `json:"categories"`
	Total      int            `json:"total"`
}

type P3MReportDashboard struct {
	ExecutiveCommentary *string                       `json:"executive_commentary"`
	DeliveryConfidence  P3MReportDeliveryConfidence   `json:"delivery_confidence"`
	ConfidenceLenses    []P3MReportConfidenceLens     `json:"confidence_lenses"`
	ActiveByRag         map[string]int                `json:"active_by_rag"`
	PipelineByTheme     []P3MReportThemeRow           `json:"pipeline_by_theme"`
	ChangeCapacity      []P3MReportCapacityRow        `json:"change_capacity"`
	BenefitsByCategory  []P3MReportBenefitCategoryRow `json:"benefits_by_category"`
	EmergingDemand      []P3MReportEmergingDemandRow  `json:"emerging_demand"`
}

type P3MReportDeliveryConfidence struct {
	Current *float64  `json:"current"`
	Trend   []float64 `json:"trend"`
}

type P3MReportConfidenceLens struct {
	Key      string `json:"key"`
	Position string `json:"position"`
	Rag      string `json:"rag"`
}

type P3MReportThemeRow struct {
	Theme       string `json:"theme"`
	Planning    int    `json:"planning"`
	AwaitingELT int    `json:"awaiting_elt"`
	Active      int    `json:"active"`
	Closed      int    `json:"closed"`
	Total       int    `json:"total"`
}

// P3MReportCapacityRow is one business group with its posture per window
// month.
type P3MReportCapacityRow struct {
	Group    string         `json:"group"`
	Postures map[string]any `json:"postures"`
}

type P3MReportBenefitCategoryRow struct {
	Category     string `json:"category"`
	Financial    int    `json:"financial"`
	NonFinancial int    `json:"non_financial"`
	Unspecified  int    `json:"unspecified"`
	Total        int    `json:"total"`
}

type P3MReportEmergingDemandRow struct {
	Group    string `json:"group"`
	Ideate   int    `json:"ideate"`
	Proposal int    `json:"proposal"`
}

type P3MReportAtRiskProject struct {
	ID                     int                        `json:"id"`
	Title                  string                     `json:"title"`
	DisplayRag             string                     `json:"display_rag"`
	DurationInStatusMonths int                        `json:"duration_in_status_months"`
	TolerancesAtRisk       []P3MReportToleranceAtRisk `json:"tolerances_at_risk"`
	BusinessOwnerGroup     *string                    `json:"business_owner_group,omitempty"`
	ResponsibleGroup       *string                    `json:"responsible_group,omitempty"`
	StatusReport           *P3MReportStatusReport     `json:"status_report"`
}

type P3MReportToleranceAtRisk struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Rag      string `json:"rag"`
}

type P3MReportStatusReport struct {
	Period    string `json:"period"`
	Issue     string `json:"issue"`
	Impact    string `json:"impact"`
	NextSteps string `json:"next_steps"`
}

type P3MReportChangeDashboard struct {
	KnownInitiatives int                        `json:"known_initiatives"`
	KeyMessages      string                     `json:"key_messages"`
	Months           []string                   `json:"months"`
	ActiveByMonth    []P3MReportChangeMonthRow  `json:"active_by_month"`
	MediumHigh       []P3MReportMediumHighEntry `json:"medium_high"`
}

type P3MReportChangeMonthRow struct {
	Month    string         `json:"month"`
	Active   int            `json:"active"`
	ByImpact map[string]int `json:"by_impact"`
}

type P3MReportMediumHighEntry struct {
	EntryID  int                        `json:"entry_id"`
	Activity string                     `json:"activity"`
	Groups   []P3MReportMediumHighGroup `json:"groups"`
}

type P3MReportMediumHighGroup struct {
	Group       string   `json:"group"`
	ImpactLevel int      `json:"impact_level"`
	Months      []string `json:"months"`
}

type P3MReportChangeCalendar struct {
	Months  []string                        `json:"months"`
	Dated   []P3MReportCalendarDatedEntry   `json:"dated"`
	Undated []P3MReportCalendarUndatedEntry `json:"undated"`
}

type P3MReportCalendarGroupImpact struct {
	BusinessGroup string `json:"business_group"`
	ImpactLevel   int    `json:"impact_level"`
}

type P3MReportCalendarEntity struct {
	Type  string `json:"type"`
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type P3MReportCalendarDatedEntry struct {
	Activity     string                         `json:"activity"`
	Category     *string                        `json:"category,omitempty"`
	GroupImpacts []P3MReportCalendarGroupImpact `json:"group_impacts"`
	Months       []string                       `json:"months"`
	Entity       *P3MReportCalendarEntity       `json:"entity"`
}

type P3MReportCalendarUndatedEntry struct {
	Activity     string                         `json:"activity"`
	Category     *string                        `json:"category,omitempty"`
	GroupImpacts []P3MReportCalendarGroupImpact `json:"group_impacts"`
}

type P3MReportIdeateRow struct {
	Title              string  `json:"title"`
	Ref                string  `json:"ref"`
	BusinessGroup      string  `json:"business_group"`
	ResponsibleGroup   *string `json:"responsible_group,omitempty"`
	FrontDoorCompleted bool    `json:"front_door_completed"`
}

type P3MReportProposalRow struct {
	Title              string  `json:"title"`
	Ref                *string `json:"ref,omitempty"`
	BusinessOwnerGroup *string `json:"business_owner_group,omitempty"`
	ResponsibleGroup   *string `json:"responsible_group,omitempty"`
	Complexity         *string `json:"complexity,omitempty"`
	Priority           *string `json:"priority,omitempty"`
}

// P3MReportInvestment is one row of the investment table: a non-exit-stage
// project or programme of the portfolio.
type P3MReportInvestment struct {
	Kind               string  `json:"kind"` // project | programme
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Ref                *string `json:"ref,omitempty"`
	ResponsibleGroup   *string `json:"responsible_group,omitempty"`
	BusinessOwnerGroup *string `json:"business_owner_group,omitempty"`
	Complexity         *string `json:"complexity,omitempty"`
	Phase              string  `json:"phase"`
	DisplayRag         string  `json:"display_rag"`
	OnHold             bool    `json:"on_hold"`
	ExpectedEnd        *string `json:"expected_end,omitempty"`
	InvestmentType     *string `json:"investment_type,omitempty"`
	Theme              *string `json:"theme,omitempty"`
	IsCEKPI            bool    `json:"is_ce_kpi"`
	RagTrend           *string `json:"rag_trend,omitempty"` // improved | worsened | unchanged
}

type P3MReportClosedRow struct {
	Kind               string  `json:"kind"` // project | programme
	Name               string  `json:"name"`
	Ref                *string `json:"ref,omitempty"`
	ResponsibleGroup   *string `json:"responsible_group,omitempty"`
	BusinessOwnerGroup *string `json:"business_owner_group,omitempty"`
	EndReportReceived  bool    `json:"end_report_received"`
}

// P3MReportELTDecision is the coversheet row in the assembled report.
type P3MReportELTDecision struct {
	ID                      int     `json:"id"`
	Title                   string  `json:"title"`
	ProjectRef              *string `json:"project_ref,omitempty"`
	ProjectTitle            *string `json:"project_title,omitempty"`
	ProjectPhase            *string `json:"project_phase,omitempty"`
	BusinessOwnerGroup      *string `json:"business_owner_group,omitempty"`
	ResponsibleGroup        *string `json:"responsible_group,omitempty"`
	Why                     string  `json:"why"`
	DecisionRequested       string  `json:"decision_requested"`
	ConditionsUntilApproved string  `json:"conditions_until_approved"`
	Status                  string  `json:"status"`
}
