<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { srm as srmApi } from '../../api/srm.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconHeartHandshake from '@tabler/icons-svelte-runes/icons/heart-handshake';
  import { formatMoney, formatDate } from './utils.js';
  import SRMOverview from './SRMOverview.svelte';
  import SRMSegmentation from './SRMSegmentation.svelte';
  import SRMPlanning from './SRMPlanning.svelte';
  import SRMMonitoring from './SRMMonitoring.svelte';
  import SRMStrategyWheel from './SRMStrategyWheel.svelte';

  const TABS = [
    { id: 'overview', labelKey: 'srm.overviewTab' },
    { id: 'segmentation', labelKey: 'srm.segmentationTab' },
    { id: 'planning', labelKey: 'srm.planningTab' },
    { id: 'monitoring', labelKey: 'srm.monitoringTab' },
  ];

  let activeTab = $state('overview');
  // Sub-tab of the Planning & Governance tab to open when arriving via the
  // strategy wheel (empty string keeps the component default).
  let planningSection = $state('');
  let loading = $state(true);
  let error = $state('');
  /** @type {{ suppliers?: { total?: number, by_state?: Record<string, number>, by_tier?: Record<string, number> }, contracts?: { by_status?: Record<string, number>, active_value_nzd?: number, upcoming_reviews?: any[] }, open_disputes?: number, pending_extractions?: number }} */
  let summary = $state({});

  const supplierTotal = $derived(summary.suppliers?.total ?? 0);
  const activeValue = $derived(summary.contracts?.active_value_nzd ?? 0);
  const upcomingReviews = $derived(summary.contracts?.upcoming_reviews ?? []);

  async function load() {
    loading = true;
    error = '';
    try {
      summary = (await srmApi.getSummary()) ?? {};
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  // Strategy wheel quadrants map onto the tabs: Preparation opens
  // Segmentation & Validation (contract management lives in Overview, the
  // default landing view), Planning and Implementation both open Planning &
  // Governance but at different sub-tabs, Monitoring opens its own tab.
  function handleWheel(phase) {
    if (phase === 'preparation') {
      activeTab = 'segmentation';
    } else if (phase === 'planning' || phase === 'implementation') {
      planningSection = phase === 'implementation' ? 'meetings' : '';
      activeTab = 'planning';
    } else if (phase === 'monitoring') {
      activeTab = 'monitoring';
    }
  }

  onMount(load);
</script>

<PageHeader icon={IconHeartHandshake} title={t('srm.title')} subtitle={t('srm.subtitle')} marginClass="mt-4 mb-8">
  {#snippet actions()}
    <div class="flex items-center gap-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      <span>{supplierTotal} {t('srm.suppliersTotal')}</span>
      <span aria-hidden="true">·</span>
      <span>{formatMoney(activeValue)} {t('srm.activeRelationships')}</span>
    </div>
  {/snippet}
</PageHeader>

<div class="flex gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] px-6 pt-2">
  {#each TABS as tab (tab.id)}
    <button
      type="button"
      class="srm-tab"
      class:active={activeTab === tab.id}
      onclick={() => (activeTab = tab.id)}
    >
      {t(tab.labelKey)}
    </button>
  {/each}
</div>

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('srm.loading')}
  </div>
{:else if error}
  <EmptyState title={t('srm.loadErrorTitle')} description={error} actions={{ label: t('srm.retry'), onClick: load }} />
{:else if supplierTotal === 0}
  <EmptyState title={t('srm.emptyTitle')} description={t('srm.emptyDescription')} />
{:else}
  {#if activeTab === 'overview'}
    <div class="px-6 pt-4">
      <SRMStrategyWheel onNavigate={handleWheel} />
    </div>
    <SRMOverview summary={summary} onRefreshed={load} />
  {:else if activeTab === 'segmentation'}
    <SRMSegmentation />
  {:else if activeTab === 'planning'}
    <SRMPlanning initialSection={planningSection} />
  {:else}
    <SRMMonitoring />
  {/if}
{/if}

<style>
  .srm-tab {
    padding: 0.5rem 1rem;
    font-size: 0.875rem;
    color: var(--color-ds-text-muted, #6b7280);
    border-bottom: 2px solid transparent;
  }

  .srm-tab:hover {
    color: var(--color-ds-text, #111827);
  }

  .srm-tab.active {
    color: var(--color-ds-accent, #2563eb);
    border-bottom-color: var(--color-ds-accent, #2563eb);
  }
</style>
