import { t } from '../../stores/i18n.svelte.js';

/** Enum value lists (mirror the DDL CHECK constraints). */
export const CLASSIFICATIONS = ['key', 'decision_maker', 'support', 'influencer', 'observer'];
export const ENGAGEMENT_STATUSES = ['passive', 'engaged', 'active', 'disengaged'];
export const LIFECYCLE_STAGES = ['onboarding', 'active', 'retained', 'exited'];
export const RELATIONSHIP_STATUSES = ['positive', 'neutral', 'tense', 'conflict'];
export const ENTITY_TYPES = ['portfolio', 'programme', 'project'];

/** Localized enum labels. */
export function classificationLabel(value) {
  switch (value) {
    case 'key': return t('sms.classKey');
    case 'decision_maker': return t('sms.classDecisionMaker');
    case 'support': return t('sms.classSupport');
    case 'influencer': return t('sms.classInfluencer');
    case 'observer': return t('sms.classObserver');
    default: return value || '';
  }
}

export function engagementLabel(value) {
  switch (value) {
    case 'passive': return t('sms.engPassive');
    case 'engaged': return t('sms.engEngaged');
    case 'active': return t('sms.engActive');
    case 'disengaged': return t('sms.engDisengaged');
    default: return value || '';
  }
}

export function lifecycleLabel(value) {
  switch (value) {
    case 'onboarding': return t('sms.lifeOnboarding');
    case 'active': return t('sms.lifeActive');
    case 'retained': return t('sms.lifeRetained');
    case 'exited': return t('sms.lifeExited');
    default: return value || '';
  }
}

export function relationshipLabel(value) {
  switch (value) {
    case 'positive': return t('sms.relPositive');
    case 'neutral': return t('sms.relNeutral');
    case 'tense': return t('sms.relTense');
    case 'conflict': return t('sms.relConflict');
    default: return value || '';
  }
}

export function entityTypeLabel(value) {
  switch (value) {
    case 'portfolio': return t('sms.typePortfolio');
    case 'programme': return t('sms.typeProgramme');
    case 'project': return t('sms.typeProject');
    default: return value || '';
  }
}

/** Chip color classes per enum family. */
export function relationshipClass(value) {
  switch (value) {
    case 'positive': return 'bg-emerald-100 text-emerald-700';
    case 'neutral': return 'bg-slate-100 text-slate-600';
    case 'tense': return 'bg-amber-100 text-amber-700';
    case 'conflict': return 'bg-red-100 text-red-700';
    default: return 'bg-slate-100 text-slate-600';
  }
}

export function engagementClass(value) {
  switch (value) {
    case 'active': return 'bg-emerald-100 text-emerald-700';
    case 'engaged': return 'bg-sky-100 text-sky-700';
    case 'passive': return 'bg-slate-100 text-slate-600';
    case 'disengaged': return 'bg-red-100 text-red-700';
    default: return 'bg-slate-100 text-slate-600';
  }
}

export function priorityClass(value) {
  switch (value) {
    case 'critical': return 'bg-red-100 text-red-700';
    case 'high': return 'bg-orange-100 text-orange-700';
    case 'medium': return 'bg-amber-100 text-amber-700';
    default: return 'bg-slate-100 text-slate-600';
  }
}

/** Health score badge class (0-100). */
export function healthClass(score) {
  if (score >= 76) return 'bg-emerald-100 text-emerald-700';
  if (score >= 51) return 'bg-sky-100 text-sky-700';
  if (score >= 26) return 'bg-amber-100 text-amber-700';
  return 'bg-red-100 text-red-700';
}

/** Format an ISO timestamp to a short date string; empty for null. */
export function formatDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
