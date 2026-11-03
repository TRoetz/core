package handlers

import (
	"context"
	"net/http"

	"windshift/internal/database"
	"windshift/internal/models"
)

// smsUserHasGlobalPermission reports whether the user holds a global
// permission directly or via an active group, with the system.admin bypass.
// Mirrors services.PermissionService.HasGlobalPermissionContext so handlers
// can enforce field-level need-to-know (WI-3.2) without service access.
func smsUserHasGlobalPermission(ctx context.Context, db database.Database, userID int, permissionKey string) bool {
	var allowed bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM user_global_permissions ugp
			JOIN permissions p ON p.id = ugp.permission_id
			WHERE ugp.user_id = ? AND p.permission_key IN (?, 'system.admin')
			UNION
			SELECT 1 FROM group_members gm
			JOIN groups g ON g.id = gm.group_id AND g.is_active = true
			JOIN group_global_permissions ggp ON ggp.group_id = gm.group_id
			JOIN permissions p ON p.id = ggp.permission_id
			WHERE gm.user_id = ? AND p.permission_key IN (?, 'system.admin')
		)`, userID, permissionKey, userID, permissionKey).Scan(&allowed)
	return err == nil && allowed
}

// smsCanViewSensitive reports whether the user may see restricted-
// classification fields (notes, custom_fields, communication bodies) in the
// SMS module.
func (h *SMSHandler) smsCanViewSensitive(ctx context.Context, userID int) bool {
	return smsUserHasGlobalPermission(ctx, h.db, userID, models.PermissionSMSSensitiveView)
}

// smsMaskRestricted blanks the need-to-know fields on a restricted
// stakeholder record for users without sms.sensitive.view (WI-3.2). It
// reports whether masking was applied so callers can audit the attempt.
func smsMaskRestricted(s *models.SMSStakeholder) bool {
	if s.InfoClassification != models.SMSInfoRestricted {
		return false
	}
	s.Notes = ""
	s.CustomFields = "{}"
	return true
}

// smsAudit records a successful SMS mutation (or a restricted-field access
// attempt) in the audit trail (WI-3.1). resourceID may be nil for list-scope
// events.
func smsAudit(h *SMSHandler, r *http.Request, user *models.User, action, resourceType string, resourceID *int, resourceName string, details map[string]any) {
	if user == nil {
		return
	}
	logAuditWithDetails(h.db, r, user, action, resourceType, resourceID, resourceName, details)
}
