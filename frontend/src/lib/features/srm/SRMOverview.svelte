<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { permissionStore } from '../../stores/permissions.svelte.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { srm } from '../../api/srm.js';
  import { getUsers } from '../../api/users.js';
  import { formatDate, formatMoney, stateLabel, SRM_STATES } from './utils.js';
  import { ragColor, stageLabel } from '../p3m/utils.js';

  let { summary = {}, onRefreshed = () => {} } = $props();

  // Directory + register data.
  let suppliers = $state([]);
  let contracts = $state([]);
  let users = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Cross-module P3M view: entity options for the contract form and project
  // aggregates for the projects section.
  let p3mOptions = $state([]);
  let srmProjects = $state([]);
  const canOpenP3M = $derived($permissionStore.canAccessP3M);
  const p3mEntityOptions = $derived(p3mOptions.filter((o) => o.type === newP3MEntityType));

  // Deep link into the P3M module's per-project detail (rollup view).
  function openP3MProject(projectId) {
    window.location.href = `/p3m?project=${projectId}`;
  }

  // New supplier form.
  let newSupplierName = $state('');
  let newSupplierTier = $state('tier_3');
  let newSupplierWebsite = $state('');

  // Supplier detail (gate position + contacts).
  let selectedSupplierId = $state(null);
  let supplierDetail = $state(null);
  let gateReason = $state('');
  let contactName = $state('');
  let contactRole = $state('');
  let contactEmail = $state('');
  let contactPhone = $state('');

  // New contract form.
  let newContractNumber = $state('');
  let newContractTitle = $state('');
  let newContractSupplierId = $state('');
  let newContractCategory = $state('');
  let newContractValue = $state('');
  let newContractStartDate = $state('');
  let newP3MEntityType = $state('');
  let newP3MEntityId = $state('');

  // Contract detail (assignments, documents, extractions, plan).
  let selectedContractId = $state(null);
  let contractDetail = $state(null);
  let assignUserId = $state('');
  let assignRole = $state('contract_owner');
  let docType = $state('contract');
  let docFileName = $state('');

  const stateChips = $derived(
    SRM_STATES.filter((s) => (summary.suppliers?.by_state ?? {})[s]).map((s) => ({
      state: s,
      count: summary.suppliers.by_state[s],
    })),
  );
  const tierChips = $derived(
    Object.entries(summary.suppliers?.by_tier ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([tier, count]) => ({ tier, count })),
  );
  const contractStatusChips = $derived(
    Object.entries(summary.contracts?.by_status ?? {}).map(([status, count]) => ({ status, count })),
  );
  const upcomingReviews = $derived(summary.contracts?.upcoming_reviews ?? []);

  async function load() {
    loading = true;
    error = '';
    try {
      const [supRows, conRows] = await Promise.all([srm.suppliers.getAll(), srm.contracts.getAll()]);
      suppliers = supRows ?? [];
      contracts = conRows ?? [];
      getUsers().then((rows) => (users = rows ?? [])).catch(() => {});
      loadCrossModule();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadCrossModule() {
    // Failures here must not break the supplier/contract views.
    try {
      const [opts, projs] = await Promise.all([srm.getP3MOptions(), srm.getProjects()]);
      p3mOptions = Array.isArray(opts) ? opts : [];
      srmProjects = Array.isArray(projs) ? projs : [];
    } catch (e) {
      // Keep whatever was loaded before; the main views own `error`.
    }
  }

  async function refreshLists() {
    try {
      const [supRows, conRows] = await Promise.all([srm.suppliers.getAll(), srm.contracts.getAll()]);
      suppliers = supRows ?? [];
      contracts = conRows ?? [];
      loadCrossModule();
      onRefreshed();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // ----- Supplier directory -----

  async function addSupplier() {
    const name = newSupplierName.trim();
    if (!name) return;
    try {
      await srm.suppliers.create({
        company_name: name,
        tier: newSupplierTier,
        website: newSupplierWebsite.trim(),
      });
      newSupplierName = '';
      newSupplierWebsite = '';
      await refreshLists();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function selectSupplier(id) {
    if (selectedSupplierId === id) return;
    selectedSupplierId = id;
    supplierDetail = null;
    gateReason = '';
    try {
      const sup = await srm.suppliers.get(id);
      supplierDetail = sup ?? null;
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addContact() {
    if (!contactName.trim()) return;
    try {
      await srm.addSupplierContact(selectedSupplierId, {
        full_name: contactName.trim(),
        role_title: contactRole.trim(),
        email: contactEmail.trim(),
        phone: contactPhone.trim(),
      });
      contactName = '';
      contactRole = '';
      contactEmail = '';
      contactPhone = '';
      supplierDetail = await srm.suppliers.get(selectedSupplierId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function advanceGate() {
    try {
      await srm.advanceGate(selectedSupplierId);
      gateReason = '';
      supplierDetail = await srm.suppliers.get(selectedSupplierId);
      await refreshLists();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function downgradeGate() {
    const reason = gateReason.trim();
    if (!reason) return;
    try {
      await srm.downgradeGate(selectedSupplierId, { reason });
      gateReason = '';
      supplierDetail = await srm.suppliers.get(selectedSupplierId);
      await refreshLists();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function closeSupplier() {
    const reason = gateReason.trim();
    if (!reason) return;
    try {
      await srm.closeSupplier(selectedSupplierId, { reason });
      gateReason = '';
      supplierDetail = await srm.suppliers.get(selectedSupplierId);
      await refreshLists();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // ----- Contract register -----

  async function addContract() {
    const number = newContractNumber.trim();
    const title = newContractTitle.trim();
    if (!number || !title || !newContractSupplierId) return;
    try {
      await srm.contracts.create({
        supplier_id: Number(newContractSupplierId),
        contract_number: number,
        title,
        category: newContractCategory.trim(),
        total_value: newContractValue === '' ? 0 : Number(newContractValue),
        currency: 'NZD',
        start_date: newContractStartDate ? `${newContractStartDate}T00:00:00Z` : undefined,
        ...(newP3MEntityType && newP3MEntityId
          ? { p3m_entity_type: newP3MEntityType, p3m_entity_id: Number(newP3MEntityId) }
          : {}),
      });
      newContractNumber = '';
      newContractTitle = '';
      newContractCategory = '';
      newContractValue = '';
      newContractStartDate = '';
      newP3MEntityType = '';
      newP3MEntityId = '';
      await refreshLists();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function selectContract(id) {
    if (selectedContractId === id) return;
    selectedContractId = id;
    contractDetail = null;
    try {
      const con = await srm.contracts.get(id);
      contractDetail = con ?? null;
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addAssignment() {
    if (!assignUserId) return;
    try {
      await srm.assignContractRole(selectedContractId, {
        user_id: Number(assignUserId),
        role: assignRole,
      });
      assignUserId = '';
      contractDetail = await srm.contracts.get(selectedContractId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeAssignment(assignmentId) {
    try {
      await srm.removeContractAssignment(selectedContractId, assignmentId);
      contractDetail = await srm.contracts.get(selectedContractId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addDocument() {
    const fileName = docFileName.trim();
    if (!fileName) return;
    try {
      await srm.addContractDocument(selectedContractId, { doc_type: docType, file_name: fileName });
      docFileName = '';
      contractDetail = await srm.contracts.get(selectedContractId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteDocument(documentId) {
    try {
      await srm.deleteContractDocument(documentId);
      contractDetail = await srm.contracts.get(selectedContractId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function reviewExtraction(extractionId, status) {
    try {
      await srm.reviewClauseExtraction(extractionId, { status });
      contractDetail = await srm.contracts.get(selectedContractId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // ----- Labels -----

  function tierLabel(tier) {
    switch (tier) {
      case 'tier_1':
        return t('srm.tier1');
      case 'tier_2':
        return t('srm.tier2');
      case 'tier_3':
        return t('srm.tier3');
      case 'tier_4':
        return t('srm.tier4');
      default:
        return tier || '';
    }
  }

  function stateClass(state) {
    if (state === 'closed') return 'bg-slate-200 text-slate-600';
    if (state === 'monitoring_active' || state === 'jbp_formalised') return 'bg-emerald-100 text-emerald-700';
    if (state === 'registered') return 'bg-slate-100 text-slate-600';
    return 'bg-blue-100 text-blue-700';
  }

  function contractStatusClass(status) {
    if (status === 'active') return 'bg-emerald-100 text-emerald-700';
    if (status === 'draft') return 'bg-slate-100 text-slate-600';
    if (status === 'terminated' || status === 'retired') return 'bg-red-100 text-red-700';
    return 'bg-blue-100 text-blue-700';
  }

  function roleLabel(role) {
    switch (role) {
      case 'internal_contract_manager':
        return t('srm.roleInternalManager');
      case 'contract_owner':
        return t('srm.roleOwner');
      case 'budget_approver':
        return t('srm.roleBudgetApprover');
      default:
        return role || '';
    }
  }

  onMount(load);
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('srm.loading')}
  </div>
{:else if error && suppliers.length === 0 && contracts.length === 0}
  <EmptyState title={t('srm.loadErrorTitle')} description={error} actions={{ label: t('srm.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    {#if error}
      <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
    {/if}

    <!-- Dashboard -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.dashboard')}</h2>
      <div class="flex flex-wrap gap-2">
        {#each stateChips as chip (chip.state)}
          <span class="rounded-full px-2 py-0.5 text-xs {stateClass(chip.state)}" title={stateLabel(chip.state)}>
            {stateLabel(chip.state)}: {chip.count}
          </span>
        {/each}
        {#each tierChips as chip (chip.tier)}
          <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600">
            {tierLabel(chip.tier)}: {chip.count}
          </span>
        {/each}
        {#each contractStatusChips as chip (chip.status)}
          <span class="rounded-full px-2 py-0.5 text-xs {contractStatusClass(chip.status)}">
            {chip.status}: {chip.count}
          </span>
        {/each}
        <span class="rounded-full bg-red-100 px-2 py-0.5 text-xs text-red-700">
          {t('srm.openDisputes')}: {summary.open_disputes ?? 0}
        </span>
        <span class="rounded-full bg-amber-100 px-2 py-0.5 text-xs text-amber-700">
          {t('srm.pendingExtractions')}: {summary.pending_extractions ?? 0}
        </span>
      </div>

      {#if upcomingReviews.length > 0}
        <h3 class="mb-1 mt-3 text-xs font-semibold">{t('srm.upcomingReviews')}</h3>
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.contractNumber')}</th>
              <th class="py-1.5 pr-3">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-3">{t('srm.reviewDate')}</th>
            </tr>
          </thead>
          <tbody>
            {#each upcomingReviews as rv (rv.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{rv.contract_number}</td>
                <td class="py-1.5 pr-3">{rv.supplier_name}</td>
                <td class="py-1.5 pr-3">{formatDate(rv.review_date)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Supplier directory -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.supplierDirectory')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.companyName')}
          value={newSupplierName}
          oninput={(e) => (newSupplierName = e.target.value)}
        />
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newSupplierTier} onchange={(e) => (newSupplierTier = e.target.value)}>
          <option value="tier_1">{t('srm.tier1')}</option>
          <option value="tier_2">{t('srm.tier2')}</option>
          <option value="tier_3">{t('srm.tier3')}</option>
          <option value="tier_4">{t('srm.tier4')}</option>
        </select>
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.website')}
          value={newSupplierWebsite}
          oninput={(e) => (newSupplierWebsite = e.target.value)}
        />
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addSupplier}>
          {t('srm.newSupplier')}
        </button>
      </div>
      {#if suppliers.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.companyName')}</th>
              <th class="py-1.5 pr-3">{t('srm.tier')}</th>
              <th class="py-1.5 pr-3">{t('srm.relationshipState')}</th>
              <th class="py-1.5 pr-3">{t('srm.activeContracts')}</th>
              <th class="py-1.5 pr-3">{t('srm.openDisputes')}</th>
            </tr>
          </thead>
          <tbody>
            {#each suppliers as sup (sup.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedSupplierId === sup.id ? 'bg-ds-surface-selected' : ''}"
                onclick={() => selectSupplier(sup.id)}
              >
                <td class="py-1.5 pr-3">{sup.company_name}</td>
                <td class="py-1.5 pr-3">{tierLabel(sup.tier)}</td>
                <td class="py-1.5 pr-3">
                  <span class="rounded-full px-2 py-0.5 {stateClass(sup.relationship_state)}">{stateLabel(sup.relationship_state)}</span>
                </td>
                <td class="py-1.5 pr-3">{sup.active_contracts ?? 0}</td>
                <td class="py-1.5 pr-3">{sup.open_disputes ?? 0}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Supplier detail: gate position + contacts -->
      {#if selectedSupplierId !== null && supplierDetail === null}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.loading')}
        </div>
      {:else if supplierDetail}
        {@const gate = supplierDetail.gate}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <div class="mb-2 flex items-center gap-2">
            <span class="text-xs font-semibold">{supplierDetail.company_name}</span>
            <span class="rounded-full px-2 py-0.5 {stateClass(supplierDetail.relationship_state)}">
              {stateLabel(supplierDetail.relationship_state)}
            </span>
            {#if gate?.next}
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">→ {stateLabel(gate.next)}</span>
            {/if}
          </div>

          {#if gate && Object.keys(gate.artifacts ?? {}).length > 0}
            <h3 class="mb-1 text-xs font-semibold">{t('srm.gateArtifacts')}</h3>
            <ul class="mb-2 space-y-0.5">
              {#each Object.entries(gate.artifacts) as [name, passed] (name)}
                <li class="text-xs">
                  <span class={passed ? 'text-emerald-600' : 'text-red-600'}>{passed ? '✓' : '✗'}</span>
                  {name}
                </li>
              {/each}
            </ul>
          {/if}

          {#if supplierDetail.relationship_state !== 'closed'}
            <div class="mb-3 flex items-center gap-2">
              <input
                class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
                placeholder={t('srm.reason')}
                value={gateReason}
                oninput={(e) => (gateReason = e.target.value)}
              />
              {#if gate?.next && (gate.missing ?? []).length === 0}
                <button type="button" class="cursor-pointer rounded-md border border-emerald-300 bg-emerald-50 px-2 py-1 text-xs text-emerald-700 hover:bg-emerald-100" onclick={advanceGate}>
                  {t('srm.advanceGate')}
                </button>
              {/if}
              <button type="button" class="cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!gateReason.trim()} onclick={downgradeGate}>
                {t('srm.downgradeGate')}
              </button>
              <button type="button" class="cursor-pointer rounded-md border border-red-300 bg-red-50 px-2 py-1 text-xs text-red-700 hover:bg-red-100" disabled={!gateReason.trim()} onclick={closeSupplier}>
                {t('srm.closeSupplier')}
              </button>
            </div>
          {:else}
            <div class="mb-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.closedReadOnly')}</div>
          {/if}

          <h3 class="mb-1 text-xs font-semibold">{t('srm.contacts')}</h3>
          {#if (supplierDetail.contacts ?? []).length === 0}
            <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              {t('srm.noRecords')}
            </div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-3">{t('srm.fullName')}</th>
                  <th class="py-1.5 pr-3">{t('srm.roleTitle')}</th>
                  <th class="py-1.5 pr-3">{t('srm.email')}</th>
                  <th class="py-1.5 pr-3">{t('srm.phone')}</th>
                </tr>
              </thead>
              <tbody>
                {#each supplierDetail.contacts as c (c.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{c.full_name}{#if c.is_primary} <span class="text-[var(--color-ds-text-muted,#6b7280)]">★</span>{/if}</td>
                    <td class="py-1.5 pr-3">{c.role_title || ''}</td>
                    <td class="py-1.5 pr-3">{c.email || ''}</td>
                    <td class="py-1.5 pr-3">{c.phone || ''}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
          {#if supplierDetail.relationship_state !== 'closed'}
            <div class="mt-2 flex items-center gap-2">
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.fullName')} value={contactName} oninput={(e) => (contactName = e.target.value)} />
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.roleTitle')} value={contactRole} oninput={(e) => (contactRole = e.target.value)} />
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.email')} value={contactEmail} oninput={(e) => (contactEmail = e.target.value)} />
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.phone')} value={contactPhone} oninput={(e) => (contactPhone = e.target.value)} />
              <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addContact}>
                {t('srm.addContact')}
              </button>
            </div>
          {/if}
        </div>
      {/if}
    </section>

    <!-- Contract register -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.contractRegister')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.contractNumber')}
          value={newContractNumber}
          oninput={(e) => (newContractNumber = e.target.value)}
        />
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.title')}
          value={newContractTitle}
          oninput={(e) => (newContractTitle = e.target.value)}
        />
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newContractSupplierId} onchange={(e) => (newContractSupplierId = e.target.value)}>
          <option value="">—</option>
          {#each suppliers as sup (sup.id)}
            <option value={sup.id}>{sup.company_name}</option>
          {/each}
        </select>
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.category')}
          value={newContractCategory}
          oninput={(e) => (newContractCategory = e.target.value)}
        />
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.totalValue')}
          type="number"
          value={newContractValue}
          oninput={(e) => (newContractValue = e.target.value)}
        />
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          type="date"
          value={newContractStartDate}
          onchange={(e) => (newContractStartDate = e.target.value)}
        />
        <select
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          title={t('srm.p3mEntity')}
          value={newP3MEntityType}
          onchange={(e) => { newP3MEntityType = e.target.value; newP3MEntityId = ''; }}
        >
          <option value="">{t('srm.p3mEntity')}: —</option>
          <option value="portfolio">{t('srm.entityPortfolio')}</option>
          <option value="programme">{t('srm.entityProgramme')}</option>
          <option value="project">{t('srm.entityProject')}</option>
        </select>
        {#if newP3MEntityType}
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newP3MEntityId} onchange={(e) => (newP3MEntityId = e.target.value)}>
            <option value="">—</option>
            {#each p3mEntityOptions as opt (opt.id)}
              <option value={opt.id}>{opt.title}</option>
            {/each}
          </select>
        {/if}
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addContract}>
          {t('srm.newContract')}
        </button>
      </div>
      {#if contracts.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.contractNumber')}</th>
              <th class="py-1.5 pr-3">{t('srm.title')}</th>
              <th class="py-1.5 pr-3">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-3">{t('srm.p3mEntity')}</th>
              <th class="py-1.5 pr-3">{t('srm.status')}</th>
              <th class="py-1.5 pr-3">{t('srm.totalValue')}</th>
              <th class="py-1.5 pr-3">{t('srm.reviewDate')}</th>
            </tr>
          </thead>
          <tbody>
            {#each contracts as con (con.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedContractId === con.id ? 'bg-ds-surface-selected' : ''}"
                onclick={() => selectContract(con.id)}
              >
                <td class="py-1.5 pr-3">{con.contract_number}</td>
                <td class="py-1.5 pr-3">{con.title}</td>
                <td class="py-1.5 pr-3">{con.supplier_name || ''}</td>
                <td class="py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{con.p3m_entity_title || '—'}</td>
                <td class="py-1.5 pr-3">
                  <span class="rounded-full px-2 py-0.5 {contractStatusClass(con.status)}">{con.status}</span>
                </td>
                <td class="py-1.5 pr-3">{formatMoney(con.total_value)} {con.currency || ''}</td>
                <td class="py-1.5 pr-3">{formatDate(con.review_date)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Contract detail: assignments, documents, extractions, plan -->
      {#if selectedContractId !== null && contractDetail === null}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.loading')}
        </div>
      {:else if contractDetail}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <div class="mb-2 flex items-center gap-2">
            <span class="text-xs font-semibold">{contractDetail.contract_number}</span>
            <span class="rounded-full px-2 py-0.5 {contractStatusClass(contractDetail.status)}">{contractDetail.status}</span>
            <span class="text-[var(--color-ds-text-muted,#6b7280)]">
              {formatMoney(contractDetail.total_value)} {contractDetail.currency || ''} · {formatDate(contractDetail.start_date)} → {formatDate(contractDetail.end_date)}
            </span>
            {#if contractDetail.p3m_entity_title}
              <span class="rounded-full bg-ds-surface-hovered px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)]">
                {t('srm.p3mEntity')}: {contractDetail.p3m_entity_title}
              </span>
            {/if}
          </div>

          <h3 class="mb-1 text-xs font-semibold">{t('srm.assignments')}</h3>
          {#if (contractDetail.assignments ?? []).length === 0}
            <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              {t('srm.noRecords')}
            </div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-3">{t('srm.user')}</th>
                  <th class="py-1.5 pr-3">{t('srm.role')}</th>
                  <th class="py-1.5 pr-3"></th>
                </tr>
              </thead>
              <tbody>
                {#each contractDetail.assignments as a (a.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{a.user_name || `#${a.user_id}`}</td>
                    <td class="py-1.5 pr-3">{roleLabel(a.role)}</td>
                    <td class="py-1.5 pr-3 text-right">
                      <button type="button" class="cursor-pointer text-red-600 hover:underline" onclick={() => removeAssignment(a.id)}>
                        {t('srm.delete')}
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
          <div class="mt-2 flex items-center gap-2">
            <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={assignUserId} onchange={(e) => (assignUserId = e.target.value)}>
              <option value="">—</option>
              {#each users as u (u.id)}
                <option value={u.id}>{u.display_name || u.username}</option>
              {/each}
            </select>
            <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={assignRole} onchange={(e) => (assignRole = e.target.value)}>
              <option value="contract_owner">{t('srm.roleOwner')}</option>
              <option value="internal_contract_manager">{t('srm.roleInternalManager')}</option>
              <option value="budget_approver">{t('srm.roleBudgetApprover')}</option>
            </select>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addAssignment}>
              {t('srm.addAssignment')}
            </button>
          </div>

          <h3 class="mb-1 mt-3 text-xs font-semibold">{t('srm.documents')}</h3>
          {#if (contractDetail.documents ?? []).length === 0}
            <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              {t('srm.noRecords')}
            </div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-3">{t('srm.docType')}</th>
                  <th class="py-1.5 pr-3">{t('srm.fileName')}</th>
                  <th class="py-1.5 pr-3">{t('srm.version')}</th>
                  <th class="py-1.5 pr-3"></th>
                </tr>
              </thead>
              <tbody>
                {#each contractDetail.documents as d (d.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{d.doc_type}</td>
                    <td class="py-1.5 pr-3">{d.file_name}</td>
                    <td class="py-1.5 pr-3">{d.version}</td>
                    <td class="py-1.5 pr-3 text-right">
                      <button type="button" class="cursor-pointer text-red-600 hover:underline" onclick={() => deleteDocument(d.id)}>
                        {t('srm.delete')}
                      </button>
                    </td>
                  </tr>
                  {#each (d.extractions ?? []) as x (x.id)}
                    <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                      <td colspan="4" class="py-1.5 pr-3">
                        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{x.field_name}:</span> {x.proposed_value}
                        {#if x.confidence !== null && x.confidence !== undefined}
                          <span class="text-[var(--color-ds-text-muted,#6b7280)]">({Math.round(x.confidence * 100)}%)</span>
                        {/if}
                        {#if x.status === 'pending'}
                          <button type="button" class="ml-2 cursor-pointer text-emerald-600 hover:underline" onclick={() => reviewExtraction(x.id, 'accepted')}>
                            {t('srm.accept')}
                          </button>
                          <button type="button" class="ml-1 cursor-pointer text-red-600 hover:underline" onclick={() => reviewExtraction(x.id, 'rejected')}>
                            {t('srm.reject')}
                          </button>
                        {:else}
                          <span class="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-[var(--color-ds-text-muted,#6b7280)]">{x.status}</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                {/each}
              </tbody>
            </table>
          {/if}
          <div class="mt-2 flex items-center gap-2">
            <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={docType} onchange={(e) => (docType = e.target.value)}>
              <option value="contract">{t('srm.docContract')}</option>
              <option value="variation">{t('srm.docVariation')}</option>
              <option value="sla_schedule">{t('srm.docSlaSchedule')}</option>
              <option value="insurance">{t('srm.docInsurance')}</option>
              <option value="other">{t('srm.docOther')}</option>
            </select>
            <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.fileName')} value={docFileName} oninput={(e) => (docFileName = e.target.value)} />
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addDocument}>
              {t('srm.addDocument')}
            </button>
          </div>

          {#if contractDetail.management_plan}
            {@const plan = contractDetail.management_plan}
            <h3 class="mb-1 mt-3 text-xs font-semibold">{t('srm.managementPlan')} v{plan.version}</h3>
            <dl class="space-y-1 text-xs">
              <div><dt class="inline font-semibold">{t('srm.rightSizeTier')}: </dt><dd class="inline">{plan.right_size_tier || ''}</dd></div>
              {#if plan.objectives}<div><dt class="inline font-semibold">{t('srm.objectives')}: </dt><dd class="inline">{plan.objectives}</dd></div>{/if}
              {#if plan.key_terms_summary}<div><dt class="inline font-semibold">{t('srm.keyTerms')}: </dt><dd class="inline">{plan.key_terms_summary}</dd></div>{/if}
              {#if plan.governance_arrangements}<div><dt class="inline font-semibold">{t('srm.governance')}: </dt><dd class="inline">{plan.governance_arrangements}</dd></div>{/if}
              {#if plan.monitoring_provisions}<div><dt class="inline font-semibold">{t('srm.monitoringProvisions')}: </dt><dd class="inline">{plan.monitoring_provisions}</dd></div>{/if}
            </dl>
          {/if}
        </div>
      {/if}
    </section>

    <!-- Cross-module P3M projects: financials and stakeholders across projects -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.projectsSection')}</h2>
      {#if srmProjects.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.title')}</th>
              <th class="py-1.5 pr-3">{t('srm.stage')}</th>
              <th class="py-1.5 pr-3">{t('p3m.ragStatus')}</th>
              <th class="py-1.5 pr-3">{t('srm.budgetApproved')}</th>
              <th class="py-1.5 pr-3">{t('srm.contractCount')}</th>
              <th class="py-1.5 pr-3">{t('p3m.contractValue')}</th>
              <th class="py-1.5 pr-3">{t('p3m.committedSpend')}</th>
              <th class="py-1.5 pr-3">{t('p3m.actualSpend')}</th>
              <th class="py-1.5 pr-3">{t('srm.stakeholders')}</th>
            </tr>
          </thead>
          <tbody>
            {#each srmProjects as pr (pr.id)}
              <tr
                class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0 {canOpenP3M ? 'cursor-pointer hover:bg-ds-background-neutral-hovered' : ''}"
                title={canOpenP3M ? t('p3m.title') : ''}
                onclick={() => canOpenP3M && openP3MProject(pr.id)}
              >
                <td class="py-1.5 pr-3 font-medium">{pr.title}</td>
                <td class="py-1.5 pr-3">{stageLabel(pr.current_stage)}</td>
                <td class="py-1.5 pr-3">
                  <span class="inline-block h-2.5 w-2.5 rounded-full" style="background: {ragColor(pr.rag_status)}"></span>
                </td>
                <td class="py-1.5 pr-3">{formatMoney(pr.budget_approved)}</td>
                <td class="py-1.5 pr-3">{pr.contract_count}</td>
                <td class="py-1.5 pr-3">{formatMoney(pr.contract_value_total)}</td>
                <td class="py-1.5 pr-3">{formatMoney(pr.committed_spend)}</td>
                <td class="py-1.5 pr-3">{formatMoney(pr.actual_spend)}</td>
                <td class="py-1.5 pr-3">{pr.stakeholder_count}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
