<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  const SORTS = [
    { id: 'key', labelKey: 'plans.listSortKey' },
    { id: 'title', labelKey: 'plans.listSortTitle' },
    { id: 'status', labelKey: 'plans.listSortStatus' },
    { id: 'type', labelKey: 'plans.listSortType' },
    { id: 'team', labelKey: 'plans.listSortTeam' },
    { id: 'assignee', labelKey: 'plans.listSortAssignee' },
    { id: 'start_date', labelKey: 'plans.listSortStart' },
    { id: 'end_date', labelKey: 'plans.listSortEnd' },
    { id: 'due_date', labelKey: 'plans.listSortDue' },
  ];

  let search = $state('');
  let sort = $state('key');
  let items = $state([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state('');
  let debounceTimer = null;

  async function load() {
    try {
      const res = await plansApi.getItems(planId, { ...(search ? { search } : {}), sort, limit: 500 });
      items = res?.items ?? [];
      total = res?.total ?? items.length;
      error = '';
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  function onSearchInput() {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(load, 300);
  }

  onMount(load);

  const fmtDate = (d) => (d ? String(d).slice(0, 10) : '');
</script>

<div class="space-y-4">
  <!-- Toolbar -->
  <div class="flex flex-wrap items-center gap-2">
    <input
      type="search"
      bind:value={search}
      oninput={onSearchInput}
      placeholder={t('plans.listSearchPlaceholder')}
      class="h-8 w-64 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
    />
    <select
      value={sort}
      onchange={(e) => {
        sort = e.currentTarget.value;
        load();
      }}
      class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none"
    >
      {#each SORTS as s (s.id)}
        <option value={s.id}>{t(s.labelKey)}</option>
      {/each}
    </select>
  </div>

  {#if loading && items.length === 0}
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
  {:else if error && items.length === 0}
    <p class="text-sm text-red-600">{error}</p>
  {:else if items.length === 0}
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.listEmpty')}</p>
  {:else}
    <div class="overflow-x-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface-hover,#f3f4f6)] text-left text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="px-3 py-2 font-medium">{t('plans.colKey')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colTitle')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colStatus')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colType')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colWorkspace')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colTeam')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colAssignee')}</th>
            <th class="px-3 py-2 font-medium">{t('plans.colSchedule')}</th>
          </tr>
        </thead>
        <tbody>
          {#each items as item (item.id)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-b-0 hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
              <td class="whitespace-nowrap px-3 py-2 tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</td>
              <td class="max-w-md truncate px-3 py-2" title={item.title}>{item.title}</td>
              <td class="whitespace-nowrap px-3 py-2">
                <span class="rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs">{item.status_name}</span>
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.item_type_name || '—'}</td>
              <td class="max-w-40 truncate px-3 py-2 text-xs" title={item.workspace_name}>{item.workspace_name}</td>
              <td class="whitespace-nowrap px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.team_name || '—'}</td>
              <td class="whitespace-nowrap px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.assignee_name || '—'}</td>
              <td class="whitespace-nowrap px-3 py-2 tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">
                {fmtDate(item.start_date) && fmtDate(item.end_date)
                  ? `${fmtDate(item.start_date)} → ${fmtDate(item.end_date)}`
                  : fmtDate(item.due_date) || '—'}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="text-xs tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.listShowing', { shown: items.length, total })}</p>
  {/if}
</div>
