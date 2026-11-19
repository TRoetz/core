package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// UC-M1/M4 regressions and the Front Door intake pack: RAID rollups across a
// portfolio/programme with child projects, and the capture→assess→decide flow
// (mandatory rationale, decision history, proceed creating a governed project).

func p3mE2eRollupJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("rollup response is not a JSON object: %v", err)
	}
	return m
}

func p3mE2eRollupInt(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	f, ok := m[key].(float64)
	if !ok {
		t.Fatalf("rollup field %q missing or not a number: %#v", key, m[key])
	}
	return int(f)
}

func TestP3MRaidRollup(t *testing.T) {
	admin := e2eEnv.admin
	uid := createUser(t, "e2e-p3m-rollup-user@windshift.test", "e2ep3mrollupuser", "E2eP3mRollup!1")

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M Rollup Portfolio")
	pgID := p3mE2eCreateProgramme(t, admin, pfID, "E2E P3M Rollup Programme")
	prID := p3mE2eCreateProject(t, admin, "E2E P3M Rollup Project", map[string]any{"programme_id": pgID})

	status, body := admin.Post(t, "/api/p3m/raid-items", map[string]any{
		"entity_type":       "project",
		"entity_id":         prID,
		"item_type":         "risk",
		"title":             "E2E P3M Rollup Risk",
		"description":       "open risk on a child project",
		"impact_score":      4,
		"probability_score": 3,
		"owner_id":          uid,
	})
	wantStatus(t, "POST", "/api/p3m/raid-items (rollup source)", status, body, http.StatusCreated)
	raidID := cfExtractID(t, body)

	t.Run("PortfolioRollupIncludesChildProjectRisks", func(t *testing.T) {
		status, body := admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/raid-rollup", pfID))
		wantStatus(t, "GET", "/api/p3m/portfolios/{id}/raid-rollup", status, body, http.StatusOK)

		m := p3mE2eRollupJSON(t, body)
		if got := p3mE2eRollupInt(t, m, "total_open"); got != 1 {
			t.Fatalf("portfolio rollup total_open = %d, want 1 (child project risk must be rolled up)", got)
		}
		byType, _ := m["by_item_type"].(map[string]any)
		if got, _ := byType["risk"].(float64); int(got) != 1 {
			t.Fatalf("portfolio rollup by_item_type.risk = %v, want 1", byType["risk"])
		}
		topRisks, _ := m["top_risks"].([]any)
		if len(topRisks) != 1 {
			t.Fatalf("portfolio rollup top_risks has %d entries, want 1", len(topRisks))
		}
		first, _ := topRisks[0].(map[string]any)
		if id, _ := first["id"].(float64); int(id) != raidID {
			t.Fatalf("portfolio rollup top_risks[0].id = %v, want %d", first["id"], raidID)
		}
	})

	t.Run("ProgrammeRollupIncludesChildProjectRisks", func(t *testing.T) {
		status, body := admin.Get(t, fmt.Sprintf("/api/p3m/programmes/%d/raid-rollup", pgID))
		wantStatus(t, "GET", "/api/p3m/programmes/{id}/raid-rollup", status, body, http.StatusOK)

		m := p3mE2eRollupJSON(t, body)
		if got := p3mE2eRollupInt(t, m, "total_open"); got != 1 {
			t.Fatalf("programme rollup total_open = %d, want 1 (child project risk must be rolled up)", got)
		}
	})

	t.Run("EmptyPortfolioRollupIsZero", func(t *testing.T) {
		emptyPF := p3mE2eCreatePortfolio(t, admin, "E2E P3M Rollup Empty Portfolio")
		status, body := admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/raid-rollup", emptyPF))
		wantStatus(t, "GET", "/api/p3m/portfolios/{id}/raid-rollup (no children)", status, body, http.StatusOK)

		m := p3mE2eRollupJSON(t, body)
		if got := p3mE2eRollupInt(t, m, "total_open"); got != 0 {
			t.Fatalf("empty portfolio rollup total_open = %d, want 0", got)
		}
	})
}

// p3mE2eFullMedIntake returns a front-door intake body completing every field
// required by the seeded "med" complexity screen.
func p3mE2eFullMedIntake(title string) map[string]any {
	return map[string]any{
		"title":                 title,
		"description":           "e2e front door intake",
		"problem_opportunity":   "demo problem statement",
		"strategic_alignment":   "aligned with e2e strategy",
		"intended_outcomes":     "ship the demo outcome",
		"resource_requirements": "1 FTE for a month",
		"risk_notes":            "low risk, reversible",
		"business_group":        "IT",
		"work_type":             "project",
		"complexity":            "med",
	}
}

func TestP3MFrontDoorIntake(t *testing.T) {
	admin := e2eEnv.admin
	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M Front Door Portfolio")

	t.Run("CaptureAndDeclineWithRationale", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/front-door", p3mE2eFullMedIntake("E2E FD Declined Idea"))
		wantStatus(t, "POST", "/api/p3m/front-door", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "captured")
		id := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/front-door/%d/decision", id), map[string]any{
			"decision": "decline",
		})
		wantStatus(t, "POST", "/api/p3m/front-door/{id}/decision (no rationale)", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/front-door/%d/decision", id), map[string]any{
			"decision":  "decline",
			"rationale": "duplicate of an existing capability",
		})
		wantStatus(t, "POST", "/api/p3m/front-door/{id}/decision (decline)", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "decided")
		assertJSONField(t, body, "decision", "decline")

		row := dbQueryOne(t, `SELECT decision, rationale, decided_by FROM p3m_intakes WHERE id = ?`, id)
		if cfStr(row["rationale"]) != "duplicate of an existing capability" {
			t.Fatalf("DB intake rationale = %q, want the recorded rationale", cfStr(row["rationale"]))
		}
		if cfInt(row["decided_by"]) == 0 {
			t.Fatal("DB intake decided_by is empty; every decision must record who decided")
		}

		status, body = admin.Get(t, "/api/p3m/front-door?decision=decline")
		wantStatus(t, "GET", "/api/p3m/front-door?decision=", status, body, http.StatusOK)
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["id"]) == id && cfStr(e["decision"]) == "decline" {
				found = true
			}
		}
		if !found {
			t.Fatal("decision history (?decision=decline) does not include the declined intake")
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/front-door/%d/decision", id), map[string]any{
			"decision":  "proceed",
			"rationale": "changed my mind",
		})
		wantStatus(t, "POST", "/api/p3m/front-door/{id}/decision (re-decide)", status, body, http.StatusConflict)
	})

	t.Run("ProceedCreatesGovernedProject", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/front-door", p3mE2eFullMedIntake("E2E FD Approved Idea"))
		wantStatus(t, "POST", "/api/p3m/front-door (proceed source)", status, body, http.StatusCreated)
		id := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/front-door/%d/decision", id), map[string]any{
			"decision":      "proceed",
			"rationale":     "fits the portfolio strategy",
			"conditions":    "budget cap at 10k; monthly review",
			"portfolio_id":  pfID,
		})
		wantStatus(t, "POST", "/api/p3m/front-door/{id}/decision (proceed)", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "decided")

		var decided map[string]any
		if err := json.Unmarshal(body, &decided); err != nil {
			t.Fatalf("proceed response is not a JSON object: %v", err)
		}
		targetID, ok := decided["target_project_id"].(float64)
		if !ok || int(targetID) == 0 {
			t.Fatalf("proceed decision did not create/link a project: %#v", decided["target_project_id"])
		}
		projectID := int(targetID)

		row := dbQueryOne(t, `SELECT title, current_stage FROM p3m_projects WHERE id = ?`, projectID)
		if cfStr(row["title"]) != "E2E FD Approved Idea" {
			t.Fatalf("created project title = %q, want the intake title (capture-once pre-fill)", cfStr(row["title"]))
		}

		gate := dbQueryOne(t, `SELECT from_stage, to_stage, passed, comments FROM p3m_stage_gate_reviews WHERE project_id = ? AND passed = TRUE`, projectID)
		if gate == nil {
			t.Fatal("proceed decision did not record a passed front-door gate review")
		}
		if !strings.Contains(cfStr(gate["comments"]), "budget cap at 10k; monthly review") {
			t.Fatalf("front-door gate comments = %q, want the decision conditions", cfStr(gate["comments"]))
		}
		stage := dbQueryOne(t, `SELECT current_stage FROM p3m_projects WHERE id = ?`, projectID)
		if cfStr(stage["current_stage"]) != cfStr(gate["to_stage"]) {
			t.Fatalf("project stage %q does not match the passed gate's to_stage %q", cfStr(stage["current_stage"]), cfStr(gate["to_stage"]))
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/raid-rollup", pfID))
		wantStatus(t, "GET", "/api/p3m/portfolios/{id}/raid-rollup (after proceed)", status, body, http.StatusOK)
	})

	t.Run("MissingRequiredFieldsBlockDecision", func(t *testing.T) {
		status, body := admin.Post(t, "/api/p3m/front-door", map[string]any{
			"title":      "E2E FD Incomplete Idea",
			"complexity": "med",
		})
		wantStatus(t, "POST", "/api/p3m/front-door (sparse)", status, body, http.StatusCreated)
		id := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/p3m/front-door/%d/decision", id), map[string]any{
			"decision":  "proceed",
			"rationale": "should be blocked by the med screen",
		})
		wantStatus(t, "POST", "/api/p3m/front-door/{id}/decision (incomplete fields)", status, body, http.StatusBadRequest)
		if !strings.Contains(string(body), "missing required assessment fields") {
			t.Fatalf("expected a missing-fields validation error, got: %s", string(body))
		}
	})
}
