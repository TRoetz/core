// P3M architecture review board (ARB) tracking: per-project architecture
// sign-off with a wip -> in_review -> approved | needs_work workflow. An
// approval requires the linked architecture page; a rejection writes a
// decision-register entry on the project.
package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

const p3mArchitectureReviewColumns = `id, project_id, title, description, status, reviewer_id, decided_at, comments, architecture_page_id, decision_item_id, created_by, created_at, updated_at`

// scanP3MArchitectureReview reads one p3m_architecture_reviews row.
func scanP3MArchitectureReview(row interface{ Scan(...any) error }) (models.P3MArchitectureReview, error) {
	var rv models.P3MArchitectureReview
	err := row.Scan(&rv.ID, &rv.ProjectID, &rv.Title, &rv.Description, &rv.Status, &rv.ReviewerID, &rv.DecidedAt, &rv.Comments, &rv.ArchitecturePageID, &rv.DecisionItemID, &rv.CreatedBy, &rv.CreatedAt, &rv.UpdatedAt)
	return rv, err
}

func (h *P3MHandler) getArchitectureReviewRow(ctx context.Context, id int) (models.P3MArchitectureReview, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+p3mArchitectureReviewColumns+` FROM p3m_architecture_reviews WHERE id = ?`, id)
	return scanP3MArchitectureReview(row)
}

// ListArchitectureReviews handles GET /api/p3m/architecture-reviews?project_id=.
func (h *P3MHandler) ListArchitectureReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + p3mArchitectureReviewColumns + ` FROM p3m_architecture_reviews`
	var args []any
	if pid, has := parseQueryInt(r.URL.Query().Get("project_id")); has {
		query += ` WHERE project_id = ?`
		args = append(args, pid)
	}
	query += ` ORDER BY id DESC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	reviews := make([]models.P3MArchitectureReview, 0)
	for rows.Next() {
		rv, err := scanP3MArchitectureReview(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		reviews = append(reviews, rv)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, reviews)
}

// CreateArchitectureReview handles POST /api/p3m/architecture-reviews. The new
// review starts in wip; submission for ARB and the decision are separate steps.
func (h *P3MHandler) CreateArchitectureReview(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		ProjectID   int    `json:"project_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		respondValidationError(w, r, "title is required")
		return
	}
	if _, err := h.getProjectRow(r.Context(), in.ProjectID); err != nil {
		if isNotFound(err) {
			respondNotFound(w, r, "p3m_project")
			return
		}
		respondInternalError(w, r, err)
		return
	}

	now := time.Now().UTC()
	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO p3m_architecture_reviews (project_id, title, description, status, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.ProjectID, in.Title, in.Description, models.P3MARBStatusWIP, user.ID, now, now)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	reviewID64, _ := res.LastInsertId()
	rv, err := h.getArchitectureReviewRow(r.Context(), int(reviewID64))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, rv)
}

// UpdateArchitectureReview handles PUT /api/p3m/architecture-reviews/{id}. Only
// undetermined reviews are mutable; provided fields replace the current values.
// The only status transition accepted here is wip -> in_review (submission for
// ARB); decided reviews return 409.
func (h *P3MHandler) UpdateArchitectureReview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Title              *string `json:"title,omitempty"`
		Description        *string `json:"description,omitempty"`
		Comments           *string `json:"comments,omitempty"`
		Status             string  `json:"status,omitempty"`
		ArchitecturePageID *int    `json:"architecture_page_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	rv, err := h.getArchitectureReviewRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_architecture_review")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if rv.Status == models.P3MARBStatusApproved || rv.Status == models.P3MARBStatusNeedsWork {
		respondConflict(w, r, "review is decided")
		return
	}

	newStatus := rv.Status
	if in.Status != "" {
		if in.Status != models.P3MARBStatusInReview || rv.Status != models.P3MARBStatusWIP {
			respondValidationError(w, r, "status can only move from wip to in_review")
			return
		}
		newStatus = models.P3MARBStatusInReview
	}

	var sets []string
	var args []any
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			respondValidationError(w, r, "title must not be empty")
			return
		}
		sets = append(sets, "title = ?")
		args = append(args, *in.Title)
	}
	if in.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *in.Description)
	}
	if in.Comments != nil {
		sets = append(sets, "comments = ?")
		args = append(args, *in.Comments)
	}
	if in.ArchitecturePageID != nil {
		var pageCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pages WHERE id = ?`, *in.ArchitecturePageID).Scan(&pageCount); err != nil || pageCount == 0 {
			respondBadRequest(w, r, "architecture_page_id does not reference an existing page")
			return
		}
		sets = append(sets, "architecture_page_id = ?")
		args = append(args, *in.ArchitecturePageID)
	}
	if newStatus != rv.Status {
		sets = append(sets, "status = ?")
		args = append(args, newStatus)
	}

	now := time.Now().UTC()
	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)
	if _, err := h.db.ExecContext(r.Context(), `UPDATE p3m_architecture_reviews SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		respondInternalError(w, r, err)
		return
	}

	rv, err = h.getArchitectureReviewRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, rv)
}

// DeleteArchitectureReview handles DELETE /api/p3m/architecture-reviews/{id}.
func (h *P3MHandler) DeleteArchitectureReview(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	rv, err := h.getArchitectureReviewRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_architecture_review")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_architecture_reviews WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		respondNotFound(w, r, "p3m_architecture_review")
		return
	}
	// A rejection writes a decision-register entry; remove it so deleting the
	// review leaves no orphaned register row behind.
	if rv.DecisionItemID != nil && *rv.DecisionItemID > 0 {
		if _, err := h.db.ExecContext(r.Context(), `DELETE FROM p3m_raid_items WHERE id = ?`, *rv.DecisionItemID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// DecideArchitectureReview handles POST /api/p3m/architecture-reviews/{id}/decision.
// The review must be in_review; approval requires the architecture page to be
// linked, rejection writes a decision-register entry on the project and stores
// its id on the review.
func (h *P3MHandler) DecideArchitectureReview(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Approved bool   `json:"approved"`
		Comments string `json:"comments"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	rv, err := h.getArchitectureReviewRow(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "p3m_architecture_review")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if rv.Status != models.P3MARBStatusInReview {
		respondConflict(w, r, "review must be submitted for ARB before a decision")
		return
	}

	now := time.Now().UTC()
	if in.Approved {
		if rv.ArchitecturePageID == nil {
			respondConflict(w, r, "architecture page is required for approval")
			return
		}
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE p3m_architecture_reviews SET status = ?, reviewer_id = ?, decided_at = ?, comments = ?, updated_at = ? WHERE id = ?`,
			models.P3MARBStatusApproved, user.ID, now, in.Comments, now, id); err != nil {
			respondInternalError(w, r, err)
			return
		}
	} else {
		res, err := h.db.ExecContext(r.Context(), `
			INSERT INTO p3m_raid_items (entity_type, entity_id, item_type, title, description, impact_score, probability_score, owner_id, status)
			VALUES (?, ?, ?, ?, ?, 1, 1, ?, 'open')`,
			models.P3MEntityProject, rv.ProjectID, models.P3MRAIDItemTypeDecision, "ARB rejected: "+rv.Title,
			rv.Description+"\n\nReviewer comments: "+in.Comments, user.ID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		decisionItemID64, _ := res.LastInsertId()
		if _, err := h.db.ExecContext(r.Context(), `
			UPDATE p3m_architecture_reviews SET status = ?, reviewer_id = ?, decided_at = ?, comments = ?, decision_item_id = ?, updated_at = ? WHERE id = ?`,
			models.P3MARBStatusNeedsWork, user.ID, now, in.Comments, int(decisionItemID64), now, id); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	rv, err = h.getArchitectureReviewRow(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, rv)
}
