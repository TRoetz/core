<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { formatMoney, formatDate } from './utils.js';

  let { portfolios = [] } = $props();

  let resources = $state([]);
  let allocations = $state([]);
  let capacityRows = $state([]);
  let loading = $state(true);
  let error = $state('');

  const resourceNameById = $derived(new Map(resources.map((r) => [r.id, r.name])));

  const projectTitleById = $derived.by(() => {
    const m = new Map();
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) m.set(pr.id, `${pf.title} › ${pg.title} › ${pr.title}`);
      }
    }
    return m;
  });

  /** Distinct months present in capacity rows, sorted. */
  const months = $derived.by(() => {
    const set = new Set(capacityRows.map((r) => r.month));
    return [...set].sort();
  });

  /** resource_id -> month -> row for heatmap lookup. */
  const heatMap = $derived.by(() => {
    const m = new Map();
    for (const row of capacityRows) {
      if (!m.has(row.resource_id)) m.set(row.resource_id, {});
      m.get(row.resource_id)[row.month] = row;
    }
    return m;
  });

  function heatStyle(row) {
    if (!row || !row.capacity_hours) return 'background: var(--ds-surface-hovered)';
    const ratio = row.demand_hours / row.capacity_hours;
    if (ratio > 1) return 'background: #ef4444; color: white';
    if (ratio >= 0.85) return 'background: #f59e0b; color: white';
    if (ratio >= 0.6) return 'background: #fde68a';
    if (ratio > 0) return 'background: #bbf7d0';
    return 'background: var(--ds-surface-hovered)';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [r, a, c] = await Promise.all([p3mApi.resources.getAll(), p3mApi.resourceAllocations.getAll(), p3mApi.getResourceCapacity()]);
      resources = r ?? [];
      allocations = a ?? [];
      capacityRows = c ?? [];
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
{:else if error && resources.length === 0 && allocations.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- Resource inventory -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.resources')}</h2>
      {#if resources.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.title')}</th>
              <th class="py-1.5 pr-3">{t('p3m.skills')}</th>
              <th class="py-1.5 pr-3">{t('p3m.costRate')}</th>
              <th class="py-1.5 pr-3">{t('p3m.capacityHoursPerWeek')}</th>
            </tr>
          </thead>
          <tbody>
            {#each resources as r (r.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3 font-medium">{r.name}</td>
                <td class="py-1.5 pr-3">
                  {#each r.skills ?? [] as skill (skill)}
                    <span class="mr-1 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-600">{skill}</span>
                  {/each}
                </td>
                <td class="py-1.5 pr-3">{formatMoney(r.cost_rate)}/h</td>
                <td class="py-1.5 pr-3">{r.capacity_hours_per_week} h</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Allocations -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.allocations')}</h2>
      {#if allocations.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.resources')}</th>
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.startDate')} → {t('p3m.endDate')}</th>
              <th class="py-1.5 pr-3">{t('p3m.hoursPerWeek')}</th>
              <th class="py-1.5 pr-3">{t('p3m.bookingType')}</th>
            </tr>
          </thead>
          <tbody>
            {#each allocations as a (a.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{resourceNameById.get(a.resource_id) ?? `#${a.resource_id}`}</td>
                <td class="py-1.5 pr-3">{a.project_id != null ? projectTitleById.get(a.project_id) ?? `#${a.project_id}` : '—'}</td>
                <td class="py-1.5 pr-3">{formatDate(a.start_date)} → {formatDate(a.end_date)}</td>
                <td class="py-1.5 pr-3">{a.hours_per_week} h</td>
                <td class="py-1.5 pr-3">
                  <span class={a.booking_type === 'hard' ? 'rounded bg-blue-100 px-1.5 py-0.5 text-[10px] text-blue-700' : 'rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-600'}>
                    {a.booking_type}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Capacity heatmap -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.capacityView')}</h2>
      {#if capacityRows.length === 0 || months.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <div class="overflow-x-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <table class="text-xs">
            <thead>
              <tr>
                <th class="py-1 pr-3 text-left font-medium">{t('p3m.resources')}</th>
                {#each months as month (month)}
                  <th class="px-1 py-1 text-center font-medium text-[var(--color-ds-text-muted,#6b7280)]">
                    {month.slice(5)}/{month.slice(2, 4)}
                  </th>
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each resources as r (r.id)}
                <tr>
                  <td class="max-w-40 truncate py-1 pr-3">{r.name}</td>
                  {#each months as month (month)}
                    {@const row = heatMap.get(r.id)?.[month]}
                    <td class="p-0.5">
                      <div
                        class="flex h-7 min-w-12 items-center justify-center rounded text-[10px]"
                        style={heatStyle(row)}
                        title={row ? `${r.name} · ${month}: ${t('p3m.demand')} {row.demand_hours}h / {t('p3m.capacity')} {row.capacity_hours}h` : ''}
                      >
                        {#if row && row.capacity_hours}
                          {Math.round((row.demand_hours / row.capacity_hours) * 100)}%
                        {/if}
                      </div>
                    </td>
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        <div class="mt-1 flex items-center gap-4 text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-[#bbf7d0]"></span>&lt; 60%</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-[#fde68a]"></span>60–85%</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-[#f59e0b]"></span>85–100%</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-[#ef4444]"></span>{t('p3m.overAllocated')}</span>
        </div>
      {/if}
    </section>
  </div>
{/if}
