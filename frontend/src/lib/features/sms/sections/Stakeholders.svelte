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
  import SMSStakeholderDetail from './SMSStakeholderDetail.svelte';

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

  // Quick-create form.
  let newName = $state('');
  let newClassification = $state('support');
  let creating = $state(false);

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
      return true;
    })
  );

  async function load() {
    loading = true;
    error = '';
    try {
      stakeholders = (await smsApi.getStakeholders()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function add() {
    if (!newName.trim() || creating) return;
    creating = true;
    try {
      await smsApi.createStakeholder({ name: newName.trim(), role_classification: newClassification });
      newName = '';
      await load();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      creating = false;
    }
  }

  onMount(load);
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

      <span class="mx-1 h-5 w-px bg-[var(--color-ds-border,#e5e7eb)]" aria-hidden="true"></span>

      <input
        class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
        placeholder={t('sms.newStakeholderName')}
        value={newName}
        oninput={(e) => (newName = e.target.value)}
      />
      <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newClassification} onchange={(e) => (newClassification = e.target.value)}>
        {#each CLASSIFICATIONS as c (c)}<option value={c}>{classificationLabel(c)}</option>{/each}
      </select>
      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" disabled={creating || !newName.trim()} onclick={add}>
        {t('sms.add')}
      </button>
    </div>

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
            <th class="py-1.5 text-right">{t('sms.healthScore')}</th>
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
              <td class="py-1.5 text-right">
                <span class="rounded-full px-2 py-0.5 font-medium {healthClass(s.health_score ?? 0)}">{s.health_score ?? '–'}</span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <!-- Detail drill-in -->
    {#if selectedId !== null}
      <SMSStakeholderDetail stakeholderId={selectedId} onBack={() => (selectedId = null)} onChanged={load} />
    {/if}
  {/if}
</div>
