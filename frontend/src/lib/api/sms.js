import { createCrudClient } from './createCrudClient.js';
import { fetchAPI } from './core.js';

/**
 * SMS (Stakeholder Management System) API client.
 * Every endpoint is gated on the global `sms.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const sms = {
  // Module dashboard.
  getDashboard: () => fetchAPI('/sms/dashboard'),

  // Stakeholder profiles (PATCH semantics: partial update).
  getStakeholders: (filters = {}) => fetchAPI(`/sms/stakeholders${qs(filters)}`),
  getStakeholder: (id) => fetchAPI(`/sms/stakeholders/${id}`),
  createStakeholder: (data) => post('/sms/stakeholders', data),
  updateStakeholder: (id, data) => patch(`/sms/stakeholders/${id}`, data),
  deleteStakeholder: (id) => del(`/sms/stakeholders/${id}`),

  // P3M entity links (shared projects).
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

  // CSV export download URLs.
  exportUrl: (scope) => `/sms/export?scope=${encodeURIComponent(scope)}&format=csv`,
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

function del(path) {
  return fetchAPI(path, { method: 'DELETE' });
}
