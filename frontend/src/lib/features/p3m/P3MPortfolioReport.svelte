<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { isSystemAdmin } from '../../stores/permissions.svelte.js';

  let { portfolios = [] } = $props();

  // --- Constants ---------------------------------------------------------------
  const LENS_KEYS = [
    'delivery_status',
    'resource_capacity',
    'change_capacity',
    'planning_pipeline',
    'financial_visibility',
  ];
  const REF_CATEGORIES = ['business_group', 'theme', 'cso', 'benefit_category'];
  const POSTURES = ['proceed', 'caution', 'pause', 'reduce'];

  // --- Pure helpers -------------------------------------------------------------
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

  const str = (v) => (v == null ? '' : v);
  const num = (v) => (v == null ? '' : v);

  function impactColor(level) {
    switch (level) {
      case 0: return '#6b7280';
      case 1: return '#10b981';
      case 2: return '#a3e635';
      case 3: return '#f59e0b';
      case 4: return '#f97316';
      case 5: return '#ef4444';
      default: return '#6b7280';
    }
  }

  // --- State --------------------------------------------------------------------
  let portfolioId = $state('');
  let asOf = $state(currentPeriod());
  let closedSince = $state('');
  let doc = $state(null);
  let loading = $state(false);
  let error = $state('');

  const selectedPortfolio = $derived(
    (portfolios || []).find((p) => String(p.id) === portfolioId) ?? null
  );
  const portfolioProjects = $derived(
    (selectedPortfolio?.programmes ?? []).flatMap((pg) =>
      (pg.projects ?? []).map((p) => ({ ...p, standalone: false }))
    )
  );

  async function loadReport() {
    if (!portfolioId) {
      doc = null;
      return;
    }
    loading = true;
    error = '';
    try {
      doc = await p3mApi.getPortfolioReport(Number(portfolioId), {
        asOf,
        closedSince: closedSince || undefined,
      });
    } catch (e) {
      doc = null;
      error = e?.message || t('p3m.loadFailed');
    } finally {
      loading = false;
    }
  }

  // --- Curated editor -------------------------------------------------------------
  let curatedOpen = $state(false);
  let curatedSaving = $state(false);
  let curatedError = $state('');
  const emptyCuratedForm = () => ({
    executive_commentary: '',
    delivery_outlook: '',
    amber_red_projects: '',
    business_readiness_change: '',
    delivery_confidence_pct: '',
    lenses: [{ key: '', position: '', rag: 'green' }],
    highlights: {},
    change_key_messages: '',
  });
  let cForm = $state(emptyCuratedForm());

  async function openCurated() {
    curatedOpen = true;
    curatedError = '';
    if (!portfolioId) return;
    cForm = emptyCuratedForm();
    try {
      const row = await p3mApi.getCuratedPortfolioReport(Number(portfolioId), { period: asOf });
      if (row) {
        cForm.executive_commentary = row.executive_commentary ?? '';
        cForm.delivery_outlook = row.key_pressures?.delivery_outlook ?? '';
        cForm.amber_red_projects = row.key_pressures?.amber_red_projects ?? '';
        cForm.business_readiness_change = row.key_pressures?.business_readiness_change ?? '';
        cForm.delivery_confidence_pct =
          row.delivery_confidence_pct == null ? '' : String(row.delivery_confidence_pct);
        cForm.lenses = (row.confidence_lenses ?? []).length
          ? row.confidence_lenses.map((l) => ({ ...l }))
          : [{ key: '', position: '', rag: 'green' }];
        cForm.highlights = {};
        for (const h of row.programme_highlights ?? []) {
          cForm.highlights[h.programme_id] = h.commentary ?? '';
        }
        cForm.change_key_messages = row.change_key_messages ?? '';
      }
    } catch (e) {
      if (!/404/.test(e?.message ?? '')) {
        curatedError = e?.message || t('p3m.loadFailed');
      }
    }
  }

  async function saveCurated() {
    if (!portfolioId) return;
    curatedSaving = true;
    curatedError = '';
    try {
      const lenses = cForm.lenses.filter((l) => l.key?.trim());
      const highlights = Object.entries(cForm.highlights)
        .filter(([, v]) => v?.trim())
        .map(([pid, commentary]) => ({ programme_id: Number(pid), commentary: commentary.trim() }));
      await p3mApi.upsertCuratedPortfolioReport(Number(portfolioId), {
        period: asOf,
        executive_commentary: cForm.executive_commentary,
        key_pressures: {
          delivery_outlook: cForm.delivery_outlook,
          amber_red_projects: cForm.amber_red_projects,
          business_readiness_change: cForm.business_readiness_change,
        },
        delivery_confidence_pct:
          cForm.delivery_confidence_pct === '' ? null : Number(cForm.delivery_confidence_pct),
        confidence_lenses: lenses,
        programme_highlights: highlights,
        change_key_messages: cForm.change_key_messages,
      });
      curatedOpen = false;
      await loadReport();
    } catch (e) {
      curatedError = e?.message || t('p3m.saveFailed');
    } finally {
      curatedSaving = false;
    }
  }

  // --- ELT decision coversheets -----------------------------------------------------
  const emptyEltForm = () => ({
    title: '',
    project_id: '',
    why: '',
    decision_requested: '',
    conditions_until_approved: '',
  });
  let eltFormOpen = $state(false);
  let eltEditingId = $state(null);
  let eltForm = $state(emptyEltForm());
  let eltSaving = $state(false);
  let eltError = $state('');

  function openEltCreate() {
    eltEditingId = null;
    eltForm = emptyEltForm();
    eltError = '';
    eltFormOpen = true;
  }

  function openEltEdit(row) {
    eltEditingId = row.id;
    eltForm = {
      title: row.title ?? '',
      project_id: portfolioProjects.find((p) => p.title === row.project_title)?.id ?? '',
      why: row.why ?? '',
      decision_requested: row.decision_requested ?? '',
      conditions_until_approved: row.conditions_until_approved ?? '',
    };
    eltError = '';
    eltFormOpen = true;
  }

  async function saveElt() {
    if (!portfolioId) return;
    eltSaving = true;
    eltError = '';
    try {
      const payload = {
        portfolio_id: Number(portfolioId),
        title: eltForm.title.trim(),
        project_id: eltForm.project_id ? Number(eltForm.project_id) : null,
        why: eltForm.why,
        decision_requested: eltForm.decision_requested,
        conditions_until_approved: eltForm.conditions_until_approved,
      };
      if (eltEditingId) {
        await p3mApi.updateEltDecision(eltEditingId, payload);
      } else {
        await p3mApi.createEltDecision(payload);
      }
      eltFormOpen = false;
      await loadReport();
    } catch (e) {
      eltError = e?.message || t('p3m.saveFailed');
    } finally {
      eltSaving = false;
    }
  }

  async function decideElt(id, status) {
    try {
      await p3mApi.decideEltDecision(id, { status });
      await loadReport();
    } catch (e) {
      error = e?.message || t('p3m.saveFailed');
    }
  }

  async function deleteElt(id) {
    if (!confirm(t('p3m.confirmDeleteEltDecision'))) return;
    try {
      await p3mApi.deleteEltDecision(id);
      await loadReport();
    } catch (e) {
      error = e?.message || t('p3m.saveFailed');
    }
  }

  // --- Reference data (admin) ---------------------------------------------------------
  let refOpen = $state(false);
  let refValues = $state({ business_group: [], theme: [], cso: [], benefit_category: [] });
  let refAdding = $state({ business_group: '', theme: '', cso: '', benefit_category: '' });
  let refError = $state('');

  async function loadRef() {
    if (!refOpen) return;
    refError = '';
    const next = {};
    for (const cat of REF_CATEGORIES) {
      try {
        const rows = await p3mApi.getReferenceValues({ category: cat });
        next[cat] = (Array.isArray(rows) ? rows : []).map((r) => r.value);
      } catch {
        next[cat] = [];
      }
    }
    refValues = next;
  }

  $effect(() => {
    if (refOpen) void loadRef();
  });

  function addRefValue(cat) {
    const v = refAdding[cat]?.trim();
    if (!v || refValues[cat].includes(v)) return;
    refValues[cat] = [...refValues[cat], v];
    refAdding[cat] = '';
  }

  async function saveRefCategory(cat) {
    refError = '';
    try {
      await p3mApi.replaceReferenceValues({ category: cat, values: refValues[cat] });
    } catch (e) {
      refError = e?.message || t('p3m.saveFailed');
    }
  }

  // --- Print view ----------------------------------------------------------------------
  function printUrl() {
    return `/p3m/portfolios/${portfolioId}/report/print?as_of=${encodeURIComponent(asOf)}`;
  }

  $effect(() => {
    if (portfolioId && asOf) {
      void loadReport();
    }
  });
</script>

<section class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
  <!-- Header: pickers + actions -->
  <div class="mb-4 flex flex-wrap items-end gap-3">
    <div>
      <label class="mb-1 block text-xs font-medium" for="ohpf-portfolio">{t('p3m.portfolio')}</label>
      <select
        id="ohpf-portfolio"
        bind:value={portfolioId}
        class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
      >
        <option value="">—</option>
        {#each portfolios ?? [] as pf (pf.id)}
          <option value={pf.id}>{pf.title}</option>
        {/each}
      </select>
    </div>
    <div>
      <label class="mb-1 block text-xs font-medium" for="ohpf-asof">{t('p3m.report.asOf')}</label>
      <input
        id="ohpf-asof"
        type="month"
        bind:value={asOf}
        class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
      />
    </div>
    <div>
      <label class="mb-1 block text-xs font-medium" for="ohpf-closed">
        {t('p3m.report.closedSince')}
      </label>
      <input
        id="ohpf-closed"
        type="date"
        bind:value={closedSince}
        class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
      />
    </div>
    <div class="ml-auto flex items-center gap-2">
      {#if portfolioId}
        <button
          onclick={openCurated}
          class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm font-medium"
        >
          {curatedOpen ? t('p3m.report.closeEditor') : t('p3m.report.curatedEditor')}
        </button>
        <button
          onclick={() => window.open(printUrl(), '_blank', 'noopener')}
          class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white"
        >
          {t('p3m.report.printPdf')}
        </button>
      {/if}
    </div>
  </div>

  {#if error}
    <p class="mb-3 text-xs" style="color:#ef4444">{error}</p>
  {/if}

  {#if !portfolioId}
    <p class="py-6 text-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
      {t('p3m.report.selectPortfolio')}
    </p>
  {:else if loading && !doc}
    <p class="py-6 text-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</p>
  {:else if doc}
    <!-- Curated editor -->
    {#if curatedOpen}
      <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
        <h4 class="mb-3 text-sm font-semibold">{t('p3m.report.curatedEditor')} — {asOf}</h4>
        <div class="grid gap-3 md:grid-cols-2">
          <div class="md:col-span-2">
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.executiveCommentary')}</label>
            <textarea
              bind:value={cForm.executive_commentary}
              rows="3"
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            ></textarea>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.keyPressureOutlook')}</label>
            <textarea
              bind:value={cForm.delivery_outlook}
              rows="3"
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            ></textarea>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.keyPressureAmberRed')}</label>
            <textarea
              bind:value={cForm.amber_red_projects}
              rows="3"
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            ></textarea>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.keyPressureReadiness')}</label>
            <textarea
              bind:value={cForm.business_readiness_change}
              rows="3"
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            ></textarea>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.confidencePct')}</label>
            <input
              type="number"
              min="0"
              max="100"
              bind:value={cForm.delivery_confidence_pct}
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            />
          </div>
        </div>

        <p class="mb-1 mt-4 text-xs font-medium">{t('p3m.report.confidenceLenses')}</p>
        {#each cForm.lenses as lens, li (li)}
          <div class="mb-1 flex flex-wrap items-center gap-2">
            <input
              list="ohpf-lens-keys"
              bind:value={lens.key}
              placeholder={t('p3m.report.lensKeyPlaceholder')}
              class="w-48 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            />
            <input
              bind:value={lens.position}
              placeholder={t('p3m.report.lensPositionPlaceholder')}
              class="min-w-40 flex-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            />
            <select
              bind:value={lens.rag}
              class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            >
              <option value="green">green</option>
              <option value="amber">amber</option>
              <option value="red">red</option>
            </select>
            <button
              onclick={() => (cForm.lenses = cForm.lenses.filter((_, i) => i !== li))}
              class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs"
              disabled={cForm.lenses.length === 1}
            >
              ×
            </button>
          </div>
        {/each}
        <datalist id="ohpf-lens-keys">
          {#each LENS_KEYS as k (k)}
            <option value={k} />
          {/each}
        </datalist>
        <button
          onclick={() => (cForm.lenses = [...cForm.lenses, { key: '', position: '', rag: 'green' }])}
          class="mt-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs"
        >
          + {t('p3m.report.addLens')}
        </button>

        {#if portfolioProjects.length}
          <p class="mb-1 mt-4 text-xs font-medium">{t('p3m.report.programmeHighlights')}</p>
          {#each (selectedPortfolio?.programmes ?? []) as pg (pg.id)}
            <div class="mb-2">
              <label class="mb-1 block text-xs">{pg.title}</label>
              <textarea
                bind:value={cForm.highlights[pg.id]}
                rows="2"
                class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
              ></textarea>
            </div>
          {/each}
        {/if}

        <div class="mt-3">
          <label class="mb-1 block text-xs font-medium">{t('p3m.report.changeKeyMessages')}</label>
          <textarea
            bind:value={cForm.change_key_messages}
            rows="2"
            class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
          ></textarea>
        </div>

        {#if curatedError}
          <p class="mt-2 text-xs" style="color:#ef4444">{curatedError}</p>
        {/if}
        <div class="mt-3 flex justify-end">
          <button
            onclick={saveCurated}
            disabled={curatedSaving}
            class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50"
          >
            {t('p3m.report.saveCurated')}
          </button>
        </div>
      </div>
    {/if}

    <!-- 1 Portfolio position -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">1. {t('p3m.report.portfolioPosition')}</h4>
      {#if doc.position.counts}
        <p class="text-sm">
          {t('p3m.report.positionSentence', {
            total: doc.position.counts.approved_total,
            delivery: (doc.position.counts.by_display_rag?.green ?? 0) +
              (doc.position.counts.by_display_rag?.amber ?? 0) +
              (doc.position.counts.by_display_rag?.red ?? 0),
            planning: doc.position.counts.by_stage?.define_planning ?? 0,
            onHold: doc.position.counts.on_hold ?? 0,
            g: doc.position.counts.by_display_rag?.green ?? 0,
            a: doc.position.counts.by_display_rag?.amber ?? 0,
            r: doc.position.counts.by_display_rag?.red ?? 0,
          })}
        </p>
      {/if}
      <div class="mt-2 flex flex-wrap gap-1.5">
        {#each Object.entries(doc.position.counts?.by_display_rag ?? {}) as [rag, n] (rag)}
          <span
            class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
            style="background-color:{ragColor(rag)}22; color:{ragColor(rag)}"
          >
            {rag}: {n}
          </span>
        {/each}
      </div>
      {#if doc.position.key_pressures}
        <div class="mt-3 grid gap-2 md:grid-cols-3">
          <div>
            <p class="text-xs font-medium">{t('p3m.report.keyPressureOutlook')}</p>
            <p class="text-xs">{doc.position.key_pressures.delivery_outlook || '—'}</p>
          </div>
          <div>
            <p class="text-xs font-medium">{t('p3m.report.keyPressureAmberRed')}</p>
            <p class="text-xs">{doc.position.key_pressures.amber_red_projects || '—'}</p>
          </div>
          <div>
            <p class="text-xs font-medium">{t('p3m.report.keyPressureReadiness')}</p>
            <p class="text-xs">{doc.position.key_pressures.business_readiness_change || '—'}</p>
          </div>
        </div>
      {/if}
      {#if (doc.position.elt_decisions_required ?? []).length}
        <p class="mt-3 text-xs font-medium">{t('p3m.report.eltRequired')}</p>
        <ul class="mt-1 list-inside list-disc text-xs">
          {#each doc.position.elt_decisions_required as d (d.id)}
            <li>{d.title}{d.project_title ? ` (${d.project_title})` : ''}</li>
          {/each}
        </ul>
      {/if}
    </div>

    <!-- 2 Programme highlights -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">2. {t('p3m.report.programmeHighlights')}</h4>
      {#if (doc.highlights ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.programmes')}</th>
              <th class="py-1 pr-2">RAG</th>
              <th class="py-1 pr-2">{t('p3m.report.progress')}</th>
              <th class="py-1 pr-2">{t('p3m.report.milestones')}</th>
              <th class="py-1 pr-2">{t('p3m.report.tasks')}</th>
              <th class="py-1">{t('p3m.report.commentary')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.highlights as h (h.programme_id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{h.title}</td>
                <td class="py-1 pr-2">
                  <span class="inline-flex items-center rounded-full px-2 py-0.5 font-medium" style="background-color:{ragColor(h.rag)}22; color:{ragColor(h.rag)}">
                    {h.rag}
                  </span>
                </td>
                <td class="py-1 pr-2">{h.progress_percent}%</td>
                <td class="py-1 pr-2">{h.milestones.done}/{h.milestones.total}</td>
                <td class="py-1 pr-2">{h.tasks.done}/{h.tasks.total}</td>
                <td class="py-1">{h.commentary || '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noProgrammes')}</p>
      {/if}
    </div>

    <!-- 3 Key portfolio risks -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">3. {t('p3m.report.keyRisks')}</h4>
      {#if (doc.risks ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">#</th>
              <th class="py-1 pr-2">{t('p3m.report.riskTitle')}</th>
              <th class="py-1 pr-2">{t('p3m.report.riskScore')}</th>
              <th class="py-1">{t('p3m.report.mitigation')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.risks as rk (rk.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]" title={rk.description}>
                <td class="py-1 pr-2">{rk.id}</td>
                <td class="py-1 pr-2 font-medium">{rk.title}</td>
                <td class="py-1 pr-2">{rk.risk_score}</td>
                <td class="py-1">{rk.mitigation_plan || '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noRisks')}</p>
      {/if}
    </div>

    <!-- 4 Benefits by type -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">4. {t('p3m.report.benefitsByType')}</h4>
      <p class="text-xs">
        {t('p3m.report.benefitsTotal', { total: doc.benefits?.total ?? 0, investments: doc.benefits?.investment_count ?? 0 })}
      </p>
      {#if Object.keys(doc.benefits?.by_type ?? {}).length}
        <table class="mt-2 w-full max-w-sm text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.benefitsTab')}</th>
              <th class="py-1 pr-2">{t('p3m.report.count')}</th>
            </tr>
          </thead>
          <tbody>
            {#each Object.entries(doc.benefits.by_type).sort((a, b) => b[1] - a[1]) as [type, n] (type)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2">{type}</td>
                <td class="py-1 pr-2">{n}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="mt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noBenefits')}</p>
      {/if}
    </div>

    <!-- 5 Benefits by CSO / owner group -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">5. {t('p3m.report.benefitsByCso')}</h4>
      {#if (doc.benefits?.by_cso ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">CSO</th>
              <th class="py-1 pr-2">{t('p3m.report.total')}</th>
              <th class="py-1 pr-2">{t('p3m.report.planning')}</th>
              <th class="py-1 pr-2">{t('p3m.report.implement')}</th>
              <th class="py-1 pr-2">{t('p3m.report.closed')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.benefits.by_cso as r (r.cso)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.cso}</td>
                <td class="py-1 pr-2">{r.total}</td>
                <td class="py-1 pr-2">{r.planning}</td>
                <td class="py-1 pr-2">{r.implement}</td>
                <td class="py-1 pr-2">{r.closed}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noBenefits')}</p>
      {/if}
      {#if (doc.benefits?.by_owner_group_and_category ?? []).length}
        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.ownerGroup')}</th>
              <th class="py-1 pr-2">{t('p3m.report.categories')}</th>
              <th class="py-1">{t('p3m.report.total')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.benefits.by_owner_group_and_category as r (r.owner_group)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.owner_group}</td>
                <td class="py-1 pr-2">
                  {Object.entries(r.categories ?? {}).map(([c, n]) => `${c}: ${n}`).join(' · ')}
                </td>
                <td class="py-1">{r.total}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <!-- 6 Portfolio dashboard -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">6. {t('p3m.report.dashboard')}</h4>
      {#if doc.dashboard?.executive_commentary}
        <p class="mb-3 text-xs italic">{doc.dashboard.executive_commentary}</p>
      {/if}
      <div class="mb-3 grid gap-3 md:grid-cols-2">
        <div>
          <p class="text-xs font-medium">
            {t('p3m.report.confidencePct')}:
            {doc.dashboard?.delivery_confidence?.current == null
              ? '—'
              : `${doc.dashboard.delivery_confidence.current}%`}
          </p>
          {#if (doc.dashboard?.delivery_confidence?.trend ?? []).length > 1}
            {@const trend = doc.dashboard.delivery_confidence.trend}
            {@const step = 200 / (trend.length - 1)}
            <svg viewBox="0 0 200 40" class="mt-1 h-10 w-full max-w-[200px]" role="img" aria-label={t('p3m.report.trend')}>
              <polyline
                fill="none"
                stroke="var(--color-ds-accent-blue, #2563eb)"
                stroke-width="2"
                points={trend.map((v, i) => `${i * step},${40 - (v / 100) * 36}`).join(' ')}
              />
            </svg>
          {/if}
        </div>
        <div>
          <p class="mb-1 text-xs font-medium">{t('p3m.report.activeByRag')}</p>
          <div class="flex flex-wrap gap-1.5">
            {#each Object.entries(doc.dashboard?.active_by_rag ?? {}) as [rag, n] (rag)}
              <span
                class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
                style="background-color:{ragColor(rag)}22; color:{ragColor(rag)}"
              >
                {rag}: {n}
              </span>
            {/each}
          </div>
        </div>
      </div>

      {#if (doc.dashboard?.confidence_lenses ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.lens')}</th>
              <th class="py-1 pr-2">{t('p3m.report.lensPosition')}</th>
              <th class="py-1">RAG</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.dashboard.confidence_lenses as l (l.key + l.position)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{l.key}</td>
                <td class="py-1 pr-2">{l.position}</td>
                <td class="py-1">
                  <span class="inline-flex items-center rounded-full px-2 py-0.5 font-medium" style="background-color:{ragColor(l.rag)}22; color:{ragColor(l.rag)}">
                    {l.rag}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if (doc.dashboard?.pipeline_by_theme ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.pipelineByTheme')}</p>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.theme')}</th>
              <th class="py-1 pr-2">{t('p3m.report.planning')}</th>
              <th class="py-1 pr-2">{t('p3m.report.awaitingElt')}</th>
              <th class="py-1 pr-2">{t('p3m.report.active')}</th>
              <th class="py-1 pr-2">{t('p3m.report.closed')}</th>
              <th class="py-1">{t('p3m.report.total')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.dashboard.pipeline_by_theme as r (r.theme)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.theme}</td>
                <td class="py-1 pr-2">{r.planning}</td>
                <td class="py-1 pr-2">{r.awaiting_elt}</td>
                <td class="py-1 pr-2">{r.active}</td>
                <td class="py-1 pr-2">{r.closed}</td>
                <td class="py-1">{r.total}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if (doc.dashboard?.change_capacity ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.changeCapacity')}</p>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.group')}</th>
              {#each (doc.dashboard.change_capacity[0]?.postures ? Object.keys(doc.dashboard.change_capacity[0].postures) : []) as m (m)}
                <th class="py-1 pr-2">{m}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each doc.dashboard.change_capacity as r (r.group)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.group}</td>
                {#each Object.keys(r.postures ?? {}) as m (m)}
                  <td class="py-1 pr-2">{r.postures[m] ?? '—'}</td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if (doc.dashboard?.benefits_by_category ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.benefitsByCategory')}</p>
        <table class="w-full max-w-lg text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.category')}</th>
              <th class="py-1 pr-2">{t('p3m.report.financial')}</th>
              <th class="py-1 pr-2">{t('p3m.report.nonFinancial')}</th>
              <th class="py-1 pr-2">{t('p3m.report.unspecified')}</th>
              <th class="py-1">{t('p3m.report.total')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.dashboard.benefits_by_category as r (r.category)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.category}</td>
                <td class="py-1 pr-2">{r.financial}</td>
                <td class="py-1 pr-2">{r.non_financial}</td>
                <td class="py-1 pr-2">{r.unspecified}</td>
                <td class="py-1">{r.total}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if (doc.dashboard?.emerging_demand ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.emergingDemand')}</p>
        <table class="w-full max-w-md text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.group')}</th>
              <th class="py-1 pr-2">{t('p3m.report.ideate')}</th>
              <th class="py-1">{t('p3m.report.proposal')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.dashboard.emerging_demand as r (r.group)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.group}</td>
                <td class="py-1 pr-2">{r.ideate}</td>
                <td class="py-1">{r.proposal}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <!-- 7 Red/amber delivery detail -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">7. {t('p3m.report.redAmberDetail')}</h4>
      {#if (doc.at_risk ?? []).length}
        {#each doc.at_risk as p (p.id)}
          <div class="mb-3 rounded border border-[var(--color-ds-border,#e5e7eb)] p-3">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-xs font-semibold">{p.title}</span>
              <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium" style="background-color:{ragColor(p.display_rag)}22; color:{ragColor(p.display_rag)}">
                {p.display_rag}
              </span>
              <span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
                {t('p3m.report.inStatusFor', { months: p.duration_in_status_months })}
              </span>
            </div>
            {#if p.business_owner_group || p.responsible_group}
              <p class="mt-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
                {p.business_owner_group ?? ''}
                {p.business_owner_group && p.responsible_group ? ' · ' : ''}
                {p.responsible_group ?? ''}
              </p>
            {/if}
            {#if p.status_report}
              <div class="mt-2 grid gap-2 text-xs md:grid-cols-3">
                <div>
                  <p class="font-medium">{t('p3m.report.issue')}</p>
                  <p>{p.status_report.issue || '—'}</p>
                </div>
                <div>
                  <p class="font-medium">{t('p3m.report.impact')}</p>
                  <p>{p.status_report.impact || '—'}</p>
                </div>
                <div>
                  <p class="font-medium">{t('p3m.report.nextSteps')}</p>
                  <p>{p.status_report.next_steps || '—'}</p>
                </div>
              </div>
            {:else}
              <p class="mt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noStatusReport')}</p>
            {/if}
            {#if (p.tolerances_at_risk ?? []).length}
              <p class="mt-2 text-xs font-medium">{t('p3m.report.tolerancesAtRisk')}</p>
              <div class="mt-1 flex flex-wrap gap-1.5">
                {#each p.tolerances_at_risk as tr (tr.category + tr.name)}
                  <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium" style="background-color:{ragColor(tr.rag)}22; color:{ragColor(tr.rag)}">
                    {tr.name}: {tr.rag}
                  </span>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noRedAmber')}</p>
      {/if}
    </div>

    <!-- 8 Change dashboard -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">8. {t('p3m.report.changeDashboard')}</h4>
      <p class="text-xs">
        {t('p3m.report.knownInitiatives', { n: doc.change_dashboard?.known_initiatives ?? 0 })}
      </p>
      {#if doc.change_dashboard?.key_messages}
        <p class="mt-1 text-xs italic">{doc.change_dashboard.key_messages}</p>
      {/if}
      {#if (doc.change_dashboard?.active_by_month ?? []).length}
        <table class="mt-2 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.month')}</th>
              <th class="py-1 pr-2">{t('p3m.report.active')}</th>
              <th class="py-1">{t('p3m.report.byImpact')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.change_dashboard.active_by_month as r (r.month)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.month}</td>
                <td class="py-1 pr-2">{r.active}</td>
                <td class="py-1">
                  {Object.entries(r.by_impact ?? {}).map(([l, n]) => `${l}: ${n}`).join(' · ') || '—'}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if (doc.change_dashboard?.medium_high ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.mediumHighImpact')}</p>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.activity')}</th>
              <th class="py-1">{t('p3m.report.groupImpacts')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.change_dashboard.medium_high as e (e.entry_id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{e.activity}</td>
                <td class="py-1">
                  {#each e.groups ?? [] as g (g.group)}
                    <span
                      class="mr-1.5 inline-flex items-center rounded-full px-2 py-0.5 font-medium"
                      style="background-color:{impactColor(g.impact_level)}22; color:{impactColor(g.impact_level)}"
                    >
                      {g.group} ({g.impact_level})
                    </span>
                  {/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <!-- 9 Change calendar -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">9. {t('p3m.report.changeCalendar')}</h4>
      {#if (doc.change_calendar?.dated ?? []).length}
        <p class="mb-1 text-xs font-medium">{t('p3m.report.datedEntries')}</p>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.activity')}</th>
              <th class="py-1 pr-2">{t('p3m.report.category')}</th>
              <th class="py-1 pr-2">{t('p3m.report.groupImpacts')}</th>
              <th class="py-1 pr-2">{t('p3m.report.months')}</th>
              <th class="py-1">{t('p3m.report.entity')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.change_calendar.dated as e (e.activity + (e.entity?.id ?? 0))}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{e.activity}</td>
                <td class="py-1 pr-2">{e.category ?? '—'}</td>
                <td class="py-1 pr-2">
                  {#each e.group_impacts ?? [] as g (g.business_group)}
                    <span
                      class="mr-1.5 inline-flex items-center rounded-full px-2 py-0.5 font-medium"
                      style="background-color:{impactColor(g.impact_level)}22; color:{impactColor(g.impact_level)}"
                    >
                      {g.business_group} ({g.impact_level})
                    </span>
                  {/each}
                </td>
                <td class="py-1 pr-2">{e.months.join(', ')}</td>
                <td class="py-1">{e.entity?.title ?? '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if (doc.change_calendar?.undated ?? []).length}
        <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.undatedEntries')}</p>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.activity')}</th>
              <th class="py-1 pr-2">{t('p3m.report.category')}</th>
              <th class="py-1">{t('p3m.report.groupImpacts')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.change_calendar.undated as e (e.activity)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{e.activity}</td>
                <td class="py-1 pr-2">{e.category ?? '—'}</td>
                <td class="py-1">
                  {#each e.group_impacts ?? [] as g (g.business_group)}
                    <span
                      class="mr-1.5 inline-flex items-center rounded-full px-2 py-0.5 font-medium"
                      style="background-color:{impactColor(g.impact_level)}22; color:{impactColor(g.impact_level)}"
                    >
                      {g.business_group} ({g.impact_level})
                    </span>
                  {/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if !(doc.change_calendar?.dated ?? []).length && !(doc.change_calendar?.undated ?? []).length}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noEntries')}</p>
      {/if}
    </div>

    <!-- 10 Ideate -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">10. {t('p3m.report.ideate')}</h4>
      {#if (doc.ideate ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.title')}</th>
              <th class="py-1 pr-2">Ref</th>
              <th class="py-1 pr-2">{t('p3m.report.businessGroup')}</th>
              <th class="py-1 pr-2">{t('p3m.report.responsibleGroup')}</th>
              <th class="py-1">{t('p3m.report.frontDoorCompleted')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.ideate as r (r.title + r.ref)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.title}</td>
                <td class="py-1 pr-2">{r.ref || '—'}</td>
                <td class="py-1 pr-2">{r.business_group}</td>
                <td class="py-1 pr-2">{r.responsible_group ?? '—'}</td>
                <td class="py-1">{r.front_door_completed ? '✓' : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.none')}</p>
      {/if}
    </div>

    <!-- 11 Proposals -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">11. {t('p3m.report.proposals')}</h4>
      {#if (doc.proposals ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.title')}</th>
              <th class="py-1 pr-2">Ref</th>
              <th class="py-1 pr-2">{t('p3m.report.businessGroup')}</th>
              <th class="py-1 pr-2">{t('p3m.report.responsibleGroup')}</th>
              <th class="py-1 pr-2">{t('p3m.report.complexity')}</th>
              <th class="py-1">{t('p3m.report.priority')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.proposals as r (r.title + (r.ref ?? ''))}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{r.title}</td>
                <td class="py-1 pr-2">{r.ref || '—'}</td>
                <td class="py-1 pr-2">{r.business_owner_group ?? '—'}</td>
                <td class="py-1 pr-2">{r.responsible_group ?? '—'}</td>
                <td class="py-1 pr-2">{r.complexity ?? '—'}</td>
                <td class="py-1">{r.priority ?? '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.none')}</p>
      {/if}
    </div>

    <!-- 12 Approved investments -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">12. {t('p3m.report.approvedInvestments')}</h4>
      {#if (doc.investments ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.kind')}</th>
              <th class="py-1 pr-2">{t('p3m.report.name')}</th>
              <th class="py-1 pr-2">Ref</th>
              <th class="py-1 pr-2">{t('p3m.report.phase')}</th>
              <th class="py-1 pr-2">RAG</th>
              <th class="py-1 pr-2">{t('p3m.report.onHold')}</th>
              <th class="py-1 pr-2">{t('p3m.report.expectedEnd')}</th>
              <th class="py-1 pr-2">{t('p3m.report.investmentType')}</th>
              <th class="py-1 pr-2">{t('p3m.report.theme')}</th>
              <th class="py-1 pr-2">{t('p3m.report.ceKpi')}</th>
              <th class="py-1">{t('p3m.report.trend')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.investments as r (r.kind + r.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2">{r.kind}</td>
                <td class="py-1 pr-2 font-medium">{r.name}</td>
                <td class="py-1 pr-2">{r.ref ?? '—'}</td>
                <td class="py-1 pr-2">{r.phase}</td>
                <td class="py-1 pr-2">
                  <span class="inline-flex items-center rounded-full px-2 py-0.5 font-medium" style="background-color:{ragColor(r.display_rag)}22; color:{ragColor(r.display_rag)}">
                    {r.display_rag}
                  </span>
                </td>
                <td class="py-1 pr-2">{r.on_hold ? '✓' : ''}</td>
                <td class="py-1 pr-2">{r.expected_end ?? '—'}</td>
                <td class="py-1 pr-2">{r.investment_type ?? '—'}</td>
                <td class="py-1 pr-2">{r.theme ?? '—'}</td>
                <td class="py-1 pr-2">{r.is_ce_kpi ? '★' : ''}</td>
                <td class="py-1">{r.rag_trend ?? '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.none')}</p>
      {/if}
    </div>

    <!-- 13 Closed -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h4 class="mb-2 text-sm font-semibold">13. {t('p3m.report.closed')}</h4>
      {#if (doc.closed ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.kind')}</th>
              <th class="py-1 pr-2">{t('p3m.report.name')}</th>
              <th class="py-1 pr-2">Ref</th>
              <th class="py-1 pr-2">{t('p3m.report.businessGroup')}</th>
              <th class="py-1">{t('p3m.report.endReportReceived')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.closed as r (r.kind + r.name)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2">{r.kind}</td>
                <td class="py-1 pr-2 font-medium">{r.name}</td>
                <td class="py-1 pr-2">{r.ref ?? '—'}</td>
                <td class="py-1 pr-2">{r.responsible_group ?? r.business_owner_group ?? '—'}</td>
                <td class="py-1">{r.end_report_received ? '✓' : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.none')}</p>
      {/if}
    </div>

    <!-- 14 ELT decision coversheets -->
    <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <div class="mb-2 flex items-center justify-between">
        <h4 class="text-sm font-semibold">14. {t('p3m.report.eltCoversheets')}</h4>
        <button
          onclick={openEltCreate}
          class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs font-medium"
        >
          + {t('p3m.report.newEltDecision')}
        </button>
      </div>
      {#if (doc.elt_decisions ?? []).length}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1 pr-2">{t('p3m.report.title')}</th>
              <th class="py-1 pr-2">{t('p3m.report.project')}</th>
              <th class="py-1 pr-2">{t('p3m.report.decisionRequested')}</th>
              <th class="py-1 pr-2">{t('p3m.report.status')}</th>
              <th class="py-1">{t('p3m.report.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {#each doc.elt_decisions as d (d.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1 pr-2 font-medium">{d.title}</td>
                <td class="py-1 pr-2">{d.project_title ?? '—'}</td>
                <td class="py-1 pr-2">{d.decision_requested || '—'}</td>
                <td class="py-1 pr-2">
                  <span
                    class="inline-flex items-center rounded-full bg-[var(--color-ds-surface,#f3f4f6)] px-2 py-0.5 font-medium"
                    style="color:{d.status === 'approved' ? '#10b981' : d.status === 'rejected' ? '#ef4444' : d.status === 'pending' ? '#f59e0b' : '#6b7280'}"
                  >
                    {d.status}
                  </span>
                </td>
                <td class="py-1">
                  {#if d.status === 'pending'}
                    <button
                      onclick={() => decideElt(d.id, 'approved')}
                      class="mr-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-1.5 py-0.5"
                    >
                      {t('p3m.report.approve')}
                    </button>
                    <button
                      onclick={() => decideElt(d.id, 'rejected')}
                      class="mr-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-1.5 py-0.5"
                    >
                      {t('p3m.report.reject')}
                    </button>
                  {/if}
                  <button
                    onclick={() => openEltEdit(d)}
                    class="mr-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-1.5 py-0.5"
                  >
                    {t('p3m.report.edit')}
                  </button>
                  <button
                    onclick={() => deleteElt(d.id)}
                    class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-1.5 py-0.5"
                  >
                    {t('p3m.report.delete')}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noEltDecisions')}</p>
      {/if}
    </div>

    <!-- Reference data (admin) -->
    {#if $isSystemAdmin}
      <div class="mb-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
        <button
          onclick={() => { refOpen = !refOpen; if (refOpen) void loadRef(); }}
          class="text-sm font-semibold"
        >
          {refOpen ? '▾' : '▸'} {t('p3m.report.referenceData')}
        </button>
        {#if refOpen}
          <div class="mt-3 grid gap-3 md:grid-cols-2">
            {#each REF_CATEGORIES as cat (cat)}
              <div class="rounded border border-[var(--color-ds-border,#e5e7eb)] p-3">
                <p class="mb-1 text-xs font-medium">{t(`p3m.report.ref_${cat}`)}</p>
                {#each refValues[cat] as v, vi (cat + v)}
                  <span class="mr-1.5 inline-flex items-center gap-1 rounded-full bg-[var(--color-ds-surface,#f3f4f6)] px-2 py-0.5 text-xs">
                    {v}
                    <button
                      onclick={() => (refValues[cat] = refValues[cat].filter((_, i) => i !== vi))}
                      class="text-[var(--color-ds-text-muted,#6b7280)]"
                    >
                      ×
                    </button>
                  </span>
                {/each}
                <div class="mt-2 flex gap-2">
                  <input
                    bind:value={refAdding[cat]}
                    placeholder={t('p3m.report.addValue')}
                    class="min-w-0 flex-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs"
                  />
                  <button
                    onclick={() => addRefValue(cat)}
                    class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs"
                  >
                    +
                  </button>
                  <button
                    onclick={() => saveRefCategory(cat)}
                    class="rounded bg-[var(--color-ds-primary,#2563eb)] px-2 py-0.5 text-xs font-medium text-white"
                  >
                    {t('p3m.report.save')}
                  </button>
                </div>
              </div>
            {/each}
          </div>
          {#if refError}
            <p class="mt-2 text-xs" style="color:#ef4444">{refError}</p>
          {/if}
        {/if}
      </div>
    {/if}
  {:else}
    <p class="py-6 text-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noData')}</p>
  {/if}
</section>

<!-- ELT decision dialog -->
{#if eltFormOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
    <div class="w-full max-w-lg rounded-lg bg-[var(--color-ds-bg,#fff)] p-4 shadow-xl">
      <h4 class="mb-3 text-sm font-semibold">
        {eltEditingId ? t('p3m.report.editEltDecision') : t('p3m.report.newEltDecision')}
      </h4>
      <div class="space-y-3">
        <div>
          <label class="mb-1 block text-xs font-medium">{t('p3m.report.title')}</label>
          <input
            bind:value={eltForm.title}
            class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
          />
        </div>
        {#if portfolioProjects.length}
          <div>
            <label class="mb-1 block text-xs font-medium">{t('p3m.report.project')}</label>
            <select
              bind:value={eltForm.project_id}
              class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
            >
              <option value="">—</option>
              {#each portfolioProjects as p (p.id)}
                <option value={p.id}>{p.title}</option>
              {/each}
            </select>
          </div>
        {/if}
        <div>
          <label class="mb-1 block text-xs font-medium">{t('p3m.report.why')}</label>
          <textarea
            bind:value={eltForm.why}
            rows="3"
            class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
          ></textarea>
        </div>
        <div>
          <label class="mb-1 block text-xs font-medium">{t('p3m.report.decisionRequested')}</label>
          <textarea
            bind:value={eltForm.decision_requested}
            rows="2"
            class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
          ></textarea>
        </div>
        <div>
          <label class="mb-1 block text-xs font-medium">{t('p3m.report.conditions')}</label>
          <textarea
            bind:value={eltForm.conditions_until_approved}
            rows="2"
            class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"
          ></textarea>
        </div>
      </div>
      {#if eltError}
        <p class="mt-2 text-xs" style="color:#ef4444">{eltError}</p>
      {/if}
      <div class="mt-4 flex justify-end gap-2">
        <button
          onclick={() => (eltFormOpen = false)}
          class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm"
        >
          {t('p3m.report.cancel')}
        </button>
        <button
          onclick={saveElt}
          disabled={eltSaving || !eltForm.title.trim()}
          class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50"
        >
          {t('p3m.report.save')}
        </button>
      </div>
    </div>
  </div>
{/if}
