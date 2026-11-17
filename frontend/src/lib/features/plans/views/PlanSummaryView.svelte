<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  let summary = $state(null);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      summary = await plansApi.getSummary(planId);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const maxStatusCount = $derived(Math.max(1, ...(summary?.by_status ?? []).map((s) => s.count)));
  const maxTypeCount = $derived(Math.max(1, ...(summary?.by_type ?? []).map((s) => s.count)));
</script>

{#if loading}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
{:else if error && !summary}
  <div class="space-y-3">
    <p class="text-sm text-red-600">{error}</p>
    <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90" onclick={load}>
      {t('plans.retry')}
    </button>
  </div>
{:else if summary && summary.total_items === 0}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryEmpty')}</p>
{:else if summary}
  <div class="space-y-6">
    <!-- Progress header -->
    <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
      <div class="mb-2 flex items-baseline justify-between text-sm">
        <span class="font-medium">{t('plans.summaryProgress')}</span>
        <span class="tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.summaryItems', { count: summary.total_items })} · {t('plans.summaryDone', { count: summary.done_items })}
        </span>
      </div>
      <div class="h-3 overflow-hidden rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)]">
        <div class="h-full rounded-full bg-[var(--color-ds-accent-blue,#3b82f6)]" style={`width:${Math.min(100, summary.progress_pct)}%`}></div>
      </div>
      <p class="mt-1.5 text-right text-xs tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{summary.progress_pct.toFixed(0)}%</p>
    </div>

    <!-- Breakdowns -->
    <div class="grid gap-4 md:grid-cols-3">
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryByStatus')}</h3>
        {#each summary.by_status as s (s.name)}
          <div class="mb-2 last:mb-0">
            <div class="flex items-center justify-between text-sm">
              <span>{s.name}</span>
              <span class="tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{s.count}</span>
            </div>
            <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)]">
              <div class="h-full rounded-full" style={`width:${(s.count / maxStatusCount) * 100}%; background:${s.color || 'var(--color-ds-accent-blue,#3b82f6)'}`}></div>
            </div>
          </div>
        {/each}
      </div>

      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryByType')}</h3>
        {#each summary.by_type as s (s.name)}
          <div class="mb-2 last:mb-0">
            <div class="flex items-center justify-between text-sm">
              <span>{s.name}</span>
              <span class="tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{s.count}</span>
            </div>
            <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)]">
              <div class="h-full rounded-full bg-[var(--color-ds-accent-blue,#3b82f6)]" style={`width:${(s.count / maxTypeCount) * 100}%; opacity:0.7`}></div>
            </div>
          </div>
        {/each}
      </div>

      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryByWorkspace')}</h3>
        {#each summary.by_workspace as w (w.workspace_id)}
          <div class="mb-2.5 last:mb-0">
            <div class="flex items-center justify-between text-sm">
              <span class="truncate pr-2">{w.workspace_name}</span>
              <span class="shrink-0 tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{w.done_count}/{w.item_count}</span>
            </div>
            <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)]">
              <div class="h-full rounded-full bg-[var(--color-ds-accent-blue,#3b82f6)]" style={`width:${Math.min(100, w.progress_pct)}%`}></div>
            </div>
          </div>
        {/each}
      </div>
    </div>

    <!-- Upcoming -->
    <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
      <h3 class="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryUpcoming')}</h3>
      {#if (summary.upcoming ?? []).length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.summaryNoUpcoming')}</p>
      {:else}
        <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each summary.upcoming as item (item.id)}
            <div class="flex items-center gap-3 border-b border-[var(--color-ds-border,#e5e7eb)] px-3 py-2 text-sm last:border-b-0">
              <span class="shrink-0 tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</span>
              <span class="flex-1 truncate">{item.title}</span>
              {#if item.due_date}<span class="shrink-0 tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{String(item.due_date).slice(0, 10)}</span>{/if}
              <span class="shrink-0 rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.status_name}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}
