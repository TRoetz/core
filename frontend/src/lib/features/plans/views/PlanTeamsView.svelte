<script>
  import { onMount } from 'svelte';
  import { plans as plansApi } from '../../../api/plans.js';
  import { t } from '../../../stores/i18n.svelte.js';

  let { planId } = $props();

  const UNITS = [
    { id: 'points', labelKey: 'plans.teamsUnitPoints' },
    { id: 'days', labelKey: 'plans.teamsUnitDays' },
    { id: 'hours', labelKey: 'plans.teamsUnitHours' },
  ];

  let teams = $state([]);
  let loading = $state(true);
  let error = $state('');
  // Per-team editor state, keyed by team_id.
  let editors = $state({});
  let savingTeamId = $state(null);

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await plansApi.getTeams(planId);
      teams = res?.teams ?? [];
      // Seed editors from server state.
      const next = {};
      for (const team of teams) {
        next[team.team_id] = {
          capacity_unit: team.capacity_unit || 'points',
          weekly_capacity: team.weekly_capacity == null ? '' : String(team.weekly_capacity),
        };
      }
      editors = next;
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function saveCapacity(teamId, clear = false) {
    const ed = editors[teamId];
    if (!ed) return;
    savingTeamId = teamId;
    try {
      await plansApi.setTeamCapacity(planId, teamId, {
        capacity_unit: ed.capacity_unit,
        weekly_capacity: clear ? null : ed.weekly_capacity === '' ? null : Number(ed.weekly_capacity),
      });
      await load();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingTeamId = null;
    }
  }

  const estHours = (minutes) => (minutes > 0 ? Math.round(minutes / 60) : 0);
</script>

{#if loading && teams.length === 0}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.loading')}</p>
{:else if error && teams.length === 0}
  <div class="space-y-3">
    <p class="text-sm text-red-600">{error}</p>
    <button type="button" class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90" onclick={load}>
      {t('plans.retry')}
    </button>
  </div>
{:else if teams.length === 0}
  <p class="text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsEmpty')}</p>
{:else}
  <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
    {#each teams as team (team.team_id)}
      {@const ed = editors[team.team_id] ?? { capacity_unit: 'points', weekly_capacity: '' }}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-[var(--color-ds-surface,#fff)] p-4">
        <!-- Header -->
        <div class="mb-3 flex items-center justify-between gap-2">
          <h3 class="flex items-center gap-2 truncate text-sm font-semibold">
            {#if team.color}
              <span class="inline-block h-3 w-3 shrink-0 rounded-full" style={`background:${team.color}`}></span>
            {/if}
            {team.name}
          </h3>
          {#if team.weekly_capacity == null}
            <span class="shrink-0 rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] px-2 py-0.5 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsNoCapacity')}</span>
          {:else if team.overloaded}
            <span class="shrink-0 rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">{t('plans.teamsOverloaded')}</span>
          {:else}
            <span class="shrink-0 rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700">{t('plans.teamsWithinCapacity')}</span>
          {/if}
        </div>

        <!-- Workload -->
        <dl class="mb-3 space-y-1 text-sm">
          <div class="flex justify-between" title={t('plans.summaryItems', { count: team.item_count })}>
            <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsOpenItems', { count: team.open_item_count })}</dt>
            <dd class="tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">/ {team.item_count}</dd>
          </div>
          {#if estHours(team.total_estimate_minutes) > 0}
            <div class="flex justify-between">
              <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsEstimate', { minutes: estHours(team.total_estimate_minutes) })}</dt>
            </div>
          {/if}
          {#if team.total_story_points > 0}
            <div class="flex justify-between">
              <dt class="text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsStoryPoints', { points: team.total_story_points })}</dt>
            </div>
          {/if}
        </dl>

        <!-- Capacity editor -->
        <div class="mb-3 rounded-md bg-[var(--color-ds-surface-hover,#f3f4f6)] p-2.5">
          <p class="mb-1.5 text-xs font-medium uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('plans.teamsCapacity')}</p>
          <div class="flex items-center gap-2">
            <select
              value={ed.capacity_unit}
              onchange={(e) => (editors[team.team_id] = { ...ed, capacity_unit: e.currentTarget.value })}
              class="h-8 flex-1 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm outline-none"
            >
              {#each UNITS as u (u.id)}
                <option value={u.id}>{t(u.labelKey)}</option>
              {/each}
            </select>
            <input
              type="number"
              min="0"
              step="any"
              bind:value={ed.weekly_capacity}
              placeholder="—"
              class="h-8 w-20 rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2 text-sm tabular-nums outline-none focus:border-[var(--color-ds-accent-blue,#3b82f6)]"
            />
          </div>
          <div class="mt-2 flex gap-2">
            <button type="button" disabled={savingTeamId === team.team_id} onclick={() => saveCapacity(team.team_id)} class="rounded-md bg-[var(--color-ds-accent-blue,#3b82f6)] px-2.5 py-1 text-xs font-medium text-white hover:opacity-90 disabled:opacity-50">
              {t('plans.teamsSetCapacity')}
            </button>
            <button type="button" disabled={savingTeamId === team.team_id} onclick={() => saveCapacity(team.team_id, true)} class="rounded-md border border-[var(--color-ds-border,#d1d5db)] bg-[var(--color-ds-surface,#fff)] px-2.5 py-1 text-xs hover:bg-[var(--color-ds-surface-hover,#f3f4f6)] disabled:opacity-50">
              {t('plans.teamsClearCapacity')}
            </button>
          </div>
        </div>

        <!-- Members -->
        <p class="mb-1.5 text-xs font-medium uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
          {team.member_count} {t('plans.teamsMembers')}
        </p>
        <ul class="space-y-1.5">
          {#each team.members ?? [] as m (m.user_id)}
            <li class="flex items-center gap-2 text-sm">
              {#if m.avatar_url}
                <img src={m.avatar_url} alt="" class="h-5 w-5 rounded-full" />
              {:else}
                <span class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--color-ds-surface-hover,#f3f4f6)] text-[10px] font-medium uppercase text-[var(--color-ds-text-muted,#6b7280)]">
                  {m.name?.slice(0, 1) || '?'}
                </span>
              {/if}
              <span class="flex-1 truncate">{m.name}</span>
              <span class="tabular-nums text-xs text-[var(--color-ds-text-muted,#6b7280)]">{m.item_count}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/each}
  </div>
{/if}
