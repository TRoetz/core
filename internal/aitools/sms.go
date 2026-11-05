package aitools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/database"
	"windshift/internal/models"
)

// This file adds MCP/AI tools for the SMS (Stakeholder Management System)
// module: the stakeholder register with its computed health score and
// tier-based minimum-field completeness, the communications log, engagement
// plans, issues & risks, tasks, feedback, relationship goals, the commitment
// and grievance registers, the decision log, stakeholder groups, network
// links, ownership transfers, and P3M entity links. Access mirrors the HTTP
// surface: the global "sms.view" permission gates every read tool and
// "sms.manage" gates every write tool; restricted-classification fields are
// masked at tool level for users without "sms.sensitive.view" (WI-3.2).

// ---------------------------------------------------------------------------
// Column lists (mirror the sms_*_columns constants in internal/handlers).
// ---------------------------------------------------------------------------

const (
	aiSMSStakeholderColumns = `id, external_id, name, title, organization, email, phone, stakeholder_type, category, tier, business_unit, role_classification, influence_level, interest_level, interests, engagement_status, lifecycle_stage, relationship_status, trust_level, risk_rating, communication_preferences, owner_user_id, backup_owner_user_id, executive_sponsor_user_id, last_engagement_at, next_engagement_at, info_classification, consent_status, retention_category, last_reviewed_at, review_cadence, location_lat, location_lon, location_text, custom_fields, notes, created_at, updated_at`
	aiSMSCommunicationColumns = `id, stakeholder_id, channel, direction, subject, body, sentiment, created_by, created_at`
	aiSMSPlanColumns          = `id, stakeholder_id, strategy, objective, approach, key_messages, success_measures, frequency, channel, status, entity_type, entity_id, start_date, end_date, notes, created_at, updated_at`
	aiSMSIssueColumns         = `id, stakeholder_id, kind, category, title, description, priority, impact, likelihood, status, escalation_level, escalation_path, mitigation, resolution_notes, resolved_at, created_at, updated_at`
	aiSMSTaskColumns          = `id, stakeholder_id, title, description, assigned_to_user_id, due_date, status, completed_at, created_at, updated_at`
	aiSMSFeedbackColumns      = `id, stakeholder_id, source, category, rating, body, resolution_status, resolution_notes, created_at, updated_at`
	aiSMSGoalColumns          = `id, stakeholder_id, goal, metric, target_value, current_value, status, created_at, updated_at`
	aiSMSCommitmentColumns    = `id, stakeholder_id, direction, title, description, owner_user_id, due_date, status, escalated, escalation_level, source_communication_id, linked_issue_id, notes, created_at, updated_at`
	aiSMSGrievanceColumns     = `id, stakeholder_id, title, description, category, severity, status, escalated, escalation_level, acknowledged_at, response_due_at, response, resolution, created_by, created_at, updated_at`
	aiSMSDecisionColumns      = `id, stakeholder_id, title, decision, context, decided_at, decided_by_user_id, entity_type, entity_id, created_at`
	aiSMSGroupColumns         = `id, name, description, created_at`
	aiSMSTransferColumns      = `id, stakeholder_id, from_user_id, to_user_id, reason, history_reviewed, risks_reviewed, commitments_reviewed, notes, completed_at, created_by, created_at`
)

// ---------------------------------------------------------------------------
// Enum allowlists (mirror the sms* slices in internal/handlers/sms.go).
// ---------------------------------------------------------------------------

var (
	aiSMSRoleClassifications  = []string{models.SMSRoleKey, models.SMSRoleDecisionMaker, models.SMSRoleSupport, models.SMSRoleInfluencer, models.SMSRoleObserver}
	aiSMSLifecycleStages      = []string{models.SMSLifecycleOnboarding, models.SMSLifecycleActive, models.SMSLifecycleRetained, models.SMSLifecycleExited}
	aiSMSRelationshipStatuses = []string{models.SMSRelationshipAdvocate, models.SMSRelationshipSupportive, models.SMSRelationshipNeutral, models.SMSRelationshipConcerned, models.SMSRelationshipOpposed}
	aiSMSEngagementStatuses   = []string{models.SMSEngagementPassive, models.SMSEngagementEngaged, models.SMSEngagementActive, models.SMSEngagementDisengaged}
	aiSMSCadences             = []string{models.SMSReviewMonthly, models.SMSReviewQuarterly, models.SMSReviewSemiAnnual, models.SMSReviewAnnual}
	aiSMSTrustLevels          = []string{"very_high", "high", "moderate", "low", "critical"}
	aiSMSInfoClassifications  = []string{models.SMSInfoPublic, models.SMSInfoInternal, models.SMSInfoConfidential, models.SMSInfoRestricted}
	aiSMSConsentStatuses      = []string{models.SMSConsentNotRequired, models.SMSConsentGranted, models.SMSConsentDeclined, models.SMSConsentWithdrawn, models.SMSConsentUnknown}
	aiSMSTiers                = []string{models.SMSTier1, models.SMSTier2, models.SMSTier3, models.SMSTier4}
	aiSMSTypes                = []string{models.SMSTypeManaWhenua, models.SMSTypeElectedMember, models.SMSTypeGovernmentAgency, models.SMSTypeRegionalAuthority, models.SMSTypeRegulatoryPartner, models.SMSTypeInfrastructurePartner, models.SMSTypeBusiness, models.SMSTypeDeveloper, models.SMSTypeBusinessAssociation, models.SMSTypeStrategicSupplier, models.SMSTypeUtilityProvider, models.SMSTypeCommunityOrganisation, models.SMSTypeIndustryGroup, models.SMSTypeAdvocacyGroup, models.SMSTypeEducationalInstitution, models.SMSTypeMajorLandowner, models.SMSTypeProjectStakeholder, models.SMSTypeOther}
	aiSMSChannels             = []string{models.SMSChannelEmail, models.SMSChannelPhone, models.SMSChannelMeeting, models.SMSChannelInPerson, models.SMSChannelSlack, models.SMSChannelTeams, models.SMSChannelOther}
	aiSMSDirections           = []string{models.SMSDirectionOutbound, models.SMSDirectionInbound}
	aiSMSSentiments           = []string{models.SMSSentimentPositive, models.SMSSentimentNeutral, models.SMSSentimentNegative}
	aiSMSFrequencies          = []string{models.SMSFrequencyDaily, models.SMSFrequencyWeekly, models.SMSFrequencyBiweekly, models.SMSFrequencyMonthly, models.SMSFrequencyQuarterly, models.SMSFrequencyAnnual, models.SMSFrequencyAsNeeded}
	aiSMSPlanStatuses         = []string{models.SMSPlanStatusDraft, models.SMSPlanStatusActive, models.SMSPlanStatusPaused, models.SMSPlanStatusCompleted}
	aiSMSApproaches           = []string{models.SMSApproachInform, models.SMSApproachConsult, models.SMSApproachInvolve, models.SMSApproachCollaborate, models.SMSApproachPartner}
	aiSMSIssueKinds           = []string{models.SMSIssueKindIssue, models.SMSIssueKindRisk}
	aiSMSPriorities           = []string{models.SMSPriorityLow, models.SMSPriorityMedium, models.SMSPriorityHigh, models.SMSPriorityCritical}
	aiSMSIssueStatuses        = []string{models.SMSIssueStatusOpen, models.SMSIssueStatusInProgress, models.SMSIssueStatusEscalated, models.SMSIssueStatusResolved, models.SMSIssueStatusClosed}
	aiSMSIssueCategories      = []string{models.SMSCategoryRelationship, models.SMSCategoryReputation, models.SMSCategoryDelivery, models.SMSCategoryCommunication, models.SMSCategoryDependency, models.SMSCategoryKnowledgeLoss, models.SMSCategoryCommitment, models.SMSCategoryOther}
	aiSMSTaskStatuses         = []string{models.SMSTaskStatusOpen, models.SMSTaskStatusInProgress, models.SMSTaskStatusDone, models.SMSTaskStatusCancelled}
	aiSMSFeedbackSources      = []string{models.SMSFeedbackSourceForm, models.SMSFeedbackSourceSurvey, models.SMSFeedbackSourceEmail, models.SMSFeedbackSourceMeeting, models.SMSFeedbackSourceOther}
	aiSMSFeedbackResolutions  = []string{models.SMSFeedbackOpen, models.SMSFeedbackAddressed, models.SMSFeedbackResolved}
	aiSMSGoalStatuses         = []string{models.SMSGoalOnTrack, models.SMSGoalAtRisk, models.SMSGoalMissed, models.SMSGoalAchieved}
	aiSMSCommitmentDirections = []string{models.SMSCommitmentFromUs, models.SMSCommitmentFromThem, models.SMSCommitmentMutual}
	aiSMSCommitmentStatuses   = []string{models.SMSCommitmentOpen, models.SMSCommitmentInProgress, models.SMSCommitmentAtRisk, models.SMSCommitmentCompleted, models.SMSCommitmentCancelled}
	aiSMSGrievanceSeverities  = []string{models.SMSGrievanceLow, models.SMSGrievanceMedium, models.SMSGrievanceHigh, models.SMSGrievanceCritical}
	aiSMSGrievanceStatuses    = []string{models.SMSGrievanceReceived, models.SMSGrievanceAcknowledged, models.SMSGrievanceInvestigating, models.SMSGrievanceResponded, models.SMSGrievanceClosed}
)

// aiSMSEntityTables maps P3M entity types to their tables (mirrors
// handlers.smsP3MEntityTables).
var aiSMSEntityTables = map[string]string{
	models.SMSEntityPortfolio: "p3m_portfolios",
	models.SMSEntityProgramme: "p3m_programmes",
	models.SMSEntityProject:   "p3m_projects",
}

// ---------------------------------------------------------------------------
// Permission gates.
// ---------------------------------------------------------------------------

func aiRequireSMSView(env *Env) (bool, error) {
	ok, err := env.PermService.HasGlobalPermission(env.UserID, models.PermissionSMSView)
	return ok, err
}

func aiRequireSMSManage(env *Env) (bool, error) {
	ok, err := env.PermService.HasGlobalPermission(env.UserID, models.PermissionSMSManage)
	return ok, err
}

// aiSMSCanViewSensitive reports whether the caller may see
// restricted-classification fields (WI-3.2).
func aiSMSCanViewSensitive(ctx context.Context, env *Env) bool {
	ok, err := env.PermService.HasGlobalPermissionContext(ctx, env.UserID, models.PermissionSMSSensitiveView)
	return err == nil && ok
}

// aiSMSValidEnum reports whether value is one of the allowed enum values.
func aiSMSValidEnum(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// aiSMSTrimRequired trims a required text field and reports whether it is now
// empty (callers should surface a "X is required" error).
func aiSMSTrimRequired(v *string) {
	*v = strings.TrimSpace(*v)
}

// aiSMSLevelOrDefault maps 0 (omitted) to the schema default of 3.
func aiSMSLevelOrDefault(v int) any {
	if v == 0 {
		return 3
	}
	return v
}

// aiSMSLevelOr keeps the existing level when the update omits it.
func aiSMSLevelOr(v *int, fallback int) int {
	if v != nil {
		return *v
	}
	return fallback
}

// aiSMSDerefInt returns the pointed value or 0 for a nil pointer.
func aiSMSDerefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// aiSMSDerefStr returns the pointed value or "" for a nil pointer.
func aiSMSDerefStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// aiSMSOrKeep applies a column default when the caller omitted an enum field.
func aiSMSOrKeep(v, fallback string) any {
	if v == "" {
		return fallback
	}
	return v
}

// aiSMSKeepOr resolves a partial update: an explicit value wins, nil keeps
// the stored value.
func aiSMSKeepOr(v *string, fallback string) string {
	if v != nil {
		return *v
	}
	return fallback
}

// aiSMSApproachOr applies PATCH semantics to the nullable IAP2 approach
// column: nil keeps, empty string clears, otherwise the value wins.
func aiSMSApproachOr(v, fallback *string) any {
	if v == nil {
		return fallback
	}
	if *v == "" {
		return nil
	}
	return *v
}

// aiSMSRiskRatingOr applies PATCH semantics to the nullable risk rating.
func aiSMSRiskRatingOr(v, fallback *string) any {
	if v == nil {
		return fallback
	}
	if *v == "" {
		return nil
	}
	return *v
}

// aiSMSUserIntOr applies PATCH semantics to a nullable user reference: an
// explicit 0 clears the reference, omission keeps the stored one.
func aiSMSUserIntOr(v *int, fallback *int) any {
	if v == nil {
		return fallback
	}
	if *v == 0 {
		return nil
	}
	return *v
}

// aiSMSTimeOr resolves a nullable timestamp update: an explicit value wins;
// nil keeps the stored value (which may itself be NULL).
func aiSMSTimeOr(v *time.Time, fallback *time.Time) any {
	if v != nil {
		return *v
	}
	return fallback
}

// aiSMSFloatOr applies PATCH semantics to a nullable float metric value.
func aiSMSFloatOr(v *float64, fallback *float64) any {
	if v == nil {
		return fallback
	}
	return *v
}

// aiSMSJSONIsObject reports whether s parses as a JSON object.
func aiSMSJSONIsObject(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}

// aiSMSStakeholderExists reports whether a stakeholder row exists for id.
// DB errors are treated as absent: the subsequent INSERT surfaces them with a
// clearer message than a bare existence probe.
func aiSMSStakeholderExists(ctx context.Context, db database.Database, id int) bool {
	var n int
	return db.QueryRowContext(ctx, `SELECT 1 FROM sms_stakeholders WHERE id = ?`, id).Scan(&n) == nil
}

// aiSMSP3MEntityExists validates an entity_type/entity_id pair against the
// P3M tables. DB errors are treated as absent: the subsequent INSERT surfaces
// them with a clearer message than a bare existence probe.
func aiSMSP3MEntityExists(ctx context.Context, db database.Database, entityType string, entityID int) bool {
	table, ok := aiSMSEntityTables[entityType]
	if !ok || entityID <= 0 {
		return false
	}
	var n int
	return db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, entityID).Scan(&n) == nil
}

// aiSMSUserIDExists reports whether a user row exists for id.
func aiSMSUserIDExists(ctx context.Context, db database.Database, id int) bool {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// ---------------------------------------------------------------------------
// Scan helpers.
// ---------------------------------------------------------------------------

func aiScanSMSStakeholder(row interface{ Scan(...any) error }) (models.SMSStakeholder, error) {
	var s models.SMSStakeholder
	err := row.Scan(&s.ID, &s.ExternalID, &s.Name, &s.Title, &s.Organization, &s.Email, &s.Phone,
		&s.StakeholderType, &s.Category, &s.Tier, &s.BusinessUnit, &s.RoleClassification,
		&s.InfluenceLevel, &s.InterestLevel, &s.Interests, &s.EngagementStatus,
		&s.LifecycleStage, &s.RelationshipStatus, &s.TrustLevel, &s.RiskRating,
		&s.CommunicationPreferences, &s.OwnerUserID, &s.BackupOwnerUserID,
		&s.ExecutiveSponsorUserID, &s.LastEngagementAt, &s.NextEngagementAt,
		&s.InfoClassification, &s.ConsentStatus, &s.RetentionCategory, &s.LastReviewedAt,
		&s.ReviewCadence, &s.LocationLat, &s.LocationLon, &s.LocationText,
		&s.CustomFields, &s.Notes, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func aiScanSMSCommunication(row interface{ Scan(...any) error }) (models.SMSCommunication, error) {
	var c models.SMSCommunication
	err := row.Scan(&c.ID, &c.StakeholderID, &c.Channel, &c.Direction, &c.Subject, &c.Body,
		&c.Sentiment, &c.CreatedBy, &c.CreatedAt)
	return c, err
}

func aiScanSMSPlan(row interface{ Scan(...any) error }) (models.SMSEngagementPlan, error) {
	var p models.SMSEngagementPlan
	err := row.Scan(&p.ID, &p.StakeholderID, &p.Strategy, &p.Objective, &p.Approach,
		&p.KeyMessages, &p.SuccessMeasures, &p.Frequency, &p.Channel, &p.Status,
		&p.EntityType, &p.EntityID, &p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func aiScanSMSIssue(row interface{ Scan(...any) error }) (models.SMSIssue, error) {
	var i models.SMSIssue
	err := row.Scan(&i.ID, &i.StakeholderID, &i.Kind, &i.Category, &i.Title, &i.Description,
		&i.Priority, &i.Impact, &i.Likelihood, &i.Status, &i.EscalationLevel,
		&i.EscalationPath, &i.Mitigation, &i.ResolutionNotes, &i.ResolvedAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

func aiScanSMSTask(row interface{ Scan(...any) error }) (models.SMSTask, error) {
	var t models.SMSTask
	err := row.Scan(&t.ID, &t.StakeholderID, &t.Title, &t.Description, &t.AssignedToUserID,
		&t.DueDate, &t.Status, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func aiScanSMSFeedback(row interface{ Scan(...any) error }) (models.SMSFeedback, error) {
	var f models.SMSFeedback
	err := row.Scan(&f.ID, &f.StakeholderID, &f.Source, &f.Category, &f.Rating, &f.Body,
		&f.ResolutionStatus, &f.ResolutionNotes, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

func aiScanSMSGoal(row interface{ Scan(...any) error }) (models.SMSRelationshipGoal, error) {
	var g models.SMSRelationshipGoal
	err := row.Scan(&g.ID, &g.StakeholderID, &g.Goal, &g.Metric, &g.TargetValue, &g.CurrentValue,
		&g.Status, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func aiScanSMSCommitment(row interface{ Scan(...any) error }) (models.SMSCommitment, error) {
	var c models.SMSCommitment
	err := row.Scan(&c.ID, &c.StakeholderID, &c.Direction, &c.Title, &c.Description,
		&c.OwnerUserID, &c.DueDate, &c.Status, &c.Escalated, &c.EscalationLevel,
		&c.SourceCommunicationID, &c.LinkedIssueID, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func aiScanSMSGrievance(row interface{ Scan(...any) error }) (models.SMSGrievance, error) {
	var g models.SMSGrievance
	err := row.Scan(&g.ID, &g.StakeholderID, &g.Title, &g.Description, &g.Category, &g.Severity,
		&g.Status, &g.Escalated, &g.EscalationLevel, &g.AcknowledgedAt, &g.ResponseDueAt,
		&g.Response, &g.Resolution, &g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func aiScanSMSDecision(row interface{ Scan(...any) error }) (models.SMSDecision, error) {
	var d models.SMSDecision
	err := row.Scan(&d.ID, &d.StakeholderID, &d.Title, &d.Decision, &d.Context, &d.DecidedAt,
		&d.DecidedByUserID, &d.EntityType, &d.EntityID, &d.CreatedAt)
	return d, err
}

func aiScanSMSGroup(row interface{ Scan(...any) error }) (models.SMSStakeholderGroup, error) {
	var g models.SMSStakeholderGroup
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt)
	return g, err
}

func aiScanSMSTransfer(row interface{ Scan(...any) error }) (models.SMSOwnershipTransfer, error) {
	var t models.SMSOwnershipTransfer
	err := row.Scan(&t.ID, &t.StakeholderID, &t.FromUserID, &t.ToUserID, &t.Reason,
		&t.HistoryReviewed, &t.RisksReviewed, &t.CommitmentsReviewed, &t.Notes,
		&t.CompletedAt, &t.CreatedBy, &t.CreatedAt)
	return t, err
}

// aiSMSStakeholderName resolves a display name for joined list responses.
func aiSMSStakeholderName(ctx context.Context, db database.Database, id int) string {
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM sms_stakeholders WHERE id = ?`, id).Scan(&name); err != nil {
		return ""
	}
	return name
}

// ---------------------------------------------------------------------------
// Computed helpers (mirror handlers.smsHealthScore / smsFillCompleteness /
// smsIssuePressure / smsMaskRestricted).
// ---------------------------------------------------------------------------

// aiSMSHealthScore computes the 0-100 stakeholder health score: relationship
// base + engagement base minus penalties for open critical and escalated
// issues, clamped to [0,100].
func aiSMSHealthScore(relationship, engagement string, openCritical, escalatedOpen int) int {
	base := 0
	switch relationship {
	case models.SMSRelationshipAdvocate:
		base = 40
	case models.SMSRelationshipSupportive:
		base = 32
	case models.SMSRelationshipNeutral:
		base = 25
	case models.SMSRelationshipConcerned:
		base = 12
	case models.SMSRelationshipOpposed:
		base = 0
	}
	switch engagement {
	case models.SMSEngagementActive:
		base += 30
	case models.SMSEngagementEngaged:
		base += 20
	case models.SMSEngagementPassive:
		base += 10
	}
	score := base - 10*openCritical - 5*escalatedOpen
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// aiSMSFillCompleteness computes the register's minimum-field completeness
// flags for a stakeholder: every tier needs contact details (email or phone);
// tiers 1-2 additionally need title, an owner and a business unit; tier 1
// also needs a risk rating.
func aiSMSFillCompleteness(s *models.SMSStakeholder) {
	s.MissingFields = nil
	if s.Email == "" && s.Phone == "" {
		s.MissingFields = append(s.MissingFields, "email_or_phone")
	}
	switch s.Tier {
	case models.SMSTier1, models.SMSTier2:
		if s.Title == "" {
			s.MissingFields = append(s.MissingFields, "title")
		}
		if s.OwnerUserID == nil {
			s.MissingFields = append(s.MissingFields, "owner_user_id")
		}
		if s.BusinessUnit == "" {
			s.MissingFields = append(s.MissingFields, "business_unit")
		}
	}
	if s.Tier == models.SMSTier1 && (s.RiskRating == nil || *s.RiskRating == "") {
		s.MissingFields = append(s.MissingFields, "risk_rating")
	}
	s.MinimumFieldsComplete = len(s.MissingFields) == 0
}

// aiSMSIssuePressure returns per-stakeholder counts of open critical issues
// and escalated open issues. "Open" means status IN ('open','in_progress',
// 'escalated').
func aiSMSIssuePressure(ctx context.Context, db database.Database) (map[int]int, map[int]int, error) {
	critical := make(map[int]int)
	escalated := make(map[int]int)
	rows, err := db.QueryContext(ctx, `SELECT stakeholder_id, priority, status FROM sms_issues WHERE status IN ('open','in_progress','escalated')`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid int
		var priority, status string
		if err := rows.Scan(&sid, &priority, &status); err != nil {
			return nil, nil, err
		}
		if priority == models.SMSPriorityCritical {
			critical[sid]++
		}
		if status == models.SMSIssueStatusEscalated {
			escalated[sid]++
		}
	}
	return critical, escalated, rows.Err()
}

// aiSMSWithHealth fills the computed HealthScore + completeness flags of one
// stakeholder.
func aiSMSWithHealth(ctx context.Context, db database.Database, s *models.SMSStakeholder) error {
	var crit, esc int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN priority = 'critical' THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status = 'escalated' THEN 1 ELSE 0 END), 0) FROM sms_issues WHERE stakeholder_id = ? AND status IN ('open','in_progress','escalated')`, s.ID).Scan(&crit, &esc)
	if err != nil {
		return err
	}
	s.HealthScore = aiSMSHealthScore(s.RelationshipStatus, s.EngagementStatus, crit, esc)
	aiSMSFillCompleteness(s)
	return nil
}

// aiSMSEntityTitle resolves the P3M entity title for one link.
func aiSMSEntityTitle(ctx context.Context, db database.Database, entityType string, entityID int) string {
	table, ok := aiSMSEntityTables[entityType]
	if !ok {
		return ""
	}
	var title string
	if err := db.QueryRowContext(ctx, `SELECT title FROM `+table+` WHERE id = ?`, entityID).Scan(&title); err != nil {
		return ""
	}
	return title
}

// aiSMSP3MOptions returns the P3M entity pickers for linking stakeholders.
func aiSMSP3MOptions(ctx context.Context, db database.Database) (map[string][]map[string]any, error) {
	opts := func(table string) ([]map[string]any, error) {
		rows, err := db.QueryContext(ctx, `SELECT id, title FROM `+table+` ORDER BY title COLLATE NOCASE`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			var id int
			var title string
			if err := rows.Scan(&id, &title); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, map[string]any{"id": id, "title": title})
		}
		return out, rows.Err()
	}
	portfolios, err := opts("p3m_portfolios")
	if err != nil {
		return nil, err
	}
	programmes, err := opts("p3m_programmes")
	if err != nil {
		return nil, err
	}
	projects, err := opts("p3m_projects")
	if err != nil {
		return nil, err
	}
	return map[string][]map[string]any{
		"portfolios": portfolios,
		"programmes": programmes,
		"projects":   projects,
	}, nil
}

// aiSMSPlanMembers fills MemberIDs (lead + explicit membership rows) for a
// batch of plans from sms_engagement_plan_stakeholders.
func aiSMSPlanMembers(ctx context.Context, db database.Database, plans []models.SMSEngagementPlan) error {
	if len(plans) == 0 {
		return nil
	}
	ids := make([]any, 0, len(plans))
	ph := make([]string, len(plans))
	for i := range plans {
		ids = append(ids, plans[i].ID)
		ph[i] = "?"
	}
	rows, err := db.QueryContext(ctx, `SELECT plan_id, stakeholder_id FROM sms_engagement_plan_stakeholders WHERE plan_id IN (`+strings.Join(ph, ",")+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	members := make(map[int][]int)
	for rows.Next() {
		var pid, sid int
		if err := rows.Scan(&pid, &sid); err != nil {
			return err
		}
		members[pid] = append(members[pid], sid)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range plans {
		p := &plans[i]
		seen := map[int]bool{p.StakeholderID: true}
		p.MemberIDs = []int{p.StakeholderID}
		for _, sid := range members[p.ID] {
			if !seen[sid] {
				seen[sid] = true
				p.MemberIDs = append(p.MemberIDs, sid)
			}
		}
	}
	return nil
}

// aiSMSGetStakeholder loads one stakeholder with its computed health score.
func aiSMSGetStakeholder(ctx context.Context, db database.Database, id int) (models.SMSStakeholder, error) {
	var s models.SMSStakeholder
	err := db.QueryRowContext(ctx, `SELECT `+aiSMSStakeholderColumns+` FROM sms_stakeholders WHERE id = ?`, id).
		Scan(&s.ID, &s.ExternalID, &s.Name, &s.Title, &s.Organization, &s.Email, &s.Phone,
			&s.StakeholderType, &s.Category, &s.Tier, &s.BusinessUnit, &s.RoleClassification,
			&s.InfluenceLevel, &s.InterestLevel, &s.Interests, &s.EngagementStatus,
			&s.LifecycleStage, &s.RelationshipStatus, &s.TrustLevel, &s.RiskRating,
			&s.CommunicationPreferences, &s.OwnerUserID, &s.BackupOwnerUserID,
			&s.ExecutiveSponsorUserID, &s.LastEngagementAt, &s.NextEngagementAt,
			&s.InfoClassification, &s.ConsentStatus, &s.RetentionCategory, &s.LastReviewedAt,
			&s.ReviewCadence, &s.LocationLat, &s.LocationLon, &s.LocationText,
			&s.CustomFields, &s.Notes, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	if err := aiSMSWithHealth(ctx, db, &s); err != nil {
		return s, err
	}
	return s, nil
}

// aiSMSApplySensitivity masks restricted-classification fields for callers
// without sms.sensitive.view (WI-3.2). It reports whether masking was applied.
func aiSMSApplySensitivity(s *models.SMSStakeholder) bool {
	if s.InfoClassification != models.SMSInfoRestricted {
		return false
	}
	s.Notes = ""
	s.CustomFields = "{}"
	return true
}



// ---------------------------------------------------------------------------
// Read tool args.
// ---------------------------------------------------------------------------

type getSMSDashboardArgs struct {
	Scope      string `json:"scope,omitempty" jsonschema:"Optional scope: executive (default) or a P3M entity."`
	EntityType string `json:"entity_type,omitempty" jsonschema:"Scope entity type: portfolio, programme, or project."`
	EntityID   int    `json:"entity_id,omitempty" jsonschema:"Scope entity ID (with entity_type)."`
}

type listSMSStakeholdersArgs struct {
	Search             string `json:"search,omitempty" jsonschema:"Substring match on name, organization, or email."`
	Classification     string `json:"classification,omitempty" jsonschema:"Role classification: key, decision_maker, support, influencer, observer."`
	EngagementStatus   string `json:"engagement_status,omitempty" jsonschema:"Engagement status: passive, engaged, active, disengaged."`
	LifecycleStage     string `json:"lifecycle_stage,omitempty" jsonschema:"Lifecycle stage: onboarding, active, retained, exited."`
	RelationshipStatus string `json:"relationship_status,omitempty" jsonschema:"Relationship status: advocate, supportive, neutral, concerned, opposed."`
	Tier               string `json:"tier,omitempty" jsonschema:"Materiality tier: tier_1, tier_2, tier_3, tier_4."`
	GroupID            int    `json:"group_id,omitempty" jsonschema:"Restrict to members of one stakeholder group."`
	Limit              int    `json:"limit,omitempty" jsonschema:"Max rows (default 200, max 500)."`
	Offset             int    `json:"offset,omitempty" jsonschema:"Offset for pagination."`
}

type getSMSStakeholderArgs struct {
	StakeholderID int `json:"stakeholder_id" jsonschema:"Stakeholder ID."`
}

type getSMSTimelineArgs struct {
	StakeholderID int `json:"stakeholder_id" jsonschema:"Stakeholder ID."`
	Limit         int `json:"limit,omitempty" jsonschema:"Max timeline entries (default 50, max 200)."`
}

type listSMSCommunicationsArgs struct {
	StakeholderID int `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for the full log)."`
	Limit         int `json:"limit,omitempty" jsonschema:"Max rows (default 200, max 500)."`
}

type listSMSEngagementPlansArgs struct {
	StakeholderID int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for all plans)."`
	Status        string `json:"status,omitempty" jsonschema:"Status: draft, active, paused, completed."`
}

type listSMSIssuesArgs struct {
	StakeholderID int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for the full register)."`
	Kind          string `json:"kind,omitempty" jsonschema:"Kind: issue or risk."`
	Category      string `json:"category,omitempty" jsonschema:"Category: relationship, reputation, delivery, communication, dependency, knowledge_loss, commitment, other."`
	Priority      string `json:"priority,omitempty" jsonschema:"Priority: low, medium, high, critical."`
	Status        string `json:"status,omitempty" jsonschema:"Status: open, in_progress, escalated, resolved, closed."`
}

type listSMSTasksArgs struct {
	StakeholderID int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for all tasks)."`
	Status        string `json:"status,omitempty" jsonschema:"Status: open, in_progress, done, cancelled."`
}

type listSMSFeedbackArgs struct {
	StakeholderID    int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for all feedback)."`
	ResolutionStatus string `json:"resolution_status,omitempty" jsonschema:"Resolution: open, addressed, resolved."`
}

type listSMSGoalsArgs struct {
	StakeholderID int `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for all goals)."`
	Status        string `json:"status,omitempty" jsonschema:"Status: on_track, at_risk, missed, achieved."`
}

type listSMSCommitmentsArgs struct {
	StakeholderID int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for the full register)."`
	Direction     string `json:"direction,omitempty" jsonschema:"Direction: from_us, from_them, mutual."`
	Status        string `json:"status,omitempty" jsonschema:"Status: open, in_progress, at_risk, completed, cancelled."`
}

type listSMSGrievancesArgs struct {
	StakeholderID int    `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for the full register)."`
	Status        string `json:"status,omitempty" jsonschema:"Status: received, acknowledged, investigating, responded, closed."`
}

type listSMSDecisionsArgs struct {
	StakeholderID int `json:"stakeholder_id,omitempty" jsonschema:"Restrict to one stakeholder (omit for the full log)."`
}

type listSMSGroupsArgs struct {
	IncludeMembers bool `json:"include_members,omitempty" jsonschema:"Include each group's member list (default: counts only)."`
}

type getSMSP3MOptionsArgs struct{}

// ---------------------------------------------------------------------------
// Write tool args.
// ---------------------------------------------------------------------------

type createSMSStakeholderArgs struct {
	Name                     string     `json:"name" jsonschema:"Stakeholder display name (required)." `
	Title                    string     `json:"title,omitempty" jsonschema:"Job title."`
	Organization             string     `json:"organization,omitempty" jsonschema:"Organisation."`
	Email                    string     `json:"email,omitempty" jsonschema:"Email address."`
	Phone                    string     `json:"phone,omitempty" jsonschema:"Phone number."`
	ExternalID               *string    `json:"external_id,omitempty" jsonschema:"Stable external identifier for integrations."`
	StakeholderType          *string    `json:"stakeholder_type,omitempty" jsonschema:"Register taxonomy type (e.g. government_agency, business, mana_whenua)." `
	Category                 string     `json:"category,omitempty" jsonschema:"Free-text category."`
	Tier                     string     `json:"tier,omitempty" jsonschema:"Materiality tier: tier_1, tier_2, tier_3, tier_4 (default tier_3)." `
	BusinessUnit             string     `json:"business_unit,omitempty" jsonschema:"Business unit."`
	RoleClassification       string     `json:"role_classification,omitempty" jsonschema:"Role: key, decision_maker, support, influencer, observer (default support)." `
	InfluenceLevel           int        `json:"influence_level,omitempty" jsonschema:"Influence 1-5 (default 3)." `
	InterestLevel            int        `json:"interest_level,omitempty" jsonschema:"Interest 1-5 (default 3)." `
	Interests                string     `json:"interests,omitempty" jsonschema:"What the stakeholder cares about."`
	EngagementStatus         string     `json:"engagement_status,omitempty" jsonschema:"passive (default), engaged, active, disengaged."`
	LifecycleStage           string     `json:"lifecycle_stage,omitempty" jsonschema:"onboarding (default), active, retained, exited."`
	RelationshipStatus       string     `json:"relationship_status,omitempty" jsonschema:"advocate, supportive, neutral (default), concerned, opposed." `
	TrustLevel               string     `json:"trust_level,omitempty" jsonschema:"very_high, high, moderate (default), low, critical." `
	RiskRating               *string    `json:"risk_rating,omitempty" jsonschema:"low, medium, high, critical."`
	CommunicationPreferences string     `json:"communication_preferences,omitempty" jsonschema:"Preferred channels and cadence."`
	OwnerUserID              *int       `json:"owner_user_id,omitempty" jsonschema:"Primary owner user ID."`
	BackupOwnerUserID        *int       `json:"backup_owner_user_id,omitempty" jsonschema:"Backup owner user ID."`
	ExecutiveSponsorUserID   *int       `json:"executive_sponsor_user_id,omitempty" jsonschema:"Executive sponsor user ID."`
	LastEngagementAt         *time.Time `json:"last_engagement_at,omitempty" jsonschema:"RFC3339 timestamp of last engagement." `
	NextEngagementAt         *time.Time `json:"next_engagement_at,omitempty" jsonschema:"RFC3339 timestamp of next planned engagement." `
	InfoClassification       string     `json:"info_classification,omitempty" jsonschema:"public, internal (default), confidential, restricted." `
	ConsentStatus            string     `json:"consent_status,omitempty" jsonschema:"not_required (default), granted, declined, withdrawn, unknown." `
	RetentionCategory        string     `json:"retention_category,omitempty" jsonschema:"Retention category."`
	LastReviewedAt           *time.Time `json:"last_reviewed_at,omitempty" jsonschema:"RFC3339 timestamp of last review." `
	ReviewCadence            string     `json:"review_cadence,omitempty" jsonschema:"monthly, quarterly (default), semi_annual, annual." `
	LocationLat              *float64   `json:"location_lat,omitempty" jsonschema:"Latitude -90..90."`
	LocationLon              *float64   `json:"location_lon,omitempty" jsonschema:"Longitude -180..180."`
	LocationText             string     `json:"location_text,omitempty" jsonschema:"Free-text location."`
	CustomFields             string     `json:"custom_fields,omitempty" jsonschema:"JSON object of custom field values."`
	Notes                    string     `json:"notes,omitempty" jsonschema:"Free-text notes."`
}

type updateSMSStakeholderArgs struct {
	StakeholderID            int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)." `
	Title                    *string    `json:"title,omitempty" jsonschema:"Job title."`
	Organization             *string    `json:"organization,omitempty" jsonschema:"Organisation."`
	Email                    *string    `json:"email,omitempty" jsonschema:"Email address."`
	Phone                    *string    `json:"phone,omitempty" jsonschema:"Phone number."`
	Tier                     *string    `json:"tier,omitempty" jsonschema:"Materiality tier: tier_1..tier_4." `
	RoleClassification       *string    `json:"role_classification,omitempty" jsonschema:"Role: key, decision_maker, support, influencer, observer." `
	InfluenceLevel           *int       `json:"influence_level,omitempty" jsonschema:"Influence 1-5."`
	InterestLevel            *int       `json:"interest_level,omitempty" jsonschema:"Interest 1-5."`
	EngagementStatus         *string    `json:"engagement_status,omitempty" jsonschema:"passive, engaged, active, disengaged."`
	LifecycleStage           *string    `json:"lifecycle_stage,omitempty" jsonschema:"onboarding, active, retained, exited."`
	RelationshipStatus       *string    `json:"relationship_status,omitempty" jsonschema:"advocate, supportive, neutral, concerned, opposed." `
	TrustLevel               *string    `json:"trust_level,omitempty" jsonschema:"very_high, high, moderate, low, critical." `
	RiskRating               *string    `json:"risk_rating,omitempty" jsonschema:"low, medium, high, critical; empty string clears." `
	OwnerUserID              *int       `json:"owner_user_id,omitempty" jsonschema:"Primary owner user ID; 0 clears."`
	BackupOwnerUserID        *int       `json:"backup_owner_user_id,omitempty" jsonschema:"Backup owner user ID; 0 clears."`
	ExecutiveSponsorUserID   *int       `json:"executive_sponsor_user_id,omitempty" jsonschema:"Executive sponsor user ID; 0 clears." `
	NextEngagementAt         *time.Time `json:"next_engagement_at,omitempty" jsonschema:"RFC3339 timestamp of next planned engagement." `
	LastReviewedAt           *time.Time `json:"last_reviewed_at,omitempty" jsonschema:"RFC3339 timestamp of last review." `
	ReviewCadence            *string    `json:"review_cadence,omitempty" jsonschema:"monthly, quarterly, semi_annual, annual." `
	LocationText             *string    `json:"location_text,omitempty" jsonschema:"Free-text location."`
	Notes                    *string    `json:"notes,omitempty" jsonschema:"Free-text notes."`
}

type createSMSCommunicationArgs struct {
	StakeholderID int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)." `
	Channel       string     `json:"channel,omitempty" jsonschema:"email (default), phone, meeting, in_person, slack, teams, other." `
	Direction     string     `json:"direction,omitempty" jsonschema:"outbound (default) or inbound." `
	Subject       string     `json:"subject,omitempty" jsonschema:"Subject line."`
	Body          string     `json:"body,omitempty" jsonschema:"Message body / summary."`
	Sentiment     *string    `json:"sentiment,omitempty" jsonschema:"positive, neutral, negative."`
}


type createSMSEngagementPlanArgs struct {
	StakeholderID   int        `json:"stakeholder_id" jsonschema:"Lead stakeholder ID (required)."`
	Strategy        string     `json:"strategy" jsonschema:"Engagement strategy (required)."`
	Objective       string     `json:"objective,omitempty" jsonschema:"Objective the plan pursues."`
	Approach        *string    `json:"approach,omitempty" jsonschema:"IAP2 spectrum: inform, consult, involve, collaborate, partner."`
	KeyMessages     string     `json:"key_messages,omitempty" jsonschema:"Key messages to convey."`
	SuccessMeasures string     `json:"success_measures,omitempty" jsonschema:"How success is measured."`
	Frequency       string     `json:"frequency,omitempty" jsonschema:"daily, weekly, biweekly, monthly (default), quarterly, annual, as_needed."`
	Channel         string     `json:"channel,omitempty" jsonschema:"email (default), phone, meeting, in_person, slack, teams, other."`
	Status          string     `json:"status,omitempty" jsonschema:"draft, active (default), paused, completed."`
	EntityType      *string    `json:"entity_type,omitempty" jsonschema:"P3M entity type: portfolio, programme, project."`
	EntityID        *int       `json:"entity_id,omitempty" jsonschema:"P3M entity ID (with entity_type)."`
	StartDate       *time.Time `json:"start_date,omitempty" jsonschema:"RFC3339 start date."`
	EndDate         *time.Time `json:"end_date,omitempty" jsonschema:"RFC3339 end date."`
	MemberIDs       []int      `json:"member_ids,omitempty" jsonschema:"Additional stakeholder IDs on the plan (lead is implicit)."`
	Notes           string     `json:"notes,omitempty" jsonschema:"Notes."`
}

type updateSMSEngagementPlanArgs struct {
	PlanID          int        `json:"plan_id" jsonschema:"Engagement plan ID (required)."`
	Strategy        *string    `json:"strategy,omitempty" jsonschema:"Engagement strategy."`
	Objective       *string    `json:"objective,omitempty" jsonschema:"Objective."`
	Approach        *string    `json:"approach,omitempty" jsonschema:"IAP2 spectrum: inform, consult, involve, collaborate, partner."`
	KeyMessages     *string    `json:"key_messages,omitempty" jsonschema:"Key messages."`
	SuccessMeasures *string    `json:"success_measures,omitempty" jsonschema:"Success measures."`
	Frequency       *string    `json:"frequency,omitempty" jsonschema:"Engagement frequency."`
	Channel         *string    `json:"channel,omitempty" jsonschema:"Channel."`
	Status          *string    `json:"status,omitempty" jsonschema:"draft, active, paused, completed."`
	StartDate       *time.Time `json:"start_date,omitempty" jsonschema:"RFC3339 start date."`
	EndDate         *time.Time `json:"end_date,omitempty" jsonschema:"RFC3339 end date."`
	Notes           *string    `json:"notes,omitempty" jsonschema:"Notes."`
	MemberIDs       []int      `json:"member_ids,omitempty" jsonschema:"Replace the plan's full member list (lead always retained)."`
}

type createSMSIssueArgs struct {
	StakeholderID  int    `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Kind           string `json:"kind,omitempty" jsonschema:"issue (default) or risk."`
	Category       string `json:"category,omitempty" jsonschema:"relationship, reputation, delivery, communication, dependency, knowledge_loss, commitment, other (default other)."`
	Title          string `json:"title" jsonschema:"Short title (required)."`
	Description    string `json:"description,omitempty" jsonschema:"Detail."`
	Priority       string `json:"priority,omitempty" jsonschema:"low, medium (default), high, critical."`
	Impact         int    `json:"impact,omitempty" jsonschema:"Impact 1-5 (default 3)."`
	Likelihood     int    `json:"likelihood,omitempty" jsonschema:"Likelihood 1-5 (default 3)."`
	Status         string `json:"status,omitempty" jsonschema:"open (default), in_progress, escalated, resolved, closed."`
	EscalationPath string `json:"escalation_path,omitempty" jsonschema:"Escalation path (e.g. project to programme to business unit to enterprise)."`
	Mitigation     string `json:"mitigation,omitempty" jsonschema:"Mitigation / response plan."`
}

type updateSMSIssueArgs struct {
	IssueID         int     `json:"issue_id" jsonschema:"Issue/risk ID (required)."`
	Kind            *string `json:"kind,omitempty" jsonschema:"issue or risk."`
	Category        *string `json:"category,omitempty" jsonschema:"Issue category."`
	Title           *string `json:"title,omitempty" jsonschema:"Short title."`
	Description     *string `json:"description,omitempty" jsonschema:"Detail."`
	Priority        *string `json:"priority,omitempty" jsonschema:"low, medium, high, critical."`
	Impact          *int    `json:"impact,omitempty" jsonschema:"Impact 1-5."`
	Likelihood      *int    `json:"likelihood,omitempty" jsonschema:"Likelihood 1-5."`
	Status          *string `json:"status,omitempty" jsonschema:"open, in_progress, escalated, resolved, closed (moving to resolved stamps resolved_at)."`
	EscalationPath  *string `json:"escalation_path,omitempty" jsonschema:"Escalation path."`
	Mitigation      *string `json:"mitigation,omitempty" jsonschema:"Mitigation / response plan."`
	ResolutionNotes *string `json:"resolution_notes,omitempty" jsonschema:"How it was resolved."`
}

type escalateSMSIssueArgs struct {
	IssueID        int    `json:"issue_id" jsonschema:"Issue/risk ID (required)."`
	EscalationPath string `json:"escalation_path,omitempty" jsonschema:"Where the item sits on the escalation path."`
}

type createSMSTaskArgs struct {
	StakeholderID    int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Title            string     `json:"title" jsonschema:"Task title (required)."`
	Description      string     `json:"description,omitempty" jsonschema:"Detail."`
	AssignedToUserID *int       `json:"assigned_to_user_id,omitempty" jsonschema:"Assignee user ID."`
	DueDate          *time.Time `json:"due_date,omitempty" jsonschema:"RFC3339 due date."`
}

type updateSMSTaskArgs struct {
	TaskID           int        `json:"task_id" jsonschema:"Task ID (required)."`
	Title            *string    `json:"title,omitempty" jsonschema:"Task title."`
	Description      *string    `json:"description,omitempty" jsonschema:"Detail."`
	AssignedToUserID *int       `json:"assigned_to_user_id,omitempty" jsonschema:"Assignee user ID; 0 clears."`
	DueDate          *time.Time `json:"due_date,omitempty" jsonschema:"RFC3339 due date."`
	Status           *string    `json:"status,omitempty" jsonschema:"open, in_progress, done (stamps completed_at), cancelled."`
}

type createSMSFeedbackArgs struct {
	StakeholderID    int    `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Source           string `json:"source,omitempty" jsonschema:"form, survey, email, meeting, other (default other)."`
	Category         string `json:"category,omitempty" jsonschema:"Free-text category."`
	Rating           *int   `json:"rating,omitempty" jsonschema:"Numeric rating."`
	Body             string `json:"body,omitempty" jsonschema:"Feedback text."`
	ResolutionStatus string `json:"resolution_status,omitempty" jsonschema:"open (default), addressed, resolved."`
}

type createSMSGoalArgs struct {
	StakeholderID int      `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Goal          string   `json:"goal" jsonschema:"Relationship goal (required)."`
	Metric        string   `json:"metric,omitempty" jsonschema:"Metric that tracks the goal."`
	TargetValue   *float64 `json:"target_value,omitempty" jsonschema:"Target value for the metric."`
	CurrentValue  *float64 `json:"current_value,omitempty" jsonschema:"Current value of the metric."`
	Status        string   `json:"status,omitempty" jsonschema:"on_track (default), at_risk, missed, achieved."`
}

type updateSMSGoalArgs struct {
	GoalID       int       `json:"goal_id" jsonschema:"Goal ID (required)."`
	Goal         *string   `json:"goal,omitempty" jsonschema:"Relationship goal."`
	Metric       *string   `json:"metric,omitempty" jsonschema:"Metric."`
	TargetValue  *float64  `json:"target_value,omitempty" jsonschema:"Target value."`
	CurrentValue *float64  `json:"current_value,omitempty" jsonschema:"Current value."`
	Status       *string   `json:"status,omitempty" jsonschema:"on_track, at_risk, missed, achieved."`
}

type createSMSCommitmentArgs struct {
	StakeholderID         int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Direction             string     `json:"direction,omitempty" jsonschema:"from_us, from_them (default), mutual."`
	Title                 string     `json:"title" jsonschema:"Commitment title (required)."`
	Description           string     `json:"description,omitempty" jsonschema:"Detail."`
	OwnerUserID           *int       `json:"owner_user_id,omitempty" jsonschema:"Owner user ID."`
	DueDate               *time.Time `json:"due_date,omitempty" jsonschema:"RFC3339 due date."`
	Status                string     `json:"status,omitempty" jsonschema:"open (default), in_progress, at_risk, completed, cancelled."`
	SourceCommunicationID *int       `json:"source_communication_id,omitempty" jsonschema:"Communication the commitment came from."`
	LinkedIssueID         *int       `json:"linked_issue_id,omitempty" jsonschema:"Related SMS issue/risk ID."`
	Notes                 string     `json:"notes,omitempty" jsonschema:"Notes."`
}

type updateSMSCommitmentArgs struct {
	CommitmentID int        `json:"commitment_id" jsonschema:"Commitment ID (required)."`
	Direction    *string    `json:"direction,omitempty" jsonschema:"from_us, from_them, mutual."`
	Title        *string    `json:"title,omitempty" jsonschema:"Commitment title."`
	Description  *string    `json:"description,omitempty" jsonschema:"Detail."`
	OwnerUserID  *int       `json:"owner_user_id,omitempty" jsonschema:"Owner user ID; 0 clears."`
	DueDate      *time.Time `json:"due_date,omitempty" jsonschema:"RFC3339 due date."`
	Status       *string    `json:"status,omitempty" jsonschema:"open, in_progress, at_risk, completed, cancelled."`
	Notes        *string    `json:"notes,omitempty" jsonschema:"Notes."`
}

type escalateSMSCommitmentArgs struct {
	CommitmentID int `json:"commitment_id" jsonschema:"Commitment ID (required)."`
}

type createSMSGrievanceArgs struct {
	StakeholderID int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Title         string     `json:"title" jsonschema:"Grievance title (required)."`
	Description   string     `json:"description,omitempty" jsonschema:"Detail."`
	Category      string     `json:"category,omitempty" jsonschema:"Free-text category."`
	Severity      string     `json:"severity,omitempty" jsonschema:"low, medium (default), high, critical."`
	Status        string     `json:"status,omitempty" jsonschema:"received (default), acknowledged, investigating, responded, closed."`
	ResponseDueAt *time.Time `json:"response_due_at,omitempty" jsonschema:"RFC3339 response deadline."`
}

type updateSMSGrievanceArgs struct {
	GrievanceID   int        `json:"grievance_id" jsonschema:"Grievance ID (required)."`
	Title         *string    `json:"title,omitempty" jsonschema:"Grievance title."`
	Description   *string    `json:"description,omitempty" jsonschema:"Detail."`
	Category      *string    `json:"category,omitempty" jsonschema:"Category."`
	Severity      *string    `json:"severity,omitempty" jsonschema:"low, medium, high, critical."`
	Status        *string    `json:"status,omitempty" jsonschema:"received, acknowledged (stamps acknowledged_at), investigating, responded, closed."`
	ResponseDueAt *time.Time `json:"response_due_at,omitempty" jsonschema:"RFC3339 response deadline."`
	Response      *string    `json:"response,omitempty" jsonschema:"Response given."`
	Resolution    *string    `json:"resolution,omitempty" jsonschema:"How it was resolved."`
}

type escalateSMSGrievanceArgs struct {
	GrievanceID int `json:"grievance_id" jsonschema:"Grievance ID (required)."`
}

type createSMSDecisionArgs struct {
	StakeholderID int        `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	Title         string     `json:"title" jsonschema:"Decision title (required)."`
	Decision      string     `json:"decision,omitempty" jsonschema:"The decision itself."`
	Context       string     `json:"context,omitempty" jsonschema:"Context behind the decision."`
	DecidedAt     *time.Time `json:"decided_at,omitempty" jsonschema:"RFC3339 decision date (defaults to now)."`
	EntityType    *string    `json:"entity_type,omitempty" jsonschema:"P3M entity type: portfolio, programme, project."`
	EntityID      *int       `json:"entity_id,omitempty" jsonschema:"P3M entity ID (with entity_type)."`
}

type createSMSGroupArgs struct {
	Name           string `json:"name" jsonschema:"Group name (required, unique)."`
	Description    string `json:"description,omitempty" jsonschema:"Group description."`
	StakeholderIDs []int  `json:"stakeholder_ids,omitempty" jsonschema:"Initial member stakeholder IDs."`
}

type addSMSGroupMemberArgs struct {
	GroupID       int `json:"group_id" jsonschema:"Stakeholder group ID (required)."`
	StakeholderID int `json:"stakeholder_id" jsonschema:"Stakeholder ID to add (required)."`
}

type addSMSStakeholderEntityArgs struct {
	StakeholderID int    `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	EntityType    string `json:"entity_type" jsonschema:"P3M entity type: portfolio, programme, project (required)."`
	EntityID      int    `json:"entity_id" jsonschema:"P3M entity ID (required)."`
}

type createSMSTransferArgs struct {
	StakeholderID     int    `json:"stakeholder_id" jsonschema:"Stakeholder ID (required)."`
	ToUserID          *int   `json:"to_user_id,omitempty" jsonschema:"Receiving user ID."`
	Reason            string `json:"reason,omitempty" jsonschema:"Reason for the handover."`
	HistoryReviewed   bool   `json:"history_reviewed,omitempty" jsonschema:"Handover checklist: history reviewed."`
	RisksReviewed     bool   `json:"risks_reviewed,omitempty" jsonschema:"Handover checklist: risks reviewed."`
	CommitmentsReviewed bool `json:"commitments_reviewed,omitempty" jsonschema:"Handover checklist: commitments reviewed. All three flags complete the transfer and move ownership."`
	Notes             string `json:"notes,omitempty" jsonschema:"Notes."`
}

func init() {
	// ------------------------------------------------------------------------
	// get_sms_dashboard
	// ------------------------------------------------------------------------
	Register(Default, Tool[getSMSDashboardArgs]{
		Name:        "get_sms_dashboard",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get the SMS module overview: stakeholder totals, open issue/task/feedback counts, relationship and engagement distributions, health-score buckets, and the influence/interest matrix.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, _ getSMSDashboardArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}

			var totals struct {
				Stakeholders int `json:"stakeholders"`
				OpenIssues   int `json:"open_issues"`
				OpenTasks    int `json:"open_tasks"`
				OpenFeedback int `json:"open_feedback"`
			}
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders`).Scan(&totals.Stakeholders); err != nil {
				return nil, err
			}
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE status IN ('open','in_progress','escalated')`).Scan(&totals.OpenIssues); err != nil {
				return nil, err
			}
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_tasks WHERE status IN ('open','in_progress')`).Scan(&totals.OpenTasks); err != nil {
				return nil, err
			}
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_feedback WHERE resolution_status = 'open'`).Scan(&totals.OpenFeedback); err != nil {
				return nil, err
			}

			counts := func(query string) ([]map[string]any, error) {
				rows, err := env.DB.QueryContext(ctx, query)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				out := make([]map[string]any, 0)
				for rows.Next() {
					var value string
					var count int
					if err := rows.Scan(&value, &count); err != nil {
						return nil, err
					}
					out = append(out, map[string]any{"value": value, "count": count})
				}
				return out, rows.Err()
			}
			byClassification, err := counts(`SELECT role_classification, COUNT(*) FROM sms_stakeholders GROUP BY role_classification ORDER BY COUNT(*) DESC, role_classification`)
			if err != nil {
				return nil, err
			}
			byEngagement, err := counts(`SELECT engagement_status, COUNT(*) FROM sms_stakeholders GROUP BY engagement_status ORDER BY COUNT(*) DESC, engagement_status`)
			if err != nil {
				return nil, err
			}
			byLifecycle, err := counts(`SELECT lifecycle_stage, COUNT(*) FROM sms_stakeholders GROUP BY lifecycle_stage ORDER BY COUNT(*) DESC, lifecycle_stage`)
			if err != nil {
				return nil, err
			}
			relationshipDist, err := counts(`SELECT relationship_status, COUNT(*) FROM sms_stakeholders GROUP BY relationship_status ORDER BY COUNT(*) DESC, relationship_status`)
			if err != nil {
				return nil, err
			}
			openByPriority, err := counts(`SELECT priority, COUNT(*) FROM sms_issues WHERE status IN ('open','in_progress','escalated') GROUP BY priority ORDER BY COUNT(*) DESC, priority`)
			if err != nil {
				return nil, err
			}

			critical, escalated, err := aiSMSIssuePressure(ctx, env.DB)
			if err != nil {
				return nil, err
			}
			buckets := []map[string]any{
				{"bucket": "0-25", "count": 0},
				{"bucket": "26-50", "count": 0},
				{"bucket": "51-75", "count": 0},
				{"bucket": "76-100", "count": 0},
			}
			matrix := make([]map[string]any, 0)
			mrows, err := env.DB.QueryContext(ctx, `SELECT id, name, influence_level, interest_level, relationship_status, engagement_status FROM sms_stakeholders ORDER BY id`)
			if err != nil {
				return nil, err
			}
			for mrows.Next() {
				var id, influence, interest int
				var name, relationship, engagement string
				if err := mrows.Scan(&id, &name, &influence, &interest, &relationship, &engagement); err != nil {
					mrows.Close()
					return nil, err
				}
				score := aiSMSHealthScore(relationship, engagement, critical[id], escalated[id])
				idx := 0
				if score > 0 {
					idx = (score - 1) / 25
				}
				if idx > 3 {
					idx = 3
				}
				buckets[idx]["count"] = buckets[idx]["count"].(int) + 1
				matrix = append(matrix, map[string]any{"stakeholder_id": id, "name": name, "influence_level": influence, "interest_level": interest, "health_score": score})
			}
			if err := mrows.Err(); err != nil {
				mrows.Close()
				return nil, err
			}
			mrows.Close()

			return map[string]any{
				"totals":                    totals,
				"by_classification":         byClassification,
				"by_engagement_status":      byEngagement,
				"by_lifecycle_stage":        byLifecycle,
				"relationship_distribution": relationshipDist,
				"health_buckets":            buckets,
				"open_issues_by_priority":   openByPriority,
				"matrix":                    matrix,
			}, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_stakeholders
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSStakeholdersArgs]{
		Name:        "list_sms_stakeholders",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS stakeholders with computed health scores and register minimum-field completeness. Filter by search, classification, engagement, lifecycle, relationship, tier, or group.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSStakeholdersArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSStakeholderColumns + ` FROM sms_stakeholders`
			var conds []string
			var qArgs []any
			if v := strings.TrimSpace(args.Classification); v != "" {
				conds = append(conds, "role_classification = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.EngagementStatus); v != "" {
				conds = append(conds, "engagement_status = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.LifecycleStage); v != "" {
				conds = append(conds, "lifecycle_stage = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.RelationshipStatus); v != "" {
				conds = append(conds, "relationship_status = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.Tier); v != "" {
				conds = append(conds, "tier = ?")
				qArgs = append(qArgs, v)
			}
			if args.GroupID > 0 {
				conds = append(conds, `id IN (SELECT stakeholder_id FROM sms_stakeholder_group_members WHERE group_id = ?)`)
				qArgs = append(qArgs, args.GroupID)
			}
			if v := strings.TrimSpace(args.Search); v != "" {
				like := "%" + v + "%"
				conds = append(conds, "(name LIKE ? OR organization LIKE ? OR email LIKE ?)")
				qArgs = append(qArgs, like, like, like)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY name COLLATE NOCASE`
			limit := args.Limit
			if limit <= 0 {
				limit = 200
			}
			if limit > 500 {
				limit = 500
			}
			query += fmt.Sprintf(" LIMIT %d", limit)
			if args.Offset > 0 {
				query += fmt.Sprintf(" OFFSET %d", args.Offset)
			}

			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			critical, escalated, err := aiSMSIssuePressure(ctx, env.DB)
			if err != nil {
				return nil, err
			}
			canSensitive := aiSMSCanViewSensitive(ctx, env)
			out := make([]models.SMSStakeholder, 0)
			for rows.Next() {
				var s models.SMSStakeholder
				if err := rows.Scan(&s.ID, &s.ExternalID, &s.Name, &s.Title, &s.Organization, &s.Email, &s.Phone,
					&s.StakeholderType, &s.Category, &s.Tier, &s.BusinessUnit, &s.RoleClassification,
					&s.InfluenceLevel, &s.InterestLevel, &s.Interests, &s.EngagementStatus,
					&s.LifecycleStage, &s.RelationshipStatus, &s.TrustLevel, &s.RiskRating,
					&s.CommunicationPreferences, &s.OwnerUserID, &s.BackupOwnerUserID,
					&s.ExecutiveSponsorUserID, &s.LastEngagementAt, &s.NextEngagementAt,
					&s.InfoClassification, &s.ConsentStatus, &s.RetentionCategory, &s.LastReviewedAt,
					&s.ReviewCadence, &s.LocationLat, &s.LocationLon, &s.LocationText,
					&s.CustomFields, &s.Notes, &s.CreatedAt, &s.UpdatedAt); err != nil {
					return nil, err
				}
				s.HealthScore = aiSMSHealthScore(s.RelationshipStatus, s.EngagementStatus, critical[s.ID], escalated[s.ID])
				aiSMSFillCompleteness(&s)
				if !canSensitive {
					aiSMSApplySensitivity(&s)
				}
				out = append(out, s)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_sms_stakeholder
	// ------------------------------------------------------------------------
	Register(Default, Tool[getSMSStakeholderArgs]{
		Name:        "get_sms_stakeholder",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get one SMS stakeholder profile with its P3M entity links and sub-resource counts (communications, open issues, open tasks, open feedback, active plans, open commitments, open grievances, decisions).",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args getSMSStakeholderArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			s, err := aiSMSGetStakeholder(ctx, env.DB, args.StakeholderID)
			if err != nil {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			if !aiSMSCanViewSensitive(ctx, env) {
				aiSMSApplySensitivity(&s)
			}

			erows, err := env.DB.QueryContext(ctx, `SELECT id, stakeholder_id, entity_type, entity_id, created_at FROM sms_stakeholder_entities WHERE stakeholder_id = ? ORDER BY entity_type, entity_id`, s.ID)
			if err != nil {
				return nil, err
			}
			entities := make([]models.SMSStakeholderEntity, 0)
			for erows.Next() {
				var e models.SMSStakeholderEntity
				if err := erows.Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.CreatedAt); err != nil {
					erows.Close()
					return nil, err
				}
				e.EntityTitle = aiSMSEntityTitle(ctx, env.DB, e.EntityType, e.EntityID)
				entities = append(entities, e)
			}
			if err := erows.Err(); err != nil {
				erows.Close()
				return nil, err
			}
			erows.Close()

			counts := map[string]int{}
			for _, tc := range []struct{ table, key, cond string }{
				{"sms_communications", "communications", ""},
				{"sms_issues", "open_issues", `status IN ('open','in_progress','escalated')`},
				{"sms_tasks", "open_tasks", `status IN ('open','in_progress')`},
				{"sms_feedback", "open_feedback", `resolution_status = 'open'`},
				{"sms_engagement_plans", "active_plans", `status = 'active'`},
				{"sms_commitments", "open_commitments", `status IN ('open','in_progress','at_risk')`},
				{"sms_grievances", "open_grievances", `status <> 'closed'`},
				{"sms_decisions", "decisions", ""},
			} {
				q := "SELECT COUNT(*) FROM " + tc.table + " WHERE stakeholder_id = ?"
				if tc.cond != "" {
					q += " AND " + tc.cond
				}
				var n int
				if err := env.DB.QueryRowContext(ctx, q, s.ID).Scan(&n); err != nil {
					return nil, err
				}
				counts[tc.key] = n
			}

			return map[string]any{
				"stakeholder": s,
				"entities":    entities,
				"counts":      counts,
			}, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_sms_timeline
	// ------------------------------------------------------------------------
	Register(Default, Tool[getSMSTimelineArgs]{
		Name:        "get_sms_timeline",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get a merged, most-recent-first activity timeline for one stakeholder: communications, issues, tasks, feedback, goals, and decisions.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args getSMSTimelineArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			limit := args.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 200 {
				limit = 200
			}
			type entry struct {
				Type    string    `json:"type"`
				ID      int       `json:"id"`
				Title   string    `json:"title"`
				Summary string    `json:"summary,omitempty"`
				At      time.Time `json:"at"`
			}
			entries := make([]entry, 0)

			scanTable := func(q string, f func(*entry)) error {
				r, err := env.DB.QueryContext(ctx, q, args.StakeholderID, limit)
				if err != nil {
					return err
				}
				defer r.Close()
				for r.Next() {
					var e entry
					if err := r.Scan(&e.ID, &e.Title, &e.Summary, &e.At); err != nil {
						return err
					}
					f(&e)
					entries = append(entries, e)
				}
				return r.Err()
			}
			if err := scanTable(`SELECT id, COALESCE(NULLIF(subject,''), channel), channel || ' ' || direction, created_at FROM sms_communications WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, func(e *entry) { e.Type = "communication" }); err != nil {
				return nil, err
			}
			if err := scanTable(`SELECT id, title, status || ' / ' || priority, created_at FROM sms_issues WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, func(e *entry) { e.Type = "issue" }); err != nil {
				return nil, err
			}
			if err := scanTable(`SELECT id, title, status, COALESCE(completed_at, created_at) FROM sms_tasks WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, func(e *entry) { e.Type = "task" }); err != nil {
				return nil, err
			}
			if err := scanTable(`SELECT id, COALESCE(NULLIF(category,''), source), resolution_status, created_at FROM sms_feedback WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT ?`, func(e *entry) { e.Type = "feedback" }); err != nil {
				return nil, err
			}
			if err := scanTable(`SELECT id, goal, status, updated_at FROM sms_relationship_goals WHERE stakeholder_id = ? ORDER BY updated_at DESC LIMIT ?`, func(e *entry) { e.Type = "goal" }); err != nil {
				return nil, err
			}
			if err := scanTable(`SELECT id, title, COALESCE(decision,''), COALESCE(decided_at, created_at) FROM sms_decisions WHERE stakeholder_id = ? ORDER BY COALESCE(decided_at, created_at) DESC LIMIT ?`, func(e *entry) { e.Type = "decision" }); err != nil {
				return nil, err
			}
			sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
			if len(entries) > limit {
				entries = entries[:limit]
			}
			return entries, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_communications
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSCommunicationsArgs]{
		Name:        "list_sms_communications",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SMS communications log, newest first. Optionally restrict to one stakeholder. Bodies of restricted-classification stakeholders are masked without sms.sensitive.view.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSCommunicationsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSCommunicationColumns + ` FROM sms_communications`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY created_at DESC, id DESC LIMIT 500`

			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			canSensitive := aiSMSCanViewSensitive(ctx, env)
			out := make([]models.SMSCommunication, 0)
			for rows.Next() {
				var c models.SMSCommunication
				if err := rows.Scan(&c.ID, &c.StakeholderID, &c.Channel, &c.Direction, &c.Subject, &c.Body,
					&c.Sentiment, &c.CreatedBy, &c.CreatedAt); err != nil {
					return nil, err
				}
				var infoClass string
				if err := env.DB.QueryRowContext(ctx, `SELECT info_classification FROM sms_stakeholders WHERE id = ?`, c.StakeholderID).Scan(&infoClass); err == nil {
					if !canSensitive && infoClass == models.SMSInfoRestricted {
						c.Body = ""
					}
				}
				c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, c.StakeholderID)
				out = append(out, c)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_engagement_plans
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSEngagementPlansArgs]{
		Name:        "list_sms_engagement_plans",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS engagement plans with their member stakeholder lists. Optionally filter by stakeholder or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSEngagementPlansArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSPlanColumns + ` FROM sms_engagement_plans`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if strings.TrimSpace(args.Status) != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, strings.TrimSpace(args.Status))
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			plans := make([]models.SMSEngagementPlan, 0)
			for rows.Next() {
				p, err := aiScanSMSPlan(rows)
				if err != nil {
					return nil, err
				}
				p.StakeholderName = aiSMSStakeholderName(ctx, env.DB, p.StakeholderID)
				plans = append(plans, p)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			if err := aiSMSPlanMembers(ctx, env.DB, plans); err != nil {
				return nil, err
			}
			return plans, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_issues
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSIssuesArgs]{
		Name:        "list_sms_issues",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SMS issues & risks register. Filter by stakeholder, kind, category, priority, or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSIssuesArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSIssueColumns + ` FROM sms_issues`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.Kind); v != "" {
				conds = append(conds, "kind = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.Category); v != "" {
				conds = append(conds, "category = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.Priority); v != "" {
				conds = append(conds, "priority = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.Status); v != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSIssue, 0)
			for rows.Next() {
				i, err := aiScanSMSIssue(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, i)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_tasks
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSTasksArgs]{
		Name:        "list_sms_tasks",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS engagement tasks. Filter by stakeholder or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSTasksArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSTaskColumns + ` FROM sms_tasks`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.Status); v != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSTask, 0)
			for rows.Next() {
				t, err := aiScanSMSTask(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, t)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_feedback
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSFeedbackArgs]{
		Name:        "list_sms_feedback",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS stakeholder feedback. Filter by stakeholder or resolution status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSFeedbackArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSFeedbackColumns + ` FROM sms_feedback`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.ResolutionStatus); v != "" {
				conds = append(conds, "resolution_status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSFeedback, 0)
			for rows.Next() {
				f, err := aiScanSMSFeedback(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, f)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_goals
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSGoalsArgs]{
		Name:        "list_sms_goals",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS relationship goals. Filter by stakeholder or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSGoalsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSGoalColumns + ` FROM sms_relationship_goals`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.Status); v != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSRelationshipGoal, 0)
			for rows.Next() {
				g, err := aiScanSMSGoal(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, g)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_commitments
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSCommitmentsArgs]{
		Name:        "list_sms_commitments",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SMS commitment register. Filter by stakeholder, direction, or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSCommitmentsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSCommitmentColumns + ` FROM sms_commitments`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.Direction); v != "" {
				conds = append(conds, "direction = ?")
				qArgs = append(qArgs, v)
			}
			if v := strings.TrimSpace(args.Status); v != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSCommitment, 0)
			for rows.Next() {
				c, err := aiScanSMSCommitment(rows)
				if err != nil {
					return nil, err
				}
				c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, c.StakeholderID)
				out = append(out, c)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_grievances
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSGrievancesArgs]{
		Name:        "list_sms_grievances",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SMS grievance register. Filter by stakeholder or status.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSGrievancesArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSGrievanceColumns + ` FROM sms_grievances`
			var conds []string
			var qArgs []any
			if args.StakeholderID > 0 {
				conds = append(conds, "stakeholder_id = ?")
				qArgs = append(qArgs, args.StakeholderID)
			}
			if v := strings.TrimSpace(args.Status); v != "" {
				conds = append(conds, "status = ?")
				qArgs = append(qArgs, v)
			}
			if len(conds) > 0 {
				query += " WHERE " + strings.Join(conds, " AND ")
			}
			query += ` ORDER BY id DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSGrievance, 0)
			for rows.Next() {
				g, err := aiScanSMSGrievance(rows)
				if err != nil {
					return nil, err
				}
				g.StakeholderName = aiSMSStakeholderName(ctx, env.DB, g.StakeholderID)
				out = append(out, g)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_decisions
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSDecisionsArgs]{
		Name:        "list_sms_decisions",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SMS decision log (decisions that belong in relationship history). Filter by stakeholder.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSDecisionsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSMSDecisionColumns + ` FROM sms_decisions`
			var qArgs []any
			if args.StakeholderID > 0 {
				query += ` WHERE stakeholder_id = ?`
				qArgs = append(qArgs, args.StakeholderID)
			}
			query += ` ORDER BY COALESCE(decided_at, created_at) DESC`
			rows, err := env.DB.QueryContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSDecision, 0)
			for rows.Next() {
				d, err := aiScanSMSDecision(rows)
				if err != nil {
					return nil, err
				}
				d.StakeholderName = aiSMSStakeholderName(ctx, env.DB, d.StakeholderID)
				out = append(out, d)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// list_sms_groups
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSMSGroupsArgs]{
		Name:        "list_sms_groups",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SMS stakeholder groups with member counts, and optionally full member lists.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, args listSMSGroupsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			rows, err := env.DB.QueryContext(ctx, `SELECT g.id, g.name, g.description, g.created_at, COUNT(m.stakeholder_id) FROM sms_stakeholder_groups g LEFT JOIN sms_stakeholder_group_members m ON m.group_id = g.id GROUP BY g.id ORDER BY g.name COLLATE NOCASE`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SMSStakeholderGroup, 0)
			for rows.Next() {
				var g models.SMSStakeholderGroup
				if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.MemberCount); err != nil {
					return nil, err
				}
				out = append(out, g)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			if args.IncludeMembers && len(out) > 0 {
				ids := make([]any, 0, len(out))
				ph := make([]string, len(out))
				for i := range out {
					ids = append(ids, out[i].ID)
					ph[i] = "?"
				}
				mrows, err := env.DB.QueryContext(ctx, `SELECT m.group_id, m.stakeholder_id, s.name FROM sms_stakeholder_group_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.group_id IN (`+strings.Join(ph, ",")+`) ORDER BY m.group_id, s.name COLLATE NOCASE`, ids...)
				if err != nil {
					return nil, err
				}
				membersByGroup := make(map[int][]models.SMSStakeholderRef)
				for mrows.Next() {
					var gid, sid int
					var name string
					if err := mrows.Scan(&gid, &sid, &name); err != nil {
						mrows.Close()
						return nil, err
					}
					membersByGroup[gid] = append(membersByGroup[gid], models.SMSStakeholderRef{ID: sid, Name: name})
				}
				if err := mrows.Err(); err != nil {
					mrows.Close()
					return nil, err
				}
				mrows.Close()
				for i := range out {
					out[i].Members = membersByGroup[out[i].ID]
				}
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// get_sms_p3m_options
	// ------------------------------------------------------------------------
	Register(Default, Tool[getSMSP3MOptionsArgs]{
		Name:        "get_sms_p3m_options",
		Group:       CapabilitySMS,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get the P3M entity pickers (portfolios, programmes, projects) available for linking to SMS stakeholders.",
		Scopes:      []string{auth.ScopeSMSRead},
		Run: func(ctx context.Context, env *Env, _ getSMSP3MOptionsArgs) (any, error) {
			ok, err := aiRequireSMSView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			return aiSMSP3MOptions(ctx, env.DB)
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_stakeholder
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSStakeholderArgs]{
		Name:        "create_sms_stakeholder",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create an SMS stakeholder profile in the register. Enum fields fall back to the register defaults when omitted; the returned record includes the computed health score and minimum-field completeness.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSStakeholderArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			aiSMSTrimRequired(&args.Name)
			if args.Name == "" {
				return map[string]string{"error": "name is required"}, nil
			}
			if args.RoleClassification != "" && !aiSMSValidEnum(args.RoleClassification, aiSMSRoleClassifications...) {
				return map[string]string{"error": "invalid role_classification"}, nil
			}
			if args.EngagementStatus != "" && !aiSMSValidEnum(args.EngagementStatus, aiSMSEngagementStatuses...) {
				return map[string]string{"error": "invalid engagement_status"}, nil
			}
			if args.LifecycleStage != "" && !aiSMSValidEnum(args.LifecycleStage, aiSMSLifecycleStages...) {
				return map[string]string{"error": "invalid lifecycle_stage"}, nil
			}
			if args.RelationshipStatus != "" && !aiSMSValidEnum(args.RelationshipStatus, aiSMSRelationshipStatuses...) {
				return map[string]string{"error": "invalid relationship_status"}, nil
			}
			if args.Tier != "" && !aiSMSValidEnum(args.Tier, aiSMSTiers...) {
				return map[string]string{"error": "invalid tier"}, nil
			}
			if args.StakeholderType != nil && !aiSMSValidEnum(*args.StakeholderType, aiSMSTypes...) {
				return map[string]string{"error": "invalid stakeholder_type"}, nil
			}
			if args.TrustLevel != "" && !aiSMSValidEnum(args.TrustLevel, aiSMSTrustLevels...) {
				return map[string]string{"error": "invalid trust_level"}, nil
			}
			if args.RiskRating != nil && !aiSMSValidEnum(*args.RiskRating, aiSMSPriorities...) {
				return map[string]string{"error": "invalid risk_rating"}, nil
			}
			if args.InfoClassification != "" && !aiSMSValidEnum(args.InfoClassification, aiSMSInfoClassifications...) {
				return map[string]string{"error": "invalid info_classification"}, nil
			}
			if args.ConsentStatus != "" && !aiSMSValidEnum(args.ConsentStatus, aiSMSConsentStatuses...) {
				return map[string]string{"error": "invalid consent_status"}, nil
			}
			if args.ReviewCadence != "" && !aiSMSValidEnum(args.ReviewCadence, aiSMSCadences...) {
				return map[string]string{"error": "invalid review_cadence"}, nil
			}
			for _, lvl := range []int{args.InfluenceLevel, args.InterestLevel} {
				if lvl != 0 && (lvl < 1 || lvl > 5) {
					return map[string]string{"error": "influence_level and interest_level must be between 1 and 5"}, nil
				}
			}
			if args.LocationLat != nil && (*args.LocationLat < -90 || *args.LocationLat > 90) {
				return map[string]string{"error": "location_lat must be between -90 and 90"}, nil
			}
			if args.LocationLon != nil && (*args.LocationLon < -180 || *args.LocationLon > 180) {
				return map[string]string{"error": "location_lon must be between -180 and 180"}, nil
			}
			customFields := strings.TrimSpace(args.CustomFields)
			if customFields == "" {
				customFields = "{}"
			} else if !aiSMSJSONIsObject(customFields) {
				return map[string]string{"error": "custom_fields must be a JSON object"}, nil
			}
			for _, uid := range []int{aiSMSDerefInt(args.OwnerUserID), aiSMSDerefInt(args.BackupOwnerUserID), aiSMSDerefInt(args.ExecutiveSponsorUserID)} {
				if uid > 0 && !aiSMSUserIDExists(ctx, env.DB, uid) {
					return map[string]string{"error": "referenced user not found: " + fmt.Sprintf("%d", uid)}, nil
				}
			}

			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO sms_stakeholders (name, title, organization, email, phone, external_id, stakeholder_type, category, tier, business_unit, role_classification, influence_level, interest_level, interests, engagement_status, lifecycle_stage, relationship_status, trust_level, risk_rating, communication_preferences, owner_user_id, backup_owner_user_id, executive_sponsor_user_id, last_engagement_at, next_engagement_at, info_classification, consent_status, retention_category, last_reviewed_at, review_cadence, location_lat, location_lon, location_text, custom_fields, notes)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.Name, args.Title, args.Organization, args.Email, args.Phone,
				args.ExternalID, args.StakeholderType, args.Category, aiSMSOrKeep(args.Tier, models.SMSTier3), args.BusinessUnit,
				aiSMSOrKeep(args.RoleClassification, models.SMSRoleSupport),
				aiSMSLevelOrDefault(args.InfluenceLevel), aiSMSLevelOrDefault(args.InterestLevel),
				args.Interests, aiSMSOrKeep(args.EngagementStatus, models.SMSEngagementPassive),
				aiSMSOrKeep(args.LifecycleStage, models.SMSLifecycleOnboarding),
				aiSMSOrKeep(args.RelationshipStatus, models.SMSRelationshipNeutral),
				aiSMSOrKeep(args.TrustLevel, "moderate"), args.RiskRating,
				args.CommunicationPreferences, args.OwnerUserID, args.BackupOwnerUserID, args.ExecutiveSponsorUserID,
				args.LastEngagementAt, args.NextEngagementAt,
				aiSMSOrKeep(args.InfoClassification, models.SMSInfoInternal), aiSMSOrKeep(args.ConsentStatus, models.SMSConsentNotRequired), args.RetentionCategory,
				args.LastReviewedAt, aiSMSOrKeep(args.ReviewCadence, models.SMSReviewQuarterly),
				args.LocationLat, args.LocationLon, args.LocationText, customFields, args.Notes)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			s, err := aiSMSGetStakeholder(ctx, env.DB, int(id64))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_stakeholder", int(id64), "create_sms_stakeholder", s.Name)
			return s, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_stakeholder
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSStakeholderArgs]{
		Name:        "update_sms_stakeholder",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS stakeholder. Omitted fields keep their stored values; pointer fields may be set to explicit values (0/empty string clears nullable references). Returns the updated record with recomputed health score.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSStakeholderArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			existing, err := aiSMSGetStakeholder(ctx, env.DB, args.StakeholderID)
			if err != nil {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			if args.Tier != nil && !aiSMSValidEnum(*args.Tier, aiSMSTiers...) {
				return map[string]string{"error": "invalid tier"}, nil
			}
			if args.RoleClassification != nil && !aiSMSValidEnum(*args.RoleClassification, aiSMSRoleClassifications...) {
				return map[string]string{"error": "invalid role_classification"}, nil
			}
			if args.EngagementStatus != nil && !aiSMSValidEnum(*args.EngagementStatus, aiSMSEngagementStatuses...) {
				return map[string]string{"error": "invalid engagement_status"}, nil
			}
			if args.LifecycleStage != nil && !aiSMSValidEnum(*args.LifecycleStage, aiSMSLifecycleStages...) {
				return map[string]string{"error": "invalid lifecycle_stage"}, nil
			}
			if args.RelationshipStatus != nil && !aiSMSValidEnum(*args.RelationshipStatus, aiSMSRelationshipStatuses...) {
				return map[string]string{"error": "invalid relationship_status"}, nil
			}
			if args.TrustLevel != nil && !aiSMSValidEnum(*args.TrustLevel, aiSMSTrustLevels...) {
				return map[string]string{"error": "invalid trust_level"}, nil
			}
			if args.RiskRating != nil && *args.RiskRating != "" && !aiSMSValidEnum(*args.RiskRating, aiSMSPriorities...) {
				return map[string]string{"error": "invalid risk_rating"}, nil
			}
			if args.ReviewCadence != nil && !aiSMSValidEnum(*args.ReviewCadence, aiSMSCadences...) {
				return map[string]string{"error": "invalid review_cadence"}, nil
			}
			for _, lvl := range []*int{args.InfluenceLevel, args.InterestLevel} {
				if lvl != nil && (*lvl < 1 || *lvl > 5) {
					return map[string]string{"error": "influence_level and interest_level must be between 1 and 5"}, nil
				}
			}
			for _, uid := range []*int{args.OwnerUserID, args.BackupOwnerUserID, args.ExecutiveSponsorUserID} {
				if uid != nil && *uid > 0 && !aiSMSUserIDExists(ctx, env.DB, *uid) {
					return map[string]string{"error": "referenced user not found: " + fmt.Sprintf("%d", *uid)}, nil
				}
			}

			title := aiSMSKeepOr(args.Title, existing.Title)
			org := aiSMSKeepOr(args.Organization, existing.Organization)
			email := aiSMSKeepOr(args.Email, existing.Email)
			phone := aiSMSKeepOr(args.Phone, existing.Phone)
			locationText := aiSMSKeepOr(args.LocationText, existing.LocationText)
			notes := aiSMSKeepOr(args.Notes, existing.Notes)

			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_stakeholders SET
					title = ?, organization = ?, email = ?, phone = ?,
					tier = ?, role_classification = ?, influence_level = ?, interest_level = ?,
					engagement_status = ?, lifecycle_stage = ?, relationship_status = ?,
					trust_level = ?, risk_rating = ?, owner_user_id = ?, backup_owner_user_id = ?,
					executive_sponsor_user_id = ?, next_engagement_at = ?, last_reviewed_at = ?,
					review_cadence = ?, location_text = ?, notes = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				title, org, email, phone,
				aiSMSKeepOr(args.Tier, existing.Tier), aiSMSKeepOr(args.RoleClassification, existing.RoleClassification),
				aiSMSLevelOr(args.InfluenceLevel, existing.InfluenceLevel), aiSMSLevelOr(args.InterestLevel, existing.InterestLevel),
				aiSMSKeepOr(args.EngagementStatus, existing.EngagementStatus), aiSMSKeepOr(args.LifecycleStage, existing.LifecycleStage),
				aiSMSKeepOr(args.RelationshipStatus, existing.RelationshipStatus),
				aiSMSKeepOr(args.TrustLevel, aiSMSDerefStr(existing.TrustLevel)),
				aiSMSRiskRatingOr(args.RiskRating, existing.RiskRating),
				aiSMSUserIntOr(args.OwnerUserID, existing.OwnerUserID), aiSMSUserIntOr(args.BackupOwnerUserID, existing.BackupOwnerUserID),
				aiSMSUserIntOr(args.ExecutiveSponsorUserID, existing.ExecutiveSponsorUserID),
				aiSMSTimeOr(args.NextEngagementAt, existing.NextEngagementAt), aiSMSTimeOr(args.LastReviewedAt, existing.LastReviewedAt),
				aiSMSKeepOr(args.ReviewCadence, existing.ReviewCadence), locationText, notes, args.StakeholderID); err != nil {
				return nil, err
			}
			updated, err := aiSMSGetStakeholder(ctx, env.DB, args.StakeholderID)
			if err != nil {
				return nil, err
			}
			if !aiSMSCanViewSensitive(ctx, env) {
				aiSMSApplySensitivity(&updated)
			}
			env.AuditWrite("sms_stakeholder", args.StakeholderID, "update_sms_stakeholder", updated.Name)
			return updated, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_communication
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSCommunicationArgs]{
		Name:        "create_sms_communication",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Log one interaction (communication) with an SMS stakeholder: channel, direction, subject, body, and an optional sentiment tag.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSCommunicationArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			if args.Channel != "" && !aiSMSValidEnum(args.Channel, aiSMSChannels...) {
				return map[string]string{"error": "invalid channel"}, nil
			}
			if args.Direction != "" && !aiSMSValidEnum(args.Direction, aiSMSDirections...) {
				return map[string]string{"error": "invalid direction"}, nil
			}
			if args.Sentiment != nil && !aiSMSValidEnum(*args.Sentiment, aiSMSSentiments...) {
				return map[string]string{"error": "invalid sentiment"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_communications (stakeholder_id, channel, direction, subject, body, sentiment, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID,
				aiSMSOrKeep(args.Channel, models.SMSChannelEmail),
				aiSMSOrKeep(args.Direction, models.SMSDirectionOutbound),
				args.Subject, args.Body, args.Sentiment, env.UserID)
			if err != nil {
				return nil, err
			}
			cid64, _ := res.LastInsertId()
			var c models.SMSCommunication
			if err := env.DB.QueryRowContext(ctx, `SELECT `+aiSMSCommunicationColumns+` FROM sms_communications WHERE id = ?`, int(cid64)).
				Scan(&c.ID, &c.StakeholderID, &c.Channel, &c.Direction, &c.Subject, &c.Body, &c.Sentiment, &c.CreatedBy, &c.CreatedAt); err != nil {
				return nil, err
			}
			c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			env.AuditWrite("sms_communication", int(cid64), "create_sms_communication", c.Subject)
			return c, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_engagement_plan
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSEngagementPlanArgs]{
		Name:        "create_sms_engagement_plan",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create an SMS engagement plan for a lead stakeholder, with an optional P3M entity scope, dates, and additional member stakeholders.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSEngagementPlanArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Strategy)
			if args.Strategy == "" {
				return map[string]string{"error": "strategy is required"}, nil
			}
			if args.Frequency != "" && !aiSMSValidEnum(args.Frequency, aiSMSFrequencies...) {
				return map[string]string{"error": "invalid frequency"}, nil
			}
			if args.Channel != "" && !aiSMSValidEnum(args.Channel, aiSMSChannels...) {
				return map[string]string{"error": "invalid channel"}, nil
			}
			if args.Status != "" && !aiSMSValidEnum(args.Status, aiSMSPlanStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			if args.Approach != nil && !aiSMSValidEnum(*args.Approach, aiSMSApproaches...) {
				return map[string]string{"error": "invalid approach"}, nil
			}
			if args.EntityType != nil {
				if !aiSMSValidEnum(*args.EntityType, models.SMSEntityPortfolio, models.SMSEntityProgramme, models.SMSEntityProject) {
					return map[string]string{"error": "invalid entity_type"}, nil
				}
				if !aiSMSP3MEntityExists(ctx, env.DB, *args.EntityType, aiSMSDerefInt(args.EntityID)) {
					return map[string]string{"error": "P3M entity not found"}, nil
				}
			}

			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_engagement_plans (stakeholder_id, strategy, objective, approach, key_messages, success_measures, frequency, channel, status, entity_type, entity_id, start_date, end_date, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, args.Strategy, args.Objective, args.Approach, args.KeyMessages, args.SuccessMeasures,
				aiSMSOrKeep(args.Frequency, models.SMSFrequencyMonthly), aiSMSOrKeep(args.Channel, models.SMSChannelEmail), aiSMSOrKeep(args.Status, models.SMSPlanStatusActive),
				args.EntityType, args.EntityID, args.StartDate, args.EndDate, args.Notes)
			if err != nil {
				return nil, err
			}
			pid64, _ := res.LastInsertId()
			for _, sid := range args.MemberIDs {
				if sid == args.StakeholderID || sid <= 0 {
					continue
				}
				if !aiSMSStakeholderExists(ctx, env.DB, sid) {
					return map[string]string{"error": "member stakeholder not found: " + fmt.Sprintf("%d", sid)}, nil
				}
				if _, err := env.DB.ExecContext(ctx, `INSERT INTO sms_engagement_plan_stakeholders (plan_id, stakeholder_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, int(pid64), sid); err != nil {
					return nil, err
				}
			}
			var p models.SMSEngagementPlan
			if err := env.DB.QueryRowContext(ctx, `SELECT `+aiSMSPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, int(pid64)).
				Scan(&p.ID, &p.StakeholderID, &p.Strategy, &p.Objective, &p.Approach, &p.KeyMessages, &p.SuccessMeasures,
					&p.Frequency, &p.Channel, &p.Status, &p.EntityType, &p.EntityID, &p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return nil, err
			}
			p.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			if err := aiSMSPlanMembers(ctx, env.DB, []models.SMSEngagementPlan{p}); err != nil {
				return nil, err
			}
			env.AuditWrite("sms_engagement_plan", int(pid64), "create_sms_engagement_plan", p.Strategy)
			return p, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_engagement_plan
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSEngagementPlanArgs]{
		Name:        "update_sms_engagement_plan",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS engagement plan. When member_ids is provided the plan's full member list is replaced (the lead stakeholder is always retained).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSEngagementPlanArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.PlanID <= 0 {
				return map[string]string{"error": "plan_id is required"}, nil
			}
			existing, err := aiScanSMSPlan(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, args.PlanID))
			if err != nil {
				return map[string]string{"error": "engagement plan not found"}, nil
			}
			if args.Approach != nil && !aiSMSValidEnum(*args.Approach, aiSMSApproaches...) {
				return map[string]string{"error": "invalid approach"}, nil
			}
			if args.Frequency != nil && !aiSMSValidEnum(*args.Frequency, aiSMSFrequencies...) {
				return map[string]string{"error": "invalid frequency"}, nil
			}
			if args.Channel != nil && !aiSMSValidEnum(*args.Channel, aiSMSChannels...) {
				return map[string]string{"error": "invalid channel"}, nil
			}
			if args.Status != nil && !aiSMSValidEnum(*args.Status, aiSMSPlanStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}

			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_engagement_plans SET strategy = ?, objective = ?, approach = ?, key_messages = ?, success_measures = ?,
					frequency = ?, channel = ?, status = ?, start_date = ?, end_date = ?, notes = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				aiSMSKeepOr(args.Strategy, existing.Strategy), aiSMSKeepOr(args.Objective, existing.Objective),
				aiSMSApproachOr(args.Approach, existing.Approach), aiSMSKeepOr(args.KeyMessages, existing.KeyMessages),
				aiSMSKeepOr(args.SuccessMeasures, existing.SuccessMeasures),
				aiSMSKeepOr(args.Frequency, existing.Frequency), aiSMSKeepOr(args.Channel, existing.Channel),
				aiSMSKeepOr(args.Status, existing.Status),
				aiSMSTimeOr(args.StartDate, existing.StartDate), aiSMSTimeOr(args.EndDate, existing.EndDate),
				aiSMSKeepOr(args.Notes, existing.Notes), args.PlanID); err != nil {
				return nil, err
			}

			if args.MemberIDs != nil {
				seen := map[int]bool{existing.StakeholderID: true}
				if _, err := env.DB.ExecContext(ctx, `DELETE FROM sms_engagement_plan_stakeholders WHERE plan_id = ? AND stakeholder_id <> ?`, args.PlanID, existing.StakeholderID); err != nil {
					return nil, err
				}
				for _, sid := range args.MemberIDs {
					if sid <= 0 || seen[sid] {
						continue
					}
					seen[sid] = true
					if !aiSMSStakeholderExists(ctx, env.DB, sid) {
						return map[string]string{"error": "member stakeholder not found: " + fmt.Sprintf("%d", sid)}, nil
					}
					if _, err := env.DB.ExecContext(ctx, `INSERT INTO sms_engagement_plan_stakeholders (plan_id, stakeholder_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, args.PlanID, sid); err != nil {
						return nil, err
					}
				}
			}

			p, err := aiScanSMSPlan(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSPlanColumns+` FROM sms_engagement_plans WHERE id = ?`, args.PlanID))
			if err != nil {
				return nil, err
			}
			p.StakeholderName = aiSMSStakeholderName(ctx, env.DB, p.StakeholderID)
			if err := aiSMSPlanMembers(ctx, env.DB, []models.SMSEngagementPlan{p}); err != nil {
				return nil, err
			}
			env.AuditWrite("sms_engagement_plan", args.PlanID, "update_sms_engagement_plan", p.Strategy)
			return p, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_issue
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSIssueArgs]{
		Name:        "create_sms_issue",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record an issue or risk on the SMS issues & risks register for a stakeholder.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSIssueArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Title)
			if args.Title == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			if args.Kind != "" && !aiSMSValidEnum(args.Kind, aiSMSIssueKinds...) {
				return map[string]string{"error": "invalid kind"}, nil
			}
			if args.Category != "" && !aiSMSValidEnum(args.Category, aiSMSIssueCategories...) {
				return map[string]string{"error": "invalid category"}, nil
			}
			if args.Priority != "" && !aiSMSValidEnum(args.Priority, aiSMSPriorities...) {
				return map[string]string{"error": "invalid priority"}, nil
			}
			if args.Status != "" && !aiSMSValidEnum(args.Status, aiSMSIssueStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			for _, lvl := range []int{args.Impact, args.Likelihood} {
				if lvl != 0 && (lvl < 1 || lvl > 5) {
					return map[string]string{"error": "impact and likelihood must be between 1 and 5"}, nil
				}
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_issues (stakeholder_id, kind, category, title, description, priority, impact, likelihood, status, escalation_path, mitigation) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID,
				aiSMSOrKeep(args.Kind, models.SMSIssueKindIssue), aiSMSOrKeep(args.Category, models.SMSCategoryOther),
				args.Title, args.Description,
				aiSMSOrKeep(args.Priority, models.SMSPriorityMedium),
				aiSMSLevelOrDefault(args.Impact), aiSMSLevelOrDefault(args.Likelihood),
				aiSMSOrKeep(args.Status, models.SMSIssueStatusOpen), args.EscalationPath, args.Mitigation)
			if err != nil {
				return nil, err
			}
			iid64, _ := res.LastInsertId()
			i, err := aiScanSMSIssue(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSIssueColumns+` FROM sms_issues WHERE id = ?`, int(iid64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_issue", int(iid64), "create_sms_issue", i.Title)
			return i, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_issue
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSIssueArgs]{
		Name:        "update_sms_issue",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS issue/risk. Moving the status to resolved stamps resolved_at; moving away clears it.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSIssueArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.IssueID <= 0 {
				return map[string]string{"error": "issue_id is required"}, nil
			}
			existing, err := aiScanSMSIssue(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSIssueColumns+` FROM sms_issues WHERE id = ?`, args.IssueID))
			if err != nil {
				return map[string]string{"error": "issue not found"}, nil
			}
			title := aiSMSKeepOr(args.Title, existing.Title)
			if strings.TrimSpace(title) == "" {
				return map[string]string{"error": "title cannot be empty"}, nil
			}
			if args.Kind != nil && !aiSMSValidEnum(*args.Kind, aiSMSIssueKinds...) {
				return map[string]string{"error": "invalid kind"}, nil
			}
			if args.Category != nil && !aiSMSValidEnum(*args.Category, aiSMSIssueCategories...) {
				return map[string]string{"error": "invalid category"}, nil
			}
			if args.Priority != nil && !aiSMSValidEnum(*args.Priority, aiSMSPriorities...) {
				return map[string]string{"error": "invalid priority"}, nil
			}
			if args.Status != nil && !aiSMSValidEnum(*args.Status, aiSMSIssueStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			for _, lvl := range []*int{args.Impact, args.Likelihood} {
				if lvl != nil && (*lvl < 1 || *lvl > 5) {
					return map[string]string{"error": "impact and likelihood must be between 1 and 5"}, nil
				}
			}

			newStatus := aiSMSKeepOr(args.Status, existing.Status)
			resolvedAt := existing.ResolvedAt
			if newStatus == models.SMSIssueStatusResolved && existing.Status != models.SMSIssueStatusResolved {
				now := time.Now().UTC()
				resolvedAt = &now
			} else if newStatus != models.SMSIssueStatusResolved && existing.Status == models.SMSIssueStatusResolved {
				resolvedAt = nil
			}

			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_issues SET kind = ?, category = ?, title = ?, description = ?, priority = ?, impact = ?, likelihood = ?,
					status = ?, escalation_path = ?, mitigation = ?, resolution_notes = ?, resolved_at = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				aiSMSKeepOr(args.Kind, existing.Kind), aiSMSKeepOr(args.Category, existing.Category), title,
				aiSMSKeepOr(args.Description, existing.Description), aiSMSKeepOr(args.Priority, existing.Priority),
				aiSMSLevelOr(args.Impact, existing.Impact), aiSMSLevelOr(args.Likelihood, existing.Likelihood),
				newStatus, aiSMSKeepOr(args.EscalationPath, existing.EscalationPath),
				aiSMSKeepOr(args.Mitigation, existing.Mitigation), aiSMSKeepOr(args.ResolutionNotes, existing.ResolutionNotes),
				resolvedAt, args.IssueID); err != nil {
				return nil, err
			}
			i, err := aiScanSMSIssue(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSIssueColumns+` FROM sms_issues WHERE id = ?`, args.IssueID))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_issue", args.IssueID, "update_sms_issue", i.Title)
			return i, nil
		},
	})

	// ------------------------------------------------------------------------
	// escalate_sms_issue
	// ------------------------------------------------------------------------
	Register(Default, Tool[escalateSMSIssueArgs]{
		Name:        "escalate_sms_issue",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Escalate an SMS issue/risk: sets status to escalated and increments its escalation level. Optionally records where it sits on the escalation path.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args escalateSMSIssueArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.IssueID <= 0 {
				return map[string]string{"error": "issue_id is required"}, nil
			}
			query := `UPDATE sms_issues SET status = 'escalated', escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP`
			var qArgs []any
			if strings.TrimSpace(args.EscalationPath) != "" {
				query += `, escalation_path = ?`
				qArgs = append(qArgs, strings.TrimSpace(args.EscalationPath))
			}
			query += ` WHERE id = ?`
			qArgs = append(qArgs, args.IssueID)
			res, err := env.DB.ExecContext(ctx, query, qArgs...)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "issue not found"}, nil
			}
			i, err := aiScanSMSIssue(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSIssueColumns+` FROM sms_issues WHERE id = ?`, args.IssueID))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_issue", args.IssueID, "escalate_sms_issue", i.Title)
			return i, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_task
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSTaskArgs]{
		Name:        "create_sms_task",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create an SMS engagement task for a stakeholder, optionally assigned to a user with a due date.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSTaskArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Title)
			if args.Title == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			if args.AssignedToUserID != nil && !aiSMSUserIDExists(ctx, env.DB, *args.AssignedToUserID) {
				return map[string]string{"error": "assignee user not found"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_tasks (stakeholder_id, title, description, assigned_to_user_id, due_date, status) VALUES (?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, args.Title, args.Description, args.AssignedToUserID, args.DueDate, models.SMSTaskStatusOpen)
			if err != nil {
				return nil, err
			}
			tid64, _ := res.LastInsertId()
			t, err := aiScanSMSTask(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSTaskColumns+` FROM sms_tasks WHERE id = ?`, int(tid64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_task", int(tid64), "create_sms_task", t.Title)
			return t, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_task
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSTaskArgs]{
		Name:        "update_sms_task",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS task. Moving the status to done stamps completed_at; other statuses clear it.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSTaskArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.TaskID <= 0 {
				return map[string]string{"error": "task_id is required"}, nil
			}
			existing, err := aiScanSMSTask(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSTaskColumns+` FROM sms_tasks WHERE id = ?`, args.TaskID))
			if err != nil {
				return map[string]string{"error": "task not found"}, nil
			}
			title := aiSMSKeepOr(args.Title, existing.Title)
			if strings.TrimSpace(title) == "" {
				return map[string]string{"error": "title cannot be empty"}, nil
			}
			if args.Status != nil && !aiSMSValidEnum(*args.Status, aiSMSTaskStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			if args.AssignedToUserID != nil && *args.AssignedToUserID > 0 && !aiSMSUserIDExists(ctx, env.DB, *args.AssignedToUserID) {
				return map[string]string{"error": "assignee user not found"}, nil
			}
			newStatus := aiSMSKeepOr(args.Status, existing.Status)
			completedAt := existing.CompletedAt
			if newStatus == models.SMSTaskStatusDone && existing.Status != models.SMSTaskStatusDone {
				now := time.Now().UTC()
				completedAt = &now
			} else if newStatus != models.SMSTaskStatusDone && existing.Status == models.SMSTaskStatusDone {
				completedAt = nil
			}
			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_tasks SET title = ?, description = ?, assigned_to_user_id = ?, due_date = ?, status = ?, completed_at = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				title, aiSMSKeepOr(args.Description, existing.Description),
				aiSMSUserIntOr(args.AssignedToUserID, existing.AssignedToUserID),
				aiSMSTimeOr(args.DueDate, existing.DueDate), newStatus, completedAt, args.TaskID); err != nil {
				return nil, err
			}
			t, err := aiScanSMSTask(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSTaskColumns+` FROM sms_tasks WHERE id = ?`, args.TaskID))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_task", args.TaskID, "update_sms_task", t.Title)
			return t, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_feedback
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSFeedbackArgs]{
		Name:        "create_sms_feedback",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record stakeholder feedback on the SMS register (source, category, rating, body).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSFeedbackArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			if args.Source != "" && !aiSMSValidEnum(args.Source, aiSMSFeedbackSources...) {
				return map[string]string{"error": "invalid source"}, nil
			}
			if args.ResolutionStatus != "" && !aiSMSValidEnum(args.ResolutionStatus, aiSMSFeedbackResolutions...) {
				return map[string]string{"error": "invalid resolution_status"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_feedback (stakeholder_id, source, category, rating, body, resolution_status) VALUES (?, ?, ?, ?, ?, ?)`,
				args.StakeholderID,
				aiSMSOrKeep(args.Source, models.SMSFeedbackSourceOther), args.Category, args.Rating, args.Body,
				aiSMSOrKeep(args.ResolutionStatus, models.SMSFeedbackOpen))
			if err != nil {
				return nil, err
			}
			fid64, _ := res.LastInsertId()
			f, err := aiScanSMSFeedback(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSFeedbackColumns+` FROM sms_feedback WHERE id = ?`, int(fid64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_feedback", int(fid64), "create_sms_feedback", f.Category)
			return f, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_goal
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSGoalArgs]{
		Name:        "create_sms_goal",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Set a relationship goal for a stakeholder with an optional tracking metric, target, and current value.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSGoalArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Goal)
			if args.Goal == "" {
				return map[string]string{"error": "goal is required"}, nil
			}
			if args.Status != "" && !aiSMSValidEnum(args.Status, aiSMSGoalStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_relationship_goals (stakeholder_id, goal, metric, target_value, current_value, status) VALUES (?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, args.Goal, args.Metric, args.TargetValue, args.CurrentValue,
				aiSMSOrKeep(args.Status, models.SMSGoalOnTrack))
			if err != nil {
				return nil, err
			}
			gid64, _ := res.LastInsertId()
			g, err := aiScanSMSGoal(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, int(gid64)))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_relationship_goal", int(gid64), "create_sms_goal", g.Goal)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_goal
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSGoalArgs]{
		Name:        "update_sms_goal",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS relationship goal (goal text, metric, target/current value, status).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSGoalArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.GoalID <= 0 {
				return map[string]string{"error": "goal_id is required"}, nil
			}
			existing, err := aiScanSMSGoal(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, args.GoalID))
			if err != nil {
				return map[string]string{"error": "goal not found"}, nil
			}
			goal := aiSMSKeepOr(args.Goal, existing.Goal)
			if strings.TrimSpace(goal) == "" {
				return map[string]string{"error": "goal cannot be empty"}, nil
			}
			if args.Status != nil && !aiSMSValidEnum(*args.Status, aiSMSGoalStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_relationship_goals SET goal = ?, metric = ?, target_value = ?, current_value = ?, status = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				goal, aiSMSKeepOr(args.Metric, existing.Metric),
				aiSMSFloatOr(args.TargetValue, existing.TargetValue), aiSMSFloatOr(args.CurrentValue, existing.CurrentValue),
				aiSMSKeepOr(args.Status, existing.Status), args.GoalID); err != nil {
				return nil, err
			}
			g, err := aiScanSMSGoal(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGoalColumns+` FROM sms_relationship_goals WHERE id = ?`, args.GoalID))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("sms_relationship_goal", args.GoalID, "update_sms_goal", g.Goal)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_commitment
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSCommitmentArgs]{
		Name:        "create_sms_commitment",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record a commitment on the SMS commitment register (from_us, from_them, or mutual), optionally linked to a communication or issue.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSCommitmentArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Title)
			if args.Title == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			direction := args.Direction
			if direction == "" {
				direction = models.SMSCommitmentFromThem
			}
			if !aiSMSValidEnum(direction, aiSMSCommitmentDirections...) {
				return map[string]string{"error": "invalid direction"}, nil
			}
			if args.Status != "" && !aiSMSValidEnum(args.Status, aiSMSCommitmentStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			if args.OwnerUserID != nil && !aiSMSUserIDExists(ctx, env.DB, *args.OwnerUserID) {
				return map[string]string{"error": "owner user not found"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_commitments (stakeholder_id, direction, title, description, owner_user_id, due_date, status, source_communication_id, linked_issue_id, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, direction, args.Title, args.Description, args.OwnerUserID, args.DueDate,
				aiSMSOrKeep(args.Status, models.SMSCommitmentOpen), args.SourceCommunicationID, args.LinkedIssueID, args.Notes)
			if err != nil {
				return nil, err
			}
			cid64, _ := res.LastInsertId()
			c, err := aiScanSMSCommitment(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSCommitmentColumns+` FROM sms_commitments WHERE id = ?`, int(cid64)))
			if err != nil {
				return nil, err
			}
			c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			env.AuditWrite("sms_commitment", int(cid64), "create_sms_commitment", c.Title)
			return c, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_commitment
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSCommitmentArgs]{
		Name:        "update_sms_commitment",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS commitment (direction, title, owner, due date, status, notes).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSCommitmentArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.CommitmentID <= 0 {
				return map[string]string{"error": "commitment_id is required"}, nil
			}
			existing, err := aiScanSMSCommitment(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSCommitmentColumns+` FROM sms_commitments WHERE id = ?`, args.CommitmentID))
			if err != nil {
				return map[string]string{"error": "commitment not found"}, nil
			}
			title := aiSMSKeepOr(args.Title, existing.Title)
			if strings.TrimSpace(title) == "" {
				return map[string]string{"error": "title cannot be empty"}, nil
			}
			if args.Direction != nil && !aiSMSValidEnum(*args.Direction, aiSMSCommitmentDirections...) {
				return map[string]string{"error": "invalid direction"}, nil
			}
			if args.Status != nil && !aiSMSValidEnum(*args.Status, aiSMSCommitmentStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			if args.OwnerUserID != nil && *args.OwnerUserID > 0 && !aiSMSUserIDExists(ctx, env.DB, *args.OwnerUserID) {
				return map[string]string{"error": "owner user not found"}, nil
			}
			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_commitments SET direction = ?, title = ?, description = ?, owner_user_id = ?, due_date = ?, status = ?, notes = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				aiSMSKeepOr(args.Direction, existing.Direction), title, aiSMSKeepOr(args.Description, existing.Description),
				aiSMSUserIntOr(args.OwnerUserID, existing.OwnerUserID), aiSMSTimeOr(args.DueDate, existing.DueDate),
				aiSMSKeepOr(args.Status, existing.Status), aiSMSKeepOr(args.Notes, existing.Notes), args.CommitmentID); err != nil {
				return nil, err
			}
			c, err := aiScanSMSCommitment(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSCommitmentColumns+` FROM sms_commitments WHERE id = ?`, args.CommitmentID))
			if err != nil {
				return nil, err
			}
			c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, c.StakeholderID)
			env.AuditWrite("sms_commitment", args.CommitmentID, "update_sms_commitment", c.Title)
			return c, nil
		},
	})

	// ------------------------------------------------------------------------
	// escalate_sms_commitment
	// ------------------------------------------------------------------------
	Register(Default, Tool[escalateSMSCommitmentArgs]{
		Name:        "escalate_sms_commitment",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Escalate an SMS commitment: marks it escalated and increments its escalation level.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args escalateSMSCommitmentArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.CommitmentID <= 0 {
				return map[string]string{"error": "commitment_id is required"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `UPDATE sms_commitments SET escalated = 1, escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, args.CommitmentID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "commitment not found"}, nil
			}
			c, err := aiScanSMSCommitment(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSCommitmentColumns+` FROM sms_commitments WHERE id = ?`, args.CommitmentID))
			if err != nil {
				return nil, err
			}
			c.StakeholderName = aiSMSStakeholderName(ctx, env.DB, c.StakeholderID)
			env.AuditWrite("sms_commitment", args.CommitmentID, "escalate_sms_commitment", c.Title)
			return c, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_grievance
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSGrievanceArgs]{
		Name:        "create_sms_grievance",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record a stakeholder grievance/complaint on the SMS grievance register (distinct from general feedback).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSGrievanceArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Title)
			if args.Title == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			severity := args.Severity
			if severity == "" {
				severity = models.SMSGrievanceMedium
			}
			if !aiSMSValidEnum(severity, aiSMSGrievanceSeverities...) {
				return map[string]string{"error": "invalid severity"}, nil
			}
			if args.Status != "" && !aiSMSValidEnum(args.Status, aiSMSGrievanceStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_grievances (stakeholder_id, title, description, category, severity, status, response_due_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, args.Title, args.Description, args.Category, severity,
				aiSMSOrKeep(args.Status, models.SMSGrievanceReceived), args.ResponseDueAt, env.UserID)
			if err != nil {
				return nil, err
			}
			gid64, _ := res.LastInsertId()
			g, err := aiScanSMSGrievance(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGrievanceColumns+` FROM sms_grievances WHERE id = ?`, int(gid64)))
			if err != nil {
				return nil, err
			}
			g.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			env.AuditWrite("sms_grievance", int(gid64), "create_sms_grievance", g.Title)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// update_sms_grievance
	// ------------------------------------------------------------------------
	Register(Default, Tool[updateSMSGrievanceArgs]{
		Name:        "update_sms_grievance",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Apply a partial update to one SMS grievance. Moving the status to acknowledged stamps acknowledged_at automatically.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args updateSMSGrievanceArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.GrievanceID <= 0 {
				return map[string]string{"error": "grievance_id is required"}, nil
			}
			existing, err := aiScanSMSGrievance(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGrievanceColumns+` FROM sms_grievances WHERE id = ?`, args.GrievanceID))
			if err != nil {
				return map[string]string{"error": "grievance not found"}, nil
			}
			title := aiSMSKeepOr(args.Title, existing.Title)
			if strings.TrimSpace(title) == "" {
				return map[string]string{"error": "title cannot be empty"}, nil
			}
			if args.Severity != nil && !aiSMSValidEnum(*args.Severity, aiSMSGrievanceSeverities...) {
				return map[string]string{"error": "invalid severity"}, nil
			}
			newStatus := aiSMSKeepOr(args.Status, existing.Status)
			if !aiSMSValidEnum(newStatus, aiSMSGrievanceStatuses...) {
				return map[string]string{"error": "invalid status"}, nil
			}
			var ackAt *time.Time
			if newStatus == models.SMSGrievanceAcknowledged && existing.AcknowledgedAt == nil {
				now := time.Now().UTC()
				ackAt = &now
			}
			if _, err := env.DB.ExecContext(ctx, `
				UPDATE sms_grievances SET title = ?, description = ?, category = ?, severity = ?, status = ?, response_due_at = ?, response = ?, resolution = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				title, aiSMSKeepOr(args.Description, existing.Description), aiSMSKeepOr(args.Category, existing.Category),
				aiSMSKeepOr(args.Severity, existing.Severity), newStatus,
				aiSMSTimeOr(args.ResponseDueAt, existing.ResponseDueAt), aiSMSKeepOr(args.Response, existing.Response),
				aiSMSKeepOr(args.Resolution, existing.Resolution), args.GrievanceID); err != nil {
				return nil, err
			}
			if ackAt != nil {
				if _, err := env.DB.ExecContext(ctx, `UPDATE sms_grievances SET acknowledged_at = ? WHERE id = ?`, *ackAt, args.GrievanceID); err != nil {
					return nil, err
				}
			}
			g, err := aiScanSMSGrievance(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGrievanceColumns+` FROM sms_grievances WHERE id = ?`, args.GrievanceID))
			if err != nil {
				return nil, err
			}
			g.StakeholderName = aiSMSStakeholderName(ctx, env.DB, g.StakeholderID)
			env.AuditWrite("sms_grievance", args.GrievanceID, "update_sms_grievance", g.Title)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// escalate_sms_grievance
	// ------------------------------------------------------------------------
	Register(Default, Tool[escalateSMSGrievanceArgs]{
		Name:        "escalate_sms_grievance",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Escalate an SMS grievance: marks it escalated and increments its escalation level.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args escalateSMSGrievanceArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.GrievanceID <= 0 {
				return map[string]string{"error": "grievance_id is required"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `UPDATE sms_grievances SET escalated = 1, escalation_level = escalation_level + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, args.GrievanceID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "grievance not found"}, nil
			}
			g, err := aiScanSMSGrievance(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGrievanceColumns+` FROM sms_grievances WHERE id = ?`, args.GrievanceID))
			if err != nil {
				return nil, err
			}
			g.StakeholderName = aiSMSStakeholderName(ctx, env.DB, g.StakeholderID)
			env.AuditWrite("sms_grievance", args.GrievanceID, "escalate_sms_grievance", g.Title)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_decision
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSDecisionArgs]{
		Name:        "create_sms_decision",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record a decision in the SMS decision log for a stakeholder (decisions belong in relationship history).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSDecisionArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			aiSMSTrimRequired(&args.Title)
			if args.Title == "" {
				return map[string]string{"error": "title is required"}, nil
			}
			if args.EntityType != nil && !aiSMSValidEnum(*args.EntityType, models.SMSEntityPortfolio, models.SMSEntityProgramme, models.SMSEntityProject) {
				return map[string]string{"error": "invalid entity_type"}, nil
			}
			decidedAt := args.DecidedAt
			if decidedAt == nil {
				now := time.Now().UTC()
				decidedAt = &now
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_decisions (stakeholder_id, title, decision, context, decided_at, decided_by_user_id, entity_type, entity_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, args.Title, args.Decision, args.Context, decidedAt, env.UserID, args.EntityType, args.EntityID)
			if err != nil {
				return nil, err
			}
			did64, _ := res.LastInsertId()
			d, err := aiScanSMSDecision(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSDecisionColumns+` FROM sms_decisions WHERE id = ?`, int(did64)))
			if err != nil {
				return nil, err
			}
			d.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			env.AuditWrite("sms_decision", int(did64), "create_sms_decision", d.Title)
			return d, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_group
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSGroupArgs]{
		Name:        "create_sms_group",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create a named SMS stakeholder group, optionally seeding initial members.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSGroupArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			aiSMSTrimRequired(&args.Name)
			if args.Name == "" {
				return map[string]string{"error": "name is required"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_stakeholder_groups (name, description) VALUES (?, ?)`, args.Name, args.Description)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unique") {
					return map[string]string{"error": "a group with this name already exists"}, nil
				}
				return nil, err
			}
			gid64, _ := res.LastInsertId()
			for _, sid := range args.StakeholderIDs {
				if sid <= 0 {
					continue
				}
				if !aiSMSStakeholderExists(ctx, env.DB, sid) {
					return map[string]string{"error": "stakeholder not found: " + fmt.Sprintf("%d", sid)}, nil
				}
				if _, err := env.DB.ExecContext(ctx, `INSERT INTO sms_stakeholder_group_members (group_id, stakeholder_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, int(gid64), sid); err != nil {
					return nil, err
				}
			}
			g, err := aiScanSMSGroup(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSGroupColumns+` FROM sms_stakeholder_groups WHERE id = ?`, int(gid64)))
			if err != nil {
				return nil, err
			}
			var count int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_group_members WHERE group_id = ?`, int(gid64)).Scan(&count); err != nil {
				return nil, err
			}
			g.MemberCount = count
			env.AuditWrite("sms_stakeholder_group", int(gid64), "create_sms_group", g.Name)
			return g, nil
		},
	})

	// ------------------------------------------------------------------------
	// add_sms_group_member
	// ------------------------------------------------------------------------
	Register(Default, Tool[addSMSGroupMemberArgs]{
		Name:        "add_sms_group_member",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Add a stakeholder to an SMS stakeholder group.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args addSMSGroupMemberArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.GroupID <= 0 || args.StakeholderID <= 0 {
				return map[string]string{"error": "group_id and stakeholder_id are required"}, nil
			}
			var groupCount, stakeholderCount int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_groups WHERE id = ?`, args.GroupID).Scan(&groupCount); err != nil || groupCount == 0 {
				return map[string]string{"error": "group not found"}, nil
			}
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders WHERE id = ?`, args.StakeholderID).Scan(&stakeholderCount); err != nil || stakeholderCount == 0 {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_stakeholder_group_members (group_id, stakeholder_id) VALUES (?, ?)`, args.GroupID, args.StakeholderID)
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unique") {
					return map[string]string{"error": "stakeholder is already a member of this group"}, nil
				}
				return nil, err
			}
			mid64, _ := res.LastInsertId()
			var m models.SMSStakeholderGroupMember
			if err := env.DB.QueryRowContext(ctx, `SELECT m.id, m.group_id, m.stakeholder_id, s.name FROM sms_stakeholder_group_members m JOIN sms_stakeholders s ON s.id = m.stakeholder_id WHERE m.id = ?`, int(mid64)).
				Scan(&m.ID, &m.GroupID, &m.StakeholderID, &m.Name); err != nil {
				return nil, err
			}
			env.AuditWrite("sms_stakeholder_group", args.GroupID, "add_sms_group_member", m.Name)
			return m, nil
		},
	})

	// ------------------------------------------------------------------------
	// add_sms_stakeholder_entity
	// ------------------------------------------------------------------------
	Register(Default, Tool[addSMSStakeholderEntityArgs]{
		Name:        "add_sms_stakeholder_entity",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Link an SMS stakeholder to a P3M portfolio, programme, or project (shared governance entity).",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args addSMSStakeholderEntityArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			if !aiSMSStakeholderExists(ctx, env.DB, args.StakeholderID) {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			entityType := strings.TrimSpace(args.EntityType)
			if !aiSMSValidEnum(entityType, models.SMSEntityPortfolio, models.SMSEntityProgramme, models.SMSEntityProject) || args.EntityID <= 0 {
				return map[string]string{"error": "entity_type must be portfolio, programme or project and entity_id is required"}, nil
			}
			if !aiSMSP3MEntityExists(ctx, env.DB, entityType, args.EntityID) {
				return map[string]string{"error": "P3M " + entityType + " not found"}, nil
			}
			var dup int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholder_entities WHERE stakeholder_id = ? AND entity_type = ? AND entity_id = ?`, args.StakeholderID, entityType, args.EntityID).Scan(&dup); err != nil {
				return nil, err
			}
			if dup > 0 {
				return map[string]string{"error": "stakeholder is already linked to this " + entityType}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_stakeholder_entities (stakeholder_id, entity_type, entity_id) VALUES (?, ?, ?)`, args.StakeholderID, entityType, args.EntityID)
			if err != nil {
				return nil, err
			}
			linkID64, _ := res.LastInsertId()
			var e models.SMSStakeholderEntity
			if err := env.DB.QueryRowContext(ctx, `SELECT id, stakeholder_id, entity_type, entity_id, created_at FROM sms_stakeholder_entities WHERE id = ?`, int(linkID64)).
				Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.CreatedAt); err != nil {
				return nil, err
			}
			e.EntityTitle = aiSMSEntityTitle(ctx, env.DB, e.EntityType, e.EntityID)
			env.AuditWrite("sms_stakeholder_entity", int(linkID64), "add_sms_stakeholder_entity", e.EntityTitle)
			return e, nil
		},
	})

	// ------------------------------------------------------------------------
	// create_sms_transfer
	// ------------------------------------------------------------------------
	Register(Default, Tool[createSMSTransferArgs]{
		Name:        "create_sms_transfer",
		Group:       CapabilitySMS,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record an ownership handover for a stakeholder. When all three review flags are set the transfer completes immediately and ownership moves to the receiving user.",
		Scopes:      []string{auth.ScopeSMSWrite},
		Run: func(ctx context.Context, env *Env, args createSMSTransferArgs) (any, error) {
			ok, err := aiRequireSMSManage(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.StakeholderID <= 0 {
				return map[string]string{"error": "stakeholder_id is required"}, nil
			}
			var existingOwner *int
			if err := env.DB.QueryRowContext(ctx, `SELECT owner_user_id FROM sms_stakeholders WHERE id = ?`, args.StakeholderID).Scan(&existingOwner); err != nil {
				return map[string]string{"error": "stakeholder not found"}, nil
			}
			fromID := existingOwner
			if args.ToUserID != nil {
				if !aiSMSUserIDExists(ctx, env.DB, *args.ToUserID) {
					return map[string]string{"error": "receiving user not found"}, nil
				}
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO sms_ownership_transfers (stakeholder_id, from_user_id, to_user_id, reason, history_reviewed, risks_reviewed, commitments_reviewed, notes, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.StakeholderID, fromID, args.ToUserID, args.Reason, args.HistoryReviewed, args.RisksReviewed, args.CommitmentsReviewed, args.Notes, env.UserID)
			if err != nil {
				return nil, err
			}
			tid64, _ := res.LastInsertId()
			t, err := aiScanSMSTransfer(env.DB.QueryRowContext(ctx, `SELECT `+aiSMSTransferColumns+` FROM sms_ownership_transfers WHERE id = ?`, int(tid64)))
			if err != nil {
				return nil, err
			}
			if t.HistoryReviewed && t.RisksReviewed && t.CommitmentsReviewed && t.CompletedAt == nil {
				now := time.Now().UTC()
				if _, err := env.DB.ExecContext(ctx, `UPDATE sms_ownership_transfers SET completed_at = ? WHERE id = ?`, now, int(tid64)); err != nil {
					return nil, err
				}
				if t.ToUserID != nil {
					if _, err := env.DB.ExecContext(ctx, `UPDATE sms_stakeholders SET owner_user_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, *t.ToUserID, t.StakeholderID); err != nil {
						return nil, err
					}
				}
				t.CompletedAt = &now
			}
			t.StakeholderName = aiSMSStakeholderName(ctx, env.DB, args.StakeholderID)
			env.AuditWrite("sms_ownership_transfer", int(tid64), "create_sms_transfer", t.StakeholderName)
			return t, nil
		},
	})
}
