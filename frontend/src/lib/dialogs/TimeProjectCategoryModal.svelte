<script>
  import Modal from './Modal.svelte';
  import Button from '../components/Button.svelte';
  import { t } from '../stores/i18n.svelte.js';
	import TextareaField from '../components/TextareaField.svelte';
	import TextField from '../components/TextField.svelte';

  // Props
  let {
    isOpen = false,
    formData = $bindable({ name: '', description: '' }),
    isEditing = false,
    onsave = () => {},
    oncancel = () => {}
  } = $props();

  function handleSubmit() {
    if (formData.name.trim()) {
      onsave();
    }
  }

  function handleCancel() {
    oncancel();
  }
</script>

{#if isOpen}
  <Modal
    {isOpen}
    onSubmit={handleSubmit}
    submitDisabled={!formData.name.trim()}
    maxWidth="max-w-lg"
    onclose={handleCancel}
  >
    {#snippet children(submitHint)}
    <div class="p-6">
      <h3 class="text-xl font-semibold mb-6" style="color: var(--ds-text);">
        {isEditing ? t('timeProjectCategory.editCategory') : t('timeProjectCategory.newCategory')}
      </h3>

      <div class="space-y-4">
        <div>
          <TextField
            label={t('timeProjectCategory.categoryName')}
            required
            placeholder={t('timeProjectCategory.categoryNamePlaceholder')}
            bind:value={formData.name}
          />
        </div>

        <div>
          <TextareaField
            label={t('common.description')}
            rows={3}
            placeholder={t('timeProjectCategory.optionalDescription')}
            bind:value={formData.description}
          />
        </div>
      </div>

      <div class="mt-6 flex gap-3">
        <Button
          variant="primary"
          onclick={handleSubmit}
          disabled={!formData.name.trim()}
          size="medium"
          keyboardHint={submitHint}
        >
          {isEditing ? t('timeProjectCategory.updateCategory') : t('timeProjectCategory.createCategory')}
        </Button>
        <Button
          variant="default"
          onclick={handleCancel}
          size="medium"
          keyboardHint="Esc"
        >
          {t('common.cancel')}
        </Button>
      </div>
    </div>
    {/snippet}
  </Modal>
{/if}
