package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"windshift/internal/database"
	"windshift/internal/services"
)

// PromoteProjectToWorkspace handles POST /api/p3m/projects/{id}/promote-to-workspace.
// It creates a workspace from the project's identity (title → name, description copied;
// key derived or caller-supplied), links it through workspace_p3m_projects so P3M
// tracking continues unchanged, and seeds the acting user as creator via the shared
// creation service. A project already linked to any workspace is rejected with 409:
// promotion means "become a workspace" — one link per project; use PUT .../workspaces
// to re-point an existing link.
func (h *P3MHandler) PromoteProjectToWorkspace(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	var in struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := newJSONDecoder(w, r).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		respondBadRequest(w, r, "Invalid request body")
		return
	}

	ctx := r.Context()
	proj, err := h.getProjectRow(ctx, id)
	if err != nil {
		respondNotFound(w, r, "project")
		return
	}

	var linked int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_p3m_projects WHERE p3m_project_id = ?`, id).Scan(&linked); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if linked > 0 {
		respondConflict(w, r, "project is already linked to a workspace; use PUT /p3m/projects/{id}/workspaces to change the link")
		return
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = proj.Title
	}
	description := in.Description
	if description == "" {
		description = proj.Description
	}

	key := strings.ToUpper(strings.TrimSpace(in.Key))
	if key != "" && !services.ValidWorkspaceKey(key) {
		respondBadRequest(w, r, "key must be 2-10 uppercase letters or digits")
		return
	}
	if key == "" {
		key, err = allocateP3MPromotionKey(ctx, h.db, name, id)
		if err != nil {
			respondInternalError(w, r, err)
			return
		}
	}

	wsSvc := services.NewWorkspaceService(h.db)
	res, err := wsSvc.Create(ctx, services.CreateWorkspaceParams{
		Name:        name,
		Key:         key,
		Description: description,
		CreatorID:   user.ID,
	})
	if err != nil {
		respondInternalError(w, r, fmt.Errorf("create workspace from project %d: %w", id, err))
		return
	}

	ws := res.Workspace
	if _, err := h.db.Exec(`INSERT INTO workspace_p3m_projects (workspace_id, p3m_project_id) VALUES (?, ?)`, ws.ID, id); err != nil {
		respondInternalError(w, r, fmt.Errorf("link project %d to workspace %d: %w", id, ws.ID, err))
		return
	}

	if h.cacheInvalidator != nil {
		if err := h.cacheInvalidator.Apply(services.AuthorizationInvalidation{
			ResetPermissions:        true,
			ActiveWorkspacesChanged: true,
			WorkspaceKeysChanged:    true,
		}); err != nil {
			respondInternalError(w, r, fmt.Errorf("invalidate authorization caches after promotion: %w", err))
			return
		}
	}

	h.p3mAudit(r, user, "p3m.project.promote_to_workspace", "p3m_project", &id, proj.Title+" promoted to workspace "+ws.Key, map[string]any{
		"workspace_id":  ws.ID,
		"workspace_key": ws.Key,
	})

	respondJSONCreated(w, map[string]any{"project_id": id, "workspace": ws})
}

// allocateP3MPromotionKey derives a workspace key from the promoted project's name:
// uppercase alphanumerics truncated to 10 characters; falls back to "WS" + project ID
// when fewer than two usable characters remain. Appends a counter until the key is free
// (workspaces.key is UNIQUE); gives up after 50 attempts.
func allocateP3MPromotionKey(ctx context.Context, db database.Database, name string, projectID int) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 10 {
				break
			}
		}
	}
	base := b.String()
	if len(base) < 2 {
		base = "WS" + strconv.Itoa(projectID)
		if len(base) > 10 {
			base = base[len(base)-8:]
		}
	}
	key := base
	for i := 2; ; i++ {
		var exists bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE key = ?)`, key).Scan(&exists)
		if err != nil {
			return "", err
		}
		if !exists {
			return key, nil
		}
		if i > 50 {
			return "", fmt.Errorf("could not allocate a free workspace key for project %d", projectID)
		}
		suffix := strconv.Itoa(i)
		if len(base)+len(suffix) > 10 {
			base = base[:10-len(suffix)]
		}
		key = base + suffix
	}
}
