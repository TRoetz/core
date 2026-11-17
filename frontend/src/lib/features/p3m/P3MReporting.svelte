<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';

  let { portfolios = [] } = $props();

  // --- Entity option list (flat) + name resolution map, built from the tree ---
  const entityOptions = $derived(
    (portfolios || []).flatMap((p) => {
      const opts = [{ value: `portfolio:${p.id}`, label: p.title, type: 'portfolio', id: p.id }];
      for (const prog of p.programmes ?? []) {
        opts.push({ value: `programme:${prog.id}`, label: `— ${prog.title}`, type: 'programme', id: prog.id });
        for (const proj of prog.projects ?? []) {
          opts.push({ value: `project:${proj.id}`, label: `    — ${proj.title}`, type: 'project', id: proj.id });
        }
        for (const sp of prog.standalone_projects ?? []) {
          opts.push({ value: `project:${sp.id}`, label: `    — ${sp.title}`, type: 'project', id: sp.id });
        }
      }
      return opts;
    })
  );

  const entityNameMap = $derived(
    Object.fromEntries(entityOptions.map((o) => [o.value, o.label.trim()]))
  );

  // --- Pure helpers ---
  function currentPeriod() {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
  }

  function ragColor(rag) {
    switch (rag) {
      case 'green': return '#10b981';
      case 'amber': return '#f59e0b';
      case 'red': return '#ef4444';
      default: return '#6b7280';
    }
  }

  const CATEGORIES = ['schedule', 'budget', 'scope', 'risk', 'benefits', 'change'];

  function jparse(s) {
    try { return s ? JSON.parse(s) : null; } catch { return null; }
  }

  // --- State ---
  let entityKey = $state('');
  const selectedEntity = $derived(entityOptions.find((o) => o.value === entityKey) ?? null);

  let loadingSuggest = $state(false);
  let suggested = $state(null);   // {schedule,budget,scope,risk,benefits,change}
  let tolerance = $state(null);   // {schedule_days,budget_pct,risk_score} in force
  let snapshots = $state([]);
  let loadingSnapshots = $state(false);

  const emptyForm = () => ({
    period: currentPeriod(),
    overall_rag: 'green',
    category_rags: Object.fromEntries(CATEGORIES.map((c) => [c, 'green'])),
    rag_override_reasons: {},
    commentary: '',
    escalations: '',
  });
  let formOpen = $state(false);
  let form = $state(emptyForm());
  let saving = $state(false);
  let error = $state('');

  // Tolerance profile editor (scope portfolio|project only)
  let tolProfileId = $state(null);
  const emptyTolForm = () => ({ schedule_days: '14', budget_pct: '10', risk_score: '3' });
  let tolFormOpen = $state(false);
  let tolForm = $state(emptyTolForm());
  let tolSaving = $state(false);

  // Expanded snapshot row (metrics / overrides detail)
  let expandedId = $state(null);

  const loadSuggest = async () => {
    if (!selectedEntity) return;
    loadingSuggest = true;
    error = '';
    try {
      const res = await p3mApi.suggestCategoryRags({ entity_type: selectedEntity.type, entity_id: selectedEntity.id });
      suggested = res?.suggested ?? null;
      tolerance = res?.tolerance ?? null;
      // Pre-fill the capture form with the live suggestions.
      if (formOpen) {
        form.category_rags = Object.fromEntries(CATEGORIES.map((c) => [c, suggested?.[c] || 'green']));
        form.overall_rag = CATEGORIES.reduce((worst, c) => {
          const rank = { green: 0, amber: 1, red: 2 };
          return (rank[suggested?.[c]] ?? 0) > (rank[worst] ?? 0) ? suggested[c] : worst;
        }, 'green');
      }
    } catch {
      error = t('p3m.loadFailed');
    } finally {
      loadingSuggest = false;
    }
  };

  const loadSnapshots = async () => {
    if (!selectedEntity) return;
    loadingSnapshots = true;
    try {
      const rows = await p3mApi.listReportSnapshots({ entity_type: selectedEntity.type, entity_id: selectedEntity.id });
      snapshots = Array.isArray(rows) ? rows : [];
      // Remember the existing tolerance profile id for this scope/entity.
      if (selectedEntity.type !== 'programme') {
        const profiles = await p3mApi.toleranceProfiles.getAll({ scope: selectedEntity.type, entity_id: selectedEntity.id });
        tolProfileId = Array.isArray(profiles) && profiles.length ? profiles[0].id : null;
      } else {
        tolProfileId = null;
      }
    } catch {
      snapshots = [];
    } finally {
      loadingSnapshots = false;
    }
  };

  $effect(() => {
    // Re-run whenever the selected entity changes.
    const key = entityKey;
    if (!key) return;
    expandedId = null;
    void loadSuggest();
    void loadSnapshots();
  });

  const openCapture = () => {
    form = emptyForm();
    if (suggested) {
      form.category_rags = Object.fromEntries(CATEGORIES.map((c) => [c, suggested[c] || 'green']));
      form.overall_rag = CATEGORIES.reduce((worst, c) => {
        const rank = { green: 0, amber: 1, red: 2 };
        return (rank[suggested?.[c]] ?? 0) > (rank[worst] ?? 0) ? suggested[c] : worst;
      }, 'green');
    }
    formOpen = true;
    error = '';
  };

  const overrideNeeded = (cat) =>
    form.category_rags[cat] !== (suggested?.[cat] || 'green') && !form.rag_override_reasons[cat];

  const saveSnapshot = async () => {
    saving = true;
    error = '';
    try {
      const reasons = {};
      for (const c of CATEGORIES) {
        if (form.category_rags[c] !== (suggested?.[c] || 'green') && form.rag_override_reasons[c]?.trim()) {
          reasons[c] = form.rag_override_reasons[c].trim();
        }
      }
      const escLines = form.escalations.split('\n').map((s) => s.trim()).filter(Boolean);
      await p3mApi.createReportSnapshot(
        {
          period: form.period,
          overall_rag: form.overall_rag,
          category_rags: form.category_rags,
          rag_override_reasons: Object.keys(reasons).length ? reasons : undefined,
          commentary: form.commentary || null,
          escalations: escLines.length ? JSON.stringify(escLines) : null,
        },
        { entity_type: selectedEntity.type, entity_id: selectedEntity.id }
      );
      formOpen = false;
      await Promise.all([loadSuggest(), loadSnapshots()]);
    } catch (e) {
      error = e?.message || t('p3m.saveFailed');
    } finally {
      saving = false;
    }
  };

  const openTolForm = () => {
    if (!tolProfileId && tolerance) {
      tolForm = {
        schedule_days: String(tolerance.schedule_days ?? 14),
        budget_pct: String(tolerance.budget_pct ?? 10),
        risk_score: String(tolerance.risk_score ?? 3),
      };
    } else if (tolProfileId) {
      // Values already shown in the "in force" panel; reuse them.
      tolForm = {
        schedule_days: String(tolerance?.schedule_days ?? 14),
        budget_pct: String(tolerance?.budget_pct ?? 10),
        risk_score: String(tolerance?.risk_score ?? 3),
      };
    } else {
      tolForm = emptyTolForm();
    }
    tolFormOpen = true;
    error = '';
  };

  const saveTolerance = async () => {
    tolSaving = true;
    error = '';
    try {
      const data = {
        scope: selectedEntity.type,
        entity_id: selectedEntity.id,
        schedule_days: Number(tolForm.schedule_days) || 0,
        budget_pct: Number(tolForm.budget_pct) || 0,
        risk_score: Number(tolForm.risk_score) || 1,
      };
      if (tolProfileId) {
        await p3mApi.toleranceProfiles.update(tolProfileId, data);
      } else {
        const created = await p3mApi.toleranceProfiles.create(data);
        tolProfileId = created?.id ?? null;
      }
      tolFormOpen = false;
      await loadSuggest();
    } catch (e) {
      error = e?.message || t('p3m.saveFailed');
    } finally {
      tolSaving = false;
    }
  };

  const deleteTolerance = async () => {
    if (!tolProfileId) return;
    try {
      await p3mApi.toleranceProfiles.delete(tolProfileId);
      tolProfileId = null;
      await loadSuggest();
    } catch (e) {
      error = e?.message || t('p3m.saveFailed');
    }
  };

  const snapshotCategoryRags = (s) => jparse(s.category_rags) ?? {};
  const snapshotOverrides = (s) => jparse(s.rag_override_reasons) ?? {};
  const snapshotEscalations = (s) => { const v = jparse(s.escalations); return Array.isArray(v) ? v : []; };
  const snapshotMetrics = (s) => jparse(s.metrics) ?? {};

  const fmtDate = (iso) => {
    if (!iso) return '';
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  };
</script>

<section class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <h3 class="text-sm font-semibold">{t('p3m.reportingTitle')}</h3>
    {#if selectedEntity}
      <button onclick={openCapture} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">
        {t('p3m.captureSnapshot')}
      </button>
    {/if}
  </div>

  <label class="mb-1 block text-xs font-medium" for="reporting-entity">{t('p3m.selectEntity')}</label>
  <select id="reporting-entity" bind:value={entityKey} class="w-full max-w-md rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
    <option value="">—</option>
    {#each entityOptions as o (o.value)}
      <option value={o.value}>{o.label}</option>
    {/each}
  </select>

  {#if error && !formOpen}
    <p class="mt-2 text-xs" style="color:#ef4444">{error}</p>
  {/if}

  {#if selectedEntity}
    <!-- Suggested RAGs + tolerance in force -->
    <div class="mt-4 grid gap-3 sm:grid-cols-2">
      <div class="rounded border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <p class="mb-2 text-xs font-semibold">{t('p3m.suggestedRags')}</p>
        {#if loadingSuggest}
          <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</p>
        {:else if suggested}
          <div class="flex flex-wrap gap-1.5">
            {#each CATEGORIES as cat (cat)}
              <span
                class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
                style="background-color:{ragColor(suggested[cat])}22; color:{ragColor(suggested[cat])}"
              >
                {t(`p3m.cat_${cat}`)}: {suggested[cat]}
              </span>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">—</p>
        {/if}
      </div>

      <div class="rounded border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <div class="mb-2 flex items-center justify-between">
          <p class="text-xs font-semibold">{t('p3m.toleranceInForce')}</p>
          {#if selectedEntity.type !== 'programme'}
            <button onclick={tolFormOpen ? () => (tolFormOpen = false) : openTolForm} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs">
              {t('p3m.edit')}
            </button>
          {/if}
        </div>
        {#if tolerance}
          <dl class="grid grid-cols-3 gap-x-4 gap-y-1 text-xs">
            <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.scheduleTolerance')}</dt>
            <dd>{tolerance.schedule_days} {t('p3m.days')}</dd>
            <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.budgetTolerance')}</dt>
            <dd>{tolerance.budget_pct}%</dd>
            <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.riskThreshold')}</dt>
            <dd>≥ {tolerance.risk_score}</dd>
          </dl>
        {:else if !loadingSuggest}
          <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">—</p>
        {/if}

        {#if tolFormOpen && selectedEntity.type !== 'programme'}
          <form onsubmit={(e) => { e.preventDefault(); saveTolerance(); }} class="mt-3 space-y-2 border-t border-[var(--color-ds-border,#e5e7eb)] pt-3">
            <div class="grid grid-cols-3 gap-2">
              <label class="text-xs">
                {t('p3m.scheduleTolerance')} ({t('p3m.days')})
                <input type="number" min="0" bind:value={tolForm.schedule_days} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
              </label>
              <label class="text-xs">
                {t('p3m.budgetTolerance')} (%)
                <input type="number" min="0" step="0.5" bind:value={tolForm.budget_pct} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
              </label>
              <label class="text-xs">
                {t('p3m.riskThreshold')} (1–5)
                <input type="number" min="1" max="5" bind:value={tolForm.risk_score} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
              </label>
            </div>
            <div class="flex gap-2">
              <button type="submit" disabled={tolSaving} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-xs font-medium text-white">{t('p3m.save')}</button>
              {#if tolProfileId}
                <button type="button" onclick={deleteTolerance} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" style="color:#ef4444">{t('p3m.delete')}</button>
              {/if}
            </div>
          </form>
        {/if}
      </div>
    </div>

    <!-- Snapshot capture form -->
    {#if formOpen}
      <form onsubmit={(e) => { e.preventDefault(); saveSnapshot(); }} class="mt-4 space-y-3 rounded border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <div class="grid gap-2 sm:grid-cols-2">
          <label class="text-xs font-medium">
            {t('p3m.period')}
            <input type="month" required bind:value={form.period} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
          </label>
          <label class="text-xs font-medium">
            {t('p3m.overallRag')}
            <select bind:value={form.overall_rag} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
              <option value="green">Green</option>
              <option value="amber">Amber</option>
              <option value="red">Red</option>
            </select>
          </label>
        </div>

        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
          {#each CATEGORIES as cat (cat)}
            <div>
              <label class="text-xs font-medium" for={`rep-cat-${cat}`}>
                {t(`p3m.cat_${cat}`)}
                {#if suggested && form.category_rags[cat] !== suggested[cat]}
                  <span style="color:#f59e0b"> (≠ {suggested[cat]})</span>
                {/if}
              </label>
              <select id={`rep-cat-${cat}`} bind:value={form.category_rags[cat]} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
                <option value="green">Green</option>
                <option value="amber">Amber</option>
                <option value="red">Red</option>
              </select>
            </div>
          {/each}
        </div>

        {#if CATEGORIES.some((c) => overrideNeeded(c))}
          <p class="text-xs" style="color:#f59e0b">{t('p3m.overrideReasonRequired')}</p>
        {/if}
        {#each CATEGORIES.filter((c) => form.category_rags[c] !== (suggested?.[c] || 'green')) as cat (cat)}
          <label class="block text-xs font-medium">
            {t('p3m.overrideReason')} — {t(`p3m.cat_${cat}`)}
            <input bind:value={form.rag_override_reasons[cat]} placeholder={t('p3m.overrideReasonPlaceholder')} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
          </label>
        {/each}

        <div class="grid gap-2 sm:grid-cols-2">
          <label class="text-xs font-medium">
            {t('p3m.commentary')}
            <textarea rows="2" bind:value={form.commentary} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
          </label>
          <label class="text-xs font-medium">
            {t('p3m.escalations')}
            <textarea rows="2" bind:value={form.escalations} placeholder={t('p3m.onePerLine')} class="mt-1 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
          </label>
        </div>

        {#if error && formOpen}
          <p class="text-xs" style="color:#ef4444">{error}</p>
        {/if}

        <div class="flex gap-2">
          <button type="submit" disabled={saving || CATEGORIES.some((c) => overrideNeeded(c))} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">{t('p3m.save')}</button>
          <button type="button" onclick={() => (formOpen = false)} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">{t('p3m.cancel')}</button>
        </div>
      </form>
    {/if}

    <!-- Snapshot history -->
    <div class="mt-4">
      <p class="mb-2 text-xs font-semibold">{t('p3m.snapshotHistory')}</p>
      {#if loadingSnapshots}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</p>
      {:else if snapshots.length === 0}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noSnapshots')}</p>
      {:else}
        <table class="w-full max-w-none text-left text-sm">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.period')}</th>
              <th class="py-1.5 pr-3">{t('p3m.overallRag')}</th>
              <th class="py-1.5 pr-3">{t('p3m.categories')}</th>
              <th class="py-1.5 pr-3">{t('p3m.commentary')}</th>
              <th class="py-1.5">{t('p3m.capturedAt')}</th>
            </tr>
          </thead>
          <tbody>
            {#each snapshots as s (s.id)}
              <tr class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)]" onclick={() => (expandedId = expandedId === s.id ? null : s.id)}>
                <td class="py-1.5 pr-3 text-xs">{s.period}</td>
                <td class="py-1.5 pr-3">
                  <span class="inline-block rounded-full px-2 py-0.5 text-xs font-semibold" style="background-color:{ragColor(s.overall_rag)}; color:#fff">{s.overall_rag}</span>
                </td>
                <td class="py-1.5 pr-3">
                  <div class="flex flex-wrap gap-1">
                    {#each CATEGORIES as cat (cat)}
                      <span class="inline-block h-2.5 w-2.5 rounded-full" title={`${t(`p3m.cat_${cat}`)}: ${snapshotCategoryRags(s)[cat] ?? '—'}`} style="background-color:{ragColor(snapshotCategoryRags(s)[cat])}"></span>
                    {/each}
                  </div>
                </td>
                <td class="max-w-56 truncate py-1.5 pr-3 text-xs" title={s.commentary ?? ''}>{s.commentary || '—'}</td>
                <td class="py-1.5 text-xs">{fmtDate(s.captured_at)}</td>
              </tr>
              {#if expandedId === s.id}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#f9fafb)]">
                  <td colspan="5" class="p-3 text-xs">
                    {#if Object.keys(snapshotOverrides(s)).length}
                      <p class="mb-1 font-semibold">{t('p3m.overrideReasons')}</p>
                      <ul class="mb-2 list-disc pl-4">
                        {#each Object.entries(snapshotOverrides(s)) as [cat, reason] (cat)}
                          <li><span style="color:#f59e0b">{t(`p3m.cat_${cat}`)}</span>: {reason}</li>
                        {/each}
                      </ul>
                    {/if}
                    {#if snapshotEscalations(s).length}
                      <p class="mb-1 font-semibold">{t('p3m.escalations')}</p>
                      <ul class="mb-2 list-disc pl-4">
                        {#each snapshotEscalations(s) as esc (esc)}
                          <li>{esc}</li>
                        {/each}
                      </ul>
                    {/if}
                    {#if Object.keys(snapshotMetrics(s)).length}
                      <p class="mb-1 font-semibold">{t('p3m.metrics')}</p>
                      <dl class="grid grid-cols-2 gap-x-4 gap-y-0.5 sm:grid-cols-3">
                        {#each Object.entries(snapshotMetrics(s)) as [k, v] (k)}
                          <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{k}</dt>
                          <dd>{typeof v === 'object' ? JSON.stringify(v) : String(v)}</dd>
                        {/each}
                      </dl>
                    {/if}
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {:else}
    <p class="mt-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.selectEntityHint')}</p>
  {/if}
</section>
