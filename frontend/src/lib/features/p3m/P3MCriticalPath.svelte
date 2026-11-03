<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { formatDate } from './utils.js';

  /** 'project' | 'programme' — selects which endpoint to query. */
  let { entityType = 'project', id = null } = $props();

  /** @type {any} */
  let data = $state(null);
  /** Leave-impact payload (visibility only); null when unavailable. */
  let leaveImpact = $state(null);
  let loading = $state(false);
  /** @type {{message: string, itemIds: number[]}} */
  let error = $state(null);

  async function load() {
    if (id === null || id === undefined) return;
    loading = true;
    error = null;
    try {
      const [cpmResult, leaveResult] = await Promise.allSettled([
        entityType === 'programme'
          ? p3mApi.getProgrammeCriticalPath(id)
          : p3mApi.getProjectCriticalPath(id),
        // Leave impact is visibility-only; its failure must not hide the CPM.
        entityType === 'programme'
          ? p3mApi.getProgrammeLeaveImpact(id)
          : p3mApi.getProjectLeaveImpact(id),
      ]);
      data = cpmResult.status === 'fulfilled' ? cpmResult.value : null;
      leaveImpact = leaveResult.status === 'fulfilled' ? leaveResult.value : null;
      if (cpmResult.status !== 'fulfilled') throw cpmResult.reason;
    } catch (e) {
      // Cycle errors arrive as API error strings; display them verbatim.
      const ids = e?.body?.item_ids;
      data = null;
      leaveImpact = null;
      error = { message: e?.message || String(e), itemIds: Array.isArray(ids) ? ids : [] };
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

  const tasks = $derived(data?.tasks ?? []);
  const criticalChain = $derived(
    (data?.critical_task_ids ?? [])
      .map((tid) => tasks.find((task) => task.item_id === tid)?.title)
      .filter(Boolean),
  );

  /** item_id -> leave-impact row for CPM tasks with overlapping assignee leave. */
  const leaveByTask = $derived(
    new Map((leaveImpact?.tasks ?? []).map((row) => [row.item_id, row])),
  );
  const affectedResources = $derived(leaveImpact?.resources ?? []);

  function leaveTooltip(row) {
    const periods = (row.leave_periods ?? [])
      .map((p) => `${formatDate(p.start_date)} → ${formatDate(p.end_date)}`)
      .join(', ');
    return [row.assignee_name, periods].filter(Boolean).join(' — ');
  }

  function days(value) {
    return `${Math.round(Number(value ?? 0) * 10) / 10}d`;
  }
</script>

{#if loading && !data}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error}
  <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">
    {error.message}
    {#if error.itemIds.length > 0}
      <span class="mt-1 block font-mono">{t('p3m.cpmCycleItems')}: {error.itemIds.join(', ')}</span>
    {/if}
  </div>
{:else if data && (data.item_count ?? 0) === 0}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.cpmNoDatedItems')}
  </div>
{:else if data}
  <div class="flex flex-wrap items-center gap-x-6 gap-y-1">
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.cpmWindow')}:</span>
      {formatDate(data.project_start)} → {formatDate(data.project_end)}
    </div>
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.cpmSpan')}:</span>
      {days(data.critical_path_days)}
    </div>
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.cpmItems')}:</span>
      {data.item_count}
    </div>
    <div>
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.cpmDeps')}:</span>
      {data.dependency_count}
    </div>
  </div>

  {#if criticalChain.length > 0}
    <div class="mt-2 flex flex-wrap items-center gap-x-1.5 gap-y-1">
      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.criticalPath')}:</span>
      {#each criticalChain as title, i (`${i}:${title}`)}
        {#if i > 0}<span class="text-[var(--color-ds-text-muted,#6b7280)]">→</span>{/if}
        <span class="rounded border border-red-200 bg-red-50 px-1.5 py-0.5 font-medium text-red-700">{title}</span>
      {/each}
    </div>
  {/if}

  <table class="mt-2 w-full text-left">
    <thead>
      <tr class="text-[var(--color-ds-text-muted,#6b7280)]">
        <th class="py-1 pr-3 font-medium">{t('p3m.cpmTask')}</th>
        <th class="py-1 pr-3 font-medium">{t('p3m.cpmPlanned')}</th>
        <th class="py-1 pr-3 text-right font-medium">{t('p3m.cpmDuration')}</th>
        <th class="py-1 pr-3 font-medium">{t('p3m.cpmEarliest')}</th>
        <th class="py-1 pr-3 font-medium">{t('p3m.cpmLatest')}</th>
        <th class="py-1 text-right font-medium">{t('p3m.cpmFloat')}</th>
      </tr>
    </thead>
    <tbody>
      {#each tasks as task (task.item_id)}
        <tr class={task.is_critical ? 'bg-red-50/70' : ''}>
          <td class="py-1 pr-3">
            {task.title}
            {#if task.is_critical}
              <span class="ml-1.5 rounded bg-red-600 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-white">
                {t('p3m.cpmCritical')}
              </span>
            {/if}
            {#if leaveByTask.has(task.item_id)}
              {@const leaveRow = leaveByTask.get(task.item_id)}
              <span class="ml-1.5 rounded bg-amber-200 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-amber-900" title={leaveTooltip(leaveRow)}>
                {t('p3m.cpmTaskLeave', { days: days(leaveRow.overlap_days) })}
              </span>
            {/if}
            {#if (task.open_risks ?? 0) > 0}
              <span class="ml-1.5 rounded bg-red-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-red-700">
                {t('p3m.cpmRiskBadge', { count: task.open_risks })}
              </span>
            {/if}
            {#if (task.pending_decisions ?? 0) > 0}
              <span class="ml-1.5 rounded bg-amber-100 px-1 py-px text-[10px] font-semibold uppercase tracking-wide text-amber-800">
                {t('p3m.cpmDecisionBadge', { count: task.pending_decisions })}
              </span>
            {/if}
          </td>
          <td class="py-1 pr-3 whitespace-nowrap">{formatDate(task.start_date)} → {formatDate(task.end_date)}</td>
          <td class="py-1 pr-3 text-right tabular-nums">{days(task.duration_days)}</td>
          <td class="py-1 pr-3 whitespace-nowrap">{formatDate(task.earliest_start)} → {formatDate(task.earliest_finish)}</td>
          <td class="py-1 pr-3 whitespace-nowrap">{formatDate(task.latest_start)} → {formatDate(task.latest_finish)}</td>
          <td class={task.is_critical ? 'py-1 text-right font-semibold tabular-nums text-red-700' : 'py-1 text-right tabular-nums'}>
            {days(task.total_float_days)}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>

  {#if affectedResources.length > 0}
    <div class="mt-3 rounded-md border border-amber-200 bg-amber-50/60 p-2">
      <div class="text-xs font-semibold text-amber-900">{t('p3m.leaveImpactTitle')}</div>
      {#each affectedResources as res (res.resource_id)}
        <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          <span class="font-medium">{res.name}</span>
          {#if res.user_name}
            <span class="text-[var(--color-ds-text-muted,#6b7280)]">({res.user_name})</span>
          {/if}
          <span class="whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">
            {formatDate(res.allocation_start)} → {formatDate(res.allocation_end)} · {res.hours_per_week}h/wk
          </span>
          <span class="rounded bg-amber-200 px-1.5 py-0.5 font-medium text-amber-900">
            {t('p3m.leaveOverlap', { days: days(res.overlap_days) })}
          </span>
          <span>{t('p3m.lostHours', { hours: Math.round(Number(res.lost_hours ?? 0) * 10) / 10 })}</span>
          {#if !res.covered_by_substitute}
            <span class="font-medium text-red-700">{t('p3m.noSubstitute')}</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}
