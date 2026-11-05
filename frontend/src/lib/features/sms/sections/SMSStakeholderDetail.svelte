<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { getUsers } from '../../../api/users.js';
  import { fetchAPI } from '../../../api/core.js';
  import {
    CLASSIFICATIONS,
    ENGAGEMENT_STATUSES,
    LIFECYCLE_STAGES,
    RELATIONSHIP_STATUSES,
    ENTITY_TYPES,
    TIERS,
    TRUST_LEVELS,
    RISK_RATINGS,
    REVIEW_CADENCES,
    INFO_CLASSIFICATIONS,
    CONSENT_STATUSES,
    APPROACHES,
    ISSUE_CATEGORIES,
    COMMITMENT_DIRECTIONS,
    COMMITMENT_STATUSES,
    GRIEVANCE_SEVERITIES,
    GRIEVANCE_STATUSES,
    STAKEHOLDER_TYPES,
    classificationLabel,
    stakeholderTypeLabel,
    engagementLabel,
    lifecycleLabel,
    relationshipLabel,
    entityTypeLabel,
    tierLabel,
    trustLabel,
    riskRatingLabel,
    cadenceLabel,
    approachLabel,
    issueCategoryLabel,
    commitmentDirectionLabel,
    grievanceStatusLabel,
    commitmentStatusLabel,
    relationshipClass,
    engagementClass,
    priorityClass,
    healthClass,
    formatDate,
    P3M_OPTIONS_KEYS,
  } from '../utils.js';

  let { stakeholderId, onBack = () => {}, onChanged } = $props();

  const PRIORITIES = ['low', 'medium', 'high', 'critical'];
  const ISSUE_STATUSES = ['open', 'in_progress', 'escalated', 'resolved', 'closed'];
  const TASK_STATUSES = ['open', 'in_progress', 'done', 'cancelled'];
  const FEEDBACK_SOURCES = ['form', 'survey', 'email', 'meeting', 'other'];
  const GOAL_STATUSES = ['on_track', 'at_risk', 'missed', 'achieved'];
  const CHANNELS = ['email', 'phone', 'meeting', 'in_person', 'slack', 'teams', 'other'];
  const DIRECTIONS = ['outbound', 'inbound'];
  const FREQUENCIES = ['daily', 'weekly', 'biweekly', 'monthly', 'quarterly', 'annual', 'as_needed'];
  const PLAN_STATUSES = ['draft', 'active', 'paused', 'completed'];
  const SENTIMENTS = ['', 'positive', 'neutral', 'negative'];
  const LEVELS = [1, 2, 3, 4, 5];

  const SECTIONS = [
    { id: 'profile', labelKey: 'sms.profile' },
    { id: 'communications', labelKey: 'sms.communications' },
    { id: 'plans', labelKey: 'sms.engagementPlans' },
    { id: 'issues', labelKey: 'sms.issuesRisks' },
    { id: 'commitments', labelKey: 'sms.commitments' },
    { id: 'grievances', labelKey: 'sms.grievances' },
    { id: 'decisions', labelKey: 'sms.decisions' },
    { id: 'tasks', labelKey: 'sms.tasks' },
    { id: 'feedback', labelKey: 'sms.feedback' },
    { id: 'goals', labelKey: 'sms.goals' },
    { id: 'network', labelKey: 'sms.network' },
    { id: 'transfers', labelKey: 'sms.transfers' },
    { id: 'timeline', labelKey: 'sms.timeline' },
  ];

  let activeSection = $state('profile');
  let loading = $state(true);
  let error = $state('');
  /** @type {Record<string, any> | null} */
  let detail = $state(null);
  let users = $state([]);
  let p3mOptions = $state({ portfolios: [], programmes: [], projects: [] });

  // Profile form (flat copy of profile fields).
  let form = $state({});
  /** @type {Array<{key: string, value: string}>} */
  let customFields = $state([]);
  let savingProfile = $state(false);
  let savedFlash = $state(false);

  // Add forms.
  let commForm = $state({ channel: 'email', direction: 'outbound', subject: '', body: '', sentiment: '' });
  let planForm = $state({ strategy: '', objective: '', approach: '', frequency: 'monthly', channel: 'meeting', status: 'draft', entity_type: '', entity_id: null, start_date: '', end_date: '' });
  let issueForm = $state({ kind: 'issue', category: 'other', title: '', description: '', priority: 'medium', impact: 3, likelihood: 3, escalation_path: '', mitigation: '' });
  let commitmentForm = $state({ direction: 'from_them', title: '', due_date: '', status: 'open' });
  let grievanceForm = $state({ title: '', category: '', severity: 'medium' });
  let decisionForm = $state({ title: '', decided_at: '' });
  let linkForm = $state({ other_stakeholder_id: null, relation_type: '' });
  let transferForm = $state({ to_user_id: null, reason: '', history_reviewed: true, risks_reviewed: true, commitments_reviewed: true });
  let taskForm = $state({ title: '', due_date: '', assigned_to_user_id: null, status: 'open' });
  let feedbackForm = $state({ source: 'meeting', category: '', rating: 3, body: '', resolution_status: 'open' });
  let goalForm = $state({ goal: '', metric: '', target_value: '', current_value: '', status: 'on_track' });
  let entityForm = $state({ entity_type: 'project', entity_id: null });

  const profile = $derived(detail ?? {});
  const entities = $derived(detail?.entities ?? []);
  const counts = $derived(detail?.counts ?? {});
  const sectionBadges = $derived({
    communications: counts.communications,
    plans: counts.active_plans,
    issues: counts.open_issues,
    commitments: counts.open_commitments,
    grievances: counts.open_grievances,
    decisions: counts.decisions,
    tasks: counts.open_tasks,
    feedback: counts.open_feedback,
  });

  function planEntityOptions() {
    if (!planForm.entity_type) return [];
    const list = p3mOptions[P3M_OPTIONS_KEYS[planForm.entity_type]] ?? [];
    return Array.isArray(list) ? list : [];
  }

  /** Display name for a P3M option entry. */
  function entityName(o) {
    return o?.title || o?.name || `#${o.id}`;
  }

  /** "Project: <name>" scope label for an engagement plan row. */
  function planEntityName(p) {
    if (!p.entity_type || !p.entity_id) return '—';
    const list = p3mOptions[P3M_OPTIONS_KEYS[p.entity_type]] ?? [];
    const o = (Array.isArray(list) ? list : []).find((x) => x.id === p.entity_id);
    return o ? `${entityTypeLabel(p.entity_type)}: ${o.title}` : `${entityTypeLabel(p.entity_type)} #${p.entity_id}`;
  }

  function userName(id) {
    if (!id) return '';
    const u = users.find((x) => x.id === id);
    if (!u) return `#${id}`;
    return [u.first_name, u.last_name].filter(Boolean).join(' ') || u.username || `#${id}`;
  }

  /** ISO timestamp → YYYY-MM-DD for date inputs; '' when empty. */
  function dateOnly(iso) {
    if (!iso) return '';
    const s = String(iso);
    return /^\d{4}-\d{2}-\d{2}/.test(s) ? s.slice(0, 10) : '';
  }

  /** Normalize form values for the PATCH payload. Nullable string fields are
   * always sent (empty string clears server-side); dates/coords omit = keep. */
  function profilePayload() {
    const p = { ...form };
    for (const k of ['external_id', 'stakeholder_type', 'trust_level', 'risk_rating']) {
      p[k] = String(p[k] ?? '').trim();
    }
    for (const k of ['next_engagement_at', 'last_reviewed_at']) {
      if (!p[k]) delete p[k];
    }
    for (const k of ['location_lat', 'location_lon']) {
      const v = String(p[k] ?? '').trim();
      if (v === '') delete p[k];
      else p[k] = Number(v);
    }
    return p;
  }

  function entityOptions() {
    const list = p3mOptions[P3M_OPTIONS_KEYS[entityForm.entity_type]] ?? [];
    return Array.isArray(list) ? list : [];
  }

  async function load() {
    loading = true;
    error = '';
    try {
      detail = (await smsApi.getStakeholder(stakeholderId)) ?? null;
      if (detail) {
        form = {
          name: detail.name ?? '',
          title: detail.title ?? '',
          organization: detail.organization ?? '',
          email: detail.email ?? '',
          phone: detail.phone ?? '',
          external_id: detail.external_id ?? '',
          stakeholder_type: detail.stakeholder_type ?? '',
          category: detail.category ?? '',
          tier: detail.tier ?? 'tier_3',
          business_unit: detail.business_unit ?? '',
          role_classification: detail.role_classification ?? 'support',
          influence_level: Number(detail.influence_level ?? 3),
          interest_level: Number(detail.interest_level ?? 3),
          interests: detail.interests ?? '',
          engagement_status: detail.engagement_status ?? 'passive',
          lifecycle_stage: detail.lifecycle_stage ?? 'onboarding',
          relationship_status: detail.relationship_status ?? 'neutral',
          trust_level: detail.trust_level ?? '',
          risk_rating: detail.risk_rating ?? '',
          communication_preferences: detail.communication_preferences ?? '',
          owner_user_id: detail.owner_user_id ?? null,
          backup_owner_user_id: detail.backup_owner_user_id ?? null,
          executive_sponsor_user_id: detail.executive_sponsor_user_id ?? null,
          next_engagement_at: dateOnly(detail.next_engagement_at),
          info_classification: detail.info_classification ?? 'internal',
          consent_status: detail.consent_status ?? 'not_required',
          retention_category: detail.retention_category ?? '',
          last_reviewed_at: dateOnly(detail.last_reviewed_at),
          review_cadence: detail.review_cadence ?? 'quarterly',
          location_text: detail.location_text ?? '',
          location_lat: detail.location_lat ?? '',
          location_lon: detail.location_lon ?? '',
          notes: detail.notes ?? '',
        };
        let parsed = {};
        try {
          parsed = JSON.parse(detail.custom_fields || '{}');
        } catch {
          parsed = {};
        }
        customFields = Object.entries(parsed).map(([key, value]) => ({ key, value: String(value) }));
      }
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function saveProfile() {
    if (savingProfile) return;
    savingProfile = true;
    savedFlash = false;
    try {
      const fieldsObj = {};
      for (const f of customFields) {
        if (f.key.trim()) fieldsObj[f.key.trim()] = f.value;
      }
      await smsApi.updateStakeholder(stakeholderId, { ...profilePayload(), custom_fields: JSON.stringify(fieldsObj) });
      savedFlash = true;
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingProfile = false;
    }
  }

  // --- Entity links ---
  async function addEntity() {
    if (!entityForm.entity_id) return;
    try {
      await smsApi.linkEntity(stakeholderId, { entity_type: entityForm.entity_type, entity_id: Number(entityForm.entity_id) });
      entityForm = { ...entityForm, entity_id: null };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeEntity(linkId) {
    try {
      await smsApi.unlinkEntity(linkId);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- Per-tab collection state (fetched lazily; detail GET only carries profile+entities+counts) ---
  let comms = $state([]);
  let plans = $state([]);
  let issues = $state([]);
  let commitmentsList = $state([]);
  let grievancesList = $state([]);
  let decisionsList = $state([]);
  let tasksList = $state([]);
  let feedbacks = $state([]);
  let goals = $state([]);
  let linksList = $state([]);
  let transfersList = $state([]);
  /** @type {Array<Record<string, any>>} */
  let allStakeholders = $state([]);
  const loadedTabs = $state(new Set());

  // Group memberships (managed from the profile section).
  /** @type {Array<Record<string, any>>} */
  let groups = $state([]);
  let addGroupId = $state('');
  const myGroups = $derived(groups.filter((g) => (g.members ?? []).some((m) => m.id === stakeholderId)));

  async function loadGroups() {
    try {
      groups = (await smsApi.getGroups()) ?? [];
    } catch {
      groups = [];
    }
  }

  async function addToGroup(groupId) {
    if (!groupId) return;
    try {
      await smsApi.addGroupMember(Number(groupId), stakeholderId);
      addGroupId = '';
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeFromGroup(groupId) {
    try {
      await smsApi.removeGroupMember(Number(groupId), stakeholderId);
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function loadSectionData(section) {
    try {
      switch (section) {
        case 'communications': comms = (await smsApi.getCommunications(stakeholderId)) ?? []; break;
        case 'plans': plans = (await smsApi.engagementPlans.getAll(stakeholderId)) ?? []; break;
        case 'issues': issues = (await smsApi.issues.getAll(stakeholderId)) ?? []; break;
        case 'commitments': commitmentsList = (await smsApi.commitments.getAll(stakeholderId)) ?? []; break;
        case 'grievances': grievancesList = (await smsApi.grievances.getAll(stakeholderId)) ?? []; break;
        case 'decisions': decisionsList = (await smsApi.decisions.getAll(stakeholderId)) ?? []; break;
        case 'tasks': tasksList = (await smsApi.tasks.getAll(stakeholderId)) ?? []; break;
        case 'feedback': feedbacks = (await smsApi.feedback.getAll(stakeholderId)) ?? []; break;
        case 'goals': goals = (await smsApi.goals.getAll(stakeholderId)) ?? []; break;
        case 'network': {
          linksList = (await smsApi.getStakeholderLinks(stakeholderId)) ?? [];
          if (allStakeholders.length === 0) allStakeholders = (await smsApi.getStakeholders()) ?? [];
          break;
        }
        case 'transfers': transfersList = (await smsApi.getTransfers(stakeholderId)) ?? []; break;
      }
      loadedTabs.add(section);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  $effect(() => {
    if (!detail) return;
    const s = activeSection;
    if ((s === 'communications' || s === 'plans' || s === 'issues' || s === 'commitments' || s === 'grievances' || s === 'decisions' || s === 'tasks' || s === 'feedback' || s === 'goals' || s === 'network' || s === 'transfers') && !loadedTabs.has(s)) {
      loadSectionData(s);
    }
  });

  async function refresh() {
    await load();
    if (loadedTabs.has(activeSection)) await loadSectionData(activeSection);
    onChanged?.();
  }
  async function addCommunication() {
    if (!commForm.subject.trim()) return;
    try {
      await smsApi.createCommunication(stakeholderId, { ...commForm });
      commForm = { channel: 'email', direction: 'outbound', subject: '', body: '', sentiment: '' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeCommunication(id) {
    try {
      await smsApi.deleteCommunication(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- Plans / issues / tasks / feedback / goals via CRUD clients ---
  async function addPlan() {
    if (!planForm.strategy.trim()) return;
    try {
      await smsApi.engagementPlans.create(stakeholderId, planPayload());
      planForm = { strategy: '', objective: '', approach: '', frequency: 'monthly', channel: 'meeting', status: 'draft', entity_type: '', entity_id: null, start_date: '', end_date: '' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  /** Omit unset optional plan fields so the server applies its defaults. */
  function planPayload() {
    const p = { ...planForm };
    if (!p.approach) delete p.approach;
    if (!p.entity_type) {
      delete p.entity_type;
      delete p.entity_id;
    } else if (!p.entity_id) {
      delete p.entity_id;
    }
    return p;
  }

  async function updatePlan(id, data) {
    try {
      await smsApi.engagementPlans.update(id, planPayload(data));
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removePlan(id) {
    try {
      await smsApi.engagementPlans.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addIssue() {
    if (!issueForm.title.trim()) return;
    try {
      await smsApi.issues.create(stakeholderId, issueForm);
      issueForm = { kind: 'issue', category: 'other', title: '', description: '', priority: 'medium', impact: 3, likelihood: 3, escalation_path: '', mitigation: '' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateIssue(id, data) {
    try {
      await smsApi.issues.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function escalateIssue(id) {
    try {
      await smsApi.escalateIssue(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeIssue(id) {
    try {
      await smsApi.issues.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- Commitments / grievances / decisions / links / transfers ---
  async function addCommitment() {
    if (!commitmentForm.title.trim()) return;
    try {
      const payload = { ...commitmentForm };
      if (!payload.due_date) delete payload.due_date;
      await smsApi.commitments.create(stakeholderId, payload);
      commitmentForm = { direction: 'from_them', title: '', due_date: '', status: 'open' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateCommitment(id, data) {
    try {
      await smsApi.commitments.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function escalateCommitment(id) {
    try {
      await smsApi.escalateCommitment(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeCommitment(id) {
    try {
      await smsApi.commitments.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addGrievance() {
    if (!grievanceForm.title.trim()) return;
    try {
      await smsApi.grievances.create(stakeholderId, grievanceForm);
      grievanceForm = { title: '', category: '', severity: 'medium' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateGrievance(id, data) {
    try {
      await smsApi.grievances.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function escalateGrievance(id) {
    try {
      await smsApi.escalateGrievance(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeGrievance(id) {
    try {
      await smsApi.grievances.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addDecision() {
    if (!decisionForm.title.trim()) return;
    try {
      const payload = { ...decisionForm };
      if (!payload.decided_at) delete payload.decided_at;
      await smsApi.decisions.create(stakeholderId, payload);
      decisionForm = { title: '', decided_at: '', decided_by_user_id: null };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeDecision(id) {
    try {
      await smsApi.decisions.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addLink() {
    if (!linkForm.other_stakeholder_id) return;
    try {
      await smsApi.createLink({ source_id: stakeholderId, target_id: Number(linkForm.other_stakeholder_id), relation_type: linkForm.relation_type });
      linkForm = { other_stakeholder_id: null, relation_type: '' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeLink(id) {
    try {
      await smsApi.deleteLink(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addTransfer() {
    if (!transferForm.to_user_id) return;
    try {
      await smsApi.createTransfer(stakeholderId, transferForm);
      transferForm = { to_user_id: null, reason: '', history_reviewed: true, risks_reviewed: true, commitments_reviewed: true };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  /** Resolve the other end of a network link to a display name. */
  function linkOtherName(link) {
    const otherId = link.source_id === stakeholderId ? link.target_id : link.source_id;
    const s = allStakeholders.find((x) => x.id === otherId);
    return s?.name || `#${otherId}`;
  }

  async function addTask() {
    if (!taskForm.title.trim()) return;
    try {
      await smsApi.tasks.create(stakeholderId, taskForm);
      taskForm = { title: '', due_date: '', assigned_to_user_id: null, status: 'open' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateTask(id, data) {
    try {
      await smsApi.tasks.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeTask(id) {
    try {
      await smsApi.tasks.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addFeedback() {
    try {
      await smsApi.feedback.create(stakeholderId, feedbackForm);
      feedbackForm = { source: 'meeting', category: '', rating: 3, body: '', resolution_status: 'open' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateFeedback(id, data) {
    try {
      await smsApi.feedback.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeFeedback(id) {
    try {
      await smsApi.feedback.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addGoal() {
    if (!goalForm.goal.trim()) return;
    try {
      const payload = { ...goalForm };
      payload.target_value = payload.target_value === '' ? null : Number(payload.target_value);
      payload.current_value = payload.current_value === '' ? null : Number(payload.current_value);
      await smsApi.goals.create(stakeholderId, payload);
      goalForm = { goal: '', metric: '', target_value: '', current_value: '', status: 'on_track' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updateGoal(id, data) {
    try {
      await smsApi.goals.update(id, data);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeGoal(id) {
    try {
      await smsApi.goals.delete(id);
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- Timeline ---
  let timeline = $state([]);
  async function loadTimeline() {
    try {
      timeline = (await smsApi.getTimeline(stakeholderId)) ?? [];
    } catch {
      timeline = [];
    }
  }

  onMount(async () => {
    await Promise.allSettled([load(), getUsers().then((u) => (users = u)).catch(() => {}), fetchAPI('/sms/p3m-options').then((o) => (p3mOptions = o ?? p3mOptions)).catch(() => {}), loadGroups()]);
  });

  $effect(() => {
    if (activeSection === 'timeline' && timeline.length === 0) loadTimeline();
  });
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
{:else if error && !detail}
  <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
{:else if detail}
  <!-- Header -->
  <div class="flex flex-wrap items-center gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-4 py-3">
    <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={onBack}>
      {t('sms.back')}
    </button>
    <span class="text-sm font-semibold">{profile.name}</span>
    {#if profile.organization}<span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">· {profile.organization}</span>{/if}
    <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs">{tierLabel(profile.tier)}</span>
    <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs">{classificationLabel(profile.role_classification)}</span>
    <span class="rounded-full px-2 py-0.5 text-xs {engagementClass(profile.engagement_status)}">{engagementLabel(profile.engagement_status)}</span>
    <span class="rounded-full px-2 py-0.5 text-xs {relationshipClass(profile.relationship_status)}">{relationshipLabel(profile.relationship_status)}</span>
    <span class="ml-auto rounded-full px-2 py-0.5 text-xs font-medium {healthClass(profile.health_score ?? 0)}" title={t('sms.healthScore')}>
      {profile.health_score ?? '–'}
    </span>
  </div>

  <!-- Sub-tabs -->
  <div class="flex flex-wrap gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] pt-3">
    {#each SECTIONS as sec (sec.id)}
      <button type="button" class="sms-subtab" class:active={activeSection === sec.id} onclick={() => (activeSection = sec.id)}>
        {t(sec.labelKey)}
        {#if sectionBadges[sec.id] !== undefined}<span class="ml-1 text-[var(--color-ds-text-muted,#6b7280)]">({sectionBadges[sec.id]})</span>{/if}
      </button>
    {/each}
  </div>

  <div class="space-y-4 py-4">
    {#if error}<div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>{/if}

    <!-- Profile -->
    {#if activeSection === 'profile'}
      <section>
        <h2 class="mb-2 text-sm font-semibold">{t('sms.profile')}</h2>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <label class="text-xs"><span>{t('sms.name')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.name} oninput={(e) => (form = { ...form, name: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.jobTitle')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.title} oninput={(e) => (form = { ...form, title: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.organization')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.organization} oninput={(e) => (form = { ...form, organization: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.email')}</span><input type="email" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.email} oninput={(e) => (form = { ...form, email: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.phone')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.phone} oninput={(e) => (form = { ...form, phone: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.classification')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.role_classification} onchange={(e) => (form = { ...form, role_classification: e.target.value })}>{#each CLASSIFICATIONS as c (c)}<option value={c}>{classificationLabel(c)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.influence')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.influence_level} onchange={(e) => (form = { ...form, influence_level: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{l}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.interest')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.interest_level} onchange={(e) => (form = { ...form, interest_level: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{l}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.interests')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.interests} oninput={(e) => (form = { ...form, interests: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.engagement')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.engagement_status} onchange={(e) => (form = { ...form, engagement_status: e.target.value })}>{#each ENGAGEMENT_STATUSES as s (s)}<option value={s}>{engagementLabel(s)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.lifecycle')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.lifecycle_stage} onchange={(e) => (form = { ...form, lifecycle_stage: e.target.value })}>{#each LIFECYCLE_STAGES as s (s)}<option value={s}>{lifecycleLabel(s)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.relationship')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.relationship_status} onchange={(e) => (form = { ...form, relationship_status: e.target.value })}>{#each RELATIONSHIP_STATUSES as s (s)}<option value={s}>{relationshipLabel(s)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.tier')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.tier} onchange={(e) => (form = { ...form, tier: e.target.value })}>{#each TIERS as tr (tr)}<option value={tr}>{tierLabel(tr)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.stakeholderType')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.stakeholder_type} onchange={(e) => (form = { ...form, stakeholder_type: e.target.value })}><option value="">{t('sms.noType')}</option>{#each STAKEHOLDER_TYPES as st (st)}<option value={st}>{stakeholderTypeLabel(st)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.categoryField')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.category} oninput={(e) => (form = { ...form, category: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.businessUnit')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.business_unit} oninput={(e) => (form = { ...form, business_unit: e.target.value })} /></label>
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
          <label class="text-xs"><span>{t('sms.lastReviewed')}</span><input type="date" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.last_reviewed_at} oninput={(e) => (form = { ...form, last_reviewed_at: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.reviewCadence')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.review_cadence} onchange={(e) => (form = { ...form, review_cadence: e.target.value })}>{#each REVIEW_CADENCES as rc (rc)}<option value={rc}>{cadenceLabel(rc)}</option>{/each}</select></label>
          <label class="text-xs"><span>{t('sms.locationText')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.location_text} oninput={(e) => (form = { ...form, location_text: e.target.value })} /></label>
          <div class="flex gap-3">
            <label class="text-xs w-24"><span>{t('sms.locationLat')}</span><input type="number" step="any" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.location_lat} oninput={(e) => (form = { ...form, location_lat: e.target.value })} /></label>
            <label class="text-xs w-24"><span>{t('sms.locationLon')}</span><input type="number" step="any" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.location_lon} oninput={(e) => (form = { ...form, location_lon: e.target.value })} /></label>
          </div>
          <div class="flex flex-wrap items-center gap-2 md:col-span-2">
            <span class="text-xs">{t('sms.groups')}:</span>
            {#each myGroups as g (g.id)}
              <span class="flex items-center gap-1 rounded-full bg-slate-100 px-2 py-0.5 text-xs">
                {g.name}
                <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('common.delete')} onclick={() => removeFromGroup(g.id)}>×</button>
              </span>
            {/each}
            <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={addGroupId} onchange={(e) => addToGroup(e.target.value)}>
              <option value="">+ {t('sms.groups')}</option>
              {#each groups as g (g.id)}{#if !myGroups.some((x) => x.id === g.id)}<option value={g.id}>{g.name}</option>{/if}{/each}
            </select>
          </div>
        </div>

        <!-- Custom fields -->
        <div class="mt-4">
          <h3 class="mb-1 text-xs font-semibold">{t('sms.customFields')}</h3>
          {#each customFields as field, i (i)}
            <div class="flex items-center gap-2 py-0.5">
              <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.fieldKey')} value={field.key} oninput={(e) => (customFields = customFields.map((f, j) => (j === i ? { ...f, key: e.target.value } : f)))} />
              <input class="flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.fieldValue')} value={field.value} oninput={(e) => (customFields = customFields.map((f, j) => (j === i ? { ...f, value: e.target.value } : f)))} />
              <button type="button" class="text-xs text-red-600 hover:underline" onclick={() => (customFields = customFields.filter((_, j) => j !== i))}>×</button>
            </div>
          {/each}
          <button type="button" class="mt-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (customFields = [...customFields, { key: '', value: '' }])}>
            + {t('sms.addField')}
          </button>
        </div>

        <label class="mt-4 block text-xs"><span>{t('sms.notes')}</span><textarea rows="3" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.notes} oninput={(e) => (form = { ...form, notes: e.target.value })}></textarea></label>

        <div class="mt-3 flex items-center gap-2">
          <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90" disabled={savingProfile} onclick={saveProfile}>
            {t('sms.save')}
          </button>
          {#if savedFlash}<span class="text-xs text-emerald-600">{t('sms.saved')}</span>{/if}
        </div>

        <!-- Linked P3M entities -->
        <div class="mt-5">
          <h3 class="mb-1 text-xs font-semibold">{t('sms.linkedEntities')}</h3>
          {#if entities.length === 0}
            <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noEntities')}</div>
          {:else}
            <ul class="space-y-1">
              {#each entities as ent (ent.id)}
                <li class="flex items-center gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs">
                  <span class="rounded-full bg-slate-100 px-2 py-0.5">{entityTypeLabel(ent.entity_type)}</span>
                  <span>{ent.entity_title || `#${ent.entity_id}`}</span>
                  <button type="button" class="ml-auto text-xs text-red-600 hover:underline" onclick={() => removeEntity(ent.id)}>{t('sms.unlink')}</button>
                </li>
              {/each}
            </ul>
          {/if}
          <div class="mt-2 flex items-center gap-2">
            <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={entityForm.entity_type} onchange={(e) => (entityForm = { ...entityForm, entity_type: e.target.value, entity_id: null })}>
              {#each ENTITY_TYPES as et (et)}<option value={et}>{entityTypeLabel(et)}</option>{/each}
            </select>
            <select class="flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={entityForm.entity_id ?? ''} onchange={(e) => (entityForm = { ...entityForm, entity_id: e.target.value ? Number(e.target.value) : null })}>
              <option value="">—</option>
              {#each entityOptions() as opt (opt.id)}<option value={opt.id}>{opt.title}</option>{/each}
            </select>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!entityForm.entity_id} onclick={addEntity}>
              {t('sms.linkEntity')}
            </button>
          </div>
        </div>
      </section>
    {/if}

    <!-- Communications -->
    {#if activeSection === 'communications'}
      <section>
        {#if comms.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.date')}</th><th class="py-1.5 pr-3">{t('sms.channel')}</th><th class="py-1.5 pr-3">{t('sms.direction')}</th><th class="py-1.5 pr-3">{t('sms.subject')}</th><th class="py-1.5 pr-3">{t('sms.sentiment')}</th><th></th></tr></thead>
            <tbody>
              {#each comms as c (c.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(c.created_at)}</td>
                  <td class="py-1.5 pr-3">{t(`sms.ch${c.channel === 'in_person' ? 'InPerson' : c.channel.charAt(0).toUpperCase() + c.channel.slice(1)}`)}</td>
                  <td class="py-1.5 pr-3">{c.direction === 'outbound' ? t('sms.dirOutbound') : t('sms.dirInbound')}</td>
                  <td class="py-1.5 pr-3"><div>{c.subject}</div>{#if c.body}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{c.body}</div>{/if}</td>
                  <td class="py-1.5 pr-3">{#if c.sentiment === 'positive'}<span class="rounded-full bg-emerald-100 px-2 py-0.5">+</span>{:else if c.sentiment === 'negative'}<span class="rounded-full bg-red-100 px-2 py-0.5">−</span>{:else}<span class="rounded-full bg-slate-100 px-2 py-0.5">±</span>{/if}</td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeCommunication(c.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={commForm.channel} onchange={(e) => (commForm = { ...commForm, channel: e.target.value })}>{#each CHANNELS as ch (ch)}<option value={ch}>{t(`sms.ch${ch === 'in_person' ? 'InPerson' : ch.charAt(0).toUpperCase() + ch.slice(1)}`)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={commForm.direction} onchange={(e) => (commForm = { ...commForm, direction: e.target.value })}>{#each DIRECTIONS as d (d)}<option value={d}>{t(d === 'outbound' ? 'sms.dirOutbound' : 'sms.dirInbound')}</option>{/each}</select>
          <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.subject')} value={commForm.subject} oninput={(e) => (commForm = { ...commForm, subject: e.target.value })} />
          <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.body')} value={commForm.body} oninput={(e) => (commForm = { ...commForm, body: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={commForm.sentiment} onchange={(e) => (commForm = { ...commForm, sentiment: e.target.value })}>{#each SENTIMENTS as s (s)}<option value={s}>{s === '' ? '—' : t(s === 'positive' ? 'sms.sentPositive' : s === 'negative' ? 'sms.sentNegative' : 'sms.sentNeutral')}</option>{/each}</select>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!commForm.subject.trim()} onclick={addCommunication}>{t('sms.addCommunication')}</button>
        </div>
      </section>
    {/if}

    <!-- Engagement plans -->
    {#if activeSection === 'plans'}
      <section>
        {#if plans.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.strategy')}</th><th class="py-1.5 pr-3">{t('sms.approach')}</th><th class="py-1.5 pr-3">{t('sms.scope')}</th><th class="py-1.5 pr-3">{t('sms.frequency')}</th><th class="py-1.5 pr-3">{t('sms.channel')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th class="py-1.5 pr-3">{t('sms.startDate')}</th><th class="py-1.5 pr-3">{t('sms.endDate')}</th><th></th></tr></thead>
            <tbody>
              {#each plans as p (p.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{p.strategy}{#if p.objective}<div class="text-[var(--color-ds-text-muted,#6b7280)]">{p.objective}</div>{/if}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{approachLabel(p.approach)}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{planEntityName(p)}</td>
                  <td class="py-1.5 pr-3">{t(`sms.freq${p.frequency === 'as_needed' ? 'AsNeeded' : p.frequency.charAt(0).toUpperCase() + p.frequency.slice(1)}`)}</td>
                  <td class="py-1.5 pr-3">{t(`sms.ch${p.channel === 'in_person' ? 'InPerson' : p.channel.charAt(0).toUpperCase() + p.channel.slice(1)}`)}</td>
                  <td class="py-1.5 pr-3"><select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={p.status} onchange={(e) => updatePlan(p.id, { status: e.target.value })}>{#each PLAN_STATUSES as s (s)}<option value={s}>{t(`sms.st${s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select></td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(p.start_date)}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(p.end_date)}</td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removePlan(p.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.strategy')} value={planForm.strategy} oninput={(e) => (planForm = { ...planForm, strategy: e.target.value })} />
          <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.objective')} value={planForm.objective} oninput={(e) => (planForm = { ...planForm, objective: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.approach} onchange={(e) => (planForm = { ...planForm, approach: e.target.value })}><option value="">{t('sms.noApproach')}</option>{#each APPROACHES as a (a)}<option value={a}>{approachLabel(a)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.entity_type} onchange={(e) => (planForm = { ...planForm, entity_type: e.target.value, entity_id: null })}><option value="">{t('sms.noEntity')}</option>{#each ENTITY_TYPES as et (et)}<option value={et}>{entityTypeLabel(et)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.entity_id ?? ''} disabled={!planForm.entity_type || planEntityOptions().length === 0} onchange={(e) => (planForm = { ...planForm, entity_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.noEntity')}</option>{#each planEntityOptions() as o (o.id)}<option value={o.id}>{entityName(o)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.frequency} onchange={(e) => (planForm = { ...planForm, frequency: e.target.value })}>{#each FREQUENCIES as f (f)}<option value={f}>{t(`sms.freq${f === 'as_needed' ? 'AsNeeded' : f.charAt(0).toUpperCase() + f.slice(1)}`)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.channel} onchange={(e) => (planForm = { ...planForm, channel: e.target.value })}>{#each CHANNELS as ch (ch)}<option value={ch}>{t(`sms.ch${ch === 'in_person' ? 'InPerson' : ch.charAt(0).toUpperCase() + ch.slice(1)}`)}</option>{/each}</select>
          <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.start_date} oninput={(e) => (planForm = { ...planForm, start_date: e.target.value })} />
          <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={planForm.end_date} oninput={(e) => (planForm = { ...planForm, end_date: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!planForm.strategy.trim()} onclick={addPlan}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Issues & risks -->
    {#if activeSection === 'issues'}
      <section>
        {#if issues.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.kind')}</th><th class="py-1.5 pr-3">{t('sms.categoryField')}</th><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.priority')}</th><th class="py-1.5 pr-3 text-center">{t('sms.impact')}/{t('sms.likelihood')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each issues as iss (iss.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5">{iss.kind === 'risk' ? t('sms.kindRisk') : t('sms.kindIssue')}</span></td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{issueCategoryLabel(iss.category)}</td>
                  <td class="py-1.5 pr-3"><div>{iss.title}</div>{#if iss.description}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{iss.description}</div>{/if}{#if iss.mitigation}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">→ {iss.mitigation}</div>{/if}</td>
                  <td class="py-1.5 pr-3"><span class="rounded-full px-2 py-0.5 {priorityClass(iss.priority)}">{t(`sms.pri${iss.priority.charAt(0).toUpperCase() + iss.priority.slice(1)}`)}</span></td>
                  <td class="py-1.5 pr-3 text-center">{iss.impact}/{iss.likelihood}</td>
                  <td class="py-1.5 pr-3">
                    <div class="flex items-center gap-1">
                      {#if iss.escalation_level > 0}<span class="text-xs text-amber-600" title={t('sms.escalatedBadge')}>↑{iss.escalation_level}</span>{/if}
                      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={iss.status} onchange={(e) => updateIssue(iss.id, { status: e.target.value })}>{#each ISSUE_STATUSES as s (s)}<option value={s}>{t(`sms.st${s === 'in_progress' ? 'InProgress' : s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select>
                      {#if iss.status === 'open' || iss.status === 'in_progress'}
                        <button type="button" class="text-xs text-amber-600 hover:underline" title={t('sms.escalate')} onclick={() => escalateIssue(iss.id)}>↑</button>
                      {/if}
                    </div>
                  </td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeIssue(iss.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.kind} onchange={(e) => (issueForm = { ...issueForm, kind: e.target.value })}><option value="issue">{t('sms.kindIssue')}</option><option value="risk">{t('sms.kindRisk')}</option></select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.category} onchange={(e) => (issueForm = { ...issueForm, category: e.target.value })}>{#each ISSUE_CATEGORIES as c (c)}<option value={c}>{issueCategoryLabel(c)}</option>{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.titleField')} value={issueForm.title} oninput={(e) => (issueForm = { ...issueForm, title: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.priority} onchange={(e) => (issueForm = { ...issueForm, priority: e.target.value })}>{#each PRIORITIES as p (p)}<option value={p}>{t(`sms.pri${p.charAt(0).toUpperCase() + p.slice(1)}`)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.impact} onchange={(e) => (issueForm = { ...issueForm, impact: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{t('sms.impact')}: {l}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.likelihood} onchange={(e) => (issueForm = { ...issueForm, likelihood: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{t('sms.likelihood')}: {l}</option>{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.escalationPath')} value={issueForm.escalation_path} oninput={(e) => (issueForm = { ...issueForm, escalation_path: e.target.value })} />
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.mitigation')} value={issueForm.mitigation} oninput={(e) => (issueForm = { ...issueForm, mitigation: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!issueForm.title.trim()} onclick={addIssue}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Tasks -->
    {#if activeSection === 'tasks'}
      <section>
        {#if tasksList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.assignee')}</th><th class="py-1.5 pr-3">{t('sms.dueDate')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each tasksList as task (task.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{task.title}</td>
                  <td class="py-1.5 pr-3">{userName(task.assigned_to_user_id) || '—'}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(task.due_date)}</td>
                  <td class="py-1.5 pr-3"><select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={task.status} onchange={(e) => updateTask(task.id, { status: e.target.value })}>{#each TASK_STATUSES as s (s)}<option value={s}>{t(`sms.st${s === 'in_progress' ? 'InProgress' : s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select></td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeTask(task.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.taskTitle')} value={taskForm.title} oninput={(e) => (taskForm = { ...taskForm, title: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={taskForm.assigned_to_user_id ?? ''} onchange={(e) => (taskForm = { ...taskForm, assigned_to_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.assignee')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select>
          <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={taskForm.due_date} oninput={(e) => (taskForm = { ...taskForm, due_date: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!taskForm.title.trim()} onclick={addTask}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Feedback -->
    {#if activeSection === 'feedback'}
      <section>
        {#if feedbacks.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.date')}</th><th class="py-1.5 pr-3">{t('sms.source')}</th><th class="py-1.5 pr-3">{t('sms.category')}</th><th class="py-1.5 pr-3 text-center">{t('sms.rating')}</th><th class="py-1.5 pr-3">{t('sms.resolutionStatus')}</th><th></th></tr></thead>
            <tbody>
              {#each feedbacks as fb (fb.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(fb.created_at)}</td>
                  <td class="py-1.5 pr-3">{t(`sms.src${fb.source.charAt(0).toUpperCase() + fb.source.slice(1)}`)}</td>
                  <td class="py-1.5 pr-3"><div>{fb.category || '—'}</div>{#if fb.body}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{fb.body}</div>{/if}</td>
                  <td class="py-1.5 pr-3 text-center">{fb.rating ?? '—'}/5</td>
                  <td class="py-1.5 pr-3"><select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={fb.resolution_status} onchange={(e) => updateFeedback(fb.id, { resolution_status: e.target.value })}><option value="open">{t('sms.fbOpen')}</option><option value="addressed">{t('sms.fbAddressed')}</option><option value="resolved">{t('sms.fbResolved')}</option></select></td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeFeedback(fb.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={feedbackForm.source} onchange={(e) => (feedbackForm = { ...feedbackForm, source: e.target.value })}>{#each FEEDBACK_SOURCES as s (s)}<option value={s}>{t(`sms.src${s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select>
          <input class="w-40 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.category')} value={feedbackForm.category} oninput={(e) => (feedbackForm = { ...feedbackForm, category: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={feedbackForm.rating} onchange={(e) => (feedbackForm = { ...feedbackForm, rating: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{l}/5</option>{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.body')} value={feedbackForm.body} oninput={(e) => (feedbackForm = { ...feedbackForm, body: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addFeedback}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Goals -->
    {#if activeSection === 'goals'}
      <section>
        {#if goals.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.goal')}</th><th class="py-1.5 pr-3">{t('sms.metric')}</th><th class="py-1.5 pr-3 text-right">{t('sms.targetValue')}</th><th class="py-1.5 pr-3 text-right">{t('sms.currentValue')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each goals as g (g.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{g.goal}</td>
                  <td class="py-1.5 pr-3">{g.metric || '—'}</td>
                  <td class="py-1.5 pr-3 text-right">{g.target_value ?? '—'}</td>
                  <td class="py-1.5 pr-3 text-right">{g.current_value ?? '—'}</td>
                  <td class="py-1.5 pr-3"><select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={g.status} onchange={(e) => updateGoal(g.id, { status: e.target.value })}>{#each GOAL_STATUSES as s (s)}<option value={s}>{t(`sms.g${s === 'on_track' ? 'OnTrack' : s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select></td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeGoal(g.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.goal')} value={goalForm.goal} oninput={(e) => (goalForm = { ...goalForm, goal: e.target.value })} />
          <input class="w-40 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.metric')} value={goalForm.metric} oninput={(e) => (goalForm = { ...goalForm, metric: e.target.value })} />
          <input type="number" step="any" class="w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.targetValue')} value={goalForm.target_value} oninput={(e) => (goalForm = { ...goalForm, target_value: e.target.value })} />
          <input type="number" step="any" class="w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.currentValue')} value={goalForm.current_value} oninput={(e) => (goalForm = { ...goalForm, current_value: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!goalForm.goal.trim()} onclick={addGoal}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Commitments (Std §21) -->
    {#if activeSection === 'commitments'}
      <section>
        {#if commitmentsList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.directionField')}</th><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.dueDate')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each commitmentsList as c (c.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3 whitespace-nowrap">{commitmentDirectionLabel(c.direction)}</td>
                  <td class="py-1.5 pr-3"><div>{c.title}</div>{#if c.description}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{c.description}</div>{/if}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(c.due_date)}</td>
                  <td class="py-1.5 pr-3">
                    <div class="flex items-center gap-1">
                      {#if c.escalated}<span class="text-xs text-amber-600" title={t('sms.escalatedBadge')}>↑</span>{/if}
                      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={c.status} onchange={(e) => updateCommitment(c.id, { status: e.target.value })}>{#each COMMITMENT_STATUSES as s (s)}<option value={s}>{commitmentStatusLabel(s)}</option>{/each}</select>
                      {#if c.status === 'open' || c.status === 'in_progress'}
                        <button type="button" class="text-xs text-amber-600 hover:underline" title={t('sms.escalate')} onclick={() => escalateCommitment(c.id)}>↑</button>
                      {/if}
                    </div>
                  </td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeCommitment(c.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={commitmentForm.direction} onchange={(e) => (commitmentForm = { ...commitmentForm, direction: e.target.value })}>{#each COMMITMENT_DIRECTIONS as d (d)}<option value={d}>{commitmentDirectionLabel(d)}</option>{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.commitmentTitle')} value={commitmentForm.title} oninput={(e) => (commitmentForm = { ...commitmentForm, title: e.target.value })} />
          <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={commitmentForm.due_date} oninput={(e) => (commitmentForm = { ...commitmentForm, due_date: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!commitmentForm.title.trim()} onclick={addCommitment}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Grievances (R3) -->
    {#if activeSection === 'grievances'}
      <section>
        {#if grievancesList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.severity')}</th><th class="py-1.5 pr-3">{t('sms.categoryField')}</th><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.responseDue')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each grievancesList as g (g.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3"><span class="rounded-full px-2 py-0.5 {priorityClass(g.severity)}">{t(`sms.pri${g.severity.charAt(0).toUpperCase() + g.severity.slice(1)}`)}</span></td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{issueCategoryLabel(g.category)}</td>
                  <td class="py-1.5 pr-3"><div>{g.title}</div>{#if g.description}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{g.description}</div>{/if}{#if g.response}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">→ {g.response}</div>{/if}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(g.response_due_at)}</td>
                  <td class="py-1.5 pr-3">
                    <div class="flex items-center gap-1">
                      {#if g.escalated}<span class="text-xs text-amber-600" title={t('sms.escalatedBadge')}>↑</span>{/if}
                      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1 py-0.5 text-xs" value={g.status} onchange={(e) => updateGrievance(g.id, { status: e.target.value })}>{#each GRIEVANCE_STATUSES as s (s)}<option value={s}>{grievanceStatusLabel(s)}</option>{/each}</select>
                      {#if g.status !== 'closed'}
                        <button type="button" class="text-xs text-amber-600 hover:underline" title={t('sms.escalate')} onclick={() => escalateGrievance(g.id)}>↑</button>
                      {/if}
                    </div>
                  </td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeGrievance(g.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.grievanceTitle')} value={grievanceForm.title} oninput={(e) => (grievanceForm = { ...grievanceForm, title: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={grievanceForm.category} onchange={(e) => (grievanceForm = { ...grievanceForm, category: e.target.value })}><option value="">{t('sms.categoryField')}</option>{#each ISSUE_CATEGORIES as c (c)}<option value={c}>{issueCategoryLabel(c)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={grievanceForm.severity} onchange={(e) => (grievanceForm = { ...grievanceForm, severity: e.target.value })}>{#each GRIEVANCE_SEVERITIES as s (s)}<option value={s}>{t(`sms.pri${s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}</select>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!grievanceForm.title.trim()} onclick={addGrievance}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Decisions log (Appendix A) -->
    {#if activeSection === 'decisions'}
      <section>
        {#if decisionsList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.decidedAt')}</th><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.decisionText')}</th><th class="py-1.5 pr-3">{t('sms.decidedBy')}</th><th></th></tr></thead>
            <tbody>
              {#each decisionsList as d (d.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(d.decided_at)}</td>
                  <td class="py-1.5 pr-3"><div>{d.title}</div>{#if d.context}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{d.context}</div>{/if}</td>
                  <td class="py-1.5 pr-3 max-w-md truncate">{d.decision || '—'}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{userName(d.decided_by_user_id)}</td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeDecision(d.id)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.decisionTitle')} value={decisionForm.title} oninput={(e) => (decisionForm = { ...decisionForm, title: e.target.value })} />
          <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={decisionForm.decided_at} oninput={(e) => (decisionForm = { ...decisionForm, decided_at: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!decisionForm.title.trim()} onclick={addDecision}>{t('sms.add')}</button>
        </div>
      </section>
    {/if}

    <!-- Network links -->
    {#if activeSection === 'network'}
      <section>
        {#if linksList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noLinks')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.otherStakeholder')}</th><th class="py-1.5 pr-3">{t('sms.relationType')}</th><th></th></tr></thead>
            <tbody>
              {#each linksList as l (l.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{linkOtherName(l)}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">{l.relation_type || '—'}</td>
                  <td class="py-1.5 text-right"><button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeLink(l.id)}>{t('sms.removeLink')}</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={linkForm.other_stakeholder_id ?? ''} onchange={(e) => (linkForm = { ...linkForm, other_stakeholder_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.otherStakeholder')}</option>{#each allStakeholders as s (s.id)}{#if s.id !== stakeholderId}<option value={s.id}>{s.name}</option>{/if}{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.relationType')} value={linkForm.relation_type} oninput={(e) => (linkForm = { ...linkForm, relation_type: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!linkForm.other_stakeholder_id} onclick={addLink}>{t('sms.addLink')}</button>
        </div>
      </section>
    {/if}

    <!-- Ownership transfers -->
    {#if activeSection === 'transfers'}
      <section>
        {#if transfersList.length === 0}
          <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noTransfers')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.fromUser')}</th><th class="py-1.5 pr-3">{t('sms.toUser')}</th><th class="py-1.5 pr-3">{t('sms.reasonField')}</th><th class="py-1.5 pr-3 text-center">{t('sms.historyReviewed')}</th><th class="py-1.5 pr-3 text-center">{t('sms.risksReviewed')}</th><th class="py-1.5 pr-3 text-center">{t('sms.commitmentsReviewed')}</th></tr></thead>
            <tbody>
              {#each transfersList as tr (tr.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3 whitespace-nowrap">{userName(tr.from_user_id)}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{userName(tr.to_user_id)}</td>
                  <td class="py-1.5 pr-3 max-w-md truncate">{tr.reason || '—'}</td>
                  <td class="py-1.5 pr-3 text-center">{tr.history_reviewed ? '✓' : '—'}</td>
                  <td class="py-1.5 pr-3 text-center">{tr.risks_reviewed ? '✓' : '—'}</td>
                  <td class="py-1.5 pr-3 text-center">{tr.commitments_reviewed ? '✓' : '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={transferForm.to_user_id ?? ''} onchange={(e) => (transferForm = { ...transferForm, to_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.toUser')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select>
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.reasonField')} value={transferForm.reason} oninput={(e) => (transferForm = { ...transferForm, reason: e.target.value })} />
          <label class="flex items-center gap-1 text-xs"><input type="checkbox" checked={transferForm.history_reviewed} onchange={(e) => (transferForm = { ...transferForm, history_reviewed: e.target.checked })} />{t('sms.historyReviewed')}</label>
          <label class="flex items-center gap-1 text-xs"><input type="checkbox" checked={transferForm.risks_reviewed} onchange={(e) => (transferForm = { ...transferForm, risks_reviewed: e.target.checked })} />{t('sms.risksReviewed')}</label>
          <label class="flex items-center gap-1 text-xs"><input type="checkbox" checked={transferForm.commitments_reviewed} onchange={(e) => (transferForm = { ...transferForm, commitments_reviewed: e.target.checked })} />{t('sms.commitmentsReviewed')}</label>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!transferForm.to_user_id} onclick={addTransfer}>{t('sms.addTransfer')}</button>
        </div>
      </section>
    {/if}

    <!-- Timeline -->
    {#if activeSection === 'timeline'}
      <section>
        {#if timeline.length === 0}
          <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <ul class="space-y-1.5">
            {#each timeline as item (item.id)}
              <li class="flex items-baseline gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs">
                <span class="w-28 shrink-0 font-medium">{t(`sms.tl${item.type.charAt(0).toUpperCase() + item.type.slice(1)}`)}</span>
                <span>{item.title}</span>
                {#if item.summary}<span class="text-[var(--color-ds-text-muted,#6b7280)]">— {item.summary}</span>{/if}
                <span class="ml-auto whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(item.at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  </div>
{:else}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
{/if}

<style>
  .sms-subtab {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    color: var(--color-ds-text-muted, #6b7280);
    border-bottom: 2px solid transparent;
  }

  .sms-subtab:hover {
    color: var(--color-ds-text, #111827);
  }

  .sms-subtab.active {
    color: var(--color-ds-accent, #2563eb);
    border-bottom-color: var(--color-ds-accent, #2563eb);
  }
</style>
