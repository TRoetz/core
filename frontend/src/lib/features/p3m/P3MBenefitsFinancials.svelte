<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { getUsers } from '../../api/users.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import IconDownload from '@tabler/icons-svelte-runes/icons/download';
  import { confirm } from '../../composables/useConfirm.js';
  import { formatMoney, formatDate, toISODate, toDateInputValue } from './utils.js';

  let { portfolios = [] } = $props();

  // Register export (CSV) via the server-side /p3m/export endpoint.
  function downloadExport(scope) {
    window.open(p3mApi.exportUrl(scope), '_blank', 'noopener');
  }

  let benefits = $state([]);
  let tranches = $state([]);
  let forecast = $state([]);
  let loading = $state(true);
  let error = $state('');

  // --- Search & filter -----------------------------------------------------
  /** all | financial | non_financial | strategic | compliance */
  let benefitTypeFilter = $state('all');
  /** all | capex | opex */
  let expenditureTypeFilter = $state('all');

  const filteredBenefits = $derived(
    benefitTypeFilter === 'all' ? benefits : benefits.filter((b) => b.benefit_type === benefitTypeFilter)
  );

  const filteredTranches = $derived(
    expenditureTypeFilter === 'all' ? tranches : tranches.filter((tr) => tr.expenditure_type === expenditureTypeFilter)
  );

  const expandedBenefits = $state({}); // benefit id -> realizations[] | undefined (loading)
  const realizationCache = $state({});
  /** benefit id -> measurements[] (loaded with the expansion). */
  const measurementCache = $state({});

  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);

  const entityTitleById = $derived.by(() => {
    const m = new Map();
    for (const pf of portfolios) {
      m.set(`portfolio:${pf.id}`, pf.title);
      for (const pg of pf.programmes ?? []) {
        m.set(`programme:${pg.id}`, `${pf.title} › ${pg.title}`);
        for (const pr of pg.projects ?? []) m.set(`project:${pr.id}`, `${pf.title} › ${pg.title} › ${pr.title}`);
      }
      for (const pr of pf.standalone_projects ?? []) m.set(`project:${pr.id}`, `${pf.title} › ${pr.title}`);
    }
    return m;
  });

  /** Projects and programmes as flat pickers (benefit parents, tranche entities). */
  const projectOptions = $derived.by(() => {
    const out = [];
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pg.title} › ${pr.title}` });
      }
      for (const pr of pf.standalone_projects ?? []) out.push({ id: pr.id, title: `${pf.title} › ${pr.title}` });
    }
    return out;
  });

  const programmeOptions = $derived.by(() =>
    portfolios.flatMap((pf) => (pf.programmes ?? []).map((pg) => ({ id: pg.id, title: `${pf.title} › ${pg.title}` })))
  );

  function entityLabel(b) {
    if (b.project_id != null) return entityTitleById.get(`project:${b.project_id}`) ?? `#${b.project_id}`;
    if (b.programme_id != null) return entityTitleById.get(`programme:${b.programme_id}`) ?? `#${b.programme_id}`;
    return '—';
  }

  function benefitProgress(b) {
    if (!b.target_value || b.latest_realized_value == null) return 0;
    const pct = (b.latest_realized_value / b.target_value) * 100;
    return Math.max(0, Math.min(100, pct));
  }

  /** Inline chip style for a benefit status value. */
  function benefitStatusStyle(status) {
    switch (status) {
      case 'realised':
        return 'background:#dcfce7;color:#166534';
      case 'measuring':
        return 'background:#dbeafe;color:#1e40af';
      case 'shortfall':
        return 'background:#fee2e2;color:#991b1b';
      case 'retired':
        return 'background:#f3f4f6;color:#6b7280';
      default: // planned
        return 'background:#fef9c3;color:#854d0e';
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [b, tr, f] = await Promise.all([p3mApi.benefits.getAll(), p3mApi.tranches.getAll(), p3mApi.getForecast()]);
      benefits = b ?? [];
      tranches = tr ?? [];
      forecast = f ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function toggleBenefit(b) {
    if (expandedBenefits[b.id] !== undefined) {
      expandedBenefits[b.id] = undefined;
      return;
    }
    expandedBenefits[b.id] = []; // mark loading
    try {
      const [rows, measRows] = await Promise.all([
        p3mApi.getBenefitRealizations(b.id),
        p3mApi.getBenefitMeasurements(b.id).catch(() => []),
      ]);
      realizationCache[b.id] = rows ?? [];
      measurementCache[b.id] = measRows ?? [];
      expandedBenefits[b.id] = realizationCache[b.id];
    } catch (e) {
      error = e?.message || 'error';
      expandedBenefits[b.id] = undefined;
    }
  }

  // --- Benefit create / edit ---------------------------------------------------
  const BENEFIT_TYPES = ['financial', 'non_financial', 'strategic', 'compliance'];
  const BENEFIT_STATUSES = ['planned', 'measuring', 'realised', 'shortfall', 'retired'];
  const MEASUREMENT_FREQUENCIES = ['monthly', 'quarterly', 'annual', 'one_off'];
  const EXPENDITURE_TYPES = ['capex', 'opex'];
  let benefitFormOpen = $state(false);
  /** Benefit being edited in place, or null when creating. */
  let editingBenefit = $state(null);
  let savingBenefit = $state(false);
  const emptyBenefitForm = () => ({
    title: '',
    benefit_type: 'financial',
    entity_kind: projectOptions.length > 0 ? 'project' : 'programme',
    project_id: '',
    programme_id: '',
    target_value: '',
    baseline_value: '',
    unit_of_measure: '',
    target_realization_date: '',
    owner_id: '',
    // Benefit profile (BEN-003/007/008).
    measurement_method: '',
    measurement_frequency: '',
    status: 'planned',
    evidence: '',
    finance_validator_id: '',
    capability_ref: '',
  });
  let benefitForm = $state(emptyBenefitForm());

  function openBenefitCreate() {
    editingBenefit = null;
    benefitForm = emptyBenefitForm();
    error = '';
    benefitFormOpen = true;
  }

  function openBenefitEdit(b) {
    editingBenefit = b;
    benefitForm = {
      title: b.title ?? '',
      benefit_type: b.benefit_type ?? 'financial',
      entity_kind: b.project_id != null ? 'project' : 'programme',
      project_id: b.project_id != null ? String(b.project_id) : '',
      programme_id: b.programme_id != null ? String(b.programme_id) : '',
      target_value: b.target_value != null ? String(b.target_value) : '',
      baseline_value: b.baseline_value != null ? String(b.baseline_value) : '',
      unit_of_measure: b.unit_of_measure ?? '',
      target_realization_date: toDateInputValue(b.target_realization_date),
      owner_id: b.owner_id != null ? String(b.owner_id) : '',
      measurement_method: b.measurement_method ?? '',
      measurement_frequency: b.measurement_frequency ?? '',
      status: b.status || 'planned',
      evidence: b.evidence ?? '',
      finance_validator_id: b.finance_validator_id != null ? String(b.finance_validator_id) : '',
      capability_ref: b.capability_ref ?? '',
    };
    error = '';
    benefitFormOpen = true;
  }

  function closeBenefitForm() {
    benefitFormOpen = false;
    editingBenefit = null;
  }

  async function submitBenefit() {
    if (savingBenefit) return;
    error = '';
    const parentKind = benefitForm.entity_kind;
    const parentId = Number(parentKind === 'project' ? benefitForm.project_id : benefitForm.programme_id);
    if (!benefitForm.title.trim()) {
      error = t('p3m.createFailed');
      return;
    }
    if (!parentId) {
      error = t('p3m.entityRequired');
      return;
    }
    const owner = Number(benefitForm.owner_id);
    if (!owner) {
      error = t('p3m.ownerRequired');
      return;
    }
    const date = toISODate(benefitForm.target_realization_date);
    if (!date) {
      error = t('p3m.createFailed');
      return;
    }
    savingBenefit = true;
    try {
      const payload = {
        project_id: parentKind === 'project' ? parentId : null,
        programme_id: parentKind === 'programme' ? parentId : null,
        title: benefitForm.title.trim(),
        benefit_type: benefitForm.benefit_type,
        target_value: Number(benefitForm.target_value) || 0,
        baseline_value: Number(benefitForm.baseline_value) || 0,
        unit_of_measure: benefitForm.unit_of_measure,
        target_realization_date: date,
        owner_id: owner,
        measurement_method: benefitForm.measurement_method.trim() || null,
        measurement_frequency: benefitForm.measurement_frequency || null,
        status: benefitForm.status || 'planned',
        evidence: benefitForm.evidence.trim() || null,
        finance_validator_id: benefitForm.finance_validator_id ? Number(benefitForm.finance_validator_id) : null,
        capability_ref: benefitForm.capability_ref.trim() || null,
      };
      if (editingBenefit) await p3mApi.benefits.update(editingBenefit.id, payload);
      else await p3mApi.benefits.create(payload);
      closeBenefitForm();
      await load();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingBenefit = false;
    }
  }

  async function deleteBenefit(b) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteBenefit') }))) return;
    try {
      await p3mApi.benefits.delete(b.id);
      await load();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  // --- Benefit realizations ------------------------------------------------------
  /** benefit id -> draft realization form. */
  const realizationForms = $state({});
  let savingRealizationId = $state(null);

  function realizationDraft(bid) {
    return realizationForms[bid] ?? { value: '', notes: '' };
  }

  async function recordRealization(b) {
    if (savingRealizationId === b.id) return;
    error = '';
    const draft = realizationDraft(b.id);
    const value = Number(draft.value);
    if (!Number.isFinite(value)) {
      error = t('p3m.invalidAmount');
      return;
    }
    savingRealizationId = b.id;
    try {
      await p3mApi.createBenefitRealization(b.id, {
        recorded_date: new Date().toISOString(),
        realized_value: value,
        notes: draft.notes,
      });
      realizationForms[b.id] = { value: '', notes: '' };
      const rows = (await p3mApi.getBenefitRealizations(b.id)) ?? [];
      realizationCache[b.id] = rows;
      expandedBenefits[b.id] = rows;
      await load(); // refresh latest_realized_value + progress bars
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      savingRealizationId = null;
    }
  }

  async function deleteRealization(b, r) {
    try {
      await p3mApi.deleteBenefitRealization(b.id, r.id);
      const rows = (await p3mApi.getBenefitRealizations(b.id)) ?? [];
      realizationCache[b.id] = rows;
      expandedBenefits[b.id] = rows;
      await load();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  // --- Benefit measurements (BEN-007/008) ------------------------------------
  /** benefit id -> draft "new measurement" form. */
  const measurementForms = $state({});
  let savingMeasurementBenefitId = $state(null);
  /** measurement row id -> completion value draft. */
  let completeDrafts = $state({});

  function measurementDraft(bid) {
    return measurementForms[bid] ?? { due_date: '', value: '', evidence: '' };
  }

  async function recordMeasurement(b) {
    const draft = measurementDraft(b.id);
    if (!draft.due_date || savingMeasurementBenefitId === b.id) return;
    savingMeasurementBenefitId = b.id;
    try {
      await p3mApi.createBenefitMeasurement(b.id, {
        due_date: toISODate(draft.due_date),
        value: draft.value !== '' ? Number(draft.value) : null,
        evidence: draft.evidence.trim() || null,
      });
      measurementForms[b.id] = { due_date: '', value: '', evidence: '' };
      const rows = (await p3mApi.getBenefitMeasurements(b.id)) ?? [];
      measurementCache[b.id] = rows;
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      savingMeasurementBenefitId = null;
    }
  }

  /** Complete an open measurement with its actual value (PUT sets completed_at). */
  async function completeMeasurement(b, m) {
    const draft = completeDrafts[m.id];
    if (draft === undefined || draft.trim() === '' || savingMeasurementBenefitId === b.id) return;
    savingMeasurementBenefitId = b.id;
    try {
      await p3mApi.updateBenefitMeasurement(m.id, { value: Number(draft) });
      delete completeDrafts[m.id];
      const rows = (await p3mApi.getBenefitMeasurements(b.id)) ?? [];
      measurementCache[b.id] = rows;
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingMeasurementBenefitId = null;
    }
  }

  // --- Tranche create / edit -------------------------------------------------------
  const TRANCHE_STATUSES = ['proposed', 'approved', 'released'];
  let trancheFormOpen = $state(false);
  /** Tranche being edited in place, or null when creating. */
  let editingTranche = $state(null);
  let savingTranche = $state(false);
  const emptyTrancheForm = () => ({
    entity_type: 'project',
    entity_id: '',
    title: '',
    amount: '',
    expenditure_type: 'capex',
    planned_date: '',
    status: 'proposed',
  });
  let trancheForm = $state(emptyTrancheForm());

  function openTrancheCreate() {
    editingTranche = null;
    trancheForm = emptyTrancheForm();
    error = '';
    trancheFormOpen = true;
  }

  function openTrancheEdit(tr) {
    editingTranche = tr;
    trancheForm = {
      entity_type: tr.entity_type ?? 'project',
      entity_id: String(tr.entity_id),
      title: tr.title ?? '',
      amount: tr.amount != null ? String(tr.amount) : '',
      expenditure_type: tr.expenditure_type ?? 'capex',
      planned_date: toDateInputValue(tr.planned_date),
      status: tr.status ?? 'proposed',
    };
    error = '';
    trancheFormOpen = true;
  }

  function closeTrancheForm() {
    trancheFormOpen = false;
    editingTranche = null;
  }

  /** Entity options for the selected tranche entity type. */
  const trancheEntityOptions = $derived.by(() => {
    if (trancheForm.entity_type === 'programme') return programmeOptions;
    if (trancheForm.entity_type === 'portfolio') return portfolios.map((pf) => ({ id: pf.id, title: pf.title }));
    return projectOptions;
  });

  async function submitTranche() {
    if (savingTranche) return;
    error = '';
    const entityId = Number(trancheForm.entity_id);
    if (!trancheForm.title.trim()) {
      error = t('p3m.createFailed');
      return;
    }
    if (!entityId) {
      error = t('p3m.entityRequired');
      return;
    }
    const date = toISODate(trancheForm.planned_date);
    if (!date) {
      error = t('p3m.createFailed');
      return;
    }
    savingTranche = true;
    try {
      const payload = {
        entity_type: trancheForm.entity_type,
        entity_id: entityId,
        title: trancheForm.title.trim(),
        amount: Number(trancheForm.amount) || 0,
        expenditure_type: trancheForm.expenditure_type,
        planned_date: date,
        status: trancheForm.status,
      };
      if (editingTranche) await p3mApi.tranches.update(editingTranche.id, payload);
      else await p3mApi.tranches.create(payload);
      closeTrancheForm();
      await load();
    } catch (e) {
      error = e?.message || t('p3m.updateFailed');
    } finally {
      savingTranche = false;
    }
  }

  async function deleteTranche(tr) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteTranche') }))) return;
    try {
      await p3mApi.tranches.delete(tr.id);
      await load();
    } catch (e) {
      error = e?.message || t('p3m.deleteFailed');
    }
  }

  const maxForecastValue = $derived(forecast.reduce((m, p) => Math.max(m, p.capex_total ?? 0, p.opex_total ?? 0), 0));

  onMount(() => {
    load();
    getUsers().then((u) => (users = u || [])).catch(() => {});
  });
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && benefits.length === 0 && tranches.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- Benefits -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.benefits')}</h2>
        <div class="flex items-center gap-2">
          {#if projectOptions.length > 0 || programmeOptions.length > 0}
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openBenefitCreate}>
              <IconPlus size={14} class="inline" /> {t('p3m.newBenefit')}
            </button>
          {/if}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => downloadExport('benefits')}>
            <IconDownload size={14} class="inline" /> {t('p3m.exportCsv')}
          </button>
        </div>
      </div>

      <!-- Benefit type filter -->
      <div class="mb-2 flex items-center gap-2 text-xs">
        <select
          value={benefitTypeFilter}
          onchange={(e) => (benefitTypeFilter = e.target.value)}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        >
          <option value="all">{t('p3m.allBenefitTypes')}</option>
          {#each BENEFIT_TYPES as bt (bt)}
            <option value={bt}>{t(`p3m.benefitType${bt.split('_').map((w) => w[0].toUpperCase() + w.slice(1)).join('')}`)}</option>
          {/each}
        </select>
      </div>

      {#if error && !benefitFormOpen && !trancheFormOpen}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
      {/if}

      {#if benefitFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
            <label class="block md:col-span-2">
              <span>{t('p3m.title')} *</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.title}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, title: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.benefitType')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.benefit_type}
                disabled={savingBenefit}
                onchange={(e) => (benefitForm = { ...benefitForm, benefit_type: e.target.value })}
              >
                {#each BENEFIT_TYPES as bt (bt)}
                  <option value={bt}>{bt}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.benefitEntity')} *</span>
              <div class="mt-0.5 flex gap-2">
                <select
                  class="w-28 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={benefitForm.entity_kind}
                  disabled={savingBenefit || (projectOptions.length === 0 && benefitForm.entity_kind === 'project') || (programmeOptions.length === 0 && benefitForm.entity_kind === 'programme')}
                  onchange={(e) => (benefitForm = { ...benefitForm, entity_kind: e.target.value, project_id: '', programme_id: '' })}
                >
                  {#if projectOptions.length > 0}
                    <option value="project">{t('p3m.projects')}</option>
                  {/if}
                  {#if programmeOptions.length > 0}
                    <option value="programme">{t('p3m.programmes')}</option>
                  {/if}
                </select>
                {#if benefitForm.entity_kind === 'project'}
                  <select class="min-w-0 flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={benefitForm.project_id} disabled={savingBenefit} onchange={(e) => (benefitForm = { ...benefitForm, project_id: e.target.value })}>
                    <option value="">{t('p3m.noSelection')}</option>
                    {#each projectOptions as p (p.id)}
                      <option value={p.id}>{p.title}</option>
                    {/each}
                  </select>
                {:else}
                  <select class="min-w-0 flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={benefitForm.programme_id} disabled={savingBenefit} onchange={(e) => (benefitForm = { ...benefitForm, programme_id: e.target.value })}>
                    <option value="">{t('p3m.noSelection')}</option>
                    {#each programmeOptions as p (p.id)}
                      <option value={p.id}>{p.title}</option>
                    {/each}
                  </select>
                {/if}
              </div>
            </label>

            <label class="block">
              <span>{t('p3m.owner')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.owner_id}
                disabled={savingBenefit}
                onchange={(e) => (benefitForm = { ...benefitForm, owner_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each users as u (u.id)}
                  <option value={u.id}>{u.display_name || u.username}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.targetValue')}</span>
              <input
                type="number"
                step="any"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.target_value}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, target_value: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.baselineValue')}</span>
              <input
                type="number"
                step="any"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.baseline_value}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, baseline_value: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.unitOfMeasure')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.unit_of_measure}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, unit_of_measure: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.targetRealizationDate')} *</span>
              <input
                type="date"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.target_realization_date}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, target_realization_date: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.measurementMethod')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.measurement_method}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, measurement_method: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.measurementFrequency')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.measurement_frequency}
                disabled={savingBenefit}
                onchange={(e) => (benefitForm = { ...benefitForm, measurement_frequency: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each MEASUREMENT_FREQUENCIES as mf (mf)}
                  <option value={mf}>{mf}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.benefitStatus')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.status}
                disabled={savingBenefit}
                onchange={(e) => (benefitForm = { ...benefitForm, status: e.target.value })}
              >
                {#each BENEFIT_STATUSES as bs (bs)}
                  <option value={bs}>{bs}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.evidence')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.evidence}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, evidence: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.financeValidator')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.finance_validator_id}
                disabled={savingBenefit}
                onchange={(e) => (benefitForm = { ...benefitForm, finance_validator_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each users as u (u.id)}
                  <option value={u.id}>{u.display_name || u.username}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.capabilityRef')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={benefitForm.capability_ref}
                disabled={savingBenefit}
                oninput={(e) => (benefitForm = { ...benefitForm, capability_ref: e.target.value })}
              />
            </label>
          </div>

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingBenefit} onclick={submitBenefit}>
              {editingBenefit ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeBenefitForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      {#if benefits.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else if filteredBenefits.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noResults')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3"></th>
              <th class="py-1.5 pr-3">{t('p3m.benefits')}</th>
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.targetValue')}</th>
              <th class="py-1.5 pr-3">{t('p3m.baselineValue')}</th>
              <th class="py-1.5 pr-3">{t('p3m.latestRealized')}</th>
              <th class="w-28 py-1.5 pr-3"></th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each filteredBenefits as b (b.id)}
              {@const open = expandedBenefits[b.id]}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">
                  <button type="button" class="cursor-pointer" onclick={() => toggleBenefit(b)} aria-label={t('p3m.realizations')}>
                    <IconChevronRight size={14} class={open !== undefined ? 'rotated' : ''} />
                  </button>
                </td>
                <td class="py-1.5 pr-3">
                  {b.title}
                  <span class="ml-2 rounded bg-slate-100 px-1 text-[10px] capitalize text-slate-500">{b.benefit_type}</span>
                  <span class="ml-1 rounded px-1 text-[10px] capitalize" style={benefitStatusStyle(b.status)}>{b.status || 'planned'}</span>
                  {#if b.measurement_frequency}
                    <span class="ml-1 text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">{b.measurement_frequency}</span>
                  {/if}
                </td>
                <td class="py-1.5 pr-3">{entityLabel(b)}</td>
                <td class="py-1.5 pr-3">
                  {formatMoney(b.target_value)}
                  {#if b.unit_of_measure}
                    <span class="text-[var(--color-ds-text-muted,#6b7280)]"> {b.unit_of_measure}</span>
                  {/if}
                </td>
                <td class="py-1.5 pr-3">{formatMoney(b.baseline_value)}</td>
                <td class="py-1.5 pr-3">{b.latest_realized_value != null ? formatMoney(b.latest_realized_value) : t('p3m.notSet')}</td>
                <td class="py-1.5 pr-3">
                  <div class="h-1.5 w-full overflow-hidden rounded bg-[var(--color-ds-border,#e5e7eb)]">
                    <div class="h-full rounded" style="width: {benefitProgress(b)}%; background: #2563eb"></div>
                  </div>
                </td>
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1">
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openBenefitEdit(b)}>
                      {t('p3m.edit')}
                    </button>
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteBenefit(b)}>
                      <IconX size={14} />
                    </button>
                  </div>
                </td>
              </tr>
              {#if open !== undefined}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td></td>
                  <td colspan="7" class="py-1.5 pr-3 pl-8">
                    <div class="mb-2 flex items-center gap-2">
                      <input
                        type="number"
                        step="any"
                        class="w-32 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                        placeholder={t('p3m.realizedValue')}
                        value={realizationDraft(b.id).value}
                        oninput={(e) => (realizationForms[b.id] = { ...realizationDraft(b.id), value: e.target.value })}
                      />
                      <input
                        class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                        placeholder={t('p3m.notes')}
                        value={realizationDraft(b.id).notes}
                        oninput={(e) => (realizationForms[b.id] = { ...realizationDraft(b.id), notes: e.target.value })}
                      />
                      <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-2 py-0.5 font-medium text-white disabled:opacity-40" disabled={savingRealizationId === b.id || realizationDraft(b.id).value === ''} onclick={() => recordRealization(b)}>
                        {t('p3m.newRealization')}
                      </button>
                    </div>
                    {#if open.length === 0}
                      <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</span>
                    {:else}
                      {#each [...open].sort((x, y) => (x.recorded_date > y.recorded_date ? -1 : 1)) as r (r.id)}
                        <div class="flex items-center gap-3 py-0.5">
                          <span>{formatDate(r.recorded_date)}</span>
                          <span class="font-medium">{formatMoney(r.realized_value)}</span>
                          {#if r.notes}
                            <span class="text-[var(--color-ds-text-muted,#6b7280)]">{r.notes}</span>
                          {/if}
                          <button type="button" class="ml-auto text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteRealization(b, r)}>
                            <IconX size={12} />
                          </button>
                        </div>
                      {/each}
                    {/if}

                    <!-- Measurements (BEN-007) -->
                    <div class="mt-2 border-t border-[var(--color-ds-border,#e5e7eb)] pt-1.5">
                      <p class="mb-1 text-xs font-semibold">{t('p3m.measurementsTitle')}</p>
                      <div class="mb-1 flex items-center gap-2">
                        <input
                          type="date"
                          class="w-36 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                          value={measurementDraft(b.id).due_date}
                          oninput={(e) => (measurementForms[b.id] = { ...measurementDraft(b.id), due_date: e.target.value })}
                        />
                        <input
                          type="number"
                          step="any"
                          class="w-28 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                          placeholder={t('p3m.realizedValue')}
                          value={measurementDraft(b.id).value}
                          oninput={(e) => (measurementForms[b.id] = { ...measurementDraft(b.id), value: e.target.value })}
                        />
                        <input
                          class="min-w-0 flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                          placeholder={t('p3m.evidence')}
                          value={measurementDraft(b.id).evidence}
                          oninput={(e) => (measurementForms[b.id] = { ...measurementDraft(b.id), evidence: e.target.value })}
                        />
                        <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-2 py-0.5 font-medium text-white disabled:opacity-40" disabled={savingMeasurementBenefitId === b.id || measurementDraft(b.id).due_date === ''} onclick={() => recordMeasurement(b)}>
                          {t('p3m.newMeasurement')}
                        </button>
                      </div>
                      {#each (measurementCache[b.id] ?? []).slice().sort((x, y) => (x.due_date > y.due_date ? 1 : -1)) as m (m.id)}
                        {@const overdue = !m.completed_at && m.due_date < new Date().toISOString().slice(0, 10)}
                        <div class="flex items-center gap-3 py-0.5">
                          <span style={overdue ? 'color:#dc2626;font-weight:600' : ''}>{m.due_date}{#if overdue} ({t('p3m.overdue')}){/if}</span>
                          {#if m.completed_at}
                            <span class="font-medium">{m.value != null ? formatMoney(m.value) : '—'}</span>
                          {:else}
                            <input
                              type="number"
                              step="any"
                              class="w-24 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5"
                              placeholder={t('p3m.realizedValue')}
                              value={completeDrafts[m.id] ?? ''}
                              oninput={(e) => (completeDrafts = { ...completeDrafts, [m.id]: e.target.value })}
                            />
                            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-2 py-0.5 font-medium text-white disabled:opacity-40" disabled={savingMeasurementBenefitId === b.id || (completeDrafts[m.id] ?? '').trim() === ''} onclick={() => completeMeasurement(b, m)}>
                              {t('p3m.save')}
                            </button>
                          {/if}
                          {#if m.evidence}
                            <span class="text-[var(--color-ds-text-muted,#6b7280)]">{m.evidence}</span>
                          {/if}
                        </div>
                      {:else}
                        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</span>
                      {/each}
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Tranches -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.tranches')}</h2>
        {#if portfolios.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openTrancheCreate}>
            <IconPlus size={14} class="inline" /> {t('p3m.addTranche')}
          </button>
        {/if}
      </div>

      <!-- Expenditure type filter -->
      <div class="mb-2 flex items-center gap-2 text-xs">
        <select
          value={expenditureTypeFilter}
          onchange={(e) => (expenditureTypeFilter = e.target.value)}
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
        >
          <option value="all">{t('p3m.allExpenditureTypes')}</option>
          {#each EXPENDITURE_TYPES as et (et)}
            <option value={et}>{t(`p3m.${et}`)}</option>
          {/each}
        </select>
      </div>

      {#if trancheFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
            <label class="block">
              <span>{t('p3m.entityType')} *</span>
              <div class="mt-0.5 flex gap-2">
                <select
                  class="w-28 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={trancheForm.entity_type}
                  disabled={savingTranche || (projectOptions.length === 0 && trancheForm.entity_type === 'project') || (programmeOptions.length === 0 && trancheForm.entity_type === 'programme')}
                  onchange={(e) => (trancheForm = { ...trancheForm, entity_type: e.target.value, entity_id: '' })}
                >
                  <option value="portfolio">{t('p3m.portfolios')}</option>
                  {#if programmeOptions.length > 0}
                    <option value="programme">{t('p3m.programmes')}</option>
                  {/if}
                  {#if projectOptions.length > 0}
                    <option value="project">{t('p3m.projects')}</option>
                  {/if}
                </select>
                <select class="min-w-0 flex-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={trancheForm.entity_id} disabled={savingTranche} onchange={(e) => (trancheForm = { ...trancheForm, entity_id: e.target.value })}>
                  <option value="">{t('p3m.noSelection')}</option>
                  {#each trancheEntityOptions as p (p.id)}
                    <option value={p.id}>{p.title}</option>
                  {/each}
                </select>
              </div>
            </label>

            <label class="block md:col-span-2">
              <span>{t('p3m.title')} *</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={trancheForm.title}
                disabled={savingTranche}
                oninput={(e) => (trancheForm = { ...trancheForm, title: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.amount')}</span>
              <input
                type="number"
                step="any"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={trancheForm.amount}
                disabled={savingTranche}
                oninput={(e) => (trancheForm = { ...trancheForm, amount: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.expenditureType')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={trancheForm.expenditure_type}
                disabled={savingTranche}
                onchange={(e) => (trancheForm = { ...trancheForm, expenditure_type: e.target.value })}
              >
                <option value="capex">{t('p3m.capex')}</option>
                <option value="opex">{t('p3m.opex')}</option>
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.plannedDate')} *</span>
              <input
                type="date"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={trancheForm.planned_date}
                disabled={savingTranche}
                oninput={(e) => (trancheForm = { ...trancheForm, planned_date: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.trancheStatus')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={trancheForm.status}
                disabled={savingTranche}
                onchange={(e) => (trancheForm = { ...trancheForm, status: e.target.value })}
              >
                <option value="proposed">{t('p3m.trancheProposed')}</option>
                <option value="approved">{t('p3m.trancheApproved')}</option>
                <option value="released">{t('p3m.trancheReleased')}</option>
              </select>
            </label>
          </div>

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingTranche} onclick={submitTranche}>
              {editingTranche ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeTrancheForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      {#if tranches.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else if filteredTranches.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noResults')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.title')}</th>
              <th class="py-1.5 pr-3">{t('p3m.amount')}</th>
              <th class="py-1.5 pr-3">{t('p3m.expenditureType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.plannedDate')}</th>
              <th class="py-1.5 pr-3">Status</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each filteredTranches as tr (tr.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{entityTitleById.get(`${tr.entity_type}:${tr.entity_id}`) ?? `#${tr.entity_id}`}</td>
                <td class="py-1.5 pr-3">{tr.title || '—'}</td>
                <td class="py-1.5 pr-3">{formatMoney(tr.amount)}</td>
                <td class="py-1.5 pr-3">
                  <span class={tr.expenditure_type === 'capex' ? 'rounded bg-blue-100 px-1.5 py-0.5 text-[10px] text-blue-700' : 'rounded bg-violet-100 px-1.5 py-0.5 text-[10px] text-violet-700'}>
                    {tr.expenditure_type}
                  </span>
                </td>
                <td class="py-1.5 pr-3">{formatDate(tr.planned_date)}</td>
                <td class="py-1.5 pr-3 capitalize">{tr.status}</td>
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1">
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openTrancheEdit(tr)}>
                      {t('p3m.edit')}
                    </button>
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteTranche(tr)}>
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

    <!-- Forecast -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.forecast')}</h2>
      {#if forecast.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <div class="flex items-end gap-1 overflow-x-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" style="min-height: 160px">
          {#each forecast as point (point.month)}
            {@const capexH = maxForecastValue ? ((point.capex_total ?? 0) / maxForecastValue) * 120 : 0}
            {@const opexH = maxForecastValue ? ((point.opex_total ?? 0) / maxForecastValue) * 120 : 0}
            <div class="flex min-w-8 flex-col items-center gap-1" title="{point.month}: {t('p3m.capex')} {formatMoney(point.capex_total)} · {t('p3m.opex')} {formatMoney(point.opex_total)}">
              <div class="flex h-[120px] w-full items-end justify-center gap-0.5">
                <div class="w-2 rounded-t bg-blue-500" style="height: {Math.max(capexH, point.capex_total ? 2 : 0)}px"></div>
                <div class="w-2 rounded-t bg-violet-400" style="height: {Math.max(opexH, point.opex_total ? 2 : 0)}px"></div>
              </div>
              <span class="text-[9px] text-[var(--color-ds-text-muted,#6b7280)]">{point.month.slice(5)}/{point.month.slice(2, 4)}</span>
            </div>
          {/each}
        </div>
        <div class="mt-1 flex items-center gap-4 text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-blue-500"></span>{t('p3m.capex')}</span>
          <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded bg-violet-400"></span>{t('p3m.opex')}</span>
        </div>
      {/if}
    </section>
  </div>
{/if}

<style>
  .rotated {
    transform: rotate(90deg);
  }
</style>
