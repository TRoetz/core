<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { getUsers } from '../../api/users.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { confirm } from '../../composables/useConfirm.js';
  import { formatMoney, formatDate, toISODate, toDateInputValue } from './utils.js';

  let { portfolios = [] } = $props();

  let resources = $state([]);
  let allocations = $state([]);
  let capacityRows = $state([]);
  let loading = $state(true);
  let error = $state('');

  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);

  const resourceNameById = $derived(new Map(resources.map((r) => [r.id, r.name])));

  const projectTitleById = $derived.by(() => {
    const m = new Map();
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) m.set(pr.id, `${pf.title} › ${pg.title} › ${pr.title}`);
      }
      for (const pr of pf.standalone_projects ?? []) m.set(pr.id, `${pf.title} › ${pr.title}`);
    }
    return m;
  });

  /** Flat project picker options for allocations. */
  const projectOptions = $derived.by(() => [...projectTitleById].map(([id, title]) => ({ id, title })));

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

  // --- Resource create / edit ---------------------------------------------------
  let resourceFormOpen = $state(false);
  /** Resource being edited in place, or null when creating. */
  let editingResource = $state(null);
  let savingResource = $state(false);
  const emptyResourceForm = () => ({ name: '', user_id: '', skills: '', cost_rate: '', capacity_hours_per_week: '' });
  let resourceForm = $state(emptyResourceForm());

  function openResourceCreate() {
    editingResource = null;
    resourceForm = emptyResourceForm();
    error = '';
    resourceFormOpen = true;
  }

  function openResourceEdit(r) {
    editingResource = r;
    resourceForm = {
      name: r.name ?? '',
      user_id: r.user_id != null ? String(r.user_id) : '',
      skills: (r.skills ?? []).join(', '),
      cost_rate: r.cost_rate != null ? String(r.cost_rate) : '',
      capacity_hours_per_week: r.capacity_hours_per_week != null ? String(r.capacity_hours_per_week) : '',
    };
    error = '';
    resourceFormOpen = true;
  }

  function closeResourceForm() {
    resourceFormOpen = false;
    editingResource = null;
  }

  async function submitResource() {
    if (savingResource) return;
    error = '';
    if (!resourceForm.name.trim()) {
      error = t('p3m.createFailed');
      return;
    }
    savingResource = true;
    try {
      const payload = {
        name: resourceForm.name.trim(),
        user_id: resourceForm.user_id ? Number(resourceForm.user_id) : null,
        skills: resourceForm.skills.split(',').map((s) => s.trim()).filter(Boolean),
        cost_rate: Number(resourceForm.cost_rate) || 0,
        capacity_hours_per_week: Math.max(0, Math.round(Number(resourceForm.capacity_hours_per_week) || 0)),
      };
      if (editingResource) await p3mApi.resources.update(editingResource.id, payload);
      else await p3mApi.resources.create(payload);
      closeResourceForm();
      await load();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingResource = false;
    }
  }

  async function deleteResource(r) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteResource') }))) return;
    try {
      await p3mApi.resources.delete(r.id);
      await load();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  // --- Allocation create / edit ----------------------------------------------------
  let allocationFormOpen = $state(false);
  /** Allocation being edited in place, or null when creating. */
  let editingAllocation = $state(null);
  let savingAllocation = $state(false);
  const emptyAllocationForm = () => ({ resource_id: '', project_id: '', start_date: '', end_date: '', hours_per_week: '', booking_type: 'soft' });
  let allocationForm = $state(emptyAllocationForm());

  function openAllocationCreate() {
    editingAllocation = null;
    allocationForm = emptyAllocationForm();
    error = '';
    allocationFormOpen = true;
  }

  function openAllocationEdit(a) {
    editingAllocation = a;
    allocationForm = {
      resource_id: String(a.resource_id),
      project_id: a.project_id != null ? String(a.project_id) : '',
      start_date: toDateInputValue(a.start_date),
      end_date: toDateInputValue(a.end_date),
      hours_per_week: a.hours_per_week != null ? String(a.hours_per_week) : '',
      booking_type: a.booking_type ?? 'soft',
    };
    error = '';
    allocationFormOpen = true;
  }

  function closeAllocationForm() {
    allocationFormOpen = false;
    editingAllocation = null;
  }

  async function submitAllocation() {
    if (savingAllocation) return;
    error = '';
    const resourceId = Number(allocationForm.resource_id);
    const projectId = Number(allocationForm.project_id);
    if (!resourceId || !projectId) {
      error = t('p3m.entityRequired');
      return;
    }
    const start = toISODate(allocationForm.start_date);
    const end = toISODate(allocationForm.end_date);
    if (!start || !end) {
      error = t('p3m.createFailed');
      return;
    }
    savingAllocation = true;
    try {
      const payload = {
        resource_id: resourceId,
        project_id: projectId,
        start_date: start,
        end_date: end,
        hours_per_week: Math.max(0, Math.round(Number(allocationForm.hours_per_week) || 0)),
        booking_type: allocationForm.booking_type,
      };
      if (editingAllocation) await p3mApi.resourceAllocations.update(editingAllocation.id, payload);
      else await p3mApi.resourceAllocations.create(payload);
      closeAllocationForm();
      await load();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingAllocation = false;
    }
  }

  async function deleteAllocation(a) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteAllocation') }))) return;
    try {
      await p3mApi.resourceAllocations.delete(a.id);
      await load();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  onMount(() => {
    load();
    getUsers().then((u) => (users = u || [])).catch(() => {});
  });
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
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.resources')}</h2>
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openResourceCreate}>
          <IconPlus size={14} class="inline" /> {t('p3m.newResource')}
        </button>
      </div>

      {#if error && !resourceFormOpen && !allocationFormOpen}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
      {/if}

      {#if resourceFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-4">
            <label class="block">
              <span>{t('p3m.title')} *</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={resourceForm.name}
                disabled={savingResource}
                oninput={(e) => (resourceForm = { ...resourceForm, name: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.owner')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={resourceForm.user_id}
                disabled={savingResource}
                onchange={(e) => (resourceForm = { ...resourceForm, user_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each users as u (u.id)}
                  <option value={u.id}>{u.display_name || u.username}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.skills')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                placeholder="Go, SQL, K8s"
                value={resourceForm.skills}
                disabled={savingResource}
                oninput={(e) => (resourceForm = { ...resourceForm, skills: e.target.value })}
              />
            </label>

            <div class="grid grid-cols-2 gap-x-3">
              <label class="block">
                <span>{t('p3m.costRate')}</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={resourceForm.cost_rate}
                  disabled={savingResource}
                  oninput={(e) => (resourceForm = { ...resourceForm, cost_rate: e.target.value })}
                />
              </label>
              <label class="block">
                <span>{t('p3m.capacityHoursPerWeek')}</span>
                <input
                  type="number"
                  min="0"
                  step="1"
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={resourceForm.capacity_hours_per_week}
                  disabled={savingResource}
                  oninput={(e) => (resourceForm = { ...resourceForm, capacity_hours_per_week: e.target.value })}
                />
              </label>
            </div>
          </div>

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingResource} onclick={submitResource}>
              {editingResource ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeResourceForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

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
              <th class="py-1.5 pr-3"></th>
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
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1">
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openResourceEdit(r)}>
                      {t('p3m.edit')}
                    </button>
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteResource(r)}>
                      <IconX size={14} />
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Allocations -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.allocations')}</h2>
        {#if resources.length > 0 && projectOptions.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openAllocationCreate}>
            <IconPlus size={14} class="inline" /> {t('p3m.newAllocation')}
          </button>
        {/if}
      </div>

      {#if allocationFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-4">
            <label class="block">
              <span>{t('p3m.resources')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={allocationForm.resource_id}
                disabled={savingAllocation}
                onchange={(e) => (allocationForm = { ...allocationForm, resource_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each resources as r (r.id)}
                  <option value={r.id}>{r.name}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.projects')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={allocationForm.project_id}
                disabled={savingAllocation}
                onchange={(e) => (allocationForm = { ...allocationForm, project_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each projectOptions as p (p.id)}
                  <option value={p.id}>{p.title}</option>
                {/each}
              </select>
            </label>

            <div class="grid grid-cols-2 gap-x-3">
              <label class="block">
                <span>{t('p3m.startDate')} *</span>
                <input
                  type="date"
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={allocationForm.start_date}
                  disabled={savingAllocation}
                  oninput={(e) => (allocationForm = { ...allocationForm, start_date: e.target.value })}
                />
              </label>
              <label class="block">
                <span>{t('p3m.endDate')} *</span>
                <input
                  type="date"
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={allocationForm.end_date}
                  disabled={savingAllocation}
                  oninput={(e) => (allocationForm = { ...allocationForm, end_date: e.target.value })}
                />
              </label>
            </div>

            <div class="grid grid-cols-2 gap-x-3">
              <label class="block">
                <span>{t('p3m.hoursPerWeek')}</span>
                <input
                  type="number"
                  min="0"
                  step="1"
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={allocationForm.hours_per_week}
                  disabled={savingAllocation}
                  oninput={(e) => (allocationForm = { ...allocationForm, hours_per_week: e.target.value })}
                />
              </label>
              <label class="block">
                <span>{t('p3m.bookingType')}</span>
                <select
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={allocationForm.booking_type}
                  disabled={savingAllocation}
                  onchange={(e) => (allocationForm = { ...allocationForm, booking_type: e.target.value })}
                >
                  <option value="soft">soft</option>
                  <option value="hard">hard</option>
                </select>
              </label>
            </div>
          </div>

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingAllocation} onclick={submitAllocation}>
              {editingAllocation ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeAllocationForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

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
              <th class="py-1.5 pr-3"></th>
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
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1">
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openAllocationEdit(a)}>
                      {t('p3m.edit')}
                    </button>
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteAllocation(a)}>
                      <IconX size={14} />
                    </button>
                  </div>
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
