package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// UC-M7 - OHP portfolio report (RPT-*): assembled portfolio report, change
// calendar group impacts and ELT decision lifecycle.

// p3mReportAsOf is the report period used across these tests. The change
// window is the six months after it (2026-12 .. 2027-05).
const p3mReportAsOf = "2026-11"

// p3mE2eStageOrder is the default configurable lifecycle, in order. The exit
// stage is benefits_realisation; close is an intermediate stage that requires
// a closure record before entry.
var p3mE2eStageOrder = []string{"front_door", "proposal", "define_planning", "implement_deliver", "close", "benefits_realisation"}

// p3mE2eAdvanceProject moves a project to toStage one stage at a time (gate
// decisions reject multi-stage jumps), creating artifact-less gates.
// Note: entering "close" requires a closure record to exist beforehand
// (ClosureRecordBlockError); the caller creates it.
func p3mE2eAdvanceProject(t *testing.T, c *Client, projectID int, toStage string) {
	t.Helper()
	row := dbQueryOne(t, `SELECT current_stage FROM p3m_projects WHERE id = ?`, projectID)
	from := cfStr(row["current_stage"])
	fromIdx, toIdx := -1, -1
	for i, s := range p3mE2eStageOrder {
		if s == from {
			fromIdx = i
		}
		if s == toStage {
			toIdx = i
		}
	}
	if fromIdx < 0 || toIdx <= fromIdx {
		t.Fatalf("cannot advance project %d from %q to %q", projectID, from, toStage)
	}
	for i := fromIdx + 1; i <= toIdx; i++ {
		status, body := c.Post(t, "/api/p3m/stage-gates", map[string]any{"project_id": projectID, "to_stage": p3mE2eStageOrder[i]})
		wantStatus(t, "POST", "/api/p3m/stage-gates", status, body, http.StatusCreated)
		gateID := cfExtractID(t, body)
		status, body = c.Post(t, fmt.Sprintf("/api/p3m/stage-gates/%d/decision", gateID), map[string]any{"passed": true})
		wantStatus(t, "POST", "/api/p3m/stage-gates/{id}/decision", status, body, http.StatusOK)
	}
}

// p3mE2eReportInvestment finds the investment row with the given ID in an
// assembled portfolio report body.
func p3mE2eReportInvestment(t *testing.T, body []byte, id int) map[string]any {
	t.Helper()
	doc := mustJSON(t, body).(map[string]any)
	for _, v := range doc["investments"].([]any) {
		row := v.(map[string]any)
		if cfInt(row["id"]) == id {
			return row
		}
	}
	t.Fatalf("investment %d not found in report: %s", id, snippet(body))
	return nil
}

func TestP3MPortfolioReportAssembly(t *testing.T) {
	admin := e2eEnv.admin
	uid := createUser(t, "e2e-p3m-report-user@windshift.test", "e2ep3mreport", "E2eP3mReport!1")

	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M Report PF")
	pgID := p3mE2eCreateProgramme(t, admin, pfID, "E2E P3M Report PG")

	mkProject := func(title string, extra map[string]any) int {
		t.Helper()
		if extra == nil {
			extra = map[string]any{}
		}
		extra["programme_id"] = pgID
		return p3mE2eCreateProject(t, admin, title, extra)
	}

	prGreen := mkProject("RPT Green CE", map[string]any{
		"budget_approved":    100000.0,
		"budget_actual":      50000.0,
		"is_ce_kpi":          true,
		"portfolio_value":    "Theme A",
		"business_owner_group": "BOG A",
	})
	prRed := mkProject("RPT Red Watch", map[string]any{"budget_approved": 100000.0})
	prHold := mkProject("RPT On Hold", map[string]any{"on_hold": true, "on_hold_reason": "Funding pause"})
	prPlan := mkProject("RPT Planning", nil)
	prProp := mkProject("RPT Proposal", nil)
	prClose := mkProject("RPT Closed", nil)
	// The gate into close requires a closure record to exist beforehand.
	status, body := admin.Post(t, fmt.Sprintf("/api/p3m/projects/%d/closure-record", prClose), map[string]any{
		"lessons_learned": "E2E closure",
	})
	wantStatus(t, "POST", "/api/p3m/projects/{id}/closure-record", status, body, http.StatusCreated)

	p3mE2eAdvanceProject(t, admin, prGreen, "implement_deliver")
	p3mE2eAdvanceProject(t, admin, prRed, "implement_deliver")
	p3mE2eAdvanceProject(t, admin, prHold, "implement_deliver")
	p3mE2eAdvanceProject(t, admin, prPlan, "define_planning")
	p3mE2eAdvanceProject(t, admin, prProp, "proposal")
	p3mE2eAdvanceProject(t, admin, prClose, "benefits_realisation")

	// A project in the exit stage without a closure record: the API enforces
	// the record before close (a prerequisite of the exit stage), so this
	// state is seeded directly to prove the end_report_received flip.
	prNoReport := mkProject("RPT Closed No Report", nil)
	if _, err := e2eEnv.db.Exec(`UPDATE p3m_projects SET current_stage = 'benefits_realisation' WHERE id = ?`, prNoReport); err != nil {
		t.Fatalf("seed exit stage for project %d: %v", prNoReport, err)
	}

	// The red project was created green (no spend); overspending more than
	// 25% flips the stored RAG to red. PUT is full-replace, so carry the
	// current budget_approved forward (increases are 409 via this route).
	status, body = admin.Put(t, fmt.Sprintf("/api/p3m/projects/%d", prRed), map[string]any{
		"programme_id":  pgID,
		"title":         "RPT Red Watch",
		"budget_approved": 100000.0,
		"budget_actual":   130000.0,
	})
	wantStatus(t, "PUT", "/api/p3m/projects/{id}", status, body, http.StatusOK)

	status, body = admin.Get(t, fmt.Sprintf("/api/p3m/projects/%d/rag-history", prRed))
	wantStatus(t, "GET", "/api/p3m/projects/{id}/rag-history", status, body, http.StatusOK)
	hist := mustJSON(t, body).([]any)
	if len(hist) != 2 {
		t.Fatalf("rag-history rows = %d, want 2 (green at create, red at overspend): %s", len(hist), snippet(body))
	}
	if got := cfStr(hist[1].(map[string]any)["rag_status"]); got != "green" {
		t.Fatalf("rag-history[1] (oldest) = %q, want green", got)
	}
	if got := cfStr(hist[0].(map[string]any)["rag_status"]); got != "red" {
		t.Fatalf("rag-history[0] (newest) = %q, want red", got)
	}

	// Status report for the red project's period.
	status, body = admin.Post(t, fmt.Sprintf("/api/p3m/projects/%d/status-reports", prRed), map[string]any{
		"period": "2026-11", "issue": "Cost overrun", "impact": "Budget red", "next_steps": "Rebaseline",
	})
	wantStatus(t, "POST", "/api/p3m/projects/{id}/status-reports", status, body, http.StatusCreated)

	// Benefits: 2 projects spanning 2 CSOs, 2 owner groups, 2 categories,
	// types incl. unspecified.
	mkBenefit := func(title string, projectID int, cso, bog, cat, btype string) {
		t.Helper()
		status, body := admin.Post(t, "/api/p3m/benefits", map[string]any{
			"title":                     title,
			"project_id":                projectID,
			"benefit_type":              btype,
			"target_value":              100.0,
			"target_realization_date":   "2027-06-30T00:00:00Z",
			"owner_id":                  uid,
			"strategic_objective":       cso,
			"benefit_category":          cat,
			"business_owner_group":      bog,
		})
		wantStatus(t, "POST", "/api/p3m/benefits", status, body, http.StatusCreated)
	}
	mkBenefit("RPT Benefit 1", prGreen, "CSO 1", "BOG A", "Category X", "financial")
	mkBenefit("RPT Benefit 2", prRed, "CSO 2", "BOG B", "Category Y", "unspecified")

	// Intakes: one captured, one assessing with a responsible group.
	status, body = admin.Post(t, "/api/p3m/front-door", map[string]any{
		"title": "RPT Intake Captured", "business_group": "BG 1", "status": "captured",
	})
	wantStatus(t, "POST", "/api/p3m/front-door", status, body, http.StatusCreated)
	status, body = admin.Post(t, "/api/p3m/front-door", map[string]any{
		"title": "RPT Intake Assessing", "business_group": "BG 2", "responsible_group": "RG 1", "status": "assessing",
	})
	wantStatus(t, "POST", "/api/p3m/front-door", status, body, http.StatusCreated)

	// Change calendar: a dated entry with two group impacts spanning two
	// window months (2026-12 + 2027-01), an undated entry, and a dated
	// single-month entry with a category.
	mkCal := func(title string, extra map[string]any) int {
		t.Helper()
		req := map[string]any{"activity": title}
		for k, v := range extra {
			req[k] = v
		}
		status, body := admin.Post(t, "/api/p3m/change-calendar", req)
		wantStatus(t, "POST", "/api/p3m/change-calendar", status, body, http.StatusCreated)
		return cfExtractID(t, body)
	}
	ceSpan := mkCal("RPT Calendar Span", map[string]any{
		"start_week":   "2026-W51",
		"end_week":     "2027-W01",
		"impact_level": 4,
		"group_impacts": []map[string]any{
			{"business_group": "BG 1", "impact_level": 4},
			{"business_group": "BG 2", "impact_level": 2},
		},
	})
	mkCal("RPT Calendar Undated", map[string]any{"impact_level": 0})
	mkCal("RPT Calendar Low", map[string]any{
		"start_week":   "2027-W02",
		"end_week":     "2027-W02",
		"impact_level": 2,
		"category":     "Comms",
	})

	// Pending ELT decision on the proposal-stage project.
	status, body = admin.Post(t, "/api/p3m/elt-decisions", map[string]any{
		"portfolio_id":     pfID,
		"project_id":       prProp,
		"title":            "RPT ELT Decision",
		"why":              "Funding gate",
		"decision_requested": "Approve budget",
	})
	wantStatus(t, "POST", "/api/p3m/elt-decisions", status, body, http.StatusCreated)
	eltID := cfExtractID(t, body)

	// --- Assembled report --------------------------------------------------
	status, body = admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/portfolio-report?as_of=%s", pfID, p3mReportAsOf))
	wantStatus(t, "GET", "/api/p3m/portfolios/{id}/portfolio-report", status, body, http.StatusOK)
	doc := mustJSON(t, body).(map[string]any)

	t.Run("PositionCounts", func(t *testing.T) {
		counts := doc["position"].(map[string]any)["counts"].(map[string]any)
		if got := cfInt(counts["programmes"]); got != 1 {
			t.Errorf("counts.programmes = %d, want 1", got)
		}
		if got := cfInt(counts["projects"]); got != 7 {
			t.Errorf("counts.projects = %d, want 7", got)
		}
		// Investment set: the 5 non-exit projects + the programme.
		// Exit-stage (closed) projects appear only in the "closed" section.
		if got := cfInt(counts["approved_total"]); got != 6 {
			t.Errorf("counts.approved_total = %d, want 6", got)
		}
		if got := cfInt(counts["on_hold"]); got != 1 {
			t.Errorf("counts.on_hold = %d, want 1", got)
		}
		byStage := counts["by_stage"].(map[string]any)
		// by_stage counts the investment set only, so exit-stage projects
		// do not increment "benefits_realisation".
		for stage, want := range map[string]int{
			"front_door": 0, "proposal": 1, "define_planning": 1, "implement_deliver": 3, "close": 0, "benefits_realisation": 0,
		} {
			if got := cfInt(byStage[stage]); got != want {
				t.Errorf("counts.by_stage[%q] = %d, want %d", stage, got, want)
			}
		}
		// Display RAG is "tbd" for on-hold, non-implement-stage and
		// programme rows.
		byRag := counts["by_display_rag"].(map[string]any)
		for rag, want := range map[string]int{"green": 1, "amber": 0, "red": 1, "tbd": 4} {
			if got := cfInt(byRag[rag]); got != want {
				t.Errorf("counts.by_display_rag[%q] = %d, want %d", rag, got, want)
			}
		}
	})

	t.Run("ELTDecisionsRequired", func(t *testing.T) {
		reqs := doc["position"].(map[string]any)["elt_decisions_required"].([]any)
		if len(reqs) != 1 {
			t.Fatalf("elt_decisions_required = %d rows, want 1: %s", len(reqs), snippet(body))
		}
		if got := cfInt(reqs[0].(map[string]any)["id"]); got != eltID {
			t.Fatalf("elt_decisions_required[0].id = %d, want %d", got, eltID)
		}
	})

	t.Run("Benefits", func(t *testing.T) {
		ben := doc["benefits"].(map[string]any)
		if got := cfInt(ben["total"]); got != 2 {
			t.Errorf("benefits.total = %d, want 2", got)
		}
		byType := ben["by_type"].(map[string]any)
		if got := cfInt(byType["financial"]); got != 1 {
			t.Errorf("benefits.by_type[financial] = %d, want 1", got)
		}
		if got := cfInt(byType["unspecified"]); got != 1 {
			t.Errorf("benefits.by_type[unspecified] = %d, want 1", got)
		}
		findRow := func(rows []any, key, want string) map[string]any {
			for _, v := range rows {
				row := v.(map[string]any)
				if cfStr(row[key]) == want {
					return row
				}
			}
			t.Fatalf("%s %q row not found: %s", key, want, snippet(body))
			return nil
		}
		cso1 := findRow(ben["by_cso"].([]any), "cso", "CSO 1")
		if got := cfInt(cso1["total"]); got != 1 {
			t.Errorf("by_cso[CSO 1].total = %d, want 1", got)
		}
		if got := cfInt(findRow(ben["by_cso"].([]any), "cso", "CSO 2")["total"]); got != 1 {
			t.Errorf("by_cso[CSO 2].total = %d, want 1", got)
		}
		bogA := findRow(ben["by_owner_group_and_category"].([]any), "owner_group", "BOG A")
		if got := cfInt(bogA["categories"].(map[string]any)["Category X"]); got != 1 {
			t.Errorf("by_owner_group_and_category[BOG A][Category X] = %d, want 1", got)
		}
		bogB := findRow(ben["by_owner_group_and_category"].([]any), "owner_group", "BOG B")
		if got := cfInt(bogB["categories"].(map[string]any)["Category Y"]); got != 1 {
			t.Errorf("by_owner_group_and_category[BOG B][Category Y] = %d, want 1", got)
		}
	})

	t.Run("AtRisk", func(t *testing.T) {
		atRisk := doc["at_risk"].([]any)
		if len(atRisk) != 1 {
			t.Fatalf("at_risk = %d rows, want exactly the red project: %s", len(atRisk), snippet(body))
		}
		row := atRisk[0].(map[string]any)
		if got := cfInt(row["id"]); got != prRed {
			t.Fatalf("at_risk[0].id = %d, want %d", got, prRed)
		}
		if got := cfStr(row["display_rag"]); got != "red" {
			t.Fatalf("at_risk[0].display_rag = %q, want red", got)
		}
		if got := cfInt(row["duration_in_status_months"]); got < 1 {
			t.Errorf("at_risk[0].duration_in_status_months = %d, want >= 1", got)
		}
		sr := row["status_report"].(map[string]any)
		if got := cfStr(sr["period"]); got != "2026-11" {
			t.Errorf("at_risk[0].status_report.period = %q, want 2026-11", got)
		}
		if got := cfStr(sr["issue"]); got != "Cost overrun" {
			t.Errorf("at_risk[0].status_report.issue = %q, want Cost overrun", got)
		}
		tols := row["tolerances_at_risk"].([]any)
		if len(tols) == 0 {
			t.Fatalf("at_risk[0].tolerances_at_risk is empty, want the budget overrun category")
		}
		sawBudget := false
		for _, v := range tols {
			tol := v.(map[string]any)
			if got := cfStr(tol["rag"]); got == "green" {
				t.Errorf("tolerances_at_risk contains green category %q", tol["category"])
			}
			if cfStr(tol["category"]) == "budget" {
				sawBudget = true
			}
		}
		if !sawBudget {
			t.Errorf("tolerances_at_risk missing the budget category: %s", snippet(body))
		}
	})

	t.Run("PipelineByTheme", func(t *testing.T) {
		themes := doc["dashboard"].(map[string]any)["pipeline_by_theme"].([]any)
		findTheme := func(name string) map[string]any {
			for _, v := range themes {
				if cfStr(v.(map[string]any)["theme"]) == name {
					return v.(map[string]any)
				}
			}
			t.Fatalf("theme %q not found: %s", name, snippet(body))
			return nil
		}
		themeA := findTheme("Theme A")
		if got := cfInt(themeA["active"]); got != 1 {
			t.Errorf("Theme A.active = %d, want 1", got)
		}
		if got := cfInt(themeA["total"]); got != 1 {
			t.Errorf("Theme A.total = %d, want 1", got)
		}
		// Unspecified: red (active) + planning + awaiting-ELT projects, the
		// on-hold project (in the total, in no bucket) and the programme
		// (programmes carry no theme field).
		unspec := findTheme("Unspecified")
		for field, want := range map[string]int{"planning": 1, "awaiting_elt": 1, "active": 1, "closed": 0, "total": 5} {
			if got := cfInt(unspec[field]); got != want {
				t.Errorf("Unspecified.%s = %d, want %d", field, got, want)
			}
		}
	})

	t.Run("ChangeDashboard", func(t *testing.T) {
		cd := doc["change_dashboard"].(map[string]any)
		months := cd["months"].([]any)
		if len(months) != 6 || cfStr(months[0]) != "2026-12" || cfStr(months[5]) != "2027-05" {
			t.Fatalf("change_dashboard.months = %v, want 2026-12..2027-05", months)
		}
		if got := cfInt(cd["known_initiatives"]); got < 3 {
			t.Errorf("change_dashboard.known_initiatives = %d, want >= 3", got)
		}
		findMonth := func(m string) map[string]any {
			for _, v := range cd["active_by_month"].([]any) {
				if cfStr(v.(map[string]any)["month"]) == m {
					return v.(map[string]any)
				}
			}
			t.Fatalf("active_by_month %q not found: %s", m, snippet(body))
			return nil
		}
		if got := cfInt(findMonth("2026-12")["active"]); got < 1 {
			t.Errorf("active_by_month[2026-12].active = %d, want >= 1 (spanning entry)", got)
		}
		if got := cfInt(findMonth("2027-01")["active"]); got < 2 {
			t.Errorf("active_by_month[2027-01].active = %d, want >= 2 (spanning + low entry)", got)
		}
		var mh map[string]any
		for _, v := range cd["medium_high"].([]any) {
			if cfInt(v.(map[string]any)["entry_id"]) == ceSpan {
				mh = v.(map[string]any)
			}
		}
		if mh == nil {
			t.Fatalf("medium_high missing the level-4 entry %d: %s", ceSpan, snippet(body))
		}
		var bg1 map[string]any
		for _, v := range mh["groups"].([]any) {
			if cfStr(v.(map[string]any)["group"]) == "BG 1" {
				bg1 = v.(map[string]any)
			}
		}
		if bg1 == nil {
			t.Fatalf("medium_high[ceSpan].groups missing BG 1 (impact 4): %s", snippet(body))
		}
		if got := cfInt(bg1["impact_level"]); got != 4 {
			t.Errorf("medium_high BG 1 impact_level = %d, want 4", got)
		}
		mhMonths := []string{}
		for _, v := range bg1["months"].([]any) {
			mhMonths = append(mhMonths, cfStr(v))
		}
		if strings.Join(mhMonths, ",") != "2026-12,2027-01" {
			t.Errorf("medium_high BG 1 months = %v, want [2026-12 2027-01]", mhMonths)
		}
	})

	t.Run("ChangeCalendar", func(t *testing.T) {
		cc := doc["change_calendar"].(map[string]any)
		findDated := func(activity string) map[string]any {
			for _, v := range cc["dated"].([]any) {
				if cfStr(v.(map[string]any)["activity"]) == activity {
					return v.(map[string]any)
				}
			}
			t.Fatalf("dated calendar entry %q not found: %s", activity, snippet(body))
			return nil
		}
		span := findDated("RPT Calendar Span")
		spanMonths := []string{}
		for _, v := range span["months"].([]any) {
			spanMonths = append(spanMonths, cfStr(v))
		}
		if strings.Join(spanMonths, ",") != "2026-12,2027-01" {
			t.Errorf("calendar span months = %v, want [2026-12 2027-01]", spanMonths)
		}
		if got := len(span["group_impacts"].([]any)); got != 2 {
			t.Errorf("calendar span group_impacts = %d, want 2", got)
		}
		low := findDated("RPT Calendar Low")
		if got := cfStr(low["category"]); got != "Comms" {
			t.Errorf("calendar low category = %q, want Comms", got)
		}
		var undatedFound bool
		for _, v := range cc["undated"].([]any) {
			if cfStr(v.(map[string]any)["activity"]) == "RPT Calendar Undated" {
				undatedFound = true
			}
		}
		if !undatedFound {
			t.Errorf("undated calendar entry not found: %s", snippet(body))
		}
	})

	t.Run("IdeateAndProposals", func(t *testing.T) {
		titles := map[string]string{}
		for _, v := range doc["ideate"].([]any) {
			row := v.(map[string]any)
			titles[cfStr(row["title"])] = cfStr(row["responsible_group"])
		}
		if _, ok := titles["RPT Intake Captured"]; !ok {
			t.Errorf("ideate missing captured intake: %s", snippet(body))
		}
		if got, ok := titles["RPT Intake Assessing"]; !ok || got != "RG 1" {
			t.Errorf("ideate assessing intake responsible_group = %q (present=%v), want RG 1", got, ok)
		}
		var propFound bool
		for _, v := range doc["proposals"].([]any) {
			if cfStr(v.(map[string]any)["title"]) == "RPT Proposal" {
				propFound = true
			}
		}
		if !propFound {
			t.Errorf("proposals missing RPT Proposal: %s", snippet(body))
		}
	})

	t.Run("Investments", func(t *testing.T) {
		green := p3mE2eReportInvestment(t, body, prGreen)
		if got := green["is_ce_kpi"].(bool); !got {
			t.Errorf("investment prGreen.is_ce_kpi = false, want true")
		}
		if got := cfStr(green["display_rag"]); got != "green" {
			t.Errorf("investment prGreen.display_rag = %q, want green", got)
		}
		if got := cfStr(green["phase"]); got != "Implement / Deliver & Monitor" {
			t.Errorf("investment prGreen.phase = %q, want the implement_deliver stage name", got)
		}
		if got := cfStr(green["theme"]); got != "Theme A" {
			t.Errorf("investment prGreen.theme = %q, want Theme A", got)
		}
		if got := cfStr(green["rag_trend"]); got != "unchanged" {
			t.Errorf("investment prGreen.rag_trend = %q, want unchanged", got)
		}
		red := p3mE2eReportInvestment(t, body, prRed)
		if got := cfStr(red["display_rag"]); got != "red" {
			t.Errorf("investment prRed.display_rag = %q, want red", got)
		}
		if got := cfStr(red["rag_trend"]); got != "worsened" {
			t.Errorf("investment prRed.rag_trend = %q, want worsened (green -> red)", got)
		}
		var progRow map[string]any
		for _, v := range doc["investments"].([]any) {
			if cfStr(v.(map[string]any)["kind"]) == "programme" && cfInt(v.(map[string]any)["id"]) == pgID {
				progRow = v.(map[string]any)
			}
		}
		if progRow == nil {
			t.Fatalf("programme investment %d missing: %s", pgID, snippet(body))
		}
		if got := cfStr(progRow["display_rag"]); got != "tbd" {
			t.Errorf("programme display_rag = %q, want tbd", got)
		}
	})

	t.Run("Closed", func(t *testing.T) {
		closed := doc["closed"].([]any)
		if len(closed) != 2 {
			t.Fatalf("closed = %d rows, want 2: %s", len(closed), snippet(body))
		}
		findClosed := func(name string) map[string]any {
			for _, v := range closed {
				if cfStr(v.(map[string]any)["name"]) == name {
					return v.(map[string]any)
				}
			}
			t.Fatalf("closed row %q not found: %s", name, snippet(body))
			return nil
		}
		// The gate-closed project had its closure record before closing.
		if got := findClosed("RPT Closed")["end_report_received"].(bool); !got {
			t.Errorf("RPT Closed end_report_received = false, want true (record captured before the close gate)")
		}
		// The seeded project has no record yet.
		if got := findClosed("RPT Closed No Report")["end_report_received"].(bool); got {
			t.Fatalf("RPT Closed No Report end_report_received = true before the closure record, want false")
		}

		status, body := admin.Post(t, fmt.Sprintf("/api/p3m/projects/%d/closure-record", prNoReport), map[string]any{
			"lessons_learned": "E2E late closure",
		})
		wantStatus(t, "POST", "/api/p3m/projects/{id}/closure-record (late)", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/portfolio-report?as_of=%s", pfID, p3mReportAsOf))
		wantStatus(t, "GET", "/api/p3m/portfolios/{id}/portfolio-report (after closure)", status, body, http.StatusOK)
		doc2 := mustJSON(t, body).(map[string]any)
		closed2 := doc2["closed"].([]any)
		if len(closed2) != 2 {
			t.Fatalf("closed after closure = %d rows, want 2: %s", len(closed2), snippet(body))
		}
		for _, v := range closed2 {
			if got := v.(map[string]any)["end_report_received"].(bool); !got {
				t.Fatalf("closed row %q end_report_received = false after the closure record, want true", v.(map[string]any)["name"])
			}
		}
	})
}

func TestP3MPortfolioReportMarkdown(t *testing.T) {
	admin := e2eEnv.admin
	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M Report Markdown PF")
	prID := p3mE2eCreateProject(t, admin, "RPT Markdown Project", map[string]any{"portfolio_id": pfID})
	p3mE2eAdvanceProject(t, admin, prID, "implement_deliver")

	status, body := admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/portfolio-report/markdown?as_of=%s", pfID, p3mReportAsOf))
	wantStatus(t, "GET", "/api/p3m/portfolios/{id}/portfolio-report/markdown", status, body, http.StatusOK)
	md := string(body)

	// All 14 section headings, in order.
	wantHeadings := []string{
		"## 1 Portfolio Position",
		"## 2 Programme Highlights",
		"## 3 Key Portfolio Risks",
		"## 4 Benefits by Type",
		"## 5 Benefits by CSO / Owner Group",
		"## 6 Portfolio Dashboard",
		"## 7 Red/Amber Delivery Detail",
		"## 8 Change Dashboard",
		"## 9 Change Calendar",
		"## 10 Ideate",
		"## 11 Proposals",
		"## 12 Approved Investments",
		"## 13 Closed",
		"## 14 ELT Decision Coversheets",
	}
	last := 0
	for _, h := range wantHeadings {
		pos := strings.Index(md, h)
		if pos < last {
			t.Fatalf("markdown heading %q missing or out of order: %s", h, snippet(body))
		}
		last = pos + len(h)
	}
	if !strings.Contains(md, "RPT Markdown Project") {
		t.Fatalf("markdown missing the project title: %s", snippet(body))
	}
}

func TestP3MChangeCalendarGroupImpacts(t *testing.T) {
	admin := e2eEnv.admin

	// Create with two group impacts.
	status, body := admin.Post(t, "/api/p3m/change-calendar", map[string]any{
		"activity":     "E2E CHG Group Impacts",
		"start_week":   "2026-W51",
		"end_week":     "2026-W51",
		"impact_level": 3,
		"group_impacts": []map[string]any{
			{"business_group": "Group A", "impact_level": 4},
			{"business_group": "Group B", "impact_level": 2},
		},
	})
	wantStatus(t, "POST", "/api/p3m/change-calendar", status, body, http.StatusCreated)
	id := cfExtractID(t, body)

	// The single entry response carries the nested rows.
	status, body = admin.Get(t, fmt.Sprintf("/api/p3m/change-calendar/%d", id))
	wantStatus(t, "GET", "/api/p3m/change-calendar/{id}", status, body, http.StatusOK)
	entry := mustJSON(t, body).(map[string]any)
	assertGroupImpacts := func(gi []any, wants map[string]int) {
		t.Helper()
		if len(gi) != len(wants) {
			t.Fatalf("group_impacts = %v, want %v", gi, wants)
		}
		for _, v := range gi {
			row := v.(map[string]any)
			g, lvl := cfStr(row["business_group"]), cfInt(row["impact_level"])
			if want, ok := wants[g]; !ok || lvl != want {
				t.Fatalf("group_impacts row %q = %d, want %d", g, lvl, wants[g])
			}
		}
	}
	assertGroupImpacts(entry["group_impacts"].([]any), map[string]int{"Group A": 4, "Group B": 2})

	// Update with a different set: replaced exactly.
	status, body = admin.Put(t, fmt.Sprintf("/api/p3m/change-calendar/%d", id), map[string]any{
		"activity":      "E2E CHG Group Impacts",
		"start_week":    "2026-W51",
		"end_week":      "2026-W51",
		"impact_level":  3,
		"group_impacts": []map[string]any{{"business_group": "Group C", "impact_level": 5}},
	})
	wantStatus(t, "PUT", "/api/p3m/change-calendar/{id}", status, body, http.StatusOK)
	status, body = admin.Get(t, fmt.Sprintf("/api/p3m/change-calendar/%d", id))
	wantStatus(t, "GET", "/api/p3m/change-calendar/{id} (after update)", status, body, http.StatusOK)
	assertGroupImpacts(mustJSON(t, body).(map[string]any)["group_impacts"].([]any), map[string]int{"Group C": 5})

	// Undated entry (no weeks) accepted.
	status, body = admin.Post(t, "/api/p3m/change-calendar", map[string]any{
		"activity":     "E2E CHG Undated",
		"impact_level": 1,
	})
	wantStatus(t, "POST", "/api/p3m/change-calendar (undated)", status, body, http.StatusCreated)

	// impact_level 0 accepted.
	status, body = admin.Post(t, "/api/p3m/change-calendar", map[string]any{
		"activity":     "E2E CHG Zero Impact",
		"start_week":   "2026-W51",
		"end_week":     "2026-W51",
		"impact_level": 0,
	})
	wantStatus(t, "POST", "/api/p3m/change-calendar (impact 0)", status, body, http.StatusCreated)

	// One week set and the other omitted: 400 in both directions.
	status, body = admin.Post(t, "/api/p3m/change-calendar", map[string]any{
		"activity":   "E2E CHG Half Dated",
		"start_week": "2026-W51",
	})
	wantStatus(t, "POST", "/api/p3m/change-calendar (start only)", status, body, http.StatusBadRequest)
	status, body = admin.Post(t, "/api/p3m/change-calendar", map[string]any{
		"activity": "E2E CHG Half Dated",
		"end_week": "2026-W51",
	})
	wantStatus(t, "POST", "/api/p3m/change-calendar (end only)", status, body, http.StatusBadRequest)
}

func TestP3MELTDecisionLifecycle(t *testing.T) {
	admin := e2eEnv.admin
	pfID := p3mE2eCreatePortfolio(t, admin, "E2E P3M ELT PF")

	mkELT := func(title string) int {
		t.Helper()
		status, body := admin.Post(t, "/api/p3m/elt-decisions", map[string]any{
			"portfolio_id":       pfID,
			"title":              title,
			"why":                "E2E",
			"decision_requested": "Approve",
		})
		wantStatus(t, "POST", "/api/p3m/elt-decisions", status, body, http.StatusCreated)
		return cfExtractID(t, body)
	}

	// Create: pending, no decided_at.
	id := mkELT("E2E ELT Approved")
	status, body := admin.Get(t, fmt.Sprintf("/api/p3m/elt-decisions/%d", id))
	wantStatus(t, "GET", "/api/p3m/elt-decisions/{id}", status, body, http.StatusOK)
	row := mustJSON(t, body).(map[string]any)
	if got := cfStr(row["status"]); got != "pending" {
		t.Fatalf("new ELT decision status = %q, want pending", got)
	}
	if row["decided_at"] != nil {
		t.Fatalf("new ELT decision decided_at = %v, want null", row["decided_at"])
	}

	// Decide approved: status + decided_at set.
	status, body = admin.Post(t, fmt.Sprintf("/api/p3m/elt-decisions/%d/decision", id), map[string]any{"status": "approved"})
	wantStatus(t, "POST", "/api/p3m/elt-decisions/{id}/decision", status, body, http.StatusOK)
	row = mustJSON(t, body).(map[string]any)
	if got := cfStr(row["status"]); got != "approved" {
		t.Fatalf("decided status = %q, want approved", got)
	}
	if row["decided_at"] == nil {
		t.Fatalf("decided_at not set after approval: %s", snippet(body))
	}

	// A second decision on a decided row is rejected.
	status, body = admin.Post(t, fmt.Sprintf("/api/p3m/elt-decisions/%d/decision", id), map[string]any{"status": "rejected"})
	wantStatus(t, "POST", "/api/p3m/elt-decisions/{id}/decision (already decided)", status, body, http.StatusConflict)

	// Second decision: rejected path.
	idRejected := mkELT("E2E ELT Rejected")
	status, body = admin.Post(t, fmt.Sprintf("/api/p3m/elt-decisions/%d/decision", idRejected), map[string]any{"status": "rejected"})
	wantStatus(t, "POST", "/api/p3m/elt-decisions/{id}/decision (reject)", status, body, http.StatusOK)
	if got := cfStr(mustJSON(t, body).(map[string]any)["status"]); got != "rejected" {
		t.Fatalf("decided status = %q, want rejected", got)
	}

	// Withdrawn row: no API path to withdrawn, so seed it directly.
	if _, err := e2eEnv.db.Exec(
		`INSERT INTO p3m_elt_decisions (portfolio_id, title, why, decision_requested, conditions_until_approved, status, created_at, updated_at)
		 VALUES (?, 'E2E ELT Withdrawn', '', '', '', 'withdrawn', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, pfID); err != nil {
		t.Fatalf("insert withdrawn ELT decision: %v", err)
	}

	// One pending decision remains and is the only one requiring decision.
	idPending := mkELT("E2E ELT Pending")

	status, body = admin.Get(t, fmt.Sprintf("/api/p3m/portfolios/%d/portfolio-report?as_of=%s", pfID, p3mReportAsOf))
	wantStatus(t, "GET", "/api/p3m/portfolios/{id}/portfolio-report", status, body, http.StatusOK)
	doc := mustJSON(t, body).(map[string]any)

	reqs := doc["position"].(map[string]any)["elt_decisions_required"].([]any)
	if len(reqs) != 1 {
		t.Fatalf("elt_decisions_required = %d rows, want exactly the pending one: %s", len(reqs), snippet(body))
	}
	if got := cfInt(reqs[0].(map[string]any)["id"]); got != idPending {
		t.Fatalf("elt_decisions_required[0].id = %d, want %d (approved/rejected/withdrawn excluded)", got, idPending)
	}

	// The coversheets section lists every decision of the portfolio.
	covers := doc["elt_decisions"].([]any)
	if len(covers) != 4 {
		t.Fatalf("elt_decisions coversheets = %d rows, want 4: %s", len(covers), snippet(body))
	}
	statuses := map[string]bool{}
	for _, v := range covers {
		statuses[cfStr(v.(map[string]any)["status"])] = true
	}
	for _, s := range []string{"pending", "approved", "rejected", "withdrawn"} {
		if !statuses[s] {
			t.Errorf("coversheets missing status %q: %s", s, snippet(body))
		}
	}
}
