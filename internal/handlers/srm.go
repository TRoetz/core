// Package handlers: SRM (Supplier Relationship Management) module.
//
// The module is gated by the global permission models.PermissionSRMView at the
// route level; every handler here additionally requires an authenticated user
// for attribution fields. It implements the NZ Government Procurement SRM
// Strategy Wheel across four phases (preparation, planning, implementation,
// monitoring) with a phase-gate state machine on each supplier relationship:
// advancement is blocked until the gate's artefacts exist in their required
// status (FR-GATE-1), every transition is audit-logged (FR-GATE-2), and a
// one-gate downgrade always requires a reason note (FR-GATE-3).
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/models"
)

// SRMHandler serves the /api/srm/* endpoints for the supplier relationship
// management module. Domain-specific methods live in srm_segmentation.go,
// srm_planning.go and srm_monitoring.go (same package).
type SRMHandler struct {
	db database.Database
}

func NewSRMHandler(db database.Database) *SRMHandler {
	return &SRMHandler{db: db}
}

// orKeep merges a partial-update string pointer with the stored value.
func orKeep(v *string, fallback string) any {
	if v == nil {
		return fallback
	}
	return *v
}

// boolOr merges a partial-update bool pointer with the stored value.
func boolOr(v *bool, fallback bool) any {
	if v == nil {
		return fallback
	}
	return *v
}

// floatOr merges a partial-update float64 pointer with the stored value.
func floatOr(v *float64, fallback float64) any {
	if v == nil {
		return fallback
	}
	return *v
}

// intOr merges a partial-update int pointer with the stored value.
func intOr(v *int, fallback int) any {
	if v == nil {
		return fallback
	}
	return *v
}

// ---------------------------------------------------------------------------
// Phase-gate state machine (§3.1, FR-GATE-1/2/3).
// ---------------------------------------------------------------------------

// srmStateOrder is the forward path of a supplier relationship. "closed" is
// reachable from any state (contract retired/terminated) and is terminal.
var srmStateOrder = []string{
	models.SRMStateRegistered,
	models.SRMStateSegmented,
	models.SRMStatePlanningReady,
	models.SRMStateImplementationReady,
	models.SRMStateKickedOff,
	models.SRMStateJBPFormalised,
	models.SRMStateMonitoringActive,
}

// srmGateArtifacts maps each non-initial state to the artefacts that must be
// true before a supplier may enter it. The human-readable names are surfaced
// verbatim in gate responses so the UI can render FR-GATE-1 tooltips.
var srmGateArtifacts = map[string][]string{
	models.SRMStateSegmented:            {"segmentation run complete"},
	models.SRMStatePlanningReady:        {"validation sign-off approved"},
	models.SRMStateImplementationReady:  {"signed value proposition", "complete communications plan", "preliminary RACI matrix"},
	models.SRMStateKickedOff:            {"kick-off meeting held with outcomes recorded"},
	models.SRMStateJBPFormalised:        {"joint business plan signed by both parties"},
	models.SRMStateMonitoringActive:     {"completed collaborative scorecard"},
}

// srmArtifactChecks evaluates one named artefact for a supplier. The map key
// must match the names in srmGateArtifacts exactly.
var srmArtifactChecks = map[string]func(ctx context.Context, db database.Database, supplierID int) bool{
	"segmentation run complete": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_segmentation_runs r
			JOIN srm_segmentation_results sr ON sr.run_id = r.id
			WHERE r.status IN (?, ?) AND sr.entity_type = 'supplier' AND sr.entity_ref = ?`,
			models.SRMRunStatusComplete, models.SRMRunStatusValidated, id).Scan(&n)
		return err == nil && n > 0
	},
	"validation sign-off approved": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_validation_requests WHERE supplier_id = ? AND status = ?`,
			id, models.SRMValidationApproved).Scan(&n)
		return err == nil && n > 0
	},
	"signed value proposition": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_value_propositions WHERE supplier_id = ? AND signed_off_at IS NOT NULL`, id).Scan(&n)
		return err == nil && n > 0
	},
	"complete communications plan": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_communications_plans WHERE supplier_id = ? AND is_complete = true`, id).Scan(&n)
		return err == nil && n > 0
	},
	"preliminary RACI matrix": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_racis WHERE supplier_id = ? AND status IN (?, ?, ?)`,
			id, models.SRMRaciStatusPreliminary, models.SRMRaciStatusUnderDiscussion, models.SRMRaciStatusAgreed).Scan(&n)
		return err == nil && n > 0
	},
	"kick-off meeting held with outcomes recorded": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_meetings
			WHERE supplier_id = ? AND status = 'held'
			  AND meeting_type IN (?, ?)
			  AND outcomes_json <> '[]'`,
			id, models.SRMMeetingSupplierKickoff, models.SRMMeetingInternalKickoff).Scan(&n)
		return err == nil && n > 0
	},
	"joint business plan signed by both parties": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_jbps WHERE supplier_id = ? AND status = ?`,
			id, models.SRMJBPStatusSigned).Scan(&n)
		return err == nil && n > 0
	},
	"completed collaborative scorecard": func(ctx context.Context, db database.Database, id int) bool {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM srm_scorecards
			WHERE supplier_id = ? AND status = 'closed' AND completed_collaboratively = true`, id).Scan(&n)
		return err == nil && n > 0
	},
}

// srmStateIndex returns the position of state in the forward path, or -1 for
// unknown/terminal states.
func srmStateIndex(state string) int {
	for i, s := range srmStateOrder {
		if s == state {
			return i
		}
	}
	return -1
}

// srmGateStatus evaluates every artefact required to enter the next state and
// reports which are missing. For a supplier in "closed" or an unknown state it
// returns no next state and all checks empty.
func (h *SRMHandler) srmGateStatus(ctx context.Context, supplierID int, current string) (nextState string, artifacts map[string]bool, missing []string, err error) {
	artifacts = map[string]bool{}
	idx := srmStateIndex(current)
	if idx < 0 || idx == len(srmStateOrder)-1 {
		return "", artifacts, nil, nil
	}
	nextState = srmStateOrder[idx+1]
	for _, name := range srmGateArtifacts[nextState] {
		check, ok := srmArtifactChecks[name]
		if !ok {
			continue
		}
		passed := check(ctx, h.db, supplierID)
		artifacts[name] = passed
		if !passed {
			missing = append(missing, name)
		}
	}
	return nextState, artifacts, missing, nil
}

// srmLogGateTransition records an SRM lifecycle event in the shared audit log
// (FR-GATE-2: actor + timestamp, never back-dated).
func (h *SRMHandler) srmLogGateTransition(r *http.Request, user *models.User, supplierID int, action, fromState, toState, details string) {
	d := map[string]any{"from": fromState, "to": toState}
	if details != "" {
		d["details"] = details
	}
	id := supplierID
	logAuditWithDetails(h.db, r, user, action, "srm_supplier", &id, fmt.Sprintf("%d", id), d)
}

// srmSupplierIsClosed reports whether the supplier is in the terminal closed
// state (read-only archive per §3.1).
func (h *SRMHandler) srmSupplierIsClosed(ctx context.Context, supplierID int) bool {
	var state string
	err := h.db.QueryRowContext(ctx, `SELECT relationship_state FROM srm_suppliers WHERE id = ?`, supplierID).Scan(&state)
	return err == nil && state == models.SRMStateClosed
}

// ---------------------------------------------------------------------------
// Suppliers (master directory)
// ---------------------------------------------------------------------------

const srmSupplierColumns = `id, company_name, nzbn, tax_id_number, website, tier, relationship_state, is_active, created_at, updated_at`

func scanSRMSupplier(row interface{ Scan(...any) error }) (*models.SRMSupplier, error) {
	var s models.SRMSupplier
	err := row.Scan(&s.ID, &s.CompanyName, &s.NZBN, &s.TaxIDNumber, &s.Website, &s.Tier, &s.RelationshipState, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSuppliers returns the supplier directory with computed contract/dispute
// counts and contacts. Query params: tier, state, q (company name substring).
func (h *SRMHandler) ListSuppliers(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := `SELECT ` + srmSupplierColumns + ` FROM srm_suppliers WHERE 1=1`
	args := []any{}
	if tier := r.URL.Query().Get("tier"); tier != "" {
		query += ` AND tier = ?`
		args = append(args, tier)
	}
	if state := r.URL.Query().Get("state"); state != "" {
		query += ` AND relationship_state = ?`
		args = append(args, state)
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query += ` AND company_name LIKE ?`
		args = append(args, "%"+q+"%")
	}
	query += ` ORDER BY company_name`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	suppliers := make([]models.SRMSupplier, 0)
	for rows.Next() {
		s, err := scanSRMSupplier(rows)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		suppliers = append(suppliers, *s)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	counts, err := h.srmSupplierCounts(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range suppliers {
		suppliers[i].ActiveContracts = counts[suppliers[i].ID]["contracts"]
		suppliers[i].OpenDisputes = counts[suppliers[i].ID]["disputes"]
	}

	contacts, err := h.srmSupplierContacts(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range suppliers {
		suppliers[i].Contacts = contacts[suppliers[i].ID]
	}

	respondJSONOK(w, suppliers)
}

// srmSupplierCounts returns per-supplier active-contract and open-dispute counts.
func (h *SRMHandler) srmSupplierCounts(ctx context.Context) (map[int]map[string]int, error) {
	out := map[int]map[string]int{}
	rows, err := h.db.QueryContext(ctx, `
		SELECT supplier_id, status FROM srm_contracts WHERE status IN (?, ?)
		UNION ALL
		SELECT supplier_id, 'dispute' FROM srm_disputes WHERE stage <> ?`,
		models.SRMContractStatusActive, models.SRMContractStatusInWindUp, models.SRMDisputeClosed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, err
		}
		m := out[id]
		if m == nil {
			m = map[string]int{}
			out[id] = m
		}
		switch {
		case kind == "dispute":
			m["disputes"]++
		default:
			m["contracts"]++
		}
	}
	return out, rows.Err()
}

// srmSupplierContacts returns all contacts grouped by supplier.
func (h *SRMHandler) srmSupplierContacts(ctx context.Context) (map[int][]models.SRMSupplierContact, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, supplier_id, full_name, role_title, email, phone, is_primary, created_at
		FROM srm_supplier_contacts ORDER BY is_primary DESC, full_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int][]models.SRMSupplierContact{}
	for rows.Next() {
		var c models.SRMSupplierContact
		if err := rows.Scan(&c.ID, &c.SupplierID, &c.FullName, &c.RoleTitle, &c.Email, &c.Phone, &c.IsPrimary, &c.CreatedAt); err != nil {
			return nil, err
		}
		out[c.SupplierID] = append(out[c.SupplierID], c)
	}
	for id := range out {
		if out[id] == nil {
			out[id] = []models.SRMSupplierContact{}
		}
	}
	return out, rows.Err()
}

// isValidNZBN reports whether v is a well-formed 9-digit NZ business number,
// mirroring the CHECK constraint on srm_suppliers.nzbn so bad input fails as
// a 400 instead of tripping the database constraint.
func isValidNZBN(v string) bool {
	if len(v) != 9 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// CreateSupplier registers a new supplier in the directory. New suppliers
// start in the "registered" state; tier defaults to tier_3 until segmentation
// maps them (FR-1.2).
func (h *SRMHandler) CreateSupplier(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		CompanyName string  `json:"company_name"`
		NZBN        *string `json:"nzbn,omitempty"`
		TaxIDNumber *string `json:"tax_id_number,omitempty"`
		Website     string  `json:"website"`
		Tier        string  `json:"tier"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	if in.CompanyName == "" {
		respondBadRequest(w, r, "company_name is required")
		return
	}
	if in.Tier == "" {
		in.Tier = models.SRMTier3
	}
	switch in.Tier {
	case models.SRMTier1, models.SRMTier2, models.SRMTier3, models.SRMTier4:
	default:
		respondBadRequest(w, r, "tier must be one of tier_1..tier_4")
		return
	}
	if in.NZBN != nil {
		nzbn := strings.TrimSpace(*in.NZBN)
		switch {
		case nzbn == "":
			in.NZBN = nil
		case !isValidNZBN(nzbn):
			respondBadRequest(w, r, "nzbn must be a 9-digit NZ business number")
			return
		default:
			in.NZBN = &nzbn
		}
	}

	res, err := h.db.ExecContext(r.Context(), `
		INSERT INTO srm_suppliers (company_name, nzbn, tax_id_number, website, tier, relationship_state)
		VALUES (?, ?, ?, ?, ?, ?)`,
		in.CompanyName, in.NZBN, in.TaxIDNumber, in.Website, in.Tier, models.SRMStateRegistered)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	supplier, err := h.getSRMSupplier(r.Context(), int(id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, supplier.ID, "srm_supplier_created", "-", supplier.RelationshipState, "")
	respondJSONCreated(w, supplier)
}

// GetSupplier returns one supplier with contacts and gate status.
func (h *SRMHandler) GetSupplier(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	supplier, err := h.getSRMSupplier(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	contacts := map[int][]models.SRMSupplierContact{}
	if contacts, err = h.srmSupplierContacts(r.Context()); err != nil {
		respondInternalError(w, r, err)
		return
	}
	supplier.Contacts = contacts[id]

	nextState, artifacts, missing, err := h.srmGateStatus(r.Context(), id, supplier.RelationshipState)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	supplier.Gate = &models.SRMGateStatus{Current: supplier.RelationshipState, Next: nextState, Artifacts: artifacts, Missing: missing}

	respondJSONOK(w, supplier)
}

// getSRMSupplier loads one supplier row (sql.ErrNoRows when absent).
func (h *SRMHandler) getSRMSupplier(ctx context.Context, id int) (*models.SRMSupplier, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+srmSupplierColumns+` FROM srm_suppliers WHERE id = ?`, id)
	return scanSRMSupplier(row)
}

// UpdateSupplier edits directory fields. A closed supplier is a read-only
// archive and rejects all updates (§3.1).
func (h *SRMHandler) UpdateSupplier(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := h.getSRMSupplier(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if existing.RelationshipState == models.SRMStateClosed {
		respondBadRequest(w, r, "closed suppliers are a read-only archive; reopen via gate downgrade is not supported")
		return
	}

	var in struct {
		CompanyName *string `json:"company_name,omitempty"`
		NZBN        *string `json:"nzbn,omitempty"`
		TaxIDNumber *string `json:"tax_id_number,omitempty"`
		Website     *string `json:"website,omitempty"`
		Tier        *string `json:"tier,omitempty"`
		IsActive    *bool   `json:"is_active,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Tier != nil {
		switch *in.Tier {
		case models.SRMTier1, models.SRMTier2, models.SRMTier3, models.SRMTier4:
		default:
			respondBadRequest(w, r, "tier must be one of tier_1..tier_4")
			return
		}
	}
	if in.NZBN != nil {
		nzbn := strings.TrimSpace(*in.NZBN)
		switch {
		case nzbn == "":
			in.NZBN = nil // empty string clears the field
		case !isValidNZBN(nzbn):
			respondBadRequest(w, r, "nzbn must be a 9-digit NZ business number")
			return
		default:
			in.NZBN = &nzbn
		}
	}

	if in.CompanyName != nil {
		name := strings.TrimSpace(*in.CompanyName)
		if name == "" {
			respondBadRequest(w, r, "company_name cannot be empty")
			return
		}
		in.CompanyName = &name
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_suppliers SET
			company_name = ?, nzbn = ?, tax_id_number = ?, website = ?, tier = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		orKeep(in.CompanyName, existing.CompanyName), in.NZBN, in.TaxIDNumber, orKeep(in.Website, existing.Website),
		orKeep(in.Tier, existing.Tier), boolOr(in.IsActive, existing.IsActive), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	supplier, err := h.getSRMSupplier(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, id, "srm_supplier_updated", supplier.RelationshipState, supplier.RelationshipState, "")
	respondJSONOK(w, supplier)
}

// DeleteSupplier removes a supplier and cascades its SRM data. Suppliers with
// active contracts are protected (RESTRICT on the contract FK).
func (h *SRMHandler) DeleteSupplier(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	existing, err := h.getSRMSupplier(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_suppliers WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	h.srmLogGateTransition(r, user, id, "srm_supplier_deleted", existing.RelationshipState, "-", "")
	w.WriteHeader(http.StatusNoContent)
}

// AddSupplierContact adds a contact to a supplier. Setting is_primary clears
// the flag on the supplier's other contacts (one primary per supplier).
func (h *SRMHandler) AddSupplierContact(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		FullName  string `json:"full_name"`
		RoleTitle string `json:"role_title"`
		Email     string `json:"email"`
		Phone     string `json:"phone"`
		IsPrimary bool   `json:"is_primary"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.TrimSpace(in.Email)
	if in.FullName == "" || in.Email == "" {
		respondBadRequest(w, r, "full_name and email are required")
		return
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer tx.Rollback()

	if in.IsPrimary {
		if _, err := tx.ExecContext(ctx, `UPDATE srm_supplier_contacts SET is_primary = false WHERE supplier_id = ?`, id); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO srm_supplier_contacts (supplier_id, full_name, role_title, email, phone, is_primary)
		VALUES (?, ?, ?, ?, ?, ?)`, id, in.FullName, in.RoleTitle, in.Email, in.Phone, in.IsPrimary)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	cid, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var c models.SRMSupplierContact
	err = h.db.QueryRowContext(ctx, `
		SELECT id, supplier_id, full_name, role_title, email, phone, is_primary, created_at
		FROM srm_supplier_contacts WHERE id = ?`, int(cid)).
		Scan(&c.ID, &c.SupplierID, &c.FullName, &c.RoleTitle, &c.Email, &c.Phone, &c.IsPrimary, &c.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, c)
}

// UpdateSupplierContact edits a contact (any supplier's).
func (h *SRMHandler) UpdateSupplierContact(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		FullName  *string `json:"full_name,omitempty"`
		RoleTitle *string `json:"role_title,omitempty"`
		Email     *string `json:"email,omitempty"`
		Phone     *string `json:"phone,omitempty"`
		IsPrimary *bool   `json:"is_primary,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	var existing models.SRMSupplierContact
	err := h.db.QueryRowContext(ctx, `
		SELECT id, supplier_id, full_name, role_title, email, phone, is_primary, created_at
		FROM srm_supplier_contacts WHERE id = ?`, id).
		Scan(&existing.ID, &existing.SupplierID, &existing.FullName, &existing.RoleTitle, &existing.Email, &existing.Phone, &existing.IsPrimary, &existing.CreatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Contact not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer tx.Rollback()
	if in.IsPrimary != nil && *in.IsPrimary {
		if _, err := tx.ExecContext(ctx, `UPDATE srm_supplier_contacts SET is_primary = false WHERE supplier_id = ?`, existing.SupplierID); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE srm_supplier_contacts SET full_name = ?, role_title = ?, email = ?, phone = ?, is_primary = ? WHERE id = ?`,
		orKeep(in.FullName, existing.FullName), orKeep(in.RoleTitle, existing.RoleTitle), orKeep(in.Email, existing.Email),
		orKeep(in.Phone, existing.Phone), boolOr(in.IsPrimary, existing.IsPrimary), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var c models.SRMSupplierContact
	err = h.db.QueryRowContext(ctx, `
		SELECT id, supplier_id, full_name, role_title, email, phone, is_primary, created_at
		FROM srm_supplier_contacts WHERE id = ?`, id).
		Scan(&c.ID, &c.SupplierID, &c.FullName, &c.RoleTitle, &c.Email, &c.Phone, &c.IsPrimary, &c.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, c)
}

// DeleteSupplierContact removes a contact.
func (h *SRMHandler) DeleteSupplierContact(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_supplier_contacts WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Contact not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Contract register (Phase 1 / Step 1.1 — Rule 34 fields)
// ---------------------------------------------------------------------------

const srmContractColumns = `id, supplier_id, p3m_entity_type, p3m_entity_id, contract_number, title, category, classification, scope_summary, document_url, status, total_value, currency, committed_spend, actual_spend, spend_source, start_date, end_date, extension_count, notice_period_days, review_date, cost_code, po_number, termination_reason, created_at, updated_at`

func scanSRMContract(row interface{ Scan(...any) error }) (*models.SRMContract, error) {
	var c models.SRMContract
	err := row.Scan(&c.ID, &c.SupplierID, &c.P3MEntityType, &c.P3MEntityID, &c.ContractNumber, &c.Title, &c.Category, &c.Classification, &c.ScopeSummary, &c.DocumentURL, &c.Status, &c.TotalValue, &c.Currency, &c.CommittedSpend, &c.ActualSpend, &c.SpendSource, &c.StartDate, &c.EndDate, &c.ExtensionCount, &c.NoticePeriodDays, &c.ReviewDate, &c.CostCode, &c.PONumber, &c.TerminationReason, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// srmValidateContractInput checks the shared create/update invariants: enums,
// date ordering and the polymorphic P3M binding (entity type + row existence).
func (h *SRMHandler) srmValidateContractInput(ctx context.Context, supplierID int, entityType *string, entityID *int, status, spendSource string, start, end time.Time) error {
	switch status {
	case models.SRMContractStatusDraft, models.SRMContractStatusActive, models.SRMContractStatusInWindUp, models.SRMContractStatusRetired, models.SRMContractStatusTerminated:
	default:
		return fmt.Errorf("status must be one of draft, active, in_wind_up, retired, terminated")
	}
	switch spendSource {
	case "manual", "erp_api":
	default:
		return fmt.Errorf("spend_source must be manual or erp_api")
	}
	if !end.After(start) {
		return fmt.Errorf("end_date must be after start_date")
	}
	var supplierExists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_suppliers WHERE id = ?`, supplierID).Scan(&supplierExists); err != nil {
		return err
	}
	if supplierExists == 0 {
		return fmt.Errorf("supplier %d not found", supplierID)
	}
	if entityType != nil && entityID != nil {
		switch *entityType {
		case models.P3MEntityPortfolio, models.P3MEntityProgramme, models.P3MEntityProject:
		default:
			return fmt.Errorf("p3m_entity_type must be portfolio, programme or project")
		}
		table := map[string]string{
			models.P3MEntityPortfolio: "p3m_portfolios",
			models.P3MEntityProgramme: "p3m_programmes",
			models.P3MEntityProject:   "p3m_projects",
		}[*entityType]
		var n int
		if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, *entityID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s %d not found", *entityType, *entityID)
		}
	} else if (entityType == nil) != (entityID == nil) {
		return fmt.Errorf("p3m_entity_type and p3m_entity_id must be provided together")
	}
	return nil
}

// ListContracts returns the contract register. Query params: supplier_id,
// status, q (number or title substring).
func (h *SRMHandler) ListContracts(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()

	query := "SELECT " + qualifyColumns("c", srmContractColumns) + ", s.company_name AS supplier_name FROM srm_contracts c JOIN srm_suppliers s ON s.id = c.supplier_id WHERE 1=1"
	args := []any{}
	if v, ok := parseQueryInt(r.URL.Query().Get("supplier_id")); ok {
		query += ` AND c.supplier_id = ?`
		args = append(args, v)
	}
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND c.status = ?`
		args = append(args, status)
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query += ` AND (c.contract_number LIKE ? OR c.title LIKE ?)`
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	query += ` ORDER BY c.created_at DESC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	contracts := make([]models.SRMContract, 0)
	for rows.Next() {
		var c models.SRMContract
		err := rows.Scan(&c.ID, &c.SupplierID, &c.P3MEntityType, &c.P3MEntityID, &c.ContractNumber, &c.Title, &c.Category, &c.Classification, &c.ScopeSummary, &c.DocumentURL, &c.Status, &c.TotalValue, &c.Currency, &c.CommittedSpend, &c.ActualSpend, &c.SpendSource, &c.StartDate, &c.EndDate, &c.ExtensionCount, &c.NoticePeriodDays, &c.ReviewDate, &c.CostCode, &c.PONumber, &c.TerminationReason, &c.CreatedAt, &c.UpdatedAt, &c.SupplierName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		contracts = append(contracts, c)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}

	assignments, err := h.srmContractAssignments(ctx)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range contracts {
		contracts[i].Assignments = assignments[contracts[i].ID]
	}

	ptrs := make([]*models.SRMContract, len(contracts))
	for i := range contracts {
		ptrs[i] = &contracts[i]
	}
	if err := h.fillP3MEntityTitles(ctx, ptrs); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, contracts)
}

// srmContractAssignments returns all role assignments grouped by contract.
func (h *SRMHandler) srmContractAssignments(ctx context.Context) (map[int][]models.SRMContractAssignment, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT a.id, a.contract_id, a.user_id, a.role, a.assigned_at, u.username
		FROM srm_contract_assignments a JOIN users u ON u.id = a.user_id
		ORDER BY a.contract_id, a.role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int][]models.SRMContractAssignment{}
	for rows.Next() {
		var a models.SRMContractAssignment
		if err := rows.Scan(&a.ID, &a.ContractID, &a.UserID, &a.Role, &a.AssignedAt, &a.UserName); err != nil {
			return nil, err
		}
		out[a.ContractID] = append(out[a.ContractID], a)
	}
	for id := range out {
		if out[id] == nil {
			out[id] = []models.SRMContractAssignment{}
		}
	}
	return out, rows.Err()
}

// CreateContract registers a Rule-34 contract. New contracts start as drafts;
// the supplier's gate state is unaffected until segmentation artefacts exist.
func (h *SRMHandler) CreateContract(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		SupplierID        int       `json:"supplier_id"`
		P3MEntityType     *string   `json:"p3m_entity_type,omitempty"`
		P3MEntityID       *int      `json:"p3m_entity_id,omitempty"`
		ContractNumber    string    `json:"contract_number"`
		Title             string    `json:"title"`
		Category          string    `json:"category"`
		Classification    string    `json:"classification"`
		ScopeSummary      string    `json:"scope_summary"`
		DocumentURL       string    `json:"document_url"`
		Status            string    `json:"status"`
		TotalValue        float64   `json:"total_value"`
		Currency          string    `json:"currency"`
		CommittedSpend    float64   `json:"committed_spend"`
		ActualSpend       float64   `json:"actual_spend"`
		SpendSource       string    `json:"spend_source"`
		StartDate         time.Time `json:"start_date"`
		EndDate           time.Time `json:"end_date"`
		ExtensionCount    int       `json:"extension_count"`
		NoticePeriodDays  *int      `json:"notice_period_days,omitempty"`
		ReviewDate        *time.Time `json:"review_date,omitempty"`
		CostCode          string    `json:"cost_code"`
		PONumber          string    `json:"po_number"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.ContractNumber = strings.TrimSpace(in.ContractNumber)
	in.Title = strings.TrimSpace(in.Title)
	if in.ContractNumber == "" || in.Title == "" {
		respondBadRequest(w, r, "contract_number and title are required")
		return
	}
	if in.Status == "" {
		in.Status = models.SRMContractStatusDraft
	}
	if in.Currency == "" {
		in.Currency = "NZD"
	}
	if in.SpendSource == "" {
		in.SpendSource = "manual"
	}
	ctx := r.Context()
	if err := h.srmValidateContractInput(ctx, in.SupplierID, in.P3MEntityType, in.P3MEntityID, in.Status, in.SpendSource, in.StartDate, in.EndDate); err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_contracts (supplier_id, p3m_entity_type, p3m_entity_id, contract_number, title, category, classification, scope_summary, document_url, status, total_value, currency, committed_spend, actual_spend, spend_source, start_date, end_date, extension_count, notice_period_days, review_date, cost_code, po_number)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.SupplierID, in.P3MEntityType, in.P3MEntityID, in.ContractNumber, in.Title, in.Category, in.Classification, in.ScopeSummary, in.DocumentURL, in.Status, in.TotalValue, in.Currency, in.CommittedSpend, in.ActualSpend, in.SpendSource, in.StartDate, in.EndDate, in.ExtensionCount, nullInt(in.NoticePeriodDays), nullTimePtr(in.ReviewDate), in.CostCode, in.PONumber)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	contract, err := h.getSRMContract(ctx, int(id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.fillP3MEntityTitles(ctx, []*models.SRMContract{contract}); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, contract.SupplierID, "srm_contract_created", "-", "-", in.ContractNumber)
	respondJSONCreated(w, contract)
}

// GetContract returns one contract with assignments, documents (with clause
// extractions) and the latest management plan.
func (h *SRMHandler) GetContract(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	contract, err := h.getSRMContract(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Contract not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	assignments := map[int][]models.SRMContractAssignment{}
	if assignments, err = h.srmContractAssignments(r.Context()); err != nil {
		respondInternalError(w, r, err)
		return
	}
	contract.Assignments = assignments[id]

	docs, err := h.srmContractDocuments(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	contract.Documents = docs

	var plan models.SRMContractManagementPlan
	err = h.db.QueryRowContext(r.Context(), `
		SELECT id, contract_id, version, right_size_tier, objectives, key_terms_summary, governance_arrangements, monitoring_provisions, change_reason, created_by, created_at
		FROM srm_contract_management_plans WHERE contract_id = ? ORDER BY version DESC LIMIT 1`, id).
		Scan(&plan.ID, &plan.ContractID, &plan.Version, &plan.RightSizeTier, &plan.Objectives, &plan.KeyTermsSummary, &plan.GovernanceArrangements, &plan.MonitoringProvisions, &plan.ChangeReason, &plan.CreatedBy, &plan.CreatedAt)
	if err == nil {
		contract.ManagementPlan = &plan
	} else if !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	if err := h.fillP3MEntityTitles(r.Context(), []*models.SRMContract{contract}); err != nil {
		respondInternalError(w, r, err)
		return
	}

	respondJSONOK(w, contract)
}

// getSRMContract loads one contract row (sql.ErrNoRows when absent).
func (h *SRMHandler) getSRMContract(ctx context.Context, id int) (*models.SRMContract, error) {
	row := h.db.QueryRowContext(ctx, `SELECT `+srmContractColumns+` FROM srm_contracts WHERE id = ?`, id)
	return scanSRMContract(row)
}

// fillP3MEntityTitles resolves the linked P3M entity title for contracts that
// carry a polymorphic p3m_entity reference, using one query per entity type.
func (h *SRMHandler) fillP3MEntityTitles(ctx context.Context, contracts []*models.SRMContract) error {
	idsByType := map[string]map[int]bool{}
	for _, c := range contracts {
		if c.P3MEntityType == nil || c.P3MEntityID == nil {
			continue
		}
		t := *c.P3MEntityType
		if idsByType[t] == nil {
			idsByType[t] = map[int]bool{}
		}
		idsByType[t][*c.P3MEntityID] = true
	}
	if len(idsByType) == 0 {
		return nil
	}

	tables := map[string]string{
		models.P3MEntityPortfolio: "p3m_portfolios",
		models.P3MEntityProgramme: "p3m_programmes",
		models.P3MEntityProject:   "p3m_projects",
	}
	titles := map[string]map[int]string{}
	for t, idSet := range idsByType {
		table, ok := tables[t]
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

	for _, c := range contracts {
		if c.P3MEntityType == nil || c.P3MEntityID == nil {
			continue
		}
		if title, ok := titles[*c.P3MEntityType][*c.P3MEntityID]; ok {
			c.P3MEntityTitle = &title
		}
	}
	return nil
}

// srmProjectRow is one P3M project with its SRM contract aggregates for the
// cross-module projects view.
type srmProjectRow struct {
	ID                 int     `json:"id"`
	Title              string  `json:"title"`
	CurrentStage       string  `json:"current_stage"`
	RagStatus          string  `json:"rag_status"`
	BudgetApproved     float64 `json:"budget_approved"`
	TimeProjectID      *int    `json:"time_project_id,omitempty"`
	ContractCount      int     `json:"contract_count"`
	ContractValueTotal float64 `json:"contract_value_total"`
	CommittedSpend     float64 `json:"committed_spend"`
	ActualSpend        float64 `json:"actual_spend"`
	StakeholderCount   int     `json:"stakeholder_count"`
}

// ListProjectsForSRM handles GET /api/srm/projects. It lists P3M projects with
// SRM contract aggregates so the SRM module can track financials and
// stakeholders across projects; per-project detail comes from
// GET /p3m/projects/{id}/rollup.
func (h *SRMHandler) ListProjectsForSRM(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT p.id, p.title, p.current_stage, p.rag_status, p.budget_approved, p.time_project_id,
			COALESCE(f.n, 0), COALESCE(f.value_total, 0), COALESCE(f.committed_spend, 0), COALESCE(f.actual_spend, 0),
			(SELECT COUNT(*) FROM p3m_stakeholders ps WHERE ps.entity_type = 'project' AND ps.entity_id = p.id) +
			(SELECT COUNT(*) FROM srm_entity_stakeholders es WHERE es.entity_type = 'project' AND es.entity_id = p.id)
		FROM p3m_projects p
		LEFT JOIN (
			SELECT p3m_entity_id, COUNT(*) AS n, SUM(total_value) AS value_total,
				SUM(committed_spend) AS committed_spend, SUM(actual_spend) AS actual_spend
			FROM srm_contracts WHERE p3m_entity_type = 'project' GROUP BY p3m_entity_id
		) f ON f.p3m_entity_id = p.id
		ORDER BY p.title`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := make([]srmProjectRow, 0)
	for rows.Next() {
		var row srmProjectRow
		if err := rows.Scan(&row.ID, &row.Title, &row.CurrentStage, &row.RagStatus, &row.BudgetApproved,
			&row.TimeProjectID, &row.ContractCount, &row.ContractValueTotal, &row.CommittedSpend, &row.ActualSpend, &row.StakeholderCount); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, out)
}

// p3mOption is a minimal P3M entity reference for the contract form's entity
// picker. It is served from the SRM module so users with only srm.view can
// link contracts to P3M entities without needing p3m.view access.
type p3mOption struct {
	Type  string `json:"type"`
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// ListP3MOptions handles GET /api/srm/p3m-options. It returns every P3M
// portfolio, programme and project as a flat option list for the contract
// form's p3m_entity picker.
func (h *SRMHandler) ListP3MOptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	out := make([]p3mOption, 0)
	for _, q := range []struct{ typ, table string }{
		{"portfolio", "p3m_portfolios"},
		{"programme", "p3m_programmes"},
		{"project", "p3m_projects"},
	} {
		rows, err := h.db.QueryContext(r.Context(), `SELECT id, title FROM `+q.table+` ORDER BY title`)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		for rows.Next() {
			var opt p3mOption
			opt.Type = q.typ
			if err := rows.Scan(&opt.ID, &opt.Title); err != nil {
				rows.Close()
				respondInternalError(w, r, err)
				return
			}
			out = append(out, opt)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
	}
	respondJSONOK(w, out)
}

// UpdateContract edits register fields. Retiring or terminating a contract is
// the trigger that may close its supplier relationship (§3.1).
func (h *SRMHandler) UpdateContract(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := h.getSRMContract(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Contract not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		SupplierID        *int       `json:"supplier_id,omitempty"`
		P3MEntityType     *string    `json:"p3m_entity_type,omitempty"`
		P3MEntityID       *int       `json:"p3m_entity_id,omitempty"`
		ContractNumber    *string    `json:"contract_number,omitempty"`
		Title             *string    `json:"title,omitempty"`
		Category          *string    `json:"category,omitempty"`
		Classification    *string    `json:"classification,omitempty"`
		ScopeSummary      *string    `json:"scope_summary,omitempty"`
		DocumentURL       *string    `json:"document_url,omitempty"`
		Status            *string    `json:"status,omitempty"`
		TotalValue        *float64   `json:"total_value,omitempty"`
		Currency          *string    `json:"currency,omitempty"`
		CommittedSpend    *float64   `json:"committed_spend,omitempty"`
		ActualSpend       *float64   `json:"actual_spend,omitempty"`
		SpendSource       *string    `json:"spend_source,omitempty"`
		StartDate         *time.Time `json:"start_date,omitempty"`
		EndDate           *time.Time `json:"end_date,omitempty"`
		ExtensionCount    *int       `json:"extension_count,omitempty"`
		NoticePeriodDays  *int       `json:"notice_period_days,omitempty"`
		ReviewDate        *time.Time `json:"review_date,omitempty"`
		CostCode          *string    `json:"cost_code,omitempty"`
		PONumber          *string    `json:"po_number,omitempty"`
		TerminationReason *string    `json:"termination_reason,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	supplierID := existing.SupplierID
	if in.SupplierID != nil {
		supplierID = *in.SupplierID
	}
	entityType := existing.P3MEntityType
	if in.P3MEntityType != nil {
		entityType = in.P3MEntityType
	}
	entityID := existing.P3MEntityID
	if in.P3MEntityID != nil {
		entityID = in.P3MEntityID
	}
	status := existing.Status
	if in.Status != nil {
		status = *in.Status
	}
	spendSource := existing.SpendSource
	if in.SpendSource != nil {
		spendSource = *in.SpendSource
	}
	startDate := existing.StartDate
	if in.StartDate != nil {
		startDate = *in.StartDate
	}
	endDate := existing.EndDate
	if in.EndDate != nil {
		endDate = *in.EndDate
	}

	if err := h.srmValidateContractInput(ctx, supplierID, entityType, entityID, status, spendSource, startDate, endDate); err != nil {
		respondBadRequest(w, r, err.Error())
		return
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_contracts SET
			supplier_id = ?, p3m_entity_type = ?, p3m_entity_id = ?, contract_number = ?, title = ?, category = ?, classification = ?, scope_summary = ?, document_url = ?, status = ?, total_value = ?, currency = ?, committed_spend = ?, actual_spend = ?, spend_source = ?, start_date = ?, end_date = ?, extension_count = ?, notice_period_days = ?, review_date = ?, cost_code = ?, po_number = ?, termination_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		supplierID, entityType, entityID, orKeep(in.ContractNumber, existing.ContractNumber), orKeep(in.Title, existing.Title), orKeep(in.Category, existing.Category), orKeep(in.Classification, existing.Classification), orKeep(in.ScopeSummary, existing.ScopeSummary), orKeep(in.DocumentURL, existing.DocumentURL), status,
		floatOr(in.TotalValue, existing.TotalValue), orKeep(in.Currency, existing.Currency), floatOr(in.CommittedSpend, existing.CommittedSpend), floatOr(in.ActualSpend, existing.ActualSpend), spendSource,
		startDate, endDate, intOr(in.ExtensionCount, existing.ExtensionCount), nullInt(in.NoticePeriodDays), nullTimePtr(in.ReviewDate), orKeep(in.CostCode, existing.CostCode), orKeep(in.PONumber, existing.PONumber), orKeep(in.TerminationReason, existing.TerminationReason), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	contract, err := h.getSRMContract(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, contract.SupplierID, "srm_contract_updated", existing.Status, status, contract.ContractNumber)
	respondJSONOK(w, contract)
}

// DeleteContract removes a contract and cascades its SRM children.
func (h *SRMHandler) DeleteContract(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	existing, err := h.getSRMContract(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Contract not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_contracts WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Contract not found")
		return
	}
	h.srmLogGateTransition(r, user, existing.SupplierID, "srm_contract_deleted", "-", "-", existing.ContractNumber)
	w.WriteHeader(http.StatusNoContent)
}

// AssignContractRole grants a Rule-34 role on a contract to a user. The same
// (contract, role, user) triple is rejected as a duplicate (DB UNIQUE).
func (h *SRMHandler) AssignContractRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		UserID int    `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Role {
	case models.SRMContractRoleInternalManager, models.SRMContractRoleOwner, models.SRMContractRoleBudgetApprover:
	default:
		respondBadRequest(w, r, "role must be internal_contract_manager, contract_owner or budget_approver")
		return
	}

	ctx := r.Context()
	var userExists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, in.UserID).Scan(&userExists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if userExists == 0 {
		respondBadRequest(w, r, "user not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_contract_assignments (contract_id, user_id, role) VALUES (?, ?, ?)`, id, in.UserID, in.Role)
	if err != nil {
		respondBadRequest(w, r, "user already holds this role on the contract")
		return
	}
	aID, _ := res.LastInsertId()

	var a models.SRMContractAssignment
	err = h.db.QueryRowContext(ctx, `
		SELECT a.id, a.contract_id, a.user_id, a.role, a.assigned_at, u.username
		FROM srm_contract_assignments a JOIN users u ON u.id = a.user_id WHERE a.id = ?`, int(aID)).
		Scan(&a.ID, &a.ContractID, &a.UserID, &a.Role, &a.AssignedAt, &a.UserName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, a)
}

// RemoveContractAssignment revokes one role assignment.
func (h *SRMHandler) RemoveContractAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "assignmentId")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_contract_assignments WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Assignment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Contract documents & clause extraction review (Phase 1 / Step 1.2)
// ---------------------------------------------------------------------------

var srmDocTypes = map[string]bool{
	"contract": true, "variation": true, "sla_schedule": true, "insurance": true, "other": true,
}

func scanSRMContractDocument(row interface{ Scan(...any) error }) (*models.SRMContractDocument, error) {
	var d models.SRMContractDocument
	err := row.Scan(&d.ID, &d.ContractID, &d.DocType, &d.FileName, &d.StoragePath, &d.Version, &d.EffectiveDate, &d.UploadedBy, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// srmContractDocuments loads a contract's documents with their clause
// extractions attached (used by GetContract and ListContractDocuments).
func (h *SRMHandler) srmContractDocuments(ctx context.Context, contractID int) ([]models.SRMContractDocument, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, contract_id, doc_type, file_name, storage_path, version, effective_date, uploaded_by, created_at
		FROM srm_contract_documents WHERE contract_id = ? ORDER BY doc_type, version`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]models.SRMContractDocument, 0)
	for rows.Next() {
		d, err := scanSRMContractDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	extractions, err := h.srmDocumentExtractions(ctx)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		docs[i].Extractions = extractions[docs[i].ID]
	}
	return docs, nil
}

// srmDocumentExtractions returns all pending/reviewed extractions grouped by document.
func (h *SRMHandler) srmDocumentExtractions(ctx context.Context) (map[int][]models.SRMClauseExtraction, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, document_id, field_name, proposed_value, confidence, status, reviewed_by, reviewed_at
		FROM srm_clause_extractions ORDER BY document_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int][]models.SRMClauseExtraction{}
	for rows.Next() {
		var e models.SRMClauseExtraction
		if err := rows.Scan(&e.ID, &e.DocumentID, &e.FieldName, &e.ProposedValue, &e.Confidence, &e.Status, &e.ReviewedBy, &e.ReviewedAt); err != nil {
			return nil, err
		}
		out[e.DocumentID] = append(out[e.DocumentID], e)
	}
	for id := range out {
		if out[id] == nil {
			out[id] = []models.SRMClauseExtraction{}
		}
	}
	return out, rows.Err()
}

// ListContractDocuments returns a contract's documents with extractions.
func (h *SRMHandler) ListContractDocuments(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	var exists int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_contracts WHERE id = ?`, id).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists == 0 {
		respondNotFound(w, r, "Contract not found")
		return
	}

	docs, err := h.srmContractDocuments(r.Context(), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, docs)
}

// AddContractDocument registers a document against a contract. The file bytes
// live in the app's blob store; this row is the SRM metadata wrapper.
func (h *SRMHandler) AddContractDocument(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		DocType       string     `json:"doc_type"`
		FileName      string     `json:"file_name"`
		StoragePath   string     `json:"storage_path"`
		Version       int        `json:"version"`
		EffectiveDate *time.Time `json:"effective_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.FileName = strings.TrimSpace(in.FileName)
	if in.DocType == "" || !srmDocTypes[in.DocType] {
		respondBadRequest(w, r, "doc_type must be one of contract, variation, sla_schedule, insurance, other")
		return
	}
	if in.FileName == "" {
		respondBadRequest(w, r, "file_name is required")
		return
	}
	if in.Version <= 0 {
		in.Version = 1
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_contracts WHERE id = ?`, id).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists == 0 {
		respondNotFound(w, r, "Contract not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_contract_documents (contract_id, doc_type, file_name, storage_path, version, effective_date, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, in.DocType, in.FileName, in.StoragePath, in.Version, nullTimePtr(in.EffectiveDate), user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	dID, _ := res.LastInsertId()

	var d models.SRMContractDocument
	err = h.db.QueryRowContext(ctx, `
		SELECT id, contract_id, doc_type, file_name, storage_path, version, effective_date, uploaded_by, created_at
		FROM srm_contract_documents WHERE id = ?`, int(dID)).
		Scan(&d.ID, &d.ContractID, &d.DocType, &d.FileName, &d.StoragePath, &d.Version, &d.EffectiveDate, &d.UploadedBy, &d.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	d.Extractions = []models.SRMClauseExtraction{}
	respondJSONCreated(w, d)
}

// DeleteContractDocument removes a document and its extractions (CASCADE).
func (h *SRMHandler) DeleteContractDocument(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_contract_documents WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Document not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListExtractions returns a document's clause extractions.
func (h *SRMHandler) ListExtractions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	var exists int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM srm_contract_documents WHERE id = ?`, id).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists == 0 {
		respondNotFound(w, r, "Document not found")
		return
	}

	all, err := h.srmDocumentExtractions(r.Context())
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	extrs := all[id]
	if extrs == nil {
		extrs = []models.SRMClauseExtraction{}
	}
	respondJSONOK(w, extrs)
}

// ReviewClauseExtraction accepts or rejects a proposed clause value. Only the
// reviewer's decision is stored; accepted values are applied to the contract
// register by the user (no silent overwrites of Rule-34 fields).
func (h *SRMHandler) ReviewClauseExtraction(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.Status {
	case "accepted", "rejected":
	default:
		respondBadRequest(w, r, "status must be accepted or rejected")
		return
	}

	ctx := r.Context()
	var existing models.SRMClauseExtraction
	err := h.db.QueryRowContext(ctx, `
		SELECT id, document_id, field_name, proposed_value, confidence, status, reviewed_by, reviewed_at
		FROM srm_clause_extractions WHERE id = ?`, id).
		Scan(&existing.ID, &existing.DocumentID, &existing.FieldName, &existing.ProposedValue, &existing.Confidence, &existing.Status, &existing.ReviewedBy, &existing.ReviewedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Extraction not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	now := time.Now()
	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_clause_extractions SET status = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ?`, in.Status, user.ID, now, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	existing.Status = in.Status
	existing.ReviewedBy = &user.ID
	existing.ReviewedAt = &now
	respondJSONOK(w, existing)
}

// CreateManagementPlan adds the next version of a contract's right-sized
// management plan. Version numbers are assigned server-side (max+1).
func (h *SRMHandler) CreateManagementPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		RightSizeTier          string `json:"right_size_tier"`
		Objectives             string `json:"objectives"`
		KeyTermsSummary        string `json:"key_terms_summary"`
		GovernanceArrangements string `json:"governance_arrangements"`
		MonitoringProvisions   string `json:"monitoring_provisions"`
		ChangeReason           string `json:"change_reason"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	switch in.RightSizeTier {
	case "minimal", "essential", "standard", "comprehensive":
	default:
		respondBadRequest(w, r, "right_size_tier must be one of minimal, essential, standard, comprehensive")
		return
	}
	if strings.TrimSpace(in.GovernanceArrangements) == "" {
		respondBadRequest(w, r, "governance_arrangements is required")
		return
	}

	ctx := r.Context()
	var exists int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_contracts WHERE id = ?`, id).Scan(&exists); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if exists == 0 {
		respondNotFound(w, r, "Contract not found")
		return
	}

	var version int
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM srm_contract_management_plans WHERE contract_id = ?`, id).Scan(&version); err != nil {
		respondInternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_contract_management_plans (contract_id, version, right_size_tier, objectives, key_terms_summary, governance_arrangements, monitoring_provisions, change_reason, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, version, in.RightSizeTier, in.Objectives, in.KeyTermsSummary, in.GovernanceArrangements, in.MonitoringProvisions, in.ChangeReason, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	pID, _ := res.LastInsertId()

	var p models.SRMContractManagementPlan
	err = h.db.QueryRowContext(ctx, `
		SELECT id, contract_id, version, right_size_tier, objectives, key_terms_summary, governance_arrangements, monitoring_provisions, change_reason, created_by, created_at
		FROM srm_contract_management_plans WHERE id = ?`, int(pID)).
		Scan(&p.ID, &p.ContractID, &p.Version, &p.RightSizeTier, &p.Objectives, &p.KeyTermsSummary, &p.GovernanceArrangements, &p.MonitoringProvisions, &p.ChangeReason, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, p)
}

// ---------------------------------------------------------------------------
// Phase-gate transitions (§3.1, FR-GATE-1/2/3) and module summary
// ---------------------------------------------------------------------------

// GetSupplierGates returns the phase-gate position and the artefacts required
// for the next gate (FR-GATE-1).
func (h *SRMHandler) GetSupplierGates(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	supplier, err := h.getSRMSupplier(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	nextState, artifacts, missing, err := h.srmGateStatus(r.Context(), id, supplier.RelationshipState)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, models.SRMGateStatus{Current: supplier.RelationshipState, Next: nextState, Artifacts: artifacts, Missing: missing})
}

// AdvanceGate moves a supplier one gate forward after verifying every entry
// artefact exists. Transitions are audit-logged (FR-GATE-2).
func (h *SRMHandler) AdvanceGate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	supplier, err := h.getSRMSupplier(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	idx := srmStateIndex(supplier.RelationshipState)
	if idx < 0 {
		respondConflict(w, r, "closed suppliers are a read-only archive and cannot advance gates")
		return
	}

	nextState, artifacts, missing, err := h.srmGateStatus(ctx, id, supplier.RelationshipState)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if nextState == "" { // monitoring_active: closed is only reachable via explicit close
		respondConflict(w, r, "no further gates; close the supplier explicitly")
		return
	}
	if len(missing) > 0 {
		respondJSON(w, http.StatusConflict, map[string]any{
			"error":     "gate artefacts missing",
			"current":   supplier.RelationshipState,
			"next":      nextState,
			"artifacts": artifacts,
			"missing":   missing,
		})
		return
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE srm_suppliers SET relationship_state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, nextState, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, id, "srm_gate_advance", supplier.RelationshipState, nextState, "")

	supplier.RelationshipState = nextState
	gNext, gArtifacts, gMissing, _ := h.srmGateStatus(ctx, id, nextState)
	supplier.Gate = &models.SRMGateStatus{Current: nextState, Next: gNext, Artifacts: gArtifacts, Missing: gMissing}
	respondJSONOK(w, supplier)
}

// DowngradeGate moves a supplier one gate back. A mandatory reason is recorded
// in the audit log (FR-GATE-3). Closed suppliers cannot be downgraded.
func (h *SRMHandler) DowngradeGate(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		respondBadRequest(w, r, "reason is required for a gate downgrade")
		return
	}

	ctx := r.Context()
	supplier, err := h.getSRMSupplier(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	idx := srmStateIndex(supplier.RelationshipState)
	if idx < 0 {
		respondConflict(w, r, "closed suppliers are a read-only archive and cannot be downgraded")
		return
	}
	if idx == 0 {
		respondConflict(w, r, "supplier is already at the first gate")
		return
	}
	prevState := srmStateOrder[idx-1]

	if _, err := h.db.ExecContext(ctx, `UPDATE srm_suppliers SET relationship_state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, prevState, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, id, "srm_gate_downgrade", supplier.RelationshipState, prevState, in.Reason)

	supplier.RelationshipState = prevState
	respondJSONOK(w, supplier)
}

// CloseSupplier archives a supplier relationship (contract retired/terminated).
// Closed suppliers are read-only: updates and gate advances are rejected.
func (h *SRMHandler) CloseSupplier(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		respondBadRequest(w, r, "reason is required to close a supplier relationship")
		return
	}

	ctx := r.Context()
	supplier, err := h.getSRMSupplier(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if supplier.RelationshipState == models.SRMStateClosed {
		respondConflict(w, r, "supplier is already closed")
		return
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE srm_suppliers SET relationship_state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, models.SRMStateClosed, id); err != nil {
		respondInternalError(w, r, err)
		return
	}
	h.srmLogGateTransition(r, user, id, "srm_supplier_closed", supplier.RelationshipState, models.SRMStateClosed, in.Reason)

	supplier.RelationshipState = models.SRMStateClosed
	supplier.Gate = &models.SRMGateStatus{Current: models.SRMStateClosed, Artifacts: map[string]bool{}, Missing: []string{}}
	respondJSONOK(w, supplier)
}

// GetSummary returns the module dashboard aggregates (landing page).
func (h *SRMHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	db := h.db

	countBy := func(query string) map[string]int {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return map[string]int{}
		}
		defer rows.Close()
		out := map[string]int{}
		for rows.Next() {
			var key string
			var n int
			if err := rows.Scan(&key, &n); err == nil {
				out[key] = n
			}
		}
		return out
	}

	suppliersByState := countBy(`SELECT relationship_state, COUNT(*) FROM srm_suppliers GROUP BY relationship_state`)
	suppliersByTier := countBy(`SELECT tier, COUNT(*) FROM srm_suppliers GROUP BY tier`)
	contractsByStatus := countBy(`SELECT status, COUNT(*) FROM srm_contracts GROUP BY status`)

	var activeValue float64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_value), 0) FROM srm_contracts WHERE status = 'active'`).Scan(&activeValue); err != nil {
		respondInternalError(w, r, err)
		return
	}

	var openDisputes, pendingExtractions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_disputes WHERE stage != 'closed'`).Scan(&openDisputes); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_clause_extractions WHERE status = 'pending'`).Scan(&pendingExtractions); err != nil {
		respondInternalError(w, r, err)
		return
	}

	type review struct {
		ID            int        `json:"id"`
		ContractNumber string    `json:"contract_number"`
		SupplierName  string     `json:"supplier_name"`
		ReviewDate    time.Time  `json:"review_date"`
	}
	reviewRows, err := db.QueryContext(ctx, `
		SELECT c.id, c.contract_number, s.company_name, c.review_date
		FROM srm_contracts c JOIN srm_suppliers s ON s.id = c.supplier_id
		WHERE c.review_date IS NOT NULL AND c.status IN ('draft', 'active')
		  AND date(c.review_date) BETWEEN date('now') AND date('now', '+30 days')
		ORDER BY c.review_date LIMIT 20`)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer reviewRows.Close()
	reviews := make([]review, 0)
	for reviewRows.Next() {
		var rv review
		if err := reviewRows.Scan(&rv.ID, &rv.ContractNumber, &rv.SupplierName, &rv.ReviewDate); err != nil {
			respondInternalError(w, r, err)
			return
		}
		reviews = append(reviews, rv)
	}

	supplierTotal := 0
	for _, n := range suppliersByState {
		supplierTotal += n
	}

	respondJSONOK(w, map[string]any{
		"suppliers": map[string]any{
			"total":     supplierTotal,
			"by_state":  suppliersByState,
			"by_tier":   suppliersByTier,
		},
		"contracts": map[string]any{
			"by_status":        contractsByStatus,
			"active_value_nzd": activeValue,
			"upcoming_reviews": reviews,
		},
		"open_disputes":       openDisputes,
		"pending_extractions": pendingExtractions,
	})
}




