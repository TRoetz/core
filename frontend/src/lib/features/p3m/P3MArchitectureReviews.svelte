<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
  import IconX from '@tabler/icons-svelte-runes/icons/x';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { formatDate } from './utils.js';

  let { portfolios = [] } = $props();

  let reviews = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Creation form.
  let showForm = $state(false);
  let form = $state({ project_key: '', title: '', description: '' });
  let saving = $state(false);

  // Decision panel (one review at a time).
  let decisionFor = $state(null);
  let comments = $state('');
  let pageIdInput = $state('');

  /** Flat project options, including standalone projects. */
  const projectOptions = $derived.by(() => {
    const opts = [];
    for (const pf of portfolios) {
      for (const pg of pf.programmes ?? []) {
        for (const pr of pg.projects ?? []) opts.push({ id: pr.id, label: `${pf.title} › ${pg.title} › ${pr.title}` });
      }
      for (const pr of pf.standalone_projects ?? []) opts.push({ id: pr.id, label: `${pf.title} › ${pr.title}` });
    }
    return opts;
  });

  const projectTitleById = $derived(new Map(projectOptions.map((p) => [p.id, p.label])));

  async function load() {
    loading = true;
    error = '';
    try {
      const r = await p3mApi.architectureReviews.getAll();
      reviews = Array.isArray(r) ? r : [];
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function submitCreate() {
    if (saving) return;
    error = '';
    const projectId = Number(form.project_key);
    if (!form.title.trim()) {
      error = t('p3m.titleRequired');
      return;
    }
    if (!projectId) {
      error = t('p3m.selectProject');
      return;
    }
    saving = true;
    try {
      await p3mApi.architectureReviews.create({
        project_id: projectId,
        title: form.title.trim(),
        description: form.description,
      });
      form = { project_key: '', title: '', description: '' };
      showForm = false;
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  async function submitForArb(review) {
    if (saving) return;
    error = '';
    saving = true;
    try {
      await p3mApi.architectureReviews.update(review.id, { status: 'in_review' });
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  function openDecision(review) {
    decisionFor = review.id;
    comments = '';
    pageIdInput = review.architecture_page_id ? String(review.architecture_page_id) : '';
  }

  function closeDecision() {
    decisionFor = null;
    comments = '';
    pageIdInput = '';
  }

  // Approval requires a linked architecture page; link it first when missing.
  async function linkPage(review) {
    if (saving) return;
    error = '';
    const pageId = Number(pageIdInput);
    if (!pageId) {
      error = t('p3m.pageIdRequired');
      return;
    }
    saving = true;
    try {
      await p3mApi.architectureReviews.update(review.id, { architecture_page_id: pageId });
      const fresh = (await p3mApi.architectureReviews.get(review.id)) ?? null;
      reviews = reviews.map((r) => (r.id === review.id ? fresh : r));
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  async function decide(approved) {
    if (saving) return;
    const rv = reviews.find((r) => r.id === decisionFor);
    if (!rv) return;
    error = '';
    saving = true;
    try {
      await p3mApi.decideArchitectureReview(rv.id, { approved, comments });
      closeDecision();
      await load();
    } catch (e) {
      error = e?.message || t('p3m.createFailed');
    } finally {
      saving = false;
    }
  }

  const statusChip = (status) => {
    switch (status) {
      case 'wip':
        return 'bg-slate-100 text-slate-600';
      case 'in_review':
        return 'bg-amber-100 text-amber-700';
      case 'approved':
        return 'bg-emerald-100 text-emerald-700';
      case 'needs_work':
        return 'bg-red-100 text-red-700';
      default:
        return 'bg-slate-100 text-slate-600';
    }
  };

  const statusLabel = (status) => {
    switch (status) {
      case 'wip':
        return t('p3m.arbWip');
      case 'in_review':
        return t('p3m.arbInReview');
      case 'approved':
        return t('p3m.arbApproved');
      case 'needs_work':
        return t('p3m.arbNeedsWork');
      default:
        return status;
    }
  };
</script>

<section class="space-y-6 px-6 py-4">
  <div>
    <h2 class="mb-2 text-sm font-semibold">{t('p3m.architectureReviews')}</h2>
    {#if error}
      <div class="mb-2 rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>
    {/if}

    {#if loading}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('p3m.loading')}
      </div>
    {:else if reviews.length === 0 && !showForm}
      <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
        {t('p3m.noReviews')}
      </div>
    {:else}
      <table class="w-full text-left text-xs">
        <thead>
          <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('p3m.projects')}</th>
            <th class="py-1.5 pr-3">{t('p3m.titleLabel')}</th>
            <th class="py-1.5 pr-3">{t('p3m.statusHeader')}</th>
            <th class="py-1.5 pr-3">{t('p3m.decidedAt')}</th>
            <th class="py-1.5"></th>
          </tr>
        </thead>
        <tbody>
          {#each reviews as review (review.id)}
            {@const open = decisionFor === review.id}
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
              <td class="py-1.5 pr-3">{projectTitleById.get(review.project_id) ?? `#${review.project_id}`}</td>
              <td class="py-1.5 pr-3">
                <div class="font-medium">{review.title}</div>
                {#if review.description}
                  <div class="mt-0.5 max-w-xl text-[var(--color-ds-text-muted,#6b7280)]">{review.description}</div>
                {/if}
              </td>
              <td class="py-1.5 pr-3">
                <span class={statusChip(review.status)}>{statusLabel(review.status)}</span>
                {#if review.architecture_page_id}
                  <div class="mt-0.5 text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.pageRef', { id: review.architecture_page_id })}</div>
                {/if}
              </td>
              <td class="py-1.5 pr-3 whitespace-nowrap">
                {review.decided_at ? formatDate(review.decided_at) : t('p3m.notSet')}
                {#if review.status === 'needs_work' && review.decision_item_id}
                  <div class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.recordedInRegister')}</div>
                {/if}
              </td>
              <td class="py-1.5 text-right">
                {#if review.status === 'wip'}
                  <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered disabled:opacity-50" disabled={saving} onclick={() => submitForArb(review)}>
                    {t('p3m.submitForArb')}
                  </button>
                {:else if review.status === 'in_review'}
                  <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered disabled:opacity-50" disabled={saving} onclick={() => (open ? closeDecision() : openDecision(review))}>
                    {t('p3m.decide')}
                  </button>
                {:else if review.comments}
                  <div class="max-w-xs text-right text-[var(--color-ds-text-muted,#6b7280)]">{review.comments}</div>
                {/if}
              </td>
            </tr>
            {#if open && review.status === 'in_review'}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                <td colspan="5" class="py-2 pr-3 pl-8">
                  {#if !review.architecture_page_id}
                    <div class="mb-2 flex items-center gap-2 text-xs">
                      <label class="text-[var(--color-ds-text-muted,#6b7280)]">{t('p3m.architecturePageId')}</label>
                      <input type="number" min="1" class="w-24 rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" bind:value={pageIdInput} />
                      <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered disabled:opacity-50" disabled={saving || !Number(pageIdInput)} onclick={() => linkPage(review)}>
                        {t('p3m.link')}
                      </button>
                    </div>
                  {/if}
                  <textarea class="mb-2 w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" rows="2" placeholder={t('p3m.decisionComments')} bind:value={comments} />
                  <div class="flex gap-2">
                    <button type="button" class="rounded-md bg-emerald-600 px-3 py-1.5 font-medium text-white hover:bg-emerald-700 disabled:opacity-50" disabled={saving || !review.architecture_page_id} onclick={() => decide(true)}>
                      {t('p3m.approve')}
                    </button>
                    <button type="button" class="rounded-md bg-red-600 px-3 py-1.5 font-medium text-white hover:bg-red-700 disabled:opacity-50" disabled={saving} onclick={() => decide(false)}>
                      {t('p3m.reject')}
                    </button>
                  </div>
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    {/if}

    <button type="button" class="mt-2 flex items-center gap-1 text-xs font-medium hover:underline" onclick={() => (showForm = !showForm)}>
      {#if showForm}<IconX size={12} />{t('p3m.cancel')}{:else}<IconPlus size={12} />{t('p3m.newReview')}{/if}
    </button>

    {#if showForm}
      <div class="mt-2 grid gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2 md:grid-cols-2">
        <select class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" value={form.project_key} onchange={(e) => (form.project_key = e.target.value)}>
          <option value="">—</option>
          {#each projectOptions as p (p.id)}
            <option value={String(p.id)}>{p.label}</option>
          {/each}
        </select>
        <input class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs" placeholder={t('p3m.titleLabel')} bind:value={form.title} />
        <textarea class="w-full rounded border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs md:col-span-2" rows="2" placeholder={t('p3m.description')} bind:value={form.description} />
        <button type="button" class="rounded bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50" disabled={saving} onclick={submitCreate}>
          {t('p3m.save')}
        </button>
      </div>
    {/if}
  </div>
</section>
