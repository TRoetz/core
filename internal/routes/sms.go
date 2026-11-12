package routes

import (
	"net/http"

	"windshift/internal/models"
)

// RegisterSMSRoutes registers the Stakeholder Management System module.
// Read endpoints require the global sms.view permission; every write action
// additionally requires sms.manage (least privilege).
// Restricted-classification fields are masked at handler level for users
// without sms.sensitive.view. The route table below is the contract for the
// handler methods implemented across handlers/sms.go (core: dashboard,
// stakeholders, P3M entity links), handlers/sms_subresources.go
// (communications, engagement plans, issues & risks, tasks, feedback,
// relationship goals), handlers/sms_relationship.go (commitments, grievances,
// decisions, groups, links, transfers, pilot baselines) and
// handlers/sms_activity.go (cross-stakeholder lists, timeline, CSV export).
func RegisterSMSRoutes(deps *Deps) {
	api := deps.API
	auth := deps.AuthMiddleware.RequireAuth
	smsView := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionSMSView)
	smsManage := deps.PermissionMiddleware.RequireGlobalPermission(models.PermissionSMSManage)

	// Module dashboard.
	api.HandleH("GET /sms/dashboard", auth(smsView(http.HandlerFunc(deps.SMS.GetDashboard))))
	api.HandleH("GET /sms/dashboard/executive", auth(smsView(http.HandlerFunc(deps.SMS.GetExecutiveDashboard))))
	api.HandleH("GET /sms/dashboard/programme", auth(smsView(http.HandlerFunc(deps.SMS.GetProgrammeDashboard))))

	// Pilot success measures (Doc 8 Go/No-Go evidence).
	api.HandleH("GET /sms/pilot-metrics", auth(smsView(http.HandlerFunc(deps.SMS.GetPilotMetrics))))

	// Stakeholder directory.
	api.HandleH("GET /sms/stakeholders", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholders))))
	api.HandleH("POST /sms/stakeholders", auth(smsManage(http.HandlerFunc(deps.SMS.CreateStakeholder))))
	api.HandleH("GET /sms/stakeholders/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetStakeholder))))
	api.HandleH("PATCH /sms/stakeholders/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateStakeholder))))
	api.HandleH("DELETE /sms/stakeholders/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteStakeholder))))

	// Register hygiene: duplicate detection + merge.
	api.HandleH("GET /sms/stakeholders/duplicates", auth(smsView(http.HandlerFunc(deps.SMS.ListDuplicateStakeholders))))
	api.HandleH("POST /sms/stakeholders/{id}/merge", auth(smsManage(http.HandlerFunc(deps.SMS.MergeStakeholder))))

	// AI assist (WI-6.5): advisory suggestions, off until an LLM connection is configured.
	api.HandleH("GET /sms/stakeholders/{id}/ai-assist", auth(smsView(http.HandlerFunc(deps.SMS.AIAssist))))

	// P3M entity links (shared entities; options endpoint lets sms.view-only
	// users pick link targets without p3m.view, mirroring SRM).
	api.HandleH("POST /sms/stakeholders/{id}/entities", auth(smsManage(http.HandlerFunc(deps.SMS.AddStakeholderEntity))))
	api.HandleH("DELETE /sms/entities/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteEntity))))
	api.HandleH("GET /sms/p3m-options", auth(smsView(http.HandlerFunc(deps.SMS.GetP3MOptions))))

	// Communications log (per stakeholder + cross-stakeholder feed).
	api.HandleH("GET /sms/stakeholders/{id}/communications", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderCommunications))))
	api.HandleH("POST /sms/stakeholders/{id}/communications", auth(smsManage(http.HandlerFunc(deps.SMS.CreateCommunication))))
	api.HandleH("DELETE /sms/communications/{cid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteCommunication))))
	api.HandleH("GET /sms/communications", auth(smsView(http.HandlerFunc(deps.SMS.GetCommunications))))

	// Engagement plans (per stakeholder + cross-stakeholder list).
	api.HandleH("GET /sms/stakeholders/{id}/engagement-plans", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderPlans))))
	api.HandleH("POST /sms/stakeholders/{id}/engagement-plans", auth(smsManage(http.HandlerFunc(deps.SMS.CreatePlan))))
	api.HandleH("PUT /sms/engagement-plans/{pid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdatePlan))))
	api.HandleH("DELETE /sms/engagement-plans/{pid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeletePlan))))
	api.HandleH("GET /sms/engagement-plans", auth(smsView(http.HandlerFunc(deps.SMS.ListEngagementPlans))))

	// Issues & risks.
	api.HandleH("GET /sms/stakeholders/{id}/issues", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderIssues))))
	api.HandleH("POST /sms/stakeholders/{id}/issues", auth(smsManage(http.HandlerFunc(deps.SMS.CreateIssue))))
	api.HandleH("PUT /sms/issues/{iid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateIssue))))
	api.HandleH("DELETE /sms/issues/{iid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteIssue))))
	api.HandleH("POST /sms/issues/{iid}/escalate", auth(smsManage(http.HandlerFunc(deps.SMS.EscalateIssue))))

	// Tasks.
	api.HandleH("GET /sms/stakeholders/{id}/tasks", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderTasks))))
	api.HandleH("POST /sms/stakeholders/{id}/tasks", auth(smsManage(http.HandlerFunc(deps.SMS.CreateTask))))
	api.HandleH("PUT /sms/tasks/{tid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateTask))))
	api.HandleH("DELETE /sms/tasks/{tid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteTask))))

	// Feedback.
	api.HandleH("GET /sms/stakeholders/{id}/feedback", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderFeedback))))
	api.HandleH("POST /sms/stakeholders/{id}/feedback", auth(smsManage(http.HandlerFunc(deps.SMS.CreateFeedback))))
	api.HandleH("PUT /sms/feedback/{fid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateFeedback))))
	api.HandleH("DELETE /sms/feedback/{fid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteFeedback))))

	// Relationship goals.
	api.HandleH("GET /sms/stakeholders/{id}/goals", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderGoals))))
	api.HandleH("POST /sms/stakeholders/{id}/goals", auth(smsManage(http.HandlerFunc(deps.SMS.CreateGoal))))
	api.HandleH("PUT /sms/goals/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateGoal))))
	api.HandleH("DELETE /sms/goals/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteGoal))))

	// Timeline and CSV export.
	api.HandleH("GET /sms/stakeholders/{id}/timeline", auth(smsView(http.HandlerFunc(deps.SMS.GetTimeline))))
	api.HandleH("GET /sms/export", auth(smsView(http.HandlerFunc(deps.SMS.ExportCSV))))

	// Cross-store duplicate report (p3m/srm ↔ SMS register links).
	api.HandleH("GET /sms/cross-store-duplicates", auth(smsView(http.HandlerFunc(deps.SMS.ListCrossStoreDuplicates))))
	api.HandleH("POST /sms/cross-store-duplicates/link", auth(smsManage(http.HandlerFunc(deps.SMS.LinkCrossStoreStakeholder))))

	// Commitment register.
	api.HandleH("GET /sms/stakeholders/{id}/commitments", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderCommitments))))
	api.HandleH("POST /sms/stakeholders/{id}/commitments", auth(smsManage(http.HandlerFunc(deps.SMS.CreateCommitment))))
	api.HandleH("PUT /sms/commitments/{cid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateCommitment))))
	api.HandleH("DELETE /sms/commitments/{cid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteCommitment))))
	api.HandleH("POST /sms/commitments/{cid}/escalate", auth(smsManage(http.HandlerFunc(deps.SMS.EscalateCommitment))))

	// Grievances.
	api.HandleH("GET /sms/stakeholders/{id}/grievances", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderGrievances))))
	api.HandleH("POST /sms/stakeholders/{id}/grievances", auth(smsManage(http.HandlerFunc(deps.SMS.CreateGrievance))))
	api.HandleH("PUT /sms/grievances/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateGrievance))))
	api.HandleH("DELETE /sms/grievances/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteGrievance))))
	api.HandleH("POST /sms/grievances/{gid}/escalate", auth(smsManage(http.HandlerFunc(deps.SMS.EscalateGrievance))))

	// Public grievance intake webform (WI-5.3): unauthenticated and rate-limited
	// like portal submissions; creates an sms_grievances row directly so the SMS
	// register stays the single system of truth for complaint lifecycle.
	api.HandleH("POST /sms/grievances/intake", deps.PortalSubmitLimiter.Limit(http.HandlerFunc(deps.SMS.IntakeGrievance)))

	// Decision log.
	api.HandleH("GET /sms/stakeholders/{id}/decisions", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderDecisions))))
	api.HandleH("POST /sms/stakeholders/{id}/decisions", auth(smsManage(http.HandlerFunc(deps.SMS.CreateDecision))))
	api.HandleH("DELETE /sms/decisions/{did}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteDecision))))

	// Stakeholder groups.
	api.HandleH("GET /sms/groups", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderGroups))))
	api.HandleH("POST /sms/groups", auth(smsManage(http.HandlerFunc(deps.SMS.CreateGroup))))
	api.HandleH("PUT /sms/groups/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateGroup))))
	api.HandleH("DELETE /sms/groups/{gid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteGroup))))
	api.HandleH("POST /sms/groups/{gid}/members", auth(smsManage(http.HandlerFunc(deps.SMS.AddGroupMember))))
	api.HandleH("DELETE /sms/groups/{gid}/members/{sid}", auth(smsManage(http.HandlerFunc(deps.SMS.RemoveGroupMember))))

	// Stakeholder network links.
	api.HandleH("GET /sms/stakeholders/{id}/links", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderLinks))))
	api.HandleH("POST /sms/links", auth(smsManage(http.HandlerFunc(deps.SMS.CreateLink))))
	api.HandleH("DELETE /sms/links/{lid}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteLink))))

	// Ownership transfers (handover).
	api.HandleH("GET /sms/stakeholders/{id}/transfers", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderTransfers))))
	api.HandleH("POST /sms/stakeholders/{id}/transfers", auth(smsManage(http.HandlerFunc(deps.SMS.CreateTransfer))))
	api.HandleH("PUT /sms/transfers/{tid}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateTransfer))))

	// Stakeholder rollups: site occupancy, notification history and RACI roles.
	api.HandleH("GET /sms/stakeholders/{id}/nodes", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderNodes))))
	api.HandleH("GET /sms/stakeholders/{id}/notifications", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderNotifications))))
	api.HandleH("GET /sms/stakeholders/{id}/raci-roles", auth(smsView(http.HandlerFunc(deps.SMS.ListStakeholderRaciRoles))))

	// Pilot success-measure baselines.
	api.HandleH("GET /sms/pilot-baselines", auth(smsView(http.HandlerFunc(deps.SMS.ListPilotBaselines))))
	api.HandleH("PUT /sms/pilot-baselines", auth(smsManage(http.HandlerFunc(deps.SMS.UpsertPilotBaseline))))

	// Data-quality report (Std §17 operational tier).
	api.HandleH("GET /sms/data-quality", auth(smsView(http.HandlerFunc(deps.SMS.GetDataQuality))))

	// Data movement: CSV import with preview/dedupe, migration-ready export,
	// GeoJSON location feed.
	api.HandleH("POST /sms/import/preview", auth(smsManage(http.HandlerFunc(deps.SMS.ImportPreview))))
	api.HandleH("POST /sms/import/execute", auth(smsManage(http.HandlerFunc(deps.SMS.ImportExecute))))
	// Site & occupancy import (roadworks structure capture).
	api.HandleH("POST /sms/import/sites/preview", auth(smsManage(http.HandlerFunc(deps.SMS.ImportSitesPreview))))
	api.HandleH("POST /sms/import/sites/execute", auth(smsManage(http.HandlerFunc(deps.SMS.ImportSitesExecute))))
	// Full-register restore: round-trips GET /sms/export/full with IDs intact.
	api.HandleH("POST /sms/import/full", auth(smsManage(http.HandlerFunc(deps.SMS.ImportFull))))
	api.HandleH("GET /sms/export/full", auth(smsView(http.HandlerFunc(deps.SMS.ExportFullJSON))))
	api.HandleH("GET /sms/export/manifest", auth(smsView(http.HandlerFunc(deps.SMS.ExportManifest))))
	api.HandleH("GET /sms/export/geojson", auth(smsView(http.HandlerFunc(deps.SMS.ExportGeoJSON))))

	// Template artifacts (WI-5.3): catalog + downloadable renderings.
	api.HandleH("GET /sms/templates", auth(smsView(http.HandlerFunc(deps.SMS.ListTemplates))))
	api.HandleH("GET /sms/templates/{name}", auth(smsView(http.HandlerFunc(deps.SMS.DownloadTemplate))))

	// Integrations (WI-6.1, WI-6.3): email-forward ingestion + BI feed.
	api.HandleH("POST /sms/ingest/email", auth(smsManage(http.HandlerFunc(deps.SMS.IngestEmail))))
	api.HandleH("GET /sms/feed/powerbi", auth(smsView(http.HandlerFunc(deps.SMS.PowerBIFeed))))

	// Node taxonomy: admin-configurable kinds + role vocabulary (roadworks
	// sites & occupancy). Kinds seed on migration; is_system kinds are protected.
	api.HandleH("GET /sms/node-kinds", auth(smsView(http.HandlerFunc(deps.SMS.ListNodeKinds))))
	api.HandleH("POST /sms/node-kinds", auth(smsManage(http.HandlerFunc(deps.SMS.CreateNodeKind))))
	api.HandleH("PUT /sms/node-kinds/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateNodeKind))))
	api.HandleH("DELETE /sms/node-kinds/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteNodeKind))))
	api.HandleH("GET /sms/node-kinds/{id}/roles", auth(smsView(http.HandlerFunc(deps.SMS.ListNodeKindRoles))))
	api.HandleH("POST /sms/node-kinds/{id}/roles", auth(smsManage(http.HandlerFunc(deps.SMS.AddNodeKindRole))))
	api.HandleH("PUT /sms/node-kind-roles/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateNodeKindRole))))
	api.HandleH("DELETE /sms/node-kind-roles/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteNodeKindRole))))

	// Nodes (areas, road segments, buildings) + stakeholder membership.
	api.HandleH("GET /sms/nodes", auth(smsView(http.HandlerFunc(deps.SMS.ListNodes))))
	api.HandleH("POST /sms/nodes", auth(smsManage(http.HandlerFunc(deps.SMS.CreateNode))))
	api.HandleH("GET /sms/nodes/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetNode))))
	api.HandleH("PUT /sms/nodes/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateNode))))
	api.HandleH("DELETE /sms/nodes/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteNode))))
	api.HandleH("GET /sms/nodes/{id}/members", auth(smsView(http.HandlerFunc(deps.SMS.ListNodeMembers))))
	api.HandleH("POST /sms/nodes/{id}/members", auth(smsManage(http.HandlerFunc(deps.SMS.AddNodeMember))))
	api.HandleH("DELETE /sms/node-members/{mid}", auth(smsManage(http.HandlerFunc(deps.SMS.RemoveNodeMember))))

	// Notification campaigns + per-stakeholder delivery ledger.
	api.HandleH("GET /sms/notification-campaigns", auth(smsView(http.HandlerFunc(deps.SMS.ListNotificationCampaigns))))
	api.HandleH("POST /sms/notification-campaigns", auth(smsManage(http.HandlerFunc(deps.SMS.CreateNotificationCampaign))))
	api.HandleH("GET /sms/notification-campaigns/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetNotificationCampaign))))
	api.HandleH("PUT /sms/notification-campaigns/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateNotificationCampaign))))
	api.HandleH("DELETE /sms/notification-campaigns/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteNotificationCampaign))))
	api.HandleH("GET /sms/notification-campaigns/{id}/targets", auth(smsView(http.HandlerFunc(deps.SMS.ListCampaignTargets))))
	api.HandleH("POST /sms/notification-campaigns/{id}/targets", auth(smsManage(http.HandlerFunc(deps.SMS.AddCampaignTarget))))
	api.HandleH("DELETE /sms/campaign-targets/{tid}", auth(smsManage(http.HandlerFunc(deps.SMS.RemoveCampaignTarget))))
	api.HandleH("POST /sms/notification-campaigns/{id}/audience", auth(smsManage(http.HandlerFunc(deps.SMS.GenerateCampaignAudience))))
	api.HandleH("GET /sms/notification-campaigns/{id}/notifications", auth(smsView(http.HandlerFunc(deps.SMS.ListCampaignNotifications))))
	api.HandleH("PUT /sms/notifications/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateNotification))))
	api.HandleH("POST /sms/notifications/bulk-update", auth(smsManage(http.HandlerFunc(deps.SMS.BulkUpdateNotifications))))

	// RACI matrices (versioned, per P3M entity) + work packages.
	api.HandleH("GET /sms/racis", auth(smsView(http.HandlerFunc(deps.SMS.ListRacis))))
	api.HandleH("POST /sms/racis", auth(smsManage(http.HandlerFunc(deps.SMS.CreateRaci))))
	api.HandleH("GET /sms/racis/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetRaci))))
	api.HandleH("PUT /sms/racis/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateRaci))))
	api.HandleH("DELETE /sms/racis/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteRaci))))
	api.HandleH("PUT /sms/racis/{id}/assignments", auth(smsManage(http.HandlerFunc(deps.SMS.SaveRaciAssignments))))

	api.HandleH("GET /sms/work-packages", auth(smsView(http.HandlerFunc(deps.SMS.ListWorkPackages))))
	api.HandleH("POST /sms/work-packages", auth(smsManage(http.HandlerFunc(deps.SMS.CreateWorkPackage))))
	api.HandleH("GET /sms/work-packages/{id}", auth(smsView(http.HandlerFunc(deps.SMS.GetWorkPackage))))
	api.HandleH("PUT /sms/work-packages/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.UpdateWorkPackage))))
	api.HandleH("DELETE /sms/work-packages/{id}", auth(smsManage(http.HandlerFunc(deps.SMS.DeleteWorkPackage))))
	api.HandleH("POST /sms/work-packages/{id}/nodes", auth(smsManage(http.HandlerFunc(deps.SMS.AddWorkPackageNode))))
	api.HandleH("DELETE /sms/work-package-nodes/{lid}", auth(smsManage(http.HandlerFunc(deps.SMS.RemoveWorkPackageNode))))
}
