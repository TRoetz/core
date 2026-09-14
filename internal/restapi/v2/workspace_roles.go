package v2

import (
	"errors"
	"net/http"
	"time"

	"windshift/internal/models"
	"windshift/internal/services"
)

type workspaceRoleDTO struct {
	ID                 int                 `json:"id"`
	BuiltinKey         *string             `json:"builtin_key"`
	Name               string              `json:"name"`
	DisplayName        string              `json:"display_name"`
	Description        string              `json:"description"`
	IsSystem           bool                `json:"is_system"`
	PermissionsEnabled bool                `json:"permissions_enabled"`
	Permissions        []models.Permission `json:"permissions,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

func workspaceRoleDTOFromModel(role *models.WorkspaceRole) workspaceRoleDTO {
	builtinKey := nullableString(role.BuiltinKey)
	return workspaceRoleDTO{
		ID:                 role.ID,
		BuiltinKey:         builtinKey,
		Name:               role.Name,
		DisplayName:        role.DisplayName,
		Description:        role.Description,
		IsSystem:           role.IsSystem,
		PermissionsEnabled: role.PermissionsEnabled,
		Permissions:        role.Permissions,
		CreatedAt:          role.CreatedAt,
		UpdatedAt:          role.UpdatedAt,
	}
}

type workspaceRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// workspaceRoleMutationError maps shared workspace-role provisioning errors
// onto v2 error semantics. ServiceError values already carry the HTTP status.
func workspaceRoleMutationError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *services.ServiceError
	if errors.As(err, &serviceErr) {
		return newError(serviceErr.StatusCode, catalogErrorCode(serviceErr.StatusCode), serviceErr.Message)
	}
	return internalError(err)
}

func registerWorkspaceRoleRoutes(b *routeBuilder, deps Deps) {
	read := []string{"workspace-roles:read"}
	write := []string{"workspace-roles:write"}

	b.Read("/workspace-roles", AuthAuthenticated, read, func(r *http.Request) ([]workspaceRoleDTO, error) {
		roles, err := deps.WorkspaceRoles.List()
		if err != nil {
			return nil, internalError(err)
		}
		items := make([]workspaceRoleDTO, len(roles))
		for i := range roles {
			items[i] = workspaceRoleDTOFromModel(&roles[i])
		}
		if err := localizeCatalog(r, deps.ObjectTranslations, "workspace_role", &items); err != nil {
			return nil, err
		}
		return items, nil
	})

	b.Read("/workspace-roles/{workspace_role_id}", AuthAuthenticated, read, func(r *http.Request) (workspaceRoleDTO, error) {
		id, err := pathID(r, "workspace_role_id")
		if err != nil {
			return workspaceRoleDTO{}, err
		}
		role, err := deps.WorkspaceRoles.Get(id)
		if err != nil {
			return workspaceRoleDTO{}, workspaceRoleMutationError(err)
		}
		item := workspaceRoleDTOFromModel(role)
		if err := localizeCatalog(r, deps.ObjectTranslations, "workspace_role", &item); err != nil {
			return workspaceRoleDTO{}, err
		}
		return item, nil
	})

	b.JSON(http.MethodPost, "/workspace-roles", http.StatusCreated, false, AuthAuthenticated, write, func(r *http.Request, input workspaceRoleRequest) (workspaceRoleDTO, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return workspaceRoleDTO{}, err
		}
		result, err := deps.WorkspaceRoles.Create(auditActorFromRequest(r), input.Name, input.Description)
		if err != nil {
			return workspaceRoleDTO{}, workspaceRoleMutationError(err)
		}
		item := workspaceRoleDTOFromModel(result.Role)
		if err := localizeCatalog(r, deps.ObjectTranslations, "workspace_role", &item); err != nil {
			return workspaceRoleDTO{}, err
		}
		return item, nil
	})

	b.JSON(http.MethodPut, "/workspace-roles/{workspace_role_id}", http.StatusOK, false, AuthAuthenticated, write, func(r *http.Request, input workspaceRoleRequest) (workspaceRoleDTO, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return workspaceRoleDTO{}, err
		}
		id, err := pathID(r, "workspace_role_id")
		if err != nil {
			return workspaceRoleDTO{}, err
		}
		result, err := deps.WorkspaceRoles.Update(auditActorFromRequest(r), id, input.Name, input.Description)
		if err != nil {
			return workspaceRoleDTO{}, workspaceRoleMutationError(err)
		}
		item := workspaceRoleDTOFromModel(result.Role)
		if err := localizeCatalog(r, deps.ObjectTranslations, "workspace_role", &item); err != nil {
			return workspaceRoleDTO{}, err
		}
		return item, nil
	})

	b.Command(http.MethodDelete, "/workspace-roles/{workspace_role_id}", AuthAuthenticated, write, func(r *http.Request) error {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return err
		}
		id, err := pathID(r, "workspace_role_id")
		if err != nil {
			return err
		}
		return workspaceRoleMutationError(deps.WorkspaceRoles.Delete(auditActorFromRequest(r), id))
	})
}
