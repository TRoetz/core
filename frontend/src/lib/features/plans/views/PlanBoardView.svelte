<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { items as itemsApi } from '../../../api/items.js';
  import { workspaces as workspacesApi } from '../../../api/workspaces.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  const COLUMNS = [
    { id: 'to_do', labelKey: 'plans.boardColToDo' },
    { id: 'in_progress', labelKey: 'plans.boardColInProgress' },
    { id: 'done', labelKey: 'plans.boardColDone' },
  ];

  let items = $state([]);
  let loading = $state(true);
  let error = $state('');
  let moveError = $state('');
  let movingId = $state(null); // item id with an in-flight transition
  let dragOverCol = $state(null);

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await plansApi.getItems(planId, { limit: 1000 });
      items = res?.items ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const byColumn = $derived.by(() => {
    const map = new Map(COLUMNS.map((c) => [c.id, []]));
    for (const item of items) {
      const cat = COLUMNS.some((c) => c.id === item.status_category) ? item.status_category : 'to_do';
      map.get(cat).push(item);
    }
    return map;
  });

  function onDragStart(e, item) {
    e.dataTransfer.setData('text/plain', String(item.id));
    e.dataTransfer.effectAllowed = 'move';
  }

  async function onDrop(e, categoryId) {
    e.preventDefault();
    dragOverCol = null;
    moveError = '';
    const itemId = Number(e.dataTransfer.getData('text/plain'));
    const item = items.find((i) => i.id === itemId);
    if (!item || movingId !== null) return;
    if (item.status_category === categoryId) return;

    movingId = itemId;
    try {
      // Resolve a target status in the destination category from the item's own workspace.
      const statuses = await workspacesApi.getStatuses(item.workspace_id);
      const candidates = (statuses ?? []).filter((s) => s.category_builtin_key === categoryId);
      if (candidates.length === 0) {
        moveError = t('plans.boardNoStatus');
        return;
      }
      const target =
        candidates.find((s) => s.name.toLowerCase() === item.status_name?.toLowerCase()) ?? candidates[0];

      const updated = await itemsApi.transition(item.id, target.id);
      // Apply the server-confirmed status locally.
      items = items.map((i) => (i.id === itemId ? { ...i, ...updated } : i));
    } catch (err) {
      moveError = err?.message || t('plans.boardMoveFailed');
    } finally {
      movingId = null;
    }
  }
</script>

{#if loading && items.length === 0}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
{:else if error && items.length === 0}
  <p class="text-sm text-red-600">{error}</p>
{:else}
  <div class="space-y-3">
    {#if moveError}
      <p class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{moveError}</p>
    {/if}

    <div class="grid gap-4 md:grid-cols-3">
      {#each COLUMNS as col (col.id)}
        {@const colItems = byColumn.get(col.id) ?? []}
        <div
          class={`rounded-md border bg-[var(--color-ds-surface-hover,#f3f4f6)]/50 p-2 transition-colors ${dragOverCol === col.id ? 'border-[var(--color-ds-accent-blue,#3b82f6)]' : 'border-[var(--color-ds-border,#e5e7eb)]'}`}
          ondragover={(e) => {
            e.preventDefault();
            dragOverCol = col.id;
          }}
          ondragleave={() => (dragOverCol = null)}
          ondrop={(e) => onDrop(e, col.id)}
        >
          <div class="mb-2 flex items-center justify-between px-1">
            <h3 class="text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t(col.labelKey)}</h3>
            <span class="rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">
              {colItems.length}
            </span>
          </div>

          <div class="min-h-24 space-y-2">
            {#each colItems as item (item.id)}
              <div
                draggable={movingId === null}
                ondragstart={(e) => onDragStart(e, item)}
                class={`cursor-grab rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-2.5 shadow-sm active:cursor-grabbing ${movingId === item.id ? 'pointer-events-none opacity-50' : ''}`}
              >
                <p class="truncate text-xs font-medium tabular-nums text-[var(--color-ds-text-muted,#6b7280)]">{item.key}</p>
                <p class="mt-1 line-clamp-2 text-sm leading-snug" title={item.title}>{item.title}</p>
                <div class="mt-2 flex items-center gap-2 text-[11px] text-[var(--color-ds-text-muted,#6b7280)]">
                  {#if item.assignee_avatar_url}
                    <img src={item.assignee_avatar_url} alt="" class="h-4 w-4 rounded-full" />
                  {/if}
                  <span class="truncate">{item.workspace_name}</span>
                  {#if item.assignee_name}<span class="shrink-0">· {item.assignee_name}</span>{/if}
                </div>
              </div>
            {:else}
              <p class="px-1 py-4 text-center text-xs text-[var(--color-ds-text-muted,#6b7280)]">—</p>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  </div>
{/if}
