package models

import "time"

// SMS role classifications.
const (
	SMSRoleKey           = "key"
	SMSRoleDecisionMaker = "decision_maker"
	SMSRoleSupport       = "support"
	SMSRoleInfluencer    = "influencer"
	SMSRoleObserver      = "observer"
)

// SMS engagement statuses.
const (
	SMSEngagementPassive    = "passive"
	SMSEngagementEngaged    = "engaged"
	SMSEngagementActive     = "active"
	SMSEngagementDisengaged = "disengaged"
)

// SMS lifecycle stages.
const (
	SMSLifecycleOnboarding = "onboarding"
	SMSLifecycleActive     = "active"
	SMSLifecycleRetained   = "retained"
	SMSLifecycleExited     = "exited"
)

// SMS relationship statuses — five-point relationship scale.
const (
	SMSRelationshipAdvocate  = "advocate"
	SMSRelationshipSupportive = "supportive"
	SMSRelationshipNeutral   = "neutral"
	SMSRelationshipConcerned = "concerned"
	SMSRelationshipOpposed   = "opposed"
)

// SMS materiality tiers (Std §8). Tier 1/2 carry the minimum-field
// completeness requirements enforced by the register.
const (
	SMSTier1 = "tier_1"
	SMSTier2 = "tier_2"
	SMSTier3 = "tier_3"
	SMSTier4 = "tier_4"
)

// SMS stakeholder types (register taxonomy).
const (
	SMSTypeManaWhenua              = "mana_whenua"
	SMSTypeElectedMember           = "elected_member"
	SMSTypeGovernmentAgency        = "government_agency"
	SMSTypeRegionalAuthority       = "regional_authority"
	SMSTypeRegulatoryPartner       = "regulatory_partner"
	SMSTypeInfrastructurePartner   = "infrastructure_partner"
	SMSTypeBusiness                = "business"
	SMSTypeDeveloper               = "developer"
	SMSTypeBusinessAssociation     = "business_association"
	SMSTypeStrategicSupplier       = "strategic_supplier"
	SMSTypeUtilityProvider         = "utility_provider"
	SMSTypeCommunityOrganisation   = "community_organisation"
	SMSTypeIndustryGroup           = "industry_group"
	SMSTypeAdvocacyGroup           = "advocacy_group"
	SMSTypeEducationalInstitution  = "educational_institution"
	SMSTypeMajorLandowner          = "major_landowner"
	SMSTypeProjectStakeholder      = "project_stakeholder"
	SMSTypeOther                   = "other"
)

// SMS information classifications (field-level need-to-know).
const (
	SMSInfoPublic        = "public"
	SMSInfoInternal      = "internal"
	SMSInfoConfidential  = "confidential"
	SMSInfoRestricted    = "restricted"
)

// SMS consent statuses.
const (
	SMSConsentNotRequired = "not_required"
	SMSConsentGranted     = "granted"
	SMSConsentDeclined    = "declined"
	SMSConsentWithdrawn   = "withdrawn"
	SMSConsentUnknown     = "unknown"
)

// SMS review cadences.
const (
	SMSReviewMonthly    = "monthly"
	SMSReviewQuarterly  = "quarterly"
	SMSReviewSemiAnnual = "semi_annual"
	SMSReviewAnnual     = "annual"
)

// SMS communication channels.
const (
	SMSChannelEmail     = "email"
	SMSChannelPhone     = "phone"
	SMSChannelMeeting   = "meeting"
	SMSChannelInPerson  = "in_person"
	SMSChannelSlack     = "slack"
	SMSChannelTeams     = "teams"
	SMSChannelOther     = "other"
)

// SMS communication directions.
const (
	SMSDirectionOutbound = "outbound"
	SMSDirectionInbound  = "inbound"
)

// SMS sentiments (manual tag on communications).
const (
	SMSSentimentPositive = "positive"
	SMSSentimentNeutral  = "neutral"
	SMSSentimentNegative = "negative"
)

// SMS engagement plan frequencies.
const (
	SMSFrequencyDaily     = "daily"
	SMSFrequencyWeekly    = "weekly"
	SMSFrequencyBiweekly  = "biweekly"
	SMSFrequencyMonthly   = "monthly"
	SMSFrequencyQuarterly = "quarterly"
	SMSFrequencyAnnual    = "annual"
	SMSFrequencyAsNeeded  = "as_needed"
)

// SMS engagement plan statuses.
const (
	SMSPlanStatusDraft     = "draft"
	SMSPlanStatusActive    = "active"
	SMSPlanStatusPaused    = "paused"
	SMSPlanStatusCompleted = "completed"
)

// IAP2 participation spectrum for engagement approaches (Std §14).
const (
	SMSApproachInform      = "inform"
	SMSApproachConsult     = "consult"
	SMSApproachInvolve     = "involve"
	SMSApproachCollaborate = "collaborate"
	SMSApproachPartner     = "partner"
)

// SMS issue kinds.
const (
	SMSIssueKindIssue = "issue"
	SMSIssueKindRisk  = "risk"
)

// SMS risk/issue categories (Std §15 taxonomy).
const (
	SMSCategoryRelationship   = "relationship"
	SMSCategoryReputation     = "reputation"
	SMSCategoryDelivery       = "delivery"
	SMSCategoryCommunication  = "communication"
	SMSCategoryDependency     = "dependency"
	SMSCategoryKnowledgeLoss  = "knowledge_loss"
	SMSCategoryCommitment     = "commitment"
	SMSCategoryOther          = "other"
)

// SMS issue priorities.
const (
	SMSPriorityLow      = "low"
	SMSPriorityMedium   = "medium"
	SMSPriorityHigh     = "high"
	SMSPriorityCritical = "critical"
)

// SMS issue statuses.
const (
	SMSIssueStatusOpen       = "open"
	SMSIssueStatusInProgress = "in_progress"
	SMSIssueStatusEscalated  = "escalated"
	SMSIssueStatusResolved   = "resolved"
	SMSIssueStatusClosed     = "closed"
)

// SMS task statuses.
const (
	SMSTaskStatusOpen       = "open"
	SMSTaskStatusInProgress = "in_progress"
	SMSTaskStatusDone       = "done"
	SMSTaskStatusCancelled  = "cancelled"
)

// SMS feedback sources.
const (
	SMSFeedbackSourceForm    = "form"
	SMSFeedbackSourceSurvey  = "survey"
	SMSFeedbackSourceEmail   = "email"
	SMSFeedbackSourceMeeting = "meeting"
	SMSFeedbackSourceOther   = "other"
)

// SMS feedback resolution statuses.
const (
	SMSFeedbackOpen      = "open"
	SMSFeedbackAddressed = "addressed"
	SMSFeedbackResolved  = "resolved"
)

// SMS relationship goal statuses.
const (
	SMSGoalOnTrack  = "on_track"
	SMSGoalAtRisk   = "at_risk"
	SMSGoalMissed   = "missed"
	SMSGoalAchieved = "achieved"
)

// P3M entity types a stakeholder can link to.
const (
	SMSEntityPortfolio = "portfolio"
	SMSEntityProgramme = "programme"
	SMSEntityProject   = "project"
)

// SMS commitment directions: who owes what.
const (
	SMSCommitmentFromUs    = "from_us"
	SMSCommitmentFromThem  = "from_them"
	SMSCommitmentMutual    = "mutual"
)

// SMS commitment statuses.
const (
	SMSCommitmentOpen       = "open"
	SMSCommitmentInProgress = "in_progress"
	SMSCommitmentAtRisk     = "at_risk"
	SMSCommitmentCompleted  = "completed"
	SMSCommitmentCancelled  = "cancelled"
)

// SMS grievance severities.
const (
	SMSGrievanceLow      = "low"
	SMSGrievanceMedium   = "medium"
	SMSGrievanceHigh     = "high"
	SMSGrievanceCritical = "critical"
)

// SMS grievance statuses.
const (
	SMSGrievanceReceived      = "received"
	SMSGrievanceAcknowledged  = "acknowledged"
	SMSGrievanceInvestigating = "investigating"
	SMSGrievanceResponded     = "responded"
	SMSGrievanceClosed        = "closed"
)

// SMSStakeholder is one row of sms_stakeholders. HealthScore and the
// completeness flags are computed, not stored.
type SMSStakeholder struct {
	ID                       int       `json:"id"`
	ExternalID               *string   `json:"external_id,omitempty"`
	Name                     string    `json:"name"`
	Title                    string    `json:"title"`
	Organization             string    `json:"organization"`
	Email                    string    `json:"email"`
	Phone                    string    `json:"phone"`
	StakeholderType          *string   `json:"stakeholder_type,omitempty"`
	Category                 string    `json:"category"`
	Tier                     string    `json:"tier"`
	BusinessUnit             string    `json:"business_unit"`
	RoleClassification       string    `json:"role_classification"`
	InfluenceLevel           int       `json:"influence_level"`
	InterestLevel            int       `json:"interest_level"`
	Interests                string    `json:"interests"`
	EngagementStatus         string    `json:"engagement_status"`
	LifecycleStage           string    `json:"lifecycle_stage"`
	RelationshipStatus       string    `json:"relationship_status"`
	TrustLevel               *string   `json:"trust_level,omitempty"`
	RiskRating               *string   `json:"risk_rating,omitempty"`
	CommunicationPreferences string    `json:"communication_preferences"`
	OwnerUserID              *int      `json:"owner_user_id,omitempty"`
	BackupOwnerUserID        *int      `json:"backup_owner_user_id,omitempty"`
	ExecutiveSponsorUserID   *int      `json:"executive_sponsor_user_id,omitempty"`
	LastEngagementAt         *time.Time `json:"last_engagement_at,omitempty"`
	NextEngagementAt         *time.Time `json:"next_engagement_at,omitempty"`
	InfoClassification       string    `json:"info_classification"`
	ConsentStatus            string    `json:"consent_status"`
	RetentionCategory        string    `json:"retention_category"`
	LastReviewedAt           *time.Time `json:"last_reviewed_at,omitempty"`
	ReviewCadence            string    `json:"review_cadence"`
	LocationLat              *float64  `json:"location_lat,omitempty"`
	LocationLon              *float64  `json:"location_lon,omitempty"`
	LocationText             string    `json:"location_text"`
	CustomFields             string    `json:"custom_fields"`
	Notes                    string    `json:"notes"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`

	HealthScore int `json:"health_score"`
	// MinimumFieldsComplete reports whether the register's minimum fields for
	// the stakeholder's tier are present; MissingFields lists what is absent.
	MinimumFieldsComplete bool     `json:"minimum_fields_complete"`
	MissingFields         []string `json:"missing_fields,omitempty"`
}

// SMSStakeholderEntity is one row of sms_stakeholder_entities. EntityTitle is resolved from P3M tables.
type SMSStakeholderEntity struct {
	ID            int       `json:"id"`
	StakeholderID int       `json:"stakeholder_id"`
	EntityType    string    `json:"entity_type"`
	EntityID      int       `json:"entity_id"`
	CreatedAt     time.Time `json:"created_at"`

	EntityTitle string `json:"entity_title,omitempty"`
}

// SMSCommunication is one row of sms_communications. StakeholderName is joined for cross-stakeholder lists.
type SMSCommunication struct {
	ID            int        `json:"id"`
	StakeholderID int        `json:"stakeholder_id"`
	Channel       string     `json:"channel"`
	Direction     string     `json:"direction"`
	Subject       string     `json:"subject"`
	Body          string     `json:"body"`
	Sentiment     *string    `json:"sentiment,omitempty"`
	CreatedBy     *int       `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSEngagementPlan is one row of sms_engagement_plans. StakeholderID is the
// lead stakeholder; additional members live in sms_engagement_plan_stakeholders.
type SMSEngagementPlan struct {
	ID            int        `json:"id"`
	StakeholderID int        `json:"stakeholder_id"`
	Strategy      string     `json:"strategy"`
	Objective     string     `json:"objective"`
	Approach      *string    `json:"approach,omitempty"` // IAP2 spectrum
	KeyMessages   string     `json:"key_messages"`
	SuccessMeasures string   `json:"success_measures"`
	Frequency     string     `json:"frequency"`
	Channel       string     `json:"channel"`
	Status        string     `json:"status"`
	EntityType    *string    `json:"entity_type,omitempty"`
	EntityID      *int       `json:"entity_id,omitempty"`
	StartDate     *time.Time `json:"start_date,omitempty"`
	EndDate       *time.Time `json:"end_date,omitempty"`
	Notes         string     `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	StakeholderName string   `json:"stakeholder_name,omitempty"`
	MemberIDs       []int    `json:"member_ids,omitempty"` // from sms_engagement_plan_stakeholders (includes lead)
}

// SMSIssue is one row of sms_issues.
type SMSIssue struct {
	ID              int        `json:"id"`
	StakeholderID   int        `json:"stakeholder_id"`
	Kind            string     `json:"kind"`
	Category        string     `json:"category"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Priority        string     `json:"priority"`
	Impact          int        `json:"impact"`
	Likelihood      int        `json:"likelihood"`
	Status          string     `json:"status"`
	EscalationLevel int        `json:"escalation_level"`
	EscalationPath  string     `json:"escalation_path"` // e.g. "project → programme" (§25.7)
	Mitigation      string     `json:"mitigation"`
	ResolutionNotes string     `json:"resolution_notes"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// SMSTask is one row of sms_tasks.
type SMSTask struct {
	ID                int        `json:"id"`
	StakeholderID     int        `json:"stakeholder_id"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	AssignedToUserID  *int       `json:"assigned_to_user_id,omitempty"`
	DueDate           *time.Time `json:"due_date,omitempty"`
	Status            string     `json:"status"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// SMSFeedback is one row of sms_feedback.
type SMSFeedback struct {
	ID              int        `json:"id"`
	StakeholderID   int        `json:"stakeholder_id"`
	Source          string     `json:"source"`
	Category        string     `json:"category"`
	Rating          *int       `json:"rating,omitempty"`
	Body            string     `json:"body"`
	ResolutionStatus string    `json:"resolution_status"`
	ResolutionNotes string     `json:"resolution_notes"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// SMSRelationshipGoal is one row of sms_relationship_goals.
type SMSRelationshipGoal struct {
	ID           int        `json:"id"`
	StakeholderID int       `json:"stakeholder_id"`
	Goal         string     `json:"goal"`
	Metric       string     `json:"metric"`
	TargetValue  *float64   `json:"target_value,omitempty"`
	CurrentValue *float64   `json:"current_value,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SMSCommitment is one row of sms_commitments (SRM-1004, Std §21 template 7).
type SMSCommitment struct {
	ID                    int        `json:"id"`
	StakeholderID         int        `json:"stakeholder_id"`
	Direction             string     `json:"direction"` // from_us / from_them / mutual
	Title                 string     `json:"title"`
	Description           string     `json:"description"`
	OwnerUserID           *int       `json:"owner_user_id,omitempty"`
	DueDate               *time.Time `json:"due_date,omitempty"`
	Status                string     `json:"status"`
	Escalated             bool       `json:"escalated"`
	EscalationLevel       int        `json:"escalation_level"`
	SourceCommunicationID *int       `json:"source_communication_id,omitempty"`
	LinkedIssueID         *int       `json:"linked_issue_id,omitempty"`
	Notes                 string     `json:"notes"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSGrievance is one row of sms_grievances (R3; distinct from general feedback).
type SMSGrievance struct {
	ID              int        `json:"id"`
	StakeholderID   int        `json:"stakeholder_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Category        string     `json:"category"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	Escalated       bool       `json:"escalated"`
	EscalationLevel int        `json:"escalation_level"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	ResponseDueAt   *time.Time `json:"response_due_at,omitempty"`
	Response        string     `json:"response"`
	Resolution      string     `json:"resolution"`
	CreatedBy       *int       `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSDecision is one row of sms_decisions (Appendix A: decisions belong in
// relationship history).
type SMSDecision struct {
	ID               int        `json:"id"`
	StakeholderID    int        `json:"stakeholder_id"`
	Title            string     `json:"title"`
	Decision         string     `json:"decision"`
	Context          string     `json:"context"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	DecidedByUserID  *int       `json:"decided_by_user_id,omitempty"`
	EntityType       *string    `json:"entity_type,omitempty"`
	EntityID         *int       `json:"entity_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSStakeholderGroup is one row of sms_stakeholder_groups. MemberCount and
// Members are populated by list endpoints.
type SMSStakeholderGroup struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`

	MemberCount int                `json:"member_count,omitempty"`
	Members     []SMSStakeholderRef `json:"members,omitempty"`
}

// SMSStakeholderGroupMember is one row of sms_stakeholder_group_members.
type SMSStakeholderGroupMember struct {
	ID            int       `json:"id"`
	GroupID       int       `json:"group_id"`
	StakeholderID int       `json:"stakeholder_id"`

	Name string `json:"name,omitempty"`
}

// SMSStakeholderRef is a lightweight stakeholder reference for group members,
// link endpoints and duplicate reports.
type SMSStakeholderRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Email string `json:"email,omitempty"`
}

// SMSStakeholderLink is one row of sms_stakeholder_links (undirected; rows are
// stored once with source_id < target_id). SourceName/TargetName are joined.
type SMSStakeholderLink struct {
	ID           int       `json:"id"`
	SourceID     int       `json:"source_id"`
	TargetID     int       `json:"target_id"`
	RelationType string    `json:"relation_type"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`

	SourceName string `json:"source_name,omitempty"`
	TargetName string `json:"target_name,omitempty"`
}

// SMSOwnershipTransfer is one row of sms_ownership_transfers (Std §16 handover).
type SMSOwnershipTransfer struct {
	ID                  int        `json:"id"`
	StakeholderID       int        `json:"stakeholder_id"`
	FromUserID          *int       `json:"from_user_id,omitempty"`
	ToUserID            *int       `json:"to_user_id,omitempty"`
	Reason              string     `json:"reason"`
	HistoryReviewed     bool       `json:"history_reviewed"`
	RisksReviewed       bool       `json:"risks_reviewed"`
	CommitmentsReviewed bool       `json:"commitments_reviewed"`
	Notes               string     `json:"notes"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CreatedBy           *int       `json:"created_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSPilotBaseline is one row of sms_pilot_baselines (Doc 7 success measures).
type SMSPilotBaseline struct {
	MetricKey     string   `json:"metric_key"`
	BaselineValue *float64 `json:"baseline_value,omitempty"`
	TargetValue   *float64 `json:"target_value,omitempty"`
	SetAt         time.Time `json:"set_at"`
}
