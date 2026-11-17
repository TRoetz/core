<script>
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { classificationLabel, engagementLabel, lifecycleLabel, relationshipLabel, P3M_OPTIONS_KEYS } from '../utils.js';

  /** @type {{ dashboard: object }} */
  let { dashboard = {} } = $props();

  const SUBTABS = [
    { id: 'register', labelKey: 'sms.registerTab' },
    { id: 'executive', labelKey: 'sms.executiveTab' },
    { id: 'programme', labelKey: 'sms.programmeTab' },
    { id: 'pilot', labelKey: 'sms.pilotMeasuresTab' },
    { id: 'quality', labelKey: 'sms.dataQualityTab' },
    { id: 'data', labelKey: 'sms.dataMovement' },
  ];

  let activeSubtab = $state('register');
  let subtabError = $state('');

  // --- Register tier (module dashboard distributions) ---
  const byClassification = $derived(dashboard.by_classification ?? []);
  const byEngagement = $derived(dashboard.by_engagement_status ?? []);
  const byLifecycle = $derived(dashboard.by_lifecycle_stage ?? []);
  const byRelationship = $derived(dashboard.relationship_distribution ?? []);

  const healthBuckets = $derived(dashboard.health_buckets ?? []);

  function percent(count, total) {
    if (!total) return '0%';
    return `${Math.round((count / total) * 100)}%`;
  }

  function bucketLabel(bucket) {
    switch (bucket) {
      case '0-25': return t('sms.healthCritical');
      case '26-50': return t('sms.healthWeak');
      case '51-75': return t('sms.healthFair');
      case '76-100': return t('sms.healthStrong');
      default: return bucket;
    }
  }

  // --- Executive / programme rollups (WI-4.1, WI-4.2) ---
  let execData = $state(null);
  let progEntityType = $state('project');
  let p3mOptions = $state({ portfolios: [], programmes: [], projects: [] });
  let progEntityId = $state('');
  let rollupLoading = $state(false);

  const entityList = $derived(p3mOptions[P3M_OPTIONS_KEYS[progEntityType]] ?? []);

  async function loadExecutive() {
    rollupLoading = true;
    subtabError = '';
    try {
      execData = await smsApi.getExecutiveDashboard();
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    } finally {
      rollupLoading = false;
    }
  }

  async function loadProgrammeOptions() {
    p3mOptions = (await smsApi.getP3MOptions()) ?? p3mOptions;
  }

  async function loadProgramme() {
    if (!progEntityId) return;
    rollupLoading = true;
    subtabError = '';
    try {
      execData = await smsApi.getProgrammeDashboard({ entity_type: progEntityType, entity_id: progEntityId });
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    } finally {
      rollupLoading = false;
    }
  }

  // --- Pilot success measures (WI-4.5, Doc 8 Go/No-Go evidence) ---
  let pilotMetrics = $state(null);
  const KNOWN_METRICS = [
    'stakeholder_count',
    'active_engagement_plans',
    'engagement_coverage_pct',
    'commitments_completion_rate_pct',
    'grievances_closed_rate_pct',
    'issues_resolved_rate_pct',
  ];
  let baselineForm = $state({ metric_key: '', baseline_value: '', target_value: '' });

  async function loadPilot() {
    subtabError = '';
    try {
      pilotMetrics = await smsApi.getPilotMetrics();
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    }
  }

  async function saveBaseline() {
    const key = baselineForm.metric_key.trim();
    if (!key) return;
    const num = (v) => (v === '' ? null : Number(v));
    try {
      await smsApi.upsertPilotBaseline({
        metric_key: key,
        baseline_value: num(baselineForm.baseline_value),
        target_value: num(baselineForm.target_value),
      });
      baselineForm = { ...baselineForm, baseline_value: '', target_value: '' };
      await loadPilot();
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    }
  }

  function pilotStatusClass(status) {
    switch (status) {
      case 'on_track': return 'bg-green-100 text-green-700';
      case 'behind': return 'bg-red-100 text-red-700';
      default: return 'bg-gray-100 text-gray-500';
    }
  }

  // --- Data quality (WI-3.3, WI-4.3) ---
  let dqData = $state(null);

  async function loadQuality() {
    subtabError = '';
    try {
      dqData = await smsApi.getDataQuality();
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    }
  }

  // --- Data movement: import wizard + exports + templates (WI-5.x) ---
  let importText = $state('');
  let preview = $state(null);
  let importMode = $state('create');
  let importResult = $state(null);
  let templates = $state([]);

  async function onCsvFile(e) {
    const file = e.target.files?.[0];
    if (!file) return;
    importText = await file.text();
    preview = null;
    importResult = null;
  }

  async function runPreview() {
    if (!importText.trim()) return;
    subtabError = '';
    try {
      preview = await smsApi.importPreview(importText);
      importResult = null;
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    }
  }

  async function runImport() {
    if (!importText.trim()) return;
    subtabError = '';
    try {
      importResult = await smsApi.importExecute(importText, importMode);
      preview = null;
    } catch (e) {
      console.error(e);
      subtabError = String(e?.message ?? e);
    }
  }

  async function loadTemplates() {
    templates = (await smsApi.getTemplates()) ?? [];
  }

  // Lazy-load each subtab on first activation.
  const loadedSubtabs = $state(new Set());
  function activate(id) {
    activeSubtab = id;
    if (!loadedSubtabs.has(id)) {
      loadedSubtabs.add(id);
      switch (id) {
        case 'executive': loadExecutive(); break;
        case 'programme':
          execData = null;
          loadProgrammeOptions().then(loadProgramme).catch((e) => console.error(e));
          break;
        case 'pilot': loadPilot(); break;
        case 'quality': loadQuality(); break;
        case 'data': loadTemplates().catch((e) => console.error(e)); break;
      }
    }
  }

  function fmtDate(v) {
    return v ? new Date(v).toLocaleDateString() : '—';
  }
</script>

<div class="space-y-6 p-6 py-4">
  <!-- Subtabs -->
  <div class="flex flex-wrap gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] pb-2">
    {#each SUBTABS as tab (tab.id)}
      <button
        type="button"
        class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors {activeSubtab === tab.id ? 'bg-[var(--color-ds-accent,#2563eb)] text-white' : 'text-[var(--color-ds-text-muted,#6b7280)] hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]'}"
        onclick={() => activate(tab.id)}
      >
        {t(tab.labelKey)}
      </button>
    {/each}
  </div>

  {#if subtabError && activeSubtab !== 'register'}
    <div class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{subtabError}</div>
  {/if}

  <!-- Register tier: distributions + CSV exports -->
  {#if activeSubtab === 'register'}
    <div class="flex flex-wrap gap-2">
      <a href={smsApi.exportUrl('stakeholders')} download class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]">
        {t('sms.exportStakeholders')}
      </a>
      <a href={smsApi.exportUrl('communications')} download class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]">
        {t('sms.exportCommunications')}
      </a>
    </div>

    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      {#each [
        { data: byClassification, titleKey: 'sms.classificationDist', label: classificationLabel },
        { data: byEngagement, titleKey: 'sms.engagementDist', label: engagementLabel },
        { data: byLifecycle, titleKey: 'sms.lifecycleDist', label: lifecycleLabel },
        { data: byRelationship, titleKey: 'sms.relationshipDist', label: relationshipLabel },
      ] as table (table.titleKey)}
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t(table.titleKey)}</h3>
          {#if table.data.length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] font-semibold"><th class="py-1.5 pr-3">{t('sms.value')}</th><th class="py-1.5 pr-3 text-right">{t('sms.count')}</th><th class="py-1.5 text-right">{t('sms.percent')}</th></tr></thead>
              <tbody>
                {#each table.data as row (row.value)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1.5 pr-3">{table.label(row.value)}</td>
                    <td class="py-1.5 pr-3 text-right">{row.count}</td>
                    <td class="py-1.5 text-right">{percent(row.count, dashboard.totals?.stakeholders ?? 0)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      {/each}
    </div>

    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.healthBuckets')}</h3>
      {#if healthBuckets.length === 0}
        <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] font-semibold"><th class="py-1.5 pr-3">{t('sms.value')}</th><th class="py-1.5 pr-3 text-right">{t('sms.count')}</th></tr></thead>
          <tbody>
            {#each healthBuckets as bucket (bucket.value)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="py-1.5 pr-3">{bucketLabel(bucket.value)}</td>
                <td class="py-1.5 text-right">{bucket.count}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}

  <!-- Executive tier (WI-4.1) -->
  {#snippet rollupView(d)}
    <div class="space-y-4">
      {#if d.scope}
        <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.scopedToEntity', { entity: `${d.scope.entity_type} #${d.scope.entity_id}` })}</div>
      {/if}

      <div class="grid grid-cols-2 gap-4 sm:grid-cols-5">
        {#each [
          { value: d.totals?.stakeholders ?? 0, label: t('sms.statStakeholders') },
          { value: d.commitments?.open ?? 0, label: t('sms.openCommitments'), sub: `${t('sms.overdue')}: ${d.commitments?.overdue ?? 0}` },
          { value: d.grievances?.escalated_open ?? 0, label: t('sms.grievancesEscalated') },
          { value: d.issues?.critical_open ?? 0, label: t('sms.criticalOpen') },
          { value: `${Math.round(d.engagement_coverage_pct ?? 0)}%`, label: t('sms.engagementCoverage') },
        ] as card (card.label)}
          <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
            <div class="text-xl font-semibold">{card.value}</div>
            <div class="mt-0.5 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{card.label}</div>
            {#if card.sub}<div class="text-xs text-red-600">{card.sub}</div>{/if}
          </div>
        {/each}
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {#each [
          { data: d.totals?.by_tier ?? [], titleKey: 'sms.classificationDist', label: classificationLabel },
          { data: d.totals?.relationship_distribution ?? [], titleKey: 'sms.relationshipDist', label: relationshipLabel },
        ] as table (table.titleKey)}
          <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
            <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t(table.titleKey)}</h3>
            {#if table.data.length === 0}
              <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
            {:else}
              <table class="w-full text-left text-xs">
                <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] font-semibold"><th class="py-1.5 pr-3">{t('sms.value')}</th><th class="py-1.5 pr-3 text-right">{t('sms.count')}</th></tr></thead>
                <tbody>
                  {#each table.data as row (row.value)}
                    <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                      <td class="py-1.5 pr-3">{table.label(row.value)}</td>
                      <td class="py-1.5 text-right">{row.count}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </div>
        {/each}
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.healthBuckets')}</h3>
          {#if (d.health_buckets ?? []).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <table class="w-full text-left text-xs">
              <tbody>
                {#each d.health_buckets as bucket (bucket.value)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1.5 pr-3">{bucketLabel(bucket.value)}</td>
                    <td class="py-1.5 text-right">{bucket.count}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>

        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.atRiskList')}</h3>
          {#if (d.at_risk ?? []).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] font-semibold"><th class="py-1.5 pr-3">{t('sms.name')}</th><th class="py-1.5 pr-3">{t('sms.organization')}</th><th class="py-1.5 pr-3 text-right">{t('sms.healthScore')}</th></tr></thead>
              <tbody>
                {#each d.at_risk as s (s.stakeholder_id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1.5 pr-3">{s.name}</td>
                    <td class="py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{s.organization || '—'}</td>
                    <td class="py-1.5 text-right font-medium {s.health_score < 25 ? 'text-red-600' : 'text-amber-600'}">{s.health_score}{#if s.open_critical_issues > 0} · {s.open_critical_issues}⚠{/if}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      </div>
    </div>
  {/snippet}

  {#if activeSubtab === 'executive'}
    {#if rollupLoading}
      <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
    {:else if execData}
      {@render rollupView(execData)}
    {:else if !subtabError}
      <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
    {/if}
  {/if}

  <!-- Programme tier (WI-4.2): rollup scoped to one P3M entity -->
  {#if activeSubtab === 'programme'}
    <div class="flex flex-wrap items-end gap-3">
      <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.p3mEntity')}
        <select class="mt-1 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" value={progEntityType} onchange={(e) => { progEntityType = e.target.value; progEntityId = ''; execData = null; loadProgrammeOptions(); }}>
          <option value="portfolio">{t('sms.entityPortfolio')}</option>
          <option value="programme">{t('sms.entityProgramme')}</option>
          <option value="project">{t('sms.entityProject')}</option>
        </select>
      </label>
      <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.entity')}
        <select class="mt-1 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" value={progEntityId} onchange={(e) => { progEntityId = e.target.value; loadProgramme(); }}>
          <option value="">{t('sms.selectEntity')}</option>
          {#each entityList as opt (opt.id)}
            <option value={opt.id}>{opt.title}</option>
          {/each}
        </select>
      </label>
    </div>

    {#if rollupLoading}
      <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
    {:else if execData}
      {@render rollupView(execData)}
    {/if}
  {/if}

  <!-- Pilot success measures (WI-4.5) -->
  {#if activeSubtab === 'pilot'}
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.pilotMeasuresTab')}</h3>
      {#if !pilotMetrics}
        <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else if (pilotMetrics.metrics ?? []).length === 0}
        <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] font-semibold"><th class="py-1.5 pr-3">{t('sms.metricKey')}</th><th class="py-1.5 pr-3 text-right">{t('sms.baseline')}</th><th class="py-1.5 pr-3 text-right">{t('sms.target')}</th><th class="py-1.5 pr-3 text-right">{t('sms.current')}</th><th class="py-1.5 pr-3 text-right">{t('sms.delta')}</th><th class="py-1.5">Status</th></tr></thead>
          <tbody>
            {#each pilotMetrics.metrics as m (m.metric_key)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="py-1.5 pr-3 font-mono">{m.metric_key}</td>
                <td class="py-1.5 pr-3 text-right">{m.baseline_value ?? '—'}</td>
                <td class="py-1.5 pr-3 text-right">{m.target_value ?? '—'}</td>
                <td class="py-1.5 pr-3 text-right">{m.current_value ?? '—'}</td>
                <td class="py-1.5 pr-3 text-right">{m.delta !== undefined ? (m.delta > 0 ? `+${m.delta}` : m.delta) : '—'}</td>
                <td class="py-1.5"><span class="rounded-full px-2 py-0.5 text-xs {pilotStatusClass(m.status)}">{t(`sms.pilotStatus${m.status === 'on_track' ? 'OnTrack' : m.status === 'behind' ? 'Behind' : m.status === 'no_target' ? 'NoTarget' : 'Unknown'}`)}</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <form class="flex flex-wrap items-end gap-3 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4" onsubmit={(e) => { e.preventDefault(); saveBaseline(); }}>
      <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.metricKey')}
        <input list="known-pilot-metrics" class="mt-1 block w-56 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 font-mono text-xs" bind:value={baselineForm.metric_key} placeholder="engagement_coverage_pct">
        <datalist id="known-pilot-metrics">{#each KNOWN_METRICS as k (k)}<option value={k}></option>{/each}</datalist>
      </label>
      <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.baseline')}
        <input type="number" step="any" class="mt-1 block w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" bind:value={baselineForm.baseline_value}>
      </label>
      <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.target')}
        <input type="number" step="any" class="mt-1 block w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" bind:value={baselineForm.target_value}>
      </label>
      <button type="submit" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90">{t('sms.saveBaseline')}</button>
    </form>
  {/if}

  <!-- Operational data quality (WI-4.3) -->
  {#if activeSubtab === 'quality'}
    {#if !dqData}
      <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
    {:else}
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {#each [
          { value: dqData.totals?.stakeholders ?? 0, label: t('sms.statStakeholders') },
          { value: `${Math.round(dqData.completeness?.percent ?? 0)}%`, label: t('sms.completenessPct') },
          { value: (dqData.stale_records ?? []).length, label: t('sms.staleRecords', { days: dqData.stale_days }) },
          { value: `${dqData.cross_store_duplicates?.actionable ?? 0}/${dqData.cross_store_duplicates?.matches ?? 0}`, label: t('sms.crossStoreDupes') },
        ] as card (card.label)}
          <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
            <div class="text-xl font-semibold">{card.value}</div>
            <div class="mt-0.5 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{card.label}</div>
          </div>
        {/each}
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.missingFields')}</h3>
          {#if Object.keys(dqData.completeness?.missing_field_counts ?? {}).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.complete')} ({dqData.completeness?.complete ?? 0}/{(dqData.totals?.active) ?? 0})</div>
          {:else}
            <table class="w-full text-left text-xs">
              <tbody>
                {#each Object.entries(dqData.completeness.missing_field_counts) as [field, count] (field)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0"><td class="py-1.5 pr-3 font-mono">{field}</td><td class="py-1.5 text-right">{count}</td></tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>

        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.missingOwnerList')}</h3>
          {#if (dqData.missing_owner ?? []).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <ul class="space-y-1 text-xs">
              {#each dqData.missing_owner as s (s.id)}
                <li>{s.name} — tier: {s.tier}</li>
              {/each}
            </ul>
          {/if}
        </div>

        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.staleRecords', { days: dqData.stale_days })}</h3>
          {#if (dqData.stale_records ?? []).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <table class="w-full text-left text-xs">
              <tbody>
                {#each dqData.stale_records as s (s.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1.5 pr-3">{s.name}</td>
                    <td class="py-1.5 text-right text-[var(--color-ds-text-muted,#6b7280)]">{s.days_since} {t('sms.daysSince')}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>

        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.overdueReviews')}</h3>
          {#if (dqData.overdue_reviews ?? []).length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <table class="w-full text-left text-xs">
              <tbody>
                {#each dqData.overdue_reviews as s (s.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1.5 pr-3">{s.name}</td>
                    <td class="py-1.5 text-right text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.dueDateField')}: {fmtDate(s.due_date)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs">
          <h3 class="mb-2 font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.duplicateCandidates')}</h3>
          {t('sms.recordsInGroups', { count: dqData.duplicate_candidates?.records_in_groups ?? 0, groups: dqData.duplicate_candidates?.groups ?? 0 })}
        </div>
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs">
          <h3 class="mb-2 font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.engagementCompliance')}</h3>
          {dqData.engagement_compliance?.with_active_plan ?? 0}/{dqData.engagement_compliance?.tier_1_2_total ?? 0} ({Math.round(dqData.engagement_compliance?.percent ?? 0)}%)
        </div>
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs">
          <h3 class="mb-2 font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.crossStoreDupes')}</h3>
          {t('sms.actionableMatches', { count: dqData.cross_store_duplicates?.actionable ?? 0, total: dqData.cross_store_duplicates?.matches ?? 0 })}
        </div>
      </div>
    {/if}
  {/if}

  <!-- Data movement (WI-5.x): import wizard + exports + templates -->
  {#if activeSubtab === 'data'}
    <div class="grid gap-6 lg:grid-cols-2">
      <section class="space-y-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
        <h3 class="text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.dataMovement')}</h3>

        <label class="block text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('sms.importCsv')}
          <input type="file" accept=".csv,text/csv" class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" onchange={onCsvFile}>
        </label>
        <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.importHint')}</p>

        {#if importText}
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]" onclick={runPreview}>{t('sms.previewImport')}</button>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              {t('sms.importModeLabel')}
              <select class="mt-1 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1.5 text-xs" bind:value={importMode}>
                <option value="create">{t('sms.importModeCreate')}</option>
                <option value="upsert">{t('sms.importModeUpdate')}</option>
              </select>
            </label>
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90" onclick={runImport}>{t('sms.executeImport')}</button>
          </div>

          {#if preview}
            <div class="space-y-2 rounded-md bg-[var(--color-ds-background-neutral-hover,#f3f4f6)] p-3 text-xs">
              <div>{t('sms.rowsTotal', { count: preview.total_rows })} · {preview.valid_rows} ✓</div>
              {#if (preview.duplicates ?? []).length > 0}
                <div class="text-amber-700">{t('sms.duplicatesInFile', { count: preview.duplicates.length })}</div>
              {/if}
              {#if (preview.errors ?? []).length > 0}
                <table class="w-full text-left">
                  <thead><tr class="font-semibold"><th class="py-1 pr-3">{t('sms.rowErrors')}</th></tr></thead>
                  <tbody>
                    {#each preview.errors as err (err.row + err.field + err.message)}
                      <tr class="border-t border-[var(--color-ds-border,#e5e7eb)]"><td class="py-1 pr-3">row {err.row}{err.field ? ` · ${err.field}` : ''}: {err.message}</td></tr>
                    {/each}
                  </tbody>
                </table>
              {/if}
            </div>
          {/if}

          {#if importResult}
            <div class="rounded-md bg-green-50 p-3 text-xs text-green-700">
              {t('sms.importedSummary', { created: importResult.created, updated: importResult.updated, skipped: importResult.skipped })}
            </div>
          {/if}
        {/if}
      </section>

      <section class="space-y-4">
        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.dataMovement')}</h3>
          <div class="flex flex-wrap gap-2">
            <a href={smsApi.exportFullUrl()} download class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]">{t('sms.exportFullJson')}</a>
            <a href={smsApi.exportManifestUrl()} download class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]">{t('sms.exportManifest')}</a>
            <a href={smsApi.exportGeoJsonUrl()} download class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-[var(--color-ds-background-neutral-hover,#f3f4f6)]">{t('sms.exportGeoJson')}</a>
          </div>
        </div>

        <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.templatesCatalog')}</h3>
          {#if templates.length === 0}
            <div class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
          {:else}
            <ul class="divide-y divide-[var(--color-ds-border,#e5e7eb)]">
              {#each templates as tpl (tpl.name)}
                <li class="flex items-center justify-between gap-3 py-2 text-xs">
                  <div>
                    <span class="font-medium">{tpl.title}</span>
                    <span class="ml-2 rounded-full bg-[var(--color-ds-background-neutral-hover,#f3f4f6)] px-2 py-0.5 font-mono text-[10px] uppercase">{tpl.format}</span>
                    <div class="text-[var(--color-ds-text-muted,#6b7280)]">{tpl.description}</div>
                  </div>
                  <a href={smsApi.templateUrl(tpl.name)} download class="shrink-0 text-[var(--color-ds-accent,#2563eb)] hover:underline">↓</a>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      </section>
    </div>
  {/if}
</div>
