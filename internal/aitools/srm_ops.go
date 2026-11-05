package aitools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"windshift/internal/auth"
	"windshift/internal/models"
)

// This file adds the operational MCP/AI tools for the SRM module: segmentation
// runs, validation sign-offs, value propositions, meetings, joint business
// plans, scorecards, performance metrics, and disputes.

// --- scan helpers (JSON-decoding for meetings and JBPs) ---

func aiScanSRMMeeting(row interface{ Scan(...any) error }, supplierName *string) (models.SRMMeeting, error) {
	var m models.SRMMeeting
	var agendaJSON, outcomesJSON string
	err := row.Scan(&m.ID, &m.MeetingType, &m.SupplierID, &m.ContractID, &m.OrganizerID, &m.ScheduledAt, &m.Location, &agendaJSON, &outcomesJSON, &m.MinutesText, &m.Status, &m.CreatedAt, supplierName)
	if err != nil {
		return m, err
	}
	m.Agenda = []models.SRMAgendaItem{}
	m.Outcomes = []models.SRMOutcome{}
	if agendaJSON != "" {
		if err := json.Unmarshal([]byte(agendaJSON), &m.Agenda); err != nil {
			return m, err
		}
	}
	if outcomesJSON != "" {
		if err := json.Unmarshal([]byte(outcomesJSON), &m.Outcomes); err != nil {
			return m, err
		}
	}
	return m, nil
}

func aiScanSRMJbp(row interface{ Scan(...any) error }, supplierName *string) (models.SRMJBP, error) {
	var j models.SRMJBP
	var charterJSON, opportunitiesJSON string
	err := row.Scan(&j.ID, &j.ContractID, &j.SupplierID, &j.Version, &j.Status, &j.Purpose, &j.Vision, &j.Scope, &j.GovernanceModel, &j.ReviewCycleMonths, &charterJSON, &j.ContractSummary, &j.SpendSummary, &j.PerformanceNotes, &j.RiskActionPlan, &j.RelationshipDevelopment, &opportunitiesJSON, &j.AgencyRepName, &j.AgencyRepRole, &j.AgencySignedAt, &j.SupplierRepName, &j.SupplierRepRole, &j.SupplierSignedAt, &j.CreatedAt, &j.UpdatedAt, supplierName)
	if err != nil {
		return j, err
	}
	j.RelationshipCharter = []string{}
	j.ValueOpportunities = []models.SRMValueOpportunity{}
	if charterJSON != "" {
		if err := json.Unmarshal([]byte(charterJSON), &j.RelationshipCharter); err != nil {
			return j, err
		}
	}
	if opportunitiesJSON != "" {
		if err := json.Unmarshal([]byte(opportunitiesJSON), &j.ValueOpportunities); err != nil {
			return j, err
		}
	}
	return j, nil
}

// ----------------------------------------------------------------------------
// Segmentation + validation
// ----------------------------------------------------------------------------

type listSRMSegmentationRunsArgs struct {
	Status     string `json:"status,omitempty" jsonschema:"Filter by status: in_progress, complete, validated, superseded."`
	CategoryID *int   `json:"category_id,omitempty" jsonschema:"Filter by category ID."`
}

type listSRMValidationRequestsArgs struct {
	Status     string `json:"status,omitempty" jsonschema:"Filter by status: pending, approved, changes_requested, rejected."`
	SupplierID *int   `json:"supplier_id,omitempty" jsonschema:"Filter by supplier ID."`
	RunID      *int   `json:"run_id,omitempty" jsonschema:"Filter by segmentation run ID."`
}

type decideSRMValidationRequestArgs struct {
	RequestID     int     `json:"request_id" jsonschema:"The validation request's numeric ID."`
	Status        string  `json:"status" jsonschema:"Decision: approved, changes_requested, or rejected."`
	Comment       *string `json:"comment,omitempty" jsonschema:"Decision comment."`
	SignoffMethod *string `json:"signoff_method,omitempty" jsonschema:"Sign-off method: in_app or email."`
}

func init() {
	Register(Default, Tool[listSRMSegmentationRunsArgs]{
		Name:        "list_srm_segmentation_runs",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM supplier segmentation runs with their category. Filter by status or category.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMSegmentationRunsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			q := `SELECT r.id, r.scope, r.category_id, r.created_by, r.status, r.notes, r.created_at, r.validated_at, COALESCE(c.name, '') AS category_name
			      FROM srm_segmentation_runs r LEFT JOIN srm_categories c ON c.id = r.category_id WHERE 1=1`
			var qArgs []any
			if args.Status != "" {
				q += ` AND r.status = ?`
				qArgs = append(qArgs, args.Status)
			}
			if args.CategoryID != nil {
				q += ` AND r.category_id = ?`
				qArgs = append(qArgs, *args.CategoryID)
			}
			q += ` ORDER BY r.created_at DESC`
			rows, err := env.DB.Query(q, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			runs := make([]models.SRMSegmentationRun, 0)
			for rows.Next() {
				var run models.SRMSegmentationRun
				if err := rows.Scan(&run.ID, &run.Scope, &run.CategoryID, &run.CreatedBy, &run.Status, &run.Notes, &run.CreatedAt, &run.ValidatedAt, &run.CategoryName); err != nil {
					return nil, err
				}
				runs = append(runs, run)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return runs, nil
		},
	})

	Register(Default, Tool[listSRMValidationRequestsArgs]{
		Name:        "list_srm_validation_requests",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM segmentation validation (sign-off) requests. Filter by status, supplier, or run.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMValidationRequestsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			q := `SELECT v.id, v.run_id, v.supplier_id, v.requested_by, v.status, v.cycle_count, v.signoff_method, v.signed_off_by, v.signed_off_at, v.comment, v.created_at, v.updated_at, COALESCE(s.company_name, '') AS supplier_name
			      FROM srm_validation_requests v LEFT JOIN srm_suppliers s ON s.id = v.supplier_id WHERE 1=1`
			var qArgs []any
			if args.Status != "" {
				q += ` AND v.status = ?`
				qArgs = append(qArgs, args.Status)
			}
			if args.SupplierID != nil {
				q += ` AND v.supplier_id = ?`
				qArgs = append(qArgs, *args.SupplierID)
			}
			if args.RunID != nil {
				q += ` AND v.run_id = ?`
				qArgs = append(qArgs, *args.RunID)
			}
			q += ` ORDER BY v.created_at DESC`
			rows, err := env.DB.Query(q, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			reqs := make([]models.SRMValidationRequest, 0)
			for rows.Next() {
				var v models.SRMValidationRequest
				if err := rows.Scan(&v.ID, &v.RunID, &v.SupplierID, &v.RequestedBy, &v.Status, &v.CycleCount, &v.SignoffMethod, &v.SignedOffBy, &v.SignedOffAt, &v.Comment, &v.CreatedAt, &v.UpdatedAt, &v.SupplierName); err != nil {
					return nil, err
				}
				reqs = append(reqs, v)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return reqs, nil
		},
	})

	Register(Default, Tool[decideSRMValidationRequestArgs]{
		Name:        "decide_srm_validation_request",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Decide a pending SRM segmentation validation request. Approving validates the run and applies its mapped tiers to suppliers; changes_requested bumps the cycle (max 3); rejecting closes it.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(ctx context.Context, env *Env, args decideSRMValidationRequestArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.RequestID <= 0 {
				return map[string]string{"error": "request_id is required"}, nil
			}
			var v models.SRMValidationRequest
			err = env.DB.QueryRowContext(ctx, `SELECT id, run_id, supplier_id, status, cycle_count FROM srm_validation_requests WHERE id = ?`, args.RequestID).
				Scan(&v.ID, &v.RunID, &v.SupplierID, &v.Status, &v.CycleCount)
			if err != nil {
				return map[string]string{"error": "validation request not found"}, nil
			}
			if v.Status != models.SRMValidationPending {
				return map[string]string{"error": "this validation request has already been decided"}, nil
			}
			now := time.Now().UTC()
			switch args.Status {
			case models.SRMValidationApproved:
				if _, err := env.DB.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'approved', signed_off_by = ?, signed_off_at = ?, comment = COALESCE(?, comment), signoff_method = COALESCE(?, signoff_method) WHERE id = ?`,
					env.UserID, now, args.Comment, args.SignoffMethod, args.RequestID); err != nil {
					return nil, err
				}
				if _, err := env.DB.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'validated', validated_at = ? WHERE id = ?`, now, v.RunID); err != nil {
					return nil, err
				}
				rows, err := env.DB.QueryContext(ctx, `SELECT entity_ref, mapped_tier FROM srm_segmentation_results WHERE run_id = ? AND entity_type = 'supplier' AND mapped_tier IS NOT NULL`, v.RunID)
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					var ref int
					var tier string
					if err := rows.Scan(&ref, &tier); err != nil {
						rows.Close()
						return nil, err
					}
					env.DB.ExecContext(ctx, `UPDATE srm_suppliers SET tier = ?, updated_at = ? WHERE id = ?`, tier, now, ref)
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					return nil, err
				}
			case models.SRMValidationChangesRequested:
				if v.CycleCount >= 3 {
					return map[string]string{"error": "maximum of 3 validation cycles reached; approve or reject"}, nil
				}
				if _, err := env.DB.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'changes_requested', cycle_count = cycle_count + 1, comment = COALESCE(?, comment) WHERE id = ?`, args.Comment, args.RequestID); err != nil {
					return nil, err
				}
				if _, err := env.DB.ExecContext(ctx, `UPDATE srm_segmentation_runs SET status = 'in_progress' WHERE id = ?`, v.RunID); err != nil {
					return nil, err
				}
			case models.SRMValidationRejected:
				if _, err := env.DB.ExecContext(ctx, `UPDATE srm_validation_requests SET status = 'rejected', comment = COALESCE(?, comment), signoff_method = COALESCE(?, signoff_method) WHERE id = ?`, args.Comment, args.SignoffMethod, args.RequestID); err != nil {
					return nil, err
				}
			default:
				return map[string]string{"error": "status must be approved, changes_requested or rejected"}, nil
			}
			env.AuditWrite("srm_validation_request", args.RequestID, "decide_srm_validation_request", args.Status)
			var out models.SRMValidationRequest
			err = env.DB.QueryRowContext(ctx, `SELECT v.id, v.run_id, v.supplier_id, v.requested_by, v.status, v.cycle_count, v.signoff_method, v.signed_off_by, v.signed_off_at, v.comment, v.created_at, v.updated_at, COALESCE(s.company_name, '') AS supplier_name
				FROM srm_validation_requests v LEFT JOIN srm_suppliers s ON s.id = v.supplier_id WHERE v.id = ?`, args.RequestID).
				Scan(&out.ID, &out.RunID, &out.SupplierID, &out.RequestedBy, &out.Status, &out.CycleCount, &out.SignoffMethod, &out.SignedOffBy, &out.SignedOffAt, &out.Comment, &out.CreatedAt, &out.UpdatedAt, &out.SupplierName)
			if err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// Value propositions
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMValuePropositionsArgs]{
		Name:        "list_srm_value_propositions",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM value propositions (the mutually-beneficial case for a supplier relationship), with sign-off state. Filter by supplier.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMValuePropositionsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			q := "SELECT " + aiQualifyColumns("vp", aiSRMValuePropColumns) + ", s.company_name AS supplier_name FROM srm_value_propositions vp JOIN srm_suppliers s ON s.id = vp.supplier_id WHERE 1=1"
			var qArgs []any
			if args.SupplierID != nil {
				q += ` AND vp.supplier_id = ?`
				qArgs = append(qArgs, *args.SupplierID)
			}
			q += ` ORDER BY vp.created_at DESC`
			rows, err := env.DB.Query(q, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMValueProposition, 0)
			for rows.Next() {
				var vp models.SRMValueProposition
				err := rows.Scan(&vp.ID, &vp.SupplierID, &vp.ContractID, &vp.AgencyObjective, &vp.SupplierServiceProduct, &vp.SrmAmbition, &vp.BenefitType, &vp.AnticipatedBenefit, &vp.EstimatedValueNzd, &vp.ValueConfidence, &vp.MeasurementMethod, &vp.EngagementFrequency, &vp.P3MBenefitRef, &vp.ActualValueNzd, &vp.SupplierStrategicObjective, &vp.OpportunityStatement, &vp.SrmSpecificAmbition, &vp.AnticipatedSupplierBenefits, &vp.SignedOffBy, &vp.SignedOffAt, &vp.CreatedAt, &vp.UpdatedAt, &vp.SupplierName)
				if err != nil {
					return nil, err
				}
				out = append(out, vp)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// Meetings
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMMeetingsArgs]{
		Name:        "list_srm_meetings",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM relationship meetings (kick-offs, workshops, QBRs, reviews) with agendas and outcomes. Filter by supplier.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMMeetingsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			q := "SELECT " + aiQualifyColumns("mt", aiSRMMeetingColumns) + ", s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id"
			var qArgs []any
			if args.SupplierID != nil {
				q += ` WHERE mt.supplier_id = ?`
				qArgs = append(qArgs, *args.SupplierID)
			}
			q += ` ORDER BY mt.scheduled_at DESC, mt.id DESC`
			rows, err := env.DB.Query(q, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMMeeting, 0)
			for rows.Next() {
				var name string
				m, err := aiScanSRMMeeting(rows, &name)
				if err != nil {
					return nil, err
				}
				out = append(out, m)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[createSRMMeetingArgs]{
		Name:        "create_srm_meeting",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Create an SRM relationship meeting. Optionally bind it to one of the supplier's contracts and attach an agenda.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(ctx context.Context, env *Env, args createSRMMeetingArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if !aiSRMValidMeetingType(args.MeetingType) {
				return map[string]string{"error": "meeting_type must be one of internal_kickoff, supplier_kickoff, jbp_workshop, operational_performance, relationship_review, strategic_relationship, qbr, other"}, nil
			}
			scheduledAt, err := aiParsePlanDate(args.ScheduledAt)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			if scheduledAt == nil {
				return map[string]string{"error": "scheduled_at is required"}, nil
			}
			status := args.Status
			if status == "" {
				status = "scheduled"
			}
			if status != "scheduled" && status != "held" && status != "cancelled" {
				return map[string]string{"error": "status must be one of scheduled, held, cancelled"}, nil
			}
			var supplierExists int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM srm_suppliers WHERE id = ?`, args.SupplierID).Scan(&supplierExists); err != nil || supplierExists == 0 {
				return map[string]string{"error": "supplier not found"}, nil
			}
			if args.ContractID != nil {
				supplierID, err := aiSRMContractSupplier(env.DB, *args.ContractID)
				if err != nil {
					return map[string]string{"error": "contract not found"}, nil
				}
				if supplierID != args.SupplierID {
					return map[string]string{"error": "contract does not belong to this supplier"}, nil
				}
			}
			agendaJSON := []byte(`[]`)
			if len(args.Agenda) > 0 {
				b, err := json.Marshal(args.Agenda)
				if err != nil {
					return map[string]string{"error": "invalid agenda"}, nil
				}
				agendaJSON = b
			}
			res, err := env.DB.ExecContext(ctx, `
				INSERT INTO srm_meetings (meeting_type, supplier_id, contract_id, organizer_id, scheduled_at, location, agenda_json, minutes_text, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				args.MeetingType, args.SupplierID, nullIntPtr(args.ContractID), nullIntPtr(args.OrganizerID), *scheduledAt, args.Location, string(agendaJSON), args.MinutesText, status)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			var name string
			m2, err := aiScanSRMMeeting(env.DB.QueryRow("SELECT "+aiQualifyColumns("mt", aiSRMMeetingColumns)+`, s.company_name AS supplier_name FROM srm_meetings mt JOIN srm_suppliers s ON s.id = mt.supplier_id WHERE mt.id = ?`, id64), &name)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("srm_meeting", int(id64), "create_srm_meeting", args.MeetingType)
			return m2, nil
		},
	})

	// ------------------------------------------------------------------------
	// Joint business plans
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMJbpsArgs]{
		Name:        "list_srm_jbps",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM joint business plan versions with sign-off state. Filter by supplier or contract.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMJbpsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			q := "SELECT " + aiQualifyColumns("j", aiSRMJbpColumns) + ", s.company_name AS supplier_name FROM srm_jbps j JOIN srm_suppliers s ON s.id = j.supplier_id WHERE 1=1"
			var qArgs []any
			if args.SupplierID != nil {
				q += ` AND j.supplier_id = ?`
				qArgs = append(qArgs, *args.SupplierID)
			}
			if args.ContractID != nil {
				q += ` AND j.contract_id = ?`
				qArgs = append(qArgs, *args.ContractID)
			}
			q += ` ORDER BY j.created_at DESC`
			rows, err := env.DB.Query(q, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMJBP, 0)
			for rows.Next() {
				var name string
				j, err := aiScanSRMJbp(rows, &name)
				if err != nil {
					return nil, err
				}
				out = append(out, j)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[getSRMJbpArgs]{
		Name:        "get_srm_jbp",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "Get one SRM joint business plan version with its quarterly initiatives.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(ctx context.Context, env *Env, args getSRMJbpArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.JbpID <= 0 {
				return map[string]string{"error": "jbp_id is required"}, nil
			}
			var name string
			j, err := aiScanSRMJbp(env.DB.QueryRow("SELECT "+aiQualifyColumns("j", aiSRMJbpColumns)+`, s.company_name AS supplier_name FROM srm_jbps j JOIN srm_suppliers s ON s.id = j.supplier_id WHERE j.id = ?`, args.JbpID), &name)
			if err != nil {
				return map[string]string{"error": "jbp not found"}, nil
			}
			// Load initiatives.
			initRows, err := env.DB.QueryContext(ctx, `SELECT id, jbp_id, quarter_label, part, initiative_name, activities, key_dates, owner_id, comment, status, created_at FROM srm_jbp_initiatives WHERE jbp_id = ? ORDER BY quarter_label, id`, args.JbpID)
			if err != nil {
				return nil, err
			}
			initiatives := make([]models.SRMJBPInitiative, 0)
			for initRows.Next() {
				var i models.SRMJBPInitiative
				var keyDatesJSON string
				if err := initRows.Scan(&i.ID, &i.JbpID, &i.QuarterLabel, &i.Part, &i.InitiativeName, &i.Activities, &keyDatesJSON, &i.OwnerID, &i.Comment, &i.Status, &i.CreatedAt); err != nil {
					initRows.Close()
					return nil, err
				}
				i.KeyDates = []string{}
				if keyDatesJSON != "" {
					if err := json.Unmarshal([]byte(keyDatesJSON), &i.KeyDates); err != nil {
						initRows.Close()
						return nil, err
					}
				}
				if i.OwnerID != nil {
					var un string
					if err := env.DB.QueryRow(`SELECT COALESCE(first_name || ' ' || last_name, '') FROM users WHERE id = ?`, *i.OwnerID).Scan(&un); err == nil {
						i.UserName = un
					}
				}
				initiatives = append(initiatives, i)
			}
			if err := initRows.Err(); err != nil {
				initRows.Close()
				return nil, err
			}
			initRows.Close()
			j.Initiatives = initiatives
			return j, nil
		},
	})

	// ------------------------------------------------------------------------
	// Scorecards
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMScorecardsArgs]{
		Name:        "list_srm_scorecards",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM collaborative balanced scorecards. Filter by contract or status.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMScorecardsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := "SELECT " + aiQualifyColumns("sc", aiSRMScorecardColumns) + ", s.company_name AS supplier_name FROM srm_scorecards sc JOIN srm_suppliers s ON s.id = sc.supplier_id WHERE 1=1"
			var qArgs []any
			if args.ContractID != nil {
				query += ` AND sc.contract_id = ?`
				qArgs = append(qArgs, *args.ContractID)
			}
			if args.Status != "" {
				query += ` AND sc.status = ?`
				qArgs = append(qArgs, args.Status)
			}
			query += ` ORDER BY sc.created_at DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMScorecard, 0)
			for rows.Next() {
				var s models.SRMScorecard
				if err := rows.Scan(&s.ID, &s.ContractID, &s.SupplierID, &s.PeriodStart, &s.PeriodEnd, &s.CompletedCollaboratively, &s.OverallScore, &s.OverallReasoning, &s.AgencyCompletedBy, &s.SupplierCompletedByName, &s.Status, &s.CreatedAt, &s.SupplierName); err != nil {
					return nil, err
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
	// Performance metrics + results
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMMetricsArgs]{
		Name:        "list_srm_metrics",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM performance metrics (SLAs and KPIs) on a contract. Filter by contract or active flag.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMMetricsArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := `SELECT m.` + aiSRMMetricColumns + ` FROM srm_performance_metrics m WHERE 1=1`
			var qArgs []any
			if args.ContractID != nil {
				query += ` AND m.contract_id = ?`
				qArgs = append(qArgs, *args.ContractID)
			}
			if args.IsActive != nil {
				query += ` AND m.is_active = ?`
				qArgs = append(qArgs, *args.IsActive)
			}
			query += ` ORDER BY m.created_at DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMPerformanceMetric, 0)
			for rows.Next() {
				var m models.SRMPerformanceMetric
				if err := rows.Scan(&m.ID, &m.ContractID, &m.Name, &m.Type, &m.Unit, &m.TargetValue, &m.Direction, &m.Frequency, &m.DataSource, &m.OwnerID, &m.IsActive, &m.CreatedAt); err != nil {
					return nil, err
				}
				out = append(out, m)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[recordSRMMetricResultArgs]{
		Name:        "record_srm_metric_result",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Record one period's measured value against an SRM performance metric. A result can only be recorded once per period.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(ctx context.Context, env *Env, args recordSRMMetricResultArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if args.MetricID <= 0 {
				return map[string]string{"error": "metric_id is required"}, nil
			}
			periodStart, err := aiParsePlanDate(args.PeriodStart)
			if err != nil {
				return map[string]string{"error": err.Error()}, nil
			}
			if periodStart == nil {
				return map[string]string{"error": "period_start is required"}, nil
			}
			var metric models.SRMPerformanceMetric
			err = env.DB.QueryRowContext(ctx, `SELECT `+aiSRMMetricColumns+` FROM srm_performance_metrics WHERE id = ?`, args.MetricID).Scan(
				&metric.ID, &metric.ContractID, &metric.Name, &metric.Type, &metric.Unit, &metric.TargetValue, &metric.Direction, &metric.Frequency, &metric.DataSource, &metric.OwnerID, &metric.IsActive, &metric.CreatedAt)
			if err != nil {
				return map[string]string{"error": "metric not found"}, nil
			}
			var n int
			err = env.DB.QueryRowContext(ctx, `SELECT 1 FROM srm_metric_results WHERE metric_id = ? AND period_start = ?`, args.MetricID, *periodStart).Scan(&n)
			if err == nil {
				return map[string]string{"error": "a result is already recorded for this period"}, nil
			} else if err != sql.ErrNoRows {
				return nil, err
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO srm_metric_results (metric_id, period_start, actual_value, achieved, recorded_by) VALUES (?, ?, ?, ?, ?)`,
				args.MetricID, *periodStart, args.ActualValue, args.Achieved, env.UserID)
			if err != nil {
				return nil, err
			}
			resultID64, _ := res.LastInsertId()
			var out models.SRMMetricResult
			out.MetricName = metric.Name
			err = env.DB.QueryRowContext(ctx, `SELECT id, metric_id, period_start, actual_value, achieved, recorded_by, recorded_at FROM srm_metric_results WHERE id = ?`, resultID64).Scan(
				&out.ID, &out.MetricID, &out.PeriodStart, &out.ActualValue, &out.Achieved, &out.RecordedBy, &out.RecordedAt)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("srm_metric_result", int(resultID64), "record_srm_metric_result", metric.Name)
			return out, nil
		},
	})

	// ------------------------------------------------------------------------
	// Disputes
	// ------------------------------------------------------------------------
	Register(Default, Tool[listSRMDisputesArgs]{
		Name:        "list_srm_disputes",
		Group:       CapabilitySRM,
		Access:      AccessRead,
		Risk:        RiskLow,
		Description: "List SRM supplier disputes with their escalation stage. Filter by contract or stage.",
		Scopes:      []string{auth.ScopeSRMRead},
		Run: func(_ context.Context, env *Env, args listSRMDisputesArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			query := "SELECT " + aiQualifyColumns("dp", aiSRMDisputeColumns) + ", s.company_name AS supplier_name FROM srm_disputes dp JOIN srm_suppliers s ON s.id = dp.supplier_id WHERE 1=1"
			var qArgs []any
			if args.ContractID != nil {
				query += ` AND dp.contract_id = ?`
				qArgs = append(qArgs, *args.ContractID)
			}
			if args.Stage != "" {
				query += ` AND dp.stage = ?`
				qArgs = append(qArgs, args.Stage)
			}
			query += ` ORDER BY dp.opened_at DESC`
			rows, err := env.DB.Query(query, qArgs...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]models.SRMDispute, 0)
			for rows.Next() {
				var d models.SRMDispute
				if err := rows.Scan(&d.ID, &d.ContractID, &d.SupplierID, &d.Description, &d.Stage, &d.OpenedAt, &d.ResolvedAt, &d.OutcomeNote, &d.SupplierName); err != nil {
					return nil, err
				}
				out = append(out, d)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	Register(Default, Tool[createSRMDisputeArgs]{
		Name:        "create_srm_dispute",
		Group:       CapabilitySRM,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Open an SRM supplier dispute against a contract. The supplier must match the contract's supplier. Starts at the working-level (l1) stage by default.",
		Scopes:      []string{auth.ScopeSRMWrite},
		Run: func(ctx context.Context, env *Env, args createSRMDisputeArgs) (any, error) {
			ok, err := aiRequireSRMView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}
			if strings.TrimSpace(args.Description) == "" {
				return map[string]string{"error": "description is required"}, nil
			}
			stage := args.Stage
			if stage == "" {
				stage = "l1_working_level"
			} else if !aiSRMValidDisputeStage(stage) {
				return map[string]string{"error": "stage must be one of: l1_working_level, l2_director, l3_executive, mediation, litigation, closed"}, nil
			}
			contractSupplier, err := aiSRMContractSupplier(env.DB, args.ContractID)
			if err != nil {
				return map[string]string{"error": "contract not found"}, nil
			}
			if args.SupplierID != contractSupplier {
				return map[string]string{"error": "supplier_id does not match the contract's supplier"}, nil
			}
			res, err := env.DB.ExecContext(ctx, `INSERT INTO srm_disputes (contract_id, supplier_id, description, stage) VALUES (?, ?, ?, ?)`,
				args.ContractID, args.SupplierID, args.Description, stage)
			if err != nil {
				return nil, err
			}
			id64, _ := res.LastInsertId()
			var d models.SRMDispute
			err = env.DB.QueryRowContext(ctx, "SELECT "+aiQualifyColumns("dp", aiSRMDisputeColumns)+`, s.company_name AS supplier_name FROM srm_disputes dp JOIN srm_suppliers s ON s.id = dp.supplier_id WHERE dp.id = ?`, id64).
				Scan(&d.ID, &d.ContractID, &d.SupplierID, &d.Description, &d.Stage, &d.OpenedAt, &d.ResolvedAt, &d.OutcomeNote, &d.SupplierName)
			if err != nil {
				return nil, err
			}
			env.AuditWrite("srm_dispute", int(id64), "create_srm_dispute", fmt.Sprintf("contract %d stage %s", args.ContractID, stage))
			return d, nil
		},
	})
}

// args structs for SRM operational tools.
type listSRMValuePropositionsArgs struct {
	SupplierID *int `json:"supplier_id,omitempty" jsonschema:"Filter by supplier ID."`
}

type listSRMMeetingsArgs struct {
	SupplierID *int `json:"supplier_id,omitempty" jsonschema:"Filter by supplier ID."`
}

type createSRMMeetingArgs struct {
	MeetingType string               `json:"meeting_type" jsonschema:"Type: internal_kickoff, supplier_kickoff, jbp_workshop, operational_performance, relationship_review, strategic_relationship, qbr, other."`
	SupplierID  int                  `json:"supplier_id" jsonschema:"The supplier this meeting is for."`
	ContractID  *int                 `json:"contract_id,omitempty" jsonschema:"Optional contract (must belong to the supplier)."`
	OrganizerID *int                 `json:"organizer_id,omitempty" jsonschema:"Organizer user ID."`
	ScheduledAt string               `json:"scheduled_at" jsonschema:"Scheduled time (RFC 3339 or YYYY-MM-DD)."`
	Location    string               `json:"location,omitempty" jsonschema:"Location."`
	Agenda      []models.SRMAgendaItem `json:"agenda,omitempty" jsonschema:"Agenda items."`
	MinutesText string               `json:"minutes_text,omitempty" jsonschema:"Minutes (free text)."`
	Status      string               `json:"status,omitempty" jsonschema:"Status: scheduled (default), held, cancelled."`
}

type listSRMJbpsArgs struct {
	SupplierID *int `json:"supplier_id,omitempty" jsonschema:"Filter by supplier ID."`
	ContractID *int `json:"contract_id,omitempty" jsonschema:"Filter by contract ID."`
}

type getSRMJbpArgs struct {
	JbpID int `json:"jbp_id" jsonschema:"The joint business plan version's numeric ID."`
}

type listSRMScorecardsArgs struct {
	ContractID *int   `json:"contract_id,omitempty" jsonschema:"Filter by contract ID."`
	Status     string `json:"status,omitempty" jsonschema:"Filter by status."`
}

type listSRMMetricsArgs struct {
	ContractID *int  `json:"contract_id,omitempty" jsonschema:"Filter by contract ID."`
	IsActive   *bool `json:"is_active,omitempty" jsonschema:"Filter by active flag."`
}

type recordSRMMetricResultArgs struct {
	MetricID    int     `json:"metric_id" jsonschema:"The metric's numeric ID."`
	PeriodStart string  `json:"period_start" jsonschema:"Period start (YYYY-MM-DD or RFC 3339)."`
	ActualValue float64 `json:"actual_value" jsonschema:"Measured value for the period."`
	Achieved    bool    `json:"achieved" jsonschema:"Whether the target was achieved for the period."`
}

type listSRMDisputesArgs struct {
	ContractID *int   `json:"contract_id,omitempty" jsonschema:"Filter by contract ID."`
	Stage      string `json:"stage,omitempty" jsonschema:"Filter by stage: l1_working_level, l2_director, l3_executive, mediation, litigation, closed."`
}

type createSRMDisputeArgs struct {
	ContractID  int    `json:"contract_id" jsonschema:"The contract the dispute is against."`
	SupplierID  int    `json:"supplier_id" jsonschema:"The supplier (must match the contract's supplier)."`
	Description string `json:"description" jsonschema:"What the dispute is about (required)."`
	Stage       string `json:"stage,omitempty" jsonschema:"Escalation stage (default l1_working_level)."`
}
