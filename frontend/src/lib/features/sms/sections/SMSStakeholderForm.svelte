<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { getUsers } from '../../../api/users.js';
  import {
    CLASSIFICATIONS,
    ENGAGEMENT_STATUSES,
    LIFECYCLE_STAGES,
    RELATIONSHIP_STATUSES,
    TIERS,
    TRUST_LEVELS,
    RISK_RATINGS,
    REVIEW_CADENCES,
    INFO_CLASSIFICATIONS,
    CONSENT_STATUSES,
    STAKEHOLDER_TYPES,
    classificationLabel,
    stakeholderTypeLabel,
    engagementLabel,
    lifecycleLabel,
    relationshipLabel,
    tierLabel,
    trustLabel,
    riskRatingLabel,
    cadenceLabel,
  } from '../utils.js';

  /**
   * Full-profile create form for a new register entry. Mirrors the field set of
   * the stakeholder profile editor; only name is required — every other field
   * falls back to its server-side default when left empty.
   */
  let { onCreated = () => {}, onCancel = () => {} } = $props();

  const LEVELS = [1, 2, 3, 4, 5];

  let form = $state({
    name: '',
    title: '',
    organization: '',
    email: '',
    phone: '',
    role_classification: 'support',
    tier: 'tier_3',
    stakeholder_type: '',
    category: '',
    business_unit: '',
    influence_level: 3,
    interest_level: 3,
    interests: '',
    engagement_status: 'passive',
    lifecycle_stage: 'onboarding',
    relationship_status: 'neutral',
    trust_level: '',
    risk_rating: '',
    communication_preferences: '',
    owner_user_id: null,
    backup_owner_user_id: null,
    executive_sponsor_user_id: null,
    next_engagement_at: '',
    info_classification: 'internal',
    consent_status: 'not_required',
    retention_category: '',
    review_cadence: 'quarterly',
    location_text: '',
    notes: '',
  });

  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);
  let saving = $state(false);
  let error = $state('');

  onMount(() => {
    getUsers().then((u) => (users = u)).catch(() => {});
  });

  function userName(id) {
    if (!id) return '';
    const u = users.find((x) => x.id === id);
    if (!u) return `#${id}`;
    return [u.first_name, u.last_name].filter(Boolean).join(' ') || u.username || `#${id}`;
  }

  /** Bare YYYY-MM-DD → RFC3339 midnight UTC (the API decodes time.Time strictly). */
  function toApiDate(v) {
    if (!v) return undefined;
    return /^\d{4}-\d{2}-\d{2}$/.test(v) ? `${v}T00:00:00Z` : v;
  }

  async function submit() {
    if (saving) return;
    error = '';
    if (!form.name.trim()) {
      error = t('sms.nameRequired');
      return;
    }
    saving = true;
    try {
      const payload = { ...form, name: form.name.trim() };
      for (const k of ['next_engagement_at']) {
        const v = toApiDate(payload[k]);
        if (v === undefined) delete payload[k];
        else payload[k] = v;
      }
      for (const k of ['owner_user_id', 'backup_owner_user_id', 'executive_sponsor_user_id']) {
        if (!payload[k]) delete payload[k];
      }
      // Nullable API fields: omit when unset so the server stores NULL, not "".
      for (const k of ['stakeholder_type', 'risk_rating']) {
        if (!String(payload[k] ?? '').trim()) delete payload[k];
      }
      const created = await smsApi.createStakeholder(payload);
      onCreated(created ?? {});
    } catch (e) {
      error = e?.message || t('sms.loadErrorTitle');
    } finally {
      saving = false;
    }
  }
</script>

<section class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
  <div class="mb-2 flex items-center gap-2">
    <h2 class="text-sm font-semibold">{t('sms.newStakeholder')}</h2>
    <button type="button" class="ml-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={onCancel}>
      {t('common.cancel')}
    </button>
  </div>

  <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-2">
    <label class="text-xs"><span>{t('sms.name')} *</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.name} oninput={(e) => (form = { ...form, name: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.jobTitle')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.title} oninput={(e) => (form = { ...form, title: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.organization')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.organization} oninput={(e) => (form = { ...form, organization: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.email')}</span><input type="email" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.email} oninput={(e) => (form = { ...form, email: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.phone')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.phone} oninput={(e) => (form = { ...form, phone: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.classification')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.role_classification} onchange={(e) => (form = { ...form, role_classification: e.target.value })}>{#each CLASSIFICATIONS as c (c)}<option value={c}>{classificationLabel(c)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.tier')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.tier} onchange={(e) => (form = { ...form, tier: e.target.value })}>{#each TIERS as tr (tr)}<option value={tr}>{tierLabel(tr)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.stakeholderType')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.stakeholder_type} onchange={(e) => (form = { ...form, stakeholder_type: e.target.value })}><option value="">{t('sms.noType')}</option>{#each STAKEHOLDER_TYPES as st (st)}<option value={st}>{stakeholderTypeLabel(st)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.categoryField')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.category} oninput={(e) => (form = { ...form, category: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.businessUnit')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.business_unit} oninput={(e) => (form = { ...form, business_unit: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.influence')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.influence_level} onchange={(e) => (form = { ...form, influence_level: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{l}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.interest')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.interest_level} onchange={(e) => (form = { ...form, interest_level: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{l}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.interests')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.interests} oninput={(e) => (form = { ...form, interests: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.engagement')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.engagement_status} onchange={(e) => (form = { ...form, engagement_status: e.target.value })}>{#each ENGAGEMENT_STATUSES as s (s)}<option value={s}>{engagementLabel(s)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.lifecycle')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.lifecycle_stage} onchange={(e) => (form = { ...form, lifecycle_stage: e.target.value })}>{#each LIFECYCLE_STAGES as s (s)}<option value={s}>{lifecycleLabel(s)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.relationship')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.relationship_status} onchange={(e) => (form = { ...form, relationship_status: e.target.value })}>{#each RELATIONSHIP_STATUSES as s (s)}<option value={s}>{relationshipLabel(s)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.trustLevel')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.trust_level} onchange={(e) => (form = { ...form, trust_level: e.target.value })}><option value="">{t('sms.noTrust')}</option>{#each TRUST_LEVELS as tl (tl)}<option value={tl}>{trustLabel(tl)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.riskRating')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.risk_rating} onchange={(e) => (form = { ...form, risk_rating: e.target.value })}><option value="">{t('sms.noRisk')}</option>{#each RISK_RATINGS as rr (rr)}<option value={rr}>{riskRatingLabel(rr)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.commPrefs')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.communication_preferences} oninput={(e) => (form = { ...form, communication_preferences: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.owner')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.owner_user_id ?? ''} onchange={(e) => (form = { ...form, owner_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.noOwner')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.backupOwner')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.backup_owner_user_id ?? ''} onchange={(e) => (form = { ...form, backup_owner_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.noOwner')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.executiveSponsor')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.executive_sponsor_user_id ?? ''} onchange={(e) => (form = { ...form, executive_sponsor_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.noOwner')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.nextEngagement')}</span><input type="date" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.next_engagement_at} oninput={(e) => (form = { ...form, next_engagement_at: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.infoClassification')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.info_classification} onchange={(e) => (form = { ...form, info_classification: e.target.value })}>{#each INFO_CLASSIFICATIONS as ic (ic)}<option value={ic}>{t(`sms.ic${ic === 'internal' ? 'Internal' : ic.charAt(0).toUpperCase() + ic.slice(1)}`)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.consentStatus')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.consent_status} onchange={(e) => (form = { ...form, consent_status: e.target.value })}>{#each CONSENT_STATUSES as cs (cs)}<option value={cs}>{t(`sms.cs${cs === 'not_required' ? 'NotRequired' : cs.charAt(0).toUpperCase() + cs.slice(1)}`)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.retentionCategory')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.retention_category} oninput={(e) => (form = { ...form, retention_category: e.target.value })} /></label>
    <label class="text-xs"><span>{t('sms.reviewCadence')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.review_cadence} onchange={(e) => (form = { ...form, review_cadence: e.target.value })}>{#each REVIEW_CADENCES as rc (rc)}<option value={rc}>{cadenceLabel(rc)}</option>{/each}</select></label>
    <label class="text-xs"><span>{t('sms.locationText')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.location_text} oninput={(e) => (form = { ...form, location_text: e.target.value })} /></label>
  </div>

  <label class="mt-3 block text-xs"><span>{t('sms.notes')}</span><textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.notes} oninput={(e) => (form = { ...form, notes: e.target.value })}></textarea></label>

  <div class="mt-3 flex items-center gap-2">
    <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90" disabled={saving || !form.name.trim()} onclick={submit}>
      {t('sms.createStakeholder')}
    </button>
    {#if error}<span class="text-xs text-red-600">{error}</span>{/if}
  </div>
</section>
