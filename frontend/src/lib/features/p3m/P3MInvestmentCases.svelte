<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconDownload from '@tabler/icons-svelte-runes/icons/download';
  import IconPencil from '@tabler/icons-svelte-runes/icons/pencil';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconStar from '@tabler/icons-svelte-runes/icons/star';
  import IconClock from '@tabler/icons-svelte-runes/icons/clock';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { formatDate, ragColor } from './utils.js';

  let { portfolios = [], onChanged = () => {} } = $props();

  const STATUSES = ['proposed', 'under_review', 'approved', 'rejected', 'deferred'];
  const STATUS_KEYS = {
    proposed: 'icStatusProposed',
    under_review: 'icStatusUnderReview',
    approved: 'icStatusApproved',
    rejected: 'icStatusRejected',
    deferred: 'icStatusDeferred',
  };
  const STATUS_BADGES = {
    proposed: 'bg-slate-100 text-slate-600',
    under_review: 'bg-amber-100 text-amber-700',
    approved: 'bg-emerald-100 text-emerald-700',
    rejected: 'bg-red-100 text-red-700',
    deferred: 'bg-purple-100 text-purple-700',
  };

  function statusLabel(status) {
    return t(`p3m.${STATUS_KEYS[status] ?? status}`);
  }

  let cases = $state([]);
  let loading = $state(true);
  let error = $state('');

  // --- Filters -----------------------------------------------------------------
  /** '' = all portfolios */
  let portfolioFilter = $state('');
  /** 'all' or one of STATUSES (chip toggle) */
  let statusFilter = $state('all');

  const visibleCases = $derived.by(() =>
    cases.filter((c) => {
      if (portfolioFilter !== '' && String(c.portfolio_id ?? '') !== portfolioFilter) return false;
      if (statusFilter !== 'all' && c.status !== statusFilter) return false;
      return true;
    })
  );

  const grouped = $derived.by(() =>
    STATUSES.map((s) => ({ status: s, items: visibleCases.filter((c) => c.status === s) })).filter(
      (g) => g.items.length > 0
    )
  );

  /** Flat project list (id -> title) for the new-case picker. */
  const allProjects = $derived.by(() => {
    const out = [];
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pg.title} › ${pr.title}` });
      }
      for (const pr of pf.standalone_projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pr.title}` });
    }
    return out;
  });

  /** Projects that do not have an investment case yet. */
  const projectsForNewCase = $derived.by(() =>
    allProjects.filter((p) => !cases.some((c) => String(c.project_id) === String(p.id)))
  );

  async function load() {
    loading = true;
    error = '';
    try {
      cases = (await p3mApi.getInvestmentCases()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  function downloadExport(scope) {
    window.open(p3mApi.exportUrl(scope), '_blank', 'noopener');
  }

  // --- Create / edit form ---------------------------------------------------------
  let formOpen = $state(false);
  /** null = create mode, otherwise the case id being edited. */
  let editingId = $state(null);
  let saving = $state(false);
  let formError = $state('');

  function emptyForm() {
    return { project_id: '', benefit_score: '3', risk_score: '3', roi_percent: '', payback_months: '', business_case: '' };
  }
  let form = $state(emptyForm());

  /** Statuses selectable in edit mode: proposed/under_review, plus the current
   * status when it is terminal (terminal statuses are decision-endpoint-only). */
  const editableStatuses = $derived.by(() => {
    if (!editingId) return [];
    const cur = cases.find((c) => c.id === editingId)?.status;
    const out = ['proposed', 'under_review'];
    if (cur && !out.includes(cur)) out.unshift(cur);
    return out;
  });

  function toggleForm() {
    formOpen = !formOpen;
    if (formOpen) {
      editingId = null;
      form = emptyForm();
      formError = '';
    }
  }

  function startEdit(c) {
    formOpen = true;
    editingId = c.id;
    form = {
      project_id: String(c.project_id),
      benefit_score: String(c.benefit_score ?? 3),
      risk_score: String(c.risk_score ?? 3),
      roi_percent: c.roi_percent == null ? '' : String(c.roi_percent),
      payback_months: c.payback_months == null ? '' : String(c.payback_months),
      business_case: c.business_case ?? '',
      status: c.status,
    };
    formError = '';
  }

  async function submitForm() {
    if (saving) return;
    formError = '';
    const payload = {
      benefit_score: Number(form.benefit_score),
      risk_score: Number(form.risk_score),
      roi_percent: form.roi_percent === '' ? null : Number(form.roi_percent),
      payback_months: form.payback_months === '' ? null : Number(form.payback_months),
      business_case: form.business_case,
    };
    saving = true;
    try {
      if (editingId) {
        payload.status = form.status;
        await p3mApi.updateInvestmentCase(editingId, payload);
      } else {
        if (!form.project_id) {
          formError = t('p3m.projectRequired');
          return;
        }
        payload.project_id = Number(form.project_id);
        await p3mApi.createInvestmentCase(payload);
      }
      formOpen = false;
      editingId = null;
      await load();
      onChanged();
    } catch (e) {
      formError = e?.message || t('p3m.updateFailed');
    } finally {
      saving = false;
    }
  }

  // --- Score history timeline (PO-008) -------------------------------------------------
  /** id of the case whose history row is expanded, or null. */
  let historyFor = $state(null);
  /** @type {Record<number, Array<{at:string,user?:string,criterion?:string,score?:number,raw?:string}>} */
  let histories = $state({});
  let historyLoadingId = $state(null);

  async function toggleHistory(c) {
    if (historyFor === c.id) {
      historyFor = null;
      return;
    }
    historyFor = c.id;
    if (!histories[c.id]) {
      historyLoadingId = c.id;
      try {
        histories = { ...histories, [c.id]: (await p3mApi.getInvestmentCaseScoreHistory(c.id)) ?? [] };
      } catch (e) {
        error = e?.message || 'error';
      } finally {
        historyLoadingId = null;
      }
    }
  }

  /** Group consecutive entries sharing a timestamp into one timeline line. */
  function groupedHistory(entries) {
    const out = [];
    for (const e of entries ?? []) {
      const last = out[out.length - 1];
      if (last && last.at === e.at) last.items.push(e);
      else out.push({ at: e.at, user: e.user, items: [e] });
    }
    return out;
  }

  // --- Decision flow ----------------------------------------------------------------
  /** { id, outcome } while the confirm panel is open. */
  let decision = $state(null);
  let decisionNote = $state('');
  let deciding = $state(false);

  function openDecision(c, outcome) {
    decision = { id: c.id, outcome };
    decisionNote = '';
  }

  async function confirmDecision() {
    if (!decision || deciding) return;
    deciding = true;
    try {
      await p3mApi.decideInvestmentCase(decision.id, { outcome: decision.outcome, notes: decisionNote });
      decision = null;
      await load();
      onChanged();
    } catch (e) {
      error = e?.message || 'error';
      decision = null;
    } finally {
      deciding = false;
    }
  }

  async function removeCase(c) {
    if (!confirm(t('p3m.confirmDeleteInvestmentCase'))) return;
    try {
      await p3mApi.deleteInvestmentCase(c.id);
      await load();
      onChanged();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  function excerpt(s, n = 90) {
    if (!s) return '';
    const flat = s.replace(/\s+/g, ' ');
    return flat.length > n ? `${flat.slice(0, n).trimEnd()}…` : flat;
  }

  // --- Prioritisation scoring criteria (PO-003/008) --------------------------------
  let criteria = $state([]);
  let critFormOpen = $state(false);
  /** null = create mode, otherwise the criterion id being edited. */
  let editingCritId = $state(null);
  let savingCrit = $state(false);
  let critError = $state('');

  function emptyCrit() {
    return { name: '', weight: '1', direction: 'higher_better', description: '' };
  }
  let critForm = $state(emptyCrit());

  async function loadCriteria() {
    try {
      criteria = (await p3mApi.scoringCriteria.getAll()) ?? [];
    } catch (e) {
      critError = e?.message || 'error';
    }
  }

  function toggleCritForm() {
    critFormOpen = !critFormOpen;
    if (critFormOpen) {
      editingCritId = null;
      critForm = emptyCrit();
      critError = '';
    }
  }

  function startEditCriterion(c) {
    critFormOpen = true;
    editingCritId = c.id;
    critForm = {
      name: c.name,
      weight: String(c.weight ?? 1),
      direction: c.direction,
      description: c.description ?? '',
    };
    critError = '';
  }

  async function submitCriterion() {
    if (savingCrit) return;
    critError = '';
    const payload = {
      name: critForm.name.trim(),
      weight: Number(critForm.weight),
      direction: critForm.direction,
      description: critForm.description || null,
    };
    if (!payload.name) {
      critError = t('p3m.criterionNameRequired');
      return;
    }
    savingCrit = true;
    try {
      if (editingCritId) await p3mApi.scoringCriteria.update(editingCritId, payload);
      else await p3mApi.scoringCriteria.create(payload);
      critFormOpen = false;
      editingCritId = null;
      await loadCriteria();
    } catch (e) {
      critError = e?.message || t('p3m.updateFailed');
    } finally {
      savingCrit = false;
    }
  }

  async function removeCriterion(c) {
    if (!confirm(t('p3m.confirmDeleteCriterion'))) return;
    try {
      await p3mApi.scoringCriteria.delete(c.id);
      await loadCriteria();
    } catch (e) {
      critError = e?.message || t('p3m.deleteFailed');
    }
  }

  // --- Score card per investment case (PO-004/008) ----------------------------------
  /** Case id whose score card is open, or null. */
  let scoringFor = $state(null);
  /** criterion_id -> { value: '1'..'5'|'', rationale } */
  let scoreDrafts = $state({});
  let savingScores = $state(false);
  let scoreError = $state('');

  async function openScorecard(c) {
    if (scoringFor === c.id) {
      scoringFor = null;
      return;
    }
    scoringFor = c.id;
    scoreDrafts = {};
    scoreError = '';
    try {
      const rows = (await p3mApi.getInvestmentCaseScores(c.id)) ?? [];
      for (const row of rows) {
        scoreDrafts[row.criterion_id] = { value: String(row.score?.score ?? ''), rationale: row.score?.rationale ?? '' };
      }
    } catch (e) {
      scoreError = e?.message || 'error';
    }
  }

  async function saveScores() {
    if (!scoringFor || savingScores) return;
    savingScores = true;
    scoreError = '';
    try {
      for (const crit of criteria.filter((c) => c.enabled)) {
        const draft = scoreDrafts[crit.id];
        if (!draft || draft.value === '') continue;
        await p3mApi.upsertInvestmentCaseScore(scoringFor, {
          criterion_id: crit.id,
          score: Number(draft.value),
          rationale: draft.rationale.trim() ? draft.rationale : null,
        });
      }
      scoringFor = null;
      await loadRanked();
    } catch (e) {
      scoreError = e?.message || 'error';
    } finally {
      savingScores = false;
    }
  }

  // --- Ranked view (PO-004) -----------------------------------------------------------
  let ranked = $state([]);

  async function loadRanked() {
    try {
      const filters = portfolioFilter !== '' ? { portfolio_id: Number(portfolioFilter) } : {};
      ranked = (await p3mApi.getRankedInvestmentCases(filters))?.ranking ?? [];
    } catch {
      ranked = [];
    }
  }

  // Re-rank when the portfolio filter changes.
  $effect(() => {
    const f = portfolioFilter;
    void f;
    loadRanked();
  });

  // --- Portfolio roadmap (PO-005) ------------------------------------------------------
  let roadmapPf = $state('');
  let roadmapProjects = $state([]);

  async function loadRoadmap() {
    if (!roadmapPf) {
      roadmapProjects = [];
      return;
    }
    try {
      roadmapProjects = (await p3mApi.getPortfolioRoadmap(Number(roadmapPf)))?.projects ?? [];
    } catch {
      roadmapProjects = [];
    }
  }

  onMount(() => {
    load();
    loadCriteria();
    if (!roadmapPf && portfolios.length > 0) {
      roadmapPf = String(portfolios[0].id);
      loadRoadmap();
    }
  });
</script>

{#if loading && cases.length === 0}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && cases.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.investmentCases')}</h2>
        <div class="flex items-center gap-2">
          {#if projectsForNewCase.length > 0}
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleForm}>
              <IconPlus size={14} class="inline" /> {t('p3m.newInvestmentCase')}
            </button>
          {/if}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => downloadExport('investment_cases')}>
            <IconDownload size={14} class="inline" /> {t('p3m.exportCsv')}
          </button>
        </div>
      </div>

      <!-- Portfolio filter + status chips -->
      <div class="mb-2 flex items-center gap-2 text-xs">
        <select value={portfolioFilter} onchange={(e) => (portfolioFilter = e.target.value)} class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1">
          <option value="">—</option>
          {#each portfolios as pf (pf.id)}
            <option value={String(pf.id)}>{pf.title}</option>
          {/each}
        </select>
        {#each STATUSES as s (s)}
          {@const active = statusFilter === s}
          <button type="button" class="rounded-full border px-2 py-0.5 text-[11px] disabled:opacity-40" class:border-transparent={active} class:bg-slate-100={!active} onclick={() => (statusFilter = active ? 'all' : s)}>
            {statusLabel(s)} ({cases.filter((c) => c.status === s).length})
          </button>
        {/each}
      </div>

      <!-- Inline create / edit form -->
      {#if formOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <h3 class="mb-2 font-semibold">{editingId ? t('p3m.editInvestmentCase') : t('p3m.newInvestmentCase')}</h3>

          {#if !editingId}
            <label class="block">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.projects')}</span>
              <select class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.project_id} disabled={saving} onchange={(e) => (form = { ...form, project_id: e.target.value })}>
                <option value="">—</option>
                {#each projectsForNewCase as p (p.id)}
                  <option value={String(p.id)}>{p.title}</option>
                {/each}
              </select>
            </label>
          {:else}
            <div class="flex items-center gap-3">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{allProjects.find((p) => String(p.id) === form.project_id)?.title ?? `#${form.project_id}`}</span>
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.status} disabled={saving} onchange={(e) => (form = { ...form, status: e.target.value })}>
                {#each editableStatuses as s (s)}
                  <option value={s}>{statusLabel(s)}</option>
                {/each}
              </select>
            </div>
          {/if}

          <div class="mt-2 flex items-center gap-3">
            <label class="flex items-center gap-1.5">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.icBenefitScore')}</span>
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.benefit_score} disabled={saving} onchange={(e) => (form = { ...form, benefit_score: e.target.value })}>
                {#each [1, 2, 3, 4, 5] as n (n)}
                  <option value={String(n)}>{n}</option>
                {/each}
              </select>
            </label>
            <label class="flex items-center gap-1.5">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.icRiskScore')}</span>
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.risk_score} disabled={saving} onchange={(e) => (form = { ...form, risk_score: e.target.value })}>
                {#each [1, 2, 3, 4, 5] as n (n)}
                  <option value={String(n)}>{n}</option>
                {/each}
              </select>
            </label>
            <label class="flex items-center gap-1.5">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.roiPercent')}</span>
              <input type="number" step="0.1" class="w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.roi_percent} disabled={saving} oninput={(e) => (form = { ...form, roi_percent: e.target.value })} />
            </label>
            <label class="flex items-center gap-1.5">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.paybackMonths')}</span>
              <input type="number" min="0" step="1" class="w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={form.payback_months} disabled={saving} oninput={(e) => (form = { ...form, payback_months: e.target.value })} />
            </label>
          </div>

          <label class="mt-2 block">
            <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.businessCase')}</span>
            <textarea rows="4" class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" placeholder={t('p3m.businessCase')} value={form.business_case} disabled={saving} oninput={(e) => (form = { ...form, business_case: e.target.value })}></textarea>
          </label>

          {#if formError}
            <div class="mt-2 text-red-600">{formError}</div>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={saving} onclick={submitForm}>
              {editingId ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      <!-- Decision confirm panel -->
      {#if decision}
        {@const decCase = cases.find((c) => c.id === decision.id)}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <h3 class="mb-1 font-semibold">
            {statusLabel(decision.outcome)} — {decCase?.project_title ?? `#${decision.id}`}
          </h3>
          <textarea rows="2" class="block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" placeholder={t('p3m.decisionNote')} value={decisionNote} disabled={deciding} oninput={(e) => (decisionNote = e.target.value)}></textarea>
          <div class="mt-2 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={deciding} onclick={confirmDecision}>
              {decision.outcome === 'approved' ? t('p3m.approve') : decision.outcome === 'rejected' ? t('p3m.reject') : t('p3m.defer')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" disabled={deciding} onclick={() => (decision = null)}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      <!-- Score card for one investment case (PO-004) -->
      {#if scoringFor != null}
        {@const scCase = cases.find((c) => c.id === scoringFor)}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <h3 class="mb-2 font-semibold">{t('p3m.scoreCase')}: {scCase?.project_title ?? `#${scoringFor}`}</h3>

          {#if scoreError}
            <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{scoreError}</div>
          {/if}

          {#if criteria.filter((c) => c.enabled).length === 0}
            <div class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noCriteria')}</div>
          {:else}
            <table class="w-full text-left">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1 pr-3 font-medium">{t('p3m.name')}</th>
                  <th class="py-1 pr-3 font-medium">{t('p3m.weight')}</th>
                  <th class="py-1 pr-3 font-medium">{t('p3m.direction')}</th>
                  <th class="py-1 pr-3 font-medium">1–5</th>
                  <th class="py-1 font-medium">{t('p3m.rationale')}</th>
                </tr>
              </thead>
              <tbody>
                {#each criteria.filter((c) => c.enabled) as crit (crit.id)}
                  {@const draft = scoreDrafts[crit.id] ?? { value: '', rationale: '' }}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{crit.name}</td>
                    <td class="py-1.5 pr-3 tabular-nums">{crit.weight}</td>
                    <td class="py-1.5 pr-3 whitespace-nowrap">{t(crit.direction === 'lower_better' ? 'p3m.lowerBetter' : 'p3m.higherBetter')}</td>
                    <td class="py-1.5 pr-3">
                      <select
                        class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                        value={draft.value}
                        disabled={savingScores}
                        onchange={(e) => (scoreDrafts = { ...scoreDrafts, [crit.id]: { ...draft, value: e.target.value } })}
                      >
                        <option value="">—</option>
                        {#each [1, 2, 3, 4, 5] as n (n)}
                          <option value={String(n)}>{n}</option>
                        {/each}
                      </select>
                    </td>
                    <td class="py-1.5">
                      <input
                        type="text"
                        class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                        placeholder={t('p3m.rationale')}
                        value={draft.rationale}
                        disabled={savingScores}
                        oninput={(e) => (scoreDrafts = { ...scoreDrafts, [crit.id]: { ...draft, rationale: e.target.value } })}
                      />
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingScores} onclick={saveScores}>
              {t('p3m.save')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (scoringFor = null)}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      <!-- Pipeline table grouped by status -->
      {#if grouped.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noInvestmentCases')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.portfolios')}</th>
              <th class="py-1.5 pr-3">{t('p3m.icBenefitScore')}</th>
              <th class="py-1.5 pr-3">{t('p3m.icRiskScore')}</th>
              <th class="py-1.5 pr-3">{t('p3m.roiPercent')}</th>
              <th class="py-1.5 pr-3">{t('p3m.paybackMonths')}</th>
              <th class="py-1.5 pr-3">{t('p3m.businessCase')}</th>
              <th class="py-1.5 pr-3">{t('p3m.decidedBy')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each grouped as g (g.status)}
              <tr class="bg-ds-surface-hovered">
                <td colspan="9" class="py-1 pr-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
                  {statusLabel(g.status)} · {g.items.length}
                </td>
              </tr>
              {#each g.items as c (c.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                  <td class="py-1.5 pr-3">{c.project_title || `#${c.project_id}`}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{c.portfolio_title || t('p3m.notSet')}</td>
                  <td class="py-1.5 pr-3">{c.benefit_score}/5</td>
                  <td class="py-1.5 pr-3">{c.risk_score}/5</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{c.roi_percent == null ? t('p3m.notSet') : `${c.roi_percent}%`}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">{c.payback_months ?? t('p3m.notSet')}</td>
                  <td class="max-w-[280px] truncate py-1.5 pr-3" title={c.business_case}>{excerpt(c.business_case) || t('p3m.notSet')}</td>
                  <td class="py-1.5 pr-3 whitespace-nowrap">
                    {c.decided_by_name || t('p3m.notSet')}
                    {#if c.decision_date}
                      <div class="text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(c.decision_date)}</div>
                    {/if}
                  </td>
                  <td class="py-1.5 pr-3">
                    <div class="flex items-center gap-1.5 whitespace-nowrap">
                      {#each STATUSES.filter((s) => s !== c.status && (c.status === 'proposed' || c.status === 'under_review')) as outcome (outcome)}
                        <button type="button" class="rounded-md border px-2 py-0.5 font-medium disabled:opacity-40" class:border-emerald-300={outcome === 'approved'} class:text-emerald-600={outcome === 'approved'} class:hover:bg-emerald-50={outcome === 'approved'} class:border-red-300={outcome === 'rejected'} class:text-red-600={outcome === 'rejected'} class:hover:bg-red-50={outcome === 'rejected'} class:border-purple-300={outcome === 'deferred'} class:text-purple-600={outcome === 'deferred'} class:hover:bg-purple-50={outcome === 'deferred'} onclick={() => openDecision(c, outcome)}>
                          {statusLabel(outcome)}
                        </button>
                      {/each}
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-accent-blue,#2563eb)]" onclick={() => openScorecard(c)} aria-label={t('p3m.scoreCase')}>
                        <IconStar size={13} />
                      </button>
                      <button type="button" class="rounded-md border px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-accent-blue,#2563eb)]" class:border-[var(--color-ds-border,#e5e7eb)]={historyFor !== c.id} class:border-[var(--color-ds-accent-blue,#2563eb)]={historyFor === c.id} onclick={() => toggleHistory(c)} aria-label={t('p3m.scoreHistory')}>
                        <IconClock size={13} />
                      </button>
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => startEdit(c)} aria-label={t('p3m.edit')}>
                        <IconPencil size={13} />
                      </button>
                      <button type="button" class="rounded-md border border-red-300 px-2 py-0.5 text-red-600 hover:bg-red-50" onclick={() => removeCase(c)} aria-label={t('p3m.delete')}>
                        <IconX size={13} />
                      </button>
                    </div>
                  </td>
                </tr>
                {#if historyFor === c.id}
                  <tr class="bg-ds-surface-hovered">
                    <td colspan="9" class="py-2 pr-3 text-[var(--color-ds-text-muted,#6b7280)]" data-testid={`p3m-ic-history-${c.id}`}>
                      {#if historyLoadingId === c.id}
                        {t('p3m.loading')}
                      {:else if (histories[c.id] ?? []).length === 0}
                        {t('p3m.noScoreHistory')}
                      {:else}
                        <ol class="space-y-1">
                          {#each groupedHistory(histories[c.id]) as grp (grp.at)}
                            <li class="flex flex-wrap items-baseline gap-x-2">
                              <span class="whitespace-nowrap">{formatDate(grp.at)}</span>
                              {#if grp.user}
                                <span class="text-[11px]">· {grp.user}</span>
                              {/if}
                              <span>{grp.items.map((e) => `${e.criterion ?? e.raw}: ${e.score != null ? e.score : '?'}`).join('  ·  ')}</span>
                            </li>
                          {/each}
                        </ol>
                      {/if}
                    </td>
                  </tr>
                {/if}
              {/each}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Scoring criteria management (PO-003) -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.scoringCriteriaTitle')}</h2>
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleCritForm}>
          {#if critFormOpen}
            {t('p3m.cancel')}
          {:else}
            <IconPlus size={14} class="inline" /> {t('p3m.newCriterion')}
          {/if}
        </button>
      </div>

      {#if critError && !critFormOpen}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{critError}</div>
      {/if}

      {#if critFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <h3 class="mb-2 font-semibold">{editingCritId ? t('p3m.edit') : t('p3m.newCriterion')}</h3>

          <div class="flex flex-wrap items-end gap-x-4 gap-y-2">
            <label class="block min-w-[180px] flex-1">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.name')} *</span>
              <input type="text" class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={critForm.name} disabled={savingCrit} oninput={(e) => (critForm = { ...critForm, name: e.target.value })} />
            </label>
            <label class="block w-28">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.weight')}</span>
              <input type="number" step="0.1" min="0" class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={critForm.weight} disabled={savingCrit} oninput={(e) => (critForm = { ...critForm, weight: e.target.value })} />
            </label>
            <label class="block">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.direction')}</span>
              <select class="mt-1 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={critForm.direction} disabled={savingCrit} onchange={(e) => (critForm = { ...critForm, direction: e.target.value })}>
                <option value="higher_better">{t('p3m.higherBetter')}</option>
                <option value="lower_better">{t('p3m.lowerBetter')}</option>
              </select>
            </label>
            <label class="block min-w-[200px] flex-1">
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.description')}</span>
              <input type="text" class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={critForm.description} disabled={savingCrit} oninput={(e) => (critForm = { ...critForm, description: e.target.value })} />
            </label>
          </div>

          {#if critError}
            <div class="mt-2 text-red-600">{critError}</div>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingCrit} onclick={submitCriterion}>
              {editingCritId ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleCritForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      {#if criteria.length === 0 && !critFormOpen}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noCriteria')}
        </div>
      {:else if criteria.length > 0}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.name')}</th>
              <th class="py-1.5 pr-3">{t('p3m.weight')}</th>
              <th class="py-1.5 pr-3">{t('p3m.direction')}</th>
              <th class="py-1.5 pr-3">{t('p3m.description')}</th>
              <th class="py-1.5 pr-3">{t('p3m.enabled')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each criteria as crit (crit.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{crit.name}</td>
                <td class="py-1.5 pr-3 tabular-nums">{crit.weight}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{t(crit.direction === 'lower_better' ? 'p3m.lowerBetter' : 'p3m.higherBetter')}</td>
                <td class="max-w-[280px] truncate py-1.5 pr-3" title={crit.description}>{crit.description || t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3">
                  {#if crit.enabled}
                    <span class="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700">{t('p3m.yes')}</span>
                  {:else}
                    <span class="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium text-slate-600">{t('p3m.no')}</span>
                  {/if}
                </td>
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1.5 whitespace-nowrap">
                    <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => startEditCriterion(crit)} aria-label={t('p3m.edit')}>
                      <IconPencil size={13} />
                    </button>
                    <button type="button" class="rounded-md border border-red-300 px-2 py-0.5 text-red-600 hover:bg-red-50" onclick={() => removeCriterion(crit)} aria-label={t('p3m.delete')}>
                      <IconX size={13} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Ranked investment cases (PO-004) -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.rankedView')}</h2>
      {#if ranked.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noInvestmentCases')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.rank')}</th>
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.status')}</th>
              <th class="py-1.5 pr-3 text-right">{t('p3m.compositeScore')}</th>
              <th class="py-1.5 pr-3 text-right">{t('p3m.scoredCriteria')}</th>
            </tr>
          </thead>
          <tbody>
            {#each ranked as row (row.ic_id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3 tabular-nums">{row.rank}</td>
                <td class="py-1.5 pr-3">{row.title || `#${row.project_id}`}</td>
                <td class="py-1.5 pr-3">
                  <span class={STATUS_BADGES[row.status] ?? 'bg-slate-100 text-slate-600'}>{statusLabel(row.status)}</span>
                </td>
                <td class="py-1.5 pr-3 text-right tabular-nums">{row.composite_score == null ? t('p3m.notSet') : row.composite_score}</td>
                <td class="py-1.5 pr-3 text-right tabular-nums">{row.scored_criteria}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Portfolio roadmap (PO-005) -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.roadmapTitle')}</h2>
        <select value={roadmapPf} onchange={(e) => { roadmapPf = e.target.value; loadRoadmap(); }} class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs">
          <option value="">{t('p3m.selectPortfolio')}</option>
          {#each portfolios as pf (pf.id)}
            <option value={String(pf.id)}>{pf.title}</option>
          {/each}
        </select>
      </div>

      {#if !roadmapPf}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.selectPortfolio')}
        </div>
      {:else if roadmapProjects.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noRoadmapProjects')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.startDate')}</th>
              <th class="py-1.5 pr-3">{t('p3m.targetCompletionDate')}</th>
              <th class="py-1.5 pr-3 w-40">%</th>
              <th class="py-1.5 pr-3">{t('p3m.ragStatus')}</th>
            </tr>
          </thead>
          <tbody>
            {#each roadmapProjects as rp (rp.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{rp.title}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{rp.start_date ? formatDate(rp.start_date) : t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{rp.target_completion_date ? formatDate(rp.target_completion_date) : t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-2">
                    <span class="inline-block h-1.5 w-24 overflow-hidden rounded bg-slate-200">
                      <span class="block h-full rounded" style="width: {rp.progress_percent}%; background: var(--color-ds-accent-blue,#2563eb)"></span>
                    </span>
                    <span class="tabular-nums">{rp.progress_percent}%</span>
                  </div>
                </td>
                <td class="py-1.5 pr-3">
                  <span class="inline-block h-2.5 w-2.5 rounded-full" style="background: {ragColor(rp.rag_status)}"></span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
