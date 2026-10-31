<script>
  import { t } from '../../../stores/i18n.svelte.js';
  import {
    classificationLabel,
    engagementLabel,
    lifecycleLabel,
    relationshipLabel,
  } from '../utils.js';

  /** @type {{ dashboard: object, onRefreshed?: Function }} */
  let { dashboard = {}, onRefreshed } = $props();

  const totals = $derived(dashboard.totals ?? {});
  const matrixRows = [5, 4, 3, 2, 1];
  const matrixCols = [1, 2, 3, 4, 5];

  function cellEntries(influence, interest) {
    return (dashboard.matrix ?? []).filter(
      (m) => m.influence_level === influence && m.interest_level === interest,
    );
  }

  function healthBucketLabel(bucket) {
    switch (bucket) {
      case '0-25': return t('sms.healthCritical');
      case '26-50': return t('sms.healthWeak');
      case '51-75': return t('sms.healthFair');
      case '76-100': return t('sms.healthStrong');
      default: return bucket;
    }
  }

  const CARDS = [
    { key: 'stakeholders', labelKey: 'sms.statStakeholders' },
    { key: 'open_issues', labelKey: 'sms.statOpenIssues' },
    { key: 'open_tasks', labelKey: 'sms.statOpenTasks' },
    { key: 'open_feedback', labelKey: 'sms.statOpenFeedback' },
  ];

  const byClassification = $derived(dashboard.by_classification ?? []);
  const byEngagement = $derived(dashboard.by_engagement_status ?? []);
  const byLifecycle = $derived(dashboard.by_lifecycle_stage ?? []);
  const byRelationship = $derived(dashboard.relationship_distribution ?? []);

  const DISTRIBUTIONS = [
    { data: byClassification, titleKey: 'sms.classificationDist', label: classificationLabel },
    { data: byEngagement, titleKey: 'sms.engagementDist', label: engagementLabel },
    { data: byLifecycle, titleKey: 'sms.lifecycleDist', label: lifecycleLabel },
    { data: byRelationship, titleKey: 'sms.relationshipDist', label: relationshipLabel },
  ];
</script>

<div class="space-y-6 px-6 py-4">
  <!-- Stat cards -->
  <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
    {#each CARDS as card (card.key)}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <div class="text-xl font-semibold">{totals[card.key] ?? 0}</div>
        <div class="mt-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t(card.labelKey)}</div>
      </div>
    {/each}
  </div>

  <!-- Influence x interest matrix -->
  <section>
    <h2 class="mb-2 text-sm font-semibold">{t('sms.matrixTitle')}</h2>
    <div class="overflow-x-auto">
      <table class="w-full border-separate border-spacing-1 text-xs">
        <thead>
          <tr>
            <th class="px-2 py-1 text-left font-medium text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.interestAxis')}</th>
            {#each matrixCols as col (col)}
              <th class="px-2 py-1 text-center font-medium text-[var(--color-ds-text-muted,#6b7280)]">{col}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each matrixRows as row (row)}
            <tr>
              <td class="px-2 py-1 text-right font-medium text-[var(--color-ds-text-muted,#6b7280)]">{row}</td>
              {#each matrixCols as col (col)}
                {@const entries = cellEntries(col, row)}
                <td
                  class="min-w-24 rounded border p-1.5 align-top {entries.length === 0 ? 'border-[var(--color-ds-border,#e5e7eb)]' : ''} {row >= 4 && col >= 4 ? 'bg-sky-500/20' : (row >= 4) !== (col >= 4) ? 'bg-amber-500/20' : ''}"
                >
                  {#if entries.length > 0}
                    <div class="flex flex-col gap-0.5">
                      {#each entries as m (m.stakeholder_id)}
                        <span>{m.name}</span>
                      {/each}
                    </div>
                  {/if}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </section>

  <!-- Distributions -->
  <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
    {#each DISTRIBUTIONS as dist (dist.titleKey)}
      <section class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <h3 class="mb-2 text-xs font-semibold">{t(dist.titleKey)}</h3>
        {#if dist.data.length === 0}
          <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <tbody>
              {#each dist.data as row (row.value)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1 pr-3">{dist.label(row.value)}</td>
                  <td class="py-1 text-right font-medium">{row.count}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>
    {/each}
  </div>

  <!-- Health buckets + activity counters -->
  <div class="flex flex-wrap items-center gap-2">
    {#each dashboard.health_buckets ?? [] as bucket (bucket.value)}
      <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600" title={healthBucketLabel(bucket.value)}>
        {healthBucketLabel(bucket.value)}: {bucket.count}
      </span>
    {/each}
    <span class="rounded-full bg-amber-100 px-2 py-0.5 text-xs text-amber-700">
      {t('sms.tasksDueSoon')}: {dashboard.tasks_due_soon_14d ?? 0}
    </span>
    <span class="rounded-full bg-sky-100 px-2 py-0.5 text-xs text-sky-700">
      {t('sms.commsLast30d')}: {dashboard.communications_last_30d ?? 0}
    </span>
  </div>
</div>
