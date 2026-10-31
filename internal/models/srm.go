package models

import "time"

// SRM (Supplier Relationship Management) domain models. The module is gated by
// the global permission PermissionSRMView and implements the NZ Government
// Procurement SRM Strategy Wheel: four phases, twelve steps, phase gates on
// supplier relationships, RACI routing and the dispute escalation ladder.

// Supplier tiers derived from segmentation (nine-box mapping).
const (
	SRMTier1 = "tier_1" // strategic / high-criticality high-value
	SRMTier2 = "tier_2"
	SRMTier3 = "tier_3"
	SRMTier4 = "tier_4"
)

// Supplier relationship states form the phase-gate state machine. Transitions
// are validated server-side (see handlers/srm.go): each gate requires its
// artifacts before advancing; downgrade always requires a reason.
const (
	SRMStateRegistered          = "registered"
	SRMStateSegmented           = "segmented"
	SRMStatePlanningReady       = "planning_ready"
	SRMStateImplementationReady = "implementation_ready"
	SRMStateKickedOff           = "kicked_off"
	SRMStateJBPFormalised       = "jbp_formalised"
	SRMStateMonitoringActive    = "monitoring_active"
	SRMStateClosed              = "closed"
)

// Contract lifecycle statuses.
const (
	SRMContractStatusDraft      = "draft"
	SRMContractStatusActive     = "active"
	SRMContractStatusInWindUp   = "in_wind_up"
	SRMContractStatusRetired    = "retired"
	SRMContractStatusTerminated = "terminated"
)

// Contract assignment roles (Rule 34: every contract needs named people).
const (
	SRMContractRoleInternalManager = "internal_contract_manager"
	SRMContractRoleOwner           = "contract_owner"
	SRMContractRoleBudgetApprover  = "budget_approver"
)

// Segmentation scopes and run lifecycle.
const (
	SRMSegScopeCategory = "category"
	SRMSegScopeSupplier = "supplier"
	SRMSegScopeLite     = "lite"

	SRMRunStatusInProgress = "in_progress"
	SRMRunStatusComplete   = "complete"
	SRMRunStatusValidated  = "validated"
	SRMRunStatusSuperseded = "superseded"
)

// Validation request lifecycle (max three cycles before forced decision).
const (
	SRMValidationPending          = "pending"
	SRMValidationChangesRequested = "changes_requested"
	SRMValidationApproved         = "approved"
	SRMValidationRejected         = "rejected"
)

// Value proposition benefit types.
const (
	SRMBenefitFinancialSaving         = "financial_saving"
	SRMBenefitOperationalEfficiency   = "operational_efficiency"
	SRMBenefitInnovationCapture       = "innovation_capture"
	SRMBenefitRiskComplianceReduction = "risk_compliance_reduction"
	SRMBenefitOther                   = "other"
)

// Stakeholder support bands and engagement levels (3x3 map axes).
const (
	SRMSupportSceptic    = "sceptic"
	SRMSupportOnTheFence = "on_the_fence"
	SRMSupportAmbassador = "ambassador"

	SRMEngagementUnaware    = "unaware"
	SRMEngagementResistant  = "resistant"
	SRMEngagementNeutral    = "neutral"
	SRMEngagementSupportive = "supportive"
	SRMEngagementChampion   = "champion"
)

// RACI values, sides and plan lifecycle.
const (
	SRMRaciValueR = "r" // Responsible
	SRMRaciValueA = "a" // Accountable (exactly one per activity side)
	SRMRaciValueC = "c" // Consulted
	SRMRaciValueI = "i" // Informed

	SRMSideAgency   = "agency"
	SRMSideSupplier = "supplier"

	SRMRaciStatusDraft           = "draft"
	SRMRaciStatusPreliminary     = "preliminary"
	SRMRaciStatusUnderDiscussion = "under_discussion"
	SRMRaciStatusAgreed          = "agreed"
)

// Meeting types (kick-off, workshops, QBRs and reviews).
const (
	SRMMeetingInternalKickoff    = "internal_kickoff"
	SRMMeetingSupplierKickoff    = "supplier_kickoff"
	SRMMeetingJBPWorkshop        = "jbp_workshop"
	SRMMeetingOperationalPerf    = "operational_performance"
	SRMMeetingRelationshipReview = "relationship_review"
	SRMMeetingStrategicRelation  = "strategic_relationship"
	SRMMeetingQBR                = "qbr"
	SRMMeetingOther              = "other"

	SRMMeetingScheduled = "scheduled"
	SRMMeetingHeld      = "held"
	SRMMeetingCancelled = "cancelled"
)

// Joint business plan parts, statuses and initiative lifecycle.
const (
	SRMJBPPartPark                = "park"
	SRMJBPPartJustDo              = "just_do"
	SRMJBPPartStrategicInitiative = "strategic_initiative"
	SRMJBPPartTopPriority         = "top_priority"

	SRMJBPStatusDraft      = "draft"
	SRMJBPStatusInReview   = "in_review"
	SRMJBPStatusSigned     = "signed"
	SRMJBPStatusSuperseded = "superseded"

	SRMInitiativeNotStarted = "not_started"
	SRMInitiativeInProgress = "in_progress"
	SRMInitiativeOnHold     = "on_hold"
	SRMInitiativeComplete   = "complete"
)

// RAG values shared across scorecards, deliverables and supplier ratings.
const (
	SRMRagGreen = "green"
	SRMRagAmber = "amber"
	SRMRagRed   = "red"
)

// Scorecard dimensions (five official areas).
const (
	SRMDimRiskSummary        = "risk_summary"
	SRMDimInitiativesSummary = "initiatives_summary"
	SRMDimPerformanceSummary = "performance_summary"
	SRMDimBenefitsDelivered  = "benefits_delivered"
	SRMDimGovernanceSummary  = "governance_summary"

	SRMScorecardOpen   = "open"
	SRMScorecardClosed = "closed"
)

// Innovation register lifecycle and decisions.
const (
	SRMInnovationSubmitted          = "submitted"
	SRMInnovationUnderReview        = "under_review"
	SRMInnovationProceed            = "proceed"
	SRMInnovationReject             = "reject"
	SRMInnovationFurtherInvestigate = "further_investigate"
	SRMInnovationImplementing       = "implementing"
	SRMInnovationImplemented        = "implemented"

	SRMDecisionProceed            = "proceed"
	SRMDecisionReject             = "reject"
	SRMDecisionFurtherInvestigate = "further_investigate"
)

// Service improvement plan and dispute lifecycle.
const (
	SRMSIPDraft          = "draft"
	SRMSIPActive         = "active"
	SRMSIPOnTrackReview  = "on_track_review"
	SRMIPSResolved       = "resolved"
	SRMSIPClosUnresolved = "closed_unresolved"

	SRMDisputeL1WorkingLevel = "l1_working_level"
	SRMDisputeL2Director     = "l2_director"
	SRMDisputeL3Executive    = "l3_executive"
	SRMDisputeMediation      = "mediation"
	SRMDisputeLitigation     = "litigation"
	SRMDisputeClosed         = "closed"

	SRMRemedyActive    = "active"
	SRMRemedyResolved  = "resolved"
	SRMRemedyBreached  = "breached"
	SRMRemedyEscalated = "escalated"
)

// SRMSupplier is a master-directory entry. RelationshipState advances through
// the phase-gate state machine as artifacts are completed.
type SRMSupplier struct {
	ID                int       `json:"id"`
	CompanyName       string    `json:"company_name"`
	NZBN              *string   `json:"nzbn,omitempty"`
	TaxIDNumber       *string   `json:"tax_id_number,omitempty"`
	Website           string    `json:"website"`
	Tier              string    `json:"tier"`
	RelationshipState string    `json:"relationship_state"`
	IsActive          bool      `json:"is_active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// Computed server-side on list/get.
	ActiveContracts int                  `json:"active_contracts" db:"-"`
	OpenDisputes    int                  `json:"open_disputes" db:"-"`
	Contacts        []SRMSupplierContact `json:"contacts,omitempty" db:"-"`

	// Gate is populated by GET /srm/suppliers/{id} (phase-gate position).
	Gate *SRMGateStatus `json:"gate,omitempty" db:"-"`
}

// SRMGateStatus is the computed phase-gate position of a supplier relationship.
// Artifacts maps each artefact required for the next gate to its current
// satisfaction; Missing lists the unsatisfied ones (FR-GATE-1 tooltips).
type SRMGateStatus struct {
	Current   string          `json:"current"`
	Next      string          `json:"next,omitempty"`
	Artifacts map[string]bool `json:"artifacts"`
	Missing   []string        `json:"missing"`
}

type SRMSupplierContact struct {
	ID         int       `json:"id"`
	SupplierID int       `json:"supplier_id"`
	FullName   string    `json:"full_name"`
	RoleTitle  string    `json:"role_title"`
	Email      string    `json:"email"`
	Phone      string    `json:"phone"`
	IsPrimary  bool      `json:"is_primary"`
	CreatedAt  time.Time `json:"created_at"`
}

// SRMContract is a Rule-34 contract register entry. P3MEntity* form the
// polymorphic binding to portfolio/programme/project (application-level FK).
type SRMContract struct {
	ID                int        `json:"id"`
	SupplierID        int        `json:"supplier_id"`
	P3MEntityType     *string    `json:"p3m_entity_type,omitempty"`
	P3MEntityID       *int       `json:"p3m_entity_id,omitempty"`
	ContractNumber    string     `json:"contract_number"`
	Title             string     `json:"title"`
	Category          string     `json:"category"`
	Classification    string     `json:"classification"`
	ScopeSummary      string     `json:"scope_summary"`
	DocumentURL       string     `json:"document_url"`
	Status            string     `json:"status"`
	TotalValue        float64    `json:"total_value"`
	Currency          string     `json:"currency"`
	CommittedSpend    float64    `json:"committed_spend"`
	ActualSpend       float64    `json:"actual_spend"`
	SpendSource       string     `json:"spend_source"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           time.Time  `json:"end_date"`
	ExtensionCount    int        `json:"extension_count"`
	NoticePeriodDays  *int       `json:"notice_period_days,omitempty"`
	ReviewDate        *time.Time `json:"review_date,omitempty"`
	CostCode          string     `json:"cost_code"`
	PONumber          string     `json:"po_number"`
	TerminationReason string     `json:"termination_reason"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Computed server-side on list/get.
	SupplierName   string                     `json:"supplier_name,omitempty" db:"-"`
	P3MEntityTitle *string                    `json:"p3m_entity_title,omitempty" db:"-"`
	Assignments    []SRMContractAssignment    `json:"assignments,omitempty" db:"-"`
	Documents      []SRMContractDocument      `json:"documents,omitempty" db:"-"`
	ManagementPlan *SRMContractManagementPlan `json:"management_plan,omitempty" db:"-"`
}

type SRMContractAssignment struct {
	ID         int       `json:"id"`
	ContractID int       `json:"contract_id"`
	UserID     int       `json:"user_id"`
	Role       string    `json:"role"`
	AssignedAt time.Time `json:"assigned_at"`

	UserName string `json:"user_name,omitempty" db:"-"`
}

type SRMContractDocument struct {
	ID            int        `json:"id"`
	ContractID    int        `json:"contract_id"`
	DocType       string     `json:"doc_type"`
	FileName      string     `json:"file_name"`
	StoragePath   string     `json:"storage_path"`
	Version       int        `json:"version"`
	EffectiveDate *time.Time `json:"effective_date,omitempty"`
	UploadedBy    *int       `json:"uploaded_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`

	Extractions []SRMClauseExtraction `json:"extractions,omitempty" db:"-"`
}

// SRMClauseExtraction is an OCR/model-extracted clause awaiting human review.
type SRMClauseExtraction struct {
	ID            int        `json:"id"`
	DocumentID    int        `json:"document_id"`
	FieldName     string     `json:"field_name"`
	ProposedValue string     `json:"proposed_value"`
	Confidence    *float64   `json:"confidence,omitempty"`
	Status        string     `json:"status"`
	ReviewedBy    *int       `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
}

// SRMContractManagementPlan is the right-sized management plan (versioned).
type SRMContractManagementPlan struct {
	ID                     int       `json:"id"`
	ContractID             int       `json:"contract_id"`
	Version                int       `json:"version"`
	RightSizeTier          string    `json:"right_size_tier"`
	Objectives             string    `json:"objectives"`
	KeyTermsSummary        string    `json:"key_terms_summary"`
	GovernanceArrangements string    `json:"governance_arrangements"`
	MonitoringProvisions   string    `json:"monitoring_provisions"`
	ChangeReason           string    `json:"change_reason"`
	CreatedBy              *int      `json:"created_by,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
}

// ------------------------- Segmentation engine (Phase 1) -------------------

type SRMCategory struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`

	// Computed server-side on list/get.
	SupplierCount int `json:"supplier_count" db:"-"`
}

type SRMSegmentationRun struct {
	ID          int        `json:"id"`
	Scope       string     `json:"scope"`
	CategoryID  *int       `json:"category_id,omitempty"`
	CreatedBy   int        `json:"created_by"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes"`
	CreatedAt   time.Time  `json:"created_at"`
	ValidatedAt *time.Time `json:"validated_at,omitempty"`

	CategoryName string                  `json:"category_name,omitempty" db:"-"`
	Results      []SRMSegmentationResult `json:"results,omitempty" db:"-"`
	Answers      []SRMSegmentationAnswer `json:"answers,omitempty" db:"-"`
}

// SRMSegmentationAnswer is one scored question. WeightedScore = Score*Weight,
// computed server-side at write time (weights come from the official sets).
type SRMSegmentationAnswer struct {
	ID            int       `json:"id"`
	RunID         int       `json:"run_id"`
	EntityRef     int       `json:"entity_ref"`
	QuestionCode  string    `json:"question_code"`
	Score         int       `json:"score"` // 0-3
	AnchorLabel   string    `json:"anchor_label"`
	Weight        float64   `json:"weight"`
	WeightedScore float64   `json:"weighted_score"`
	Justification string    `json:"justification"`
	ScoredBy      int       `json:"scored_by"`
	ScoredAt      time.Time `json:"scored_at"`

	EntityName string `json:"entity_name,omitempty" db:"-"`
}

// SRMSegmentationResult is the nine-box outcome for one entity in a run.
type SRMSegmentationResult struct {
	ID               int      `json:"id"`
	RunID            int      `json:"run_id"`
	EntityType       string   `json:"entity_type"` // category | supplier
	EntityRef        int      `json:"entity_ref"`
	CriticalityScore *float64 `json:"criticality_score,omitempty"`
	ValueScore       *float64 `json:"value_score,omitempty"`
	SpendAmount      *float64 `json:"spend_amount,omitempty"`
	RankCriticality  *string  `json:"rank_criticality,omitempty"` // high|medium|low
	RankValue        *string  `json:"rank_value,omitempty"`       // high|medium|low
	SegmentationBox  string   `json:"segmentation_box"`
	BoxNumber        *int     `json:"box_number,omitempty"` // 1-9
	MappedTier       *string  `json:"mapped_tier,omitempty"`

	EntityName string `json:"entity_name,omitempty" db:"-"`
}

// ------------------------- Validation (Phase 1 / Step 1.3) ------------------

type SRMValidationRequest struct {
	ID            int        `json:"id"`
	RunID         int        `json:"run_id"`
	SupplierID    *int       `json:"supplier_id,omitempty"`
	RequestedBy   int        `json:"requested_by"`
	Status        string     `json:"status"`
	CycleCount    int        `json:"cycle_count"`              // 1-3, then forced decision
	SignoffMethod *string    `json:"signoff_method,omitempty"` // in_app | email
	SignedOffBy   *int       `json:"signed_off_by,omitempty"`
	SignedOffAt   *time.Time `json:"signed_off_at,omitempty"`
	Comment       string     `json:"comment"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	SupplierName string                 `json:"supplier_name,omitempty" db:"-"`
	Adjustments  []SRMScoringAdjustment `json:"adjustments,omitempty" db:"-"`
}

type SRMScoringAdjustment struct {
	ID           int       `json:"id"`
	RequestID    int       `json:"request_id"`
	RunID        int       `json:"run_id"`
	EntityRef    int       `json:"entity_ref"`
	QuestionCode string    `json:"question_code"`
	OldScore     *int      `json:"old_score,omitempty"`
	NewScore     *int      `json:"new_score,omitempty"`
	ChangedBy    int       `json:"changed_by"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`

	EntityName string `json:"entity_name,omitempty" db:"-"`
}

// ------------------------- Planning phase (Phase 2) ------------------------

type SRMValueProposition struct {
	ID                          int        `json:"id"`
	SupplierID                  int        `json:"supplier_id"`
	ContractID                  *int       `json:"contract_id,omitempty"`
	AgencyObjective             string     `json:"agency_objective"`
	SupplierServiceProduct      string     `json:"supplier_service_product"`
	SrmAmbition                 string     `json:"srm_ambition"`
	BenefitType                 string     `json:"benefit_type"`
	AnticipatedBenefit          string     `json:"anticipated_benefit"`
	EstimatedValueNzd           *float64   `json:"estimated_value_nzd,omitempty"`
	ValueConfidence             *string    `json:"value_confidence,omitempty"` // high|medium|low
	MeasurementMethod           string     `json:"measurement_method"`
	EngagementFrequency         string     `json:"engagement_frequency"`
	P3MBenefitRef               string     `json:"p3m_benefit_ref"`
	ActualValueNzd              *float64   `json:"actual_value_nzd,omitempty"`
	SupplierStrategicObjective  string     `json:"supplier_strategic_objective"`
	OpportunityStatement        string     `json:"opportunity_statement"`
	SrmSpecificAmbition         string     `json:"srm_specific_ambition"`
	AnticipatedSupplierBenefits string     `json:"anticipated_supplier_benefits"`
	SignedOffBy                 *int       `json:"signed_off_by,omitempty"`
	SignedOffAt                 *time.Time `json:"signed_off_at,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`

	SupplierName string `json:"supplier_name,omitempty" db:"-"`
}

type SRMStakeholder struct {
	ID           int       `json:"id"`
	FullName     string    `json:"full_name"`
	Email        string    `json:"email"`
	Organization string    `json:"organization"`
	JobTitle     string    `json:"job_title"`
	BusinessArea string    `json:"business_area"`
	IsExternal   bool      `json:"is_external"`
	SupplierID   *int      `json:"supplier_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`

	SupplierName string `json:"supplier_name,omitempty" db:"-"`
}

// SRMEntityStakeholder binds a stakeholder to a portfolio/programme/project or
// contract. InfluenceBand, Quadrant and SalienceClass are derived server-side
// from the stored scores (see handlers/srm_planning.go).
type SRMEntityStakeholder struct {
	ID                int       `json:"id"`
	StakeholderID     int       `json:"stakeholder_id"`
	EntityType        string    `json:"entity_type"` // portfolio|programme|project|contract
	EntityID          int       `json:"entity_id"`
	RoleDescription   string    `json:"role_description"`
	PowerScore        int       `json:"power_score"`                // 1-5
	InterestScore     int       `json:"interest_score"`             // 1-5
	SupportBand       string    `json:"support_band"`               // sceptic|on_the_fence|ambassador
	SentimentScore    int       `json:"sentiment_score"`            // -2..+2
	LegitimacyScore   *int      `json:"legitimacy_score,omitempty"` // 1-5
	UrgencyScore      *int      `json:"urgency_score,omitempty"`    // 1-5
	CurrentEngagement string    `json:"current_engagement"`
	TargetEngagement  string    `json:"target_engagement"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// Derived, computed server-side (spec §5.2.3a generated columns).
	InfluenceBand string `json:"influence_band" db:"-"` // high|medium|low (power_score band: 4-5/3/1-2)
	Quadrant      string `json:"quadrant" db:"-"`       // manage_closely|keep_satisfied|keep_informed|monitor
	SalienceClass string `json:"salience_class" db:"-"` // Mitchell: definitive|dominant|dangerous|demanding|dependent|discretionary|reactive|dormant

	StakeholderName string `json:"stakeholder_name,omitempty" db:"-"`
}

type SRMStakeholderPhaseSnapshot struct {
	ID                  int       `json:"id"`
	EntityStakeholderID int       `json:"entity_stakeholder_id"`
	ContractPhase       string    `json:"contract_phase"` // preparation|planning|implementation|monitoring
	PowerScore          int       `json:"power_score"`
	InterestScore       int       `json:"interest_score"`
	SentimentScore      int       `json:"sentiment_score"`
	RecordedBy          int       `json:"recorded_by"`
	RecordedAt          time.Time `json:"recorded_at"`
}

type SRMCommunicationsPlan struct {
	ID         int       `json:"id"`
	SupplierID int       `json:"supplier_id"`
	Scope      string    `json:"scope"` // all|internal|external
	IsComplete bool      `json:"is_complete"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	Messages []SRMCommsMessage `json:"messages,omitempty" db:"-"`
}

type SRMCommsMessage struct {
	ID                   int        `json:"id"`
	PlanID               int        `json:"plan_id"`
	StakeholderID        int        `json:"stakeholder_id"`
	CommunicationMessage string     `json:"communication_message"`
	Method               string     `json:"method"`
	Frequency            string     `json:"frequency"`
	CommunicatorID       *int       `json:"communicator_id,omitempty"`
	SignedOff            bool       `json:"signed_off"`
	SignedOffBy          *int       `json:"signed_off_by,omitempty"`
	SignedOffAt          *time.Time `json:"signed_off_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`

	StakeholderName string `json:"stakeholder_name,omitempty" db:"-"`
}

// SRMRaci is a versioned RACI matrix for one supplier relationship. Exactly
// one Accountable per activity side is enforced server-side on save.
type SRMRaci struct {
	ID             int        `json:"id"`
	SupplierID     int        `json:"supplier_id"`
	Version        int        `json:"version"`
	Status         string     `json:"status"` // draft|preliminary|under_discussion|agreed
	CreatedBy      *int       `json:"created_by,omitempty"`
	AgreedAt       *time.Time `json:"agreed_at,omitempty"`
	LastReviewedAt *time.Time `json:"last_reviewed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`

	SupplierName string              `json:"supplier_name,omitempty" db:"-"`
	Assignments  []SRMRaciAssignment `json:"assignments,omitempty" db:"-"`
}

type SRMRaciAssignment struct {
	ID           int    `json:"id"`
	RaciID       int    `json:"raci_id"`
	ActivityCode string `json:"activity_code"`
	Side         string `json:"side"` // agency|supplier
	RoleName     string `json:"role_name"`
	UserID       *int   `json:"user_id,omitempty"`
	Value        string `json:"value"` // r|a|c|i

	UserName string `json:"user_name,omitempty" db:"-"`
}

// ------------------------- Meetings engine (Phase 3) -----------------------

// SRMAgendaItem and SRMOutcome are the typed shapes stored in the meeting's
// agenda_json / outcomes_json TEXT columns.
type SRMAgendaItem struct {
	No          int    `json:"no"`
	Description string `json:"description"`
	Mode        string `json:"mode"` // discussion | decision | information
	RaisedBy    string `json:"raised_by,omitempty"`
}

type SRMOutcome struct {
	ItemNo   int        `json:"item_no"`
	Outcome  string     `json:"outcome"`
	NextStep string     `json:"next_step,omitempty"`
	OwnerID  *int       `json:"owner_id,omitempty"`
	DueDate  *time.Time `json:"due_date,omitempty"`
}

type SRMMeeting struct {
	ID          int             `json:"id"`
	MeetingType string          `json:"meeting_type"`
	SupplierID  int             `json:"supplier_id"`
	ContractID  *int            `json:"contract_id,omitempty"`
	OrganizerID *int            `json:"organizer_id,omitempty"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	Location    string          `json:"location"`
	Agenda      []SRMAgendaItem `json:"agenda"`
	Outcomes    []SRMOutcome    `json:"outcomes"`
	MinutesText string          `json:"minutes_text"`
	Status      string          `json:"status"` // scheduled|held|cancelled
	CreatedAt   time.Time       `json:"created_at"`

	SupplierName string               `json:"supplier_name,omitempty" db:"-"`
	Attendees    []SRMMeetingAttendee `json:"attendees,omitempty" db:"-"`
	Actions      []SRMMeetingAction   `json:"actions,omitempty" db:"-"`
}

type SRMMeetingAttendee struct {
	ID              int    `json:"id"`
	MeetingID       int    `json:"meeting_id"`
	StakeholderID   *int   `json:"stakeholder_id,omitempty"`
	UserID          *int   `json:"user_id,omitempty"`
	Side            string `json:"side"` // agency|supplier
	RoleDescription string `json:"role_description"`
	Attended        *bool  `json:"attended,omitempty"`

	Name string `json:"name,omitempty" db:"-"`
}

type SRMMeetingAction struct {
	ID           int        `json:"id"`
	MeetingID    int        `json:"meeting_id"`
	AgendaItemNo *int       `json:"agenda_item_no,omitempty"`
	Kind         string     `json:"kind"` // note|question|action
	Text         string     `json:"text"`
	OwnerID      *int       `json:"owner_id,omitempty"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	Status       string     `json:"status"` // open|closed

	UserName string `json:"user_name,omitempty" db:"-"`
}

// ------------------------- Joint business plans (Phase 3) ------------------

// SRMValueOpportunity is one entry of the JBP value_opportunities JSON array.
type SRMValueOpportunity struct {
	Initiative     string `json:"initiative"`
	Quantification string `json:"quantification,omitempty"`
	Metrics        string `json:"metrics,omitempty"`
	ValueSharing   string `json:"value_sharing,omitempty"`
}

// SRMJBP is a versioned joint business plan. Both parties must sign before the
// status becomes "signed" (gate to monitoring_active).
type SRMJBP struct {
	ID                      int                   `json:"id"`
	ContractID              int                   `json:"contract_id"`
	SupplierID              int                   `json:"supplier_id"`
	Version                 int                   `json:"version"`
	Status                  string                `json:"status"` // draft|in_review|signed|superseded
	Purpose                 string                `json:"purpose"`
	Vision                  string                `json:"vision"`
	Scope                   string                `json:"scope"`
	GovernanceModel         string                `json:"governance_model"`
	ReviewCycleMonths       int                   `json:"review_cycle_months"`
	RelationshipCharter     []string              `json:"relationship_charter"`
	ContractSummary         string                `json:"contract_summary"`
	SpendSummary            *float64              `json:"spend_summary,omitempty"`
	PerformanceNotes        string                `json:"performance_notes"`
	RiskActionPlan          string                `json:"risk_action_plan"`
	RelationshipDevelopment string                `json:"relationship_development"`
	ValueOpportunities      []SRMValueOpportunity `json:"value_opportunities"`
	AgencyRepName           string                `json:"agency_rep_name"`
	AgencyRepRole           string                `json:"agency_rep_role"`
	AgencySignedAt          *time.Time            `json:"agency_signed_at,omitempty"`
	SupplierRepName         string                `json:"supplier_rep_name"`
	SupplierRepRole         string                `json:"supplier_rep_role"`
	SupplierSignedAt        *time.Time            `json:"supplier_signed_at,omitempty"`
	CreatedAt               time.Time             `json:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at"`

	SupplierName string             `json:"supplier_name,omitempty" db:"-"`
	Initiatives  []SRMJBPInitiative `json:"initiatives,omitempty" db:"-"`
}

type SRMJBPInitiative struct {
	ID             int       `json:"id"`
	JbpID          int       `json:"jbp_id"`
	QuarterLabel   string    `json:"quarter_label"`
	Part           string    `json:"part"` // park|just_do|strategic_initiative|top_priority
	InitiativeName string    `json:"initiative_name"`
	Activities     string    `json:"activities"`
	KeyDates       []string  `json:"key_dates"`
	OwnerID        *int      `json:"owner_id,omitempty"`
	Comment        string    `json:"comment"`
	Status         string    `json:"status"` // not_started|in_progress|on_hold|complete
	CreatedAt      time.Time `json:"created_at"`

	UserName string `json:"user_name,omitempty" db:"-"`
}

// ------------------------- Monitoring (Phase 4) ----------------------------

type SRMPerformanceMetric struct {
	ID          int       `json:"id"`
	ContractID  int       `json:"contract_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"` // sla|kpi
	Unit        string    `json:"unit"`
	TargetValue float64   `json:"target_value"`
	Direction   string    `json:"direction"`   // above|below
	Frequency   string    `json:"frequency"`   // monthly|quarterly
	DataSource  string    `json:"data_source"` // manual|erp_api|supplier_reported
	OwnerID     *int      `json:"owner_id,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`

	Results []SRMMetricResult `json:"results,omitempty" db:"-"`
}

// SRMMetricResult is one period measurement. Achieved is computed server-side:
// direction "above" -> actual >= target, "below" -> actual <= target.
type SRMMetricResult struct {
	ID          int       `json:"id"`
	MetricID    int       `json:"metric_id"`
	PeriodStart time.Time `json:"period_start"`
	ActualValue float64   `json:"actual_value"`
	Achieved    bool      `json:"achieved"`
	RecordedBy  *int      `json:"recorded_by,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`

	MetricName string `json:"metric_name,omitempty" db:"-"`
}

type SRMScorecard struct {
	ID                       int       `json:"id"`
	ContractID               int       `json:"contract_id"`
	SupplierID               int       `json:"supplier_id"`
	PeriodStart              time.Time `json:"period_start"`
	PeriodEnd                time.Time `json:"period_end"`
	CompletedCollaboratively bool      `json:"completed_collaboratively"`
	OverallScore             *int      `json:"overall_score,omitempty"` // 1-5
	OverallReasoning         string    `json:"overall_reasoning"`
	AgencyCompletedBy        *int      `json:"agency_completed_by,omitempty"`
	SupplierCompletedByName  string    `json:"supplier_completed_by_name"`
	Status                   string    `json:"status"` // open|closed
	CreatedAt                time.Time `json:"created_at"`

	SupplierName string                  `json:"supplier_name,omitempty" db:"-"`
	Dimensions   []SRMScorecardDimension `json:"dimensions,omitempty" db:"-"`
}

// SRMScorecardDimension is one of the five official scorecard areas. A supplier
// RAG that differs from the agency RAG requires a joint resolution note (DB CHECK).
type SRMScorecardDimension struct {
	ID                  int     `json:"id"`
	ScorecardID         int     `json:"scorecard_id"`
	Dimension           string  `json:"dimension"` // risk_summary|initiatives_summary|performance_summary|benefits_delivered|governance_summary
	MainHighlights      string  `json:"main_highlights"`
	KeyConcerns         string  `json:"key_concerns"`
	Rag                 string  `json:"rag"` // green|amber|red
	NextSteps           string  `json:"next_steps"`
	SupplierRag         *string `json:"supplier_rag,omitempty"`
	JointResolutionNote string  `json:"joint_resolution_note"`
}

type SRMPulseSurvey struct {
	ID              int        `json:"id"`
	SupplierID      int        `json:"supplier_id"`
	Direction       string     `json:"direction"` // internal_rates_supplier|supplier_rates_agency
	TemplateVersion int        `json:"template_version"`
	SentAt          time.Time  `json:"sent_at"`
	DueDate         *time.Time `json:"due_date,omitempty"`
	ResponsesCount  int        `json:"responses_count"`

	SupplierName string              `json:"supplier_name,omitempty" db:"-"`
	Responses    []SRMSurveyResponse `json:"responses,omitempty" db:"-"`
}

type SRMSurveyResponse struct {
	ID            int            `json:"id"`
	SurveyID      int            `json:"survey_id"`
	StakeholderID *int           `json:"stakeholder_id,omitempty"`
	IsAnonymous   bool           `json:"is_anonymous"`
	Answers       map[string]any `json:"answers"`
	OverallScore  *int           `json:"overall_score,omitempty"` // 1-5
	RespondedAt   time.Time      `json:"responded_at"`

	StakeholderName string `json:"stakeholder_name,omitempty" db:"-"`
}

// ------------------------- Innovation register (Phase 4 / Step 4.2) --------

type SRMInnovation struct {
	ID                      int       `json:"id"`
	IdeaCode                *string   `json:"idea_code,omitempty"` // INNN###, auto-generated on create
	ContractID              int       `json:"contract_id"`
	SupplierID              int       `json:"supplier_id"`
	Name                    string    `json:"name"`
	Description             string    `json:"description"`
	IdeaDate                time.Time `json:"idea_date"`
	SupplierTeam            string    `json:"supplier_team"`
	AgencyTeam              string    `json:"agency_team"`
	Source                  string    `json:"source"` // supplier_submission|agency_observation|jbp_workshop|scorecard_review|other
	Status                  string    `json:"status"` // submitted|under_review|proceed|reject|further_investigate|implementing|implemented
	PotentialFinancialValue float64   `json:"potential_financial_value"`
	RealisedFinancialValue  *float64  `json:"realised_financial_value,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`

	SupplierName   string                       `json:"supplier_name,omitempty" db:"-"`
	Factors        []SRMInnovationFactor        `json:"factors,omitempty" db:"-"`
	Recommendation *SRMInnovationRecommendation `json:"recommendation,omitempty" db:"-"`
	Objectives     []SRMInnovationObjective     `json:"objectives,omitempty" db:"-"`
	Tasks          []SRMInnovationTask          `json:"tasks,omitempty" db:"-"`
}

// SRMInnovationFactor is one evaluation factor rated 1-5 where 1 = optimal
// (official inversion: lower is better).
type SRMInnovationFactor struct {
	ID            int    `json:"id"`
	InnovationID  int    `json:"innovation_id"`
	EvaluatorName string `json:"evaluator_name"`
	FactorName    string `json:"factor_name"`
	IsCustom      bool   `json:"is_custom"`
	Rating        int    `json:"rating"` // 1-5, 1 = optimal
}

type SRMInnovationRecommendation struct {
	ID                   int       `json:"id"`
	InnovationID         int       `json:"innovation_id"`
	Decision             string    `json:"decision"` // proceed|reject|further_investigate
	Justification        string    `json:"justification"`
	Priority             *string   `json:"priority,omitempty"` // high|medium|low
	Timing               string    `json:"timing"`
	AgencyChampionID     *int      `json:"agency_champion_id,omitempty"`
	SupplierChampionName string    `json:"supplier_champion_name"`
	DecidedAt            time.Time `json:"decided_at"`

	UserName string `json:"user_name,omitempty" db:"-"`
}

type SRMInnovationObjective struct {
	ID               int      `json:"id"`
	InnovationID     int      `json:"innovation_id"`
	ObjectiveText    string   `json:"objective_text"`
	ActualOutcome    string   `json:"actual_outcome"`
	RealisedValueNzd *float64 `json:"realised_value_nzd,omitempty"`
}

type SRMInnovationTask struct {
	ID           int        `json:"id"`
	InnovationID int        `json:"innovation_id"`
	TaskNo       int        `json:"task_no"`
	TaskName     string     `json:"task_name"`
	Description  string     `json:"description"`
	Deliverable  string     `json:"deliverable"`
	OwnerName    string     `json:"owner_name"`
	Side         string     `json:"side"` // agency|supplier
	StartDate    *time.Time `json:"start_date,omitempty"`
	EndDate      *time.Time `json:"end_date,omitempty"`
	Status       string     `json:"status"` // in_progress|awaiting_approval|sign_off_stage|complete
}

// ------------------------- SIPs, disputes and remedies (Phase 4 / Step 4.3) -

type SRMServiceImprovementPlan struct {
	ID                  int       `json:"id"`
	ContractID          int       `json:"contract_id"`
	SupplierID          int       `json:"supplier_id"`
	Title               string    `json:"title"`
	SipOwnerID          int       `json:"sip_owner_id"`
	ProblemStatement    string    `json:"problem_statement"`
	Subtype             string    `json:"subtype"` // breach_driven|continuous_improvement
	SuccessMeasureSmart string    `json:"success_measure_smart"`
	ExitCriteria        string    `json:"exit_criteria"`
	Status              string    `json:"status"` // draft|active|on_track_review|resolved|closed_unresolved
	SourceRef           string    `json:"source_ref"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	SupplierName string              `json:"supplier_name,omitempty" db:"-"`
	Deliverables []SRMSIPDeliverable `json:"deliverables,omitempty" db:"-"`
}

type SRMSIPDeliverable struct {
	ID                  int        `json:"id"`
	SipID               int        `json:"sip_id"`
	Seq                 int        `json:"seq"`
	KeyDeliverable      string     `json:"key_deliverable"`
	CompletionDate      *time.Time `json:"completion_date,omitempty"`
	Rag                 string     `json:"rag"` // green|amber|red
	OwnerName           string     `json:"owner_name"`
	CommentsRisksIssues string     `json:"comments_risks_issues"`
}

type SRMDispute struct {
	ID          int        `json:"id"`
	ContractID  int        `json:"contract_id"`
	SupplierID  int        `json:"supplier_id"`
	Description string     `json:"description"`
	Stage       string     `json:"stage"` // l1_working_level|l2_director|l3_executive|mediation|litigation|closed
	OpenedAt    time.Time  `json:"opened_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	OutcomeNote string     `json:"outcome_note"`

	SupplierName string                 `json:"supplier_name,omitempty" db:"-"`
	Escalations  []SRMDisputeEscalation `json:"escalations,omitempty" db:"-"`
	RemedyPlan   *SRMRemedyPlan         `json:"remedy_plan,omitempty" db:"-"`
}

type SRMDisputeEscalation struct {
	ID          int       `json:"id"`
	DisputeID   int       `json:"dispute_id"`
	FromStage   string    `json:"from_stage"`
	ToStage     string    `json:"to_stage"`
	EscalatedAt time.Time `json:"escalated_at"`
	EscalatedTo string    `json:"escalated_to"`
	PauseReason string    `json:"pause_reason"`
	Notes       string    `json:"notes"`
}

// SRMRemedyPlan is a corrective-action plan attached to either a dispute or a
// service improvement plan (DB CHECK requires at least one).
type SRMRemedyPlan struct {
	ID                       int                 `json:"id"`
	DisputeID                *int                `json:"dispute_id,omitempty"`
	SipID                    *int                `json:"sip_id,omitempty"`
	ContractID               int                 `json:"contract_id"`
	NonPerformanceIssue      string              `json:"non_performance_issue"`
	RootCause                string              `json:"root_cause"`
	CorrectiveActionRequired string              `json:"corrective_action_required"`
	Timetable                []SRMTimetableEntry `json:"timetable"`
	TargetResolutionDate     time.Time           `json:"target_resolution_date"`
	Status                   string              `json:"status"` // active|resolved|breached|escalated
	CreatedAt                time.Time           `json:"created_at"`

	SupplierName string `json:"supplier_name,omitempty" db:"-"`
}

// SRMTimetableEntry is one row of the remedy plan timetable JSON.
type SRMTimetableEntry struct {
	Action    string     `json:"action"`
	OwnerSide string     `json:"owner_side"` // agency|supplier
	DueDate   *time.Time `json:"due_date,omitempty"`
}
