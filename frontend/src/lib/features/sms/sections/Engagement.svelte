<script>
  import { onMount } from 'svelte';
  import { t } from '../../../stores/i18n.svelte.js';
  import { sms as smsApi } from '../../../api/sms.js';
  import { formatDate, engagementClass } from '../utils.js';

  const PLAN_STATUSES = ['draft', 'active', 'paused', 'completed'];

  let loading = $state(true);
  let error = $state('');
  /** @type {Array<Record<string, any>>} */
  let plans = $state([]);
  /** @type {Array<Record<string, any>>} */
  let communications = $state([]);
  let planStatusFilter = $state('active');

  async function load() {
    loading = true;
    error = '';
    try {
      const [planRows, commRows] = await Promise.all([
        smsApi.getGlobalPlans(planStatusFilter ? { status: planStatusFilter } : {}),
        smsApi.getGlobalCommunications({ limit: 200 }),
      ]);
      plans = planRows ?? [];
      communications = commRows ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  function channelLabel(ch) {
    const key = ch === 'in_person' ? 'InPerson' : ch.charAt(0).toUpperCase() + ch.slice(1);
    return t(`sms.ch${key}`);
  }

  onMount(load);
</script>

<div class="space-y-6 px-6 py-4">
  {#if loading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.loading')}</div>
  {:else if error && plans.length === 0 && communications.length === 0}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
  {:else}
    <!-- Engagement plans -->
    <section>
      <div class="mb-2 flex items-center gap-2">
        <h2 class="text-sm font-semibold">{t('sms.engagementPlans')}</h2>
        <select
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          value={planStatusFilter}
          onchange={(e) => { planStatusFilter = e.target.value; load(); }}
        >
          <option value="">{t('sms.plansAll')}</option>
          {#each PLAN_STATUSES as s (s)}<option value={s}>{t(`sms.st${s.charAt(0).toUpperCase() + s.slice(1)}`)}</option>{/each}
        </select>
      </div>
      {#if plans.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.stakeholder')}</th>
              <th class="py-1.5 pr-3">{t('sms.strategy')}</th>
              <th class="py-1.5 pr-3">{t('sms.frequency')}</th>
              <th class="py-1.5 pr-3">{t('sms.channel')}</th>
              <th class="py-1.5 pr-3">{t('sms.status')}</th>
              <th class="py-1.5 pr-3">{t('sms.startDate')}</th>
              <th class="py-1.5">{t('sms.endDate')}</th>
            </tr>
          </thead>
          <tbody>
            {#each plans as p (p.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="py-1.5 pr-3 font-medium">{p.stakeholder_name}</td>
                <td class="py-1.5 pr-3">{p.strategy}</td>
                <td class="py-1.5 pr-3">{t(`sms.freq${p.frequency === 'as_needed' ? 'AsNeeded' : p.frequency.charAt(0).toUpperCase() + p.frequency.slice(1)}`)}</td>
                <td class="py-1.5 pr-3">{channelLabel(p.channel)}</td>
                <td class="py-1.5 pr-3"><span class="rounded-full px-2 py-0.5 {engagementClass(p.status === 'active' ? 'active' : p.status === 'paused' ? 'passive' : 'neutral')}">{t(`sms.st${p.status.charAt(0).toUpperCase() + p.status.slice(1)}`)}</span></td>
                <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(p.start_date)}</td>
                <td class="py-1.5 whitespace-nowrap">{formatDate(p.end_date)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Communication log -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('sms.commLog')}</h2>
      {#if communications.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('sms.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('sms.date')}</th>
              <th class="py-1.5 pr-3">{t('sms.stakeholder')}</th>
              <th class="py-1.5 pr-3">{t('sms.channel')}</th>
              <th class="py-1.5 pr-3">{t('sms.direction')}</th>
              <th class="py-1.5 pr-3">{t('sms.subject')}</th>
              <th class="py-1.5">{t('sms.sentiment')}</th>
            </tr>
          </thead>
          <tbody>
            {#each communications as c (c.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] last:border-0">
                <td class="py-1.5 pr-3 whitespace-nowrap">{formatDate(c.created_at)}</td>
                <td class="py-1.5 pr-3 font-medium">{c.stakeholder_name}</td>
                <td class="py-1.5 pr-3">{channelLabel(c.channel)}</td>
                <td class="py-1.5 pr-3">{c.direction === 'outbound' ? t('sms.dirOutbound') : t('sms.dirInbound')}</td>
                <td class="py-1.5 pr-3"><div>{c.subject}</div>{#if c.body}<div class="max-w-md truncate text-[var(--color-ds-text-muted,#6b7280)]">{c.body}</div>{/if}</td>
                <td class="py-1.5">{#if c.sentiment === 'positive'}<span class="rounded-full bg-emerald-100 px-2 py-0.5">+</span>{:else if c.sentiment === 'negative'}<span class="rounded-full bg-red-100 px-2 py-0.5">−</span>{:else}<span class="rounded-full bg-slate-100 px-2 py-0.5">±</span>{/if}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
</div>
