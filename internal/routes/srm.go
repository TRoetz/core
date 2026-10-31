package routes

import (
	"net/http"

	"windshift/internal/models"
)

// RegisterSRMRoutes registers the Supplier Relationship Management module.
// Every endpoint requires authentication plus the global srm.view permission,
// which is granted per group through role permissions. The route table below
// is the contract for the handler methods implemented across
// handlers/srm.go (core: suppliers, contracts, gates),
// handlers/srm_segmentation.go (categories, segmentation runs, validation),
// handlers/srm_planning.go (value props, stakeholders, comms, RACI, meetings,
// JBP) and handlers/srm_monitoring.go (metrics, scorecards, surveys,
// innovation register, SIPs, disputes).
func RegisterSRMRoutes(deps *Deps) {
	api := deps.API
	auth := deps.AuthMiddleware.RequireAuth
	srmView := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionSRMView)

	// Module dashboard.
	api.HandleH("GET /srm/summary", auth(srmView(http.HandlerFunc(deps.SRM.GetSummary))))

	// Supplier directory + phase-gate state machine (§3.1).
	api.HandleH("GET /srm/suppliers", auth(srmView(http.HandlerFunc(deps.SRM.ListSuppliers))))
	api.HandleH("POST /srm/suppliers", auth(srmView(http.HandlerFunc(deps.SRM.CreateSupplier))))
	api.HandleH("GET /srm/suppliers/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetSupplier))))
	api.HandleH("PUT /srm/suppliers/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateSupplier))))
	api.HandleH("DELETE /srm/suppliers/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteSupplier))))

	// Supplier contacts.
	api.HandleH("POST /srm/suppliers/{id}/contacts", auth(srmView(http.HandlerFunc(deps.SRM.AddSupplierContact))))
	api.HandleH("PUT /srm/contacts/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateSupplierContact))))
	api.HandleH("DELETE /srm/contacts/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteSupplierContact))))

	// Phase gates (FR-GATE-1/2/3).
	api.HandleH("GET /srm/suppliers/{id}/gates", auth(srmView(http.HandlerFunc(deps.SRM.GetSupplierGates))))
	api.HandleH("POST /srm/suppliers/{id}/gate-advance", auth(srmView(http.HandlerFunc(deps.SRM.AdvanceGate))))
	api.HandleH("POST /srm/suppliers/{id}/gate-downgrade", auth(srmView(http.HandlerFunc(deps.SRM.DowngradeGate))))
	api.HandleH("POST /srm/suppliers/{id}/close", auth(srmView(http.HandlerFunc(deps.SRM.CloseSupplier))))

	// Contract register (Rule 34).
	api.HandleH("GET /srm/contracts", auth(srmView(http.HandlerFunc(deps.SRM.ListContracts))))
	api.HandleH("POST /srm/contracts", auth(srmView(http.HandlerFunc(deps.SRM.CreateContract))))
	api.HandleH("GET /srm/projects", auth(srmView(http.HandlerFunc(deps.SRM.ListProjectsForSRM))))
	api.HandleH("GET /srm/p3m-options", auth(srmView(http.HandlerFunc(deps.SRM.ListP3MOptions))))
	api.HandleH("GET /srm/contracts/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetContract))))
	api.HandleH("PUT /srm/contracts/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateContract))))
	api.HandleH("DELETE /srm/contracts/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteContract))))

	// Contract role assignments.
	api.HandleH("POST /srm/contracts/{id}/assignments", auth(srmView(http.HandlerFunc(deps.SRM.AssignContractRole))))
	api.HandleH("DELETE /srm/contracts/{id}/assignments/{assignmentId}", auth(srmView(http.HandlerFunc(deps.SRM.RemoveContractAssignment))))

	// Contract documents & clause extraction review.
	api.HandleH("GET /srm/contracts/{id}/documents", auth(srmView(http.HandlerFunc(deps.SRM.ListContractDocuments))))
	api.HandleH("POST /srm/contracts/{id}/documents", auth(srmView(http.HandlerFunc(deps.SRM.AddContractDocument))))
	api.HandleH("DELETE /srm/documents/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteContractDocument))))
	api.HandleH("GET /srm/documents/{id}/extractions", auth(srmView(http.HandlerFunc(deps.SRM.ListExtractions))))
	api.HandleH("PUT /srm/extractions/{id}", auth(srmView(http.HandlerFunc(deps.SRM.ReviewClauseExtraction))))

	// Right-sized management plans (versioned).
	api.HandleH("POST /srm/contracts/{id}/management-plans", auth(srmView(http.HandlerFunc(deps.SRM.CreateManagementPlan))))

	// Phase 1: categories, segmentation runs and validation.
	api.HandleH("GET /srm/categories", auth(srmView(http.HandlerFunc(deps.SRM.ListCategories))))
	api.HandleH("POST /srm/categories", auth(srmView(http.HandlerFunc(deps.SRM.CreateCategory))))
	api.HandleH("PUT /srm/categories/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateCategory))))
	api.HandleH("DELETE /srm/categories/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteCategory))))

	api.HandleH("GET /srm/segmentation/runs", auth(srmView(http.HandlerFunc(deps.SRM.ListSegmentationRuns))))
	api.HandleH("POST /srm/segmentation/runs", auth(srmView(http.HandlerFunc(deps.SRM.CreateSegmentationRun))))
	api.HandleH("GET /srm/segmentation/runs/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetSegmentationRun))))
	api.HandleH("PUT /srm/segmentation/runs/{id}/answers", auth(srmView(http.HandlerFunc(deps.SRM.UpsertSegmentationAnswer))))
	api.HandleH("POST /srm/segmentation/runs/{id}/complete", auth(srmView(http.HandlerFunc(deps.SRM.CompleteSegmentationRun))))

	api.HandleH("GET /srm/validation", auth(srmView(http.HandlerFunc(deps.SRM.ListValidationRequests))))
	api.HandleH("POST /srm/validation", auth(srmView(http.HandlerFunc(deps.SRM.CreateValidationRequest))))
	api.HandleH("GET /srm/validation/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetValidationRequest))))
	api.HandleH("PUT /srm/validation/{id}/decision", auth(srmView(http.HandlerFunc(deps.SRM.DecideValidationRequest))))

	// Phase 2/3: value propositions, stakeholders, communications, RACI.
	api.HandleH("GET /srm/value-propositions", auth(srmView(http.HandlerFunc(deps.SRM.ListValuePropositions))))
	api.HandleH("POST /srm/value-propositions", auth(srmView(http.HandlerFunc(deps.SRM.CreateValueProposition))))
	api.HandleH("PUT /srm/value-propositions/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateValueProposition))))
	api.HandleH("DELETE /srm/value-propositions/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteValueProposition))))

	api.HandleH("GET /srm/stakeholders", auth(srmView(http.HandlerFunc(deps.SRM.ListStakeholders))))
	api.HandleH("POST /srm/stakeholders", auth(srmView(http.HandlerFunc(deps.SRM.CreateStakeholder))))
	api.HandleH("PUT /srm/stakeholders/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateStakeholder))))
	api.HandleH("DELETE /srm/stakeholders/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteStakeholder))))

	api.HandleH("GET /srm/entity-stakeholders", auth(srmView(http.HandlerFunc(deps.SRM.ListEntityStakeholders))))
	api.HandleH("POST /srm/entity-stakeholders", auth(srmView(http.HandlerFunc(deps.SRM.CreateEntityStakeholder))))
	api.HandleH("PUT /srm/entity-stakeholders/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateEntityStakeholder))))
	api.HandleH("DELETE /srm/entity-stakeholders/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteEntityStakeholder))))

	api.HandleH("GET /srm/suppliers/{id}/comms-plan", auth(srmView(http.HandlerFunc(deps.SRM.GetCommsPlan))))
	api.HandleH("PUT /srm/suppliers/{id}/comms-plan", auth(srmView(http.HandlerFunc(deps.SRM.UpsertCommsPlan))))
	api.HandleH("GET /srm/comms-plans/{id}/messages", auth(srmView(http.HandlerFunc(deps.SRM.ListCommsMessages))))
	api.HandleH("POST /srm/comms-plans/{id}/messages", auth(srmView(http.HandlerFunc(deps.SRM.AddCommsMessage))))
	api.HandleH("PUT /srm/comms-messages/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateCommsMessage))))
	api.HandleH("DELETE /srm/comms-messages/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteCommsMessage))))

	api.HandleH("GET /srm/suppliers/{id}/raci", auth(srmView(http.HandlerFunc(deps.SRM.GetRaciMatrix))))
	api.HandleH("POST /srm/suppliers/{id}/raci", auth(srmView(http.HandlerFunc(deps.SRM.CreateRaciVersion))))
	api.HandleH("PUT /srm/racis/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateRaciStatus))))
	api.HandleH("PUT /srm/racis/{id}/assignments", auth(srmView(http.HandlerFunc(deps.SRM.UpsertRaciAssignment))))
	api.HandleH("DELETE /srm/raci-assignments/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteRaciAssignment))))

	// Meetings (kick-offs, QBRs, relationship reviews).
	api.HandleH("GET /srm/meetings", auth(srmView(http.HandlerFunc(deps.SRM.ListMeetings))))
	api.HandleH("POST /srm/meetings", auth(srmView(http.HandlerFunc(deps.SRM.CreateMeeting))))
	api.HandleH("GET /srm/meetings/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetMeeting))))
	api.HandleH("PUT /srm/meetings/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateMeeting))))
	api.HandleH("DELETE /srm/meetings/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteMeeting))))
	api.HandleH("POST /srm/meetings/{id}/attendees", auth(srmView(http.HandlerFunc(deps.SRM.AddMeetingAttendee))))
	api.HandleH("DELETE /srm/meeting-attendees/{id}", auth(srmView(http.HandlerFunc(deps.SRM.RemoveMeetingAttendee))))
	api.HandleH("POST /srm/meetings/{id}/actions", auth(srmView(http.HandlerFunc(deps.SRM.AddMeetingAction))))
	api.HandleH("PUT /srm/meeting-actions/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateMeetingAction))))

	// Joint business plans (versioned per supplier).
	api.HandleH("GET /srm/jbps", auth(srmView(http.HandlerFunc(deps.SRM.ListJbps))))
	api.HandleH("POST /srm/suppliers/{id}/jbps", auth(srmView(http.HandlerFunc(deps.SRM.CreateJbp))))
	api.HandleH("GET /srm/jbps/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetJbp))))
	api.HandleH("PUT /srm/jbps/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateJbp))))
	api.HandleH("DELETE /srm/jbps/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteJbp))))

	// Phase 4/5: performance metrics, scorecards, pulse surveys.
	api.HandleH("GET /srm/metrics", auth(srmView(http.HandlerFunc(deps.SRM.ListMetrics))))
	api.HandleH("POST /srm/metrics", auth(srmView(http.HandlerFunc(deps.SRM.CreateMetric))))
	api.HandleH("PUT /srm/metrics/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateMetric))))
	api.HandleH("DELETE /srm/metrics/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteMetric))))
	api.HandleH("GET /srm/metrics/{id}/results", auth(srmView(http.HandlerFunc(deps.SRM.ListMetricResults))))
	api.HandleH("POST /srm/metrics/{id}/results", auth(srmView(http.HandlerFunc(deps.SRM.RecordMetricResult))))

	api.HandleH("GET /srm/scorecards", auth(srmView(http.HandlerFunc(deps.SRM.ListScorecards))))
	api.HandleH("POST /srm/scorecards", auth(srmView(http.HandlerFunc(deps.SRM.CreateScorecard))))
	api.HandleH("GET /srm/scorecards/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetScorecard))))
	api.HandleH("PUT /srm/scorecards/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateScorecard))))
	api.HandleH("PUT /srm/scorecards/{id}/dimensions", auth(srmView(http.HandlerFunc(deps.SRM.UpsertScorecardDimension))))

	api.HandleH("GET /srm/pulse-surveys", auth(srmView(http.HandlerFunc(deps.SRM.ListPulseSurveys))))
	api.HandleH("POST /srm/pulse-surveys", auth(srmView(http.HandlerFunc(deps.SRM.CreatePulseSurvey))))
	api.HandleH("GET /srm/pulse-surveys/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetPulseSurvey))))
	api.HandleH("POST /srm/pulse-surveys/{id}/responses", auth(srmView(http.HandlerFunc(deps.SRM.RecordSurveyResponse))))

	// Innovation register (Capture, Evaluation, Implementation).
	api.HandleH("GET /srm/innovations", auth(srmView(http.HandlerFunc(deps.SRM.ListInnovations))))
	api.HandleH("POST /srm/innovations", auth(srmView(http.HandlerFunc(deps.SRM.CreateInnovation))))
	api.HandleH("PUT /srm/innovations/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateInnovation))))
	api.HandleH("DELETE /srm/innovations/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteInnovation))))
	api.HandleH("PUT /srm/innovations/{id}/factors", auth(srmView(http.HandlerFunc(deps.SRM.UpsertInnovationFactor))))
	api.HandleH("GET /srm/innovations/{id}/recommendation", auth(srmView(http.HandlerFunc(deps.SRM.GetInnovationRecommendation))))
	api.HandleH("PUT /srm/innovations/{id}/recommendation", auth(srmView(http.HandlerFunc(deps.SRM.CreateInnovationRecommendation))))
	api.HandleH("POST /srm/innovations/{id}/objectives", auth(srmView(http.HandlerFunc(deps.SRM.AddInnovationObjective))))
	api.HandleH("PUT /srm/innovation-objectives/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateInnovationObjective))))
	api.HandleH("DELETE /srm/innovation-objectives/{id}", auth(srmView(http.HandlerFunc(deps.SRM.DeleteInnovationObjective))))
	api.HandleH("POST /srm/innovations/{id}/tasks", auth(srmView(http.HandlerFunc(deps.SRM.AddInnovationTask))))
	api.HandleH("PUT /srm/innovation-tasks/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateInnovationTask))))

	// Service improvement plans and disputes.
	api.HandleH("GET /srm/sips", auth(srmView(http.HandlerFunc(deps.SRM.ListServiceImprovementPlans))))
	api.HandleH("POST /srm/sips", auth(srmView(http.HandlerFunc(deps.SRM.CreateServiceImprovementPlan))))
	api.HandleH("GET /srm/sips/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetServiceImprovementPlan))))
	api.HandleH("PUT /srm/sips/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateServiceImprovementPlan))))
	api.HandleH("POST /srm/sips/{id}/deliverables", auth(srmView(http.HandlerFunc(deps.SRM.AddSipDeliverable))))
	api.HandleH("PUT /srm/sip-deliverables/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateSipDeliverable))))

	api.HandleH("GET /srm/disputes", auth(srmView(http.HandlerFunc(deps.SRM.ListDisputes))))
	api.HandleH("POST /srm/disputes", auth(srmView(http.HandlerFunc(deps.SRM.CreateDispute))))
	api.HandleH("GET /srm/disputes/{id}", auth(srmView(http.HandlerFunc(deps.SRM.GetDispute))))
	api.HandleH("PUT /srm/disputes/{id}", auth(srmView(http.HandlerFunc(deps.SRM.UpdateDispute))))
	api.HandleH("POST /srm/disputes/{id}/escalations", auth(srmView(http.HandlerFunc(deps.SRM.AddDisputeEscalation))))
	api.HandleH("PUT /srm/disputes/{id}/remedy-plan", auth(srmView(http.HandlerFunc(deps.SRM.UpsertRemedyPlan))))
}
