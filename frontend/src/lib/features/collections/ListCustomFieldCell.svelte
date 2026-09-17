<script>
  import CustomFieldRenderer from '../items/CustomFieldRenderer.svelte';
  import { collectionEditorOptions } from '../../stores/collectionEditorOptions.svelte.js';

  let {
    field,
    value = null,
    canEdit = false,
    milestones = [],
    iterations = [],
    users = [],
    editorOptions = null,
    workspaceId = null,
    itemId = null,
    fieldLinks = [],
    onFieldLinksChanged = null,
    onChange = (_value) => {}
  } = $props();

  // Never use page-level editable options for a row in a mixed-workspace
  // collection. The cache is keyed by the owning workspace and each family is
  // loaded only when its picker first opens.
  const editorUsers = $derived(
    editorOptions?.loaded?.users ? editorOptions.users : (users?.length ? users : null)
  );
</script>

<CustomFieldRenderer
  {field}
  {value}
  readonly={true}
  disabled={!canEdit}
  selfEditing={canEdit}
  {milestones}
  {iterations}
  users={editorUsers}
  optionData={editorOptions ?? {}}
  optionLoading={editorOptions?.loading ?? {}}
  onRequestOptions={(family) => editorOptions && collectionEditorOptions.load(workspaceId, family)}
  loadAssetOptions={(assetSetId, cqlQuery, search) => collectionEditorOptions.loadAssets(workspaceId, assetSetId, cqlQuery, search)}
  {itemId}
  displayTestId={`list-custom-field-${field.id}-${itemId}`}
  {fieldLinks}
  {onFieldLinksChanged}
  {onChange}
/>
