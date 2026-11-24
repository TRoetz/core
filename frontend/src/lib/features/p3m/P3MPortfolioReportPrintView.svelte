<script>
  import { onMount } from 'svelte';
  import { p3m as p3mApi } from '../../api/p3m.js';
  import { navigate } from '../../router.js';
  import { t } from '../../stores/i18n.svelte.js';
  import MarkdownPrintView from '../print/MarkdownPrintView.svelte';

  let { portfolioId, asOf } = $props();

  let content = $state('');
  let loading = $state(true);
  let error = $state('');

  onMount(async () => {
    try {
      content = await p3mApi.fetchPortfolioReportMarkdown(portfolioId, {
        asOf: asOf || undefined,
      });
    } catch (err) {
      console.error('Failed to load portfolio report:', err);
      error = t('p3m.report.loadError');
    } finally {
      loading = false;
    }
  });
</script>

<MarkdownPrintView
  {content}
  title={t('p3m.report.title')}
  {loading}
  {error}
  loadingLabel={t('p3m.report.printLoading')}
  backLabel={t('p3m.report.backToReport')}
  onback={() => navigate('/p3m')}
  testId="p3m-report-print"
/>
