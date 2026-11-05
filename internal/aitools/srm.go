package aitools

import (
	"context"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/database"
	"windshift/internal/models"
)

// This file adds MCP/AI tools for the SRM (Supplier Relationship Management)
// module: the supplier directory with its phase-gate state machine, the
// contract register, and categories. Access mirrors the HTTP surface: the
// global "srm.view" permission gates every tool.

const (
	aiSRMSupplierColumns  = `id, company_name, business_id, tax_id_number, website, tier, relationship_state, is_active, created_at, updated_at`
	aiSRMContractColumns  = `id, supplier_id, p3m_entity_type, p3m_entity_id, contract_number, title, category, classification, scope_summary, document_url, status, total_value, currency, committed_spend, actual_spend, spend_source, start_date, end_date, extension_count, notice_period_days, review_date, cost_code, po_number, termination_reason, created_at, updated_at`
	aiSRMMetricColumns    = `id, contract_id, name, type, unit, target_value, direction, frequency, data_source, owner_id, is_active, created_at`
	aiSRMScorecardColumns = `id, contract_id, supplier_id, period_start, period_end, completed_collaboratively, overall_score, overall_reasoning, agency_completed_by, supplier_completed_by_name, status, created_at`
	aiSRMDisputeColumns   = `id, contract_id, supplier_id, description, stage, opened_at, resolved_at, outcome_note`
	aiSRMMeetingColumns   = `id, meeting_type, supplier_id, contract_id, organizer_id, scheduled_at, location, agenda_json, outcomes_json, minutes_text, status, created_at`
	aiSRMJbpColumns       = `id, contract_id, supplier_id, version, status, purpose, vision, scope, governance_model, review_cycle_months, relationship_charter, contract_summary, spend_summary, performance_notes, risk_action_plan, relationship_development, value_opportunities, agency_rep_name, agency_rep_role, agency_signed_at, supplier_rep_name, supplier_rep_role, supplier_signed_at, created_at, updated_at`
	aiSRMValuePropColumns = `id, supplier_id, contract_id, agency_objective, supplier_service_product, srm_ambition, benefit_type, anticipated_benefit, estimated_value_nzd, value_confidence, measurement_method, engagement_frequency, p3m_benefit_ref, actual_value_nzd, supplier_strategic_objective, opportunity_statement, srm_specific_ambition, anticipated_supplier_benefits, signed_off_by, signed_off_at, created_at, updated_at`
)

func aiRequireSRMView(env *Env) (bool, error) {
	ok, err := env.PermService.HasGlobalPermission(env.UserID, models.PermissionSRMView)
	return ok, err
}

func aiScanSRMSupplier(row interface{ Scan(...any) error }) (models.SRMSupplier, error) {
	var s models.SRMSupplier
	err := row.Scan(&s.ID, &s.CompanyName, &s.BusinessID, &s.TaxIDNumber, &s.Website, &s.Tier, &s.RelationshipState, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func aiGetSRMSupplier(db database.Database, id int) (models.SRMSupplier, error) {
	return aiScanSRMSupplier(db.QueryRow(`SELECT `+aiSRMSupplierColumns+` FROM srm_suppliers WHERE id = ?`, id))
}

func aiSRMSupplierContacts(db database.Database) (map[int][]models.SRMSupplierContact, error) {
	rows, err := db.Query(`SELECT id, supplier_id, full_name, role_title, email, phone, is_primary, created_at FROM srm_supplier_contacts ORDER BY is_primary DESC, full_name`)
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
	return out, rows.Err()
}

func aiSRMSupplierCounts(db database.Database) (map[int]map[string]int, error) {
	out := map[int]map[string]int{}
	rows, err := db.Query(`
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
		if kind == "dispute" {
			m["disputes"]++
		} else {
			m["contracts"]++
		}
	}
	return out, rows.Err()
}

// --- Phase-gate state machine (mirrors handlers.srmGateStatus) ---

var aiSRMStateOrder = []string{
	models.SRMStateRegistered,
	models.SRMStateSegmented,
	models.SRMStatePlanningReady,
	models.SRMStateImplementationReady,
	models.SRMStateKickedOff,
	models.SRMStateJBPFormalised,
	models.SRMStateMonitoringActive,
}

var aiSRMGateArtifacts = map[string][]string{
	models.SRMStateSegmented:           {"segmentation run complete"},
	models.SRMStatePlanningReady:       {"validation sign-off approved"},
	models.SRMStateImplementationReady: {"signed value proposition", "complete communications plan", "preliminary RACI matrix"},
	models.SRMStateKickedOff:           {"kick-off meeting held with outcomes recorded"},
	models.SRMStateJBPFormalised:       {"joint business plan signed by both parties"},
	models.SRMStateMonitoringActive:    {"completed collaborative scorecard"},
}

func aiSRMArtifactChecks() map[string]func(ctx context.Context, db database.Database, supplierID int) bool {
	return map[string]func(ctx context.Context, db database.Database, supplierID int) bool{
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
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_validation_requests WHERE supplier_id = ? AND status = ?`, id, models.SRMValidationApproved).Scan(&n)
			return err == nil && n > 0
		},
		"signed value proposition": func(ctx context.Context, db database.Database, id int) bool {
			var n int
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_value_propositions WHERE supplier_id = ? AND signed_off_at IS NOT NULL`, id).Scan(&n)
			return err == nil && n > 0
		},
		"complete communications plan": func(ctx context.Context, db database.Database, id int) bool {
			var n int
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_communications_plans WHERE supplier_id = ? AND is_complete = true`, id).Scan(&n)
			return err == nil && n > 0
		},
		"preliminary RACI matrix": func(ctx context.Context, db database.Database, id int) bool {
			var n int
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_racis WHERE supplier_id = ? AND status IN (?, ?, ?)`,
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
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_jbps WHERE supplier_id = ? AND status = ?`, id, models.SRMJBPStatusSigned).Scan(&n)
			return err == nil && n > 0
		},
		"completed collaborative scorecard": func(ctx context.Context, db database.Database, id int) bool {
			var n int
			err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_scorecards WHERE supplier_id = ? AND status = 'closed' AND completed_collaboratively = true`, id).Scan(&n)
			return err == nil && n > 0
		},
	}
}

func aiSRMStateIndex(state string) int {
	for i, s := range aiSRMStateOrder {
		if s == state {
			return i
		}
	}
	return -1
}

func aiSRMGateStatus(ctx context.Context, db database.Database, supplierID int, current string) (string, map[string]bool, []string) {
	artifacts := map[string]bool{}
	idx := aiSRMStateIndex(current)
	if idx < 0 || idx == len(aiSRMStateOrder)-1 {
		return "", artifacts, nil
	}
	nextState := aiSRMStateOrder[idx+1]
	checks := aiSRMArtifactChecks()
	var missing []string
	for _, name := range aiSRMGateArtifacts[nextState] {
		check, ok := checks[name]
		if !ok {
			continue
		}
		passed := check(ctx, db, supplierID)
		artifacts[name] = passed
		if !passed {
			missing = append(missing, name)
		}
	}
	return nextState, artifacts, missing
}

// --- validation helpers ---

func aiSRMValidTier(t string) bool {
	switch t {
	case models.SRMTier1, models.SRMTier2, models.SRMTier3, models.SRMTier4:
		return true
	default:
		return false
	}
}

func aiSRMContractSupplier(db database.Database, contractID int) (int, error) {
	var supplierID int
	err := db.QueryRow(`SELECT supplier_id FROM srm_contracts WHERE id = ?`, contractID).Scan(&supplierID)
	return supplierID, err
}

func aiSRMValidDisputeStage(s string) bool {
	switch s {
	case "l1_working_level", "l2_director", "l3_executive", "mediation", "litigation", "closed":
		return true
	default:
		return false
	}
}

func aiSRMValidMeetingType(s string) bool {
	switch s {
	case models.SRMMeetingInternalKickoff, models.SRMMeetingSupplierKickoff, models.SRMMeetingJBPWorkshop,
		models.SRMMeetingOperationalPerf, models.SRMMeetingRelationshipReview, models.SRMMeetingStrategicRelation,
		models.SRMMeetingQBR, models.SRMMeetingOther:
		return true
	default:
		return false
	}
}

// ----------------------------------------------------------------------------
// Suppliers + contracts + categories
// ----------------------------------------------------------------------------

type listSRMSuppliersArgs struct {
	Tier  string `json:"tier,omitempty" jsonschema:"Filter by tier: tier_1..tier_4."`
	State string `json:"state,omitempty" jsonschema:"Filter by relationship state (e.g. registered, segmented, monitoring_active)."`
	Q     string `json:"q,omitempty" jsonschema:"Company name substring."`
}

type getSRMSupplierArgs struct {
	SupplierID int `json:"supplier_id" jsonschema:"The supplier's numeric ID."`
}

type createSRMSupplierArgs struct {
	CompanyName string  `json:"company_name" jsonschema:"Company name (required)."`
	BusinessID  *string `json:"business_id,omitempty" jsonschema:"Vendor-neutral business identifier (max 64 chars)."`
	TaxIDNumber *string `json:"tax_id_number,omitempty" jsonschema:"Tax ID number."`
	Website     string  `json:"website,omitempty" jsonschema:"Website."`
	Tier        string  `json:"tier,omitempty" jsonschema:"Tier: tier_1..tier_4 (default tier_3)."`
}

type updateSRMSupplierArgs struct {
	SupplierID int     `json:"supplier_id" jsonschema:"The supplier's numeric ID."`
	CompanyName *string `json:"company_name,omitempty" jsonschema:"New company name."`
	BusinessID  *string `json:"business_id,omitempty" jsonschema:"New business identifier."`
	TaxIDNumber *string `json:"tax_id_number,omitempty" jsonschema:"New tax ID number."`
	Website     *string `json:"website,omitempty" jsonschema:"New website."`
	Tier        *string `json:"tier,omitempty" jsonschema:"New tier: tier_1..tier_4."`
	IsActive    *bool   `json:"is_active,omitempty" jsonschema:"New active flag."`
}

type listSRMContractsArgs struct {
	SupplierID *int   `json:"supplier_id,omitempty" jsonschema:"Filter by supplier ID."`
	Status     string `json:"status,omitempty" jsonschema:"Filter by status: draft, active, in_wind_up, retired, terminated."`
	Q          string `json:"q,omitempty" jsonschema:"Contract number or title substring."`
}

type listSRMCategoriesArgs struct{}

func init() {
	Register(Default, Tool[listSRMSuppliersArgs]{
		Name:        "list_srm_suppliers",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SRM supplier directory with active-contract and open-dispute counts and contacts. Filter by tier, relationship state, or name.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMSuppliersArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT ` + aiSRMSupplierColumns + ` FROM srm_suppliers WHERE 1=1`
			var qArgs []any
			if args.Tier != "" {
				query += ` AND tier = ?`
				qArgs = append(qArgs, args.Tier)
			}
			if args.State != "" {
				query += ` AND relationship_state = ?`
				qArgs = append(qArgs, args.State)
			}
			if q := strings.TrimSpace(args.Q); q != "" {
				query += ` AND company_name LIKE ?`
				qArgs = append(qArgs, "%"+q+"%")
			}
			query += ` ORDER BY company_name`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			suppliers := make([]models.SRMSupplier, 0)
			for rows.Next() {
				s, err := aiScanSRMSupplier(rows)
				if err != nil {
					return nil, err
				}
				suppliers = append(suppliers, s)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			counts, err := aiSRMSupplierCounts(env.DB)
			if err != nil {
				return nil, err
			}
			for i := range suppliers {
				suppliers[i].ActiveContracts = counts[suppliers[i].ID]["contracts"]
				suppliers[i].OpenDisputes = counts[suppliers[i].ID]["disputes"]
			}
			contacts, err := aiSRMSupplierContacts(env.DB)
			if err != nil {
				return nil, err
			}
			for i := range suppliers {
				suppliers[i].Contacts = contacts[suppliers[i].ID]
			}
			return suppliers, nil
		},
	})

	Register(Default, Tool[getSRMSupplierArgs]{
		Name:        "get_srm_supplier",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get one SRM supplier with its contacts and computed phase-gate position (current state, next state, and which gate artifacts are still missing).",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(ctx context.Context, env *Env, args getSRMSupplierArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.SupplierID <= 0 {
				return map[string]string{"error": "supplier_id is required"}, nil
			}
			supplier, err := aiGetSRMSupplier(env.DB, args.SupplierID)
			if err != nil {
				return map[string]string{"error": "supplier not found"}, nil
			}
			contacts, err := aiSRMSupplierContacts(env.DB)
			if err != nil {
				return nil, err
			}
			supplier.Contacts = contacts[args.SupplierID]
			nextState, artifacts, missing := aiSRMGateStatus(ctx, env.DB, args.SupplierID, supplier.RelationshipState)
			supplier.Gate = &models.SRMGateStatus{Current: supplier.RelationshipState, Next: nextState, Artifacts: artifacts, Missing: missing}
			return supplier, nil
		},
	})

	Register(Default, Tool[createSRMSupplierArgs]{
		Name:        "create_srm_supplier",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Register a new supplier in the SRM directory. New suppliers start in the 'registered' state.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(_ context.Context, env *Env, args createSRMSupplierArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			companyName := strings.TrimSpace(args.CompanyName)
			if companyName == "" {
				return map[string]string{"error": "company_name is required"}, nil
			}
			tier := args.Tier
			if tier == "" {
				tier = models.SRMTier3
			}
			if !aiSRMValidTier(tier) {
				return map[string]string{"error": "tier must be one of tier_1..tier_4"}, nil
			}
			var businessID *string
			if args.BusinessID != nil {
				b := strings.TrimSpace(*args.BusinessID)
				switch {
				case b == "":
					businessID = nil
				case len(b) > 64:
					return map[string]string{"error": "business_id must be at most 64 characters"}, nil
				default:
					businessID = &b
				}
			}
			res, err := env.DB.Exec(`INSERT INTO srm_suppliers (company_name, business_id, tax_id_number, website, tier, relationship_state) VALUES (?, ?, ?, ?, ?, ?)`,
				companyName, businessID, args.TaxIDNumber, args.Website, tier, models.SRMStateRegistered)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			supplier, err := aiGetSRMSupplier(env.DB, int(id64))
			if err != nil {
				return nil, err
			}
			env.AuditWrite("srm_supplier", int(id64), "create_srm_supplier", companyName)
			return supplier, nil
		},
	})

	Register(Default, Tool[updateSRMSupplierArgs]{
		Name:        "update_srm_supplier",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Update SRM supplier directory fields. A closed supplier is a read-only archive and rejects all updates.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(ctx context.Context, env *Env, args updateSRMSupplierArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.SupplierID <= 0 {
				return map[string]string{"error": "supplier_id is required"}, nil
			}
			var state string
			if err := env.DB.QueryRowContext(ctx, `SELECT relationship_state FROM srm_suppliers WHERE id = ?`, args.SupplierID).Scan(&state); err != nil {
				return map[string]string{"error": "supplier not found"}, nil
			}
			if state == models.SRMStateClosed {
				return map[string]string{"error": "a closed supplier is a read-only archive"}, nil
			}
			existing, err := aiGetSRMSupplier(env.DB, args.SupplierID)
			if err != nil {
				return map[string]string{"error": "supplier not found"}, nil
			}
			companyName := existing.CompanyName
			if args.CompanyName != nil && strings.TrimSpace(*args.CompanyName) != "" {
				companyName = strings.TrimSpace(*args.CompanyName)
			}
			businessID := existing.BusinessID
			if args.BusinessID != nil {
				b := strings.TrimSpace(*args.BusinessID)
				if b == "" {
					businessID = nil
				} else if len(b) > 64 {
					return map[string]string{"error": "business_id must be at most 64 characters"}, nil
				} else {
					businessID = &b
				}
			}
			taxID := existing.TaxIDNumber
			if args.TaxIDNumber != nil {
				taxID = args.TaxIDNumber
			}
			website := existing.Website
			if args.Website != nil {
				website = *args.Website
			}
			tier := existing.Tier
			if args.Tier != nil {
				if !aiSRMValidTier(*args.Tier) {
					return map[string]string{"error": "tier must be one of tier_1..tier_4"}, nil
				}
				tier = *args.Tier
			}
			isActive := existing.IsActive
			if args.IsActive != nil {
				isActive = *args.IsActive
			}
			now := time.Now().UTC()
			res, err := env.DB.ExecContext(ctx,
				`UPDATE srm_suppliers SET company_name = ?, business_id = ?, tax_id_number = ?, website = ?, tier = ?, is_active = ?, updated_at = ? WHERE id = ?`,
				companyName, businessID, taxID, website, tier, isActive, now, args.SupplierID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return map[string]string{"error": "supplier not found"}, nil
			}
			updated, err := aiGetSRMSupplier(env.DB, args.SupplierID)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("srm_supplier", args.SupplierID, "update_srm_supplier", companyName)
			return updated, nil
		},
	})

	Register(Default, Tool[listSRMContractsArgs]{
		Name:        "list_srm_contracts",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List the SRM contract register with supplier names. Filter by supplier, status, or contract number/title.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMContractsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := "SELECT " + aiQualifyColumns("c", aiSRMContractColumns) + ", s.company_name AS supplier_name FROM srm_contracts c JOIN srm_suppliers s ON s.id = c.supplier_id WHERE 1=1"
			var qArgs []any
			if args.SupplierID != nil {
				query += ` AND c.supplier_id = ?`
				qArgs = append(qArgs, *args.SupplierID)
			}
			if args.Status != "" {
				query += ` AND c.status = ?`
				qArgs = append(qArgs, args.Status)
			}
			if q := strings.TrimSpace(args.Q); q != "" {
				query += ` AND (c.contract_number LIKE ? OR c.title LIKE ?)`
				qArgs = append(qArgs, "%"+q+"%", "%"+q+"%")
			}
			query += ` ORDER BY c.created_at DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			contracts := make([]models.SRMContract, 0)
			for rows.Next() {
				var c models.SRMContract
				err := rows.Scan(&c.ID, &c.SupplierID, &c.P3MEntityType, &c.P3MEntityID, &c.ContractNumber, &c.Title, &c.Category, &c.Classification, &c.ScopeSummary, &c.DocumentURL, &c.Status, &c.TotalValue, &c.Currency, &c.CommittedSpend, &c.ActualSpend, &c.SpendSource, &c.StartDate, &c.EndDate, &c.ExtensionCount, &c.NoticePeriodDays, &c.ReviewDate, &c.CostCode, &c.PONumber, &c.TerminationReason, &c.CreatedAt, &c.UpdatedAt, &c.SupplierName)
				if err != nil {
					return nil, err
				}
				contracts = append(contracts, c)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return contracts, nil
		},
	})

	Register(Default, Tool[listSRMCategoriesArgs]{
		Name:        "list_srm_categories",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM procurement categories used to scope segmentation runs.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMCategoriesArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			rows, err := env.DB.Query(`SELECT id, name, created_at FROM srm_categories ORDER BY name`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMCategory, 0)
			for rows.Next() {
				var c models.SRMCategory
				if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
					return nil, err
				}
				out = append(out, c)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})
}

// aiQualifyColumns prefixes every column in a comma list with alias.,
// mirroring handlers.qualifyColumns.
func aiQualifyColumns(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
