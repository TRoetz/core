import { createCrudClient } from './createCrudClient.js';
import { fetchAPI } from './core.js';

/**
 * P3M (Portfolio, Programme and Project Management) API client.
 * Every endpoint is gated on the global `p3m.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const p3m = {
  // Aggregate views backing the P3M page.
  getHierarchy: (filters = {}) => fetchAPI(`/p3m/hierarchy${qs(filters)}`),
  getExecutiveSummary: () => fetchAPI('/p3m/executive-summary'),

  portfolios: createCrudClient('/p3m/portfolios'),
  programmes: createCrudClient('/p3m/programmes'),
  projects: createCrudClient('/p3m/projects'),

  // Governance.
  getStageGates: (filters = {}) => fetchAPI(`/p3m/stage-gates${qs(filters)}`),
  createStageGate: (data) => post('/p3m/stage-gates', data),
  decideStageGate: (id, data) => post(`/p3m/stage-gates/${id}/decision`, data),
  updateGateArtifact: (id, data) => put(`/p3m/gate-artifacts/${id}`, data),

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
  createBenefitRealization: (benefitId, data) => post(`/p3m/benefits/${benefitId}/realizations`, data),
  deleteBenefitRealization: (benefitId, realizationId) => del(`/p3m/benefits/${benefitId}/realizations/${realizationId}`),

  getProjectBaselines: (projectId) => fetchAPI(`/p3m/projects/${projectId}/baselines`),
  createProjectBaseline: (projectId, data = {}) => post(`/p3m/projects/${projectId}/baselines`, data),

  tranches: createCrudClient('/p3m/tranches'),
  getForecast: (filters = {}) => fetchAPI(`/p3m/forecast${qs(filters)}`),

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
};

function qs(params = {}) {
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== '');
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
