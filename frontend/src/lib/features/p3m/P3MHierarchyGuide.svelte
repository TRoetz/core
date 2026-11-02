<script>
  import { t } from '../../stores/i18n.svelte.js';
  import IconChevronDown from '@tabler/icons-svelte-runes/icons/chevron-down';

  // Explains the P3M tier model under the page header:
  //   Portfolio  -> WHAT to invest in (investment / strategy)
  //   Programme  -> HOW to deliver change (transformation / benefit)
  //   Project    -> WHAT is produced (product / output)
  //   -> Operational work (business-as-usual, owns the benefit)
  let open = $state(true);

  const TIERS = [
    {
      nameKey: 'p3m.tierPortfolio',
      descKey: 'p3m.tierPortfolioDesc',
      meaningKey: 'p3m.tierPortfolioMeaning',
      subKey: 'p3m.tierPortfolioSub',
      color: '#6366f1',
    },
    {
      nameKey: 'p3m.tierProgramme',
      descKey: 'p3m.tierProgrammeDesc',
      meaningKey: 'p3m.tierProgrammeMeaning',
      subKey: 'p3m.tierProgrammeSub',
      color: '#0ea5e9',
    },
    {
      nameKey: 'p3m.tierProject',
      descKey: 'p3m.tierProjectDesc',
      meaningKey: 'p3m.tierProjectMeaning',
      subKey: 'p3m.tierProjectSub',
      color: '#10b981',
    },
  ];
</script>

<div class="p3m-guide">
  <button
    type="button"
    class="p3m-guide-toggle"
    onclick={() => (open = !open)}
    aria-expanded={open}
  >
    <IconChevronDown size={14} class={open ? '' : '-rotate-90'} />
    <span>{t('p3m.hierarchyTitle')}</span>
  </button>

  {#if open}
    <div class="rounded-lg border border-[var(--color-ds-border,#e5e7eb)]">
      {#each TIERS as tier, i (tier.nameKey)}
        <div
          class="flex items-center gap-4 px-4 py-2.5 {i > 0
            ? 'border-t border-[var(--color-ds-border,#e5e7eb)]'
            : ''}"
        >
          <span class="h-6 w-1 shrink-0 rounded-full" style="background: {tier.color}"></span>
          <div class="min-w-0 flex-1">
            <div class="text-xs font-bold uppercase tracking-wide" style="color: {tier.color}">
              {t(tier.nameKey)}
            </div>
            <div class="text-xs text-[var(--color-ds-text-secondary,#4b5563)]">{t(tier.descKey)}</div>
          </div>
          <div class="shrink-0 text-right">
            <div class="text-xs font-semibold text-[var(--color-ds-text,#111827)]">{t(tier.meaningKey)}</div>
            <div class="text-[11px] text-[var(--color-ds-text-muted,#6b7280)]">{t(tier.subKey)}</div>
          </div>
        </div>
      {/each}
    </div>

    <div class="flex flex-col items-center pt-1">
      <svg width="14" height="18" viewBox="0 0 14 18" fill="none" aria-hidden="true">
        <path d="M7 0v13M2.5 9.5 7 14l4.5-4.5" stroke="var(--color-ds-text-muted,#6b7280)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <div class="rounded-md bg-[var(--color-ds-surface-hover,#f3f4f6)] px-3 py-1 text-xs text-[var(--color-ds-text-secondary,#4b5563)]">
        {t('p3m.operationalWork')}
      </div>
    </div>
  {/if}
</div>

<style>
  .p3m-guide {
    padding: 0 1.5rem;
    margin-top: 0;
    margin-bottom: 1.25rem;
  }

  .p3m-guide-toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.125rem 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-ds-text-muted, #6b7280);
    background: none;
    border: none;
    cursor: pointer;
  }

  .p3m-guide-toggle:hover {
    color: var(--color-ds-text, #111827);
  }

  .p3m-guide-toggle svg {
    transition: transform 120ms ease;
  }

  .p3m-guide-toggle svg.-rotate-90 {
    transform: rotate(-90deg);
  }
</style>
