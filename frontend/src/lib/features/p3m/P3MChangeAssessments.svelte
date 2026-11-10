<script>
	import { t } from '../../stores/i18n.svelte.js';
	import { p3m as p3mApi } from '../../api/p3m.js';

	let { project } = $props();

	const jparse = (s) => { try { return s ? JSON.parse(s) : null; } catch { return null; } };
	const parseItems = (s) => { const v = jparse(s); return Array.isArray(v) ? v : []; };
	const fmtDate = (iso) => { if (!iso) return ''; const d = new Date(iso); return isNaN(d.getTime()) ? iso : d.toLocaleString(); };

	let error = $state('');

	// --- Change impact assessment (CHG-002/003): at most one active per project.
	let assessment = $state(null);
	let loadingAssessment = $state(true);
	let showAssessmentForm = $state(false);
	let savingAssessment = $state(false);

	const emptyImpactRow = () => ({ characteristic: '', nature: '', severity: '' });
	const emptyAssessmentForm = () => ({
		impacted_groups: '',
		rows: [emptyImpactRow()],
		overall_rating: '3',
		support_level: 'none',
		approach: '',
		deliverables: '',
	});
	let assessmentForm = $state(emptyAssessmentForm());

	const loadAssessment = async (id) => {
		loadingAssessment = true;
		try {
			const rows = await p3mApi.getChangeAssessments(id);
			assessment = Array.isArray(rows) && rows.length ? rows[0] : null;
		} catch {
			assessment = null;
		} finally {
			loadingAssessment = false;
		}
	};

	const openNewAssessment = () => { error = ''; assessmentForm = emptyAssessmentForm(); showAssessmentForm = true; };
	const openEditAssessment = () => {
		error = '';
		assessmentForm = {
			impacted_groups: parseItems(assessment.impacted_groups).join(', '),
			rows: parseItems(assessment.impact_by_characteristic).map((r) => ({ characteristic: r.characteristic ?? '', nature: r.nature ?? '', severity: r.severity ?? '' })),
			overall_rating: String(assessment.overall_rating),
			support_level: assessment.support_level || 'none',
			approach: assessment.approach ?? '',
			deliverables: parseItems(assessment.deliverables).join('\n'),
		};
		showAssessmentForm = true;
	};

	const saveAssessment = async () => {
		savingAssessment = true;
		error = '';
		try {
			const groups = assessmentForm.impacted_groups.split(',').map((s) => s.trim()).filter(Boolean);
			const rows = assessmentForm.rows.map((r) => ({ characteristic: r.characteristic.trim(), nature: r.nature.trim(), severity: r.severity })).filter((r) => r.characteristic || r.nature);
			const deliverables = assessmentForm.deliverables.split('\n').map((s) => s.trim()).filter(Boolean);
			const data = {
				impacted_groups: groups.length ? JSON.stringify(groups) : null,
				impact_by_characteristic: rows.length ? JSON.stringify(rows) : null,
				overall_rating: Number(assessmentForm.overall_rating),
				support_level: assessmentForm.support_level,
				approach: assessmentForm.approach.trim() || null,
				deliverables: deliverables.length ? JSON.stringify(deliverables) : null,
			};
			if (assessment?.id) {
				await p3mApi.updateChangeAssessment(assessment.id, data);
			} else {
				await p3mApi.createChangeAssessment(project.id, data);
			}
			showAssessmentForm = false;
			await loadAssessment(project.id);
		} catch (e) {
			error = e?.message || 'error';
		} finally {
			savingAssessment = false;
		}
	};

	const addImpactRow = () => { assessmentForm.rows.push(emptyImpactRow()); };
	const removeImpactRow = (i) => { assessmentForm.rows.splice(i, 1); if (!assessmentForm.rows.length) assessmentForm.rows.push(emptyImpactRow()); };

	// --- Readiness assessments (CHG-006): the latest gates entry to implement_deliver.
	let readinessList = $state([]);
	let loadingReadiness = $state(true);
	let showReadinessForm = $state(false);
	let savingReadiness = $state(false);

	const defaultQuestions = () => [
		{ id: 'q1', text: 'Change impact assessment completed', required: true },
		{ id: 'q2', text: 'Impacted groups notified of the change', required: true },
		{ id: 'q3', text: 'Budget and schedule impact approved by sponsor', required: true },
		{ id: 'q4', text: 'Support plan agreed (level, approach, deliverables)', required: false },
	];

	const emptyReadinessForm = () => ({ questions: defaultQuestions(), answers: {}, outcome: 'reassess' });
	let readinessForm = $state(emptyReadinessForm());

	const loadReadiness = async (id) => {
		loadingReadiness = true;
		try {
			const rows = await p3mApi.getReadinessAssessments(id);
			readinessList = Array.isArray(rows) ? rows : [];
		} catch {
			readinessList = [];
		} finally {
			loadingReadiness = false;
		}
	};

	const openNewReadiness = () => { error = ''; readinessForm = emptyReadinessForm(); showReadinessForm = true; };

	const setAnswer = (qid, patch) => {
		readinessForm.answers = { ...readinessForm.answers, [qid]: { answer: '', gap_action: '', ...(readinessForm.answers[qid] ?? {}), ...patch } };
	};

	const saveReadiness = async () => {
		for (const q of readinessForm.questions) {
			if (!q.text.trim()) continue;
			if (q.required && !readinessForm.answers[q.id]?.answer) {
				error = t('p3m.requiredQuestionsUnanswered');
				return;
			}
		}
		savingReadiness = true;
		error = '';
		try {
			const questions = readinessForm.questions.filter((q) => q.text.trim()).map(({ id, text, required }) => ({ id, text: text.trim(), required }));
			const answers = [];
			for (const q of questions) {
				const a = readinessForm.answers[q.id];
				if (!a?.answer) continue;
				answers.push({ question_id: q.id, answer: a.answer, gap_action: a.answer === 'no' ? (a.gap_action || '').trim() : null });
			}
			await p3mApi.createReadinessAssessment(project.id, {
				questions: JSON.stringify(questions),
				answers: answers.length ? JSON.stringify(answers) : null,
				outcome: readinessForm.outcome,
			});
			showReadinessForm = false;
			await loadReadiness(project.id);
		} catch (e) {
			error = e?.message || 'error';
		} finally {
			savingReadiness = false;
		}
	};

	const addQuestion = () => { readinessForm.questions.push({ id: `q${Date.now()}`, text: '', required: false }); };
	const removeQuestion = (i) => { readinessForm.questions.splice(i, 1); };

	const outcomeColor = (o) => (o === 'go' ? '#10b981' : o === 'no_go' ? '#ef4444' : '#f59e0b');
	const outcomeLabel = (o) => (o === 'go' ? t('p3m.outcomeGo') : o === 'no_go' ? t('p3m.outcomeNoGo') : t('p3m.outcomeReassess'));

	$effect(() => {
		const id = project?.id;
		if (id) { void loadAssessment(id); void loadReadiness(id); }
	});
</script>

<!-- Change impact assessment -->
<section class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)] p-4">
	<h3 class="mb-2 text-sm font-semibold">{t('p3m.assessmentsTitle')}</h3>

	{#if loadingAssessment}
		<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
	{:else if showAssessmentForm}
		<form onsubmit={(e) => { e.preventDefault(); saveAssessment(); }} class="space-y-3">
			<div>
				<label class="mb-1 block text-xs font-medium" for="ca-groups">{t('p3m.impactedGroups')}</label>
				<input id="ca-groups" type="text" bind:value={assessmentForm.impacted_groups} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
			</div>

			<div>
				<p class="mb-1 text-xs font-medium">{t('p3m.impactByCharacteristic')}</p>
				{#each assessmentForm.rows as row, i}
					<div class="mb-1 flex items-center gap-2">
						<input type="text" placeholder={t('p3m.characteristic')} value={row.characteristic} oninput={(e) => (assessmentForm.rows[i].characteristic = e.target.value)} class="min-w-0 flex-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
						<input type="text" placeholder={t('p3m.nature')} value={row.nature} oninput={(e) => (assessmentForm.rows[i].nature = e.target.value)} class="min-w-0 flex-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
						<select value={row.severity} onchange={(e) => (assessmentForm.rows[i].severity = e.target.value)} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
							<option value="">—</option>
							<option value="low">Low</option>
							<option value="medium">Medium</option>
							<option value="high">High</option>
						</select>
						<button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => removeImpactRow(i)}>✕</button>
					</div>
				{/each}
				<button type="button" class="text-xs text-[var(--color-ds-accent-blue,#2563eb)] hover:underline" onclick={addImpactRow}>+ {t('p3m.characteristic')}</button>
			</div>

			<div class="grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-2">
				<div>
					<label class="mb-1 block text-xs font-medium" for="ca-rating">{t('p3m.overallRating')}</label>
					<select id="ca-rating" bind:value={assessmentForm.overall_rating} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
						{#each ['1', '2', '3', '4', '5'] as n}<option value={n}>{n}</option>{/each}
					</select>
				</div>
				<div>
					<label class="mb-1 block text-xs font-medium" for="ca-support">{t('p3m.supportLevel')}</label>
					<select id="ca-support" bind:value={assessmentForm.support_level} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
						<option value="none">None</option>
						<option value="light">Light</option>
						<option value="full">Full</option>
					</select>
				</div>
			</div>

			<div>
				<label class="mb-1 block text-xs font-medium" for="ca-approach">{t('p3m.approach')}</label>
				<textarea id="ca-approach" rows="2" bind:value={assessmentForm.approach} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
			</div>

			<div>
				<label class="mb-1 block text-xs font-medium" for="ca-deliverables">{t('p3m.deliverables')}</label>
				<textarea id="ca-deliverables" rows="2" bind:value={assessmentForm.deliverables} placeholder="One item per line" class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm"></textarea>
			</div>

			{#if error}<p class="text-xs" style="color:#ef4444">{error}</p>{/if}

			<div class="flex gap-2">
				<button type="submit" disabled={savingAssessment} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50">{savingAssessment ? 'Saving…' : 'Save'}</button>
				<button type="button" onclick={() => (showAssessmentForm = false)} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Cancel</button>
			</div>
		</form>
	{:else if assessment}
		<div class="space-y-3">
			<div class="flex flex-wrap items-center gap-2">
				<span class="inline-block rounded-full px-2 py-0.5 text-xs font-semibold" style="background-color:#f59e0b; color:#fff">{t('p3m.overallRating')}: {assessment.overall_rating}/5</span>
				<span class="inline-block rounded-full bg-slate-100 px-2 py-0.5 text-xs capitalize text-slate-600">{t('p3m.supportLevel')}: {assessment.support_level}</span>
			</div>

			{#if parseItems(assessment.impacted_groups).length}
				<div>
					<p class="mb-1 text-xs font-medium">{t('p3m.impactedGroups')}</p>
					<ul class="list-disc space-y-0.5 pl-4 text-sm">
						{#each parseItems(assessment.impacted_groups) as g}<li>{g}</li>{/each}
					</ul>
				</div>
			{/if}

			{#if parseItems(assessment.impact_by_characteristic).length}
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead><tr class="text-left text-xs text-[var(--color-ds-text-muted,#6b7280)]"><th class="py-1 pr-3">{t('p3m.characteristic')}</th><th class="py-1 pr-3">{t('p3m.nature')}</th><th class="py-1">Severity</th></tr></thead>
						<tbody>
							{#each parseItems(assessment.impact_by_characteristic) as r}
								<tr class="border-t border-[var(--color-ds-border,#e5e7eb)]"><td class="py-1 pr-3">{r.characteristic}</td><td class="py-1 pr-3">{r.nature}</td><td class="py-1 capitalize">{r.severity || '—'}</td></tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if assessment.approach}
				<div><p class="mb-1 text-xs font-medium">{t('p3m.approach')}</p><p class="whitespace-pre-wrap text-sm">{assessment.approach}</p></div>
			{/if}

			{#if parseItems(assessment.deliverables).length}
				<div>
					<p class="mb-1 text-xs font-medium">{t('p3m.deliverables')}</p>
					<ul class="list-disc space-y-0.5 pl-4 text-sm">
						{#each parseItems(assessment.deliverables) as d}<li>{d}</li>{/each}
					</ul>
				</div>
			{/if}

			<button onclick={openEditAssessment} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Edit</button>
		</div>
	{:else if error}
		<p class="text-xs" style="color:#ef4444">{error}</p>
	{:else}
		<button onclick={openNewAssessment} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">{t('p3m.newAssessment')}</button>
	{/if}

	<!-- Readiness assessments -->
	<h4 class="mb-2 mt-4 border-t border-[var(--color-ds-border,#e5e7eb)] pt-3 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.readinessTitle')}</h4>

	{#if loadingReadiness}
		<p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">Loading…</p>
	{:else if showReadinessForm}
		<form onsubmit={(e) => { e.preventDefault(); saveReadiness(); }} class="space-y-3">
			<div>
				<p class="mb-1 text-xs font-medium">{t('p3m.readinessTitle')}</p>
				{#each readinessForm.questions as q, i}
					<div class="mb-1 flex items-center gap-2">
						<input type="text" value={q.text} oninput={(e) => (readinessForm.questions[i].text = e.target.value)} placeholder={t('p3m.question')} class="min-w-0 flex-1 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
						<label class="flex items-center gap-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
							<input type="checkbox" checked={q.required} onchange={(e) => (readinessForm.questions[i].required = e.target.checked)} /> {t('p3m.required')}
						</label>
						<button type="button" class="text-[var(--color-ds-text-muted,#6b7280)] hover:text-red-600" title={t('p3m.delete')} onclick={() => removeQuestion(i)}>✕</button>
					</div>
				{/each}
				<button type="button" class="text-xs text-[var(--color-ds-accent-blue,#2563eb)] hover:underline" onclick={addQuestion}>+ {t('p3m.question')}</button>
			</div>

			<div>
				<p class="mb-1 text-xs font-medium">{t('p3m.outcome')}</p>
				{#each readinessForm.questions.filter((q) => q.text.trim()) as q}
					{@const a = readinessForm.answers[q.id] ?? { answer: '', gap_action: '' }}
					<div class="mb-1 flex items-center gap-2">
						<span class="min-w-0 flex-1 truncate text-sm" title={q.text}>{q.text}</span>
						<select value={a.answer} onchange={(e) => setAnswer(q.id, { answer: e.target.value })} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
							<option value="">—</option>
							<option value="yes">Yes</option>
							<option value="no">No</option>
						</select>
						{#if a.answer === 'no'}
							<input type="text" placeholder={t('p3m.gapAction')} value={a.gap_action} oninput={(e) => setAnswer(q.id, { gap_action: e.target.value })} class="w-48 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm" />
						{/if}
					</div>
				{/each}
			</div>

			<div>
				<label class="mb-1 block text-xs font-medium" for="ra-outcome">{t('p3m.outcome')}</label>
				<select id="ra-outcome" bind:value={readinessForm.outcome} class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-sm">
					<option value="go">{t('p3m.outcomeGo')}</option>
					<option value="no_go">{t('p3m.outcomeNoGo')}</option>
					<option value="reassess">{t('p3m.outcomeReassess')}</option>
				</select>
			</div>

			{#if error}<p class="text-xs" style="color:#ef4444">{error}</p>{/if}

			<div class="flex gap-2">
				<button type="submit" disabled={savingReadiness} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white disabled:opacity-50">{savingReadiness ? 'Saving…' : 'Save'}</button>
				<button type="button" onclick={() => (showReadinessForm = false)} class="rounded border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-sm">Cancel</button>
			</div>
		</form>
	{:else if readinessList.length}
		<div class="space-y-3">
			{#each [...readinessList].sort((x, y) => String(y.created_at).localeCompare(String(x.created_at))) as ra (ra.id)}
				{@const questions = parseItems(ra.questions) ?? []}
				{@const answers = Object.fromEntries((parseItems(ra.answers) ?? []).map((a) => [a.question_id, a]))}
				<div class="rounded border border-[var(--color-ds-border,#e5e7eb)] p-2">
					<div class="mb-1 flex items-center gap-2">
						<span class="inline-block rounded-full px-2 py-0.5 text-xs font-semibold" style="background-color:{outcomeColor(ra.outcome)}; color:#fff">{outcomeLabel(ra.outcome)}</span>
						<span class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{fmtDate(ra.created_at)}</span>
					</div>
					<ul class="space-y-0.5">
						{#each questions as q}
							{@const a = answers[q.id]}
							<li class="flex items-start gap-2 text-sm">
								<span class={a?.answer === 'no' ? 'text-red-600' : a?.answer === 'yes' ? '' : 'text-[var(--color-ds-text-muted,#6b7280)]'}>{q.text}{#if q.required} *{/if}</span>
								{#if a?.answer}<span class="ml-auto shrink-0 text-xs capitalize">{a.answer}</span>{/if}
							</li>
							{#if a?.gap_action}<li class="pl-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.gapAction')}: {a.gap_action}</li>{/if}
						{/each}
					</ul>
				</div>
			{/each}
		</div>
	{:else if error}
		<p class="text-xs" style="color:#ef4444">{error}</p>
	{:else}
		<button onclick={openNewReadiness} class="rounded bg-[var(--color-ds-primary,#2563eb)] px-3 py-1 text-sm font-medium text-white">{t('p3m.newReadiness')}</button>
	{/if}
</section>
