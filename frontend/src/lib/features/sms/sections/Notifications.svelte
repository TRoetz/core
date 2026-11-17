<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { formatDate, entityTypeLabel, P3M_OPTIONS_KEYS } from '../utils.js';

  let loading = $state(true);
  let error = $state('');
  /** @type {Array<Record<string, any>>} */
  let campaigns = $state([]);
  /** @type {Array<Record<string, any>>} */
  let nodes = $state([]);
  /** @type {{ portfolios: Array<Record<string, any>>, programmes: Array<Record<string, any>>, projects: Array<Record<string, any>> }} */
  let p3mOptions = $state({ portfolios: [], programmes: [], projects: [] });

  // Campaign form.
  let campaignFormOpen = $state(false);
  /** @type {Record<string, any> | null} */
  let editingCampaign = $state(null);
  let campaignForm = $state({ title: '', description: '', entity_type: 'project', entity_id: '', channel: 'email', scheduled_at: '', due_by: '', status: 'draft' });

  // Selected campaign detail.
  /** @type {Record<string, any> | null} */
  let selected = $state(null);
  /** @type {Array<Record<string, any>>} */
  let targets = $state([]);
  /** @type {Array<Record<string, any>>} */
  let ledger = $state([]);
  let targetNodeId = $state('');
  let includeDescendants = $state(true);
  let generating = $state(false);
  let generateResult = $state('');
  let statusFilter = $state('');
  /** @type {Set<number>} */
  let selectedRows = $state(new Set());

  const NOTIF_STATUSES = ['pending', 'sent', 'read', 'acknowledged', 'failed', 'exempt'];
  const CAMPAIGN_STATUSES = ['draft', 'scheduled', 'in_progress', 'completed', 'cancelled'];

  function notifStatusKey(s) {
    return `sms.notif${s.charAt(0).toUpperCase()}${s.slice(1)}`;
  }
  function campaignStatusKey(s) {
    if (s === 'draft') return 'sms.stDraft';
    if (s === 'scheduled') return 'sms.stScheduled';
    if (s === 'in_progress') return 'sms.stInProgress';
    if (s === 'completed') return 'sms.stCompleted';
    return 'sms.stCancelled';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [campaignRows, nodeRows, options] = await Promise.all([
        smsApi.getNotificationCampaigns({}),
        smsApi.getNodes({}),
        smsApi.getP3MOptions(),
      ]);
      campaigns = campaignRows ?? [];
      nodes = nodeRows ?? [];
      p3mOptions = { portfolios: [], programmes: [], projects: [], ...(options ?? {}) };
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function openCampaign(campaign) {
    try {
      selected = campaign;
      generateResult = '';
      statusFilter = '';
      selectedRows = new Set();
      const [targetRows, ledgerRows] = await Promise.all([
        smsApi.getCampaignTargets(campaign.id),
        smsApi.getCampaignNotifications(campaign.id),
      ]);
      targets = targetRows ?? [];
      ledger = ledgerRows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function closeCampaign() {
    selected = null;
    targets = [];
    ledger = [];
  }

  async function refreshLedger() {
    if (!selected) return;
    try {
      const [targetRows, ledgerRows] = await Promise.all([
        smsApi.getCampaignTargets(selected.id),
        smsApi.getCampaignNotifications(selected.id, statusFilter ? { status: statusFilter } : {}),
      ]);
      targets = targetRows ?? [];
      ledger = ledgerRows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function openCreateCampaign() {
    editingCampaign = null;
    campaignForm = { title: '', description: '', entity_type: 'project', entity_id: '', channel: 'email', scheduled_at: '', due_by: '', status: 'draft' };
    campaignFormOpen = true;
  }

  function openEditCampaign(campaign) {
    editingCampaign = campaign;
    campaignForm = {
      title: campaign.title,
      description: campaign.description ?? '',
      entity_type: campaign.entity_type || 'project',
      entity_id: campaign.entity_id ? String(campaign.entity_id) : '',
      channel: campaign.channel || 'email',
      scheduled_at: campaign.scheduled_at ? String(campaign.scheduled_at).slice(0, 16) : '',
      due_by: campaign.due_by ?? '',
      status: campaign.status,
    };
    campaignFormOpen = true;
  }

  async function saveCampaign() {
    if (!campaignForm.title.trim()) return;
    /** @type {Record<string, any>} */
    const payload = { ...campaignForm };
    if (payload.entity_id) payload.entity_id = Number(payload.entity_id);
    else delete payload.entity_id;
    if (!payload.scheduled_at) delete payload.scheduled_at;
    try {
      if (editingCampaign) await smsApi.updateNotificationCampaign(editingCampaign.id, payload);
      else await smsApi.createNotificationCampaign(payload);
      campaignFormOpen = false;
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteCampaign(campaign) {
    if (!confirm(t('sms.deleteMessage'))) return;
    try {
      await smsApi.deleteNotificationCampaign(campaign.id);
      if (selected?.id === campaign.id) closeCampaign();
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addTarget() {
    if (!selected || !targetNodeId) return;
    try {
      await smsApi.addCampaignTarget(selected.id, { node_id: Number(targetNodeId), include_descendants: !!includeDescendants });
      targetNodeId = '';
      const rows = await smsApi.getCampaignTargets(selected.id);
      targets = rows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeTarget(linkId) {
    if (!selected) return;
    try {
      await smsApi.removeCampaignTarget(linkId);
      const rows = await smsApi.getCampaignTargets(selected.id);
      targets = rows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function generateAudience() {
    if (!selected) return;
    generating = true;
    generateResult = '';
    try {
      const result = await smsApi.generateCampaignAudience(selected.id);
      generateResult = t('sms.audienceGenerated', { created: result?.created ?? 0, skipped: result?.skipped ?? 0 });
      await refreshLedger();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      generating = false;
    }
  }

  async function updateRow(row, status) {
    try {
      await smsApi.updateNotification(row.id, { status });
      row.status = status;
      if (status === 'sent' && !row.sent_at) row.sent_at = new Date().toISOString();
      if (status === 'acknowledged') row.acknowledged_at = new Date().toISOString();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function toggleRow(id) {
    const next = new Set(selectedRows);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedRows = next;
  }

  async function bulkUpdate(status) {
    if (!selected || selectedRows.size === 0) return;
    try {
      await smsApi.bulkUpdateNotifications({ ids: [...selectedRows], status });
      selectedRows = new Set();
      await refreshLedger();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function entityLabel(campaign) {
    if (!campaign.entity_type) return '—';
    const list = p3mOptions[P3M_OPTIONS_KEYS[campaign.entity_type]] ?? [];
    const found = list.find((o) => o.id === campaign.entity_id);
    return `${entityTypeLabel(campaign.entity_type)}: ${found?.title ?? `#${campaign.entity_id}`}`;
  }

  onMount(load);
</script>

<div class="space-y-6 px-6 py-4">
  {#if loading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
  {:else if error && campaigns.length === 0}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
  {:else}
    <!-- Campaign list -->
    <section>
      <div class="mb-2 flex items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.campaigns')}</h2>
        <button type="button" class="ml-auto rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={openCreateCampaign}>
          {t('sms.newCampaign')}
        </button>
      </div>

      {#if campaignFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{editingCampaign ? t('sms.editCampaign') : t('sms.newCampaign')}</h3>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-4">
            <label class="text-xs md:col-span-2">
              {t('sms.title')} *
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.title} oninput={(e) => (campaignForm.title = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.channel')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.channel} onchange={(e) => (campaignForm.channel = e.target.value)}>
                {#each ['email', 'phone', 'meeting', 'in_person', 'slack', 'teams', 'other'] as ch (ch)}<option value={ch}>{t(`sms.ch${ch.charAt(0).toUpperCase()}${ch.slice(1)}`)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.status')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.status} onchange={(e) => (campaignForm.status = e.target.value)}>
                {#each CAMPAIGN_STATUSES as s (s)}<option value={s}>{t(campaignStatusKey(s))}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.entityType')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.entity_type} onchange={(e) => (campaignForm.entity_type = e.target.value)}>
                <option value="">{t('sms.noEntity')}</option>
                {#each ['portfolio', 'programme', 'project'] as et (et)}<option value={et}>{entityTypeLabel(et)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.entity')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.entity_id} onchange={(e) => (campaignForm.entity_id = e.target.value)} disabled={!campaignForm.entity_type}>
                <option value="">{t('sms.selectEntity')}</option>
                {#each p3mOptions[P3M_OPTIONS_KEYS[campaignForm.entity_type]] ?? [] as o (o.id)}<option value={String(o.id)}>{o.title}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.scheduledAt')}
              <input type="datetime-local" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.scheduled_at} oninput={(e) => (campaignForm.scheduled_at = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.dueBy')}
              <input type="date" class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={campaignForm.due_by} oninput={(e) => (campaignForm.due_by = e.target.value)} />
            </label>
          </div>
          <div class="mt-2 flex gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={saveCampaign}>{t('sms.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={() => (campaignFormOpen = false)}>{t('sms.back')}</button>
          </div>
        </div>
      {/if}

      {#if error && campaigns.length > 0}<div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>{/if}

      {#if campaigns.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noCampaigns')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.title')}</th>
              <th class="py-1.5 pr-3">{t('sms.entity')}</th>
              <th class="py-1.5 pr-3">{t('sms.channel')}</th>
              <th class="py-1.5 pr-3">{t('sms.dueBy')}</th>
              <th class="py-1.5 pr-3">{t('sms.status')}</th>
              <th class="py-1.5 pr-3 text-right">{t('sms.coverage')}</th>
              <th class="py-1.5"></th>
            </tr>
          </thead>
          <tbody>
            {#each campaigns as c (c.id)}
              <tr class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0 hover:bg-slate-50" class:selected={selected?.id === c.id} onclick={() => openCampaign(c)}>
                <td class="py-1.5 pr-3 font-medium">{c.title}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{entityLabel(c)}</td>
                <td class="py-1.5 pr-3">{t(`sms.ch${(c.channel || 'email').charAt(0).toUpperCase()}${(c.channel || 'email').slice(1)}`)}</td>
                <td class="py-1.5 pr-3 whitespace-nowrap">
                  {formatDate(c.due_by)}
                  {#if c.overdue_count > 0}<span class="ml-1 rounded-full bg-red-100 px-1.5 py-0.5 text-[10px] font-medium text-red-700">{c.overdue_count} {t('sms.overdue')}</span>{/if}
                </td>
                <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium">{t(campaignStatusKey(c.status))}</span></td>
                <td class="py-1.5 pr-3 text-right whitespace-nowrap">
                  {c.informed_count}/{c.total_notifications}
                  <div class="mt-0.5 h-1 w-24 overflow-hidden rounded-full bg-slate-200">
                    <div class="h-full rounded-full" style="width: {Math.min(100, c.informed_pct ?? 0)}%; background: var(--color-ds-accent,#2563eb);"></div>
                  </div>
                </td>
                <td class="py-1.5 text-right">
                  <button type="button" class="mr-2 text-[var(--color-ds-accent,#2563eb)]" onclick={(e) => { e.stopPropagation(); openEditCampaign(c); }}>{t('sms.edit')}</button>
                  <button type="button" class="text-red-600" onclick={(e) => { e.stopPropagation(); deleteCampaign(c); }}>{t('sms.delete')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Campaign detail -->
      {#if selected}
        <div class="mt-3 space-y-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <div class="flex items-center gap-2">
            <h3 class="text-xs font-semibold">{selected.title}</h3>
            <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium">{t(campaignStatusKey(selected.status))}</span>
            <button type="button" class="ml-auto text-xs text-[var(--color-ds-accent,#2563eb)]" onclick={closeCampaign}>{t('sms.back')}</button>
          </div>

          <!-- Targets -->
          <section>
            <h4 class="mb-1.5 text-xs font-semibold">{t('sms.targets')} ({targets.length})</h4>
            {#if targets.length === 0}
              <div class="mb-2 rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noTargets')}</div>
            {:else}
              <ul class="mb-2 space-y-1">
                {#each targets as tg (tg.id)}
                  <li class="flex items-center gap-2 text-xs">
                    <span class="font-medium">{nodes.find((n) => n.id === tg.node_id)?.name ?? `#${tg.node_id}`}</span>
                    {#if tg.include_descendants}<span class="rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px]">{t('sms.includeDescendants')}</span>{/if}
                    <button type="button" class="ml-auto text-red-600" onclick={() => removeTarget(tg.id)}>{t('sms.delete')}</button>
                  </li>
                {/each}
              </ul>
            {/if}
            <div class="flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-2">
              <label class="text-xs">
                {t('sms.node')} *
                <select class="mt-0.5 block w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={targetNodeId} onchange={(e) => (targetNodeId = e.target.value)}>
                  <option value="">{t('sms.selectNode')}</option>
                  {#each nodes as n (n.id)}<option value={String(n.id)}>{n.name}</option>{/each}
                </select>
              </label>
              <label class="flex items-center gap-1.5 pb-1 text-xs">
                <input type="checkbox" checked={includeDescendants} onchange={(e) => (includeDescendants = e.target.checked)} />
                {t('sms.includeDescendants')}
              </label>
              <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={addTarget}>{t('sms.add')}</button>
            </div>
          </section>

          <!-- Audience generation -->
          <section class="flex flex-wrap items-center gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white disabled:opacity-50" onclick={generateAudience} disabled={generating || targets.length === 0}>
              {t('sms.generateAudience')}
            </button>
            {#if generateResult}<span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{generateResult}</span>{/if}
          </section>

          <!-- Ledger -->
          <section>
            <div class="mb-1.5 flex flex-wrap items-center gap-2">
              <h4 class="text-xs font-semibold">{t('sms.notificationsLedger')} ({ledger.length})</h4>
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={statusFilter} onchange={(e) => { statusFilter = e.target.value; refreshLedger(); }}>
                <option value="">{t('sms.allStatuses')}</option>
                {#each NOTIF_STATUSES as s (s)}<option value={s}>{t(notifStatusKey(s))}</option>{/each}
              </select>
              <div class="ml-auto flex gap-1.5">
                <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs disabled:opacity-40" disabled={selectedRows.size === 0} onclick={() => bulkUpdate('sent')}>{t('sms.markSent')}</button>
                <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs disabled:opacity-40" disabled={selectedRows.size === 0} onclick={() => bulkUpdate('read')}>{t('sms.markRead')}</button>
                <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs disabled:opacity-40" disabled={selectedRows.size === 0} onclick={() => bulkUpdate('acknowledged')}>{t('sms.markAcknowledged')}</button>
              </div>
            </div>

            {#if ledger.length === 0}
              <div class="rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noNotifications')}</div>
            {:else}
              <table class="w-full text-left text-xs">
                <thead>
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                    <th class="py-1 pr-2"></th>
                    <th class="py-1.5 pr-3">{t('sms.stakeholder')}</th>
                    <th class="py-1.5 pr-3">{t('sms.node')}</th>
                    <th class="py-1.5 pr-3">{t('sms.channel')}</th>
                    <th class="py-1.5 pr-3">{t('sms.dueBy')}</th>
                    <th class="py-1.5 pr-3">{t('sms.sentAt')}</th>
                    <th class="py-1.5 pr-3">{t('sms.status')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each ledger as row (row.id)}
                    <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                      <td class="py-1.5 pr-2"><input type="checkbox" checked={selectedRows.has(row.id)} onchange={() => toggleRow(row.id)} /></td>
                      <td class="py-1.5 pr-3 font-medium">{row.name}<span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {row.organization}</span></td>
                      <td class="py-1.5 pr-3">{row.node_name || '—'}</td>
                      <td class="py-1.5 pr-3">{t(`sms.ch${(row.channel || selected?.channel || 'email').charAt(0).toUpperCase()}${(row.channel || selected?.channel || 'email').slice(1)}`)}</td>
                      <td class="py-1.5 pr-3 whitespace-nowrap">
                        {formatDate(row.required_by)}
                        {#if row.status === 'pending' && row.required_by && new Date(row.required_by + 'T23:59:59') < new Date()}<span class="ml-1 rounded-full bg-red-100 px-1.5 py-0.5 text-[10px] font-medium text-red-700">{t('sms.overdue')}</span>{/if}
                      </td>
                      <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(row.sent_at)}</td>
                      <td class="py-1.5 pr-3">
                        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-1.5 py-0.5 text-xs" value={row.status} onchange={(e) => updateRow(row, e.target.value)}>
                          {#each NOTIF_STATUSES as s (s)}<option value={s}>{t(notifStatusKey(s))}</option>{/each}
                        </select>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>
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
