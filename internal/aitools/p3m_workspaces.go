package aitools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"windshift/internal/auth"
	"windshift/internal/database"
	"windshift/internal/services"
)

// This file adds the MCP/AI tool for promoting a P3M project to a workspace,
// mirroring POST /api/p3m/projects/{id}/promote-to-workspace: create a workspace
// from the project's identity and link it through workspace_p3m_projects so P3M
// tracking continues unchanged. Access mirrors the HTTP surface: the global
// "p3m.view" permission gates the tool; writes additionally require the p3m.write
// token scope.

type promoteP3MProjectToWorkspaceArgs struct {
	ProjectID   int    `json:"project_id" jsonschema:"The P3M project to promote."`
	Key         string `json:"key,omitempty" jsonschema:"Optional workspace key (2-10 uppercase A-Z0-9); derived from the project title when omitted."`
	Name        string `json:"name,omitempty" jsonschema:"Optional workspace name; defaults to the project title."`
	Description string `json:"description,omitempty" jsonschema:"Optional workspace description; defaults to the project description."`
}

func init() {
	Register(Default, Tool[promoteP3MProjectToWorkspaceArgs]{
		Name:        "promote_p3m_project_to_workspace",
		Group:       CapabilityP3M,
		Access:      AccessWrite,
		Risk:        RiskMedium,
		Description: "Promote a P3M project to a workspace: creates a workspace from the project's title/description (or caller-supplied name/key), links it through the project's workspace bridge so P3M tracking continues unchanged, and makes the acting user its creator. Fails if the project is already linked to any workspace.",
		Scopes:      []string{auth.ScopeP3MWrite},
		Run: func(ctx context.Context, env *Env, args promoteP3MProjectToWorkspaceArgs) (any, error) {
			ok, err := aiRequireP3MView(env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return map[string]string{"error": "permission denied"}, nil
			}

			var title, description string
			err = env.DB.QueryRowContext(ctx, `SELECT title, description FROM p3m_projects WHERE id = ?`, args.ProjectID).Scan(&title, &description)
			if err != nil {
				return map[string]string{"error": "project not found"}, nil
			}

			var linked int
			if err := env.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_p3m_projects WHERE p3m_project_id = ?`, args.ProjectID).Scan(&linked); err != nil {
				return nil, err
			}
			if linked > 0 {
				return map[string]string{"error": "project is already linked to a workspace"}, nil
			}

			name := strings.TrimSpace(args.Name)
			if name == "" {
				name = title
			}
			desc := args.Description
			if desc == "" {
				desc = description
			}

			key := strings.ToUpper(strings.TrimSpace(args.Key))
			if key != "" && !services.ValidWorkspaceKey(key) {
				return map[string]string{"error": "key must be 2-10 uppercase letters or digits"}, nil
			}
			if key == "" {
				key, err = aiAllocateP3MPromotionKey(ctx, env.DB, name, args.ProjectID)
				if err != nil {
					return nil, err
				}
			}

			wsSvc := services.NewWorkspaceService(env.DB)
			res, err := wsSvc.Create(ctx, services.CreateWorkspaceParams{
				Name:        name,
				Key:         key,
				Description: desc,
				CreatorID:   env.UserID,
			})
			if err != nil {
				return nil, fmt.Errorf("create workspace from project %d: %w", args.ProjectID, err)
			}

			ws := res.Workspace
			if _, err := env.DB.Exec(`INSERT INTO workspace_p3m_projects (workspace_id, p3m_project_id) VALUES (?, ?)`, ws.ID, args.ProjectID); err != nil {
				return nil, fmt.Errorf("link project %d to workspace %d: %w", args.ProjectID, ws.ID, err)
			}

			env.AuditWrite("p3m_projects", args.ProjectID, "promote_p3m_project_to_workspace", title+" promoted to workspace "+ws.Key)
			return map[string]any{"project_id": args.ProjectID, "workspace": ws}, nil
		},
	})
}

// aiAllocateP3MPromotionKey mirrors handlers.allocateP3MPromotionKey: derive a
// workspace key from the project name (uppercase alphanumerics, ≤10 chars;
// "WS"+id fallback) and append a counter until it is free.
func aiAllocateP3MPromotionKey(ctx context.Context, db database.Database, name string, projectID int) (string, error) {
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
