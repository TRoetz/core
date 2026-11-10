<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { time } from '../../api/time.js';
  import { workspaces as wsApi } from '../../api/workspaces.js';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import P3MCriticalPath from './P3MCriticalPath.svelte';
  import P3MProjectRegisters from './P3MProjectRegisters.svelte';
  import P3MChangeAssessments from './P3MChangeAssessments.svelte';
  import P3MFinanceAlerts from './P3MFinanceAlerts.svelte';
  import P3MClosureRecord from './P3MClosureRecord.svelte';
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

  /** @type {Array<{id:number,label:string,snapshot:Record<string,any>,created_at:string}>} */
  let baselines = $state([]);
  let baselineLabel = $state('');
  let savingBaseline = $state(false);

  /** @type {{stage_gates?: any[], budget_approvals?: any[], architecture_reviews?: any[], communications?: any[]} | null} */
  let activity = $state(null);

  /** Merged, newest-first event timeline across the four activity sources. */
  const activityFeed = $derived.by(() => {
    if (!activity) return [];
    const items = [];
    for (const g of activity.stage_gates ?? []) items.push({ at: String(g.created_at), type: 'stage_gate', data: g });
    for (const b of activity.budget_approvals ?? []) items.push({ at: String(b.updated_at), type: 'budget_approval', data: b });
    for (const a of activity.architecture_reviews ?? []) items.push({ at: String(a.decided_at || a.created_at), type: 'architecture_review', data: a });
    for (const c of activity.communications ?? []) items.push({ at: String(c.date), type: 'communication', data: c });
    return items.sort((x, y) => y.at.localeCompare(x.at));
  });

  const arbStatusLabel = { wip: () => t('p3m.arbWip'), in_review: () => t('p3m.arbInReview'), approved: () => t('p3m.arbApproved'), needs_work: () => t('p3m.arbNeedsWork') };

  const timeProjectName = $derived(rollup?.time_project?.name ?? '');

  async function load() {
    if (!project) return;
    loadingRollup = true;
    error = '';
    selectedTimeProjectId = project.time_project_id ? String(project.time_project_id) : '';
    checkedWorkspaceIds = new Set();
    linksSaved = false;
    try {
      const [r, tps, wss, bls, act] = await Promise.all([
        p3mApi.getProjectRollup(project.id),
        time.projects.getAll().catch(() => []),
        wsApi.getAll().catch(() => []),
        p3mApi.getProjectBaselines(project.id).catch(() => []),
        p3mApi.getProjectActivity(project.id).catch(() => null),
      ]);
      rollup = r ?? null;
      timeProjects = Array.isArray(tps) ? tps : [];
      allWorkspaces = Array.isArray(wss) ? wss : [];
      checkedWorkspaceIds = new Set((r?.workspaces ?? []).map((w) => w.id));
      baselines = Array.isArray(bls) ? bls : [];
      activity = act && typeof act === 'object' ? act : null;
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loadingRollup = false;
    }
  }

  async function takeBaseline() {
    if (!project || savingBaseline) return;
    const label = baselineLabel.trim();
    if (!label) {
      error = t('p3m.labelRequired');
      return;
    }
    savingBaseline = true;
    error = '';
    try {
      await p3mApi.createProjectBaseline(project.id, { label });
      baselineLabel = '';
      const bls = await p3mApi.getProjectBaselines(project.id);
      baselines = Array.isArray(bls) ? bls : [];
      onChanged();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingBaseline = false;
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

      {#if rollup.financials?.synced}
        <div class="mt-2 max-w-md rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3">
          <h4 class="mb-1 text-xs font-semibold">{t('p3m.syncedFinancials')}</h4>
          <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-xs md:grid-cols-3">
            {#if rollup.financials.synced.actual_total != null}
              <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.syncedActual')}:</span> {formatMoney(rollup.financials.synced.actual_total)}</div>
            {/if}
            {#if rollup.financials.synced.commitment_total != null}
              <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.syncedCommitment')}:</span> {formatMoney(rollup.financials.synced.commitment_total)}</div>
            {/if}
            {#if rollup.financials.synced.forecast_total != null}
              <div><span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.syncedForecast')}:</span> {formatMoney(rollup.financials.synced.forecast_total)}</div>
            {/if}
          </div>
          <div class="mt-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
            {t('p3m.lastSyncedPeriod')}: {rollup.financials.synced.last_synced_period}
          </div>
        </div>
      {:else if rollup}
        <div class="mt-2 max-w-md text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noSyncedFinancials')}</div>
      {/if}
    {/if}

    <!-- Baselines: immutable snapshots of budget/schedule at a point in time -->
    <h3 class="mb-1 mt-4 font-semibold">{t('p3m.baselines')}</h3>
    <div class="flex items-end gap-2">
      <label class="block w-56">
        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.baselineLabel')}</span>
        <input
          type="text"
          class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
          placeholder={t('p3m.baselineLabel')}
          value={baselineLabel}
          disabled={savingBaseline}
          oninput={(e) => (baselineLabel = e.target.value)}
        />
      </label>
      <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingBaseline} onclick={takeBaseline}>
        {t('p3m.takeBaseline')}
      </button>
    </div>
    {#if baselines.length === 0}
      <div class="mt-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noBaselines')}</div>
    {:else}
      <table class="mt-2 w-full max-w-lg text-left">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('p3m.baselineLabel')}</th>
            <th class="py-1.5 pr-3">{t('p3m.budgetApproved')}</th>
            <th class="py-1.5 pr-3">{t('p3m.progressPercent')}</th>
            <th class="py-1.5 pr-3">{t('p3m.capturedAt')}</th>
          </tr>
        </thead>
        <tbody>
          {#each baselines as b (b.id)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="py-1.5 pr-3">{b.label}</td>
              <td class="py-1.5 pr-3">{formatMoney(b.snapshot?.budget_approved ?? 0)}</td>
              <td class="py-1.5 pr-3">{b.snapshot?.progress_percent ?? 0}%</td>
              <td class="py-1.5 pr-3">{formatDate(b.created_at) || t('p3m.notSet')}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <!-- Critical path: CPM over the dated work items in linked workspaces -->
    <h3 class="mb-1 mt-4 font-semibold">{t('p3m.criticalPath')}</h3>
    <P3MCriticalPath entityType="project" id={project.id} />

    <!-- Per-project risk and decision registers (feed the CPM badges above) -->
    <h3 class="mb-1 mt-4 font-semibold">{t('p3m.registers')}</h3>
    <P3MProjectRegisters project={project} />

    <!-- Recent events: stage gates, budget approvals, architecture reviews, communications -->
    <h3 class="mb-1 mt-4 font-semibold">{t('p3m.activityTitle')}</h3>
    {#if !activity}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.loading')}</div>
    {:else if activityFeed.length === 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
    {:else}
      <ul class="mt-1 divide-y divide-[var(--color-ds-border,#e5e7eb)] rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
        {#each activityFeed as item (item.at + '|' + item.type + JSON.stringify(item.data))}
          <li class="flex items-start gap-3 p-2">
            <span class="w-24 shrink-0 tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(item.at)}</span>
            {#if item.type === 'stage_gate'}
              <span class="shrink-0 rounded bg-blue-50 px-1.5 py-0.5 font-medium text-blue-700">{t('p3m.activityStageGate')}</span>
              <span>{stageLabel(item.data.from_stage)} → {stageLabel(item.data.to_stage)}{item.data.reviewer_name ? ` — ${item.data.reviewer_name}` : ''}</span>
              <span class="ml-auto shrink-0 rounded px-1.5 py-0.5 font-medium {item.data.passed ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}">
                {t(item.data.passed ? 'p3m.passed' : 'p3m.activityGateNotPassed')}
              </span>
            {:else if item.type === 'budget_approval'}
              <span class="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 font-medium text-amber-700">{t('p3m.activityBudgetRequest')}</span>
              <span>{formatMoney(item.data.requested_amount)}{item.data.decision_notes ? ` — ${item.data.decision_notes}` : ''}</span>
              <span class="ml-auto shrink-0 rounded px-1.5 py-0.5 font-medium {item.data.status === 'approved' ? 'bg-green-50 text-green-700' : item.data.status === 'rejected' ? 'bg-red-50 text-red-700' : 'bg-gray-100 text-gray-600'}">
                {t(item.data.status === 'pending' ? 'p3m.decisionPending' : item.data.status === 'approved' ? 'p3m.passed' : 'p3m.rejected')}
              </span>
            {:else if item.type === 'architecture_review'}
              <span class="shrink-0 rounded bg-purple-50 px-1.5 py-0.5 font-medium text-purple-700">{t('p3m.activityArchReview')}</span>
              <span>{item.data.title}</span>
              <span class="ml-auto shrink-0 rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-600">
                {(arbStatusLabel[item.data.status] ?? (() => item.data.status))()}
              </span>
            {:else}
              <span class="shrink-0 rounded bg-cyan-50 px-1.5 py-0.5 font-medium text-cyan-700">{t('p3m.activityCommunication')}</span>
              <span>{item.data.audience}{item.data.channel ? ` · ${item.data.channel}` : ''}: {item.data.summary}</span>
            {/if}
          </li>
        {/each}
      </ul>
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

    <!-- Change impact + readiness gates delivery start (CHG-002/006) -->
    <P3MChangeAssessments project={project} />

    <!-- Financial exception alert rules for this project (FIN-009) -->
    <P3MFinanceAlerts project={project} />

    <!-- Closure & handover record gates the close stage (CLS-001) -->
    <P3MClosureRecord project={project} />

    <button type="button" class="mt-3 flex items-center gap-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={onClose}>
      <IconX size={14} /> {t('p3m.close')}
    </button>
  </div>
{/if}
