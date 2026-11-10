<script>
	import { t } from '../../stores/i18n.svelte.js';
	import { p3m as p3mApi } from '../../api/p3m.js';

	let { project } = $props();

	const jparse = (s) => { try { return s ? JSON.parse(s) : null; } catch { return null; } };
	const parseItems = (s) => { const v = jparse(s); return Array.isArray(v) ? v : []; };
	const toLocalInput = (iso) => {
		if (!iso) return '';
		const d = new Date(iso);
		if (isNaN(d.getTime())) return '';
		const pad = (n) => String(n).padStart(2, '0');
		return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
	};
	const fmtDate = (iso) => {
		if (!iso) return '';
		const d = new Date(iso);
		return isNaN(d.getTime()) ? iso : d.toLocaleString();
	};

	let record = $state(null);
	let loading = $state(true);
	let error = $state('');
	let showForm = $state(false);
	let saving = $state(false);

	const emptyForm = () => ({
		acceptance_signoff_by: '',
		acceptance_signoff_at: '',
		outstanding_items: '',
		lessons_learned: '',
		cost_ownership: '',
		handover_to: '',
		status: 'recommended',
	});

	let form = $state(emptyForm());

	const outstandingItems = $derived(parseItems(record?.outstanding_items));
	const statusColor = $derived(record?.status === 'approved' ? '#10b981' : '#f59e0b');

	const load = async (id) => {
		loading = true;
		error = '';
		try {
			const rec = await p3mApi.getClosureRecord(id);
			record = rec && rec.id ? rec : null;
		} catch {
			error = 'Failed to load closure record';
		} finally {
			loading = false;
		}
	};

	$effect(() => {
		const id = project?.id;
		if (id) void load(id);
	});

	const openNew = () => { form = emptyForm(); showForm = true; };
	const openEdit = () => {
		form = {
			acceptance_signoff_by: record.acceptance_signoff_by != null ? String(record.acceptance_signoff_by) : '',
			acceptance_signoff_at: toLocalInput(record.acceptance_signoff_at),
			outstanding_items: parseItems(record.outstanding_items).join('\n'),
			lessons_learned: record.lessons_learned ?? '',
			cost_ownership: record.cost_ownership ?? '',
			handover_to: record.handover_to ?? '',
			status: record.status === 'approved' ? 'approved' : 'recommended',
		};
		showForm = true;
	};
	const cancel = () => { showForm = false; };

	const save = async () => {
		saving = true;
		error = '';
		try {
			const items = form.outstanding_items.split('\n').map((s) => s.trim()).filter(Boolean);
			const data = {
				acceptance_signoff_by: form.acceptance_signoff_by === '' ? null : Number(form.acceptance_signoff_by),
				acceptance_signoff_at: form.acceptance_signoff_at ? new Date(form.acceptance_signoff_at).toISOString() : null,
				outstanding_items: items.length ? JSON.stringify(items) : null,
				lessons_learned: form.lessons_learned || null,
				cost_ownership: form.cost_ownership || null,
				handover_to: form.handover_to || null,
				status: form.status,
			};
			if (record?.id) {
				await p3mApi.updateClosureRecord(record.id, data);
			} else {
				await p3mApi.createClosureRecord(project.id, data);
			}
			showForm = false;
			await load(project.id);
		} catch {
			error = 'Failed to save closure record';
		} finally {
			saving = false;
		}
	};
</script>

<section class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
	<h3 class="mb-2 text-sm font-semibold">{t('p3m.closureTitle')}</h3>

	{#if loading}
		<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
	{:else if showForm}
		<form onsubmit={(e) => { e.preventDefault(); save(); }} class="space-y-3">
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-signoff-by">{t('p3m.acceptanceSignoffBy')}</label>
				<input id="closure-signoff-by" type="number" bind:value={form.acceptance_signoff_by} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-signoff-at">{t('p3m.acceptanceSignoffAt')}</label>
				<input id="closure-signoff-at" type="datetime-local" bind:value={form.acceptance_signoff_at} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-items">{t('p3m.outstandingItems')}</label>
				<textarea id="closure-items" rows="3" bind:value={form.outstanding_items} placeholder="One item per line" class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-lessons">{t('p3m.lessonsLearned')}</label>
				<textarea id="closure-lessons" rows="3" bind:value={form.lessons_learned} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-cost">{t('p3m.costOwnership')}</label>
				<input id="closure-cost" type="text" bind:value={form.cost_ownership} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-handover">{t('p3m.handoverTo')}</label>
				<input id="closure-handover" type="text" bind:value={form.handover_to} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>
			<div>
				<label class="mb-1 block text-xs font-medium" for="closure-status">Status</label>
				<select id="closure-status" bind:value={form.status} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
					<option value="recommended">Recommended</option>
					<option value="approved">Approved</option>
				</select>
			</div>

			{#if error}
				<p class="text-xs" style="color:#ef4444">{error}</p>
			{/if}

			<div class="flex gap-2">
				<button type="submit" disabled={saving} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50">{saving ? 'Saving…' : 'Save'}</button>
				<button type="button" onclick={cancel} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Cancel</button>
			</div>
		</form>
	{:else if record}
		<div class="space-y-3">
			<span class="inline-block rounded-full px-2 py-0.5 text-xs font-semibold" style="background-color:{statusColor}; color:#fff">{record.status}</span>

			<dl class="grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-2">
				<div>
					<dt class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.acceptanceSignoffBy')}</dt>
					<dd class="text-sm">{record.acceptance_signoff_by != null ? record.acceptance_signoff_by : '—'}</dd>
				</div>
				<div>
					<dt class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.acceptanceSignoffAt')}</dt>
					<dd class="text-sm">{record.acceptance_signoff_at ? fmtDate(record.acceptance_signoff_at) : '—'}</dd>
				</div>
				<div>
					<dt class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.costOwnership')}</dt>
					<dd class="text-sm">{record.cost_ownership || '—'}</dd>
				</div>
				<div>
					<dt class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.handoverTo')}</dt>
					<dd class="text-sm">{record.handover_to || '—'}</dd>
				</div>
			</dl>

			<div>
				<p class="mb-1 text-xs font-medium">{t('p3m.outstandingItems')}</p>
				{#if outstandingItems.length}
					<ul class="list-disc space-y-0.5 pl-4 text-sm">
						{#each outstandingItems as item}
							<li>{item}</li>
						{/each}
					</ul>
				{:else}
					<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">None</p>
				{/if}
			</div>

			<div>
				<p class="mb-1 text-xs font-medium">{t('p3m.lessonsLearned')}</p>
				<p class="whitespace-pre-wrap text-sm">{record.lessons_learned || '—'}</p>
			</div>

			<button onclick={openEdit} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Edit</button>
		</div>
	{:else if error}
		<p class="text-xs" style="color:#ef4444">{error}</p>
	{:else}
		<button onclick={openNew} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">{t('p3m.newClosureRecord')}</button>
	{/if}
</section>
