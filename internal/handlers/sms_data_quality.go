package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"windshift/internal/models"
)

// smsReviewCadenceDays maps review cadences to their interval in days.
var smsReviewCadenceDays = map[string]int{
	models.SMSReviewMonthly:    30,
	models.SMSReviewQuarterly:  91,
	models.SMSReviewSemiAnnual: 182,
	models.SMSReviewAnnual:     365,
}

// dqStakeholderRef identifies one register record in data-quality lists.
type dqStakeholderRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// dqStaleRecord is a stakeholder with no engagement within the window.
type dqStaleRecord struct {
	dqStakeholderRef
	LastEngagementAt *time.Time `json:"last_engagement_at,omitempty"`
	DaysSince        int        `json:"days_since"`
}

// dqOverdueReview is a stakeholder whose review cadence has lapsed.
type dqOverdueReview struct {
	dqStakeholderRef
	ReviewCadence  string     `json:"review_cadence"`
	LastReviewedAt *time.Time `json:"last_reviewed_at,omitempty"`
	DueDate        time.Time  `json:"due_date"`
}

// dqCompleteness aggregates the WI-1.4 minimum-field completeness flags.
type dqCompleteness struct {
	Percent            float64        `json:"percent"`
	Complete           int            `json:"complete"`
	Incomplete         int            `json:"incomplete"`
	MissingFieldCounts map[string]int `json:"missing_field_counts"`
}

// dqDuplicates summarises the WI-1.3 duplicate-candidate matcher output.
type dqDuplicates struct {
	Groups  int `json:"groups"`
	Records int `json:"records_in_groups"`
}

// dqEngagementCompliance reports tier 1/2 coverage by active plans.
type dqEngagementCompliance struct {
	Tier12Total    int     `json:"tier_1_2_total"`
	WithActivePlan int     `json:"with_active_plan"`
	Percent        float64 `json:"percent"`
}

// dqCrossStore summarises the WI-0.2 cross-store duplicate report.
type dqCrossStore struct {
	Matches    int `json:"matches"`
	Actionable int `json:"actionable"`
}

// GetDataQuality computes the register data-quality report (WI-3.3, Std §17):
// completeness, missing owners, stale records, overdue reviews, duplicate
// candidates, engagement compliance and cross-store duplicates. All metrics
// are query-backed; exited stakeholders are excluded from activity metrics.
func (h *SMSHandler) GetDataQuality(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	staleDays := 90
	if v := r.URL.Query().Get("stale_days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			respondBadRequest(w, r, "invalid stale_days")
			return
		}
		staleDays = n
	}

	rows, err := h.db.QueryContext(ctx, `SELECT `+smsStakeholderColumns+` FROM sms_stakeholders ORDER BY id`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	all := make([]*models.SMSStakeholder, 0)
	for rows.Next() {
		s, err := scanSMSStakeholder(rows)
		if err != nil {
			rows.Close()
			respondInternalError(w, r, err)
			return
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	activePlanIDs := make(map[int]bool)
	pr, err := h.db.QueryContext(ctx, `SELECT stakeholder_id FROM sms_engagement_plans WHERE status = ?`, models.SMSPlanStatusActive)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for pr.Next() {
		var sid int
		if err := pr.Scan(&sid); err != nil {
			pr.Close()
			respondInternalError(w, r, err)
			return
		}
		activePlanIDs[sid] = true
	}
	if err := pr.Err(); err != nil {
		pr.Close()
		respondInternalError(w, r, err)
		return
	}
	pr.Close()

	now := time.Now().UTC()

	completeness := dqCompleteness{MissingFieldCounts: map[string]int{}}
	missingOwner := make([]dqStakeholderRef, 0)
	stale := make([]dqStaleRecord, 0)
	overdueReviews := make([]dqOverdueReview, 0)
	dupGroups := make(map[string][]int)
	compliance := dqEngagementCompliance{}

	total := len(all)
	activeTotal := 0
	for _, s := range all {
		smsFillCompleteness(s)
		if !s.MinimumFieldsComplete {
			completeness.Incomplete++
			for _, f := range s.MissingFields {
				completeness.MissingFieldCounts[f]++
			}
		} else {
			completeness.Complete++
		}

		key := smsDuplicateKey(s.Name, s.Email, s.Phone)
		dupGroups[key] = append(dupGroups[key], s.ID)

		if s.LifecycleStage == models.SMSLifecycleExited {
			continue
		}
		activeTotal++

		if (s.Tier == models.SMSTier1 || s.Tier == models.SMSTier2) && s.OwnerUserID == nil && s.BackupOwnerUserID == nil {
			missingOwner = append(missingOwner, dqStakeholderRef{ID: s.ID, Name: s.Name, Tier: s.Tier})
		}

		baseline := s.CreatedAt
		if s.LastEngagementAt != nil {
			baseline = *s.LastEngagementAt
		}
		daysSince := int(now.Sub(baseline).Hours() / 24)
		if daysSince >= staleDays {
			stale = append(stale, dqStaleRecord{dqStakeholderRef: dqStakeholderRef{ID: s.ID, Name: s.Name, Tier: s.Tier}, LastEngagementAt: s.LastEngagementAt, DaysSince: daysSince})
		}

		cadenceDays := smsReviewCadenceDays[s.ReviewCadence]
		if cadenceDays > 0 {
			reviewBase := s.CreatedAt
			if s.LastReviewedAt != nil {
				reviewBase = *s.LastReviewedAt
			}
			due := reviewBase.AddDate(0, 0, cadenceDays)
			if now.After(due) {
				overdueReviews = append(overdueReviews, dqOverdueReview{dqStakeholderRef: dqStakeholderRef{ID: s.ID, Name: s.Name, Tier: s.Tier}, ReviewCadence: s.ReviewCadence, LastReviewedAt: s.LastReviewedAt, DueDate: due})
			}
		}

		if s.Tier == models.SMSTier1 || s.Tier == models.SMSTier2 {
			compliance.Tier12Total++
			if activePlanIDs[s.ID] {
				compliance.WithActivePlan++
			}
		}
	}

	duplicates := dqDuplicates{}
	for _, ids := range dupGroups {
		if len(ids) > 1 {
			duplicates.Groups++
			duplicates.Records += len(ids)
		}
	}

	crossStore, err := h.smsCountCrossStoreDuplicates(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Delivery (roadworks scenario): structural checks over nodes, campaigns,
	// RACI matrices and work packages.
	var buildingsNoOccupants, activeCampaignsNoAudience int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_nodes n JOIN sms_node_kinds k ON k.id = n.kind_id WHERE k.key = 'building' AND NOT EXISTS (SELECT 1 FROM sms_node_members m WHERE m.node_id = n.id)`).Scan(&buildingsNoOccupants); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_notification_campaigns c WHERE c.status IN ('scheduled','in_progress') AND NOT EXISTS (SELECT 1 FROM sms_notifications n WHERE n.campaign_id = c.id)`).Scan(&activeCampaignsNoAudience); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var raciActivitiesMissingAccountable int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT a.raci_id, a.activity_code FROM sms_raci_assignments a GROUP BY a.raci_id, a.activity_code HAVING SUM(CASE WHEN a.value = 'a' THEN 1 ELSE 0 END) = 0)`).Scan(&raciActivitiesMissingAccountable); err != nil {
		respondInternalError(w, r, err)
		return
	}
	var workPackagesNoContractor int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_work_packages WHERE contractor_stakeholder_id IS NULL AND status IN ('planned','in_progress')`).Scan(&workPackagesNoContractor); err != nil {
		respondInternalError(w, r, err)
		return
	}

	if activeTotal > 0 {
		completeness.Percent = round1(float64(completeness.Complete) / float64(activeTotal) * 100)
	}
	if compliance.Tier12Total > 0 {
		compliance.Percent = round1(float64(compliance.WithActivePlan) / float64(compliance.Tier12Total) * 100)
	}

	respondJSONOK(w, map[string]any{
		"generated_at":           now.Format(time.RFC3339),
		"stale_days":             staleDays,
		"totals":                 map[string]int{"stakeholders": total, "active": activeTotal},
		"completeness":           completeness,
		"missing_owner":          missingOwner,
		"stale_records":          stale,
		"overdue_reviews":        overdueReviews,
		"duplicate_candidates":   duplicates,
		"engagement_compliance":  compliance,
		"cross_store_duplicates": crossStore,
		"delivery": map[string]any{
			"buildings_without_occupants":         buildingsNoOccupants,
			"active_campaigns_no_audience":        activeCampaignsNoAudience,
			"raci_activities_missing_accountable": raciActivitiesMissingAccountable,
			"work_packages_without_contractor":    workPackagesNoContractor,
		},
	})
}

// smsCountCrossStoreDuplicates counts identity matches between the SMS
// register and the p3m/srm stores (WI-0.2), split into already-linked and
// actionable rows.
func (h *SMSHandler) smsCountCrossStoreDuplicates(ctx context.Context) (dqCrossStore, error) {
	var out dqCrossStore
	rows, err := h.db.QueryContext(ctx, smsCrossStoreDuplicateQuery)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var row crossStoreDuplicateRow
		if err := rows.Scan(&row.Store, &row.ID, &row.Name, &row.Email, &row.LinkedSMSID, &row.CandidateSMSID, &row.CandidateName, &row.MatchOn); err != nil {
			return out, err
		}
		out.Matches++
		if row.LinkedSMSID == nil || *row.LinkedSMSID != row.CandidateSMSID {
			out.Actionable++
		}
	}
	return out, rows.Err()
}

// round1 rounds a percentage to one decimal place.
func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
