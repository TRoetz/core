<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { plans as plansApi } from '../../api/plans.js';
  import { navigate } from '../../router.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Modal from '../../dialogs/Modal.svelte';
  import Button from '../../components/Button.svelte';
  import Label from '../../components/Label.svelte';
  import TextField from '../../components/TextField.svelte';
  import TextareaField from '../../components/TextareaField.svelte';
  import IconMap from '@tabler/icons-svelte-runes/icons/map';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';

  let plans = $state([]);
  let loading = $state(true);
  let error = $state('');
  let showForm = $state(false);
  let saving = $state(false);
  let formError = $state('');
  let form = $state(emptyForm());

  function emptyForm() {
    return { name: '', description: '', status: 'active', color: '', start_date: '', end_date: '' };
  }

  const totalWorkspaces = $derived(plans.reduce((n, p) => n + (p.workspace_count || 0), 0));

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await plansApi.list();
      plans = res?.data ?? (Array.isArray(res) ? res : []);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  function openForm() {
    form = emptyForm();
    formError = '';
    showForm = true;
  }

  function toPayload(f) {
    return {
      name: f.name.trim(),
      description: f.description,
      status: f.status,
      color: f.color,
      start_date: f.start_date || null,
      end_date: f.end_date || null,
    };
  }

  async function save() {
    if (!form.name.trim()) {
      formError = t('plans.nameRequired');
      return;
    }
    saving = true;
    formError = '';
    try {
      await plansApi.create(toPayload(form));
      showForm = false;
      await load();
    } catch (e) {
      formError = e?.message || 'error';
    } finally {
      saving = false;
    }
  }

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

  function statusClass(status) {
    switch (status) {
      case 'active':
        return 'bg-emerald-100 text-emerald-700';
      case 'draft':
        return 'bg-slate-100 text-slate-600';
      case 'completed':
        return 'bg-blue-100 text-blue-700';
      default:
        return 'bg-slate-100 text-slate-500';
    }
  }

  onMount(load);
</script>

<div class="px-6">
  <PageHeader icon={IconMap} title={t('plans.title')} subtitle={t('plans.subtitle')} marginClass="mt-4 mb-8">
    {#snippet actions()}
      <div class="flex items-center gap-3">
        <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {plans.length} {t('plans.plansWord')} · {totalWorkspaces} {t('plans.workspacesWord')}
        </div>
        <Button variant="primary" size="sm" onclick={openForm}>
          <IconPlus size={14} />
          {t('plans.newPlan')}
        </Button>
      </div>
    {/snippet}
  </PageHeader>
</div>

{#if loading}
  <div class="flex h-64 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('plans.loading')}
  </div>
{:else if error}
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
{:else if plans.length === 0}
  <EmptyState icon={IconMap} title={t('plans.emptyTitle')} description={t('plans.emptyDescription')} class="h-64">
    {#snippet action()}
      <Button variant="primary" size="sm" onclick={openForm}>
        <IconPlus size={14} />
        {t('plans.newPlan')}
      </Button>
    {/snippet}
  </EmptyState>
{:else}
  <div class="grid grid-cols-1 gap-4 px-6 pb-8 md:grid-cols-2 xl:grid-cols-3">
    {#each plans as plan (plan.id)}
      <button
        type="button"
        class="plan-card group text-left"
        onclick={() => navigate(`/plans/${plan.id}?tab=workspaces`)}
      >
        <div class="flex items-center gap-2">
          {#if plan.color}
            <span class="h-2.5 w-2.5 shrink-0 rounded-full" style="background: {plan.color}"></span>
          {/if}
          <span class="truncate text-sm font-semibold text-[var(--color-ds-text,#111827)] group-hover:text-[var(--color-ds-accent-blue,#2563eb)]">
            {plan.name}
          </span>
          <span class="ml-auto shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium {statusClass(plan.status)}">
            {statusLabel(plan.status)}
          </span>
        </div>
        {#if plan.description}
          <p class="mt-2 line-clamp-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{plan.description}</p>
        {/if}
        <div class="mt-3 flex items-center gap-3 text-[11px] text-[var(--color-ds-text-muted,#6b7280)]">
          <span>{plan.workspace_count || 0} {t('plans.workspacesWord')}</span>
          <span aria-hidden="true">·</span>
          <span>{plan.p3m_entity_count || 0} P3M</span>
          <span aria-hidden="true">·</span>
          <span>{plan.stakeholder_count || 0} {t('plans.stakeholdersWord')}</span>
        </div>
      </button>
    {/each}
  </div>
{/if}

{#if showForm}
  <Modal isOpen={true} maxWidth="max-w-lg" onclose={() => (showForm = false)}>
    <div class="px-8 py-6">
      <h2 class="mb-4 text-lg font-semibold">{t('plans.newPlan')}</h2>
      <form
        class="space-y-4"
        onsubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <TextField
          label={t('plans.name')}
          placeholder={t('plans.namePlaceholder')}
          bind:value={form.name}
          required
        />
        <div>
          <Label for="plan-status">{t('plans.status')}</Label>
          <select
            id="plan-status"
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
        <TextareaField
          label={t('plans.description')}
          placeholder={t('plans.descriptionPlaceholder')}
          bind:value={form.description}
          rows={3}
        />
        {#if formError}
          <p class="text-sm text-red-600">{formError}</p>
        {/if}
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="secondary" size="sm" onclick={() => (showForm = false)}>{t('common.cancel')}</Button>
          <Button variant="primary" size="sm" type="submit" disabled={saving}>{t('common.create')}</Button>
        </div>
      </form>
    </div>
  </Modal>
{/if}

<style>
  .plan-card {
    border: 1px solid var(--color-ds-border, #e5e7eb);
    border-radius: 8px;
    background: var(--color-ds-surface, #fff);
    padding: 1rem;
    transition:
      border-color 120ms ease,
      box-shadow 120ms ease;
  }

  .plan-card:hover {
    border-color: var(--color-ds-accent-blue, #3b82f6);
    box-shadow: 0 1px 4px rgb(0 0 0 / 0.08);
  }
</style>
