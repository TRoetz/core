// SMS (Stakeholder Management System) module handlers.
//
// The module is gated by the global permission models.PermissionSMSView at the
// route level; every handler here additionally requires an authenticated user.
// Sub-resource CRUD lives in sms_subresources.go, cross-stakeholder lists,
// timeline and CSV export live in sms_activity.go (same package).
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// SMSHandler serves the /api/sms/* endpoints for the stakeholder management
// system module.
type SMSHandler struct {
	db database.Database
	// llmManager enables AI assist (WI-6.5); nil keeps the feature off.
	llmManager llmFeatureResolver
}

func NewSMSHandler(db database.Database) *SMSHandler {
	return &SMSHandler{db: db}
}

const smsStakeholderColumns = `id, external_id, name, title, organization, email, phone, stakeholder_type, category, tier, business_unit, role_classification, influence_level, interest_level, interests, engagement_status, lifecycle_stage, relationship_status, trust_level, risk_rating, communication_preferences, owner_user_id, backup_owner_user_id, executive_sponsor_user_id, last_engagement_at, next_engagement_at, info_classification, consent_status, retention_category, last_reviewed_at, review_cadence, location_lat, location_lon, location_text, custom_fields, notes, created_at, updated_at`

func scanSMSStakeholder(row interface{ Scan(dest ...any) error }) (*models.SMSStakeholder, error) {
	s := &models.SMSStakeholder{}
	err := row.Scan(&s.ID, &s.ExternalID, &s.Name, &s.Title, &s.Organization, &s.Email, &s.Phone, &s.StakeholderType, &s.Category, &s.Tier, &s.BusinessUnit, &s.RoleClassification, &s.InfluenceLevel, &s.InterestLevel, &s.Interests, &s.EngagementStatus, &s.LifecycleStage, &s.RelationshipStatus, &s.TrustLevel, &s.RiskRating, &s.CommunicationPreferences, &s.OwnerUserID, &s.BackupOwnerUserID, &s.ExecutiveSponsorUserID, &s.LastEngagementAt, &s.NextEngagementAt, &s.InfoClassification, &s.ConsentStatus, &s.RetentionCategory, &s.LastReviewedAt, &s.ReviewCadence, &s.LocationLat, &s.LocationLon, &s.LocationText, &s.CustomFields, &s.Notes, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// smsFillCompleteness computes the register's minimum-field completeness flags
// for a stakeholder: every tier needs contact details (email or phone);
// tiers 1-2 additionally need title, an owner and a business unit; tier 1
// also needs a risk rating.
func smsFillCompleteness(s *models.SMSStakeholder) {
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

// smsValidEnum reports whether value is one of the allowed enum values.
func smsValidEnum(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

var (
	smsRoleClassifications = []string{models.SMSRoleKey, models.SMSRoleDecisionMaker, models.SMSRoleSupport, models.SMSRoleInfluencer, models.SMSRoleObserver}
	smsEngagementStatuses  = []string{models.SMSEngagementPassive, models.SMSEngagementEngaged, models.SMSEngagementActive, models.SMSEngagementDisengaged}
	smsLifecycleStages     = []string{models.SMSLifecycleOnboarding, models.SMSLifecycleActive, models.SMSLifecycleRetained, models.SMSLifecycleExited}
	smsRelationshipStatuses = []string{models.SMSRelationshipAdvocate, models.SMSRelationshipSupportive, models.SMSRelationshipNeutral, models.SMSRelationshipConcerned, models.SMSRelationshipOpposed}
	smsTiers                = []string{models.SMSTier1, models.SMSTier2, models.SMSTier3, models.SMSTier4}
	smsStakeholderTypes     = []string{models.SMSTypeManaWhenua, models.SMSTypeElectedMember, models.SMSTypeGovernmentAgency, models.SMSTypeRegionalAuthority, models.SMSTypeRegulatoryPartner, models.SMSTypeInfrastructurePartner, models.SMSTypeBusiness, models.SMSTypeDeveloper, models.SMSTypeBusinessAssociation, models.SMSTypeStrategicSupplier, models.SMSTypeUtilityProvider, models.SMSTypeCommunityOrganisation, models.SMSTypeIndustryGroup, models.SMSTypeAdvocacyGroup, models.SMSTypeEducationalInstitution, models.SMSTypeMajorLandowner, models.SMSTypeProjectStakeholder, models.SMSTypeOther}
	smsTrustLevels          = []string{"very_high", "high", "moderate", "low", "critical"}
	smsInfoClassifications  = []string{models.SMSInfoPublic, models.SMSInfoInternal, models.SMSInfoConfidential, models.SMSInfoRestricted}
	smsConsentStatuses      = []string{models.SMSConsentNotRequired, models.SMSConsentGranted, models.SMSConsentDeclined, models.SMSConsentWithdrawn, models.SMSConsentUnknown}
	smsReviewCadences       = []string{models.SMSReviewMonthly, models.SMSReviewQuarterly, models.SMSReviewSemiAnnual, models.SMSReviewAnnual}
	smsChannels            = []string{models.SMSChannelEmail, models.SMSChannelPhone, models.SMSChannelMeeting, models.SMSChannelInPerson, models.SMSChannelSlack, models.SMSChannelTeams, models.SMSChannelOther}
	smsDirections          = []string{models.SMSDirectionOutbound, models.SMSDirectionInbound}
	smsSentiments          = []string{models.SMSSentimentPositive, models.SMSSentimentNeutral, models.SMSSentimentNegative}
	smsPlanFrequencies     = []string{models.SMSFrequencyDaily, models.SMSFrequencyWeekly, models.SMSFrequencyBiweekly, models.SMSFrequencyMonthly, models.SMSFrequencyQuarterly, models.SMSFrequencyAnnual, models.SMSFrequencyAsNeeded}
	smsPlanStatuses        = []string{models.SMSPlanStatusDraft, models.SMSPlanStatusActive, models.SMSPlanStatusPaused, models.SMSPlanStatusCompleted}
	smsIssueKinds          = []string{models.SMSIssueKindIssue, models.SMSIssueKindRisk}
	smsPriorities          = []string{models.SMSPriorityLow, models.SMSPriorityMedium, models.SMSPriorityHigh, models.SMSPriorityCritical}
	smsIssueStatuses       = []string{models.SMSIssueStatusOpen, models.SMSIssueStatusInProgress, models.SMSIssueStatusEscalated, models.SMSIssueStatusResolved, models.SMSIssueStatusClosed}
	smsTaskStatuses        = []string{models.SMSTaskStatusOpen, models.SMSTaskStatusInProgress, models.SMSTaskStatusDone, models.SMSTaskStatusCancelled}
	smsFeedbackSources     = []string{models.SMSFeedbackSourceForm, models.SMSFeedbackSourceSurvey, models.SMSFeedbackSourceEmail, models.SMSFeedbackSourceMeeting, models.SMSFeedbackSourceOther}
	smsFeedbackResolutions = []string{models.SMSFeedbackOpen, models.SMSFeedbackAddressed, models.SMSFeedbackResolved}
	smsGoalStatuses        = []string{models.SMSGoalOnTrack, models.SMSGoalAtRisk, models.SMSGoalMissed, models.SMSGoalAchieved}
	smsApproaches          = []string{models.SMSApproachInform, models.SMSApproachConsult, models.SMSApproachInvolve, models.SMSApproachCollaborate, models.SMSApproachPartner}
	smsIssueCategories     = []string{models.SMSCategoryRelationship, models.SMSCategoryReputation, models.SMSCategoryDelivery, models.SMSCategoryCommunication, models.SMSCategoryDependency, models.SMSCategoryKnowledgeLoss, models.SMSCategoryCommitment, models.SMSCategoryOther}
	smsCommitmentDirections = []string{models.SMSCommitmentFromUs, models.SMSCommitmentFromThem, models.SMSCommitmentMutual}
	smsCommitmentStatuses  = []string{models.SMSCommitmentOpen, models.SMSCommitmentInProgress, models.SMSCommitmentAtRisk, models.SMSCommitmentCompleted, models.SMSCommitmentCancelled}
	smsGrievanceSeverities = []string{models.SMSGrievanceLow, models.SMSGrievanceMedium, models.SMSGrievanceHigh, models.SMSGrievanceCritical}
	smsGrievanceStatuses   = []string{models.SMSGrievanceReceived, models.SMSGrievanceAcknowledged, models.SMSGrievanceInvestigating, models.SMSGrievanceResponded, models.SMSGrievanceClosed}
)

// smsHealthScore computes the 0-100 stakeholder health score:
// relationship base (five-point scale) + engagement base minus penalties for open
// critical and escalated issues, clamped to [0,100].
func smsHealthScore(relationship, engagement string, openCritical, escalatedOpen int) int {
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

// smsIssuePressure returns per-stakeholder counts of open critical issues and
// escalated open issues. "Open" means status IN ('open','in_progress','escalated').
func (h *SMSHandler) smsIssuePressure(ctx context.Context) (map[int]int, map[int]int, error) {
	critical := make(map[int]int)
	escalated := make(map[int]int)
	rows, err := h.db.QueryContext(ctx, `SELECT stakeholder_id, priority, status FROM sms_issues WHERE status IN ('open','in_progress','escalated')`)
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

// smsWithHealth fills the computed HealthScore of one stakeholder.
func (h *SMSHandler) smsWithHealth(ctx context.Context, s *models.SMSStakeholder) error {
	var crit, esc int
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN priority = 'critical' THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status = 'escalated' THEN 1 ELSE 0 END), 0) FROM sms_issues WHERE stakeholder_id = ? AND status IN ('open','in_progress','escalated')`, s.ID).Scan(&crit, &esc)
	if err != nil {
		return err
	}
	s.HealthScore = smsHealthScore(s.RelationshipStatus, s.EngagementStatus, crit, esc)
	smsFillCompleteness(s)
	return nil
}

// smsStakeholderExists reports whether a stakeholder row exists for id.
func (h *SMSHandler) smsStakeholderExists(ctx context.Context, id int) (bool, error) {
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_stakeholders WHERE id = ?`, id).Scan(&n)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type smsCountRow struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

func (h *SMSHandler) smsGroupCounts(ctx context.Context, query string, args ...any) ([]smsCountRow, error) {
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]smsCountRow, 0)
	for rows.Next() {
		var c smsCountRow
		if err := rows.Scan(&c.Value, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetDashboard returns the module overview aggregates.
func (h *SMSHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	var totals struct {
		Stakeholders int `json:"stakeholders"`
		OpenIssues   int `json:"open_issues"`
		OpenTasks    int `json:"open_tasks"`
		OpenFeedback int `json:"open_feedback"`
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_stakeholders`).Scan(&totals.Stakeholders); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE status IN ('open','in_progress','escalated')`).Scan(&totals.OpenIssues); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_tasks WHERE status IN ('open','in_progress')`).Scan(&totals.OpenTasks); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_feedback WHERE resolution_status = 'open'`).Scan(&totals.OpenFeedback); err != nil {
		respondInternalError(w, r, err)
		return
	}

	counts := func(query string) []smsCountRow {
		rows, err := h.smsGroupCounts(ctx, query)
		if err != nil {
			respondInternalError(w, r, err)
			return nil
		}
		return rows
	}
	byClassification := counts(`SELECT role_classification, COUNT(*) FROM sms_stakeholders GROUP BY role_classification ORDER BY COUNT(*) DESC, role_classification`)
	byEngagementStatus := counts(`SELECT engagement_status, COUNT(*) FROM sms_stakeholders GROUP BY engagement_status ORDER BY COUNT(*) DESC, engagement_status`)
	byLifecycleStage := counts(`SELECT lifecycle_stage, COUNT(*) FROM sms_stakeholders GROUP BY lifecycle_stage ORDER BY COUNT(*) DESC, lifecycle_stage`)
	relationshipDistribution := counts(`SELECT relationship_status, COUNT(*) FROM sms_stakeholders GROUP BY relationship_status ORDER BY COUNT(*) DESC, relationship_status`)
	openIssuesByPriority := counts(`SELECT priority, COUNT(*) FROM sms_issues WHERE status IN ('open','in_progress','escalated') GROUP BY priority ORDER BY COUNT(*) DESC, priority`)

	// Health buckets + influence/interest matrix in one pass.
	critical, escalated, err := h.smsIssuePressure(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	buckets := []smsCountRow{{Value: "0-25"}, {Value: "26-50"}, {Value: "51-75"}, {Value: "76-100"}}
	matrixRows, err := h.db.QueryContext(ctx, `SELECT id, name, influence_level, interest_level, relationship_status, engagement_status FROM sms_stakeholders ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer matrixRows.Close()
	matrix := make([]map[string]any, 0)
	for matrixRows.Next() {
		var id, influence, interest int
		var name, relationship, engagement string
		if err := matrixRows.Scan(&id, &name, &influence, &interest, &relationship, &engagement); err != nil {
			matrixRows.Close()
			respondInternalError(w, r, err)
			return
		}
		score := smsHealthScore(relationship, engagement, critical[id], escalated[id])
		idx := 0
		if score > 0 {
			idx = (score - 1) / 25
		}
		if idx > 3 {
			idx = 3
		}
		buckets[idx].Count++
		matrix = append(matrix, map[string]any{
			"stakeholder_id":   id,
			"name":             name,
			"influence_level":  influence,
			"interest_level":   interest,
		})
	}
	if err := matrixRows.Err(); err != nil {
		matrixRows.Close()
		respondInternalError(w, r, err)
		return
	}

	// Time-windowed counts are filtered in Go so the comparison is identical
	// across sqlite and postgres timestamp storage.
	now := time.Now().UTC()
	dueSoon := 0
	drows, err := h.db.QueryContext(ctx, `SELECT due_date FROM sms_tasks WHERE status IN ('open','in_progress') AND due_date IS NOT NULL`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for drows.Next() {
		var d time.Time
		if err := drows.Scan(&d); err != nil {
			drows.Close()
			respondInternalError(w, r, err)
			return
		}
		if !d.Before(now) && !d.After(now.AddDate(0, 0, 14)) {
			dueSoon++
		}
	}
	if err := drows.Err(); err != nil {
		drows.Close()
		respondInternalError(w, r, err)
		return
	}
	drows.Close()

	comms30d := 0
	crows, err := h.db.QueryContext(ctx, `SELECT created_at FROM sms_communications`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for crows.Next() {
		var t time.Time
		if err := crows.Scan(&t); err != nil {
			crows.Close()
			respondInternalError(w, r, err)
			return
		}
		if !t.Before(now.AddDate(0, 0, -30)) {
			comms30d++
		}
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		respondInternalError(w, r, err)
		return
	}
	crows.Close()

	respondJSONOK(w, map[string]any{
		"totals":                   totals,
		"by_classification":        byClassification,
		"by_engagement_status":     byEngagementStatus,
		"by_lifecycle_stage":       byLifecycleStage,
		"relationship_distribution": relationshipDistribution,
		"health_buckets":           buckets,
		"open_issues_by_priority":  openIssuesByPriority,
		"tasks_due_soon_14d":       dueSoon,
		"communications_last_30d":  comms30d,
		"matrix":                   matrix,
	})
}

// ListStakeholders returns all stakeholders with computed health scores.
// Supported filters: classification, engagement_status, lifecycle_stage,
// relationship_status and search (name/organization/email substring).
func (h *SMSHandler) ListStakeholders(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	q := r.URL.Query()
	var conds []string
	var args []any
	if v := q.Get("classification"); v != "" {
		conds = append(conds, "role_classification = ?")
		args = append(args, v)
	}
	if v := q.Get("engagement_status"); v != "" {
		conds = append(conds, "engagement_status = ?")
		args = append(args, v)
	}
	if v := q.Get("lifecycle_stage"); v != "" {
		conds = append(conds, "lifecycle_stage = ?")
		args = append(args, v)
	}
	if v := q.Get("relationship_status"); v != "" {
		conds = append(conds, "relationship_status = ?")
		args = append(args, v)
	}
	if v := q.Get("tier"); v != "" {
		if !smsValidEnum(v, smsTiers...) {
			respondBadRequest(w, r, "invalid tier")
			return
		}
		conds = append(conds, "tier = ?")
		args = append(args, v)
	}
	if v := q.Get("group_id"); v != "" {
		gid, err := strconv.Atoi(v)
		if err != nil || gid <= 0 {
			respondBadRequest(w, r, "invalid group_id")
			return
		}
		conds = append(conds, `id IN (SELECT stakeholder_id FROM sms_stakeholder_group_members WHERE group_id = ?)`)
		args = append(args, gid)
	}
	if v := strings.TrimSpace(q.Get("search")); v != "" {
		like := "%" + v + "%"
		conds = append(conds, "(name LIKE ? OR organization LIKE ? OR email LIKE ?)")
		args = append(args, like, like, like)
	}
	query := "SELECT " + smsStakeholderColumns + ` FROM sms_stakeholders`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY name COLLATE NOCASE`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	critical, escalated, err := h.smsIssuePressure(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	canSensitive := h.smsCanViewSensitive(ctx, user.ID)
	maskedCount := 0
	out := make([]models.SMSStakeholder, 0)
	for rows.Next() {
		s, err := scanSMSStakeholder(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		s.HealthScore = smsHealthScore(s.RelationshipStatus, s.EngagementStatus, critical[s.ID], escalated[s.ID])
		smsFillCompleteness(s)
		if !canSensitive && smsMaskRestricted(s) {
			maskedCount++
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		respondInternalError(w, r, err)
		return
	}
	if maskedCount > 0 {
		smsAudit(h, r, user, "sms_restricted_access", "sms_stakeholder", nil, fmt.Sprintf("list(%d)", maskedCount), map[string]any{"masked_fields": []string{"notes", "custom_fields"}})
	}
	respondJSONOK(w, out)
}

// CreateStakeholder adds a new stakeholder profile.
func (h *SMSHandler) CreateStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name                     string     `json:"name"`
		Title                    string     `json:"title"`
		Organization             string     `json:"organization"`
		Email                    string     `json:"email"`
		Phone                    string     `json:"phone"`
		ExternalID               *string    `json:"external_id,omitempty"`
		StakeholderType          *string    `json:"stakeholder_type,omitempty"`
		Category                 string     `json:"category"`
		Tier                     string     `json:"tier"`
		BusinessUnit             string     `json:"business_unit"`
		RoleClassification       string     `json:"role_classification"`
		InfluenceLevel           int        `json:"influence_level"`
		InterestLevel            int        `json:"interest_level"`
		Interests                string     `json:"interests"`
		EngagementStatus         string     `json:"engagement_status"`
		LifecycleStage           string     `json:"lifecycle_stage"`
		RelationshipStatus       string     `json:"relationship_status"`
		TrustLevel               string     `json:"trust_level"`
		RiskRating               *string    `json:"risk_rating,omitempty"`
		CommunicationPreferences string     `json:"communication_preferences"`
		OwnerUserID              *int       `json:"owner_user_id,omitempty"`
		BackupOwnerUserID        *int       `json:"backup_owner_user_id,omitempty"`
		ExecutiveSponsorUserID   *int       `json:"executive_sponsor_user_id,omitempty"`
		LastEngagementAt         *time.Time `json:"last_engagement_at,omitempty"`
		NextEngagementAt         *time.Time `json:"next_engagement_at,omitempty"`
		InfoClassification       string     `json:"info_classification"`
		ConsentStatus            string     `json:"consent_status"`
		RetentionCategory        string     `json:"retention_category"`
		LastReviewedAt           *time.Time `json:"last_reviewed_at,omitempty"`
		ReviewCadence            string     `json:"review_cadence"`
		LocationLat              *float64   `json:"location_lat,omitempty"`
		LocationLon              *float64   `json:"location_lon,omitempty"`
		LocationText             string     `json:"location_text"`
		CustomFields             string     `json:"custom_fields"`
		Notes                    string     `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	if !smsValidEnum(in.RoleClassification, smsRoleClassifications...) && in.RoleClassification != "" {
		respondBadRequest(w, r, "invalid role_classification")
		return
	}
	if !smsValidEnum(in.EngagementStatus, smsEngagementStatuses...) && in.EngagementStatus != "" {
		respondBadRequest(w, r, "invalid engagement_status")
		return
	}
	if !smsValidEnum(in.LifecycleStage, smsLifecycleStages...) && in.LifecycleStage != "" {
		respondBadRequest(w, r, "invalid lifecycle_stage")
		return
	}
	if !smsValidEnum(in.RelationshipStatus, smsRelationshipStatuses...) && in.RelationshipStatus != "" {
		respondBadRequest(w, r, "invalid relationship_status")
		return
	}
	if in.Tier != "" && !smsValidEnum(in.Tier, smsTiers...) {
		respondBadRequest(w, r, "invalid tier")
		return
	}
	if in.StakeholderType != nil && !smsValidEnum(*in.StakeholderType, smsStakeholderTypes...) {
		respondBadRequest(w, r, "invalid stakeholder_type")
		return
	}
	if in.TrustLevel != "" && !smsValidEnum(in.TrustLevel, smsTrustLevels...) {
		respondBadRequest(w, r, "invalid trust_level")
		return
	}
	if in.RiskRating != nil && !smsValidEnum(*in.RiskRating, smsPriorities...) {
		respondBadRequest(w, r, "invalid risk_rating")
		return
	}
	if in.InfoClassification != "" && !smsValidEnum(in.InfoClassification, smsInfoClassifications...) {
		respondBadRequest(w, r, "invalid info_classification")
		return
	}
	if in.ConsentStatus != "" && !smsValidEnum(in.ConsentStatus, smsConsentStatuses...) {
		respondBadRequest(w, r, "invalid consent_status")
		return
	}
	if in.ReviewCadence != "" && !smsValidEnum(in.ReviewCadence, smsReviewCadences...) {
		respondBadRequest(w, r, "invalid review_cadence")
		return
	}
	for _, lvl := range []int{in.InfluenceLevel, in.InterestLevel} {
		if lvl != 0 && (lvl < 1 || lvl > 5) {
			respondBadRequest(w, r, "influence_level and interest_level must be between 1 and 5")
			return
		}
	}
	if in.LocationLat != nil && (*in.LocationLat < -90 || *in.LocationLat > 90) {
		respondBadRequest(w, r, "location_lat must be between -90 and 90")
		return
	}
	if in.LocationLon != nil && (*in.LocationLon < -180 || *in.LocationLon > 180) {
		respondBadRequest(w, r, "location_lon must be between -180 and 180")
		return
	}
	customFields := strings.TrimSpace(in.CustomFields)
	if customFields == "" {
		customFields = "{}"
	} else if !jsonIsObject(customFields) {
		respondBadRequest(w, r, "custom_fields must be a JSON object")
		return
	}

	ctx := r.Context()
	res, err := h.db.ExecContext(ctx, `
		INSERT INTO sms_stakeholders (name, title, organization, email, phone, external_id, stakeholder_type, category, tier, business_unit, role_classification, influence_level, interest_level, interests, engagement_status, lifecycle_stage, relationship_status, trust_level, risk_rating, communication_preferences, owner_user_id, backup_owner_user_id, executive_sponsor_user_id, last_engagement_at, next_engagement_at, info_classification, consent_status, retention_category, last_reviewed_at, review_cadence, location_lat, location_lon, location_text, custom_fields, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Title, in.Organization, in.Email, in.Phone,
		in.ExternalID, in.StakeholderType, in.Category, orKeepStr(in.Tier, models.SMSTier3), in.BusinessUnit,
		orKeepStr(in.RoleClassification, models.SMSRoleSupport),
		smsLevelOrDefault(in.InfluenceLevel), smsLevelOrDefault(in.InterestLevel),
		in.Interests, orKeepStr(in.EngagementStatus, models.SMSEngagementPassive),
		orKeepStr(in.LifecycleStage, models.SMSLifecycleOnboarding),
		orKeepStr(in.RelationshipStatus, models.SMSRelationshipNeutral),
		orKeepStr(in.TrustLevel, "moderate"), in.RiskRating,
		in.CommunicationPreferences, nullInt(in.OwnerUserID), nullInt(in.BackupOwnerUserID), nullInt(in.ExecutiveSponsorUserID),
		nullTimePtr(in.LastEngagementAt), nullTimePtr(in.NextEngagementAt),
		orKeepStr(in.InfoClassification, models.SMSInfoInternal), orKeepStr(in.ConsentStatus, models.SMSConsentNotRequired), in.RetentionCategory,
		nullTimePtr(in.LastReviewedAt), orKeepStr(in.ReviewCadence, models.SMSReviewQuarterly),
		in.LocationLat, in.LocationLon, in.LocationText, customFields, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	s, err := h.smsGetStakeholder(ctx, int(id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_stakeholder_created", "sms_stakeholder", &s.ID, s.Name, map[string]any{"tier": s.Tier})
	respondJSONCreated(w, s)
}

// orKeepStr applies a column default when the client omitted an enum field.
func orKeepStr(v, fallback string) any {
	if v == "" {
		return fallback
	}
	return v
}

// smsLevelOrDefault maps 0 (omitted) to the schema default of 3.
func smsLevelOrDefault(v int) any {
	if v == 0 {
		return 3
	}
	return v
}

// jsonIsObject reports whether s parses as a JSON object.
func jsonIsObject(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}

// orPtrStr keeps the fallback pointer when v is nil (field omitted).
func orPtrStr(v, fallback *string) any {
	if v == nil {
		return fallback
	}
	return v
}

// orPtrClear applies PATCH semantics to nullable string columns: an explicit
// empty string clears the value to NULL; omission keeps the existing one.
func orPtrClear(v, fallback *string) any {
	if v == nil {
		return fallback
	}
	if *v == "" {
		return nil
	}
	return v
}

// orPtrFloat keeps the fallback coordinate when v is nil (field omitted).
func orPtrFloat(v, fallback *float64) any {
	if v == nil {
		return fallback
	}
	return v
}

// smsGetStakeholder loads one stakeholder with its computed health score.
func (h *SMSHandler) smsGetStakeholder(ctx context.Context, id int) (*models.SMSStakeholder, error) {
	s, err := scanSMSStakeholder(h.db.QueryRowContext(ctx, `SELECT `+smsStakeholderColumns+` FROM sms_stakeholders WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if err := h.smsWithHealth(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// GetStakeholder returns one stakeholder profile plus its P3M entity links and
// sub-resource counts.
func (h *SMSHandler) GetStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	s, err := h.smsGetStakeholder(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	if !h.smsCanViewSensitive(ctx, user.ID) && smsMaskRestricted(s) {
		smsAudit(h, r, user, "sms_restricted_access", "sms_stakeholder", &id, s.Name, map[string]any{"masked_fields": []string{"notes", "custom_fields"}})
	}

	erows, err := h.db.QueryContext(ctx, `SELECT id, stakeholder_id, entity_type, entity_id, created_at FROM sms_stakeholder_entities WHERE stakeholder_id = ? ORDER BY entity_type, entity_id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer erows.Close()
	entities := make([]models.SMSStakeholderEntity, 0)
	for erows.Next() {
		var e models.SMSStakeholderEntity
		if err := erows.Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.CreatedAt); err != nil {
			erows.Close()
			respondInternalError(w, r, err)
			return
		}
		entities = append(entities, e)
	}
	if err := erows.Err(); err != nil {
		erows.Close()
		respondInternalError(w, r, err)
		return
	}
	if err := h.smsFillEntityTitles(ctx, entities); err != nil {
		respondInternalError(w, r, err)
		return
	}

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
		query := "SELECT COUNT(*) FROM " + tc.table + " WHERE stakeholder_id = ?"
		if tc.cond != "" {
			query += " AND " + tc.cond
		}
		var n int
		if err := h.db.QueryRowContext(ctx, query, id).Scan(&n); err != nil {
			respondInternalError(w, r, err)
			return
		}
		counts[tc.key] = n
	}

	payload := map[string]any{}
	b, err := json.Marshal(s)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		respondInternalError(w, r, err)
		return
	}
	payload["entities"] = entities
	payload["counts"] = counts
	respondJSONOK(w, payload)
}

// UpdateStakeholder applies a partial update to one stakeholder.
func (h *SMSHandler) UpdateStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := h.smsGetStakeholder(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Name                     *string    `json:"name,omitempty"`
		Title                    *string    `json:"title,omitempty"`
		Organization             *string    `json:"organization,omitempty"`
		Email                    *string    `json:"email,omitempty"`
		Phone                    *string    `json:"phone,omitempty"`
		ExternalID               *string    `json:"external_id,omitempty"`
		StakeholderType          *string    `json:"stakeholder_type,omitempty"`
		Category                 *string    `json:"category,omitempty"`
		Tier                     *string    `json:"tier,omitempty"`
		BusinessUnit             *string    `json:"business_unit,omitempty"`
		RoleClassification       *string    `json:"role_classification,omitempty"`
		InfluenceLevel           *int       `json:"influence_level,omitempty"`
		InterestLevel            *int       `json:"interest_level,omitempty"`
		Interests                *string    `json:"interests,omitempty"`
		EngagementStatus         *string    `json:"engagement_status,omitempty"`
		LifecycleStage           *string    `json:"lifecycle_stage,omitempty"`
		RelationshipStatus       *string    `json:"relationship_status,omitempty"`
		TrustLevel               *string    `json:"trust_level,omitempty"`
		RiskRating               *string    `json:"risk_rating,omitempty"`
		CommunicationPreferences *string    `json:"communication_preferences,omitempty"`
		OwnerUserID              *int       `json:"owner_user_id,omitempty"`
		BackupOwnerUserID        *int       `json:"backup_owner_user_id,omitempty"`
		ExecutiveSponsorUserID   *int       `json:"executive_sponsor_user_id,omitempty"`
		LastEngagementAt         *time.Time `json:"last_engagement_at,omitempty"`
		NextEngagementAt         *time.Time `json:"next_engagement_at,omitempty"`
		InfoClassification       *string    `json:"info_classification,omitempty"`
		ConsentStatus            *string    `json:"consent_status,omitempty"`
		RetentionCategory        *string    `json:"retention_category,omitempty"`
		LastReviewedAt           *time.Time `json:"last_reviewed_at,omitempty"`
		ReviewCadence            *string    `json:"review_cadence,omitempty"`
		LocationLat              *float64   `json:"location_lat,omitempty"`
		LocationLon              *float64   `json:"location_lon,omitempty"`
		LocationText             *string    `json:"location_text,omitempty"`
		CustomFields             *string    `json:"custom_fields,omitempty"`
		Notes                    *string    `json:"notes,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	name := orKeep(in.Name, existing.Name)
	if name == "" {
		respondBadRequest(w, r, "name cannot be empty")
		return
	}
	if in.RoleClassification != nil && !smsValidEnum(*in.RoleClassification, smsRoleClassifications...) {
		respondBadRequest(w, r, "invalid role_classification")
		return
	}
	if in.EngagementStatus != nil && !smsValidEnum(*in.EngagementStatus, smsEngagementStatuses...) {
		respondBadRequest(w, r, "invalid engagement_status")
		return
	}
	if in.LifecycleStage != nil && !smsValidEnum(*in.LifecycleStage, smsLifecycleStages...) {
		respondBadRequest(w, r, "invalid lifecycle_stage")
		return
	}
	if in.RelationshipStatus != nil && !smsValidEnum(*in.RelationshipStatus, smsRelationshipStatuses...) {
		respondBadRequest(w, r, "invalid relationship_status")
		return
	}
	if in.Tier != nil && !smsValidEnum(*in.Tier, smsTiers...) {
		respondBadRequest(w, r, "invalid tier")
		return
	}
	// Nullable fields: an explicit empty string clears the value (applied as
	// NULL via orPtrClear); omission keeps it. Validation skips empties.
	if in.StakeholderType != nil && *in.StakeholderType != "" && !smsValidEnum(*in.StakeholderType, smsStakeholderTypes...) {
		respondBadRequest(w, r, "invalid stakeholder_type")
		return
	}
	if in.TrustLevel != nil && *in.TrustLevel != "" && !smsValidEnum(*in.TrustLevel, smsTrustLevels...) {
		respondBadRequest(w, r, "invalid trust_level")
		return
	}
	if in.RiskRating != nil && *in.RiskRating != "" && !smsValidEnum(*in.RiskRating, smsPriorities...) {
		respondBadRequest(w, r, "invalid risk_rating")
		return
	}
	if in.InfoClassification != nil && !smsValidEnum(*in.InfoClassification, smsInfoClassifications...) {
		respondBadRequest(w, r, "invalid info_classification")
		return
	}
	if in.ConsentStatus != nil && !smsValidEnum(*in.ConsentStatus, smsConsentStatuses...) {
		respondBadRequest(w, r, "invalid consent_status")
		return
	}
	if in.ReviewCadence != nil && !smsValidEnum(*in.ReviewCadence, smsReviewCadences...) {
		respondBadRequest(w, r, "invalid review_cadence")
		return
	}
	for _, lvl := range []*int{in.InfluenceLevel, in.InterestLevel} {
		if lvl != nil && (*lvl < 1 || *lvl > 5) {
			respondBadRequest(w, r, "influence_level and interest_level must be between 1 and 5")
			return
		}
	}
	if in.LocationLat != nil && (*in.LocationLat < -90 || *in.LocationLat > 90) {
		respondBadRequest(w, r, "location_lat must be between -90 and 90")
		return
	}
	if in.LocationLon != nil && (*in.LocationLon < -180 || *in.LocationLon > 180) {
		respondBadRequest(w, r, "location_lon must be between -180 and 180")
		return
	}
	customFields := orKeep(in.CustomFields, existing.CustomFields).(string)
	if !jsonIsObject(customFields) {
		respondBadRequest(w, r, "custom_fields must be a JSON object")
		return
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE sms_stakeholders SET name = ?, title = ?, organization = ?, email = ?, phone = ?, external_id = ?, stakeholder_type = ?, category = ?, tier = ?, business_unit = ?, role_classification = ?, influence_level = ?, interest_level = ?, interests = ?, engagement_status = ?, lifecycle_stage = ?, relationship_status = ?, trust_level = ?, risk_rating = ?, communication_preferences = ?, owner_user_id = ?, backup_owner_user_id = ?, executive_sponsor_user_id = ?, last_engagement_at = ?, next_engagement_at = ?, info_classification = ?, consent_status = ?, retention_category = ?, last_reviewed_at = ?, review_cadence = ?, location_lat = ?, location_lon = ?, location_text = ?, custom_fields = ?, notes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, orKeep(in.Title, existing.Title), orKeep(in.Organization, existing.Organization), orKeep(in.Email, existing.Email), orKeep(in.Phone, existing.Phone),
		orPtrClear(in.ExternalID, existing.ExternalID), orPtrClear(in.StakeholderType, existing.StakeholderType),
		orKeep(in.Category, existing.Category), orKeep(in.Tier, existing.Tier), orKeep(in.BusinessUnit, existing.BusinessUnit),
		orKeep(in.RoleClassification, existing.RoleClassification),
		intOr(in.InfluenceLevel, existing.InfluenceLevel), intOr(in.InterestLevel, existing.InterestLevel),
		orKeep(in.Interests, existing.Interests), orKeep(in.EngagementStatus, existing.EngagementStatus),
		orKeep(in.LifecycleStage, existing.LifecycleStage), orKeep(in.RelationshipStatus, existing.RelationshipStatus),
		orPtrClear(in.TrustLevel, existing.TrustLevel), orPtrClear(in.RiskRating, existing.RiskRating),
		orKeep(in.CommunicationPreferences, existing.CommunicationPreferences), nullInt(orPtrInt(in.OwnerUserID, existing.OwnerUserID)),
		nullInt(orPtrInt(in.BackupOwnerUserID, existing.BackupOwnerUserID)), nullInt(orPtrInt(in.ExecutiveSponsorUserID, existing.ExecutiveSponsorUserID)),
		orPtrTime(in.LastEngagementAt, existing.LastEngagementAt), orPtrTime(in.NextEngagementAt, existing.NextEngagementAt),
		orKeep(in.InfoClassification, existing.InfoClassification), orKeep(in.ConsentStatus, existing.ConsentStatus),
		orKeep(in.RetentionCategory, existing.RetentionCategory), orPtrTime(in.LastReviewedAt, existing.LastReviewedAt),
		orKeep(in.ReviewCadence, existing.ReviewCadence),
		orPtrFloat(in.LocationLat, existing.LocationLat), orPtrFloat(in.LocationLon, existing.LocationLon),
		orKeep(in.LocationText, existing.LocationText), customFields, orKeep(in.Notes, existing.Notes), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	s, err := h.smsGetStakeholder(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	details := map[string]any{}
	if in.OwnerUserID != nil && (existing.OwnerUserID == nil || *in.OwnerUserID != *existing.OwnerUserID) {
		oldOwner := 0
		if existing.OwnerUserID != nil {
			oldOwner = *existing.OwnerUserID
		}
		details["owner_user_id"] = fmt.Sprintf("%d->%d", oldOwner, *in.OwnerUserID)
	}
	if in.BackupOwnerUserID != nil && (existing.BackupOwnerUserID == nil || *in.BackupOwnerUserID != *existing.BackupOwnerUserID) {
		oldBackup := 0
		if existing.BackupOwnerUserID != nil {
			oldBackup = *existing.BackupOwnerUserID
		}
		details["backup_owner_user_id"] = fmt.Sprintf("%d->%d", oldBackup, *in.BackupOwnerUserID)
	}
	smsAudit(h, r, user, "sms_stakeholder_updated", "sms_stakeholder", &s.ID, s.Name, details)
	respondJSONOK(w, s)
}


// DeleteStakeholder removes one stakeholder (sub-resources cascade).
func (h *SMSHandler) DeleteStakeholder(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var name string
	if err := h.db.QueryRowContext(ctx, `SELECT name FROM sms_stakeholders WHERE id = ?`, id).Scan(&name); isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `DELETE FROM sms_stakeholders WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	smsAudit(h, r, user, "sms_stakeholder_deleted", "sms_stakeholder", &id, name, nil)
	w.WriteHeader(http.StatusNoContent)
}

var smsP3MEntityTables = map[string]string{
	models.SMSEntityPortfolio: "p3m_portfolios",
	models.SMSEntityProgramme: "p3m_programmes",
	models.SMSEntityProject:   "p3m_projects",
}

// AddStakeholderEntity links a stakeholder to a shared P3M entity. The
// referenced P3M row must exist (404) and the link must be unique (409).
func (h *SMSHandler) AddStakeholderEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		EntityType string `json:"entity_type"`
		EntityID   int    `json:"entity_id"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.EntityType = strings.TrimSpace(in.EntityType)
	table, valid := smsP3MEntityTables[in.EntityType]
	if !valid || in.EntityID <= 0 {
		respondBadRequest(w, r, "entity_type must be portfolio, programme or project and entity_id is required")
		return
	}

	ctx := r.Context()
	exists, err := h.smsStakeholderExists(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !exists {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	var n int
	if err := h.db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, in.EntityID).Scan(&n); isNotFound(err) {
		respondNotFound(w, r, "P3M "+in.EntityType+" not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM sms_stakeholder_entities WHERE stakeholder_id = ? AND entity_type = ? AND entity_id = ?`, id, in.EntityType, in.EntityID).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "stakeholder is already linked to this "+in.EntityType)
		return
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO sms_stakeholder_entities (stakeholder_id, entity_type, entity_id) VALUES (?, ?, ?)`, id, in.EntityType, in.EntityID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	linkID, _ := res.LastInsertId()

	var e models.SMSStakeholderEntity
	err = h.db.QueryRowContext(ctx, `SELECT id, stakeholder_id, entity_type, entity_id, created_at FROM sms_stakeholder_entities WHERE id = ?`, linkID).
		Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.smsFillEntityTitles(ctx, []models.SMSStakeholderEntity{e}); err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_entity_linked", "sms_stakeholder_entity", &e.ID, e.EntityTitle, map[string]any{"entity_type": e.EntityType, "entity_id": e.EntityID})
	respondJSONCreated(w, e)
}

// DeleteEntity removes one stakeholder-to-P3M-entity link by link-row id.
func (h *SMSHandler) DeleteEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var sid int
	var etype string
	var eid int
	err := h.db.QueryRowContext(ctx, `SELECT stakeholder_id, entity_type, entity_id FROM sms_stakeholder_entities WHERE id = ?`, id).Scan(&sid, &etype, &eid)
	if isNotFound(err) {
		respondNotFound(w, r, "Entity link not found")
		return
	} else if err != nil {
		respondInternalError(w, r, err)
		return
	}

	_, err = h.db.ExecContext(ctx, `DELETE FROM sms_stakeholder_entities WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	smsAudit(h, r, user, "sms_entity_unlinked", "sms_stakeholder_entity", &id, fmt.Sprintf("%s %d", etype, eid), map[string]any{"stakeholder_id": sid})
	w.WriteHeader(http.StatusNoContent)
}

// smsFillEntityTitles resolves the P3M entity title for each link using one
// query per entity type (same approach as SRM's fillP3MEntityTitles).
func (h *SMSHandler) smsFillEntityTitles(ctx context.Context, entities []models.SMSStakeholderEntity) error {
	idsByType := map[string]map[int]bool{}
	for _, e := range entities {
		if idsByType[e.EntityType] == nil {
			idsByType[e.EntityType] = map[int]bool{}
		}
		idsByType[e.EntityType][e.EntityID] = true
	}
	titles := map[string]map[int]string{}
	for t, idSet := range idsByType {
		table, ok := smsP3MEntityTables[t]
		if !ok {
			continue
		}
		idList := make([]int, 0, len(idSet))
		for id := range idSet {
			idList = append(idList, id)
		}
		args := make([]any, len(idList))
		placeholders := make([]string, len(idList))
		for i, id := range idList {
			args[i] = id
			placeholders[i] = "?"
		}
		rows, err := h.db.QueryContext(ctx, `SELECT id, title FROM `+table+` WHERE id IN (`+joinPlaceholders(placeholders)+`)`, args...)
		if err != nil {
			return err
		}
		m := map[int]string{}
		for rows.Next() {
			var id int
			var title string
			if err := rows.Scan(&id, &title); err != nil {
				rows.Close()
				return err
			}
			m[id] = title
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		titles[t] = m
	}
	for i := range entities {
		e := &entities[i]
		if m, ok := titles[e.EntityType]; ok {
			e.EntityTitle = m[e.EntityID]
		}
	}
	return nil
}

// GetP3MOptions returns the P3M entity pickers for linking stakeholders. It is
// gated on sms.view only so SMS-only users can link without p3m.view (same as
// SRM's /srm/p3m-options).
func (h *SMSHandler) GetP3MOptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	opts := func(table string) ([]map[string]any, error) {
		rows, err := h.db.QueryContext(ctx, `SELECT id, title FROM `+table+` ORDER BY title COLLATE NOCASE`)
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
		respondInternalError(w, r, err)
		return
	}
	programmes, err := opts("p3m_programmes")
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	projects, err := opts("p3m_projects")
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, map[string]any{
		"portfolios": portfolios,
		"programmes": programmes,
		"projects":   projects,
	})
}
