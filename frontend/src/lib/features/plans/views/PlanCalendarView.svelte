<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  let items = $state([]);
  let cursor = $state(new Date());
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await plansApi.getItems(planId, { limit: 1000 });
      items = res?.items ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function dayKey(d) {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  }

  // Map of YYYY-MM-DD -> items scheduled (start or due) that day.
  const byDay = $derived.by(() => {
    const map = new Map();
    for (const item of items) {
      for (const d of [item.start_date, item.due_date]) {
        if (!d) continue;
        const k = String(d).slice(0, 10);
        if (!map.has(k)) map.set(k, []);
        const list = map.get(k);
        if (!list.some((i) => i.id === item.id)) list.push(item);
      }
    }
    return map;
  });

  // Cells for the visible month: leading blanks + days (+ trailing to fill last week).
  const cells = $derived.by(() => {
    const year = cursor.getFullYear();
    const month = cursor.getMonth();
    const firstWeekday = new Date(year, month, 1).getDay(); // Sunday=0
    const daysInMonth = new Date(year, month + 1, 0).getDate();
    const out = [];
    for (let i = 0; i < firstWeekday; i++) out.push(null);
    for (let d = 1; d <= daysInMonth; d++) {
      const date = new Date(year, month, d);
      out.push({ day: d, key: dayKey(date), items: byDay.get(dayKey(date)) ?? [] });
    }
    while (out.length % 7 !== 0) out.push(null);
    return out;
  });

  const unscheduled = $derived(items.filter((i) => !i.start_date && !i.due_date));

  const monthLabel = $derived(cursor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' }));
  const todayKey = dayKey(new Date());

  function shiftMonth(delta) {
    cursor = new Date(cursor.getFullYear(), cursor.getMonth() + delta, 1);
  }
</script>

<div class="space-y-4">
  <!-- Header -->
  <div class="flex items-center gap-2">
    <button type="button" title={t('plans.calendarPrevMonth')} onclick={() => shiftMonth(-1)} class="rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] p-1.5 text-sm hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
      ‹
    </button>
    <h2 class="min-w-40 text-center text-base font-semibold">{monthLabel}</h2>
    <button type="button" title={t('plans.calendarNextMonth')} onclick={() => shiftMonth(1)} class="rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] p-1.5 text-sm hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
      ›
    </button>
    <button type="button" onclick={() => (cursor = new Date())} class="ml-2 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-3 py-1.5 text-sm hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
      {t('plans.calendarToday')}
    </button>
  </div>

  {#if loading && items.length === 0}
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
  {:else if error && items.length === 0}
    <p class="text-sm text-red-600">{error}</p>
  {:else}
    <!-- Month grid -->
    <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      <div class="grid grid-cols-7 border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface-hover,#f3f4f6)] text-center text-xs font-medium uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
        {#each ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'] as wd (wd)}
          <div class="py-1.5">{wd}</div>
        {/each}
      </div>
      <div class="grid grid-cols-7">
        {#each cells as cell, i (i)}
          {#if cell === null}
            <div class="min-h-20 border-b border-r border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface-hover,#f3f4f6)]/40"></div>
          {:else}
            <div class="min-h-20 space-y-1 overflow-hidden border-b border-r border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-1 last:border-r-0">
              <span class={`inline-block rounded px-1 text-xs tabular-nums ${cell.key === todayKey ? 'bg-[var(--color-ds-accent-blue,#3b82f6)] font-semibold text-white' : 'text-[var(--color-ds-text-muted,#6b7280)]'}`}>
                {cell.day}
              </span>
              {#each cell.items.slice(0, 4) as item (item.id)}
                <div class="truncate rounded bg-[var(--color-ds-surface-hover,#f3f4f6)] px-1 py-0.5 text-[11px] leading-tight" title={`${item.key} ${item.title}`}>
                  <span class="text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</span> {item.title}
                </div>
              {/each}
              {#if cell.items.length > 4}
                <span class="block px-1 text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">+{cell.items.length - 4}</span>
              {/if}
            </div>
          {/if}
        {/each}
      </div>
    </div>

    <!-- Unscheduled -->
    {#if unscheduled.length > 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-3">
        <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.calendarUnscheduled')}</h3>
        <div class="flex flex-wrap gap-1.5">
          {#each unscheduled as item (item.id)}
            <span class="rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs" title={item.title}>
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</span> {item.title}
            </span>
          {/each}
        </div>
      </div>
    {/if}
  {/if}
</div>
