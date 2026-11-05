<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { getUsers } from '../../api/users.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { confirm } from '../../composables/useConfirm.js';
  import { formatDate, quadrantLabel, toISODate } from './utils.js';

  let { portfolios = [] } = $props();

  let raidItems = $state([]);
  let stakeholders = $state([]);
  let communications = $state([]);
  let loading = $state(true);
  let error = $state('');

  /** @type {Array<{id:number,display_name?:string,username?:string}>} */
  let users = $state([]);

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
  /** Stakeholder being edited in place, or null when creating. */
  let editingStakeholder = $state(null);
  let stakeholderForm = $state({ entity_key: '', name: '', role_title: '', power_score: 3, interest_score: 3, notes: '' });
  let savingStakeholder = $state(false);
  let stakeholderError = $state('');

  function openStakeholderCreate() {
    editingStakeholder = null;
    stakeholderForm = { entity_key: '', name: '', role_title: '', power_score: 3, interest_score: 3, notes: '' };
    stakeholderError = '';
    showStakeholderForm = true;
  }

  function openStakeholderEdit(s) {
    editingStakeholder = s;
    stakeholderForm = {
      entity_key: `${s.entity_type}:${s.entity_id}`,
      name: s.name ?? '',
      role_title: s.role_title ?? '',
      power_score: s.power_score ?? 3,
      interest_score: s.interest_score ?? 3,
      notes: s.notes ?? '',
    };
    stakeholderError = '';
    showStakeholderForm = true;
  }

  function closeStakeholderForm() {
    showStakeholderForm = false;
    editingStakeholder = null;
  }

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
      const payload = {
        entity_type: type,
        entity_id: Number(idStr),
        name: stakeholderForm.name.trim(),
        role_title: stakeholderForm.role_title,
        power_score: Math.max(1, Math.min(5, Number(stakeholderForm.power_score) || 3)),
        interest_score: Math.max(1, Math.min(5, Number(stakeholderForm.interest_score) || 3)),
        notes: stakeholderForm.notes,
      };
      if (editingStakeholder) await p3mApi.stakeholders.update(editingStakeholder.id, payload);
      else await p3mApi.stakeholders.create(payload);
      closeStakeholderForm();
      await load();
    } catch (e) {
      stakeholderError = e?.message || t('p3m.updateFailed');
    } finally {
      savingStakeholder = false;
    }
  }

  async function removeStakeholder(id) {
    try {
      await p3mApi.stakeholders.delete(id);
      await load();
    } catch (e) {
      stakeholderError = e?.message || t('p3m.deleteFailed');
    }
  }

  // --- RAID items -------------------------------------------------------------
  const RAID_TYPES = ['risk', 'assumption', 'issue', 'dependency', 'decision'];
  const RAID_STATUSES = ['open', 'mitigating', 'closed', 'escalated'];
  let showRaidForm = $state(false);
  /** RAID item being edited in place, or null when creating. */
  let editingRaidItem = $state(null);
  let savingRaid = $state(false);
  let raidError = $state('');
  const emptyRaidForm = () => ({ entity_key: '', item_type: 'risk', title: '', description: '', impact_score: 3, probability_score: 3, mitigation_plan: '', owner_id: '', status: 'open' });
  let raidForm = $state(emptyRaidForm());

  function openRaidCreate() {
    editingRaidItem = null;
    raidForm = emptyRaidForm();
    raidError = '';
    showRaidForm = true;
  }

  function openRaidEdit(item) {
    editingRaidItem = item;
    raidForm = {
      entity_key: `${item.entity_type}:${item.entity_id}`,
      item_type: item.item_type ?? 'risk',
      title: item.title ?? '',
      description: item.description ?? '',
      impact_score: item.impact_score ?? 3,
      probability_score: item.probability_score ?? 3,
      mitigation_plan: item.mitigation_plan ?? '',
      owner_id: item.owner_id != null ? String(item.owner_id) : '',
      status: item.status ?? 'open',
    };
    raidError = '';
    showRaidForm = true;
  }

  function closeRaidForm() {
    showRaidForm = false;
    editingRaidItem = null;
  }

  async function submitRaid() {
    if (savingRaid) return;
    raidError = '';
    const [type, idStr] = raidForm.entity_key.split(':');
    if (!raidForm.title.trim()) {
      raidError = t('p3m.nameRequired');
      return;
    }
    if (!type || !idStr) {
      raidError = t('p3m.entityRequired');
      return;
    }
    const owner = Number(raidForm.owner_id);
    if (!owner) {
      raidError = t('p3m.ownerRequired');
      return;
    }
    savingRaid = true;
    try {
      const payload = {
        entity_type: type,
        entity_id: Number(idStr),
        item_type: raidForm.item_type,
        title: raidForm.title.trim(),
        description: raidForm.description,
        impact_score: Math.max(1, Math.min(5, Number(raidForm.impact_score) || 3)),
        probability_score: Math.max(1, Math.min(5, Number(raidForm.probability_score) || 3)),
        mitigation_plan: raidForm.mitigation_plan,
        owner_id: owner,
        status: raidForm.status,
      };
      if (editingRaidItem) await p3mApi.raidItems.update(editingRaidItem.id, payload);
      else await p3mApi.raidItems.create(payload);
      closeRaidForm();
      await load();
    } catch (e) {
      raidError = e?.message || t('p3m.updateFailed');
    } finally {
      savingRaid = false;
    }
  }

  async function deleteRaidItem(item) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteRaidItem') }))) return;
    try {
      await p3mApi.raidItems.delete(item.id);
      await load();
    } catch (e) {
      raidError = e?.message || t('p3m.deleteFailed');
    }
  }

  /** One-click escalation for items not yet escalated. */
  async function escalateRaidItem(item) {
    try {
      const updated = await p3mApi.raidItems.update(item.id, { ...item, status: 'escalated' });
      raidItems = raidItems.map((x) => (x.id === updated.id ? updated : x));
    } catch (e) {
      raidError = e?.message || t('p3m.updateFailed');
    }
  }

  // --- Communications log ---------------------------------------------------------
  const CHANNELS = ['meeting', 'workshop', 'consultation', 'report', 'email', 'other'];
  let showCommForm = $state(false);
  let savingComm = $state(false);
  let commError = $state('');
  const emptyCommForm = () => ({ entity_key: '', date: '', audience: '', channel: 'meeting', summary: '' });
  let commForm = $state(emptyCommForm());

  function openCommCreate() {
    commForm = emptyCommForm();
    commError = '';
    showCommForm = true;
  }

  async function submitCommunication() {
    if (savingComm) return;
    commError = '';
    const [type, idStr] = commForm.entity_key.split(':');
    if (!type || !idStr) {
      commError = t('p3m.entityRequired');
      return;
    }
    savingComm = true;
    try {
      await p3mApi.communications.create({
        entity_type: type,
        entity_id: Number(idStr),
        date: toISODate(commForm.date) || new Date().toISOString(),
        audience: commForm.audience,
        channel: commForm.channel,
        summary: commForm.summary,
      });
      showCommForm = false;
      await load();
    } catch (e) {
      commError = e?.message || t('p3m.createFailed');
    } finally {
      savingComm = false;
    }
  }

  async function deleteCommunication(c) {
    if (!(await confirm({ title: t('p3m.delete'), message: t('p3m.confirmDeleteCommunication') }))) return;
    try {
      await p3mApi.communications.delete(c.id);
      await load();
    } catch (e) {
      commError = e?.message || t('p3m.deleteFailed');
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

  onMount(() => {
    load();
    getUsers().then((u) => (users = u || [])).catch(() => {});
  });
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
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.raidItems')}</h2>
        {#if entityOptions.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openRaidCreate}>
            <IconPlus size={14} class="inline" /> {t('p3m.newRaidItem')}
          </button>
        {/if}
      </div>

      {#if raidError && !showRaidForm}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{raidError}</div>
      {/if}

      {#if showRaidForm && entityOptions.length > 0}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-4">
            <label class="block">
              <span>{t('p3m.entityType')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.entity_key}
                disabled={savingRaid}
                onchange={(e) => (raidForm = { ...raidForm, entity_key: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each entityOptions as opt (opt.type + ':' + opt.id)}
                  <option value={opt.type + ':' + opt.id}>{opt.label}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.itemType')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.item_type}
                disabled={savingRaid}
                onchange={(e) => (raidForm = { ...raidForm, item_type: e.target.value })}
              >
                {#each RAID_TYPES as rt (rt)}
                  <option value={rt}>{rt}</option>
                {/each}
              </select>
            </label>

            <label class="block md:col-span-2">
              <span>{t('p3m.title')} *</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.title}
                disabled={savingRaid}
                oninput={(e) => (raidForm = { ...raidForm, title: e.target.value })}
              />
            </label>

            <label class="block md:col-span-2">
              <span>{t('p3m.description')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.description}
                disabled={savingRaid}
                oninput={(e) => (raidForm = { ...raidForm, description: e.target.value })}
              />
            </label>

            <div class="grid grid-cols-2 gap-x-4">
              <label class="block">
                <span>{t('p3m.impactScore')} (1–5)</span>
                <select
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={raidForm.impact_score}
                  disabled={savingRaid}
                  onchange={(e) => (raidForm = { ...raidForm, impact_score: Number(e.target.value) })}
                >
                  {#each [1, 2, 3, 4, 5] as n (n)}
                    <option value={n}>{n}</option>
                  {/each}
                </select>
              </label>
              <label class="block">
                <span>{t('p3m.probabilityScore')} (1–5)</span>
                <select
                  class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                  value={raidForm.probability_score}
                  disabled={savingRaid}
                  onchange={(e) => (raidForm = { ...raidForm, probability_score: Number(e.target.value) })}
                >
                  {#each [1, 2, 3, 4, 5] as n (n)}
                    <option value={n}>{n}</option>
                  {/each}
                </select>
              </label>
            </div>

            <label class="block">
              <span>{t('p3m.owner')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.owner_id}
                disabled={savingRaid}
                onchange={(e) => (raidForm = { ...raidForm, owner_id: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each users as u (u.id)}
                  <option value={u.id}>{u.display_name || u.username}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>Status</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.status}
                disabled={savingRaid}
                onchange={(e) => (raidForm = { ...raidForm, status: e.target.value })}
              >
                {#each RAID_STATUSES as rs (rs)}
                  <option value={rs}>{t(`p3m.${rs === 'open' ? 'statusOpen' : rs === 'mitigating' ? 'statusMitigating' : rs === 'closed' ? 'statusClosed' : 'statusEscalated'}`)}</option>
                {/each}
              </select>
            </label>

            <label class="block md:col-span-4">
              <span>{t('p3m.mitigationPlan')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={raidForm.mitigation_plan}
                disabled={savingRaid}
                oninput={(e) => (raidForm = { ...raidForm, mitigation_plan: e.target.value })}
              />
            </label>
          </div>

          {#if raidError}
            <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{raidError}</div>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingRaid} onclick={submitRaid}>
              {editingRaidItem ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeRaidForm}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

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
              <th class="py-1.5 pr-3"></th>
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
                <td class="py-1.5 pr-3">
                  <div class="flex items-center gap-1 whitespace-nowrap">
                    {#if item.status !== 'escalated'}
                      <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-purple-600" title={t('p3m.escalate')} onclick={() => escalateRaidItem(item)}>
                        {t('p3m.escalate')}
                      </button>
                    {/if}
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openRaidEdit(item)}>
                      {t('p3m.edit')}
                    </button>
                    <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteRaidItem(item)}>
                      <IconX size={14} />
                    </button>
                  </div>
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
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openStakeholderCreate}>
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
              {editingStakeholder ? t('p3m.save') : t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={closeStakeholderForm}>
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
                      <button type="button" class="hover:text-[var(--color-ds-text,#111827)]" title={t('p3m.edit')} onclick={() => openStakeholderEdit(s)}>
                        {t('p3m.edit')}
                      </button>
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
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('p3m.communications')}</h2>
        {#if entityOptions.length > 0}
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={openCommCreate}>
            <IconPlus size={14} class="inline" /> {t('p3m.addCommunication')}
          </button>
        {/if}
      </div>

      {#if commError && !showCommForm}
        <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{commError}</div>
      {/if}

      {#if showCommForm && entityOptions.length > 0}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-3 text-xs">
          <div class="grid grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-4">
            <label class="block">
              <span>{t('p3m.entityType')} *</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={commForm.entity_key}
                disabled={savingComm}
                onchange={(e) => (commForm = { ...commForm, entity_key: e.target.value })}
              >
                <option value="">{t('p3m.noSelection')}</option>
                {#each entityOptions as opt (opt.type + ':' + opt.id)}
                  <option value={opt.type + ':' + opt.id}>{opt.label}</option>
                {/each}
              </select>
            </label>

            <label class="block">
              <span>{t('p3m.recordedDate')}</span>
              <input
                type="date"
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={commForm.date}
                disabled={savingComm}
                oninput={(e) => (commForm = { ...commForm, date: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.audience')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={commForm.audience}
                disabled={savingComm}
                oninput={(e) => (commForm = { ...commForm, audience: e.target.value })}
              />
            </label>

            <label class="block">
              <span>{t('p3m.channel')}</span>
              <select
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={commForm.channel}
                disabled={savingComm}
                onchange={(e) => (commForm = { ...commForm, channel: e.target.value })}
              >
                {#each CHANNELS as ch (ch)}
                  <option value={ch}>{t(`p3m.${ch === 'meeting' ? 'channelMeeting' : ch === 'workshop' ? 'channelWorkshop' : ch === 'consultation' ? 'channelConsultation' : ch === 'report' ? 'channelReport' : ch === 'email' ? 'channelEmail' : 'channelOther'}`)}</option>
                {/each}
              </select>
            </label>

            <label class="block md:col-span-4">
              <span>{t('p3m.summary')}</span>
              <input
                class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1"
                value={commForm.summary}
                disabled={savingComm}
                oninput={(e) => (commForm = { ...commForm, summary: e.target.value })}
              />
            </label>
          </div>

          {#if commError}
            <div class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-red-700">{commError}</div>
          {/if}

          <div class="mt-3 flex items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#2563eb)] px-3 py-1 font-medium text-white disabled:opacity-50" disabled={savingComm} onclick={submitCommunication}>
              {t('p3m.create')}
            </button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]" onclick={() => (showCommForm = false)}>
              {t('p3m.cancel')}
            </button>
          </div>
        </div>
      {/if}

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
              <th class="py-1.5 pr-3"></th>
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
                <td class="py-1.5 pr-3">
                  <button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => deleteCommunication(c)}>
                    <IconX size={14} />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
