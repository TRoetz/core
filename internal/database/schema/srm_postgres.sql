-- SRM (Supplier Relationship Management) module tables.
-- Implements the NZ Government Procurement SRM Strategy Wheel: four phases
-- (preparation, planning, implementation, monitoring), twelve steps, phase
-- gates on supplier relationships, RACI routing and the dispute ladder.
--
-- Conventions follow p3m.sql: INTEGER autoincrement PKs, TEXT + CHECK for
-- enum columns, REAL for money/scores, polymorphic (entity_type, entity_id)
-- pairs for P3M bindings (application-level FK checks), JSON payloads as TEXT.

-- ---------------------------------------------------------------------------
-- 1. SUPPLIERS (master directory)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_suppliers (
	id SERIAL PRIMARY KEY,
	company_name TEXT NOT NULL,
	nzbn TEXT UNIQUE CHECK (nzbn IS NULL OR nzbn ~ '^[0-9]{9}$'),
	tax_id_number TEXT UNIQUE,
	website TEXT DEFAULT '',
	tier TEXT NOT NULL DEFAULT 'tier_3' CHECK (tier IN ('tier_1', 'tier_2', 'tier_3', 'tier_4')),
	relationship_state TEXT NOT NULL DEFAULT 'registered' CHECK (relationship_state IN ('registered', 'segmented', 'planning_ready', 'implementation_ready', 'kicked_off', 'jbp_formalised', 'monitoring_active', 'closed')),
	is_active BOOLEAN NOT NULL DEFAULT true,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_supplier_contacts (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	full_name TEXT NOT NULL,
	role_title TEXT DEFAULT '',
	email TEXT NOT NULL,
	phone TEXT DEFAULT '',
	is_primary BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 2. CONTRACT REGISTER (Phase 1 / Step 1.1 — Rule 34 fields)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_contracts (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE RESTRICT,
	p3m_entity_type TEXT CHECK (p3m_entity_type IS NULL OR p3m_entity_type IN ('portfolio', 'programme', 'project')),
	p3m_entity_id INTEGER,
	contract_number TEXT UNIQUE NOT NULL,
	title TEXT NOT NULL,
	category TEXT DEFAULT '',
	classification TEXT DEFAULT '',
	scope_summary TEXT DEFAULT '',
	document_url TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'in_wind_up', 'retired', 'terminated')),
	total_value REAL NOT NULL DEFAULT 0 CHECK (total_value >= 0),
	currency TEXT NOT NULL DEFAULT 'NZD',
	committed_spend REAL NOT NULL DEFAULT 0,
	actual_spend REAL NOT NULL DEFAULT 0,
	spend_source TEXT NOT NULL DEFAULT 'manual' CHECK (spend_source IN ('manual', 'erp_api')),
	start_date DATE NOT NULL,
	end_date DATE NOT NULL CHECK (end_date > start_date),
	extension_count INTEGER NOT NULL DEFAULT 0 CHECK (extension_count >= 0),
	notice_period_days INTEGER CHECK (notice_period_days IS NULL OR notice_period_days > 0),
	review_date DATE,
	cost_code TEXT DEFAULT '',
	po_number TEXT DEFAULT '',
	termination_reason TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_contract_assignments (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id),
	role TEXT NOT NULL CHECK (role IN ('internal_contract_manager', 'contract_owner', 'budget_approver')),
	assigned_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (contract_id, role, user_id)
);

CREATE TABLE IF NOT EXISTS srm_contract_documents (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	doc_type TEXT NOT NULL CHECK (doc_type IN ('contract', 'variation', 'sla_schedule', 'insurance', 'other')),
	file_name TEXT NOT NULL,
	storage_path TEXT NOT NULL DEFAULT '',
	version INTEGER NOT NULL DEFAULT 1,
	effective_date DATE,
	uploaded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_clause_extractions (
	id SERIAL PRIMARY KEY,
	document_id INTEGER NOT NULL REFERENCES srm_contract_documents(id) ON DELETE CASCADE,
	field_name TEXT NOT NULL,
	proposed_value TEXT NOT NULL,
	confidence REAL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
	reviewed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	reviewed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS srm_contract_management_plans (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	version INTEGER NOT NULL DEFAULT 1,
	right_size_tier TEXT NOT NULL CHECK (right_size_tier IN ('minimal', 'essential', 'standard', 'comprehensive')),
	objectives TEXT DEFAULT '',
	key_terms_summary TEXT DEFAULT '',
	governance_arrangements TEXT NOT NULL,
	monitoring_provisions TEXT NOT NULL,
	change_reason TEXT DEFAULT '',
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (contract_id, version)
);

-- ---------------------------------------------------------------------------
-- 3. SEGMENTATION ENGINE (Phase 1 / Step 1.2 — category, supplier and lite tools)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_categories (
	id SERIAL PRIMARY KEY,
	name TEXT UNIQUE NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_segmentation_runs (
	id SERIAL PRIMARY KEY,
	scope TEXT NOT NULL CHECK (scope IN ('category', 'supplier', 'lite')),
	category_id INTEGER REFERENCES srm_categories(id) ON DELETE SET NULL,
	created_by INTEGER NOT NULL REFERENCES users(id),
	status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'complete', 'validated', 'superseded')),
	notes TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	validated_at TIMESTAMPTZ
);

-- One row per answered question. question_code keys the official question sets;
-- weighted_score is computed server-side as score * weight at write time.
CREATE TABLE IF NOT EXISTS srm_segmentation_answers (
	id SERIAL PRIMARY KEY,
	run_id INTEGER NOT NULL REFERENCES srm_segmentation_runs(id) ON DELETE CASCADE,
	entity_ref INTEGER NOT NULL,
	question_code TEXT NOT NULL,
	score INTEGER NOT NULL CHECK (score BETWEEN 0 AND 3),
	anchor_label TEXT NOT NULL DEFAULT '',
	weight REAL NOT NULL,
	weighted_score REAL NOT NULL DEFAULT 0,
	justification TEXT DEFAULT '',
	scored_by INTEGER NOT NULL REFERENCES users(id),
	scored_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (run_id, entity_ref, question_code)
);

CREATE TABLE IF NOT EXISTS srm_segmentation_results (
	id SERIAL PRIMARY KEY,
	run_id INTEGER NOT NULL REFERENCES srm_segmentation_runs(id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('category', 'supplier')),
	entity_ref INTEGER NOT NULL,
	criticality_score REAL,
	value_score REAL,
	spend_amount REAL,
	rank_criticality TEXT CHECK (rank_criticality IS NULL OR rank_criticality IN ('high', 'medium', 'low')),
	rank_value TEXT CHECK (rank_value IS NULL OR rank_value IN ('high', 'medium', 'low')),
	segmentation_box TEXT DEFAULT '',
	box_number INTEGER CHECK (box_number IS NULL OR box_number BETWEEN 1 AND 9),
	mapped_tier TEXT CHECK (mapped_tier IS NULL OR mapped_tier IN ('tier_1', 'tier_2', 'tier_3', 'tier_4')),
	UNIQUE (run_id, entity_type, entity_ref)
);

-- ---------------------------------------------------------------------------
-- 4. VALIDATION (Phase 1 / Step 1.3)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_validation_requests (
	id SERIAL PRIMARY KEY,
	run_id INTEGER NOT NULL REFERENCES srm_segmentation_runs(id),
	supplier_id INTEGER REFERENCES srm_suppliers(id) ON DELETE SET NULL,
	requested_by INTEGER NOT NULL REFERENCES users(id),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'changes_requested', 'approved', 'rejected')),
	cycle_count INTEGER NOT NULL DEFAULT 1 CHECK (cycle_count BETWEEN 1 AND 3),
	signoff_method TEXT CHECK (signoff_method IS NULL OR signoff_method IN ('in_app', 'email')),
	signed_off_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	signed_off_at TIMESTAMPTZ,
	comment TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_scoring_adjustments (
	id SERIAL PRIMARY KEY,
	request_id INTEGER NOT NULL REFERENCES srm_validation_requests(id) ON DELETE CASCADE,
	run_id INTEGER NOT NULL REFERENCES srm_segmentation_runs(id),
	entity_ref INTEGER NOT NULL,
	question_code TEXT NOT NULL,
	old_score INTEGER,
	new_score INTEGER,
	changed_by INTEGER NOT NULL REFERENCES users(id),
	reason TEXT NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 5. VALUE PROPOSITIONS (Phase 2 / Step 2.1)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_value_propositions (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	contract_id INTEGER REFERENCES srm_contracts(id) ON DELETE SET NULL,
	agency_objective TEXT NOT NULL,
	supplier_service_product TEXT DEFAULT '',
	srm_ambition TEXT DEFAULT '',
	benefit_type TEXT NOT NULL CHECK (benefit_type IN ('financial_saving', 'operational_efficiency', 'innovation_capture', 'risk_compliance_reduction', 'other')),
	anticipated_benefit TEXT DEFAULT '',
	estimated_value_nzd REAL,
	value_confidence TEXT CHECK (value_confidence IS NULL OR value_confidence IN ('high', 'medium', 'low')),
	measurement_method TEXT DEFAULT '',
	engagement_frequency TEXT DEFAULT '',
	p3m_benefit_ref TEXT DEFAULT '',
	actual_value_nzd REAL,
	supplier_strategic_objective TEXT DEFAULT '',
	opportunity_statement TEXT DEFAULT '',
	srm_specific_ambition TEXT DEFAULT '',
	anticipated_supplier_benefits TEXT DEFAULT '',
	signed_off_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	signed_off_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 6. STAKEHOLDERS & ENGAGEMENT PLANNING (Phase 2 / Step 2.2)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_stakeholders (
	id SERIAL PRIMARY KEY,
	full_name TEXT NOT NULL,
	email TEXT UNIQUE NOT NULL,
	organization TEXT DEFAULT '',
	job_title TEXT DEFAULT '',
	business_area TEXT DEFAULT '',
	is_external BOOLEAN NOT NULL DEFAULT false,
	supplier_id INTEGER REFERENCES srm_suppliers(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Polymorphic binding to portfolio/programme/project/contract. Influence and
-- support bands, the 3x3 quadrant and the salience class are derived
-- server-side from the stored scores (see handlers/srm_planning.go).
CREATE TABLE IF NOT EXISTS srm_entity_stakeholders (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES srm_stakeholders(id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project', 'contract')),
	entity_id INTEGER NOT NULL,
	role_description TEXT DEFAULT '',
	power_score INTEGER NOT NULL DEFAULT 3 CHECK (power_score BETWEEN 1 AND 5),
	interest_score INTEGER NOT NULL DEFAULT 3 CHECK (interest_score BETWEEN 1 AND 5),
	support_band TEXT NOT NULL DEFAULT 'on_the_fence' CHECK (support_band IN ('sceptic', 'on_the_fence', 'ambassador')),
	sentiment_score INTEGER NOT NULL DEFAULT 0 CHECK (sentiment_score BETWEEN -2 AND 2),
	legitimacy_score INTEGER CHECK (legitimacy_score IS NULL OR legitimacy_score BETWEEN 1 AND 5),
	urgency_score INTEGER CHECK (urgency_score IS NULL OR urgency_score BETWEEN 1 AND 5),
	current_engagement TEXT NOT NULL DEFAULT 'neutral' CHECK (current_engagement IN ('unaware', 'resistant', 'neutral', 'supportive', 'champion')),
	target_engagement TEXT NOT NULL DEFAULT 'supportive' CHECK (target_engagement IN ('unaware', 'resistant', 'neutral', 'supportive', 'champion')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_stakeholder_phase_snapshots (
	id SERIAL PRIMARY KEY,
	entity_stakeholder_id INTEGER NOT NULL REFERENCES srm_entity_stakeholders(id) ON DELETE CASCADE,
	contract_phase TEXT NOT NULL CHECK (contract_phase IN ('preparation', 'planning', 'implementation', 'monitoring')),
	power_score INTEGER NOT NULL CHECK (power_score BETWEEN 1 AND 5),
	interest_score INTEGER NOT NULL CHECK (interest_score BETWEEN 1 AND 5),
	sentiment_score INTEGER NOT NULL CHECK (sentiment_score BETWEEN -2 AND 2),
	recorded_by INTEGER NOT NULL REFERENCES users(id),
	recorded_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_communications_plans (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	scope TEXT NOT NULL DEFAULT 'all' CHECK (scope IN ('all', 'internal', 'external')),
	is_complete BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_comms_messages (
	id SERIAL PRIMARY KEY,
	plan_id INTEGER NOT NULL REFERENCES srm_communications_plans(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES srm_stakeholders(id),
	communication_message TEXT NOT NULL,
	method TEXT NOT NULL DEFAULT '',
	frequency TEXT NOT NULL DEFAULT '',
	communicator_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	signed_off BOOLEAN NOT NULL DEFAULT false,
	signed_off_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	signed_off_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 7. RACI ENGINE (Phase 2 / Step 2.3)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_racis (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	version INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'preliminary', 'under_discussion', 'agreed')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	agreed_at TIMESTAMPTZ,
	last_reviewed_at DATE,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (supplier_id, version)
);

-- Accountable must resolve to a person: enforced server-side on save.
CREATE TABLE IF NOT EXISTS srm_raci_assignments (
	id SERIAL PRIMARY KEY,
	raci_id INTEGER NOT NULL REFERENCES srm_racis(id) ON DELETE CASCADE,
	activity_code TEXT NOT NULL,
	side TEXT NOT NULL CHECK (side IN ('agency', 'supplier')),
	role_name TEXT NOT NULL DEFAULT '',
	user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	value TEXT NOT NULL CHECK (value IN ('r', 'a', 'c', 'i'))
);

-- ---------------------------------------------------------------------------
-- 8. MEETINGS ENGINE (Phase 3 — kick-off, workshops, QBRs, reviews)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_meetings (
	id SERIAL PRIMARY KEY,
	meeting_type TEXT NOT NULL CHECK (meeting_type IN ('internal_kickoff', 'supplier_kickoff', 'jbp_workshop', 'operational_performance', 'relationship_review', 'strategic_relationship', 'qbr', 'other')),
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	contract_id INTEGER REFERENCES srm_contracts(id) ON DELETE SET NULL,
	organizer_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	scheduled_at TIMESTAMPTZ NOT NULL,
	location TEXT DEFAULT '',
	agenda_json TEXT NOT NULL DEFAULT '[]',
	outcomes_json TEXT NOT NULL DEFAULT '[]',
	minutes_text TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'held', 'cancelled')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_meeting_attendees (
	id SERIAL PRIMARY KEY,
	meeting_id INTEGER NOT NULL REFERENCES srm_meetings(id) ON DELETE CASCADE,
	stakeholder_id INTEGER REFERENCES srm_stakeholders(id) ON DELETE SET NULL,
	user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	side TEXT NOT NULL CHECK (side IN ('agency', 'supplier')),
	role_description TEXT DEFAULT '',
	attended BOOLEAN
);

CREATE TABLE IF NOT EXISTS srm_meeting_actions (
	id SERIAL PRIMARY KEY,
	meeting_id INTEGER NOT NULL REFERENCES srm_meetings(id) ON DELETE CASCADE,
	agenda_item_no INTEGER,
	kind TEXT NOT NULL CHECK (kind IN ('note', 'question', 'action')),
	text TEXT NOT NULL,
	owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	due_date DATE,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed'))
);

-- ---------------------------------------------------------------------------
-- 9. JOINT BUSINESS PLANS (Phase 3 / Steps 3.2-3.3)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_jbps (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id),
	version INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'in_review', 'signed', 'superseded')),
	purpose TEXT DEFAULT '',
	vision TEXT DEFAULT '',
	scope TEXT DEFAULT '',
	governance_model TEXT DEFAULT '',
	review_cycle_months INTEGER NOT NULL DEFAULT 3 CHECK (review_cycle_months > 0),
	relationship_charter TEXT NOT NULL DEFAULT '[]',
	contract_summary TEXT DEFAULT '',
	spend_summary REAL,
	performance_notes TEXT DEFAULT '',
	risk_action_plan TEXT DEFAULT '',
	relationship_development TEXT DEFAULT '',
	value_opportunities TEXT NOT NULL DEFAULT '[]',
	agency_rep_name TEXT DEFAULT '',
	agency_rep_role TEXT DEFAULT '',
	agency_signed_at TIMESTAMPTZ,
	supplier_rep_name TEXT DEFAULT '',
	supplier_rep_role TEXT DEFAULT '',
	supplier_signed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (contract_id, version)
);

CREATE TABLE IF NOT EXISTS srm_jbp_initiatives (
	id SERIAL PRIMARY KEY,
	jbp_id INTEGER NOT NULL REFERENCES srm_jbps(id) ON DELETE CASCADE,
	quarter_label TEXT NOT NULL DEFAULT '',
	part TEXT NOT NULL CHECK (part IN ('park', 'just_do', 'strategic_initiative', 'top_priority')),
	initiative_name TEXT NOT NULL,
	activities TEXT DEFAULT '',
	key_dates TEXT DEFAULT '[]',
	owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	comment TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'not_started' CHECK (status IN ('not_started', 'in_progress', 'on_hold', 'complete')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 10. MONITORING — METRICS, SCORECARDS, SURVEYS (Phase 4 / Step 4.1)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_performance_metrics (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	type TEXT NOT NULL CHECK (type IN ('sla', 'kpi')),
	unit TEXT DEFAULT '',
	target_value REAL NOT NULL,
	direction TEXT NOT NULL DEFAULT 'above' CHECK (direction IN ('above', 'below')),
	frequency TEXT NOT NULL DEFAULT 'monthly' CHECK (frequency IN ('monthly', 'quarterly')),
	data_source TEXT NOT NULL DEFAULT 'manual' CHECK (data_source IN ('manual', 'erp_api', 'supplier_reported')),
	owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	is_active BOOLEAN NOT NULL DEFAULT true,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- achieved is computed at write time: direction='above' -> actual >= target.
CREATE TABLE IF NOT EXISTS srm_metric_results (
	id SERIAL PRIMARY KEY,
	metric_id INTEGER NOT NULL REFERENCES srm_performance_metrics(id) ON DELETE CASCADE,
	period_start DATE NOT NULL,
	actual_value REAL NOT NULL,
	achieved BOOLEAN NOT NULL DEFAULT false,
	recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	recorded_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (metric_id, period_start)
);

CREATE TABLE IF NOT EXISTS srm_scorecards (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id),
	period_start DATE NOT NULL,
	period_end DATE NOT NULL,
	completed_collaboratively BOOLEAN NOT NULL DEFAULT false,
	overall_score INTEGER CHECK (overall_score IS NULL OR overall_score BETWEEN 1 AND 5),
	overall_reasoning TEXT DEFAULT '',
	agency_completed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	supplier_completed_by_name TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- A supplier RAG mismatch requires a joint resolution note: enforced in the DB.
CREATE TABLE IF NOT EXISTS srm_scorecard_dimensions (
	id SERIAL PRIMARY KEY,
	scorecard_id INTEGER NOT NULL REFERENCES srm_scorecards(id) ON DELETE CASCADE,
	dimension TEXT NOT NULL CHECK (dimension IN ('risk_summary', 'initiatives_summary', 'performance_summary', 'benefits_delivered', 'governance_summary')),
	main_highlights TEXT DEFAULT '',
	key_concerns TEXT DEFAULT '',
	rag TEXT NOT NULL DEFAULT 'green' CHECK (rag IN ('green', 'amber', 'red')),
	next_steps TEXT DEFAULT '',
	supplier_rag TEXT CHECK (supplier_rag IS NULL OR supplier_rag IN ('green', 'amber', 'red')),
	joint_resolution_note TEXT DEFAULT '',
	CHECK (supplier_rag IS NULL OR supplier_rag = rag OR joint_resolution_note <> ''),
	UNIQUE (scorecard_id, dimension)
);

CREATE TABLE IF NOT EXISTS srm_pulse_surveys (
	id SERIAL PRIMARY KEY,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id) ON DELETE CASCADE,
	direction TEXT NOT NULL CHECK (direction IN ('internal_rates_supplier', 'supplier_rates_agency')),
	template_version INTEGER NOT NULL DEFAULT 1,
	sent_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	due_date DATE,
	responses_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS srm_survey_responses (
	id SERIAL PRIMARY KEY,
	survey_id INTEGER NOT NULL REFERENCES srm_pulse_surveys(id) ON DELETE CASCADE,
	stakeholder_id INTEGER REFERENCES srm_stakeholders(id) ON DELETE SET NULL,
	is_anonymous BOOLEAN NOT NULL DEFAULT false,
	answers_json TEXT NOT NULL DEFAULT '{}',
	overall_score INTEGER CHECK (overall_score IS NULL OR overall_score BETWEEN 1 AND 5),
	responded_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 11. INNOVATION REGISTER (Phase 4 / Step 4.2 — Capture, Evaluation, Implementation)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_innovations (
	id SERIAL PRIMARY KEY,
	idea_code TEXT UNIQUE,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id),
	name TEXT NOT NULL,
	description TEXT DEFAULT '',
	idea_date DATE NOT NULL DEFAULT now(),
	supplier_team TEXT DEFAULT '',
	agency_team TEXT DEFAULT '',
	source TEXT NOT NULL DEFAULT 'other' CHECK (source IN ('supplier_submission', 'agency_observation', 'jbp_workshop', 'scorecard_review', 'other')),
	status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'under_review', 'proceed', 'reject', 'further_investigate', 'implementing', 'implemented')),
	potential_financial_value REAL NOT NULL DEFAULT 0,
	realised_financial_value REAL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Evaluation factors are rated 1-5 where 1 = optimal (official inversion).
CREATE TABLE IF NOT EXISTS srm_innovation_factors (
	id SERIAL PRIMARY KEY,
	innovation_id INTEGER NOT NULL REFERENCES srm_innovations(id) ON DELETE CASCADE,
	evaluator_name TEXT DEFAULT '',
	factor_name TEXT NOT NULL,
	is_custom BOOLEAN NOT NULL DEFAULT false,
	rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5)
);

CREATE TABLE IF NOT EXISTS srm_innovation_recommendations (
	id SERIAL PRIMARY KEY,
	innovation_id INTEGER NOT NULL UNIQUE REFERENCES srm_innovations(id) ON DELETE CASCADE,
	decision TEXT NOT NULL CHECK (decision IN ('proceed', 'reject', 'further_investigate')),
	justification TEXT NOT NULL,
	priority TEXT CHECK (priority IS NULL OR priority IN ('high', 'medium', 'low')),
	timing TEXT DEFAULT '',
	agency_champion_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	supplier_champion_name TEXT DEFAULT '',
	decided_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_innovation_objectives (
	id SERIAL PRIMARY KEY,
	innovation_id INTEGER NOT NULL REFERENCES srm_innovations(id) ON DELETE CASCADE,
	objective_text TEXT NOT NULL,
	actual_outcome TEXT DEFAULT '',
	realised_value_nzd REAL
);

CREATE TABLE IF NOT EXISTS srm_innovation_tasks (
	id SERIAL PRIMARY KEY,
	innovation_id INTEGER NOT NULL REFERENCES srm_innovations(id) ON DELETE CASCADE,
	task_no INTEGER NOT NULL,
	task_name TEXT NOT NULL,
	description TEXT DEFAULT '',
	deliverable TEXT DEFAULT '',
	owner_name TEXT DEFAULT '',
	side TEXT NOT NULL CHECK (side IN ('agency', 'supplier')),
	start_date DATE,
	end_date DATE,
	status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'awaiting_approval', 'sign_off_stage', 'complete')),
	UNIQUE (innovation_id, task_no)
);

-- ---------------------------------------------------------------------------
-- 12. SERVICE IMPROVEMENT PLANS & DISPUTES (Phase 4 / Step 4.3)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srm_service_improvement_plans (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id),
	title TEXT NOT NULL,
	sip_owner_id INTEGER NOT NULL REFERENCES users(id),
	problem_statement TEXT NOT NULL,
	subtype TEXT NOT NULL DEFAULT 'breach_driven' CHECK (subtype IN ('breach_driven', 'continuous_improvement')),
	success_measure_smart TEXT NOT NULL,
	exit_criteria TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'on_track_review', 'resolved', 'closed_unresolved')),
	source_ref TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS srm_sip_deliverables (
	id SERIAL PRIMARY KEY,
	sip_id INTEGER NOT NULL REFERENCES srm_service_improvement_plans(id) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	key_deliverable TEXT NOT NULL,
	completion_date DATE,
	rag TEXT NOT NULL DEFAULT 'green' CHECK (rag IN ('green', 'amber', 'red')),
	owner_name TEXT DEFAULT '',
	comments_risks_issues TEXT DEFAULT '',
	UNIQUE (sip_id, seq)
);

CREATE TABLE IF NOT EXISTS srm_disputes (
	id SERIAL PRIMARY KEY,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	supplier_id INTEGER NOT NULL REFERENCES srm_suppliers(id),
	description TEXT NOT NULL,
	stage TEXT NOT NULL DEFAULT 'l1_working_level' CHECK (stage IN ('l1_working_level', 'l2_director', 'l3_executive', 'mediation', 'litigation', 'closed')),
	opened_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	resolved_at TIMESTAMPTZ,
	outcome_note TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS srm_dispute_escalations (
	id SERIAL PRIMARY KEY,
	dispute_id INTEGER NOT NULL REFERENCES srm_disputes(id) ON DELETE CASCADE,
	from_stage TEXT NOT NULL CHECK (from_stage IN ('l1_working_level', 'l2_director', 'l3_executive', 'mediation', 'litigation', 'closed')),
	to_stage TEXT NOT NULL CHECK (to_stage IN ('l1_working_level', 'l2_director', 'l3_executive', 'mediation', 'litigation', 'closed')),
	escalated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	escalated_to TEXT DEFAULT '',
	pause_reason TEXT DEFAULT '',
	notes TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS srm_remedy_plans (
	id SERIAL PRIMARY KEY,
	dispute_id INTEGER REFERENCES srm_disputes(id) ON DELETE CASCADE,
	sip_id INTEGER REFERENCES srm_service_improvement_plans(id) ON DELETE CASCADE,
	contract_id INTEGER NOT NULL REFERENCES srm_contracts(id) ON DELETE CASCADE,
	non_performance_issue TEXT NOT NULL,
	root_cause TEXT DEFAULT '',
	corrective_action_required TEXT NOT NULL,
	timetable_json TEXT NOT NULL DEFAULT '[]',
	target_resolution_date DATE NOT NULL,
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'resolved', 'breached', 'escalated')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	CHECK (dispute_id IS NOT NULL OR sip_id IS NOT NULL)
);

-- ---------------------------------------------------------------------------
-- Indexes
-- ---------------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_srm_contacts_supplier ON srm_supplier_contacts(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_contracts_supplier ON srm_contracts(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_contracts_p3m ON srm_contracts(p3m_entity_type, p3m_entity_id);
CREATE INDEX IF NOT EXISTS idx_srm_contracts_dates ON srm_contracts(start_date, end_date);
CREATE INDEX IF NOT EXISTS idx_srm_assignments_contract ON srm_contract_assignments(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_docs_contract ON srm_contract_documents(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_extractions_document ON srm_clause_extractions(document_id);
CREATE INDEX IF NOT EXISTS idx_smgmt_plans_contract ON srm_contract_management_plans(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_runs_category ON srm_segmentation_runs(category_id);
CREATE INDEX IF NOT EXISTS idx_srm_answers_run ON srm_segmentation_answers(run_id, entity_ref);
CREATE INDEX IF NOT EXISTS idx_srm_results_run ON srm_segmentation_results(run_id, entity_ref);
CREATE INDEX IF NOT EXISTS idx_srm_validation_run ON srm_validation_requests(run_id);
CREATE INDEX IF NOT EXISTS idx_srm_validation_supplier ON srm_validation_requests(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_adjustments_request ON srm_scoring_adjustments(request_id);
CREATE INDEX IF NOT EXISTS idx_srm_vps_supplier ON srm_value_propositions(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_stakeholders_supplier ON srm_stakeholders(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_esh_entity ON srm_entity_stakeholders(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_srm_snapshots_esh ON srm_stakeholder_phase_snapshots(entity_stakeholder_id);
CREATE INDEX IF NOT EXISTS idx_srm_comms_plans_supplier ON srm_communications_plans(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_comms_messages_plan ON srm_comms_messages(plan_id);
CREATE INDEX IF NOT EXISTS idx_srm_racis_supplier ON srm_racis(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_raci_assign ON srm_raci_assignments(raci_id, activity_code);
CREATE INDEX IF NOT EXISTS idx_srm_meetings_supplier ON srm_meetings(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_attendees_meeting ON srm_meeting_attendees(meeting_id);
CREATE INDEX IF NOT EXISTS idx_srm_actions_meeting ON srm_meeting_actions(meeting_id);
CREATE INDEX IF NOT EXISTS idx_srm_jbps_contract ON srm_jbps(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_jbp_init ON srm_jbp_initiatives(jbp_id, quarter_label);
CREATE INDEX IF NOT EXISTS idx_srm_metrics_contract ON srm_performance_metrics(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_metric_results_metric ON srm_metric_results(metric_id, period_start);
CREATE INDEX IF NOT EXISTS idx_srm_scorecards_supplier ON srm_scorecards(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_dimensions_card ON srm_scorecard_dimensions(scorecard_id);
CREATE INDEX IF NOT EXISTS idx_srm_surveys_supplier ON srm_pulse_surveys(supplier_id);
CREATE INDEX IF NOT EXISTS idx_srm_responses_survey ON srm_survey_responses(survey_id);
CREATE INDEX IF NOT EXISTS idx_srm_innovations_contract ON srm_innovations(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_factors_innovation ON srm_innovation_factors(innovation_id);
CREATE INDEX IF NOT EXISTS idx_srm_objectives_innovation ON srm_innovation_objectives(innovation_id);
CREATE INDEX IF NOT EXISTS idx_srm_tasks_innovation ON srm_innovation_tasks(innovation_id, task_no);
CREATE INDEX IF NOT EXISTS idx_srm_sips_contract ON srm_service_improvement_plans(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_deliverables_sip ON srm_sip_deliverables(sip_id);
CREATE INDEX IF NOT EXISTS idx_srm_disputes_contract ON srm_disputes(contract_id);
CREATE INDEX IF NOT EXISTS idx_srm_escalations_dispute ON srm_dispute_escalations(dispute_id);
CREATE INDEX IF NOT EXISTS idx_srm_remedies_contract ON srm_remedy_plans(contract_id);
