<script>
  import { t } from '../../stores/i18n.svelte.js';
  import { channelCategoriesStore } from '../../stores/channelCategories.js';
  import Button from '../../components/Button.svelte';
	import TextareaField from '../../components/TextareaField.svelte';
	import SelectField from '../../components/SelectField.svelte';
	import TextField from '../../components/TextField.svelte';

  let {
    channelFormData = $bindable({
      name: '',
      description: '',
      category_id: null,
    }),
    saving = false,
    onSave = () => {},
    children,
  } = $props();
</script>

<div class="px-16 py-8 max-w-3xl">
  <div class="mb-8">
    <h4 class="text-sm font-semibold mb-4" style="color: var(--ds-text);">{t('channel.basicInformation')}</h4>
    <div class="space-y-4">
      <div class="grid grid-cols-2 gap-4">
        <div>
          <TextField
            label={t('channel.name')}
            labelColor="default"
            placeholder={t('channel.channelName')}
            bind:value={channelFormData.name}
          />
        </div>
        <div>
          <SelectField
            label={t('channel.category')}
            labelColor="default"
            options={[
            { value: null, label: t('channel.noCategory') },
            ...$channelCategoriesStore.map(c => ({ value: c.id, label: c.name })),
            ]}
            bind:value={channelFormData.category_id}
          />
        </div>
      </div>
      <div>
        <TextareaField
          label={t('channel.description')}
          labelColor="default"
          rows={2}
          placeholder={t('channel.briefDescription')}
          bind:value={channelFormData.description}
        />
      </div>
    </div>
  </div>

  {@render children?.()}

  <div class="mt-8 flex justify-end">
    <Button
      onclick={onSave}
      variant="primary"
      disabled={saving}
      dataTestid="channel-save"
    >
      {saving ? t('common.saving') : t('channel.saveChanges')}
    </Button>
  </div>
</div>
