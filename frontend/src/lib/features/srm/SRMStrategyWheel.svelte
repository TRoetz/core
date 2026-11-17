<script>
  import { t } from '../../stores/i18n.svelte.js';

  /** @type {{ phase: string, labelKey: string, class: string }[]} */
  const QUADRANTS = [
    // Clockwise from top-right, matching the official wheel layout.
    { phase: 'preparation', labelKey: 'srm.phasePreparation', class: 'top-0 right-0' },
    { phase: 'planning', labelKey: 'srm.phasePlanning', class: 'bottom-0 right-0' },
    { phase: 'implementation', labelKey: 'srm.phaseImplementation', class: 'bottom-0 left-0' },
    { phase: 'monitoring', labelKey: 'srm.phaseMonitoring', class: 'top-0 left-0' },
  ];

  let { onNavigate } = $props();
</script>

<figure class="mx-auto w-full max-w-[340px]">
  <div class="relative mx-auto aspect-square w-full">
    <img
      src="/images/srm-strategy-wheel.png"
      alt={t('srm.strategyWheel')}
      draggable="false"
      class="pointer-events-none h-full w-full select-none rounded-full"
    />
    {#each QUADRANTS as q (q.phase)}
      <button
        type="button"
        aria-label={t(q.labelKey)}
        title={t(q.labelKey)}
        class="absolute h-1/2 w-1/2 cursor-pointer rounded-full transition-colors hover:bg-white/15 focus-visible:bg-white/15 {q.class}"
        onclick={() => onNavigate?.(q.phase)}
      ></button>
    {/each}
    <!-- Absorbs clicks on the central hub so they never trigger a quadrant. -->
    <div class="pointer-events-auto absolute left-1/2 top-1/2 h-[38%] w-[38%] -translate-x-1/2 -translate-y-1/2 rounded-full"></div>
  </div>
  <figcaption class="mt-2 text-center text-xs text-[var(--color-ds-text-muted,#6b7280)]">
    {t('srm.wheelHint')}
  </figcaption>
</figure>
