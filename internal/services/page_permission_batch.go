package services

import (
	"database/sql"
	"fmt"
	"strings"

	"windshift/internal/models"
)

type pageVisibilityAccess struct {
	isSystemAdmin     bool
	hasWorkspaceAdmin bool
	hasPageAdmin      bool
	hasPageView       bool
}

func (a pageVisibilityAccess) hasLivePageAdmin() bool {
	return a.isSystemAdmin || a.hasWorkspaceAdmin || a.hasPageAdmin
}

func (s *PagePermissionService) loadPageVisibilityAccess(userID, workspaceID int) (pageVisibilityAccess, error) {
	var access pageVisibilityAccess
	var err error
	access.isSystemAdmin, err = s.perm.IsSystemAdmin(userID)
	if err != nil {
		return pageVisibilityAccess{}, err
	}
	access.hasWorkspaceAdmin, err = s.perm.HasWorkspacePermission(userID, workspaceID, models.PermissionWorkspaceAdmin)
	if err != nil {
		return pageVisibilityAccess{}, err
	}
	access.hasPageAdmin, err = s.perm.HasWorkspacePermission(userID, workspaceID, models.PermissionPageAdmin)
	if err != nil {
		return pageVisibilityAccess{}, err
	}
	access.hasPageView, err = s.perm.HasWorkspacePermission(userID, workspaceID, models.PermissionPageView)
	if err != nil {
		return pageVisibilityAccess{}, err
	}
	return access, nil
}

func filterLivePagesForACL(pages []models.Page, workspaceID int, access pageVisibilityAccess, visible map[int]bool) []models.Page {
	livePages := make([]models.Page, 0, len(pages))
	for _, page := range pages {
		if page.WorkspaceID != workspaceID {
			continue
		}
		if page.ArchivedAt != nil {
			visible[page.ID] = access.isSystemAdmin || access.hasWorkspaceAdmin
			continue
		}
		if access.hasLivePageAdmin() {
			visible[page.ID] = true
			continue
		}
		livePages = append(livePages, page)
	}
	return livePages
}

func pageVisibilityCandidateIDs(pages []models.Page) []int {
	candidates := make(map[int]struct{}, len(pages)*2)
	for _, page := range pages {
		candidates[page.ID] = struct{}{}
		if page.InheritPermissions {
			for _, ancestorID := range splitPathIDs(page.Path) {
				candidates[ancestorID] = struct{}{}
			}
		}
	}
	ids := make([]int, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	return ids
}

func markVisibleLivePages(
	visible map[int]bool,
	pages []models.Page,
	inheritFlags map[int]bool,
	permissionsByPage map[int][]models.PagePermission,
	userID int,
	groupIDs, roleIDs []int,
	hasPageView bool,
) {
	viewLevels := pagePermissionLevelSet(PageOpView)
	for _, page := range pages {
		acl := collectACLInMemory(page, inheritFlags, permissionsByPage)
		if len(acl) > 0 {
			visible[page.ID] = hasPageView && matchesACLInMemory(userID, groupIDs, roleIDs, acl, viewLevels)
			continue
		}
		if page.InheritPermissions {
			visible[page.ID] = hasPageView
		}
	}
}

// EffectiveLevels returns the caller's strongest effective level per live
// page: "admin", "edit", or "view". Archived and cross-workspace pages are
// skipped, and pages without access are omitted. It preserves Can's
// semantics: admins short-circuit, restricted pages need page.view plus a
// matching ACL row, open pages fall back to workspace page.* keys.
func (s *PagePermissionService) EffectiveLevels(userID, workspaceID int, pages []models.Page) (map[int]string, error) {
	out := make(map[int]string, len(pages))
	if userID == 0 || len(pages) == 0 {
		return out, nil
	}

	access, err := s.loadPageVisibilityAccess(userID, workspaceID)
	if err != nil {
		return nil, err
	}

	live := make([]models.Page, 0, len(pages))
	for _, page := range pages {
		if page.WorkspaceID != workspaceID || page.ArchivedAt != nil {
			continue
		}
		live = append(live, page)
		if access.hasLivePageAdmin() {
			out[page.ID] = PageOpAdmin
		}
	}
	if len(live) == 0 || access.hasLivePageAdmin() {
		return out, nil
	}

	hasPageEdit, err := s.perm.HasWorkspacePermission(userID, workspaceID, models.PermissionPageEdit)
	if err != nil {
		return nil, err
	}

	candidateIDs := pageVisibilityCandidateIDs(live)
	inheritFlags, err := s.loadAncestorInheritFlags(candidateIDs)
	if err != nil {
		return nil, err
	}
	aclsByPage, err := s.loadPagePermissionsByPage(candidateIDs)
	if err != nil {
		return nil, err
	}

	subjectID, err := s.delegatedPagePrincipalUserID(userID)
	if err != nil {
		return nil, err
	}
	groupIDs, err := s.userGroupIDs(subjectID)
	if err != nil {
		return nil, err
	}
	roleIDs, err := s.userWorkspaceRoleIDs(subjectID, workspaceID)
	if err != nil {
		return nil, err
	}

	adminLevels := pagePermissionLevelSet(PageOpAdmin)
	editLevels := pagePermissionLevelSet(PageOpEdit)
	viewLevels := pagePermissionLevelSet(PageOpView)
	for _, page := range live {
		acl := collectACLInMemory(page, inheritFlags, aclsByPage)
		switch {
		case len(acl) > 0:
			// Restricted pages additionally require the workspace page.view
			// membership floor, matching Can.
			if !access.hasPageView {
				continue
			}
			switch {
			case matchesACLInMemory(subjectID, groupIDs, roleIDs, acl, adminLevels):
				out[page.ID] = PageOpAdmin
			case matchesACLInMemory(subjectID, groupIDs, roleIDs, acl, editLevels):
				out[page.ID] = PageOpEdit
			case matchesACLInMemory(subjectID, groupIDs, roleIDs, acl, viewLevels):
				out[page.ID] = PageOpView
			}
		case !page.InheritPermissions:
			// A closed page without ACL rows is admin-only; admins were
			// short-circuited above.
		case hasPageEdit:
			out[page.ID] = PageOpEdit
		case access.hasPageView:
			out[page.ID] = PageOpView
		}
	}
	return out, nil
}

func pagePermissionLevelSet(op string) map[string]bool {
	levels := allowedLevelsForOp(op)
	set := make(map[string]bool, len(levels))
	for _, level := range levels {
		set[level] = true
	}
	return set
}

func pagePermissionQueryArgs(ids []int) (placeholders string, args []any) {
	args = make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", "), args
}

func scanPagePermissionRows(rows *sql.Rows) ([]models.PagePermission, error) {
	var permissions []models.PagePermission
	for rows.Next() {
		var permission models.PagePermission
		var grantedBy sql.NullInt64
		if err := rows.Scan(&permission.ID, &permission.PageID, &permission.PrincipalType, &permission.PrincipalID,
			&permission.PermissionLevel, &grantedBy, &permission.GrantedAt); err != nil {
			return nil, fmt.Errorf("scan ACL row: %w", err)
		}
		if grantedBy.Valid {
			id := int(grantedBy.Int64)
			permission.GrantedBy = &id
		}
		permissions = append(permissions, permission)
	}
	return permissions, rows.Err()
}

func (s *PagePermissionService) loadPagePermissions(ids []int, errorContext string) ([]models.PagePermission, error) {
	placeholders, args := pagePermissionQueryArgs(ids)
	rows, err := s.db.Query(`
		SELECT id, page_id, principal_type, principal_id, permission_level, granted_by, granted_at
		FROM page_permissions
		WHERE page_id IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errorContext, err)
	}
	defer func() { _ = rows.Close() }()
	return scanPagePermissionRows(rows)
}

func scanIntegerRows(rows *sql.Rows) ([]int, error) {
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
