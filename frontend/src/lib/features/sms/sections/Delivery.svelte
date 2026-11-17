<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { formatDate, entityTypeLabel, P3M_OPTIONS_KEYS } from '../utils.js';

  let loading = $state(true);
  let error = $state('');
  /** @type {{ portfolios: Array<Record<string, any>>, programmes: Array<Record<string, any>>, projects: Array<Record<string, any>> }} */
  let p3mOptions = $state({ portfolios: [], programmes: [], projects: [] });
  /** @type {Array<Record<string, any>>} */
  let stakeholders = $state([]);
  /** @type {Array<Record<string, any>>} */
  let nodes = $state([]);

  // ---- RACI ---------------------------------------------------------------

  let raciEntityType = $state('project');
  let raciEntityId = $state('');
  /** @type {Array<Record<string, any>>} */
  let racis = $state([]);
  /** @type {Record<string, any> | null} */
  let selectedRaci = $state(null);
  /** @type {Array<Record<string, any>>} */
  let assignments = $state([]);
  let saveError = $state('');
  let saving = $state(false);

  const RACI_STATUSES = ['draft', 'preliminary', 'under_discussion', 'agreed'];
  const RACI_VALUES = ['r', 'a', 'c', 'i'];

  function raciStatusKey(s) {
    if (s === 'draft') return 'sms.stDraft';
    if (s === 'preliminary') return 'sms.stPreliminary';
    if (s === 'under_discussion') return 'sms.stUnderDiscussion';
    return 'sms.stAgreed';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [options, stakeholderRows, nodeRows] = await Promise.all([
        smsApi.getP3MOptions(),
        smsApi.getStakeholders({}),
        smsApi.getNodes({}),
      ]);
      p3mOptions = { portfolios: [], programmes: [], projects: [], ...(options ?? {}) };
      stakeholders = stakeholderRows ?? [];
      nodes = nodeRows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadRacis() {
    if (!raciEntityType || !raciEntityId) {
      racis = [];
      selectedRaci = null;
      assignments = [];
      return;
    }
    try {
      const rows = await smsApi.getRacis({ entity_type: raciEntityType, entity_id: Number(raciEntityId) });
      racis = rows ?? [];
      if (selectedRaci && !racis.some((r) => r.id === selectedRaci.id)) {
        selectedRaci = null;
        assignments = [];
      }
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function openRaci(raci) {
    try {
      const detail = await smsApi.getRaci(raci.id);
      selectedRaci = detail;
      assignments = (detail.assignments ?? []).map((a) => ({ ...a, assignee_type: a.stakeholder_id ? 'stakeholder' : 'user', assignee_id: a.stakeholder_id || a.user_id || '' }));
      saveError = '';
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function createRaci() {
    if (!raciEntityType || !raciEntityId) return;
    try {
      const created = await smsApi.createRaci({ entity_type: raciEntityType, entity_id: Number(raciEntityId) });
      await loadRacis();
      if (created?.id) openRaci(created);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteRaci(raci) {
    if (!confirm(t('sms.deleteMessage'))) return;
    try {
      await smsApi.deleteRaci(raci.id);
      if (selectedRaci?.id === raci.id) {
        selectedRaci = null;
        assignments = [];
      }
      await loadRacis();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function advanceStatus() {
    if (!selectedRaci) return;
    const idx = RACI_STATUSES.indexOf(selectedRaci.status);
    if (idx < 0 || idx >= RACI_STATUSES.length - 1) return;
    try {
      await smsApi.updateRaci(selectedRaci.id, { status: RACI_STATUSES[idx + 1] });
      openRaci(selectedRaci);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function saveLastReviewed() {
    if (!selectedRaci) return;
    try {
      await smsApi.updateRaci(selectedRaci.id, { last_reviewed_at: selectedRaci.last_reviewed_at });
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function addAssignmentRow() {
    assignments = [...assignments, { activity_code: '', description: '', assignee_type: 'stakeholder', assignee_id: '', value: 'r' }];
  }

  function removeAssignmentRow(index) {
    assignments = assignments.filter((_, i) => i !== index);
  }

  async function saveAssignments() {
    if (!selectedRaci) return;
    saving = true;
    saveError = '';
    try {
      const payload = assignments.map((a) => ({
        activity_code: a.activity_code,
        description: a.description ?? '',
        value: a.value,
        ...(a.assignee_type === 'stakeholder' && a.assignee_id ? { stakeholder_id: Number(a.assignee_id) } : {}),
        ...(a.assignee_type === 'user' && a.assignee_id ? { user_id: Number(a.assignee_id) } : {}),
      }));
      await smsApi.saveRaciAssignments(selectedRaci.id, payload);
      openRaci(selectedRaci);
    } catch (e) {
      saveError = e?.message || 'error';
    } finally {
      saving = false;
    }
  }

  // ---- Work packages ------------------------------------------------------

  /** @type {Array<Record<string, any>>} */
  let workPackages = $state([]);
  /** @type {Record<string, any> | null} */
  let selectedWp = $state(null);
  /** @type {Array<Record<string, any>>} */
  let wpNodes = $state([]);
  let wpFormOpen = $state(false);
  /** @type {Record<string, any> | null} */
  let editingWp = $state(null);
  let wpForm = $state({ title: '', description: '', entity_type: 'project', entity_id: '', contractor_stakeholder_id: '', status: 'planned', planned_start: '', planned_end: '', notes: '' });
  let wpNodeLink = $state({ node_id: '', impact_notes: '' });

  const WP_STATUSES = ['planned', 'in_progress', 'completed', 'cancelled'];

  function wpStatusKey(s) {
    if (s === 'planned') return 'sms.stPlanned';
    if (s === 'in_progress') return 'sms.stInProgress';
    if (s === 'completed') return 'sms.stCompleted';
    return 'sms.stCancelled';
  }

  async function loadWorkPackages() {
    try {
      const rows = await smsApi.getWorkPackages({});
      workPackages = rows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function openWp(wp) {
    try {
      selectedWp = wp;
      const detail = await smsApi.getWorkPackage(wp.id);
      wpNodes = detail.nodes ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function openCreateWp() {
    editingWp = null;
    wpForm = { title: '', description: '', entity_type: raciEntityType, entity_id: raciEntityId, contractor_stakeholder_id: '', status: 'planned', planned_start: '', planned_end: '', notes: '' };
    wpFormOpen = true;
  }

  function openEditWp(wp) {
    editingWp = wp;
    wpForm = {
      title: wp.title,
      description: wp.description ?? '',
      entity_type: wp.entity_type || 'project',
      entity_id: wp.entity_id ? String(wp.entity_id) : '',
      contractor_stakeholder_id: wp.contractor_stakeholder_id ? String(wp.contractor_stakeholder_id) : '',
      status: wp.status,
      planned_start: wp.planned_start ?? '',
      planned_end: wp.planned_end ?? '',
      notes: wp.notes ?? '',
    };
    wpFormOpen = true;
  }

  async function saveWp() {
    if (!wpForm.title.trim()) return;
    /** @type {Record<string, any>} */
    const payload = { ...wpForm };
    if (payload.entity_id) payload.entity_id = Number(payload.entity_id);
    else delete payload.entity_id;
    if (payload.contractor_stakeholder_id) payload.contractor_stakeholder_id = Number(payload.contractor_stakeholder_id);
    else delete payload.contractor_stakeholder_id;
    try {
      if (editingWp) await smsApi.updateWorkPackage(editingWp.id, payload);
      else await smsApi.createWorkPackage(payload);
      wpFormOpen = false;
      await loadWorkPackages();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteWp(wp) {
    if (!confirm(t('sms.deleteMessage'))) return;
    try {
      await smsApi.deleteWorkPackage(wp.id);
      if (selectedWp?.id === wp.id) {
        selectedWp = null;
        wpNodes = [];
      }
      await loadWorkPackages();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addWpNode() {
    if (!selectedWp || !wpNodeLink.node_id) return;
    try {
      await smsApi.addWorkPackageNode(selectedWp.id, { node_id: Number(wpNodeLink.node_id), impact_notes: wpNodeLink.impact_notes });
      wpNodeLink = { node_id: '', impact_notes: '' };
      const detail = await smsApi.getWorkPackage(selectedWp.id);
      wpNodes = detail.nodes ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeWpNode(linkId) {
    if (!selectedWp) return;
    try {
      await smsApi.removeWorkPackageNode(linkId);
      const detail = await smsApi.getWorkPackage(selectedWp.id);
      wpNodes = detail.nodes ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function entityTitle(entityType, entityId) {
    if (!entityType) return '—';
    const list = p3mOptions[P3M_OPTIONS_KEYS[entityType]] ?? [];
    const found = list.find((o) => o.id === Number(entityId));
    return `${entityTypeLabel(entityType)}: ${found?.title ?? `#${entityId}`}`;
  }

  onMount(() => {
    load();
    loadWorkPackages();
  });
</script>

<div class="space-y-6 px-6 py-4">
  {#if loading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
  {:else if error && racis.length === 0 && workPackages.length === 0}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
  {:else}
    <!-- RACI matrices -->
    <section>
      <div class="mb-2 flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.raciMatrices')}</h2>
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciEntityType} onchange={(e) => { raciEntityType = e.target.value; raciEntityId = ''; loadRacis(); }}>
          <option value="">{t('sms.allEntities')}</option>
          {#each ['portfolio', 'programme', 'project'] as et (et)}<option value={et}>{entityTypeLabel(et)}</option>{/each}
        </select>
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={raciEntityId} onchange={(e) => { raciEntityId = e.target.value; loadRacis(); }} disabled={!raciEntityType}>
          <option value="">{t('sms.selectEntity')}</option>
          {#each p3mOptions[P3M_OPTIONS_KEYS[raciEntityType]] ?? [] as o (o.id)}<option value={String(o.id)}>{o.title}</option>{/each}
        </select>
        <button type="button" class="ml-auto rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white disabled:opacity-40" onclick={createRaci} disabled={!raciEntityType || !raciEntityId}>
          {t('sms.newMatrixVersion')}
        </button>
      </div>

      {#if error && (racis.length > 0 || workPackages.length > 0)}<div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>{/if}

      {#if !raciEntityType || !raciEntityId}
        <div class="rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.selectEntityHint')}</div>
      {:else if racis.length === 0}
        <div class="rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRacis')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.version')}</th>
              <th class="py-1.5 pr-3">{t('sms.status')}</th>
              <th class="py-1.5 pr-3">{t('sms.lastReviewedAt')}</th>
              <th class="py-1.5"></th>
            </tr>
          </thead>
          <tbody>
            {#each racis as r (r.id)}
              <tr class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0 hover:bg-slate-50" class:selected={selectedRaci?.id === r.id} onclick={() => openRaci(r)}>
                <td class="py-1.5 pr-3 font-medium">v{r.version}</td>
                <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium">{t(raciStatusKey(r.status))}</span></td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(r.last_reviewed_at)}</td>
                <td class="py-1.5 text-right">
                  <button type="button" class="text-red-600" onclick={(e) => { e.stopPropagation(); deleteRaci(r); }}>{t('sms.delete')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Matrix detail: assignment editor -->
      {#if selectedRaci}
        <div class="mt-3 space-y-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="text-xs font-semibold">{t('sms.raciMatrix')} v{selectedRaci.version} — {entityTitle(selectedRaci.entity_type, selectedRaci.entity_id)}</h3>
            <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium">{t(raciStatusKey(selectedRaci.status))}</span>
            {#if RACI_STATUSES.indexOf(selectedRaci.status) < RACI_STATUSES.length - 1}
              <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs" onclick={advanceStatus}>
                {t('sms.advanceTo')} {t(raciStatusKey(RACI_STATUSES[RACI_STATUSES.indexOf(selectedRaci.status) + 1]))}
              </button>
            {/if}
            <label class="ml-auto flex items-center gap-1.5 text-xs">
              {t('sms.lastReviewedAt')}
              <input type="date" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-0.5" value={selectedRaci.last_reviewed_at ? String(selectedRaci.last_reviewed_at).slice(0, 10) : ''} onchange={(e) => { selectedRaci = { ...selectedRaci, last_reviewed_at: e.target.value }; saveLastReviewed(); }} />
            </label>
          </div>

          <table class="w-full text-left text-xs">
            <thead>
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                <th class="py-1.5 pr-3">{t('sms.activityCode')}</th>
                <th class="py-1.5 pr-3">{t('sms.description')}</th>
                <th class="py-1.5 pr-3">{t('sms.assigneeType')}</th>
                <th class="py-1.5 pr-3">{t('sms.assignee')}</th>
                <th class="py-1.5 pr-3">{t('sms.raciValue')}</th>
                <th class="py-1.5"></th>
              </tr>
            </thead>
            <tbody>
              {#each assignments as a, i (a.id ?? `new-${i}`)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                  <td class="py-1.5 pr-3"><input class="w-28 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5" value={a.activity_code} oninput={(e) => (a.activity_code = e.target.value)} /></td>
                  <td class="py-1.5 pr-3"><input class="w-full min-w-40 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5" value={a.description ?? ''} oninput={(e) => (a.description = e.target.value)} /></td>
                  <td class="py-1.5 pr-3">
                    <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5" value={a.assignee_type} onchange={(e) => { a.assignee_type = e.target.value; a.assignee_id = ''; }}>
                      <option value="stakeholder">{t('sms.stakeholder')}</option>
                      <option value="user">{t('sms.user')}</option>
                    </select>
                  </td>
                  <td class="py-1.5 pr-3">
                    {#if a.assignee_type === 'stakeholder'}
                      <select class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5" value={String(a.assignee_id ?? '')} onchange={(e) => (a.assignee_id = e.target.value)}>
                        <option value="">{t('sms.selectStakeholder')}</option>
                        {#each stakeholders as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
                      </select>
                    {:else}
                      <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5" placeholder={t('sms.userIdPlaceholder')} value={String(a.assignee_id ?? '')} oninput={(e) => (a.assignee_id = e.target.value)} />
                    {/if}
                  </td>
                  <td class="py-1.5 pr-3">
                    <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5 font-mono" value={a.value} onchange={(e) => (a.value = e.target.value)}>
                      {#each RACI_VALUES as v (v)}<option value={v}>{t(`sms.raci${v.toUpperCase()}`)}</option>{/each}
                    </select>
                  </td>
                  <td class="py-1.5 text-right"><button type="button" class="text-red-600" onclick={() => removeAssignmentRow(i)}>{t('sms.delete')}</button></td>
                </tr>
              {/each}
            </tbody>
          </table>

          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={addAssignmentRow}>{t('sms.addRow')}</button>
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white disabled:opacity-50" onclick={saveAssignments} disabled={saving}>
              {t('sms.saveAssignments')}
            </button>
            {#if saveError}<span class="text-xs text-red-700">{saveError}</span>{/if}
          </div>
        </div>
      {/if}
    </section>

    <!-- Work packages -->
    <section>
      <div class="mb-2 flex items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.workPackages')}</h2>
        <button type="button" class="ml-auto rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={openCreateWp}>
          {t('sms.newWorkPackage')}
        </button>
      </div>

      {#if wpFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{editingWp ? t('sms.editWorkPackage') : t('sms.newWorkPackage')}</h3>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-4">
            <label class="text-xs md:col-span-2">
              {t('sms.title')} *
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.title} oninput={(e) => (wpForm.title = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.contractor')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.contractor_stakeholder_id} onchange={(e) => (wpForm.contractor_stakeholder_id = e.target.value)}>
                <option value="">{t('sms.selectStakeholder')}</option>
                {#each stakeholders as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.status')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.status} onchange={(e) => (wpForm.status = e.target.value)}>
                {#each WP_STATUSES as s (s)}<option value={s}>{t(wpStatusKey(s))}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.entityType')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.entity_type} onchange={(e) => (wpForm.entity_type = e.target.value)}>
                <option value="">{t('sms.noEntity')}</option>
                {#each ['portfolio', 'programme', 'project'] as et (et)}<option value={et}>{entityTypeLabel(et)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.entity')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.entity_id} onchange={(e) => (wpForm.entity_id = e.target.value)} disabled={!wpForm.entity_type}>
                <option value="">{t('sms.selectEntity')}</option>
                {#each p3mOptions[P3M_OPTIONS_KEYS[wpForm.entity_type]] ?? [] as o (o.id)}<option value={String(o.id)}>{o.title}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.plannedStart')}
              <input type="date" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.planned_start} oninput={(e) => (wpForm.planned_start = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.plannedEnd')}
              <input type="date" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpForm.planned_end} oninput={(e) => (wpForm.planned_end = e.target.value)} />
            </label>
          </div>
          <div class="mt-2 flex gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={saveWp}>{t('sms.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={() => (wpFormOpen = false)}>{t('sms.back')}</button>
          </div>
        </div>
      {/if}

      {#if workPackages.length === 0}
        <div class="rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noWorkPackages')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.title')}</th>
              <th class="py-1.5 pr-3">{t('sms.contractor')}</th>
              <th class="py-1.5 pr-3">{t('sms.entity')}</th>
              <th class="py-1.5 pr-3">{t('sms.status')}</th>
              <th class="py-1.5 pr-3">{t('sms.plannedStart')} → {t('sms.plannedEnd')}</th>
              <th class="py-1.5"></th>
            </tr>
          </thead>
          <tbody>
            {#each workPackages as wp (wp.id)}
              <tr class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0 hover:bg-slate-50" class:selected={selectedWp?.id === wp.id} onclick={() => openWp(wp)}>
                <td class="py-1.5 pr-3 font-medium">{wp.title}</td>
                <td class="py-1.5 pr-3">{wp.contractor_name || '—'}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{entityTitle(wp.entity_type, wp.entity_id)}</td>
                <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium">{t(wpStatusKey(wp.status))}</span></td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(wp.planned_start)} → {formatDate(wp.planned_end)}</td>
                <td class="py-1.5 text-right">
                  <button type="button" class="mr-2 text-[var(--color-ds-accent,#2563eb)]" onclick={(e) => { e.stopPropagation(); openEditWp(wp); }}>{t('sms.edit')}</button>
                  <button type="button" class="text-red-600" onclick={(e) => { e.stopPropagation(); deleteWp(wp); }}>{t('sms.delete')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Work package detail: impacted nodes -->
      {#if selectedWp}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{selectedWp.title} — {t('sms.impactedNodes')}</h3>
          {#if wpNodes.length === 0}
            <div class="mb-2 rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noImpactedNodes')}</div>
          {:else}
            <ul class="mb-2 space-y-1">
              {#each wpNodes as n (n.id)}
                <li class="flex items-center gap-2 text-xs">
                  <span class="font-medium">{n.node_name || `#${n.node_id}`}</span>
                  {#if n.impact_notes}<span class="text-[var(--color-ds-text-muted,#6b7280)]">— {n.impact_notes}</span>{/if}
                  <button type="button" class="ml-auto text-red-600" onclick={() => removeWpNode(n.id)}>{t('sms.delete')}</button>
                </li>
              {/each}
            </ul>
          {/if}
          <div class="flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-2">
            <label class="text-xs">
              {t('sms.node')} *
              <select class="mt-0.5 block w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpNodeLink.node_id} onchange={(e) => (wpNodeLink.node_id = e.target.value)}>
                <option value="">{t('sms.selectNode')}</option>
                {#each nodes as n (n.id)}<option value={String(n.id)}>{n.name}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.impactNotes')}
              <input class="mt-0.5 block w-64 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={wpNodeLink.impact_notes} oninput={(e) => (wpNodeLink.impact_notes = e.target.value)} />
            </label>
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={addWpNode}>{t('sms.add')}</button>
          </div>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  tr.selected td {
    background: color-mix(in srgb, var(--color-ds-accent, #2563eb) 8%, transparent);
  }
</style>
