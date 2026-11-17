<script>
  import { onMount } from 'svelte';
  import StateDisplay from '../components/StateDisplay.svelte';
  import { p3m as p3mApi } from '../api/p3m.js';
  import { t } from '../stores/i18n.svelte.js';
  import ClipboardCheck from '@lucide/svelte/icons/clipboard-check';
  import Plus from '@lucide/svelte/icons/plus';
  import Edit from '@lucide/svelte/icons/edit';
  import Trash2 from '@lucide/svelte/icons/trash-2';
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

  const WORK_TYPE_LABELS = {
    '': 'settings.p3mGateTemplates.allWorkTypes',
    programme: 'p3m.workTypeProgramme',
    project: 'p3m.workTypeProject',
    continuous_improvement: 'p3m.workTypeContinuousImprovement',
    digital_product: 'p3m.workTypeDigitalProduct',
    operational: 'p3m.workTypeOperational',
  };

  let stages = $state([]);
  let templates = $state([]);
  let loading = $state(true);
  let error = $state(null);

  const stageName = (key) => {
    if (!key || key === '*') return t('settings.p3mGateTemplates.anyStage');
    return stages.find((s) => s.stage_key === key)?.name ?? key;
  };

  // Template create/edit modal.
  let showTemplateModal = $state(false);
  /** @type {any|null} */
  let editingTemplate = $state(null);
  const emptyForm = () => ({
    name: '',
    from_stage_key: '',
    to_stage_key: '',
    work_type: '',
    mandatory: true,
    enabled: true,
    description: '',
    artifacts: [], // [{name, required, description}]
  });
  let formData = $state(emptyForm());
  let saving = $state(false);

  const loadAll = async () => {
    error = null;
    try {
      const cfg = await p3mApi.getLifecycleConfig();
      stages = Array.isArray(cfg?.stages) ? [...cfg.stages].sort((a, b) => a.sort_order - b.sort_order) : [];
      templates = Array.isArray(cfg?.gate_templates) ? cfg.gate_templates : [];
    } catch (e) {
      error = e?.message || 'Failed to load gate templates';
    } finally {
      loading = false;
    }
  };

  onMount(() => {
    void loadAll();
  });

  const openCreateModal = () => {
    editingTemplate = null;
    formData = emptyForm();
    if (stages.length) formData.to_stage_key = stages[0].stage_key;
    showTemplateModal = true;
  };

  const openEditModal = (tpl) => {
    editingTemplate = tpl;
    formData = {
      name: tpl.name ?? '',
      from_stage_key: tpl.from_stage_key ?? '',
      to_stage_key: tpl.to_stage_key ?? '',
      work_type: tpl.work_type ?? '',
      mandatory: !!tpl.mandatory,
      enabled: !!tpl.enabled,
      description: tpl.description ?? '',
      artifacts: Array.isArray(tpl.required_artifacts)
        ? tpl.required_artifacts.map((a) => ({ name: a.name ?? '', required: !!a.required, description: a.description ?? '' }))
        : [],
    };
    showTemplateModal = true;
  };

  const closeModals = () => { showTemplateModal = false; };

  const addArtifactRow = () => {
    formData.artifacts = [...formData.artifacts, { name: '', required: true, description: '' }];
  };

  const removeArtifactRow = (index) => {
    formData.artifacts = formData.artifacts.filter((_, i) => i !== index);
  };

  const setArtifactField = (index, field, value) => {
    formData.artifacts = formData.artifacts.map((a, i) => (i === index ? { ...a, [field]: value } : a));
  };

  const handleSubmit = async () => {
    saving = true;
    try {
      const data = {
        name: formData.name.trim(),
        from_stage_key: formData.from_stage_key || null,
        to_stage_key: formData.to_stage_key,
        work_type: formData.work_type || null,
        mandatory: !!formData.mandatory,
        enabled: !!formData.enabled,
        description: formData.description,
        required_artifacts: formData.artifacts.filter((a) => a.name.trim()).map((a) => ({
          name: a.name.trim(),
          required: !!a.required,
          ...(a.description?.trim() ? { description: a.description.trim() } : {}),
        })),
      };
      if (editingTemplate) {
        await p3mApi.gateTemplates.update(editingTemplate.id, data);
        successToast(t('settings.p3mGateTemplates.templateUpdated'), t('settings.p3mGateTemplates.title'));
      } else {
        await p3mApi.gateTemplates.create(data);
        successToast(t('settings.p3mGateTemplates.templateCreated'), t('settings.p3mGateTemplates.title'));
      }
      showTemplateModal = false;
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to save gate template', t('settings.p3mGateTemplates.title'));
    } finally {
      saving = false;
    }
  };

  const deleteTemplate = async (tpl) => {
    const ok = await confirm({
      title: t('settings.p3mGateTemplates.deleteTemplateTitle'),
      message: t('settings.p3mGateTemplates.deleteTemplateMessage', { name: tpl.name }),
      confirmText: t('common.delete'),
      variant: 'danger',
    });
    if (!ok) return;
    try {
      await p3mApi.gateTemplates.delete(tpl.id);
      successToast(t('settings.p3mGateTemplates.templateDeleted'), t('settings.p3mGateTemplates.title'));
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to delete gate template', t('settings.p3mGateTemplates.title'));
    }
  };
</script>

<PageHeader title={t('settings.p3mGateTemplates.title')} subtitle={t('settings.p3mGateTemplates.subtitle')} icon={ClipboardCheck}>
  {#snippet actions()}
    <Button variant="primary" onclick={openCreateModal} dataTestid="p3m-gate-tpl-add">
      <Plus class="w-4 h-4 mr-2" />
      {t('settings.p3mGateTemplates.addTemplate')}
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
{:else if templates.length === 0}
  <EmptyState icon={ClipboardCheck} title={t('settings.p3mGateTemplates.noTemplates')} description={t('settings.p3mGateTemplates.getStarted')}>
    {#snippet action()}
      <Button variant="primary" icon={Plus} onclick={openCreateModal}>{t('settings.p3mGateTemplates.addTemplate')}</Button>
    {/snippet}
  </EmptyState>
{:else}
  <Card shadow padding="none" class="divide-y">
    {#each templates as tpl (tpl.id)}
      <div class="p-4 flex items-center justify-between gap-3" data-testid={`p3m-gate-tpl-${tpl.id}`}>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 class="text-sm font-medium truncate" style="color: var(--ds-text);">{tpl.name}</h3>
            {#if tpl.mandatory}
              <Lozenge color="yellow">{t('settings.p3mGateTemplates.mandatory')}</Lozenge>
            {:else}
              <Lozenge color="gray">{t('settings.p3mGateTemplates.optional')}</Lozenge>
            {/if}
            {#if tpl.enabled}
              <Lozenge color="green">{t('settings.p3mGateTemplates.enabled')}</Lozenge>
            {:else}
              <Lozenge color="gray">{t('settings.p3mGateTemplates.disabled')}</Lozenge>
            {/if}
          </div>
          <DescriptionText class="mt-1">
            {stageName(tpl.from_stage_key)} → {stageName(tpl.to_stage_key)} • {t(WORK_TYPE_LABELS[tpl.work_type ?? ''] ?? 'settings.p3mGateTemplates.allWorkTypes')}
            {#if tpl.required_artifacts?.length}• {t('settings.p3mGateTemplates.artifactCount', { count: tpl.required_artifacts.length })}{/if}
          </DescriptionText>
        </div>
        <div class="flex items-center space-x-2 flex-shrink-0">
          <Button variant="ghost" size="sm" title={t('common.edit')} onclick={() => openEditModal(tpl)} dataTestid={`p3m-gate-tpl-edit-${tpl.id}`}>
            <Edit class="w-4 h-4" />
          </Button>
          <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('common.delete')} onclick={() => deleteTemplate(tpl)} dataTestid={`p3m-gate-tpl-delete-${tpl.id}`}></Button>
        </div>
      </div>
    {/each}
  </Card>
{/if}

<!-- Template create/edit modal -->
<Modal isOpen={showTemplateModal} onclose={closeModals} maxWidth="max-w-2xl">
  <ModalHeader title={editingTemplate ? t('settings.p3mGateTemplates.editTemplate') : t('settings.p3mGateTemplates.addTemplate')} onClose={closeModals} />

  <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="p-4 space-y-4">
    <FormField label={t('settings.p3mGateTemplates.name')}>
      <Input bind:value={formData.name} placeholder="Planning gate" dataTestid="p3m-gate-tpl-name" />
    </FormField>

    <div class="grid grid-cols-2 gap-4">
      <FormField label={t('settings.p3mGateTemplates.fromStage')} helper={t('settings.p3mGateTemplates.fromStageHelp')}>
        <select bind:value={formData.from_stage_key} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-gate-tpl-from">
          <option value="">{t('settings.p3mGateTemplates.anyStage')}</option>
          {#each stages as stage (stage.stage_key)}
            <option value={stage.stage_key}>{stage.name}</option>
          {/each}
        </select>
      </FormField>
      <FormField label={t('settings.p3mGateTemplates.toStage')}>
        <select bind:value={formData.to_stage_key} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-gate-tpl-to">
          {#each stages as stage (stage.stage_key)}
            <option value={stage.stage_key}>{stage.name}</option>
          {/each}
        </select>
      </FormField>
    </div>

    <div class="grid grid-cols-2 gap-4">
      <FormField label={t('settings.p3mGateTemplates.workType')}>
        <select bind:value={formData.work_type} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-gate-tpl-work-type">
          {#each Object.entries(WORK_TYPE_LABELS) as [value, labelKey] (value)}
            <option value={value}>{t(labelKey)}</option>
          {/each}
        </select>
      </FormField>
      <div class="space-y-2 pt-6">
        <Checkbox bind:checked={formData.mandatory} label={t('settings.p3mGateTemplates.mandatoryLabel')} dataTestid="p3m-gate-tpl-mandatory" />
        <Checkbox bind:checked={formData.enabled} label={t('settings.p3mGateTemplates.enabled')} dataTestid="p3m-gate-tpl-enabled" />
      </div>
    </div>

    <FormField label={t('settings.p3mGateTemplates.description')}>
      <Textarea rows="2" bind:value={formData.description} dataTestid="p3m-gate-tpl-description" />
    </FormField>

    <!-- Artifact checklist builder -->
    <div>
      <div class="mb-1 flex items-center justify-between">
        <span class="text-xs font-medium" style="color: var(--ds-text);">{t('settings.p3mGateTemplates.artifacts')}</span>
        <Button variant="ghost" size="sm" icon={Plus} onclick={addArtifactRow}>{t('settings.p3mGateTemplates.addArtifact')}</Button>
      </div>
      {#if formData.artifacts.length === 0}
        <p class="text-xs" style="color: var(--ds-text-subtle);">{t('settings.p3mGateTemplates.noArtifacts')}</p>
      {:else}
        <div class="space-y-2">
          {#each formData.artifacts as artifact, i (i)}
            <div class="flex items-center gap-2" data-testid={`p3m-gate-tpl-artifact-${i}`}>
              <Input bind:value={artifact.name} placeholder={t('settings.p3mGateTemplates.artifactNamePlaceholder')} class="flex-1" />
              <Input bind:value={artifact.description} placeholder={t('settings.p3mGateTemplates.artifactDescription')} class="flex-1" />
              <Checkbox checked={artifact.required} onchange={(e) => setArtifactField(i, 'required', e.target.checked)} label={t('settings.p3mGateTemplates.artifactRequired')} dataTestid={`p3m-gate-tpl-artifact-required-${i}`} />
              <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('common.delete')} onclick={() => removeArtifactRow(i)}></Button>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="flex justify-end gap-2">
      <Button variant="ghost" onclick={closeModals}>{t('common.cancel')}</Button>
      <Button variant="primary" type="submit" disabled={saving || !formData.name.trim() || !formData.to_stage_key}>
        {t('common.save')}
      </Button>
    </div>
  </form>
</Modal>
