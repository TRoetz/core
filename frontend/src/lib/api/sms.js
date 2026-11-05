import { createCrudClient } from './createCrudClient.js';
import { fetchAPI, API_BASE } from './core.js';

/**
 * SMS (Stakeholder Management System) API client.
 * Every endpoint is gated on the global `sms.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const sms = {
  // Module dashboard + reporting tiers.
  getDashboard: () => fetchAPI('/sms/dashboard'),
  getExecutiveDashboard: () => fetchAPI('/sms/dashboard/executive'),
  getProgrammeDashboard: (params = {}) => fetchAPI(`/sms/dashboard/programme${qs(params)}`),

  // Pilot success measures (Doc 8 Go/No-Go evidence).
  getPilotMetrics: () => fetchAPI('/sms/pilot-metrics'),
  getPilotBaselines: () => fetchAPI('/sms/pilot-baselines'),
  upsertPilotBaseline: (data) => put('/sms/pilot-baselines', data),

  // Data-quality report.
  getDataQuality: (params = {}) => fetchAPI(`/sms/data-quality${qs(params)}`),

  // Stakeholder profiles (PATCH semantics: partial update).
  getStakeholders: (filters = {}) => fetchAPI(`/sms/stakeholders${qs(filters)}`),
  getStakeholder: (id) => fetchAPI(`/sms/stakeholders/${id}`),
  createStakeholder: (data) => post('/sms/stakeholders', data),
  updateStakeholder: (id, data) => patch(`/sms/stakeholders/${id}`, data),
  deleteStakeholder: (id) => del(`/sms/stakeholders/${id}`),

  // Register hygiene: duplicate detection + merge.
  getDuplicateStakeholders: () => fetchAPI('/sms/stakeholders/duplicates'),
  mergeStakeholder: (targetId, sourceId) => post(`/sms/stakeholders/${targetId}/merge`, { source_id: sourceId }),

  // Cross-store duplicates (p3m/srm identity links).
  getCrossStoreDuplicates: () => fetchAPI('/sms/cross-store-duplicates'),
  linkCrossStoreStakeholder: (data) => post('/sms/cross-store-duplicates/link', data),

  // P3M entity links (shared projects).
  getP3MOptions: (params = {}) => fetchAPI(`/sms/p3m-options${qs(params)}`),
  linkEntity: (stakeholderId, data) => post(`/sms/stakeholders/${stakeholderId}/entities`, data),
  unlinkEntity: (entityLinkId) => del(`/sms/entities/${entityLinkId}`),

  // Interaction log (no update endpoint; entries are immutable).
  getCommunications: (stakeholderId) => fetchAPI(`/sms/stakeholders/${stakeholderId}/communications`),
  createCommunication: (stakeholderId, data) => post(`/sms/stakeholders/${stakeholderId}/communications`, data),
  deleteCommunication: (id) => del(`/sms/communications/${id}`),

  // Engagement plans.
  engagementPlans: createCrudClient('/engagement-plans', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/engagement-plans',
  }),

  // Issues & risk register with escalation.
  issues: createCrudClient('/issues', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/issues',
  }),
  escalateIssue: (id) => post(`/sms/issues/${id}/escalate`),

  // Commitment register with escalation.
  commitments: createCrudClient('/commitments', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/commitments',
  }),
  escalateCommitment: (id) => post(`/sms/commitments/${id}/escalate`),

  // Grievance register with escalation.
  grievances: createCrudClient('/grievances', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/grievances',
  }),
  escalateGrievance: (id) => post(`/sms/grievances/${id}/escalate`),

  // Decision log.
  decisions: createCrudClient('/decisions', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/decisions',
  }),

  // Stakeholder groups.
  getGroups: () => fetchAPI('/sms/groups'),
  createGroup: (data) => post('/sms/groups', data),
  updateGroup: (id, data) => put(`/sms/groups/${id}`, data),
  deleteGroup: (id) => del(`/sms/groups/${id}`),
  addGroupMember: (groupId, stakeholderId) => post(`/sms/groups/${groupId}/members`, { stakeholder_id: stakeholderId }),
  removeGroupMember: (groupId, stakeholderId) => del(`/sms/groups/${groupId}/members/${stakeholderId}`),

  // Stakeholder network links.
  getStakeholderLinks: (stakeholderId) => fetchAPI(`/sms/stakeholders/${stakeholderId}/links`),
  createLink: (data) => post('/sms/links', data),
  deleteLink: (id) => del(`/sms/links/${id}`),

  // Ownership transfers (handover). Completing all three review checkboxes
  // finalises the transfer server-side and moves ownership.
  getTransfers: (stakeholderId) => fetchAPI(`/sms/stakeholders/${stakeholderId}/transfers`),
  createTransfer: (stakeholderId, data) => post(`/sms/stakeholders/${stakeholderId}/transfers`, data),
  updateTransfer: (id, data) => put(`/sms/transfers/${id}`, data),

  // Tasks, feedback and relationship goals.
  tasks: createCrudClient('/tasks', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/tasks',
  }),
  feedback: createCrudClient('/feedback', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/feedback',
  }),
  goals: createCrudClient('/goals', {
    parentPath: '/sms/stakeholders',
    itemPath: '/sms/goals',
  }),

  // Cross-stakeholder lists (Engagement tab).
  getGlobalCommunications: (params = {}) => fetchAPI(`/sms/communications${qs(params)}`),
  getGlobalPlans: (params = {}) => fetchAPI(`/sms/engagement-plans${qs(params)}`),

  // Merged activity timeline.
  getTimeline: (stakeholderId, limit = 50) => fetchAPI(`/sms/stakeholders/${stakeholderId}/timeline${qs({ limit })}`),

  // Data movement: CSV import with preview/dedupe + exports.
  importPreview: (csvText) => post('/sms/import/preview', { csv: csvText }),
  importExecute: (csvText, mode = 'create') => post('/sms/import/execute', { csv: csvText, mode }),
  exportManifest: () => fetchAPI('/sms/export/manifest'),
  exportUrl: (scope) => `${API_BASE}/sms/export?scope=${encodeURIComponent(scope)}&format=csv`,
  exportManifestUrl: () => `${API_BASE}/sms/export/manifest`,
  exportFullUrl: () => `${API_BASE}/sms/export/full`,
  exportGeoJsonUrl: () => `${API_BASE}/sms/export/geojson`,

  // Template artifacts (WI-5.3): catalog + downloads.
  getTemplates: () => fetchAPI('/sms/templates'),
  templateUrl: (name) => `${API_BASE}/sms/templates/${encodeURIComponent(name)}`,
};

function qs(params = {}) {
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== '');
  if (!entries.length) return '';
  return `?${new URLSearchParams(entries.map(([k, v]) => [k, String(v)])).toString()}`;
}

function post(path, data) {
  return fetchAPI(path, { method: 'POST', body: JSON.stringify(data ?? {}) });
}

function patch(path, data) {
  return fetchAPI(path, { method: 'PATCH', body: JSON.stringify(data ?? {}) });
}

function put(path, data) {
  return fetchAPI(path, { method: 'PUT', body: JSON.stringify(data ?? {}) });
}

function del(path) {
  return fetchAPI(path, { method: 'DELETE' });
}
