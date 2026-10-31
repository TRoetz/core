<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { time } from '../../api/time.js';
  import { getUsers } from '../../api/users.js';
  import { toISODate } from './utils.js';

  /**
   * Create form for a P3M project. The parent is either a portfolio directly
   * (standalone) or one of the portfolio's programmes; the server resolves
   * both shapes via resolveProjectParents. RAG is computed server-side and is
   * not part of the payload.
   */
  let { portfolios = [], defaultPortfolioId = null, defaultProgrammeId = null, onCreated = () => {}, onCancel = () => {} } = $props();

  const STAGES = ['initiation', 'planning', 'execution', 'closure'];

  let form = $state({
    portfolio_id: defaultPortfolioId ? String(defaultPortfolioId) : '',
    programme_id: defaultProgrammeId ? String(defaultProgrammeId) : '', // '' = standalone under the selected portfolio
    title: '',
    description: '',
    manager_id: '',
    time_project_id: '',
    current_stage: 'initiation',
    budget_approved: '',
    budget_actual: '',
    progress_percent: 0,
    start_date: '',
    target_completion_date: '',
  });
  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);
  /** @type {Array<{id:number,name:string}>} */
  let timeProjects = $state([]);
  let saving = $state(false);
  let error = $state('');

  // Option values in the selects below are rendered as strings, so form.portfolio_id
  // stays a string; compare via String() on both sides anyway (ids arrive as numbers).
  const programmesForPortfolio = $derived.by(() => {
    if (!form.portfolio_id) return [];
    const pf = portfolios.find((p) => String(p.id) === String(form.portfolio_id));
    return pf?.programmes ?? [];
  });

  onMount(async () => {
    [users, timeProjects] = await Promise.all([
      getUsers().catch(() => []),
      time.projects.getAll().then((r) => (Array.isArray(r) ? r : [])).catch(() => []),
    ]);
  });

  async function submit() {
    if (saving) return;
    error = '';
    if (!form.title.trim()) {
      error = t('p3m.titleRequired');
      return;
    }
    if (!form.portfolio_id) {
      error = t('p3m.portfolioRequired');
      return;
    }
    saving = true;
    try {
      const payload = {
        portfolio_id: Number(form.portfolio_id),
        title: form.title.trim(),
        description: form.description,
        current_stage: form.current_stage,
        budget_approved: Number(form.budget_approved) || 0,
        budget_actual: Number(form.budget_actual) || 0,
        progress_percent: Math.max(0, Math.min(100, Number(form.progress_percent) || 0)),
      };
      if (form.programme_id) payload.programme_id = Number(form.programme_id);
      if (form.manager_id) payload.manager_id = Number(form.manager_id);
      if (form.time_project_id) payload.time_project_id = Number(form.time_project_id);
      const start = toISODate(form.start_date);
      if (start) payload.start_date = start;
      const target = toISODate(form.target_completion_date);
      if (target) payload.target_completion_date = target;

      await p3mApi.projects.create(payload);
      onCreated();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }
</script>

<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
  <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-2">
    <label class="block">
      <span>{t('p3m.parentPortfolio')}</span>
      <select
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        bind:value={form.portfolio_id}
        disabled={saving || !!defaultPortfolioId}
        onchange={() => (form = { ...form, programme_id: '' })}
      >
        <option value="">{t('p3m.noSelection')}</option>
        {#each portfolios as pf (pf.id)}
          <!-- String values: bind:value assigns the option's typed value on change and
               matches it strictly against state, so keep both sides strings. -->
          <option value={String(pf.id)}>{pf.title}</option>
        {/each}
      </select>
    </label>

    <label class="block">
      <span>{t('p3m.parentProgramme')}</span>
      <select
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        bind:value={form.programme_id}
        disabled={saving || !form.portfolio_id}
      >
        <option value="">{t('p3m.standaloneUnderPortfolio')}</option>
        {#each programmesForPortfolio as pg (pg.id)}
          <option value={String(pg.id)}>{pg.title}</option>
        {/each}
      </select>
    </label>

    <label class="block">
      <span>{t('p3m.titleLabel')} *</span>
      <input
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.title}
        disabled={saving}
        oninput={(e) => (form = { ...form, title: e.target.value })}
      />
    </label>

    <label class="block">
      <span>{t('p3m.manager')}</span>
      <select
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.manager_id}
        disabled={saving}
        onchange={(e) => (form = { ...form, manager_id: e.target.value })}
      >
        <option value="">{t('p3m.noSelection')}</option>
        {#each users as u (u.id)}
          <option value={u.id}>{u.display_name || u.username}</option>
        {/each}
      </select>
    </label>

    <label class="block">
      <span>{t('p3m.timeProjectLink')}</span>
      <select
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.time_project_id}
        disabled={saving}
        onchange={(e) => (form = { ...form, time_project_id: e.target.value })}
      >
        <option value="">{t('p3m.noTimeProject')}</option>
        {#each timeProjects as tp (tp.id)}
          <option value={tp.id}>{tp.name}</option>
        {/each}
      </select>
    </label>

    <label class="block">
      <span>{t('p3m.currentStage')}</span>
      <select
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.current_stage}
        disabled={saving}
        onchange={(e) => (form = { ...form, current_stage: e.target.value })}
      >
        {#each STAGES as s (s)}
          <option value={s}>{t(`p3m.stage${s[0].toUpperCase()}${s.slice(1)}`)}</option>
        {/each}
      </select>
    </label>

    <label class="block">
      <span>{t('p3m.budgetApproved')}</span>
      <input
        type="number" step="any" min="0"
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.budget_approved}
        disabled={saving}
        oninput={(e) => (form = { ...form, budget_approved: e.target.value })}
      />
    </label>

    <label class="block">
      <span>{t('p3m.budgetActual')}</span>
      <input
        type="number" step="any" min="0"
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.budget_actual}
        disabled={saving}
        oninput={(e) => (form = { ...form, budget_actual: e.target.value })}
      />
    </label>

    <label class="block">
      <span>{t('p3m.progressPercent')} (%)</span>
      <input
        type="number" min="0" max="100" step="5"
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.progress_percent}
        disabled={saving}
        oninput={(e) => (form = { ...form, progress_percent: e.target.value })}
      />
    </label>

    <div class="grid grid-cols-2 gap-x-4">
      <label class="block">
        <span>{t('p3m.startDate')}</span>
        <input
          type="date"
          class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
          value={form.start_date}
          disabled={saving}
          oninput={(e) => (form = { ...form, start_date: e.target.value })}
        />
      </label>
      <label class="block">
        <span>{t('p3m.targetCompletionDate')}</span>
        <input
          type="date"
          class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
          value={form.target_completion_date}
          disabled={saving}
          oninput={(e) => (form = { ...form, target_completion_date: e.target.value })}
        />
      </label>
    </div>

    <label class="block md:col-span-2">
      <span>{t('p3m.description')}</span>
      <textarea
        rows="2"
        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        value={form.description}
        disabled={saving}
        oninput={(e) => (form = { ...form, description: e.target.value })}
      ></textarea>
    </label>
  </div>

  {#if error}
    <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{error}</div>
  {/if}

  <div class="mt-3 flex items-center gap-2">
    <button
      type="button"
      class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50"
      disabled={saving}
      onclick={submit}
    >
      {t('p3m.create')}
    </button>
    <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={onCancel}>
      {t('p3m.cancel')}
    </button>
  </div>
</div>
