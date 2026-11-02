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

	// Plan work items (aggregated across the plan's sources) + summary.
	api.HandleH("GET /plans/{id}/items", auth(plansView(http.HandlerFunc(deps.Plans.ListPlanItems))))
	api.HandleH("GET /plans/{id}/summary", auth(plansView(http.HandlerFunc(deps.Plans.GetPlanSummary))))

	// Plan filters (custom CQL sources).
	api.HandleH("GET /plans/{id}/filters", auth(plansView(http.HandlerFunc(deps.Plans.ListPlanFilters))))
	api.HandleH("POST /plans/{id}/filters", auth(plansView(http.HandlerFunc(deps.Plans.AddPlanFilter))))
	api.HandleH("PUT /plans/{id}/filters/{filter_id}", auth(plansView(http.HandlerFunc(deps.Plans.UpdatePlanFilter))))
	api.HandleH("DELETE /plans/{id}/filters/{filter_id}", auth(plansView(http.HandlerFunc(deps.Plans.RemovePlanFilter))))

	// Item dependencies (blocks/related) inside the plan.
	api.HandleH("GET /plans/{id}/dependencies", auth(plansView(http.HandlerFunc(deps.Plans.ListPlanDependencies))))
	api.HandleH("POST /plans/{id}/items/{item_id}/dependencies", auth(plansView(http.HandlerFunc(deps.Plans.AddItemDependency))))
	api.HandleH("DELETE /plans/{id}/items/{item_id}/dependencies/{dep_id}", auth(plansView(http.HandlerFunc(deps.Plans.RemoveItemDependency))))

	// Teams: workload per team + capacity management.
	api.HandleH("GET /plans/{id}/teams", auth(plansView(http.HandlerFunc(deps.Plans.GetPlanTeams))))
	api.HandleH("PUT /plans/{id}/teams/{team_id}/capacity", auth(plansView(http.HandlerFunc(deps.Plans.SetTeamCapacity))))

	// Scenarios ("what if" planning with dependency ripple impact).
	api.HandleH("GET /plans/{id}/scenarios", auth(plansView(http.HandlerFunc(deps.Plans.ListPlanScenarios))))
	api.HandleH("POST /plans/{id}/scenarios", auth(plansView(http.HandlerFunc(deps.Plans.CreatePlanScenario))))
	api.HandleH("PUT /plans/{id}/scenarios/{sid}", auth(plansView(http.HandlerFunc(deps.Plans.UpdatePlanScenario))))
	api.HandleH("DELETE /plans/{id}/scenarios/{sid}", auth(plansView(http.HandlerFunc(deps.Plans.DeletePlanScenario))))
	api.HandleH("GET /plans/{id}/scenarios/{sid}/detail", auth(plansView(http.HandlerFunc(deps.Plans.GetPlanScenarioDetail))))
	api.HandleH("PUT /plans/{id}/scenarios/{sid}/items", auth(plansView(http.HandlerFunc(deps.Plans.UpsertScenarioItems))))

	// Flat plan/workspace pairs for Big Gantt plan grouping.
	api.HandleH("GET /plans/workspace-map", auth(plansView(http.HandlerFunc(deps.Plans.GetWorkspaceMap))))

	// Flat plan/P3M-entity pairs for the P3M views (linked plans per entity).
	api.HandleH("GET /plans/p3m-map", auth(plansView(http.HandlerFunc(deps.Plans.GetP3MMap))))
}
