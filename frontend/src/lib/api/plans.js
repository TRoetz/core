import { createCrudClient } from './createCrudClient.js';
import { fetchAPI } from './core.js';

/**
 * Plans module API client.
 * Every endpoint is gated on the global `plans.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const plans = {
  list: (params) => fetchAPI(`/plans${params ? `?${new URLSearchParams(params)}` : ''}`),
  create: (data) => fetchAPI('/plans', { method: 'POST', body: JSON.stringify(data) }),
  get: (id) => fetchAPI(`/plans/${id}`),
  update: (id, data) => fetchAPI(`/plans/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id) => fetchAPI(`/plans/${id}`, { method: 'DELETE' }),

  // Aggregate plan payload (plan + workspace/P3M/stakeholder links).
  getDetail: (id) => fetchAPI(`/plans/${id}/detail`),

  // Plan <-> workspaces.
  setWorkspaces: (id, workspaceIds) =>
    fetchAPI(`/plans/${id}/workspaces`, { method: 'PUT', body: JSON.stringify({ workspace_ids: workspaceIds }) }),
  addWorkspace: (id, workspaceId) => fetchAPI(`/plans/${id}/workspaces/${workspaceId}`, { method: 'POST' }),
  removeWorkspace: (id, workspaceId) =>
    fetchAPI(`/plans/${id}/workspaces/${workspaceId}`, { method: 'DELETE' }),

  // Plan <-> P3M entities (portfolio/programme/project).
  addP3MEntity: (id, data) => fetchAPI(`/plans/${id}/p3m-entities`, { method: 'POST', body: JSON.stringify(data) }),
  removeP3MEntity: (id, entityType, entityId) =>
    fetchAPI(`/plans/${id}/p3m-entities/${entityType}/${entityId}`, { method: 'DELETE' }),

  // Plan <-> SMS stakeholders.
  addStakeholder: (id, stakeholderId) =>
    fetchAPI(`/plans/${id}/stakeholders`, { method: 'POST', body: JSON.stringify({ stakeholder_id: stakeholderId }) }),
  removeStakeholder: (id, stakeholderId) =>
    fetchAPI(`/plans/${id}/stakeholders/${stakeholderId}`, { method: 'DELETE' }),

  // Flat plan/workspace pairs (Big Gantt plan grouping).
  getWorkspaceMap: () => fetchAPI('/plans/workspace-map'),

  // Flat plan/P3M-entity pairs (linked plans per P3M entity).
  getP3MMap: () => fetchAPI('/plans/p3m-map'),
};
