<script>
  import { onMount } from 'svelte';
  import Activity from '@lucide/svelte/icons/activity';
  import AlertTriangle from '@lucide/svelte/icons/alert-triangle';
  import CalendarClock from '@lucide/svelte/icons/calendar-clock';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { navigate } from '../../router.js';
  import { formatMoney, ragColor } from '../../features/p3m/utils.js';

  const MAX_PORTFOLIOS = 6;

  let summary = $state(null);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const s = await p3mApi.getExecutiveSummary();
      summary = s && Array.isArray(s.portfolios) ? s : null;
    } catch (e) {
      // P3M may be disabled or the user lacks p3m_view: show a quiet error.
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const totals = $derived.by(() => {
    if (!summary) return null;
    let approved = 0;
    let actual = 0;
    for (const pf of summary.portfolios) {
      approved += pf.budget_approved || 0;
      actual += pf.budget_actual || 0;
    }
    return { approved, actual };
  });

  function openP3M() {
    navigate('/p3m');
  }
</script>

{#if loading}
  <div class="flex h-24 items-center justify-center text-xs" style:color="var(--ds-text-subtle)">
    {t('p3m.loading')}
  </div>
{:else if error}
  <div class="flex h-24 flex-col items-center justify-center gap-1 px-4 text-center text-xs" style:color="var(--ds-text-subtle)">
    <span>{error}</span>
    <button type="button" class="text-[var(--ds-accent,#2563eb)] hover:underline" onclick={openP3M}>
      {t('p3m.pulseOpenP3m')}
    </button>
  </div>
{:else if !summary || summary.portfolios.length === 0}
  <div class="flex h-24 flex-col items-center justify-center gap-2 text-xs" style:color="var(--ds-text-subtle)">
    <Activity size={18} />
    <span>{t('p3m.pulseNoPortfolios')}</span>
    <button type="button" class="text-[var(--ds-accent,#2563eb)] hover:underline" onclick={openP3M}>
      {t('p3m.pulseOpenP3m')}
    </button>
  </div>
{:else}
  <ul class="divide-y divide-[var(--color-ds-border,#e5e7eb)]">
    {#each summary.portfolios.slice(0, MAX_PORTFOLIOS) as pf (pf.portfolio_id)}
      {@const gates = pf.upcoming_gates?.length ?? 0}
      {@const risks = pf.top_risks?.length ?? 0}
      <li class="flex items-center gap-2 py-1.5 text-xs">
        <span class="h-2 w-2 shrink-0 rounded-full" style:background={ragColor(pf.rag_status)}></span>
        <button type="button" class="truncate font-medium hover:underline" onclick={openP3M}>
          {pf.title}
        </button>
        <span class="ml-auto flex shrink-0 items-center gap-2 tabular-nums" style:color="var(--ds-text-subtle)">
          {#if gates > 0}
            <span class="flex items-center gap-1" title={t('p3m.upcomingGates')}>
              <CalendarClock size={12} /> {gates}
            </span>
          {/if}
          {#if risks > 0}
            <span class="flex items-center gap-1 text-[var(--ds-warning,#f59e0b)]" title={t('p3m.raidItems')}>
              <AlertTriangle size={12} /> {risks}
            </span>
          {/if}
          <span>{formatMoney(pf.budget_actual)} / {formatMoney(pf.budget_approved)}</span>
        </span>
      </li>
    {/each}
  </ul>
  {#if totals}
    <div class="mt-2 flex items-center justify-between border-t border-[var(--color-ds-border,#e5e7eb)] pt-2 text-xs font-medium">
      <span>{t('p3m.totalPortfolios')} ({summary.portfolios.length})</span>
      <span class="tabular-nums">{formatMoney(totals.actual)} / {formatMoney(totals.approved)}</span>
    </div>
  {/if}
{/if}
