<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { srm } from '../../api/srm.js';
  import { formatDate, formatMoney } from './utils.js';

  let categories = $state([]);
  let runs = $state([]);
  let validations = $state([]);
  let suppliers = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Category form.
  let catName = $state('');
  let catDesc = $state('');

  // New run form (scope: category | supplier | lite).
  let newScope = $state('category');
  let newCategoryId = $state('');
  let newNotes = $state('');

  // Run detail.
  let selectedRunId = $state(null);
  let runDetail = $state(null);
  let answerEdits = $state({}); // answer.id -> { score, anchor_label, weight, justification }
  let savingAnswerId = $state(null);

  // Add-answer form inside the detail panel.
  let ansEntityRef = $state('');
  let ansQuestionCode = $state('');
  let ansScore = $state('2');
  let ansAnchorLabel = $state('');
  let ansWeight = $state('1');
  let ansJustification = $state('');

  // Validation request form.
  let valRunId = $state('');
  let valSupplierId = $state('');
  let valComment = $state('');

  // Inline decide controls (one row expanded at a time).
  let decidingId = $state(null);
  let decideComment = $state('');

  const supplierNameById = $derived(new Map(suppliers.map((s) => [s.id, s.company_name])));
  const completeRuns = $derived(runs.filter((r) => r.status === 'complete'));

  async function load() {
    loading = true;
    error = '';
    try {
      const [cats, runRows, valRows, sups] = await Promise.all([
        srm.categories.getAll(),
        srm.getSegmentationRuns(),
        srm.getValidationRequests(),
        srm.suppliers.getAll(),
      ]);
      categories = cats ?? [];
      runs = runRows ?? [];
      validations = valRows ?? [];
      suppliers = sups ?? [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadRunDetail(id) {
    try {
      const run = await srm.getSegmentationRun(id);
      runDetail = run ?? null;
      selectedRunId = id;
      const edits = {};
      for (const a of run?.answers ?? []) {
        edits[a.id] = { score: a.score, anchor_label: a.anchor_label || '', weight: a.weight, justification: a.justification || '' };
      }
      answerEdits = edits;
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function selectRun(id) {
    if (selectedRunId === id) return;
    selectedRunId = id;
    runDetail = null;
    answerEdits = {};
    loadRunDetail(id);
  }

  async function addCategory() {
    const name = catName.trim();
    if (!name) return;
    try {
      await srm.categories.create({ name, description: catDesc.trim() });
      catName = '';
      catDesc = '';
      categories = (await srm.categories.getAll()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function deleteCategory(id) {
    try {
      await srm.categories.delete(id);
      categories = (await srm.categories.getAll()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function createRun() {
    const data = { scope: newScope, notes: newNotes.trim() };
    if (newScope === 'category') {
      if (!newCategoryId) return;
      data.category_id = Number(newCategoryId);
    }
    try {
      await srm.createSegmentationRun(data);
      newNotes = '';
      runs = (await srm.getSegmentationRuns()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function saveAnswer(answerId, answer) {
    const edit = answerEdits[answerId];
    if (!edit) return;
    savingAnswerId = answerId;
    try {
      await srm.upsertSegmentationAnswer(selectedRunId, {
        entity_ref: answer.entity_ref,
        question_code: answer.question_code,
        score: Number(edit.score),
        anchor_label: String(edit.anchor_label).trim() || null,
        weight: Number(edit.weight) > 0 ? Number(edit.weight) : 1,
        justification: String(edit.justification).trim() || null,
      });
      await loadRunDetail(selectedRunId);
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      savingAnswerId = null;
    }
  }

  async function addAnswer() {
    if (!ansEntityRef || !ansQuestionCode.trim()) return;
    try {
      await srm.upsertSegmentationAnswer(selectedRunId, {
        entity_ref: Number(ansEntityRef),
        question_code: ansQuestionCode.trim(),
        score: Number(ansScore),
        anchor_label: ansAnchorLabel.trim() || null,
        weight: Number(ansWeight) > 0 ? Number(ansWeight) : 1,
        justification: ansJustification.trim() || null,
      });
      ansQuestionCode = '';
      ansAnchorLabel = '';
      ansJustification = '';
      await loadRunDetail(selectedRunId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function completeRun() {
    try {
      await srm.completeSegmentationRun(selectedRunId);
      runs = (await srm.getSegmentationRuns()) ?? [];
      await loadRunDetail(selectedRunId);
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  async function createValidation() {
    if (!valRunId) return;
    try {
      await srm.createValidationRequest({
        run_id: Number(valRunId),
        supplier_id: valSupplierId ? Number(valSupplierId) : null,
        comment: valComment.trim(),
      });
      valComment = '';
      validations = (await srm.getValidationRequests()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function toggleDecide(id) {
    if (decidingId === id) {
      decidingId = null;
    } else {
      decidingId = id;
      decideComment = '';
    }
  }

  async function decide(id, status) {
    try {
      const data = { status };
      if (decideComment.trim()) data.comment = decideComment.trim();
      await srm.decideValidationRequest(id, data);
      decidingId = null;
      decideComment = '';
      // Approve validates the run and applies tiers; changes_requested reopens it.
      validations = (await srm.getValidationRequests()) ?? [];
      runs = (await srm.getSegmentationRuns()) ?? [];
    } catch (e) {
      error = e?.message || 'error';
    }
  }

  function scopeLabel(scope) {
    switch (scope) {
      case 'category':
        return t('srm.category');
      case 'supplier':
        return t('srm.supplier');
      case 'lite':
        return t('srm.lite');
      default:
        return scope || '';
    }
  }

  function runStatusLabel(status) {
    switch (status) {
      case 'in_progress':
        return t('srm.statusInProgress');
      case 'complete':
        return t('srm.statusComplete');
      case 'validated':
        return t('srm.statusValidated');
      case 'superseded':
        return t('srm.statusSuperseded');
      default:
        return status || '';
    }
  }

  function runStatusClass(status) {
    if (status === 'complete' || status === 'validated') return 'bg-emerald-100 text-emerald-700';
    if (status === 'in_progress') return 'bg-amber-100 text-amber-700';
    return 'bg-slate-100 text-slate-600';
  }

  function valStatusLabel(status) {
    switch (status) {
      case 'pending':
        return t('srm.statusPending');
      case 'approved':
        return t('srm.statusApproved');
      case 'changes_requested':
        return t('srm.statusChangesRequested');
      case 'rejected':
        return t('srm.statusRejected');
      default:
        return status || '';
    }
  }

  function valStatusClass(status) {
    if (status === 'approved') return 'bg-emerald-100 text-emerald-700';
    if (status === 'pending' || status === 'changes_requested') return 'bg-amber-100 text-amber-700';
    if (status === 'rejected') return 'bg-red-100 text-red-700';
    return 'bg-slate-100 text-slate-600';
  }

  onMount(load);
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">
    {t('srm.loading')}
  </div>
{:else if error && categories.length === 0 && runs.length === 0 && validations.length === 0}
  <EmptyState title={t('srm.loadErrorTitle')} description={error} actions={{ label: t('srm.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    {#if error}
      <div class="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
    {/if}

    <!-- Categories -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.categories')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.name')}
          value={catName}
          oninput={(e) => (catName = e.target.value)}
        />
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.description')}
          value={catDesc}
          oninput={(e) => (catDesc = e.target.value)}
        />
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addCategory}>
          {t('srm.addCategory')}
        </button>
      </div>
      {#if categories.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.name')}</th>
              <th class="py-1.5 pr-3">{t('srm.description')}</th>
              <th class="py-1.5 pr-3">{t('srm.supplierCount')}</th>
              <th class="py-1.5 pr-3">{t('srm.createdAt')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each categories as c (c.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{c.name}</td>
                <td class="py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{c.description || ''}</td>
                <td class="py-1.5 pr-3">{c.supplier_count ?? 0}</td>
                <td class="py-1.5 pr-3">{formatDate(c.created_at)}</td>
                <td class="py-1.5 pr-3 text-right">
                  <button type="button" class="cursor-pointer text-red-600 hover:underline" onclick={() => deleteCategory(c.id)}>
                    {t('srm.delete')}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Segmentation runs -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.segmentationRuns')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newScope} onchange={(e) => (newScope = e.target.value)}>
          <option value="category">{t('srm.category')}</option>
          <option value="supplier">{t('srm.supplier')}</option>
          <option value="lite">{t('srm.lite')}</option>
        </select>
        {#if newScope === 'category'}
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={newCategoryId} onchange={(e) => (newCategoryId = e.target.value)}>
            <option value="">—</option>
            {#each categories as c (c.id)}
              <option value={c.id}>{c.name}</option>
            {/each}
          </select>
        {/if}
        <input
          class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs"
          placeholder={t('srm.notes')}
          value={newNotes}
          oninput={(e) => (newNotes = e.target.value)}
        />
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={createRun}>
          {t('srm.newRun')}
        </button>
      </div>
      {#if runs.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.run')}</th>
              <th class="py-1.5 pr-3">{t('srm.scope')}</th>
              <th class="py-1.5 pr-3">{t('srm.status')}</th>
              <th class="py-1.5 pr-3">{t('srm.createdAt')}</th>
            </tr>
          </thead>
          <tbody>
            {#each runs as run (run.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedRunId === run.id ? 'bg-ds-surface-selected' : ''}"
                onclick={() => selectRun(run.id)}
              >
                <td class="py-1.5 pr-3">#{run.id}</td>
                <td class="py-1.5 pr-3">
                  {scopeLabel(run.scope)}{#if run.category_name}<span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {run.category_name}</span>{/if}
                </td>
                <td class="py-1.5 pr-3">
                  <span class="rounded-full px-2 py-0.5 {runStatusClass(run.status)}">{runStatusLabel(run.status)}</span>
                </td>
                <td class="py-1.5 pr-3">{formatDate(run.created_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <!-- Run detail -->
      {#if selectedRunId !== null && runDetail === null}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.loading')}
        </div>
      {:else if runDetail}
        {@const editable = runDetail.status === 'in_progress'}
        <div class="mt-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <div class="mb-2 flex items-center gap-2">
            <span class="text-xs font-semibold">{t('srm.run')} #{runDetail.id}</span>
            <span class="rounded-full px-2 py-0.5 {runStatusClass(runDetail.status)}">{runStatusLabel(runDetail.status)}</span>
            {#if runDetail.notes}
              <span class="text-[var(--color-ds-text-muted,#6b7280)]">{runDetail.notes}</span>
            {/if}
            {#if editable}
              <button type="button" class="ml-auto cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={completeRun}>
                {t('srm.completeRun')}
              </button>
            {/if}
          </div>

          <h3 class="mb-1 text-xs font-semibold">{t('srm.answers')}</h3>
          {#if (runDetail.answers ?? []).length === 0}
            <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
              {t('srm.noRecords')}
            </div>
          {:else}
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-3">{t('srm.entity')}</th>
                  <th class="py-1.5 pr-3">{t('srm.question')}</th>
                  <th class="py-1.5 pr-3">{t('srm.score')}</th>
                  <th class="py-1.5 pr-3">{t('srm.anchorLabel')}</th>
                  <th class="py-1.5 pr-3">{t('srm.weight')}</th>
                  <th class="py-1.5 pr-3">{t('srm.justification')}</th>
                  {#if editable}<th class="py-1.5 pr-3"></th>{/if}
                </tr>
              </thead>
              <tbody>
                {#each runDetail.answers as a (a.id)}
                  {@const edit = answerEdits[a.id]}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{supplierNameById.get(a.entity_ref) ?? a.entity_name ?? `#${a.entity_ref}`}</td>
                    <td class="py-1.5 pr-3 font-mono text-[10px]">{a.question_code}</td>
                    {#if editable && edit}
                      <td class="py-1.5 pr-3">
                        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={String(edit.score)} onchange={(e) => (edit.score = e.target.value)}>
                          {#each [0, 1, 2, 3] as n (n)}
                            <option value={n}>{n}</option>
                          {/each}
                        </select>
                      </td>
                      <td class="py-1.5 pr-3">
                        <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={edit.anchor_label} oninput={(e) => (edit.anchor_label = e.target.value)} />
                      </td>
                      <td class="py-1.5 pr-3">
                        <input type="number" step="0.1" min="0" class="w-16 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={edit.weight} oninput={(e) => (edit.weight = e.target.value)} />
                      </td>
                      <td class="py-1.5 pr-3">
                        <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={edit.justification} oninput={(e) => (edit.justification = e.target.value)} />
                      </td>
                      <td class="py-1.5 pr-3 text-right">
                        <button type="button" disabled={savingAnswerId === a.id} class="cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered disabled:opacity-50" onclick={() => saveAnswer(a.id, a)}>
                          {t('srm.save')}
                        </button>
                      </td>
                    {:else}
                      <td class="py-1.5 pr-3">{a.score}</td>
                      <td class="py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{a.anchor_label || ''}</td>
                      <td class="py-1.5 pr-3">{a.weight}</td>
                      <td class="py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]">{a.justification || ''}</td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}

          {#if editable}
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={ansEntityRef} onchange={(e) => (ansEntityRef = e.target.value)}>
                <option value="">—</option>
                {#each suppliers as s (s.id)}
                  <option value={s.id}>{s.company_name}</option>
                {/each}
              </select>
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.question')} value={ansQuestionCode} oninput={(e) => (ansQuestionCode = e.target.value)} />
              <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={ansScore} onchange={(e) => (ansScore = e.target.value)}>
                {#each [0, 1, 2, 3] as n (n)}
                  <option value={String(n)}>{t('srm.score')}: {n}</option>
                {/each}
              </select>
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.anchorLabel')} value={ansAnchorLabel} oninput={(e) => (ansAnchorLabel = e.target.value)} />
              <input type="number" step="0.1" min="0" class="w-16 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.weight')} value={ansWeight} oninput={(e) => (ansWeight = e.target.value)} />
              <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.justification')} value={ansJustification} oninput={(e) => (ansJustification = e.target.value)} />
              <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={addAnswer}>
                {t('srm.addAnswer')}
              </button>
            </div>
          {/if}

          {#if (runDetail.results ?? []).length > 0}
            <h3 class="mt-3 mb-1 text-xs font-semibold">{t('srm.results')}</h3>
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-3">{t('srm.entity')}</th>
                  <th class="py-1.5 pr-3">{t('srm.box')}</th>
                  <th class="py-1.5 pr-3">{t('srm.ranks')}</th>
                  <th class="py-1.5 pr-3">{t('srm.spend')}</th>
                  <th class="py-1.5 pr-3">{t('srm.mappedTier')}</th>
                </tr>
              </thead>
              <tbody>
                {#each runDetail.results as res (res.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{res.entity_name ?? `#${res.entity_ref}`}</td>
                    <td class="py-1.5 pr-3">{res.box_number ?? res.segmentation_box}</td>
                    <td class="py-1.5 pr-3 capitalize text-[var(--color-ds-text-muted,#6b7280)]">
                      {res.rank_criticality || '—'} / {res.rank_value || '—'}
                    </td>
                    <td class="py-1.5 pr-3">{res.spend_amount != null ? formatMoney(res.spend_amount) : ''}</td>
                    <td class="py-1.5 pr-3">{res.mapped_tier || ''}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      {/if}
    </section>

    <!-- Validation requests -->
    <section>
      <h2 class="mb-2 text-sm font-semibold">{t('srm.validationRequests')}</h2>
      <div class="mb-2 flex items-center gap-2">
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={valRunId} onchange={(e) => (valRunId = e.target.value)}>
          <option value="">—</option>
          {#each completeRuns as r (r.id)}
            <option value={r.id}>{t('srm.run')} #{r.id}</option>
          {/each}
        </select>
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={valSupplierId} onchange={(e) => (valSupplierId = e.target.value)}>
          <option value="">—</option>
          {#each suppliers as s (s.id)}
            <option value={s.id}>{s.company_name}</option>
          {/each}
        </select>
        <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.comment')} value={valComment} oninput={(e) => (valComment = e.target.value)} />
        <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={createValidation}>
          {t('srm.create')}
        </button>
      </div>
      {#if validations.length === 0}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
          {t('srm.noRecords')}
        </div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.run')}</th>
              <th class="py-1.5 pr-3">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-3">{t('srm.status')}</th>
              <th class="py-1.5 pr-3">{t('srm.cycles')}</th>
              <th class="py-1.5 pr-3">{t('srm.comment')}</th>
              <th class="py-1.5 pr-3">{t('srm.createdAt')}</th>
              <th class="py-1.5 pr-3"></th>
            </tr>
          </thead>
          <tbody>
            {#each validations as v (v.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">#{v.run_id}</td>
                <td class="py-1.5 pr-3">{v.supplier_name || '—'}</td>
                <td class="py-1.5 pr-3">
                  <span class="rounded-full px-2 py-0.5 {valStatusClass(v.status)}">{valStatusLabel(v.status)}</span>
                </td>
                <td class="py-1.5 pr-3">{v.cycle_count}</td>
                <td class="max-w-[16rem] truncate py-1.5 pr-3 text-[var(--color-ds-text-muted,#6b7280)]" title={v.comment}>{v.comment || ''}</td>
                <td class="py-1.5 pr-3">{formatDate(v.created_at)}</td>
                <td class="py-1.5 pr-3 text-right">
                  {#if v.status === 'pending'}
                    <button type="button" class="cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => toggleDecide(v.id)}>
                      {t('srm.decide')}
                    </button>
                  {/if}
                </td>
              </tr>
              {#if decidingId === v.id}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td colspan="7" class="py-1.5 pr-3 pl-8">
                    <div class="flex flex-wrap items-center gap-2">
                      <input class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" placeholder={t('srm.comment')} value={decideComment} oninput={(e) => (decideComment = e.target.value)} />
                      <button type="button" class="cursor-pointer rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs text-emerald-700 hover:bg-emerald-100" onclick={() => decide(v.id, 'approved')}>
                        {t('srm.approve')}
                      </button>
                      <button type="button" class="cursor-pointer rounded-md border border-amber-200 bg-amber-50 px-2 py-1 text-xs text-amber-700 hover:bg-amber-100" onclick={() => decide(v.id, 'changes_requested')}>
                        {t('srm.requestChanges')}
                      </button>
                      <button type="button" class="cursor-pointer rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-700 hover:bg-red-100" onclick={() => decide(v.id, 'rejected')}>
                        {t('srm.reject')}
                      </button>
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
{/if}
