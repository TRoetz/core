package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"windshift/internal/database"
	"windshift/internal/logger"
)

// ErasurePolicyVersion identifies the GDPR retention-aware erasure policy this
// execution applied. Bump when the policy's data-category matrix changes so
// erasure evidence stays interpretable after the fact.
const ErasurePolicyVersion = "2026-09-14.1"

// UserErasureInput carries the DSAR intake evidence supplied at filing time.
type UserErasureInput struct {
	// RequestedBy records where the erasure request came from — the data
	// subject's email address or an intake-channel reference.
	RequestedBy string
	// RequestedAt is when the controller received the request. Zero defaults
	// to execution time.
	RequestedAt time.Time
	// Notes optionally records the controller's decision context.
	Notes string
}

// UserErasureEvidence is the completion evidence persisted for every erasure.
type UserErasureEvidence struct {
	UserID        int       `json:"user_id"`
	RequestedBy   string    `json:"requested_by"`
	RequestedAt   time.Time `json:"requested_at"`
	ApprovedBy    int       `json:"approved_by"`
	ExecutedAt    time.Time `json:"executed_at"`
	PolicyVersion string    `json:"policy_version"`
}

// ErrUserAlreadyErased refuses a second erasure — Article 17 execution is
// irreversible and recorded once.
var ErrUserAlreadyErased = errors.New("user has already been erased")

// EraseUser executes an Article 17 erasure on top of the security-offboarding
// primitive. The two are distinct operations with distinct purposes:
// offboarding is reversible-in-effect security deactivation (anonymize,
// revoke, deactivate), while erasure is the DSAR decision record — the user
// row is pseudonymized (deleted-user-N) and work history is retained under
// that pseudonym per the approved policy; audit logs are intentionally
// untouched (their pseudonymized user IDs are retained per the policy's audit
// justification). Users not yet offboarded are offboarded first; an already
// offboarded user is erasure-ready as-is.
func EraseUser(db database.Database, userID int, actor AuditActor, input UserErasureInput, notificationDeleter UserNotificationDeleter, invalidators ...*AuthorizationCacheInvalidator) (UserErasureEvidence, OffboardResult, error) {
	var evidence UserErasureEvidence
	var offboard OffboardResult

	if input.RequestedBy == "" {
		return evidence, offboard, NewServiceError(400, "requested_by is required (DSAR intake reference)")
	}
	requestedAt := input.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now()
	}

	var erasedAt sql.NullTime
	if err := db.QueryRow(`SELECT erased_at FROM users WHERE id = ?`, userID).Scan(&erasedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return evidence, offboard, NewServiceError(404, "User not found")
		}
		return evidence, offboard, fmt.Errorf("load user erasure state: %w", err)
	}
	if erasedAt.Valid {
		return evidence, offboard, ErrUserAlreadyErased
	}

	// Deactivation precedes erasure. The offboarded_at lifecycle state blocks
	// every reactivation path; erasure additionally records the DSAR decision.
	var offboardedAt sql.NullTime
	var err error
	if err = db.QueryRow(`SELECT offboarded_at FROM users WHERE id = ?`, userID).Scan(&offboardedAt); err != nil {
		return evidence, offboard, fmt.Errorf("load user offboarding state: %w", err)
	}
	if !offboardedAt.Valid {
		offboard, err = OffboardUser(db, userID, notificationDeleter, invalidators...)
		if err != nil {
			return evidence, offboard, fmt.Errorf("offboard before erasure: %w", err)
		}
	}

	executedAt := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return evidence, offboard, fmt.Errorf("failed to begin erasure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE users SET erased_at = ?, updated_at = ? WHERE id = ? AND erased_at IS NULL`, executedAt, executedAt, userID); err != nil {
		return evidence, offboard, fmt.Errorf("failed to record erasure: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO user_erasure_records (user_id, requested_by, requested_at, approved_by, executed_at, policy_version, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, input.RequestedBy, requestedAt, actor.UserID, executedAt, ErasurePolicyVersion, nullErasureNotes(input.Notes)); err != nil {
		return evidence, offboard, fmt.Errorf("failed to record erasure evidence: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return evidence, offboard, fmt.Errorf("failed to commit erasure transaction: %w", err)
	}

	auditDetails := map[string]any{
		"requested_by":   input.RequestedBy,
		"requested_at":   requestedAt.Format(time.RFC3339),
		"policy_version": ErasurePolicyVersion,
		"policy_summary": "audit_logs_retained_pseudonymized; work_history_retained_pseudonymized; backups_age_out_30d",
	}
	_ = logger.LogAudit(db, logger.AuditEvent{
		UserID:       actor.UserID,
		Username:     actor.Username,
		IPAddress:    actor.IPAddress,
		UserAgent:    actor.UserAgent,
		ActionType:   logger.ActionUserErase,
		ResourceType: logger.ResourceUser,
		ResourceID:   &userID,
		Success:      true,
		Details:      auditDetails,
	})

	return UserErasureEvidence{
		UserID:        userID,
		RequestedBy:   input.RequestedBy,
		RequestedAt:   requestedAt,
		ApprovedBy:    actor.UserID,
		ExecutedAt:    executedAt,
		PolicyVersion: ErasurePolicyVersion,
	}, offboard, nil
}

func nullErasureNotes(notes string) any {
	if notes == "" {
		return nil
	}
	return notes
}
