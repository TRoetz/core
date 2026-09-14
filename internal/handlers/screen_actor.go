package handlers

import (
	"net/http"

	"windshift/internal/middleware"
	"windshift/internal/models"
	"windshift/internal/services"
)

// serviceActor builds the audit actor for screen provisioning mutations on
// the session-auth surface. The cookie /api surface carries no bearer token,
// so token metadata stays zero; the request's session user, IP, and user
// agent identify the actor.
func serviceActor(r *http.Request) services.AuditActor {
	user, _ := r.Context().Value(middleware.ContextKeyUser).(*models.User)
	token, _ := r.Context().Value(middleware.ContextKeyAPIToken).(*models.APIToken)
	authMethod, _ := r.Context().Value(middleware.ContextKeyAuthMethod).(string)
	return services.NewAuditActorFromRequest(r, user, token, authMethod)
}
