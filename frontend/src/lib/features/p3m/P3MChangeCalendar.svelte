<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';

  let { portfolios = [] } = $props();

  // --- Entity option list (flat) + name resolution map, built from the tree ---
  const entityOptions = $derived(
    (portfolios || []).flatMap((p) => {
      const opts = [{ value: `portfolio:${p.id}`, label: p.title, name: p.title }];
      for (const prog of p.programmes ?? []) {
        opts.push({ value: `programme:${prog.id}`, label: `— ${prog.title}`, name: prog.title });
        for (const proj of prog.projects ?? []) {
          opts.push({ value: `project:${proj.id}`, label: `    — ${proj.title}`, name: proj.title });
        }
        for (const sp of prog.standalone_projects ?? []) {
          opts.push({ value: `project:${sp.id}`, label: `    — ${sp.title}`, name: sp.title });
        }
      }
      return opts;
    })
  );

  const entityNameMap = $derived(
    Object.fromEntries(entityOptions.map((o) => [o.value, o.name]))
  );

  // --- Pure helpers (function declarations: hoisted, safe to call at init) ---
  function todayISO() {
    const d = new Date();
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
  }

  function impactColor(level) {
    switch (Number(level)) {
      case 1: return '#10b981'; // green
      case 2: return '#84cc16'; // lime
      case 3: return '#eab308'; // yellow
      case 4: return '#f97316'; // orange
      case 5: return '#ef4444'; // red
      default: return '#6b7280';
    }
  }

  function sourceLabel(source) {
    switch (source) {
      case 'corporate_project': return 'Corporate project';
      case 'corporate_activity': return 'Corporate activity';
      case 'bau': return 'BAU';
      case 'local': return 'Local';
      default: return source ?? '—';
    }
  }

  function entityLabel(entry) {
    if (!entry.entity_type || entry.entity_id == null) return '—';
    const key = `${entry.entity_type}:${entry.entity_id}`;
    return entityNameMap[key] ?? '—';
  }

  function weeksLabel(entry) {
    const s = entry.start_week || '';
    const e = entry.end_week || '';
    if (s && e) return `${s} → ${e}`;
    return s || e || '—';
  }

  // --- Group × month grid helpers ------------------------------------------
  const IMPACT_LABELS = ['unknown', 'minimal', 'low', 'moderate', 'high', 'severe'];

  /** Parse an ISO week label (YYYY-Www) to its Monday, or null. */
  function isoWeekMonday(week) {
    const m = /^(\d{4})-W(\d{1,2})$/.exec(week ?? '');
    if (!m) return null;
    const year = Number(m[1]);
    const weekNo = Number(m[2]);
    // Jan 4 is always in ISO week 1.
    const jan4 = new Date(Date.UTC(year, 0, 4));
    const day = (jan4.getUTCDay() + 6) % 7; // Monday=0
    const week1Monday = new Date(jan4);
    week1Monday.setUTCDate(jan4.getUTCDate() - day);
    const monday = new Date(week1Monday);
    monday.setUTCDate(week1Monday.getUTCDate() + (weekNo - 1) * 7);
    return monday;
  }

  /** Months (YYYY-MM) covered by a dated entry, in order. */
  function entryMonths(entry) {
    const start = isoWeekMonday(entry.start_week);
    const end = isoWeekMonday(entry.end_week);
    if (!start || !end) return [];
    const months = [];
    for (let d = new Date(start); d <= end && months.length < 400; d.setUTCDate(d.getUTCDate() + 7)) {
      const m = `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
      if (months[months.length - 1] !== m) months.push(m);
    }
    return months;
  }

  /** Effective per-group level for one entry: group impacts override the base level. */
  function entryGroupLevels(entry) {
    const base = Number(entry.impact_level ?? 0);
    const byGroup = new Map();
    for (const g of entry.group_impacts ?? []) {
      byGroup.set(g.business_group, Math.max(base, Number(g.impact_level ?? 0)));
    }
    if (byGroup.size === 0 && entry.business_unit?.trim()) {
      byGroup.set(entry.business_unit.trim(), base);
    }
    return byGroup;
  }

  const POSTURES = ['proceed', 'caution', 'pause', 'reduce'];
  function postureColor(p) {
    switch (p) {
      case 'proceed': return '#10b981';
      case 'caution': return '#f59e0b';
      case 'pause': return '#ef4444';
      case 'reduce': return '#8b5cf6';
      default: return '#6b7280';
    }
  }

  /** Next N months (YYYY-MM) starting from the current month. */
  function nextMonths(n) {
    const out = [];
    const d = new Date();
    for (let i = 0; i < n; i++) {
      const m = new Date(Date.UTC(d.getFullYear(), d.getMonth() + i, 1));
      out.push(`${m.getUTCFullYear()}-${String(m.getUTCMonth() + 1).padStart(2, '0')}`);
    }
    return out;
  }

  const grid = $derived.by(() => {
    const dated = entries.filter((e) => e.start_week && e.end_week);
    const undated = entries.filter((e) => !(e.start_week && e.end_week));
    const months = new Set();
    const byGroup = new Map(); // group -> month -> max level
    for (const e of dated) {
      const levels = entryGroupLevels(e);
      const monthsHere = entryMonths(e);
      for (const [group, level] of levels) {
        if (!byGroup.has(group)) byGroup.set(group, new Map());
        const cell = byGroup.get(group);
        for (const m of monthsHere) {
          months.add(m);
          cell.set(m, Math.max(cell.get(m) ?? 0, level));
        }
      }
    }
    return {
      months: [...months].sort(),
      groups: [...byGroup.keys()].sort(),
      cells: byGroup,
      undated,
    };
  });

  // --- Change capacity (group × month posture) ------------------------------
  const CAPACITY_MONTHS = 6;
  let capacityMonths = $state(nextMonths(CAPACITY_MONTHS));
  /** "group|month" -> { id, posture, notes } */
  let capacity = $state({});
  let capacityGroups = $state([]);
  let capacityGroupDraft = $state('');
  let capacityLoading = $state(false);
  let capacityError = $state('');
  /** Curated business-group reference values (datalists + capacity groups). */
  let refBusinessGroups = $state([]);

  const capacityKey = (group, month) => `${group}|${month}`;

  async function loadCapacity() {
    capacityLoading = true;
    capacityError = '';
    try {
      const rows = await p3mApi.getChangeCapacity({
        from_month: capacityMonths[0],
        to_month: capacityMonths[capacityMonths.length - 1],
      });
      const next = {};
      const groups = new Set(capacityGroups);
      for (const r of Array.isArray(rows) ? rows : []) {
        next[capacityKey(r.business_group, r.month)] = {
          id: r.id,
          posture: r.posture,
          notes: r.notes ?? '',
        };
        groups.add(r.business_group);
      }
      capacity = next;
      // Seed group rows from the business-group reference values so the grid
      // is usable before any posture has been saved.
      const ref = await p3mApi
        .getReferenceValues({ category: 'business_group' })
        .catch(() => []);
      refBusinessGroups = (Array.isArray(ref) ? ref : []).map((r) => r.value);
      for (const r of Array.isArray(ref) ? ref : []) groups.add(r.value);
      capacityGroups = [...groups].sort();
    } catch (e) {
      capacityError = 'Failed to load change capacity.';
    } finally {
      capacityLoading = false;
    }
  }

  async function setCapacityCell(group, month, posture) {
    capacityError = '';
    const key = capacityKey(group, month);
    const existing = capacity[key];
    try {
      if (posture) {
        await p3mApi.upsertChangeCapacity({
          rows: [{ business_group: group, month, posture }],
        });
        capacity[key] = { ...(existing ?? {}), posture };
      } else if (existing) {
        await p3mApi.deleteChangeCapacity({ business_group: group, month });
        const { [key]: _removed, ...rest } = capacity;
        capacity = rest;
      }
    } catch (e) {
      capacityError = e?.message || 'Failed to save change capacity.';
    }
  }

  function addCapacityGroup() {
    const g = capacityGroupDraft.trim();
    if (!g || capacityGroups.includes(g)) return;
    capacityGroups = [...capacityGroups, g].sort();
    capacityGroupDraft = '';
  }

  onMount(() => {
    void loadCapacity();
  });

  // --- State ---
  let entries = $state([]);
  let loading = $state(false);
  let error = $state('');

  let formOpen = $state(false);
  let editingId = $state(null);
  const emptyForm = () => ({
    activity: '',
    business_unit: '',
    entity: '',
    start_week: '',
    end_week: '',
    impact_level: 3,
    source: 'corporate_project',
    category: '',
    group_impacts: [{ business_group: '', impact_level: 0 }],
    notes: ''
  });
  let form = $state(emptyForm());

  let weekFrom = $state(todayISO());
  let weeks = $state([]);
  let weeksLoading = $state(false);
  let weeksError = $state('');

  const inputCls = 'w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1.5 text-sm';

  // --- Data loading ---
  async function loadEntries() {
    loading = true;
    error = '';
    try {
      entries = await p3mApi.changeCalendar.getAll();
    } catch (e) {
      error = 'Failed to load change calendar entries.';
    } finally {
      loading = false;
    }
  }

  onMount(loadEntries);

  // --- Form handlers ---
  function openNew() {
    editingId = null;
    form = emptyForm();
    formOpen = true;
  }

  function openEdit(entry) {
    editingId = entry.id;
    const entityKey =
      entry.entity_type && entry.entity_id != null ? `${entry.entity_type}:${entry.entity_id}` : '';
    form = {
      activity: entry.activity ?? '',
      business_unit: entry.business_unit ?? '',
      entity: entityKey,
      start_week: entry.start_week ?? '',
      end_week: entry.end_week ?? '',
      impact_level: entry.impact_level ?? 3,
      source: entry.source ?? 'corporate_project',
      category: entry.category ?? '',
      group_impacts: (entry.group_impacts ?? []).length
        ? entry.group_impacts.map((g) => ({ business_group: g.business_group, impact_level: Number(g.impact_level ?? 0) }))
        : [{ business_group: '', impact_level: 0 }],
      notes: entry.notes ?? ''
    };
    formOpen = true;
  }

  function closeForm() {
    formOpen = false;
    editingId = null;
  }

  async function saveEntry() {
    if (!form.activity.trim()) return;
    const payload = {
      activity: form.activity,
      business_unit: form.business_unit || null,
      start_week: form.start_week || null,
      end_week: form.end_week || null,
      impact_level: Number(form.impact_level) || 1,
      source: form.source,
      category: form.category.trim() || null,
      group_impacts: form.group_impacts
        .filter((g) => g.business_group.trim())
        .map((g) => ({ business_group: g.business_group.trim(), impact_level: Number(g.impact_level) || 0 })),
      notes: form.notes || null
    };
    if (form.entity) {
      const [type, id] = form.entity.split(':');
      payload.entity_type = type;
      payload.entity_id = Number(id);
    } else {
      payload.entity_type = null;
      payload.entity_id = null;
    }
    try {
      if (editingId != null) {
        await p3mApi.changeCalendar.update(editingId, payload);
      } else {
        await p3mApi.changeCalendar.create(payload);
      }
      closeForm();
      await loadEntries();
    } catch (e) {
      error = 'Failed to save entry.';
    }
  }

  async function deleteEntry(entry) {
    if (!confirm(t('p3m.confirmDeleteEntry'))) return;
    try {
      await p3mApi.changeCalendar.delete(entry.id);
      await loadEntries();
    } catch (e) {
      error = 'Failed to delete entry.';
    }
  }

  // --- Week grid ---
  async function loadWeeks() {
    weeksLoading = true;
    weeksError = '';
    try {
      const res = await p3mApi.getChangeCalendarWeek({ from: weekFrom });
      weeks = res.weeks ?? [];
    } catch (e) {
      weeksError = 'Failed to load weekly load.';
      weeks = [];
    } finally {
      weeksLoading = false;
    }
  }
</script>

<div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
  <!-- Header -->
  <div class="flex items-start justify-between gap-3">
    <div>
      <h2 class="text-lg font-semibold">{t('p3m.changeTitle')}</h2>
      <p class="mt-0.5 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.changeSubtitle')}</p>
    </div>
    <button
      onclick={openNew}
      class="shrink-0 rounded-md bg-[#2563eb] px-3 py-1.5 text-sm font-medium text-white hover:bg-[#1d4ed8]"
    >
      {t('p3m.newEntry')}
    </button>
  </div>

  <!-- Form -->
  {#if formOpen}
    <div class="mt-4 rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
      <h3 class="mb-2 text-sm font-semibold">{editingId != null ? 'Edit entry' : t('p3m.newEntry')}</h3>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.activity')} *</span>
          <input type="text" bind:value={form.activity} required class={inputCls} />
        </label>

        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.businessUnit')}</span>
          <input type="text" bind:value={form.business_unit} class={inputCls} />
        </label>

        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.linkedEntity')}</span>
          <select bind:value={form.entity} class={inputCls}>
            <option value="">{t('p3m.standaloneEntry')}</option>
            {#each entityOptions as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </label>

        <div class="grid grid-cols-2 gap-3">
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{t('p3m.startWeek')}</span>
            <input type="text" bind:value={form.start_week} placeholder="2026-W45" class={inputCls} />
          </label>
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{t('p3m.endWeek')}</span>
            <input type="text" bind:value={form.end_week} placeholder="2026-W45" class={inputCls} />
          </label>
        </div>

        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.impactLevel')}</span>
          <input type="number" min="1" max="5" bind:value={form.impact_level} class={inputCls} />
        </label>

        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.source')}</span>
          <select bind:value={form.source} class={inputCls}>
            <option value="corporate_project">Corporate project</option>
            <option value="corporate_activity">Corporate activity</option>
            <option value="bau">BAU</option>
            <option value="local">Local</option>
          </select>
        </label>

        <label class="block">
          <span class="mb-1 block text-xs font-medium">{t('p3m.report.category')}</span>
          <input type="text" bind:value={form.category} class={inputCls} />
        </label>

        <!-- Group-impact sub-editor: one row per affected business group. -->
        <div class="md:col-span-2">
          <span class="mb-1 block text-xs font-medium">{t('p3m.report.groupImpacts')}</span>
          {#each form.group_impacts as gi, giIdx (giIdx)}
            <div class="mb-1.5 flex items-center gap-2">
              <input
                type="text"
                list="ohpf-ref-business-group"
                value={gi.business_group}
                placeholder={t('p3m.businessUnit')}
                class={inputCls + ' flex-1'}
                oninput={(e) => {
                  const next = [...form.group_impacts];
                  next[giIdx] = { ...next[giIdx], business_group: e.target.value };
                  form.group_impacts = next;
                }}
              />
              <select
                value={String(gi.impact_level)}
                class={inputCls + ' w-28'}
                onchange={(e) => {
                  const next = [...form.group_impacts];
                  next[giIdx] = { ...next[giIdx], impact_level: Number(e.target.value) };
                  form.group_impacts = next;
                }}
              >
                {#each IMPACT_LABELS as lbl, lvl (lvl)}
                  <option value={lvl}>{lvl} — {t(`p3m.report.impact_${lbl}`)}</option>
                {/each}
              </select>
              <button
                type="button"
                onclick={() => (form.group_impacts = form.group_impacts.filter((_, i) => i !== giIdx))}
                class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-black/5"
                disabled={form.group_impacts.length === 1}
              >
                ×
              </button>
            </div>
          {/each}
          <button
            type="button"
            onclick={() => (form.group_impacts = [...form.group_impacts, { business_group: '', impact_level: 0 }])}
            class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-black/5"
          >
            + {t('p3m.report.addGroupImpact')}
          </button>
        </div>

        <label class="block md:col-span-2">
          <span class="mb-1 block text-xs font-medium">{t('p3m.notes')}</span>
          <textarea bind:value={form.notes} rows="2" class={inputCls}></textarea>
        </label>
      </div>

      <datalist id="ohpf-ref-business-group">
        {#each refBusinessGroups as v (v)}
          <option value={v} />
        {/each}
      </datalist>

      <div class="mt-4 flex gap-2">
        <button
          onclick={saveEntry}
          class="rounded-md bg-[#2563eb] px-3 py-1.5 text-sm font-medium text-white hover:bg-[#1d4ed8]"
        >
          Save
        </button>
        <button
          onclick={closeForm}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-sm hover:bg-black/5"
        >
          Cancel
        </button>
      </div>
    </div>
  {/if}

  <!-- Error -->
  {#if error}
    <p class="mt-2 text-xs text-[#ef4444]">{error}</p>
  {/if}

  <!-- Entries table -->
  <div class="mt-4 overflow-x-auto rounded-lg border border-[var(--color-ds-border,#e5e7eb)]">
    <table class="w-full text-sm">
      <thead>
        <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-left text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
          <th class="px-3 py-2 font-medium">{t('p3m.activity')}</th>
          <th class="px-3 py-2 font-medium">{t('p3m.linkedEntity')}</th>
          <th class="px-3 py-2 font-medium">Weeks</th>
          <th class="px-3 py-2 font-medium">{t('p3m.impactLevel')}</th>
          <th class="px-3 py-2 font-medium">{t('p3m.source')}</th>
          <th class="px-3 py-2 font-medium">{t('p3m.businessUnit')}</th>
          <th class="px-3 py-2"></th>
        </tr>
      </thead>
      <tbody>
        {#if loading}
          <tr>
            <td colspan="7" class="px-3 py-4 text-center text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</td>
          </tr>
        {:else if entries.length === 0}
          <tr>
            <td colspan="7" class="px-3 py-4 text-center text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noEntries')}</td>
          </tr>
        {:else}
          {#each entries as entry (entry.id)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="px-3 py-2">{entry.activity}</td>
              <td class="px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{entityLabel(entry)}</td>
              <td class="whitespace-nowrap px-3 py-2 text-xs">{weeksLabel(entry)}</td>
              <td class="px-3 py-2">
                <span
                  class="inline-flex min-w-[1.5rem] items-center justify-center rounded-full px-2 py-0.5 text-xs font-semibold text-white"
                  style="background-color:{impactColor(entry.impact_level)}"
                >{entry.impact_level}</span>
              </td>
              <td class="px-3 py-2 text-xs">{sourceLabel(entry.source)}</td>
              <td class="px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{entry.business_unit ?? '—'}</td>
              <td class="whitespace-nowrap px-3 py-2 text-right">
                <button onclick={() => openEdit(entry)} class="text-xs font-medium text-[#2563eb] hover:underline">Edit</button>
                <span class="mx-1 text-[var(--color-ds-border,#e5e7eb)]">|</span>
                <button onclick={() => deleteEntry(entry)} class="text-xs font-medium text-[#ef4444] hover:underline">Delete</button>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  <!-- Group × month impact grid -->
  <div class="mt-6">
    <h3 class="text-sm font-semibold">{t('p3m.report.groupMonthGrid')}</h3>
    {#if grid.groups.length && grid.months.length}
      <div class="mt-3 overflow-x-auto rounded-lg border border-[var(--color-ds-border,#e5e7eb)]">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-left text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="px-3 py-2 font-medium">{t('p3m.report.group')}</th>
              {#each grid.months as m (m)}
                <th class="px-3 py-2 font-medium">{m}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each grid.groups as g (g)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="px-3 py-2 text-xs font-medium">{g}</td>
                {#each grid.months as m (m)}
                  {@const level = grid.cells.get(g)?.get(m) ?? 0}
                  <td class="px-3 py-2">
                    {#if level > 0}
                      <span
                        class="inline-flex min-w-[1.5rem] items-center justify-center rounded-full px-2 py-0.5 text-xs font-semibold text-white"
                        style="background-color:{impactColor(level)}"
                        title={t(`p3m.report.impact_${IMPACT_LABELS[level]}`)}
                      >{level}</span>
                    {:else}
                      <span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">·</span>
                    {/if}
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="mt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noGridData')}</p>
    {/if}

    {#if grid.undated.length}
      <p class="mb-1 mt-3 text-xs font-medium">{t('p3m.report.undatedEntries')}</p>
      <div class="flex flex-wrap gap-1.5">
        {#each grid.undated as entry (entry.id)}
          <span
            class="rounded-full px-2 py-0.5 text-xs font-medium text-white"
            style="background-color:{impactColor(entry.impact_level)}"
          >{entry.activity}</span>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Change capacity (group × month posture) -->
  <div class="mt-6">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold">{t('p3m.report.changeCapacityTitle')}</h3>
      <div class="flex items-center gap-2">
        <input
          type="text"
          list="ohpf-ref-business-group"
          bind:value={capacityGroupDraft}
          placeholder={t('p3m.report.addGroup')}
          class={inputCls + ' w-44'}
        />
        <button
          onclick={addCapacityGroup}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-sm hover:bg-black/5"
        >
          +
        </button>
      </div>
    </div>

    {#if capacityError}
      <p class="mt-2 text-xs text-[#ef4444]">{capacityError}</p>
    {/if}
    {#if capacityLoading}
      <p class="mt-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
    {:else if capacityGroups.length}
      <div class="mt-3 overflow-x-auto rounded-lg border border-[var(--color-ds-border,#e5e7eb)]">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-left text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="px-3 py-2 font-medium">{t('p3m.report.group')}</th>
              {#each capacityMonths as m (m)}
                <th class="px-3 py-2 font-medium">{m}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each capacityGroups as g (g)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="px-3 py-2 text-xs font-medium">{g}</td>
                {#each capacityMonths as m (m)}
                  {@const cell = capacity[capacityKey(g, m)]}
                  <td class="px-3 py-2">
                    <select
                      value={cell?.posture ?? ''}
                      class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs"
                      style={cell?.posture ? `color:${postureColor(cell.posture)}` : ''}
                      onchange={(e) => setCapacityCell(g, m, e.target.value)}
                    >
                      <option value="">—</option>
                      {#each POSTURES as p (p)}
                        <option value={p}>{t(`p3m.report.posture_${p}`)}</option>
                      {/each}
                    </select>
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="mt-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.report.noCapacityGroups')}</p>
    {/if}
  </div>

  <!-- Week grid -->
  <div class="mt-6">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold">{t('p3m.weeklyLoad')}</h3>
      <div class="flex items-center gap-2">
        <input type="date" bind:value={weekFrom} class={inputCls + ' w-auto'} />
        <button
          onclick={loadWeeks}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1.5 text-sm hover:bg-black/5"
        >
          Load
        </button>
      </div>
    </div>

    {#if weeksError}
      <p class="mt-2 text-xs text-[#ef4444]">{weeksError}</p>
    {/if}

    {#if weeksLoading}
      <p class="mt-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
    {:else if weeks.length > 0}
      <div class="mt-3 space-y-2">
        {#each weeks as week (week.start)}
          <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
            <div class="flex items-center justify-between gap-2">
              <span class="text-sm font-medium">{week.start} – {week.end}</span>
              <span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">Load: {week.load}</span>
            </div>
            <div class="mt-2 flex flex-wrap gap-1.5">
              {#each week.entries as entry (entry.id)}
                <span
                  class="rounded-full px-2 py-0.5 text-xs font-medium text-white"
                  style="background-color:{impactColor(entry.impact_level)}"
                >{entry.activity}</span>
              {:else}
                <span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">No entries</span>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else if !weeksError}
      <p class="mt-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">Select a date and press Load to see weekly load.</p>
    {/if}
  </div>
</div>
