package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// UC-P3M-WS: promote a P3M project to a workspace. The bridge link keeps the
// project tracked in P3M; renaming the workspace afterwards must not break it.
func TestP3MPromoteProjectToWorkspace(t *testing.T) {
	c := e2eEnv.admin
	fid := p3mE2eCreatePortfolio(t, c, "Promotion Portfolio")
	pid := p3mE2eCreateProject(t, c, "Promotion Candidate", map[string]any{"portfolio_id": fid})

	var wsID int
	t.Run("promote", func(t *testing.T) {
		status, body := c.Post(t, fmt.Sprintf("/api/p3m/projects/%d/promote-to-workspace", pid), map[string]any{})
		wantStatus(t, "POST", "/api/p3m/projects/{id}/promote-to-workspace", status, body, http.StatusCreated)

		var out struct {
			ProjectID int `json:"project_id"`
			Workspace struct {
				ID          int    `json:"id"`
				Key         string `json:"key"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"workspace"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("promote response not JSON: %s", snippet(body))
		}
		wsID = out.Workspace.ID
		if out.ProjectID != pid || wsID == 0 {
			t.Fatalf("promote response mismatch: %+v", out)
		}
		if out.Workspace.Name != "Promotion Candidate" {
			t.Fatalf("workspace name should default to project title, got %q", out.Workspace.Name)
		}

		row := dbQueryOne(t, `SELECT key FROM workspaces WHERE id = ?`, wsID)
		key := fmt.Sprint(row["key"])
		if len(key) < 2 || len(key) > 10 {
			t.Fatalf("derived workspace key out of contract: %q", key)
		}

		link := dbQueryOne(t, `SELECT workspace_id FROM workspace_p3m_projects WHERE p3m_project_id = ?`, pid)
		if fmt.Sprint(link["workspace_id"]) != fmt.Sprint(wsID) {
			t.Fatalf("bridge row missing or wrong: %v", link)
		}

		// The creator must hold a role in the promoted workspace (the shared
		// creation service seeds it; without it nobody could enter).
		adminID := fmt.Sprint(dbQueryOne(t, `SELECT id FROM users WHERE username = ?`, adminUsername)["id"])
		if n := dbCount(t, "user_workspace_roles", "workspace_id = ? AND user_id = ?", wsID, adminID); n == 0 {
			t.Fatalf("creator has no role in promoted workspace %d", wsID)
		}
	})

	t.Run("re-promote-conflict", func(t *testing.T) {
		status, body := c.Post(t, fmt.Sprintf("/api/p3m/projects/%d/promote-to-workspace", pid), map[string]any{})
		wantStatus(t, "POST", "/api/p3m/projects/{id}/promote-to-workspace (re-promote)", status, body, http.StatusConflict)
	})

	t.Run("rename-keeps-link", func(t *testing.T) {
		newName := "Promoted Renamed"
		status, body := v2Patch(t, c, fmt.Sprintf("/api/v2/workspaces/%d", wsID), map[string]any{"name": newName})
		wantStatus(t, "PATCH", "/api/v2/workspaces/{id} (rename)", status, body, http.StatusOK)

		status, body = c.Get(t, fmt.Sprintf("/api/p3m/projects/%d/workspaces", pid))
		wantStatus(t, "GET", "/api/p3m/projects/{id}/workspaces (after rename)", status, body, http.StatusOK)
		var refs []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &refs); err != nil || len(refs) == 0 {
			t.Fatalf("project workspaces list empty or invalid after rename: %s", snippet(body))
		}
		found := false
		for _, ref := range refs {
			if ref.ID == wsID && ref.Name == newName {
				found = true
			}
		}
		if !found {
			t.Fatalf("renamed workspace no longer tracked on P3M project: %s", snippet(body))
		}
	})
}
