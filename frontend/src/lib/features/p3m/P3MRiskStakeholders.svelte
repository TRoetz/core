<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { formatDate, quadrantLabel } from './utils.js';

  let { portfolios = [] } = $props();

  let raidItems = $state([]);
  let stakeholders = $state([]);
  let communications = $state([]);
  let loading = $state(true);
  let error = $state('');

  const entityTitleById = $derived.by(() => {
    const m = new Map();
    for (const pf of portfolios) {
      m.set(`portfolio:${pf.id}`, pf.title);
      for (const pg of pf.programmes ?? []) {
        m.set(`programme:${pg.id}`, `${pf.title} › ${pg.title}`);
        for (const pr of pg.projects ?? []) m.set(`project:${pr.id}`, `${pf.title} › ${pg.title} › ${pr.title}`);
      }
    }
    return m;
  });

  function entityLabel(e) {
    return entityTitleById.get(`${e.entity_type}:${e.entity_id}`) ?? `#${e.entity_id}`;
  }

  const statusChip = (status) => {
    switch (status) {
      case 'open':
        return 'bg-red-100 text-red-700';
      case 'mitigating':
        return 'bg-amber-100 text-amber-700';
      case 'closed':
        return 'bg-emerald-100 text-emerald-700';
      case 'escalated':
        return 'bg-purple-100 text-purple-700';
      default:
        return 'bg-slate-100 text-slate-600';
    }
  };

  const bucketChip = (bucket) => {
    switch (bucket) {
      case 'high':
        return 'bg-red-100 text-red-700';
      case 'medium':
        return 'bg-amber-100 text-amber-700';
      default:
        return 'bg-emerald-100 text-emerald-700';
    }
  };

  const quadrantColor = (quadrant) => {
    switch (quadrant) {
      case 'manage_closely':
        return '#ef4444';
      case 'keep_satisfied':
        return '#f59e0b';
      case 'monitor':
        return '#3b82f6';
      default:
        return '#10b981';
    }
  };

  /** Stakeholders grouped by quadrant for the power/interest matrix. */
  const quadrants = $derived.by(() => {
    const q = { manage_closely: [], keep_satisfied: [], monitor: [], keep_informed: [] };
    for (const s of stakeholders) {
      if (q[s.quadrant]) q[s.quadrant].push(s);
    }
    return q;
  });

  // --- Stakeholder creation ---------------------------------------------
  /** Flat entity picker options derived from the hierarchy. */
  const entityOptions = $derived.by(() => {
    const opts = [];
    for (const pf of portfolios) {
      opts.push({ type: 'portfolio', id: pf.id, label: pf.title });
      for (const pg of pf.programmes ?? []) {
        opts.push({ type: 'programme', id: pg.id, label: `${pf.title} › ${pg.title}` });
        for (const pr of pg.projects ?? []) opts.push({ type: 'project', id: pr.id, label: `${pf.title} › ${pg.title} › ${pr.title}` });
      }
      for (const pr of pf.standalone_projects ?? []) opts.push({ type: 'project', id: pr.id, label: `${pf.title} › ${pr.title}` });
    }
    return opts;
  });

  let showStakeholderForm = $state(false);
  let stakeholderForm = $state({ entity_key: '', name: '', role_title: '', power_score: 3, interest_score: 3, notes: '' });
  let savingStakeholder = $state(false);
  let stakeholderError = $state('');

  async function submitStakeholder() {
    if (savingStakeholder) return;
    stakeholderError = '';
    const [type, idStr] = stakeholderForm.entity_key.split(':');
    if (!stakeholderForm.name.trim()) {
      stakeholderError = t('p3m.nameRequired');
      return;
    }
    if (!type || !idStr) {
      stakeholderError = t('p3m.entityRequired');
      return;
    }
    savingStakeholder = true;
    try {
      await p3mApi.stakeholders.create({
        entity_type: type,
        entity_id: Number(idStr),
        name: stakeholderForm.name.trim(),
        role_title: stakeholderForm.role_title,
        power_score: Math.max(1, Math.min(5, Number(stakeholderForm.power_score) || 3)),
        interest_score: Math.max(1, Math.min(5, Number(stakeholderForm.interest_score) || 3)),
        notes: stakeholderForm.notes,
      });
      stakeholderForm = { ...stakeholderForm, name: '', role_title: '', power_score: 3, interest_score: 3, notes: '' };
      await load();
    } catch (e) {
      stakeholderError = e?.message || t('p3m.createFailed');
    } finally {
      savingStakeholder = false;
    }
  }

  async function removeStakeholder(id) {
    try {
      await p3mApi.stakeholders.delete(id);
      await load();
    } catch (e) {
      stakeholderError = e?.message || t('p3m.createFailed');
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [r, s, c] = await Promise.all([p3mApi.raidItems.getAll(), p3mApi.stakeholders.getAll(), p3mApi.communications.getAll()]);
      raidItems = r ?? [];
      stakeholders = s ?? [];
      communications = c ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error && raidItems.length === 0 && stakeholders.length === 0}
  <EmptyState title={t('p3m.loadErrorTitle')} description={error} actions={{ label: t('p3m.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    <!-- RAID items -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.raidItems')}</h2>
      {#if raidItems.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.title')}</th>
              <th class="py-1.5 pr-3">{t('p3m.itemType')}</th>
              <th class="py-1.5 pr-3">I×P</th>
              <th class="py-1.5 pr-3">{t('p3m.priorityBucket')}</th>
              <th class="py-1.5 pr-3">Status</th>
            </tr>
          </thead>
          <tbody>
            {#each raidItems as item (item.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] align-top">
                <td class="py-1.5 pr-3">{entityLabel(item)}</td>
                <td class="py-1.5 pr-3">
                  {item.title}
                  {#if item.requires_escalation}
                    <span class="ml-2 rounded bg-purple-100 px-1 text-[10px] text-purple-700">{t('p3m.statusEscalated')}</span>
                  {/if}
                  {#if item.mitigation_plan}
                    <div class="mt-0.5 text-[var(--color-ds-text-muted,#6b7280)]">{item.mitigation_plan}</div>
                  {/if}
                </td>
                <td class="py-1.5 pr-3 capitalize">{item.item_type}</td>
                <td class="py-1.5 pr-3">
                  {item.probability_score}×{item.impact_score} = <span class="font-semibold">{item.risk_score}</span>
                </td>
                <td class="py-1.5 pr-3">
                  <span class={bucketChip(item.priority_bucket)}>{t(`p3m.bucket${item.priority_bucket === 'high' ? 'High' : item.priority_bucket === 'medium' ? 'Medium' : 'Low'}`)}</span>
                </td>
                <td class="py-1.5 pr-3">
                  <span class={statusChip(item.status)}>{t(`p3m.${item.status === 'open' ? 'statusOpen' : item.status === 'mitigating' ? 'statusMitigating' : item.status === 'closed' ? 'statusClosed' : 'statusEscalated'}`)}</span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Stakeholder power/interest matrix -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.stakeholders')}</h2>
        {#if entityOptions.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (showStakeholderForm = !showStakeholderForm)}>
            {t('p3m.addStakeholder')}
          </button>
        {/if}
      </div>

      {#if showStakeholderForm && entityOptions.length > 0}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-3">
            <label class="block">
              <span>{t('p3m.entityType')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={stakeholderForm.entity_key}
                disabled={savingStakeholder}
                onchange={(e) => (stakeholderForm = { ...stakeholderForm, entity_key: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each entityOptions as opt (opt.type + ':' + opt.id)}
                  <option value={opt.type + ':' + opt.id}>{opt.label}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.stakeholderName')} *</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={stakeholderForm.name}
                disabled={savingStakeholder}
                oninput={(e) => (stakeholderForm = { ...stakeholderForm, name: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.roleTitle')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={stakeholderForm.role_title}
                disabled={savingStakeholder}
                oninput={(e) => (stakeholderForm = { ...stakeholderForm, role_title: e.target.value })}
              />
            </label>

            <div class="grid grid-cols-2 gap-x-4">
              <label class="block">
                <span>{t('p3m.powerScore')} (1–5)</span>
                <select
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={stakeholderForm.power_score}
                  disabled={savingStakeholder}
                  onchange={(e) => (stakeholderForm = { ...stakeholderForm, power_score: Number(e.target.value) })}
                >
                  {#each [1, 2, 3, 4, 5] as n (n)}
                    <option value={n}>{n}</option>
                  {/each}
                </select>
              </label>
              <label class="block">
                <span>{t('p3m.interestScore')} (1–5)</span>
                <select
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={stakeholderForm.interest_score}
                  disabled={savingStakeholder}
                  onchange={(e) => (stakeholderForm = { ...stakeholderForm, interest_score: Number(e.target.value) })}
                >
                  {#each [1, 2, 3, 4, 5] as n (n)}
                    <option value={n}>{n}</option>
                  {/each}
                </select>
              </label>
            </div>

            <label class="block md:col-span-2">
              <span>{t('p3m.notes')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={stakeholderForm.notes}
                disabled={savingStakeholder}
                oninput={(e) => (stakeholderForm = { ...stakeholderForm, notes: e.target.value })}
              />
            </label>
          </div>

          {#if stakeholderError}
            <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{stakeholderError}</div>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingStakeholder} onclick={submitStakeholder}>
              {t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (showStakeholderForm = false)}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

      {#if stakeholders.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          {#each [
            ['manage_closely', 'quadrantManageClosely'],
            ['keep_satisfied', 'quadrantKeepSatisfied'],
            ['monitor', 'quadrantMonitor'],
            ['keep_informed', 'quadrantKeepInformed']
          ] as [key, labelKey] (key)}
            <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-3">
              <div class="mb-2 flex items-center gap-2 text-xs font-semibold">
                <span class="inline-block h-2.5 w-2.5 rounded-full" style="background: {quadrantColor(key)}"></span>
                {t(`p3m.${labelKey}`)}
              </div>
              {#if quadrants[key].length === 0}
                <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noData')}</div>
              {:else}
                {#each quadrants[key] as s (s.id)}
                  <div class="flex items-center justify-between gap-2 border-t border-[var(--color-ds-border,#e5e7eb)] py-1.5 text-xs first:border-t-0">
                    <span>
                      {s.name}
                      {#if s.role_title}
                        <span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {s.role_title}</span>
                      {/if}
                      <div class="text-[10px] text-[var(--color-ds-text-muted,#6b7280)]">{entityLabel(s)}</div>
                    </span>
                    <span class="flex items-center gap-2 whitespace-nowrap text-[var(--color-ds-text-muted,#6b7280)]">
                      <span title={t('p3m.powerScore')}>P{s.power_score}</span>
                      <span title={t('p3m.interestScore')}>I{s.interest_score}</span>
                      <button type="button" class="hover:text-red-600" title={t('p3m.removeStakeholder')} onclick={() => removeStakeholder(s.id)}>
                        <IconX size={14} />
                      </button>
                    </span>
                  </div>
                {/each}
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <!-- Communications log -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('p3m.communications')}</h2>
      {#if communications.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('p3m.noData')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('p3m.recordedDate')}</th>
              <th class="py-1.5 pr-3">{t('p3m.entityType')}</th>
              <th class="py-1.5 pr-3">{t('p3m.audience')}</th>
              <th class="py-1.5 pr-3">{t('p3m.channel')}</th>
              <th class="py-1.5 pr-3">{t('p3m.summary')}</th>
            </tr>
          </thead>
          <tbody>
            {#each [...communications].sort((a, b) => (a.date > b.date ? -1 : 1)) as c (c.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{formatDate(c.date)}</td>
                <td class="py-1.5 pr-3">{entityLabel(c)}</td>
                <td class="py-1.5 pr-3">{c.audience}</td>
                <td class="py-1.5 pr-3 capitalize">{c.channel}</td>
                <td class="py-1.5 pr-3">{c.summary}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
