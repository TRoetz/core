<script>
  import { onMount } from 'svelte';
  import StateDisplay from '../components/StateDisplay.svelte';
  import { p3m as p3mApi } from '../api/p3m.js';
  import { t } from '../stores/i18n.svelte.js';
  import BellRing from '@lucide/svelte/icons/bell-ring';
  import Plus from '@lucide/svelte/icons/plus';
  import Edit from '@lucide/svelte/icons/edit';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Play from '@lucide/svelte/icons/play';
  import Button from '../components/Button.svelte';
  import Modal from '../dialogs/Modal.svelte';
  import ModalHeader from '../dialogs/ModalHeader.svelte';
  import Input from '../components/Input.svelte';
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

  const METRICS = [
    { value: 'forecast_overrun_pct', labelKey: 'settings.p3mAlertRules.metricForecastOverrun' },
    { value: 'variance_vs_budget', labelKey: 'settings.p3mAlertRules.metricVarianceVsBudget' },
    { value: 'commitment_vs_remaining', labelKey: 'settings.p3mAlertRules.metricCommitmentVsRemaining' },
  ];
  const COMPARATORS = ['>', '<', '>=', '<='];

  let rules = $state([]);
  let portfolios = $state([]);
  let projects = $state([]);
  let loading = $state(true);
  let error = $state(null);

  // Evaluate-now results
  let evaluating = $state(false);
  let evalResult = $state(null); // { evaluated_at, rules_evaluated, alerts }

  // Rule create/edit modal
  let showRuleModal = $state(false);
  let editingRule = $state(null);
  const emptyForm = () => ({
    name: '',
    scope: 'project',
    portfolio_id: '',
    project_id: '',
    metric: 'forecast_overrun_pct',
    comparator: '>',
    threshold: '10',
    notify_email: '',
    enabled: true,
  });
  let formData = $state(emptyForm());
  let saving = $state(false);

  const portfolioName = (id) => portfolios.find((p) => p.id === id)?.title ?? `#${id}`;
  const projectName = (id) => projects.find((p) => p.id === id)?.title ?? `#${id}`;

  const scopeLabel = (rule) => {
    if (rule.scope === 'portfolio' && rule.portfolio_id != null) return portfolioName(rule.portfolio_id);
    if (rule.project_id != null) return projectName(rule.project_id);
    return rule.scope;
  };

  const metricLabel = (metric) => t(METRICS.find((m) => m.value === metric)?.labelKey ?? metric);

  const loadAll = async () => {
    error = null;
    try {
      const [ruleRows, portRows, projRows] = await Promise.all([
        p3mApi.financeAlertRules.getAll(),
        p3mApi.portfolios.getAll(),
        p3mApi.projects.getAll(),
      ]);
      rules = Array.isArray(ruleRows) ? ruleRows : [];
      portfolios = Array.isArray(portRows) ? portRows : [];
      projects = Array.isArray(projRows) ? projRows : [];
    } catch (e) {
      error = e?.message || 'Failed to load alert rules';
    } finally {
      loading = false;
    }
  };

  onMount(() => void loadAll());

  const openCreateModal = () => {
    editingRule = null;
    formData = emptyForm();
    showRuleModal = true;
  };

  const openEditModal = (rule) => {
    editingRule = rule;
    formData = {
      name: rule.name ?? '',
      scope: rule.scope ?? 'project',
      portfolio_id: rule.portfolio_id != null ? String(rule.portfolio_id) : '',
      project_id: rule.project_id != null ? String(rule.project_id) : '',
      metric: rule.metric ?? 'forecast_overrun_pct',
      comparator: rule.comparator ?? '>',
      threshold: String(rule.threshold ?? 0),
      notify_email: rule.notify_email ?? '',
      enabled: !!rule.enabled,
    };
    showRuleModal = true;
  };

  const closeModals = () => { showRuleModal = false; };

  const handleSubmit = async () => {
    saving = true;
    try {
      const data = {
        name: formData.name.trim(),
        scope: formData.scope,
        portfolio_id: formData.scope === 'portfolio' && formData.portfolio_id ? Number(formData.portfolio_id) : null,
        project_id: formData.project_id ? Number(formData.project_id) : null,
        metric: formData.metric,
        comparator: formData.comparator,
        threshold: Number(formData.threshold) || 0,
        notify_email: formData.notify_email.trim() || null,
        enabled: !!formData.enabled,
      };
      if (editingRule) {
        await p3mApi.financeAlertRules.update(editingRule.id, data);
        successToast(t('settings.p3mAlertRules.ruleUpdated'), t('settings.p3mAlertRules.title'));
      } else {
        await p3mApi.financeAlertRules.create(data);
        successToast(t('settings.p3mAlertRules.ruleCreated'), t('settings.p3mAlertRules.title'));
      }
      showRuleModal = false;
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to save rule', t('settings.p3mAlertRules.title'));
    } finally {
      saving = false;
    }
  };

  const deleteRule = async (rule) => {
    const ok = await confirm({
      title: t('settings.p3mAlertRules.deleteRuleTitle'),
      message: t('settings.p3mAlertRules.deleteRuleMessage', { name: rule.name }),
      confirmText: t('common.delete'),
      variant: 'danger',
    });
    if (!ok) return;
    try {
      await p3mApi.financeAlertRules.delete(rule.id);
      successToast(t('settings.p3mAlertRules.ruleDeleted'), t('settings.p3mAlertRules.title'));
      await loadAll();
    } catch (e) {
      errorToast(e?.message || 'Failed to delete rule', t('settings.p3mAlertRules.title'));
    }
  };

  const evaluateNow = async () => {
    evaluating = true;
    try {
      evalResult = await p3mApi.evaluateAllFinanceAlerts();
      if (evalResult?.alerts?.length) {
        errorToast(t('settings.p3mAlertRules.alertsTriggered', { count: evalResult.alerts.length }), t('settings.p3mAlertRules.title'));
      } else {
        successToast(t('settings.p3mAlertRules.noAlerts'), t('settings.p3mAlertRules.title'));
      }
    } catch (e) {
      errorToast(e?.message || 'Evaluation failed', t('settings.p3mAlertRules.title'));
    } finally {
      evaluating = false;
    }
  };

  const formValid = $derived(
    formData.name.trim() &&
    Number(formData.threshold) >= 0 &&
    (formData.scope !== 'portfolio' || !!formData.portfolio_id)
  );
</script>

<PageHeader title={t('settings.p3mAlertRules.title')} subtitle={t('settings.p3mAlertRules.subtitle')} icon={BellRing}>
  {#snippet actions()}
    <Button variant="secondary" onclick={evaluateNow} disabled={evaluating || rules.length === 0} dataTestid="p3m-ar-evaluate">
      <Play class="w-4 h-4 mr-2" />
      {t('settings.p3mAlertRules.evaluateNow')}
    </Button>
    <Button variant="primary" onclick={openCreateModal} dataTestid="p3m-ar-add-rule">
      <Plus class="w-4 h-4 mr-2" />
      {t('settings.p3mAlertRules.addRule')}
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
  <!-- Triggered alerts from the last manual evaluation -->
  {#if evalResult}
    <Card shadow class="mb-4">
      <div class="p-4 space-y-2">
        <div class="flex items-center gap-2 text-xs" style="color: var(--ds-text-subtle);">
          <span>{t('settings.p3mAlertRules.lastEvaluation')}</span>
          <Lozenge color={evalResult.alerts?.length ? 'red' : 'green'}>
            {t('settings.p3mAlertRules.triggeredCount', { count: evalResult.alerts?.length ?? 0 })}
          </Lozenge>
        </div>
        {#if evalResult.alerts?.length}
          <ul class="space-y-1">
            {#each evalResult.alerts as a (a.rule_id + ':' + a.scope)}
              <li class="text-xs flex flex-wrap items-center gap-x-2" data-testid={`p3m-ar-alert-${a.rule_id}`}>
                <span class="font-medium" style="color: var(--ds-text);">{a.name}</span>
                <span style="color: var(--ds-text-subtle);">
                  {metricLabel(a.metric)} = <b>{a.value}</b> {a.comparator} {a.threshold}
                </span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="text-xs" style="color: var(--ds-text-subtle);">{t('settings.p3mAlertRules.noAlerts')}</p>
        {/if}
      </div>
    </Card>
  {/if}

  <!-- Rules -->
  {#if rules.length === 0}
    <EmptyState icon={BellRing} title={t('settings.p3mAlertRules.noRules')} description={t('settings.p3mAlertRules.getStarted')}>
      {#snippet action()}
        <Button variant="primary" icon={Plus} onclick={openCreateModal}>{t('settings.p3mAlertRules.addRule')}</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <Card shadow padding="none" class="divide-y">
      {#each rules as rule (rule.id)}
        <div class="p-4 flex items-center justify-between gap-3" data-testid={`p3m-ar-rule-${rule.id}`}>
          <div class="min-w-0">
            <div class="flex items-center space-x-2">
              <h3 class="text-sm font-medium truncate" style="color: var(--ds-text);">{rule.name}</h3>
              {#if rule.enabled}
                <Lozenge color="green">{t('settings.p3mAlertRules.enabled')}</Lozenge>
              {:else}
                <Lozenge color="gray">{t('settings.p3mAlertRules.disabled')}</Lozenge>
              {/if}
            </div>
            <DescriptionText class="mt-1">
              {rule.scope}: {scopeLabel(rule)} • {metricLabel(rule.metric)} {rule.comparator} {rule.threshold}
              {#if rule.notify_email}• {t('settings.p3mAlertRules.notify')}: {rule.notify_email}{/if}
            </DescriptionText>
          </div>
          <div class="flex items-center space-x-2 flex-shrink-0">
            <Button variant="ghost" size="sm" onclick={() => openEditModal(rule)} dataTestid={`p3m-ar-edit-${rule.id}`}>
              <Edit class="w-4 h-4" />
            </Button>
            <Button variant="danger-ghost" size="sm" icon={Trash2} title={t('common.delete')} onclick={() => deleteRule(rule)} dataTestid={`p3m-ar-delete-${rule.id}`}></Button>
          </div>
        </div>
      {/each}
    </Card>
  {/if}
{/if}

<!-- Rule create/edit modal -->
<Modal isOpen={showRuleModal} onclose={closeModals} maxWidth="max-w-2xl">
  <ModalHeader title={editingRule ? t('settings.p3mAlertRules.editRule') : t('settings.p3mAlertRules.addRule')} onClose={closeModals} />

  <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="p-4 space-y-4">
    <FormField label={t('settings.p3mAlertRules.name')}>
      <Input bind:value={formData.name} placeholder="Budget overrun watch" dataTestid="p3m-ar-name" />
    </FormField>

    <div class="grid grid-cols-2 gap-4">
      <FormField label={t('settings.p3mAlertRules.scope')}>
        <select bind:value={formData.scope} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-ar-scope">
          <option value="project">{t('settings.p3mAlertRules.project')}</option>
          <option value="portfolio">{t('settings.p3mAlertRules.portfolio')}</option>
        </select>
      </FormField>
      {#if formData.scope === 'portfolio'}
        <FormField label={t('settings.p3mAlertRules.portfolio')}>
          <select bind:value={formData.portfolio_id} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-ar-portfolio">
            <option value="">—</option>
            {#each portfolios as p (p.id)}<option value={String(p.id)}>{p.name}</option>{/each}
          </select>
        </FormField>
      {:else}
        <FormField label={t('settings.p3mAlertRules.project')}>
          <select bind:value={formData.project_id} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-ar-project">
            <option value="">—</option>
            {#each projects as p (p.id)}<option value={String(p.id)}>{p.name}</option>{/each}
          </select>
        </FormField>
      {/if}
    </div>

    <div class="grid grid-cols-3 gap-4">
      <FormField label={t('settings.p3mAlertRules.metric')}>
        <select bind:value={formData.metric} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-ar-metric">
          {#each METRICS as m (m.value)}<option value={m.value}>{t(m.labelKey)}</option>{/each}
        </select>
      </FormField>
      <FormField label={t('settings.p3mAlertRules.comparator')}>
        <select bind:value={formData.comparator} class="w-full rounded border px-2 py-1.5 text-sm" style="border-color: var(--ds-border);" dataTestid="p3m-ar-comparator">
          {#each COMPARATORS as c (c)}<option value={c}>{c}</option>{/each}
        </select>
      </FormField>
      <FormField label={t('settings.p3mAlertRules.threshold')}>
        <Input type="number" step="0.1" bind:value={formData.threshold} dataTestid="p3m-ar-threshold" />
      </FormField>
    </div>

    <FormField label={t('settings.p3mAlertRules.notifyEmail')} helper={t('settings.p3mAlertRules.notifyHelp')}>
      <Input bind:value={formData.notify_email} placeholder="finance@ohp.example, pm@ohp.example" dataTestid="p3m-ar-notify" />
    </FormField>

    <Checkbox bind:checked={formData.enabled} label={t('settings.p3mAlertRules.enabled')} dataTestid="p3m-ar-enabled" />

    <div class="flex justify-end gap-2">
      <Button variant="ghost" onclick={closeModals}>{t('common.cancel')}</Button>
      <Button variant="primary" type="submit" disabled={saving || !formValid}>{t('common.save')}</Button>
    </div>
  </form>
</Modal>
