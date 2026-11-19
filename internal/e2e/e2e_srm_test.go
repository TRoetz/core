// e2e_srm_test.go covers the SRM (Supplier Relationship Management) use cases:
// UC-N1 supplier registration CRUD with tier segmentation and exact list filters,
// UC-N2 planning rollup consistency (summary aggregates track seeded rows),
// UC-N3 cross-store identity between the SMS register and the SRM stakeholder store.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// UC-N1 Supplier registration: supplier CRUD with segmentation attributes;
// list filters return exact matches for the seeded rows.
func TestSRMSupplierRegister(t *testing.T) {
	const name = "E2E SRM Acme Widgets Ltd"

	var supplierID int
	t.Run("Create", func(t *testing.T) {
		st, b := e2eEnv.admin.Post(t, "/api/srm/suppliers", map[string]any{
			"company_name": name,
			"tier":         "tier_1",
			"business_id":  "E2E-BIZ-001",
		})
		wantStatus(t, "POST", "/api/srm/suppliers", st, b, http.StatusCreated)
		assertJSONField(t, b, "company_name", name)
		assertJSONField(t, b, "tier", "tier_1")
		assertJSONField(t, b, "relationship_state", "registered")

		var s struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(b, &s); err != nil || s.ID <= 0 {
			t.Fatalf("created supplier has no id: %s", b)
		}
		supplierID = s.ID

		row := dbQueryOne(t, "SELECT tier, relationship_state FROM srm_suppliers WHERE id = ?", supplierID)
		if cfStr(row["tier"]) != "tier_1" || cfStr(row["relationship_state"]) != "registered" {
			t.Fatalf("supplier not persisted with expected segmentation: %v", row)
		}
	})

	t.Run("ListFilterExact", func(t *testing.T) {
		q := url.QueryEscape(name)
		st, b := e2eEnv.admin.Get(t, "/api/srm/suppliers?q="+q)
		wantStatus(t, "GET", "/api/srm/suppliers?q=...", st, b, http.StatusOK)
		var list []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatalf("supplier list is not a JSON array: %s", b)
		}
		if len(list) != 1 || list[0].ID != supplierID {
			t.Fatalf("q filter did not return exactly the seeded supplier: got %d rows, want 1 (id %d)", len(list), supplierID)
		}

		// The same name under a different tier filter must be excluded.
		st, b = e2eEnv.admin.Get(t, "/api/srm/suppliers?tier=tier_2&q="+q)
		wantStatus(t, "GET", "/api/srm/suppliers?tier=...&q=...", st, b, http.StatusOK)
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatalf("supplier list is not a JSON array: %s", b)
		}
		for _, s := range list {
			if s.ID == supplierID {
				t.Fatalf("tier=tier_2 filter returned tier_1 supplier %d", supplierID)
			}
		}
	})

	t.Run("Update", func(t *testing.T) {
		st, b := e2eEnv.admin.Put(t, fmt.Sprintf("/api/srm/suppliers/%d", supplierID), map[string]any{
			"tier": "tier_2",
		})
		wantStatus(t, "PUT", "/api/srm/suppliers/{id}", st, b, http.StatusOK)
		assertJSONField(t, b, "tier", "tier_2")

		row := dbQueryOne(t, "SELECT tier FROM srm_suppliers WHERE id = ?", supplierID)
		if cfStr(row["tier"]) != "tier_2" {
			t.Fatalf("supplier tier not updated in DB: %v", row)
		}
	})

	t.Run("Negatives", func(t *testing.T) {
		st, b := e2eEnv.admin.Post(t, "/api/srm/suppliers", map[string]any{"tier": "tier_1"})
		wantStatus(t, "POST", "/api/srm/suppliers (missing company_name)", st, b, http.StatusBadRequest)

		st, b = e2eEnv.admin.Post(t, "/api/srm/suppliers", map[string]any{
			"company_name": "E2E SRM Bad Tier Ltd",
			"tier":         "tier_9",
		})
		wantStatus(t, "POST", "/api/srm/suppliers (invalid tier)", st, b, http.StatusBadRequest)

		st, b = e2eEnv.admin.Get(t, "/api/srm/suppliers/999999")
		wantStatus(t, "GET", "/api/srm/suppliers/{unknown}", st, b, http.StatusNotFound)
	})

	t.Run("Delete", func(t *testing.T) {
		st, b := e2eEnv.admin.Delete(t, fmt.Sprintf("/api/srm/suppliers/%d", supplierID))
		wantStatus(t, "DELETE", "/api/srm/suppliers/{id}", st, b, http.StatusNoContent)
		if n := dbCount(t, "srm_suppliers", "id = ?", supplierID); n != 0 {
			t.Fatalf("supplier row still present after delete")
		}
	})
}

// UC-N2 Planning & monitoring: seeded planning entities are reflected in the
// module summary rollup (counts and the computed active contract value).
func TestSRMPlanningRollup(t *testing.T) {
	type srmSummary struct {
		Suppliers struct {
			Total  int            `json:"total"`
			ByTier map[string]int `json:"by_tier"`
		} `json:"suppliers"`
		Contracts struct {
			ByStatus       map[string]int `json:"by_status"`
			ActiveValueNZD float64        `json:"active_value_nzd"`
		} `json:"contracts"`
	}

	getSummary := func(t *testing.T) srmSummary {
		t.Helper()
		st, b := e2eEnv.admin.Get(t, "/api/srm/summary")
		wantStatus(t, "GET", "/api/srm/summary", st, b, http.StatusOK)
		var s srmSummary
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatalf("summary is not JSON: %s", b)
		}
		return s
	}

	before := getSummary(t)

	st, b := e2eEnv.admin.Post(t, "/api/srm/suppliers", map[string]any{
		"company_name": "E2E SRM Rollup Supplier Ltd",
		"tier":         "tier_3",
	})
	wantStatus(t, "POST", "/api/srm/suppliers (rollup)", st, b, http.StatusCreated)
	var sup struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(b, &sup); err != nil || sup.ID <= 0 {
		t.Fatalf("created supplier has no id: %s", b)
	}

	const value = 5000.0
	st, b = e2eEnv.admin.Post(t, "/api/srm/contracts", map[string]any{
		"supplier_id":     sup.ID,
		"contract_number": "E2E-CTR-ROLLUP-1",
		"title":           "Rollup coverage contract",
		"status":          "active",
		"total_value":     value,
		"start_date":      "2026-01-05T00:00:00Z",
		"end_date":        "2027-01-05T00:00:00Z",
	})
	wantStatus(t, "POST", "/api/srm/contracts (rollup)", st, b, http.StatusCreated)

	row := dbQueryOne(t, `SELECT status, total_value FROM srm_contracts WHERE supplier_id = ? AND contract_number = 'E2E-CTR-ROLLUP-1'`, sup.ID)
	if cfStr(row["status"]) != "active" {
		t.Fatalf("contract not persisted as active: %v", row)
	}

	after := getSummary(t)
	if after.Suppliers.Total != before.Suppliers.Total+1 {
		t.Fatalf("summary supplier total did not increase by 1: %d -> %d", before.Suppliers.Total, after.Suppliers.Total)
	}
	if got, want := after.Suppliers.ByTier["tier_3"], before.Suppliers.ByTier["tier_3"]+1; got != want {
		t.Fatalf("summary tier_3 count did not increase by 1: %d -> %d", before.Suppliers.ByTier["tier_3"], got)
	}
	if got, want := after.Contracts.ByStatus["active"], before.Contracts.ByStatus["active"]+1; got != want {
		t.Fatalf("summary active contract count did not increase by 1: %d -> %d", before.Contracts.ByStatus["active"], got)
	}
	delta := after.Contracts.ActiveValueNZD - before.Contracts.ActiveValueNZD
	if delta < value-0.01 || delta > value+0.01 {
		t.Fatalf("summary active_value_nzd did not increase by %v: delta %v", value, delta)
	}

	// OBSERVED: DeleteSupplier has no pre-check for the srm_contracts FK; with
	// an active contract the ON DELETE RESTRICT constraint fails and the error
	// surfaces as 500 (respondInternalError). The supplier must survive.
	st, b = e2eEnv.admin.Delete(t, fmt.Sprintf("/api/srm/suppliers/%d", sup.ID))
	wantStatus(t, "DELETE", "/api/srm/suppliers/{id} (active contract)", st, b, http.StatusInternalServerError)
	if n := dbCount(t, "srm_suppliers", "id = ?", sup.ID); n != 1 {
		t.Fatalf("supplier with active contract should survive the delete; %d rows remain", n)
	}
}

// UC-N3 Cross-store identity: an SRM stakeholder whose email matches an SMS
// register row appears in the cross-store duplicate report; linking via API
// sets the FK column.
func TestSRMCrossStoreIdentity(t *testing.T) {
	const (
		email = "e2e-srm-cross@example.test"
		name  = "E2E Cross Store Person"
	)

	st, b := e2eEnv.admin.Post(t, "/api/sms/stakeholders", map[string]any{
		"name":  name,
		"email": email,
	})
	wantStatus(t, "POST", "/api/sms/stakeholders (cross-store)", st, b, http.StatusCreated)
	var sms struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(b, &sms); err != nil || sms.ID <= 0 {
		t.Fatalf("created SMS stakeholder has no id: %s", b)
	}

	st, b = e2eEnv.admin.Post(t, "/api/srm/stakeholders", map[string]any{
		"full_name":   name,
		"email":       email,
		"is_external": true,
	})
	wantStatus(t, "POST", "/api/srm/stakeholders (cross-store)", st, b, http.StatusCreated)
	var srm struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(b, &srm); err != nil || srm.ID <= 0 {
		t.Fatalf("created SRM stakeholder has no id: %s", b)
	}

	type dupRow struct {
		Store          string `json:"store"`
		ID             int    `json:"id"`
		CandidateSMSID int    `json:"candidate_sms_id"`
		LinkedSMSID    *int   `json:"linked_sms_id"`
		MatchOn        string `json:"match_on"`
	}

	findSRMRow := func(t *testing.T) dupRow {
		t.Helper()
		st, b := e2eEnv.admin.Get(t, "/api/sms/cross-store-duplicates")
		wantStatus(t, "GET", "/api/sms/cross-store-duplicates", st, b, http.StatusOK)
		var rows []dupRow
		if err := json.Unmarshal(b, &rows); err != nil {
			t.Fatalf("duplicate report is not a JSON array: %s", b)
		}
		for _, r := range rows {
			if r.Store == "srm" && r.ID == srm.ID {
				return r
			}
		}
		t.Fatalf("cross-store duplicate report has no row for SRM stakeholder %d (report: %s)", srm.ID, b)
		return dupRow{}
	}

	row := findSRMRow(t)
	if row.MatchOn != "email" {
		t.Fatalf("SRM duplicate matched on %q, want email", row.MatchOn)
	}
	if row.CandidateSMSID != sms.ID {
		t.Fatalf("duplicate report candidate_sms_id = %d, want SMS stakeholder %d", row.CandidateSMSID, sms.ID)
	}

	st, b = e2eEnv.admin.Post(t, "/api/sms/cross-store-duplicates/link", map[string]any{
		"store":              "srm",
		"id":                 srm.ID,
		"sms_stakeholder_id": sms.ID,
	})
	wantStatus(t, "POST", "/api/sms/cross-store-duplicates/link", st, b, http.StatusNoContent)

	dbRow := dbQueryOne(t, "SELECT sms_stakeholder_id FROM srm_stakeholders WHERE id = ?", srm.ID)
	if got := cfInt(dbRow["sms_stakeholder_id"]); got != sms.ID {
		t.Fatalf("srm_stakeholders.sms_stakeholder_id = %d, want %d", got, sms.ID)
	}

	linked := findSRMRow(t)
	if linked.LinkedSMSID == nil || *linked.LinkedSMSID != sms.ID {
		t.Fatalf("duplicate report does not show the link: linked_sms_id=%v, want %d", linked.LinkedSMSID, sms.ID)
	}
}
