<script>
  import Badge from '../../components/Badge.svelte';
  import { t } from '../../stores/i18n.svelte.js';

  /**
   * Read-only rendering of one BDD scenario: feature context, background and
   * scenario steps, and the Examples tables of a Scenario Outline. Pure
   * display — data comes from a parsed gherkin.ScenarioSpec.
   *
   * @type {{
   *   spec?: object | null,
   *   raw?: string,
   *   dataTestid?: string,
   * }}
   */
  let { spec = null, raw = '', dataTestid = 'bdd-scenario-view' } = $props();

  let showRaw = $state(false);

  const featureName = $derived(spec?.feature_name || '');
  const featureTags = $derived(Array.isArray(spec?.feature_tags) ? spec.feature_tags : []);
  const background = $derived(Array.isArray(spec?.background) ? spec.background : []);
  const steps = $derived(Array.isArray(spec?.steps) ? spec.steps : []);
  const examples = $derived(Array.isArray(spec?.examples) ? spec.examples : []);
  const scenarioKeyword = $derived(spec?.scenario_keyword || 'Scenario');
  const scenarioName = $derived(spec?.scenario_name || '');
  const scenarioTags = $derived(Array.isArray(spec?.scenario_tags) ? spec.scenario_tags : []);

  // A step like "When the <user> logs in" renders its placeholders in mono so
  // readers can see what varies per example row.
  function stepParts(text) {
    return String(text || '')
      .split(/(<[^<>]+>)/)
      .filter(Boolean)
      .map((part) => ({ placeholder: part.startsWith('<') && part.endsWith('>'), part }));
  }
</script>

<div class="space-y-4" data-testid={dataTestid}>
  {#if showRaw}
    <pre
      class="text-xs leading-5 font-mono p-4 rounded-lg overflow-x-auto whitespace-pre"
      style="background-color: var(--ds-surface-raised); color: var(--ds-text); border: 1px solid var(--ds-border);">{raw || ''}</pre>
  {:else}
    {#if featureName}
      <div class="flex items-center gap-2 flex-wrap">
        <span class="text-sm font-semibold" style="color: var(--ds-text-subtle);">
          {t('testing.feature')}: {featureName}
        </span>
        {#each featureTags as tag (tag)}
          <Badge size="sm">{tag}</Badge>
        {/each}
      </div>
    {/if}

    {#if background.length > 0}
      <div class="rounded-lg p-3" style="background-color: var(--ds-background-neutral);">
        <p class="text-xs font-semibold uppercase tracking-wider mb-2" style="color: var(--ds-text-subtle);">
          {t('testing.background')}
        </p>
        {#each background as step, i (i)}
          <p class="text-sm" style="color: var(--ds-text-subtle);">
            <span class="font-semibold">{step.keyword || ''}</span>
            {step.text || ''}
          </p>
        {/each}
      </div>
    {/if}

    <div class="flex items-center gap-2 flex-wrap">
      <h3 class="text-base font-semibold" style="color: var(--ds-text);">
        {scenarioKeyword}: {scenarioName}
      </h3>
      {#each scenarioTags as tag (tag)}
        <Badge size="sm">{tag}</Badge>
      {/each}
    </div>

    <ol class="space-y-2" data-testid="bdd-steps">
      {#each steps as step, i (i)}
        <li class="flex items-start gap-2 text-sm">
          <span class="font-semibold min-w-14" style="color: var(--ds-interactive);">
            {step.keyword || ''}
          </span>
          <span style="color: var(--ds-text);">
            {#each stepParts(step.text) as { placeholder, part } (part)}
              {#if placeholder}<code
                  class="px-1 rounded font-mono text-xs"
                  style="background-color: var(--ds-background-neutral); color: var(--ds-text);">{part}</code
                >{:else}{part}{/if}
            {/each}
          </span>
        </li>
      {/each}
    </ol>

    {#each examples as block, blockIndex (blockIndex)}
      <div data-testid="bdd-examples-block">
        <p class="text-xs font-semibold uppercase tracking-wider mb-1" style="color: var(--ds-text-subtle);">
          {t('testing.examples')}{block.name ? `: ${block.name}` : ''}
        </p>
        <div class="overflow-x-auto">
          <table class="text-sm border-collapse">
            <thead>
              <tr>
                {#each block.header as column (column)}
                  <th
                    class="px-3 py-1.5 text-left font-semibold border"
                    style="color: var(--ds-text-subtle); border-color: var(--ds-border);">{column}</th
                  >
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each block.rows as row, rowIndex (rowIndex)}
                <tr>
                  {#each row as cell, cellIndex (cellIndex)}
                    <td
                      class="px-3 py-1.5 border font-mono text-xs"
                      style="color: var(--ds-text); border-color: var(--ds-border);">{cell}</td
                    >
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/each}
  {/if}

  {#if raw}
    <button
      type="button"
      class="text-xs underline"
      style="color: var(--ds-text-subtle);"
      data-testid="bdd-toggle-raw"
      onclick={() => (showRaw = !showRaw)}
    >
      {showRaw ? t('testing.showRendered') : t('testing.showSource')}
    </button>
  {/if}
</div>
