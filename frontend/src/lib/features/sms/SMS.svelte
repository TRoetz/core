<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../api/sms.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconUsers from '@tabler/icons-svelte-runes/icons/users';
  import SMSOverview from './sections/Overview.svelte';
  import SMSStakeholders from './sections/Stakeholders.svelte';
  import SMSEngagement from './sections/Engagement.svelte';
  import SMSReports from './sections/Reports.svelte';

  const TABS = [
    { id: 'overview', labelKey: 'sms.overviewTab' },
    { id: 'stakeholders', labelKey: 'sms.stakeholdersTab' },
    { id: 'engagement', labelKey: 'sms.engagementTab' },
    { id: 'reports', labelKey: 'sms.reportsTab' },
  ];

  let activeTab = $state('overview');
  let loading = $state(true);
  let error = $state('');
  /** @type {{ totals?: Record<string, number>, tasks_due_soon_14d?: number }} */
  let dashboard = $state({});

  const stakeholderTotal = $derived(dashboard.totals?.stakeholders ?? 0);
  const openIssues = $derived(dashboard.totals?.open_issues ?? 0);

  async function load() {
    loading = true;
    error = '';
    try {
      const data = await smsApi.getDashboard();
      dashboard = data ?? {};
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<PageHeader icon={IconUsers} title={t('sms.title')} subtitle={t('sms.subtitle')} marginClass="mt-4 mb-8">
  {#snippet actions()}
    <div class="flex items-center gap-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      <span>{stakeholderTotal} {t('sms.statStakeholders')}</span>
      <span aria-hidden="true">·</span>
      <span>{openIssues} {t('sms.statOpenIssues')}</span>
    </div>
  {/snippet}
</PageHeader>

<div class="flex gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] px-6 pt-2">
  {#each TABS as tab (tab.id)}
    <button
      type="button"
      class="sms-tab"
      class:active={activeTab === tab.id}
      onclick={() => (activeTab = tab.id)}
    >
      {t(tab.labelKey)}
    </button>
  {/each}
</div>

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('sms.loading')}
  </div>
{:else if error}
  <EmptyState title={t('sms.loadErrorTitle')} description={error} actions={{ label: t('sms.retry'), onClick: load }} />
{:else}
  {#if activeTab === 'overview'}
    <SMSOverview dashboard={dashboard} onRefreshed={load} />
  {:else if activeTab === 'stakeholders'}
    <SMSStakeholders />
  {:else if activeTab === 'engagement'}
    <SMSEngagement />
  {:else}
    <SMSReports dashboard={dashboard} />
  {/if}
{/if}

<style>
  .sms-tab {
    padding: 0.5rem 1rem;
    font-size: 0.875rem;
    color: var(--color-ds-text-muted, #6b7280);
    border-bottom: 2px solid transparent;
  }

  .sms-tab:hover {
    color: var(--color-ds-text, #111827);
  }

  .sms-tab.active {
    color: var(--color-ds-accent, #2563eb);
    border-bottom-color: var(--color-ds-accent, #2563eb);
  }
</style>
