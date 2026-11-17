<script>
  import { onMount } from 'svelte';
  import StateDisplay from '../components/StateDisplay.svelte';
  import { p3m as p3mApi } from '../api/p3m.js';
  import { t } from '../stores/i18n.svelte.js';
  import Database from '@lucide/svelte/icons/database';
  import Plus from '@lucide/svelte/icons/plus';
  import Edit from '@lucide/svelte/icons/edit';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Play from '@lucide/svelte/icons/play';
  import Upload from '@lucide/svelte/icons/upload';
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

  const PERIOD_FORMATS = ['YYYY-MM', 'YYYY-Q'];
  const MATCH_MODES = [
    { value: 'ref_only', labelKey: 'settings.p3mFinanceSync.matchRefOnly' },
    { value: 'ref_then_title', labelKey: 'settings.p3mFinanceSync.matchRefThenTitle' },
  ];

  let configs = $state([]);
  let runs = $state([]);
  let unmatched = $state([]);
  let projects = $state([]);
  let summary = $state(null);
  let loading = $state(true);
  let error = $state(null);

  // Config create/edit modal
  let showConfigModal = $state(false);
  let editingConfig = $state(null);
  const emptyForm = () => ({
    name: '',
    enabled: true,
    source_type: 'file_path',
    source_ref: '',
    schedule_minutes: '15',
    period_format: 'YYYY-MM',
    default_currency: 'GBP',
    match_mode: 'ref_only',
  });
  let formData = $state(emptyForm());
  let saving = $state(false);

  // Per-config busy flags (run now / upload)
  let runningId = $state(null);
  let uploadingId = $state(null);
  let fileInputEl = $state(null);
  let uploadTargetId = $state(null);

  // Unmatched row actions
  let assignProject = $state({});   // factId -> selected project id
  let rejectingId = $state(null);
  let rejectReason = $state('');

  const statusColor = (status) => {
    switch (status) {
      case 'success': return 'green';
      case 'partial': return 'yellow';
      case 'failed': return 'red';
      default: return 'gray';
    }
  };

  const fmtDate = (iso) => {
    if (!iso) return '';
    const d = new Date(iso);
    return isNaN(d.getTime()) ? iso : d.toLocaleString();
  };

  const loadAll = async () => {
    error = null;
    try {
      const [cfg, runRows, unmatch, proj] = await Promise.all([
        p3mApi.getFinanceSyncConfigs(),
        p3mApi.getFinanceSyncRuns({ limit: 20 }),
        p3mApi.getUnmatchedFinanceRows({ limit: 100 }),
        p3mApi.projects.getAll(),
      ]);
      configs = Array.isArray(cfg) ? cfg : [];
      runs = Array.isArray(runRows) ? runRows : [];
      unmatched = Array.isArray(unmatch) ? unmatch : [];
      projects = Array.isArray(proj) ? proj : [];
    } catch (e) {
      error = e?.message || 'Failed to load finance sync settings';
    } finally {
      loading = false;
    }
  };

  const loadSummary = async () => {
    try {
      summary = await p3mApi.getFinanceSyncSummary();
    } catch {
      // Non-fatal: the queue count below is authoritative for review.
      summary = null;
    }
  };

  onMount(() => {
    void loadAll();
    void loadSummary();
  });

  const openCreateModal = () => {
    editingConfig = null;
    formData = emptyForm();
    showConfigModal = true;
  };

  const openEditModal = (cfg) => {
    editingConfig = cfg;
    formData = {
      name: cfg.name ?? '',
      enabled: !!cfg.enabled,
      source_type: cfg.source_type ?? 'file_path',
      source_ref: cfg.source_ref ?? '',
      schedule_minutes: String(cfg.schedule_minutes ?? 15),
      period_format: cfg.period_format ?? 'YYYY-MM',
      default_currency: cfg.default_currency ?? 'GBP',
      match_mode: cfg.match_mode ?? 'ref_only',
    };
    showConfigModal = true;
  };

  const closeModals = () => { showConfigModal = false; };

  const handleSubmit = async () => {
    saving = true;
    try {
      const data = {
        name: formData.name.trim(),
        enabled: !!formData.enabled,
        source_type: formData.source_type,
        source_ref: formData.source_ref.trim(),
        schedule_minutes: Number(formData.schedule_minutes) || 0,
        period_format: formData.period_format,
        default_currency: formData.default_currency.trim() || 'GBP',
        match_mode: formData.match_mode,
      };
      if (editingConfig) {
        await p3mApi.updateFinanceSyncConfig(editingConfig.id, data);
        successToast(t('settings.p3mFinanceSync.configUpdated'), t('settings.p3mFinanceSync.title'));
      } else {
        await p3mApi.createFinanceSyncConfig(data);
        successToast(t('settings.p3mFinanceSync.configCreated'), t('settings.p3mFinanceSync.title'));
      }
      showConfigModal = false;
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to save config', t('settings.p3mFinanceSync.title'));
    } finally {
      saving = false;
    }
  };

  const deleteConfig = async (cfg) => {
    const ok = await confirm({
      title: t('settings.p3mFinanceSync.deleteConfigTitle'),
      message: t('settings.p3mFinanceSync.deleteConfigMessage', { name: cfg.name }),
      confirmText: t('common.delete'),
      variant: 'danger',
    });
    if (!ok) return;
    try {
      await p3mApi.deleteFinanceSyncConfig(cfg.id);
      successToast(t('settings.p3mFinanceSync.configDeleted'), t('settings.p3mFinanceSync.title'));
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to delete config', t('settings.p3mFinanceSync.title'));
    }
  };

  const runNow = async (cfg) => {
    runningId = cfg.id;
    try {
      await p3mApi.runFinanceSyncNow(cfg.id);
      successToast(t('settings.p3mFinanceSync.runStarted'), t('settings.p3mFinanceSync.title'));
      setTimeout(() => { void loadAll(); void loadSummary(); }, 1500);
    } catch (e) {
      errorToast(e?.message || 'Failed to start sync run', t('settings.p3mFinanceSync.title'));
    } finally {
      runningId = null;
    }
  };

  const triggerUpload = (cfg) => {
    uploadTargetId = cfg.id;
    fileInputEl?.click();
  };

  const onFileChosen = async (e) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file || !uploadTargetId) return;
    uploadingId = uploadTargetId;
    try {
      await p3mApi.uploadFinanceSyncCSV(uploadTargetId, file);
      successToast(t('settings.p3mFinanceSync.uploadDone'), t('settings.p3mFinanceSync.title'));
      await loadAll();
      void loadSummary();
    } catch (err) {
      errorToast(err?.message || 'Upload failed', t('settings.p3mFinanceSync.title'));
    } finally {
      uploadingId = null;
      uploadTargetId = null;
    }
  };

  const assignRow = async (fact, projectId) => {
    try {
      await p3mApi.assignUnmatchedFinanceRow(fact.id, { project_id: Number(projectId) });
      successToast(t('settings.p3mFinanceSync.rowAssigned'), t('settings.p3mFinanceSync.title'));
      assignProject[fact.id] = '';
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to assign row', t('settings.p3mFinanceSync.title'));
    }
  };

  const startReject = (factId) => {
    rejectingId = factId;
    rejectReason = '';
  };

  const confirmReject = async (fact) => {
    try {
      await p3mApi.rejectUnmatchedFinanceRow(fact.id, { reason: rejectReason.trim() });
      successToast(t('settings.p3mFinanceSync.rowRejected'), t('settings.p3mFinanceSync.title'));
      rejectingId = null;
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to reject row', t('settings.p3mFinanceSync.title'));
    }
  };

  const configName = $derived(Object.fromEntries(configs.map((c) => [c.id, c.name])));
</script>

<PageHeader title={t('settings.p3mFinanceSync.title')} subtitle={t('settings.p3mFinanceSync.subtitle')} icon={Database}>
  {#snippet actions()}
    <Button variant="primary" onclick={openCreateModal} dataTestid="p3m-fs-add-config">
      <Plus class="w-4 h-4 mr-2" />
      {t('settings.p3mFinanceSync.addConfig')}
    </Button>
  {/snippet}
</PageHeader>

<input type="file" accept=".csv,text/csv" class="hidden" bind:this={fileInputEl} onchange={onFileChosen} />

{#if error}
  <AlertBox type="error" dismissible ondismiss={() => (error = null)}>
    {error}
  </AlertBox>
{/if}

{#if loading}
  <StateDisplay type="loading" size="lg" />
{:else}
  <!-- Summary strip -->
  <div class="flex flex-wrap items-center gap-3 mb-4">
    <Lozenge color={unmatched.length ? 'yellow' : 'green'}>
      {t('settings.p3mFinanceSync.unmatchedCount', { count: unmatched.length })}
    </Lozenge>
    {#if summary?.synced_by_portfolio}
      {#each Object.entries(summary.synced_by_portfolio) as [pid, amt] (pid)}
        <span class="text-xs" style="color: var(--ds-text-subtle);">
          #{pid}: {Number(amt).toLocaleString()}
        </span>
      {/each}
    {/if}
  </div>

  <!-- Configs -->
  <h2 class="text-sm font-semibold mb-2" style="color: var(--ds-text);">{t('settings.p3mFinanceSync.configs')}</h2>
  {#if configs.length === 0}
    <EmptyState icon={Database} title={t('settings.p3mFinanceSync.noConfigs')} description={t('settings.p3mFinanceSync.getStarted')}>
      {#snippet action()}
        <Button variant="primary" icon={Plus} onclick={openCreateModal}>{t('settings.p3mFinanceSync.addConfig')}</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <Card shadow padding="none" class="divide-y">
      {#each configs as cfg (cfg.id)}
        <div class="p-4 flex items-center justify-between gap-3" data-testid={`p3m-fs-config-${cfg.id}`}>
          <div class="min-w-0">
            <div class="flex items-center space-x-2">
              <h3 class="text-sm font-medium truncate" style="color: var(--ds-text);">{cfg.name}</h3>
              {#if cfg.enabled}
                <Lozenge color="green">{t('settings.p3mFinanceSync.enabled')}</Lozenge>
              {:else}
                <Lozenge color="gray">{t('settings.p3mFinanceSync.disabled')}</Lozenge>
              {/if}
              {#if cfg.last_run_status}
                <Lozenge color={statusColor(cfg.last_run_status)}>{cfg.last_run_status}</Lozenge>
              {/if}
            </div>
            <DescriptionText class="mt-1">
              {cfg.source_type} • {cfg.schedule_minutes} min • {cfg.period_format} • {cfg.default_currency}
              {#if cfg.last_run_at}• {t('settings.p3mFinanceSync.lastRun')}: {fmtDate(cfg.last_run_at)}{/if}
            </DescriptionText>
          </div>
          <div class="flex items-center space-x-2 flex-shrink-0">
            <Button variant="ghost" size="sm" title={t('settings.p3mFinanceSync.runNow')} disabled={runningId === cfg.id || !cfg.enabled} onclick={() => runNow(cfg)} dataTestid={`p3m-fs-run-${cfg.id}`}>
              {#if runningId === cfg.id}<RefreshCw class="w-4 h-4 animate-spin" />{:else}<Play class="w-4 h-4" />{/if}
            </Button>
            <Button variant="ghost" size="sm" title={t('settings.p3mFinanceSync.uploadCSV')} disabled={uploadingId === cfg.id || !cfg.enabled} onclick={() => triggerUpload(cfg)} dataTestid={`p3m-fs-upload-${cfg.id}`}>
              {#if uploadingId === cfg.id}<RefreshCw class="w-4 h-4 animate-spin" />{:else}<Upload class="w-4 h-4" />{/if}
            </Button>
            <Button variant="ghost" size="sm" onclick={() => openEditModal(cfg)} dataTestid={`p3m-fs-edit-${cfg.id}`}>
              <Edit class="w-4 h-4" />
            </Button>
            <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('common.delete')} onclick={() => deleteConfig(cfg)} dataTestid={`p3m-fs-delete-${cfg.id}`}></Button>
          </div>
        </div>
      {/each}
    </Card>
  {/if}

  <!-- Recent runs -->
  <h2 class="text-sm font-semibold mt-6 mb-2" style="color: var(--ds-text);">{t('settings.p3mFinanceSync.recentRuns')}</h2>
  {#if runs.length === 0}
    <p class="text-xs" style="color: var(--ds-text-subtle);">{t('settings.p3mFinanceSync.noRuns')}</p>
  {:else}
    <Card shadow padding="none">
      <table class="w-full text-left text-sm">
        <thead>
          <tr class="border-b" style="border-color: var(--ds-border); color: var(--ds-text-subtle);" >
            <th class="p-3 text-xs font-medium">{t('settings.p3mFinanceSync.config')}</th>
            <th class="p-3 text-xs font-medium">{t('settings.p3mFinanceSync.trigger')}</th>
            <th class="p-3 text-xs font-medium">{t('settings.p3mFinanceSync.status')}</th>
            <th class="p-3 text-xs font-medium">{t('settings.p3mFinanceSync.rows')}</th>
            <th class="p-3 text-xs font-medium">{t('settings.p3mFinanceSync.started')}</th>
          </tr>
        </thead>
        <tbody>
          {#each runs as run (run.id)}
            <tr class="border-b last:border-b-0" style="border-color: var(--ds-border);" data-testid={`p3m-fs-run-row-${run.id}`}>
              <td class="p-3 text-xs">{configName[run.config_id] ?? `#${run.config_id}`}</td>
              <td class="p-3 text-xs capitalize">{run.trigger}</td>
              <td class="p-3"><Lozenge color={statusColor(run.status)}>{run.status}</Lozenge></td>
              <td class="p-3 text-xs" style="color: var(--ds-text-subtle);">
                {t('settings.p3mFinanceSync.rowsSummary', { total: run.rows_total, matched: run.rows_matched, unmatched: run.rows_unmatched, failed: run.rows_failed })}
              </td>
              <td class="p-3 text-xs">{fmtDate(run.started_at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </Card>
  {/if}

  <!-- Unmatched review queue -->
  <h2 class="text-sm font-semibold mt-6 mb-2" style="color: var(--ds-text);">{t('settings.p3mFinanceSync.reviewQueue')}</h2>
  {#if unmatched.length === 0}
    <p class="text-xs" style="color: var(--ds-text-subtle);">{t('settings.p3mFinanceSync.noUnmatched')}</p>
  {:else}
    <Card shadow padding="none" class="divide-y">
      {#each unmatched as fact (fact.id)}
        <div class="p-4 space-y-2" data-testid={`p3m-fs-unmatched-${fact.id}`}>
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
            <span class="font-medium" style="color: var(--ds-text);">{fact.external_ref}</span>
            <span style="color: var(--ds-text-subtle);">{fact.period} • {fact.line_type}</span>
            <span class="font-semibold">{Number(fact.amount).toLocaleString()} {fact.currency}</span>
            {#if fact.description}<span class="truncate max-w-xs" title={fact.description}>{fact.description}</span>{/if}
          </div>

          {#if rejectingId === fact.id}
            <div class="flex items-center gap-2">
              <Input bind:value={rejectReason} placeholder={t('settings.p3mFinanceSync.rejectReasonPlaceholder')} dataTestid={`p3m-fs-reject-reason-${fact.id}`} />
              <Button variant="danger" size="sm" disabled={!rejectReason.trim()} onclick={() => confirmReject(fact)}>{t('settings.p3mFinanceSync.confirmReject')}</Button>
              <Button variant="ghost" size="sm" onclick={() => (rejectingId = null)}>{t('common.cancel')}</Button>
            </div>
          {:else}
            <div class="flex flex-wrap items-center gap-2">
              {#if fact.candidates?.length}
                {#each fact.candidates as cand (cand.id)}
                  <button
                    class="rounded-full border px-2 py-0.5 text-xs hover:opacity-80"
                    style="border-color: var(--ds-border); color: var(--ds-text);"
                    onclick={() => assignRow(fact, cand.id)}
                    title={t('settings.p3mFinanceSync.assignTo', { name: cand.title })}
                  >→ {cand.title}</button>
                {/each}
              {/if}
              <select class="rounded border px-2 py-1 text-xs" style="border-color: var(--ds-border);" bind:value={assignProject[fact.id]} dataTestid={`p3m-fs-assign-${fact.id}`}>
                <option value="">—</option>
                {#each projects as p (p.id)}
                  <option value={p.id}>{p.title}</option>
                {/each}
              </select>
              <Button variant="ghost" size="sm" disabled={!assignProject[fact.id]} onclick={() => assignRow(fact, assignProject[fact.id])}>{t('settings.p3mFinanceSync.assign')}</Button>
              <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('settings.p3mFinanceSync.reject')} onclick={() => startReject(fact.id)}></Button>
            </div>
          {/if}
        </div>
      {/each}
    </Card>
  {/if}
{/if}

<!-- Config create/edit modal -->
<Modal isOpen={showConfigModal} onclose={closeModals} maxWidth="max-w-2xl">
  <ModalHeader title={editingConfig ? t('settings.p3mFinanceSync.editConfig') : t('settings.p3mFinanceSync.addConfig')} onClose={closeModals} />

  <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="p-4 space-y-4">
    <FormField label={t('settings.p3mFinanceSync.name')}>
      <Input bind:value={formData.name} placeholder="Monthly finance export" dataTestid="p3m-fs-name" />
    </FormField>

    <div class="grid grid-cols-2 gap-4">
      <FormField label={t('settings.p3mFinanceSync.sourceType')}>
        <select bind:value={formData.source_type} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-fs-source-type">
          <option value="file_path">{t('settings.p3mFinanceSync.filePath')}</option>
          <option value="s3">{t('settings.p3mFinanceSync.s3')}</option>
        </select>
      </FormField>
      <FormField label={t('settings.p3mFinanceSync.scheduleMinutes')} helper={t('settings.p3mFinanceSync.scheduleHelp')}>
        <Input type="number" min="1" bind:value={formData.schedule_minutes} dataTestid="p3m-fs-schedule" />
      </FormField>
    </div>

    <FormField label={t('settings.p3mFinanceSync.sourceRef')} helper={t('settings.p3mFinanceSync.sourceRefHelp')}>
      <Input bind:value={formData.source_ref} placeholder="/data/finance/export.csv or s3://bucket/key" dataTestid="p3m-fs-source-ref" />
    </FormField>

    <div class="grid grid-cols-3 gap-4">
      <FormField label={t('settings.p3mFinanceSync.periodFormat')}>
        <select bind:value={formData.period_format} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-fs-period-format">
          {#each PERIOD_FORMATS as pf (pf)}<option value={pf}>{pf}</option>{/each}
        </select>
      </FormField>
      <FormField label={t('settings.p3mFinanceSync.defaultCurrency')}>
        <Input bind:value={formData.default_currency} placeholder="GBP" dataTestid="p3m-fs-currency" />
      </FormField>
      <FormField label={t('settings.p3mFinanceSync.matchMode')}>
        <select bind:value={formData.match_mode} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-fs-match-mode">
          {#each MATCH_MODES as mm (mm.value)}<option value={mm.value}>{t(mm.labelKey)}</option>{/each}
        </select>
      </FormField>
    </div>

    <Checkbox bind:checked={formData.enabled} label={t('settings.p3mFinanceSync.enabled')} dataTestid="p3m-fs-enabled" />

    <div class="flex justify-end gap-2">
      <Button variant="ghost" onclick={closeModals}>{t('common.cancel')}</Button>
      <Button variant="primary" type="submit" disabled={saving || !formData.name.trim() || !formData.source_ref.trim()}>{t('common.save')}</Button>
    </div>
  </form>
</Modal>
