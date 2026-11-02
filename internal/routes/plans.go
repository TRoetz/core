package routes

import (
	"net/http"

	"windshift/internal/models"
)

// RegisterPlansRoutes registers the Plans module (cross-workspace plan
// grouping). Every endpoint requires authentication plus the global
// plans.view permission, which is granted per group through role
// permissions; system admins always have it.
func RegisterPlansRoutes(deps *Deps) {
	api := deps.API
	auth := deps.AuthMiddleware.RequireAuth
	plansView := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionPlansView)

	// Plans.
	api.HandleH("GET /plans", auth(plansView(http.HandlerFunc(deps.Plans.ListPlans))))
	api.HandleH("POST /plans", auth(plansView(http.HandlerFunc(deps.Plans.CreatePlan))))
	api.HandleH("GET /plans/{id}", auth(plansView(http.HandlerFunc(deps.Plans.GetPlan))))
	api.HandleH("PUT /plans/{id}", auth(plansView(http.HandlerFunc(deps.Plans.UpdatePlan))))
	api.HandleH("DELETE /plans/{id}", auth(plansView(http.HandlerFunc(deps.Plans.DeletePlan))))
	api.HandleH("GET /plans/{id}/detail", auth(plansView(http.HandlerFunc(deps.Plans.GetPlanDetail))))

	// Plan <-> workspaces (the core grouping).
	api.HandleH("PUT /plans/{id}/workspaces", auth(plansView(http.HandlerFunc(deps.Plans.SetPlanWorkspaces))))
	api.HandleH("POST /plans/{id}/workspaces/{workspace_id}", auth(plansView(http.HandlerFunc(deps.Plans.AddPlanWorkspace))))
	api.HandleH("DELETE /plans/{id}/workspaces/{workspace_id}", auth(plansView(http.HandlerFunc(deps.Plans.RemovePlanWorkspace))))

	// Plan <-> P3M entities (portfolio/programme/project).
	api.HandleH("POST /plans/{id}/p3m-entities", auth(plansView(http.HandlerFunc(deps.Plans.AddPlanP3MEntity))))
	api.HandleH("DELETE /plans/{id}/p3m-entities/{entity_type}/{entity_id}", auth(plansView(http.HandlerFunc(deps.Plans.RemovePlanP3MEntity))))

	// Plan <-> SMS stakeholders.
	api.HandleH("POST /plans/{id}/stakeholders", auth(plansView(http.HandlerFunc(deps.Plans.AddPlanStakeholder))))
	api.HandleH("DELETE /plans/{id}/stakeholders/{stakeholder_id}", auth(plansView(http.HandlerFunc(deps.Plans.RemovePlanStakeholder))))

	// Flat plan/workspace pairs for Big Gantt plan grouping.
	api.HandleH("GET /plans/workspace-map", auth(plansView(http.HandlerFunc(deps.Plans.GetWorkspaceMap))))

	// Flat plan/P3M-entity pairs for the P3M views (linked plans per entity).
	api.HandleH("GET /plans/p3m-map", auth(plansView(http.HandlerFunc(deps.Plans.GetP3MMap))))
}
