import { t } from '../../stores/i18n.svelte.js';

/** Relationship states in gate order (mirrors the DDL CHECK constraint). */
export const SRM_STATES = [
  'registered',
  'segmented',
  'planning_ready',
  'implementation_ready',
  'kicked_off',
  'jbp_formalised',
  'monitoring_active',
  'closed',
];

/** Localized relationship-state label. */
export function stateLabel(state) {
  switch (state) {
    case 'registered':
      return t('srm.stateRegistered');
    case 'segmented':
      return t('srm.stateSegmented');
    case 'planning_ready':
      return t('srm.statePlanningReady');
    case 'implementation_ready':
      return t('srm.stateImplementationReady');
    case 'kicked_off':
      return t('srm.stateKickedOff');
    case 'jbp_formalised':
      return t('srm.stateJbpFormalised');
    case 'monitoring_active':
      return t('srm.stateMonitoringActive');
    case 'closed':
      return t('srm.stateClosed');
    default:
      return state || '';
  }
}

/** Format a contract value as a compact number (no currency symbol). */
export function formatMoney(value) {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return '—';
  const n = Number(value);
  if (Math.abs(n) >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (Math.abs(n) >= 10_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toLocaleString(undefined, { maximumFractionDigits: 2 });
}

/** Format an ISO timestamp to a short date string; empty for null. */
export function formatDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
