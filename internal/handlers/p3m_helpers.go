package handlers

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

// isNotFound reports whether a database row lookup came back empty.
func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// parseQueryInt parses an optional integer query parameter.
func parseQueryInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// nullInt renders a nullable int column argument: nil pointer binds NULL.
func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullTime converts an optional time.Time request field to a nullable column
// value; the zero time means "not provided".
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// nullTimePtr renders a *time.Time as an SQL argument (nil stays NULL).
func nullTimePtr(v *time.Time) any {
	return v
}

// joinPlaceholders joins IN-list placeholders with commas.
func joinPlaceholders(parts []string) string {
	return strings.Join(parts, ", ")
}

// p3mValidRag reports whether s is one of the three RAG states.
func p3mValidRag(s string) bool {
	switch s {
	case models.P3MRagRed, models.P3MRagAmber, models.P3MRagGreen:
		return true
	default:
		return false
	}
}

// p3mValidStage reports whether s is one of the four project stages.
func p3mValidStage(s string) bool {
	switch s {
	case models.P3MStageInitiation, models.P3MStagePlanning, models.P3MStageExecution, models.P3MStageClosure:
		return true
	default:
		return false
	}
}

// p3mApplyComputedRaid fills the derived RAID fields: risk score (impact x
// probability), priority bucket and the escalation flag for project-level
// items scoring 15 or above.
func p3mApplyComputedRaid(item *models.P3MRaidItem) {
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

// p3mStakeholderQuadrant maps power/interest scores (1-5) to the classic
// power-interest matrix quadrant; a score of 4 or more counts as high.
func p3mStakeholderQuadrant(power, interest int) string {
	switch {
	case power >= 4 && interest >= 4:
		return "manage_closely"
	case power >= 4:
		return "keep_satisfied"
	case interest >= 4:
		return "keep_informed"
	default:
		return "monitor"
	}
}
