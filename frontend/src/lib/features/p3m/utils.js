import { t } from '../../stores/i18n.svelte.js';

/** Format a budget amount as a compact number (no currency symbol: amounts are unitless). */
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

/** CSS color for a RAG status value. */
export function ragColor(status) {
  switch (status) {
    case 'red':
      return '#ef4444';
    case 'amber':
      return '#f59e0b';
    default:
      return '#10b981';
  }
}

/** Localized RAG label. */
export function ragLabel(status) {
  switch (status) {
    case 'red':
      return t('p3m.ragRed');
    case 'amber':
      return t('p3m.ragAmber');
    default:
      return t('p3m.ragGreen');
  }
}

/** Localized project stage label. */
export function stageLabel(stage) {
  switch (stage) {
    case 'initiation':
      return t('p3m.stageInitiation');
    case 'planning':
      return t('p3m.stagePlanning');
    case 'execution':
      return t('p3m.stageExecution');
    case 'closure':
      return t('p3m.stageClosure');
    default:
      return stage || '';
  }
}

/** Localized RAID status label. */
export function raidStatusLabel(status) {
  switch (status) {
    case 'open':
      return t('p3m.statusOpen');
    case 'mitigating':
      return t('p3m.statusMitigating');
    case 'closed':
      return t('p3m.statusClosed');
    case 'escalated':
      return t('p3m.statusEscalated');
    default:
      return status || '';
  }
}

/** Localized stakeholder quadrant label. */
export function quadrantLabel(quadrant) {
  switch (quadrant) {
    case 'manage_closely':
      return t('p3m.quadrantManageClosely');
    case 'keep_satisfied':
      return t('p3m.quadrantKeepSatisfied');
    case 'monitor':
      return t('p3m.quadrantMonitor');
    case 'keep_informed':
      return t('p3m.quadrantKeepInformed');
    default:
      return quadrant || '';
  }
}
