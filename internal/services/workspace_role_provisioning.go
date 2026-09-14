package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"windshift/internal/database"
	"windshift/internal/logger"
	"windshift/internal/models"
	"windshift/internal/repository"
	"windshift/internal/sanitize"
)

// WorkspaceRoleProvisioningService owns the workspace-role definition flows
// shared by the admin browser surface and public API v2 (WI-1306): validation,
// sanitization, audit, and the permission-cache invalidation that keeps role
// changes visible immediately.
type WorkspaceRoleProvisioningService struct {
	db          database.Database
	repo        *repository.WorkspaceRoleRepository
	permissions *PermissionService
	approvals   *ApprovalService
}

func NewWorkspaceRoleProvisioningService(db database.Database, repo *repository.WorkspaceRoleRepository, permissions *PermissionService, approvals *ApprovalService) *WorkspaceRoleProvisioningService {
	return &WorkspaceRoleProvisioningService{db: db, repo: repo, permissions: permissions, approvals: approvals}
}

// WorkspaceRoleMutationResult carries the persisted role plus sanitize
// warnings.
type WorkspaceRoleMutationResult struct {
	Role     *models.WorkspaceRole
	Warnings []string
}

// List returns every workspace role definition.
func (s *WorkspaceRoleProvisioningService) List() ([]models.WorkspaceRole, error) {
	roles, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// Get returns one role with its permissions.
func (s *WorkspaceRoleProvisioningService) Get(id int) (*models.WorkspaceRole, error) {
	role, err := s.repo.GetByID(id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, NewServiceError(404, "Workspace role not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace role: %w", err)
	}
	permissions, err := s.repo.GetPermissions(id)
	if err != nil {
		return nil, fmt.Errorf("get workspace role: %w", err)
	}
	role.Permissions = permissions
	return role, nil
}

// Create adds a custom role with no permissions enabled.
func (s *WorkspaceRoleProvisioningService) Create(actor AuditActor, name, description string) (*WorkspaceRoleMutationResult, error) {
	// Role Name renders in every member list, role picker, and assignment
	// dialog — a short user-facing label. Description shows in the role
	// directory and is multi-line free-form text.
	warnings := sanitize.ApplyAllWithWarnings(
		sanitize.Pair{Target: &name, Policy: sanitize.PlainTextField, Label: "Name"},
		sanitize.Pair{Target: &description, Policy: sanitize.RichText, Label: "Description"},
	)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, NewServiceError(400, "name is required")
	}

	// Workspace_roles.name is UNIQUE — short-circuit with a friendly conflict
	// before letting the DB raise a generic constraint error.
	if nameTaken, err := s.repo.NameExists(name); err == nil && nameTaken {
		return nil, NewServiceError(409, fmt.Sprintf("A role named %q already exists", name))
	}

	now := time.Now()
	id, err := s.repo.CreateCustomRole(name, description, now)
	if err != nil {
		return nil, fmt.Errorf("create workspace role: %w", err)
	}

	emitServiceAudit(s.db, actor, logger.ActionWorkspaceRoleCreate, logger.ResourceRole, &id, name, nil)

	role := &models.WorkspaceRole{
		ID:                 id,
		Name:               name,
		Description:        description,
		IsSystem:           false,
		PermissionsEnabled: false,
		CreatedAt:          now,
		UpdatedAt:          now,
		Permissions:        []models.Permission{},
	}
	return &WorkspaceRoleMutationResult{Role: role, Warnings: warnings}, nil
}

// Update changes a workspace role's canonical fallback label without changing
// its identity, built-in key, permission behavior, or assignments.
func (s *WorkspaceRoleProvisioningService) Update(actor AuditActor, id int, name, description string) (*WorkspaceRoleMutationResult, error) {
	role, err := s.repo.GetByID(id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, NewServiceError(404, "Workspace role not found")
	}
	if err != nil {
		return nil, fmt.Errorf("update workspace role: %w", err)
	}

	warnings := sanitize.ApplyAllWithWarnings(
		sanitize.Pair{Target: &name, Policy: sanitize.PlainTextField, Label: "Name"},
		sanitize.Pair{Target: &description, Policy: sanitize.RichText, Label: "Description"},
	)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, NewServiceError(400, "name is required")
	}
	if nameTaken, err := s.repo.NameExistsExcept(name, id); err != nil {
		return nil, fmt.Errorf("update workspace role: %w", err)
	} else if nameTaken {
		return nil, NewServiceError(409, fmt.Sprintf("A role named %q already exists", name))
	}

	now := time.Now()
	if err := s.repo.UpdateMetadata(id, name, description, now); err != nil {
		return nil, fmt.Errorf("update workspace role: %w", err)
	}
	emitServiceAudit(s.db, actor, logger.ActionWorkspaceRoleUpdate, logger.ResourceRole, &id, name, nil)

	role.Name = name
	role.Description = description
	role.UpdatedAt = now
	role.Permissions, err = s.repo.GetPermissions(id)
	if err != nil {
		return nil, fmt.Errorf("update workspace role: %w", err)
	}
	return &WorkspaceRoleMutationResult{Role: role, Warnings: warnings}, nil
}

// Delete removes a custom workspace role. System roles cannot be deleted, and
// roles referenced by pending approvals or manual-action restrictions are
// refused with a conflict. The DELETE cascades to user_workspace_roles +
// group_workspace_roles + role_permissions via existing FKs; affected users'
// permission caches are invalidated so cached label-only assignments go away.
func (s *WorkspaceRoleProvisioningService) Delete(actor AuditActor, id int) error {
	role, err := s.repo.GetByID(id)
	if errors.Is(err, repository.ErrNotFound) {
		return NewServiceError(404, "Workspace role not found")
	}
	if err != nil {
		return fmt.Errorf("delete workspace role: %w", err)
	}
	if role.IsSystem {
		return NewServiceError(400, "System roles cannot be deleted")
	}

	// Refuse delete if the role is referenced by any pending approval — the
	// snapshot's source_role_id stays intact for audit, but we don't want to
	// orphan an in-flight pool. Cancel the approval first, then delete.
	if s.approvals != nil {
		if pendingCount, err := s.approvals.CountPendingApproversForRole(context.Background(), id); err == nil && pendingCount > 0 {
			return NewServiceError(409, fmt.Sprintf("Cannot delete: %d pending approval(s) still reference this role", pendingCount))
		}
	}
	if actionCount, err := s.repo.CountManualActionRestrictions(id); err != nil {
		return fmt.Errorf("delete workspace role: %w", err)
	} else if actionCount > 0 {
		return NewServiceError(409, fmt.Sprintf("Cannot delete: %d manual action(s) still restrict access to this role", actionCount))
	}

	// Snapshot affected users for cache invalidation before the DELETE cascades.
	affected := s.repo.AffectedUserIDs(id)

	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("delete workspace role: %w", err)
	}

	emitServiceAudit(s.db, actor, logger.ActionWorkspaceRoleDelete, logger.ResourceRole, &id, role.Name, nil)

	if s.permissions != nil && len(affected) > 0 {
		ids := make([]int, 0, len(affected))
		for uid := range affected {
			ids = append(ids, uid)
		}
		_ = s.permissions.InvalidateMultipleUserCaches(ids)
	}
	return nil
}
