<script>
  import Chart from '../../widgets/Chart.svelte';
  import { getSource, getWidgetDataContext } from '../context.js';
  import { itemSpec, assetSpec, sourceKey } from '../querySpec.js';
  import { groupCount, groupSum } from '../aggregate.js';
  import SourceBoundary from './SourceBoundary.svelte';

  /**
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

  $effect(() => {
    if (source) return;
    context?.requestSource?.(spec, sourceKey(spec));
  });

  const buckets = $derived.by(() => {
    const rows = source?.rows ?? [];
    if (rows.length === 0) return [];
    const options = { limit: props.group_limit ?? 10 };
    return (props.metric ?? 'count') === 'sum'
      ? groupSum(rows, props.group_by, props.field, options)
      : groupCount(rows, props.group_by, options);
  });

  const categories = $derived(buckets.map((bucket) => bucket.label));
  const series = $derived([
    {
      key: 'value',
      label: props.metric === 'sum' && props.field ? props.field : 'Count',
      color: '#6366f1',
      values: buckets.map((bucket) => bucket.value),
    },
  ]);
</script>

<SourceBoundary {source}>
  {#snippet ready()}
    <Chart type="bar" {series} {categories} minHeight={160} showYAxis={false} />
  {/snippet}
</SourceBoundary>
