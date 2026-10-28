<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { formatDate, quadrantLabel } from './utils.js';

  let { portfolios = [] } = $props();

  let raidItems = $state([]);
  let stakeholders = $state([]);
  let communications = $state([]);
  let loading = $state(true);
  let error = $state('');

  const entityTitleById = $derived.by(() => {
    const m = new Map();
    for (const pf of portfolios) {
      m.set(`portfolio:${pf.id}`, pf.title);
      for (const pg of pf.programmes ?? []) {
        m.set(`programme:${pg.id}`, `${pf.title} › ${pg.title}`);
        for (const pr of pg.projects ?? []) m.set(`project:${pr.id}`, `${pf.title} › ${pg.title} › ${pr.title}`);
      }
    }
    return m;
  });

  function entityLabel(e) {
    return entityTitleById.get(`${e.entity_type}:${e.entity_id}`) ?? `#${e.entity_id}`;
  }

  const statusChip = (status) => {
    switch (status) {
      case 'open':
        return 'bg-red-100 text-red-700';
      case 'mitigating':
        return 'bg-amber-100 text-amber-700';
      case 'closed':
        return 'bg-emerald-100 text-emerald-700';
      case 'escalated':
        return 'bg-purple-100 text-purple-700';
      default:
        return 'bg-slate-100 text-slate-600';
    }
  };

  const bucketChip = (bucket) => {
    switch (bucket) {
      case 'high':
        return 'bg-red-100 text-red-700';
      case 'medium':
        return 'bg-amber-100 text-amber-700';
      default:
        return 'bg-emerald-100 text-emerald-700';
    }
  };

  const quadrantColor = (quadrant) => {
    switch (quadrant) {
      case 'manage_closely':
        return '#ef4444';
      case 'keep_satisfied':
        return '#f59e0b';
      case 'monitor':
        return '#3b82f6';
      default:
        return '#10b981';
    }
  };

  /** Stakeholders grouped by quadrant for the power/interest matrix. */
  const quadrants = $derived.by(() => {
    const q = { manage_closely: [], keep_satisfied: [], monitor: [], keep_informed: [] };
    for (const s of stakeholders) {
      if (q[s.quadrant]) q[s.quadrant].push(s);
    }
    return q;
  });

  async function load() {
    loading = true;
    error = '';
    try {
      const [r, s, c] = await Promise.all([p3mApi.raidItems.getAll(), p3mApi.stakeholders.getAll(), p3mApi.communications.getAll()]);
      raidItems = r ?? [];
      stakeholders = s ?? [];
      communications = c ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && raidItems.length === 0 && stakeholders.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- RAID items -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.raidItems')}</h2>
      {#if raidItems.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.title')}</th>
              <th class="py-1.5 pr-3">{t('p3m.itemType')}</th>
              <th class="py-1.5 pr-3">I×P</th>
              <th class="py-1.5 pr-3">{t('p3m.priorityBucket')}</th>
              <th class="py-1.5 pr-3">Status</th>
            </tr>
          </thead>
          <tbody>
            {#each raidItems as item (item.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] align-top">
                <td class="py-1.5 pr-3">{entityLabel(item)}</td>
                <td class="py-1.5 pr-3">
                  {item.title}
                  {#if item.requires_escalation}
                    <span class="ml-2 rounded bg-purple-100 px-1 text-[10px] text-purple-700">{t('p3m.statusEscalated')}</span>
                  {/if}
                  {#if item.mitigation_plan}
                    <div class="mt-0.5 text-[var(--color-ds-text-muted,#6b7280)]">{item.mitigation_plan}</div>
                  {/if}
                </td>
                <td class="py-1.5 pr-3 capitalize">{item.item_type}</td>
                <td class="py-1.5 pr-3">
                  {item.probability_score}×{item.impact_score} = <span class="font-semibold">{item.risk_score}</span>
                </td>
                <td class="py-1.5 pr-3">
                  <span class={bucketChip(item.priority_bucket)}>{t(`p3m.bucket${item.priority_bucket === 'high' ? 'High' : item.priority_bucket === 'medium' ? 'Medium' : 'Low'}`)}</span>
                </td>
                <td class="py-1.5 pr-3">
                  <span class={statusChip(item.status)}>{t(`p3m.${item.status === 'open' ? 'statusOpen' : item.status === 'mitigating' ? 'statusMitigating' : item.status === 'closed' ? 'statusClosed' : 'statusEscalated'}`)}</span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Stakeholder power/interest matrix -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.stakeholders')}</h2>
      {#if stakeholders.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          {#each [
            ['manage_closely', 'quadrantManageClosely'],
            ['keep_satisfied', 'quadrantKeepSatisfied'],
            ['monitor', 'quadrantMonitor'],
            ['keep_informed', 'quadrantKeepInformed']
          ] as [key, labelKey] (key)}
            <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
              <div class="mb-2 flex items-center gap-2 text-xs font-semibold">
                <span class="inline-block h-2.5 w-2.5 rounded-full" style="background: {quadrantColor(key)}"></span>
                {t(`p3m.${labelKey}`)}
              </div>
              {#if quadrants[key].length === 0}
                <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
              {:else}
                {#each quadrants[key] as s (s.id)}
                  <div class="flex items-center justify-between gap-2 border-t border-[var(--color-ds-border,#e5e7eb)] py-1.5 text-xs first:border-t-0">
                    <span>
                      {s.name}
                      {#if s.role_title}
                        <span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {s.role_title}</span>
                      {/if}
                    </span>
                    <span class="flex items-center gap-2 whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">
                      <span title={t('p3m.powerScore')}>P{s.power_score}</span>
                      <span title={t('p3m.interestScore')}>I{s.interest_score}</span>
                    </span>
                  </div>
                {/each}
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <!-- Communications log -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.communications')}</h2>
      {#if communications.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.recordedDate')}</th>
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.audience')}</th>
              <th class="py-1.5 pr-3">{t('p3m.channel')}</th>
              <th class="py-1.5 pr-3">{t('p3m.summary')}</th>
            </tr>
          </thead>
          <tbody>
            {#each [...communications].sort((a, b) => (a.date > b.date ? -1 : 1)) as c (c.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{formatDate(c.date)}</td>
                <td class="py-1.5 pr-3">{entityLabel(c)}</td>
                <td class="py-1.5 pr-3">{c.audience}</td>
                <td class="py-1.5 pr-3 capitalize">{c.channel}</td>
                <td class="py-1.5 pr-3">{c.summary}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
