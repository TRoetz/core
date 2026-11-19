package e2e

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// UC-M1..M6 — Portfolio, Programme & Project Management (P3M): hierarchy and rollups,
// governance (stage gates, RACI, budget approvals), benefits and financials, RAID lifecycle,
// investment cases, resources and CPM analysis.

const p3mE2eTol = 1e-6

func p3mE2eApprox(a, b float64) bool { return a-b < p3mE2eTol && b-a < p3mE2eTol }

// p3mE2eF converts a dbQueryOne value (int64/float64/string/nil) to float64.
func p3mE2eF(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

var p3mE2eFracSeq int64

// p3mE2eCreatePortfolio creates a portfolio via the API and returns its ID.
func p3mE2eCreatePortfolio(t *testing.T, c *Client, title string) int {
	t.Helper()
	status, body := c.Post(t, "/api/p3m/portfolios", map[string]any{"title": title})
	wantStatus(t, "POST", "/api/p3m/portfolios", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// p3mE2eCreateProgramme creates a programme under the given portfolio and returns its ID.
func p3mE2eCreateProgramme(t *testing.T, c *Client, portfolioID int, title string) int {
	t.Helper()
	status, body := c.Post(t, "/api/p3m/programmes", map[string]any{
		"portfolio_id": portfolioID,
		"title":        title,
	})
	wantStatus(t, "POST", "/api/p3m/programmes", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// p3mE2eCreateProject creates a project; extra carries parent and budget fields.
func p3mE2eCreateProject(t *testing.T, c *Client, title string, extra map[string]any) int {
	t.Helper()
	req := map[string]any{"title": title}
	for k, v := range extra {
		req[k] = v
	}
	status, body := c.Post(t, "/api/p3m/projects", req)
	wantStatus(t, "POST", "/api/p3m/projects", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// p3mE2eSetThreshold upserts the authorization threshold for a tier.
func p3mE2eSetThreshold(t *testing.T, c *Client, level string, amount float64) {
	t.Helper()
	status, body := c.Put(t, "/api/p3m/authorization-thresholds", map[string]any{
		"level":            level,
		"threshold_amount": amount,
	})
	wantStatus(t, "PUT", "/api/p3m/authorization-thresholds", status, body, http.StatusOK)
}

// p3mE2eTreeBudget returns (projectCount, approvedSum, actualSum) for a portfolio tree.
func p3mE2eTreeBudget(t *testing.T, portfolioID int) (int, float64, float64) {
	t.Helper()
	row := dbQueryOne(t, `SELECT COUNT(*) AS n, COALESCE(SUM(budget_approved), 0) AS appr, COALESCE(SUM(budget_actual), 0) AS act
		FROM p3m_projects p LEFT JOIN p3m_programmes pg ON pg.id = p.programme_id
		WHERE COALESCE(p.portfolio_id, pg.portfolio_id) = ?`, portfolioID)
	return cfInt(row["n"]), p3mE2eF(row["appr"]), p3mE2eF(row["act"])
}

// p3mE2ePipelineCounts returns per-status investment case counts for a portfolio tree.
func p3mE2ePipelineCounts(t *testing.T, portfolioID int) map[string]int {
	t.Helper()
	out := make(map[string]int, 5)
	for _, st := range []string{"proposed", "under_review", "approved", "rejected", "deferred"} {
		out[st] = dbCount(t, `p3m_investment_cases ic JOIN p3m_projects p ON p.id = ic.project_id LEFT JOIN p3m_programmes pg ON pg.id = p.programme_id`,
			"ic.status = ? AND COALESCE(p.portfolio_id, pg.portfolio_id) = ?", st, portfolioID)
	}
	return out
}

// p3mE2eFindNode locates a node by ID in the /p3m/hierarchy response.
func p3mE2eFindNode(t *testing.T, body []byte, id int) map[string]any {
	t.Helper()
	root := mustJSON(t, body).(map[string]any)
	pfs, _ := root["portfolios"].([]any)
	for _, pfV := range pfs {
		pf := pfV.(map[string]any)
		if cfInt(pf["id"]) == id {
			return pf
		}
		pgs, _ := pf["programmes"].([]any) // null when the portfolio has no programmes
		for _, pgV := range pgs {
			pg := pgV.(map[string]any)
			if cfInt(pg["id"]) == id {
				return pg
			}
			prs, _ := pg["projects"].([]any) // null when the programme has no projects
			for _, prV := range prs {
				pr := prV.(map[string]any)
				if cfInt(pr["id"]) == id {
					return pr
				}
			}
		}
		sps, _ := pf["standalone_projects"].([]any) // null when there are none
		for _, spV := range sps {
			sp := spV.(map[string]any)
			if cfInt(sp["id"]) == id {
				return sp
			}
		}
	}
	t.Fatalf("node %d not found in hierarchy: %s", id, snippet(body))
	return nil
}

// UC-M1 — Hierarchy and rollups: portfolio → programme → project (with RAG status, budget, owner);
// executive summary totals equal the tree sums (DB cross-check); per-tier authorization threshold CRUD.
func TestP3MHierarchyAndRollups(t *testing.T) {
	admin := e2eEnv.admin
	ownerID := createUser(t, "e2e-p3m-m1-owner@windshift.test", "e2ep3mm1owner", "E2eP3mM1Owner!1")

	status, body := admin.Post(t, "/api/p3m/portfolios", map[string]any{
		"title":               "E2E P3M M1 Portfolio",
		"description":         "e2e hierarchy portfolio",
		"strategic_objective": "e2e objective",
		"budget_allocated":    500000,
		"rag_status":          "green",
		"owner_id":            ownerID,
	})
	wantStatus(t, "POST", "/api/p3m/portfolios", status, body, http.StatusCreated)
	assertJSONField(t, body, "title", "E2E P3M M1 Portfolio")
	assertJSONField(t, body, "rag_status", "green")
	pfID := cfExtractID(t, body)

	status, body = admin.Post(t, "/api/p3m/programmes", map[string]any{
		"portfolio_id":     pfID,
		"title":            "E2E P3M M1 Programme",
		"manager_id":       ownerID,
		"budget_allocated": 300000,
		"rag_status":       "amber",
	})
	wantStatus(t, "POST", "/api/p3m/programmes", status, body, http.StatusCreated)
	assertJSONField(t, body, "rag_status", "amber")
	pgID := cfExtractID(t, body)

	status, body = admin.Post(t, "/api/p3m/projects", map[string]any{
		"programme_id":     pgID,
		"title":            "E2E P3M M1 Project",
		"budget_approved":  120000,
		"budget_actual":    90000,
		"progress_percent": 50,
	})
	wantStatus(t, "POST", "/api/p3m/projects", status, body, http.StatusCreated)
	assertJSONField(t, body, "title", "E2E P3M M1 Project")
	assertJSONField(t, body, "rag_status", "green") // no overrun and no SPI signal (no dates) → green
	prID := cfExtractID(t, body)

	row := dbQueryOne(t, `SELECT programme_id FROM p3m_projects WHERE id = ?`, prID)
	if got := cfInt(row["programme_id"]); got != pgID {
		t.Fatalf("p3m_projects.programme_id = %d, want %d", got, pgID)
	}

	t.Run("HierarchyTreeNestsProjectUnderProgramme", func(t *testing.T) {
		status, body := admin.Get(t, "/api/p3m/hierarchy")
		wantStatus(t, "GET", "/api/p3m/hierarchy", status, body, http.StatusOK)
		pfNode := p3mE2eFindNode(t, body, pfID)
		pgs, _ := pfNode["programmes"].([]any)
		if len(pgs) != 1 {
			t.Fatalf("portfolio %d has %d programmes in hierarchy, want 1: %s", pfID, len(pgs), snippet(body))
		}
		pgNode := pgs[0].(map[string]any)
		prs, _ := pgNode["projects"].([]any)
		if len(prs) != 1 || cfInt(prs[0].(map[string]any)["id"]) != prID {
			t.Fatalf("programme %d does not nest project %d in hierarchy: %s", pgID, prID, snippet(body))
		}
	})

	t.Run("ExecutiveSummaryMatchesDBTreeSums", func(t *testing.T) {
		status, body := admin.Get(t, "/api/p3m/executive-summary")
		wantStatus(t, "GET", "/api/p3m/executive-summary", status, body, http.StatusOK)
		root := mustJSON(t, body).(map[string]any)
		var sum map[string]any
		for _, v := range root["portfolios"].([]any) {
			m := v.(map[string]any)
			if cfInt(m["portfolio_id"]) == pfID {
				sum = m
			}
		}
		if sum == nil {
			t.Fatalf("executive summary missing portfolio %d: %s", pfID, snippet(body))
		}
		n, appr, act := p3mE2eTreeBudget(t, pfID)
		if n != 1 {
			t.Fatalf("DB tree has %d projects, want 1 (test setup broken)", n)
		}
		if got := p3mE2eF(sum["budget_approved"]); !p3mE2eApprox(got, appr) {
			t.Fatalf("executive summary budget_approved = %v, DB tree sum = %v", got, appr)
		}
		if got := p3mE2eF(sum["budget_actual"]); !p3mE2eApprox(got, act) {
			t.Fatalf("executive summary budget_actual = %v, DB tree sum = %v", got, act)
		}
	})

	t.Run("AuthorizationThresholdCRUD", func(t *testing.T) {
		p3mE2eSetThreshold(t, admin, "project", 25000)

		status, body := admin.Get(t, "/api/p3m/authorization-thresholds")
		wantStatus(t, "GET", "/api/p3m/authorization-thresholds", status, body, http.StatusOK)
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfStr(e["level"]) == "project" && p3mE2eF(e["threshold_amount"]) == 25000 {
				found = true
			}
		}
		if !found {
			t.Fatalf("GET thresholds missing project=25000: %s", snippet(body))
		}
		row := dbQueryOne(t, `SELECT threshold_amount FROM p3m_authorization_thresholds WHERE level = 'project'`)
		if got := p3mE2eF(row["threshold_amount"]); !p3mE2eApprox(got, 25000) {
			t.Fatalf("DB project threshold = %v, want 25000", got)
		}

		status, body = admin.Put(t, "/api/p3m/authorization-thresholds", map[string]any{
			"level": "department", "threshold_amount": 1,
		})
		wantStatus(t, "PUT", "/api/p3m/authorization-thresholds (invalid level)", status, body, http.StatusBadRequest)

		status, body = admin.Put(t, "/api/p3m/authorization-thresholds", map[string]any{
			"level": "project", "threshold_amount": -1,
		})
		wantStatus(t, "PUT", "/api/p3m/authorization-thresholds (negative)", status, body, http.StatusBadRequest)
	})
}

// UC-M2 — Governance: stage gates (pass requires completed required artifacts; fail allowed),
// RACI matrix CRUD with upsert semantics, budget approval routing by authorization thresholds.
func TestP3MGovernance(t *testing.T) {
	admin := e2eEnv.admin
	uid := createUser(t, "e2e-p3m-m2-user@windshift.test", "e2ep3mm2user", "E2eP3mM2User!1")

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M M2 Portfolio")
	pgID := p3mE2eCreateProgramme(t, admin, pfID, "E2E P3M M2 Programme")
	prID := p3mE2eCreateProject(t, admin, "E2E P3M M2 Project", map[string]any{
		"programme_id":    pgID,
		"budget_approved": 50000,
	})

	t.Run("StageGatePassRequiresCompletedArtifacts", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/stage-gates", map[string]any{
			"project_id": prID,
			"to_stage":   "proposal",
			"artifacts": []map[string]any{
				{"name": "Classification", "required": true},
				{"name": "Intake notes", "required": false},
			},
		})
		wantStatus(t, "POST", "/api/p3m/stage-gates", status, body, http.StatusCreated)
		gateID := cfExtractID(t, body)

		var reqArtifactID int
		root := mustJSON(t, body).(map[string]any)
		for _, aV := range root["artifacts"].([]any) {
			a := aV.(map[string]any)
			if b, _ := a["required"].(bool); b {
				reqArtifactID = cfInt(a["id"])
			}
		}
		if reqArtifactID == 0 {
			t.Fatalf("no required artifact in gate response: %s", snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/stage-gates/%d/decision", gateID), map[string]any{"passed": true})
		wantStatus(t, "POST", "/api/p3m/stage-gates/{id}/decision (incomplete)", status, body, http.StatusConflict)

		status, body = admin.Put(t, fmt.Sprintf("/api/p3m/gate-artifacts/%d", reqArtifactID), map[string]any{
			"completed":    true,
			"link_or_note": "classified",
		})
		wantStatus(t, "PUT", "/api/p3m/gate-artifacts/{id}", status, body, http.StatusOK)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/stage-gates/%d/decision", gateID), map[string]any{"passed": true})
		wantStatus(t, "POST", "/api/p3m/stage-gates/{id}/decision (complete)", status, body, http.StatusOK)
		assertJSONField(t, body, "passed", true)

		row := dbQueryOne(t, `SELECT current_stage FROM p3m_projects WHERE id = ?`, prID)
		if got := cfStr(row["current_stage"]); got != "proposal" {
			t.Fatalf("project stage after pass = %q, want proposal", got)
		}
	})

	t.Run("StageGateFailAllowedWithoutArtifacts", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/stage-gates", map[string]any{
			"project_id": prID,
			"to_stage":   "define_planning",
			"artifacts":  []map[string]any{{"name": "Plan outline", "required": true}},
		})
		wantStatus(t, "POST", "/api/p3m/stage-gates (fail case)", status, body, http.StatusCreated)
		gateID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/stage-gates/%d/decision", gateID), map[string]any{"passed": false})
		wantStatus(t, "POST", "/api/p3m/stage-gates/{id}/decision (fail)", status, body, http.StatusOK)
		assertJSONField(t, body, "passed", false)

		row := dbQueryOne(t, `SELECT current_stage FROM p3m_projects WHERE id = ?`, prID)
		if got := cfStr(row["current_stage"]); got != "proposal" {
			t.Fatalf("project stage after fail = %q, want proposal (unchanged)", got)
		}
	})

	t.Run("RaciMatrixCRUD", func(t *testing.T) {
		status, body := admin.Put(t, "/api/p3m/raci", map[string]any{
			"entity_type": "project", "entity_id": prID, "user_id": uid, "role": "responsible",
		})
		wantStatus(t, "PUT", "/api/p3m/raci (create)", status, body, http.StatusOK)

		status, body = admin.Get(t, fmt.Sprintf("/api/p3m/raci?entity_type=project&entity_id=%d", prID))
		wantStatus(t, "GET", "/api/p3m/raci", status, body, http.StatusOK)
		var rowID int
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["user_id"]) == uid && cfStr(e["role"]) == "responsible" {
				rowID = cfInt(e["id"])
			}
		}
		if rowID == 0 {
			t.Fatalf("RACI matrix missing responsible assignment: %s", snippet(body))
		}

		status, body = admin.Put(t, "/api/p3m/raci", map[string]any{
			"entity_type": "project", "entity_id": prID, "user_id": uid, "role": "accountable",
		})
		wantStatus(t, "PUT", "/api/p3m/raci (upsert)", status, body, http.StatusOK)
		if n := dbCount(t, "p3m_raci_assignments", "entity_type = 'project' AND entity_id = ? AND user_id = ?", prID, uid); n != 1 {
			t.Fatalf("RACI rows for triple = %d, want exactly 1 after upsert", n)
		}
		row := dbQueryOne(t, `SELECT role FROM p3m_raci_assignments WHERE entity_type = 'project' AND entity_id = ? AND user_id = ?`, prID, uid)
		if got := cfStr(row["role"]); got != "accountable" {
			t.Fatalf("RACI role after upsert = %q, want accountable", got)
		}

		status, body = admin.Put(t, "/api/p3m/raci", map[string]any{
			"entity_type": "project", "entity_id": prID, "user_id": uid, "role": "owner",
		})
		wantStatus(t, "PUT", "/api/p3m/raci (invalid role)", status, body, http.StatusBadRequest)

		status, body = admin.Put(t, "/api/p3m/raci", map[string]any{
			"entity_type": "project", "entity_id": 99999999, "user_id": uid, "role": "responsible",
		})
		wantStatus(t, "PUT", "/api/p3m/raci (missing entity)", status, body, http.StatusNotFound)

		status, body = admin.Delete(t, fmt.Sprintf("/api/p3m/raci/%d", rowID))
		wantStatus(t, "DELETE", "/api/p3m/raci/{id}", status, body, http.StatusNoContent)
		if n := dbCount(t, "p3m_raci_assignments", "entity_type = 'project' AND entity_id = ? AND user_id = ?", prID, uid); n != 0 {
			t.Fatalf("RACI rows after delete = %d, want 0", n)
		}
	})

	t.Run("BudgetApprovalRoutingByThresholds", func(t *testing.T) {
		p3mE2eSetThreshold(t, admin, "project", 10000)
		p3mE2eSetThreshold(t, admin, "programme", 100000)

		status, body := admin.Post(t, "/api/p3m/budget-approvals", map[string]any{
			"project_id":       prID,
			"requested_amount": 58000, // delta 8000 ≤ project threshold → auto-approved and applied
		})
		wantStatus(t, "POST", "/api/p3m/budget-approvals (auto)", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "approved")
		row := dbQueryOne(t, `SELECT budget_approved FROM p3m_projects WHERE id = ?`, prID)
		if got := p3mE2eF(row["budget_approved"]); !p3mE2eApprox(got, 58000) {
			t.Fatalf("project budget after auto-approval = %v, want 58000", got)
		}

		status, body = admin.Post(t, "/api/p3m/budget-approvals", map[string]any{
			"project_id":       prID,
			"requested_amount": 140000, // delta 82000 → programme tier
		})
		wantStatus(t, "POST", "/api/p3m/budget-approvals (routed)", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "pending")
		assertJSONField(t, body, "routed_to_level", "programme")
		apprID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/budget-approvals/%d/decision", apprID), map[string]any{
			"passed": true,
			"notes":  "e2e board approval",
		})
		wantStatus(t, "POST", "/api/p3m/budget-approvals/{id}/decision", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "approved")
		row = dbQueryOne(t, `SELECT budget_approved FROM p3m_projects WHERE id = ?`, prID)
		if got := p3mE2eF(row["budget_approved"]); !p3mE2eApprox(got, 140000) {
			t.Fatalf("project budget after decision = %v, want 140000", got)
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/budget-approvals/%d/decision", apprID), map[string]any{"passed": true})
		wantStatus(t, "POST", "/api/p3m/budget-approvals/{id}/decision (repeat)", status, body, http.StatusConflict)

		status, body = admin.Post(t, "/api/p3m/budget-approvals", map[string]any{
			"project_id":       prID,
			"requested_amount": 140000, // not above the current budget
		})
		wantStatus(t, "POST", "/api/p3m/budget-approvals (not exceeding)", status, body, http.StatusBadRequest)
	})
}

// UC-M3 — Benefits and financials: benefit register + realizations, immutable baseline snapshots
// with visible delta, tranche forecast consistent with DB sums of approved/released tranches.
func TestP3MBenefitsAndFinancials(t *testing.T) {
	admin := e2eEnv.admin
	uid := createUser(t, "e2e-p3m-m3-user@windshift.test", "e2ep3mm3user", "E2eP3mM3User!1")

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M M3 Portfolio")
	prID := p3mE2eCreateProject(t, admin, "E2E P3M M3 Project", map[string]any{
		"portfolio_id":    pfID,
		"budget_approved": 100000,
	})

	t.Run("BenefitRegisterAndRealization", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/benefits", map[string]any{
			"project_id":              prID,
			"title":                   "E2E P3M M3 Cost Savings",
			"benefit_type":            "financial",
			"target_value":            250000,
			"unit_of_measure":         "GBP",
			"baseline_value":          0,
			"target_realization_date": "2027-06-30T00:00:00Z",
			"owner_id":                uid,
		})
		wantStatus(t, "POST", "/api/p3m/benefits", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "planned")
		benefitID := cfExtractID(t, body)

		status, body = admin.Post(t, "/api/p3m/benefits", map[string]any{
			"project_id": prID, "title": "E2E P3M M3 Bad Type", "benefit_type": "cosmetic",
			"target_value": 1, "unit_of_measure": "GBP", "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/benefits (invalid type)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/benefits/%d/realizations", benefitID), map[string]any{
			"recorded_date":  "2026-12-01T00:00:00Z",
			"realized_value": 90000,
			"notes":          "Q4 e2e realization",
		})
		wantStatus(t, "POST", "/api/p3m/benefits/{id}/realizations", status, body, http.StatusCreated)

		status, body = admin.Get(t, "/api/p3m/benefits")
		wantStatus(t, "GET", "/api/p3m/benefits", status, body, http.StatusOK)
		var latest float64
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["id"]) == benefitID {
				latest = p3mE2eF(e["latest_realized_value"])
			}
		}
		row := dbQueryOne(t, `SELECT COALESCE(MAX(realized_value), 0) AS v FROM p3m_benefit_realizations WHERE benefit_id = ?`, benefitID)
		wantVal := p3mE2eF(row["v"])
		if !p3mE2eApprox(latest, wantVal) || !p3mE2eApprox(wantVal, 90000) {
			t.Fatalf("latest_realized_value = %v (DB max = %v), want 90000", latest, wantVal)
		}
	})

	t.Run("BaselineSnapshotsImmutableWithDelta", func(t *testing.T) {
		status, body := admin.Post(t, fmt.Sprintf("/api/p3m/projects/%d/baselines", prID), map[string]any{"label": "Pre-change"})
		wantStatus(t, "POST", "/api/p3m/projects/{id}/baselines (1)", status, body, http.StatusCreated)
		assertJSONField(t, body, "snapshot.budget_approved", 100000)

		p3mE2eSetThreshold(t, admin, "project", 10000)
		status, body = admin.Post(t, "/api/p3m/budget-approvals", map[string]any{
			"project_id":       prID,
			"requested_amount": 108000, // delta 8000 ≤ threshold → auto-applied
		})
		wantStatus(t, "POST", "/api/p3m/budget-approvals (baseline change)", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "approved")

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/projects/%d/baselines", prID), map[string]any{"label": "Post-change"})
		wantStatus(t, "POST", "/api/p3m/projects/{id}/baselines (2)", status, body, http.StatusCreated)
		assertJSONField(t, body, "snapshot.budget_approved", 108000)

		status, body = admin.Get(t, fmt.Sprintf("/api/p3m/projects/%d/baselines", prID))
		wantStatus(t, "GET", "/api/p3m/projects/{id}/baselines", status, body, http.StatusOK)
		entries := cfListEntries(t, body) // ORDER BY id DESC → newest first
		if len(entries) != 2 {
			t.Fatalf("baseline list has %d entries, want 2: %s", len(entries), snippet(body))
		}
		snapBudget := func(e map[string]any) float64 {
			m, _ := e["snapshot"].(map[string]any)
			return p3mE2eF(m["budget_approved"])
		}
		if got := snapBudget(entries[0]); !p3mE2eApprox(got, 108000) {
			t.Fatalf("newest baseline snapshot budget = %v, want 108000", got)
		}
		if got := snapBudget(entries[1]); !p3mE2eApprox(got, 100000) {
			t.Fatalf("first baseline snapshot changed to %v, want immutable 100000", got)
		}
	})

	t.Run("TrancheForecastMatchesDBSum", func(t *testing.T) {
		now := time.Now().UTC()
		monthStart := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC) // first of next month
		nextMonth := monthStart.AddDate(0, 1, 0)

		status, body := admin.Post(t, "/api/p3m/tranches", map[string]any{
			"entity_type":      "project",
			"entity_id":        prID,
			"title":            "E2E P3M M3 Capex Tranche",
			"amount":           25000,
			"expenditure_type": "capex",
			"planned_date":     monthStart.Format(time.RFC3339),
			"status":           "approved",
		})
		wantStatus(t, "POST", "/api/p3m/tranches", status, body, http.StatusCreated)

		status, body = admin.Post(t, "/api/p3m/tranches", map[string]any{
			"entity_type":      "project",
			"entity_id":        prID,
			"title":            "E2E P3M M3 Bad Type Tranche",
			"amount":           1000,
			"expenditure_type": "capexx",
			"planned_date":     monthStart.Format(time.RFC3339),
			"status":           "approved",
		})
		wantStatus(t, "POST", "/api/p3m/tranches (invalid type)", status, body, http.StatusBadRequest)

		status, body = admin.Get(t, fmt.Sprintf("/api/p3m/forecast?from=%s&to=%s",
			monthStart.Format("2006-01-02"), nextMonth.AddDate(0, 0, -1).Format("2006-01-02")))
		wantStatus(t, "GET", "/api/p3m/forecast", status, body, http.StatusOK)

		var capex float64
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfStr(e["month"]) == monthStart.Format("2006-01") {
				capex = p3mE2eF(e["capex_total"])
				found = true
			}
		}
		if !found {
			t.Fatalf("forecast missing month %s: %s", monthStart.Format("2006-01"), snippet(body))
		}

		row := dbQueryOne(t, `SELECT COALESCE(SUM(amount), 0) AS v FROM p3m_budget_tranches
			WHERE status IN ('approved', 'released') AND planned_date >= ? AND planned_date < ?`,
			monthStart.Format("2006-01-02"), nextMonth.Format("2006-01-02"))
		wantVal := p3mE2eF(row["v"])
		if !p3mE2eApprox(capex, wantVal) {
			t.Fatalf("forecast capex_total = %v, DB sum of approved/released tranches in month = %v", capex, wantVal)
		}
	})
}

// UC-M4 — RAID lifecycle: risk/issue register with score validation; escalation copies a
// project-level item to its parent tier and marks both rows escalated; non-project items
// cannot be escalated and orphan projects have no target.
func TestP3MRaidLifecycle(t *testing.T) {
	admin := e2eEnv.admin
	uid := createUser(t, "e2e-p3m-m4-user@windshift.test", "e2ep3mm4user", "E2eP3mM4User!1")

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M M4 Portfolio")
	pgID := p3mE2eCreateProgramme(t, admin, pfID, "E2E P3M M4 Programme")
	prID := p3mE2eCreateProject(t, admin, "E2E P3M M4 Project", map[string]any{"programme_id": pgID})

	t.Run("RaidItemValidation", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "project", "entity_id": prID, "item_type": "risk",
			"title": "E2E P3M M4 Risk", "impact_score": 6, "probability_score": 3, "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (impact out of range)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "project", "entity_id": prID, "item_type": "hazard",
			"title": "E2E P3M M4 Bad Type", "impact_score": 3, "probability_score": 3, "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (invalid item_type)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "project", "entity_id": prID, "item_type": "risk",
			"title": "E2E P3M M4 No Owner", "impact_score": 3, "probability_score": 3, "owner_id": 99999999,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (missing owner)", status, body, http.StatusNotFound)

		status, body = admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type":       "project",
			"entity_id":         prID,
			"item_type":         "risk",
			"title":             "E2E P3M M4 Risk",
			"description":       "e2e risk description",
			"impact_score":      4,
			"probability_score": 3,
			"owner_id":          uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "open")

		row := dbQueryOne(t, `SELECT entity_type, item_type FROM p3m_raid_items WHERE id = ?`, cfExtractID(t, body))
		if got := cfStr(row["entity_type"]); got != "project" {
			t.Fatalf("DB raid entity_type = %q, want project", got)
		}
	})

	t.Run("EscalationCopiesToParentTier", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "project", "entity_id": prID, "item_type": "issue",
			"title": "E2E P3M M4 Escalatable Issue", "impact_score": 5, "probability_score": 5, "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (escalation source)", status, body, http.StatusCreated)
		srcID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/raid-items/%d/escalate", srcID), map[string]any{})
		wantStatus(t, "POST", "/api/p3m/raid-items/{id}/escalate", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "escalated")
		assertJSONField(t, body, "entity_type", "programme") // project's parent tier
		copyID := cfExtractID(t, body)

		row := dbQueryOne(t, `SELECT status FROM p3m_raid_items WHERE id = ?`, srcID)
		if got := cfStr(row["status"]); got != "escalated" {
			t.Fatalf("source raid item status after escalation = %q, want escalated", got)
		}
		row = dbQueryOne(t, `SELECT entity_type, entity_id FROM p3m_raid_items WHERE id = ?`, copyID)
		if cfStr(row["entity_type"]) != "programme" || cfInt(row["entity_id"]) != pgID {
			t.Fatalf("escalation copy points at %v#%v, want programme#%d", row["entity_type"], row["entity_id"], pgID)
		}

		status, body = admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "programme", "entity_id": pgID, "item_type": "risk",
			"title": "E2E P3M M4 Programme Risk", "impact_score": 3, "probability_score": 3, "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (programme level)", status, body, http.StatusCreated)
		pgItemID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/raid-items/%d/escalate", pgItemID), map[string]any{})
		wantStatus(t, "POST", "/api/p3m/raid-items/{id}/escalate (non-project)", status, body, http.StatusBadRequest)

		// The API requires every project to have a portfolio or programme parent, so the
		// no-parent state is seeded directly (legacy/migration data can still carry it).
		res, err := e2eEnv.db.Exec(`INSERT INTO p3m_projects (title) VALUES (?)`, "E2E P3M M4 Orphan Project")
		if err != nil {
			t.Fatal(err)
		}
		orphanID64, _ := res.LastInsertId()
		orphanID := int(orphanID64)
		status, body = admin.Post(t, "/api/p3m/raid-items", map[string]any{
			"entity_type": "project", "entity_id": orphanID, "item_type": "issue",
			"title": "E2E P3M M4 Orphan Issue", "impact_score": 5, "probability_score": 5, "owner_id": uid,
		})
		wantStatus(t, "POST", "/api/p3m/raid-items (orphan)", status, body, http.StatusCreated)
		orphanItemID := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/raid-items/%d/escalate", orphanItemID), map[string]any{})
		wantStatus(t, "POST", "/api/p3m/raid-items/{id}/escalate (no parent)", status, body, http.StatusConflict)
	})
}

// UC-M5 — Investment cases: one case per project with score validation; terminal statuses only via the
// decision endpoint (which stamps decision_date and appends dated notes); list filters match DB counts;
// CSV export carries project data.
func TestP3MInvestmentCases(t *testing.T) {
	admin := e2eEnv.admin

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M M5 Portfolio")
	prA := p3mE2eCreateProject(t, admin, "E2E P3M M5 Project A", map[string]any{"portfolio_id": pfID})
	prB := p3mE2eCreateProject(t, admin, "E2E P3M M5 Project B", map[string]any{"portfolio_id": pfID})

	var caseA, caseB int

	t.Run("CaseCreationAndDuplicateRule", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/investment-cases", map[string]any{
			"project_id":    prA,
			"business_case": "E2E P3M M5 case A business case",
			"benefit_score": 4,
			"risk_score":    2,
		})
		wantStatus(t, "POST", "/api/p3m/investment-cases (A)", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "proposed")
		caseA = cfExtractID(t, body)

		status, body = admin.Post(t, "/api/p3m/investment-cases", map[string]any{"project_id": prA})
		wantStatus(t, "POST", "/api/p3m/investment-cases (duplicate)", status, body, http.StatusConflict)

		status, body = admin.Post(t, "/api/p3m/investment-cases", map[string]any{
			"project_id":    prB,
			"business_case": "E2E P3M M5 case B business case",
		})
		wantStatus(t, "POST", "/api/p3m/investment-cases (B)", status, body, http.StatusCreated)
		caseB = cfExtractID(t, body)

		row := dbQueryOne(t, `SELECT benefit_score, risk_score FROM p3m_investment_cases WHERE id = ?`, caseA)
		if got := cfInt(row["benefit_score"]); got != 4 {
			t.Fatalf("DB benefit_score = %d, want 4", got)
		}
		row = dbQueryOne(t, `SELECT benefit_score, risk_score FROM p3m_investment_cases WHERE id = ?`, caseB)
		if cfInt(row["benefit_score"]) != 3 || cfInt(row["risk_score"]) != 3 {
			t.Fatalf("DB default scores for case B = %v/%v, want 3/3", row["benefit_score"], row["risk_score"])
		}
	})

	t.Run("StatusTransitionsAndDecision", func(t *testing.T) {
		status, body := admin.Put(t, fmt.Sprintf("/api/p3m/investment-cases/%d", caseA), map[string]any{
			"status": "approved", // terminal statuses are set by the decision endpoint only
		})
		wantStatus(t, "PUT", "/api/p3m/investment-cases/{id} (terminal status)", status, body, http.StatusBadRequest)

		status, body = admin.Put(t, fmt.Sprintf("/api/p3m/investment-cases/%d", caseA), map[string]any{"status": "under_review"})
		wantStatus(t, "PUT", "/api/p3m/investment-cases/{id} (under_review)", status, body, http.StatusOK)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/investment-cases/%d/decision", caseA), map[string]any{
			"outcome": "vetoed",
		})
		wantStatus(t, "POST", "/api/p3m/investment-cases/{id}/decision (invalid outcome)", status, body, http.StatusBadRequest)

		today := time.Now().UTC().Format("2006-01-02")
		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/investment-cases/%d/decision", caseA), map[string]any{
			"outcome": "approved",
			"notes":   "e2e board sign-off",
		})
		wantStatus(t, "POST", "/api/p3m/investment-cases/{id}/decision (approve)", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "approved")

		row := dbQueryOne(t, `SELECT status, strftime('%Y-%m-%d', decision_date) AS decision_date, business_case FROM p3m_investment_cases WHERE id = ?`, caseA)
		if got := cfStr(row["status"]); got != "approved" {
			t.Fatalf("DB status after decision = %q, want approved", got)
		}
		if got := cfStr(row["decision_date"]); got != today {
			t.Fatalf("DB decision_date = %q, want %s (today UTC)", got, today)
		}
		if bc := cfStr(row["business_case"]); !strings.Contains(bc, "["+today+"] e2e board sign-off") {
			t.Fatalf("business_case missing dated notes: %q", bc)
		}
	})

	t.Run("ListFilterMatchesDB", func(t *testing.T) {
		status, body := admin.Get(t, fmt.Sprintf("/api/p3m/investment-cases?status=proposed&portfolio_id=%d", pfID))
		wantStatus(t, "GET", "/api/p3m/investment-cases (filtered)", status, body, http.StatusOK)
		entries := cfListEntries(t, body)
		if len(entries) != 1 || cfInt(entries[0]["id"]) != caseB {
			t.Fatalf("filtered list = %d entries (first id %v), want exactly case %d: %s",
				len(entries), firstEntryID(entries), caseB, snippet(body))
		}

		counts := p3mE2ePipelineCounts(t, pfID)
		if counts["proposed"] != 1 || counts["approved"] != 1 {
			t.Fatalf("DB pipeline counts = %v, want proposed=1 approved=1", counts)
		}
	})

	t.Run("CSVExport", func(t *testing.T) {
		status, body := admin.Get(t, "/api/p3m/export?scope=investment_cases")
		wantStatus(t, "GET", "/api/p3m/export (investment_cases)", status, body, http.StatusOK)
		if !strings.Contains(string(body), "E2E P3M M5 Project A") {
			t.Fatalf("CSV export missing project title: %s", snippet(body))
		}

		status, body = admin.Get(t, "/api/p3m/export?scope=galaxies")
		wantStatus(t, "GET", "/api/p3m/export (invalid scope)", status, body, http.StatusBadRequest)
	})
}

// UC-M6 — Resources and analytics: resource CRUD with validation; allocation rules (bau vs project);
// capacity view demand/capacity consistent with DB sums over the month; CPM critical path over dated
// items in a linked workspace.
func TestP3MResourcesAndAnalytics(t *testing.T) {
	admin := e2eEnv.admin

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M M6 Portfolio")
	prID := p3mE2eCreateProject(t, admin, "E2E P3M M6 Project", map[string]any{"portfolio_id": pfID})

	var resID int

	t.Run("ResourceCRUD", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/resources", map[string]any{
			"name": "E2E P3M M6 Engineer", "cost_rate": 150, "capacity_hours_per_week": 40,
		})
		wantStatus(t, "POST", "/api/p3m/resources", status, body, http.StatusCreated)
		assertJSONField(t, body, "name", "E2E P3M M6 Engineer")
		resID = cfExtractID(t, body)

		status, body = admin.Post(t, "/api/p3m/resources", map[string]any{
			"name": "E2E P3M M6 Negative Rate", "cost_rate": -5, "capacity_hours_per_week": 40,
		})
		wantStatus(t, "POST", "/api/p3m/resources (negative cost)", status, body, http.StatusBadRequest)

		status, body = admin.Put(t, fmt.Sprintf("/api/p3m/resources/%d", resID), map[string]any{
			"name": "E2E P3M M6 Engineer Sr", "cost_rate": 175, "capacity_hours_per_week": 40,
		})
		wantStatus(t, "PUT", "/api/p3m/resources/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "name", "E2E P3M M6 Engineer Sr")

		row := dbQueryOne(t, `SELECT cost_rate FROM p3m_resources WHERE id = ?`, resID)
		if got := p3mE2eF(row["cost_rate"]); !p3mE2eApprox(got, 175) {
			t.Fatalf("DB cost_rate after update = %v, want 175", got)
		}

		status, body = admin.Post(t, "/api/p3m/resources", map[string]any{
			"name": "E2E P3M M6 Scratch", "cost_rate": 0, "capacity_hours_per_week": 8,
		})
		wantStatus(t, "POST", "/api/p3m/resources (scratch)", status, body, http.StatusCreated)
		scratchID := cfExtractID(t, body)

		status, body = admin.Delete(t, fmt.Sprintf("/api/p3m/resources/%d", scratchID))
		wantStatus(t, "DELETE", "/api/p3m/resources/{id}", status, body, http.StatusNoContent)
		if n := dbCount(t, "p3m_resources", "id = ?", scratchID); n != 0 {
			t.Fatalf("scratch resource still in DB after delete")
		}
	})

	t.Run("AllocationsAndCapacityView", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/resource-allocations", map[string]any{
			"resource_id":    resID,
			"project_id":     prID,
			"start_date":     "2027-03-01T00:00:00Z",
			"end_date":       "2027-04-01T00:00:00Z", // half-open coverage of March
			"hours_per_week": 20,
			"booking_type":   "hard",
		})
		wantStatus(t, "POST", "/api/p3m/resource-allocations (project)", status, body, http.StatusCreated)

		status, body = admin.Post(t, "/api/p3m/resource-allocations", map[string]any{
			"resource_id":    resID,
			"start_date":     "2027-03-01T00:00:00Z",
			"end_date":       "2027-04-01T00:00:00Z",
			"hours_per_week": 10,
			"allocation_class": "bau",
		})
		wantStatus(t, "POST", "/api/p3m/resource-allocations (bau)", status, body, http.StatusCreated)

		status, body = admin.Post(t, "/api/p3m/resource-allocations", map[string]any{
			"resource_id":    resID,
			"project_id":     prID,
			"start_date":     "2027-04-01T00:00:00Z",
			"end_date":       "2027-03-01T00:00:00Z", // end before start
			"hours_per_week": 5,
		})
		wantStatus(t, "POST", "/api/p3m/resource-allocations (inverted dates)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/p3m/resource-allocations", map[string]any{
			"resource_id":      resID,
			"project_id":       prID, // bau allocations must not reference a project
			"start_date":       "2027-03-01T00:00:00Z",
			"end_date":         "2027-03-15T00:00:00Z",
			"hours_per_week":   5,
			"allocation_class": "bau",
		})
		wantStatus(t, "POST", "/api/p3m/resource-allocations (bau with project)", status, body, http.StatusBadRequest)

		status, body = admin.Get(t, "/api/p3m/resource-capacity?from=2027-03-01&to=2027-03-31")
		wantStatus(t, "GET", "/api/p3m/resource-capacity", status, body, http.StatusOK)

		var row map[string]any
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["resource_id"]) == resID && cfStr(e["month"]) == "2027-03" {
				row = e
			}
		}
		if row == nil {
			t.Fatalf("capacity view missing resource %d for 2027-03: %s", resID, snippet(body))
		}

		dbRow := dbQueryOne(t, `SELECT COALESCE(SUM(CASE WHEN allocation_class = 'bau' THEN hours_per_week ELSE 0 END), 0) AS bau_h,
			COALESCE(SUM(hours_per_week), 0) AS total_h FROM p3m_resource_allocations
			WHERE resource_id = ? AND start_date <= '2027-03-31' AND end_date > '2027-03-01'`, resID)
		capRow := dbQueryOne(t, `SELECT capacity_hours_per_week FROM p3m_resources WHERE id = ?`, resID)
		const days float64 = 31 // March
		wantDemand := p3mE2eF(dbRow["total_h"]) * days / 7.0 // BAU consumes capacity (RES-004)
		wantBau := p3mE2eF(dbRow["bau_h"]) * days / 7.0
		wantCap := p3mE2eF(capRow["capacity_hours_per_week"]) * days / 7.0

		if got := p3mE2eF(row["demand_hours"]); !p3mE2eApprox(got, wantDemand) {
			t.Fatalf("demand_hours = %v, DB-derived = %v", got, wantDemand)
		}
		if got := p3mE2eF(row["bau_hours"]); !p3mE2eApprox(got, wantBau) {
			t.Fatalf("bau_hours = %v, DB-derived = %v", got, wantBau)
		}
		if got := p3mE2eF(row["capacity_hours"]); !p3mE2eApprox(got, wantCap) {
			t.Fatalf("capacity_hours = %v, DB-derived = %v", got, wantCap)
		}
	})

	t.Run("ProjectCriticalPath", func(t *testing.T) {
		wsID := createWorkspace(t, admin, "e2ep3mc6")
		if _, err := e2eEnv.db.Exec(`INSERT INTO workspace_p3m_projects (workspace_id, p3m_project_id) VALUES (?, ?)`, wsID, prID); err != nil {
			t.Fatal(err)
		}

		mkItem := func(num int, title, start, end string) int {
			seq := atomic.AddInt64(&p3mE2eFracSeq, 1)
			res, err := e2eEnv.db.Exec(
				`INSERT INTO items (workspace_id, workspace_item_number, title, frac_index, start_date, end_date) VALUES (?, ?, ?, ?, ?, ?)`,
				wsID, num, title, fmt.Sprintf("e2ep3mm6%08d", seq), start, end)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			return int(id)
		}
		itemA := mkItem(1, "E2E P3M M6 CPM A", "2027-03-01", "2027-03-08") // 8 days inclusive
		itemB := mkItem(2, "E2E P3M M6 CPM B", "2027-03-08", "2027-03-20") // A blocks B → starts Mar 9
		itemC := mkItem(3, "E2E P3M M6 CPM C", "2027-03-01", "2027-03-04") // independent, float 17

		if _, err := e2eEnv.db.Exec(`INSERT INTO item_dependencies (source_item_id, target_item_id) VALUES (?, ?)`, itemA, itemB); err != nil {
			t.Fatal(err)
		}

		status, body := admin.Get(t, fmt.Sprintf("/api/p3m/projects/%d/critical-path", prID))
		wantStatus(t, "GET", "/api/p3m/projects/{id}/critical-path", status, body, http.StatusOK)
		root := mustJSON(t, body).(map[string]any)

		if got := cfInt(root["item_count"]); got != 3 {
			t.Fatalf("item_count = %d, want 3: %s", got, snippet(body))
		}
		if got := cfInt(root["dependency_count"]); got != 1 {
			t.Fatalf("dependency_count = %d, want 1", got)
		}
		// A finishes Mar 8 (inclusive), so B starts Mar 9 and ends Mar 21: span Mar 1..21.
		if got := p3mE2eF(root["critical_path_days"]); !p3mE2eApprox(got, 21) {
			t.Fatalf("critical_path_days = %v, want 21", got)
		}

		critIDs, _ := root["critical_task_ids"].([]any)
		if len(critIDs) != 2 || cfInt(critIDs[0]) != itemA || cfInt(critIDs[1]) != itemB {
			t.Fatalf("critical_task_ids = %v, want [%d %d] in topological order", critIDs, itemA, itemB)
		}

		var cTask map[string]any
		for _, tv := range root["tasks"].([]any) {
			tm := tv.(map[string]any)
			if cfInt(tm["item_id"]) == itemC {
				cTask = tm
			}
		}
		if cTask == nil {
			t.Fatalf("critical path tasks missing independent item %d: %s", itemC, snippet(body))
		}
		if got := p3mE2eF(cTask["total_float_days"]); !p3mE2eApprox(got, 17) {
			t.Fatalf("independent task total_float_days = %v, want 17", got)
		}
		if b, _ := cTask["is_critical"].(bool); b {
			t.Fatalf("independent task marked critical")
		}
	})
}

// firstEntryID returns the id of the first list entry (0 when empty).
func firstEntryID(entries []map[string]any) int {
	if len(entries) == 0 {
		return 0
	}
	return cfInt(entries[0]["id"])
}
