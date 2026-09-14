<script>
  import Badge from './Badge.svelte';

  /**
   * TabStrip - Underline tab bar for local (non-URL) tab state.
   *
   * Use TabNav when the active tab lives in the URL query, Tabs for a full
   * bordered panel, and TabStrip for a bare inline tab bar.
   *
   * @example
   * <TabStrip
   *   tabs={[
   *     { id: 'managers', label: t('time.permissions.managers'), count: managers.length },
   *     { id: 'members', label: t('time.permissions.members'), count: members.length },
   *   ]}
   *   bind:activeTab
   *   onTabChange={resetAddForm}
   * />
   */
  /**
   * @type {{
   *   tabs?: Array<{
   *     id: string,
   *     label: string,
   *     count?: number,
   *     countVariant?: 'neutral' | 'info' | 'success' | 'warning' | 'danger',
   *     icon?: import('svelte').Component<any, any, any> | null,
   *     testid?: string,
   *   }>,
   *   activeTab?: string,
   *   onTabChange?: ((tab: string) => void) | null,
   *   ariaLabel?: string,
   *   class?: string,
   * }}
   */
  let {
    tabs = [],
    activeTab = $bindable(''),
    onTabChange = null,
    ariaLabel = '',
    class: className = ''
  } = $props();

  function select(id) {
    if (id === activeTab) return;
    activeTab = id;
    onTabChange?.(id);
  }
</script>

<div class="flex border-b {className}" style="border-color: var(--ds-border);" role="tablist" aria-label={ariaLabel}>
  {#each tabs as tab (tab.id)}
    <button
      type="button"
      role="tab"
      data-testid={tab.testid}
      aria-selected={activeTab === tab.id}
      class="px-4 py-2 text-sm font-medium flex items-center gap-2 border-b-2 -mb-px transition-colors whitespace-nowrap"
      class:border-transparent={activeTab !== tab.id}
      style="{activeTab === tab.id
        ? 'border-color: var(--ds-interactive); color: var(--ds-interactive);'
        : 'color: var(--ds-text-subtle);'}"
      onclick={() => select(tab.id)}
    >
      {#if tab.icon}
        {@const Icon = tab.icon}
        <Icon size={16} />
      {/if}
      {tab.label}
      {#if tab.count}
        <Badge size="xs" variant={tab.countVariant || 'neutral'}>{tab.count}</Badge>
      {/if}
    </button>
  {/each}
</div>
