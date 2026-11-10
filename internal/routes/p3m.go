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
	admin := deps.PermissionMiddleware.RequireSystemAdmin()

	// Aggregate views (the P3M page's primary data sources).
	api.HandleH("GET /p3m/hierarchy", auth(p3mView(http.HandlerFunc(deps.P3M.GetHierarchy))))
	api.HandleH("GET /p3m/executive-summary", auth(p3mView(http.HandlerFunc(deps.P3M.GetExecutiveSummary))))

	// Register exports (CSV).
	api.HandleH("GET /p3m/export", auth(p3mView(http.HandlerFunc(deps.P3M.ExportCSV))))

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
	api.HandleH("GET /p3m/programmes/{id}/critical-path", auth(p3mView(http.HandlerFunc(deps.P3M.GetProgrammeCriticalPath))))
	api.HandleH("GET /p3m/programmes/{id}/leave-impact", auth(p3mView(http.HandlerFunc(deps.P3M.GetProgrammeLeaveImpact))))

	// Projects.
	api.HandleH("GET /p3m/projects", auth(p3mView(http.HandlerFunc(deps.P3M.ListProjects))))
	api.HandleH("POST /p3m/projects", auth(p3mView(http.HandlerFunc(deps.P3M.CreateProject))))
	api.HandleH("GET /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetProject))))
	api.HandleH("PUT /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateProject))))
	api.HandleH("DELETE /p3m/projects/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteProject))))
	api.HandleH("GET /p3m/projects/{id}/workspaces", auth(p3mView(http.HandlerFunc(deps.P3M.ListProjectWorkspaces))))
	api.HandleH("PUT /p3m/projects/{id}/workspaces", auth(p3mView(http.HandlerFunc(deps.P3M.SetProjectWorkspaces))))
	api.HandleH("GET /p3m/projects/{id}/rollup", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectRollup))))
	// Financial close-out (FIN-010): one record per project, validated by the
	// finance reviewer at the close stage.
	api.HandleH("GET /p3m/projects/{id}/close-out", auth(p3mView(http.HandlerFunc(deps.P3M.GetCloseOut))))
	api.HandleH("POST /p3m/projects/{id}/close-out", auth(p3mView(http.HandlerFunc(deps.P3M.CreateCloseOut))))
	api.HandleH("PUT /p3m/projects/{id}/close-out", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateCloseOut))))
	api.HandleH("GET /p3m/projects/{id}/activity", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectActivity))))
	api.HandleH("GET /p3m/projects/{id}/critical-path", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectCriticalPath))))
	api.HandleH("GET /p3m/projects/{id}/leave-impact", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectLeaveImpact))))

	// Front Door intake (FD-001..005, FD-008/010/011): single entry point for
	// new work; screen configuration is admin-only like lifecycle config.
	api.HandleH("GET /p3m/front-door", auth(p3mView(http.HandlerFunc(deps.P3M.ListIntakes))))
	api.HandleH("POST /p3m/front-door", auth(p3mView(http.HandlerFunc(deps.P3M.CreateIntake))))
	api.HandleH("GET /p3m/front-door/screens", auth(p3mView(http.HandlerFunc(deps.P3M.GetFrontDoorScreens))))
	api.HandleH("PUT /p3m/front-door/screens", admin(http.HandlerFunc(deps.P3M.UpdateFrontDoorScreens)))
	api.HandleH("GET /p3m/front-door/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetIntake))))
	api.HandleH("PUT /p3m/front-door/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateIntake))))
	api.HandleH("POST /p3m/front-door/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideIntake))))

	// Governance: stage gates, artifacts, RACI, thresholds, budget approvals.
	api.HandleH("GET /p3m/stage-gates", auth(p3mView(http.HandlerFunc(deps.P3M.ListStageGates))))
	api.HandleH("POST /p3m/stage-gates", auth(p3mView(http.HandlerFunc(deps.P3M.CreateStageGate))))
	api.HandleH("POST /p3m/stage-gates/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideStageGate))))
	api.HandleH("PUT /p3m/gate-artifacts/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateGateArtifact))))

	// Configurable lifecycle (BRD §5.2/§5.5): global stage and gate-template
	// configuration is admin-only; project-level gate status and skips follow
	// the module permission like every other P3M route.
	api.HandleH("GET /p3m/config/lifecycle", auth(p3mView(http.HandlerFunc(deps.P3M.ListLifecycleConfig))))
	api.HandleH("POST /p3m/config/lifecycle/stages", admin(http.HandlerFunc(deps.P3M.CreateLifecycleStage)))
	api.HandleH("PUT /p3m/config/lifecycle/stages/{key}", admin(http.HandlerFunc(deps.P3M.UpdateLifecycleStage)))
	api.HandleH("DELETE /p3m/config/lifecycle/stages/{key}", admin(http.HandlerFunc(deps.P3M.DeleteLifecycleStage)))
	api.HandleH("POST /p3m/config/lifecycle/gate-templates", admin(http.HandlerFunc(deps.P3M.CreateGateTemplate)))
	api.HandleH("PUT /p3m/config/lifecycle/gate-templates/{id}", admin(http.HandlerFunc(deps.P3M.UpdateGateTemplate)))
	api.HandleH("DELETE /p3m/config/lifecycle/gate-templates/{id}", admin(http.HandlerFunc(deps.P3M.DeleteGateTemplate)))
	api.HandleH("GET /p3m/projects/{id}/gates", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectGates))))
	api.HandleH("POST /p3m/projects/{id}/stage-skips", auth(p3mView(http.HandlerFunc(deps.P3M.RecordStageSkip))))
	api.HandleH("GET /p3m/projects/{id}/stage-skips", auth(p3mView(http.HandlerFunc(deps.P3M.ListStageSkips))))

	// Architecture review board.
	api.HandleH("GET /p3m/architecture-reviews", auth(p3mView(http.HandlerFunc(deps.P3M.ListArchitectureReviews))))
	api.HandleH("POST /p3m/architecture-reviews", auth(p3mView(http.HandlerFunc(deps.P3M.CreateArchitectureReview))))
	api.HandleH("PUT /p3m/architecture-reviews/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateArchitectureReview))))
	api.HandleH("DELETE /p3m/architecture-reviews/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteArchitectureReview))))
	api.HandleH("POST /p3m/architecture-reviews/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideArchitectureReview))))

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

	// Financial system sync (FIN-007): scheduled CSV import from the external
	// finance system. Config mutation and review-queue actions are admin-only;
	// reads follow the module permission.
	api.HandleH("GET /p3m/finance-sync/configs", auth(p3mView(http.HandlerFunc(deps.P3M.ListFinanceSyncConfigs))))
	api.HandleH("POST /p3m/finance-sync/configs", admin(http.HandlerFunc(deps.P3M.CreateFinanceSyncConfig)))
	api.HandleH("PUT /p3m/finance-sync/configs/{id}", admin(http.HandlerFunc(deps.P3M.UpdateFinanceSyncConfig)))
	api.HandleH("DELETE /p3m/finance-sync/configs/{id}", admin(http.HandlerFunc(deps.P3M.DeleteFinanceSyncConfig)))
	api.HandleH("POST /p3m/finance-sync/configs/{id}/run", auth(p3mView(http.HandlerFunc(deps.P3M.RunFinanceSyncNow))))
	api.HandleH("POST /p3m/finance-sync/configs/{id}/upload", auth(p3mView(http.HandlerFunc(deps.P3M.UploadFinanceSyncCSV))))
	api.HandleH("GET /p3m/finance-sync/runs", auth(p3mView(http.HandlerFunc(deps.P3M.ListFinanceSyncRuns))))
	api.HandleH("GET /p3m/finance-sync/runs/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetFinanceSyncRun))))
	api.HandleH("GET /p3m/finance-sync/unmatched", auth(p3mView(http.HandlerFunc(deps.P3M.ListUnmatchedFinanceRows))))
	api.HandleH("POST /p3m/finance-sync/unmatched/{fact_id}/assign", admin(http.HandlerFunc(deps.P3M.AssignUnmatchedFinanceRow)))
	api.HandleH("POST /p3m/finance-sync/unmatched/{fact_id}/reject", admin(http.HandlerFunc(deps.P3M.RejectUnmatchedFinanceRow)))
	api.HandleH("GET /p3m/finance-sync/actuals", auth(p3mView(http.HandlerFunc(deps.P3M.ListEffectiveFinanceActuals))))
	api.HandleH("GET /p3m/projects/{id}/finance-reconciliation", auth(p3mView(http.HandlerFunc(deps.P3M.GetProjectFinanceReconciliation))))
	api.HandleH("GET /p3m/finance-sync/summary", auth(p3mView(http.HandlerFunc(deps.P3M.GetFinanceSyncSummary))))

	// Investment cases: per-project prioritisation pipeline and scores.
	api.HandleH("GET /p3m/investment-cases", auth(p3mView(http.HandlerFunc(deps.P3M.ListInvestmentCases))))
	api.HandleH("POST /p3m/investment-cases", auth(p3mView(http.HandlerFunc(deps.P3M.CreateInvestmentCase))))
	api.HandleH("PUT /p3m/investment-cases/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateInvestmentCase))))
	api.HandleH("DELETE /p3m/investment-cases/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteInvestmentCase))))
	api.HandleH("POST /p3m/investment-cases/{id}/decision", auth(p3mView(http.HandlerFunc(deps.P3M.DecideInvestmentCase))))

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

	// Phase-2 gap closure: change calendar (CHG-008/009).
	api.HandleH("GET /p3m/change-calendar", auth(p3mView(http.HandlerFunc(deps.P3M.ListChangeCalendarEntries))))
	api.HandleH("POST /p3m/change-calendar", auth(p3mView(http.HandlerFunc(deps.P3M.CreateChangeCalendarEntry))))
	api.HandleH("GET /p3m/change-calendar/week", auth(p3mView(http.HandlerFunc(deps.P3M.GetChangeCalendarWeek))))
	api.HandleH("GET /p3m/change-calendar/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetChangeCalendarEntry))))
	api.HandleH("PUT /p3m/change-calendar/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateChangeCalendarEntry))))
	api.HandleH("DELETE /p3m/change-calendar/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteChangeCalendarEntry))))

	// Phase-2 gap closure: change impact + readiness assessments (CHG-002/006).
	api.HandleH("GET /p3m/projects/{id}/change-assessments", auth(p3mView(http.HandlerFunc(deps.P3M.ListChangeAssessments))))
	api.HandleH("POST /p3m/projects/{id}/change-assessments", auth(p3mView(http.HandlerFunc(deps.P3M.CreateChangeAssessment))))
	api.HandleH("PUT /p3m/change-assessments/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateChangeAssessment))))
	api.HandleH("GET /p3m/projects/{id}/readiness-assessments", auth(p3mView(http.HandlerFunc(deps.P3M.ListReadinessAssessments))))
	api.HandleH("POST /p3m/projects/{id}/readiness-assessments", auth(p3mView(http.HandlerFunc(deps.P3M.CreateReadinessAssessment))))
	api.HandleH("PUT /p3m/readiness-assessments/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateReadinessAssessment))))

	// Phase-2 gap closure: benefit measurements + portfolio rollup (BEN-007/009).
	api.HandleH("GET /p3m/benefits/{id}/measurements", auth(p3mView(http.HandlerFunc(deps.P3M.ListBenefitMeasurements))))
	api.HandleH("POST /p3m/benefits/{id}/measurements", auth(p3mView(http.HandlerFunc(deps.P3M.CreateBenefitMeasurement))))
	api.HandleH("PUT /p3m/benefit-measurements/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateBenefitMeasurement))))
	api.HandleH("GET /p3m/benefits/rollup", auth(p3mView(http.HandlerFunc(deps.P3M.GetBenefitsRollup))))

	// Phase-2 gap closure: report snapshots + tolerance profiles (RPT-006/007).
	api.HandleH("POST /p3m/report-snapshots", auth(p3mView(http.HandlerFunc(deps.P3M.CreateReportSnapshot))))
	api.HandleH("GET /p3m/report-snapshots", auth(p3mView(http.HandlerFunc(deps.P3M.ListReportSnapshots))))
	api.HandleH("GET /p3m/report-snapshots/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.GetReportSnapshot))))
	api.HandleH("GET /p3m/report-snapshots/suggest", auth(p3mView(http.HandlerFunc(deps.P3M.SuggestCategoryRags))))
	api.HandleH("GET /p3m/tolerance-profiles", auth(p3mView(http.HandlerFunc(deps.P3M.ListToleranceProfiles))))
	api.HandleH("POST /p3m/tolerance-profiles", auth(p3mView(http.HandlerFunc(deps.P3M.CreateToleranceProfile))))
	api.HandleH("PUT /p3m/tolerance-profiles/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateToleranceProfile))))
	api.HandleH("DELETE /p3m/tolerance-profiles/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteToleranceProfile))))

	// Phase-2 gap closure: RAID roll-ups + CPM what-if preview (RAID-004/006).
	api.HandleH("GET /p3m/programmes/{id}/raid-rollup", auth(p3mView(http.HandlerFunc(deps.P3M.GetProgrammeRaidRollup))))
	api.HandleH("GET /p3m/portfolios/{id}/raid-rollup", auth(p3mView(http.HandlerFunc(deps.P3M.GetPortfolioRaidRollup))))
	api.HandleH("POST /p3m/projects/{id}/cpm-preview", auth(p3mView(http.HandlerFunc(deps.P3M.GetCpmPreview))))

	// Phase-2 gap closure: resource scenario modelling (RES-004/006).
	api.HandleH("POST /p3m/resource-scenarios", auth(p3mView(http.HandlerFunc(deps.P3M.RunResourceScenario))))

	// Phase-2 gap closure: prioritisation scoring + roadmap (PO-003..005, 008).
	api.HandleH("GET /p3m/scoring-criteria", auth(p3mView(http.HandlerFunc(deps.P3M.ListScoringCriteria))))
	api.HandleH("POST /p3m/scoring-criteria", auth(p3mView(http.HandlerFunc(deps.P3M.CreateScoringCriterion))))
	api.HandleH("PUT /p3m/scoring-criteria/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateScoringCriterion))))
	api.HandleH("DELETE /p3m/scoring-criteria/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteScoringCriterion))))
	api.HandleH("PUT /p3m/investment-cases/{id}/scores", auth(p3mView(http.HandlerFunc(deps.P3M.UpsertInvestmentCaseScore))))
	api.HandleH("GET /p3m/investment-cases/{id}/scores", auth(p3mView(http.HandlerFunc(deps.P3M.ListInvestmentCaseScores))))
	api.HandleH("GET /p3m/investment-cases/ranked", auth(p3mView(http.HandlerFunc(deps.P3M.GetRankedInvestmentCases))))
	api.HandleH("GET /p3m/portfolios/{id}/roadmap", auth(p3mView(http.HandlerFunc(deps.P3M.GetPortfolioRoadmap))))

	// Phase-2 gap closure: financial exception alert rules (FIN-009).
	api.HandleH("GET /p3m/finance-alert-rules", auth(p3mView(http.HandlerFunc(deps.P3M.ListFinanceAlertRules))))
	api.HandleH("POST /p3m/finance-alert-rules", auth(p3mView(http.HandlerFunc(deps.P3M.CreateFinanceAlertRule))))
	api.HandleH("PUT /p3m/finance-alert-rules/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateFinanceAlertRule))))
	api.HandleH("DELETE /p3m/finance-alert-rules/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.DeleteFinanceAlertRule))))
	api.HandleH("POST /p3m/evaluate-finance-alerts", auth(p3mView(http.HandlerFunc(deps.P3M.EvaluateFinanceAlerts))))
	api.HandleH("POST /p3m/projects/{id}/evaluate-finance-alerts", auth(p3mView(http.HandlerFunc(deps.P3M.EvaluateProjectFinanceAlerts))))

	// Phase-2 gap closure: closure & handover records (CLS-001..006).
	api.HandleH("GET /p3m/projects/{id}/closure-record", auth(p3mView(http.HandlerFunc(deps.P3M.GetClosureRecord))))
	api.HandleH("POST /p3m/projects/{id}/closure-record", auth(p3mView(http.HandlerFunc(deps.P3M.CreateClosureRecord))))
	api.HandleH("PUT /p3m/closure-records/{id}", auth(p3mView(http.HandlerFunc(deps.P3M.UpdateClosureRecord))))
}
