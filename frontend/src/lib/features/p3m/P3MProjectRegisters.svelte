<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { authStore } from '../../stores/auth.svelte.js';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { formatDate } from './utils.js';

  /** @type {{id: number, title: string}} */
  let { project = null } = $props();

  let items = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Creation forms; only one register form is open at a time.
  let showRiskForm = $state(false);
  let showDecisionForm = $state(false);
  let riskForm = $state({ title: '', description: '', impact_score: 3, probability_score: 3, mitigation_plan: '' });
  let decisionForm = $state({ title: '', description: '' });
  let saving = $state(false);

  const risks = $derived(items.filter((i) => i.item_type === 'risk'));
  const decisions = $derived(items.filter((i) => i.item_type === 'decision'));

  async function load() {
    if (!project?.id) return;
    loading = true;
    error = '';
    try {
      const r = await p3mApi.raidItems.getAll({ entity_type: 'project', entity_id: project.id });
      items = Array.isArray(r) ? r : [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    // Reload whenever the selected project changes.
    const id = project?.id;
    if (id !== undefined && id !== null) load();
  });

  async function submitRisk() {
    if (saving || !project) return;
    error = '';
    if (!riskForm.title.trim()) {
      error = t('p3m.titleRequired');
      return;
    }
    saving = true;
    try {
      await p3mApi.raidItems.create({
        entity_type: 'project',
        entity_id: project.id,
        item_type: 'risk',
        title: riskForm.title.trim(),
        description: riskForm.description,
        impact_score: Math.max(1, Math.min(5, Number(riskForm.impact_score) || 3)),
        probability_score: Math.max(1, Math.min(5, Number(riskForm.probability_score) || 3)),
        mitigation_plan: riskForm.mitigation_plan,
        owner_id: authStore.currentUser?.id,
      });
      riskForm = { title: '', description: '', impact_score: 3, probability_score: 3, mitigation_plan: '' };
      showRiskForm = false;
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  async function submitDecision() {
    if (saving || !project) return;
    error = '';
    if (!decisionForm.title.trim()) {
      error = t('p3m.titleRequired');
      return;
    }
    saving = true;
    try {
      await p3mApi.raidItems.create({
        entity_type: 'project',
        entity_id: project.id,
        item_type: 'decision',
        title: decisionForm.title.trim(),
        description: decisionForm.description,
        owner_id: authStore.currentUser?.id,
      });
      decisionForm = { title: '', description: '' };
      showDecisionForm = false;
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  // Full-replace update: send the whole row back with only status changed.
  async function toggleStatus(item) {
    if (saving) return;
    error = '';
    const next = item.status === 'closed' ? 'open' : 'closed';
    saving = true;
    try {
      await p3mApi.raidItems.update(item.id, { ...item, status: next });
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  const statusChip = (status) => {
    switch (status) {
      case 'open':
        return 'bg-red-100 text-red-700';
      case 'mitigating':
        return 'bg-amber-100 text-amber-700';
      case 'closed':
        return 'bg-emerald-100 text-emerald-700';
      default:
        return 'bg-slate-100 text-slate-600';
    }
  };

  const statusLabel = (status) => {
    switch (status) {
      case 'open':
        return t('p3m.statusOpen');
      case 'mitigating':
        return t('p3m.statusMitigating');
      case 'closed':
        return t('p3m.statusClosed');
      default:
        return status;
    }
  };
</script>

{#if loading}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error}
  <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
{:else}
  <!-- Risk register -->
  <h4 class="mb-1 mt-3 font-semibold">
    {t('p3m.riskRegister')} ({risks.length})
  </h4>
  {#if risks.length === 0 && !showRiskForm}
    <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      {t('p3m.noRisks')}
    </div>
  {:else}
    <table class="w-full text-left text-xs">
      <thead>
        <tr class="text-[var(--color-ds-text-muted,#6b7280)]">
          <th class="py-1 pr-3 font-medium">{t('p3m.titleLabel')}</th>
          <th class="py-1 pr-3 text-right font-medium">{t('p3m.impactScore')}</th>
          <th class="py-1 pr-3 text-right font-medium">{t('p3m.probabilityScore')}</th>
          <th class="py-1 pr-3 font-medium">{t('p3m.statusHeader')}</th>
          <th class="py-1"></th>
        </tr>
      </thead>
      <tbody>
        {#each risks as item (item.id)}
          <tr class="border-t border-[var(--color-ds-border,#e5e7eb)]">
            <td class="py-1.5 pr-3">
              <div class="font-medium">{item.title}</div>
              {#if item.description}
                <div class="mt-0.5 max-w-xl text-[var(--color-ds-text-muted,#6b7280)]">{item.description}</div>
              {/if}
            </td>
            <td class="py-1.5 pr-3 text-right tabular-nums">{item.impact_score}</td>
            <td class="py-1.5 pr-3 text-right tabular-nums">{item.probability_score}</td>
            <td class="py-1.5">
              <span class={statusChip(item.status)}>{statusLabel(item.status)}</span>
              {#if item.mitigation_plan}
                <div class="mt-0.5 max-w-xs text-[var(--color-ds-text-muted,#6b7280)]">{item.mitigation_plan}</div>
              {/if}
            </td>
            <td class="py-1.5 text-right">
              <button type="button" class="text-xs hover:underline disabled:opacity-50" disabled={saving} onclick={() => toggleStatus(item)}>
                {item.status === 'closed' ? t('p3m.reopen') : t('p3m.close')}
              </button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
  <button type="button" class="mt-1.5 flex items-center gap-1 text-xs font-medium hover:underline" onclick={() => (showRiskForm = !showRiskForm)}>
    {#if showRiskForm}<IconX size={12} />{t('p3m.cancel')}{:else}<IconPlus size={12} />{t('p3m.addRisk')}{/if}
  </button>

  {#if showRiskForm}
    <div class="mt-2 grid gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2 md:grid-cols-2">
      <input class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" placeholder={t('p3m.titleLabel')} bind:value={riskForm.title} />
      <div class="flex gap-2">
        <label class="flex items-center gap-1 text-xs">
          {t('p3m.impactScore')}
          <input type="number" min="1" max="5" class="w-14 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" bind:value={riskForm.impact_score} />
        </label>
        <label class="flex items-center gap-1 text-xs">
          {t('p3m.probabilityScore')}
          <input type="number" min="1" max="5" class="w-14 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" bind:value={riskForm.probability_score} />
        </label>
      </div>
      <textarea class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs md:col-span-2" rows="2" placeholder={t('p3m.description')} bind:value={riskForm.description} />
      <input class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs md:col-span-2" placeholder={t('p3m.mitigationPlan')} bind:value={riskForm.mitigation_plan} />
      <button type="button" class="rounded bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50" disabled={saving} onclick={submitRisk}>
        {t('p3m.save')}
      </button>
    </div>
  {/if}

  <!-- Decision register -->
  <h4 class="mb-1 mt-4 font-semibold">
    {t('p3m.decisionRegister')} ({decisions.length})
  </h4>
  {#if decisions.length === 0 && !showDecisionForm}
    <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
      {t('p3m.noDecisions')}
    </div>
  {:else}
    <table class="w-full text-left text-xs">
      <thead>
        <tr class="text-[var(--color-ds-text-muted,#6b7280)]">
          <th class="py-1 pr-3 font-medium">{t('p3m.titleLabel')}</th>
          <th class="py-1 pr-3 font-medium">{t('p3m.created')}</th>
          <th class="py-1"></th>
        </tr>
      </thead>
      <tbody>
        {#each decisions as item (item.id)}
          <tr class="border-t border-[var(--color-ds-border,#e5e7eb)]">
            <td class="py-1.5 pr-3">
              <div class="font-medium">{item.title}</div>
              {#if item.description}
                <div class="mt-0.5 max-w-xl whitespace-pre-wrap text-[var(--color-ds-text-muted,#6b7280)]">{item.description}</div>
              {/if}
            </td>
            <td class="py-1.5">
              <span class={statusChip(item.status)}>{statusLabel(item.status)}</span>
              <div class="mt-0.5 whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(item.created_at)}</div>
            </td>
            <td class="py-1.5 text-right">
              <button type="button" class="text-xs hover:underline disabled:opacity-50" disabled={saving} onclick={() => toggleStatus(item)}>
                {item.status === 'closed' ? t('p3m.reopen') : t('p3m.close')}
              </button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
  <button type="button" class="mt-1.5 flex items-center gap-1 text-xs font-medium hover:underline" onclick={() => (showDecisionForm = !showDecisionForm)}>
    {#if showDecisionForm}<IconX size={12} />{t('p3m.cancel')}{:else}<IconPlus size={12} />{t('p3m.addDecision')}{/if}
  </button>

  {#if showDecisionForm}
    <div class="mt-2 grid gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2">
      <input class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" placeholder={t('p3m.titleLabel')} bind:value={decisionForm.title} />
      <textarea class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" rows="2" placeholder={t('p3m.description')} bind:value={decisionForm.description} />
      <button type="button" class="rounded bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50" disabled={saving} onclick={submitDecision}>
        {t('p3m.save')}
      </button>
    </div>
  {/if}
{/if}
