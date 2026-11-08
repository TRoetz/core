<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import Modal from '../../../dialogs/Modal.svelte';
  import IconPencil from '@tabler/icons-svelte-runes/icons/pencil';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  let scenarios = $state([]);
  let selectedId = $state(null);
  let detail = $state(null); // { scenario, items, impact }
  let allItems = $state([]);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);

  // New-scenario form.
  let newScenarioName = $state('');
  let newScenarioDesc = $state('');

  // Edit-scenario modal (rename / description / status).
  const SCENARIO_STATUSES = [
    { id: 'draft', labelKey: 'plans.scenariosStatusDraft' },
    { id: 'active', labelKey: 'plans.scenariosStatusActive' },
    { id: 'archived', labelKey: 'plans.scenariosStatusArchived' },
  ];
  let showEdit = $state(false);
  let editSid = $state(null);
  let editName = $state('');
  let editDesc = $state('');
  let editStatus = $state('draft');

  function openScenarioEdit(s) {
    editSid = s.id;
    editName = s.name;
    editDesc = s.description ?? '';
    editStatus = SCENARIO_STATUSES.some((st) => st.id === s.status) ? s.status : 'draft';
    showEdit = true;
  }

  async function saveScenarioEdit() {
    if (!editName.trim()) return;
    busy = true;
    try {
      await plansApi.updateScenario(planId, editSid, {
        name: editName.trim(),
        description: editDesc.trim(),
        status: editStatus,
      });
      showEdit = false;
      await loadScenarios();
      if (selectedId === editSid) await loadDetail(editSid);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  // Add-override form (for the selected scenario).
  let ovItemId = $state('');
  let ovStart = $state('');
  let ovEnd = $state('');
  let ovNotes = $state('');
  const removedIds = new Set();

  async function loadScenarios() {
    try {
      const res = await plansApi.listScenarios(planId);
      scenarios = Array.isArray(res) ? res : (res?.scenarios ?? []);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function loadDetail(sid) {
    try {
      detail = await plansApi.getScenarioDetail(planId, sid);
    } catch (e) {
      error = e?.message || 'error';
      detail = null;
    }
  }

  onMount(async () => {
    try {
      const [scenRes, itemsRes] = await Promise.all([
        plansApi.listScenarios(planId),
        plansApi.getItems(planId, { limit: 1000 }),
      ]);
      scenarios = Array.isArray(scenRes) ? scenRes : (scenRes?.scenarios ?? []);
      allItems = itemsRes?.items ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  });

  function selectScenario(sid) {
    selectedId = sid;
    detail = null;
    ovItemId = '';
    ovStart = '';
    ovEnd = '';
    ovNotes = '';
    removedIds.clear();
    loadDetail(sid);
  }

  async function createScenario() {
    const name = newScenarioName.trim();
    if (!name) return;
    busy = true;
    try {
      const created = await plansApi.createScenario(planId, { name, description: newScenarioDesc.trim() || undefined });
      newScenarioName = '';
      newScenarioDesc = '';
      await loadScenarios();
      selectScenario(created?.id ?? scenarios[scenarios.length - 1]?.id);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  async function deleteScenario(sid) {
    if (!window.confirm(t('common.delete'))) return;
    busy = true;
    try {
      await plansApi.deleteScenario(planId, sid);
      if (selectedId === sid) selectedId = null;
      detail = null;
      await loadScenarios();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  async function saveOverride() {
    if (!selectedId || ovItemId === '') return;
    busy = true;
    try {
      await plansApi.upsertScenarioItems(planId, selectedId, {
        items: [
          {
            item_id: Number(ovItemId),
            start_date: ovStart || null,
            end_date: ovEnd || null,
            notes: ovNotes.trim() || undefined,
          },
        ],
        remove: [...removedIds],
      });
      removedIds.clear();
      ovItemId = '';
      ovStart = '';
      ovEnd = '';
      ovNotes = '';
      await loadDetail(selectedId);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  const fmtDate = (d) => (d ? String(d).slice(0, 10) : '');
</script>

<div class="grid gap-4 lg:grid-cols-[300px_1fr]">
  <!-- Scenario list -->
  <div class="space-y-3">
    <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-3">
      <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosNew')}</h3>
      <div class="space-y-2">
        <input
          bind:value={newScenarioName}
          placeholder={t('plans.scenariosNamePlaceholder')}
          class="h-8 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        />
        <input
          bind:value={newScenarioDesc}
          placeholder="…"
          class="h-8 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
        />
        <button type="button" disabled={busy || !newScenarioName.trim()} onclick={createScenario} class="w-full rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50">
          {t('plans.scenariosNew')}
        </button>
      </div>
    </div>

    <div class="space-y-2">
      {#if loading && scenarios.length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
      {:else if scenarios.length === 0}
        <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosEmpty')}</p>
      {:else}
        {#each scenarios as s (s.id)}
          <div
            role="button"
            tabindex="0"
            class={`w-full cursor-pointer rounded-md border p-3 text-left transition-colors ${selectedId === s.id ? 'border-[var(--color-ds-accent-blue,#3b82f6)] bg-[var(--color-ds-surface,#fff)]' : 'border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]'}`}
            onclick={() => selectScenario(s.id)}
          >
            <div class="flex items-start justify-between gap-2">
              <p class="min-w-0 flex-1 truncate text-sm font-medium">{s.name}</p>
              <span class={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] ${s.status === 'active' ? 'bg-emerald-100 text-emerald-700' : s.status === 'archived' ? 'bg-slate-200 text-slate-600' : 'bg-slate-100 text-slate-500'}`}>
                {t(SCENARIO_STATUSES.find((st) => st.id === (s.status || 'draft'))?.labelKey ?? 'plans.scenariosStatusDraft')}
              </span>
              <button type="button" title={t('common.edit')} disabled={busy} onclick={(e) => { e.stopPropagation(); openScenarioEdit(s); }} class="shrink-0 p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-accent-blue,#2563eb)] disabled:opacity-50">
                <IconPencil size={14} />
              </button>
              <button type="button" title={t('common.delete')} disabled={busy} onclick={(e) => { e.stopPropagation(); deleteScenario(s.id); }} class="shrink-0 p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600 disabled:opacity-50">
                <IconX size={14} />
              </button>
            </div>
            {#if s.description}<p class="mt-1 line-clamp-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{s.description}</p>{/if}
          </div>
        {/each}
      {/if}
    </div>
  </div>

  <!-- Scenario detail -->
  <div class="min-w-0 space-y-4">
    {#if error && !detail}
      <p class="text-sm text-red-600">{error}</p>
    {:else if selectedId === null}
      <p class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4 text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosEmpty')}</p>
    {:else if !detail}
      <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
    {:else}
      <!-- Impact banner -->
      {#if detail.impact && (detail.impact.affected_item_count > 0 || detail.impact.max_shift_days > 0)}
        <div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm">
          <span class="font-semibold">{t('plans.scenariosImpact')}:</span>
          {t('plans.scenariosAffected', { count: detail.impact.affected_item_count })}
          {#if detail.impact.max_shift_days > 0} · {t('plans.scenariosMaxShift', { days: detail.impact.max_shift_days })}{/if}
        </div>
      {/if}

      <!-- Overrides -->
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)]">
        <h3 class="border-b border-[var(--color-ds-border,#e5e7eb)] px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosOverrides')}</h3>
        {#if (detail.items ?? []).length === 0}
          <p class="px-3 py-4 text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosNoOverrides')}</p>
        {:else}
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <tbody>
                {#each detail.items as ov (ov.item_id)}
                  {@const removed = removedIds.has(ov.item_id)}
                  <tr class={`border-b border-[var(--color-ds-border,#e5e7eb)] last:border-b-0 ${removed ? 'opacity-40' : ''}`}>
                    <td class="max-w-64 truncate px-3 py-2" title={ov.item_title}>
                      <span class="tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{ov.item_key}</span> {ov.item_title}
                    </td>
                    <td class="whitespace-nowrap px-3 py-2 tabular-nums text-xs">
                      {fmtDate(ov.start_date) && fmtDate(ov.end_date) ? `${fmtDate(ov.start_date)} → ${fmtDate(ov.end_date)}` : fmtDate(ov.due_date) || '—'}
                    </td>
                    <td class="max-w-48 truncate px-3 py-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]" title={ov.notes}>{ov.notes || ''}</td>
                    <td class="whitespace-nowrap px-3 py-2">
                      {#if ov.shift_days}
                        <span class="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium tabular-nums text-amber-700">{t('plans.scenariosShifted', { days: ov.shift_days })}</span>
                      {/if}
                      {#if (ov.downstream_count ?? 0) > 0}
                        <span class="ml-1 rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.scenariosDownstream', { count: ov.downstream_count })}</span>
                      {/if}
                    </td>
                    <td class="px-3 py-2">
                      <button type="button" title={t('plans.scenariosRemoveOverride')} disabled={busy || removed} onclick={() => removedIds.add(ov.item_id)} class="p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600 disabled:opacity-50">
                        <IconX size={14} />
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>

      <!-- Add override -->
      <div class="flex flex-wrap items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-3">
        <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.depsSource')}
          <select bind:value={ovItemId} class="mt-1 block h-8 w-56 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none">
            <option value="">—</option>
            {#each allItems as item (item.id)}
              <option value={String(item.id)}>{item.key} {item.title}</option>
            {/each}
          </select>
        </label>
        <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.scenariosStartDate')}
          <input type="date" bind:value={ovStart} class="mt-1 block h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none" />
        </label>
        <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.scenariosEndDate')}
          <input type="date" bind:value={ovEnd} class="mt-1 block h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none" />
        </label>
        <label class="min-w-40 flex-1 text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
          {t('plans.scenariosNotes')}
          <input bind:value={ovNotes} class="mt-1 block h-8 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none" />
        </label>
        <button type="button" disabled={busy || ovItemId === ''} onclick={saveOverride} class="h-8 rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50">
          {t('plans.scenariosSave')}
        </button>
      </div>
    {/if}
  </div>
</div>

{#if showEdit}
  <Modal isOpen={true} maxWidth="max-w-lg" onclose={() => (showEdit = false)}>
    <div class="px-8 py-6">
      <h2 class="mb-4 text-lg font-semibold">{t('plans.scenariosEdit')}</h2>
      <form
        class="space-y-4"
        onsubmit={(e) => {
          e.preventDefault();
          saveScenarioEdit();
        }}
      >
        <label class="block">
          <span class="mb-1 block text-sm font-medium">{t('plans.name')}</span>
          <input
            type="text"
            bind:value={editName}
            required
            class="h-9 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
          />
        </label>
        <label class="block">
          <span class="mb-1 block text-sm font-medium">{t('plans.description')}</span>
          <textarea
            bind:value={editDesc}
            rows={3}
            class="w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 py-1.5 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
          ></textarea>
        </label>
        <label class="block">
          <span class="mb-1 block text-sm font-medium">{t('plans.status')}</span>
          <select
            bind:value={editStatus}
            class="h-9 w-full rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
          >
            {#each SCENARIO_STATUSES as st (st.id)}
              <option value={st.id}>{t(st.labelKey)}</option>
            {/each}
          </select>
        </label>
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" onclick={() => (showEdit = false)} class="rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-3 py-1.5 text-sm hover:bg-[var(--color-ds-surface-hover,#f3f4f6)]">
            {t('common.cancel')}
          </button>
          <button type="submit" disabled={busy || !editName.trim()} class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50">
            {t('common.save')}
          </button>
        </div>
      </form>
    </div>
  </Modal>
{/if}
