<script>
  import { onMount } from 'svelte';
  import { t } from '../../stores/i18n.svelte.js';
  import EmptyState from '../../components/EmptyState.svelte';
  import { srm } from '../../api/srm.js';
  import { getUsers } from '../../api/users.js';
  import { formatDate, formatMoney } from './utils.js';

  const SECTIONS = [
    { id: 'metrics', labelKey: 'srm.performanceMetrics' },
    { id: 'scorecards', labelKey: 'srm.scorecards' },
    { id: 'surveys', labelKey: 'srm.pulseSurveys' },
    { id: 'innovations', labelKey: 'srm.innovationRegister' },
    { id: 'sips', labelKey: 'srm.serviceImprovementPlans' },
    { id: 'disputes', labelKey: 'srm.disputes' },
  ];

  const METRIC_TYPES = ['kpi', 'sla'];
  const DIRECTIONS = [
    { value: 'above', key: 'srm.dirAbove' },
    { value: 'below', key: 'srm.dirBelow' },
  ];
  const FREQUENCIES = [
    { value: 'monthly', key: 'srm.freqMonthly' },
    { value: 'quarterly', key: 'srm.freqQuarterly' },
  ];
  const DATA_SOURCES = [
    { value: 'manual', key: 'srm.dsManual' },
    { value: 'erp_api', key: 'srm.dsErpApi' },
    { value: 'supplier_reported', key: 'srm.dsSupplierReported' },
  ];
  const SCORECARD_DIMENSIONS = [
    { value: 'risk_summary', key: 'srm.dimRiskSummary' },
    { value: 'initiatives_summary', key: 'srm.dimInitiativesSummary' },
    { value: 'performance_summary', key: 'srm.dimPerformanceSummary' },
    { value: 'benefits_delivered', key: 'srm.dimBenefitsDelivered' },
    { value: 'governance_summary', key: 'srm.dimGovernanceSummary' },
  ];
  const RAGS = [
    { value: 'green', label: 'Green' },
    { value: 'amber', label: 'Amber' },
    { value: 'red', label: 'Red' },
  ];
  const SURVEY_DIRECTIONS = [
    { value: 'internal_rates_supplier', key: 'srm.internalRatesSupplier' },
    { value: 'supplier_rates_agency', key: 'srm.supplierRatesAgency' },
  ];
  const INNOVATION_SOURCES = [
    { value: 'supplier_submission', key: 'srm.srcSupplierSubmission' },
    { value: 'agency_observation', key: 'srm.srcAgencyObservation' },
    { value: 'jbp_workshop', key: 'srm.srcJbpWorkshop' },
    { value: 'scorecard_review', key: 'srm.srcScorecardReview' },
    { value: 'other', key: 'srm.srcOther' },
  ];
  const INNOVATION_STATUSES = [
    { value: 'submitted', key: 'srm.innoSubmitted' },
    { value: 'under_review', key: 'srm.innoUnderReview' },
    { value: 'proceed', key: 'srm.decProceed' },
    { value: 'reject', key: 'srm.reject' },
    { value: 'further_investigate', key: 'srm.innoFurtherInvestigate' },
    { value: 'implementing', key: 'srm.innoImplementing' },
    { value: 'implemented', key: 'srm.innoImplemented' },
  ];
  const RECOMMEND_DECISIONS = [
    { value: 'proceed', key: 'srm.decProceed' },
    { value: 'reject', key: 'srm.reject' },
    { value: 'further_investigate', key: 'srm.innoFurtherInvestigate' },
  ];
  const PRIORITIES = [
    { value: '', key: '' },
    { value: 'high', key: 'srm.priHigh' },
    { value: 'medium', key: 'srm.priMedium' },
    { value: 'low', key: 'srm.priLow' },
  ];
  const SIDES = [
    { value: 'agency', key: 'srm.sideAgency' },
    { value: 'supplier', key: 'srm.sideSupplier' },
  ];
  const SIP_SUBTYPES = [
    { value: 'breach_driven', key: 'srm.subBreachDriven' },
    { value: 'continuous_improvement', key: 'srm.subContinuousImprovement' },
  ];
  const SIP_STATUSES = [
    { value: 'draft', key: 'srm.sipDraft' },
    { value: 'active', key: 'srm.sipActive' },
    { value: 'on_track_review', key: 'srm.sipOnTrackReview' },
    { value: 'resolved', key: 'srm.sipResolved' },
    { value: 'closed_unresolved', key: 'srm.sipClosedUnresolved' },
  ];
  const DISPUTE_STAGES = [
    { value: 'l1_working_level', key: 'srm.stageL1WorkingLevel' },
    { value: 'l2_director', key: 'srm.stageL2Director' },
    { value: 'l3_executive', key: 'srm.stageL3Executive' },
    { value: 'mediation', key: 'srm.stageMediation' },
    { value: 'litigation', key: 'srm.stageLitigation' },
    { value: 'closed', key: 'srm.stateClosed' },
  ];

  let section = $state('metrics');
  const loadedSections = new Set();

  // Base reference data shared by every section.
  let contracts = $state([]);
  let suppliers = $state([]);
  let users = $state([]);
  let baseLoading = $state(true);
  let baseError = $state('');

  const contractOptions = $derived(contracts.filter((c) => c.status === 'active' || c.status === 'draft'));

  function supplierName(id) {
    return suppliers.find((s) => s.id === id)?.company_name ?? `#${id}`;
  }
  function contractLabel(c) {
    return `${c.contract_number} — ${c.title}`;
  }
  function ragClass(rag) {
    if (rag === 'green') return 'bg-emerald-100 text-emerald-800';
    if (rag === 'amber') return 'bg-amber-100 text-amber-800';
    if (rag === 'red') return 'bg-red-100 text-red-800';
    return 'bg-slate-100 text-slate-600';
  }

  /** Localized label for a {value,key} option list; falls back to the raw value. */
  function labelOf(list, value) {
    const entry = list.find((x) => x.value === value);
    return entry ? t(entry.key) : value;
  }

  async function loadBase() {
    baseLoading = true;
    baseError = '';
    try {
      [contracts, suppliers, users] = await Promise.all([
        srm.contracts.getAll(),
        srm.suppliers.getAll(),
        getUsers().catch(() => []),
      ]);
    } catch (e) {
      baseError = e?.message || 'error';
    } finally {
      baseLoading = false;
    }
  }

  function openSection(id) {
    section = id;
    if (!loadedSections.has(id)) {
      loadedSections.add(id);
      loadSection(id).catch(() => {});
    }
  }

  async function loadSection(id) {
    if (id === 'metrics') await loadMetrics();
    else if (id === 'scorecards') await loadScorecards();
    else if (id === 'surveys') await loadSurveys();
    else if (id === 'innovations') await loadInnovations();
    else if (id === 'sips') await loadSips();
    else if (id === 'disputes') await loadDisputes();
  }

  onMount(loadBase);

  // ------------------------------------------------------------------ metrics
  let metrics = $state([]);
  let metricsLoading = $state(false);
  let metricsError = $state('');
  let selectedMetricId = $state(null);
  let metricDetail = $state(null);
  let mContractId = $state('');
  let mName = $state('');
  let mType = $state('kpi');
  let mUnit = $state('');
  let mTarget = $state('');
  let mDirection = $state('above');
  let mFrequency = $state('monthly');
  let mDataSource = $state('manual');
  let rPeriod = $state('');
  let rActual = $state('');

  async function loadMetrics() {
    metricsLoading = true;
    metricsError = '';
    try {
      metrics = (await srm.metrics.getAll()) ?? [];
    } catch (e) {
      metricsError = e?.message || 'error';
    } finally {
      metricsLoading = false;
    }
  }

  async function addMetric() {
    if (!mContractId || !mName.trim()) return;
    try {
      await srm.metrics.create({
        contract_id: Number(mContractId),
        name: mName.trim(),
        type: mType,
        unit: mUnit.trim(),
        target_value: parseFloat(mTarget) || 0,
        direction: mDirection,
        frequency: mFrequency,
        data_source: mDataSource,
      });
      mName = '';
      mUnit = '';
      mTarget = '';
      await loadMetrics();
    } catch (e) {
      metricsError = e?.message || 'error';
    }
  }

  async function selectMetric(id) {
    selectedMetricId = id;
    metricDetail = null;
    try {
      const [m, results] = await Promise.all([srm.metrics.get(id), srm.getMetricResults(id)]);
      metricDetail = { ...m, results: (results ?? []).sort((a, b) => a.period_start.localeCompare(b.period_start)) };
    } catch (e) {
      metricsError = e?.message || 'error';
    }
  }

  async function addResult() {
    if (!metricDetail || !rPeriod || rActual === '') return;
    const actual = parseFloat(rActual);
    const achieved = metricDetail.direction === 'above' ? actual >= metricDetail.target_value : actual <= metricDetail.target_value;
    try {
      await srm.recordMetricResult(metricDetail.id, {
        period_start: `${rPeriod}T00:00:00Z`,
        actual_value: actual,
        achieved,
      });
      rPeriod = '';
      rActual = '';
      const results = (await srm.getMetricResults(metricDetail.id)) ?? [];
      metricDetail.results = results.sort((a, b) => a.period_start.localeCompare(b.period_start));
    } catch (e) {
      metricsError = e?.message || 'error';
    }
  }

  // --------------------------------------------------------------- scorecards
  let scorecards = $state([]);
  let scLoading = $state(false);
  let scError = $state('');
  let selectedScId = $state(null);
  let scDetail = $state(null);
  let dimForms = $state({});
  let scContractId = $state('');
  let scPeriodStart = $state('');
  let scPeriodEnd = $state('');

  async function loadScorecards() {
    scLoading = true;
    scError = '';
    try {
      scorecards = (await srm.scorecards.getAll()) ?? [];
    } catch (e) {
      scError = e?.message || 'error';
    } finally {
      scLoading = false;
    }
  }

  function emptyDimForm() {
    return { main_highlights: '', key_concerns: '', rag: 'green', next_steps: '', supplier_rag: '', joint_resolution_note: '' };
  }

  async function selectScorecard(id) {
    selectedScId = id;
    scDetail = null;
    dimForms = {};
    try {
      const s = await srm.scorecards.get(id);
      for (const d of SCORECARD_DIMENSIONS) {
        const existing = (s.dimensions ?? []).find((x) => x.dimension === d.value);
        dimForms[d.value] = existing
          ? {
              main_highlights: existing.main_highlights,
              key_concerns: existing.key_concerns,
              rag: existing.rag,
              next_steps: existing.next_steps,
              supplier_rag: existing.supplier_rag ?? '',
              joint_resolution_note: existing.joint_resolution_note,
            }
          : emptyDimForm();
      }
      scDetail = s;
    } catch (e) {
      scError = e?.message || 'error';
    }
  }

  async function addScorecard() {
    if (!scContractId || !scPeriodStart || !scPeriodEnd) return;
    const contract = contracts.find((c) => c.id === Number(scContractId));
    try {
      await srm.scorecards.create({
        contract_id: Number(scContractId),
        supplier_id: contract?.supplier_id,
        period_start: `${scPeriodStart}T00:00:00Z`,
        period_end: `${scPeriodEnd}T00:00:00Z`,
        status: 'open',
      });
      scPeriodStart = '';
      scPeriodEnd = '';
      await loadScorecards();
    } catch (e) {
      scError = e?.message || 'error';
    }
  }

  async function saveDimension(dimValue) {
    if (!scDetail) return;
    const f = dimForms[dimValue];
    try {
      await srm.upsertScorecardDimension(scDetail.id, {
        dimension: dimValue,
        main_highlights: f.main_highlights.trim(),
        key_concerns: f.key_concerns.trim(),
        rag: f.rag,
        next_steps: f.next_steps.trim(),
        supplier_rag: f.supplier_rag || null,
        joint_resolution_note: f.joint_resolution_note.trim(),
      });
      await selectScorecard(scDetail.id);
    } catch (e) {
      scError = e?.message || 'error';
    }
  }

  async function closeScorecard() {
    if (!scDetail) return;
    try {
      await srm.scorecards.update(scDetail.id, { status: 'closed' });
      await selectScorecard(scDetail.id);
      await loadScorecards();
    } catch (e) {
      scError = e?.message || 'error';
    }
  }

  // ------------------------------------------------------------------ surveys
  let surveys = $state([]);
  let svLoading = $state(false);
  let svError = $state('');
  let selectedSvId = $state(null);
  let svDetail = $state(null);
  let svSupplierId = $state('');
  let svDirection = $state('internal_rates_supplier');
  let q1 = $state('');
  let q2 = $state('');
  let q3 = $state('');
  let vNotes = $state('');
  let vOverall = $state('');

  async function loadSurveys() {
    svLoading = true;
    svError = '';
    try {
      surveys = (await srm.pulseSurveys.getAll()) ?? [];
    } catch (e) {
      svError = e?.message || 'error';
    } finally {
      svLoading = false;
    }
  }

  async function selectSurvey(id) {
    selectedSvId = id;
    svDetail = null;
    try {
      svDetail = await srm.pulseSurveys.get(id);
    } catch (e) {
      svError = e?.message || 'error';
    }
  }

  async function addSurvey() {
    if (!svSupplierId) return;
    try {
      await srm.pulseSurveys.create({ supplier_id: Number(svSupplierId), direction: svDirection });
      await loadSurveys();
    } catch (e) {
      svError = e?.message || 'error';
    }
  }

  async function recordResponse() {
    if (!svDetail) return;
    const answers = {};
    if (q1 !== '') answers.communication = Number(q1);
    if (q2 !== '') answers.responsiveness = Number(q2);
    if (q3 !== '') answers.value_for_money = Number(q3);
    if (vNotes.trim()) answers.notes = vNotes.trim();
    try {
      await srm.recordSurveyResponse(svDetail.id, {
        answers,
        overall_score: vOverall === '' ? null : Number(vOverall),
      });
      q1 = '';
      q2 = '';
      q3 = '';
      vNotes = '';
      vOverall = '';
      await selectSurvey(svDetail.id);
      await loadSurveys();
    } catch (e) {
      svError = e?.message || 'error';
    }
  }

  // ------------------------------------------------------------- innovations
  let innovations = $state([]);
  let innoLoading = $state(false);
  let innoError = $state('');
  let selectedInnoId = $state(null);
  let innoDetail = $state(null);
  let iContractId = $state('');
  let iName = $state('');
  let iDescription = $state('');
  let iSource = $state('supplier_submission');
  let fEvaluator = $state('');
  let fFactor = $state('');
  let fRating = $state(3);
  let recDecision = $state('proceed');
  let recJustification = $state('');
  let recPriority = $state('');
  let recTiming = $state('');
  let recSupplierChampion = $state('');
  let objText = $state('');
  const objForms = $state({});
  let tNo = $state(1);
  let tName = $state('');
  let tSide = $state('agency');

  async function loadInnovations() {
    innoLoading = true;
    innoError = '';
    try {
      innovations = (await srm.innovations.getAll()) ?? [];
    } catch (e) {
      innoError = e?.message || 'error';
    } finally {
      innoLoading = false;
    }
  }

  async function selectInnovation(id) {
    selectedInnoId = id;
    innoDetail = null;
    try {
      const i = await srm.innovations.get(id);
      recDecision = i.recommendation?.decision ?? 'proceed';
      recJustification = i.recommendation?.justification ?? '';
      recPriority = i.recommendation?.priority ?? '';
      recTiming = i.recommendation?.timing ?? '';
      recSupplierChampion = i.recommendation?.supplier_champion_name ?? '';
      for (const o of i.objectives ?? []) {
        objForms[o.id] = { actual_outcome: o.actual_outcome, realised_value: o.realised_value_nzd === null ? '' : String(o.realised_value_nzd) };
      }
      innoDetail = i;
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function addInnovation() {
    if (!iContractId || !iName.trim()) return;
    const contract = contracts.find((c) => c.id === Number(iContractId));
    try {
      await srm.innovations.create({
        contract_id: Number(iContractId),
        supplier_id: contract?.supplier_id,
        name: iName.trim(),
        description: iDescription.trim(),
        source: iSource,
      });
      iName = '';
      iDescription = '';
      await loadInnovations();
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function setInnovationStatus(status) {
    if (!innoDetail) return;
    try {
      const updated = await srm.innovations.update(innoDetail.id, { status });
      innoDetail.status = updated.status;
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function saveFactor() {
    if (!innoDetail || !fFactor.trim()) return;
    try {
      await srm.upsertInnovationFactor(innoDetail.id, {
        evaluator_name: fEvaluator.trim(),
        factor_name: fFactor.trim(),
        rating: Number(fRating),
      });
      fEvaluator = '';
      fFactor = '';
      fRating = 3;
      await selectInnovation(innoDetail.id);
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function saveRecommendation() {
    if (!innoDetail || !recJustification.trim()) return;
    try {
      await srm.createInnovationRecommendation(innoDetail.id, {
        decision: recDecision,
        justification: recJustification.trim(),
        priority: recPriority || null,
        timing: recTiming.trim(),
        supplier_champion_name: recSupplierChampion.trim(),
      });
      await selectInnovation(innoDetail.id);
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function addObjective() {
    if (!innoDetail || !objText.trim()) return;
    try {
      await srm.addInnovationObjective(innoDetail.id, { objective_text: objText.trim() });
      objText = '';
      await selectInnovation(innoDetail.id);
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function saveObjective(objId) {
    if (!innoDetail) return;
    const f = objForms[objId];
    try {
      await srm.updateInnovationObjective(objId, {
        actual_outcome: f.actual_outcome.trim(),
        realised_value_nzd: f.realised_value === '' ? null : Number(f.realised_value),
      });
      await selectInnovation(innoDetail.id);
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  async function addTask() {
    if (!innoDetail || !tName.trim()) return;
    try {
      await srm.addInnovationTask(innoDetail.id, { task_no: Number(tNo), task_name: tName.trim(), side: tSide });
      tName = '';
      tNo += 1;
      await selectInnovation(innoDetail.id);
    } catch (e) {
      innoError = e?.message || 'error';
    }
  }

  // --------------------------------------------------------------------- sips
  let sips = $state([]);
  let sipLoading = $state(false);
  let sipError = $state('');
  let selectedSipId = $state(null);
  let sipDetail = $state(null);
  let pContractId = $state('');
  let pTitle = $state('');
  let pOwner = $state('');
  let pProblem = $state('');
  let pSubtype = $state('breach_driven');
  let pSmart = $state('');
  let pExit = $state('');
  let dKey = $state('');
  let dRag = $state('green');
  let dOwner = $state('');

  async function loadSips() {
    sipLoading = true;
    sipError = '';
    try {
      sips = (await srm.sips.getAll()) ?? [];
    } catch (e) {
      sipError = e?.message || 'error';
    } finally {
      sipLoading = false;
    }
  }

  async function selectSip(id) {
    selectedSipId = id;
    sipDetail = null;
    try {
      sipDetail = await srm.sips.get(id);
    } catch (e) {
      sipError = e?.message || 'error';
    }
  }

  async function addSip() {
    if (!pContractId || !pTitle.trim() || !pOwner || !pProblem.trim() || !pSmart.trim() || !pExit.trim()) return;
    const contract = contracts.find((c) => c.id === Number(pContractId));
    try {
      await srm.sips.create({
        contract_id: Number(pContractId),
        supplier_id: contract?.supplier_id,
        title: pTitle.trim(),
        sip_owner_id: Number(pOwner),
        problem_statement: pProblem.trim(),
        subtype: pSubtype,
        success_measure_smart: pSmart.trim(),
        exit_criteria: pExit.trim(),
      });
      pTitle = '';
      pProblem = '';
      pSmart = '';
      pExit = '';
      await loadSips();
    } catch (e) {
      sipError = e?.message || 'error';
    }
  }

  async function setSipStatus(status) {
    if (!sipDetail) return;
    try {
      const updated = await srm.sips.update(sipDetail.id, { status });
      sipDetail.status = updated.status;
    } catch (e) {
      sipError = e?.message || 'error';
    }
  }

  async function addDeliverable() {
    if (!sipDetail || !dKey.trim()) return;
    try {
      await srm.addSipDeliverable(sipDetail.id, {
        seq: (sipDetail.deliverables ?? []).length + 1,
        key_deliverable: dKey.trim(),
        rag: dRag,
        owner_name: dOwner.trim(),
      });
      dKey = '';
      dOwner = '';
      await selectSip(sipDetail.id);
    } catch (e) {
      sipError = e?.message || 'error';
    }
  }

  // ----------------------------------------------------------------- disputes
  let disputes = $state([]);
  let dpLoading = $state(false);
  let dpError = $state('');
  let selectedDpId = $state(null);
  let dpDetail = $state(null);
  let dpContractId = $state('');
  let dpDescription = $state('');
  let eToStage = $state('l2_director');
  let eEscalatedTo = $state('');
  let eNotes = $state('');
  const rpForm = $state({ non_performance_issue: '', root_cause: '', corrective_action: '', target_date: '' });
  let timetableRows = $state([]);

  async function loadDisputes() {
    dpLoading = true;
    dpError = '';
    try {
      disputes = (await srm.disputes.getAll()) ?? [];
    } catch (e) {
      dpError = e?.message || 'error';
    } finally {
      dpLoading = false;
    }
  }

  async function selectDispute(id) {
    selectedDpId = id;
    dpDetail = null;
    try {
      const d = await srm.disputes.get(id);
      dpDetail = d;
      eToStage = DISPUTE_STAGES.find((s) => s.value !== d.stage)?.value ?? 'l2_director';
      if (d.remedy_plan) {
        rpForm.non_performance_issue = d.remedy_plan.non_performance_issue;
        rpForm.root_cause = d.remedy_plan.root_cause;
        rpForm.corrective_action = d.remedy_plan.corrective_action_required;
        rpForm.target_date = d.remedy_plan.target_resolution_date ? d.remedy_plan.target_resolution_date.slice(0, 10) : '';
        timetableRows = (d.remedy_plan.timetable ?? []).map((row) => ({ ...row }));
      } else {
        rpForm.non_performance_issue = '';
        rpForm.root_cause = '';
        rpForm.corrective_action = '';
        rpForm.target_date = '';
        timetableRows = [];
      }
    } catch (e) {
      dpError = e?.message || 'error';
    }
  }

  async function addDispute() {
    if (!dpContractId || !dpDescription.trim()) return;
    const contract = contracts.find((c) => c.id === Number(dpContractId));
    try {
      await srm.disputes.create({
        contract_id: Number(dpContractId),
        supplier_id: contract?.supplier_id,
        description: dpDescription.trim(),
      });
      dpDescription = '';
      await loadDisputes();
    } catch (e) {
      dpError = e?.message || 'error';
    }
  }

  async function addEscalation() {
    if (!dpDetail) return;
    try {
      await srm.addDisputeEscalation(dpDetail.id, {
        to_stage: eToStage,
        escalated_to: eEscalatedTo.trim(),
        notes: eNotes.trim(),
      });
      eEscalatedTo = '';
      eNotes = '';
      await selectDispute(dpDetail.id);
    } catch (e) {
      dpError = e?.message || 'error';
    }
  }

  function addTimetableRow() {
    timetableRows.push({ action: '', owner_side: 'agency', due_date: '' });
  }

  async function saveRemedyPlan() {
    if (!dpDetail) return;
    const rows = timetableRows.filter((r) => r.action.trim());
    try {
      await srm.upsertRemedyPlan(dpDetail.id, {
        non_performance_issue: rpForm.non_performance_issue.trim(),
        root_cause: rpForm.root_cause.trim(),
        corrective_action_required: rpForm.corrective_action.trim(),
        target_resolution_date: `${rpForm.target_date}T00:00:00Z`,
        timetable: rows.map((r) => ({
          action: r.action.trim(),
          owner_side: r.owner_side,
          due_date: r.due_date ? `${r.due_date}T00:00:00Z` : null,
        })),
      });
      await selectDispute(dpDetail.id);
    } catch (e) {
      dpError = e?.message || 'error';
    }
  }

  const inputCls = 'rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-background-input px-2 py-1 text-xs';
  const btnCls = 'ml-auto cursor-pointer rounded-md border border-[var(--color-ds-border,#e5e7eb)] px-3 py-1 text-xs hover:bg-ds-background-neutral-hovered';
</script>

<div class="px-6 pt-4">
  {#if baseLoading}
    <div class="flex h-48 items-center justify-center text-sm text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.loading')}</div>
  {:else if baseError}
    <div class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
      {baseError}
      <button type="button" class={btnCls} onClick={() => loadBase()}>{t('srm.retry')}</button>
    </div>
  {:else}
    <div class="flex gap-1 border-b border-[var(--color-ds-border,#e5e7eb)] pb-0">
      {#each SECTIONS as s (s.id)}
        <button
          type="button"
          onClick={() => openSection(s.id)}
          class="cursor-pointer border-b-2 px-3 py-2 text-xs font-medium {section === s.id ? 'border-[var(--color-ds-accent,#2563eb)] text-[var(--color-ds-text,#111827)]' : 'border-transparent text-[var(--color-ds-text-muted,#6b7280)] hover:text-[var(--color-ds-text,#111827)]'}"
        >
          {t(s.labelKey)}
        </button>
      {/each}
    </div>

    <!-- ============================== METRICS ============================== -->
    {#if section === 'metrics'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.performanceMetrics')}</h2>
        {#if metricsError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{metricsError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.addMetric')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.contract')}
              <select class={inputCls} bind:value={mContractId}>
                <option value="" disabled selected></option>
                {#each contractOptions as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.name')}
              <input class={inputCls} bind:value={mName} placeholder="SLA uptime" />
            </label>
            <label class="text-xs">
              {t('srm.type')}
              <select class={inputCls} bind:value={mType}>
                {#each METRIC_TYPES as ty (ty)}<option value={ty}>{ty.toUpperCase()}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.unit')}
              <input class={inputCls} bind:value={mUnit} placeholder="% / $ / days" />
            </label>
            <label class="text-xs">
              {t('srm.target')}
              <input type="number" step="any" class={inputCls} bind:value={mTarget} />
            </label>
            <label class="text-xs">
              {t('srm.direction')}
              <select class={inputCls} bind:value={mDirection}>
                {#each DIRECTIONS as d (d.value)}<option value={d.value}>{t(d.key)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.frequency')}
              <select class={inputCls} bind:value={mFrequency}>
                {#each FREQUENCIES as f (f.value)}<option value={f.value}>{t(f.key)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.dataSource')}
              <select class={inputCls} bind:value={mDataSource}>
                {#each DATA_SOURCES as d (d.value)}<option value={d.value}>{t(d.key)}</option>{/each}
              </select>
            </label>
            <button type="button" class={btnCls} onClick={() => addMetric()} disabled={!mContractId || !mName.trim()}>
              {t('srm.create')}
            </button>
          </div>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.name')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.contract')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.type')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.target')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.frequency')}</th>
            </tr>
          </thead>
          <tbody>
            {#each metrics as m (m.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedMetricId === m.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectMetric(m.id)}
              >
                <td class="py-1.5 pr-2 font-medium">{m.name}</td>
                <td class="py-1.5 pr-2">{contracts.find((c) => c.id === m.contract_id)?.contract_number ?? `#${m.contract_id}`}</td>
                <td class="py-1.5 pr-2 uppercase">{m.type}</td>
                <td class="py-1.5 pr-2">{m.target_value} {m.unit}</td>
                <td class="py-1.5 pr-2">{t(m.frequency === 'monthly' ? 'srm.freqMonthly' : 'srm.freqQuarterly')}</td>
              </tr>
            {:else}
              <tr><td colspan="5" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if metricDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              {metricDetail.name} — {t('srm.results')}
            </h3>
            <div class="flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.periodStart')}
                <input type="date" class={inputCls} bind:value={rPeriod} />
              </label>
              <label class="text-xs">
                {t('srm.actualValue')}
                <input type="number" step="any" class={inputCls} bind:value={rActual} />
              </label>
              <button type="button" class={btnCls} onClick={() => addResult()} disabled={!rPeriod || rActual === ''}>
                {t('srm.recordResult')}
              </button>
            </div>
            <table class="mt-3 w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1.5 pr-2 font-medium">{t('srm.periodStart')}</th>
                  <th class="py-1.5 pr-2 font-medium">{t('srm.actualValue')}</th>
                  <th class="py-1.5 pr-2 font-medium">{t('srm.target')}</th>
                  <th class="py-1.5 pr-2 font-medium">{t('srm.status')}</th>
                </tr>
              </thead>
              <tbody>
                {#each metricDetail.results as r (r.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1.5 pr-2">{formatDate(r.period_start)}</td>
                    <td class="py-1.5 pr-2">{r.actual_value} {metricDetail.unit}</td>
                    <td class="py-1.5 pr-2">{metricDetail.target_value} {metricDetail.unit}</td>
                    <td class="py-1.5 pr-2">
                      <span class="rounded-full px-2 py-0.5 text-[11px] font-medium {r.achieved ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'}">
                        {r.achieved ? t('srm.achieved') : t('srm.notAchieved')}
                      </span>
                    </td>
                  </tr>
                {:else}
                  <tr><td colspan="4" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </section>

      <!-- ============================ SCORECARDS =========================== -->
    {:else if section === 'scorecards'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.scorecards')}</h2>
        {#if scError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{scError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.newScorecard')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.contract')}
              <select class={inputCls} bind:value={scContractId}>
                <option value="" disabled selected></option>
                {#each contractOptions as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.periodStart')}
              <input type="date" class={inputCls} bind:value={scPeriodStart} />
            </label>
            <label class="text-xs">
              {t('srm.periodEnd')}
              <input type="date" class={inputCls} bind:value={scPeriodEnd} />
            </label>
            <button type="button" class={btnCls} onClick={() => addScorecard()} disabled={!scContractId || !scPeriodStart || !scPeriodEnd}>
              {t('srm.create')}
            </button>
          </div>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.contract')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.periodStart')} → {t('srm.periodEnd')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.overallScore')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.status')}</th>
            </tr>
          </thead>
          <tbody>
            {#each scorecards as s (s.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedScId === s.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectScorecard(s.id)}
              >
                <td class="py-1.5 pr-2 font-medium">{s.supplier_name || supplierName(s.supplier_id)}</td>
                <td class="py-1.5 pr-2">{contracts.find((c) => c.id === s.contract_id)?.contract_number ?? `#${s.contract_id}`}</td>
                <td class="py-1.5 pr-2">{formatDate(s.period_start)} → {formatDate(s.period_end)}</td>
                <td class="py-1.5 pr-2">{s.overall_score ?? '—'}/5</td>
                <td class="py-1.5 pr-2">
                  <span class="rounded-full px-2 py-0.5 text-[11px] font-medium {s.status === 'open' ? 'bg-sky-100 text-sky-800' : 'bg-slate-100 text-slate-600'}">
                    {s.status}
                  </span>
                </td>
              </tr>
            {:else}
              <tr><td colspan="5" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if scDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <div class="mb-3 flex items-center gap-2">
              <h3 class="text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
                {scDetail.supplier_name || supplierName(scDetail.supplier_id)} — {formatDate(scDetail.period_start)} → {formatDate(scDetail.period_end)}
              </h3>
              {#if scDetail.status === 'open'}
                <button type="button" class={btnCls} onClick={() => closeScorecard()}>{t('srm.closeScorecard')}</button>
              {:else}
                <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">{scDetail.status}</span>
              {/if}
            </div>

            {#each SCORECARD_DIMENSIONS as d (d.value)}
              {@const f = dimForms[d.value]}
              <div class="mb-3 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-3">
                <div class="mb-2 flex items-center gap-2">
                  <span class="text-xs font-semibold">{t(d.key)}</span>
                  {#if f.rag}
                    <span class="rounded-full px-2 py-0.5 text-[11px] font-medium {ragClass(f.rag)}">{f.rag}</span>
                  {/if}
                  {#if f.supplier_rag && f.supplier_rag !== f.rag}
                    <span class="rounded-full px-2 py-0.5 text-[11px] font-medium {ragClass(f.supplier_rag)}">
                      {t('srm.supplierRag')}: {f.supplier_rag}
                    </span>
                  {/if}
                </div>
                <div class="grid grid-cols-2 gap-2">
                  <label class="text-xs">
                    {t('srm.mainHighlights')}
                    <textarea class="{inputCls} w-full" rows="2" bind:value={f.main_highlights} disabled={scDetail.status === 'closed'}></textarea>
                  </label>
                  <label class="text-xs">
                    {t('srm.keyConcerns')}
                    <textarea class="{inputCls} w-full" rows="2" bind:value={f.key_concerns} disabled={scDetail.status === 'closed'}></textarea>
                  </label>
                  <label class="text-xs">
                    {t('srm.rag')}
                    <select class={inputCls} bind:value={f.rag} disabled={scDetail.status === 'closed'}>
                      {#each RAGS as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
                    </select>
                  </label>
                  <label class="text-xs">
                    {t('srm.supplierRag')}
                    <select class={inputCls} bind:value={f.supplier_rag} disabled={scDetail.status === 'closed'}>
                      <option value="">—</option>
                      {#each RAGS as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
                    </select>
                  </label>
                  <label class="text-xs">
                    {t('srm.nextSteps')}
                    <textarea class="{inputCls} w-full" rows="2" bind:value={f.next_steps} disabled={scDetail.status === 'closed'}></textarea>
                  </label>
                  <label class="text-xs">
                    {t('srm.jointResolutionNote')}
                    <textarea class="{inputCls} w-full" rows="2" bind:value={f.joint_resolution_note} disabled={scDetail.status === 'closed'}></textarea>
                  </label>
                </div>
                {#if scDetail.status !== 'closed'}
                  <button type="button" class={btnCls} onClick={() => saveDimension(d.value)}>{t('srm.save')}</button>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </section>

      <!-- ============================== SURVEYS ============================ -->
    {:else if section === 'surveys'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.pulseSurveys')}</h2>
        {#if svError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{svError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.newSurvey')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.supplier')}
              <select class={inputCls} bind:value={svSupplierId}>
                <option value="" disabled selected></option>
                {#each suppliers as s (s.id)}<option value={s.id}>{s.company_name}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.direction')}
              <select class={inputCls} bind:value={svDirection}>
                {#each SURVEY_DIRECTIONS as d (d.value)}<option value={d.value}>{t(d.key)}</option>{/each}
              </select>
            </label>
            <button type="button" class={btnCls} onClick={() => addSurvey()} disabled={!svSupplierId}>{t('srm.create')}</button>
          </div>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.direction')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.sentAt')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.responsesCount')}</th>
            </tr>
          </thead>
          <tbody>
            {#each surveys as s (s.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedSvId === s.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectSurvey(s.id)}
              >
                <td class="py-1.5 pr-2 font-medium">{s.supplier_name || supplierName(s.supplier_id)}</td>
                <td class="py-1.5 pr-2">
                  {t(s.direction === 'internal_rates_supplier' ? 'srm.internalRatesSupplier' : 'srm.supplierRatesAgency')}
                </td>
                <td class="py-1.5 pr-2">{formatDate(s.sent_at)}</td>
                <td class="py-1.5 pr-2">{s.responses_count}</td>
              </tr>
            {:else}
              <tr><td colspan="4" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if svDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              {svDetail.supplier_name || supplierName(svDetail.supplier_id)} — {t('srm.recordResponse')}
            </h3>
            <div class="flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.qCommunication')} (1–5)
                <input type="number" min="1" max="5" class={inputCls} bind:value={q1} />
              </label>
              <label class="text-xs">
                {t('srm.qResponsiveness')} (1–5)
                <input type="number" min="1" max="5" class={inputCls} bind:value={q2} />
              </label>
              <label class="text-xs">
                {t('srm.qValueForMoney')} (1–5)
                <input type="number" min="1" max="5" class={inputCls} bind:value={q3} />
              </label>
              <label class="text-xs">
                {t('srm.overallScore')} (1–5)
                <select class={inputCls} bind:value={vOverall}>
                  <option value="">—</option>
                  {#each [1, 2, 3, 4, 5] as n (n)}<option value={String(n)}>{n}</option>{/each}
                </select>
              </label>
              <label class="text-xs">
                {t('srm.notes')}
                <input class="{inputCls} w-64" bind:value={vNotes} />
              </label>
              <button type="button" class={btnCls} onClick={() => recordResponse()}>{t('srm.save')}</button>
            </div>

            {#if (svDetail.responses ?? []).length > 0}
              <table class="mt-3 w-full text-left text-xs">
                <thead>
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                    <th class="py-1.5 pr-2 font-medium">{t('srm.respondent')}</th>
                    <th class="py-1.5 pr-2 font-medium">{t('srm.answers')}</th>
                    <th class="py-1.5 pr-2 font-medium">{t('srm.overallScore')}</th>
                    <th class="py-1.5 pr-2 font-medium">{t('srm.respondedAt')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each svDetail.responses as r (r.id)}
                    <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                      <td class="py-1.5 pr-2">{r.is_anonymous ? t('srm.anonymous') : (r.stakeholder_name || '—')}</td>
                      <td class="max-w-md truncate py-1.5 pr-2" title={JSON.stringify(r.answers ?? {})}>{JSON.stringify(r.answers ?? {})}</td>
                      <td class="py-1.5 pr-2">{r.overall_score ?? '—'}/5</td>
                      <td class="py-1.5 pr-2">{formatDate(r.responded_at)}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </div>
        {/if}
      </section>

      <!-- ============================ INNOVATIONS ========================== -->
    {:else if section === 'innovations'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.innovationRegister')}</h2>
        {#if innoError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{innoError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.addInnovation')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.contract')}
              <select class={inputCls} bind:value={iContractId}>
                <option value="" disabled selected></option>
                {#each contractOptions as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.name')}
              <input class={inputCls} bind:value={iName} />
            </label>
            <label class="text-xs">
              {t('srm.source')}
              <select class={inputCls} bind:value={iSource}>
                {#each INNOVATION_SOURCES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
              </select>
            </label>
            <button type="button" class={btnCls} onClick={() => addInnovation()} disabled={!iContractId || !iName.trim()}>
              {t('srm.create')}
            </button>
          </div>
          <label class="mt-2 block text-xs">
            {t('srm.description')}
            <textarea class="{inputCls} w-full" rows="2" bind:value={iDescription}></textarea>
          </label>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.ideaCode')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.name')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.potentialValue')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.status')}</th>
            </tr>
          </thead>
          <tbody>
            {#each innovations as i (i.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedInnoId === i.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectInnovation(i.id)}
              >
                <td class="py-1.5 pr-2 font-mono">{i.idea_code ?? `#${i.id}`}</td>
                <td class="py-1.5 pr-2 font-medium">{i.name}</td>
                <td class="py-1.5 pr-2">{i.supplier_name || supplierName(i.supplier_id)}</td>
                <td class="py-1.5 pr-2">${formatMoney(i.potential_financial_value)}</td>
                <td class="py-1.5 pr-2">
                  <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-700">{labelOf(INNOVATION_STATUSES, i.status)}</span>
                </td>
              </tr>
            {:else}
              <tr><td colspan="5" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if innoDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <div class="mb-3 flex items-center gap-2">
              <h3 class="text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
                {innoDetail.idea_code ?? `#${innoDetail.id}`} — {innoDetail.name}
              </h3>
              <select
                class={inputCls}
                value={innoDetail.status}
                onChange={(e) => setInnovationStatus(e.target.value)}
              >
                {#each INNOVATION_STATUSES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
              </select>
            </div>

            <p class="mb-3 whitespace-pre-wrap text-xs">{innoDetail.description}</p>

            <!-- Factors -->
            <h4 class="mb-1 mt-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              {t('srm.factors')} (1 = {t('srm.optimal')})
            </h4>
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1 pr-2 font-medium">{t('srm.evaluator')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.factor')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.rating')}</th>
                </tr>
              </thead>
              <tbody>
                {#each innoDetail.factors ?? [] as f (f.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1 pr-2">{f.evaluator_name}</td>
                    <td class="py-1 pr-2">{f.factor_name}{#if f.is_custom} *{/if}</td>
                    <td class="py-1 pr-2">{f.rating}/5</td>
                  </tr>
                {:else}
                  <tr><td colspan="3" class="py-2 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
                {/each}
              </tbody>
            </table>
            <div class="mt-2 flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.evaluator')}
                <input class={inputCls} bind:value={fEvaluator} />
              </label>
              <label class="text-xs">
                {t('srm.factor')}
                <input class={inputCls} bind:value={fFactor} />
              </label>
              <label class="text-xs">
                {t('srm.rating')}
                <select class={inputCls} bind:value={fRating}>
                  {#each [1, 2, 3, 4, 5] as n (n)}<option value={n}>{n}</option>{/each}
                </select>
              </label>
              <button type="button" class={btnCls} onClick={() => saveFactor()} disabled={!fFactor.trim()}>{t('srm.save')}</button>
            </div>

            <!-- Recommendation -->
            <h4 class="mb-1 mt-4 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.recommendation')}</h4>
            {#if innoDetail.recommendation}
              <p class="text-xs">
                <span class="font-medium">{t(RECOMMEND_DECISIONS.find((d) => d.value === innoDetail.recommendation.decision)?.key ?? '')}</span>
                — {innoDetail.recommendation.justification}
                {#if innoDetail.recommendation.priority}(priority: {innoDetail.recommendation.priority}){/if}
              </p>
            {/if}
            <div class="mt-2 flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.decision')}
                <select class={inputCls} bind:value={recDecision}>
                  {#each RECOMMEND_DECISIONS as d (d.value)}<option value={d.value}>{t(d.key)}</option>{/each}
                </select>
              </label>
              <label class="text-xs">
                {t('srm.priority')}
                <select class={inputCls} bind:value={recPriority}>
                  {#each PRIORITIES as p (p.value)}<option value={p.value}>{p.key ? t(p.key) : '—'}</option>{/each}
                </select>
              </label>
              <label class="text-xs">
                {t('srm.timing')}
                <input class={inputCls} bind:value={recTiming} />
              </label>
              <label class="text-xs">
                {t('srm.supplierChampion')}
                <input class={inputCls} bind:value={recSupplierChampion} />
              </label>
            </div>
            <label class="mt-2 block text-xs">
              {t('srm.justification')}
              <textarea class="{inputCls} w-full" rows="2" bind:value={recJustification}></textarea>
            </label>
            <button type="button" class={btnCls} onClick={() => saveRecommendation()} disabled={!recJustification.trim()}>
              {t('srm.save')}
            </button>

            <!-- Objectives -->
            <h4 class="mb-1 mt-4 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.objectives')}</h4>
            {#each innoDetail.objectives ?? [] as o (o.id)}
              {@const of = objForms[o.id] ?? { actual_outcome: '', realised_value: '' }}
              <div class="mb-2 flex flex-wrap items-end gap-2 rounded-md border border-[var(--color-ds-border,#e5e7eb)] p-2">
                <span class="w-full text-xs font-medium">{o.objective_text}</span>
                <label class="text-xs">
                  {t('srm.actualOutcome')}
                  <input class="{inputCls} w-64" bind:value={of.actual_outcome} />
                </label>
                <label class="text-xs">
                  {t('srm.realisedValue')} ($)
                  <input type="number" step="any" class={inputCls} bind:value={of.realised_value} />
                </label>
                <button type="button" class={btnCls} onClick={() => saveObjective(o.id)}>{t('srm.save')}</button>
              </div>
            {:else}
              <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</p>
            {/each}
            <div class="flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.objective')}
                <input class="{inputCls} w-96" bind:value={objText} />
              </label>
              <button type="button" class={btnCls} onClick={() => addObjective()} disabled={!objText.trim()}>{t('srm.create')}</button>
            </div>

            <!-- Tasks -->
            <h4 class="mb-1 mt-4 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.tasks')}</h4>
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1 pr-2 font-medium">#</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.name')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.side')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.deliverable')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.status')}</th>
                </tr>
              </thead>
              <tbody>
                {#each innoDetail.tasks ?? [] as tk (tk.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1 pr-2">{tk.task_no}</td>
                    <td class="py-1 pr-2">{tk.task_name}</td>
                    <td class="py-1 pr-2">{t(tk.side === 'agency' ? 'srm.sideAgency' : 'srm.sideSupplier')}</td>
                    <td class="py-1 pr-2">{tk.deliverable || '—'}</td>
                    <td class="py-1 pr-2">{tk.status}</td>
                  </tr>
                {:else}
                  <tr><td colspan="5" class="py-2 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
                {/each}
              </tbody>
            </table>
            <div class="mt-2 flex flex-wrap items-end gap-2">
              <label class="text-xs">
                #
                <input type="number" min="1" class={inputCls} bind:value={tNo} />
              </label>
              <label class="text-xs">
                {t('srm.name')}
                <input class="{inputCls} w-64" bind:value={tName} />
              </label>
              <label class="text-xs">
                {t('srm.side')}
                <select class={inputCls} bind:value={tSide}>
                  {#each SIDES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
                </select>
              </label>
              <button type="button" class={btnCls} onClick={() => addTask()} disabled={!tName.trim()}>{t('srm.create')}</button>
            </div>
          </div>
        {/if}
      </section>

      <!-- ================================ SIPS ============================= -->
    {:else if section === 'sips'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.serviceImprovementPlans')}</h2>
        {#if sipError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{sipError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.addSip')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.contract')}
              <select class={inputCls} bind:value={pContractId}>
                <option value="" disabled selected></option>
                {#each contractOptions as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.name')}
              <input class="{inputCls} w-64" bind:value={pTitle} />
            </label>
            <label class="text-xs">
              {t('srm.owner')}
              <select class={inputCls} bind:value={pOwner}>
                <option value="" disabled selected></option>
                {#each users as u (u.id)}<option value={u.id}>{u.display_name || u.username}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.subtype')}
              <select class={inputCls} bind:value={pSubtype}>
                {#each SIP_SUBTYPES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
              </select>
            </label>
          </div>
          <div class="mt-2 grid grid-cols-3 gap-2">
            <label class="text-xs">
              {t('srm.problemStatement')}
              <textarea class="{inputCls} w-full" rows="2" bind:value={pProblem}></textarea>
            </label>
            <label class="text-xs">
              {t('srm.successMeasure')} (SMART)
              <textarea class="{inputCls} w-full" rows="2" placeholder="e.g. Reduce defects by 30% by 30/6/2027" bind:value={pSmart}></textarea>
            </label>
            <label class="text-xs">
              {t('srm.exitCriteria')}
              <textarea class="{inputCls} w-full" rows="2" bind:value={pExit}></textarea>
            </label>
          </div>
          <button
            type="button"
            class={btnCls}
            onClick={() => addSip()}
            disabled={!pContractId || !pTitle.trim() || !pOwner || !pProblem.trim() || !pSmart.trim() || !pExit.trim()}
          >
            {t('srm.create')}
          </button>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.name')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.subtype')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.status')}</th>
            </tr>
          </thead>
          <tbody>
            {#each sips as p (p.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedSipId === p.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectSip(p.id)}
              >
                <td class="py-1.5 pr-2 font-medium">{p.title}</td>
                <td class="py-1.5 pr-2">{p.supplier_name || supplierName(p.supplier_id)}</td>
                <td class="py-1.5 pr-2">
                  {t(p.subtype === 'breach_driven' ? 'srm.subBreachDriven' : 'srm.subContinuousImprovement')}
                </td>
                <td class="py-1.5 pr-2">
                  <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-700">{labelOf(SIP_STATUSES, p.status)}</span>
                </td>
              </tr>
            {:else}
              <tr><td colspan="4" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if sipDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <div class="mb-3 flex items-center gap-2">
              <h3 class="text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{sipDetail.title}</h3>
              <select class={inputCls} value={sipDetail.status} onChange={(e) => setSipStatus(e.target.value)}>
                {#each SIP_STATUSES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
              </select>
            </div>
            <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
              <dt class="font-medium">{t('srm.problemStatement')}</dt><dd>{sipDetail.problem_statement}</dd>
              <dt class="font-medium">{t('srm.successMeasure')}</dt><dd>{sipDetail.success_measure_smart}</dd>
              <dt class="font-medium">{t('srm.exitCriteria')}</dt><dd>{sipDetail.exit_criteria}</dd>
            </dl>

            <h4 class="mb-1 mt-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.deliverables')}</h4>
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
                  <th class="py-1 pr-2 font-medium">#</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.deliverable')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.rag')}</th>
                  <th class="py-1 pr-2 font-medium">{t('srm.owner')}</th>
                </tr>
              </thead>
              <tbody>
                {#each sipDetail.deliverables ?? [] as d (d.id)}
                  <tr class="border-b border-[var(--color-ds-border,#e5e7eb)]">
                    <td class="py-1 pr-2">{d.seq}</td>
                    <td class="py-1 pr-2">{d.key_deliverable}</td>
                    <td class="py-1 pr-2"><span class="rounded-full px-2 py-0.5 text-[11px] font-medium {ragClass(d.rag)}">{d.rag}</span></td>
                    <td class="py-1 pr-2">{d.owner_name || '—'}</td>
                  </tr>
                {:else}
                  <tr><td colspan="4" class="py-2 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
                {/each}
              </tbody>
            </table>
            <div class="mt-2 flex flex-wrap items-end gap-2">
              <label class="text-xs">
                {t('srm.deliverable')}
                <input class="{inputCls} w-64" bind:value={dKey} />
              </label>
              <label class="text-xs">
                {t('srm.rag')}
                <select class={inputCls} bind:value={dRag}>
                  {#each RAGS as r (r.value)}<option value={r.value}>{r.label}</option>{/each}
                </select>
              </label>
              <label class="text-xs">
                {t('srm.owner')}
                <input class={inputCls} bind:value={dOwner} />
              </label>
              <button type="button" class={btnCls} onClick={() => addDeliverable()} disabled={!dKey.trim()}>{t('srm.create')}</button>
            </div>
          </div>
        {/if}
      </section>

      <!-- ============================== DISPUTES =========================== -->
    {:else if section === 'disputes'}
      <section class="mt-4">
        <h2 class="mb-2 text-sm font-semibold">{t('srm.disputes')}</h2>
        {#if dpError}
          <div class="mb-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{dpError}</div>
        {/if}

        <div class="rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.addDispute')}</h3>
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-xs">
              {t('srm.contract')}
              <select class={inputCls} bind:value={dpContractId}>
                <option value="" disabled selected></option>
                {#each contractOptions as c (c.id)}<option value={c.id}>{contractLabel(c)}</option>{/each}
              </select>
            </label>
            <label class="text-xs">
              {t('srm.description')}
              <input class="{inputCls} w-96" bind:value={dpDescription} />
            </label>
            <button type="button" class={btnCls} onClick={() => addDispute()} disabled={!dpContractId || !dpDescription.trim()}>
              {t('srm.create')}
            </button>
          </div>
        </div>

        <table class="mt-3 w-full text-left text-xs">
          <thead>
            <tr class="border-b border-[var(--color-ds-border,#e5e7eb)] text-[var(--color-ds-text-muted,#6b7280)]">
              <th class="py-1.5 pr-2 font-medium">{t('srm.supplier')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.description')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.stage')}</th>
              <th class="py-1.5 pr-2 font-medium">{t('srm.openedAt')}</th>
            </tr>
          </thead>
          <tbody>
            {#each disputes as d (d.id)}
              <tr
                class="cursor-pointer border-b border-[var(--color-ds-border,#e5e7eb)] hover:bg-ds-surface-hovered {selectedDpId === d.id ? 'bg-ds-surface-selected' : ''}"
                onClick={() => selectDispute(d.id)}
              >
                <td class="py-1.5 pr-2 font-medium">{d.supplier_name || supplierName(d.supplier_id)}</td>
                <td class="max-w-md truncate py-1.5 pr-2" title={d.description}>{d.description}</td>
                <td class="py-1.5 pr-2">
                  <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-700">{labelOf(DISPUTE_STAGES, d.stage)}</span>
                </td>
                <td class="py-1.5 pr-2">{formatDate(d.opened_at)}</td>
              </tr>
            {:else}
              <tr><td colspan="4" class="py-3 text-center text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</td></tr>
            {/each}
          </tbody>
        </table>

        {#if dpDetail}
          <div class="mt-4 rounded-md border border-[var(--color-ds-border,#e5e7eb)] bg-ds-surface-card p-4">
            <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">
              {dpDetail.supplier_name || supplierName(dpDetail.supplier_id)} — {t('srm.stage')}: {labelOf(DISPUTE_STAGES, dpDetail.stage)}
            </h3>
            <p class="mb-3 whitespace-pre-wrap text-xs">{dpDetail.description}</p>

            <!-- Escalations -->
            <h4 class="mb-1 mt-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.escalations')}</h4>
            {#each dpDetail.escalations ?? [] as e (e.id)}
              <p class="text-xs">
                {formatDate(e.escalated_at)}: {labelOf(DISPUTE_STAGES, e.from_stage)} → {labelOf(DISPUTE_STAGES, e.to_stage)}
                {#if e.escalated_to}(→ {e.escalated_to}){/if}
              </p>
            {:else}
              <p class="text-xs text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.noRecords')}</p>
            {/each}
            {#if dpDetail.stage !== 'closed'}
              <div class="mt-2 flex flex-wrap items-end gap-2">
                <label class="text-xs">
                  {t('srm.toStage')}
                  <select class={inputCls} bind:value={eToStage}>
                    {#each DISPUTE_STAGES.filter((x) => x.value !== dpDetail.stage) as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
                  </select>
                </label>
                <label class="text-xs">
                  {t('srm.escalatedTo')}
                  <input class={inputCls} bind:value={eEscalatedTo} />
                </label>
                <label class="text-xs">
                  {t('srm.notes')}
                  <input class="{inputCls} w-64" bind:value={eNotes} />
                </label>
                <button type="button" class={btnCls} onClick={() => addEscalation()}>{t('srm.create')}</button>
              </div>
            {/if}

            <!-- Remedy plan -->
            <h4 class="mb-1 mt-4 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.remedyPlan')}</h4>
            {#if dpDetail.remedy_plan}
              <p class="text-xs">
                <span class="font-medium">{dpDetail.remedy_plan.status}</span> — {dpDetail.remedy_plan.non_performance_issue}
              </p>
            {/if}
            <div class="mt-2 grid grid-cols-2 gap-2">
              <label class="text-xs">
                {t('srm.nonPerformanceIssue')}
                <textarea class="{inputCls} w-full" rows="2" bind:value={rpForm.non_performance_issue}></textarea>
              </label>
              <label class="text-xs">
                {t('srm.rootCause')}
                <textarea class="{inputCls} w-full" rows="2" bind:value={rpForm.root_cause}></textarea>
              </label>
              <label class="col-span-2 text-xs">
                {t('srm.correctiveAction')}
                <textarea class="{inputCls} w-full" rows="2" bind:value={rpForm.corrective_action}></textarea>
              </label>
              <label class="text-xs">
                {t('srm.targetResolutionDate')}
                <input type="date" class={inputCls} bind:value={rpForm.target_date} />
              </label>
            </div>

            <h5 class="mb-1 mt-3 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-ds-text-muted,#6b7280)]">{t('srm.timetable')}</h5>
            {#each timetableRows as row, idx (idx)}
              <div class="mb-1 flex items-end gap-2">
                <label class="flex-1 text-xs">
                  {t('srm.action')}
                  <input class="{inputCls} w-full" bind:value={row.action} />
                </label>
                <label class="text-xs">
                  {t('srm.side')}
                  <select class={inputCls} bind:value={row.owner_side}>
                    {#each SIDES as s (s.value)}<option value={s.value}>{t(s.key)}</option>{/each}
                  </select>
                </label>
                <label class="text-xs">
                  {t('srm.dueDate')}
                  <input type="date" class={inputCls} bind:value={row.due_date} />
                </label>
                <button type="button" class="cursor-pointer text-xs text-red-600 hover:underline" onClick={() => timetableRows.splice(idx, 1)}>
                  {t('srm.delete')}
                </button>
              </div>
            {/each}
            <div class="flex items-end gap-2">
              <button type="button" class="cursor-pointer text-xs hover:underline" onClick={() => addTimetableRow()}>{t('srm.addTimetableRow')}</button>
              <button
                type="button"
                class={btnCls}
                onClick={() => saveRemedyPlan()}
                disabled={!rpForm.non_performance_issue.trim() || !rpForm.corrective_action.trim() || !rpForm.target_date}
              >
                {t('srm.save')}
              </button>
            </div>
          </div>
        {/if}
      </section>
    {/if}
  {/if}
</div>
