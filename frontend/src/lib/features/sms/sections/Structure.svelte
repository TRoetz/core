<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { formatDate, entityTypeLabel } from '../utils.js';

  let loading = $state(true);
  let error = $state('');
  /** @type {Array<Record<string, any>>} */
  let kinds = $state([]);
  /** @type {Array<Record<string, any>>} */
  let nodes = $state([]);
  /** @type {Array<Record<string, any>>} */
  let stakeholders = $state([]);

  // Node list filters.
  let kindFilter = $state('');
  let search = $state('');

  // Selected node detail (with members).
  /** @type {Record<string, any> | null} */
  let selectedNode = $state(null);
  let memberForm = $state({ stakeholder_id: '', role: '', start_date: '', end_date: '', notes: '' });
  let memberRoles = $derived(selectedNode ? (kindById(selectedNode.kind_id)?.roles ?? []) : []);

  // Node create/edit form.
  let nodeFormOpen = $state(false);
  /** @type {Record<string, any> | null} */
  let editingNode = $state(null);
  let nodeForm = $state({ name: '', kind_id: '', parent_id: '', description: '', location_text: '', notes: '' });

  // Kind admin.
  let kindFormOpen = $state(false);
  /** @type {Record<string, any> | null} */
  let editingKind = $state(null);
  let kindForm = $state({ key: '', label: '', description: '', allows_children: true, child_kinds: '' });
  // Roles editor for one kind.
  /** @type {Record<string, any> | null} */
  let rolesForKind = $state(null);
  let roleForm = $state({ role_key: '', label: '' });

  function kindById(id) {
    return kinds.find((k) => k.id === Number(id));
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [kindRows, nodeRows, stakeholderRows] = await Promise.all([
        smsApi.getNodeKinds(),
        smsApi.getNodes(kindFilter ? { kind_id: kindFilter } : {}),
        smsApi.getStakeholders({}),
      ]);
      kinds = (kindRows ?? []).map((k) => ({ ...k, roles: [] }));
      nodes = nodeRows ?? [];
      stakeholders = stakeholderRows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadRoles(kindId) {
    try {
      const roles = await smsApi.getNodeKindRoles(kindId);
      const k = kinds.find((x) => x.id === kindId);
      if (k) k.roles = roles ?? [];
      return roles ?? [];
    } catch {
      return [];
    }
  }

  async function selectNode(node) {
    try {
      selectedNode = await smsApi.getNode(node.id);
      memberForm = { stakeholder_id: '', role: '', start_date: '', end_date: '', notes: '' };
      const roles = await loadRoles(selectedNode.kind_id);
      if (roles.length > 0) memberForm.role = roles[0].role_key;
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function openCreateNode() {
    editingNode = null;
    nodeForm = { name: '', kind_id: kinds.length ? String(kinds.find((k) => k.allows_children)?.id ?? kinds[0].id) : '', parent_id: '', description: '', location_text: '', notes: '' };
    nodeFormOpen = true;
  }

  function openEditNode(node) {
    editingNode = node;
    nodeForm = {
      name: node.name,
      kind_id: String(node.kind_id),
      parent_id: node.parent_id ? String(node.parent_id) : '',
      description: node.description ?? '',
      location_text: node.location_text ?? '',
      notes: node.notes ?? '',
    };
    nodeFormOpen = true;
  }

  async function saveNode() {
    if (!nodeForm.name.trim()) return;
    const payload = {
      name: nodeForm.name,
      kind_id: Number(nodeForm.kind_id),
      description: nodeForm.description,
      location_text: nodeForm.location_text,
      notes: nodeForm.notes,
    };
    if (nodeForm.parent_id) payload.parent_id = Number(nodeForm.parent_id);
    try {
      if (editingNode) {
        await smsApi.updateNode(editingNode.id, payload);
      } else {
        await smsApi.createNode(payload);
      }
      nodeFormOpen = false;
      await load();
      if (selectedNode && (!editingNode || editingNode.id === selectedNode.id)) {
        const fresh = nodes.find((n) => n.id === selectedNode.id);
        if (fresh) selectNode(fresh);
      }
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteNode(node) {
    if (!confirm(t('sms.deleteMessage'))) return;
    try {
      await smsApi.deleteNode(node.id);
      if (selectedNode?.id === node.id) selectedNode = null;
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addMember() {
    if (!selectedNode || !memberForm.stakeholder_id) return;
    const payload = { stakeholder_id: Number(memberForm.stakeholder_id), role: memberForm.role };
    if (memberForm.start_date) payload.start_date = memberForm.start_date;
    if (memberForm.end_date) payload.end_date = memberForm.end_date;
    if (memberForm.notes) payload.notes = memberForm.notes;
    try {
      await smsApi.addNodeMember(selectedNode.id, payload);
      memberForm = { stakeholder_id: '', role: memberRoles[0]?.role_key ?? '', start_date: '', end_date: '', notes: '' };
      selectNode(selectedNode);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeMember(memberId) {
    if (!selectedNode) return;
    try {
      await smsApi.removeNodeMember(memberId);
      selectNode(selectedNode);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  // ---- Kind admin ----------------------------------------------------------

  function openCreateKind() {
    editingKind = null;
    kindForm = { key: '', label: '', description: '', allows_children: true, child_kinds: '' };
    kindFormOpen = true;
  }

  async function openEditKind(kind) {
    editingKind = kind;
    kindForm = {
      key: kind.key,
      label: kind.label,
      description: kind.description ?? '',
      allows_children: !!kind.allows_children,
      child_kinds: kind.child_kinds ?? '',
    };
    kindFormOpen = true;
  }

  async function saveKind() {
    if (!kindForm.key.trim() || !kindForm.label.trim()) return;
    const payload = { ...kindForm, allows_children: !!kindForm.allows_children };
    try {
      if (editingKind) await smsApi.updateNodeKind(editingKind.id, payload);
      else await smsApi.createNodeKind(payload);
      kindFormOpen = false;
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteKind(kind) {
    if (!confirm(t('sms.deleteMessage'))) return;
    try {
      await smsApi.deleteNodeKind(kind.id);
      if (rolesForKind?.id === kind.id) rolesForKind = null;
      await load();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function openRolesEditor(kind) {
    const roles = await loadRoles(kind.id);
    rolesForKind = { ...kind, roles };
    roleForm = { role_key: '', label: '' };
  }

  async function addRole() {
    if (!rolesForKind || !roleForm.role_key.trim()) return;
    try {
      await smsApi.addNodeKindRole(rolesForKind.id, { role_key: roleForm.role_key, label: roleForm.label });
      const roles = await loadRoles(rolesForKind.id);
      rolesForKind = { ...rolesForKind, roles };
      roleForm = { role_key: '', label: '' };
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteRole(roleId) {
    if (!rolesForKind) return;
    try {
      await smsApi.deleteNodeKindRole(roleId);
      const roles = await loadRoles(rolesForKind.id);
      rolesForKind = { ...rolesForKind, roles };
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  onMount(load);
</script>

<div class="space-y-6 px-6 py-4">
  {#if loading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
  {:else if error && nodes.length === 0}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
  {:else}
    <!-- Nodes -->
    <section>
      <div class="mb-2 flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.nodes')}</h2>
        <select
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          value={kindFilter}
          onchange={(e) => { kindFilter = e.target.value; load(); }}
        >
          <option value="">{t('sms.allKinds')}</option>
          {#each kinds as k (k.id)}<option value={String(k.id)}>{k.label}</option>{/each}
        </select>
        <input
          type="search"
          class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('sms.searchPlaceholder')}
          value={search}
          oninput={(e) => (search = e.target.value)}
        />
        <button type="button" class="ml-auto rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={openCreateNode}>
          {t('sms.newNode')}
        </button>
      </div>

      {#if nodeFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{editingNode ? t('sms.editNode') : t('sms.newNode')}</h3>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-3">
            <label class="text-xs">
              {t('sms.name')} *
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={nodeForm.name} oninput={(e) => (nodeForm.name = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.kind')} *
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={nodeForm.kind_id} onchange={(e) => (nodeForm.kind_id = e.target.value)}>
                {#each kinds as k (k.id)}<option value={String(k.id)}>{k.label}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.parent')}
              <select class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={nodeForm.parent_id} onchange={(e) => (nodeForm.parent_id = e.target.value)}>
                <option value="">{t('sms.rootParent')}</option>
                {#each nodes.filter((n) => n.id !== editingNode?.id) as n (n.id)}<option value={String(n.id)}>{n.name}</option>{/each}
              </select>
            </label>
            <label class="text-xs md:col-span-2">
              {t('sms.locationText')}
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={nodeForm.location_text} oninput={(e) => (nodeForm.location_text = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.notes')}
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={nodeForm.notes} oninput={(e) => (nodeForm.notes = e.target.value)} />
            </label>
          </div>
          <div class="mt-2 flex gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={saveNode}>{t('sms.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={() => (nodeFormOpen = false)}>{t('sms.back')}</button>
          </div>
        </div>
      {/if}

      {#if error && nodes.length > 0}<div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>{/if}

      {#if nodes.filter((n) => !search || n.name.toLowerCase().includes(search.toLowerCase())).length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.name')}</th>
              <th class="py-1.5 pr-3">{t('sms.kind')}</th>
              <th class="py-1.5 pr-3">{t('sms.entity')}</th>
              <th class="py-1.5 pr-3 text-right">{t('sms.membersCount')}</th>
              <th class="py-1.5 pr-3 text-right">{t('sms.childrenCount')}</th>
              <th class="py-1.5"></th>
            </tr>
          </thead>
          <tbody>
            {#each nodes.filter((n) => !search || n.name.toLowerCase().includes(search.toLowerCase())) as n (n.id)}
              <tr class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0 hover:bg-slate-50" class:selected={selectedNode?.id === n.id} onclick={() => selectNode(n)}>
                <td class="py-1.5 pr-3 font-medium">{n.name}</td>
                <td class="py-1.5 pr-3">{n.kind_label}</td>
                <td class="py-1.5 pr-3">{n.entity_type ? `${entityTypeLabel(n.entity_type)} #${n.entity_id}` : '—'}</td>
                <td class="py-1.5 pr-3 text-right">{n.member_count ?? 0}</td>
                <td class="py-1.5 pr-3 text-right">{n.child_count ?? 0}</td>
                <td class="py-1.5 text-right">
                  <button type="button" class="mr-2 text-[var(--color-ds-accent,#2563eb)]" onclick={(e) => { e.stopPropagation(); openEditNode(n); }}>{t('sms.edit')}</button>
                  <button type="button" class="text-red-600" onclick={(e) => { e.stopPropagation(); deleteNode(n); }}>{t('sms.delete')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Selected node detail: occupancy / membership -->
      {#if selectedNode}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{selectedNode.name} — {t('sms.membersList')}</h3>
          {#if (selectedNode.members ?? []).length === 0}
            <div class="mb-2 rounded-md border border-dashed border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noMembers')}</div>
          {:else}
            <table class="mb-2 w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1 pr-3">{t('sms.stakeholder')}</th>
                  <th class="py-1 pr-3">{t('sms.memberRole')}</th>
                  <th class="py-1 pr-3">{t('sms.startDate')}</th>
                  <th class="py-1 pr-3">{t('sms.endDate')}</th>
                  <th class="py-1"></th>
                </tr>
              </thead>
              <tbody>
                {#each selectedNode.members as m (m.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                    <td class="py-1 pr-3 font-medium">{m.name}<span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {m.organization}</span></td>
                    <td class="py-1 pr-3">{m.role || '—'}</td>
                    <td class="py-1 pr-3 whitespace-nowrap">{formatDate(m.start_date)}</td>
                    <td class="py-1 pr-3 whitespace-nowrap">{formatDate(m.end_date)}</td>
                    <td class="py-1 text-right"><button type="button" class="text-red-600" onclick={() => removeMember(m.id)}>{t('sms.removeMember')}</button></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}

          <div class="flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-2">
            <label class="text-xs">
              {t('sms.stakeholder')} *
              <select class="mt-0.5 block w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={memberForm.stakeholder_id} onchange={(e) => (memberForm.stakeholder_id = e.target.value)}>
                <option value="">{t('sms.selectStakeholder')}</option>
                {#each stakeholders as s (s.id)}<option value={String(s.id)}>{s.name}{s.organization ? ` (${s.organization})` : ''}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('sms.memberRole')}
              <input list="member-role-options" class="mt-0.5 block w-32 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={memberForm.role} oninput={(e) => (memberForm.role = e.target.value)} />
              <datalist id="member-role-options">
                {#each memberRoles as r (r.role_key)}<option value={r.role_key}>{r.label}</option>{/each}
              </datalist>
            </label>
            <label class="text-xs">
              {t('sms.startDate')}
              <input type="date" class="mt-0.5 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={memberForm.start_date} oninput={(e) => (memberForm.start_date = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.endDate')}
              <input type="date" class="mt-0.5 block rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={memberForm.end_date} oninput={(e) => (memberForm.end_date = e.target.value)} />
            </label>
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={addMember}>{t('sms.add')}</button>
          </div>
        </div>
      {/if}
    </section>

    <!-- Node kinds admin -->
    <section>
      <div class="mb-2 flex items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.nodeKinds')}</h2>
        <button type="button" class="ml-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={openCreateKind}>{t('sms.newNodeKind')}</button>
      </div>

      {#if kindFormOpen}
        <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{editingKind ? t('sms.editNodeKind') : t('sms.newNodeKind')}</h3>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-4">
            <label class="text-xs">
              {t('sms.kindKey')} *
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={kindForm.key} oninput={(e) => (kindForm.key = e.target.value)} disabled={!!editingKind?.is_system} />
            </label>
            <label class="text-xs">
              {t('sms.label')} *
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={kindForm.label} oninput={(e) => (kindForm.label = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.childKinds')}
              <input class="mt-0.5 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" placeholder="building, road_segment" value={kindForm.child_kinds} oninput={(e) => (kindForm.child_kinds = e.target.value)} />
            </label>
            <label class="flex items-end gap-2 pb-1 text-xs">
              <input type="checkbox" checked={kindForm.allows_children} onchange={(e) => (kindForm.allows_children = e.target.checked)} />
              {t('sms.allowsChildren')}
            </label>
          </div>
          <div class="mt-2 flex gap-2">
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={saveKind}>{t('sms.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs" onclick={() => (kindFormOpen = false)}>{t('sms.back')}</button>
          </div>
        </div>
      {/if}

      <table class="w-full text-left text-xs">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('sms.kindKey')}</th>
            <th class="py-1.5 pr-3">{t('sms.label')}</th>
            <th class="py-1.5 pr-3 text-right">{t('sms.count')}</th>
            <th class="py-1.5"></th>
          </tr>
        </thead>
        <tbody>
          {#each kinds as k (k.id)}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
              <td class="py-1.5 pr-3 font-mono">{k.key}{#if k.is_system}<span class="ml-1 rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px]">{t('sms.systemKind')}</span>{/if}</td>
              <td class="py-1.5 pr-3">{k.label}</td>
              <td class="py-1.5 pr-3 text-right">{k.node_count ?? 0}</td>
              <td class="py-1.5 text-right">
                <button type="button" class="mr-2 text-[var(--color-ds-accent,#2563eb)]" onclick={() => openRolesEditor(k)}>{t('sms.roles')}</button>
                <button type="button" class="mr-2 text-[var(--color-ds-accent,#2563eb)]" onclick={() => openEditKind(k)}>{t('sms.edit')}</button>
                {#if !k.is_system}<button type="button" class="text-red-600" onclick={() => deleteKind(k)}>{t('sms.delete')}</button>{/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>

      {#if rolesForKind}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <h3 class="mb-2 text-xs font-semibold">{t('sms.roles')}: {rolesForKind.label}</h3>
          {#if (rolesForKind.roles ?? []).length === 0}
            <div class="mb-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRoles')}</div>
          {:else}
            <ul class="mb-2 space-y-1">
              {#each rolesForKind.roles as r (r.id)}
                <li class="flex items-center gap-2 text-xs">
                  <span class="font-mono">{r.role_key}</span>
                  <span>{r.label}</span>
                  <button type="button" class="ml-auto text-red-600" onclick={() => deleteRole(r.id)}>{t('sms.delete')}</button>
                </li>
              {/each}
            </ul>
          {/if}
          <div class="flex items-end gap-2">
            <label class="text-xs">
              {t('sms.kindKey')}
              <input class="mt-0.5 block w-32 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={roleForm.role_key} oninput={(e) => (roleForm.role_key = e.target.value)} />
            </label>
            <label class="text-xs">
              {t('sms.label')}
              <input class="mt-0.5 block w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1" value={roleForm.label} oninput={(e) => (roleForm.label = e.target.value)} />
            </label>
            <button type="button" class="rounded-md bg-[var(--color-ds-accent,#2563eb)] px-3 py-1 text-xs font-medium text-white" onclick={addRole}>{t('sms.add')}</button>
          </div>
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
