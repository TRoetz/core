// SRM planning-phase handlers (Phase 2/3): value propositions, stakeholder
// and engagement planning, communications plans, RACI matrices, the meetings
// engine and joint business plans. The cross-domain helpers used by every SRM
// handler file also live here: srmUserName, srmSupplierExists and
// srmContractSupplier (srmEntityName and srmSupplierSpend are defined in
// srm_segmentation.go).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"windshift/internal/models"
)

// ---------------------------------------------------------------------------
// Shared cross-domain helpers.
// ---------------------------------------------------------------------------

// srmUserName renders "First Last" for a nullable user reference.
func (h *SRMHandler) srmUserName(ctx context.Context, userID *int) string {
	if userID == nil {
		return ""
	}
	var first, last string
	err := h.db.QueryRowContext(ctx, `SELECT first_name, last_name FROM users WHERE id = ?`, *userID).Scan(&first, &last)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(first + " " + last)
}

// srmSupplierExists reports whether a supplier row exists.
func (h *SRMHandler) srmSupplierExists(ctx context.Context, id int) bool {
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_suppliers WHERE id = ?`, id).Scan(&n)
	return err == nil && n > 0
}

// srmContractSupplier returns the owning supplier of a contract
// (sql.ErrNoRows when absent).
func (h *SRMHandler) srmContractSupplier(ctx context.Context, contractID int) (int, error) {
	var supplierID int
	err := h.db.QueryRowContext(ctx, `SELECT supplier_id FROM srm_contracts WHERE id = ?`, contractID).Scan(&supplierID)
	return supplierID, err
}

// ---------------------------------------------------------------------------
// Value propositions (Phase 2 / Step 2.1).
// ---------------------------------------------------------------------------

const srmValuePropColumns = `id, supplier_id, contract_id, agency_objective, supplier_service_product, srm_ambition, benefit_type, anticipated_benefit, estimated_value_nzd, value_confidence, measurement_method, engagement_frequency, p3m_benefit_ref, actual_value_nzd, supplier_strategic_objective, opportunity_statement, srm_specific_ambition, anticipated_supplier_benefits, signed_off_by, signed_off_at, created_at, updated_at`

func (h *SRMHandler) getSRMValueProp(ctx context.Context, id int) (*models.SRMValueProposition, error) {
	var v models.SRMValueProposition
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("vp", srmValuePropColumns)+`, s.company_name AS supplier_name FROM srm_value_propositions vp JOIN srm_suppliers s ON s.id = vp.supplier_id WHERE vp.id = ?`, id).
		Scan(&v.ID, &v.SupplierID, &v.ContractID, &v.AgencyObjective, &v.SupplierServiceProduct, &v.SrmAmbition, &v.BenefitType, &v.AnticipatedBenefit, &v.EstimatedValueNzd, &v.ValueConfidence, &v.MeasurementMethod, &v.EngagementFrequency, &v.P3MBenefitRef, &v.ActualValueNzd, &v.SupplierStrategicObjective, &v.OpportunityStatement, &v.SrmSpecificAmbition, &v.AnticipatedSupplierBenefits, &v.SignedOffBy, &v.SignedOffAt, &v.CreatedAt, &v.UpdatedAt, &v.SupplierName)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func srmValidBenefitType(s string) bool {
	switch s {
	case models.SRMBenefitFinancialSaving, models.SRMBenefitOperationalEfficiency, models.SRMBenefitInnovationCapture, models.SRMBenefitRiskComplianceReduction, models.SRMBenefitOther:
		return true
	default:
		return false
	}
}

func srmValidConfidence(s string) bool {
	switch s {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

// ListValuePropositions returns value propositions, optionally filtered by
// supplier.
func (h *SRMHandler) ListValuePropositions(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := "SELECT " + qualifyColumns("vp", srmValuePropColumns) + ", s.company_name AS supplier_name FROM srm_value_propositions vp JOIN srm_suppliers s ON s.id = vp.supplier_id"
	args := []any{}
	if v := r.URL.Query().Get("supplier_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid supplier_id")
			return
		}
		q += ` WHERE vp.supplier_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY vp.id DESC`

	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMValueProposition{}
	for rows.Next() {
		var v models.SRMValueProposition
		err := rows.Scan(&v.ID, &v.SupplierID, &v.ContractID, &v.AgencyObjective, &v.SupplierServiceProduct, &v.SrmAmbition, &v.BenefitType, &v.AnticipatedBenefit, &v.EstimatedValueNzd, &v.ValueConfidence, &v.MeasurementMethod, &v.EngagementFrequency, &v.P3MBenefitRef, &v.ActualValueNzd, &v.SupplierStrategicObjective, &v.OpportunityStatement, &v.SrmSpecificAmbition, &v.AnticipatedSupplierBenefits, &v.SignedOffBy, &v.SignedOffAt, &v.CreatedAt, &v.UpdatedAt, &v.SupplierName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, v)
	}
	respondJSONOK(w, out)
}

// CreateValueProposition records one value proposition for a supplier. The
// optional sign_off flag stamps the current user and time (gate artefact).
func (h *SRMHandler) CreateValueProposition(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	var in struct {
		SupplierID                   int        `json:"supplier_id"`
		ContractID                   *int       `json:"contract_id,omitempty"`
		AgencyObjective              string     `json:"agency_objective"`
		SupplierServiceProduct       string     `json:"supplier_service_product"`
		SrmAmbition                  string     `json:"srm_ambition"`
		BenefitType                  string     `json:"benefit_type"`
		AnticipatedBenefit           string     `json:"anticipated_benefit"`
		EstimatedValueNzd            *float64   `json:"estimated_value_nzd,omitempty"`
		ValueConfidence              *string    `json:"value_confidence,omitempty"`
		MeasurementMethod            string     `json:"measurement_method"`
		EngagementFrequency          string     `json:"engagement_frequency"`
		P3MBenefitRef                string     `json:"p3m_benefit_ref"`
		ActualValueNzd               *float64   `json:"actual_value_nzd,omitempty"`
		SupplierStrategicObjective   string     `json:"supplier_strategic_objective"`
		OpportunityStatement         string     `json:"opportunity_statement"`
		SrmSpecificAmbition          string     `json:"srm_specific_ambition"`
		AnticipatedSupplierBenefits  string     `json:"anticipated_supplier_benefits"`
		SignOff                      *bool      `json:"sign_off,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.AgencyObjective = strings.TrimSpace(in.AgencyObjective)
	if in.AgencyObjective == "" {
		respondBadRequest(w, r, "agency_objective is required")
		return
	}
	if !srmValidBenefitType(in.BenefitType) {
		respondBadRequest(w, r, "benefit_type must be one of financial_saving, operational_efficiency, innovation_capture, risk_compliance_reduction, other")
		return
	}
	if in.ValueConfidence != nil && !srmValidConfidence(*in.ValueConfidence) {
		respondBadRequest(w, r, "value_confidence must be high, medium or low")
		return
	}
	ctx := r.Context()
	if !h.srmSupplierExists(ctx, in.SupplierID) {
		respondNotFound(w, r, "Supplier not found")
		return
	}

	var signedBy any
	var signedAt any
	if in.SignOff != nil && *in.SignOff {
		now := time.Now().UTC()
		signedBy = user.ID
		signedAt = now
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_value_propositions (supplier_id, contract_id, agency_objective, supplier_service_product, srm_ambition, benefit_type, anticipated_benefit, estimated_value_nzd, value_confidence, measurement_method, engagement_frequency, p3m_benefit_ref, actual_value_nzd, supplier_strategic_objective, opportunity_statement, srm_specific_ambition, anticipated_supplier_benefits, signed_off_by, signed_off_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.SupplierID, nullInt(in.ContractID), in.AgencyObjective, in.SupplierServiceProduct, in.SrmAmbition, in.BenefitType, in.AnticipatedBenefit, nullFloat(in.EstimatedValueNzd), nullStringPtr(in.ValueConfidence), in.MeasurementMethod, in.EngagementFrequency, in.P3MBenefitRef, nullFloat(in.ActualValueNzd), in.SupplierStrategicObjective, in.OpportunityStatement, in.SrmSpecificAmbition, in.AnticipatedSupplierBenefits, signedBy, signedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	vp, err := h.getSRMValueProp(ctx, int(id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, vp)
}

// nullStringPtr renders a nullable string column argument: nil pointer binds
// NULL.
func nullStringPtr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullFloat renders a nullable float column argument: nil pointer binds NULL.
func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// UpdateValueProposition applies a partial update. sign_off=true stamps the
// current user and time; sign_off=false clears both.
func (h *SRMHandler) UpdateValueProposition(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	existing, err := h.getSRMValueProp(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "Value proposition not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		SupplierID                   *int       `json:"supplier_id,omitempty"`
		ContractID                   *int       `json:"contract_id,omitempty"`
		AgencyObjective              *string    `json:"agency_objective,omitempty"`
		SupplierServiceProduct       *string    `json:"supplier_service_product,omitempty"`
		SrmAmbition                  *string    `json:"srm_ambition,omitempty"`
		BenefitType                  *string    `json:"benefit_type,omitempty"`
		AnticipatedBenefit           *string    `json:"anticipated_benefit,omitempty"`
		EstimatedValueNzd            *float64   `json:"estimated_value_nzd,omitempty"`
		ValueConfidence              *string    `json:"value_confidence,omitempty"`
		MeasurementMethod            *string    `json:"measurement_method,omitempty"`
		EngagementFrequency          *string    `json:"engagement_frequency,omitempty"`
		P3MBenefitRef                *string    `json:"p3m_benefit_ref,omitempty"`
		ActualValueNzd               *float64   `json:"actual_value_nzd,omitempty"`
		SupplierStrategicObjective   *string    `json:"supplier_strategic_objective,omitempty"`
		OpportunityStatement         *string    `json:"opportunity_statement,omitempty"`
		SrmSpecificAmbition          *string    `json:"srm_specific_ambition,omitempty"`
		AnticipatedSupplierBenefits  *string    `json:"anticipated_supplier_benefits,omitempty"`
		SignOff                      *bool      `json:"sign_off,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.BenefitType != nil && !srmValidBenefitType(*in.BenefitType) {
		respondBadRequest(w, r, "benefit_type must be one of financial_saving, operational_efficiency, innovation_capture, risk_compliance_reduction, other")
		return
	}
	if in.ValueConfidence != nil && *in.ValueConfidence != "" && !srmValidConfidence(*in.ValueConfidence) {
		respondBadRequest(w, r, "value_confidence must be high, medium or low")
		return
	}

	var signedBy any = nullInt(existing.SignedOffBy)
	var signedAt any = nullTimePtr(existing.SignedOffAt)
	if in.SignOff != nil {
		if *in.SignOff {
			signedBy = user.ID
			signedAt = time.Now().UTC()
		} else {
			signedBy = nil
			signedAt = nil
		}
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_value_propositions SET supplier_id = ?, contract_id = ?, agency_objective = ?, supplier_service_product = ?, srm_ambition = ?, benefit_type = ?, anticipated_benefit = ?, estimated_value_nzd = ?, value_confidence = ?, measurement_method = ?, engagement_frequency = ?, p3m_benefit_ref = ?, actual_value_nzd = ?, supplier_strategic_objective = ?, opportunity_statement = ?, srm_specific_ambition = ?, anticipated_supplier_benefits = ?, signed_off_by = ?, signed_off_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		intOr(in.SupplierID, existing.SupplierID), nullInt(in.ContractID), orKeep(in.AgencyObjective, existing.AgencyObjective), orKeep(in.SupplierServiceProduct, existing.SupplierServiceProduct), orKeep(in.SrmAmbition, existing.SrmAmbition), orKeep(in.BenefitType, existing.BenefitType), orKeep(in.AnticipatedBenefit, existing.AnticipatedBenefit), nullFloat(orPtrF(in.EstimatedValueNzd, existing.EstimatedValueNzd)), nullStringPtr(orPtr(in.ValueConfidence, existing.ValueConfidence)), orKeep(in.MeasurementMethod, existing.MeasurementMethod), orKeep(in.EngagementFrequency, existing.EngagementFrequency), orKeep(in.P3MBenefitRef, existing.P3MBenefitRef), nullFloat(orPtrF(in.ActualValueNzd, existing.ActualValueNzd)), orKeep(in.SupplierStrategicObjective, existing.SupplierStrategicObjective), orKeep(in.OpportunityStatement, existing.OpportunityStatement), orKeep(in.SrmSpecificAmbition, existing.SrmSpecificAmbition), orKeep(in.AnticipatedSupplierBenefits, existing.AnticipatedSupplierBenefits), signedBy, signedAt, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	vp, err := h.getSRMValueProp(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, vp)
}

// orPtrF merges a partial-update float pointer with the stored pointer, so an
// omitted field keeps its previous NULL-ness.
func orPtrF(in, existing *float64) *float64 {
	if in != nil {
		return in
	}
	return existing
}

// orPtr merges a partial-update string pointer with the stored pointer, so an
// omitted field keeps its previous NULL-ness.
func orPtr(in, existing *string) *string {
	if in != nil {
		return in
	}
	return existing
}

// DeleteValueProposition removes one value proposition.
func (h *SRMHandler) DeleteValueProposition(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_value_propositions WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Value proposition not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Stakeholders (Phase 2 / Step 2.2).
// ---------------------------------------------------------------------------

const srmStakeholderColumns = `id, full_name, email, organization, job_title, business_area, is_external, supplier_id, created_at`

func scanSRMStakeholder(row interface{ Scan(dest ...any) error }) (*models.SRMStakeholder, error) {
	var s models.SRMStakeholder
	err := row.Scan(&s.ID, &s.FullName, &s.Email, &s.Organization, &s.JobTitle, &s.BusinessArea, &s.IsExternal, &s.SupplierID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListStakeholders returns the stakeholder directory, optionally filtered by
// supplier.
func (h *SRMHandler) ListStakeholders(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := "SELECT " + qualifyColumns("st", srmStakeholderColumns) + ", COALESCE(s.company_name, '') AS supplier_name FROM srm_stakeholders st LEFT JOIN srm_suppliers s ON s.id = st.supplier_id"
	args := []any{}
	if v := r.URL.Query().Get("supplier_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid supplier_id")
			return
		}
		q += ` WHERE st.supplier_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY st.full_name COLLATE NOCASE`

	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMStakeholder{}
	for rows.Next() {
		var s models.SRMStakeholder
		err := rows.Scan(&s.ID, &s.FullName, &s.Email, &s.Organization, &s.JobTitle, &s.BusinessArea, &s.IsExternal, &s.SupplierID, &s.CreatedAt, &s.SupplierName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, s)
	}
	respondJSONOK(w, out)
}

// CreateStakeholder adds a person to the stakeholder directory. Email is the
// natural key (DB UNIQUE).
func (h *SRMHandler) CreateStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		FullName     string `json:"full_name"`
		Email        string `json:"email"`
		Organization string `json:"organization"`
		JobTitle     string `json:"job_title"`
		BusinessArea string `json:"business_area"`
		IsExternal   bool   `json:"is_external"`
		SupplierID   *int   `json:"supplier_id,omitempty"`
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

	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_stakeholders WHERE email = ?`, in.Email).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "a stakeholder with this email already exists")
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_stakeholders (full_name, email, organization, job_title, business_area, is_external, supplier_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.FullName, in.Email, in.Organization, in.JobTitle, in.BusinessArea, in.IsExternal, nullInt(in.SupplierID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	var s models.SRMStakeholder
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("st", srmStakeholderColumns)+`, COALESCE(s.company_name, '') AS supplier_name FROM srm_stakeholders st LEFT JOIN srm_suppliers s ON s.id = st.supplier_id WHERE st.id = ?`, id).
		Scan(&s.ID, &s.FullName, &s.Email, &s.Organization, &s.JobTitle, &s.BusinessArea, &s.IsExternal, &s.SupplierID, &s.CreatedAt, &s.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, s)
}

// UpdateStakeholder applies a partial update to one stakeholder.
func (h *SRMHandler) UpdateStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMStakeholder
	err := h.db.QueryRowContext(ctx, `SELECT `+srmStakeholderColumns+` FROM srm_stakeholders WHERE id = ?`, id).
		Scan(&existing.ID, &existing.FullName, &existing.Email, &existing.Organization, &existing.JobTitle, &existing.BusinessArea, &existing.IsExternal, &existing.SupplierID, &existing.CreatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		FullName     *string `json:"full_name,omitempty"`
		Email        *string `json:"email,omitempty"`
		Organization *string `json:"organization,omitempty"`
		JobTitle     *string `json:"job_title,omitempty"`
		BusinessArea *string `json:"business_area,omitempty"`
		IsExternal   *bool   `json:"is_external,omitempty"`
		SupplierID   *int    `json:"supplier_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	email := orKeep(in.Email, existing.Email)
	var n int
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_stakeholders WHERE email = ? AND id != ?`, email, id).Scan(&n)
	if err == nil && n > 0 {
		respondConflict(w, r, "a stakeholder with this email already exists")
		return
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_stakeholders SET full_name = ?, email = ?, organization = ?, job_title = ?, business_area = ?, is_external = ?, supplier_id = ? WHERE id = ?`,
		orKeep(in.FullName, existing.FullName), email, orKeep(in.Organization, existing.Organization), orKeep(in.JobTitle, existing.JobTitle), orKeep(in.BusinessArea, existing.BusinessArea), boolOr(in.IsExternal, existing.IsExternal), nullInt(orPtrInt(in.SupplierID, existing.SupplierID)), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var s models.SRMStakeholder
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("st", srmStakeholderColumns)+`, COALESCE(s.company_name, '') AS supplier_name FROM srm_stakeholders st LEFT JOIN srm_suppliers s ON s.id = st.supplier_id WHERE st.id = ?`, id).
		Scan(&s.ID, &s.FullName, &s.Email, &s.Organization, &s.JobTitle, &s.BusinessArea, &s.IsExternal, &s.SupplierID, &s.CreatedAt, &s.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, s)
}

// orPtrInt merges a partial-update int pointer with the stored pointer.
func orPtrInt(in, existing *int) *int {
	if in != nil {
		return in
	}
	return existing
}

// DeleteStakeholder removes one stakeholder (entity bindings cascade).
func (h *SRMHandler) DeleteStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_stakeholders WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Entity stakeholders (polymorphic binding + derived salience fields).
// ---------------------------------------------------------------------------

const srmEntityStakeholderColumns = `id, stakeholder_id, entity_type, entity_id, role_description, power_score, interest_score, support_band, sentiment_score, legitimacy_score, urgency_score, current_engagement, target_engagement, created_at, updated_at`

func scanSRMEntityStakeholder(row interface{ Scan(dest ...any) error }) (*models.SRMEntityStakeholder, error) {
	var e models.SRMEntityStakeholder
	err := row.Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.RoleDescription, &e.PowerScore, &e.InterestScore, &e.SupportBand, &e.SentimentScore, &e.LegitimacyScore, &e.UrgencyScore, &e.CurrentEngagement, &e.TargetEngagement, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	srmApplyStakeholderDerived(&e)
	return &e, nil
}

// srmApplyStakeholderDerived fills the spec §5.2.3a generated columns from
// the stored scores. NULL legitimacy/urgency count as absent (0), matching
// the Postgres generated-column semantics where NULL comparisons are false.
func srmApplyStakeholderDerived(e *models.SRMEntityStakeholder) {
	p := e.PowerScore
	switch {
	case p >= 4:
		e.InfluenceBand = "high"
	case p == 3:
		e.InfluenceBand = "medium"
	default:
		e.InfluenceBand = "low"
	}

	i := e.InterestScore
	switch {
	case p >= 4 && i >= 4:
		e.Quadrant = "manage_closely"
	case p >= 4:
		e.Quadrant = "keep_satisfied"
	case i >= 4:
		e.Quadrant = "keep_informed"
	default:
		e.Quadrant = "monitor"
	}

	l, u := 0, 0
	if e.LegitimacyScore != nil {
		l = *e.LegitimacyScore
	}
	if e.UrgencyScore != nil {
		u = *e.UrgencyScore
	}
	switch {
	case p >= 4 && l >= 4 && u >= 4:
		e.SalienceClass = "definitive"
	case p >= 4 && l >= 4:
		e.SalienceClass = "dominant"
	case p >= 4 && u >= 4:
		e.SalienceClass = "dangerous"
	case l >= 4 && u >= 4:
		e.SalienceClass = "demanding"
	case p >= 4:
		e.SalienceClass = "dependent"
	case l >= 4:
		e.SalienceClass = "discretionary"
	case u >= 4:
		e.SalienceClass = "reactive"
	default:
		e.SalienceClass = "dormant"
	}
}

func srmValidEntityType(s string) bool {
	switch s {
	case "portfolio", "programme", "project", "contract":
		return true
	default:
		return false
	}
}

func srmValidSupportBand(s string) bool {
	switch s {
	case models.SRMSupportSceptic, models.SRMSupportOnTheFence, models.SRMSupportAmbassador:
		return true
	default:
		return false
	}
}

func srmValidEngagement(s string) bool {
	switch s {
	case models.SRMEngagementUnaware, models.SRMEngagementResistant, models.SRMEngagementNeutral, models.SRMEngagementSupportive, models.SRMEngagementChampion:
		return true
	default:
		return false
	}
}

// ListEntityStakeholders returns entity bindings with derived salience
// fields, optionally filtered by entity.
func (h *SRMHandler) ListEntityStakeholders(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := "SELECT " + qualifyColumns("es", srmEntityStakeholderColumns) + ", COALESCE(st.full_name, '') AS stakeholder_name FROM srm_entity_stakeholders es LEFT JOIN srm_stakeholders st ON st.id = es.stakeholder_id"
	args := []any{}
	if v := r.URL.Query().Get("entity_type"); v != "" {
		q += ` WHERE es.entity_type = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("entity_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid entity_id")
			return
		}
		if len(args) == 0 {
			q += ` WHERE`
		} else {
			q += ` AND`
		}
		q += ` es.entity_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY es.id DESC`

	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMEntityStakeholder{}
	for rows.Next() {
		var e models.SRMEntityStakeholder
		err := rows.Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.RoleDescription, &e.PowerScore, &e.InterestScore, &e.SupportBand, &e.SentimentScore, &e.LegitimacyScore, &e.UrgencyScore, &e.CurrentEngagement, &e.TargetEngagement, &e.CreatedAt, &e.UpdatedAt, &e.StakeholderName)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		srmApplyStakeholderDerived(&e)
		out = append(out, e)
	}
	respondJSONOK(w, out)
}

// CreateEntityStakeholder binds a stakeholder to an entity with influence and
// salience scores.
func (h *SRMHandler) CreateEntityStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		StakeholderID     int    `json:"stakeholder_id"`
		EntityType        string `json:"entity_type"`
		EntityID          int    `json:"entity_id"`
		RoleDescription   string `json:"role_description"`
		PowerScore        int    `json:"power_score"`
		InterestScore     int    `json:"interest_score"`
		SupportBand       string `json:"support_band"`
		SentimentScore    int    `json:"sentiment_score"`
		LegitimacyScore   *int   `json:"legitimacy_score,omitempty"`
		UrgencyScore      *int   `json:"urgency_score,omitempty"`
		CurrentEngagement string `json:"current_engagement"`
		TargetEngagement  string `json:"target_engagement"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !srmValidEntityType(in.EntityType) {
		respondBadRequest(w, r, "entity_type must be one of portfolio, programme, project, contract")
		return
	}
	if in.PowerScore < 1 || in.PowerScore > 5 || in.InterestScore < 1 || in.InterestScore > 5 {
		respondBadRequest(w, r, "power_score and interest_score must be between 1 and 5")
		return
	}
	if in.SentimentScore < -2 || in.SentimentScore > 2 {
		respondBadRequest(w, r, "sentiment_score must be between -2 and +2")
		return
	}
	if (in.LegitimacyScore != nil && (*in.LegitimacyScore < 1 || *in.LegitimacyScore > 5)) || (in.UrgencyScore != nil && (*in.UrgencyScore < 1 || *in.UrgencyScore > 5)) {
		respondBadRequest(w, r, "legitimacy_score and urgency_score must be between 1 and 5")
		return
	}
	if in.SupportBand == "" {
		in.SupportBand = models.SRMSupportOnTheFence
	} else if !srmValidSupportBand(in.SupportBand) {
		respondBadRequest(w, r, "support_band must be one of sceptic, on_the_fence, ambassador")
		return
	}
	if in.CurrentEngagement == "" {
		in.CurrentEngagement = models.SRMEngagementNeutral
	} else if !srmValidEngagement(in.CurrentEngagement) {
		respondBadRequest(w, r, "current_engagement must be one of unaware, resistant, neutral, supportive, champion")
		return
	}
	if in.TargetEngagement == "" {
		in.TargetEngagement = models.SRMEngagementSupportive
	} else if !srmValidEngagement(in.TargetEngagement) {
		respondBadRequest(w, r, "target_engagement must be one of unaware, resistant, neutral, supportive, champion")
		return
	}

	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_stakeholders WHERE id = ?`, in.StakeholderID).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_entity_stakeholders (stakeholder_id, entity_type, entity_id, role_description, power_score, interest_score, support_band, sentiment_score, legitimacy_score, urgency_score, current_engagement, target_engagement)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.StakeholderID, in.EntityType, in.EntityID, in.RoleDescription, in.PowerScore, in.InterestScore, in.SupportBand, in.SentimentScore, nullInt(in.LegitimacyScore), nullInt(in.UrgencyScore), in.CurrentEngagement, in.TargetEngagement)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id, _ := res.LastInsertId()

	var e models.SRMEntityStakeholder
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("es", srmEntityStakeholderColumns)+`, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_entity_stakeholders es LEFT JOIN srm_stakeholders st ON st.id = es.stakeholder_id WHERE es.id = ?`, id).
		Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.RoleDescription, &e.PowerScore, &e.InterestScore, &e.SupportBand, &e.SentimentScore, &e.LegitimacyScore, &e.UrgencyScore, &e.CurrentEngagement, &e.TargetEngagement, &e.CreatedAt, &e.UpdatedAt, &e.StakeholderName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	srmApplyStakeholderDerived(&e)
	respondJSONCreated(w, e)
}

// UpdateEntityStakeholder applies a partial update to one entity binding.
func (h *SRMHandler) UpdateEntityStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMEntityStakeholder
	err := h.db.QueryRowContext(ctx, `SELECT `+srmEntityStakeholderColumns+` FROM srm_entity_stakeholders WHERE id = ?`, id).
		Scan(&existing.ID, &existing.StakeholderID, &existing.EntityType, &existing.EntityID, &existing.RoleDescription, &existing.PowerScore, &existing.InterestScore, &existing.SupportBand, &existing.SentimentScore, &existing.LegitimacyScore, &existing.UrgencyScore, &existing.CurrentEngagement, &existing.TargetEngagement, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Entity stakeholder not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		StakeholderID     *int    `json:"stakeholder_id,omitempty"`
		EntityType        *string `json:"entity_type,omitempty"`
		EntityID          *int    `json:"entity_id,omitempty"`
		RoleDescription   *string `json:"role_description,omitempty"`
		PowerScore        *int    `json:"power_score,omitempty"`
		InterestScore     *int    `json:"interest_score,omitempty"`
		SupportBand       *string `json:"support_band,omitempty"`
		SentimentScore    *int    `json:"sentiment_score,omitempty"`
		LegitimacyScore   *int    `json:"legitimacy_score,omitempty"`
		UrgencyScore      *int    `json:"urgency_score,omitempty"`
		CurrentEngagement *string `json:"current_engagement,omitempty"`
		TargetEngagement  *string `json:"target_engagement,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	power := existing.PowerScore
	if in.PowerScore != nil {
		power = *in.PowerScore
	}
	interest := existing.InterestScore
	if in.InterestScore != nil {
		interest = *in.InterestScore
	}
	sentiment := existing.SentimentScore
	if in.SentimentScore != nil {
		sentiment = *in.SentimentScore
	}
	if power < 1 || power > 5 || interest < 1 || interest > 5 {
		respondBadRequest(w, r, "power_score and interest_score must be between 1 and 5")
		return
	}
	if sentiment < -2 || sentiment > 2 {
		respondBadRequest(w, r, "sentiment_score must be between -2 and +2")
		return
	}
	if in.SupportBand != nil && !srmValidSupportBand(*in.SupportBand) {
		respondBadRequest(w, r, "support_band must be one of sceptic, on_the_fence, ambassador")
		return
	}
	if in.CurrentEngagement != nil && !srmValidEngagement(*in.CurrentEngagement) {
		respondBadRequest(w, r, "current_engagement must be one of unaware, resistant, neutral, supportive, champion")
		return
	}
	if in.TargetEngagement != nil && !srmValidEngagement(*in.TargetEngagement) {
		respondBadRequest(w, r, "target_engagement must be one of unaware, resistant, neutral, supportive, champion")
		return
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_entity_stakeholders SET stakeholder_id = ?, entity_type = ?, entity_id = ?, role_description = ?, power_score = ?, interest_score = ?, support_band = ?, sentiment_score = ?, legitimacy_score = ?, urgency_score = ?, current_engagement = ?, target_engagement = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		intOr(in.StakeholderID, existing.StakeholderID), orKeep(in.EntityType, existing.EntityType), intOr(in.EntityID, existing.EntityID), orKeep(in.RoleDescription, existing.RoleDescription), power, interest, orKeep(in.SupportBand, existing.SupportBand), sentiment, nullInt(orPtrInt(in.LegitimacyScore, existing.LegitimacyScore)), nullInt(orPtrInt(in.UrgencyScore, existing.UrgencyScore)), orKeep(in.CurrentEngagement, existing.CurrentEngagement), orKeep(in.TargetEngagement, existing.TargetEngagement), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var e models.SRMEntityStakeholder
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("es", srmEntityStakeholderColumns)+`, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_entity_stakeholders es LEFT JOIN srm_stakeholders st ON st.id = es.stakeholder_id WHERE es.id = ?`, id).
		Scan(&e.ID, &e.StakeholderID, &e.EntityType, &e.EntityID, &e.RoleDescription, &e.PowerScore, &e.InterestScore, &e.SupportBand, &e.SentimentScore, &e.LegitimacyScore, &e.UrgencyScore, &e.CurrentEngagement, &e.TargetEngagement, &e.CreatedAt, &e.UpdatedAt, &e.StakeholderName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	srmApplyStakeholderDerived(&e)
	respondJSONOK(w, e)
}

// DeleteEntityStakeholder removes one entity binding (snapshots cascade).
func (h *SRMHandler) DeleteEntityStakeholder(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_entity_stakeholders WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Entity stakeholder not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Communications plans & messages (Phase 2 / Step 2.4).
// ---------------------------------------------------------------------------

const srmCommsPlanColumns = `id, supplier_id, scope, is_complete, created_at, updated_at`

func scanSRMCommsPlan(row interface{ Scan(dest ...any) error }) (*models.SRMCommunicationsPlan, error) {
	var p models.SRMCommunicationsPlan
	err := row.Scan(&p.ID, &p.SupplierID, &p.Scope, &p.IsComplete, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetCommsPlan returns the supplier's communications plan with its messages;
// 404 when no plan exists yet (UpsertCommsPlan creates it).
func (h *SRMHandler) GetCommsPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var p models.SRMCommunicationsPlan
	err := h.db.QueryRowContext(ctx, `SELECT `+srmCommsPlanColumns+` FROM srm_communications_plans WHERE supplier_id = ?`, id).
		Scan(&p.ID, &p.SupplierID, &p.Scope, &p.IsComplete, &p.CreatedAt, &p.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Communications plan not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT m.id, m.plan_id, m.stakeholder_id, m.communication_message, m.method, m.frequency, m.communicator_id, m.signed_off, m.signed_off_by, m.signed_off_at, m.created_at, COALESCE(st.full_name, '') AS stakeholder_name
		FROM srm_comms_messages m LEFT JOIN srm_stakeholders st ON st.id = m.stakeholder_id
		WHERE m.plan_id = ? ORDER BY m.id`, p.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	msgs := []models.SRMCommsMessage{}
	for rows.Next() {
		var m models.SRMCommsMessage
		if err := rows.Scan(&m.ID, &m.PlanID, &m.StakeholderID, &m.CommunicationMessage, &m.Method, &m.Frequency, &m.CommunicatorID, &m.SignedOff, &m.SignedOffBy, &m.SignedOffAt, &m.CreatedAt, &m.StakeholderName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		msgs = append(msgs, m)
	}
	p.Messages = msgs
	respondJSONOK(w, p)
}

// UpsertCommsPlan creates or updates the supplier's communications plan.
func (h *SRMHandler) UpsertCommsPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Scope      string `json:"scope"`
		IsComplete *bool  `json:"is_complete,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	if !h.srmSupplierExists(ctx, id) {
		respondNotFound(w, r, "Supplier not found")
		return
	}

	var existing models.SRMCommunicationsPlan
	err := h.db.QueryRowContext(ctx, `SELECT `+srmCommsPlanColumns+` FROM srm_communications_plans WHERE supplier_id = ?`, id).
		Scan(&existing.ID, &existing.SupplierID, &existing.Scope, &existing.IsComplete, &existing.CreatedAt, &existing.UpdatedAt)
	if err != nil && !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	scope := orKeep(&in.Scope, existing.Scope)
	if scope == "" {
		scope = "all"
	}
	switch scope {
	case "all", "internal", "external":
	default:
		respondBadRequest(w, r, "scope must be one of all, internal, external")
		return
	}
	complete := boolOr(in.IsComplete, existing.IsComplete)

	var planID int
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_communications_plans (supplier_id, scope, is_complete) VALUES (?, ?, ?)`, id, scope, complete)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		n, _ := res.LastInsertId()
		planID = int(n)
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_communications_plans SET scope = ?, is_complete = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, scope, complete, existing.ID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		planID = existing.ID
	}

	var p models.SRMCommunicationsPlan
	err = h.db.QueryRowContext(ctx, `SELECT `+srmCommsPlanColumns+` FROM srm_communications_plans WHERE id = ?`, planID).
		Scan(&p.ID, &p.SupplierID, &p.Scope, &p.IsComplete, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, p)
}

// ListCommsMessages returns one plan's messages with stakeholder names.
func (h *SRMHandler) ListCommsMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var n int
	err := h.db.QueryRowContext(r.Context(), `SELECT 1 FROM srm_communications_plans WHERE id = ?`, id).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Communications plan not found")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT m.id, m.plan_id, m.stakeholder_id, m.communication_message, m.method, m.frequency, m.communicator_id, m.signed_off, m.signed_off_by, m.signed_off_at, m.created_at, COALESCE(st.full_name, '') AS stakeholder_name
		FROM srm_comms_messages m LEFT JOIN srm_stakeholders st ON st.id = m.stakeholder_id
		WHERE m.plan_id = ? ORDER BY m.id`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMCommsMessage{}
	for rows.Next() {
		var m models.SRMCommsMessage
		if err := rows.Scan(&m.ID, &m.PlanID, &m.StakeholderID, &m.CommunicationMessage, &m.Method, &m.Frequency, &m.CommunicatorID, &m.SignedOff, &m.SignedOffBy, &m.SignedOffAt, &m.CreatedAt, &m.StakeholderName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, m)
	}
	respondJSONOK(w, out)
}

// AddCommsMessage adds one planned message to a communications plan.
func (h *SRMHandler) AddCommsMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StakeholderID        int    `json:"stakeholder_id"`
		CommunicationMessage string `json:"communication_message"`
		Method               string `json:"method"`
		Frequency            string `json:"frequency"`
		CommunicatorID       *int   `json:"communicator_id,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.CommunicationMessage = strings.TrimSpace(in.CommunicationMessage)
	if in.CommunicationMessage == "" {
		respondBadRequest(w, r, "communication_message is required")
		return
	}
	ctx := r.Context()

	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_communications_plans WHERE id = ?`, id).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Communications plan not found")
		return
	}
	err = h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_stakeholders WHERE id = ?`, in.StakeholderID).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Stakeholder not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_comms_messages (plan_id, stakeholder_id, communication_message, method, frequency, communicator_id)
		VALUES (?, ?, ?, ?, ?, ?)`, id, in.StakeholderID, in.CommunicationMessage, in.Method, in.Frequency, nullInt(in.CommunicatorID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	mid, _ := res.LastInsertId()

	var m models.SRMCommsMessage
	err = h.db.QueryRowContext(ctx, `SELECT m.id, m.plan_id, m.stakeholder_id, m.communication_message, m.method, m.frequency, m.communicator_id, m.signed_off, m.signed_off_by, m.signed_off_at, m.created_at, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_comms_messages m LEFT JOIN srm_stakeholders st ON st.id = m.stakeholder_id WHERE m.id = ?`, mid).
		Scan(&m.ID, &m.PlanID, &m.StakeholderID, &m.CommunicationMessage, &m.Method, &m.Frequency, &m.CommunicatorID, &m.SignedOff, &m.SignedOffBy, &m.SignedOffAt, &m.CreatedAt, &m.StakeholderName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, m)
}

// UpdateCommsMessage applies a partial update; sign_off=true stamps the
// current user and time.
func (h *SRMHandler) UpdateCommsMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMCommsMessage
	err := h.db.QueryRowContext(ctx, `SELECT id, plan_id, stakeholder_id, communication_message, method, frequency, communicator_id, signed_off, signed_off_by, signed_off_at, created_at FROM srm_comms_messages WHERE id = ?`, id).
		Scan(&existing.ID, &existing.PlanID, &existing.StakeholderID, &existing.CommunicationMessage, &existing.Method, &existing.Frequency, &existing.CommunicatorID, &existing.SignedOff, &existing.SignedOffBy, &existing.SignedOffAt, &existing.CreatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Comms message not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		StakeholderID        *int    `json:"stakeholder_id,omitempty"`
		CommunicationMessage *string `json:"communication_message,omitempty"`
		Method               *string `json:"method,omitempty"`
		Frequency            *string `json:"frequency,omitempty"`
		CommunicatorID       *int    `json:"communicator_id,omitempty"`
		SignOff              *bool   `json:"sign_off,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	signedBy := nullInt(existing.SignedOffBy)
	signedAt := nullTimePtr(existing.SignedOffAt)
	if in.SignOff != nil && *in.SignOff {
		signedBy = user.ID
		signedAt = time.Now().UTC()
	} else if in.SignOff != nil && !*in.SignOff {
		signedBy = nil
		signedAt = nil
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_comms_messages SET stakeholder_id = ?, communication_message = ?, method = ?, frequency = ?, communicator_id = ?, signed_off = ?, signed_off_by = ?, signed_off_at = ? WHERE id = ?`,
		intOr(in.StakeholderID, existing.StakeholderID), orKeep(in.CommunicationMessage, existing.CommunicationMessage), orKeep(in.Method, existing.Method), orKeep(in.Frequency, existing.Frequency), nullInt(orPtrInt(in.CommunicatorID, existing.CommunicatorID)), boolOr(in.SignOff, existing.SignedOff), signedBy, signedAt, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var m models.SRMCommsMessage
	err = h.db.QueryRowContext(ctx, `SELECT m.id, m.plan_id, m.stakeholder_id, m.communication_message, m.method, m.frequency, m.communicator_id, m.signed_off, m.signed_off_by, m.signed_off_at, m.created_at, COALESCE(st.full_name, '') AS stakeholder_name FROM srm_comms_messages m LEFT JOIN srm_stakeholders st ON st.id = m.stakeholder_id WHERE m.id = ?`, id).
		Scan(&m.ID, &m.PlanID, &m.StakeholderID, &m.CommunicationMessage, &m.Method, &m.Frequency, &m.CommunicatorID, &m.SignedOff, &m.SignedOffBy, &m.SignedOffAt, &m.CreatedAt, &m.StakeholderName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, m)
}

// DeleteCommsMessage removes one planned message.
func (h *SRMHandler) DeleteCommsMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_comms_messages WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Comms message not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// RACI engine (Phase 2 / Step 2.3). One assignment row per (activity_code,
// side); an Accountable value must resolve to a person (spec CHECK).
// ---------------------------------------------------------------------------

const srmRaciColumns = `id, supplier_id, version, status, created_by, agreed_at, last_reviewed_at, created_at`

func scanSRMRaci(row interface{ Scan(dest ...any) error }) (*models.SRMRaci, error) {
	var m models.SRMRaci
	err := row.Scan(&m.ID, &m.SupplierID, &m.Version, &m.Status, &m.CreatedBy, &m.AgreedAt, &m.LastReviewedAt, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func srmValidRaciValue(v string) bool {
	switch v {
	case models.SRMRaciValueR, models.SRMRaciValueA, models.SRMRaciValueC, models.SRMRaciValueI:
		return true
	default:
		return false
	}
}

func srmValidSide(s string) bool {
	switch s {
	case models.SRMSideAgency, models.SRMSideSupplier:
		return true
	default:
		return false
	}
}

// srmRaciAssignments loads one matrix's assignments with user names.
func (h *SRMHandler) srmRaciAssignments(ctx context.Context, raciID int) ([]models.SRMRaciAssignment, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, raci_id, activity_code, side, role_name, user_id, value FROM srm_raci_assignments WHERE raci_id = ? ORDER BY activity_code, side`, raciID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SRMRaciAssignment{}
	for rows.Next() {
		var a models.SRMRaciAssignment
		if err := rows.Scan(&a.ID, &a.RaciID, &a.ActivityCode, &a.Side, &a.RoleName, &a.UserID, &a.Value); err != nil {
			return nil, err
		}
		a.UserName = h.srmUserName(ctx, a.UserID)
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetRaciMatrix returns the supplier's latest RACI version with assignments;
// 404 when no matrix exists yet.
func (h *SRMHandler) GetRaciMatrix(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var m models.SRMRaci
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("rc", srmRaciColumns)+`, s.company_name AS supplier_name FROM srm_racis rc JOIN srm_suppliers s ON s.id = rc.supplier_id WHERE rc.supplier_id = ? ORDER BY rc.version DESC LIMIT 1`, id).
		Scan(&m.ID, &m.SupplierID, &m.Version, &m.Status, &m.CreatedBy, &m.AgreedAt, &m.LastReviewedAt, &m.CreatedAt, &m.SupplierName)
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	m.Assignments, err = h.srmRaciAssignments(ctx, m.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, m)
}

// CreateRaciVersion starts a new RACI version for the supplier (version =
// max+1). The input assignments must not repeat an (activity_code, side) pair
// and every Accountable must name a user.
func (h *SRMHandler) CreateRaciVersion(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Status      string `json:"status"`
		Assignments []struct {
			ActivityCode string `json:"activity_code"`
			Side         string `json:"side"`
			RoleName     string `json:"role_name"`
			UserID       *int   `json:"user_id,omitempty"`
			Value        string `json:"value"`
		} `json:"assignments"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()
	if !h.srmSupplierExists(ctx, id) {
		respondNotFound(w, r, "Supplier not found")
		return
	}

	status := in.Status
	if status == "" {
		status = models.SRMRaciStatusDraft
	}
	switch status {
	case models.SRMRaciStatusDraft, models.SRMRaciStatusPreliminary, models.SRMRaciStatusUnderDiscussion:
	default:
		respondBadRequest(w, r, "new RACI versions start as draft, preliminary or under_discussion")
		return
	}

	seen := map[string]bool{}
	for _, a := range in.Assignments {
		a.ActivityCode = strings.TrimSpace(a.ActivityCode)
		if a.ActivityCode == "" || !srmValidSide(a.Side) || !srmValidRaciValue(a.Value) {
			respondBadRequest(w, r, "each assignment needs activity_code, side (agency|supplier) and value (r|a|c|i)")
			return
		}
		key := a.ActivityCode + "|" + a.Side
		if seen[key] {
			respondBadRequest(w, r, "duplicate assignment for activity "+a.ActivityCode+" on the "+a.Side+" side")
			return
		}
		seen[key] = true
		if a.Value == models.SRMRaciValueA && a.UserID == nil {
			respondBadRequest(w, r, "an Accountable must resolve to a user (user_id)")
			return
		}
	}

	var version int
	err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM srm_racis WHERE supplier_id = ?`, id).Scan(&version)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	version++

	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_racis (supplier_id, version, status, created_by) VALUES (?, ?, ?, ?)`, id, version, status, user.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	raciID, _ := res.LastInsertId()

	for _, a := range in.Assignments {
		if _, err := h.db.ExecContext(ctx, `INSERT INTO srm_raci_assignments (raci_id, activity_code, side, role_name, user_id, value) VALUES (?, ?, ?, ?, ?, ?)`, raciID, a.ActivityCode, a.Side, a.RoleName, nullInt(a.UserID), a.Value); err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	var m models.SRMRaci
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("rc", srmRaciColumns)+`, s.company_name AS supplier_name FROM srm_racis rc JOIN srm_suppliers s ON s.id = rc.supplier_id WHERE rc.id = ?`, raciID).
		Scan(&m.ID, &m.SupplierID, &m.Version, &m.Status, &m.CreatedBy, &m.AgreedAt, &m.LastReviewedAt, &m.CreatedAt, &m.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Assignments, err = h.srmRaciAssignments(ctx, m.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, m)
}

// UpdateRaciStatus moves a matrix through its lifecycle. Entering "agreed"
// stamps agreed_at and last_reviewed_at (gate artefact for implementation).
func (h *SRMHandler) UpdateRaciStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
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
	case models.SRMRaciStatusDraft, models.SRMRaciStatusPreliminary, models.SRMRaciStatusUnderDiscussion, models.SRMRaciStatusAgreed:
	default:
		respondBadRequest(w, r, "status must be one of draft, preliminary, under_discussion, agreed")
		return
	}

	ctx := r.Context()
	var existing models.SRMRaci
	err := h.db.QueryRowContext(ctx, `SELECT `+srmRaciColumns+` FROM srm_racis WHERE id = ?`, id).
		Scan(&existing.ID, &existing.SupplierID, &existing.Version, &existing.Status, &existing.CreatedBy, &existing.AgreedAt, &existing.LastReviewedAt, &existing.CreatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var agreedAt any = nullTimePtr(existing.AgreedAt)
	var reviewedAt any = nullTimePtr(existing.LastReviewedAt)
	if in.Status == models.SRMRaciStatusAgreed {
		now := time.Now().UTC()
		agreedAt = now
		reviewedAt = now
	}

	_, err = h.db.ExecContext(ctx, `UPDATE srm_racis SET status = ?, agreed_at = ?, last_reviewed_at = ? WHERE id = ?`, in.Status, agreedAt, reviewedAt, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var m models.SRMRaci
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("rc", srmRaciColumns)+`, s.company_name AS supplier_name FROM srm_racis rc JOIN srm_suppliers s ON s.id = rc.supplier_id WHERE rc.id = ?`, id).
		Scan(&m.ID, &m.SupplierID, &m.Version, &m.Status, &m.CreatedBy, &m.AgreedAt, &m.LastReviewedAt, &m.CreatedAt, &m.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Assignments, err = h.srmRaciAssignments(ctx, m.ID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, m)
}

// UpsertRaciAssignment sets one (activity_code, side) cell of a matrix. An
// Accountable value must resolve to a user.
func (h *SRMHandler) UpsertRaciAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ActivityCode string `json:"activity_code"`
		Side         string `json:"side"`
		RoleName     string `json:"role_name"`
		UserID       *int   `json:"user_id,omitempty"`
		Value        string `json:"value"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.ActivityCode = strings.TrimSpace(in.ActivityCode)
	if in.ActivityCode == "" || !srmValidSide(in.Side) || !srmValidRaciValue(in.Value) {
		respondBadRequest(w, r, "activity_code, side (agency|supplier) and value (r|a|c|i) are required")
		return
	}
	if in.Value == models.SRMRaciValueA && in.UserID == nil {
		respondBadRequest(w, r, "an Accountable must resolve to a user (user_id)")
		return
	}

	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_racis WHERE id = ?`, id).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "RACI matrix not found")
		return
	}

	var existingID int
	err = h.db.QueryRowContext(ctx, `SELECT id FROM srm_raci_assignments WHERE raci_id = ? AND activity_code = ? AND side = ?`, id, in.ActivityCode, in.Side).Scan(&existingID)
	if err != nil && !isNotFound(err) {
		respondInternalError(w, r, err)
		return
	}

	var a models.SRMRaciAssignment
	if isNotFound(err) {
		res, err := h.db.ExecContext(ctx, `INSERT INTO srm_raci_assignments (raci_id, activity_code, side, role_name, user_id, value) VALUES (?, ?, ?, ?, ?, ?)`, id, in.ActivityCode, in.Side, in.RoleName, nullInt(in.UserID), in.Value)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		n, _ := res.LastInsertId()
		a.ID = int(n)
	} else {
		if _, err := h.db.ExecContext(ctx, `UPDATE srm_raci_assignments SET role_name = ?, user_id = ?, value = ? WHERE id = ?`, in.RoleName, nullInt(in.UserID), in.Value, existingID); err != nil {
			respondInternalError(w, r, err)
			return
		}
		a.ID = existingID
	}

	err = h.db.QueryRowContext(ctx, `SELECT id, raci_id, activity_code, side, role_name, user_id, value FROM srm_raci_assignments WHERE id = ?`, a.ID).
		Scan(&a.ID, &a.RaciID, &a.ActivityCode, &a.Side, &a.RoleName, &a.UserID, &a.Value)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	a.UserName = h.srmUserName(ctx, a.UserID)
	respondJSONOK(w, a)
}

// DeleteRaciAssignment removes one cell from a matrix.
func (h *SRMHandler) DeleteRaciAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_raci_assignments WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "RACI assignment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Meetings engine (Phase 3). Agenda and outcomes are typed JSON payloads in
// TEXT columns.
// ---------------------------------------------------------------------------

const srmMeetingColumns = `id, meeting_type, supplier_id, contract_id, organizer_id, scheduled_at, location, agenda_json, outcomes_json, minutes_text, status, created_at`

func scanSRMMeeting(row interface{ Scan(dest ...any) error }) (*models.SRMMeeting, error) {
	var m models.SRMMeeting
	var agendaJSON, outcomesJSON string
	err := row.Scan(&m.ID, &m.MeetingType, &m.SupplierID, &m.ContractID, &m.OrganizerID, &m.ScheduledAt, &m.Location, &agendaJSON, &outcomesJSON, &m.MinutesText, &m.Status, &m.CreatedAt, &m.SupplierName)
	if err != nil {
		return nil, err
	}
	if err := srmDecodeMeetingJSON(&m, agendaJSON, outcomesJSON); err != nil {
		return nil, err
	}
	return &m, nil
}

// srmDecodeMeetingJSON fills Agenda/Outcomes from the raw JSON columns.
func srmDecodeMeetingJSON(m *models.SRMMeeting, agendaJSON, outcomesJSON string) error {
	m.Agenda = []models.SRMAgendaItem{}
	m.Outcomes = []models.SRMOutcome{}
	if agendaJSON != "" {
		if err := json.Unmarshal([]byte(agendaJSON), &m.Agenda); err != nil {
			return err
		}
	}
	if outcomesJSON != "" {
		if err := json.Unmarshal([]byte(outcomesJSON), &m.Outcomes); err != nil {
			return err
		}
	}
	return nil
}

func srmValidMeetingType(s string) bool {
	switch s {
	case "internal_kickoff", "supplier_kickoff", "jbp_workshop", "operational_performance", "relationship_review", "strategic_relationship", "qbr", "other":
		return true
	default:
		return false
	}
}

// srmMeetingAttendees loads attendees grouped by meeting with resolved names.
func (h *SRMHandler) srmMeetingAttendees(ctx context.Context, meetingIDs []int) (map[int][]models.SRMMeetingAttendee, error) {
	out := map[int][]models.SRMMeetingAttendee{}
	if len(meetingIDs) == 0 {
		return out, nil
	}
	ph := joinPlaceholders(make([]string, len(meetingIDs)))
	args := make([]any, len(meetingIDs))
	for i, id := range meetingIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, meeting_id, stakeholder_id, user_id, side, role_description, attended FROM srm_meeting_attendees WHERE meeting_id IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var a models.SRMMeetingAttendee
		if err := rows.Scan(&a.ID, &a.MeetingID, &a.StakeholderID, &a.UserID, &a.Side, &a.RoleDescription, &a.Attended); err != nil {
			return nil, err
		}
		switch {
		case a.UserID != nil:
			a.Name = h.srmUserName(ctx, a.UserID)
		case a.StakeholderID != nil:
			var name string
			if err := h.db.QueryRowContext(ctx, `SELECT full_name FROM srm_stakeholders WHERE id = ?`, *a.StakeholderID).Scan(&name); err == nil {
				a.Name = name
			}
		}
		out[a.MeetingID] = append(out[a.MeetingID], a)
	}
	return out, rows.Err()
}

// srmMeetingActions loads actions grouped by meeting with owner names.
func (h *SRMHandler) srmMeetingActions(ctx context.Context, meetingIDs []int) (map[int][]models.SRMMeetingAction, error) {
	out := map[int][]models.SRMMeetingAction{}
	if len(meetingIDs) == 0 {
		return out, nil
	}
	ph := joinPlaceholders(make([]string, len(meetingIDs)))
	args := make([]any, len(meetingIDs))
	for i, id := range meetingIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, meeting_id, agenda_item_no, kind, text, owner_id, due_date, status FROM srm_meeting_actions WHERE meeting_id IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var a models.SRMMeetingAction
		if err := rows.Scan(&a.ID, &a.MeetingID, &a.AgendaItemNo, &a.Kind, &a.Text, &a.OwnerID, &a.DueDate, &a.Status); err != nil {
			return nil, err
		}
		a.UserName = h.srmUserName(ctx, a.OwnerID)
		out[a.MeetingID] = append(out[a.MeetingID], a)
	}
	return out, rows.Err()
}

// ListMeetings returns meetings (newest first), optionally filtered by
// supplier.
func (h *SRMHandler) ListMeetings(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := "SELECT " + qualifyColumns("mt", srmMeetingColumns) + ", s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id"
	args := []any{}
	if v := r.URL.Query().Get("supplier_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid supplier_id")
			return
		}
		q += ` WHERE mt.supplier_id = ?`
		args = append(args, id)
	}
	q += ` ORDER BY mt.scheduled_at DESC, mt.id DESC`

	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMMeeting{}
	var ids []int
	for rows.Next() {
		var m models.SRMMeeting
		var agendaJSON, outcomesJSON string
		if err := rows.Scan(&m.ID, &m.MeetingType, &m.SupplierID, &m.ContractID, &m.OrganizerID, &m.ScheduledAt, &m.Location, &agendaJSON, &outcomesJSON, &m.MinutesText, &m.Status, &m.CreatedAt, &m.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if err := srmDecodeMeetingJSON(&m, agendaJSON, outcomesJSON); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}

	attendees, err := h.srmMeetingAttendees(ctx, ids)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	actions, err := h.srmMeetingActions(ctx, ids)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range out {
		out[i].Attendees = attendees[out[i].ID]
		out[i].Actions = actions[out[i].ID]
	}
	respondJSONOK(w, out)
}

// CreateMeeting schedules a meeting. A contract reference must belong to the
// same supplier.
func (h *SRMHandler) CreateMeeting(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	var in struct {
		MeetingType  string                `json:"meeting_type"`
		SupplierID   int                   `json:"supplier_id"`
		ContractID   *int                  `json:"contract_id,omitempty"`
		OrganizerID  *int                  `json:"organizer_id,omitempty"`
		ScheduledAt  time.Time             `json:"scheduled_at"`
		Location     string                `json:"location"`
		Agenda       []models.SRMAgendaItem `json:"agenda"`
		MinutesText  string                `json:"minutes_text"`
		Status       string                `json:"status"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !srmValidMeetingType(in.MeetingType) {
		respondBadRequest(w, r, "meeting_type must be one of internal_kickoff, supplier_kickoff, jbp_workshop, operational_performance, relationship_review, strategic_relationship, qbr, other")
		return
	}
	if in.ScheduledAt.IsZero() {
		respondBadRequest(w, r, "scheduled_at is required")
		return
	}
	if in.Status == "" {
		in.Status = "scheduled"
	}
	switch in.Status {
	case "scheduled", "held", "cancelled":
	default:
		respondBadRequest(w, r, "status must be one of scheduled, held, cancelled")
		return
	}

	ctx := r.Context()
	if !h.srmSupplierExists(ctx, in.SupplierID) {
		respondNotFound(w, r, "Supplier not found")
		return
	}
	if in.ContractID != nil {
		supplierID, err := h.srmContractSupplier(ctx, *in.ContractID)
		if isNotFound(err) {
			respondNotFound(w, r, "Contract not found")
			return
		}
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
		if supplierID != in.SupplierID {
			respondBadRequest(w, r, "contract does not belong to this supplier")
			return
		}
	}

	agendaJSON := []byte(`[]`)
	if len(in.Agenda) > 0 {
		b, err := json.Marshal(in.Agenda)
		if err != nil {
			respondBadRequest(w, r, "invalid agenda")
			return
		}
		agendaJSON = b
	}

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_meetings (meeting_type, supplier_id, contract_id, organizer_id, scheduled_at, location, agenda_json, minutes_text, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.MeetingType, in.SupplierID, nullInt(in.ContractID), nullInt(in.OrganizerID), in.ScheduledAt, in.Location, string(agendaJSON), in.MinutesText, in.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	id64, _ := res.LastInsertId()

	var m models.SRMMeeting
	var storedAgendaJSON, storedOutcomesJSON string
	err = h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("mt", srmMeetingColumns)+`, s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id WHERE mt.id = ?`, id64).
		Scan(&m.ID, &m.MeetingType, &m.SupplierID, &m.ContractID, &m.OrganizerID, &m.ScheduledAt, &m.Location, &storedAgendaJSON, &storedOutcomesJSON, &m.MinutesText, &m.Status, &m.CreatedAt, &m.SupplierName)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := srmDecodeMeetingJSON(&m, storedAgendaJSON, storedOutcomesJSON); err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, m)
}

// GetMeeting returns one meeting with attendees and actions.
func (h *SRMHandler) GetMeeting(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	m, err := scanSRMMeeting(h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("mt", srmMeetingColumns)+`, s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id WHERE mt.id = ?`, id))
	if isNotFound(err) {
		respondNotFound(w, r, "Meeting not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	attendees, err := h.srmMeetingAttendees(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Attendees = attendees[id]
	actions, err := h.srmMeetingActions(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Actions = actions[id]
	respondJSONOK(w, m)
}

// UpdateMeeting applies a partial update; agenda and outcomes replace the
// stored payloads when provided.
func (h *SRMHandler) UpdateMeeting(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMMeeting
	var existingAgendaJSON, existingOutcomesJSON string
	err := h.db.QueryRowContext(ctx, `SELECT `+srmMeetingColumns+` FROM srm_meetings WHERE id = ?`, id).
		Scan(&existing.ID, &existing.MeetingType, &existing.SupplierID, &existing.ContractID, &existing.OrganizerID, &existing.ScheduledAt, &existing.Location, &existingAgendaJSON, &existingOutcomesJSON, &existing.MinutesText, &existing.Status, &existing.CreatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "Meeting not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		MeetingType  *string               `json:"meeting_type,omitempty"`
		SupplierID   *int                  `json:"supplier_id,omitempty"`
		ContractID   *int                  `json:"contract_id,omitempty"`
		OrganizerID  *int                  `json:"organizer_id,omitempty"`
		ScheduledAt  *time.Time            `json:"scheduled_at,omitempty"`
		Location     *string               `json:"location,omitempty"`
		Agenda       []models.SRMAgendaItem `json:"agenda,omitempty"`
		Outcomes     []models.SRMOutcome   `json:"outcomes,omitempty"`
		MinutesText  *string               `json:"minutes_text,omitempty"`
		Status       *string               `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.MeetingType != nil && !srmValidMeetingType(*in.MeetingType) {
		respondBadRequest(w, r, "meeting_type must be one of internal_kickoff, supplier_kickoff, jbp_workshop, operational_performance, relationship_review, strategic_relationship, qbr, other")
		return
	}
	if in.Status != nil && *in.Status != "scheduled" && *in.Status != "held" && *in.Status != "cancelled" {
		respondBadRequest(w, r, "status must be one of scheduled, held, cancelled")
		return
	}

	agendaJSON := existingAgendaJSON
	if in.Agenda != nil {
		b, err := json.Marshal(in.Agenda)
		if err != nil {
			respondBadRequest(w, r, "invalid agenda")
			return
		}
		agendaJSON = string(b)
	}
	outcomesJSON := existingOutcomesJSON
	if in.Outcomes != nil {
		b, err := json.Marshal(in.Outcomes)
		if err != nil {
			respondBadRequest(w, r, "invalid outcomes")
			return
		}
		outcomesJSON = string(b)
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_meetings SET meeting_type = ?, supplier_id = ?, contract_id = ?, organizer_id = ?, scheduled_at = ?, location = ?, agenda_json = ?, outcomes_json = ?, minutes_text = ?, status = ? WHERE id = ?`,
		orKeep(in.MeetingType, existing.MeetingType), intOr(in.SupplierID, existing.SupplierID), nullInt(orPtrInt(in.ContractID, existing.ContractID)), nullInt(orPtrInt(in.OrganizerID, existing.OrganizerID)), timeOr(in.ScheduledAt, existing.ScheduledAt), orKeep(in.Location, existing.Location), agendaJSON, outcomesJSON, orKeep(in.MinutesText, existing.MinutesText), orKeep(in.Status, existing.Status), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	m, err := scanSRMMeeting(h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("mt", srmMeetingColumns)+`, s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id WHERE mt.id = ?`, id))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	attendees, err := h.srmMeetingAttendees(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Attendees = attendees[id]
	actions, err := h.srmMeetingActions(ctx, []int{id})
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	m.Actions = actions[id]
	respondJSONOK(w, m)
}

// timeOr merges a partial-update time pointer with the stored value.
func timeOr(in *time.Time, existing time.Time) any {
	if in == nil {
		return existing
	}
	return *in
}

// DeleteMeeting removes one meeting (attendees and actions cascade).
func (h *SRMHandler) DeleteMeeting(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_meetings WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Meeting not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddMeetingAttendee records one attendee (internal user or stakeholder).
func (h *SRMHandler) AddMeetingAttendee(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		StakeholderID   *int   `json:"stakeholder_id,omitempty"`
		UserID          *int   `json:"user_id,omitempty"`
		Side            string `json:"side"`
		RoleDescription string `json:"role_description"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if !srmValidSide(in.Side) {
		respondBadRequest(w, r, "side must be agency or supplier")
		return
	}
	if in.StakeholderID == nil && in.UserID == nil {
		respondBadRequest(w, r, "either stakeholder_id or user_id is required")
		return
	}

	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_meetings WHERE id = ?`, id).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Meeting not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_meeting_attendees (meeting_id, stakeholder_id, user_id, side, role_description) VALUES (?, ?, ?, ?, ?)`, id, nullInt(in.StakeholderID), nullInt(in.UserID), in.Side, in.RoleDescription)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	aid, _ := res.LastInsertId()

	var a models.SRMMeetingAttendee
	err = h.db.QueryRowContext(ctx, `SELECT id, meeting_id, stakeholder_id, user_id, side, role_description, attended FROM srm_meeting_attendees WHERE id = ?`, aid).
		Scan(&a.ID, &a.MeetingID, &a.StakeholderID, &a.UserID, &a.Side, &a.RoleDescription, &a.Attended)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	switch {
	case a.UserID != nil:
		a.Name = h.srmUserName(ctx, a.UserID)
	case a.StakeholderID != nil:
		var name string
		if err := h.db.QueryRowContext(ctx, `SELECT full_name FROM srm_stakeholders WHERE id = ?`, *a.StakeholderID).Scan(&name); err == nil {
			a.Name = name
		}
	}
	respondJSONCreated(w, a)
}

// RemoveMeetingAttendee removes one attendee.
func (h *SRMHandler) RemoveMeetingAttendee(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_meeting_attendees WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "Meeting attendee not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddMeetingAction records a note, question or action from the meeting.
func (h *SRMHandler) AddMeetingAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		AgendaItemNo *int        `json:"agenda_item_no,omitempty"`
		Kind         string      `json:"kind"`
		Text         string      `json:"text"`
		OwnerID      *int        `json:"owner_id,omitempty"`
		DueDate      *time.Time  `json:"due_date,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	switch in.Kind {
	case "note", "question", "action":
	default:
		respondBadRequest(w, r, "kind must be one of note, question, action")
		return
	}
	if in.Text == "" {
		respondBadRequest(w, r, "text is required")
		return
	}

	ctx := r.Context()
	var n int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM srm_meetings WHERE id = ?`, id).Scan(&n)
	if err != nil || n == 0 {
		respondNotFound(w, r, "Meeting not found")
		return
	}

	res, err := h.db.ExecContext(ctx, `INSERT INTO srm_meeting_actions (meeting_id, agenda_item_no, kind, text, owner_id, due_date) VALUES (?, ?, ?, ?, ?, ?)`, id, nullInt(in.AgendaItemNo), in.Kind, in.Text, nullInt(in.OwnerID), nullTimePtr(in.DueDate))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	aid, _ := res.LastInsertId()

	var a models.SRMMeetingAction
	err = h.db.QueryRowContext(ctx, `SELECT id, meeting_id, agenda_item_no, kind, text, owner_id, due_date, status FROM srm_meeting_actions WHERE id = ?`, aid).
		Scan(&a.ID, &a.MeetingID, &a.AgendaItemNo, &a.Kind, &a.Text, &a.OwnerID, &a.DueDate, &a.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	a.UserName = h.srmUserName(ctx, a.OwnerID)
	respondJSONCreated(w, a)
}

// UpdateMeetingAction applies a partial update (typically closing an action).
func (h *SRMHandler) UpdateMeetingAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMMeetingAction
	err := h.db.QueryRowContext(ctx, `SELECT id, meeting_id, agenda_item_no, kind, text, owner_id, due_date, status FROM srm_meeting_actions WHERE id = ?`, id).
		Scan(&existing.ID, &existing.MeetingID, &existing.AgendaItemNo, &existing.Kind, &existing.Text, &existing.OwnerID, &existing.DueDate, &existing.Status)
	if isNotFound(err) {
		respondNotFound(w, r, "Meeting action not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		AgendaItemNo *int       `json:"agenda_item_no,omitempty"`
		Kind         *string    `json:"kind,omitempty"`
		Text         *string    `json:"text,omitempty"`
		OwnerID      *int       `json:"owner_id,omitempty"`
		DueDate      *time.Time `json:"due_date,omitempty"`
		Status       *string    `json:"status,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Kind != nil && *in.Kind != "note" && *in.Kind != "question" && *in.Kind != "action" {
		respondBadRequest(w, r, "kind must be one of note, question, action")
		return
	}
	if in.Status != nil && *in.Status != "open" && *in.Status != "closed" {
		respondBadRequest(w, r, "status must be open or closed")
		return
	}

	_, err = h.db.ExecContext(ctx, `UPDATE srm_meeting_actions SET agenda_item_no = ?, kind = ?, text = ?, owner_id = ?, due_date = ?, status = ? WHERE id = ?`,
		nullInt(orPtrInt(in.AgendaItemNo, existing.AgendaItemNo)), orKeep(in.Kind, existing.Kind), orKeep(in.Text, existing.Text), nullInt(orPtrInt(in.OwnerID, existing.OwnerID)), nullTimePtr(orPtrTime(in.DueDate, existing.DueDate)), orKeep(in.Status, existing.Status), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var a models.SRMMeetingAction
	err = h.db.QueryRowContext(ctx, `SELECT id, meeting_id, agenda_item_no, kind, text, owner_id, due_date, status FROM srm_meeting_actions WHERE id = ?`, id).
		Scan(&a.ID, &a.MeetingID, &a.AgendaItemNo, &a.Kind, &a.Text, &a.OwnerID, &a.DueDate, &a.Status)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	a.UserName = h.srmUserName(ctx, a.OwnerID)
	respondJSONOK(w, a)
}

// orPtrTime merges a partial-update time pointer with the stored pointer.
func orPtrTime(in, existing *time.Time) *time.Time {
	if in != nil {
		return in
	}
	return existing
}

// ---------------------------------------------------------------------------
// Joint business plans (Phase 3 / Steps 3.2-3.3). Versioned per contract; the
// plan becomes "signed" only once both parties have signed (gate to
// monitoring_active).
// ---------------------------------------------------------------------------

const srmJbpColumns = `id, contract_id, supplier_id, version, status, purpose, vision, scope, governance_model, review_cycle_months, relationship_charter, contract_summary, spend_summary, performance_notes, risk_action_plan, relationship_development, value_opportunities, agency_rep_name, agency_rep_role, agency_signed_at, supplier_rep_name, supplier_rep_role, supplier_signed_at, created_at, updated_at`

func scanSRMJbp(row interface{ Scan(dest ...any) error }) (*models.SRMJBP, error) {
	var j models.SRMJBP
	var charterJSON, opportunitiesJSON string
	err := row.Scan(&j.ID, &j.ContractID, &j.SupplierID, &j.Version, &j.Status, &j.Purpose, &j.Vision, &j.Scope, &j.GovernanceModel, &j.ReviewCycleMonths, &charterJSON, &j.ContractSummary, &j.SpendSummary, &j.PerformanceNotes, &j.RiskActionPlan, &j.RelationshipDevelopment, &opportunitiesJSON, &j.AgencyRepName, &j.AgencyRepRole, &j.AgencySignedAt, &j.SupplierRepName, &j.SupplierRepRole, &j.SupplierSignedAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := srmDecodeJbpJSON(&j, charterJSON, opportunitiesJSON); err != nil {
		return nil, err
	}
	return &j, nil
}

// srmDecodeJbpJSON fills RelationshipCharter/ValueOpportunities from the raw
// JSON columns.
func srmDecodeJbpJSON(j *models.SRMJBP, charterJSON, opportunitiesJSON string) error {
	j.RelationshipCharter = []string{}
	j.ValueOpportunities = []models.SRMValueOpportunity{}
	if charterJSON != "" {
		if err := json.Unmarshal([]byte(charterJSON), &j.RelationshipCharter); err != nil {
			return err
		}
	}
	if opportunitiesJSON != "" {
		if err := json.Unmarshal([]byte(opportunitiesJSON), &j.ValueOpportunities); err != nil {
			return err
		}
	}
	return nil
}

func srmValidJBPStatus(s string) bool {
	switch s {
	case models.SRMJBPStatusDraft, models.SRMJBPStatusInReview, models.SRMJBPStatusSigned:
		return true
	default:
		return false
	}
}

// srmJbpInitiatives loads initiatives grouped by JBP with owner names.
func (h *SRMHandler) srmJbpInitiatives(ctx context.Context, jbpIDs []int) (map[int][]models.SRMJBPInitiative, error) {
	out := map[int][]models.SRMJBPInitiative{}
	if len(jbpIDs) == 0 {
		return out, nil
	}
	ph := joinPlaceholders(make([]string, len(jbpIDs)))
	args := make([]any, len(jbpIDs))
	for i, id := range jbpIDs {
		args[i] = id
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, jbp_id, quarter_label, part, initiative_name, activities, key_dates, owner_id, comment, status, created_at FROM srm_jbp_initiatives WHERE jbp_id IN (`+ph+`) ORDER BY quarter_label, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var i models.SRMJBPInitiative
		var keyDatesJSON string
		if err := rows.Scan(&i.ID, &i.JbpID, &i.QuarterLabel, &i.Part, &i.InitiativeName, &i.Activities, &keyDatesJSON, &i.OwnerID, &i.Comment, &i.Status, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.KeyDates = []string{}
		if keyDatesJSON != "" {
			if err := json.Unmarshal([]byte(keyDatesJSON), &i.KeyDates); err != nil {
				return nil, err
			}
		}
		i.UserName = h.srmUserName(ctx, i.OwnerID)
		out[i.JbpID] = append(out[i.JbpID], i)
	}
	return out, rows.Err()
}

// ListJbps returns JBP versions (newest first), optionally filtered by
// supplier or contract.
func (h *SRMHandler) ListJbps(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	ctx := r.Context()
	q := "SELECT " + qualifyColumns("j", srmJbpColumns) + ", s.company_name AS supplier_name FROM srm_jbps j JOIN srm_suppliers s ON s.id = j.supplier_id"
	args := []any{}
	where := false
	if v := r.URL.Query().Get("supplier_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid supplier_id")
			return
		}
		q += ` WHERE j.supplier_id = ?`
		args = append(args, id)
		where = true
	}
	if v := r.URL.Query().Get("contract_id"); v != "" {
		id, okq := parseQueryInt(v)
		if !okq {
			respondBadRequest(w, r, "invalid contract_id")
			return
		}
		if where {
			q += ` AND j.contract_id = ?`
		} else {
			q += ` WHERE j.contract_id = ?`
		}
		args = append(args, id)
		where = true
	}
	q += ` ORDER BY j.id DESC`

	rows, err := h.db.QueryContext(ctx, q, args...)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	defer rows.Close()

	out := []models.SRMJBP{}
	var ids []int
	for rows.Next() {
		var j models.SRMJBP
		var charterJSON, opportunitiesJSON string
		if err := rows.Scan(&j.ID, &j.ContractID, &j.SupplierID, &j.Version, &j.Status, &j.Purpose, &j.Vision, &j.Scope, &j.GovernanceModel, &j.ReviewCycleMonths, &charterJSON, &j.ContractSummary, &j.SpendSummary, &j.PerformanceNotes, &j.RiskActionPlan, &j.RelationshipDevelopment, &opportunitiesJSON, &j.AgencyRepName, &j.AgencyRepRole, &j.AgencySignedAt, &j.SupplierRepName, &j.SupplierRepRole, &j.SupplierSignedAt, &j.CreatedAt, &j.UpdatedAt, &j.SupplierName); err != nil {
			respondInternalError(w, r, err)
			return
		}
		if err := srmDecodeJbpJSON(&j, charterJSON, opportunitiesJSON); err != nil {
			respondInternalError(w, r, err)
			return
		}
		out = append(out, j)
		ids = append(ids, j.ID)
	}

	initiatives, err := h.srmJbpInitiatives(ctx, ids)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for i := range out {
		out[i].Initiatives = initiatives[out[i].ID]
	}
	respondJSONOK(w, out)
}

// CreateJbp starts a new JBP version for the supplier's contract (version =
// max+1). A previously signed version is superseded.
func (h *SRMHandler) CreateJbp(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ContractID              int                       `json:"contract_id"`
		Status                  string                    `json:"status"`
		Purpose                 string                    `json:"purpose"`
		Vision                  string                    `json:"vision"`
		Scope                   string                    `json:"scope"`
		GovernanceModel         string                    `json:"governance_model"`
		ReviewCycleMonths       int                       `json:"review_cycle_months"`
		RelationshipCharter     []string                  `json:"relationship_charter"`
		ContractSummary         string                    `json:"contract_summary"`
		SpendSummary            *float64                  `json:"spend_summary,omitempty"`
		PerformanceNotes        string                    `json:"performance_notes"`
		RiskActionPlan          string                    `json:"risk_action_plan"`
		RelationshipDevelopment string                    `json:"relationship_development"`
		ValueOpportunities      []models.SRMValueOpportunity `json:"value_opportunities"`
		AgencyRepName           string                    `json:"agency_rep_name"`
		AgencyRepRole           string                    `json:"agency_rep_role"`
		SupplierRepName         string                    `json:"supplier_rep_name"`
		SupplierRepRole         string                    `json:"supplier_rep_role"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	ctx := r.Context()

	supplierID, err := h.srmContractSupplier(ctx, in.ContractID)
	if isNotFound(err) {
		respondNotFound(w, r, "Contract not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if supplierID != id {
		respondBadRequest(w, r, "contract does not belong to this supplier")
		return
	}

	status := in.Status
	if status == "" {
		status = models.SRMJBPStatusDraft
	}
	if !srmValidJBPStatus(status) {
		respondBadRequest(w, r, "status must be one of draft, in_review, signed")
		return
	}
	cycle := in.ReviewCycleMonths
	if cycle <= 0 {
		cycle = 3
	}

	charterJSON, err := json.Marshal(orSlice(in.RelationshipCharter))
	if err != nil {
		respondBadRequest(w, r, "invalid relationship_charter")
		return
	}
	opportunitiesJSON, err := json.Marshal(orSliceOpportunities(in.ValueOpportunities))
	if err != nil {
		respondBadRequest(w, r, "invalid value_opportunities")
		return
	}

	var version int
	err = h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM srm_jbps WHERE contract_id = ?`, in.ContractID).Scan(&version)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	version++

	res, err := h.db.ExecContext(ctx, `
		INSERT INTO srm_jbps (contract_id, supplier_id, version, status, purpose, vision, scope, governance_model, review_cycle_months, relationship_charter, contract_summary, spend_summary, performance_notes, risk_action_plan, relationship_development, value_opportunities, agency_rep_name, agency_rep_role, supplier_rep_name, supplier_rep_role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ContractID, supplierID, version, status, in.Purpose, in.Vision, in.Scope, in.GovernanceModel, cycle, string(charterJSON), in.ContractSummary, nullFloat(in.SpendSummary), in.PerformanceNotes, in.RiskActionPlan, in.RelationshipDevelopment, string(opportunitiesJSON), in.AgencyRepName, in.AgencyRepRole, in.SupplierRepName, in.SupplierRepRole)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	jbpID, _ := res.LastInsertId()

	// A new version supersedes any previously signed one.
	if _, err := h.db.ExecContext(ctx, `UPDATE srm_jbps SET status = ? WHERE contract_id = ? AND id != ? AND status = ?`, models.SRMJBPStatusSuperseded, in.ContractID, jbpID, models.SRMJBPStatusSigned); err != nil {
		respondInternalError(w, r, err)
		return
	}

	j, err := h.getSRMJbp(ctx, int(jbpID))
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONCreated(w, j)
}

// orSlice renders a nullable string slice as JSON-ready non-nil.
func orSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// orSliceOpportunities renders a nullable opportunity slice as non-nil.
func orSliceOpportunities(v []models.SRMValueOpportunity) []models.SRMValueOpportunity {
	if v == nil {
		return []models.SRMValueOpportunity{}
	}
	return v
}

// getSRMJbp loads one JBP with supplier name and initiatives.
func (h *SRMHandler) getSRMJbp(ctx context.Context, id int) (*models.SRMJBP, error) {
	var j models.SRMJBP
	var charterJSON, opportunitiesJSON string
	err := h.db.QueryRowContext(ctx, "SELECT "+qualifyColumns("j", srmJbpColumns)+`, s.company_name AS supplier_name FROM srm_jbps j JOIN srm_suppliers s ON s.id = j.supplier_id WHERE j.id = ?`, id).
		Scan(&j.ID, &j.ContractID, &j.SupplierID, &j.Version, &j.Status, &j.Purpose, &j.Vision, &j.Scope, &j.GovernanceModel, &j.ReviewCycleMonths, &charterJSON, &j.ContractSummary, &j.SpendSummary, &j.PerformanceNotes, &j.RiskActionPlan, &j.RelationshipDevelopment, &opportunitiesJSON, &j.AgencyRepName, &j.AgencyRepRole, &j.AgencySignedAt, &j.SupplierRepName, &j.SupplierRepRole, &j.SupplierSignedAt, &j.CreatedAt, &j.UpdatedAt, &j.SupplierName)
	if err != nil {
		return nil, err
	}
	if err := srmDecodeJbpJSON(&j, charterJSON, opportunitiesJSON); err != nil {
		return nil, err
	}
	initiatives, err := h.srmJbpInitiatives(ctx, []int{id})
	if err != nil {
		return nil, err
	}
	j.Initiatives = initiatives[id]
	return &j, nil
}

// GetJbp returns one JBP version with its initiatives.
func (h *SRMHandler) GetJbp(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	j, err := h.getSRMJbp(r.Context(), id)
	if isNotFound(err) {
		respondNotFound(w, r, "JBP not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, j)
}

// UpdateJbp applies a partial update. Once both parties have signed, the
// status becomes "signed" server-side (gate to monitoring_active).
func (h *SRMHandler) UpdateJbp(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	var existing models.SRMJBP
	var existingCharterJSON, existingOpportunitiesJSON string
	err := h.db.QueryRowContext(ctx, `SELECT `+srmJbpColumns+` FROM srm_jbps WHERE id = ?`, id).
		Scan(&existing.ID, &existing.ContractID, &existing.SupplierID, &existing.Version, &existing.Status, &existing.Purpose, &existing.Vision, &existing.Scope, &existing.GovernanceModel, &existing.ReviewCycleMonths, &existingCharterJSON, &existing.ContractSummary, &existing.SpendSummary, &existing.PerformanceNotes, &existing.RiskActionPlan, &existing.RelationshipDevelopment, &existingOpportunitiesJSON, &existing.AgencyRepName, &existing.AgencyRepRole, &existing.AgencySignedAt, &existing.SupplierRepName, &existing.SupplierRepRole, &existing.SupplierSignedAt, &existing.CreatedAt, &existing.UpdatedAt)
	if isNotFound(err) {
		respondNotFound(w, r, "JBP not found")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var in struct {
		Status                  *string                      `json:"status,omitempty"`
		Purpose                 *string                      `json:"purpose,omitempty"`
		Vision                  *string                      `json:"vision,omitempty"`
		Scope                   *string                      `json:"scope,omitempty"`
		GovernanceModel         *string                      `json:"governance_model,omitempty"`
		ReviewCycleMonths       *int                         `json:"review_cycle_months,omitempty"`
		RelationshipCharter     []string                     `json:"relationship_charter,omitempty"`
		ContractSummary         *string                      `json:"contract_summary,omitempty"`
		SpendSummary            *float64                     `json:"spend_summary,omitempty"`
		PerformanceNotes        *string                      `json:"performance_notes,omitempty"`
		RiskActionPlan          *string                      `json:"risk_action_plan,omitempty"`
		RelationshipDevelopment *string                      `json:"relationship_development,omitempty"`
		ValueOpportunities      []models.SRMValueOpportunity `json:"value_opportunities,omitempty"`
		AgencyRepName           *string                      `json:"agency_rep_name,omitempty"`
		AgencyRepRole           *string                      `json:"agency_rep_role,omitempty"`
		AgencySignedAt          *time.Time                   `json:"agency_signed_at,omitempty"`
		SupplierRepName         *string                      `json:"supplier_rep_name,omitempty"`
		SupplierRepRole         *string                      `json:"supplier_rep_role,omitempty"`
		SupplierSignedAt        *time.Time                   `json:"supplier_signed_at,omitempty"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil {
		respondBadRequest(w, r, "Invalid request body")
		return
	}
	if in.Status != nil && !srmValidJBPStatus(*in.Status) {
		respondBadRequest(w, r, "status must be one of draft, in_review, signed")
		return
	}

	status := orKeep(in.Status, existing.Status)
	agencySignedAt := orPtrTime(in.AgencySignedAt, existing.AgencySignedAt)
	supplierSignedAt := orPtrTime(in.SupplierSignedAt, existing.SupplierSignedAt)
	if in.AgencySignedAt != nil || in.SupplierSignedAt != nil {
		if agencySignedAt != nil && supplierSignedAt != nil {
			status = models.SRMJBPStatusSigned
		}
	}

	charterJSON := existingCharterJSON
	if in.RelationshipCharter != nil {
		b, err := json.Marshal(in.RelationshipCharter)
		if err != nil {
			respondBadRequest(w, r, "invalid relationship_charter")
			return
		}
		charterJSON = string(b)
	}
	opportunitiesJSON := existingOpportunitiesJSON
	if in.ValueOpportunities != nil {
		b, err := json.Marshal(in.ValueOpportunities)
		if err != nil {
			respondBadRequest(w, r, "invalid value_opportunities")
			return
		}
		opportunitiesJSON = string(b)
	}

	_, err = h.db.ExecContext(ctx, `
		UPDATE srm_jbps SET status = ?, purpose = ?, vision = ?, scope = ?, governance_model = ?, review_cycle_months = ?, relationship_charter = ?, contract_summary = ?, spend_summary = ?, performance_notes = ?, risk_action_plan = ?, relationship_development = ?, value_opportunities = ?, agency_rep_name = ?, agency_rep_role = ?, agency_signed_at = ?, supplier_rep_name = ?, supplier_rep_role = ?, supplier_signed_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		status, orKeep(in.Purpose, existing.Purpose), orKeep(in.Vision, existing.Vision), orKeep(in.Scope, existing.Scope), orKeep(in.GovernanceModel, existing.GovernanceModel), intOr(in.ReviewCycleMonths, existing.ReviewCycleMonths), charterJSON, orKeep(in.ContractSummary, existing.ContractSummary), nullFloat(orPtrF(in.SpendSummary, existing.SpendSummary)), orKeep(in.PerformanceNotes, existing.PerformanceNotes), orKeep(in.RiskActionPlan, existing.RiskActionPlan), orKeep(in.RelationshipDevelopment, existing.RelationshipDevelopment), opportunitiesJSON, orKeep(in.AgencyRepName, existing.AgencyRepName), orKeep(in.AgencyRepRole, existing.AgencyRepRole), nullTimePtr(agencySignedAt), orKeep(in.SupplierRepName, existing.SupplierRepName), orKeep(in.SupplierRepRole, existing.SupplierRepRole), nullTimePtr(supplierSignedAt), id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	j, err := h.getSRMJbp(ctx, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, j)
}

// DeleteJbp removes one JBP version (initiatives cascade).
func (h *SRMHandler) DeleteJbp(w http.ResponseWriter, r *http.Request) {
	if _, ok := RequireAuth(w, r); !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(r.Context(), `DELETE FROM srm_jbps WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondNotFound(w, r, "JBP not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
