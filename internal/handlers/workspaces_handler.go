package handlers

import (
	"log/slog"

	"windshift/internal/authz"
	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/repository"
	"windshift/internal/services"
)

type WorkspaceHandler struct {
	db                database.Database
	repo              *repository.WorkspaceRepository
	workspaceService  *services.WorkspaceService
	permissionService *services.PermissionService
	authz             *authz.Authz
	activityTracker   *services.ActivityTracker
	keyCache          *WorkspaceKeyCache
	cacheInvalidator  *services.AuthorizationCacheInvalidator
}

func NewWorkspaceHandler(db database.Database, permissionService *services.PermissionService, activityTracker *services.ActivityTracker, keyCache *WorkspaceKeyCache, invalidators ...*services.AuthorizationCacheInvalidator) *WorkspaceHandler {
	authzService := authz.New(db, permissionService)
	cacheInvalidator := services.NewAuthorizationCacheInvalidator(permissionService, keyCache)
	if len(invalidators) > 0 && invalidators[0] != nil {
		cacheInvalidator = invalidators[0]
	}
	return &WorkspaceHandler{
		db:                db,
		repo:              repository.NewWorkspaceRepository(db),
		workspaceService:  services.NewWorkspaceServiceWithAccess(db, authzService),
		permissionService: permissionService,
		authz:             authzService,
		activityTracker:   activityTracker,
		keyCache:          keyCache,
		cacheInvalidator:  cacheInvalidator,
	}
}

// loadWorkspaceForUser resolves a workspace and masks access denials as not found.
func (h *WorkspaceHandler) loadWorkspaceForUser(currentUser *models.User, workspaceID int) (*models.Workspace, error) {
	workspace, err := h.repo.FindByID(workspaceID)
	if err != nil {
		return nil, err
	}

	var canAccess bool
	if workspace.Active {
		canAccess, err = h.canViewWorkspace(currentUser.ID, workspace.ID)
	} else {
		canAccess, err = h.authz.HasWorkspacePermission(currentUser.ID, workspace.ID, models.PermissionWorkspaceAdmin)
	}
	if err != nil {
		return nil, err
	}
	if !canAccess {
		return nil, repository.ErrNotFound
	}

	timeProjectCategories, err := h.repo.GetTimeProjectCategories(workspace.ID)
	if err != nil {
		slog.Error("failed to load time project categories", "component", "workspaces", "workspace_id", workspace.ID, "error", err)
		workspace.TimeProjectCategories = []int{}
	} else {
		workspace.TimeProjectCategories = timeProjectCategories
	}
	return workspace, nil
}

func (h *WorkspaceHandler) trackWorkspaceVisit(userID, workspaceID int) {
	if h.activityTracker == nil {
		return
	}
	if err := h.activityTracker.TrackWorkspaceVisit(userID, workspaceID); err != nil {
		slog.Error("failed to track workspace visit", "component", "workspaces", "user_id", userID, "workspace_id", workspaceID, "error", err)
	}
}
