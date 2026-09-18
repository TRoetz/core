// Custom field values are stored as one JSON object per item, and the update
// endpoint replaces the whole object. Callers that merge against the row prop
// they rendered with lose edits made during an in-flight request (a second
// cell edited quickly, another component editing the same item). This module
// keeps the freshest issued values per item so every update merges on top of
// everything already in flight, and adopts the server's authoritative blob
// only when no newer request has been issued in the meantime.

const latestValuesByItem = new Map();
const latestSeqByItem = new Map();

/**
 * Persist one custom field value, merging against the freshest issued values
 * for the item. Resolves with the updated item from the server.
 */
export async function updateCustomFieldValue(api, itemId, fieldIdentifier, value) {
  const merged = { ...(latestValuesByItem.get(itemId) ?? {}), [fieldIdentifier]: value };
  latestValuesByItem.set(itemId, merged);
  const seq = (latestSeqByItem.get(itemId) ?? 0) + 1;
  latestSeqByItem.set(itemId, seq);
  try {
    const updatedItem = await api.items.update(itemId, { custom_field_values: merged });
    if (latestSeqByItem.get(itemId) === seq) {
      latestValuesByItem.set(itemId, updatedItem?.custom_field_values ?? merged);
    }
    return updatedItem;
  } catch (error) {
    // Only roll the overlay back when this failed blob is still the newest
    // one; a later edit already merged on top of it and owns cleanup.
    if (latestSeqByItem.get(itemId) === seq) {
      latestValuesByItem.delete(itemId);
      latestSeqByItem.delete(itemId);
    }
    throw error;
  }
}

/** Test hook: forget all tracked values. */
export function resetCustomFieldValueTracking() {
  latestValuesByItem.clear();
  latestSeqByItem.clear();
}
