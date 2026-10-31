package routes

import (
	"net/http"

	"windshift/internal/models"
)

// RegisterP3MRoutes registers the Portfolio, Programme and Project Management
// module. Every endpoint requires authentication plus the global p3m.view
// permission, which is granted per group through role permissions.
func RegisterP3MRoutes(deps *Deps) {
	api := deps.API
	auth := deps.AuthMiddleware.RequireAuth
	p3mView := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionP3MView)

	// Aggregate views (the P3M page's primary data sources).
	api.HandleH("GET /p3m/hierarchy", auth(p3mView(http.HandlerFunc(deps.P3M.GetHierarchy))))
	api.HandleH("GET /p3m/executive-summary", auth(p3mView(http.HandlerFunc(deps.P3M.GetExecutiveSummary))))

	// Portfolios.
	api.HandleH("GET /p3m/portfolios", auth(p3mView(http.HandlerFunc(deps.P3M.ListPortfolios))))
	api.HandleH("POST /p3m/portfolios", auth(p3mView(http.HandlerFunc(deps.P3M.CreatePortfolio))))
	api.HandleH("GET /p3m/portfolios/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetPortfolio))))
	api.HandleH("PUT /p3m/portfolios/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdatePortfolio))))
	api.HandleH("DELETE /p3m/portfolios/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeletePortfolio))))

	// Programmes.
	api.HandleH("GET /p3m/programmes", auth(p3mView(http.HandlerFunc(deps.P3M.ListProgrammes))))
	api.HandleH("POST /p3m/programmes", auth(p3mView(http.HandlerFunc(deps.P3M.CreateProgramme))))
	api.HandleH("GET /p3m/programmes/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetProgramme))))
	api.HandleH("PUT /p3m/programmes/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateProgramme))))
	api.HandleH("DELETE /p3m/programmes/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteProgramme))))

	// Projects.
	api.HandleH("GET /p3m/projects", auth(p3mView(http.HandlerFunc(deps.P3M.ListProjects))))
	api.HandleH("POST /p3m/projects", auth(p3mView(http.HandlerFunc(deps.P3M.CreateProject))))
	api.HandleH("GET /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetProject))))
	api.HandleH("PUT /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateProject))))
	api.HandleH("DELETE /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteProject))))
	api.HandleH("GET /p3m/projects/{id}/workspaces", auth(p3mView(http.HandlerFunc(deps.P3M.ListProjectWorkspaces))))
	api.HandleH("PUT /p3m/projects/{id}/workspaces", auth(p3mView(http.HandlerFunc(deps.P3M.SetProjectWorkspaces))))
	api.HandleH("GET /p3m/projects/{id}/rollup", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectRollup))))

	// Governance: stage gates, artifacts, RACI, thresholds, budget approvals.
	api.HandleH("GET /p3m/stage-gates", auth(p3mView(http.HandlerFunc(deps.P3M.ListStageGates))))
	api.HandleH("POST /p3m/stage-gates", auth(p3mView(http.HandlerFunc(deps.P3M.CreateStageGate))))
	api.HandleH("POST /p3m/stage-gates/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideStageGate))))
	api.HandleH("PUT /p3m/gate-artifacts/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateGateArtifact))))

	api.HandleH("GET /p3m/raci", auth(p3mView(http.HandlerFunc(deps.P3M.GetRaciMatrix))))
	api.HandleH("PUT /p3m/raci", auth(p3mView(http.HandlerFunc(deps.P3M.UpsertRaciAssignment))))
	api.HandleH("DELETE /p3m/raci/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteRaciAssignment))))

	api.HandleH("GET /p3m/authorization-thresholds", auth(p3mView(http.HandlerFunc(deps.P3M.ListAuthorizationThresholds))))
	api.HandleH("PUT /p3m/authorization-thresholds", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateAuthorizationThreshold))))

	api.HandleH("GET /p3m/budget-approvals", auth(p3mView(http.HandlerFunc(deps.P3M.ListBudgetApprovals))))
	api.HandleH("POST /p3m/budget-approvals", auth(p3mView(http.HandlerFunc(deps.P3M.CreateBudgetApproval))))
	api.HandleH("POST /p3m/budget-approvals/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideBudgetApproval))))

	// Benefits and financials.
	api.HandleH("GET /p3m/benefits", auth(p3mView(http.HandlerFunc(deps.P3M.ListBenefits))))
	api.HandleH("POST /p3m/benefits", auth(p3mView(http.HandlerFunc(deps.P3M.CreateBenefit))))
	api.HandleH("GET /p3m/benefits/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetBenefit))))
	api.HandleH("PUT /p3m/benefits/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateBenefit))))
	api.HandleH("DELETE /p3m/benefits/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteBenefit))))

	api.HandleH("GET /p3m/benefits/{id}/realizations", auth(p3mView(http.HandlerFunc(deps.P3M.ListBenefitRealizations))))
	api.HandleH("POST /p3m/benefits/{id}/realizations", auth(p3mView(http.HandlerFunc(deps.P3M.CreateBenefitRealization))))
	api.HandleH("DELETE /p3m/benefits/{id}/realizations/{rid}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteBenefitRealization))))

	api.HandleH("GET /p3m/projects/{id}/baselines", auth(p3mView(http.HandlerFunc(deps.P3M.ListProjectBaselines))))
	api.HandleH("POST /p3m/projects/{id}/baselines", auth(p3mView(http.HandlerFunc(deps.P3M.CreateProjectBaseline))))

	api.HandleH("GET /p3m/tranches", auth(p3mView(http.HandlerFunc(deps.P3M.ListTranches))))
	api.HandleH("POST /p3m/tranches", auth(p3mView(http.HandlerFunc(deps.P3M.CreateTranche))))
	api.HandleH("PUT /p3m/tranches/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateTranche))))
	api.HandleH("DELETE /p3m/tranches/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteTranche))))

	api.HandleH("GET /p3m/forecast", auth(p3mView(http.HandlerFunc(deps.P3M.GetForecast))))

	// Risk, stakeholders and resources.
	api.HandleH("GET /p3m/raid-items", auth(p3mView(http.HandlerFunc(deps.P3M.ListRaidItems))))
	api.HandleH("POST /p3m/raid-items", auth(p3mView(http.HandlerFunc(deps.P3M.CreateRaidItem))))
	api.HandleH("PUT /p3m/raid-items/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateRaidItem))))
	api.HandleH("DELETE /p3m/raid-items/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteRaidItem))))
	api.HandleH("POST /p3m/raid-items/{id}/escalate", auth(p3mView(http.HandlerFunc(deps.P3M.EscalateRaidItem))))

	api.HandleH("GET /p3m/stakeholders", auth(p3mView(http.HandlerFunc(deps.P3M.ListStakeholders))))
	api.HandleH("POST /p3m/stakeholders", auth(p3mView(http.HandlerFunc(deps.P3M.CreateStakeholder))))
	api.HandleH("PUT /p3m/stakeholders/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateStakeholder))))
	api.HandleH("DELETE /p3m/stakeholders/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteStakeholder))))

	api.HandleH("GET /p3m/communications", auth(p3mView(http.HandlerFunc(deps.P3M.ListCommunications))))
	api.HandleH("POST /p3m/communications", auth(p3mView(http.HandlerFunc(deps.P3M.CreateCommunication))))
	api.HandleH("DELETE /p3m/communications/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteCommunication))))

	api.HandleH("GET /p3m/resources", auth(p3mView(http.HandlerFunc(deps.P3M.ListResources))))
	api.HandleH("POST /p3m/resources", auth(p3mView(http.HandlerFunc(deps.P3M.CreateResource))))
	api.HandleH("PUT /p3m/resources/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateResource))))
	api.HandleH("DELETE /p3m/resources/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteResource))))

	api.HandleH("GET /p3m/resource-allocations", auth(p3mView(http.HandlerFunc(deps.P3M.ListResourceAllocations))))
	api.HandleH("POST /p3m/resource-allocations", auth(p3mView(http.HandlerFunc(deps.P3M.CreateResourceAllocation))))
	api.HandleH("PUT /p3m/resource-allocations/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateResourceAllocation))))
	api.HandleH("DELETE /p3m/resource-allocations/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteResourceAllocation))))

	api.HandleH("GET /p3m/resource-capacity", auth(p3mView(http.HandlerFunc(deps.P3M.GetResourceCapacity))))
}
