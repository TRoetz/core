<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { time } from '../../api/time.js';
  import { workspaces as wsApi } from '../../api/workspaces.js';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { formatMoney, formatDate, ragColor, ragLabel, stageLabel } from './utils.js';

  let { project = null, onChanged = () => {}, onClose = () => {} } = $props();

  /** @type {any} */
  let rollup = $state(null);
  let loadingRollup = $state(false);
  let error = $state('');
  /** @type {Array<{id:number,name:string}>} */
  let timeProjects = $state([]);
  /** @type {Array<{id:number,name:string}>} */
  let allWorkspaces = $state([]);
  let selectedTimeProjectId = $state('');
  /** @type {Set<number>} */
  let checkedWorkspaceIds = $state(new Set());
  let savingLinks = $state(false);
  let linksSaved = $state(false);

  const timeProjectName = $derived(rollup?.time_project?.name ?? '');

  async function load() {
    if (!project) return;
    loadingRollup = true;
    error = '';
    selectedTimeProjectId = project.time_project_id ? String(project.time_project_id) : '';
    checkedWorkspaceIds = new Set();
    linksSaved = false;
    try {
      const [r, tps, wss] = await Promise.all([
        p3mApi.getProjectRollup(project.id),
        time.projects.getAll().catch(() => []),
        wsApi.getAll().catch(() => []),
      ]);
      rollup = r ?? null;
      timeProjects = Array.isArray(tps) ? tps : [];
      allWorkspaces = Array.isArray(wss) ? wss : [];
      checkedWorkspaceIds = new Set((r?.workspaces ?? []).map((w) => w.id));
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loadingRollup = false;
    }
  }

  $effect(() => {
    // Reload whenever the selected project changes.
    const id = project?.id;
    if (id !== undefined) load();
  });

  async function saveTimeProjectLink() {
    if (!project || savingLinks) return;
    const val = selectedTimeProjectId ? Number(selectedTimeProjectId) : null;
    if ((val ?? null) === (project.time_project_id ?? null)) return;
    savingLinks = true;
    error = '';
    linksSaved = false;
    try {
      // Fetch the canonical row so the full-replace update keeps every field.
      const current = await p3mApi.projects.get(project.id);
      await p3mApi.projects.update(project.id, { ...current, time_project_id: val });
      project.time_project_id = val;
      linksSaved = true;
      await load();
      onChanged();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingLinks = false;
    }
  }

  async function toggleWorkspace(wsId) {
    if (!project || savingLinks) return;
    const next = new Set(checkedWorkspaceIds);
    if (next.has(wsId)) next.delete(wsId);
    else next.add(wsId);
    checkedWorkspaceIds = next;
    savingLinks = true;
    error = '';
    linksSaved = false;
    try {
      await p3mApi.setProjectWorkspaces(project.id, [...next]);
      linksSaved = true;
      rollup = rollup ? { ...rollup, workspaces: allWorkspaces.filter((w) => next.has(w.id)) } : rollup;
      onChanged();
    } catch (e) {
      error = e?.message || 'error';
      await load();
    } finally {
      savingLinks = false;
    }
  }

  const financialRows = $derived(rollup ? [
    { label: t('p3m.budgetApproved'), value: formatMoney(rollup.financials?.budget_approved ?? 0) },
    { label: t('p3m.budgetActual'), value: formatMoney(rollup.financials?.budget_actual ?? 0) },
    { label: t('p3m.trancheTotal'), value: formatMoney(rollup.financials?.tranche_amount_total ?? 0) },
    { label: t('p3m.contractValue'), value: formatMoney(rollup.financials?.contract_value_total ?? 0) },
    { label: t('p3m.committedSpend'), value: formatMoney(rollup.financials?.contract_committed_spend ?? 0) },
    { label: t('p3m.actualSpend'), value: formatMoney(rollup.financials?.contract_actual_spend ?? 0) },
    { label: t('p3m.worklogHours'), value: `${Math.round((rollup.financials?.worklog_hours ?? 0) * 10) / 10} h` },
    { label: t('p3m.worklogCost'), value: formatMoney(rollup.financials?.worklog_cost ?? 0) },
  ] : []);
</script>

{#if project}
  <div class="mb-2 ml-10 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
    <!-- Summary fields -->
    <div class="grid grid-cols-2 gap-x-8 gap-y-1 md:grid-cols-4">
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.currentStage')}:</span> {stageLabel(project.current_stage)}</div>
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.ragStatus')}:</span> {ragLabel(project.rag_status)}</div>
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.budgetApproved')}:</span> {formatMoney(project.budget_approved)}</div>
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.progressPercent')}:</span> {project.progress_percent}%</div>
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.startDate')}:</span> {formatDate(project.start_date) || t('p3m.notSet')}</div>
      <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.targetCompletionDate')}:</span> {formatDate(project.target_completion_date) || t('p3m.notSet')}</div>
      {#if project.evm}
        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">SPI:</span> {project.evm.spi.toFixed(2)}</div>
        <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">CPI:</span> {project.evm.cpi.toFixed(2)}</div>
      {/if}
      {#if project.description}
        <div class="col-span-2 md:col-span-4 text-[var(--color-ds-text-muted,#6b7280)]">{project.description}</div>
      {/if}
    </div>

    <!-- Core links: Time & Projects + workspace membership -->
    <h3 class="mb-1 mt-3 font-semibold">{t('p3m.coreLinks')}</h3>
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2">
      <label class="flex items-center gap-2">
        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.timeProjectLink')}:</span>
        <select
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          value={selectedTimeProjectId}
          disabled={savingLinks}
          onchange={(e) => { selectedTimeProjectId = e.target.value; saveTimeProjectLink(); }}
        >
          <option value="">{t('p3m.noTimeProject')}</option>
          {#each timeProjects as tp (tp.id)}
            <option value={tp.id}>{tp.name}</option>
          {/each}
        </select>
      </label>

      <div class="flex flex-wrap items-center gap-2">
        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.workspaceMembership')}:</span>
        {#if allWorkspaces.length === 0}
          <span>{t('p3m.noData')}</span>
        {:else}
          {#each allWorkspaces as ws (ws.id)}
            <label class="flex cursor-pointer items-center gap-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5">
              <input type="checkbox" checked={checkedWorkspaceIds.has(ws.id)} disabled={savingLinks} onchange={() => toggleWorkspace(ws.id)} />
              {ws.name}
            </label>
          {/each}
        {/if}
      </div>

      {#if savingLinks}
        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</span>
      {:else if linksSaved && !error}
        <span style="color: #10b981;">{t('p3m.saved')}</span>
      {/if}
    </div>

    {#if error}
      <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{error}</div>
    {/if}

    <!-- Financial rollup -->
    <h3 class="mb-1 mt-4 font-semibold">{t('p3m.rollupFinancials')}</h3>
    {#if loadingRollup && !rollup}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</div>
    {:else if rollup}
      <table class="w-full max-w-md text-left">
        <tbody>
          {#each financialRows as row (row.label)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="py-1 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{row.label}</td>
              <td class="py-1 font-medium">{row.value}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <!-- Merged stakeholders (P3M + SRM) -->
    <h3 class="mb-1 mt-4 font-semibold">
      {t('p3m.stakeholders')} ({rollup?.stakeholders?.length ?? 0})
    </h3>
    {#if rollup && (rollup.stakeholders ?? []).length === 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
    {:else if rollup}
      <table class="w-full text-left">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('p3m.title')}</th>
            <th class="py-1.5 pr-3">{t('srm.source')}</th>
            <th class="py-1.5 pr-3">{t('p3m.roleTitle')}</th>
            <th class="py-1.5 pr-3">P/I</th>
          </tr>
        </thead>
        <tbody>
          {#each rollup.stakeholders as st (st.source + st.name)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="py-1.5 pr-3">{st.name}</td>
              <td class="py-1.5 pr-3">
                <span class="rounded-full px-2 py-0.5" style="background: {st.source === 'srm' ? '#dbeafe' : '#dcfce7'}; color: {st.source === 'srm' ? '#1d4ed8' : '#15803d'};">{st.source.toUpperCase()}</span>
              </td>
              <td class="py-1.5 pr-3">{st.role_title || st.job_title || ''}</td>
              <td class="py-1.5 pr-3">{st.power_score}/{st.interest_score}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <!-- Attached SRM contracts -->
    <h3 class="mb-1 mt-4 font-semibold">
      {t('p3m.attachedContracts')} ({rollup?.contracts?.length ?? 0})
    </h3>
    {#if rollup && (rollup.contracts ?? []).length === 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
    {:else if rollup}
      <table class="w-full text-left">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('srm.contractNumber')}</th>
            <th class="py-1.5 pr-3">{t('p3m.title')}</th>
            <th class="py-1.5 pr-3">{t('srm.supplier')}</th>
            <th class="py-1.5 pr-3">{t('srm.totalValue')}</th>
            <th class="py-1.5 pr-3">{t('p3m.committedSpend')}</th>
            <th class="py-1.5 pr-3">{t('p3m.actualSpend')}</th>
          </tr>
        </thead>
        <tbody>
          {#each rollup.contracts as c (c.id)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="py-1.5 pr-3">{c.contract_number}</td>
              <td class="py-1.5 pr-3">{c.title}</td>
              <td class="py-1.5 pr-3">{c.supplier_name || ''}</td>
              <td class="py-1.5 pr-3">{formatMoney(c.total_value)} {c.currency || ''}</td>
              <td class="py-1.5 pr-3">{formatMoney(c.committed_spend)}</td>
              <td class="py-1.5 pr-3">{formatMoney(c.actual_spend)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <button type="button" class="mt-3 flex items-center gap-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={onClose}>
      <IconX size={14} /> {t('p3m.close')}
    </button>
  </div>
{/if}
