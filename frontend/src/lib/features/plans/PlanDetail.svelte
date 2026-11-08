<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { plans as plansApi } from '../../api/plans.js';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { sms as smsApi } from '../../api/sms.js';
  import { workspaces as workspacesApi } from '../../api/workspaces.js';
  import { navigate } from '../../router.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Modal from '../../dialogs/Modal.svelte';
  import Button from '../../components/Button.svelte';
  import Label from '../../components/Label.svelte';
  import TextField from '../../components/TextField.svelte';
  import TextareaField from '../../components/TextareaField.svelte';
  import IconMap from '@tabler/icons-svelte-runes/icons/map';
  import IconArrowLeft from '@tabler/icons-svelte-runes/icons/arrow-left';
  import IconPencil from '@tabler/icons-svelte-runes/icons/pencil';
  import IconX from '@tabler/icons-svelte-runes/icons/x';

  import PlanSummaryView from './views/PlanSummaryView.svelte';
  import PlanTimelineView from './views/PlanTimelineView.svelte';
  import PlanListView from './views/PlanListView.svelte';
  import PlanBoardView from './views/PlanBoardView.svelte';
  import PlanCalendarView from './views/PlanCalendarView.svelte';
  import PlanTeamsView from './views/PlanTeamsView.svelte';
  import PlanDependenciesView from './views/PlanDependenciesView.svelte';
  import PlanScenariosView from './views/PlanScenariosView.svelte';

  let { id } = $props();

  let detail = $state(null);
  let loading = $state(true);
  let error = $state('');
  // Top-level view switcher (spec: horizontal navigation between plan views).
  let activeView = $state('summary');
  // Sub-tabs inside the Sources view (workspace/P3M/stakeholder management).
  let activeTab = $state('workspaces');

  // Edit modal
  let showEdit = $state(false);
  let saving = $state(false);
  let formError = $state('');
  let form = $state({ name: '', description: '', status: 'active', color: '', start_date: '', end_date: '' });

  // Workspace picker
  let wsSearch = $state('');
  let wsOptions = $state([]);
  let wsLoading = $state(false);

  // P3M picker
  let p3mType = $state('programme');
  let p3mOptions = $state([]);
  let p3mLoading = $state(false);

  // Stakeholder picker
  let stakeholderOptions = $state([]);
  let stakeholderLoading = $state(false);

  // Plan filters (named CQL sources that pull items into the plan).
  let planFilters = $state([]);
  let filterLoading = $state(false);
  let filterSaving = $state(false);
  let filterError = $state('');
  let editingFilterId = $state(null); // null = adding a new filter
  let fName = $state('');
  let fQuery = $state('');

  function statusLabel(status) {
    switch (status) {
      case 'draft':
        return t('plans.statusDraft');
      case 'active':
        return t('plans.statusActive');
      case 'completed':
        return t('plans.statusCompleted');
      default:
        return t('plans.statusArchived');
    }
  }

  function p3mTypeLabel(entityType) {
    switch (entityType) {
      case 'portfolio':
        return t('plans.p3mTypePortfolio');
      case 'programme':
        return t('plans.p3mTypeProgramme');
      default:
        return t('plans.p3mTypeProject');
    }
  }

  const VIEW_TABS = [
    { id: 'summary', labelKey: 'plans.tabSummary' },
    { id: 'timeline', labelKey: 'plans.tabTimeline' },
    { id: 'list', labelKey: 'plans.tabList' },
    { id: 'board', labelKey: 'plans.tabBoard' },
    { id: 'calendar', labelKey: 'plans.tabCalendar' },
    { id: 'teams', labelKey: 'plans.tabTeams' },
    { id: 'dependencies', labelKey: 'plans.tabDependencies' },
    { id: 'scenarios', labelKey: 'plans.tabScenarios' },
    { id: 'sources', labelKey: 'plans.tabSources' },
  ];

  const TABS = [
    { id: 'workspaces', labelKey: 'plans.tabWorkspaces' },
    { id: 'add-workspaces', labelKey: 'plans.tabAddWorkspace' },
    { id: 'p3m', labelKey: 'plans.tabP3M' },
    { id: 'stakeholders', labelKey: 'plans.tabStakeholders' },
    { id: 'filters', labelKey: 'plans.tabFilters' },
    { id: 'overview', labelKey: 'plans.tabOverview' },
  ];

  // Support deep links like /plans/:id?view=board or ?tab=workspaces (used by the plan cards).
  const qs = new URLSearchParams(window.location.search);
  if (VIEW_TABS.some((v) => v.id === qs.get('view'))) activeView = qs.get('view');
  if (TABS.some((tab) => tab.id === qs.get('tab'))) {
    activeTab = qs.get('tab');
    activeView = 'sources';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      detail = await plansApi.getDetail(id);
      if (!detail) detail = { plan: null, workspaces: [], p3m_entities: [], stakeholders: [] };
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  // --- workspace picker ------------------------------------------------------

  async function searchWorkspaces() {
    wsLoading = true;
    try {
      const res = await workspacesApi.getAll({ search: wsSearch || undefined, limit: 200 });
      const all = res?.data ?? (Array.isArray(res) ? res : []);
      const linked = new Set((detail?.workspaces ?? []).map((w) => w.workspace_id));
      wsOptions = all.filter((w) => !linked.has(w.id) && !w.is_personal).slice(0, 50);
    } catch (e) {
      wsOptions = [];
    } finally {
      wsLoading = false;
    }
  }

  async function addWorkspace(wsId) {
    try {
      await plansApi.addWorkspace(id, wsId);
      wsOptions = wsOptions.filter((w) => w.id !== wsId);
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeWorkspace(wsId) {
    try {
      await plansApi.removeWorkspace(id, wsId);
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- P3M picker -------------------------------------------------------------

  async function loadP3MOptions() {
    p3mLoading = true;
    try {
      const client = p3mApi[p3mType === 'portfolio' ? 'portfolios' : p3mType === 'programme' ? 'programmes' : 'projects'];
      const res = await client.getAll({});
      const all = res?.data ?? (Array.isArray(res) ? res : []);
      const linked = new Set(
        (detail?.p3m_entities ?? []).filter((e) => e.entity_type === p3mType).map((e) => e.entity_id)
      );
      p3mOptions = all.filter((e) => !linked.has(e.id)).slice(0, 100);
    } catch (e) {
      p3mOptions = [];
    } finally {
      p3mLoading = false;
    }
  }

  async function addP3MEntity(entityId) {
    if (!entityId) return;
    try {
      await plansApi.addP3MEntity(id, { entity_type: p3mType, entity_id: Number(entityId) });
      p3mOptions = p3mOptions.filter((e) => e.id !== Number(entityId));
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeP3MEntity(entityType, entityId) {
    try {
      await plansApi.removeP3MEntity(id, entityType, entityId);
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- stakeholder picker ------------------------------------------------------

  async function loadStakeholders() {
    stakeholderLoading = true;
    try {
      const res = await smsApi.getStakeholders({ limit: 200 });
      const all = res?.data ?? (Array.isArray(res) ? res : []);
      const linked = new Set((detail?.stakeholders ?? []).map((s) => s.stakeholder_id));
      stakeholderOptions = all.filter((s) => !linked.has(s.id)).slice(0, 100);
    } catch (e) {
      stakeholderOptions = [];
    } finally {
      stakeholderLoading = false;
    }
  }

  async function addStakeholder(stakeholderId) {
    if (!stakeholderId) return;
    try {
      await plansApi.addStakeholder(id, Number(stakeholderId));
      stakeholderOptions = stakeholderOptions.filter((s) => s.id !== Number(stakeholderId));
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeStakeholder(stakeholderId) {
    try {
      await plansApi.removeStakeholder(id, stakeholderId);
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- plan filters (named CQL sources) ----------------------------------------

  async function loadFilters() {
    filterLoading = true;
    try {
      const res = await plansApi.listFilters(id);
      planFilters = Array.isArray(res) ? res : (res?.filters ?? []);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      filterLoading = false;
    }
  }

  function resetFilterForm() {
    editingFilterId = null;
    fName = '';
    fQuery = '';
    filterError = '';
  }

  function startEditFilter(f) {
    editingFilterId = f.id;
    fName = f.name;
    fQuery = f.cql_query;
    filterError = '';
  }

  async function saveFilter() {
    if (!fName.trim() || !fQuery.trim()) {
      filterError = t('plans.filterFieldsRequired');
      return;
    }
    filterSaving = true;
    filterError = '';
    try {
      const payload = { name: fName.trim(), cql_query: fQuery.trim() };
      if (editingFilterId === null) {
        await plansApi.createFilter(id, payload);
      } else {
        await plansApi.updateFilter(id, editingFilterId, payload);
      }
      resetFilterForm();
      await loadFilters();
    } catch (e) {
      filterError = e?.message || 'error';
    } finally {
      filterSaving = false;
    }
  }

  async function removeFilter(f) {
    if (!confirm(t('plans.filterDeleteConfirm', { name: f.name }))) return;
    try {
      await plansApi.deleteFilter(id, f.id);
      if (editingFilterId === f.id) resetFilterForm();
      await loadFilters();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // --- edit / delete -----------------------------------------------------------

  function openEdit() {
    const p = detail?.plan;
    if (!p) return;
    form = {
      name: p.name,
      description: p.description ?? '',
      status: p.status,
      color: p.color ?? '',
      start_date: p.start_date ? String(p.start_date).slice(0, 10) : '',
      end_date: p.end_date ? String(p.end_date).slice(0, 10) : '',
    };
    formError = '';
    showEdit = true;
  }

  async function saveEdit() {
    if (!form.name.trim()) {
      formError = t('plans.nameRequired');
      return;
    }
    saving = true;
    formError = '';
    try {
      await plansApi.update(id, {
        name: form.name.trim(),
        description: form.description,
        status: form.status,
        color: form.color,
        start_date: form.start_date || null,
        end_date: form.end_date || null,
      });
      showEdit = false;
      await load();
    } catch (e) {
      formError = e?.message || 'error';
    } finally {
      saving = false;
    }
  }

  async function deletePlan() {
    if (!confirm(t('plans.deleteConfirm'))) return;
    try {
      await plansApi.delete(id);
      navigate('/plans');
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  onMount(() => {
    load();
    searchWorkspaces();
    loadP3MOptions();
    loadStakeholders();
    loadFilters();
  });
</script>

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('plans.loading')}
  </div>
{:else if error && !detail}
  <EmptyState title={t('plans.loadErrorTitle')} description={error} class="h-64">
    {#snippet action()}
      <button
        type="button"
        class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
        onclick={load}
      >
        {t('plans.retry')}
      </button>
    {/snippet}
  </EmptyState>
{:else if detail}
  <div class="px-6">
    <button
      type="button"
      class="mb-2 inline-flex items-center gap-1 text-sm text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]"
      onclick={() => navigate('/plans')}
    >
      <IconArrowLeft size={14} />
      {t('plans.backToPlans')}
    </button>
    <PageHeader
      icon={IconMap}
      title={detail.plan?.name}
      marginClass="mt-0 mb-6"
      subtitle={
        [
          statusLabel(detail.plan?.status),
          detail.plan?.start_date ? String(detail.plan.start_date).slice(0, 10) : null,
          detail.plan?.end_date ? String(detail.plan.end_date).slice(0, 10) : null,
        ]
          .filter(Boolean)
          .join(' · ')
      }
    >
      {#snippet actions()}
        <div class="flex items-center gap-2">
          <Button variant="secondary" size="sm" onclick={openEdit}>{t('common.edit')}</Button>
          <Button variant="danger-ghost" size="sm" onclick={deletePlan}>{t('common.delete')}</Button>
        </div>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex items-center gap-1 overflow-x-auto border-b border-[var(--color-ds-border,#e5e7eb)] px-6 pt-2">
    {#each VIEW_TABS as v (v.id)}
      <button type="button" class="plan-tab" class:active={activeView === v.id} onclick={() => (activeView = v.id)}>
        {t(v.labelKey)}
      </button>
    {/each}
  </div>

  {#if activeView === 'sources'}
  <div class="flex gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] px-6 pt-2">
    {#each TABS as tab (tab.id)}
      <button type="button" class="plan-tab" class:active={activeTab === tab.id} onclick={() => (activeTab = tab.id)}>
        {t(tab.labelKey)}
        <span class="ml-1.5 rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">
          {tab.id === 'workspaces'
            ? detail.workspaces?.length
            : tab.id === 'p3m'
              ? detail.p3m_entities?.length
              : tab.id === 'stakeholders'
                ? detail.stakeholders?.length
                : tab.id === 'filters'
                  ? planFilters.length
                  : ''}
        </span>
      </button>
    {/each}
  </div>

  <div class="px-6 py-6">
    {#if activeTab === 'workspaces'}
      {#if (detail.workspaces ?? []).length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.noWorkspaces')}</p>
      {:else}
        <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each detail.workspaces as ws (ws.workspace_id)}
            <div class="flex items-center border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] last:border-b-0">
              <span class="flex-1 truncate px-3 py-2 text-sm">{ws.workspace_name}</span>
              <span class="px-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{ws.item_count ?? 0} items</span>
              <button
                type="button"
                class="p-2 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600"
                title={t('common.delete')}
                onclick={() => removeWorkspace(ws.workspace_id)}
              >
                <IconX size={14} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    {:else if activeTab === 'add-workspaces'}
      <div class="mb-4 flex items-center gap-2">
        <Label class="mb-0">{t('plans.addWorkspace')}</Label>
        <input
          type="search"
          bind:value={wsSearch}
          oninput={() => wsSearch && searchWorkspaces()}
          placeholder={t('plans.searchWorkspacesPlaceholder')}
          class="h-8 w-64 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        />
        {#if wsLoading}<span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</span>{/if}
      </div>

      {#if wsOptions.length > 0}
        <div class="max-h-96 overflow-y-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each wsOptions as ws (ws.id)}
            <div class="flex items-center border-b border-[var(--color-ds-border,#e5e7eb)] last:border-b-0">
              <span class="flex-1 truncate px-3 py-1.5 text-sm">{ws.name}</span>
              <button
                type="button"
                class="px-3 py-1.5 text-xs font-medium text-[var(--color-ds-accent-blue,#2563eb)] hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]"
                onclick={() => addWorkspace(ws.id)}
              >
                {t('common.add')}
              </button>
            </div>
          {/each}
        </div>
      {:else if !wsLoading}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.noWorkspacesToAdd')}</p>
      {/if}
    {:else if activeTab === 'p3m'}
      <div class="mb-4 flex items-center gap-2">
        <Label class="mb-0">{t('plans.addP3M')}</Label>
        <select
          value={p3mType}
          onchange={(e) => {
            p3mType = e.currentTarget.value;
            loadP3MOptions();
          }}
          class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none"
        >
          <option value="portfolio">{t('plans.p3mPortfolio')}</option>
          <option value="programme">{t('plans.p3mProgramme')}</option>
          <option value="project">{t('plans.p3mProject')}</option>
        </select>
        <select
          onchange={(e) => addP3MEntity(e.currentTarget.value)}
          class="h-8 w-72 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none"
        >
          <option value="">
            {p3mLoading ? t('plans.loading') : p3mOptions.length ? t('plans.selectToLink') : t('plans.noP3MAvailable')}
          </option>
          {#each p3mOptions as e (e.id)}
            <option value={e.id}>{e.title}</option>
          {/each}
        </select>
      </div>

      {#if (detail.p3m_entities ?? []).length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.noP3M')}</p>
      {:else}
        <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each detail.p3m_entities as e (e.entity_type + ':' + e.entity_id)}
            <div class="flex items-center border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] last:border-b-0">
              <span class="rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-[10px] uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
                {p3mTypeLabel(e.entity_type)}
              </span>
              <span class="flex-1 truncate px-3 py-2 text-sm">{e.entity_title || `#${e.entity_id}`}</span>
              <button
                type="button"
                class="p-2 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600"
                title={t('common.delete')}
                onclick={() => removeP3MEntity(e.entity_type, e.entity_id)}
              >
                <IconX size={14} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    {:else if activeTab === 'stakeholders'}
      <div class="mb-4 flex items-center gap-2">
        <Label class="mb-0">{t('plans.addStakeholder')}</Label>
        <select
          onchange={(e) => addStakeholder(e.currentTarget.value)}
          class="h-8 w-72 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none"
        >
          <option value="">
            {stakeholderLoading
              ? t('plans.loading')
              : stakeholderOptions.length
                ? t('plans.selectToLink')
                : t('plans.noStakeholdersAvailable')}
          </option>
          {#each stakeholderOptions as s (s.id)}
            <option value={s.id}>{s.name}{s.organization ? ` — ${s.organization}` : ''}</option>
          {/each}
        </select>
      </div>

      {#if (detail.stakeholders ?? []).length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.noStakeholders')}</p>
      {:else}
        <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each detail.stakeholders as s (s.stakeholder_id)}
            <div class="flex items-center border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] last:border-b-0">
              <span class="flex-1 truncate px-3 py-2 text-sm">
                {s.name}
                {#if s.organization}<span class="text-[var(--color-ds-text-muted,#6b7280)]"> — {s.organization}</span>{/if}
              </span>
              {#if s.role_classification}
                <span class="px-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{s.role_classification}</span>
              {/if}
              <button
                type="button"
                class="p-2 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600"
                title={t('common.delete')}
                onclick={() => removeStakeholder(s.stakeholder_id)}
              >
                <IconX size={14} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    {:else if activeTab === 'filters'}
      <!-- Add / edit form -->
      <form
        class="mb-4 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-3"
        onsubmit={(e) => {
          e.preventDefault();
          saveFilter();
        }}
      >
        <div class="flex flex-wrap items-end gap-2">
          <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
            {t('plans.filterName')}
            <input
              type="text"
              bind:value={fName}
              placeholder={t('plans.filterNamePlaceholder')}
              class="mt-1 block h-8 w-56 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
            />
          </label>
          <button
            type="submit"
            disabled={filterSaving || !fName.trim() || !fQuery.trim()}
            class="h-8 rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
          >
            {editingFilterId === null ? t('plans.filterAdd') : t('common.save')}
          </button>
          {#if editingFilterId !== null}
            <button type="button" onclick={resetFilterForm} class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-3 text-sm hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
              {t('common.cancel')}
            </button>
          {/if}
        </div>
        <label class="block text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.filterQueryLabel')}
          <textarea
            bind:value={fQuery}
            rows={2}
            spellcheck="false"
            placeholder={t('plans.filterQueryPlaceholder')}
            class="mt-1 block w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 py-1.5 font-mono text-xs outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
          ></textarea>
        </label>
        {#if filterError}
          <p class="text-sm text-red-600">{filterError}</p>
        {/if}
      </form>

      <!-- Filter list -->
      {#if filterLoading && planFilters.length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
      {:else if planFilters.length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.filtersEmpty')}</p>
      {:else}
        <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each planFilters as f (f.id)}
            <div class="flex items-center gap-3 border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] px-3 py-2 last:border-b-0">
              <span class="shrink-0 text-sm font-medium">{f.name}</span>
              <code class="min-w-0 flex-1 truncate rounded bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs" title={f.cql_query}>
                {f.cql_query}
              </code>
              <button
                type="button"
                class="shrink-0 p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-accent-blue,#2563eb)]"
                title={t('common.edit')}
                onclick={() => startEditFilter(f)}
              >
                <IconPencil size={14} />
              </button>
              <button
                type="button"
                class="shrink-0 p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600"
                title={t('common.delete')}
                onclick={() => removeFilter(f)}
              >
                <IconX size={14} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    {:else}
      <div class="max-w-2xl space-y-4">
        <div>
          <h3 class="mb-1 text-sm font-semibold">{t('plans.description')}</h3>
          <p class="whitespace-pre-wrap text-sm text-[var(--color-ds-text-secondary,#4b5563)]">
            {detail.plan?.description || t('plans.noDescription')}
          </p>
        </div>
        <div class="grid grid-cols-3 gap-4">
          <div>
            <h3 class="text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.workspacesWord')}</h3>
            <p class="mt-1 text-2xl font-semibold tabular-nums">{detail.workspaces?.length || 0}</p>
          </div>
          <div>
            <h3 class="text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">P3M</h3>
            <p class="mt-1 text-2xl font-semibold tabular-nums">{detail.p3m_entities?.length || 0}</p>
          </div>
          <div>
            <h3 class="text-xs uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.stakeholdersWord')}</h3>
            <p class="mt-1 text-2xl font-semibold tabular-nums">{detail.stakeholders?.length || 0}</p>
          </div>
        </div>
      </div>
    {/if}
  </div>
  {:else}
    <div class="px-6 py-6">
      {#if activeView === 'summary'}
        <PlanSummaryView planId={id} />
      {:else if activeView === 'timeline'}
        <PlanTimelineView planId={id} />
      {:else if activeView === 'list'}
        <PlanListView planId={id} />
      {:else if activeView === 'board'}
        <PlanBoardView planId={id} />
      {:else if activeView === 'calendar'}
        <PlanCalendarView planId={id} />
      {:else if activeView === 'teams'}
        <PlanTeamsView planId={id} />
      {:else if activeView === 'dependencies'}
        <PlanDependenciesView planId={id} />
      {:else}
        <PlanScenariosView planId={id} />
      {/if}
    </div>
  {/if}
{/if}

{#if showEdit}
  <Modal isOpen={true} maxWidth="max-w-lg" onclose={() => (showEdit = false)}>
    <div class="px-8 py-6">
      <h2 class="mb-4 text-lg font-semibold">{t('plans.editPlan')}</h2>
      <form
        class="space-y-4"
        onsubmit={(e) => {
          e.preventDefault();
          saveEdit();
        }}
      >
        <TextField label={t('plans.name')} bind:value={form.name} required />
        <div>
          <Label for="plan-edit-status">{t('plans.status')}</Label>
          <select
            id="plan-edit-status"
            bind:value={form.status}
            class="h-9 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
          >
            <option value="draft">{t('plans.statusDraft')}</option>
            <option value="active">{t('plans.statusActive')}</option>
            <option value="completed">{t('plans.statusCompleted')}</option>
            <option value="archived">{t('plans.statusArchived')}</option>
          </select>
        </div>
        <TextField label={t('plans.color')} type="color" bind:value={form.color} />
        <div class="grid grid-cols-2 gap-3">
          <TextField label={t('plans.startDate')} type="date" bind:value={form.start_date} />
          <TextField label={t('plans.endDate')} type="date" bind:value={form.end_date} />
        </div>
        <TextareaField label={t('plans.description')} bind:value={form.description} rows={3} />
        {#if formError}
          <p class="text-sm text-red-600">{formError}</p>
        {/if}
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="secondary" size="sm" onclick={() => (showEdit = false)}>{t('common.cancel')}</Button>
          <Button variant="primary" size="sm" type="submit" disabled={saving}>{t('common.save')}</Button>
        </div>
      </form>
    </div>
  </Modal>
{/if}

<style>
  .plan-tab {
    padding: 0.5rem 0.875rem;
    font-size: 0.8125rem;
    color: var(--color-ds-text-muted, #6b7280);
    border-bottom: 2px solid transparent;
    cursor: pointer;
    background: none;
    border-top: none;
    border-left: none;
    border-right: none;
  }

  .plan-tab:hover {
    color: var(--color-ds-text, #111827);
  }

  .plan-tab.active {
    color: var(--color-ds-accent-blue, #2563eb);
    border-bottom-color: var(--color-ds-accent-blue, #2563eb);
  }
</style>
