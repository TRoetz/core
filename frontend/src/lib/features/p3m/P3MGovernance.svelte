<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { getUsers } from '../../api/users.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import P3MArchitectureReviews from './P3MArchitectureReviews.svelte';
  import { formatMoney, formatDate, stageLabel } from './utils.js';

  let { portfolios = [], onChanged = () => {} } = $props();

  let gates = $state([]);
  let thresholds = $state([]);
  let approvals = $state([]);
  let raciAssignments = $state([]);
  let loading = $state(true);
  let error = $state('');

  // --- Search & filter -----------------------------------------------------
  /** all | project | programme | portfolio */
  let approvalLevelFilter = $state('all');
  /** all | pending | approved | rejected */
  let approvalStatusFilter = $state('all');

  const APPROVAL_LEVELS = ['project', 'programme', 'portfolio'];
  const APPROVAL_STATUSES = ['pending', 'approved', 'rejected'];

  const filteredApprovals = $derived.by(() => {
    return approvals.filter((a) => {
      if (approvalLevelFilter !== 'all' && a.routed_to_level !== approvalLevelFilter) return false;
      if (approvalStatusFilter !== 'all' && a.status !== approvalStatusFilter) return false;
      return true;
    });
  });

  const expandedGates = $state({});

  /** Flat project list (id -> title) for pickers; carries current_stage so the
   * gate form can exclude it from the "to stage" options. */
  const allProjects = $derived.by(() => {
    const out = [];
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pg.title} › ${pr.title}`, current_stage: pr.current_stage });
      }
      for (const pr of pf.standalone_projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pr.title}`, current_stage: pr.current_stage });
    }
    return out;
  });

  const allProgrammes = $derived.by(() =>
    portfolios.flatMap((pf) => (pf.programmes ?? []).map((pg) => ({ id: pg.id, title: `${pf.title} › ${pg.title}` })))
  );

  let raciEntityType = $state('project');
  let raciEntityId = $state(null);

  const projectTitleById = $derived(new Map(allProjects.map((p) => [p.id, p.title])));

  async function load() {
    loading = true;
    error = '';
    try {
      const [g, th, a] = await Promise.all([
        p3mApi.getStageGates(),
        p3mApi.getAuthorizationThresholds(),
        p3mApi.getBudgetApprovals(),
      ]);
      gates = g ?? [];
      thresholds = th ?? [];
      approvals = a ?? [];
      thresholdDrafts = Object.fromEntries(thresholds.map((x) => [x.level, String(x.threshold_amount)]));
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadRaci() {
    if (!raciEntityId) return;
    try {
      const rows = await p3mApi.getRaciMatrix({ entity_type: raciEntityType, entity_id: raciEntityId });
      raciAssignments = rows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function onRaciTypeChange() {
    raciEntityId = null;
    raciAssignments = [];
  }

  // --- Users (RACI picker + display names) ---------------------------------
  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);
  const userNameById = $derived(new Map(users.map((u) => [u.id, u.display_name || u.username])));

  // --- Stage gate creation ---------------------------------------------------
  const STAGES = ['initiation', 'planning', 'execution', 'closure'];
  let gateFormOpen = $state(false);
  let gateForm = $state(emptyGateForm());
  let savingGate = $state(false);
  let gateError = $state('');

  function emptyGateForm() {
    return { project_id: '', to_stage: '', comments: '', artifacts: [{ name: '', required: true, link_or_note: '' }] };
  }

  /** Target-stage options exclude the selected project's current stage. */
  const toStageOptions = $derived.by(() => {
    if (!gateForm.project_id) return STAGES;
    const p = allProjects.find((x) => String(x.id) === String(gateForm.project_id));
    return STAGES.filter((s) => s !== (p?.current_stage ?? ''));
  });

  function toggleGateForm() {
    gateFormOpen = !gateFormOpen;
    if (gateFormOpen) {
      gateForm = emptyGateForm();
      gateError = '';
    }
  }

  async function submitGate() {
    if (savingGate) return;
    gateError = '';
    if (!gateForm.project_id) {
      gateError = t('p3m.projectRequired');
      return;
    }
    if (!gateForm.to_stage) {
      gateError = t('p3m.stageRequired');
      return;
    }
    savingGate = true;
    try {
      await p3mApi.createStageGate({
        project_id: Number(gateForm.project_id),
        to_stage: gateForm.to_stage,
        comments: gateForm.comments,
        artifacts: gateForm.artifacts
          .filter((a) => a.name.trim())
          .map((a) => ({ name: a.name.trim(), required: !!a.required, link_or_note: a.link_or_note })),
      });
      gateFormOpen = false;
      await load();
    } catch (e) {
      gateError = e?.message || t('p3m.createFailed');
    } finally {
      savingGate = false;
    }
  }

  async function decideGate(gate, passed) {
    gateError = '';
    try {
      await p3mApi.decideStageGate(gate.id, { passed });
      await load();
      onChanged(); // a passing decision advances the project's stage
    } catch (e) {
      gateError = e?.message || t('p3m.updateFailed');
    }
  }

  /** Patch one artifact in place after a server round-trip. */
  function patchArtifact(updated) {
    gates = gates.map((g) => ({ ...g, artifacts: (g.artifacts ?? []).map((a) => (a.id === updated.id ? updated : a)) }));
  }

  async function toggleArtifact(art) {
    try {
      patchArtifact(await p3mApi.updateGateArtifact(art.id, { completed: !art.completed, link_or_note: art.link_or_note ?? '' }));
    } catch (e) {
      gateError = e?.message || t('p3m.updateFailed');
    }
  }

  async function saveArtifactNote(art, note) {
    try {
      patchArtifact(await p3mApi.updateGateArtifact(art.id, { completed: art.completed, link_or_note: note }));
    } catch (e) {
      gateError = e?.message || t('p3m.updateFailed');
    }
  }

  // --- RACI assignments --------------------------------------------------------
  const RACI_ROLES = ['responsible', 'accountable', 'consulted', 'informed'];
  let raciForm = $state({ user_id: '', role: 'responsible' });
  let savingRaci = $state(false);

  async function addRaciAssignment() {
    if (savingRaci || !raciEntityId) return;
    error = '';
    if (!raciForm.user_id) {
      error = t('p3m.userRequired');
      return;
    }
    savingRaci = true;
    try {
      await p3mApi.upsertRaciAssignment({
        entity_type: raciEntityType,
        entity_id: Number(raciEntityId),
        user_id: Number(raciForm.user_id),
        role: raciForm.role,
      });
      await loadRaci();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingRaci = false;
    }
  }

  async function removeRaciAssignment(a) {
    try {
      await p3mApi.deleteRaciAssignment(a.id);
      await loadRaci();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    }
  }

  // --- Authorization thresholds --------------------------------------------------
  /** level -> draft amount string for the editable threshold cards. */
  let thresholdDrafts = $state({});
  let savingThresholdLevel = $state(null);

  async function saveThreshold(level) {
    if (savingThresholdLevel) return;
    error = '';
    const amount = Number(thresholdDrafts[level]);
    if (!Number.isFinite(amount) || amount < 0) {
      error = t('p3m.invalidAmount');
      return;
    }
    savingThresholdLevel = level;
    try {
      await p3mApi.updateAuthorizationThreshold({ level, threshold_amount: amount });
      await load();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingThresholdLevel = null;
    }
  }

  // --- Budget approvals -------------------------------------------------------------
  let approvalFormOpen = $state(false);
  let approvalForm = $state({ project_id: '', requested_amount: '' });
  let savingApproval = $state(false);
  /** approval id -> decision notes draft. */
  const approvalNotes = $state({});

  function toggleApprovalForm() {
    approvalFormOpen = !approvalFormOpen;
    if (approvalFormOpen) error = '';
  }

  async function submitBudgetRequest() {
    if (savingApproval) return;
    error = '';
    if (!approvalForm.project_id) {
      error = t('p3m.projectRequired');
      return;
    }
    const amount = Number(approvalForm.requested_amount);
    if (!Number.isFinite(amount)) {
      error = t('p3m.invalidAmount');
      return;
    }
    savingApproval = true;
    try {
      await p3mApi.createBudgetApproval({ project_id: Number(approvalForm.project_id), requested_amount: amount });
      approvalFormOpen = false;
      approvalForm = { project_id: '', requested_amount: '' };
      await load();
      onChanged(); // within-threshold requests apply the budget immediately
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      savingApproval = false;
    }
  }

  async function decideBudgetApproval(a, passed) {
    try {
      await p3mApi.decideBudgetApproval(a.id, { passed, notes: approvalNotes[a.id] ?? '' });
      delete approvalNotes[a.id];
      await load();
      onChanged(); // an approved request applies the new budget to the project
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
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
{:else if error && gates.length === 0 && approvals.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- Stage gates -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.stageGates')}</h2>
        {#if allProjects.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleGateForm}>
            <IconPlus size={14} class="inline" /> {t('p3m.newGate')}
          </button>
        {/if}
      </div>

      {#if gateError}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{gateError}</div>
      {/if}

      {#if gateFormOpen && allProjects.length > 0}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
            <label class="block">
              <span>{t('p3m.projects')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={gateForm.project_id}
                disabled={savingGate}
                onchange={(e) => (gateForm = { ...gateForm, project_id: e.target.value, to_stage: '' })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each allProjects as p (p.id)}
                  <option value={p.id}>{p.title}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.toStage')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={gateForm.to_stage}
                disabled={savingGate || !gateForm.project_id}
                onchange={(e) => (gateForm = { ...gateForm, to_stage: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each toStageOptions as s (s)}
                  <option value={s}>{stageLabel(s)}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.decisionComments')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={gateForm.comments}
                disabled={savingGate}
                oninput={(e) => (gateForm = { ...gateForm, comments: e.target.value })}
              />
            </label>
          </div>

          <div class="mt-2">
            <span>{t('p3m.artifacts')}</span>
            {#each gateForm.artifacts as art, i (i)}
              <div class="mt-1 flex items-center gap-2">
                <input
                  class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  placeholder={t('p3m.artifactName')}
                  value={art.name}
                  disabled={savingGate}
                  oninput={(e) => (gateForm = { ...gateForm, artifacts: gateForm.artifacts.map((a, j) => (j === i ? { ...a, name: e.target.value } : a)) })}
                />
                <label class="flex shrink-0 items-center gap-1 whitespace-nowrap">
                  <input type="checkbox" checked={art.required} disabled={savingGate} onchange={(e) => (gateForm = { ...gateForm, artifacts: gateForm.artifacts.map((a, j) => (j === i ? { ...a, required: e.target.checked } : a)) })} />
                  {t('p3m.required')}
                </label>
                <input
                  class="w-40 shrink-0 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  placeholder={t('p3m.linkOrNote')}
                  value={art.link_or_note}
                  disabled={savingGate}
                  oninput={(e) => (gateForm = { ...gateForm, artifacts: gateForm.artifacts.map((a, j) => (j === i ? { ...a, link_or_note: e.target.value } : a)) })}
                />
                <button type="button" class="shrink-0 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600 disabled:opacity-40" title={t('p3m.delete')} disabled={savingGate || gateForm.artifacts.length === 1} onclick={() => (gateForm = { ...gateForm, artifacts: gateForm.artifacts.filter((_, j) => j !== i) })}>
                  <IconX size={14} />
                </button>
              </div>
            {/each}
            <button type="button" class="mt-2 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" disabled={savingGate} onclick={() => (gateForm = { ...gateForm, artifacts: [...gateForm.artifacts, { name: '', required: true, link_or_note: '' }] })}>
              + {t('p3m.addArtifact')}
            </button>
          </div>

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingGate} onclick={submitGate}>
              {t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleGateForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      {#if gates.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3"></th>
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.fromStage')} → {t('p3m.toStage')}</th>
              <th class="py-1.5 pr-3">{t('p3m.ragStatus')}</th>
              <th class="py-1.5 pr-3">{t('p3m.recordedDate')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each gates as gate (gate.id)}
              {@const open = expandedGates[gate.id] === true}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">
                  <button type="button" class="cursor-pointer" onclick={() => (expandedGates[gate.id] = !open)} aria-label={t('p3m.artifacts')}>
                    <IconChevronRight size={14} class={open ? 'rotated' : ''} />
                  </button>
                </td>
                <td class="py-1.5 pr-3">{projectTitleById.get(gate.project_id) ?? `#${gate.project_id}`}</td>
                <td class="py-1.5 pr-3">{stageLabel(gate.from_stage)} → {stageLabel(gate.to_stage)}</td>
                <td class="py-1.5 pr-3">
                  {#if gate.passed}
                    <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('p3m.passed')}</span>
                  {:else}
                    <span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('p3m.decisionPending')}</span>
                  {/if}
                </td>
                <td class="py-1.5 pr-3">{formatDate(gate.review_date) || t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3">
                  {#if !gate.passed}
                    <div class="flex items-center gap-2 whitespace-nowrap">
                      <button type="button" class="rounded-md bg-emerald-600 px-2 py-0.5 font-medium text-white hover:bg-emerald-700" onclick={() => decideGate(gate, true)}>
                        {t('p3m.passGate')}
                      </button>
                      <button type="button" class="rounded-md border border-red-300 px-2 py-0.5 font-medium text-red-600 hover:bg-red-50" onclick={() => decideGate(gate, false)}>
                        {t('p3m.rejectGate')}
                      </button>
                    </div>
                  {/if}
                </td>
              </tr>
              {#if open && gate.artifacts?.length}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td></td>
                  <td colspan="5" class="py-1.5 pr-3 pl-8">
                    {#each gate.artifacts as art (art.id)}
                      <div class="flex items-center gap-2 py-0.5">
                        <input type="checkbox" checked={art.completed} disabled={gate.passed} onchange={() => toggleArtifact(art)} />
                        <span>{art.name}</span>
                        {#if art.required}
                          <span class="rounded bg-slate-100 px-1 text-[10px] text-slate-500">{t('p3m.required')}</span>
                        {/if}
                        <input
                          class="ml-auto w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                          placeholder={t('p3m.linkOrNote')}
                          value={art.link_or_note ?? ''}
                          disabled={gate.passed}
                          onchange={(e) => saveArtifactNote(art, e.target.value)}
                        />
                      </div>
                    {/each}
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Architecture review board -->
    <P3MArchitectureReviews portfolios={portfolios} />

    <!-- RACI matrix -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.raciMatrix')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <select
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          value={raciEntityType}
          onchange={(e) => {
            raciEntityType = e.target.value;
            onRaciTypeChange();
          }}
        >
          <option value="portfolio">{t('p3m.portfolios')}</option>
          <option value="programme">{t('p3m.programmes')}</option>
          <option value="project">{t('p3m.projects')}</option>
        </select>
        {#if raciEntityType === 'project'}
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciEntityId ?? ''} onchange={(e) => (raciEntityId = e.target.value ? Number(e.target.value) : null)}>
            <option value="">—</option>
            {#each allProjects as p (p.id)}
              <option value={p.id}>{p.title}</option>
            {/each}
          </select>
        {:else if raciEntityType === 'programme'}
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciEntityId ?? ''} onchange={(e) => (raciEntityId = e.target.value ? Number(e.target.value) : null)}>
            <option value="">—</option>
            {#each allProgrammes as p (p.id)}
              <option value={p.id}>{p.title}</option>
            {/each}
          </select>
        {:else}
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciEntityId ?? ''} onchange={(e) => (raciEntityId = e.target.value ? Number(e.target.value) : null)}>
            <option value="">—</option>
            {#each portfolios as pf (pf.id)}
              <option value={pf.id}>{pf.title}</option>
            {/each}
          </select>
        {/if}
        {#if raciEntityId}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={loadRaci}>
            {t('p3m.raciMatrix')}
          </button>
        {/if}
      </div>

      {#if raciEntityId && users.length > 0}
        <div class="mb-2 flex items-center gap-2">
          <select
            class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
            value={raciForm.user_id}
            disabled={savingRaci}
            onchange={(e) => (raciForm = { ...raciForm, user_id: e.target.value })}
          >
            <option value="">{t('p3m.noSelection')}</option>
            {#each users as u (u.id)}
              <option value={u.id}>{u.display_name || u.username}</option>
            {/each}
          </select>
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciForm.role} disabled={savingRaci} onchange={(e) => (raciForm = { ...raciForm, role: e.target.value })}>
            {#each RACI_ROLES as r (r)}
              <option value={r}>{r}</option>
            {/each}
          </select>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered disabled:opacity-40" disabled={savingRaci || !raciForm.user_id} onclick={addRaciAssignment}>
            + {t('p3m.add')}
          </button>
        </div>
      {/if}

      {#if raciAssignments.length > 0}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.owner')}</th>
              <th class="py-1.5 pr-3">{t('p3m.roleTitle')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each raciAssignments as a (a.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{userNameById.get(a.user_id) ?? `${t('p3m.owner')} #${a.user_id}`}</td>
                <td class="py-1.5 pr-3 uppercase">{a.role}</td>
                <td class="py-1.5 pr-3">
                  <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => removeRaciAssignment(a)}>
                    <IconX size={14} />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else if raciEntityId}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {/if}
    </section>

    <!-- Authorization thresholds -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.authorizationThresholds')}</h2>
      <div class="grid grid-cols-3 gap-3">
        {#each ['project', 'programme', 'portfolio'] as level (level)}
          {@const row = thresholds.find((x) => x.level === level)}
          <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs">
            <div class="text-[var(--color-ds-text-muted,#6b7280)] capitalize">{level}</div>
            <div class="mt-1 flex items-center gap-2">
              <input
                type="number"
                min="0"
                step="any"
                class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-base font-semibold"
                value={thresholdDrafts[level] ?? ''}
                placeholder={row ? formatMoney(row.threshold_amount) : t('p3m.notSet')}
                disabled={savingThresholdLevel !== null && savingThresholdLevel !== level}
                oninput={(e) => (thresholdDrafts = { ...thresholdDrafts, [level]: e.target.value })}
              />
              <button type="button" class="shrink-0 rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-2 py-1 font-medium text-white disabled:opacity-40" disabled={savingThresholdLevel === level || thresholdDrafts[level] == null} onclick={() => saveThreshold(level)}>
                {t('p3m.save')}
              </button>
            </div>
          </div>
        {/each}
      </div>
    </section>

    <!-- Budget approvals -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.budgetApprovals')}</h2>
        {#if allProjects.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleApprovalForm}>
            <IconPlus size={14} class="inline" /> {t('p3m.newBudgetRequest')}
          </button>
        {/if}
      </div>

      <!-- Approval filters: level + status -->
      <div class="mb-2 flex flex-wrap items-center gap-2 text-xs">
        <select
          value={approvalLevelFilter}
          onchange={(e) => (approvalLevelFilter = e.target.value)}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        >
          <option value="all">{t('p3m.allLevels')}</option>
          {#each APPROVAL_LEVELS as lv (lv)}
            <option value={lv}>{lv[0].toUpperCase() + lv.slice(1)}</option>
          {/each}
        </select>
        <select
          value={approvalStatusFilter}
          onchange={(e) => (approvalStatusFilter = e.target.value)}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        >
          <option value="all">{t('p3m.allStatuses')}</option>
          {#each APPROVAL_STATUSES as st (st)}
            <option value={st}>{st === 'pending' ? t('p3m.decisionPending') : st === 'approved' ? t('p3m.passed') : t('p3m.rejected')}</option>
          {/each}
        </select>
      </div>

      {#if approvalFormOpen && allProjects.length > 0}
        <div class="mb-3 flex items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <label class="block min-w-64 flex-1">
            <span>{t('p3m.projects')} *</span>
            <select
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={approvalForm.project_id}
              disabled={savingApproval}
              onchange={(e) => (approvalForm = { ...approvalForm, project_id: e.target.value })}
            >
              <option value="">{t('p3m.noSelection')}</option>
              {#each allProjects as p (p.id)}
                <option value={p.id}>{p.title}</option>
              {/each}
            </select>
          </label>
          <label class="block w-40">
            <span>{t('p3m.requestedAmount')} *</span>
            <input
              type="number"
              min="0"
              step="any"
              class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
              value={approvalForm.requested_amount}
              disabled={savingApproval}
              oninput={(e) => (approvalForm = { ...approvalForm, requested_amount: e.target.value })}
            />
          </label>
          <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingApproval || !approvalForm.project_id} onclick={submitBudgetRequest}>
            {t('p3m.create')}
          </button>
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={toggleApprovalForm}>
            {t('p3m.cancel')}
          </button>
        </div>
      {/if}

      {#if approvals.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else if filteredApprovals.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noResults')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
              <th class="py-1.5 pr-3">{t('p3m.currentBudget')}</th>
              <th class="py-1.5 pr-3">{t('p3m.requestedAmount')}</th>
              <th class="py-1.5 pr-3">{t('p3m.routedToLevel')}</th>
              <th class="py-1.5 pr-3">Status</th>
            </tr>
          </thead>
          <tbody>
            {#each filteredApprovals as a (a.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{projectTitleById.get(a.project_id) ?? `#${a.project_id}`}</td>
                <td class="py-1.5 pr-3">{formatMoney(a.current_budget)}</td>
                <td class="py-1.5 pr-3">{formatMoney(a.requested_amount)}</td>
                <td class="py-1.5 pr-3 capitalize">{a.routed_to_level}</td>
                <td class="py-1.5 pr-3">
                  {#if a.status === 'approved'}
                    <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('p3m.passed')}</span>
                  {:else if a.status === 'rejected'}
                    <span class="rounded-full bg-red-100 px-2 py-0.5 text-red-700">{t('p3m.rejected')}</span>
                  {:else}
                    <div class="flex flex-col gap-1">
                      <span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('p3m.decisionPending')}</span>
                      <input
                        class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                        placeholder={t('p3m.notes')}
                        value={approvalNotes[a.id] ?? ''}
                        oninput={(e) => (approvalNotes[a.id] = e.target.value)}
                      />
                      <div class="flex items-center gap-2">
                        <button type="button" class="rounded-md bg-emerald-600 px-2 py-0.5 font-medium text-white hover:bg-emerald-700" onclick={() => decideBudgetApproval(a, true)}>
                          {t('p3m.approve')}
                        </button>
                        <button type="button" class="rounded-md border border-red-300 px-2 py-0.5 font-medium text-red-600 hover:bg-red-50" onclick={() => decideBudgetApproval(a, false)}>
                          {t('p3m.reject')}
                        </button>
                      </div>
                    </div>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}

<style>
  .rotated {
    transform: rotate(90deg);
  }
</style>
