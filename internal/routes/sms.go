package routes

import (
	"net/http"

	"windshift/internal/models"
)

// RegisterSMSRoutes registers the Stakeholder Management System module.
// Every endpoint requires authentication plus the global sms.view permission,
// which is granted per group through role permissions. The route table below
// is the contract for the handler methods implemented across
// handlers/sms.go (core: dashboard, stakeholders, P3M entity links),
// handlers/sms_subresources.go (communications, engagement plans, issues &
// risks, tasks, feedback, relationship goals) and handlers/sms_activity.go
// (cross-stakeholder lists, timeline, CSV export).
func RegisterSMSRoutes(deps *Deps) {
	api := deps.API
	auth := deps.AuthMiddleware.RequireAuth
	smsView := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionSMSView)

	// Module dashboard.
	api.HandleH("GET /sms/dashboard", auth(smsView(http.HandlerFunc(deps.SMS.GetDashboard))))

	// Stakeholder directory.
	api.HandleH("GET /sms/stakeholders", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholders))))
	api.HandleH("POST /sms/stakeholders", auth(smsView(http.HandlerFunc(deps.SMS.CreateStakeholder))))
	api.HandleH("GET /sms/stakeholders/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetStakeholder))))
	api.HandleH("PATCH /sms/stakeholders/{id}", auth(smsView(http.HandlerFunc(deps.SMS.UpdateStakeholder))))
	api.HandleH("DELETE /sms/stakeholders/{id}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteStakeholder))))

	// P3M entity links (shared entities; options endpoint lets sms.view-only
	// users pick link targets without p3m.view, mirroring SRM).
	api.HandleH("POST /sms/stakeholders/{id}/entities", auth(smsView(http.HandlerFunc(deps.SMS.AddStakeholderEntity))))
	api.HandleH("DELETE /sms/entities/{id}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteEntity))))
	api.HandleH("GET /sms/p3m-options", auth(smsView(http.HandlerFunc(deps.SMS.GetP3MOptions))))

	// Communications log (per stakeholder + cross-stakeholder feed).
	api.HandleH("GET /sms/stakeholders/{id}/communications", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderCommunications))))
	api.HandleH("POST /sms/stakeholders/{id}/communications", auth(smsView(http.HandlerFunc(deps.SMS.CreateCommunication))))
	api.HandleH("DELETE /sms/communications/{cid}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteCommunication))))
	api.HandleH("GET /sms/communications", auth(smsView(http.HandlerFunc(deps.SMS.GetCommunications))))

	// Engagement plans (per stakeholder + cross-stakeholder list).
	api.HandleH("GET /sms/stakeholders/{id}/engagement-plans", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderPlans))))
	api.HandleH("POST /sms/stakeholders/{id}/engagement-plans", auth(smsView(http.HandlerFunc(deps.SMS.CreatePlan))))
	api.HandleH("PUT /sms/engagement-plans/{pid}", auth(smsView(http.HandlerFunc(deps.SMS.UpdatePlan))))
	api.HandleH("DELETE /sms/engagement-plans/{pid}", auth(smsView(http.HandlerFunc(deps.SMS.DeletePlan))))
	api.HandleH("GET /sms/engagement-plans", auth(smsView(http.HandlerFunc(deps.SMS.ListEngagementPlans))))

	// Issues & risks.
	api.HandleH("GET /sms/stakeholders/{id}/issues", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderIssues))))
	api.HandleH("POST /sms/stakeholders/{id}/issues", auth(smsView(http.HandlerFunc(deps.SMS.CreateIssue))))
	api.HandleH("PUT /sms/issues/{iid}", auth(smsView(http.HandlerFunc(deps.SMS.UpdateIssue))))
	api.HandleH("DELETE /sms/issues/{iid}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteIssue))))
	api.HandleH("POST /sms/issues/{iid}/escalate", auth(smsView(http.HandlerFunc(deps.SMS.EscalateIssue))))

	// Tasks.
	api.HandleH("GET /sms/stakeholders/{id}/tasks", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderTasks))))
	api.HandleH("POST /sms/stakeholders/{id}/tasks", auth(smsView(http.HandlerFunc(deps.SMS.CreateTask))))
	api.HandleH("PUT /sms/tasks/{tid}", auth(smsView(http.HandlerFunc(deps.SMS.UpdateTask))))
	api.HandleH("DELETE /sms/tasks/{tid}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteTask))))

	// Feedback.
	api.HandleH("GET /sms/stakeholders/{id}/feedback", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderFeedback))))
	api.HandleH("POST /sms/stakeholders/{id}/feedback", auth(smsView(http.HandlerFunc(deps.SMS.CreateFeedback))))
	api.HandleH("PUT /sms/feedback/{fid}", auth(smsView(http.HandlerFunc(deps.SMS.UpdateFeedback))))
	api.HandleH("DELETE /sms/feedback/{fid}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteFeedback))))

	// Relationship goals.
	api.HandleH("GET /sms/stakeholders/{id}/goals", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderGoals))))
	api.HandleH("POST /sms/stakeholders/{id}/goals", auth(smsView(http.HandlerFunc(deps.SMS.CreateGoal))))
	api.HandleH("PUT /sms/goals/{gid}", auth(smsView(http.HandlerFunc(deps.SMS.UpdateGoal))))
	api.HandleH("DELETE /sms/goals/{gid}", auth(smsView(http.HandlerFunc(deps.SMS.DeleteGoal))))

	// Timeline and CSV export.
	api.HandleH("GET /sms/stakeholders/{id}/timeline", auth(smsView(http.HandlerFunc(deps.SMS.GetTimeline))))
	api.HandleH("GET /sms/export", auth(smsView(http.HandlerFunc(deps.SMS.ExportCSV))))
}
