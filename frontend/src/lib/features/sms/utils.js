import { t } from '../../stores/i18n.svelte.js';

/** Enum value lists (mirror the DDL CHECK constraints). */
export const CLASSIFICATIONS = ['key', 'decision_maker', 'support', 'influencer', 'observer'];
export const ENGAGEMENT_STATUSES = ['passive', 'engaged', 'active', 'disengaged'];
export const LIFECYCLE_STAGES = ['onboarding', 'active', 'retained', 'exited'];
// HCC §13 relationship scale.
export const RELATIONSHIP_STATUSES = ['advocate', 'supportive', 'neutral', 'concerned', 'opposed'];
export const ENTITY_TYPES = ['portfolio', 'programme', 'project'];
// /sms/p3m-options response keys (plural list names) per entity type.
export const P3M_OPTIONS_KEYS = { portfolio: 'portfolios', programme: 'programmes', project: 'projects' };
export const TIERS = ['tier_1', 'tier_2', 'tier_3', 'tier_4'];
export const TRUST_LEVELS = ['very_high', 'high', 'moderate', 'low', 'critical'];
export const RISK_RATINGS = ['low', 'medium', 'high', 'critical'];
export const REVIEW_CADENCES = ['monthly', 'quarterly', 'semi_annual', 'annual'];
export const INFO_CLASSIFICATIONS = ['public', 'internal', 'confidential', 'restricted'];
export const CONSENT_STATUSES = ['not_required', 'granted', 'declined', 'withdrawn', 'unknown'];
// IAP2 engagement spectrum (Std §14).
export const APPROACHES = ['inform', 'consult', 'involve', 'collaborate', 'partner'];
export const ISSUE_CATEGORIES = ['relationship', 'reputation', 'delivery', 'communication', 'dependency', 'knowledge_loss', 'commitment', 'other'];
export const COMMITMENT_DIRECTIONS = ['from_us', 'from_them', 'mutual'];
export const COMMITMENT_STATUSES = ['open', 'in_progress', 'at_risk', 'completed', 'cancelled'];
export const GRIEVANCE_SEVERITIES = ['low', 'medium', 'high', 'critical'];
export const GRIEVANCE_STATUSES = ['received', 'acknowledged', 'investigating', 'responded', 'closed'];
// HCC stakeholder type taxonomy (Std §8).
export const STAKEHOLDER_TYPES = [
  'mana_whenua', 'elected_member', 'government_agency', 'regional_authority', 'regulatory_partner',
  'infrastructure_partner', 'business', 'developer', 'business_association', 'strategic_supplier',
  'utility_provider', 'community_organisation', 'industry_group', 'advocacy_group',
  'educational_institution', 'major_landowner', 'project_stakeholder', 'other',
];

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
    case 'advocate': return t('sms.relAdvocate');
    case 'supportive': return t('sms.relSupportive');
    case 'neutral': return t('sms.relNeutral');
    case 'concerned': return t('sms.relConcerned');
    case 'opposed': return t('sms.relOpposed');
    default: return value || '';
  }
}

export function tierLabel(value) {
  switch (value) {
    case 'tier_1': return t('sms.tier1');
    case 'tier_2': return t('sms.tier2');
    case 'tier_3': return t('sms.tier3');
    case 'tier_4': return t('sms.tier4');
    default: return value || '';
  }
}

export function trustLabel(value) {
  switch (value) {
    case 'very_high': return t('sms.trustVeryHigh');
    case 'high': return t('sms.trustHigh');
    case 'moderate': return t('sms.trustModerate');
    case 'low': return t('sms.trustLow');
    case 'critical': return t('sms.trustCritical');
    default: return value || '';
  }
}

export function riskRatingLabel(value) {
  switch (value) {
    case 'low': return t('sms.riskLow');
    case 'medium': return t('sms.riskMedium');
    case 'high': return t('sms.riskHigh');
    case 'critical': return t('sms.riskCritical');
    default: return value || '';
  }
}

export function cadenceLabel(value) {
  switch (value) {
    case 'monthly': return t('sms.cadMonthly');
    case 'quarterly': return t('sms.cadQuarterly');
    case 'semi_annual': return t('sms.cadSemiAnnual');
    case 'annual': return t('sms.cadAnnual');
    default: return value || '';
  }
}

export function approachLabel(value) {
  switch (value) {
    case 'inform': return t('sms.approachInform');
    case 'consult': return t('sms.approachConsult');
    case 'involve': return t('sms.approachInvolve');
    case 'collaborate': return t('sms.approachCollaborate');
    case 'partner': return t('sms.approachPartner');
    default: return value || '';
  }
}

export function issueCategoryLabel(value) {
  switch (value) {
    case 'relationship': return t('sms.catRelationship');
    case 'reputation': return t('sms.catReputation');
    case 'delivery': return t('sms.catDelivery');
    case 'communication': return t('sms.catCommunication');
    case 'dependency': return t('sms.catDependency');
    case 'knowledge_loss': return t('sms.catKnowledgeLoss');
    case 'commitment': return t('sms.catCommitment');
    case 'other': return t('sms.catOther');
    default: return value || '';
  }
}

export function commitmentDirectionLabel(value) {
  switch (value) {
    case 'from_us': return t('sms.dirFromUs');
    case 'from_them': return t('sms.dirFromThem');
    case 'mutual': return t('sms.dirMutual');
    default: return value || '';
  }
}

export function grievanceStatusLabel(value) {
  switch (value) {
    case 'received': return t('sms.grvReceived');
    case 'acknowledged': return t('sms.grvAcknowledged');
    case 'investigating': return t('sms.grvInvestigating');
    case 'responded': return t('sms.grvResponded');
    case 'closed': return t('sms.stClosed');
    default: return value || '';
  }
}

export function commitmentStatusLabel(value) {
  switch (value) {
    case 'open': return t('sms.stOpen');
    case 'in_progress': return t('sms.stInProgress');
    case 'at_risk': return t('sms.gAtRisk');
    case 'completed': return t('sms.stCompleted');
    case 'cancelled': return t('sms.stCancelled');
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
    case 'advocate': return 'bg-emerald-100 text-emerald-700';
    case 'supportive': return 'bg-sky-100 text-sky-700';
    case 'neutral': return 'bg-slate-100 text-slate-600';
    case 'concerned': return 'bg-amber-100 text-amber-700';
    case 'opposed': return 'bg-red-100 text-red-700';
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
