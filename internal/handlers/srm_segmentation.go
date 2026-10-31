// SRM segmentation engine (Phase 1): supplier categories, segmentation runs
// with official question scoring, nine-box results and the validation
// sign-off cycle that applies mapped tiers to live suppliers (FR-4.2.x).
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"windshift/internal/models"
)

// Question codes follow "<group>_<key>" where group is "criticality" or
// "value": the completion engine sums weighted scores per group to produce
// the two axes of the nine-box (FR-4.2.3a). Weights are supplied per answer
// (admin-editable question sets, §8.3) and weighted_score = score*weight is
// stored at write time.

// FR-4.2.3a ranking boundaries (defaults from the official Limits sheet): a
// weighted total of at least srmBandMedEdge ranks medium; high requires a
// full-mark total for the question group ("High above" the medium band);
// everything else is low, with srmBandLowEdge marking the low band's lower
// edge (scores below it still floor to low). Boundaries are admin-configurable
// per §8.3; these constants are the shipped defaults.
const (
	srmBandLowEdge  = 1.0
	srmBandMedEdge  = 2.0
	srmBandHighEdge = 3.0
)

func srmRankBand(score float64) string {
	switch {
	case score >= srmBandHighEdge:
		return "high"
	case score >= srmBandMedEdge:
		return "medium"
	default:
		return "low"
	}
}

// srmBoxNumber maps criticality × value ranks to the official nine-box
// numbering (FR-4.2.3a): LowLow=1, LowMedium=2, LowHigh=4, MediumLow=3,
// MediumMedium=5, MediumHigh=7, HighLow=6, HighMedium=8, HighHigh=9.
func srmBoxNumber(rankCriticality, rankValue string) int {
	c := map[string]int{"low": 0, "medium": 1, "high": 2}[rankCriticality]
	v := map[string]int{"low": 0, "medium": 1, "high": 2}[rankValue]
	return [3][3]int{{1, 2, 4}, {3, 5, 7}, {6, 8, 9}}[c][v]
}

// srmBoxLabel is the official box name for a nine-box number.
func srmBoxLabel(rankCriticality, rankValue string) string {
	cap := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	return cap(rankCriticality) + cap(rankValue)
}

// srmTierForBox maps a nine-box number to the system treatment tier (§1.5).
func srmTierForBox(box int) string {
	switch box {
	case 7, 8, 9: // MediumHigh / HighMedium / HighHigh
		return models.SRMTier1
	case 4, 5, 6: // LowHigh / MediumMedium / HighLow
		return models.SRMTier2
	case 2, 3: // LowMedium / MediumLow
		return models.SRMTier3
	default: // 1 = LowLow
		return models.SRMTier4
	}
}

// FR-4.2.2c category box thresholds (defaults from the official Limits sheet):
// spend High ≥ $15m, Low ≥ $8m; criticality High ≥ 150, Low ≥ 130.
const (
	srmCatSpendHighEdge = 15_000_000.0
	srmCatSpendLowEdge  = 8_000_000.0
	srmCatCritHighEdge  = 150.0
	srmCatCritLowEdge   = 130.0
)

// srmCategoryBox maps a category's aggregate criticality score and total spend
// to the official four-box classification (FR-4.2.2c). A Tactical box with
// material spend (≥ $8m) stays operational tier_3; plain Tactical is tier_4
// (§1.5).
func srmCategoryBox(critScore, spend float64) (box string, rankCrit, rankSpend string, tier string) {
	critHigh := critScore >= srmCatCritHighEdge
	spendHigh := spend >= srmCatSpendHighEdge
	switch {
	case critHigh && spendHigh:
		return "partner", "high", "high", models.SRMTier1
	case critHigh:
		return "collaborative", "high", "low", models.SRMTier2
	case spendHigh:
		return "managed", "low", "high", models.SRMTier2
	default:
		if spend >= srmCatSpendLowEdge {
			return "tactical", "low", "low", models.SRMTier3
		}
		return "tactical", "low", "low", models.SRMTier4
	}
}

// srmLiteEntityCap is the official cap on suppliers per lite run (FR-4.2.4a).
const srmLiteEntityCap = 50

func scanSRMSegmentationResult(row interface{ Scan(...any) error }) (*models.SRMSegmentationResult, error) {
	res := &models.SRMSegmentationResult{}
	err := row.Scan(&res.ID, &res.RunID, &res.EntityType, &res.EntityRef,
		&res.CriticalityScore, &res.ValueScore, &res.SpendAmount,
		&res.RankCriticality, &res.RankValue, &res.SegmentationBox,
		&res.BoxNumber, &res.MappedTier)
	return res, err
}

const srmSegmentationResultCols = `id, run_id, entity_type, entity_ref, criticality_score, value_score, spend_amount, rank_criticality, rank_value, segmentation_box, box_number, mapped_tier`

func (h *SRMHandler) loadSegmentationResults(ctx context.Context, runID int) ([]models.SRMSegmentationResult, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT `+srmSegmentationResultCols+` FROM srm_segmentation_results WHERE run_id = ? ORDER BY entity_type DESC, entity_ref`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []models.SRMSegmentationResult
	for rows.Next() {
		res, err := scanSRMSegmentationResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *res)
	}
	return results, rows.Err()
}

func (h *SRMHandler) srmEntityName(ctx context.Context, entityType string, ref int) string {
	var q string
	switch entityType {
	case "supplier":
		q = `SELECT company_name FROM srm_suppliers WHERE id = ?`
	case "category":
		q = `SELECT name FROM srm_categories WHERE id = ?`
	default:
		return ""
	}
	var name string
	if err := h.db.QueryRowContext(ctx, q, ref).Scan(&name); err != nil {
		return ""
	}
	return name
}

// ListCategories returns all categories with the count of distinct suppliers
// scored in their completed or validated runs.
func (h *SRMHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT c.id, c.name, c.created_at,
		       (SELECT COUNT(DISTINCT sr.entity_ref)
		        FROM srm_segmentation_runs rn
		        JOIN srm_segmentation_results sr ON sr.run_id = rn.id AND sr.entity_type = 'supplier'
		        WHERE rn.category_id = c.id AND rn.status IN ('complete', 'validated')) AS supplier_count
		FROM srm_categories c ORDER BY c.name`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	cats := []models.SRMCategory{}
	for rows.Next() {
		var c models.SRMCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.SupplierCount); err != nil {
			respondInternalError(w, r, err)
			return
		}
		cats = append(cats, c)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, cats)
}

// CreateCategory adds a supplier category (FR-4.2.1).
func (h *SRMHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	var exists int
	err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_categories WHERE name = ?`, in.Name).Scan(&exists)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists > 0 {
		respondBadRequest(w, r, "a category with this name already exists")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO srm_categories (name) VALUES (?)`, in.Name)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	id := int(id64)
	var c models.SRMCategory
	if err := h.db.QueryRowContext(r.Context(), `SELECT id, name, created_at FROM srm_categories WHERE id = ?`, id).Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAuditWithDetails(h.db, r, user, "srm_category_created", "srm_category", &id, c.Name, map[string]any{"name": c.Name})
	respondJSONCreated(w, c)
}

// UpdateCategory renames a category.
func (h *SRMHandler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respondBadRequest(w, r, "name is required")
		return
	}
	var exists int
	err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_categories WHERE name = ? AND id != ?`, in.Name, id).Scan(&exists)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists > 0 {
		respondBadRequest(w, r, "a category with this name already exists")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `UPDATE srm_categories SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, in.Name, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "category not found")
		return
	}
	var c models.SRMCategory
	if err := h.db.QueryRowContext(r.Context(), `SELECT id, name, created_at FROM srm_categories WHERE id = ?`, id).Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
		respondInternalError(w, r, err)
		return
	}
	logAuditWithDetails(h.db, r, user, "srm_category_updated", "srm_category", &id, c.Name, map[string]any{"name": c.Name})
	respondJSONOK(w, c)
}

// DeleteCategory removes a category; its runs keep their results with the
// category reference cleared (FR-4.2.1).
func (h *SRMHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_categories WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "category not found")
		return
	}
	logAuditWithDetails(h.db, r, user, "srm_category_deleted", "srm_category", &id, fmt.Sprintf("%d", id), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ListSegmentationRuns returns runs (newest first). Query params: status,
// category_id. Results are included per run.
func (h *SRMHandler) ListSegmentationRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := `SELECT r.id, r.scope, r.category_id, r.created_by, r.status, r.notes, r.created_at, r.validated_at, COALESCE(c.name, '') AS category_name
	      FROM srm_segmentation_runs r LEFT JOIN srm_categories c ON c.id = r.category_id WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("status"); v != "" {
		q += ` AND r.status = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("category_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			respondBadRequest(w, r, "invalid category_id")
			return
		}
		q += ` AND r.category_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY r.created_at DESC`
	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	runs := []models.SRMSegmentationRun{}
	for rows.Next() {
		var run models.SRMSegmentationRun
		if err := rows.Scan(&run.ID, &run.Scope, &run.CategoryID, &run.CreatedBy, &run.Status, &run.Notes, &run.CreatedAt, &run.ValidatedAt, &run.CategoryName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range runs {
		results, err := h.loadSegmentationResults(ctx, runs[i].ID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for j := range results {
			results[j].EntityName = h.srmEntityName(ctx, results[j].EntityType, results[j].EntityRef)
		}
		runs[i].Results = results
	}
	respondJSONOK(w, runs)
}

// CreateSegmentationRun starts a new segmentation run (FR-4.2.3). Scope is
// "category" (requires an existing category_id), "supplier" or "lite".
func (h *SRMHandler) CreateSegmentationRun(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		Scope      string `json:"scope"`
		CategoryID *int   `json:"category_id"`
		Notes      string `json:"notes"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	switch in.Scope {
	case models.SRMSegScopeCategory:
		if in.CategoryID == nil {
			respondBadRequest(w, r, "category_id is required for category scope")
			return
		}
		var exists int
		err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_categories WHERE id = ?`, *in.CategoryID).Scan(&exists)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if exists == 0 {
			respondBadRequest(w, r, "category not found")
			return
		}
	case models.SRMSegScopeSupplier, models.SRMSegScopeLite:
	default:
		respondBadRequest(w, r, "scope must be category, supplier or lite")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `INSERT INTO srm_segmentation_runs (scope, category_id, created_by, status, notes) VALUES (?, ?, ?, 'in_progress', ?)`,
		in.Scope, in.CategoryID, user.ID, in.Notes)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	id := int(id64)
	logAuditWithDetails(h.db, r, user, "srm_segmentation_run_created", "srm_segmentation_run", &id, fmt.Sprintf("%d", id), map[string]any{"scope": in.Scope})
	var run models.SRMSegmentationRun
	if err := h.db.QueryRowContext(r.Context(), `SELECT id, scope, category_id, created_by, status, notes, created_at FROM srm_segmentation_runs WHERE id = ?`, id).Scan(&run.ID, &run.Scope, &run.CategoryID, &run.CreatedBy, &run.Status, &run.Notes, &run.CreatedAt); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if run.CategoryID != nil {
		run.CategoryName = h.srmEntityName(r.Context(), "category", *run.CategoryID)
	}
	respondJSONCreated(w, run)
}

// GetSegmentationRun returns a run with its results and answers.
func (h *SRMHandler) GetSegmentationRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var run models.SRMSegmentationRun
	err := h.db.QueryRowContext(ctx, `SELECT r.id, r.scope, r.category_id, r.created_by, r.status, r.notes, r.created_at, r.validated_at, COALESCE(c.name, '') AS category_name
		FROM srm_segmentation_runs r LEFT JOIN srm_categories c ON c.id = r.category_id WHERE r.id = ?`, id).
		Scan(&run.ID, &run.Scope, &run.CategoryID, &run.CreatedBy, &run.Status, &run.Notes, &run.CreatedAt, &run.ValidatedAt, &run.CategoryName)
	if err != nil {
		respondNotFound(w, r, "segmentation run not found")
		return
	}
	results, err := h.loadSegmentationResults(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range results {
		results[i].EntityName = h.srmEntityName(ctx, results[i].EntityType, results[i].EntityRef)
	}
	run.Results = results

	rows, err := h.db.QueryContext(ctx, `SELECT id, run_id, entity_ref, question_code, score, anchor_label, weight, weighted_score, justification, scored_by, scored_at FROM srm_segmentation_answers WHERE run_id = ? ORDER BY entity_ref, question_code`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a models.SRMSegmentationAnswer
		if err := rows.Scan(&a.ID, &a.RunID, &a.EntityRef, &a.QuestionCode, &a.Score, &a.AnchorLabel, &a.Weight, &a.WeightedScore, &a.Justification, &a.ScoredBy, &a.ScoredAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		a.EntityName = h.srmEntityName(ctx, "supplier", a.EntityRef)
		run.Answers = append(run.Answers, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, run)
}

// UpsertSegmentationAnswer records or revises one scored question for an
// entity within an in-progress run (FR-4.2.3). Score is 0-3; weight defaults
// to 1 and weighted_score = score*weight is computed server-side. When the
// run has a changes_requested validation cycle, score revisions are recorded
// as scoring adjustments for audit (FR-4.3).
func (h *SRMHandler) UpsertSegmentationAnswer(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		EntityRef    int     `json:"entity_ref"`
		QuestionCode string  `json:"question_code"`
		Score        int     `json:"score"`
		AnchorLabel  *string `json:"anchor_label"`
		Weight       float64 `json:"weight"`
		Justification *string `json:"justification"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	in.QuestionCode = strings.TrimSpace(in.QuestionCode)
	if in.EntityRef <= 0 || in.QuestionCode == "" {
		respondBadRequest(w, r, "entity_ref and question_code are required")
		return
	}
	if in.Score < 0 || in.Score > 3 {
		respondBadRequest(w, r, "score must be between 0 and 3")
		return
	}
	if in.Weight <= 0 {
		in.Weight = 1
	}
	var status string
	err := h.db.QueryRowContext(ctx, `SELECT status FROM srm_segmentation_runs WHERE id = ?`, id).Scan(&status)
	if err != nil {
		respondNotFound(w, r, "segmentation run not found")
		return
	}
	if status != models.SRMRunStatusInProgress {
		respondConflict(w, r, "answers can only be recorded while the run is in progress")
		return
	}
	var supplierName string
	err = h.db.QueryRowContext(ctx, `SELECT company_name FROM srm_suppliers WHERE id = ?`, in.EntityRef).Scan(&supplierName)
	if err != nil {
		respondBadRequest(w, r, "entity_ref does not match a known supplier")
		return
	}

	var existingID int
	var oldScore *int
	err = h.db.QueryRowContext(ctx, `SELECT id, score FROM srm_segmentation_answers WHERE run_id = ? AND entity_ref = ? AND question_code = ?`, id, in.EntityRef, in.QuestionCode).Scan(&existingID, &oldScore)
	if err != nil && !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	weighted := float64(in.Score) * in.Weight
	now := time.Now().UTC()
	var anchorLabel string
	if in.AnchorLabel != nil {
		anchorLabel = *in.AnchorLabel
	}
	var justification string
	if in.Justification != nil {
		justification = *in.Justification
	}

	var answerID int
	if existingID > 0 && oldScore != nil {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_segmentation_answers SET score = ?, anchor_label = ?, weight = ?, weighted_score = ?, justification = ?, scored_by = ?, scored_at = ? WHERE id = ?`,
			in.Score, anchorLabel, in.Weight, weighted, justification, user.ID, now, existingID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		answerID = existingID
		if *oldScore != in.Score && justification == "" {
			justification = "validation cycle adjustment"
		}
		// Record the revision when a validation cycle requested changes.
		var reqID int
		err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_validation_requests WHERE run_id = ? AND status = 'changes_requested' ORDER BY created_at DESC LIMIT 1`, id).Scan(&reqID)
		if err == nil {
			old := *oldScore
			newScore := in.Score
			h.db.ExecContext(ctx, `INSERT INTO srm_scoring_adjustments (request_id, run_id, entity_ref, question_code, old_score, new_score, changed_by, reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				reqID, id, in.EntityRef, in.QuestionCode, &old, &newScore, user.ID, justification)
		}
	} else {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_segmentation_answers (run_id, entity_ref, question_code, score, anchor_label, weight, weighted_score, justification, scored_by, scored_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.EntityRef, in.QuestionCode, in.Score, anchorLabel, in.Weight, weighted, justification, user.ID, now)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		n, _ := res.LastInsertId()
		answerID = int(n)
	}

	var a models.SRMSegmentationAnswer
	err = h.db.QueryRowContext(ctx, `SELECT id, run_id, entity_ref, question_code, score, anchor_label, weight, weighted_score, justification, scored_by, scored_at FROM srm_segmentation_answers WHERE id = ?`, answerID).
		Scan(&a.ID, &a.RunID, &a.EntityRef, &a.QuestionCode, &a.Score, &a.AnchorLabel, &a.Weight, &a.WeightedScore, &a.Justification, &a.ScoredBy, &a.ScoredAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	a.EntityName = supplierName
	logAuditWithDetails(h.db, r, user, "srm_segmentation_answer_saved", "srm_segmentation_run", &id, fmt.Sprintf("%d", id), map[string]any{"entity_ref": in.EntityRef, "question_code": in.QuestionCode, "score": in.Score})
	respondJSONOK(w, a)
}

// srmAnswerGroups sums weighted scores per entity and per question group.
type srmEntityScores struct {
	criticality float64
	value       float64
}

func (h *SRMHandler) loadRunScoreGroups(ctx context.Context, runID int) (map[int]*srmEntityScores, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT entity_ref, question_code, weighted_score FROM srm_segmentation_answers WHERE run_id = ?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[int]*srmEntityScores{}
	for rows.Next() {
		var ref int
		var code string
		var weighted float64
		if err := rows.Scan(&ref, &code, &weighted); err != nil {
			return nil, err
		}
		g := groups[ref]
		if g == nil {
			g = &srmEntityScores{}
			groups[ref] = g
		}
		switch {
		case strings.HasPrefix(code, "criticality_"):
			g.criticality += weighted
		case strings.HasPrefix(code, "value_"):
			g.value += weighted
		}
	}
	return groups, rows.Err()
}

// srmSupplierSpend sums total_value across the supplier's non-terminated
// contracts (the 12-month rolling spend proxy from the register).
func (h *SRMHandler) srmSupplierSpend(ctx context.Context, supplierID int) float64 {
	var spend float64
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_value), 0) FROM srm_contracts WHERE supplier_id = ? AND status != 'terminated'`, supplierID).Scan(&spend)
	if err != nil {
		return 0
	}
	return spend
}

// CompleteSegmentationRun scores every answered entity, writes the nine-box
// results (plus a category aggregate row for category scope), marks the run
// complete and supersedes prior runs of the same scope/category (FR-4.2.3).
func (h *SRMHandler) CompleteSegmentationRun(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var run models.SRMSegmentationRun
	err := h.db.QueryRowContext(ctx, `SELECT id, scope, category_id, status FROM srm_segmentation_runs WHERE id = ?`, id).Scan(&run.ID, &run.Scope, &run.CategoryID, &run.Status)
	if err != nil {
		respondNotFound(w, r, "segmentation run not found")
		return
	}
	if run.Status != models.SRMRunStatusInProgress {
		respondConflict(w, r, "only in-progress runs can be completed")
		return
	}

	groups, err := h.loadRunScoreGroups(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if run.Scope == models.SRMSegScopeLite && len(groups) > srmLiteEntityCap {
		respondBadRequest(w, r, fmt.Sprintf("lite runs are capped at %d suppliers", srmLiteEntityCap))
		return
	}

	var catCritTotal, catSpendTotal float64
	for ref, g := range groups {
		rankCrit := srmRankBand(g.criticality)
		rankValue := srmRankBand(g.value)
		box := srmBoxNumber(rankCrit, rankValue)
		tier := srmTierForBox(box)
		spend := h.srmSupplierSpend(ctx, ref)

		crit, val, boxNum, tierPtr, spendPtr := g.criticality, g.value, box, tier, spend
		if _, err := h.db.ExecContext(ctx, `INSERT INTO srm_segmentation_results (run_id, entity_type, entity_ref, criticality_score, value_score, spend_amount, rank_criticality, rank_value, segmentation_box, box_number, mapped_tier) VALUES (?, 'supplier', ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id, entity_type, entity_ref) DO UPDATE SET criticality_score = excluded.criticality_score, value_score = excluded.value_score, spend_amount = excluded.spend_amount, rank_criticality = excluded.rank_criticality, rank_value = excluded.rank_value, segmentation_box = excluded.segmentation_box, box_number = excluded.box_number, mapped_tier = excluded.mapped_tier`,
			id, ref, crit, val, spendPtr, rankCrit, rankValue, srmBoxLabel(rankCrit, rankValue), &boxNum, &tierPtr); err != nil {
			respondInternalError(w, r, err)
			return
		}
		catCritTotal += g.criticality
		catSpendTotal += spend
	}

	if run.Scope == models.SRMSegScopeCategory && run.CategoryID != nil {
		box, rankCrit, rankSpend, tier := srmCategoryBox(catCritTotal, catSpendTotal)
		tierPtr := tier
		crit, val, spend := catCritTotal, catSpendTotal, catSpendTotal
		if _, err := h.db.ExecContext(ctx, `INSERT INTO srm_segmentation_results (run_id, entity_type, entity_ref, criticality_score, value_score, spend_amount, rank_criticality, rank_value, segmentation_box, mapped_tier) VALUES (?, 'category', ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id, entity_type, entity_ref) DO UPDATE SET criticality_score = excluded.criticality_score, value_score = excluded.value_score, spend_amount = excluded.spend_amount, rank_criticality = excluded.rank_criticality, rank_value = excluded.rank_value, segmentation_box = excluded.segmentation_box, mapped_tier = excluded.mapped_tier`,
			id, *run.CategoryID, crit, val, spend, rankCrit, rankSpend, box, &tierPtr); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'complete' WHERE id = ?`, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	// Supersede prior completed/validated runs of the same scope and category.
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'superseded' WHERE id != ? AND scope = ? AND status IN ('complete', 'validated') AND COALESCE(category_id, -1) = COALESCE(?, -1)`,
		id, run.Scope, run.CategoryID); err != nil {
		respondInternalError(w, r, err)
		return
	}

	logAuditWithDetails(h.db, r, user, "srm_segmentation_run_completed", "srm_segmentation_run", &id, fmt.Sprintf("%d", id), map[string]any{"entities": len(groups)})
	var out models.SRMSegmentationRun
	if err := h.db.QueryRowContext(ctx, `SELECT r.id, r.scope, r.category_id, r.created_by, r.status, r.notes, r.created_at, COALESCE(c.name, '') AS category_name FROM srm_segmentation_runs r LEFT JOIN srm_categories c ON c.id = r.category_id WHERE r.id = ?`, id).
		Scan(&out.ID, &out.Scope, &out.CategoryID, &out.CreatedBy, &out.Status, &out.Notes, &out.CreatedAt, &out.CategoryName); err != nil {
		respondInternalError(w, r, err)
		return
	}
	results, err := h.loadSegmentationResults(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range results {
		results[i].EntityName = h.srmEntityName(ctx, results[i].EntityType, results[i].EntityRef)
	}
	out.Results = results
	respondJSONOK(w, out)
}

// ListValidationRequests returns validation sign-off requests (newest first).
// Query params: status, supplier_id, run_id. Adjustments are included per
// request (FR-4.3).
func (h *SRMHandler) ListValidationRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := `SELECT v.id, v.run_id, v.supplier_id, v.requested_by, v.status, v.cycle_count, v.signoff_method, v.signed_off_by, v.signed_off_at, v.comment, v.created_at, v.updated_at, COALESCE(s.company_name, '') AS supplier_name
	      FROM srm_validation_requests v LEFT JOIN srm_suppliers s ON s.id = v.supplier_id WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("status"); v != "" {
		q += ` AND v.status = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("supplier_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			respondBadRequest(w, r, "invalid supplier_id")
			return
		}
		q += ` AND v.supplier_id = ?`
		args = append(args, id)
	}
	if v := r.URL.Query().Get("run_id"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			respondBadRequest(w, r, "invalid run_id")
			return
		}
		q += ` AND v.run_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY v.created_at DESC`
	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	reqs := []models.SRMValidationRequest{}
	for rows.Next() {
		var v models.SRMValidationRequest
		if err := rows.Scan(&v.ID, &v.RunID, &v.SupplierID, &v.RequestedBy, &v.Status, &v.CycleCount, &v.SignoffMethod, &v.SignedOffBy, &v.SignedOffAt, &v.Comment, &v.CreatedAt, &v.UpdatedAt, &v.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		reqs = append(reqs, v)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range reqs {
		rows2, err := h.db.QueryContext(ctx, `SELECT id, request_id, run_id, entity_ref, question_code, old_score, new_score, changed_by, reason, created_at FROM srm_scoring_adjustments WHERE request_id = ? ORDER BY created_at`, reqs[i].ID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for rows2.Next() {
			var a models.SRMScoringAdjustment
			if err := rows2.Scan(&a.ID, &a.RequestID, &a.RunID, &a.EntityRef, &a.QuestionCode, &a.OldScore, &a.NewScore, &a.ChangedBy, &a.Reason, &a.CreatedAt); err != nil {
				rows2.Close()
				respondInternalError(w, r, err)
				return
			}
			a.EntityName = h.srmEntityName(ctx, "supplier", a.EntityRef)
			reqs[i].Adjustments = append(reqs[i].Adjustments, a)
		}
		rows2.Close()
		if err := rows2.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	respondJSONOK(w, reqs)
}

// CreateValidationRequest opens the sign-off cycle for a completed run
// (FR-4.3). supplier_id is optional: category-scope runs validate per entity
// result; single-supplier focus sets it explicitly.
func (h *SRMHandler) CreateValidationRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		RunID      int     `json:"run_id"`
		SupplierID *int    `json:"supplier_id"`
		Comment    string  `json:"comment"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	ctx := r.Context()
	var status string
	err := h.db.QueryRowContext(ctx, `SELECT status FROM srm_segmentation_runs WHERE id = ?`, in.RunID).Scan(&status)
	if err != nil {
		respondBadRequest(w, r, "run_id does not match a known segmentation run")
		return
	}
	if status != models.SRMRunStatusComplete {
		respondConflict(w, r, "validation requires a completed run")
		return
	}
	var supplierName string
	if in.SupplierID != nil {
		err = h.db.QueryRowContext(ctx, `SELECT company_name FROM srm_suppliers WHERE id = ?`, *in.SupplierID).Scan(&supplierName)
		if err != nil {
			respondBadRequest(w, r, "supplier_id does not match a known supplier")
			return
		}
	}
	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_validation_requests (run_id, supplier_id, requested_by, status, cycle_count, comment) VALUES (?, ?, ?, 'pending', 1, ?)`,
		in.RunID, in.SupplierID, user.ID, in.Comment)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()
	id := int(id64)
	logAuditWithDetails(h.db, r, user, "srm_validation_requested", "srm_validation_request", &id, fmt.Sprintf("%d", id), map[string]any{"run_id": in.RunID})
	var v models.SRMValidationRequest
	if err := h.db.QueryRowContext(ctx, `SELECT id, run_id, supplier_id, requested_by, status, cycle_count, signoff_method, signed_off_by, signed_off_at, comment, created_at, updated_at FROM srm_validation_requests WHERE id = ?`, id).
		Scan(&v.ID, &v.RunID, &v.SupplierID, &v.RequestedBy, &v.Status, &v.CycleCount, &v.SignoffMethod, &v.SignedOffBy, &v.SignedOffAt, &v.Comment, &v.CreatedAt, &v.UpdatedAt); err != nil {
		respondInternalError(w, r, err)
		return
	}
	v.SupplierName = supplierName
	respondJSONCreated(w, v)
}

// GetValidationRequest returns a request with its scoring adjustments.
func (h *SRMHandler) GetValidationRequest(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var v models.SRMValidationRequest
	err := h.db.QueryRowContext(ctx, `SELECT v.id, v.run_id, v.supplier_id, v.requested_by, v.status, v.cycle_count, v.signoff_method, v.signed_off_by, v.signed_off_at, v.comment, v.created_at, v.updated_at, COALESCE(s.company_name, '') AS supplier_name
		FROM srm_validation_requests v LEFT JOIN srm_suppliers s ON s.id = v.supplier_id WHERE v.id = ?`, id).
		Scan(&v.ID, &v.RunID, &v.SupplierID, &v.RequestedBy, &v.Status, &v.CycleCount, &v.SignoffMethod, &v.SignedOffBy, &v.SignedOffAt, &v.Comment, &v.CreatedAt, &v.UpdatedAt, &v.SupplierName)
	if err != nil {
		respondNotFound(w, r, "validation request not found")
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, request_id, run_id, entity_ref, question_code, old_score, new_score, changed_by, reason, created_at FROM srm_scoring_adjustments WHERE request_id = ? ORDER BY created_at`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a models.SRMScoringAdjustment
		if err := rows.Scan(&a.ID, &a.RequestID, &a.RunID, &a.EntityRef, &a.QuestionCode, &a.OldScore, &a.NewScore, &a.ChangedBy, &a.Reason, &a.CreatedAt); err != nil {
			respondInternalError(w, r, err)
			return
		}
		a.EntityName = h.srmEntityName(ctx, "supplier", a.EntityRef)
		v.Adjustments = append(v.Adjustments, a)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, v)
}

// DecideValidationRequest resolves a pending validation request (FR-4.3):
// approved signs off and applies the mapped tiers to live suppliers;
// changes_requested reopens the run for rescoring (max 3 cycles); rejected
// closes the cycle without applying tiers.
func (h *SRMHandler) DecideValidationRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Status        string  `json:"status"`
		Comment       *string `json:"comment"`
		SignoffMethod *string `json:"signoff_method"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "invalid JSON body")
		return
	}
	var v models.SRMValidationRequest
	err := h.db.QueryRowContext(ctx, `SELECT id, run_id, supplier_id, status, cycle_count FROM srm_validation_requests WHERE id = ?`, id).Scan(&v.ID, &v.RunID, &v.SupplierID, &v.Status, &v.CycleCount)
	if err != nil {
		respondNotFound(w, r, "validation request not found")
		return
	}
	if v.Status != models.SRMValidationPending {
		respondConflict(w, r, "this validation request has already been decided")
		return
	}

	now := time.Now().UTC()
	switch in.Status {
	case models.SRMValidationApproved:
		res, err := h.db.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'approved', signed_off_by = ?, signed_off_at = ?, comment = COALESCE(?, comment), signoff_method = COALESCE(?, signoff_method) WHERE id = ?`,
			user.ID, now, in.Comment, in.SignoffMethod, id)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondNotFound(w, r, "validation request not found")
			return
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'validated', validated_at = ? WHERE id = ?`, now, v.RunID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		// Apply mapped tiers to live suppliers (FR-4.3: never direct from the
		// scoring engine — only after sign-off).
		rows, err := h.db.QueryContext(ctx, `SELECT entity_ref, mapped_tier FROM srm_segmentation_results WHERE run_id = ? AND entity_type = 'supplier' AND mapped_tier IS NOT NULL`, v.RunID)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for rows.Next() {
			var ref int
			var tier string
			if err := rows.Scan(&ref, &tier); err != nil {
				rows.Close()
				respondInternalError(w, r, err)
				return
			}
			h.db.ExecContext(ctx, `UPDATE srm_suppliers SET tier = ?, updated_at = ? WHERE id = ?`, tier, now, ref)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			respondInternalError(w, r, err)
			return
		}
	case models.SRMValidationChangesRequested:
		if v.CycleCount >= 3 {
			respondConflict(w, r, "maximum of 3 validation cycles reached; approve or reject")
			return
		}
		res, err := h.db.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'changes_requested', cycle_count = cycle_count + 1, comment = COALESCE(?, comment) WHERE id = ?`, in.Comment, id)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondNotFound(w, r, "validation request not found")
			return
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'in_progress' WHERE id = ?`, v.RunID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	case models.SRMValidationRejected:
		res, err := h.db.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'rejected', comment = COALESCE(?, comment), signoff_method = COALESCE(?, signoff_method) WHERE id = ?`, in.Comment, in.SignoffMethod, id)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondNotFound(w, r, "validation request not found")
			return
		}
	default:
		respondBadRequest(w, r, "status must be approved, changes_requested or rejected")
		return
	}

	logAuditWithDetails(h.db, r, user, "srm_validation_decided", "srm_validation_request", &id, fmt.Sprintf("%d", id), map[string]any{"decision": in.Status})
	var out models.SRMValidationRequest
	if err := h.db.QueryRowContext(ctx, `SELECT v.id, v.run_id, v.supplier_id, v.requested_by, v.status, v.cycle_count, v.signoff_method, v.signed_off_by, v.signed_off_at, v.comment, v.created_at, v.updated_at, COALESCE(s.company_name, '') AS supplier_name
		FROM srm_validation_requests v LEFT JOIN srm_suppliers s ON s.id = v.supplier_id WHERE v.id = ?`, id).
		Scan(&out.ID, &out.RunID, &out.SupplierID, &out.RequestedBy, &out.Status, &out.CycleCount, &out.SignoffMethod, &out.SignedOffBy, &out.SignedOffAt, &out.Comment, &out.CreatedAt, &out.UpdatedAt, &out.SupplierName); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}
