<script>
  import { Trash2 } from '@lucide/svelte';
  import { homepageStore } from '../stores';
  import { t } from '../stores/i18n.svelte.js';
  import Button from '../components/Button.svelte';
  import GradientPicker from '../components/GradientPicker.svelte';
  import BackgroundPresetGrid from '../components/BackgroundPresetGrid.svelte';
  import Label from '../components/Label.svelte';
</script>

<div class="space-y-6">
  <div>
    <h3 class="text-sm font-medium mb-1" style="color: var(--ds-text);">{t('lookAndFeel.gradientTitle')}</h3>
    <p class="text-xs mb-4" style="color: var(--ds-text-subtle);">{t('lookAndFeel.gradientDescription')}</p>
    <GradientPicker
      selectedGradient={homepageStore.gradient}
      hasBackgroundImage={homepageStore.hasBackgroundImage}
      onSelect={(index) => homepageStore.selectGradient(index)}
      testIdPrefix="dashboard-gradient-swatch"
    />
  </div>

  <div class="border-t pt-6" style="border-color: var(--ds-border);">
    <Label class="mb-4">{t('lookAndFeel.backgroundImages')}</Label>

    {#if homepageStore.hasBackgroundImage}
      <div
        class="mb-4 p-3 rounded-lg border flex items-center gap-3"
        style="border-color: var(--ds-border-focused); background-color: var(--ds-surface);"
      >
        <div class="w-16 h-10 rounded overflow-hidden flex-shrink-0">
          <img
            src={homepageStore.backgroundImageUrl}
            alt={t('lookAndFeel.currentBackground')}
            class="w-full h-full object-cover"
          />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-xs font-medium" style="color: var(--ds-text);">
            {t('lookAndFeel.currentBackground')}
          </div>
        </div>
        <Button
          variant="default"
          size="sm"
          onclick={() => homepageStore.removeBackgroundImage()}
          icon={Trash2}
          dataTestid="dashboard-background-remove"
        >
          {t('workspaceSettings.remove')}
        </Button>
      </div>
    {/if}

    <BackgroundPresetGrid
      currentImageUrl={homepageStore.backgroundImageUrl}
      onSelectImage={(url) => homepageStore.selectBackgroundImage(url)}
      presetAspectRatio="16 / 9"
      testIdPrefix="dashboard-background-preset"
    />
  </div>
</div>
