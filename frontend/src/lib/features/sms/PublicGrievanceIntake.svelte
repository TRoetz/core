<script>
  import { fetchAPI } from '../../api/core.js';

  const SEVERITIES = [
    { value: 'low', label: 'Low' },
    { value: 'medium', label: 'Medium' },
    { value: 'high', label: 'High' },
    { value: 'critical', label: 'Critical' },
  ];

  let form = $state({ name: '', email: '', organization: '', severity: 'medium', description: '' });
  let submitting = $state(false);
  let error = $state('');
  let result = $state(null); // { grievance_id, stakeholder_id, matched_stakeholder }

  const inputClass =
    'mt-1 block w-full rounded-md border px-3 py-2 text-sm focus:outline-none';
  const labelClass = 'block text-xs font-medium';

  function fieldError(field) {
    if (field === 'name' && !form.name.trim()) return 'Name is required.';
    if (field === 'description' && form.description.trim().length < 10)
      return 'Please describe the issue in at least 10 characters.';
    return '';
  }

  async function handleSubmit(event) {
    event.preventDefault();
    error = '';
    if (!form.name.trim() || form.description.trim().length < 10) return;
    submitting = true;
    try {
      const res = await fetchAPI('/sms/grievances/intake', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name.trim(),
          email: form.email.trim(),
          organization: form.organization.trim(),
          severity: form.severity,
          description: form.description.trim(),
        }),
      });
      result = res;
    } catch (e) {
      error = e?.message || 'Something went wrong. Please try again.';
    } finally {
      submitting = false;
    }
  }

  function resetForm() {
    form = { name: '', email: '', organization: '', severity: 'medium', description: '' };
    error = '';
    result = null;
  }
</script>

<div class="min-h-screen flex flex-col" style="background-color: var(--ds-surface);">
  <main class="flex-1">
    <div class="mx-auto max-w-xl px-4 py-10 sm:py-16">
      <h1 class="text-2xl font-semibold leading-8 sm:text-3xl" style="color: var(--ds-text);">
        Stakeholder grievance intake
      </h1>
      <p class="mt-2 text-sm leading-5" style="color: var(--ds-text-subtle);">
        Use this form to raise a concern or complaint about our work. Submissions are logged in the
        stakeholder relationship register and acknowledged by the responsible team.
      </p>

      {#if result}
        <div class="mt-8 rounded-lg border p-6" style="border-color: var(--ds-border);">
          <h2 class="text-xl font-medium" style="color: var(--ds-text);">Thank you — received</h2>
          <p class="mt-2 text-sm leading-5" style="color: var(--ds-text-subtle);">
            Your grievance has been recorded (reference #{result.grievance_id}). The responsible team
            will acknowledge it and respond within the agreed service window.
          </p>
          <button
            type="button"
            class="mt-4 rounded-md border px-3 py-1.5 text-xs hover:opacity-80"
            style="border-color: var(--ds-border); color: var(--ds-text);"
            onclick={resetForm}
          >
            Submit another grievance
          </button>
        </div>
      {:else}
        <form class="mt-8 space-y-4 rounded-lg border p-5 sm:p-6" style="border-color: var(--ds-border);" onsubmit={handleSubmit}>
          {#if error}
            <div class="rounded-md border px-3 py-2 text-xs" style="border-color: #fecaca; background-color: #fef2f2; color: #b91c1c;">
              {error}
            </div>
          {/if}

          <label class={labelClass} style="color: var(--ds-text);">
            Your name *
            <input
              type="text"
              class={inputClass}
              style="border-color: var(--ds-border);"
              bind:value={form.name}
              maxlength="200"
            />
          </label>

          <div class="grid gap-4 sm:grid-cols-2">
            <label class={labelClass} style="color: var(--ds-text);">
              Email address
              <input
                type="email"
                class={inputClass}
                style="border-color: var(--ds-border);"
                bind:value={form.email}
                maxlength="254"
              />
            </label>
            <label class={labelClass} style="color: var(--ds-text);">
              Organization
              <input
                type="text"
                class={inputClass}
                style="border-color: var(--ds-border);"
                bind:value={form.organization}
                maxlength="200"
              />
            </label>
          </div>

          <label class={labelClass} style="color: var(--ds-text);">
            Severity
            <select class={inputClass} style="border-color: var(--ds-border);" bind:value={form.severity}>
              {#each SEVERITIES as s (s.value)}
                <option value={s.value}>{s.label}</option>
              {/each}
            </select>
          </label>

          <label class={labelClass} style="color: var(--ds-text);">
            What is the issue? *
            <textarea
              class="{inputClass} min-h-32 resize-y"
              style="border-color: var(--ds-border);"
              bind:value={form.description}
              maxlength="5000"
              placeholder="Describe what happened, when, and how it affected you."
            ></textarea>
          </label>

          <button
            type="submit"
            class="rounded-md px-4 py-2 text-sm font-medium text-white disabled:opacity-60"
            style="background-color: var(--ds-accent);"
            disabled={submitting}
          >
            {submitting ? 'Submitting…' : 'Submit grievance'}
          </button>
        </form>
      {/if}
    </div>
  </main>
</div>
