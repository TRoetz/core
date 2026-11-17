<script>
	import { t } from '../../stores/i18n.svelte.js';
	import { p3m as p3mApi } from '../../api/p3m.js';
	import { onMount } from 'svelte';

	let summary = $state(null);
	let error = $state('');

	onMount(async () => {
		try {
			summary = await p3mApi.getFinanceSyncSummary();
		} catch (e) {
			error = e?.message || String(e);
		}
	});

	const fmtTime = (iso) => {
		if (!iso) return '';
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	};

	const statusOf = (c) => {
		if (!c.enabled) return 'disabled';
		if (c.last_run_status === 'failed') return 'failed';
		if (!c.stale && c.last_run_at) return 'ok';
		return 'stale';
	};

	const chipFor = (s) => {
		switch (s) {
			case 'ok':
				return { label: t('p3m.syncOk'), color: '#10b981' };
			case 'failed':
				return { label: t('p3m.syncFailed'), color: '#ef4444' };
			default:
				return { label: t('p3m.syncStale'), color: '#f59e0b' };
		}
	};
</script>

<div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
	<h3 class="text-sm font-semibold">{t('p3m.pulseTitle')}</h3>

	{#if error}
		<p class="mt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{error}</p>
	{:else if summary}
		<ul class="mt-2 space-y-1.5">
			{#each (summary.configs ?? []) as c (c.id)}
				<li class="flex flex-wrap items-center gap-x-2 text-xs">
					<span>{c.name}</span>
					{#if statusOf(c) === 'disabled'}
						<span class="text-[var(--color-ds-text-muted,#6b7280)]">disabled</span>
					{:else}
						{@const chip = chipFor(statusOf(c))}
						<span
							class="rounded px-1.5 py-0.5 font-medium"
							style="background-color:{chip.color}1f;color:{chip.color}"
						>{chip.label}</span>
					{/if}
					{#if c.last_run_at}
						<span class="text-[var(--color-ds-text-muted,#6b7280)]">
							{t('p3m.lastRun')} {fmtTime(c.last_run_at)} · {c.age_minutes >= 0 ? `${c.age_minutes} min` : 'never'}
						</span>
					{/if}
				</li>
			{/each}
		</ul>

		<p class="mt-3 text-xs" style="color:{(summary.unmatched_count ?? 0) > 0 ? '#ef4444' : 'var(--color-ds-text-muted,#6b7280)'}">
			{summary.unmatched_count ?? 0} {t('p3m.unmatchedRows')}
		</p>
	{:else}
		<p class="mt-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
	{/if}
</div>
