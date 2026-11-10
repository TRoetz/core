<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { plans as plansApi } from '../../api/plans.js';
  import { currentRoute } from '../../router.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconTarget from '@tabler/icons-svelte-runes/icons/target';
  import P3MFrontDoor from './P3MFrontDoor.svelte';
  import P3MOverview from './P3MOverview.svelte';
  import P3MInvestmentCases from './P3MInvestmentCases.svelte';
  import P3MHierarchyGuide from './P3MHierarchyGuide.svelte';
  import P3MGovernance from './P3MGovernance.svelte';
  import P3MBenefitsFinancials from './P3MBenefitsFinancials.svelte';
  import P3MRiskStakeholders from './P3MRiskStakeholders.svelte';
  import P3MResources from './P3MResources.svelte';
  import P3MChangeCalendar from './P3MChangeCalendar.svelte';
  import P3MReporting from './P3MReporting.svelte';
  import P3MPortfolioPulse from './P3MPortfolioPulse.svelte';

  const TABS = [
    { id: 'frontdoor', labelKey: 'p3m.frontDoorTab' },
    { id: 'overview', labelKey: 'p3m.overview' },
    { id: 'investment', labelKey: 'p3m.investmentTab' },
    { id: 'governance', labelKey: 'p3m.governance' },
    { id: 'benefits', labelKey: 'p3m.benefitsTab' },
    { id: 'risk', labelKey: 'p3m.riskTab' },
    { id: 'resources', labelKey: 'p3m.resourcesTab' },
    { id: 'change', labelKey: 'p3m.changeTab' },
    { id: 'reporting', labelKey: 'p3m.reportingTab' },
  ];

  let activeTab = $state('overview');
  let loading = $state(true);
  let error = $state('');
  // True once a load() succeeds. Background reloads (success or failure)
  // must not unmount the tab content, or open forms/expanded rows lose
  // their state on every onChanged().
  let hasLoaded = $state(false);
  /** @type {{ portfolios: any[] }} */
  let hierarchy = $state({ portfolios: [] });
  /** @type {any} */
  let summary = $state(null);
  /**
   * "${entity_type}:${entity_id}" -> [{ plan_id, plan_name, plan_color }]
   * (from /plans/p3m-map) so each P3M node can show its linked plans.
   * @type {Map<string, Array<{plan_id:number, plan_name:string, plan_color:string}>>}
   */
  let planMap = $state(new Map());
  // Configurable lifecycle (BRD §5.2/§5.5): the enabled stage list drives the
  // stage pickers/filters in Overview, Governance and the project form. An
  // empty list means config is unavailable; views fall back to legacy labels.
  /** @type {{stages: any[], gate_templates: any[]}} */
  let lifecycle = $state({ stages: [], gate_templates: [] });

  const portfolioCount = $derived(hierarchy.portfolios.length);

  // Deep link to a project's detail view: the /p3m/projects/:id route, or the
  // SRM projects section (?project=<id>). Auto-expands that project on load.
  const initialProjectId = $derived.by(() => {
    const fromRoute = Number($currentRoute.params?.id) || null;
    if (fromRoute) return fromRoute;
    try {
      return Number(new URLSearchParams(window.location.search).get('project')) || null;
    } catch (e) {
      return null;
    }
  });

  // A route deep link always lands on the overview tab, where the detail renders.
  $effect(() => {
    if ($currentRoute.params?.id) activeTab = 'overview';
  });
  const programmeCount = $derived(
    hierarchy.portfolios.reduce((n, pf) => n + (pf.programmes?.length ?? 0), 0)
  );
  const projectCount = $derived(
    hierarchy.portfolios.reduce(
      (n, pf) =>
        n +
        (pf.standalone_projects?.length ?? 0) +
        (pf.programmes?.reduce((m, pg) => m + (pg.projects?.length ?? 0), 0) ?? 0),
      0
    )
  );

  async function loadPlanMap() {
    try {
      const res = await plansApi.getP3MMap();
      const pairs = res?.data ?? (Array.isArray(res) ? res : []);
      const m = new Map();
      for (const p of pairs) {
        const key = `${p.entity_type}:${p.entity_id}`;
        let arr = m.get(key);
        if (!arr) {
          arr = [];
          m.set(key, arr);
        }
        arr.push({ plan_id: p.plan_id, plan_name: p.plan_name, plan_color: p.plan_color });
      }
      planMap = m;
    } catch (e) {
      // Plans are optional; the P3M views simply show no linked-plan chips.
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [h, s] = await Promise.all([p3mApi.getHierarchy(), p3mApi.getExecutiveSummary()]);
      hierarchy = h?.portfolios ? h : { portfolios: [] };
      summary = s ?? null;
      hasLoaded = true;
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
    loadPlanMap();
  }

  async function loadLifecycle() {
    try {
      const cfg = await p3mApi.getLifecycleConfig();
      lifecycle = { stages: cfg?.stages ?? [], gate_templates: cfg?.gate_templates ?? [] };
    } catch (e) {
      // Lifecycle config is optional at runtime; views fall back to the legacy
      // four-stage labels when it cannot be loaded.
    }
  }

  onMount(() => {
    load();
    loadLifecycle();
  });
</script>

<div class="px-6">
  <PageHeader icon={IconTarget} title={t('p3m.title')} subtitle={t('p3m.subtitle')} marginClass="mt-4 mb-8">
    {#snippet actions()}
      <div class="flex items-center gap-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        <span>{portfolioCount} {t('p3m.portfolios')}</span>
        <span aria-hidden="true">·</span>
        <span>{programmeCount} {t('p3m.programmes')}</span>
        <span aria-hidden="true">·</span>
        <span>{projectCount} {t('p3m.projects')}</span>
      </div>
    {/snippet}
  </PageHeader>
</div>

<P3MHierarchyGuide />

<div class="flex gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] px-6 pt-2">
  {#each TABS as tab (tab.id)}
    <button
      type="button"
      class="p3m-tab"
      class:active={activeTab === tab.id}
      onclick={() => (activeTab = tab.id)}
    >
      {t(tab.labelKey)}
    </button>
  {/each}
</div>

{#if loading && !hasLoaded}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && !hasLoaded}
  <EmptyState
    title={t('p3m.loadErrorTitle')}
    description={error}
    actions={{ label: t('p3m.retry'), onClick: load }}
  />
{:else}
  {#if activeTab === 'frontdoor'}
    <P3MFrontDoor portfolios={hierarchy.portfolios} onChanged={load} />
  {:else if activeTab === 'overview'}
    <P3MPortfolioPulse />
    <div class="mt-4">
      <P3MOverview portfolios={hierarchy.portfolios} summary={summary} onChanged={load} initialProjectId={initialProjectId} planMap={planMap} stages={lifecycle.stages} />
    </div>
  {:else if activeTab === 'investment'}
    <P3MInvestmentCases portfolios={hierarchy.portfolios} onChanged={load} />
  {:else if activeTab === 'governance'}
    <P3MGovernance portfolios={hierarchy.portfolios} onChanged={load} stages={lifecycle.stages} />
  {:else if activeTab === 'benefits'}
    <P3MBenefitsFinancials portfolios={hierarchy.portfolios} />
  {:else if activeTab === 'risk'}
    <P3MRiskStakeholders portfolios={hierarchy.portfolios} />
  {:else if activeTab === 'change'}
    <P3MChangeCalendar portfolios={hierarchy.portfolios} />
  {:else if activeTab === 'reporting'}
    <P3MReporting portfolios={hierarchy.portfolios} />
  {:else}
    <P3MResources portfolios={hierarchy.portfolios} />
  {/if}
{/if}

<style>
  .p3m-tab {
    padding: 0.5rem 0.875rem;
    font-size: 0.8125rem;
    color: var(--color-ds-text-muted, #6b7280);
    border-bottom: 2px solid transparent;
    cursor: pointer;
    background: none;
    border-top: none;
    border-left: none;
    border-right: none;
  }

  .p3m-tab:hover {
    color: var(--color-ds-text, #111827);
  }

  .p3m-tab.active {
    color: var(--color-ds-accent-blue, #2563eb);
    border-bottom-color: var(--color-ds-accent-blue, #2563eb);
  }
</style>
