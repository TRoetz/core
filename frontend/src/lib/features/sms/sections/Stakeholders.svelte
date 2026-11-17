<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import {
    CLASSIFICATIONS,
    ENGAGEMENT_STATUSES,
    LIFECYCLE_STAGES,
    RELATIONSHIP_STATUSES,
    classificationLabel,
    engagementLabel,
    lifecycleLabel,
    relationshipLabel,
    relationshipClass,
    engagementClass,
    healthClass,
  } from '../utils.js';
  import { confirm } from '../../../composables/useConfirm.js';
  import SMSStakeholderDetail from './SMSStakeholderDetail.svelte';
  import SMSStakeholderForm from './SMSStakeholderForm.svelte';

  let loading = $state(true);
  let error = $state('');
  /** @type {Array<Record<string, any>>} */
  let stakeholders = $state([]);
  let selectedId = $state(null);

  // Filters.
  let search = $state('');
  let filterClassification = $state('');
  let filterEngagement = $state('');
  let filterLifecycle = $state('');
  let filterRelationship = $state('');
  let filterGroup = $state('');

  // Groups (named collections for engagement planning).
  /** @type {Array<Record<string, any>>} */
  let groups = $state([]);
  let groupForm = $state({ name: '', description: '' });
  let savingGroup = $state(false);

  // Full-profile create form (mirrors the profile editor field set).
  let showCreate = $state(false);

  // Duplicate detection + merge.
  /** @type {Array<{key:string, stakeholders:Array<Record<string, any>>}>} */
  let dupGroups = $state([]);
  let checkingDups = $state(false);
  let mergingKey = $state('');
  /** Per duplicate-group key: id of the record to keep. */
  let mergeTargets = $state({});

  // Group editing + membership management.
  let editingGroupId = $state(null);
  let groupEditForm = $state({ name: '', description: '' });

  const filtered = $derived(
    stakeholders.filter((s) => {
      if (search) {
        const q = search.toLowerCase();
        if (!`${s.name ?? ''} ${s.organization ?? ''}`.toLowerCase().includes(q)) return false;
      }
      if (filterClassification && s.role_classification !== filterClassification) return false;
      if (filterEngagement && s.engagement_status !== filterEngagement) return false;
      if (filterLifecycle && s.lifecycle_stage !== filterLifecycle) return false;
      if (filterRelationship && s.relationship_status !== filterRelationship) return false;
      if (filterGroup) {
        const g = groups.find((x) => x.id === Number(filterGroup));
        const ids = new Set((g?.members ?? []).map((m) => m.id));
        if (!ids.has(s.id)) return false;
      }
      return true;
    })
  );

  async function load(silent = false) {
    // Silent refreshes (e.g. after a detail-view mutation) must not flip
    // `loading`: the spinner branch unmounts the open detail view and resets it.
    if (!silent) loading = true;
    error = '';
    try {
      stakeholders = (await smsApi.getStakeholders()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadGroups() {
    try {
      groups = (await smsApi.getGroups()) ?? [];
    } catch {
      groups = [];
    }
  }

  async function createGroup() {
    if (!groupForm.name.trim() || savingGroup) return;
    savingGroup = true;
    try {
      await smsApi.createGroup({ name: groupForm.name.trim(), description: groupForm.description });
      groupForm = { name: '', description: '' };
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingGroup = false;
    }
  }

  async function removeGroup(id) {
    try {
      await smsApi.deleteGroup(id);
      if (filterGroup === String(id)) filterGroup = '';
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function handleCreated(s) {
    showCreate = false;
    selectedId = s.id ?? null;
    load();
  }

  async function removeStakeholder(id, name) {
    const ok = await confirm({
      title: t('sms.deleteTitle'),
      message: t('sms.deleteMessage', { name }),
      confirmText: t('common.delete'),
    });
    if (!ok) return;
    try {
      await smsApi.deleteStakeholder(id);
      if (selectedId === id) selectedId = null;
      await load(true);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function checkDuplicates() {
    checkingDups = true;
    try {
      const res = await smsApi.getDuplicateStakeholders();
      dupGroups = res?.groups ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      checkingDups = false;
    }
  }

  /** Folds every non-target member of a duplicate group into the selected target. */
  async function mergeGroup(group) {
    const targetId = mergeTargets[group.key];
    if (!targetId || mergingKey) return;
    const sourceIds = group.stakeholders.filter((s) => s.id !== targetId).map((s) => s.id);
    if (sourceIds.length === 0) return;
    const target = group.stakeholders.find((s) => s.id === targetId);
    const ok = await confirm({
      title: t('sms.mergeConfirmTitle'),
      message: sourceIds.map((id) => {
        const src = group.stakeholders.find((s) => s.id === id);
        return t('sms.mergeFoldIn', { source: src?.name ?? `#${id}`, target: target?.name ?? `#${targetId}` });
      }).join('\n'),
      confirmText: t('common.confirm'),
    });
    if (!ok) return;
    mergingKey = group.key;
    try {
      for (const sourceId of sourceIds) {
        await smsApi.mergeStakeholder(targetId, sourceId);
      }
      delete mergeTargets[group.key];
      dupGroups = dupGroups.filter((g) => g.key !== group.key);
      if (selectedId && sourceIds.includes(selectedId)) selectedId = targetId;
      await load();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      mergingKey = '';
    }
  }

  function startEditGroup(g) {
    editingGroupId = g.id;
    groupEditForm = { name: g.name ?? '', description: g.description ?? '' };
  }

  function toggleEditGroup(g) {
    if (editingGroupId === g.id) {
      editingGroupId = null;
      return;
    }
    startEditGroup(g);
  }

  async function saveGroupEdit() {
    if (editingGroupId === null || !groupEditForm.name.trim()) return;
    try {
      await smsApi.updateGroup(editingGroupId, { name: groupEditForm.name.trim(), description: groupEditForm.description });
      editingGroupId = null;
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function addMember(groupId, stakeholderId) {
    if (!stakeholderId) return;
    try {
      await smsApi.addGroupMember(groupId, Number(stakeholderId));
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function removeMember(groupId, stakeholderId) {
    try {
      await smsApi.removeGroupMember(groupId, stakeholderId);
      await loadGroups();
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  onMount(() => {
    load();
    loadGroups();
  });
</script>

<div class="space-y-4 px-6 py-4">
  {#if loading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
  {:else if error && stakeholders.length === 0}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
  {:else}
    <!-- Filters + quick create -->
    <div class="flex flex-wrap items-center gap-2">
      <input
        class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
        placeholder={t('sms.searchPlaceholder')}
        value={search}
        oninput={(e) => (search = e.target.value)}
      />
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={filterClassification} onchange={(e) => (filterClassification = e.target.value)}>
        <option value="">{t('sms.allClassifications')}</option>
        {#each CLASSIFICATIONS as c (c)}<option value={c}>{classificationLabel(c)}</option>{/each}
      </select>
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={filterEngagement} onchange={(e) => (filterEngagement = e.target.value)}>
        <option value="">{t('sms.allEngagements')}</option>
        {#each ENGAGEMENT_STATUSES as s (s)}<option value={s}>{engagementLabel(s)}</option>{/each}
      </select>
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={filterLifecycle} onchange={(e) => (filterLifecycle = e.target.value)}>
        <option value="">{t('sms.allLifecycles')}</option>
        {#each LIFECYCLE_STAGES as s (s)}<option value={s}>{lifecycleLabel(s)}</option>{/each}
      </select>
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={filterRelationship} onchange={(e) => (filterRelationship = e.target.value)}>
        <option value="">{t('sms.allRelationships')}</option>
        {#each RELATIONSHIP_STATUSES as s (s)}<option value={s}>{relationshipLabel(s)}</option>{/each}
      </select>
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={filterGroup} onchange={(e) => (filterGroup = e.target.value)}>
        <option value="">{t('sms.allGroups')}</option>
        {#each groups as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
      </select>

      <span class="mx-1 h-5 w-px bg-[var(--color-ds-border,#e5e7eb)]" aria-hidden="true"></span>

      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (showCreate = !showCreate)}>
        {t('sms.newStakeholder')}
      </button>
    </div>

    <!-- Full-profile create form -->
    {#if showCreate}
      <SMSStakeholderForm onCreated={handleCreated} onCancel={() => (showCreate = false)} />
    {/if}

    <!-- Groups management -->
    <details class="rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      <summary class="cursor-pointer px-3 py-2 text-xs font-medium">{t('sms.groups')}</summary>
      <div class="space-y-2 border-t border-[var(--color-ds-border,#e5e7eb)] p-3">
        {#if groups.length === 0}
          <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
        {:else}
          <ul class="space-y-2">
            {#each groups as g (g.id)}
              <li class="text-xs">
                <div class="flex items-center gap-2">
                  <span class="font-medium">{g.name}</span>
                  <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.membersCount', { count: g.member_count ?? 0 })}</span>
                  {#if g.description}<span class="truncate text-[var(--color-ds-text-muted,#6b7280)]">— {g.description}</span>{/if}
                  <button type="button" class="ml-auto text-xs hover:underline" onclick={() => toggleEditGroup(g)}>{t('common.edit')}</button>
                  <button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeGroup(g.id)}>{t('common.delete')}</button>
                </div>
                {#if editingGroupId === g.id}
                  <div class="mt-1 flex flex-wrap items-center gap-2">
                    <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={groupEditForm.name} oninput={(e) => (groupEditForm = { ...groupEditForm, name: e.target.value })} />
                    <input class="w-64 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.groupDescription')} value={groupEditForm.description} oninput={(e) => (groupEditForm = { ...groupEditForm, description: e.target.value })} />
                    <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={!groupEditForm.name.trim()} onclick={saveGroupEdit}>{t('sms.saveGroup')}</button>
                  </div>
                {:else}
                  <details class="mt-1">
                    <summary class="cursor-pointer text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.membersList')}</summary>
                    <div class="mt-1 flex flex-wrap items-center gap-1.5">
                      {#each g.members ?? [] as m (m.id)}
                        <span class="inline-flex items-center gap-1 rounded-full border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5">
                          {m.name}
                          <button type="button" class="text-red-600 hover:underline" title={t('sms.removeMember')} onclick={() => removeMember(g.id, m.id)}>×</button>
                        </span>
                      {:else}
                        <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noMembers')}</span>
                      {/each}
                      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" onchange={(e) => { if (e.target.value) addMember(g.id, e.target.value); e.currentTarget.selectedIndex = 0; }}>
                        <option value="">{t('sms.addMemberPlaceholder')}</option>
                        {#each stakeholders as s (s.id)}
                          {#if !(g.members ?? []).some((m) => m.id === s.id)}<option value={s.id}>{s.name}</option>{/if}
                        {/each}
                      </select>
                    </div>
                  </details>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
        <div class="flex flex-wrap items-center gap-2">
          <input class="w-48 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.groupName')} value={groupForm.name} oninput={(e) => (groupForm = { ...groupForm, name: e.target.value })} />
          <input class="w-64 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('sms.groupDescription')} value={groupForm.description} oninput={(e) => (groupForm = { ...groupForm, description: e.target.value })} />
          <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={savingGroup || !groupForm.name.trim()} onclick={createGroup}>{t('sms.createGroup')}</button>
        </div>
      </div>
    </details>

    <!-- Duplicate candidates + merge -->
    <details class="rounded-md border border-[var(--color-ds-border,#e5e7eb)]">
      <summary class="flex cursor-pointer items-center gap-2 px-3 py-2 text-xs font-medium">
        {t('sms.duplicatesPanel')}
        <button type="button" class="ml-auto rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 font-normal hover:bg-ds-background-neutral-hovered" disabled={checkingDups} onclick={(e) => { e.preventDefault(); checkDuplicates(); }}>
          {t('sms.checkDuplicates')}
        </button>
      </summary>
      <div class="space-y-2 border-t border-[var(--color-ds-border,#e5e7eb)] p-3">
        {#if dupGroups.length === 0}
          <div class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noDuplicates')}</div>
        {:else}
          {#each dupGroups as group (group.key)}
            <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2">
              <ul class="space-y-1">
                {#each group.stakeholders as s (s.id)}
                  <li class="flex items-center gap-2 text-xs">
                    <input type="radio" name={`dup-${group.key}`} value={s.id} checked={(mergeTargets[group.key] ?? group.stakeholders[0].id) === s.id} onchange={() => (mergeTargets = { ...mergeTargets, [group.key]: s.id })} />
                    <span class="font-medium">{s.name}</span>
                    {#if s.title}<span class="text-[var(--color-ds-text-muted,#6b7280)]">— {s.title}</span>{/if}
                    {#if s.organization}<span class="text-[var(--color-ds-text-muted,#6b7280)]">{s.organization}</span>{/if}
                  </li>
                {/each}
              </ul>
              <div class="mt-1.5 flex items-center gap-2">
                <span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.mergeKeep')}</span>
                <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={mergingKey === group.key || !mergeTargets[group.key]} onclick={() => mergeGroup(group)}>
                  {t('sms.mergeNow')}
                </button>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </details>

    <!-- List -->
    {#if filtered.length === 0}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('sms.noRecords')}
      </div>
    {:else}
      <table class="w-full text-left text-xs">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('sms.name')}</th>
            <th class="py-1.5 pr-3">{t('sms.classification')}</th>
            <th class="py-1.5 pr-3 text-center">{t('sms.influenceInterest')}</th>
            <th class="py-1.5 pr-3">{t('sms.engagement')}</th>
            <th class="py-1.5 pr-3">{t('sms.lifecycle')}</th>
            <th class="py-1.5 pr-3">{t('sms.relationship')}</th>
            <th class="py-1.5 pr-3 text-right">{t('sms.healthScore')}</th>
            <th class="py-1.5"></th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as s (s.id)}
            <tr
              class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedId === s.id ? 'bg-ds-surface-selected' : ''}"
              onclick={() => (selectedId = selectedId === s.id ? null : s.id)}
            >
              <td class="py-1.5 pr-3">
                <div class="font-medium">{s.name}</div>
                {#if s.organization}<div class="text-[var(--color-ds-text-muted,#6b7280)]">{s.organization}</div>{/if}
              </td>
              <td class="py-1.5 pr-3">{classificationLabel(s.role_classification)}</td>
              <td class="py-1.5 pr-3 text-center">{s.influence_level ?? '–'} / {s.interest_level ?? '–'}</td>
              <td class="py-1.5 pr-3">
                <span class="rounded-full px-2 py-0.5 {engagementClass(s.engagement_status)}">{engagementLabel(s.engagement_status)}</span>
              </td>
              <td class="py-1.5 pr-3">{lifecycleLabel(s.lifecycle_stage)}</td>
              <td class="py-1.5 pr-3">
                <span class="rounded-full px-2 py-0.5 {relationshipClass(s.relationship_status)}">{relationshipLabel(s.relationship_status)}</span>
              </td>
              <td class="py-1.5 pr-3 text-right">
                <span class="rounded-full px-2 py-0.5 font-medium {healthClass(s.health_score ?? 0)}">{s.health_score ?? '–'}</span>
              </td>
              <td class="py-1.5 text-right">
                <button type="button" class="text-xs text-red-600 hover:underline" title={t('common.delete')} onclick={(e) => { e.stopPropagation(); removeStakeholder(s.id, s.name); }}>
                  {t('common.delete')}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <!-- Detail drill-in -->
    {#if selectedId !== null}
      <SMSStakeholderDetail stakeholderId={selectedId} onBack={() => (selectedId = null)} onChanged={() => load(true)} />
    {/if}
  {/if}
</div>
