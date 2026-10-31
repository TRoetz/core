<script>
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { classificationLabel, engagementLabel, lifecycleLabel, relationshipLabel } from '../utils.js';

  /** @type {{ dashboard: object }} */
  let { dashboard = {} } = $props();

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

  const TABLES = [
    { data: byClassification, titleKey: 'sms.classificationDist', label: classificationLabel },
    { data: byEngagement, titleKey: 'sms.engagementDist', label: engagementLabel },
    { data: byLifecycle, titleKey: 'sms.lifecycleDist', label: lifecycleLabel },
    { data: byRelationship, titleKey: 'sms.relationshipDist', label: relationshipLabel },
  ];

  const stakeholderTotal = $derived(byClassification.reduce((sum, r) => sum + (r.count ?? 0), 0));
</script>

<div class="space-y-6 px-6 py-4">
  <!-- CSV exports -->
  <div class="flex flex-wrap gap-2">
    <a
      href={smsApi.exportUrl('stakeholders')}
      download
      class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-ds-background-neutral-hovered"
    >
      {t('sms.exportStakeholders')}
    </a>
    <a
      href={smsApi.exportUrl('communications')}
      download
      class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-xs hover:bg-ds-background-neutral-hovered"
    >
      {t('sms.exportCommunications')}
    </a>
  </div>

  <!-- Distribution tables -->
  <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
    {#each TABLES as table (table.titleKey)}
      <section class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
        <h3 class="mb-2 text-xs font-semibold">{t(table.titleKey)}</h3>
        {#if table.data.length === 0}
          <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <table class="w-full text-left text-xs">
            <thead>
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                <th class="py-1.5 pr-3">{t('sms.value')}</th>
                <th class="py-1.5 pr-3 text-right">{t('sms.count')}</th>
                <th class="py-1.5 text-right">{t('sms.percent')}</th>
              </tr>
            </thead>
            <tbody>
              {#each table.data as row (row.value)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3">{table.label(row.value)}</td>
                  <td class="py-1.5 pr-3 text-right font-medium">{row.count}</td>
                  <td class="py-1.5 text-right text-[var(--color-ds-text-muted,#6b7280)]">
                    {percent(row.count, stakeholderTotal)}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>
    {/each}

    <!-- Health score distribution -->
    <section class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <h3 class="mb-2 text-xs font-semibold">{t('sms.healthBuckets')}</h3>
      {#if healthBuckets.length === 0}
        <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.value')}</th>
              <th class="py-1.5 text-right">{t('sms.count')}</th>
            </tr>
          </thead>
          <tbody>
            {#each healthBuckets as bucket (bucket.value)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="py-1.5 pr-3">{bucketLabel(bucket.value)}</td>
                <td class="py-1.5 text-right font-medium">{bucket.count}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
</div>
