package e2e

// Cross-module journey coverage (UC-T1..T3): end-to-end use-case simulations
// that span multiple feature areas over real HTTP and assert cross-module data
// consistency — plan aggregation vs workspace items, SMS programme-dashboard
// scoping vs P3M entity links, portal request lifecycle vs workflow + SLA
// state — followed by a final whole-database integrity sweep (UC-T3 is the
// last test in this file).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// UC-T1: Program delivery. P3M portfolio→programme→project + a workspace with
// items scheduled into a sprint + SMS stakeholders bound to the project via
// sms_stakeholder_entities + a Plan linking all three. Assertions: plan item
// aggregation contains exactly and only that workspace's items; the
// programme-scoped SMS dashboard shows the bound stakeholder count (and does
// not leak across entities); P3M executive-summary budget rollups are scoped
// per portfolio tree.
// ---------------------------------------------------------------------------

func TestJourneyProgramDelivery(t *testing.T) {
	admin := e2eEnv.admin

	// Two portfolios with different budgets so the scoping assertion is
	// meaningful: each tree's rollup must equal only its own projects' sums.
	pfA, pfB := 0, 0
	for i, title := range []string{"E2E T1 Portfolio A", "E2E T1 Portfolio B"} {
		status, body := admin.Post(t, "/api/p3m/portfolios", map[string]any{
			"title":            title,
			"budget_allocated": 500000 + float64(i)*400000,
		})
		wantStatus(t, "POST", "/api/p3m/portfolios (t1)", status, body, http.StatusCreated)
		if i == 0 {
			pfA = cfExtractID(t, body)
		} else {
			pfB = cfExtractID(t, body)
		}
	}

	prA := p3mE2eCreateProject(t, admin, "E2E T1 Project A", map[string]any{
		"programme_id":    p3mE2eCreateProgramme(t, admin, pfA, "E2E T1 Programme A"),
		"budget_approved": 150000,
		"budget_actual":   80000,
	})
	prB := p3mE2eCreateProject(t, admin, "E2E T1 Project B", map[string]any{
		"programme_id":    p3mE2eCreateProgramme(t, admin, pfB, "E2E T1 Programme B"),
		"budget_approved": 400000,
		"budget_actual":   390000,
	})

	// Workspace with items scheduled into a sprint (iteration).
	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	status, body := admin.Post(t, fmt.Sprintf("/api/v2/workspaces/%d/iterations", wsID), map[string]any{
		"name":       wscfgName(t, "t1-sprint"),
		"start_date": "2026-12-01",
		"end_date":   "2026-12-15",
	})
	wantStatus(t, "POST", "/api/v2/workspaces/{ws}/iterations (t1)", status, body, http.StatusCreated)
	iterID := v2ID(t, body)

	var itemIDs []int
	for i := 1; i <= 3; i++ {
		itemIDs = append(itemIDs, t1CreateScheduledItem(t, admin, wsID, iterID, fmt.Sprintf("E2E T1 sprint item %d", i)))
	}
	if n := dbCount(t, "items", "workspace_id = ? AND iteration_id = ?", wsID, iterID); n != 3 {
		t.Fatalf("items scheduled on the sprint = %d, want 3 (DB)", n)
	}

	// SMS stakeholders bound to P3M project A.
	shA := t1CreateStakeholder(t, admin, "E2E T1 stakeholder A")
	shB := t1CreateStakeholder(t, admin, "E2E T1 stakeholder B")
	for _, shID := range []int{shA, shB} {
		status, body = admin.Post(t, fmt.Sprintf("/api/sms/stakeholders/%d/entities", shID), map[string]any{
			"entity_type": "project",
			"entity_id":   prA,
		})
		wantStatus(t, "POST", "/api/sms/stakeholders/{id}/entities (t1)", status, body, http.StatusCreated)
	}

	// Plan linking workspace + P3M project A + stakeholder A.
	status, body = admin.Post(t, "/api/plans", map[string]any{
		"name":        "E2E T1 delivery plan",
		"description": "cross-module journey plan",
	})
	wantStatus(t, "POST", "/api/plans (t1)", status, body, http.StatusCreated)
	planID := cfExtractID(t, body)

	link := func(path string, reqBody any) {
		st, b := admin.Post(t, path, reqBody)
		wantStatus(t, "POST", path+" (t1 link)", st, b, http.StatusNoContent)
	}
	link(fmt.Sprintf("/api/plans/%d/workspaces/%d", planID, wsID), nil)
	link(fmt.Sprintf("/api/plans/%d/p3m-entities", planID), map[string]any{"entity_type": "project", "entity_id": prA})
	link(fmt.Sprintf("/api/plans/%d/stakeholders", planID), map[string]any{"stakeholder_id": shA})

	t.Run("PlanItemsExactlyWorkspaceSet", func(t *testing.T) {
		st, b := admin.Get(t, fmt.Sprintf("/api/plans/%d/items?limit=100", planID))
		wantStatus(t, "GET", "/api/plans/{id}/items (t1)", st, b, http.StatusOK)
		var resp struct {
			Items []struct {
				ID int `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(b, &resp); err != nil || resp.Total != 3 || len(resp.Items) != 3 {
			t.Fatalf("plan items = total %d / %d entries, want exactly the 3 workspace items: %s", resp.Total, len(resp.Items), snippet(b))
		}
		got := map[int]bool{}
		for _, it := range resp.Items {
			got[it.ID] = true
		}
		for _, wantID := range itemIDs {
			if !got[wantID] {
				t.Fatalf("plan items missing workspace item %d: got %v", wantID, got)
			}
		}
	})

	t.Run("ProgrammeScopedSMSDashboard", func(t *testing.T) {
		st, b := admin.Get(t, fmt.Sprintf("/api/sms/dashboard/programme?entity_type=project&entity_id=%d", prA))
		wantStatus(t, "GET", "/api/sms/dashboard/programme (t1)", st, b, http.StatusOK)
		var dash struct {
			Totals struct {
				Stakeholders int `json:"stakeholders"`
			} `json:"totals"`
			Scope map[string]any `json:"scope"`
		}
		if err := json.Unmarshal(b, &dash); err != nil {
			t.Fatalf("decode programme dashboard: %v\n%s", err, b)
		}
		if dash.Totals.Stakeholders != 2 {
			t.Fatalf("programme-scoped stakeholder count = %d, want 2 (bound to project %d): %s", dash.Totals.Stakeholders, prA, snippet(b))
		}
		if cfInt(dash.Scope["entity_id"]) != prA {
			t.Fatalf("dashboard scope.entity_id = %v, want %d", dash.Scope["entity_id"], prA)
		}

		// Unbound entity: bound stakeholders must not leak across P3M entities.
		st, b = admin.Get(t, fmt.Sprintf("/api/sms/dashboard/programme?entity_type=project&entity_id=%d", prB))
		wantStatus(t, "GET", "/api/sms/dashboard/programme (t1 unbound)", st, b, http.StatusOK)
		assertJSONField(t, b, "totals.stakeholders", 0)
	})

	t.Run("ExecutiveSummaryBudgetScopedPerTree", func(t *testing.T) {
		st, b := admin.Get(t, "/api/p3m/executive-summary")
		wantStatus(t, "GET", "/api/p3m/executive-summary (t1)", st, b, http.StatusOK)
		root := mustJSON(t, b).(map[string]any)
		sums := map[int]map[string]any{}
		for _, v := range root["portfolios"].([]any) {
			m := v.(map[string]any)
			sums[cfInt(m["portfolio_id"])] = m
		}
		for pfID, wantAppr := range map[int]float64{pfA: 150000, pfB: 400000} {
			sum := sums[pfID]
			if sum == nil {
				t.Fatalf("executive summary missing portfolio %d", pfID)
			}
			n, appr, act := p3mE2eTreeBudget(t, pfID)
			if n != 1 {
				t.Fatalf("DB tree for portfolio %d has %d projects, want 1 (setup broken)", pfID, n)
			}
			if got := p3mE2eF(sum["budget_approved"]); !p3mE2eApprox(got, appr) || !p3mE2eApprox(got, wantAppr) {
				t.Fatalf("portfolio %d budget_approved = %v, want %v (DB tree sum %v)", pfID, got, wantAppr, appr)
			}
			if got := p3mE2eF(sum["budget_actual"]); !p3mE2eApprox(got, act) {
				t.Fatalf("portfolio %d budget_actual = %v, DB tree sum = %v", pfID, got, act)
			}
		}
	})
}

func t1CreateStakeholder(t *testing.T, admin *Client, name string) int {
	t.Helper()
	status, body := admin.Post(t, "/api/sms/stakeholders", map[string]any{"name": name})
	wantStatus(t, "POST", "/api/sms/stakeholders (t1)", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

func t1CreateScheduledItem(t *testing.T, admin *Client, wsID, iterID int, title string) int {
	t.Helper()
	status, body := admin.Post(t, "/api/v2/items", map[string]any{
		"workspace_id": wsID,
		"title":        title,
		"iteration_id": iterID,
	})
	wantStatus(t, "POST", "/api/v2/items (t1 scheduled)", status, body, http.StatusCreated)
	return v2ID(t, body)
}

// ---------------------------------------------------------------------------
// UC-T2: Customer request lifecycle. A portal customer authenticates via magic
// link and submits a request; the item enters the channel queue with a live SLA
// cycle (start on creation, stop when it enters the workflow's final status
// category); staff walks it through the workflow to completion; the requester
// sees the closed state in my-requests; the completed SLA cycle is met.
// ---------------------------------------------------------------------------

func TestJourneyCustomerRequestLifecycle(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, wscfgWSKey(t))
	itID, aID, bID, cID := d2WorkflowSetup(t, admin, wsID)

	// A completed "done" category with final status D; the workflow gains C→D
	// so staff can close the request. The SLA stop condition (entering the done
	// category) fires exactly on closure, and my-requests derives
	// status_is_completed from that category's is_completed flag.
	status, body := admin.Post(t, "/api/v2/status-categories", map[string]any{
		"name": wscfgName(t, "t2-done"), "color": "#16a34a", "is_completed": true,
	})
	wantStatus(t, "POST", "/api/v2/status-categories (t2 done)", status, body, http.StatusCreated)
	doneCatID := v2ID(t, body)

	status, body = admin.Post(t, "/api/v2/statuses", map[string]any{
		"name": wscfgName(t, "t2-d"), "category_id": doneCatID,
	})
	wantStatus(t, "POST", "/api/v2/statuses (t2 d)", status, body, http.StatusCreated)
	dID := v2ID(t, body)

	wfRow := dbQueryOne(t, `SELECT cs.workflow_id FROM workspace_configuration_sets wcs JOIN configuration_sets cs ON cs.id = wcs.configuration_set_id WHERE wcs.workspace_id = ?`, wsID)
	wfID := cfInt(wfRow["workflow_id"])
	if wfID <= 0 {
		t.Fatalf("no workflow linked to workspace %d: %v", wsID, wfRow)
	}
	status, body = admin.Put(t, fmt.Sprintf("/api/v2/workflows/%d/transitions", wfID), map[string]any{
		"transitions": []map[string]any{
			{"to_status_id": aID},
			{"from_status_id": aID, "to_status_id": bID},
			{"from_status_id": bID, "to_status_id": cID},
			{"from_status_id": cID, "to_status_id": dID},
		},
	})
	wantStatus(t, "PUT", "/api/v2/workflows/{id}/transitions (t2)", status, body, http.StatusOK)

	// SLA metric: cycle starts on creation and stops when the item enters the
	// done category. Condition references live in a config blob of numeric IDs
	// (models.SLACondition.Config), not top-level names.
	status, body = admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/calendars", wsID), spSLACalendarPayload(wscfgName(t, "t2-cal"), ""))
	wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars (t2)", status, body, http.StatusCreated)
	calID := cfExtractID(t, body)

	status, body = admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/metrics", wsID), map[string]any{
		"name": wscfgName(t, "t2-metric"),
		"conditions": []map[string]any{
			{"phase": "start", "condition_type": "created"},
			{"phase": "stop", "condition_type": "status_category_entered", "config": map[string]any{"category_ids": []int{doneCatID}}},
		},
		"goals": []map[string]any{{"position": 0, "ql_query": "1 = 1", "targets": []map[string]any{{"position": 0, "target_ms": 3600000, "calendar_id": calID}}}},
	})
	wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics (t2)", status, body, http.StatusCreated)
	metricID := cfExtractID(t, body)

	// Portal channel serving this workspace + a request type bound to the d2
	// item type so staff can walk the workflow.
	slug := "e2e-t2-journey"
	status, body = admin.Post(t, "/api/channels", map[string]any{
		"name": wscfgName(t, "t2-portal"), "type": "portal", "direction": "inbound", "slug": slug,
	})
	wantStatus(t, "POST", "/api/channels (t2 portal)", status, body, http.StatusCreated)
	chID := cfExtractID(t, body)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", chID), map[string]any{"config": map[string]any{"portal_workspace_ids": []int{wsID}}})
	wantStatus(t, "PUT", "/api/channels/{id}/config (t2)", status, body, http.StatusOK)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", chID), nil)
	wantStatus(t, "PUT", "/api/channels/{id}/toggle (t2)", status, body, http.StatusOK)
	if row := dbQueryOne(t, `SELECT status FROM channels WHERE id = ?`, chID); cfStr(row["status"]) != "enabled" {
		t.Fatalf("portal channel %d not enabled after toggle: %v", chID, row)
	}

	status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", chID), map[string]any{
		"name": wscfgName(t, "t2-rt"), "item_type_id": itID, "workspace_id": wsID, "is_active": true,
	})
	wantStatus(t, "POST", "/api/channels/{id}/request-types (t2)", status, body, http.StatusCreated)
	rtID := cfExtractID(t, body)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/request-types/%d/fields", chID, rtID), []map[string]any{
		{"field_identifier": "title", "field_type": "default", "display_order": 1, "is_required": true},
	})
	wantStatus(t, "PUT", "/api/channels/{id}/request-types/{rtId}/fields (t2)", status, body, http.StatusOK)

	// Point the seeded outbound SMTP channel at a local sink for the magic link.
	smtpRow := dbQueryOne(t, `SELECT id FROM channels WHERE type = 'smtp' AND direction = 'outbound' LIMIT 1`)
	smtpID := cfInt(smtpRow["id"])
	if smtpID <= 0 {
		t.Fatal("no seeded outbound SMTP channel found")
	}
	sink := startFakeSMTP(t)
	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", smtpID), map[string]any{"config": map[string]any{
		"smtp_host":       "127.0.0.1",
		"smtp_port":       sink.port(),
		"smtp_from_email": "e2e@windshift.test",
		"smtp_encryption": "none",
	}})
	wantStatus(t, "PUT", "/api/channels/{id}/config (t2 smtp)", status, body, http.StatusOK)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", smtpID), nil)
	wantStatus(t, "PUT", "/api/channels/{id}/toggle (t2 smtp)", status, body, http.StatusOK)
	t.Cleanup(func() {
		if st, b := admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", smtpID), nil); st != http.StatusOK {
			t.Logf("e2e: restore smtp channel state failed (%d): %s", st, snippet(b))
		}
	})

	// Customer authenticates via magic link (open registration auto-provisions).
	portal := newClient(e2eEnv.baseURL)
	status, body = portal.Post(t, fmt.Sprintf("/api/portal/%s/auth/request", slug), map[string]any{"email": "e2e-t2-cust@example.test"})
	wantStatus(t, "POST", "/api/portal/{slug}/auth/request (t2)", status, body, http.StatusOK)
	assertJSONField(t, body, "success", true)

	token := spExtractMagicToken(t, sink.waitMessage(t, 15*time.Second))
	status, body = portal.Get(t, fmt.Sprintf("/api/portal/%s/auth/verify?token=%s", slug, token))
	wantStatus(t, "GET", "/api/portal/{slug}/auth/verify (t2)", status, body, http.StatusOK)

	// Submit the request.
	status, body = portal.Post(t, fmt.Sprintf("/api/portal/%s/submit", slug), map[string]any{
		"request_type_id": rtID,
		"title":           "E2E T2 customer request",
	})
	wantStatus(t, "POST", "/api/portal/{slug}/submit (t2)", status, body, http.StatusCreated)
	var subRes struct {
		ItemID int `json:"item_id"`
	}
	if err := json.Unmarshal(body, &subRes); err != nil || subRes.ItemID <= 0 {
		t.Fatalf("no item_id in submit response: %s", snippet(body))
	}
	itemID := subRes.ItemID

	slaPath := fmt.Sprintf("/api/items/%d/sla", itemID)

	t.Run("SLACycleLiveInQueue", func(t *testing.T) {
		var ongoing map[string]any
		spWaitFor(t, 5*time.Second, "ongoing SLA cycle on the submitted item", func() bool {
			st, b := admin.Get(t, slaPath)
			if st != http.StatusOK {
				return false
			}
			for _, e := range cfListEntries(t, b) {
				if cfInt(e["metric_id"]) == metricID {
					o, ok := e["ongoing"].(map[string]any)
					if !ok {
						return false
					}
					ongoing = o
					return true
				}
			}
			return false
		})
		if cfStr(ongoing["status"]) != "ongoing" || cfBool(ongoing["breached"]) {
			t.Fatalf("queue SLA cycle inconsistent: status=%q breached=%v", cfStr(ongoing["status"]), cfBool(ongoing["breached"]))
		}
		rem, _ := ongoing["remaining_ms"].(float64)
		goal, _ := ongoing["goal_duration_ms"].(float64)
		if rem <= 0 || rem > goal {
			t.Fatalf("remaining_ms = %v not in (0, %v]", rem, goal)
		}
	})

	// Staff walks the item through the workflow to its final (done) status.
	for _, next := range []int{bID, cID, dID} {
		status, body = admin.Post(t, fmt.Sprintf("/api/v2/items/%d/transition", itemID), map[string]any{"to_status_id": next})
		wantStatus(t, "POST", "/api/v2/items/{id}/transition (t2)", status, body, http.StatusOK)
	}
	if row := dbQueryOne(t, `SELECT status_id FROM items WHERE id = ?`, itemID); cfInt(row["status_id"]) != dID {
		t.Fatalf("items.status_id = %v after workflow walk, want final status %d", row["status_id"], dID)
	}

	t.Run("SLACycleCompletedMet", func(t *testing.T) {
		var completed []map[string]any
		spWaitFor(t, 5*time.Second, "completed SLA cycle after reaching the final status", func() bool {
			st, b := admin.Get(t, slaPath)
			if st != http.StatusOK {
				return false
			}
			completed = nil
			for _, e := range cfListEntries(t, b) {
				if cfInt(e["metric_id"]) != metricID {
					continue
				}
				if cs, ok := e["completed"].([]any); ok {
					for _, cv := range cs {
						c := cv.(map[string]any)
						if cfStr(c["stopped_at"]) != "" {
							completed = append(completed, c)
						}
					}
				}
			}
			return len(completed) > 0
		})
		cyc := completed[0]
		if cfStr(cyc["status"]) != "completed" || cfBool(cyc["breached"]) {
			t.Fatalf("completed SLA cycle not met: status=%q breached=%v (elapsed %v ms vs goal %v ms)", cfStr(cyc["status"]), cfBool(cyc["breached"]), cyc["elapsed_ms"], cyc["goal_duration_ms"])
		}
	})

	t.Run("RequesterSeesClosedState", func(t *testing.T) {
		finalName := cfStr(dbQueryOne(t, `SELECT name FROM statuses WHERE id = ?`, dID)["name"])
		st, b := portal.Get(t, fmt.Sprintf("/api/portal/%s/my-requests", slug))
		wantStatus(t, "GET", "/api/portal/{slug}/my-requests (t2)", st, b, http.StatusOK)
		var entry map[string]any
		for _, e := range cfListEntries(t, b) {
			if cfInt(e["id"]) == itemID {
				entry = e
				break
			}
		}
		if entry == nil {
			t.Fatalf("submitted request %d missing from my-requests: %s", itemID, snippet(b))
		}
		if !cfBool(entry["status_is_completed"]) || cfStr(entry["status"]) != finalName {
			t.Fatalf("my-requests state = status %q completed=%v, want %q/true", cfStr(entry["status"]), cfBool(entry["status_is_completed"]), finalName)
		}
	})
}

// ---------------------------------------------------------------------------
// UC-T3: Data integrity sweep (last test in this file). No orphan rows across
// the cross-module FK pairs, the SMS full-register export still round-trips
// after all journey mutations, and the audit log is non-empty and covers the
// modules the suite touched.
// ---------------------------------------------------------------------------

func TestJourneyDataIntegritySweep(t *testing.T) {
	admin := e2eEnv.admin

	t.Run("NoOrphanRows", func(t *testing.T) {
		pairs := []struct{ desc, query string }{
			{"items.workspace_id -> workspaces", `SELECT COUNT(*) AS n FROM items i WHERE i.workspace_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.id = i.workspace_id)`},
			{"time_worklogs.project_id -> time_projects", `SELECT COUNT(*) AS n FROM time_worklogs wl WHERE NOT EXISTS (SELECT 1 FROM time_projects tp WHERE tp.id = wl.project_id)`},
			{"time_worklogs.item_id -> items", `SELECT COUNT(*) AS n FROM time_worklogs wl WHERE wl.item_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM items i WHERE i.id = wl.item_id)`},
			{"sms_stakeholder_group_members.stakeholder_id -> sms_stakeholders", `SELECT COUNT(*) AS n FROM sms_stakeholder_group_members m WHERE NOT EXISTS (SELECT 1 FROM sms_stakeholders s WHERE s.id = m.stakeholder_id)`},
			{"sms_engagement_plan_stakeholders.stakeholder_id -> sms_stakeholders", `SELECT COUNT(*) AS n FROM sms_engagement_plan_stakeholders m WHERE NOT EXISTS (SELECT 1 FROM sms_stakeholders s WHERE s.id = m.stakeholder_id)`},
			{"p3m_investment_cases.project_id -> p3m_projects", `SELECT COUNT(*) AS n FROM p3m_investment_cases ic WHERE NOT EXISTS (SELECT 1 FROM p3m_projects p WHERE p.id = ic.project_id)`},
		}
		for _, p := range pairs {
			if n := cfInt(dbQueryOne(t, p.query)["n"]); n != 0 {
				t.Errorf("%s: %d orphan rows", p.desc, n)
			}
		}
	})

	t.Run("SMSFullExportRoundTrip", func(t *testing.T) {
		st, b := admin.Get(t, "/api/sms/export/full")
		wantStatus(t, "GET", "/api/sms/export/full (t3)", st, b, http.StatusOK)
		var doc struct {
			Module        string `json:"module"`
			SchemaVersion int    `json:"schema_version"`
			Stakeholders  []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"stakeholders"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("full export is not valid JSON: %v\n%s", err, snippet(b))
		}
		if doc.Module != "sms" || doc.SchemaVersion <= 0 || len(doc.Stakeholders) == 0 {
			t.Fatalf("full export invalid after journey mutations: module=%q schema_version=%d stakeholders=%d", doc.Module, doc.SchemaVersion, len(doc.Stakeholders))
		}
		for _, s := range doc.Stakeholders {
			if s.ID <= 0 || s.Name == "" {
				t.Fatalf("exported stakeholder missing id/name: %+v", s)
			}
		}
	})

	t.Run("AuditLogCoversTouchedModules", func(t *testing.T) {
		st, b := admin.Get(t, "/api/admin/audit-logs?per_page=1")
		wantStatus(t, "GET", "/api/admin/audit-logs (t3)", st, b, http.StatusOK)
		var resp struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(b, &resp); err != nil || resp.Total <= 0 {
			t.Fatalf("audit log empty after full suite run: total=%d", resp.Total)
		}

		st, b = admin.Get(t, "/api/admin/audit-logs/resource-types")
		wantStatus(t, "GET", "/api/admin/audit-logs/resource-types (t3)", st, b, http.StatusOK)
		var types []string
		if err := json.Unmarshal(b, &types); err != nil {
			t.Fatalf("decode resource types: %v\n%s", err, snippet(b))
		}
		set := map[string]bool{}
		for _, ty := range types {
			set[ty] = true
		}
		for _, wantType := range []string{"p3m_portfolio", "sms_stakeholder"} {
			if !set[wantType] {
				t.Errorf("audit log resource types missing %q (module touched by journeys)", wantType)
			}
		}
	})
}
