<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { formatDate } from './utils.js';

  /** 'programme' | 'portfolio'. */
  let { entityType = 'programme', id = null } = $props();

  /** @type {any} */
  let data = $state(null);
  let loading = $state(false);
  let error = $state('');

  async function load() {
    if (id === null || id === undefined) return;
    loading = true;
    error = '';
    try {
      data = entityType === 'portfolio' ? await p3mApi.getPortfolioRaidRollup(id) : await p3mApi.getProgrammeRaidRollup(id);
    } catch (e) {
      data = null;
      error = e?.message || String(e);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    // Reload whenever the target entity changes.
    const type = entityType;
    const targetId = id;
    if (targetId !== null && targetId !== undefined) load();
  });

  const byTypeEntries = $derived(Object.entries(data?.by_item_type ?? {}));
  const overdueItems = $derived(data?.overdue ?? []);
  const topRisks = $derived(data?.top_risks ?? []);
</script>

{#if loading && !data}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error}
  <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
{:else if data && (data.total_open ?? 0) === 0}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.noOpenRaid')}
  </div>
{:else if data}
  <div class="flex flex-wrap items-center gap-x-6 gap-y-1 text-xs">
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.totalOpen')}:</span> {data.total_open}
    </div>
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.escalatedCount')}:</span>
      <span class={(data.escalated_count ?? 0) > 0 ? 'font-semibold text-red-700' : ''}>{data.escalated_count}</span>
    </div>
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.overdue')}:</span>
      <span class={overdueItems.length > 0 ? 'font-semibold text-red-700' : ''}>{overdueItems.length}</span>
    </div>
    {#each byTypeEntries as [type, count] (type)}
      <span class="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-600">{type} · {count}</span>
    {/each}
  </div>

  {#if overdueItems.length > 0}
    <div class="mt-2 space-y-1">
      {#each overdueItems as item (item.id)}
        <div class="flex flex-wrap items-center gap-x-3 rounded-md border border-red-200 bg-red-50/60 px-2 py-1 text-xs">
          <span class="font-medium">{item.title}</span>
          <span class="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-600">{item.item_type}</span>
          <span class="text-red-700">{t('p3m.overdue')}: {formatDate(item.due_date)}</span>
          <span class="ml-auto tabular-nums">score {item.risk_score}</span>
        </div>
      {/each}
    </div>
  {/if}

  {#if topRisks.length > 0}
    <table class="mt-2 w-full text-left text-xs">
      <thead>
        <tr class="text-[var(--color-ds-text-muted,#6b7280)]">
          <th class="py-1 pr-3 font-medium">{t('p3m.topRisks')}</th>
          <th class="py-1 pr-3 font-medium">{t('p3m.status')}</th>
          <th class="py-1 pr-3 text-right font-medium">score</th>
          <th class="py-1 text-right font-medium">{t('p3m.dueDate')}</th>
        </tr>
      </thead>
      <tbody>
        {#each topRisks as risk (risk.id)}
          <tr class={risk.status === 'escalated' ? 'bg-red-50/70' : ''}>
            <td class="py-1 pr-3">
              {risk.title}
              {#if risk.item_subtype}
                <span class="ml-1.5 rounded bg-amber-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-amber-800">{risk.item_subtype}</span>
              {/if}
            </td>
            <td class="py-1 pr-3">
              <span class={risk.status === 'escalated' ? 'rounded bg-red-100 px-1.5 py-0.5 text-[10px] font-medium text-red-700' : 'rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-600'}>
                {risk.status}
              </span>
            </td>
            <td class="py-1 pr-3 text-right tabular-nums">{risk.risk_score}</td>
            <td class="py-1 text-right whitespace-nowrap">{risk.due_date ? formatDate(risk.due_date) : '—'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
{/if}
