import { API_BASE, fetchAPI } from './core.js';
import { createCrudClient } from './createCrudClient.js';

/**
 * P3M (Portfolio, Programme and Project Management) API client.
 * Every endpoint is gated on the global `p3m.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const p3m = {
  // Aggregate views backing the P3M page.
  getHierarchy: (filters = {}) => fetchAPI(`/p3m/hierarchy${qs(filters)}`),
  getExecutiveSummary: () => fetchAPI('/p3m/executive-summary'),

  // Register exports (CSV). Direct-download URL; cookie auth applies.
  exportUrl: (scope) => `${API_BASE}/p3m/export?scope=${encodeURIComponent(scope)}&format=csv`,

  portfolios: createCrudClient('/p3m/portfolios'),
  programmes: createCrudClient('/p3m/programmes'),
  projects: createCrudClient('/p3m/projects'),

  // Cross-module project detail: financial rollup (budget, tranches, SRM
  // contract spend, worklog cost), merged stakeholders and attached contracts.
  getProjectRollup: (projectId) => fetchAPI(`/p3m/projects/${projectId}/rollup`),
  // Unified recent-event feed for the project detail view: stage gate reviews,
  // budget approvals, architecture review decisions and communications.
  getProjectActivity: (projectId) => fetchAPI(`/p3m/projects/${projectId}/activity`),
  getProjectWorkspaces: (projectId) => fetchAPI(`/p3m/projects/${projectId}/workspaces`),
  setProjectWorkspaces: (projectId, workspaceIds) =>
    put(`/p3m/projects/${projectId}/workspaces`, { workspace_ids: workspaceIds }),

  // Critical path: CPM schedule analysis over the dated work items in the
  // entity's linked workspaces. Cycle errors arrive as API error strings.
  getProjectCriticalPath: (projectId) => fetchAPI(`/p3m/projects/${projectId}/critical-path`),
  getProgrammeCriticalPath: (programmeId) =>
    fetchAPI(`/p3m/programmes/${programmeId}/critical-path`),

  // What-if schedule preview (RAID-004): recompute the project CPM with the
  // proposed item changes applied. Read-only - nothing is saved.
  getCpmPreview: (projectId, data) => post(`/p3m/projects/${projectId}/cpm-preview`, data),

  // Leave impact: active user leaves overlapping resource allocations and
  // critical-path tasks. Visibility only - no rescheduling is performed.
  getProjectLeaveImpact: (projectId) => fetchAPI(`/p3m/projects/${projectId}/leave-impact`),
  getProgrammeLeaveImpact: (programmeId) => fetchAPI(`/p3m/programmes/${programmeId}/leave-impact`),

  // Governance.
  getStageGates: (filters = {}) => fetchAPI(`/p3m/stage-gates${qs(filters)}`),
  createStageGate: (data) => post('/p3m/stage-gates', data),
  decideStageGate: (id, data) => post(`/p3m/stage-gates/${id}/decision`, data),
  updateGateArtifact: (id, data) => put(`/p3m/gate-artifacts/${id}`, data),

  // Architecture review board.
  architectureReviews: createCrudClient('/p3m/architecture-reviews'),
  decideArchitectureReview: (id, data) => post(`/p3m/architecture-reviews/${id}/decision`, data),

  getRaciMatrix: (filters = {}) => fetchAPI(`/p3m/raci${qs(filters)}`),
  upsertRaciAssignment: (data) => put('/p3m/raci', data),
  deleteRaciAssignment: (id) => del(`/p3m/raci/${id}`),

  getAuthorizationThresholds: () => fetchAPI('/p3m/authorization-thresholds'),
  updateAuthorizationThreshold: (data) => put('/p3m/authorization-thresholds', data),

  getBudgetApprovals: (filters = {}) => fetchAPI(`/p3m/budget-approvals${qs(filters)}`),
  createBudgetApproval: (data) => post('/p3m/budget-approvals', data),
  decideBudgetApproval: (id, data) => post(`/p3m/budget-approvals/${id}/decision`, data),

  // Benefits and financials.
  benefits: createCrudClient('/p3m/benefits'),
  getBenefitRealizations: (benefitId) => fetchAPI(`/p3m/benefits/${benefitId}/realizations`),
  createBenefitRealization: (benefitId, data) =>
    post(`/p3m/benefits/${benefitId}/realizations`, data),
  deleteBenefitRealization: (benefitId, realizationId) =>
    del(`/p3m/benefits/${benefitId}/realizations/${realizationId}`),

  getProjectBaselines: (projectId) => fetchAPI(`/p3m/projects/${projectId}/baselines`),
  createProjectBaseline: (projectId, data = {}) =>
    post(`/p3m/projects/${projectId}/baselines`, data),

  tranches: createCrudClient('/p3m/tranches'),
  getForecast: (filters = {}) => fetchAPI(`/p3m/forecast${qs(filters)}`),

  // Investment cases: per-project prioritisation pipeline and scores.
  getInvestmentCases: (filters = {}) => fetchAPI(`/p3m/investment-cases${qs(filters)}`),
  createInvestmentCase: (data) => post('/p3m/investment-cases', data),
  updateInvestmentCase: (id, data) => put(`/p3m/investment-cases/${id}`, data),
  deleteInvestmentCase: (id) => del(`/p3m/investment-cases/${id}`),
  decideInvestmentCase: (id, data) => post(`/p3m/investment-cases/${id}/decision`, data),

  // Risk, stakeholders and resources.
  raidItems: createCrudClient('/p3m/raid-items'),
  escalateRaidItem: (id, data = {}) => post(`/p3m/raid-items/${id}/escalate`, data),

  stakeholders: createCrudClient('/p3m/stakeholders'),
  communications: {
    getAll: (filters = {}) => fetchAPI(`/p3m/communications${qs(filters)}`),
    create: (data) => post('/p3m/communications', data),
    delete: (id) => del(`/p3m/communications/${id}`),
  },

  resources: createCrudClient('/p3m/resources'),
  resourceAllocations: createCrudClient('/p3m/resource-allocations'),
  getResourceCapacity: (filters = {}) => fetchAPI(`/p3m/resource-capacity${qs(filters)}`),

  // Front Door intake pack (FD-*).
  getFrontDoorIntakes: (filters = {}) => fetchAPI(`/p3m/front-door${qs(filters)}`),
  createFrontDoorIntake: (data) => post('/p3m/front-door', data),
  updateFrontDoorIntake: (id, data) => put(`/p3m/front-door/${id}`, data),
  decideFrontDoorIntake: (id, data) => post(`/p3m/front-door/${id}/decision`, data),

  // Finance system sync (FIN-007): configs, runs and the unmatched review queue.
  getFinanceSyncConfigs: () => fetchAPI('/p3m/finance-sync/configs'),
  createFinanceSyncConfig: (data) => post('/p3m/finance-sync/configs', data),
  updateFinanceSyncConfig: (id, data) => put(`/p3m/finance-sync/configs/${id}`, data),
  deleteFinanceSyncConfig: (id) => del(`/p3m/finance-sync/configs/${id}`),
  runFinanceSyncNow: (id) => post(`/p3m/finance-sync/configs/${id}/run`),
  // Multipart CSV upload; the browser sets the multipart boundary, so this
  // bypasses the JSON helpers.
  uploadFinanceSyncCSV: async (id, file) => {
    const fd = new FormData();
    fd.append('file', file);
    const res = await fetch(`${API_BASE}/p3m/finance-sync/configs/${id}/upload`, {
      method: 'POST',
      body: fd, // Don't set Content-Type for FormData - browser sets it with boundary
      credentials: 'same-origin',
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Upload failed');
    }
    return res.json().catch(() => ({}));
  },
  getFinanceSyncRuns: (filters = {}) => fetchAPI(`/p3m/finance-sync/runs${qs(filters)}`),
  getFinanceSyncRun: (id) => fetchAPI(`/p3m/finance-sync/runs/${id}`),
  getUnmatchedFinanceRows: (filters = {}) => fetchAPI(`/p3m/finance-sync/unmatched${qs(filters)}`),
  assignUnmatchedFinanceRow: (factId, data) => post(`/p3m/finance-sync/unmatched/${factId}/assign`, data),
  rejectUnmatchedFinanceRow: (factId, data = {}) => post(`/p3m/finance-sync/unmatched/${factId}/reject`, data),
  getFinanceSyncSummary: () => fetchAPI('/p3m/finance-sync/summary'),

  // Phase-2 gap closure: change calendar (CHG-008/009).
  changeCalendar: createCrudClient('/p3m/change-calendar'),
  getChangeCalendarWeek: (filters = {}) => fetchAPI(`/p3m/change-calendar/week${qs(filters)}`),

  // Change impact + readiness assessments (CHG-002/006).
  getChangeAssessments: (projectId) => fetchAPI(`/p3m/projects/${projectId}/change-assessments`),
  createChangeAssessment: (projectId, data) => post(`/p3m/projects/${projectId}/change-assessments`, data),
  updateChangeAssessment: (id, data) => put(`/p3m/change-assessments/${id}`, data),
  getReadinessAssessments: (projectId) => fetchAPI(`/p3m/projects/${projectId}/readiness-assessments`),
  createReadinessAssessment: (projectId, data) => post(`/p3m/projects/${projectId}/readiness-assessments`, data),
  updateReadinessAssessment: (id, data) => put(`/p3m/readiness-assessments/${id}`, data),

  // Benefit measurements + portfolio rollup (BEN-007/009).
  getBenefitMeasurements: (benefitId) => fetchAPI(`/p3m/benefits/${benefitId}/measurements`),
  createBenefitMeasurement: (benefitId, data) => post(`/p3m/benefits/${benefitId}/measurements`, data),
  updateBenefitMeasurement: (id, data) => put(`/p3m/benefit-measurements/${id}`, data),
  getBenefitsRollup: (filters = {}) => fetchAPI(`/p3m/benefits/rollup${qs(filters)}`),

  // Report snapshots + tolerance profiles (RPT-005..007).
  listReportSnapshots: (filters = {}) => fetchAPI(`/p3m/report-snapshots${qs(filters)}`),
  createReportSnapshot: (data, filters = {}) => post(`/p3m/report-snapshots${qs(filters)}`, data),
  getReportSnapshot: (id) => fetchAPI(`/p3m/report-snapshots/${id}`),
  suggestCategoryRags: (filters = {}) => fetchAPI(`/p3m/report-snapshots/suggest${qs(filters)}`),
  toleranceProfiles: createCrudClient('/p3m/tolerance-profiles'),

  // RAID rollups + CPM what-if preview (RAID-001/004/006).
  getProgrammeRaidRollup: (programmeId) => fetchAPI(`/p3m/programmes/${programmeId}/raid-rollup`),
  getPortfolioRaidRollup: (portfolioId) => fetchAPI(`/p3m/portfolios/${portfolioId}/raid-rollup`),
  previewProjectCpm: (projectId, data) => post(`/p3m/projects/${projectId}/cpm-preview`, data),

  // Resource scenario modelling (RES-004/006).
  runResourceScenario: (data) => post('/p3m/resource-scenarios', data),

  // Prioritisation scoring + roadmap (PO-003..005, 008).
  scoringCriteria: createCrudClient('/p3m/scoring-criteria'),
  getInvestmentCaseScores: (id) => fetchAPI(`/p3m/investment-cases/${id}/scores`),
  upsertInvestmentCaseScore: (id, data) => put(`/p3m/investment-cases/${id}/scores`, data),
  getRankedInvestmentCases: (filters = {}) => fetchAPI(`/p3m/investment-cases/ranked${qs(filters)}`),
  getPortfolioRoadmap: (portfolioId) => fetchAPI(`/p3m/portfolios/${portfolioId}/roadmap`),

  // Financial exception alerts (FIN-009).
  financeAlertRules: createCrudClient('/p3m/finance-alert-rules'),
  evaluateAllFinanceAlerts: () => post('/p3m/evaluate-finance-alerts'),
  evaluateProjectFinanceAlerts: (projectId) => post(`/p3m/projects/${projectId}/evaluate-finance-alerts`),

  // Closure & handover record (CLS-001).
  getClosureRecord: (projectId) => fetchAPI(`/p3m/projects/${projectId}/closure-record`),
  createClosureRecord: (projectId, data) => post(`/p3m/projects/${projectId}/closure-record`, data),
  updateClosureRecord: (id, data) => put(`/p3m/closure-records/${id}`, data),
};

function qs(params = {}) {
  const entries = Object.entries(params).filter(
    ([, v]) => v !== undefined && v !== null && v !== ''
  );
  if (!entries.length) return '';
  return `?${new URLSearchParams(entries.map(([k, v]) => [k, String(v)])).toString()}`;
}

function post(path, data) {
  return fetchAPI(path, { method: 'POST', body: JSON.stringify(data ?? {}) });
}

function put(path, data) {
  return fetchAPI(path, { method: 'PUT', body: JSON.stringify(data ?? {}) });
}

function del(path) {
  return fetchAPI(path, { method: 'DELETE' });
}
