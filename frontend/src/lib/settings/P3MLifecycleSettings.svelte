<script>
  import { onMount } from 'svelte';
  import StateDisplay from '../components/StateDisplay.svelte';
  import { p3m as p3mApi } from '../api/p3m.js';
  import { t } from '../stores/i18n.svelte.js';
  import GitBranch from '@lucide/svelte/icons/git-branch';
  import Plus from '@lucide/svelte/icons/plus';
  import Edit from '@lucide/svelte/icons/edit';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import Button from '../components/Button.svelte';
  import Modal from '../dialogs/Modal.svelte';
  import ModalHeader from '../dialogs/ModalHeader.svelte';
  import Input from '../components/Input.svelte';
  import Textarea from '../components/Textarea.svelte';
  import FormField from '../components/FormField.svelte';
  import AlertBox from '../components/AlertBox.svelte';
  import Lozenge from '../components/Lozenge.svelte';
  import Checkbox from '../components/Checkbox.svelte';
  import { successToast, errorToast } from '../stores/toasts.svelte.js';
  import PageHeader from '../layout/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import DescriptionText from '../components/DescriptionText.svelte';
  import { confirm } from '../composables/useConfirm.js';

  let stages = $state([]);
  let previews = $state([]);
  let loading = $state(true);
  let error = $state(null);
  let busyKey = $state(null); // stage key with an in-flight mutation

  const WORK_TYPE_LABELS = {
    '': 'settings.p3mLifecycle.defaultPath',
    programme: 'p3m.workTypeProgramme',
    project: 'p3m.workTypeProject',
    continuous_improvement: 'p3m.workTypeContinuousImprovement',
    digital_product: 'p3m.workTypeDigitalProduct',
    operational: 'p3m.workTypeOperational',
  };

  const stageName = $derived(Object.fromEntries(stages.map((s) => [s.stage_key, s.name])));

  // Stage create/edit modal. The key is immutable after creation.
  let showStageModal = $state(false);
  let editingKey = $state(null); // null = creating
  const emptyForm = () => ({
    stage_key: '',
    name: '',
    description: '',
    sort_order: '0',
    is_entry: false,
    is_exit: false,
    gate_required: true,
    skippable: false,
    enabled: true,
  });
  let formData = $state(emptyForm());
  let saving = $state(false);

  const loadAll = async () => {
    error = null;
    try {
      const cfg = await p3mApi.getLifecycleConfig();
      stages = Array.isArray(cfg?.stages) ? [...cfg.stages].sort((a, b) => a.sort_order - b.sort_order) : [];
      previews = Array.isArray(cfg?.path_previews) ? cfg.path_previews : [];
    } catch (e) {
      error = e?.message || 'Failed to load lifecycle configuration';
    } finally {
      loading = false;
    }
  };

  onMount(() => {
    void loadAll();
  });

  const openCreateModal = () => {
    editingKey = null;
    formData = emptyForm();
    formData.sort_order = String(stages.length ? Math.max(...stages.map((s) => s.sort_order)) + 1 : 0);
    showStageModal = true;
  };

  const openEditModal = (stage) => {
    editingKey = stage.stage_key;
    formData = {
      stage_key: stage.stage_key,
      name: stage.name ?? '',
      description: stage.description ?? '',
      sort_order: String(stage.sort_order),
      is_entry: !!stage.is_entry,
      is_exit: !!stage.is_exit,
      gate_required: !!stage.gate_required,
      skippable: !!stage.skippable,
      enabled: !!stage.enabled,
    };
    showStageModal = true;
  };

  const closeModals = () => { showStageModal = false; };

  // The update endpoint takes the full stage object (non-pointer bools), so
  // every mutation sends all fields.
  const payloadFromForm = () => ({
    name: formData.name.trim(),
    description: formData.description,
    sort_order: Number(formData.sort_order) || 0,
    is_entry: !!formData.is_entry,
    is_exit: !!formData.is_exit,
    gate_required: !!formData.gate_required,
    skippable: !!formData.skippable,
    enabled: !!formData.enabled,
  });

  const handleSubmit = async () => {
    saving = true;
    try {
      if (editingKey) {
        await p3mApi.updateLifecycleStage(editingKey, payloadFromForm());
        successToast(t('settings.p3mLifecycle.stageUpdated'), t('settings.p3mLifecycle.title'));
      } else {
        await p3mApi.createLifecycleStage({ ...payloadFromForm(), stage_key: formData.stage_key.trim() });
        successToast(t('settings.p3mLifecycle.stageCreated'), t('settings.p3mLifecycle.title'));
      }
      showStageModal = false;
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to save stage', t('settings.p3mLifecycle.title'));
    } finally {
      saving = false;
    }
  };

  const deleteStage = async (stage) => {
    const ok = await confirm({
      title: t('settings.p3mLifecycle.deleteStageTitle'),
      message: t('settings.p3mLifecycle.deleteStageMessage', { key: stage.stage_key }),
      confirmText: t('common.delete'),
      variant: 'danger',
    });
    if (!ok) return;
    try {
      await p3mApi.deleteLifecycleStage(stage.stage_key);
      successToast(t('settings.p3mLifecycle.stageDeleted'), t('settings.p3mLifecycle.title'));
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to delete stage', t('settings.p3mLifecycle.title'));
    }
  };

  // Reorder by swapping sort_order with the adjacent row, then persisting both.
  const moveStage = async (index, dir) => {
    const j = index + dir;
    if (j < 0 || j >= stages.length) return;
    const a = stages[index];
    const b = stages[j];
    busyKey = `${a.stage_key}|${b.stage_key}`;
    try {
      await Promise.all([
        p3mApi.updateLifecycleStage(a.stage_key, {
          name: a.name, description: a.description, sort_order: b.sort_order,
          is_entry: a.is_entry, is_exit: a.is_exit, gate_required: a.gate_required,
          skippable: a.skippable, enabled: a.enabled,
        }),
        p3mApi.updateLifecycleStage(b.stage_key, {
          name: b.name, description: b.description, sort_order: a.sort_order,
          is_entry: b.is_entry, is_exit: b.is_exit, gate_required: b.gate_required,
          skippable: b.skippable, enabled: b.enabled,
        }),
      ]);
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to reorder stages', t('settings.p3mLifecycle.title'));
    } finally {
      busyKey = null;
    }
  };

  const isBusy = (key) => busyKey !== null && busyKey.includes(key);
</script>

<PageHeader title={t('settings.p3mLifecycle.title')} subtitle={t('settings.p3mLifecycle.subtitle')} icon={GitBranch}>
  {#snippet actions()}
    <Button variant="primary" onclick={openCreateModal} dataTestid="p3m-lifecycle-add-stage">
      <Plus class="w-4 h-4 mr-2" />
      {t('settings.p3mLifecycle.addStage')}
    </Button>
  {/snippet}
</PageHeader>

{#if error}
  <AlertBox type="error" dismissible ondismiss={() => (error = null)}>
    {error}
  </AlertBox>
{/if}

{#if loading}
  <StateDisplay type="loading" size="lg" />
{:else}
  <!-- Stages -->
  <h2 class="text-sm font-semibold mb-2" style="color: var(--ds-text);">{t('settings.p3mLifecycle.stages')}</h2>
  {#if stages.length === 0}
    <EmptyState icon={GitBranch} title={t('settings.p3mLifecycle.noStages')} description={t('settings.p3mLifecycle.getStarted')}>
      {#snippet action()}
        <Button variant="primary" icon={Plus} onclick={openCreateModal}>{t('settings.p3mLifecycle.addStage')}</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <Card shadow padding="none" class="divide-y">
      {#each stages as stage, i (stage.stage_key)}
        <div class="p-4 flex items-center justify-between gap-3" data-testid={`p3m-lifecycle-stage-${stage.stage_key}`}>
          <div class="min-w-0 flex items-center gap-2">
            <div class="flex flex-col space-y-0.5 flex-shrink-0">
              <Button variant="ghost" size="sm" title={t('settings.p3mLifecycle.moveUp')} disabled={i === 0 || isBusy(stage.stage_key)} onclick={() => moveStage(i, -1)}>
                <ArrowUp class="w-3.5 h-3.5" />
              </Button>
              <Button variant="ghost" size="sm" title={t('settings.p3mLifecycle.moveDown')} disabled={i === stages.length - 1 || isBusy(stage.stage_key)} onclick={() => moveStage(i, 1)}>
                <ArrowDown class="w-3.5 h-3.5" />
              </Button>
            </div>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                <h3 class="text-sm font-medium truncate" style="color: var(--ds-text);">{stage.name}</h3>
                <code class="rounded px-1 text-xs" style="background: var(--ds-surface-subtle, rgba(0,0,0,0.05)); color: var(--ds-text-subtle);">{stage.stage_key}</code>
                {#if stage.is_entry}<Lozenge color="blue">{t('settings.p3mLifecycle.entry')}</Lozenge>{/if}
                {#if stage.is_exit}<Lozenge color="purple">{t('settings.p3mLifecycle.exit')}</Lozenge>{/if}
                {#if stage.gate_required}<Lozenge color="yellow">{t('settings.p3mLifecycle.gateRequired')}</Lozenge>{/if}
                {#if stage.skippable}<Lozenge color="gray">{t('settings.p3mLifecycle.skippable')}</Lozenge>{/if}
              </div>
              <DescriptionText class="mt-1">
                {stage.enabled ? t('settings.p3mLifecycle.enabled') : t('settings.p3mLifecycle.disabled')}
                {#if stage.description}• {stage.description}{/if}
              </DescriptionText>
            </div>
          </div>
          <div class="flex items-center space-x-2 flex-shrink-0">
            <Button variant="ghost" size="sm" title={t('common.edit')} onclick={() => openEditModal(stage)} dataTestid={`p3m-lifecycle-edit-${stage.stage_key}`}>
              <Edit class="w-4 h-4" />
            </Button>
            <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('common.delete')} onclick={() => deleteStage(stage)} dataTestid={`p3m-lifecycle-delete-${stage.stage_key}`}></Button>
          </div>
        </div>
      {/each}
    </Card>
  {/if}

  <!-- Path previews -->
  <h2 class="text-sm font-semibold mt-6 mb-2" style="color: var(--ds-text);">{t('settings.p3mLifecycle.pathPreviews')}</h2>
  <Card shadow padding="none" class="divide-y">
    {#each previews as preview (preview.work_type)}
      <div class="p-4 space-y-1.5" data-testid={`p3m-lifecycle-preview-${preview.work_type || 'default'}`}>
        <div class="text-xs font-medium" style="color: var(--ds-text);">
          {t(WORK_TYPE_LABELS[preview.work_type] ?? 'settings.p3mLifecycle.defaultPath')}
        </div>
        <div class="flex flex-wrap items-center gap-1 text-xs" style="color: var(--ds-text-subtle);">
          {#each preview.stage_keys as key, i (key)}
            {#if i > 0}<span aria-hidden="true">→</span>{/if}
            <span class="rounded px-1.5 py-0.5" style="background: var(--ds-surface-subtle, rgba(0,0,0,0.05)); color: var(--ds-text);">{stageName[key] ?? key}</span>
          {/each}
        </div>
        <div class="text-xs">
          <span style="color: var(--ds-text-subtle);">{t('settings.p3mLifecycle.mandatoryGates')}:</span>
          {#if preview.mandatory_gates.length === 0}
            <span style="color: var(--ds-text-subtle);">{t('settings.p3mLifecycle.noMandatoryGates')}</span>
          {:else}
            {#each preview.mandatory_gates as gate (gate)}
              <code class="ml-1 rounded px-1" style="background: var(--ds-surface-subtle, rgba(0,0,0,0.05)); color: var(--ds-text);">{gate}</code>
            {/each}
          {/if}
        </div>
      </div>
    {/each}
  </Card>
{/if}

<!-- Stage create/edit modal -->
<Modal isOpen={showStageModal} onclose={closeModals} maxWidth="max-w-2xl">
  <ModalHeader title={editingKey ? t('settings.p3mLifecycle.editStage') : t('settings.p3mLifecycle.addStage')} onClose={closeModals} />

  <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="p-4 space-y-4">
    {#if !editingKey}
      <FormField label={t('settings.p3mLifecycle.stageKey')} helper={t('settings.p3mLifecycle.stageKeyHelp')}>
        <Input bind:value={formData.stage_key} placeholder="execution" dataTestid="p3m-lifecycle-key" />
      </FormField>
    {:else}
      <FormField label={t('settings.p3mLifecycle.stageKey')} helper={t('settings.p3mLifecycle.stageKeyImmutable')}>
        <Input value={formData.stage_key} disabled dataTestid="p3m-lifecycle-key-readonly" />
      </FormField>
    {/if}

    <div class="grid grid-cols-2 gap-4">
      <FormField label={t('settings.p3mLifecycle.name')}>
        <Input bind:value={formData.name} placeholder="Execution" dataTestid="p3m-lifecycle-name" />
      </FormField>
      <FormField label={t('settings.p3mLifecycle.sortOrder')} helper={t('settings.p3mLifecycle.sortOrderHelp')}>
        <Input type="number" min="0" bind:value={formData.sort_order} dataTestid="p3m-lifecycle-sort-order" />
      </FormField>
    </div>

    <FormField label={t('settings.p3mLifecycle.description')}>
      <Textarea rows="2" bind:value={formData.description} dataTestid="p3m-lifecycle-description" />
    </FormField>

    <div class="grid grid-cols-2 gap-x-4 gap-y-2">
      <Checkbox bind:checked={formData.is_entry} label={t('settings.p3mLifecycle.entryStage')} dataTestid="p3m-lifecycle-entry" />
      <Checkbox bind:checked={formData.is_exit} label={t('settings.p3mLifecycle.exitStage')} dataTestid="p3m-lifecycle-exit" />
      <Checkbox bind:checked={formData.gate_required} label={t('settings.p3mLifecycle.gateRequiredLabel')} dataTestid="p3m-lifecycle-gate-required" />
      <Checkbox bind:checked={formData.skippable} label={t('settings.p3mLifecycle.skippableLabel')} dataTestid="p3m-lifecycle-skippable" />
      <Checkbox bind:checked={formData.enabled} label={t('settings.p3mLifecycle.enabled')} dataTestid="p3m-lifecycle-enabled" />
    </div>

    <div class="flex justify-end gap-2">
      <Button variant="ghost" onclick={closeModals}>{t('common.cancel')}</Button>
      <Button variant="primary" type="submit" disabled={saving || !formData.name.trim() || (!editingKey && !formData.stage_key.trim())}>
        {t('common.save')}
      </Button>
    </div>
  </form>
</Modal>
