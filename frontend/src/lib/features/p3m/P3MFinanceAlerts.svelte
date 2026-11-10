<script>
	import { t } from '../../stores/i18n.svelte.js';
	import { p3m as p3mApi } from '../../api/p3m.js';

	let { project } = $props();

	const METRICS = ['forecast_overrun_pct', 'variance_vs_budget', 'commitment_vs_remaining'];
	const COMPARATORS = ['>', '<', '>=', '<='];

	const metricLabel = (m) =>
		m === 'forecast_overrun_pct' ? t('p3m.metricForecastOverrunPct') : m === 'variance_vs_budget' ? t('p3m.metricVarianceVsBudget') : m === 'commitment_vs_remaining' ? t('p3m.metricCommitmentVsRemaining') : String(m).replace(/_/g, ' ');

	let rules = $state([]);
	let loading = $state(true);
	let error = $state('');
	let showForm = $state(false);
	let saving = $state(false);
	let editingRuleId = $state(null);
	let evaluating = $state(false);
	let evalResult = $state(null);

	const emptyForm = () => ({ name: '', metric: METRICS[0], comparator: '>', threshold: '', notify_email: '', enabled: true });
	let form = $state(emptyForm());

	const load = async (id) => {
		loading = true;
		error = '';
		evalResult = null;
		try {
			const rows = await p3mApi.financeAlertRules.getAll({ project_id: id });
			rules = Array.isArray(rows) ? rows : [];
		} catch (e) {
			error = e?.message || 'error';
			rules = [];
		} finally {
			loading = false;
		}
	};

	const openNew = () => { error = ''; editingRuleId = null; form = emptyForm(); showForm = true; };
	const openEdit = (r) => {
		error = '';
		editingRuleId = r.id;
		form = { name: r.name, metric: r.metric, comparator: r.comparator, threshold: String(r.threshold), notify_email: r.notify_email ?? '', enabled: r.enabled };
		showForm = true;
	};

	const save = async () => {
		if (!form.name.trim()) return;
		saving = true;
		error = '';
		try {
			const data = {
				name: form.name.trim(),
				scope: 'project',
				project_id: project.id,
				metric: form.metric,
				comparator: form.comparator,
				threshold: Number(form.threshold),
				notify_email: form.notify_email.trim() || null,
				enabled: form.enabled,
			};
			if (editingRuleId) {
				await p3mApi.financeAlertRules.update(editingRuleId, data);
			} else {
				await p3mApi.financeAlertRules.create(data);
			}
			showForm = false;
			await load(project.id);
		} catch (e) {
			error = e?.message || 'error';
		} finally {
			saving = false;
		}
	};

	const removeRule = async (r) => {
		if (!confirm(`${t('p3m.confirmDeleteRule')} "${r.name}"`)) return;
		try {
			await p3mApi.financeAlertRules.delete(r.id);
			await load(project.id);
		} catch (e) {
			error = e?.message || 'error';
		}
	};

	const toggleEnabled = async (r, enabled) => {
		rules = rules.map((x) => (x.id === r.id ? { ...x, enabled } : x));
		try {
			await p3mApi.financeAlertRules.update(r.id, { ...r, enabled });
		} catch (e) {
			error = e?.message || 'error';
			await load(project.id);
		}
	};

	const evaluateNow = async () => {
		evaluating = true;
		error = '';
		try {
			evalResult = await p3mApi.evaluateProjectFinanceAlerts(project.id);
		} catch (e) {
			error = e?.message || 'error';
		} finally {
			evaluating = false;
		}
	};

	const fmtTime = (iso) => { if (!iso) return ''; const d = new Date(iso); return isNaN(d.getTime()) ? iso : d.toLocaleString(); };

	$effect(() => {
		const id = project?.id;
		if (id) void load(id);
	});
</script>

<section class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
	<div class="mb-2 flex items-center justify-between gap-2">
		<h3 class="text-sm font-semibold">{t('p3m.financeAlertsTitle')}</h3>
		<button type="button" disabled={evaluating} onclick={evaluateNow} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-0.5 text-xs hover:bg-slate-50 disabled:opacity-50">
			{evaluating ? '…' : t('p3m.evaluateNow')}
		</button>
	</div>

	{#if evalResult}
		<div class="mb-2 rounded border border-[var(--color-ds-border,#e5e7eb)] bg-slate-50 p-2 text-xs">
			<p>{t('p3m.evaluatedRules', { n: evalResult.rules_evaluated ?? 0 })} · {fmtTime(evalResult.evaluated_at)}</p>
			{#if (evalResult.alerts ?? []).length}
				<ul class="mt-1 space-y-0.5">
					{#each evalResult.alerts as a (a.rule_id)}
						<li style="color:#dc2626">{a.name}: {metricLabel(a.metric)} = {a.value} ({a.comparator} {a.threshold})</li>
					{/each}
				</ul>
			{:else}
				<p class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.noAlertsTriggered')}</p>
			{/if}
		</div>
	{/if}

	{#if loading}
		<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
	{:else if showForm}
		<form onsubmit={(e) => { e.preventDefault(); save(); }} class="space-y-3">
			<div>
				<label class="mb-1 block text-xs font-medium" for="fa-name">{t('p3m.name')}</label>
				<input id="fa-name" type="text" bind:value={form.name} required class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<div class="grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-3">
				<div>
					<label class="mb-1 block text-xs font-medium" for="fa-metric">{t('p3m.metric')}</label>
					<select id="fa-metric" bind:value={form.metric} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
						{#each METRICS as m}<option value={m}>{metricLabel(m)}</option>{/each}
					</select>
				</div>
				<div>
					<label class="mb-1 block text-xs font-medium" for="fa-comparator">{t('p3m.comparator')}</label>
					<select id="fa-comparator" bind:value={form.comparator} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
						{#each COMPARATORS as c}<option value={c}>{c}</option>{/each}
					</select>
				</div>
				<div>
					<label class="mb-1 block text-xs font-medium" for="fa-threshold">{t('p3m.threshold')}</label>
					<input id="fa-threshold" type="number" step="any" bind:value={form.threshold} required class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
				</div>
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="fa-email">{t('p3m.notifyEmail')}</label>
				<input id="fa-email" type="text" bind:value={form.notify_email} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" bind:checked={form.enabled} /> {t('p3m.enabled')}
			</label>

			{#if error}<p class="text-xs" style="color:#ef4444">{error}</p>{/if}

			<div class="flex gap-2">
				<button type="submit" disabled={saving || !form.name.trim()} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50">{saving ? 'Saving…' : 'Save'}</button>
				<button type="button" onclick={() => (showForm = false)} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Cancel</button>
			</div>
		</form>
	{:else if rules.length}
		<ul class="space-y-1">
			{#each rules as r (r.id)}
				<li class="flex items-center gap-2 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
					<input type="checkbox" checked={r.enabled} onchange={(e) => toggleEnabled(r, e.target.checked)} title={t('p3m.enabled')} />
					<span class={r.enabled ? '' : 'text-[var(--color-ds-text-muted,#6b7280)]'}>{r.name}</span>
					<span class="ml-auto text-xs text-[var(--color-ds-text-muted,#6b7280)]">{metricLabel(r.metric)} {r.comparator} {r.threshold}</span>
					<button type="button" class="text-xs text-[var(--color-ds-accent-blue,#2563eb)] hover:underline" onclick={() => openEdit(r)}>Edit</button>
					<button type="button" class="text-xs text-red-600 hover:underline" onclick={() => removeRule(r)}>{t('p3m.delete')}</button>
				</li>
			{/each}
		</ul>
	{:else if error}
		<p class="text-xs" style="color:#ef4444">{error}</p>
	{/if}

	{#if !showForm && !loading}
		<button type="button" onclick={openNew} class="mt-2 rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">{t('p3m.newRule')}</button>
	{/if}
</section>
