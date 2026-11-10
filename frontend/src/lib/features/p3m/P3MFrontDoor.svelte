<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import EmptyState from '../../components/EmptyState.svelte';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { formatMoney, formatDate } from './utils.js';

  let { portfolios = [], onChanged = () => {} } = $props();

  const COMPLEXITIES = ['low', 'med', 'high'];
  const WORK_TYPES = [
    { value: 'programme', labelKey: 'p3m.workTypeProgramme' },
    { value: 'project', labelKey: 'p3m.workTypeProject' },
    { value: 'continuous_improvement', labelKey: 'p3m.workTypeContinuousImprovement' },
    { value: 'digital_product', labelKey: 'p3m.workTypeDigitalProduct' },
    { value: 'operational', labelKey: 'p3m.workTypeOperational' },
  ];
  const DECISIONS = ['proceed', 'redirect', 'defer', 'decline', 'return_info'];

  let intakes = $state([]);
  /** @type {Array<{complexity:string,field_key:string,required:boolean}>} */
  let screens = $state([]);
  let loading = $state(true);
  let error = $state('');

  // --- Intake form (create / edit) -------------------------------------------
  function emptyForm() {
    return {
      title: '', source: 'standard', description: '', problem_opportunity: '', strategic_alignment: '',
      intended_outcomes: '', indicative_cost: '', resource_requirements: '', risk_notes: '',
      change_impact_notes: '', business_group: '', work_type: '', complexity: 'low', lifecycle_position: '',
    };
  }

  let formOpen = $state(false);
  /** @type {any|null} */
  let editingIntake = $state(null);
  let form = $state(emptyForm());
  let savingForm = $state(false);
  let formError = $state('');

  function openCreate() {
    editingIntake = null;
    form = emptyForm();
    formError = '';
    formOpen = true;
  }

  function openEdit(i) {
    editingIntake = i;
    form = {
      title: i.title ?? '', source: i.source || 'standard', description: i.description ?? '',
      problem_opportunity: i.problem_opportunity ?? '', strategic_alignment: i.strategic_alignment ?? '',
      intended_outcomes: i.intended_outcomes ?? '', indicative_cost: i.indicative_cost != null ? String(i.indicative_cost) : '',
      resource_requirements: i.resource_requirements ?? '', risk_notes: i.risk_notes ?? '',
      change_impact_notes: i.change_impact_notes ?? '', business_group: i.business_group ?? '',
      work_type: i.work_type ?? '', complexity: i.complexity || 'low', lifecycle_position: i.lifecycle_position ?? '',
    };
    formError = '';
    formOpen = true;
  }

  async function submitForm() {
    if (savingForm) return;
    savingForm = true;
    formError = '';
    const payload = {
      ...form,
      indicative_cost: form.indicative_cost === '' ? null : Number(form.indicative_cost),
      work_type: form.work_type || null,
      lifecycle_position: form.lifecycle_position.trim() || null,
    };
    try {
      if (editingIntake) await p3mApi.updateFrontDoorIntake(editingIntake.id, payload);
      else await p3mApi.createFrontDoorIntake(payload);
      formOpen = false;
      await load();
      onChanged();
    } catch (e) {
      formError = e?.message || t('p3m.createFailed');
    } finally {
      savingForm = false;
    }
  }

  // --- Status moves (captured <-> assessing, reopen after return_info) --------
  async function moveStatus(i, status) {
    error = '';
    try {
      await p3mApi.updateFrontDoorIntake(i.id, { status });
      await load();
      onChanged();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    }
  }

  // --- Decision panel ------------------------------------------------------------
  let deciding = $state(null);
  let decisionForm = $state({ decision: 'proceed', rationale: '', conditions: '', portfolio_id: '', programme_id: '', target_project_id: '' });
  let savingDecision = $state(false);
  let decisionError = $state('');

  function openDecide(i) {
    deciding = i;
    decisionForm = { decision: 'proceed', rationale: '', conditions: '', portfolio_id: '', programme_id: '', target_project_id: '' };
    decisionError = '';
  }

  const allProjects = $derived(portfolios.flatMap((pf) => (pf.programmes ?? []).flatMap((pg) => pg.projects ?? [])));
  const programmesForDecision = $derived(
    portfolios.find((pf) => String(pf.id) === String(decisionForm.portfolio_id))?.programmes ?? []
  );

  /** Required field keys for a complexity, per the admin-configured screens. */
  function requiredKeys(complexity) {
    return screens.filter((s) => s.complexity === complexity && s.required).map((s) => s.field_key);
  }

  const FIELD_LABELS = {
    title: 'p3m.intakeTitle', source: 'p3m.intakeSource', description: 'p3m.description',
    problem_opportunity: 'p3m.problemOpportunity', strategic_alignment: 'p3m.strategicAlignment',
    intended_outcomes: 'p3m.intendedOutcomes', indicative_cost: 'p3m.indicativeCost',
    resource_requirements: 'p3m.resourceRequirements', risk_notes: 'p3m.riskNotes',
    change_impact_notes: 'p3m.changeImpactNotes', business_group: 'p3m.businessGroup', work_type: 'p3m.workType',
  };

  /** Mirrors the backend screen check: required fields still empty for this intake. */
  function missingFields(i) {
    const present = {
      title: !!i.title?.trim(), source: i.source !== '', description: !!i.description?.trim(),
      problem_opportunity: !!i.problem_opportunity?.trim(), strategic_alignment: !!i.strategic_alignment?.trim(),
      intended_outcomes: !!i.intended_outcomes?.trim(), indicative_cost: i.indicative_cost != null,
      resource_requirements: !!i.resource_requirements?.trim(), risk_notes: !!i.risk_notes?.trim(),
      change_impact_notes: !!i.change_impact_notes?.trim(), business_group: !!i.business_group?.trim(),
      work_type: !!i.work_type,
    };
    return requiredKeys(i.complexity).filter((k) => !present[k]);
  }

  async function submitDecision() {
    if (!deciding || savingDecision) return;
    savingDecision = true;
    decisionError = '';
    const payload = {
      decision: decisionForm.decision,
      rationale: decisionForm.rationale.trim(),
      conditions: decisionForm.conditions,
    };
    if (decisionForm.decision === 'proceed' && !deciding.target_project_id) {
      // Re-decision after return_info keeps the existing project; otherwise link or create.
      if (decisionForm.target_project_id) payload.target_project_id = Number(decisionForm.target_project_id);
      else {
        if (decisionForm.portfolio_id) payload.portfolio_id = Number(decisionForm.portfolio_id);
        if (decisionForm.programme_id) payload.programme_id = Number(decisionForm.programme_id);
      }
    }
    try {
      await p3mApi.decideFrontDoorIntake(deciding.id, payload);
      deciding = null;
      await load();
      onChanged();
    } catch (e) {
      decisionError = e?.message || t('p3m.updateFailed');
    } finally {
      savingDecision = false;
    }
  }

  // --- Board ---------------------------------------------------------------------
  const columns = $derived([
    { key: 'captured', items: intakes.filter((i) => i.status === 'captured') },
    { key: 'assessing', items: intakes.filter((i) => i.status === 'assessing') },
    { key: 'decided', items: intakes.filter((i) => i.status === 'decided') },
  ]);

  const complexityChip = { low: 'bg-green-50 text-green-700', med: 'bg-amber-50 text-amber-700', high: 'bg-red-50 text-red-700' };
  const decisionChip = { proceed: 'bg-green-50 text-green-700', redirect: 'bg-blue-50 text-blue-700', defer: 'bg-amber-50 text-amber-700', decline: 'bg-red-50 text-red-700', return_info: 'bg-purple-50 text-purple-700' };

  function complexityLabel(c) {
    const key = c === 'med' ? 'Med' : `${c[0].toUpperCase()}${c.slice(1)}`;
    return t(`p3m.complexity${key}`);
  }

  function decisionLabel(d) {
    return t(`p3m.decision${d[0].toUpperCase()}${d.slice(1).replace('_info', 'Info')}`);
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [list, scr] = await Promise.all([
        p3mApi.getFrontDoorIntakes(),
        p3mApi.getFrontDoorScreens().catch(() => []),
      ]);
      intakes = Array.isArray(list) ? list : [];
      screens = Array.isArray(scr) ? scr : [];
    } catch (e) {
      error = e?.message || t('p3m.loadErrorTitle');
    } finally {
      loading = false;
    }
  }

  onMount(() => { load(); });
</script>

{#if loading && intakes.length === 0}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && intakes.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-4 px-6 py-4">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.frontDoorSubtitle')}</p>
      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:text-[var(--color-ds-text,#111827)]" onclick={openCreate}>
        <IconPlus size={14} class="inline" /> {t('p3m.frontDoorNewIntake')}
      </button>
    </div>

    {#if error}
      <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
    {/if}

    <!-- Intake form (create / edit) -->
    {#if formOpen}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
        <h2 class="mb-2 font-semibold">{editingIntake ? `${t('p3m.intakeTitle')}: ${editingIntake.title}` : t('p3m.frontDoorNewIntake')}</h2>
        {#if formError}
          <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{formError}</div>
        {/if}
        <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
          <label class="block">
            <span>{t('p3m.intakeTitle')} *</span>
            <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.title} disabled={savingForm} oninput={(e) => (form = { ...form, title: e.target.value })} />
          </label>
          <label class="block">
            <span>{t('p3m.intakeSource')}</span>
            <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.source} disabled={savingForm} onchange={(e) => (form = { ...form, source: e.target.value })}>
              <option value="standard">{t('p3m.sourceStandard')}</option>
              <option value="exceptional_path">{t('p3m.sourceExceptional')}</option>
            </select>
          </label>
          <label class="block">
            <span>{t('p3m.complexity')}</span>
            <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.complexity} disabled={savingForm} onchange={(e) => (form = { ...form, complexity: e.target.value })}>
              {#each COMPLEXITIES as c (c)}
                <option value={c}>{complexityLabel(c)}</option>
              {/each}
            </select>
          </label>
          <label class="block">
            <span>{t('p3m.workType')}</span>
            <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.work_type} disabled={savingForm} onchange={(e) => (form = { ...form, work_type: e.target.value })}>
              <option value="">{t('p3m.noSelection')}</option>
              {#each WORK_TYPES as wt (wt.value)}
                <option value={wt.value}>{t(wt.labelKey)}</option>
              {/each}
            </select>
          </label>
          <label class="block">
            <span>{t('p3m.businessGroup')}</span>
            <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.business_group} disabled={savingForm} oninput={(e) => (form = { ...form, business_group: e.target.value })} />
          </label>
          <label class="block">
            <span>{t('p3m.indicativeCost')}</span>
            <input type="number" min="0" step="any" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.indicative_cost} disabled={savingForm} oninput={(e) => (form = { ...form, indicative_cost: e.target.value })} />
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.description')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.description} disabled={savingForm} oninput={(e) => (form = { ...form, description: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.problemOpportunity')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.problem_opportunity} disabled={savingForm} oninput={(e) => (form = { ...form, problem_opportunity: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.strategicAlignment')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.strategic_alignment} disabled={savingForm} oninput={(e) => (form = { ...form, strategic_alignment: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.intendedOutcomes')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.intended_outcomes} disabled={savingForm} oninput={(e) => (form = { ...form, intended_outcomes: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.resourceRequirements')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.resource_requirements} disabled={savingForm} oninput={(e) => (form = { ...form, resource_requirements: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.riskNotes')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.risk_notes} disabled={savingForm} oninput={(e) => (form = { ...form, risk_notes: e.target.value })}></textarea>
          </label>
          <label class="block md:col-span-3">
            <span>{t('p3m.changeImpactNotes')}</span>
            <textarea rows="2" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.change_impact_notes} disabled={savingForm} oninput={(e) => (form = { ...form, change_impact_notes: e.target.value })}></textarea>
          </label>
          {#if form.source === 'exceptional_path'}
            <label class="block md:col-span-3">
              <span>{t('p3m.lifecyclePosition')}</span>
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.lifecycle_position} disabled={savingForm} oninput={(e) => (form = { ...form, lifecycle_position: e.target.value })} />
            </label>
          {/if}
        </div>
        <div class="mt-3 flex gap-2">
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:text-[var(--color-ds-text,#111827)]" disabled={savingForm || !form.title.trim()} onclick={submitForm}>{t('p3m.save')}</button>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (formOpen = false)}>{t('p3m.cancel')}</button>
        </div>
      </div>
    {/if}

    <!-- Decision panel -->
    {#if deciding}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
        <h2 class="mb-2 font-semibold">{t('p3m.decideIntake')}: {deciding.title}</h2>
        {#if decisionError}
          <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{decisionError}</div>
        {/if}
        {#if missingFields(deciding).length > 0}
          <div class="mb-2 rounded-md border border-amber-300 bg-amber-50 p-2 text-xs text-amber-800">
            <span class="font-semibold">{t('p3m.missingRequiredFields')}</span> {missingFields(deciding).map((k) => t(FIELD_LABELS[k] ?? k)).join(', ')}
          </div>
        {/if}
        <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
          <label class="block">
            <span>{t('p3m.decision')} *</span>
            <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={decisionForm.decision} disabled={savingDecision} onchange={(e) => (decisionForm = { ...decisionForm, decision: e.target.value })}>
              {#each DECISIONS as d (d)}
                <option value={d}>{decisionLabel(d)}</option>
              {/each}
            </select>
          </label>
          {#if decisionForm.decision === 'proceed' && !deciding.target_project_id}
            <div class="block">
              <span>{t('p3m.createNewProject')}</span>
              <div class="mt-0.5 flex gap-2">
                <select class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={decisionForm.portfolio_id} disabled={savingDecision} onchange={(e) => (decisionForm = { ...decisionForm, portfolio_id: e.target.value, programme_id: '' })}>
                  <option value="">{t('p3m.noSelection')}</option>
                  {#each portfolios as pf (pf.id)}
                    <option value={pf.id}>{pf.title}</option>
                  {/each}
                </select>
                <select class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={decisionForm.programme_id} disabled={savingDecision || !decisionForm.portfolio_id} onchange={(e) => (decisionForm = { ...decisionForm, programme_id: e.target.value })}>
                  <option value="">{t('p3m.noSelection')}</option>
                  {#each programmesForDecision as pg (pg.id)}
                    <option value={pg.id}>{pg.title}</option>
                  {/each}
                </select>
              </div>
            </div>
          {:else if decisionForm.decision === 'proceed'}
            <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.targetProject')}: #{deciding.target_project_id}</span>
          {/if}
        </div>
        <div class="mt-2 grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
          <label class="block md:col-span-2">
            <span>{t('p3m.rationale')} *</span>
            <textarea class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" rows="2" value={decisionForm.rationale} disabled={savingDecision} oninput={(e) => (decisionForm = { ...decisionForm, rationale: e.target.value })}></textarea>
          </label>
          <label class="block">
            <span>{t('p3m.conditions')} {decisionForm.decision === 'proceed' ? '' : '(—)'}</span>
            <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={decisionForm.conditions} disabled={savingDecision || decisionForm.decision !== 'proceed'} oninput={(e) => (decisionForm = { ...decisionForm, conditions: e.target.value })} />
          </label>
        </div>
        <p class="mt-1 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.conditionsHint')}</p>
        <div class="mt-2 flex gap-2">
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:text-[var(--color-ds-text,#111827)]" disabled={savingDecision || !decisionForm.rationale.trim()} onclick={submitDecision}>{t('p3m.decideIntake')}</button>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (deciding = null)}>{t('p3m.cancel')}</button>
        </div>
      </div>
    {/if}

    <!-- Kanban board -->
    {#if intakes.length === 0}
      <EmptyState title={t('p3m.frontDoor')} description={t('p3m.frontDoorEmpty')} actions={{ label: t('p3m.frontDoorNewIntake'), onClick: openCreate }} />
    {:else}
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {#each columns as col (col.key)}
          <section>
            <h2 class="mb-2 text-sm font-semibold">{t(`p3m.status${col.key[0].toUpperCase()}${col.key.slice(1)}`)} <span class="text-[var(--color-ds-text-muted,#6b7280)]">({col.items.length})</span></h2>
            <div class="space-y-2">
              {#each col.items as i (i.id)}
                <article class="cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs hover:border-[var(--color-ds-accent,#3b82f6)]" onclick={() => openEdit(i)}>
                  <div class="mb-1 flex items-start justify-between gap-2">
                    <span class="font-semibold">{i.title}</span>
                    <span class="shrink-0 rounded px-1.5 py-0.5 font-medium {complexityChip[i.complexity] ?? ''}">{complexityLabel(i.complexity)}</span>
                  </div>
                  <div class="mb-2 flex flex-wrap gap-x-3 text-[var(--color-ds-text-muted,#6b7280)]">
                    {#if i.work_type}
                      {@const wt = WORK_TYPES.find((w) => w.value === i.work_type)}
                      <span>{wt ? t(wt.labelKey) : i.work_type}</span>
                    {/if}
                    {#if i.business_group}
                      <span>{i.business_group}</span>
                    {/if}
                    {#if i.indicative_cost != null}
                      <span>{formatMoney(i.indicative_cost)}</span>
                    {/if}
                    {#if i.source === 'exceptional_path'}
                      <span class="rounded bg-purple-50 px-1.5 py-0.5 font-medium text-purple-700">{t('p3m.sourceExceptional')}</span>
                    {/if}
                  </div>
                  {#if i.status === 'decided' && i.decision}
                    <div class="mb-2 flex items-center gap-2">
                      <span class="rounded px-1.5 py-0.5 font-medium {decisionChip[i.decision] ?? ''}">{decisionLabel(i.decision)}</span>
                      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(i.decision_date)}</span>
                    </div>
                  {/if}
                  <div class="flex flex-wrap gap-1" onclick={(e) => e.stopPropagation()}>
                    {#if i.status === 'captured'}
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => moveStatus(i, 'assessing')}>{t('p3m.moveToAssessing')}</button>
                    {:else if i.status === 'assessing'}
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => moveStatus(i, 'captured')}>{t('p3m.backToCaptured')}</button>
                    {/if}
                    {#if i.status !== 'decided'}
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 hover:text-[var(--color-ds-text,#111827)]" onclick={() => openDecide(i)}>{t('p3m.decideIntake')}</button>
                    {:else if i.decision === 'return_info'}
                      <button type="button" class="rounded-md border border-amber-300 bg-amber-50 px-2 py-0.5 text-amber-700 hover:bg-amber-100" onclick={() => moveStatus(i, 'assessing')}>{t('p3m.reopenToAssessing')}</button>
                    {/if}
                  </div>
                </article>
              {:else}
                <div class="rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-4 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
              {/each}
            </div>
          </section>
        {/each}
      </div>
    {/if}
  </div>
{/if}
