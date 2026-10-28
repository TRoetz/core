<script>
  import { t } from '../../stores/i18n.svelte.js';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import { formatMoney, formatDate, ragColor, ragLabel, stageLabel } from './utils.js';

  let { portfolios = [], summary = null } = $props();

  const expanded = $state({}); // node id -> bool; default: portfolios open

  function toggle(id) {
    expanded[id] = !expanded[id];
  }

  const totals = $derived.by(() => {
    let budgetAllocated = 0;
    let budgetApproved = 0;
    let budgetActual = 0;
    const ragCounts = { red: 0, amber: 0, green: 0 };
    for (const pf of portfolios) {
      budgetAllocated += pf.budget_allocated || 0;
      if (pf.rollup) {
        budgetApproved += pf.rollup.budget_approved || 0;
        budgetActual += pf.rollup.budget_actual || 0;
      }
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) countProject(pr);
      }
      for (const pr of pf.standalone_projects ?? []) countProject(pr);
    }
    function countProject(pr) {
      const rag = pr.rag_status || 'green';
      if (ragCounts[rag] !== undefined) ragCounts[rag] += 1;
    }
    return { budgetAllocated, budgetApproved, budgetActual, ragCounts };
  });

  function progressColor(percent) {
    if (percent >= 80) return '#10b981';
    if (percent >= 40) return '#f59e0b';
    return '#ef4444';
  }
</script>

<div class="p3m-overview">
  <!-- Executive summary cards -->
  <div class="grid grid-cols-2 gap-3 px-6 pt-4 md:grid-cols-5">
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.totalPortfolios')}</div>
      <div class="mt-1 text-xl font-semibold">{portfolios.length}</div>
    </div>
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.totalProgrammes')}</div>
      <div class="mt-1 text-xl font-semibold">
        {portfolios.reduce((n, pf) => n + (pf.programmes?.length ?? 0), 0)}
      </div>
    </div>
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.totalProjects')}</div>
      <div class="mt-1 text-xl font-semibold">
        {portfolios.reduce(
          (n, pf) =>
            n +
            (pf.standalone_projects?.length ?? 0) +
            (pf.programmes?.reduce((m, pg) => m + (pg.projects?.length ?? 0), 0) ?? 0),
          0
        )}
      </div>
    </div>
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.budgetHealth')}</div>
      <div class="mt-1 text-sm leading-6">
        <span>{formatMoney(totals.budgetApproved)}</span>
        <span class="text-[var(--color-ds-text-muted,#6b7280)]"> / {formatMoney(totals.budgetAllocated)}</span>
      </div>
    </div>
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
      <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.ragStatus')}</div>
      <div class="mt-1 flex items-center gap-2 text-sm">
        {#each ['red', 'amber', 'green'] as rag (rag)}
          <span class="flex items-center gap-1" title={ragLabel(rag)}>
            <span class="inline-block h-2.5 w-2.5 rounded-full" style="background: {ragColor(rag)}"></span>
            {totals.ragCounts[rag]}
          </span>
        {/each}
      </div>
    </div>
  </div>

  <!-- Hierarchy tree -->
  <div class="px-6 py-4">
    {#each portfolios as pf (pf.id)}
      {@const open = expanded[pf.id] !== false}
      <div class="mb-2 overflow-hidden rounded-lg border border-[var(--color-ds-border,#e5e7eb)]">
        <button type="button" class="p3m-node-row p3m-pf" onclick={() => toggle(pf.id)}>
          <IconChevronRight class={open ? 'rotated' : ''} size={16} />
          <span class="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style="background: {ragColor(pf.rollup?.rag_status ?? pf.rag_status)}"></span>
          <span class="font-medium">{pf.title}</span>
          <span class="p3m-meta">
            {pf.programmes?.length ?? 0} {t('p3m.programmes')} · {(pf.standalone_projects?.length ?? 0) + (pf.programmes?.reduce((n, pg) => n + (pg.projects?.length ?? 0), 0) ?? 0)} {t('p3m.projects')}
          </span>
          <span class="p3m-meta ml-auto">{formatMoney(pf.budget_allocated)}</span>
        </button>

        {#if open}
          <div class="border-t border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface-2,rgba(0,0,0,0.02))]">
            {#if pf.strategic_objective}
              <div class="px-10 py-2 text-xs italic text-[var(--color-ds-text-muted,#6b7280)]">{pf.strategic_objective}</div>
            {/if}

            {#each pf.programmes ?? [] as pg (pg.id)}
              {@const pgOpen = expanded[pg.id] !== false}
              <button type="button" class="p3m-node-row p3m-pg" onclick={() => toggle(pg.id)}>
                <IconChevronRight class={pgOpen ? 'rotated' : ''} size={14} />
                <span class="inline-block h-2 w-2 shrink-0 rounded-full" style="background: {ragColor(pg.rollup?.rag_status ?? pg.rag_status)}"></span>
                <span>{pg.title}</span>
                <span class="p3m-meta">{pg.projects?.length ?? 0} {t('p3m.projects')}</span>
                <span class="p3m-meta ml-auto">{formatMoney(pg.budget_allocated)}</span>
              </button>

              {#if pgOpen}
                <div class="border-l-2 border-[var(--color-ds-border,#e5e7eb)] ml-10">
                  {#each pg.projects ?? [] as pr (pr.id)}
                    {@const prOpen = expanded[pr.id] === true}
                    <button type="button" class="p3m-node-row p3m-pr" onclick={() => toggle(pr.id)}>
                      <IconChevronRight class={prOpen ? 'rotated' : ''} size={14} />
                      <span class="inline-block h-2 w-2 shrink-0 rounded-full" style="background: {ragColor(pr.rag_status)}"></span>
                      <span>{pr.title}</span>
                      <span class="p3m-meta">{stageLabel(pr.current_stage)}</span>
                      <span class="p3m-progress">
                        <span class="p3m-progress-fill" style="width: {pr.progress_percent}%; background: {progressColor(pr.progress_percent)}"></span>
                      </span>
                      <span class="p3m-meta">{pr.progress_percent}%</span>
                    </button>

                    {#if prOpen}
                      <div class="mb-2 ml-10 grid grid-cols-2 gap-x-8 gap-y-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-white p-3 text-xs md:grid-cols-4">
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.currentStage')}:</span> {stageLabel(pr.current_stage)}</div>
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.ragStatus')}:</span> {ragLabel(pr.rag_status)}</div>
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.budgetApproved')}:</span> {formatMoney(pr.budget_approved)}</div>
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.progressPercent')}:</span> {pr.progress_percent}%</div>
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.startDate')}:</span> {formatDate(pr.start_date) || t('p3m.notSet')}</div>
                        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.targetCompletionDate')}:</span> {formatDate(pr.target_completion_date) || t('p3m.notSet')}</div>
                        {#if pr.evm}
                          <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">SPI:</span> {pr.evm.spi.toFixed(2)}</div>
                          <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">CPI:</span> {pr.evm.cpi.toFixed(2)}</div>
                        {/if}
                        {#if pr.description}
                          <div class="col-span-2 md:col-span-4 text-[var(--color-ds-text-muted,#6b7280)]">{pr.description}</div>
                        {/if}
                      </div>
                    {/if}
                  {/each}
                </div>
              {/if}
            {/each}

            {#each pf.standalone_projects ?? [] as pr (pr.id)}
              {@const prOpen = expanded[pr.id] === true}
              <button type="button" class="p3m-node-row p3m-pr ml-6" onclick={() => toggle(pr.id)}>
                <IconChevronRight class={prOpen ? 'rotated' : ''} size={14} />
                <span class="inline-block h-2 w-2 shrink-0 rounded-full" style="background: {ragColor(pr.rag_status)}"></span>
                <span>{pr.title}</span>
                <span class="p3m-meta">{stageLabel(pr.current_stage)}</span>
                <span class="p3m-progress">
                  <span class="p3m-progress-fill" style="width: {pr.progress_percent}%; background: {progressColor(pr.progress_percent)}"></span>
                </span>
                <span class="p3m-meta">{pr.progress_percent}%</span>
              </button>

              {#if prOpen}
                <div class="mb-2 ml-10 grid grid-cols-2 gap-x-8 gap-y-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-white p-3 text-xs md:grid-cols-4">
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.currentStage')}:</span> {stageLabel(pr.current_stage)}</div>
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.ragStatus')}:</span> {ragLabel(pr.rag_status)}</div>
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.budgetApproved')}:</span> {formatMoney(pr.budget_approved)}</div>
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.progressPercent')}:</span> {pr.progress_percent}%</div>
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.startDate')}:</span> {formatDate(pr.start_date) || t('p3m.notSet')}</div>
                  <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.targetCompletionDate')}:</span> {formatDate(pr.target_completion_date) || t('p3m.notSet')}</div>
                  {#if pr.evm}
                    <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">SPI:</span> {pr.evm.spi.toFixed(2)}</div>
                    <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">CPI:</span> {pr.evm.cpi.toFixed(2)}</div>
                  {/if}
                  {#if pr.description}
                    <div class="col-span-2 md:col-span-4 text-[var(--color-ds-text-muted,#6b7280)]">{pr.description}</div>
                  {/if}
                </div>
              {/if}
            {/each}

            {#if (pf.programmes?.length ?? 0) === 0 && (pf.standalone_projects?.length ?? 0) === 0}
              <div class="px-10 py-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .p3m-node-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.5rem 0.75rem;
    font-size: 0.8125rem;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
  }

  .p3m-node-row:hover {
    background: var(--color-ds-surface-2, rgba(0, 0, 0, 0.03));
  }

  .p3m-pf {
    font-size: 0.875rem;
  }

  .p3m-pg {
    padding-left: 1.5rem;
  }

  .p3m-pr {
    padding-left: 2.5rem;
  }

  .rotated {
    transform: rotate(90deg);
  }

  .p3m-meta {
    font-size: 0.75rem;
    color: var(--color-ds-text-muted, #6b7280);
    white-space: nowrap;
  }

  .p3m-progress {
    position: relative;
    display: inline-block;
    width: 64px;
    height: 6px;
    border-radius: 3px;
    background: var(--color-ds-border, #e5e7eb);
    overflow: hidden;
  }

  .p3m-progress-fill {
    position: absolute;
    inset: 0 auto 0 0;
    border-radius: 3px;
  }
</style>
