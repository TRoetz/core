package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// UC-L1..L6 — Stakeholder Management System (SMS): onboarding + sensitive-data
// masking, roadworks delivery (nodes/members/campaigns), RACI + work packages,
// data movement (CSV import/export/restore), reporting rollups, and public
// grievance intake.

// smsE2eCreateStakeholder creates a stakeholder through the API and returns its ID.
func smsE2eCreateStakeholder(t *testing.T, c *Client, fields map[string]any) int {
	t.Helper()
	status, body := c.Post(t, "/api/sms/stakeholders", fields)
	wantStatus(t, "POST", "/api/sms/stakeholders", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// smsE2eNodeKindID returns the id of a seeded node kind by key.
func smsE2eNodeKindID(t *testing.T, c *Client, key string) int {
	t.Helper()
	status, body := c.Get(t, "/api/sms/node-kinds")
	wantStatus(t, "GET", "/api/sms/node-kinds", status, body, http.StatusOK)
	for _, e := range cfListEntries(t, body) {
		if cfStr(e["key"]) == key {
			return cfInt(e["id"])
		}
	}
	t.Fatalf("node kind %q not found in seeded kinds: %s", key, snippet(body))
	return 0
}

// smsE2eCreateNode creates a node of the given kind (optionally under parentID) and returns its ID.
func smsE2eCreateNode(t *testing.T, c *Client, name string, kindID, parentID int) int {
	t.Helper()
	req := map[string]any{"name": name, "kind_id": kindID}
	if parentID > 0 {
		req["parent_id"] = parentID
	}
	status, body := c.Post(t, "/api/sms/nodes", req)
	wantStatus(t, "POST", "/api/sms/nodes", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// smsE2eP3MProject creates a P3M portfolio + project and returns the project ID.
func smsE2eP3MProject(t *testing.T, c *Client, prefix string) int {
	t.Helper()
	status, body := c.Post(t, "/api/p3m/portfolios", map[string]any{"title": prefix + " portfolio"})
	wantStatus(t, "POST", "/api/p3m/portfolios", status, body, http.StatusCreated)
	pfID := cfExtractID(t, body)
	status, body = c.Post(t, "/api/p3m/projects", map[string]any{"portfolio_id": pfID, "title": prefix + " project"})
	wantStatus(t, "POST", "/api/p3m/projects", status, body, http.StatusCreated)
	return cfExtractID(t, body)
}

// UC-L1 — Stakeholder onboarding: full profile round-trip, sensitive-field
// masking for sms.view-only users (with audit trail), and validation negatives.
func TestSMSStakeholderOnboarding(t *testing.T) {
	admin := e2eEnv.admin

	viewerID := createUser(t, "e2e-sms-viewer@windshift.test", "e2esmsviewer", "E2eSmsViewer!1")
	grantGlobalPermission(t, viewerID, "sms.view")
	viewer := login(t, "e2e-sms-viewer@windshift.test", "E2eSmsViewer!1", "e2esmsviewer")

	t.Run("CreateFullProfile", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/stakeholders", map[string]any{
			"name":                "E2E SMS Onboarded Resident",
			"tier":                "tier_3",
			"email":               "onboarded@e2e-sms.test",
			"organization":        "E2E SMS Residents Inc",
			"influence_level":     4,
			"interest_level":      3,
			"info_classification": "restricted",
			"notes":               "sensitive onboarding note",
			"custom_fields":       `{"building":"B-7"}`,
		})
		wantStatus(t, "POST", "/api/sms/stakeholders", status, body, http.StatusCreated)
		assertJSONField(t, body, "name", "E2E SMS Onboarded Resident")
		assertJSONField(t, body, "tier", "tier_3")
		id := cfExtractID(t, body)

		row := dbQueryOne(t, `SELECT tier, email, info_classification FROM sms_stakeholders WHERE id = ?`, id)
		if cfStr(row["tier"]) != "tier_3" || cfStr(row["email"]) != "onboarded@e2e-sms.test" || cfStr(row["info_classification"]) != "restricted" {
			t.Fatalf("stakeholder row does not round-trip: %v", row)
		}
	})

	t.Run("SensitiveFieldsMaskedForViewer", func(t *testing.T) {
		id := dbQueryOne(t, `SELECT id FROM sms_stakeholders WHERE email = ?`, "onboarded@e2e-sms.test")
		stakeholderID := cfInt(id["id"])

		before := dbCount(t, "audit_logs", `action_type = 'sms_restricted_access'`)
		status, body := viewer.Get(t, fmt.Sprintf("/api/sms/stakeholders/%d", stakeholderID))
		wantStatus(t, "GET", "/api/sms/stakeholders/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "name", "E2E SMS Onboarded Resident") // non-sensitive fields stay visible
		assertJSONField(t, body, "notes", "")                          // restricted content masked
		assertJSONField(t, body, "custom_fields", "{}")

		status, body = admin.Get(t, fmt.Sprintf("/api/sms/stakeholders/%d", stakeholderID))
		wantStatus(t, "GET", "/api/sms/stakeholders/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "notes", "sensitive onboarding note") // admin sees the real value

		if got := dbCount(t, "audit_logs", `action_type = 'sms_restricted_access'`); got <= before {
			t.Fatalf("viewer access to restricted fields was not audited (before=%d after=%d)", before, got)
		}
	})

	t.Run("Negatives", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/stakeholders", map[string]any{"tier": "tier_1"})
		wantStatus(t, "POST", "/api/sms/stakeholders", status, body, http.StatusBadRequest) // name required

		status, body = admin.Post(t, "/api/sms/stakeholders", map[string]any{"name": "E2E SMS Bad Tier", "tier": "tier_9"})
		wantStatus(t, "POST", "/api/sms/stakeholders", status, body, http.StatusBadRequest) // invalid tier
	})
}

// UC-L2 — Roadworks delivery: node hierarchy with kind/role validation,
// notification campaign audience generation, and the delivery ledger.
func TestSMSRoadworksDelivery(t *testing.T) {
	admin := e2eEnv.admin

	areaKind := smsE2eNodeKindID(t, admin, "area")
	segKind := smsE2eNodeKindID(t, admin, "road_segment")
	bldgKind := smsE2eNodeKindID(t, admin, "building")

	// Seed the roadworks hierarchy: area -> segment -> building.
	area := smsE2eCreateNode(t, admin, "E2E SMS Area North", areaKind, 0)
	seg := smsE2eCreateNode(t, admin, "E2E SMS Segment A1", segKind, area)
	bldg := smsE2eCreateNode(t, admin, "E2E SMS Building B-7", bldgKind, seg)

	row := dbQueryOne(t, `SELECT parent_id FROM sms_nodes WHERE id = ?`, bldg)
	if cfInt(row["parent_id"]) != seg {
		t.Fatalf("building parent_id = %v, want segment %d", row["parent_id"], seg)
	}

	t.Run("InvalidParentRejected", func(t *testing.T) {
		// road_segment only allows 'building' children; an area under it is invalid.
		status, body := admin.Post(t, "/api/sms/nodes", map[string]any{"name": "E2E SMS Bad Child", "kind_id": areaKind, "parent_id": seg})
		wantStatus(t, "POST", "/api/sms/nodes", status, body, http.StatusBadRequest)

		status, body = admin.Post(t, "/api/sms/nodes", map[string]any{"name": "E2E SMS Bad Kind", "kind_id": 999999})
		wantStatus(t, "POST", "/api/sms/nodes", status, body, http.StatusNotFound) // unknown kind
	})

	t.Run("MembersWithRoles", func(t *testing.T) {
		owner := smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS Owner Corp"})
		tenant := smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS Tenant Ltd"})

		status, body := admin.Post(t, fmt.Sprintf("/api/sms/nodes/%d/members", bldg), map[string]any{"stakeholder_id": owner, "role": "owner"})
		wantStatus(t, "POST", "/api/sms/nodes/{id}/members", status, body, http.StatusCreated)
		assertJSONField(t, body, "role", "owner")

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/nodes/%d/members", bldg), map[string]any{"stakeholder_id": tenant, "role": "manager"})
		wantStatus(t, "POST", "/api/sms/nodes/{id}/members", status, body, http.StatusBadRequest) // role not defined for building kind

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/nodes/%d/members", bldg), map[string]any{"stakeholder_id": owner, "role": "owner"})
		wantStatus(t, "POST", "/api/sms/nodes/{id}/members", status, body, http.StatusConflict) // duplicate member+role

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/nodes/%d/members", bldg), map[string]any{"stakeholder_id": tenant, "role": "tenant"})
		wantStatus(t, "POST", "/api/sms/nodes/{id}/members", status, body, http.StatusCreated)

		if got := dbCount(t, "sms_node_members", "node_id = ?", bldg); got != 2 {
			t.Fatalf("building member count = %d, want 2", got)
		}
	})

	t.Run("CampaignDeliveryLedger", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/notification-campaigns", map[string]any{
			"title": "E2E SMS Roadworks Notice", "description": "Lane closure from Monday", "channel": "email",
		})
		wantStatus(t, "POST", "/api/sms/notification-campaigns", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "draft")
		cid := cfExtractID(t, body)

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/notification-campaigns/%d/targets", cid), map[string]any{"node_id": bldg})
		wantStatus(t, "POST", "/api/sms/notification-campaigns/{id}/targets", status, body, http.StatusCreated)

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/notification-campaigns/%d/targets", cid), map[string]any{"node_id": bldg})
		wantStatus(t, "POST", "/api/sms/notification-campaigns/{id}/targets", status, body, http.StatusConflict) // duplicate target

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/notification-campaigns/%d/audience", cid), nil)
		wantStatus(t, "POST", "/api/sms/notification-campaigns/{id}/audience", status, body, http.StatusOK)
		assertJSONField(t, body, "created", 2) // owner + tenant of the building
		assertJSONField(t, body, "skipped", 0)

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/notification-campaigns/%d/audience", cid), nil)
		wantStatus(t, "POST", "/api/sms/notification-campaigns/{id}/audience", status, body, http.StatusOK)
		assertJSONField(t, body, "created", 0) // idempotent: UNIQUE(campaign_id, stakeholder_id)
		assertJSONField(t, body, "skipped", 2)

		status, body = admin.Get(t, fmt.Sprintf("/api/sms/notification-campaigns/%d", cid))
		wantStatus(t, "GET", "/api/sms/notification-campaigns/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "campaign.total_notifications", 2)
		assertJSONField(t, body, "notifications_by_status.pending", 2)

		status, body = admin.Get(t, fmt.Sprintf("/api/sms/notification-campaigns/%d/notifications", cid))
		wantStatus(t, "GET", "/api/sms/notification-campaigns/{id}/notifications", status, body, http.StatusOK)
		var notifs []struct {
			ID            int    `json:"id"`
			StakeholderID int    `json:"stakeholder_id"`
			Status        string `json:"status"`
		}
		if err := json.Unmarshal(body, &notifs); err != nil || len(notifs) != 2 {
			t.Fatalf("expected 2 pending ledger rows: %s", snippet(body))
		}
		for _, n := range notifs {
			if n.Status != "pending" {
				t.Fatalf("ledger row %d status = %q, want pending", n.ID, n.Status)
			}
		}

		// Mark the first as sent: stamps sent_at and writes an outbound communication log entry.
		status, body = admin.Put(t, fmt.Sprintf("/api/sms/notifications/%d", notifs[0].ID), map[string]any{"status": "sent"})
		wantStatus(t, "PUT", "/api/sms/notifications/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "status", "sent")
		row := dbQueryOne(t, `SELECT status, sent_at, communication_id FROM sms_notifications WHERE id = ?`, notifs[0].ID)
		if cfStr(row["status"]) != "sent" || row["sent_at"] == nil || row["communication_id"] == nil {
			t.Fatalf("sent transition did not stamp sent_at/communication_id: %v", row)
		}
		if got := dbCount(t, "sms_communications", `direction = 'outbound' AND stakeholder_id = ?`, notifs[0].StakeholderID); got < 1 {
			t.Fatalf("no outbound communication log entry for sent notification")
		}

		// Mark the second as acknowledged: stamps acknowledged_at.
		status, body = admin.Put(t, fmt.Sprintf("/api/sms/notifications/%d", notifs[1].ID), map[string]any{"status": "acknowledged"})
		wantStatus(t, "PUT", "/api/sms/notifications/{id}", status, body, http.StatusOK)
		row = dbQueryOne(t, `SELECT status, acknowledged_at FROM sms_notifications WHERE id = ?`, notifs[1].ID)
		if cfStr(row["status"]) != "acknowledged" || row["acknowledged_at"] == nil {
			t.Fatalf("acknowledged transition did not stamp acknowledged_at: %v", row)
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/sms/notification-campaigns/%d", cid))
		wantStatus(t, "GET", "/api/sms/notification-campaigns/{id}", status, body, http.StatusOK)
		assertJSONField(t, body, "notifications_by_status.sent", 1)
		assertJSONField(t, body, "notifications_by_status.acknowledged", 1) // informed total = 2

		status, body = admin.Put(t, fmt.Sprintf("/api/sms/notifications/%d", notifs[0].ID), map[string]any{"status": "delivered"})
		wantStatus(t, "PUT", "/api/sms/notifications/{id}", status, body, http.StatusBadRequest) // invalid ledger status
	})
}

// UC-L3 — RACI matrices + work packages: versioned P3M-scoped matrices with
// single-accountable validation, and work-package node/status lifecycle.
func TestSMSRaciWorkPackages(t *testing.T) {
	admin := e2eEnv.admin
	projID := smsE2eP3MProject(t, admin, "E2E SMS RACI")
	shA := smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS RACI Party A"})
	shB := smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS RACI Party B"})

	t.Run("MatrixLifecycle", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/racis", map[string]any{"entity_type": "project", "entity_id": projID})
		wantStatus(t, "POST", "/api/sms/racis", status, body, http.StatusCreated)
		assertJSONField(t, body, "version", 1)
		assertJSONField(t, body, "status", "draft")
		raciID := cfExtractID(t, body)

		status, body = admin.Post(t, "/api/sms/racis", map[string]any{"entity_type": "project", "entity_id": 999999})
		wantStatus(t, "POST", "/api/sms/racis", status, body, http.StatusNotFound) // unknown P3M parent

		save := func(assignments []map[string]any) (int, []byte) {
			return admin.Put(t, fmt.Sprintf("/api/sms/racis/%d/assignments", raciID), map[string]any{"assignments": assignments})
		}
		status, body = save([]map[string]any{
			{"activity_code": "design_review", "stakeholder_id": shA, "value": "a"},
			{"activity_code": "design_review", "stakeholder_id": shB, "value": "r"},
		})
		wantStatus(t, "PUT", "/api/sms/racis/{id}/assignments", status, body, http.StatusOK)
		if got := dbCount(t, "sms_raci_assignments", "raci_id = ?", raciID); got != 2 {
			t.Fatalf("assignment count = %d, want 2", got)
		}

		status, body = save([]map[string]any{
			{"activity_code": "design_review", "stakeholder_id": shA, "value": "a"},
			{"activity_code": "design_review", "stakeholder_id": shB, "value": "a"}, // two accountables in one activity
		})
		wantStatus(t, "PUT", "/api/sms/racis/{id}/assignments", status, body, http.StatusBadRequest)

		status, body = save([]map[string]any{
			{"activity_code": "design_review", "stakeholder_id": 999999, "value": "a"}, // unknown stakeholder
		})
		wantStatus(t, "PUT", "/api/sms/racis/{id}/assignments", status, body, http.StatusNotFound)
	})

	t.Run("WorkPackages", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/work-packages", map[string]any{
			"title": "E2E SMS WP Lane Closure", "entity_type": "project", "entity_id": projID,
		})
		wantStatus(t, "POST", "/api/sms/work-packages", status, body, http.StatusCreated)
		assertJSONField(t, body, "status", "planned")
		wpID := cfExtractID(t, body)

		nodeKind := smsE2eNodeKindID(t, admin, "area")
		node := smsE2eCreateNode(t, admin, "E2E SMS WP Node", nodeKind, 0)

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/work-packages/%d/nodes", wpID), map[string]any{"node_id": node})
		wantStatus(t, "POST", "/api/sms/work-packages/{id}/nodes", status, body, http.StatusCreated)

		status, body = admin.Post(t, fmt.Sprintf("/api/sms/work-packages/%d/nodes", wpID), map[string]any{"node_id": node})
		wantStatus(t, "POST", "/api/sms/work-packages/{id}/nodes", status, body, http.StatusConflict) // duplicate link

		if got := dbCount(t, "sms_work_package_nodes", "work_package_id = ?", wpID); got != 1 {
			t.Fatalf("work package node count = %d, want 1", got)
		}

		status, body = admin.Put(t, fmt.Sprintf("/api/sms/work-packages/%d", wpID), map[string]any{"status": "in_progress"})
		wantStatus(t, "PUT", "/api/sms/work-packages/{id}", status, body, http.StatusOK)
		row := dbQueryOne(t, `SELECT status, actual_start FROM sms_work_packages WHERE id = ?`, wpID)
		if cfStr(row["status"]) != "in_progress" || row["actual_start"] == nil {
			t.Fatalf("in_progress transition did not stamp actual_start: %v", row)
		}
	})
}

// UC-L4 — Data movement: CSV preview/execute with error + duplicate detection,
// upsert idempotency, full export/import round-trip preserving IDs, and the
// GeoJSON register export.
func TestSMSDataMovement(t *testing.T) {
	admin := e2eEnv.admin

	// Existing stakeholder the CSV will try to duplicate by email.
	dupID := smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS Import Dup", "email": "dup@e2e-sms.test"})

	csvText := "name,title,organization,email,tier\n" +
		"E2E SMS Import New,,Acme E2E,new1@e2e-sms.test,tier_2\n" +
		"Bad Tier Row,,Acme E2E,badtier@e2e-sms.test,tier_9\n" + // invalid tier -> row error
		"Dup Name,,Other Org,dup@e2e-sms.test,tier_1\n"          // duplicate by email

	t.Run("PreviewReportsErrorsAndDuplicates", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/import/preview", map[string]any{"csv": csvText})
		wantStatus(t, "POST", "/api/sms/import/preview", status, body, http.StatusOK)
		var prev struct {
			TotalRows  int `json:"total_rows"`
			ValidRows  int `json:"valid_rows"`
			Errors     []struct {
				Row   int    `json:"row"`
				Field string `json:"field"`
			} `json:"errors"`
			Duplicates []struct {
				ExistingID int    `json:"existing_id"`
				MatchOn    string `json:"match_on"`
			} `json:"duplicates"`
		}
		if err := json.Unmarshal(body, &prev); err != nil {
			t.Fatalf("bad preview JSON: %v\n%s", err, snippet(body))
		}
		if prev.TotalRows != 3 || prev.ValidRows != 2 {
			t.Fatalf("preview rows = total %d / valid %d, want 3/2", prev.TotalRows, prev.ValidRows)
		}
		if len(prev.Errors) != 1 || prev.Errors[0].Field != "tier" {
			t.Fatalf("preview errors = %+v, want one tier error", prev.Errors)
		}
		if len(prev.Duplicates) != 1 || prev.Duplicates[0].MatchOn != "email" || prev.Duplicates[0].ExistingID != dupID {
			t.Fatalf("preview duplicates = %+v, want email match on existing %d", prev.Duplicates, dupID)
		}
	})

	t.Run("ExecuteCreateSkipsDuplicates", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/import/execute", map[string]any{"csv": csvText, "mode": "create"})
		wantStatus(t, "POST", "/api/sms/import/execute", status, body, http.StatusOK)
		var exec struct {
			Created int64 `json:"created"`
			Skipped int64 `json:"skipped"`
		}
		if err := json.Unmarshal(body, &exec); err != nil {
			t.Fatalf("bad execute JSON: %v\n%s", err, snippet(body))
		}
		if exec.Created != 1 || exec.Skipped != 1 {
			t.Fatalf("execute = created %d / skipped %d, want 1/1", exec.Created, exec.Skipped)
		}
		if got := dbCount(t, "sms_stakeholders", "email = ?", "new1@e2e-sms.test"); got != 1 {
			t.Fatalf("imported stakeholder count = %d, want 1", got)
		}
	})

	t.Run("UpsertDoesNotDuplicate", func(t *testing.T) {
		status, body := admin.Post(t, "/api/sms/import/execute", map[string]any{"csv": csvText, "mode": "upsert"})
		wantStatus(t, "POST", "/api/sms/import/execute", status, body, http.StatusOK)
		var exec struct {
			Created int64 `json:"created"`
		}
		if err := json.Unmarshal(body, &exec); err != nil {
			t.Fatalf("bad execute JSON: %v\n%s", err, snippet(body))
		}
		if exec.Created != 0 {
			t.Fatalf("upsert created %d new rows, want 0 (matched row must be updated in place)", exec.Created)
		}
		if got := dbCount(t, "sms_stakeholders", "email = ?", "new1@e2e-sms.test"); got != 1 {
			t.Fatalf("stakeholder count after upsert = %d, want 1", got)
		}
	})

	t.Run("FullExportRoundTripPreservesIDs", func(t *testing.T) {
		status, body := admin.Get(t, "/api/sms/export/full")
		wantStatus(t, "GET", "/api/sms/export/full", status, body, http.StatusOK)
		var full struct {
			Module        string           `json:"module"`
			SchemaVersion int              `json:"schema_version"`
			Stakeholders  []map[string]any `json:"stakeholders"`
		}
		if err := json.Unmarshal(body, &full); err != nil || full.Module != "sms" || len(full.Stakeholders) == 0 {
			t.Fatalf("bad full export: %s", snippet(body))
		}
		var row map[string]any
		for _, s := range full.Stakeholders {
			if cfStr(s["email"]) == "dup@e2e-sms.test" {
				row = s
				break
			}
		}
		if row == nil {
			t.Fatalf("exported register missing dup stakeholder")
		}

		status, body = admin.Delete(t, fmt.Sprintf("/api/sms/stakeholders/%d", dupID))
		wantStatus(t, "DELETE", "/api/sms/stakeholders/{id}", status, body, http.StatusNoContent)

		status, body = admin.Post(t, "/api/sms/import/full", map[string]any{
			"module":         "sms",
			"schema_version": full.SchemaVersion,
			"stakeholders":   []map[string]any{row},
		})
		wantStatus(t, "POST", "/api/sms/import/full", status, body, http.StatusOK)
		var imp struct {
			Wiped    bool           `json:"wiped"`
			Restored map[string]int `json:"restored"`
		}
		if err := json.Unmarshal(body, &imp); err != nil || imp.Wiped || imp.Restored["sms_stakeholders"] != 1 {
			t.Fatalf("full import response wrong: %s", snippet(body))
		}

		status, body = admin.Get(t, fmt.Sprintf("/api/sms/stakeholders/%d", dupID))
		wantStatus(t, "GET", "/api/sms/stakeholders/{id}", status, body, http.StatusOK) // same ID survived the round trip
		assertJSONField(t, body, "name", "Dup Name") // upsert renamed it in place; export restored that state
	})

	t.Run("GeoJSONExport", func(t *testing.T) {
		smsE2eCreateStakeholder(t, admin, map[string]any{"name": "E2E SMS Mapped Co", "location_lat": -41.29, "location_lon": 174.78})

		status, body := admin.Get(t, "/api/sms/export/geojson")
		wantStatus(t, "GET", "/api/sms/export/geojson", status, body, http.StatusOK)
		var geo struct {
			Type     string `json:"type"`
			Features []struct {
				Geometry   map[string]any `json:"geometry"`
				Properties map[string]any `json:"properties"`
			} `json:"features"`
		}
		if err := json.Unmarshal(body, &geo); err != nil || geo.Type != "FeatureCollection" {
			t.Fatalf("bad geojson export: %s", snippet(body))
		}
		found := false
		for _, f := range geo.Features {
			if cfStr(f.Properties["name"]) == "E2E SMS Mapped Co" {
				coords, _ := f.Geometry["coordinates"].([]any)
				if len(coords) != 2 || coords[0].(float64) != 174.78 || coords[1].(float64) != -41.29 {
					t.Fatalf("geojson coordinates must be [lon, lat]: %v", f.Geometry["coordinates"])
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("mapped stakeholder missing from geojson export")
		}
	})
}

// UC-L5 — Reporting rollups: dashboard totals consistent with the database,
// pilot baselines vs live metrics, and the data-quality report.
func TestSMSReportingRollups(t *testing.T) {
	admin := e2eEnv.admin

	t.Run("DashboardTotalsMatchDB", func(t *testing.T) {
		status, body := admin.Get(t, "/api/sms/dashboard")
		wantStatus(t, "GET", "/api/sms/dashboard", status, body, http.StatusOK)
		var dash struct {
			Totals map[string]int `json:"totals"`
		}
		if err := json.Unmarshal(body, &dash); err != nil {
			t.Fatalf("bad dashboard JSON: %v\n%s", err, snippet(body))
		}
		if got := dash.Totals["stakeholders"]; got != dbCount(t, "sms_stakeholders", "") {
			t.Fatalf("dashboard stakeholders = %d, want %d (db)", got, dbCount(t, "sms_stakeholders", ""))
		}
		if got := dash.Totals["open_issues"]; got != dbCount(t, "sms_issues", `status = 'open'`) {
			t.Fatalf("dashboard open_issues = %d, want %d (db)", got, dbCount(t, "sms_issues", `status = 'open'`))
		}
		var keys map[string]any
		if err := json.Unmarshal(body, &keys); err != nil {
			t.Fatalf("bad dashboard JSON: %v", err)
		}
		for _, k := range []string{"by_classification", "health_buckets"} {
			if _, ok := keys[k]; !ok {
				t.Fatalf("dashboard missing %q: %s", k, snippet(body))
			}
		}
	})

	t.Run("PilotBaselinesVsCurrents", func(t *testing.T) {
		status, body := admin.Put(t, "/api/sms/pilot-baselines", map[string]any{
			"metric_key": "stakeholder_count", "baseline_value": 1.0, "target_value": 999999.0,
		})
		wantStatus(t, "PUT", "/api/sms/pilot-baselines", status, body, http.StatusOK)
		assertJSONField(t, body, "metric_key", "stakeholder_count")

		status, body = admin.Get(t, "/api/sms/pilot-metrics")
		wantStatus(t, "GET", "/api/sms/pilot-metrics", status, body, http.StatusOK)
		var pm struct {
			Metrics []struct {
				MetricKey     string   `json:"metric_key"`
				BaselineValue *float64 `json:"baseline_value"`
				TargetValue   *float64 `json:"target_value"`
				CurrentValue  *float64 `json:"current_value"`
				Delta         float64  `json:"delta"`
				Status        string   `json:"status"`
			} `json:"metrics"`
		}
		if err := json.Unmarshal(body, &pm); err != nil {
			t.Fatalf("bad pilot-metrics JSON: %v\n%s", err, snippet(body))
		}
		var m *struct {
			MetricKey     string   `json:"metric_key"`
			BaselineValue *float64 `json:"baseline_value"`
			TargetValue   *float64 `json:"target_value"`
			CurrentValue  *float64 `json:"current_value"`
			Delta         float64  `json:"delta"`
			Status        string   `json:"status"`
		}
		for i := range pm.Metrics {
			if pm.Metrics[i].MetricKey == "stakeholder_count" {
				m = &pm.Metrics[i]
			}
		}
		if m == nil || m.CurrentValue == nil {
			t.Fatalf("pilot-metrics missing stakeholder_count: %s", snippet(body))
		}
		want := dbCount(t, "sms_stakeholders", "")
		if int(*m.CurrentValue) != want {
			t.Fatalf("current_value = %v, want %d (db)", *m.CurrentValue, want)
		}
		if m.Delta != float64(want)-1 {
			t.Fatalf("delta = %v, want %d (current - baseline 1)", m.Delta, want-1)
		}
		if m.Status != "behind" {
			t.Fatalf("status = %q, want behind (current %d < target 999999)", m.Status, want)
		}
	})

	t.Run("DataQualityReport", func(t *testing.T) {
		status, body := admin.Get(t, "/api/sms/data-quality")
		wantStatus(t, "GET", "/api/sms/data-quality", status, body, http.StatusOK)
		var dq struct {
			Totals map[string]int `json:"totals"`
		}
		if err := json.Unmarshal(body, &dq); err != nil {
			t.Fatalf("bad data-quality JSON: %v\n%s", err, snippet(body))
		}
		if got := dq.Totals["stakeholders"]; got != dbCount(t, "sms_stakeholders", "") {
			t.Fatalf("data-quality stakeholders = %d, want %d (db)", got, dbCount(t, "sms_stakeholders", ""))
		}
	})
}

// UC-L6 — Public grievance intake: unauthenticated submissions create a
// minimal register entry for unknown submitters, match existing ones by email,
// and validate required fields.
func TestSMSGrievanceIntake(t *testing.T) {
	public := newClient(e2eEnv.baseURL) // no session cookie

	t.Run("UnknownSubmitterCreatesRegisterEntry", func(t *testing.T) {
		status, body := public.Post(t, "/api/sms/grievances/intake", map[string]any{
			"name":        "E2E Intake Resident",
			"email":       "intake@e2e-sms.test",
			"description": "The roadworks noise is unbearable at night",
		})
		wantStatus(t, "POST", "/api/sms/grievances/intake", status, body, http.StatusCreated)
		var res struct {
			GrievanceID        int  `json:"grievance_id"`
			StakeholderID      int  `json:"stakeholder_id"`
			MatchedStakeholder bool `json:"matched_stakeholder"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.GrievanceID == 0 || res.StakeholderID == 0 {
			t.Fatalf("bad intake response: %s", snippet(body))
		}
		if res.MatchedStakeholder {
			t.Fatalf("first intake must not match an existing stakeholder")
		}

		g := dbQueryOne(t, `SELECT status, severity FROM sms_grievances WHERE id = ?`, res.GrievanceID)
		if cfStr(g["status"]) != "received" || cfStr(g["severity"]) != "medium" {
			t.Fatalf("default grievance state wrong: %v", g)
		}

		s := dbQueryOne(t, `SELECT notes FROM sms_stakeholders WHERE id = ?`, res.StakeholderID)
		if cfStr(s["notes"]) != "Created via public grievance intake" {
			t.Fatalf("intake-created stakeholder notes = %q", s["notes"])
		}
	})

	t.Run("KnownSubmitterMatchesExisting", func(t *testing.T) {
		status, body := public.Post(t, "/api/sms/grievances/intake", map[string]any{
			"name":        "E2E Intake Resident Again",
			"email":       "intake@e2e-sms.test",
			"description": "Second complaint about the same works",
		})
		wantStatus(t, "POST", "/api/sms/grievances/intake", status, body, http.StatusCreated)
		var res struct {
			StakeholderID      int  `json:"stakeholder_id"`
			MatchedStakeholder bool `json:"matched_stakeholder"`
		}
		if err := json.Unmarshal(body, &res); err != nil || !res.MatchedStakeholder {
			t.Fatalf("second intake must match the existing stakeholder: %s", snippet(body))
		}
		first := dbQueryOne(t, `SELECT id FROM sms_stakeholders WHERE lower(email) = ?`, "intake@e2e-sms.test")
		if cfInt(first["id"]) != res.StakeholderID {
			t.Fatalf("matched stakeholder %d != existing %v", res.StakeholderID, first["id"])
		}
		if got := dbCount(t, "sms_stakeholders", `lower(email) = ?`, "intake@e2e-sms.test"); got != 1 {
			t.Fatalf("duplicate register entry created: %d rows for one email", got)
		}
	})

	t.Run("Negatives", func(t *testing.T) {
		status, body := public.Post(t, "/api/sms/grievances/intake", map[string]any{
			"name": "E2E Intake Short", "description": "short", // < 10 chars
		})
		wantStatus(t, "POST", "/api/sms/grievances/intake", status, body, http.StatusBadRequest)

		status, body = public.Post(t, "/api/sms/grievances/intake", map[string]any{
			"email": "x@e2e-sms.test", "description": "no name given here at all", // missing name
		})
		wantStatus(t, "POST", "/api/sms/grievances/intake", status, body, http.StatusBadRequest)
	})
}
