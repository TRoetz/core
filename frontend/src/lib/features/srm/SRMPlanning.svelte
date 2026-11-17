<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import { srm } from '../../api/srm.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { formatMoney, formatDate } from './utils.js';

  let suppliers = $state([]);
  let contracts = $state([]);
  let stakeholders = $state([]);
  let valueProps = $state([]);
  let bindings = $state([]);
  let jbps = $state([]);
  let meetings = $state([]);
  let loading = $state(true);
  let error = $state('');
  let actionError = $state('');

  const supplierName = (id) => suppliers.find((s) => s.id === id)?.company_name ?? `#${id}`;
  const contractLabel = (c) => [c.contract_number, c.title].filter(Boolean).join(' — ') || `#${c.id}`;
  const contractsFor = (sid) => contracts.filter((c) => String(c.supplier_id) === String(sid));
  const contractById = (id) => contracts.find((c) => c.id === id);

  async function load() {
    loading = true;
    error = '';
    try {
      const [sup, con, stk, vps, bds, jbs] = await Promise.all([
        srm.suppliers.getAll(),
        srm.contracts.getAll(),
        srm.stakeholders.getAll(),
        srm.valuePropositions.getAll(),
        srm.entityStakeholders.getAll(),
        srm.jbps.getAll(),
      ]);
      suppliers = sup ?? [];
      contracts = con ?? [];
      stakeholders = stk ?? [];
      valueProps = vps ?? [];
      bindings = bds ?? [];
      jbps = jbs ?? [];
      await loadMeetings();
    } catch (e) {
      error = e?.message || 'error';
    } finally {
      loading = false;
    }
  }

  async function loadMeetings() {
    const rows = await srm.meetings.getAll(meetingFilter ? { supplier_id: meetingFilter } : {});
    meetings = rows ?? [];
  }

  onMount(load);

  // ---------- Value propositions ----------
  let vpOpen = $state(false);
  let vpEditingId = $state(null);
  const emptyVpForm = () => ({ supplier_id: '', contract_id: '', agency_objective: '', benefit_type: 'financial_saving', anticipated_benefit: '', estimated_value_nzd: '', value_confidence: 'medium' });
  let vpForm = $state(emptyVpForm());

  function openVpCreate() { actionError = ''; vpEditingId = null; vpForm = emptyVpForm(); vpOpen = true; }
  function openVpEdit(vp) {
    actionError = '';
    vpEditingId = vp.id;
    vpForm = { supplier_id: String(vp.supplier_id), contract_id: vp.contract_id ? String(vp.contract_id) : '', agency_objective: vp.agency_objective || '', benefit_type: vp.benefit_type || 'financial_saving', anticipated_benefit: vp.anticipated_benefit || '', estimated_value_nzd: vp.estimated_value_nzd != null ? String(vp.estimated_value_nzd) : '', value_confidence: vp.value_confidence || 'medium' };
    vpOpen = true;
  }

  async function saveVp(e) {
    e.preventDefault();
    actionError = '';
    const payload = { supplier_id: Number(vpForm.supplier_id), agency_objective: vpForm.agency_objective, benefit_type: vpForm.benefit_type, anticipated_benefit: vpForm.anticipated_benefit };
    if (vpForm.contract_id) payload.contract_id = Number(vpForm.contract_id);
    if (vpForm.value_confidence) payload.value_confidence = vpForm.value_confidence;
    if (vpForm.estimated_value_nzd !== '') payload.estimated_value_nzd = Number(vpForm.estimated_value_nzd);
    try {
      if (vpEditingId == null) await srm.valuePropositions.create(payload);
      else await srm.valuePropositions.update(vpEditingId, payload);
      vpOpen = false;
      valueProps = (await srm.valuePropositions.getAll()) ?? [];
    } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteVp(id) {
    actionError = '';
    try { await srm.valuePropositions.delete(id); valueProps = (await srm.valuePropositions.getAll()) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- Stakeholders ----------
  let stOpen = $state(false);
  let stEditingId = $state(null);
  const emptyStForm = () => ({ full_name: '', email: '', organization: '', job_title: '', business_area: '', is_external: false, supplier_id: '' });
  let stForm = $state(emptyStForm());

  function openStCreate() { actionError = ''; stEditingId = null; stForm = emptyStForm(); stOpen = true; }
  function openStEdit(st) {
    actionError = '';
    stEditingId = st.id;
    stForm = { full_name: st.full_name || '', email: st.email || '', organization: st.organization || '', job_title: st.job_title || '', business_area: st.business_area || '', is_external: !!st.is_external, supplier_id: st.supplier_id ? String(st.supplier_id) : '' };
    stOpen = true;
  }

  async function saveSt(e) {
    e.preventDefault();
    actionError = '';
    const payload = { full_name: stForm.full_name, email: stForm.email, organization: stForm.organization, job_title: stForm.job_title, business_area: stForm.business_area, is_external: stForm.is_external };
    if (stForm.supplier_id) payload.supplier_id = Number(stForm.supplier_id);
    try {
      if (stEditingId == null) await srm.stakeholders.create(payload);
      else await srm.stakeholders.update(stEditingId, payload);
      stOpen = false;
      stakeholders = (await srm.stakeholders.getAll()) ?? [];
    } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteSt(id) {
    actionError = '';
    try { await srm.stakeholders.delete(id); stakeholders = (await srm.stakeholders.getAll()) ?? []; bindings = (await srm.entityStakeholders.getAll()) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- Entity stakeholder bindings ----------
  const emptyBindForm = () => ({ stakeholder_id: '', entity_type: 'contract', entity_id: '', role_description: '', power_score: 3, interest_score: 3, support_band: 'on_the_fence', current_engagement: 'neutral', target_engagement: 'supportive' });
  let bindForm = $state(emptyBindForm());

  async function addBinding(e) {
    e.preventDefault();
    actionError = '';
    try {
      await srm.entityStakeholders.create({ stakeholder_id: Number(bindForm.stakeholder_id), entity_type: bindForm.entity_type, entity_id: Number(bindForm.entity_id), role_description: bindForm.role_description, power_score: Number(bindForm.power_score), interest_score: Number(bindForm.interest_score), support_band: bindForm.support_band, current_engagement: bindForm.current_engagement, target_engagement: bindForm.target_engagement });
      bindings = (await srm.entityStakeholders.getAll()) ?? [];
      bindForm = emptyBindForm();
    } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteBinding(id) {
    actionError = '';
    try { await srm.entityStakeholders.delete(id); bindings = (await srm.entityStakeholders.getAll()) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- Communications plans ----------
  let commsSupplierId = $state('');
  let commsPlan = $state(null);
  let commsMessages = $state([]);
  let commsScope = $state('all');
  let commsComplete = $state(false);
  const emptyMsgForm = () => ({ stakeholder_id: '', communication_message: '', method: '', frequency: '' });
  let msgForm = $state(emptyMsgForm());

  async function loadComms() {
    actionError = '';
    if (!commsSupplierId) return;
    try {
      commsPlan = await srm.getCommsPlan(Number(commsSupplierId));
      commsScope = commsPlan.scope || 'all';
      commsComplete = !!commsPlan.is_complete;
      commsMessages = (await srm.getCommsMessages(commsPlan.id)) ?? [];
    } catch (e) {
      if (e?.status === 404) { commsPlan = null; commsMessages = []; return; }
      actionError = e?.message || 'error';
    }
  }

  async function saveCommsPlan() {
    actionError = '';
    try {
      commsPlan = await srm.upsertCommsPlan(Number(commsSupplierId), { scope: commsScope, is_complete: commsComplete });
      commsMessages = (await srm.getCommsMessages(commsPlan.id)) ?? [];
    } catch (err) { actionError = err?.message || 'error'; }
  }

  async function addMsg(e) {
    e.preventDefault();
    actionError = '';
    try {
      await srm.addCommsMessage(commsPlan.id, { stakeholder_id: Number(msgForm.stakeholder_id), communication_message: msgForm.communication_message, method: msgForm.method, frequency: msgForm.frequency });
      commsMessages = (await srm.getCommsMessages(commsPlan.id)) ?? [];
      msgForm = emptyMsgForm();
    } catch (err) { actionError = err?.message || 'error'; }
  }

  async function signOffMsg(id) {
    actionError = '';
    try { await srm.updateCommsMessage(id, { sign_off: true }); commsMessages = (await srm.getCommsMessages(commsPlan.id)) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteMsg(id) {
    actionError = '';
    try { await srm.deleteCommsMessage(id); commsMessages = (await srm.getCommsMessages(commsPlan.id)) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- RACI matrices ----------
  let raciSupplierId = $state('');
  let raci = $state(null);
  const emptyRaciForm = () => ({ activity_code: '', side: 'agency', role_name: '', user_id: '', value: 'r' });
  let raciForm = $state(emptyRaciForm());

  async function loadRaci() {
    actionError = '';
    if (!raciSupplierId) return;
    try {
      raci = await srm.getRaciMatrix(Number(raciSupplierId));
    } catch (e) {
      if (e?.status === 404) { raci = null; return; }
      actionError = e?.message || 'error';
    }
  }

  async function createRaciVersion() {
    actionError = '';
    try { await srm.createRaciVersion(Number(raciSupplierId), {}); await loadRaci(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function setRaciStatus(status) {
    actionError = '';
    try { raci = await srm.updateRaciStatus(raci.id, { status }); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function addRaciAssignment(e) {
    e.preventDefault();
    actionError = '';
    const payload = { activity_code: raciForm.activity_code, side: raciForm.side, role_name: raciForm.role_name, value: raciForm.value };
    if (raciForm.user_id !== '') payload.user_id = Number(raciForm.user_id);
    try { await srm.upsertRaciAssignment(raci.id, payload); await loadRaci(); raciForm = emptyRaciForm(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteRaciAssignment(id) {
    actionError = '';
    try { await srm.deleteRaciAssignment(id); await loadRaci(); } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- Meetings ----------
  let meetingFilter = $state('');
  let mOpen = $state(false);
  const emptyMForm = () => ({ supplier_id: '', contract_id: '', meeting_type: 'qbr', scheduled_at: '', location: '' });
  let mForm = $state(emptyMForm());

  async function saveMeeting(e) {
    e.preventDefault();
    actionError = '';
    const payload = { meeting_type: mForm.meeting_type, supplier_id: Number(mForm.supplier_id), scheduled_at: new Date(mForm.scheduled_at).toISOString(), location: mForm.location };
    if (mForm.contract_id) payload.contract_id = Number(mForm.contract_id);
    try { await srm.meetings.create(payload); mOpen = false; mForm = emptyMForm(); await loadMeetings(); } catch (err) { actionError = err?.message || 'error'; }
  }

  let activeMeetingId = $state(null);
  const activeMeeting = $derived(meetings.find((m) => m.id === activeMeetingId) ?? null);

  async function deleteMeeting(id) {
    actionError = '';
    try { await srm.meetings.delete(id); if (activeMeetingId === id) activeMeetingId = null; await loadMeetings(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function setMeetingStatus(m, status) {
    actionError = '';
    try { const updated = await srm.meetings.update(m.id, { status }); meetings = meetings.map((x) => (x.id === m.id ? updated : x)); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function refreshActiveMeeting() {
    if (!activeMeetingId) return;
    const m = await srm.meetings.get(activeMeetingId);
    meetings = meetings.map((x) => (x.id === m.id ? m : x));
  }

  const emptyAttForm = () => ({ side: 'agency', stakeholder_id: '', user_id: '', role_description: '' });
  let attForm = $state(emptyAttForm());

  async function addAttendee(e) {
    e.preventDefault();
    actionError = '';
    if (!attForm.stakeholder_id && !attForm.user_id) { actionError = t('srm.attendeeNeedsPerson'); return; }
    const payload = { side: attForm.side, role_description: attForm.role_description };
    if (attForm.stakeholder_id) payload.stakeholder_id = Number(attForm.stakeholder_id);
    else payload.user_id = Number(attForm.user_id);
    try { await srm.addMeetingAttendee(activeMeetingId, payload); attForm = emptyAttForm(); await refreshActiveMeeting(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function removeAttendee(id) {
    actionError = '';
    try { await srm.removeMeetingAttendee(id); await refreshActiveMeeting(); } catch (err) { actionError = err?.message || 'error'; }
  }

  const emptyActForm = () => ({ kind: 'action', text: '', due_date: '' });
  let actForm = $state(emptyActForm());

  async function addAction(e) {
    e.preventDefault();
    actionError = '';
    const payload = { kind: actForm.kind, text: actForm.text };
    if (actForm.due_date) payload.due_date = new Date(actForm.due_date).toISOString();
    try { await srm.addMeetingAction(activeMeetingId, payload); actForm = emptyActForm(); await refreshActiveMeeting(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function closeAction(id) {
    actionError = '';
    try { await srm.updateMeetingAction(id, { status: 'closed' }); await refreshActiveMeeting(); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function saveMinutes(e) {
    e.preventDefault();
    actionError = '';
    try { const updated = await srm.meetings.update(activeMeetingId, { minutes_text: activeMeeting.minutes_text }); meetings = meetings.map((x) => (x.id === activeMeetingId ? updated : x)); } catch (err) { actionError = err?.message || 'error'; }
  }

  // ---------- Joint business plans ----------
  let jbpOpen = $state(false);
  const emptyJbpForm = () => ({ supplier_id: '', contract_id: '', purpose: '', vision: '', scope: '', governance_model: '', review_cycle_months: 3, relationship_charter: '' });
  let jbpForm = $state(emptyJbpForm());

  async function saveJbp(e) {
    e.preventDefault();
    actionError = '';
    const payload = { contract_id: Number(jbpForm.contract_id), purpose: jbpForm.purpose, vision: jbpForm.vision, scope: jbpForm.scope, governance_model: jbpForm.governance_model, review_cycle_months: Number(jbpForm.review_cycle_months) || 3 };
    const charter = jbpForm.relationship_charter.split(',').map((s) => s.trim()).filter(Boolean);
    if (charter.length) payload.relationship_charter = charter;
    try { await srm.createJbp(Number(jbpForm.supplier_id), payload); jbpOpen = false; jbpForm = emptyJbpForm(); jbps = (await srm.jbps.getAll()) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }

  let jbpEditOpen = $state(false);
  let jbpEditingId = $state(null);
  const emptyJbpEditForm = () => ({ status: 'draft', purpose: '', vision: '', scope: '', governance_model: '', review_cycle_months: 3, relationship_charter: '', agency_rep_name: '', agency_rep_role: '', supplier_rep_name: '', supplier_rep_role: '' });
  let jbpEditForm = $state(emptyJbpEditForm());

  function openJbpEdit(j) {
    actionError = '';
    jbpEditingId = j.id;
    jbpEditForm = { status: j.status || 'draft', purpose: j.purpose || '', vision: j.vision || '', scope: j.scope || '', governance_model: j.governance_model || '', review_cycle_months: j.review_cycle_months || 3, relationship_charter: (j.relationship_charter ?? []).join(', '), agency_rep_name: j.agency_rep_name || '', agency_rep_role: j.agency_rep_role || '', supplier_rep_name: j.supplier_rep_name || '', supplier_rep_role: j.supplier_rep_role || '' };
    jbpEditOpen = true;
  }

  async function saveJbpEdit(e) {
    e.preventDefault();
    actionError = '';
    const payload = { status: jbpEditForm.status, purpose: jbpEditForm.purpose, vision: jbpEditForm.vision, scope: jbpEditForm.scope, governance_model: jbpEditForm.governance_model, review_cycle_months: Number(jbpEditForm.review_cycle_months) || 3, agency_rep_name: jbpEditForm.agency_rep_name, agency_rep_role: jbpEditForm.agency_rep_role, supplier_rep_name: jbpEditForm.supplier_rep_name, supplier_rep_role: jbpEditForm.supplier_rep_role };
    const charter = jbpEditForm.relationship_charter.split(',').map((s) => s.trim()).filter(Boolean);
    payload.relationship_charter = charter;
    try { const updated = await srm.jbps.update(jbpEditingId, payload); jbpEditOpen = false; jbps = jbps.map((x) => (x.id === updated.id ? updated : x)); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function signJbp(j, side) {
    actionError = '';
    try { const updated = await srm.jbps.update(j.id, { [side === 'agency' ? 'agency_signed_at' : 'supplier_signed_at']: new Date().toISOString() }); jbps = jbps.map((x) => (x.id === updated.id ? updated : x)); } catch (err) { actionError = err?.message || 'error'; }
  }

  async function deleteJbp(id) {
    actionError = '';
    try { await srm.jbps.delete(id); if (jbpEditingId === id) jbpEditOpen = false; jbps = (await srm.jbps.getAll()) ?? []; } catch (err) { actionError = err?.message || 'error'; }
  }
</script>

{#if loading}
  <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.loading')}</div>
{:else if error && !valueProps.length && !stakeholders.length && !jbps.length}
  <EmptyState title={t('srm.loadErrorTitle')} description={error} actions={{ label: t('srm.retry'), onClick: load }} />
{:else}
  <div class="space-y-6 px-6 py-4">
    {#if actionError}<div class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{actionError}</div>{/if}

    <!-- Value propositions -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('srm.valuePropositions')}</h2>
        <button type="button" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700" onclick={openVpCreate}>{t('srm.create')}</button>
      </div>
      {#if vpOpen}
        <form class="mb-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={saveVp}>
          <div class="grid gap-2 md:grid-cols-4">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplier')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.supplier_id} required>
              <option value="" disabled>{t('srm.supplier')}</option>
              {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
            </select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.contract')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.contract_id}>
              <option value="">{t('srm.none')}</option>
              {#each contractsFor(vpForm.supplier_id) as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
            </select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.agencyObjective')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.agency_objective} required /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.benefitType')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.benefit_type}>{#each ['financial_saving', 'operational_efficiency', 'innovation_capture', 'risk_compliance_reduction', 'other'] as bt (bt)}<option value={bt}>{t(`srm.bt_${bt}`)}</option>{/each}</select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.anticipatedBenefit')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.anticipated_benefit} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.estimatedValue')}<input type="number" step="any" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.estimated_value_nzd} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.valueConfidence')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={vpForm.value_confidence}>{#each ['high', 'medium', 'low'] as vc (vc)}<option value={vc}>{t(`srm.conf_${vc}`)}</option>{/each}</select></label>
          </div>
          <div class="flex gap-2">
            <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (vpOpen = false)}>{t('srm.cancel')}</button>
          </div>
        </form>
      {/if}
      {#if valueProps.length === 0}<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('srm.supplier')}</th><th class="py-1.5 pr-3">{t('srm.agencyObjective')}</th><th class="py-1.5 pr-3">{t('srm.benefitType')}</th>
            <th class="py-1.5 pr-3">{t('srm.estimatedValue')}</th><th class="py-1.5 pr-3">{t('srm.valueConfidence')}</th><th class="py-1.5 pr-3">{t('srm.actions')}</th>
          </tr></thead>
          <tbody>
            {#each valueProps as vp (vp.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{supplierName(vp.supplier_id)}</td>
                <td class="max-w-xs truncate py-1.5 pr-3" title={vp.agency_objective}>{vp.agency_objective}</td>
                <td class="py-1.5 pr-3">{t(`srm.bt_${vp.benefit_type}`)}</td>
                <td class="py-1.5 pr-3">{vp.estimated_value_nzd != null ? formatMoney(vp.estimated_value_nzd) : '—'}</td>
                <td class="py-1.5 pr-3">{#if vp.value_confidence === 'high'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.conf_high')}</span>{:else if vp.value_confidence === 'low'}<span class="rounded-full bg-red-100 px-2 py-0.5 text-red-700">{t('srm.conf_low')}</span>{:else}<span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('srm.conf_medium')}</span>{/if}</td>
                <td class="py-1.5 pr-3"><button type="button" class="mr-2 cursor-pointer underline hover:text-blue-600" onclick={() => openVpEdit(vp)}>{t('srm.update')}</button><button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteVp(vp.id)}>{t('srm.delete')}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Stakeholders -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('srm.stakeholders')}</h2>
        <button type="button" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700" onclick={openStCreate}>{t('srm.create')}</button>
      </div>
      {#if stOpen}
        <form class="mb-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={saveSt}>
          <div class="grid gap-2 md:grid-cols-4">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.fullName')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.full_name} required /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.email')}<input type="email" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.email} required /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.organization')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.organization} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.jobTitle')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.job_title} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.businessArea')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.business_area} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplier')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={stForm.supplier_id}>
              <option value="">{t('srm.none')}</option>
              {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
            </select></label>
            <label class="flex items-center gap-2 self-end pb-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]"><input type="checkbox" bind:checked={stForm.is_external} /> {t('srm.external')}</label>
          </div>
          <div class="flex gap-2">
            <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (stOpen = false)}>{t('srm.cancel')}</button>
          </div>
        </form>
      {/if}
      {#if stakeholders.length === 0}<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('srm.fullName')}</th><th class="py-1.5 pr-3">{t('srm.email')}</th><th class="py-1.5 pr-3">{t('srm.organization')}</th>
            <th class="py-1.5 pr-3">{t('srm.jobTitle')}</th><th class="py-1.5 pr-3">{t('srm.supplier')}</th><th class="py-1.5 pr-3">{t('srm.actions')}</th>
          </tr></thead>
          <tbody>
            {#each stakeholders as st (st.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{st.full_name}{#if st.is_external}<span class="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t('srm.external')}</span>{/if}</td>
                <td class="py-1.5 pr-3">{st.email}</td>
                <td class="py-1.5 pr-3">{st.organization || '—'}</td>
                <td class="py-1.5 pr-3">{st.job_title || '—'}</td>
                <td class="py-1.5 pr-3">{st.supplier_id ? supplierName(st.supplier_id) : '—'}</td>
                <td class="py-1.5 pr-3"><button type="button" class="mr-2 cursor-pointer underline hover:text-blue-600" onclick={() => openStEdit(st)}>{t('srm.update')}</button><button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteSt(st.id)}>{t('srm.delete')}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <h3 class="mb-2 mt-4 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.entityBindings')}</h3>
      <form class="mb-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={addBinding}>
        <div class="grid gap-2 md:grid-cols-4">
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.stakeholders')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.stakeholder_id} required>
            <option value="" disabled>{t('srm.stakeholders')}</option>
            {#each stakeholders as st (st.id)}<option value={st.id}>{st.full_name}</option>{/each}
          </select></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.entityType')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.entity_type}>{#each ['portfolio', 'programme', 'project', 'contract'] as et (et)}<option value={et}>{t(`srm.et_${et}`)}</option>{/each}</select></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.entityId')}<input type="number" min="1" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.entity_id} required /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.roleDescription')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.role_description} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.powerScore')}<input type="number" min="1" max="5" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.power_score} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.interestScore')}<input type="number" min="1" max="5" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.interest_score} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supportBand')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.support_band}>{#each ['ambassador', 'on_the_fence', 'sceptic'] as sb (sb)}<option value={sb}>{t(`srm.sb_${sb}`)}</option>{/each}</select></label>
          <div class="grid grid-cols-2 gap-2">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.currentEngagement')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.current_engagement}>{#each ['unaware', 'resistant', 'neutral', 'supportive', 'champion'] as ce (ce)}<option value={ce}>{t(`srm.eng_${ce}`)}</option>{/each}</select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.targetEngagement')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={bindForm.target_engagement}>{#each ['unaware', 'resistant', 'neutral', 'supportive', 'champion'] as te (te)}<option value={te}>{t(`srm.eng_${te}`)}</option>{/each}</select></label>
          </div>
        </div>
        <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.create')}</button>
      </form>
      {#if bindings.length === 0}<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('srm.stakeholders')}</th><th class="py-1.5 pr-3">{t('srm.entityType')} / {t('srm.entityId')}</th>
            <th class="py-1.5 pr-3">{t('srm.quadrant')}</th><th class="py-1.5 pr-3">{t('srm.influence')}</th><th class="py-1.5 pr-3">{t('srm.supportBand')}</th>
            <th class="py-1.5 pr-3">{t('srm.currentEngagement')} → {t('srm.targetEngagement')}</th><th class="py-1.5 pr-3"></th>
          </tr></thead>
          <tbody>
            {#each bindings as b (b.id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{b.stakeholder_name || `#${b.stakeholder_id}`}</td>
                <td class="py-1.5 pr-3">{t(`srm.et_${b.entity_type}`)} #{b.entity_id}</td>
                <td class="py-1.5 pr-3">{b.quadrant ? t(`srm.q_${b.quadrant}`) : '—'}</td>
                <td class="py-1.5 pr-3">{b.influence_band ? t(`srm.ib_${b.influence_band}`) : '—'}</td>
                <td class="py-1.5 pr-3">{#if b.support_band === 'ambassador'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.sb_ambassador')}</span>{:else if b.support_band === 'sceptic'}<span class="rounded-full bg-red-100 px-2 py-0.5 text-red-700">{t('srm.sb_sceptic')}</span>{:else}<span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('srm.sb_on_the_fence')}</span>{/if}</td>
                <td class="py-1.5 pr-3">{t(`srm.eng_${b.current_engagement}`)} → {t(`srm.eng_${b.target_engagement}`)}</td>
                <td class="py-1.5 pr-3"><button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteBinding(b.id)}>{t('srm.delete')}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Communications plans -->
    <section>
      <div class="mb-2 flex items-center justify-between gap-2">
        <h2 class="text-sm font-semibold">{t('srm.commsPlans')}</h2>
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={commsSupplierId} onchange={loadComms}>
          <option value="" disabled>{t('srm.supplier')}</option>
          {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
        </select>
      </div>
      {#if commsSupplierId}
        <div class="mb-3 flex items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.scope')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={commsScope}>{#each ['all', 'internal', 'external'] as sc (sc)}<option value={sc}>{t(`srm.scope_${sc}`)}</option>{/each}</select></label>
          <label class="flex items-center gap-2 pb-1 text-xs text-[var(--color-ds-text-muted,#6b7280)]"><input type="checkbox" bind:checked={commsComplete} /> {t('srm.complete')}</label>
          <button type="button" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700" onclick={saveCommsPlan}>{t('srm.save')}</button>
        </div>
        {#if commsMessages.length === 0}<div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
        {:else}
          <table class="mb-3 w-full text-left text-xs">
            <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-3">{t('srm.stakeholders')}</th><th class="py-1.5 pr-3">{t('srm.message')}</th><th class="py-1.5 pr-3">{t('srm.method')}</th>
              <th class="py-1.5 pr-3">{t('srm.frequency')}</th><th class="py-1.5 pr-3">{t('srm.signedOff')}</th><th class="py-1.5 pr-3"></th>
            </tr></thead>
            <tbody>
              {#each commsMessages as msg (msg.id)}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                  <td class="py-1.5 pr-3">{msg.stakeholder_name || `#${msg.stakeholder_id}`}</td>
                  <td class="max-w-xs truncate py-1.5 pr-3" title={msg.communication_message}>{msg.communication_message}</td>
                  <td class="py-1.5 pr-3">{msg.method || '—'}</td>
                  <td class="py-1.5 pr-3">{msg.frequency || '—'}</td>
                  <td class="py-1.5 pr-3">{#if msg.signed_off}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.yes')}</span>{:else}<button type="button" class="cursor-pointer underline hover:text-blue-600" onclick={() => signOffMsg(msg.id)}>{t('srm.signOff')}</button>{/if}</td>
                  <td class="py-1.5 pr-3"><button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteMsg(msg.id)}>{t('srm.delete')}</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <form class="flex flex-wrap items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={addMsg}>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.stakeholders')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={msgForm.stakeholder_id} required>
            <option value="" disabled>{t('srm.stakeholders')}</option>
            {#each stakeholders as st (st.id)}<option value={st.id}>{st.full_name}</option>{/each}
          </select></label>
          <label class="flex-1 basis-40 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.message')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={msgForm.communication_message} required /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.method')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={msgForm.method} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.frequency')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={msgForm.frequency} /></label>
          <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.create')}</button>
        </form>
      {:else}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.selectSupplierFirst')}</div>
      {/if}
    </section>

    <!-- RACI matrices -->
    <section>
      <div class="mb-2 flex items-center justify-between gap-2">
        <h2 class="text-sm font-semibold">{t('srm.raciMatrices')}</h2>
        <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciSupplierId} onchange={loadRaci}>
          <option value="" disabled>{t('srm.supplier')}</option>
          {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
        </select>
      </div>
      {#if raciSupplierId}
        {#if !raci}
          <div class="mb-3 flex items-center justify-between rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">
            <span>{t('srm.noRecords')}</span>
            <button type="button" class="rounded-md bg-blue-600 px-2 py-1 font-medium text-white hover:bg-blue-700" onclick={createRaciVersion}>{t('srm.newVersion')}</button>
          </div>
        {:else}
          <div class="mb-3 flex items-center gap-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3 text-xs">
            <span>{t('srm.version')} {raci.version}</span>
            {#if raci.status === 'agreed'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.raci_agreed')}</span>{:else if raci.status === 'draft'}<span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t('srm.raci_draft')}</span>{:else}<span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t(`srm.raci_${raci.status}`)}</span>{/if}
            {#if raci.agreed_at}<span class="text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(raci.agreed_at)}</span>{/if}
            <div class="ml-auto flex gap-2">
              {#if raci.status !== 'agreed'}<button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered" onclick={() => setRaciStatus('agreed')}>{t('srm.markAgreed')}</button>{/if}
              <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered" onclick={createRaciVersion}>{t('srm.newVersion')}</button>
            </div>
          </div>
          {#if (raci.assignments ?? []).length === 0}<div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
          {:else}
            <table class="mb-3 w-full text-left text-xs">
              <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                <th class="py-1.5 pr-3">{t('srm.activityCode')}</th><th class="py-1.5 pr-3">{t('srm.side')}</th><th class="py-1.5 pr-3">{t('srm.roleName')}</th>
                <th class="py-1.5 pr-3">{t('srm.userId')}</th><th class="py-1.5 pr-3">{t('srm.raciValue')}</th><th class="py-1.5 pr-3"></th>
              </tr></thead>
              <tbody>
                {#each raci.assignments as a (a.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-3">{a.activity_code}</td>
                    <td class="py-1.5 pr-3"><span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t(`srm.side_${a.side}`)}</span></td>
                    <td class="py-1.5 pr-3">{a.role_name || '—'}</td>
                    <td class="py-1.5 pr-3">{a.user_id ? (a.user_name || `#${a.user_id}`) : '—'}</td>
                    <td class="py-1.5 pr-3">{#if a.value === 'a'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 font-semibold text-emerald-700">A</span>{:else if a.value === 'r'}<span class="rounded-full bg-blue-100 px-2 py-0.5 font-semibold text-blue-700">R</span>{:else}<span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{a.value.toUpperCase()}</span>{/if}</td>
                    <td class="py-1.5 pr-3"><button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteRaciAssignment(a.id)}>{t('srm.delete')}</button></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        {/if}
        <form class="flex flex-wrap items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={addRaciAssignment}>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.activityCode')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciForm.activity_code} required /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.side')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciForm.side}>{#each ['agency', 'supplier'] as sd (sd)}<option value={sd}>{t(`srm.side_${sd}`)}</option>{/each}</select></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.roleName')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciForm.role_name} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.userId')}<input type="number" min="1" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciForm.user_id} /></label>
          <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.raciValue')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={raciForm.value}>{#each ['r', 'a', 'c', 'i'] as rv (rv)}<option value={rv}>{rv.toUpperCase()}</option>{/each}</select></label>
          <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
        </form>
      {:else}
        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.selectSupplierFirst')}</div>
      {/if}
    </section>

    <!-- Meetings -->
    <section>
      <div class="mb-2 flex items-center justify-between gap-2">
        <h2 class="text-sm font-semibold">{t('srm.meetings')}</h2>
        <div class="flex items-center gap-2">
          <select class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={meetingFilter} onchange={loadMeetings}>
            <option value="">{t('srm.allSuppliers')}</option>
            {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
          </select>
          <button type="button" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700" onclick={() => { actionError = ''; mForm = emptyMForm(); mOpen = true; }}>{t('srm.create')}</button>
        </div>
      </div>
      {#if mOpen}
        <form class="mb-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={saveMeeting}>
          <div class="grid gap-2 md:grid-cols-4">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplier')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={mForm.supplier_id} required>
              <option value="" disabled>{t('srm.supplier')}</option>
              {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
            </select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.contract')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={mForm.contract_id}>
              <option value="">{t('srm.none')}</option>
              {#each contractsFor(mForm.supplier_id) as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
            </select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.meetingType')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={mForm.meeting_type}>{#each ['qbr', 'internal_kickoff', 'supplier_kickoff', 'jbp_workshop', 'operational_performance', 'relationship_review', 'strategic_relationship', 'other'] as mt (mt)}<option value={mt}>{t(`srm.mt_${mt}`)}</option>{/each}</select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.scheduledAt')}<input type="datetime-local" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={mForm.scheduled_at} required /></label>
            <label class="md:col-span-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.location')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={mForm.location} /></label>
          </div>
          <div class="flex gap-2">
            <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (mOpen = false)}>{t('srm.cancel')}</button>
          </div>
        </form>
      {/if}
      {#if meetings.length === 0}<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3"></th><th class="py-1.5 pr-3">{t('srm.scheduledAt')}</th><th class="py-1.5 pr-3">{t('srm.meetingType')}</th>
            <th class="py-1.5 pr-3">{t('srm.supplier')}</th><th class="py-1.5 pr-3">{t('srm.status')}</th><th class="py-1.5 pr-3"></th>
          </tr></thead>
          <tbody>
            {#each meetings as m (m.id)}
              {@const open = activeMeetingId === m.id}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3"><button type="button" class="cursor-pointer underline hover:text-blue-600" onclick={() => (activeMeetingId = open ? null : m.id)} aria-label={t('srm.details')}>{open ? '−' : '+'}</button></td>
                <td class="py-1.5 pr-3">{formatDate(m.scheduled_at)}</td>
                <td class="py-1.5 pr-3">{t(`srm.mt_${m.meeting_type}`)}{#if m.location}<span class="text-[var(--color-ds-text-muted,#6b7280)]"> · {m.location}</span>{/if}</td>
                <td class="py-1.5 pr-3">{supplierName(m.supplier_id)}</td>
                <td class="py-1.5 pr-3">{#if m.status === 'held'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.held')}</span>{:else if m.status === 'cancelled'}<span class="rounded-full bg-red-100 px-2 py-0.5 text-red-700">{t('srm.cancelled')}</span>{:else}<span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t('srm.scheduled')}</span>{/if}</td>
                <td class="py-1.5 pr-3">{#if m.status === 'scheduled'}<button type="button" class="mr-2 cursor-pointer underline hover:text-blue-600" onclick={() => setMeetingStatus(m, 'held')}>{t('srm.markHeld')}</button>{/if}<button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteMeeting(m.id)}>{t('srm.delete')}</button></td>
              </tr>
              {#if open}
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-hovered">
                  <td colspan="6" class="space-y-3 px-4 py-3 text-xs">
                    <div>
                      <h4 class="mb-1 font-semibold">{t('srm.agenda')}</h4>
                      {#if (m.agenda ?? []).length === 0}<span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</span>{:else}
                        <ul class="list-inside list-disc space-y-0.5">{#each m.agenda as item (item.no)}<li>#{item.no} {item.description} <span class="text-[var(--color-ds-text-muted,#6b7280)]">({t(`srm.mode_${item.mode}`)})</span></li>{/each}</ul>
                      {/if}
                    </div>
                    <div>
                      <h4 class="mb-1 font-semibold">{t('srm.attendees')}</h4>
                      {#each m.attendees ?? [] as att (att.id)}
                        <div class="flex items-center gap-2">
                          <span>{att.name || `#${att.stakeholder_id ?? att.user_id}`}</span>
                          <span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t(`srm.side_${att.side}`)}</span>
                          {#if att.role_description}<span class="text-[var(--color-ds-text-muted,#6b7280)]">{att.role_description}</span>{/if}
                          <button type="button" class="ml-auto cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => removeAttendee(att.id)}>{t('srm.delete')}</button>
                        </div>
                      {/each}
                      <form class="mt-2 flex flex-wrap items-end gap-2" onsubmit={addAttendee}>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.side')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={attForm.side}>{#each ['agency', 'supplier'] as sd (sd)}<option value={sd}>{t(`srm.side_${sd}`)}</option>{/each}</select></label>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.stakeholders')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={attForm.stakeholder_id}>
                          <option value="">—</option>
                          {#each stakeholders as st (st.id)}<option value={st.id}>{st.full_name}</option>{/each}
                        </select></label>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.userId')}<input type="number" min="1" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={attForm.user_id} /></label>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.roleDescription')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={attForm.role_description} /></label>
                        <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 font-medium text-white hover:bg-blue-700">{t('srm.create')}</button>
                      </form>
                    </div>
                    <div>
                      <h4 class="mb-1 font-semibold">{t('srm.actionItems')}</h4>
                      {#each m.actions ?? [] as act (act.id)}
                        <div class="flex items-center gap-2">
                          <span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t(`srm.kind_${act.kind}`)}</span>
                          <span class={act.status === 'closed' ? 'text-[var(--color-ds-text-muted,#6b7280)] line-through' : ''}>{act.text}</span>
                          {#if act.due_date}<span class="text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.dueDate')}: {formatDate(act.due_date)}</span>{/if}
                          {#if act.status === 'open'}<button type="button" class="ml-auto cursor-pointer underline hover:text-blue-600" onclick={() => closeAction(act.id)}>{t('srm.close')}</button>{:else}<span class="ml-auto rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t('srm.closed')}</span>{/if}
                        </div>
                      {/each}
                      <form class="mt-2 flex flex-wrap items-end gap-2" onsubmit={addAction}>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.kind')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={actForm.kind}>{#each ['action', 'note', 'question'] as kd (kd)}<option value={kd}>{t(`srm.kind_${kd}`)}</option>{/each}</select></label>
                        <label class="flex-1 basis-40 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.text')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={actForm.text} required /></label>
                        <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.dueDate')}<input type="date" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={actForm.due_date} /></label>
                        <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 font-medium text-white hover:bg-blue-700">{t('srm.create')}</button>
                      </form>
                    </div>
                    <form onsubmit={saveMinutes}>
                      <h4 class="mb-1 font-semibold">{t('srm.minutes')}</h4>
                      <textarea rows="2" class="w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={m.minutes_text}></textarea>
                      <button type="submit" class="mt-1 rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 hover:bg-ds-background-neutral-hovered">{t('srm.save')}</button>
                    </form>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <!-- Joint business plans -->
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h2 class="text-sm font-semibold">{t('srm.jointBusinessPlans')}</h2>
        <button type="button" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700" onclick={() => { actionError = ''; jbpForm = emptyJbpForm(); jbpOpen = true; }}>{t('srm.create')}</button>
      </div>
      {#if jbpOpen}
        <form class="mb-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={saveJbp}>
          <div class="grid gap-2 md:grid-cols-4">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplier')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.supplier_id} required>
              <option value="" disabled>{t('srm.supplier')}</option>
              {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
            </select></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.contract')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.contract_id} required>
              <option value="" disabled>{t('srm.contract')}</option>
              {#each contractsFor(jbpForm.supplier_id) as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
            </select></label>
            <label class="md:col-span-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.purpose')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.purpose} /></label>
            <label class="md:col-span-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.vision')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.vision} /></label>
            <label class="md:col-span-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.scope')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.scope} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.governanceModel')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.governance_model} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.reviewCycleMonths')}<input type="number" min="1" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.review_cycle_months} /></label>
            <label class="md:col-span-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.relationshipCharter')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpForm.relationship_charter} placeholder="item one, item two" /></label>
          </div>
          <div class="flex gap-2">
            <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (jbpOpen = false)}>{t('srm.cancel')}</button>
          </div>
        </form>
      {/if}
      {#if jbps.length === 0}<div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</div>
      {:else}
        <table class="w-full text-left text-xs">
          <thead><tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
            <th class="py-1.5 pr-3">{t('srm.supplier')}</th><th class="py-1.5 pr-3">{t('srm.contract')}</th><th class="py-1.5 pr-3">{t('srm.version')}</th>
            <th class="py-1.5 pr-3">{t('srm.purpose')}</th><th class="py-1.5 pr-3">{t('srm.status')}</th>
            <th class="py-1.5 pr-3">{t('srm.agencyRepName')}</th><th class="py-1.5 pr-3">{t('srm.supplierRepName')}</th><th class="py-1.5 pr-3">{t('srm.actions')}</th>
          </tr></thead>
          <tbody>
            {#each jbps as j (j.id)}
              {@const c = contractById(j.contract_id)}
              <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                <td class="py-1.5 pr-3">{supplierName(j.supplier_id)}</td>
                <td class="max-w-xs truncate py-1.5 pr-3" title={c ? contractLabel(c) : ''}>{c ? contractLabel(c) : `#${j.contract_id}`}</td>
                <td class="py-1.5 pr-3">v{j.version}</td>
                <td class="max-w-xs truncate py-1.5 pr-3" title={j.purpose}>{j.purpose || '—'}</td>
                <td class="py-1.5 pr-3">{#if j.status === 'signed'}<span class="rounded-full bg-emerald-100 px-2 py-0.5 text-emerald-700">{t('srm.jbp_signed')}</span>{:else if j.status === 'in_review'}<span class="rounded-full bg-amber-100 px-2 py-0.5 text-amber-700">{t('srm.jbp_in_review')}</span>{:else}<span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{t(`srm.jbp_${j.status}`)}</span>{/if}</td>
                <td class="py-1.5 pr-3">{j.agency_rep_name || '—'}{#if j.agency_signed_at}<div class="text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(j.agency_signed_at)}</div>{/if}</td>
                <td class="py-1.5 pr-3">{j.supplier_rep_name || '—'}{#if j.supplier_signed_at}<div class="text-[var(--color-ds-text-muted,#6b7280)]">{formatDate(j.supplier_signed_at)}</div>{/if}</td>
                <td class="py-1.5 pr-3">
                  {#if j.status !== 'signed'}<button type="button" class="mr-2 cursor-pointer underline hover:text-blue-600" onclick={() => openJbpEdit(j)}>{t('srm.update')}</button><button type="button" class="mr-2 cursor-pointer underline hover:text-emerald-700" onclick={() => signJbp(j, 'agency')}>{t('srm.signAgency')}</button><button type="button" class="mr-2 cursor-pointer underline hover:text-emerald-700" onclick={() => signJbp(j, 'supplier')}>{t('srm.signSupplier')}</button>{/if}
                  <button type="button" class="cursor-pointer text-red-600 underline hover:text-red-700" onclick={() => deleteJbp(j.id)}>{t('srm.delete')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if jbpEditOpen}
        <form class="mt-3 space-y-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3" onsubmit={saveJbpEdit}>
          <div class="grid gap-2 md:grid-cols-4">
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.status')}<select class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.status}>{#each ['draft', 'in_review'] as st (st)}<option value={st}>{t(`srm.jbp_${st}`)}</option>{/each}</select></label>
            <label class="md:col-span-3 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.purpose')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.purpose} /></label>
            <label class="md:col-span-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.vision')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.vision} /></label>
            <label class="md:col-span-2 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.scope')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.scope} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.governanceModel')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.governance_model} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.reviewCycleMonths')}<input type="number" min="1" class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.review_cycle_months} /></label>
            <label class="md:col-span-4 text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.relationshipCharter')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.relationship_charter} placeholder="item one, item two" /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.agencyRepName')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.agency_rep_name} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.agencyRepRole')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.agency_rep_role} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplierRepName')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.supplier_rep_name} /></label>
            <label class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.supplierRepRole')}<input class="mt-1 w-full rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs" bind:value={jbpEditForm.supplier_rep_role} /></label>
          </div>
          <div class="flex gap-2">
            <button type="submit" class="rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">{t('srm.save')}</button>
            <button type="button" class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-2 py-1 text-xs hover:bg-ds-background-neutral-hovered" onclick={() => (jbpEditOpen = false)}>{t('srm.cancel')}</button>
          </div>
        </form>
      {/if}
    </section>

  </div>
{/if}

