<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import { formatMoney, formatDate, stageLabel } from './utils.js';

  let { portfolios = [] } = $props();

  let gates = $state([]);
  let thresholds = $state([]);
  let approvals = $state([]);
  let raciAssignments = $state([]);
  let loading = $state(true);
  let error = $state('');

  const expandedGates = $state({});

  /** Flat project list (id -> title) for the RACI entity picker. */
  const allProjects = $derived.by(() => {
    const out = [];
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pg.title} › ${pr.title}` });
      }
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

  onMount(load);
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
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.stageGates')}</h2>
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
              </tr>
              {#if open && gate.artifacts?.length}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td></td>
                  <td colspan="4" class="py-1.5 pr-3 pl-8">
                    {#each gate.artifacts as art (art.id)}
                      <div class="flex items-center gap-2 py-0.5">
                        <span class={art.completed ? 'text-emerald-600' : 'text-[var(--color-ds-text-muted,#6b7280)]'}>
                          {art.completed ? '✓' : '○'}
                        </span>
                        <span>{art.name}</span>
                        {#if art.required}
                          <span class="rounded bg-slate-100 px-1 text-[10px] text-slate-500">{t('p3m.required')}</span>
                        {/if}
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
      {#if raciAssignments.length > 0}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.owner')}</th>
              <th class="py-1.5 pr-3">{t('p3m.roleTitle')}</th>
            </tr>
          </thead>
          <tbody>
            {#each raciAssignments as a (a.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{t('p3m.owner')} #{a.user_id}</td>
                <td class="py-1.5 pr-3 uppercase">{a.role}</td>
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
            <div class="mt-1 text-base font-semibold">{row ? formatMoney(row.threshold_amount) : t('p3m.notSet')}</div>
          </div>
        {/each}
      </div>
    </section>

    <!-- Budget approvals -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.budgetApprovals')}</h2>
      {#if approvals.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
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
            {#each approvals as a (a.id)}
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
                    <span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('p3m.decisionPending')}</span>
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
