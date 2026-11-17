<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import IconArrowRight from '@tabler/icons-svelte-runes/icons/arrow-right';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  let deps = $state([]);
  let items = $state([]);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);

  // Add form state.
  let sourceId = $state('');
  let targetId = $state('');
  let depType = $state('blocks');

  async function load() {
    loading = true;
    error = '';
    try {
      const [depsRes, itemsRes] = await Promise.all([
        plansApi.listDependencies(planId),
        plansApi.getItems(planId, { limit: 1000 }),
      ]);
      deps = Array.isArray(depsRes) ? depsRes : (depsRes?.dependencies ?? []);
      items = itemsRes?.items ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const canAdd = $derived(sourceId !== '' && targetId !== '' && sourceId !== targetId && !busy);

  async function add() {
    if (!canAdd) return;
    busy = true;
    error = '';
    try {
      await plansApi.addDependency(planId, Number(sourceId), {
        target_item_id: Number(targetId),
        dep_type: depType,
      });
      sourceId = '';
      targetId = '';
      const res = await plansApi.listDependencies(planId);
      deps = Array.isArray(res) ? res : (res?.dependencies ?? []);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  async function remove(dep) {
    busy = true;
    try {
      await plansApi.removeDependency(planId, dep.source_item_id, dep.id);
      const res = await plansApi.listDependencies(planId);
      deps = Array.isArray(res) ? res : (res?.dependencies ?? []);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      busy = false;
    }
  }

  const itemLabel = (id, key, title) => `${key} ${title}`.trim();
</script>

<div class="space-y-4">
  <!-- Add form -->
  <div class="flex flex-wrap items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-3">
    <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
      {t('plans.depsSource')}
      <select bind:value={sourceId} class="mt-1 block h-8 w-56 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none">
        <option value="">—</option>
        {#each items as item (item.id)}
          <option value={String(item.id)}>{itemLabel(item.id, item.key, item.title)}</option>
        {/each}
      </select>
    </label>

    <span class="pb-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.depsTarget')}</span>

    <label class="text-xs font-medium text-[var(--color-ds-text-muted,#6b7280)]">
      &nbsp;
      <select bind:value={targetId} class="mt-1 block h-8 w-56 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none">
        <option value="">—</option>
        {#each items as item (item.id)}
          <option value={String(item.id)}>{itemLabel(item.id, item.key, item.title)}</option>
        {/each}
      </select>
    </label>

    <select bind:value={depType} class="h-8 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none">
      <option value="blocks">{t('plans.depsTypeBlocks')}</option>
      <option value="related">{t('plans.depsTypeRelated')}</option>
    </select>

    <button type="button" disabled={!canAdd} onclick={add} class="h-8 rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50">
      {t('plans.depsAdd')}
    </button>
  </div>

  {#if error}
    <p class="text-sm text-red-600">{error}</p>
  {/if}

  <!-- List -->
  {#if loading && deps.length === 0}
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
  {:else if deps.length === 0}
    <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.depsEmpty')}</p>
  {:else}
    <div class="overflow-hidden rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      {#each deps as dep (dep.id)}
        <div class="flex items-center gap-3 border-b border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] px-3 py-2 text-sm last:border-b-0">
          <span class="min-w-0 flex-1 truncate" title={dep.source_item_title}>
            <span class="tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{dep.source_item_key}</span> {dep.source_item_title}
          </span>
          <IconArrowRight size={14} class="shrink-0 text-[var(--color-ds-text-muted,#6b7280)]" />
          <span class="min-w-0 flex-1 truncate" title={dep.target_item_title}>
            <span class="tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{dep.target_item_key}</span> {dep.target_item_title}
          </span>
          <span class={`shrink-0 rounded-full px-2 py-0.5 text-xs ${dep.dep_type === 'blocks' ? 'bg-red-100 text-red-700' : 'bg-[var(--color-ds-surface-hover,#f3f4f6)] text-[var(--color-ds-text-muted,#6b7280)]'}`}>
            {dep.dep_type === 'blocks' ? t('plans.depsTypeBlocks') : t('plans.depsTypeRelated')}
          </span>
          <button type="button" title={t('plans.depsRemove')} disabled={busy} onclick={() => remove(dep)} class="shrink-0 p-1 text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600 disabled:opacity-50">
            <IconX size={14} />
          </button>
        </div>
      {/each}
    </div>
  {/if}
</div>
