<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconTarget from '@tabler/icons-svelte-runes/icons/target';
  import P3MOverview from './P3MOverview.svelte';
  import P3MGovernance from './P3MGovernance.svelte';
  import P3MBenefitsFinancials from './P3MBenefitsFinancials.svelte';
  import P3MRiskStakeholders from './P3MRiskStakeholders.svelte';
  import P3MResources from './P3MResources.svelte';

  const TABS = [
    { id: 'overview', labelKey: 'p3m.overview' },
    { id: 'governance', labelKey: 'p3m.governance' },
    { id: 'benefits', labelKey: 'p3m.benefitsTab' },
    { id: 'risk', labelKey: 'p3m.riskTab' },
    { id: 'resources', labelKey: 'p3m.resourcesTab' },
  ];

  let activeTab = $state('overview');
  let loading = $state(true);
  let error = $state('');
  /** @type {{ portfolios: any[] }} */
  let hierarchy = $state({ portfolios: [] });
  /** @type {any} */
  let summary = $state(null);

  const portfolioCount = $derived(hierarchy.portfolios.length);

  // Deep link from the SRM projects section (?project=<id>): auto-expand that
  // project's detail on first load.
  const initialProjectId = (() => {
    try {
      return Number(new URLSearchParams(window.location.search).get('project')) || null;
    } catch (e) {
      return null;
    }
  })();
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

  async function load() {
    loading = true;
    error = '';
    try {
      const [h, s] = await Promise.all([p3mApi.getHierarchy(), p3mApi.getExecutiveSummary()]);
      hierarchy = h?.portfolios ? h : { portfolios: [] };
      summary = s ?? null;
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

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

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error}
  <EmptyState
    title={t('p3m.loadErrorTitle')}
    description={error}
    actions={{ label: t('p3m.retry'), onClick: load }}
  />
{:else}
  {#if activeTab === 'overview'}
    <P3MOverview portfolios={hierarchy.portfolios} summary={summary} onChanged={load} initialProjectId={initialProjectId} />
  {:else if activeTab === 'governance'}
    <P3MGovernance />
  {:else if activeTab === 'benefits'}
    <P3MBenefitsFinancials />
  {:else if activeTab === 'risk'}
    <P3MRiskStakeholders />
  {:else}
    <P3MResources />
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
