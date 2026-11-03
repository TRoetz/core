<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import PageHeader from '../../layout/PageHeader.svelte';
  import IconNotebook from '@tabler/icons-svelte-runes/icons/notebook';
  import IconSearch from '@tabler/icons-svelte-runes/icons/search';
  import { pages as pagesApi } from '../../api/pages.js';
  import { workspaces as wsApi } from '../../api/workspaces.js';

  let workspaces = $state([]);
  /** workspace_id -> [{ page_id, title }] */
  let pagesByWorkspace = $state(new Map());
  let loading = $state(true);
  let error = $state('');
  let query = $state('');

  const filteredGroups = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const groups = [];
    for (const ws of workspaces) {
      const rows = pagesByWorkspace.get(ws.id) ?? [];
      const visible = q ? rows.filter((p) => p.title.toLowerCase().includes(q)) : rows;
      if (visible.length > 0) groups.push({ workspace: ws, pages: visible });
    }
    return groups;
  });

  async function load() {
    loading = true;
    error = '';
    try {
      const wss = await wsApi.getAll();
      workspaces = Array.isArray(wss) ? wss : [];
      // The titles endpoint caps at 50 workspaces per request.
      const ids = workspaces.map((w) => w.id);
      let rows = [];
      for (let i = 0; i < ids.length; i += 50) {
        const chunk = ids.slice(i, i + 50);
        if (!chunk.length) continue;
        // A workspace the user cannot access contributes no rows.
        const r = await pagesApi.getTitles(chunk).catch(() => []);
        rows = rows.concat(Array.isArray(r) ? r : []);
      }
      const m = new Map();
      for (const row of rows) {
        if (!m.has(row.workspace_id)) m.set(row.workspace_id, []);
        m.get(row.workspace_id).push({ page_id: row.page_id, title: row.title });
      }
      pagesByWorkspace = m;
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<PageHeader icon={IconNotebook} title={t('nav.pages')} subtitle={t('pages.subtitle')} marginClass="mt-4 mb-8">
  <div class="relative w-full max-w-sm">
    <IconSearch size={14} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--color-ds-text-muted,#6b7280)]" />
    <input
      type="search"
      class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input py-1.5 pl-8 pr-3 text-xs"
      placeholder={t('pages.searchPlaceholder')}
      bind:value={query}
    />
  </div>
</PageHeader>

{#if loading}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('p3m.loading')}
  </div>
{:else if error}
  <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
{:else if filteredGroups.length === 0}
  <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('pages.noPages')}
  </div>
{:else}
  <div class="space-y-6">
    {#each filteredGroups as group (group.workspace.id)}
      <section>
        <h2 class="mb-1.5 text-sm font-semibold">{group.workspace.name}</h2>
        <ul class="divide-y divide-[var(--color-ds-border,#e5e7eb)] rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
          {#each group.pages as page (page.page_id)}
            <li>
              <a href="/workspaces/{group.workspace.id}/pages/{page.page_id}" class="block px-3 py-2 text-xs hover:bg-ds-background-neutral-hovered">
                {page.title}
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
{/if}
