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

// SMS relationship statuses.
const (
	SMSRelationshipPositive = "positive"
	SMSRelationshipNeutral  = "neutral"
	SMSRelationshipTense    = "tense"
	SMSRelationshipConflict = "conflict"
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

// SMS issue kinds.
const (
	SMSIssueKindIssue = "issue"
	SMSIssueKindRisk  = "risk"
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

// SMSStakeholder is one row of sms_stakeholders. HealthScore is computed, not stored.
type SMSStakeholder struct {
	ID                      int       `json:"id"`
	Name                    string    `json:"name"`
	Title                   string    `json:"title"`
	Organization            string    `json:"organization"`
	Email                   string    `json:"email"`
	Phone                   string    `json:"phone"`
	RoleClassification      string    `json:"role_classification"`
	InfluenceLevel          int       `json:"influence_level"`
	InterestLevel           int       `json:"interest_level"`
	Interests               string    `json:"interests"`
	EngagementStatus        string    `json:"engagement_status"`
	LifecycleStage          string    `json:"lifecycle_stage"`
	RelationshipStatus      string    `json:"relationship_status"`
	CommunicationPreferences string   `json:"communication_preferences"`
	OwnerUserID             *int      `json:"owner_user_id,omitempty"`
	CustomFields            string    `json:"custom_fields"`
	Notes                   string    `json:"notes"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`

	HealthScore int `json:"health_score"`
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

// SMSEngagementPlan is one row of sms_engagement_plans. StakeholderName is joined for cross-stakeholder lists.
type SMSEngagementPlan struct {
	ID            int        `json:"id"`
	StakeholderID int        `json:"stakeholder_id"`
	Strategy      string     `json:"strategy"`
	Frequency     string     `json:"frequency"`
	Channel       string     `json:"channel"`
	Status        string     `json:"status"`
	StartDate     *time.Time `json:"start_date,omitempty"`
	EndDate       *time.Time `json:"end_date,omitempty"`
	Notes         string     `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	StakeholderName string `json:"stakeholder_name,omitempty"`
}

// SMSIssue is one row of sms_issues.
type SMSIssue struct {
	ID             int        `json:"id"`
	StakeholderID  int        `json:"stakeholder_id"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Priority       string     `json:"priority"`
	Impact         int        `json:"impact"`
	Likelihood     int        `json:"likelihood"`
	Status         string     `json:"status"`
	EscalationLevel int       `json:"escalation_level"`
	Mitigation     string     `json:"mitigation"`
	ResolutionNotes string    `json:"resolution_notes"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
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
