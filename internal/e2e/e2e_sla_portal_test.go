// Use cases UC-G1..UC-G4: workspace SLA configuration (calendars, metrics,
// warning thresholds, Jira-style import), item-level SLA state driven by the
// e2e test clock, portal customer entities and organisations, and the full
// magic-link request flow against a real in-process SMTP sink.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// spAllDayWeekly returns the 24/7 weekly-interval map (every weekday open
// 00:00-24:00). Elapsed calendar time on this schedule equals wall-clock
// time, so e2e test-clock advances are exact.
func spAllDayWeekly() map[string][]map[string]string {
	days := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	out := make(map[string][]map[string]string, len(days))
	for _, d := range days {
		out[d] = []map[string]string{{"start": "00:00", "end": "24:00"}}
	}
	return out
}

// spSLACalendarPayload builds a workspace SLA calendar create/update payload.
func spSLACalendarPayload(name, description string) map[string]any {
	return map[string]any{
		"name":             name,
		"description":      description,
		"timezone":         "UTC",
		"weekly_intervals": spAllDayWeekly(),
		"holidays":         []any{},
	}
}

// spMetricPayload builds an SLA metric payload: the cycle starts when the item
// is created and targets targetMS of the given calendar for every item.
func spMetricPayload(name string, calID int, targetMS int64) map[string]any {
	return map[string]any{
		"name":       name,
		"conditions": []map[string]any{{"phase": "start", "condition_type": "created"}},
		"goals":      []map[string]any{{"position": 0, "ql_query": "1 = 1", "targets": []map[string]any{{"position": 0, "target_ms": targetMS, "calendar_id": calID}}}},
	}
}

// spOngoingCycle extracts the ongoing cycle of one metric from an item SLA
// response (a bare array of per-metric states).
func spOngoingCycle(t *testing.T, body []byte, metricID int) map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("item SLA response is not an array: %s", snippet(body))
	}
	for _, e := range entries {
		if cfInt(e["metric_id"]) == metricID {
			o, ok := e["ongoing"].(map[string]any)
			if !ok {
				t.Fatalf("no ongoing cycle for metric %d: %s", metricID, snippet(body))
			}
			return o
		}
	}
	t.Fatalf("metric %d not in item SLA response: %s", metricID, snippet(body))
	return nil
}

// ---------------------------------------------------------------------------
// UC-G1: SLA configuration CRUD + import round-trip
// ---------------------------------------------------------------------------

func TestSLAConfiguration(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, "slag")
	calBase := fmt.Sprintf("/api/workspaces/%d/sla/calendars", wsID)
	metricBase := fmt.Sprintf("/api/workspaces/%d/sla/metrics", wsID)

	t.Run("CalendarCRUD", func(t *testing.T) {
		status, body := admin.Post(t, calBase, spSLACalendarPayload("e2e-slag-cal", "created by e2e"))
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusCreated)
		calID := cfExtractID(t, body)

		status, body = admin.Get(t, calBase)
		wantStatus(t, "GET", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusOK)
		found := false
		for _, c := range cfListEntries(t, body) {
			if cfInt(c["id"]) == calID && cfStr(c["name"]) == "e2e-slag-cal" {
				found = true
			}
		}
		if !found {
			t.Fatalf("created calendar %d not in list: %s", calID, snippet(body))
		}

		// update persists (API + DB)
		status, body = admin.Put(t, fmt.Sprintf("%s/%d", calBase, calID), spSLACalendarPayload("e2e-slag-cal", "updated by e2e"))
		wantStatus(t, "PUT", "/api/workspaces/{id}/sla/calendars/{calendarId}", status, body, http.StatusOK)
		assertJSONField(t, body, "description", "updated by e2e")
		row := dbQueryOne(t, `SELECT description FROM working_calendars WHERE id = ?`, calID)
		if cfStr(row["description"]) != "updated by e2e" {
			t.Fatalf("working_calendars.description = %q after update", cfStr(row["description"]))
		}

		// impact reports the calendar (no metrics reference it yet -> empty usage)
		status, body = admin.Get(t, fmt.Sprintf("%s/%d/impact", calBase, calID))
		wantStatus(t, "GET", "/api/workspaces/{id}/sla/calendars/{calendarId}/impact", status, body, http.StatusOK)
		assertJSONField(t, body, "calendar_id", calID)

		// negatives: missing name -> 400; interval ending before it starts -> 400
		status, body = admin.Post(t, calBase, spSLACalendarPayload("", ""))
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars (missing name)", status, body, http.StatusBadRequest)

		badIntervals := map[string]any{
			"name":             "e2e-slag-bad-interval",
			"timezone":         "UTC",
			"weekly_intervals": map[string][]map[string]string{"monday": {{"start": "17:00", "end": "09:00"}}},
			"holidays":         []any{},
		}
		status, body = admin.Post(t, calBase, badIntervals)
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars (bad interval)", status, body, http.StatusBadRequest)

		// delete removes the row from DB (second calendar keeps MetricCRUD independent)
		status, body = admin.Post(t, calBase, spSLACalendarPayload("e2e-slag-cal-todelete", ""))
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusCreated)
		delID := cfExtractID(t, body)
		status, body = admin.Delete(t, fmt.Sprintf("%s/%d", calBase, delID))
		wantStatus(t, "DELETE", "/api/workspaces/{id}/sla/calendars/{calendarId}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "working_calendars", "id = ?", delID); n != 0 {
			t.Fatalf("working_calendars row %d still present after delete (count=%d)", delID, n)
		}
	})

	t.Run("MetricCRUD", func(t *testing.T) {
		status, body := admin.Post(t, calBase, spSLACalendarPayload("e2e-slag-metric-cal", ""))
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusCreated)
		calID := cfExtractID(t, body)

		status, body = admin.Post(t, metricBase, spMetricPayload("e2e-slag-metric", calID, 3600000))
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics", status, body, http.StatusCreated)
		metricID := cfExtractID(t, body)

		// conditions and goals round-trip in the create response
		assertJSONField(t, body, "name", "e2e-slag-metric")
		assertJSONField(t, body, "conditions.0.condition_type", "created")
		assertJSONField(t, body, "goals.0.targets.0.target_ms", 3600000)

		// update persists (API + DB)
		status, body = admin.Put(t, fmt.Sprintf("%s/%d", metricBase, metricID), spMetricPayload("e2e-slag-metric-upd", calID, 7200000))
		wantStatus(t, "PUT", "/api/workspaces/{id}/sla/metrics/{metricId}", status, body, http.StatusOK)
		assertJSONField(t, body, "goals.0.targets.0.target_ms", 7200000)
		row := dbQueryOne(t, `SELECT name FROM sla_metrics WHERE id = ?`, metricID)
		if cfStr(row["name"]) != "e2e-slag-metric-upd" {
			t.Fatalf("sla_metrics.name = %q after update", cfStr(row["name"]))
		}

		// negatives: no goals -> 400; target without calendar_id -> 400; unknown condition type -> 400
		noGoals := spMetricPayload("e2e-slag-no-goals", calID, 1000)
		noGoals["goals"] = []any{}
		status, body = admin.Post(t, metricBase, noGoals)
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics (no goals)", status, body, http.StatusBadRequest)

		badTarget := spMetricPayload("e2e-slag-bad-target", calID, 1000)
		badTarget["goals"] = []map[string]any{{"position": 0, "ql_query": "1 = 1", "targets": []map[string]any{{"position": 0, "target_ms": 1000}}}}
		status, body = admin.Post(t, metricBase, badTarget)
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics (target without calendar)", status, body, http.StatusBadRequest)

		badCond := spMetricPayload("e2e-slag-bad-cond", calID, 1000)
		badCond["conditions"] = []map[string]any{{"phase": "start", "condition_type": "bogus"}}
		status, body = admin.Post(t, metricBase, badCond)
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics (bad condition)", status, body, http.StatusBadRequest)

		// delete removes the row from DB
		status, body = admin.Delete(t, fmt.Sprintf("%s/%d", metricBase, metricID))
		wantStatus(t, "DELETE", "/api/workspaces/{id}/sla/metrics/{metricId}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "sla_metrics", "id = ?", metricID); n != 0 {
			t.Fatalf("sla_metrics row %d still present after delete (count=%d)", metricID, n)
		}
	})

	t.Run("WarningThresholds", func(t *testing.T) {
		base := fmt.Sprintf("/api/workspaces/%d/sla/warning-thresholds", wsID)

		status, body := admin.Post(t, base, map[string]any{"label": "80%", "percent": 80})
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/warning-thresholds", status, body, http.StatusCreated)
		thID := cfExtractID(t, body)

		status, body = admin.Get(t, base)
		wantStatus(t, "GET", "/api/workspaces/{id}/sla/warning-thresholds", status, body, http.StatusOK)
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["id"]) == thID && cfInt(e["percent"]) == 80 {
				found = true
			}
		}
		if !found {
			t.Fatalf("created threshold %d not in list: %s", thID, snippet(body))
		}

		status, body = admin.Put(t, fmt.Sprintf("%s/%d", base, thID), map[string]any{"label": "90%", "percent": 90})
		wantStatus(t, "PUT", "/api/workspaces/{id}/sla/warning-thresholds/{thresholdId}", status, body, http.StatusOK)
		assertJSONField(t, body, "percent", 90)
		row := dbQueryOne(t, `SELECT percent FROM sla_warning_thresholds WHERE id = ?`, thID)
		if cfInt(row["percent"]) != 90 {
			t.Fatalf("sla_warning_thresholds.percent = %v after update", row["percent"])
		}

		// negatives: percent outside [1,99] -> 400
		status, body = admin.Post(t, base, map[string]any{"label": "zero", "percent": 0})
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/warning-thresholds (percent=0)", status, body, http.StatusBadRequest)
		status, body = admin.Post(t, base, map[string]any{"label": "over", "percent": 150})
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/warning-thresholds (percent=150)", status, body, http.StatusBadRequest)

		status, body = admin.Delete(t, fmt.Sprintf("%s/%d", base, thID))
		wantStatus(t, "DELETE", "/api/workspaces/{id}/sla/warning-thresholds/{thresholdId}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "sla_warning_thresholds", "id = ?", thID); n != 0 {
			t.Fatalf("sla_warning_thresholds row %d still present after delete (count=%d)", thID, n)
		}
	})

	t.Run("ImportRoundTrip", func(t *testing.T) {
		doc := map[string]any{
			"calendars": []map[string]any{{
				"source_id":        "cal-e2e",
				"name":             "e2e-slag-imported-cal",
				"description":      "imported by e2e",
				"timezone":         "UTC",
				"weekly_intervals": spAllDayWeekly(),
				"holidays":         []any{},
			}},
			"metrics": []map[string]any{{
				"source_id":      "metric-e2e",
				"name":           "e2e-slag-imported-metric",
				"display_format": "time",
				"conditions":     []map[string]any{{"phase": "start", "condition_type": "created"}},
				"goals": []map[string]any{{
					"source_id": "goal-e2e",
					"position":  0,
					"ql":        "1 = 1",
					"targets": []map[string]any{{
						"source_id":          "target-e2e",
						"position":           0,
						"is_fallback":        true,
						"target_ms":          7200000,
						"calendar_source_id": "cal-e2e",
					}},
				}},
			}},
		}

		// preview resolves without writing anything (DB check: the response may echo names)
		status, body := admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/import/preview", wsID), doc)
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/import/preview", status, body, http.StatusOK)
		if n := dbCount(t, "sla_metrics", "name = ?", "e2e-slag-imported-metric"); n != 0 {
			t.Fatalf("import preview wrote %d metric rows", n)
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/import", wsID), map[string]any{"mode": "merge", "document": doc})
		wantStatus(t, "POST", "/api/workspaces/{id}/sla/import", status, body, http.StatusOK)

		status, body = admin.Get(t, calBase)
		wantStatus(t, "GET", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusOK)
		if !strings.Contains(string(body), "e2e-slag-imported-cal") {
			t.Fatalf("imported calendar missing from list: %s", snippet(body))
		}
		status, body = admin.Get(t, metricBase)
		wantStatus(t, "GET", "/api/workspaces/{id}/sla/metrics", status, body, http.StatusOK)
		if !strings.Contains(string(body), "e2e-slag-imported-metric") {
			t.Fatalf("imported metric missing from list: %s", snippet(body))
		}
	})
}

// ---------------------------------------------------------------------------
// UC-G2: item-level SLA state via the e2e test clock
// ---------------------------------------------------------------------------

func TestItemSLAState(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, "slag2")

	status, body := admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/calendars", wsID), spSLACalendarPayload("e2e-slag-item-cal", ""))
	wantStatus(t, "POST", "/api/workspaces/{id}/sla/calendars", status, body, http.StatusCreated)
	calID := cfExtractID(t, body)

	status, body = admin.Post(t, fmt.Sprintf("/api/workspaces/%d/sla/metrics", wsID), spMetricPayload("e2e-slag-item-metric", calID, 3600000))
	wantStatus(t, "POST", "/api/workspaces/{id}/sla/metrics", status, body, http.StatusCreated)
	metricID := cfExtractID(t, body)

	// Item creation starts the SLA cycle inline through the fact observer.
	status, body = admin.Post(t, "/api/v2/items", map[string]any{"workspace_id": wsID, "title": "e2e-sla-item"})
	wantStatus(t, "POST", "/api/v2/items", status, body, http.StatusCreated)
	var itemRes struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &itemRes); err != nil || itemRes.Data.ID == 0 {
		t.Fatalf("no item id in create response: %s", snippet(body))
	}
	itemID := itemRes.Data.ID

	slaPath := fmt.Sprintf("/api/items/%d/sla", itemID)
	var ongoing map[string]any
	spWaitFor(t, 5*time.Second, "ongoing SLA cycle on the new item", func() bool {
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

	// not breached yet; remaining time within (0, target]
	if cfStr(ongoing["status"]) != "ongoing" {
		t.Fatalf("ongoing.status = %q, want ongoing", cfStr(ongoing["status"]))
	}
	if cfBool(ongoing["breached"]) {
		t.Fatal("cycle breached before the target elapsed")
	}
	rem, _ := ongoing["remaining_ms"].(float64)
	goal, _ := ongoing["goal_duration_ms"].(float64)
	if rem <= 0 || rem > goal {
		t.Fatalf("remaining_ms = %v, want in (0, %v]", rem, goal)
	}

	// Advance the e2e test clock past the 1h target; derivation then reports a
	// breach. The advance is one-way and no later suite test depends on SLA timing.
	status, body = admin.Post(t, "/api/test/sla/clock", map[string]any{"advance_ms": 7200000})
	wantStatus(t, "POST", "/api/test/sla/clock", status, body, http.StatusOK)

	status, body = admin.Get(t, slaPath)
	wantStatus(t, "GET", "/api/items/{id}/sla", status, body, http.StatusOK)
	ongoing = spOngoingCycle(t, body, metricID)
	if !cfBool(ongoing["breached"]) {
		t.Fatalf("cycle not breached 2h past the 1h target: %s", snippet(body))
	}
	if bt := cfStr(ongoing["breach_time"]); bt == "" {
		t.Fatal("breach_time not set on breached cycle")
	}

	// DB spot check: the stored cycle is ongoing for this item.
	row := dbQueryOne(t, `SELECT status FROM item_sla_cycles WHERE item_id = ?`, itemID)
	if cfStr(row["status"]) != "ongoing" {
		t.Fatalf("item_sla_cycles.status = %q, want ongoing", cfStr(row["status"]))
	}
}

// ---------------------------------------------------------------------------
// UC-G3: portal customer entities and organisations
// ---------------------------------------------------------------------------

func TestPortalCustomersAndOrgs(t *testing.T) {
	admin := e2eEnv.admin

	t.Run("ContactRoles", func(t *testing.T) {
		status, body := admin.Post(t, "/api/contact-roles", map[string]any{"name": "E2E SLA Role", "description": "created by e2e"})
		wantStatus(t, "POST", "/api/contact-roles", status, body, http.StatusCreated)
		roleID := cfExtractID(t, body)

		status, body = admin.Get(t, "/api/contact-roles")
		wantStatus(t, "GET", "/api/contact-roles", status, body, http.StatusOK)
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["id"]) == roleID && cfStr(e["name"]) == "E2E SLA Role" {
				found = true
			}
		}
		if !found {
			t.Fatalf("created contact role %d not in list: %s", roleID, snippet(body))
		}

		status, body = admin.Put(t, fmt.Sprintf("/api/contact-roles/%d", roleID), map[string]any{"name": "E2E SLA Role", "description": "updated by e2e", "sort_order": 5})
		wantStatus(t, "PUT", "/api/contact-roles/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "description", "updated by e2e")
		row := dbQueryOne(t, `SELECT description FROM contact_roles WHERE id = ?`, roleID)
		if cfStr(row["description"]) != "updated by e2e" {
			t.Fatalf("contact_roles.description = %q after update", cfStr(row["description"]))
		}

		status, body = admin.Post(t, "/api/contact-roles", map[string]any{"description": "no name"})
		wantStatus(t, "POST", "/api/contact-roles (missing name)", status, body, http.StatusBadRequest)

		status, body = admin.Delete(t, fmt.Sprintf("/api/contact-roles/%d", roleID))
		wantStatus(t, "DELETE", "/api/contact-roles/{id}", status, body, http.StatusNoContent, http.StatusOK)
		if n := dbCount(t, "contact_roles", "id = ?", roleID); n != 0 {
			t.Fatalf("contact_roles row %d still present after delete (count=%d)", roleID, n)
		}
	})

	t.Run("CustomerOrganisations", func(t *testing.T) {
		status, body := admin.Post(t, "/api/customer-organisations", map[string]any{"name": "E2E Acme Corp", "email": "acme@example.test"})
		wantStatus(t, "POST", "/api/customer-organisations", status, body, http.StatusCreated)
		orgID := cfExtractID(t, body)

		status, body = admin.Get(t, fmt.Sprintf("/api/customer-organisations/%d", orgID))
		wantStatus(t, "GET", "/api/customer-organisations/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "name", "E2E Acme Corp")

		status, body = admin.Put(t, fmt.Sprintf("/api/customer-organisations/%d", orgID), map[string]any{"name": "E2E Acme Corp", "email": "acme@example.test", "description": "updated by e2e", "active": true})
		wantStatus(t, "PUT", "/api/customer-organisations/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "description", "updated by e2e")
		row := dbQueryOne(t, `SELECT description FROM customer_organisations WHERE id = ?`, orgID)
		if cfStr(row["description"]) != "updated by e2e" {
			t.Fatalf("customer_organisations.description = %q after update", cfStr(row["description"]))
		}

		// Members: the setup-wizard admin is a system admin, which carries org
		// manager rights (HasCustomersManagePermission).
		memberUID := createUser(t, "e2e-org-member@example.test", "e2eorgmember", "E2eOrg123!")
		status, body = admin.Post(t, fmt.Sprintf("/api/customer-organisations/%d/members", orgID), map[string]any{"member_type": "user", "member_id": memberUID})
		wantStatus(t, "POST", "/api/customer-organisations/{id}/members", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/customer-organisations/%d/members", orgID))
		wantStatus(t, "GET", "/api/customer-organisations/{id}/members", status, body, http.StatusOK)
		found := false
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["member_id"]) == memberUID && cfStr(e["member_type"]) == "user" {
				found = true
			}
		}
		if !found {
			t.Fatalf("org member %d not in list: %s", memberUID, snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/customer-organisations/%d/members", orgID), map[string]any{"member_type": "bogus", "member_id": memberUID})
		wantStatus(t, "POST", "/api/customer-organisations/{id}/members (bad type)", status, body, http.StatusBadRequest)

		row = dbQueryOne(t, `SELECT id FROM customer_organisation_members WHERE customer_organisation_id = ? AND member_id = ?`, orgID, memberUID)
		memberRowID := cfInt(row["id"])
		status, body = admin.Delete(t, fmt.Sprintf("/api/customer-organisations/%d/members/%d", orgID, memberRowID))
		wantStatus(t, "DELETE", "/api/customer-organisations/{id}/members/{memberId}", status, body, http.StatusNoContent)
		if n := dbCount(t, "customer_organisation_members", "customer_organisation_id = ? AND member_id = ?", orgID, memberUID); n != 0 {
			t.Fatalf("org member row still present after delete (count=%d)", n)
		}

		// Managers: add + remove a user manager.
		managerUID := createUser(t, "e2e-org-manager@example.test", "e2eorgmgr", "E2eMgr123!")
		status, body = admin.Post(t, fmt.Sprintf("/api/customer-organisations/%d/managers", orgID), map[string]any{"manager_type": "user", "manager_id": managerUID})
		wantStatus(t, "POST", "/api/customer-organisations/{id}/managers", status, body, http.StatusCreated)

		status, body = admin.Get(t, fmt.Sprintf("/api/customer-organisations/%d/managers", orgID))
		wantStatus(t, "GET", "/api/customer-organisations/{id}/managers", status, body, http.StatusOK)
		found = false
		for _, e := range cfListEntries(t, body) {
			if cfInt(e["manager_id"]) == managerUID && cfStr(e["manager_type"]) == "user" {
				found = true
			}
		}
		if !found {
			t.Fatalf("org manager %d not in list: %s", managerUID, snippet(body))
		}

		row = dbQueryOne(t, `SELECT id FROM customer_organisation_managers WHERE customer_organisation_id = ? AND manager_id = ?`, orgID, managerUID)
		managerRowID := cfInt(row["id"])
		status, body = admin.Delete(t, fmt.Sprintf("/api/customer-organisations/%d/managers/%d", orgID, managerRowID))
		wantStatus(t, "DELETE", "/api/customer-organisations/{id}/managers/{managerId}", status, body, http.StatusNoContent)
		if n := dbCount(t, "customer_organisation_managers", "customer_organisation_id = ? AND manager_id = ?", orgID, managerUID); n != 0 {
			t.Fatalf("org manager row still present after delete (count=%d)", n)
		}
	})

	t.Run("PortalCustomerLifecycle", func(t *testing.T) {
		status, body := admin.Post(t, "/api/portal-customers", map[string]any{"name": "E2E Cust", "email": "e2e-cust@example.test"})
		wantStatus(t, "POST", "/api/portal-customers", status, body, http.StatusCreated)
		custID := cfExtractID(t, body)

		// default seeded 'Portal Customer' role assigned on create
		if n := dbCount(t, "portal_customer_roles", "portal_customer_id = ?", custID); n != 1 {
			t.Fatalf("portal_customer_roles count for customer %d = %d, want 1 (default role)", custID, n)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/portal-customers/%d", custID))
		wantStatus(t, "GET", "/api/portal-customers/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "email", "e2e-cust@example.test")

		status, body = admin.Put(t, fmt.Sprintf("/api/portal-customers/%d", custID), map[string]any{"name": "E2E Cust Updated", "email": "e2e-cust@example.test", "phone": "+15550001"})
		wantStatus(t, "PUT", "/api/portal-customers/{id}", status, body, http.StatusOK)
		row := dbQueryOne(t, `SELECT name FROM portal_customers WHERE id = ?`, custID)
		if cfStr(row["name"]) != "E2E Cust Updated" {
			t.Fatalf("portal_customers.name = %q after update", cfStr(row["name"]))
		}

		status, body = admin.Post(t, "/api/portal-customers", map[string]any{"name": "Dup Cust", "email": "e2e-cust@example.test"})
		wantStatus(t, "POST", "/api/portal-customers (duplicate email)", status, body, http.StatusConflict)

		status, body = admin.Post(t, fmt.Sprintf("/api/portal-customers/%d/deactivate", custID), nil)
		wantStatus(t, "POST", "/api/portal-customers/{id}/deactivate", status, body, http.StatusOK)
		row = dbQueryOne(t, `SELECT deactivated_at FROM portal_customers WHERE id = ?`, custID)
		if row["deactivated_at"] == nil {
			t.Fatal("deactivated_at not stamped after deactivate")
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/portal-customers/%d/deactivate", custID), nil)
		wantStatus(t, "POST", "/api/portal-customers/{id}/deactivate (already)", status, body, http.StatusConflict)

		status, body = admin.Post(t, fmt.Sprintf("/api/portal-customers/%d/activate", custID), nil)
		wantStatus(t, "POST", "/api/portal-customers/{id}/activate", status, body, http.StatusOK)
		row = dbQueryOne(t, `SELECT deactivated_at FROM portal_customers WHERE id = ?`, custID)
		if row["deactivated_at"] != nil {
			t.Fatalf("deactivated_at = %v after activate, want NULL", row["deactivated_at"])
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/portal-customers/%d/export", custID))
		wantStatus(t, "GET", "/api/portal-customers/{id}/export", status, body, http.StatusOK)
		if !strings.Contains(string(body), "e2e-cust@example.test") {
			t.Fatalf("export payload missing customer identity: %s", snippet(body))
		}

		status, body = admin.Post(t, fmt.Sprintf("/api/portal-customers/%d/erase", custID), map[string]any{"requested_by": adminEmail})
		wantStatus(t, "POST", "/api/portal-customers/{id}/erase", status, body, http.StatusCreated)

		status, body = admin.Post(t, fmt.Sprintf("/api/portal-customers/%d/erase", custID), map[string]any{"requested_by": adminEmail})
		wantStatus(t, "POST", "/api/portal-customers/{id}/erase (already)", status, body, http.StatusConflict)
	})
}

// ---------------------------------------------------------------------------
// UC-G4: magic-link request flow end to end
// ---------------------------------------------------------------------------

// fakeSMTP is a minimal in-process SMTP sink that captures DATA payloads. It
// speaks just enough of the protocol for net/smtp in "none" mode: greeting,
// EHLO (no extensions advertised), MAIL FROM / RCPT TO / DATA / QUIT.

func TestPortalRequestFlow(t *testing.T) {
	admin := e2eEnv.admin
	wsID := createWorkspace(t, admin, "slag4")
	slug := "e2e-slag-portal"

	// portal channel + workspace config + enable (pending -> enabled)
	status, body := admin.Post(t, "/api/channels", map[string]any{"name": "e2e-slag-portal", "type": "portal", "direction": "inbound", "slug": slug})
	wantStatus(t, "POST", "/api/channels", status, body, http.StatusCreated)
	chID := cfExtractID(t, body)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/config", chID), map[string]any{"config": map[string]any{"portal_workspace_ids": []int{wsID}}})
	wantStatus(t, "PUT", "/api/channels/{id}/config", status, body, http.StatusOK)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", chID), nil)
	wantStatus(t, "PUT", "/api/channels/{id}/toggle", status, body, http.StatusOK)
	row := dbQueryOne(t, `SELECT status FROM channels WHERE id = ?`, chID)
	if cfStr(row["status"]) != "enabled" {
		t.Fatalf("portal channel status = %q after toggle, want enabled", cfStr(row["status"]))
	}

	// request type with a required title field
	itemTypeID := cfAnyItemType(t)
	status, body = admin.Post(t, fmt.Sprintf("/api/channels/%d/request-types", chID), map[string]any{"name": "e2e-slag-rt", "item_type_id": itemTypeID, "workspace_id": wsID, "is_active": true})
	wantStatus(t, "POST", "/api/channels/{id}/request-types", status, body, http.StatusCreated)
	rtID := cfExtractID(t, body)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/request-types/%d/fields", chID, rtID), []map[string]any{{"field_identifier": "title", "field_type": "default", "display_order": 1, "is_required": true}})
	wantStatus(t, "PUT", "/api/channels/{id}/request-types/{rtId}/fields", status, body, http.StatusOK)

	// point the seeded outbound SMTP channel at a local sink and enable it
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
	wantStatus(t, "PUT", "/api/channels/{id}/config (smtp)", status, body, http.StatusOK)

	status, body = admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", smtpID), nil)
	wantStatus(t, "PUT", "/api/channels/{id}/toggle (smtp)", status, body, http.StatusOK)
	row = dbQueryOne(t, `SELECT status, is_default FROM channels WHERE id = ?`, smtpID)
	if cfStr(row["status"]) != "enabled" || !cfBool(row["is_default"]) {
		t.Fatalf("smtp channel not enabled+default after toggle: %v", row)
	}

	// Restore the seeded channel state when done (other tests rely on SMTP being
	// unconfigured); best effort because cleanup runs even after failures.
	t.Cleanup(func() {
		if st, b := admin.Put(t, fmt.Sprintf("/api/channels/%d/toggle", smtpID), nil); st != http.StatusOK {
			t.Logf("e2e: restore smtp channel state failed (%d): %s", st, snippet(b))
		}
	})

	// Request a magic link for an unknown email: open registration mode
	// auto-provisions it, and every failure path returns the same generic
	// success (no enumeration). Distinct from PortalCustomerLifecycle's
	// customer on purpose: that test erases its row, which would make this
	// request hit an erased identity.
	portal := newClient(e2eEnv.baseURL) // fresh jar, no session cookies
	status, body = portal.Post(t, fmt.Sprintf("/api/portal/%s/auth/request", slug), map[string]any{"email": "e2e-portal@example.test"})
	wantStatus(t, "POST", "/api/portal/{slug}/auth/request", status, body, http.StatusOK)
	assertJSONField(t, body, "success", true)

	// capture the email and extract the token from the verify URL fragment
	raw := sink.waitMessage(t, 15*time.Second)
	token := spExtractMagicToken(t, raw)

	// redeem with a fresh client: sets the portal session cookie
	status, body = portal.Get(t, fmt.Sprintf("/api/portal/%s/auth/verify?token=%s", slug, token))
	wantStatus(t, "GET", "/api/portal/{slug}/auth/verify", status, body, http.StatusOK)
	assertJSONField(t, body, "success", true)

	// authenticated identity round-trips (identity is nested under customer)
	status, body = portal.Get(t, fmt.Sprintf("/api/portal/%s/auth/me", slug))
	wantStatus(t, "GET", "/api/portal/{slug}/auth/me", status, body, http.StatusOK)
	assertJSONField(t, body, "customer.email", "e2e-portal@example.test")

	// submit a request through the portal
	status, body = portal.Post(t, fmt.Sprintf("/api/portal/%s/submit", slug), map[string]any{"request_type_id": rtID, "title": "e2e-slag-portal-request"})
	wantStatus(t, "POST", "/api/portal/{slug}/submit", status, body, http.StatusCreated)
	assertJSONField(t, body, "success", true)
	var subRes struct {
		ItemID int `json:"item_id"`
	}
	if err := json.Unmarshal(body, &subRes); err != nil || subRes.ItemID <= 0 {
		t.Fatalf("no item_id in submit response: %s", snippet(body))
	}
	itemID := subRes.ItemID

	// customer sees their own request in my-requests
	status, body = portal.Get(t, fmt.Sprintf("/api/portal/%s/my-requests", slug))
	wantStatus(t, "GET", "/api/portal/{slug}/my-requests", status, body, http.StatusOK)
	found := false
	for _, e := range cfListEntries(t, body) {
		if strings.Contains(cfStr(e["title"]), "e2e-slag-portal-request") {
			found = true
		}
	}
	if !found {
		t.Fatalf("submitted request not in my-requests: %s", snippet(body))
	}

	// the item landed in the workspace (DB check, envelope-agnostic)
	row = dbQueryOne(t, `SELECT title FROM items WHERE id = ?`, itemID)
	if cfStr(row["title"]) != "e2e-slag-portal-request" {
		t.Fatalf("items.title = %q for portal-created item", cfStr(row["title"]))
	}

	// negatives: bogus token -> 401; unknown slug still answers generically (no enumeration)
	status, body = portal.Get(t, fmt.Sprintf("/api/portal/%s/auth/verify?token=bogus-token", slug))
	wantStatus(t, "GET", "/api/portal/{slug}/auth/verify (bogus token)", status, body, http.StatusUnauthorized)

	anon := newClient(e2eEnv.baseURL)
	status, body = anon.Post(t, "/api/portal/no-such-portal/auth/request", map[string]any{"email": "x@example.test"})
	wantStatus(t, "POST", "/api/portal/{slug}/auth/request (unknown slug)", status, body, http.StatusOK)
	assertJSONField(t, body, "success", true)
}
