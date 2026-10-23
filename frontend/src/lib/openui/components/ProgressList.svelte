<script>
  import { getSource, getWidgetDataContext } from '../context.js';
  import { itemSpec, assetSpec, sourceKey } from '../querySpec.js';
  import { groupCount, groupSum } from '../aggregate.js';
  import SourceBoundary from './SourceBoundary.svelte';

  /**
   * Share-of-total bars: one row per distinct value of `group_by`.
   *
   * @type {{
   *   props: {
   *     query?: string,
   *     sort?: string,
   *     limit?: number,
   *     group_by: string,
   *     metric?: 'count'|'sum',
   *     field?: string,
   *     group_limit?: number,
   *     entity?: 'item'|'asset'
   *   }
   * }}
   */
  let { props } = $props();

  const context = getWidgetDataContext();
  const entity = $derived(props.entity === 'asset' ? 'asset' : 'item');
  const spec = $derived(
    entity === 'asset'
      ? assetSpec({
          query: props.query,
          sort: props.sort,
          limit: props.limit,
          assetSetId: context?.assetSetId,
        })
      : itemSpec({ query: props.query, sort: props.sort, limit: props.limit })
  );
  const source = $derived(getSource(sourceKey(spec)));

  const buckets = $derived.by(() => {
    const rows = source?.rows ?? [];
    if (rows.length === 0) return [];
    const options = { limit: props.group_limit ?? 8 };
    return (props.metric ?? 'count') === 'sum'
      ? groupSum(rows, props.group_by, props.field, options)
      : groupCount(rows, props.group_by, options);
  });

  const total = $derived(buckets.reduce((sum, bucket) => sum + bucket.value, 0));
</script>

<SourceBoundary {source}>
  {#snippet ready()}
    <ul class="space-y-2" data-testid="openui-progress-list">
      {#each buckets as bucket (bucket.label)}
        <li>
          <div class="flex items-center justify-between text-xs" style="color: var(--ds-text-subtle);">
            <span class="truncate">{bucket.label}</span>
            <span>{bucket.value}</span>
          </div>
          <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full" style="background-color: var(--ds-surface-raised);">
            <div
              class="h-full rounded-full"
              style="width: {total > 0 ? Math.round((bucket.value / total) * 100) : 0}%; background-color: var(--ds-accent, #6366f1);"
            ></div>
          </div>
        </li>
      {/each}
    </ul>
  {/snippet}
</SourceBoundary>
