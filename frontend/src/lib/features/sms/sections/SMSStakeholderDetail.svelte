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
    classificationLabel,
    engagementLabel,
    lifecycleLabel,
    relationshipLabel,
    entityTypeLabel,
    relationshipClass,
    engagementClass,
    priorityClass,
    healthClass,
    formatDate,
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
    { id: 'tasks', labelKey: 'sms.tasks' },
    { id: 'feedback', labelKey: 'sms.feedback' },
    { id: 'goals', labelKey: 'sms.goals' },
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
  let planForm = $state({ strategy: '', frequency: 'monthly', channel: 'meeting', status: 'draft', start_date: '', end_date: '' });
  let issueForm = $state({ kind: 'issue', title: '', description: '', priority: 'medium', impact: 3, likelihood: 3, mitigation: '' });
  let taskForm = $state({ title: '', due_date: '', assigned_to_user_id: null, status: 'open' });
  let feedbackForm = $state({ source: 'meeting', category: '', rating: 3, body: '', resolution_status: 'open' });
  let goalForm = $state({ goal: '', metric: '', target_value: '', current_value: '', status: 'on_track' });
  let entityForm = $state({ entity_type: 'project', entity_id: null });

  const profile = $derived(detail ?? {});
  const entities = $derived(detail?.entities ?? []);
  const counts = $derived(detail?.counts ?? {});
  const sectionBadges = $derived({ communications: counts.communications, plans: counts.active_plans, issues: counts.open_issues, tasks: counts.open_tasks, feedback: counts.open_feedback });

  function userName(id) {
    if (!id) return '';
    const u = users.find((x) => x.id === id);
    if (!u) return `#${id}`;
    return [u.first_name, u.last_name].filter(Boolean).join(' ') || u.username || `#${id}`;
  }

  function entityOptions() {
    const list = p3mOptions[entityForm.entity_type] ?? [];
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
          role_classification: detail.role_classification ?? 'support',
          influence_level: Number(detail.influence_level ?? 3),
          interest_level: Number(detail.interest_level ?? 3),
          interests: detail.interests ?? '',
          engagement_status: detail.engagement_status ?? 'passive',
          lifecycle_stage: detail.lifecycle_stage ?? 'onboarding',
          relationship_status: detail.relationship_status ?? 'neutral',
          communication_preferences: detail.communication_preferences ?? '',
          owner_user_id: detail.owner_user_id ?? null,
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
      await smsApi.updateStakeholder(stakeholderId, { ...form, custom_fields: JSON.stringify(fieldsObj) });
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
  let tasksList = $state([]);
  let feedbacks = $state([]);
  let goals = $state([]);
  const loadedTabs = $state(new Set());

  async function loadSectionData(section) {
    try {
      switch (section) {
        case 'communications': comms = (await smsApi.getCommunications(stakeholderId)) ?? []; break;
        case 'plans': plans = (await smsApi.engagementPlans.getAll(stakeholderId)) ?? []; break;
        case 'issues': issues = (await smsApi.issues.getAll(stakeholderId)) ?? []; break;
        case 'tasks': tasksList = (await smsApi.tasks.getAll(stakeholderId)) ?? []; break;
        case 'feedback': feedbacks = (await smsApi.feedback.getAll(stakeholderId)) ?? []; break;
        case 'goals': goals = (await smsApi.goals.getAll(stakeholderId)) ?? []; break;
      }
      loadedTabs.add(section);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  $effect(() => {
    if (!detail) return;
    const s = activeSection;
    if ((s === 'communications' || s === 'plans' || s === 'issues' || s === 'tasks' || s === 'feedback' || s === 'goals') && !loadedTabs.has(s)) {
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
      await smsApi.engagementPlans.create(stakeholderId, planForm);
      planForm = { strategy: '', frequency: 'monthly', channel: 'meeting', status: 'draft', start_date: '', end_date: '' };
      await refresh();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function updatePlan(id, data) {
    try {
      await smsApi.engagementPlans.update(id, data);
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
      issueForm = { kind: 'issue', title: '', description: '', priority: 'medium', impact: 3, likelihood: 3, mitigation: '' };
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
    await Promise.allSettled([load(), getUsers().then((u) => (users = u)).catch(() => {}), fetchAPI('/sms/p3m-options').then((o) => (p3mOptions = o ?? p3mOptions)).catch(() => {})]);
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
          <label class="text-xs"><span>{t('sms.commPrefs')}</span><input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.communication_preferences} oninput={(e) => (form = { ...form, communication_preferences: e.target.value })} /></label>
          <label class="text-xs"><span>{t('sms.owner')}</span><select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.owner_user_id ?? ''} onchange={(e) => (form = { ...form, owner_user_id: e.target.value ? Number(e.target.value) : null })}><option value="">{t('sms.noOwner')}</option>{#each users as u (u.id)}<option value={u.id}>{userName(u.id)}</option>{/each}</select></label>
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
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.strategy')}</th><th class="py-1.5 pr-3">{t('sms.frequency')}</th><th class="py-1.5 pr-3">{t('sms.channel')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th class="py-1.5 pr-3">{t('sms.startDate')}</th><th class="py-1.5 pr-3">{t('sms.endDate')}</th><th></th></tr></thead>
            <tbody>
              {#each plans as p (p.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{p.strategy}</td>
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
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1.5 pr-3">{t('sms.kind')}</th><th class="py-1.5 pr-3">{t('sms.titleField')}</th><th class="py-1.5 pr-3">{t('sms.priority')}</th><th class="py-1.5 pr-3 text-center">{t('sms.impact')}/{t('sms.likelihood')}</th><th class="py-1.5 pr-3">{t('sms.status')}</th><th></th></tr></thead>
            <tbody>
              {#each issues as iss (iss.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5">{iss.kind === 'risk' ? t('sms.kindRisk') : t('sms.kindIssue')}</span></td>
                  <td class="py-1.5 pr-3"><div>{iss.title}</div>{#if iss.description}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{iss.description}</div>{/if}{#if iss.mitigation}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">→ {iss.mitigation}</div>{/if}</td>
                  <td class="py-1.5 pr-3"><span class="rounded-full px-2 py-0.5 {priorityClass(iss.priority)}">{t(`sms.pri${iss.priority.charAt(0).toUpperCase() + iss.priority.slice(1)}`)}</span></td>
                  <td class="py-1.5 pr-3 text-center">{iss.impact}/{iss.likelihood}</td>
                  <td class="py-1.5 pr-3">
                    <div class="flex items-center gap-1">
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
          <input class="w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.titleField')} value={issueForm.title} oninput={(e) => (issueForm = { ...issueForm, title: e.target.value })} />
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.priority} onchange={(e) => (issueForm = { ...issueForm, priority: e.target.value })}>{#each PRIORITIES as p (p)}<option value={p}>{t(`sms.pri${p.charAt(0).toUpperCase() + p.slice(1)}`)}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.impact} onchange={(e) => (issueForm = { ...issueForm, impact: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{t('sms.impact')}: {l}</option>{/each}</select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={issueForm.likelihood} onchange={(e) => (issueForm = { ...issueForm, likelihood: Number(e.target.value) })}>{#each LEVELS as l (l)}<option value={l}>{t('sms.likelihood')}: {l}</option>{/each}</select>
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
