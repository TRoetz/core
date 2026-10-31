import { createCrudClient } from './createCrudClient.js';
import { fetchAPI } from './core.js';

/**
 * SRM (Supplier Relationship Management) API client.
 * Every endpoint is gated on the global `srm.view` permission server-side;
 * groups gain access through role permissions assigned to them.
 */
export const srm = {
  // Module dashboard.
  getSummary: () => fetchAPI('/srm/summary'),

  // Supplier directory + phase-gate state machine.
  suppliers: createCrudClient('/srm/suppliers'),
  addSupplierContact: (supplierId, data) => post(`/srm/suppliers/${supplierId}/contacts`, data),
  updateSupplierContact: (id, data) => put(`/srm/contacts/${id}`, data),
  deleteSupplierContact: (id) => del(`/srm/contacts/${id}`),

  getSupplierGates: (supplierId) => fetchAPI(`/srm/suppliers/${supplierId}/gates`),
  advanceGate: (supplierId, data = {}) => post(`/srm/suppliers/${supplierId}/gate-advance`, data),
  downgradeGate: (supplierId, data = {}) => post(`/srm/suppliers/${supplierId}/gate-downgrade`, data),
  closeSupplier: (supplierId, data = {}) => post(`/srm/suppliers/${supplierId}/close`, data),

  // Contract register.
  contracts: createCrudClient('/srm/contracts'),
  assignContractRole: (contractId, data) => post(`/srm/contracts/${contractId}/assignments`, data),
  removeContractAssignment: (contractId, assignmentId) => del(`/srm/contracts/${contractId}/assignments/${assignmentId}`),

  // Contract documents & clause extraction review.
  getContractDocuments: (contractId) => fetchAPI(`/srm/contracts/${contractId}/documents`),
  addContractDocument: (contractId, data) => post(`/srm/contracts/${contractId}/documents`, data),
  deleteContractDocument: (id) => del(`/srm/documents/${id}`),
  getExtractions: (documentId) => fetchAPI(`/srm/documents/${documentId}/extractions`),
  reviewClauseExtraction: (id, data) => put(`/srm/extractions/${id}`, data),

  createManagementPlan: (contractId, data) => post(`/srm/contracts/${contractId}/management-plans`, data),

  // Cross-module P3M view: projects with contract aggregates and the entity
  // option list for linking contracts to portfolios/programmes/projects.
  getProjects: () => fetchAPI('/srm/projects'),
  getP3MOptions: () => fetchAPI('/srm/p3m-options'),

  // Phase 1: categories, segmentation runs and validation.
  categories: createCrudClient('/srm/categories'),
  getSegmentationRuns: (filters = {}) => fetchAPI(`/srm/segmentation/runs${qs(filters)}`),
  createSegmentationRun: (data) => post('/srm/segmentation/runs', data),
  getSegmentationRun: (id) => fetchAPI(`/srm/segmentation/runs/${id}`),
  upsertSegmentationAnswer: (runId, data) => put(`/srm/segmentation/runs/${runId}/answers`, data),
  completeSegmentationRun: (runId, data = {}) => post(`/srm/segmentation/runs/${runId}/complete`, data),

  getValidationRequests: (filters = {}) => fetchAPI(`/srm/validation${qs(filters)}`),
  createValidationRequest: (data) => post('/srm/validation', data),
  getValidationRequest: (id) => fetchAPI(`/srm/validation/${id}`),
  decideValidationRequest: (id, data) => put(`/srm/validation/${id}/decision`, data),

  // Phase 2/3: value propositions, stakeholders, communications, RACI.
  valuePropositions: createCrudClient('/srm/value-propositions'),
  stakeholders: createCrudClient('/srm/stakeholders'),
  entityStakeholders: createCrudClient('/srm/entity-stakeholders'),

  getCommsPlan: (supplierId) => fetchAPI(`/srm/suppliers/${supplierId}/comms-plan`),
  upsertCommsPlan: (supplierId, data) => put(`/srm/suppliers/${supplierId}/comms-plan`, data),
  getCommsMessages: (planId) => fetchAPI(`/srm/comms-plans/${planId}/messages`),
  addCommsMessage: (planId, data) => post(`/srm/comms-plans/${planId}/messages`, data),
  updateCommsMessage: (id, data) => put(`/srm/comms-messages/${id}`, data),
  deleteCommsMessage: (id) => del(`/srm/comms-messages/${id}`),

  getRaciMatrix: (supplierId) => fetchAPI(`/srm/suppliers/${supplierId}/raci`),
  createRaciVersion: (supplierId, data = {}) => post(`/srm/suppliers/${supplierId}/raci`, data),
  updateRaciStatus: (id, data) => put(`/srm/racis/${id}`, data),
  upsertRaciAssignment: (id, data) => put(`/srm/racis/${id}/assignments`, data),
  deleteRaciAssignment: (id) => del(`/srm/raci-assignments/${id}`),

  // Meetings.
  meetings: createCrudClient('/srm/meetings'),
  addMeetingAttendee: (meetingId, data) => post(`/srm/meetings/${meetingId}/attendees`, data),
  removeMeetingAttendee: (id) => del(`/srm/meeting-attendees/${id}`),
  addMeetingAction: (meetingId, data) => post(`/srm/meetings/${meetingId}/actions`, data),
  updateMeetingAction: (id, data) => put(`/srm/meeting-actions/${id}`, data),

  // Joint business plans.
  jbps: createCrudClient('/srm/jbps'),
  createJbp: (supplierId, data) => post(`/srm/suppliers/${supplierId}/jbps`, data),

  // Phase 4/5: performance metrics, scorecards, pulse surveys.
  metrics: createCrudClient('/srm/metrics'),
  getMetricResults: (metricId) => fetchAPI(`/srm/metrics/${metricId}/results`),
  recordMetricResult: (metricId, data) => post(`/srm/metrics/${metricId}/results`, data),

  scorecards: createCrudClient('/srm/scorecards'),
  upsertScorecardDimension: (id, data) => put(`/srm/scorecards/${id}/dimensions`, data),

  pulseSurveys: createCrudClient('/srm/pulse-surveys'),
  recordSurveyResponse: (surveyId, data) => post(`/srm/pulse-surveys/${surveyId}/responses`, data),

  // Innovation register.
  innovations: createCrudClient('/srm/innovations'),
  upsertInnovationFactor: (id, data) => put(`/srm/innovations/${id}/factors`, data),
  getInnovationRecommendation: (id) => fetchAPI(`/srm/innovations/${id}/recommendation`),
  createInnovationRecommendation: (id, data) => put(`/srm/innovations/${id}/recommendation`, data),
  addInnovationObjective: (id, data) => post(`/srm/innovations/${id}/objectives`, data),
  updateInnovationObjective: (id, data) => put(`/srm/innovation-objectives/${id}`, data),
  deleteInnovationObjective: (id) => del(`/srm/innovation-objectives/${id}`),
  addInnovationTask: (id, data) => post(`/srm/innovations/${id}/tasks`, data),
  updateInnovationTask: (id, data) => put(`/srm/innovation-tasks/${id}`, data),

  // Service improvement plans and disputes.
  sips: createCrudClient('/srm/sips'),
  addSipDeliverable: (id, data) => post(`/srm/sips/${id}/deliverables`, data),
  updateSipDeliverable: (id, data) => put(`/srm/sip-deliverables/${id}`, data),

  disputes: createCrudClient('/srm/disputes'),
  addDisputeEscalation: (id, data) => post(`/srm/disputes/${id}/escalations`, data),
  upsertRemedyPlan: (id, data) => put(`/srm/disputes/${id}/remedy-plan`, data),
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
