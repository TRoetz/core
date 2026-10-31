<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import { formatMoney, formatDate } from './utils.js';

  let { portfolios = [] } = $props();

  let benefits = $state([]);
  let tranches = $state([]);
  let forecast = $state([]);
  let loading = $state(true);
  let error = $state('');

  const expandedBenefits = $state({}); // benefit id -> realizations[] | undefined (loading)
  const realizationCache = $state({});

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

  function entityLabel(b) {
    if (b.project_id != null) return entityTitleById.get(`project:${b.project_id}`) ?? `#${b.project_id}`;
    if (b.programme_id != null) return entityTitleById.get(`programme:${b.programme_id}`) ?? `#${b.programme_id}`;
    return '—';
  }

  function benefitProgress(b) {
    if (!b.target_value || b.latest_realized_value == null) return 0;
    const pct = (b.latest_realized_value / b.target_value) * 100;
    return Math.max(0, Math.min(100, pct));
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [b, tr, f] = await Promise.all([p3mApi.benefits.getAll(), p3mApi.tranches.getAll(), p3mApi.getForecast()]);
      benefits = b ?? [];
      tranches = tr ?? [];
      forecast = f ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function toggleBenefit(b) {
    if (expandedBenefits[b.id] !== undefined) {
      expandedBenefits[b.id] = undefined;
      return;
    }
    expandedBenefits[b.id] = []; // mark loading
    try {
      const rows = await p3mApi.getBenefitRealizations(b.id);
      realizationCache[b.id] = rows ?? [];
      expandedBenefits[b.id] = realizationCache[b.id];
    } catch (e) {
      error = e?.message || 'error';
      expandedBenefits[b.id] = undefined;
    }
  }

  const maxForecastValue = $derived(forecast.reduce((m, p) => Math.max(m, p.capex_total ?? 0, p.opex_total ?? 0), 0));

  onMount(load);
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && benefits.length === 0 && tranches.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- Benefits -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.benefits')}</h2>
      {#if benefits.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3"></th>
              <th class="py-1.5 pr-3">{t('p3m.benefits')}</th>
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.targetValue')}</th>
              <th class="py-1.5 pr-3">{t('p3m.baselineValue')}</th>
              <th class="py-1.5 pr-3">{t('p3m.latestRealized')}</th>
              <th class="w-28 py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each benefits as b (b.id)}
              {@const open = expandedBenefits[b.id]}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">
                  <button type="button" class="cursor-pointer" onclick={() => toggleBenefit(b)} aria-label={t('p3m.realizations')}>
                    <IconChevronRight size={14} class={open !== undefined ? 'rotated' : ''} />
                  </button>
                </td>
                <td class="py-1.5 pr-3">
                  {b.title}
                  <span class="ml-2 rounded bg-slate-100 px-1 text-[10px] capitalize text-slate-500">{b.benefit_type}</span>
                </td>
                <td class="py-1.5 pr-3">{entityLabel(b)}</td>
                <td class="py-1.5 pr-3">
                  {formatMoney(b.target_value)}
                  {#if b.unit_of_measure}
                    <span class="text-[var(--color-ds-text-muted,#6b7280)]"> {b.unit_of_measure}</span>
                  {/if}
                </td>
                <td class="py-1.5 pr-3">{formatMoney(b.baseline_value)}</td>
                <td class="py-1.5 pr-3">{b.latest_realized_value != null ? formatMoney(b.latest_realized_value) : t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3">
                  <div class="h-1.5 w-full overflow-hidden rounded bg-[var(--color-ds-border,#e5e7eb)]">
                    <div class="h-full rounded" style="width: {benefitProgress(b)}%; background: #2563eb"></div>
                  </div>
                </td>
              </tr>
              {#if open !== undefined}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td></td>
                  <td colspan="6" class="py-1.5 pr-3 pl-8">
                    {#if open.length === 0}
                      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</span>
                    {:else}
                      {#each [...open].sort((x, y) => (x.recorded_at > y.recorded_at ? -1 : 1)) as r (r.id)}
                        <div class="flex items-center gap-3 py-0.5">
                          <span>{formatDate(r.recorded_at)}</span>
                          <span class="font-medium">{formatMoney(r.realized_value)}</span>
                          {#if r.notes}
                            <span class="text-[var(--color-ds-text-muted,#6b7280)]">{r.notes}</span>
                          {/if}
                        </div>
                      {/each}
                    {/if}
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Tranches -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.tranches')}</h2>
      {#if tranches.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.title')}</th>
              <th class="py-1.5 pr-3">{t('p3m.amount')}</th>
              <th class="py-1.5 pr-3">{t('p3m.expenditureType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.plannedDate')}</th>
              <th class="py-1.5 pr-3">Status</th>
            </tr>
          </thead>
          <tbody>
            {#each tranches as tr (tr.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{entityTitleById.get(`${tr.entity_type}:${tr.entity_id}`) ?? `#${tr.entity_id}`}</td>
                <td class="py-1.5 pr-3">{tr.title || '—'}</td>
                <td class="py-1.5 pr-3">{formatMoney(tr.amount)}</td>
                <td class="py-1.5 pr-3">
                  <span class={tr.expenditure_type === 'capex' ? 'rounded bg-blue-100 px-1.5 py-0.5 text-[10px] text-blue-700' : 'rounded bg-violet-100 px-1.5 py-0.5 text-[10px] text-violet-700'}>
                    {tr.expenditure_type}
                  </span>
                </td>
                <td class="py-1.5 pr-3">{formatDate(tr.planned_date)}</td>
                <td class="py-1.5 pr-3 capitalize">{tr.status}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Forecast -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.forecast')}</h2>
      {#if forecast.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <div class="flex items-end gap-1 overflow-x-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" style="min-height: 160px">
          {#each forecast as point (point.month)}
            {@const capexH = maxForecastValue ? ((point.capex_total ?? 0) / maxForecastValue) * 120 : 0}
            {@const opexH = maxForecastValue ? ((point.opex_total ?? 0) / maxForecastValue) * 120 : 0}
            <div class="flex min-w-8 flex-col items-center gap-1" title="{point.month}: {t('p3m.capex')} {formatMoney(point.capex_total)} · {t('p3m.opex')} {formatMoney(point.opex_total)}">
              <div class="flex h-[120px] w-full items-end justify-center gap-0.5">
                <div class="w-2 rounded-t bg-blue-500" style="height: {Math.max(capexH, point.capex_total ? 2 : 0)}px"></div>
                <div class="w-2 rounded-t bg-violet-400" style="height: {Math.max(opexH, point.opex_total ? 2 : 0)}px"></div>
              </div>
              <span class="text-[9px] text-[var(--color-ds-text-muted,#6b7280)]">{point.month.slice(5)}/{point.month.slice(2, 4)}</span>
            </div>
          {/each}
        </div>
        <div class="mt-1 flex items-center gap-4 text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-blue-500"></span>{t('p3m.capex')}</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-violet-400"></span>{t('p3m.opex')}</span>
        </div>
      {/if}
    </section>
  </div>
{/if}

<style>
  .rotated {
    transform: rotate(90deg);
  }
</style>
