<script>
  import MilkdownEditor from '../../editors/LazyMilkdownEditor.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  let { briefing = '', itemKey = '' } = $props();
</script>

<div class="space-y-3">
  <div class="flex items-center justify-between">
    <span class="text-xs font-medium px-2 py-1 rounded" style="background-color: var(--ds-background-neutral); color: var(--ds-text-subtle);">
      {itemKey}
    </span>
    <CopyButton getText={() => briefing} label="Copy" copiedLabel="Copied" size="sm" title="Copy briefing" />
  </div>

  <div class="prose-sm max-w-none text-sm leading-relaxed" style="color: var(--ds-text);">
    <MilkdownEditor content={briefing} readonly={true} showToolbar={false} compact={true} />
  </div>
</div>

