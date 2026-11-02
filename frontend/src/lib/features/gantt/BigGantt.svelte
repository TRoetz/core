<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { items as itemsApi } from '../../api/items.js';
  import { plans as plansApi } from '../../api/plans.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { itemModalStore } from '../../stores/itemModalStore.svelte.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import GanttChartIcon from '@lucide/svelte/icons/gantt-chart';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import IconSearch from '@tabler/icons-svelte-runes/icons/search';

  const PAGE_SIZE = 1000;
  const MAX_ITEMS = 5000;
  const DAY_MS = 86400000;
  const EPOCH = Date.UTC(1970, 0, 1);
  const LABEL_W = 340;
  /** Distinct hues cycled across workspace groups in view order. */
  const WS_PALETTE = [
    '#f59e0b', // amber
    '#10b981', // emerald
    '#0ea5e9', // sky
    '#6366f1', // indigo
    '#ec4899', // pink
    '#8b5cf6', // violet
    '#14b8a6', // teal
    '#84cc16', // lime
    '#fb923c', // orange
    '#f43f5e', // rose
  ];

  let items = $state([]);
  let loading = $state(true);
  let error = $state('');
  let truncated = $state(false);
  let totalItems = $state(null);
  let search = $state('');
  let zoom = $state('auto'); // 'auto' | 'day' | 'week' | 'month'
  let typeFilter = $state('all'); // 'all' or an item_type_name
  let groupBy = $state('workspace'); // 'workspace' | 'plan' | 'epic' | 'project' | 'p3m-programme'

  /** workspace_id -> [{ plan_id, plan_name }] (from /plans/workspace-map). */
  let planMap = $state(new Map());
  /** workspace_id -> P3M programme title (via the workspace's P3M project). */
  let p3mProgrammeMap = $state(new Map());

  const collapsedGroups = $state({});

  const itemById = $derived(new Map(items.map((i) => [i.id, i])));

  /** First ancestor of the given item type name, walking up parent_id. */
  function findAncestorType(item, typeName) {
    let cur = item;
    const visited = new Set();
    while (cur && cur.parent_id != null && !visited.has(cur.id)) {
      visited.add(cur.id);
      const parent = itemById.get(cur.parent_id);
      if (!parent) break;
      if (parent.item_type_name === typeName) return parent;
      cur = parent;
    }
    return null;
  }

  /** The workspace root item (top of the hierarchy chain), or the item itself. */
  function findRoot(item) {
    let cur = item;
    const visited = new Set();
    while (cur && cur.parent_id != null && !visited.has(cur.id)) {
      visited.add(cur.id);
      const parent = itemById.get(cur.parent_id);
      if (!parent) break;
      cur = parent;
    }
    return cur;
  }

  /** Group key/label for an item under the active grouping dimension. */
  function itemGroups(item) {
    switch (groupBy) {
      case 'plan': {
        const plans = planMap.get(item.workspace_id) ?? [];
        if (plans.length === 0) {
          return [{ key: 'plan:none', name: t('bigGantt.noPlan') }];
        }
        return plans.map((p) => ({ key: `plan:${p.plan_id}`, name: p.plan_name }));
      }
      case 'epic': {
        const epic = findAncestorType(item, 'Epic');
        return epic
          ? [{ key: `epic:${epic.id}`, name: epic.title || epic.key }]
          : [{ key: 'epic:none', name: t('bigGantt.noEpic') }];
      }
      case 'project': {
        const root = findRoot(item);
        return [{ key: `project:${root.id}`, name: root.title || root.key }];
      }
      case 'p3m-programme': {
        const name = p3mProgrammeMap.get(item.workspace_id);
        return name
          ? [{ key: `p3m:${name}`, name }]
          : [{ key: 'p3m:none', name: t('bigGantt.noP3MProgramme') }];
      }
      default:
        return [
          { key: `ws:${item.workspace_id ?? item.workspace_name}`, name: item.workspace_name || t('bigGantt.unknownWorkspace') },
        ];
    }
  }

  /** Grouped view, preserving first-seen order; an item may appear in
   * several groups (e.g. its workspace belongs to multiple plans). */
  const groups = $derived.by(() => {
    const gMap = new Map();
    for (const item of visibleItems) {
      for (const g of itemGroups(item)) {
        let group = gMap.get(g.key);
        if (!group) {
          group = { key: g.key, name: g.name, items: [], count: 0 };
          gMap.set(g.key, group);
        }
        group.items.push(item);
        group.count += 1;
      }
    }
    return [...gMap.values()].map((g, i) => ({
      ...g,
      color: WS_PALETTE[i % WS_PALETTE.length],
    }));
  });
  let scrollEl = null;

  /** Parse the date portion of an ISO timestamp into a UTC day index. */
  function dayIndex(iso) {
    if (!iso || typeof iso !== 'string') return null;
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
    if (!m) return null;
    return Math.floor(Date.UTC(+m[1], +m[2] - 1, +m[3]) / DAY_MS);
  }

  const now = new Date();
  const todayIndex = Math.floor(
    Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / DAY_MS
  );

  /** [startDay, endDay] for an item, or null when it has no dates at all. */
  function itemSpan(item) {
    const start = dayIndex(item.start_date);
    const due = dayIndex(item.due_date);
    const end = dayIndex(item.end_date);
    let s = start ?? due;
    let e = end ?? due ?? start;
    if (s == null && e == null) return null;
    if (s == null) s = e;
    if (e == null) e = s;
    if (e < s) [s, e] = [e, s];
    return { start: s, end: e };
  }

  function fmtDay(idx) {
    const d = new Date(idx * DAY_MS);
    const p = (n) => String(n).padStart(2, '0');
    return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())}`;
  }

  // --- data loading -------------------------------------------------------

  let loadGeneration = 0;

  async function loadItems() {
    const gen = ++loadGeneration;
    loading = true;
    error = '';
    items = [];
    truncated = false;
    totalItems = null;
    lastScrolledEpoch = ''; // re-center on today once this load settles
    try {
      const seen = new Set();
      let page = 1;
      for (;;) {
        const res = await itemsApi.getAll({
          limit: PAGE_SIZE,
          page,
          omit_descriptions: true,
        });
        if (gen !== loadGeneration) return; // superseded by a newer load
        const batch = res?.data ?? [];
        for (const item of batch) {
          if (!seen.has(item.id)) {
            seen.add(item.id);
            items.push(item);
          }
        }
        totalItems = res?.pagination?.total_items ?? null;
        if (items.length >= MAX_ITEMS || batch.length < PAGE_SIZE) break;
        page += 1;
      }
      truncated = items.length > MAX_ITEMS;
      if (truncated) items = items.slice(0, MAX_ITEMS);
    } catch (err) {
      if (gen !== loadGeneration) return;
      error = err?.message || String(err);
    } finally {
      if (gen === loadGeneration) loading = false;
    }
  }

  // --- grouping data (plans + P3M programmes) --------------------------------

  async function loadGroupingData() {
    try {
      const mapRes = await plansApi.getWorkspaceMap();
      const pairs = mapRes?.data ?? (Array.isArray(mapRes) ? mapRes : []);
      const m = new Map();
      for (const p of pairs) {
        let arr = m.get(p.workspace_id);
        if (!arr) {
          arr = [];
          m.set(p.workspace_id, arr);
        }
        arr.push({ plan_id: p.plan_id, plan_name: p.plan_name });
      }
      planMap = m;
    } catch (e) {
      // Plans are optional; grouping by plan simply reports "no plan".
    }
    try {
      const [projectsRes, programmesRes] = await Promise.all([p3mApi.projects.getAll({}), p3mApi.programmes.getAll({})]);
      const projects = projectsRes?.data ?? (Array.isArray(projectsRes) ? projectsRes : []);
      const programmes = programmesRes?.data ?? (Array.isArray(programmesRes) ? programmesRes : []);
      const programmeTitles = new Map(programmes.map((p) => [p.id, p.title]));
      const m = new Map();
      for (const project of projects) {
        const title = programmeTitles.get(project.programme_id);
        if (!title) continue;
        try {
          const wsRes = await p3mApi.getProjectWorkspaces(project.id);
          const wss = wsRes?.data ?? (Array.isArray(wsRes) ? wsRes : []);
          for (const ws of wss) m.set(ws.id ?? ws.workspace_id, title);
        } catch (e) {
          // skip projects whose workspace links fail
        }
      }
      p3mProgrammeMap = m;
    } catch (e) {
      // P3M data is optional.
    }
  }

  onMount(() => {
    loadItems();
    loadGroupingData();
  });

  // --- filtering & grouping -------------------------------------------------

  const visibleItems = $derived.by(() => {
    let list = items;
    if (typeFilter !== 'all') {
      list = list.filter((it) => it.item_type_name === typeFilter);
    }
    const q = search.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (it) =>
        it.title?.toLowerCase().includes(q) || it.key?.toLowerCase().includes(q)
    );
  });

  /** Distinct item types present in the loaded set, with counts. */
  const typeOptions = $derived.by(() => {
    const m = new Map();
    for (const it of items) {
      if (!it.item_type_name) continue;
      const opt = m.get(it.item_type_name);
      if (opt) opt.count += 1;
      else m.set(it.item_type_name, { name: it.item_type_name, count: 1 });
    }
    return [...m.values()];
  });

  /**
   * Rollup spans for the active type filter: each item's own date range
   * unioned with every descendant's range, so undated parents still get a bar.
   */
  const rollupSpans = $derived.by(() => {
    if (typeFilter === 'all') return null;
    const byId = new Map();
    for (const it of items) byId.set(it.id, it);
    const children = new Map();
    for (const it of items) {
      if (it.parent_id == null) continue;
      let arr = children.get(it.parent_id);
      if (!arr) {
        arr = [];
        children.set(it.parent_id, arr);
      }
      arr.push(it.id);
    }
    const memo = new Map();
    function spanOf(id, visiting) {
      const cached = memo.get(id);
      if (cached !== undefined) return cached;
      if (visiting.has(id)) return null; // cycle guard
      visiting.add(id);
      let s = Infinity;
      let e = -Infinity;
      const own = itemSpan(byId.get(id));
      if (own) {
        s = Math.min(s, own.start);
        e = Math.max(e, own.end);
      }
      for (const cid of children.get(id) ?? []) {
        const cs = spanOf(cid, visiting);
        if (cs) {
          s = Math.min(s, cs.start);
          e = Math.max(e, cs.end);
        }
      }
      visiting.delete(id);
      const span = Number.isFinite(s) ? { start: s, end: e } : null;
      memo.set(id, span);
      return span;
    }
    const out = new Map();
    for (const it of items) out.set(it.id, spanOf(it.id, new Set()));
    return out;
  });

  /** Span used for rendering: rollup union while a type filter is active. */
  function activeSpan(item) {
    if (typeFilter === 'all') return itemSpan(item);
    return rollupSpans.get(item.id) ?? null;
  }

  // --- timeline geometry ----------------------------------------------------

  const timeline = $derived.by(() => {
    let min = Infinity;
    let max = -Infinity;
    for (const item of visibleItems) {
      const span = activeSpan(item);
      if (!span) continue;
      if (span.start < min) min = span.start;
      if (span.end > max) max = span.end;
    }
    if (!Number.isFinite(min)) return null;
    // Always keep today in view with padding so the red line is reachable.
    min = Math.min(min, todayIndex - 7);
    max = Math.max(max, todayIndex + 14);
    const days = [];
    for (let d = min; d <= max; d++) days.push(d);
    return { min, max, dayCount: days.length };
  });

  const dayWidth = $derived.by(() => {
    if (!timeline) return 16;
    switch (zoom) {
      case 'day':
        return 32;
      case 'week':
        return 14;
      case 'month':
        return 7;
      default:
        return Math.max(5, Math.min(32, Math.floor(1200 / timeline.dayCount)));
    }
  });

  const totalWidth = $derived(timeline ? timeline.dayCount * dayWidth : 0);

  /** Month header segments with their pixel widths. */
  const monthSegments = $derived.by(() => {
    if (!timeline) return [];
    const segs = [];
    let cur = null;
    for (let d = timeline.min; d <= timeline.max; d++) {
      const dt = new Date(d * DAY_MS);
      const key = dt.getUTCFullYear() * 12 + dt.getUTCMonth();
      if (!cur || cur.key !== key) {
        if (cur) segs.push(cur);
        const label = new Intl.DateTimeFormat(undefined, {
          month: 'short',
          year: 'numeric',
          timeZone: 'UTC',
        }).format(dt);
        cur = { key, label, days: 0 };
      }
      cur.days += 1;
    }
    if (cur) segs.push(cur);
    return segs.map((s) => ({ ...s, width: s.days * dayWidth }));
  });

  /** Day cells for the header row. */
  const dayCells = $derived.by(() => {
    if (!timeline) return [];
    const cells = [];
    for (let d = timeline.min; d <= timeline.max; d++) {
      const dt = new Date(d * DAY_MS);
      const dow = dt.getUTCDay(); // 0=Sun .. 6=Sat
      const showLabel = dayWidth >= 20 || dow === 1;
      cells.push({
        idx: d,
        label: showLabel ? String(dt.getUTCDate()) : '',
        weekend: dow === 0 || dow === 6,
      });
    }
    return cells;
  });

  const rowCount = $derived.by(() => {
    let n = 0;
    for (const ws of groups) {
      n += 1; // workspace row
      if (!collapsedGroups[ws.key]) n += ws.items.length;
    }
    return n;
  });

  const contentHeight = $derived(64 + rowCount * 36); // header + rows

  // --- actions ---------------------------------------------------------------

  function toggleGroup(key) {
    collapsedGroups[key] = !collapsedGroups[key];
  }


  function openItem(item) {
    itemModalStore.open(item.id, item.workspace_id ?? null);
  }

  function scrollToToday(smooth = true) {
    if (!scrollEl || !timeline) return;
    const x = (todayIndex - timeline.min) * dayWidth - scrollEl.clientWidth / 2;
    scrollEl.scrollTo({ left: Math.max(0, x), behavior: smooth ? 'smooth' : 'auto' });
  }

  // The date range can stretch far into the past or future; opening at the
  // left edge hides all current work. Re-center today whenever a new range is
  // established (initial load or type-filter switch); manual scrolling and
  // search in between are preserved because this only fires on epoch change.
  const scrollEpoch = $derived(loading ? 'loading' : `${typeFilter}:${items.length}`);
  let lastScrolledEpoch = '';
  $effect(() => {
    const epoch = scrollEpoch;
    if (epoch === 'loading' || !timeline) return;
    if (epoch === lastScrolledEpoch) return;
    lastScrolledEpoch = epoch;
    let raf = requestAnimationFrame(() => scrollToToday(false));
    return () => cancelAnimationFrame(raf);
  });

  const workspaceCount = $derived(groups.length);
</script>

<PageHeader
  icon={GanttChartIcon}
  title={t('bigGantt.title')}
  marginClass="mt-4 mb-8"
  subtitle={
    loading
      ? t('bigGantt.loading')
      : totalItems != null
        ? t('bigGantt.subtitleAll', {
            count: visibleItems.length,
            workspaces: workspaceCount,
          })
        : ''
  }
>
  {#snippet actions()}
    <div class="flex items-center gap-2">
      <select
        value={groupBy}
        onchange={(e) => (groupBy = e.currentTarget.value)}
        class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        aria-label={t('bigGantt.groupByLabel')}
      >
        <option value="workspace">{t('bigGantt.groupWorkspace')}</option>
        <option value="plan">{t('bigGantt.groupPlan')}</option>
        <option value="epic">{t('bigGantt.groupEpic')}</option>
        <option value="project">{t('bigGantt.groupProject')}</option>
        <option value="p3m-programme">{t('bigGantt.groupP3MProgramme')}</option>
      </select>
      <select
        value={typeFilter}
        onchange={(e) => (typeFilter = e.currentTarget.value)}
        class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        aria-label={t('bigGantt.typeFilterLabel')}
      >
        <option value="all">{t('bigGantt.allTypes')}</option>
        {#each typeOptions as opt (opt.name)}
          <option value={opt.name}>{opt.name} ({opt.count})</option>
        {/each}
      </select>
      <label class="relative block">
        <IconSearch
          size={14}
          class="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-ds-text-muted,#6b7280)]"
        />
        <input
          type="search"
          bind:value={search}
          placeholder={t('bigGantt.searchPlaceholder')}
          class="h-8 w-56 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] pl-7 pr-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        />
      </label>
      <div class="flex overflow-hidden rounded-md border border-[var(--color-ds-border,#d1d5db)]">
        {#each [['auto', t('bigGantt.zoomAuto')], ['day', t('bigGantt.zoomDay')], ['week', t('bigGantt.zoomWeek')], ['month', t('bigGantt.zoomMonth')]] as [value, label] (value)}
          <button
            type="button"
            class="h-8 px-2.5 text-xs font-medium transition-colors {zoom === value ? 'bg-[var(--color-ds-accent-blue,#3b82f6)] text-white' : 'bg-transparent text-[var(--color-ds-text-secondary,#4b5563)] hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]'}"
            onclick={() => (zoom = value)}
          >
            {label}
          </button>
        {/each}
      </div>
      <button
        type="button"
        class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] px-2.5 text-xs font-medium text-[var(--color-ds-text-secondary,#4b5563)] hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]"
        onclick={scrollToToday}
      >
        {t('bigGantt.today')}
      </button>
    </div>
  {/snippet}
</PageHeader>

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('bigGantt.loading')}
  </div>
{:else if error}
  <EmptyState
    title={t('bigGantt.loadErrorTitle')}
    description={error}
    class="h-64"
  >
    {#snippet action()}
      <button
        type="button"
        class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
        onclick={loadItems}
      >
        {t('bigGantt.retry')}
      </button>
    {/snippet}
  </EmptyState>
{:else if items.length === 0}
  <EmptyState
    icon={GanttChartIcon}
    title={t('bigGantt.emptyTitle')}
    description={t('bigGantt.emptyDescription')}
    class="h-64"
  />
{:else if visibleItems.length === 0}
  <EmptyState
    title={t('bigGantt.noResults')}
    class="h-64"
  />
{:else if !timeline}
  <EmptyState
    icon={GanttChartIcon}
    title={t('bigGantt.noDatesTitle')}
    description={t('bigGantt.noDatesDescription')}
    class="h-64"
  />
{:else}
  {#if truncated}
    <div class="px-6 pt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      {t('bigGantt.truncatedNote', { shown: MAX_ITEMS, total: totalItems ?? items.length })}
    </div>
  {/if}

  <div class="gantt-shell" style="--day-w: {dayWidth}px; --label-w: {LABEL_W}px">
    <div bind:this={scrollEl} class="gantt-scroll">
      <div class="gantt-content" style="width: {LABEL_W + totalWidth}px">
        <!-- Sticky header: month row + day row -->
        <div class="gantt-head">
          <div class="flex h-7">
            <div class="gantt-corner sticky left-0 z-40 flex items-center border-b border-r border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] text-xs font-semibold text-[var(--color-ds-text-secondary,#4b5563)]">
              {t('bigGantt.timelineLabel')}
            </div>
            <div class="flex" style="width: {totalWidth}px">
              {#each monthSegments as seg (seg.key)}
                <div
                  class="flex h-7 items-center border-b border-l border-[var(--color-ds-border,#d1d5db)] px-2 text-xs font-semibold text-[var(--color-ds-text-secondary,#4b5563)]"
                  style="width: {seg.width}px; min-width: 0"
                >
                  <span class="truncate">{seg.label}</span>
                </div>
              {/each}
            </div>
          </div>
          <div class="flex h-6">
            <div class="gantt-corner sticky left-0 z-40 border-b border-r border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)]"></div>
            <div class="relative" style="width: {totalWidth}px">
              {#each dayCells as cell (cell.idx)}
                <div
                  class="absolute top-0 flex h-6 items-center justify-center border-b border-[var(--color-ds-border,#d1d5db)] text-[10px] {cell.weekend ? 'text-[var(--color-ds-text-muted,#9ca3af)]' : 'text-[var(--color-ds-text-secondary,#4b5563)]'}"
                  style="left: {(cell.idx - timeline.min) * dayWidth}px; width: {dayWidth}px"
                >
                  {cell.label}
                </div>
              {/each}
            </div>
          </div>
        </div>

        <!-- Today line overlay (spans full body height, under sticky labels) -->
        <div
          class="gantt-today"
          style="left: {LABEL_W + (todayIndex - timeline.min) * dayWidth}px; height: {contentHeight - 40}px"
        ></div>

        <!-- Rows -->
        {#each groups as ws (ws.key)}
          <div class="gantt-row gantt-row-ws">
            <button
              type="button"
              class="row-label sticky left-0 z-10 flex h-9 items-center gap-1.5 border-b border-r border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-3 text-left hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]"
              style="width: {LABEL_W}px"
              onclick={() => toggleGroup(ws.key)}
            >
              <IconChevronRight
                size={14}
                class="shrink-0 text-[var(--color-ds-text-muted,#9ca3af)] transition-transform {collapsedGroups[ws.key] ? '' : 'rotate-90'}"
              />
              <span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background: {ws.color}"></span>
              <span class="truncate text-sm font-semibold">{ws.name}</span>
              <span class="ml-auto shrink-0 rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">
                {ws.count}
              </span>
            </button>
            <div class="gantt-tl h-9 border-b border-[var(--color-ds-border,#d1d5db)]" style="width: {totalWidth}px"></div>
          </div>

          {#if !collapsedGroups[ws.key]}
            {#each ws.items as item (item.id)}
              {@const span = activeSpan(item)}
              <div class="gantt-row gantt-row-item">
                <button
                  type="button"
                  class="row-label sticky left-0 z-10 flex h-9 items-center gap-2 border-b border-r border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] pl-7 pr-3 text-left hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]"
                  style="width: {LABEL_W}px; border-left: 3px solid {ws.color}"
                  onclick={() => openItem(item)}
                >
                  <span class="shrink-0 font-mono text-xs text-[var(--color-ds-text-muted,#9ca3af)]">{item.key}</span>
                  <span class="truncate text-sm">{item.title || '—'}</span>
                </button>
                <div class="gantt-tl relative h-9 border-b border-[var(--color-ds-border,#d1d5db)]" style="width: {totalWidth}px">
                  {#if span}
                    {#if span.start === span.end}
                      <div
                        class="bar-diamond absolute top-1/2 z-[5] h-3 w-3 -translate-x-1/2 -translate-y-1/2 rotate-45 rounded-[2px]"
                        style="left: {(span.start - timeline.min) * dayWidth + dayWidth / 2}px; background: {ws.color}"
                        title={t('bigGantt.milestoneTooltip', { key: item.key, title: item.title || '—', date: fmtDay(span.start) })}
                      ></div>
                    {:else}
                      <div
                        class="bar absolute top-1/2 z-[5] h-[18px] -translate-y-1/2 rounded-md"
                        style="left: {(span.start - timeline.min) * dayWidth + 1}px; width: {Math.max((span.end - span.start + 1) * dayWidth - 2, 6)}px; background: {ws.color}"
                        title={t('bigGantt.barTooltip', { key: item.key, title: item.title || '—', start: fmtDay(span.start), end: fmtDay(span.end) })}
                      ></div>
                    {/if}
                  {/if}
                </div>
              </div>
            {/each}
          {/if}
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .gantt-shell {
    height: calc(100vh - 220px);
    min-height: 320px;
    margin: 0 1.5rem 1.5rem;
    border: 1px solid var(--color-ds-border, #d1d5db);
    border-radius: 8px;
    overflow: hidden;
    background: var(--color-ds-surface, #fff);
  }

  .gantt-scroll {
    height: 100%;
    overflow: auto;
  }

  .gantt-content {
    position: relative;
  }

  .gantt-head {
    position: sticky;
    top: 0;
    z-index: 30;
    background: var(--color-ds-surface, #fff);
  }

  /* Day grid lines drawn cheaply with a repeating gradient. */
  .gantt-tl {
    background-image: repeating-linear-gradient(
      to right,
      var(--color-ds-border, #d1d5db) 0,
      var(--color-ds-border, #d1d5db) 1px,
      transparent 1px,
      transparent var(--day-w)
    );
  }

  .gantt-today {
    position: absolute;
    top: 40px; /* below the sticky header */
    width: 2px;
    z-index: 6;
    background: #ef4444;
    pointer-events: none;
  }

  .gantt-row:hover .row-label {
    background: var(--color-ds-surface-hover, #f3f4f6);
  }

  .bar,
  .bar-diamond {
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
    transition: filter 120ms ease;
  }

  .gantt-row:hover .bar,
  .gantt-row:hover .bar-diamond {
    filter: brightness(1.12);
  }
</style>
