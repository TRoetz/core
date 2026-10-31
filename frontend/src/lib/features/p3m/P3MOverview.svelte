<script>
  import { t } from '../../stores/i18n.svelte.js';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import P3MProjectDetail from './P3MProjectDetail.svelte';
  import P3MProjectForm from './P3MProjectForm.svelte';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { getUsers } from '../../api/users.js';
  import { formatMoney, formatDate, ragColor, ragLabel, stageLabel, toISODate } from './utils.js';

  let { portfolios = [], summary = null, onChanged = () => {}, initialProjectId = null } = $props();

  // node id -> bool; default: portfolios open. A ?project= deep link starts
  // with that project's detail expanded (parents are open by default).
  const expanded = $state(initialProjectId ? { [initialProjectId]: true } : {});

  function toggle(id) {
    expanded[id] = !expanded[id];
  }

  // --- Creation forms -----------------------------------------------------
  const RAGS = ['green', 'amber', 'red'];

  let newPortfolioOpen = $state(false);
  let portfolioForm = $state(emptyPortfolioForm());
  let savingPortfolio = $state(false);
  let portfolioError = $state('');

  /** Portfolio id the programme form is open for, or null. */
  let programmeFormFor = $state(null);
  let programmeForm = $state(emptyProgrammeForm());
  let savingProgramme = $state(false);
  let programmeError = $state('');
  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);

  /** { portfolioId: number, programmeId: number|null } while the project form is open. */
  let projectFormCtx = $state(null);

  function emptyPortfolioForm() {
    return { title: '', description: '', strategic_objective: '', budget_allocated: '', rag_status: 'green' };
  }

  function emptyProgrammeForm() {
    return { title: '', description: '', manager_id: '', budget_allocated: '', target_start_date: '', target_end_date: '', rag_status: 'green' };
  }

  function toggleNewPortfolio() {
    newPortfolioOpen = !newPortfolioOpen;
    if (newPortfolioOpen) {
      portfolioForm = emptyPortfolioForm();
      portfolioError = '';
    }
  }

  async function submitPortfolio() {
    if (savingPortfolio) return;
    portfolioError = '';
    if (!portfolioForm.title.trim()) {
      portfolioError = t('p3m.titleRequired');
      return;
    }
    savingPortfolio = true;
    try {
      await p3mApi.portfolios.create({
        title: portfolioForm.title.trim(),
        description: portfolioForm.description,
        strategic_objective: portfolioForm.strategic_objective,
        budget_allocated: Number(portfolioForm.budget_allocated) || 0,
        rag_status: portfolioForm.rag_status,
      });
      newPortfolioOpen = false;
      onChanged();
    } catch (e) {
      portfolioError = e?.message || t('p3m.createFailed');
    } finally {
      savingPortfolio = false;
    }
  }

  function openProgrammeForm(pfId) {
    programmeFormFor = pfId;
    projectFormCtx = null;
    programmeForm = emptyProgrammeForm();
    programmeError = '';
    if (!users.length) getUsers().then((u) => (users = u || [])).catch(() => {});
  }

  async function submitProgramme() {
    if (savingProgramme) return;
    programmeError = '';
    if (!programmeForm.title.trim()) {
      programmeError = t('p3m.titleRequired');
      return;
    }
    savingProgramme = true;
    try {
      const payload = {
        portfolio_id: Number(programmeFormFor),
        title: programmeForm.title.trim(),
        description: programmeForm.description,
        budget_allocated: Number(programmeForm.budget_allocated) || 0,
        rag_status: programmeForm.rag_status,
      };
      if (programmeForm.manager_id) payload.manager_id = Number(programmeForm.manager_id);
      const start = toISODate(programmeForm.target_start_date);
      if (start) payload.target_start_date = start;
      const end = toISODate(programmeForm.target_end_date);
      if (end) payload.target_end_date = end;

      await p3mApi.programmes.create(payload);
      programmeFormFor = null;
      onChanged();
    } catch (e) {
      programmeError = e?.message || t('p3m.createFailed');
    } finally {
      savingProgramme = false;
    }
  }

  function openProjectForm(pfId, pgId = null) {
    projectFormCtx = { portfolioId: pfId, programmeId: pgId };
    programmeFormFor = null;
  }

  function closeProjectForm() {
    projectFormCtx = null;
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

  <!-- New portfolio -->
  <div class="px-6 pt-4">
    <button type="button" class="p3m-action p3m-action-primary" onclick={toggleNewPortfolio}>
      <IconPlus size={14} /> {t('p3m.newPortfolio')}
    </button>

    {#if newPortfolioOpen}
      <div class="mt-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
        <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-2">
          <label class="block">
            <span>{t('p3m.titleLabel')} *</span>
            <input
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={portfolioForm.title}
              disabled={savingPortfolio}
              oninput={(e) => (portfolioForm = { ...portfolioForm, title: e.target.value })}
            />
          </label>

          <label class="block">
            <span>{t('p3m.ragStatus')}</span>
            <select
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={portfolioForm.rag_status}
              disabled={savingPortfolio}
              onchange={(e) => (portfolioForm = { ...portfolioForm, rag_status: e.target.value })}
            >
              {#each RAGS as r (r)}
                <option value={r}>{ragLabel(r)}</option>
              {/each}
            </select>
          </label>

          <label class="block">
            <span>{t('p3m.budgetAllocated')}</span>
            <input
              type="number" step="any" min="0"
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={portfolioForm.budget_allocated}
              disabled={savingPortfolio}
              oninput={(e) => (portfolioForm = { ...portfolioForm, budget_allocated: e.target.value })}
            />
          </label>

          <label class="block">
            <span>{t('p3m.strategicObjective')}</span>
            <input
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={portfolioForm.strategic_objective}
              disabled={savingPortfolio}
              oninput={(e) => (portfolioForm = { ...portfolioForm, strategic_objective: e.target.value })}
            />
          </label>

          <label class="block md:col-span-2">
            <span>{t('p3m.description')}</span>
            <textarea
              rows="2"
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={portfolioForm.description}
              disabled={savingPortfolio}
              oninput={(e) => (portfolioForm = { ...portfolioForm, description: e.target.value })}
            ></textarea>
          </label>
        </div>

        {#if portfolioError}
          <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{portfolioError}</div>
        {/if}

        <div class="mt-3 flex items-center gap-2">
          <button type="button" class="p3m-submit" disabled={savingPortfolio} onclick={submitPortfolio}>
            {t('p3m.create')}
          </button>
          <button type="button" class="p3m-cancel" onclick={toggleNewPortfolio}>{t('p3m.cancel')}</button>
        </div>
      </div>
    {/if}
  </div>

  <!-- Hierarchy tree -->
  <div class="px-6 py-4">
    {#if portfolios.length === 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('p3m.emptyTitle')} — {t('p3m.emptyDescription')}
      </div>
    {:else}
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
          <div class="border-t border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
            {#if pf.strategic_objective}
              <div class="px-10 py-2 text-xs italic text-[var(--color-ds-text-muted,#6b7280)]">{pf.strategic_objective}</div>
            {/if}

            <!-- Create actions -->
            <div class="flex items-center gap-2 px-10 py-2">
              <button type="button" class="p3m-action" onclick={() => openProgrammeForm(pf.id)}>
                <IconPlus size={14} /> {t('p3m.addProgramme')}
              </button>
              <button type="button" class="p3m-action" onclick={() => openProjectForm(pf.id, null)}>
                <IconPlus size={14} /> {t('p3m.addProject')}
              </button>
            </div>

            {#if programmeFormFor === pf.id}
              <div class="mx-10 mb-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
                <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-2">
                  <label class="block">
                    <span>{t('p3m.titleLabel')} *</span>
                    <input
                      class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                      value={programmeForm.title}
                      disabled={savingProgramme}
                      oninput={(e) => (programmeForm = { ...programmeForm, title: e.target.value })}
                    />
                  </label>

                  <label class="block">
                    <span>{t('p3m.manager')}</span>
                    <select
                      class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                      value={programmeForm.manager_id}
                      disabled={savingProgramme}
                      onchange={(e) => (programmeForm = { ...programmeForm, manager_id: e.target.value })}
                    >
                      <option value="">{t('p3m.noSelection')}</option>
                      {#each users as u (u.id)}
                        <option value={u.id}>{u.display_name || u.username}</option>
                      {/each}
                    </select>
                  </label>

                  <label class="block">
                    <span>{t('p3m.budgetAllocated')}</span>
                    <input
                      type="number" step="any" min="0"
                      class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                      value={programmeForm.budget_allocated}
                      disabled={savingProgramme}
                      oninput={(e) => (programmeForm = { ...programmeForm, budget_allocated: e.target.value })}
                    />
                  </label>

                  <label class="block">
                    <span>{t('p3m.ragStatus')}</span>
                    <select
                      class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                      value={programmeForm.rag_status}
                      disabled={savingProgramme}
                      onchange={(e) => (programmeForm = { ...programmeForm, rag_status: e.target.value })}
                    >
                      {#each RAGS as r (r)}
                        <option value={r}>{ragLabel(r)}</option>
                      {/each}
                    </select>
                  </label>

                  <div class="grid grid-cols-2 gap-x-4">
                    <label class="block">
                      <span>{t('p3m.startDate')}</span>
                      <input
                        type="date"
                        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                        value={programmeForm.target_start_date}
                        disabled={savingProgramme}
                        oninput={(e) => (programmeForm = { ...programmeForm, target_start_date: e.target.value })}
                      />
                    </label>
                    <label class="block">
                      <span>{t('p3m.targetCompletionDate')}</span>
                      <input
                        type="date"
                        class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                        value={programmeForm.target_end_date}
                        disabled={savingProgramme}
                        oninput={(e) => (programmeForm = { ...programmeForm, target_end_date: e.target.value })}
                      />
                    </label>
                  </div>

                  <label class="block md:col-span-2">
                    <span>{t('p3m.description')}</span>
                    <textarea
                      rows="2"
                      class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                      value={programmeForm.description}
                      disabled={savingProgramme}
                      oninput={(e) => (programmeForm = { ...programmeForm, description: e.target.value })}
                    ></textarea>
                  </label>
                </div>

                {#if programmeError}
                  <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{programmeError}</div>
                {/if}

                <div class="mt-3 flex items-center gap-2">
                  <button type="button" class="p3m-submit" disabled={savingProgramme} onclick={submitProgramme}>
                    {t('p3m.create')}
                  </button>
                  <button type="button" class="p3m-cancel" onclick={() => (programmeFormFor = null)}>{t('p3m.cancel')}</button>
                </div>
              </div>
            {/if}

            {#if projectFormCtx && projectFormCtx.portfolioId === pf.id && projectFormCtx.programmeId === null}
              <div class="mx-10 mb-2">
                <P3MProjectForm
                  portfolios={portfolios}
                  defaultPortfolioId={pf.id}
                  onCreated={() => { closeProjectForm(); onChanged(); }}
                  onCancel={closeProjectForm}
                />
              </div>
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
                  <div class="flex items-center gap-2 px-4 py-1.5">
                    <button type="button" class="p3m-action" onclick={() => openProjectForm(pf.id, pg.id)}>
                      <IconPlus size={14} /> {t('p3m.addProject')}
                    </button>
                  </div>

                  {#if projectFormCtx && projectFormCtx.portfolioId === pf.id && projectFormCtx.programmeId === pg.id}
                    <div class="px-4 pb-2">
                      <P3MProjectForm
                        portfolios={portfolios}
                        defaultPortfolioId={pf.id}
                        defaultProgrammeId={pg.id}
                        onCreated={() => { closeProjectForm(); onChanged(); }}
                        onCancel={closeProjectForm}
                      />
                    </div>
                  {/if}

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
                      <P3MProjectDetail project={pr} onChanged={onChanged} onClose={() => toggle(pr.id)} />
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
                <P3MProjectDetail project={pr} onChanged={onChanged} onClose={() => toggle(pr.id)} />
              {/if}
            {/each}

            {#if (pf.programmes?.length ?? 0) === 0 && (pf.standalone_projects?.length ?? 0) === 0}
              <div class="px-10 py-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
    {/if}
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
    background: var(--ds-surface-hovered);
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

  .p3m-action {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.125rem 0.5rem;
    font-size: 0.75rem;
    color: var(--color-ds-text-muted, #6b7280);
    background: none;
    border: 1px solid var(--color-ds-border, #e5e7eb);
    border-radius: 0.375rem;
    cursor: pointer;
  }

  .p3m-action:hover {
    color: var(--color-ds-text, #111827);
  }

  .p3m-action-primary {
    color: var(--color-ds-accent-blue, #2563eb);
    border-color: var(--color-ds-accent-blue, #2563eb);
  }

  .p3m-submit {
    padding: 0.25rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 500;
    color: white;
    background: var(--color-ds-accent-blue, #2563eb);
    border: none;
    border-radius: 0.375rem;
    cursor: pointer;
  }

  .p3m-submit:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .p3m-cancel {
    padding: 0.25rem 0.75rem;
    font-size: 0.75rem;
    color: var(--color-ds-text-muted, #6b7280);
    background: none;
    border: 1px solid var(--color-ds-border, #e5e7eb);
    border-radius: 0.375rem;
    cursor: pointer;
  }

  .p3m-cancel:hover {
    color: var(--color-ds-text, #111827);
  }
</style>
