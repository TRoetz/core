<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  const LABEL_W = 260;
  const ROW_H = 36;
  const HEADER_H = 28;
  const DAY_MS = 24 * 60 * 60 * 1000;

  let items = $state([]);
  let deps = $state([]);
  let loading = $state(true);
  let error = $state('');

  function dayStart(s) {
    const d = new Date(s);
    return new Date(d.getFullYear(), d.getMonth(), d.getDate());
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [itemsRes, depsRes] = await Promise.all([
        plansApi.getItems(planId, { limit: 1000 }),
        plansApi.listDependencies(planId),
      ]);
      items = itemsRes?.items ?? [];
      deps = Array.isArray(depsRes) ? depsRes : (depsRes?.dependencies ?? []);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  // Rows: items with both dates, sorted by start.
  const scheduled = $derived(
    items
      .filter((i) => i.start_date && i.end_date)
      .sort((a, b) => String(a.start_date).localeCompare(String(b.start_date)))
  );

  const geometry = $derived.by(() => {
    if (scheduled.length === 0) return null;
    let min = Infinity;
    let max = -Infinity;
    for (const i of scheduled) {
      const s = dayStart(i.start_date).getTime();
      const e = dayStart(i.end_date).getTime() + DAY_MS; // end date inclusive
      if (s < min) min = s;
      if (e > max) max = e;
    }
    const spanDays = Math.max(1, Math.ceil((max - min) / DAY_MS));
    const dayW = spanDays <= 180 ? 34 : Math.max(4, Math.floor(900 / spanDays));
    const totalW = spanDays * dayW;
    const x = (tms) => ((tms - min) / DAY_MS) * dayW;

    // Month boundaries + labels.
    const months = [];
    const cursor = new Date(min);
    cursor.setDate(1);
    if (cursor.getTime() < min) cursor.setMonth(cursor.getMonth() + 1);
    while (cursor.getTime() <= max - DAY_MS) {
      months.push({ x: x(cursor.getTime()), label: cursor.toLocaleDateString(undefined, { month: 'short', year: '2-digit' }) });
      cursor.setMonth(cursor.getMonth() + 1);
    }

    // Today marker.
    const today = new Date();
    const todayTms = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
    const showToday = todayTms >= min && todayTms <= max - DAY_MS;

    // Row geometry keyed by item id.
    const rowOf = new Map(scheduled.map((i, idx) => [i.id, idx]));
    return { min, max, spanDays, dayW, totalW, x, months, showToday, todayX: x(todayTms), rowOf };
  });

  // Dependency arrows between visible rows.
  const arrows = $derived.by(() => {
    if (!geometry) return [];
    const out = [];
    for (const d of deps) {
      const si = scheduled.find((i) => i.id === d.source_item_id);
      const ti = scheduled.find((i) => i.id === d.target_item_id);
      if (!si || !ti) continue;
      const sx = geometry.x(dayStart(si.end_date).getTime() + DAY_MS); // source bar right edge
      const sy = HEADER_H + geometry.rowOf.get(si.id) * ROW_H + ROW_H / 2;
      const tx = geometry.x(dayStart(ti.start_date).getTime()); // target bar left edge
      const ty = HEADER_H + geometry.rowOf.get(ti.id) * ROW_H + ROW_H / 2;
      let path;
      if (tx > sx) {
        const mid = (sx + tx) / 2;
        path = `M ${sx} ${sy} H ${mid} V ${ty} H ${tx}`;
      } else {
        const rx = Math.max(sx, tx) + 16;
        path = `M ${sx} ${sy} H ${rx} V ${ty} H ${tx}`;
      }
      out.push({ id: d.id, path, blocks: d.dep_type === 'blocks' });
    }
    return out;
  });

  const contentH = $derived(scheduled.length * ROW_H);
</script>

{#if loading}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
{:else if error && items.length === 0}
  <p class="text-sm text-red-600">{error}</p>
{:else if scheduled.length === 0}
  <div class="space-y-1">
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.timelineEmpty')}</p>
    {#if items.length > 0}<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.timelineNoDates')}</p>{/if}
  </div>
{:else if geometry}
  <div class="space-y-3">
    {#if items.length > scheduled.length}
      <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.timelineNoDates')}</p>
    {/if}

    <div class="overflow-x-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      <div style={`width:${LABEL_W + geometry.totalW}px`}>
        <!-- Month header -->
        <div class="flex border-b border-[var(--color-ds-border,#e5e7eb)]" style={`height:${HEADER_H}px`}>
          <div class="shrink-0 border-r border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 text-xs font-medium leading-[28px] text-[var(--color-ds-text-muted,#6b7280)]" style={`width:${LABEL_W}px`}>
            {t('plans.colKey')} / {t('plans.colTitle')}
          </div>
          <div class="relative flex-1">
            {#each geometry.months as m (m.x)}
              <span class="absolute top-1.5 text-[10px] tabular-nums text-[var(--color-ds-text-muted,#6b7280)]" style={`left:${m.x + 4}px`}>
                {m.label}
              </span>
            {/each}
          </div>
        </div>

        <!-- Rows -->
        <div class="relative">
          {#each scheduled as item, i (item.id)}
            {@const s = dayStart(item.start_date).getTime()}
            {@const e = dayStart(item.end_date).getTime() + DAY_MS}
            {@const left = geometry.x(s)}
            {@const width = Math.max(6, ((e - s) / DAY_MS) * geometry.dayW)}
            <div class="flex border-b border-[var(--color-ds-border,#e5e7eb)] last:border-b-0" style={`height:${ROW_H}px`}>
              <div class="shrink-0 truncate border-r border-[var(--color-ds-border,#e5e7eb)] px-2 text-xs leading-[36px]" style={`width:${LABEL_W}px`} title={item.title}>
                <span class="text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</span>
                <span class="ml-1.5 text-[var(--color-ds-text,#111827)]">{item.title}</span>
              </div>
              <div class="relative flex-1">
                <div
                  class="absolute rounded"
                  style={`left:${left}px; width:${width}px; top:9px; height:14px; background:var(--color-ds-accent-blue,#3b82f6); opacity:{item.is_completed ? 0.45 : 0.85}`}
                ></div>
              </div>
            </div>
          {/each}

          <!-- Gridlines, today marker, dependency arrows -->
          <svg class="pointer-events-none absolute top-0" style={`left:${LABEL_W}px`} width={geometry.totalW} height={contentH}>
            <defs>
              <marker id="tl-arrow-blocks" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" fill="#ef4444" />
              </marker>
              <marker id="tl-arrow-related" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" fill="#6b7280" />
              </marker>
            </defs>
            {#each geometry.months as m (m.x)}
              <line x1={m.x} y1="0" x2={m.x} y2={contentH} stroke="var(--color-ds-border,#e5e7eb)" stroke-width="1" />
            {/each}
            {#if geometry.showToday}
              <line x1={geometry.todayX} y1="0" x2={geometry.todayX} y2={contentH} stroke="#ef4444" stroke-width="1" stroke-dasharray="4 3" />
              <text x={geometry.todayX + 4} y={contentH - 6} font-size="10" fill="#ef4444">{t('plans.timelineToday')}</text>
            {/if}
            {#each arrows as a (a.id)}
              <path d={a.path} fill="none" stroke={a.blocks ? '#ef4444' : 'var(--color-ds-text-muted,#6b7280)'} stroke-width="1.5" stroke-dasharray={a.blocks ? '' : '5 4'} marker-end={`url(#tl-arrow-${a.blocks ? 'blocks' : 'related'})`} />
            {/each}
          </svg>
        </div>
      </div>
    </div>

    <!-- Legend -->
    <div class="flex items-center gap-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      <span class="inline-flex items-center gap-1.5">
        <svg width="26" height="8"><line x1="0" y1="4" x2="26" y2="4" stroke="#ef4444" stroke-width="1.5" /></svg>
        {t('plans.timelineLegendBlocks')}
      </span>
      <span class="inline-flex items-center gap-1.5">
        <svg width="26" height="8"><line x1="0" y1="4" x2="26" y2="4" stroke="#6b7280" stroke-width="1.5" stroke-dasharray="5 4" /></svg>
        {t('plans.timelineLegendRelated')}
      </span>
    </div>
  </div>
{/if}
