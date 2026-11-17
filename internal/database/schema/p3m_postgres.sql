-- P3M (Portfolio, Programme, Project Management) module tables.

CREATE TABLE IF NOT EXISTS p3m_portfolios (
	id SERIAL PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	strategic_objective TEXT DEFAULT '',
	budget_allocated REAL NOT NULL DEFAULT 0,
	rag_status TEXT NOT NULL DEFAULT 'green' CHECK (rag_status IN ('red', 'amber', 'green')),
	owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_programmes (
	id SERIAL PRIMARY KEY,
	portfolio_id INTEGER NOT NULL REFERENCES p3m_portfolios(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	manager_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	budget_allocated REAL NOT NULL DEFAULT 0,
	target_start_date DATE,
	target_end_date DATE,
	rag_status TEXT NOT NULL DEFAULT 'green' CHECK (rag_status IN ('red', 'amber', 'green')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_projects (
	id SERIAL PRIMARY KEY,
	portfolio_id INTEGER REFERENCES p3m_portfolios(id) ON DELETE CASCADE,
	programme_id INTEGER REFERENCES p3m_programmes(id) ON DELETE SET NULL,
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	manager_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	-- Optional link to the core Time & Projects entry for this project. Lets
	-- worklog financials roll up into P3M/SRM per-project tracking.
	time_project_id INTEGER REFERENCES time_projects(id) ON DELETE SET NULL,
	-- Work type drives proportional gate paths (BRD §5.5); external_ref is the
	-- unique finance-system reference used by FIN-007 CSV sync matching.
	work_type TEXT,
	external_ref TEXT UNIQUE,
	-- Funding source (FIN-002/006) and contingency reserve (FIN-010).
	funding_source TEXT,
	contingency REAL NOT NULL DEFAULT 0,
	-- Stage values come from p3m_lifecycle_stages (admin-configurable), so no
	-- CHECK here; validation happens in the handlers against enabled stages.
	current_stage TEXT NOT NULL DEFAULT 'front_door',
	budget_approved REAL NOT NULL DEFAULT 0,
	budget_actual REAL NOT NULL DEFAULT 0,
	progress_percent INTEGER NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
	start_date DATE,
	target_completion_date DATE,
	rag_status TEXT NOT NULL DEFAULT 'green' CHECK (rag_status IN ('red', 'amber', 'green')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_benefits (
	id SERIAL PRIMARY KEY,
	project_id INTEGER REFERENCES p3m_projects(id) ON DELETE CASCADE,
	programme_id INTEGER REFERENCES p3m_programmes(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	benefit_type TEXT NOT NULL CHECK (benefit_type IN ('financial', 'non_financial', 'strategic', 'compliance')),
	target_value REAL NOT NULL DEFAULT 0,
	unit_of_measure TEXT NOT NULL DEFAULT '',
	baseline_value REAL NOT NULL DEFAULT 0,
	target_realization_date DATE NOT NULL,
	owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	measurement_method TEXT,
	measurement_frequency TEXT CHECK (measurement_frequency IN ('monthly', 'quarterly', 'annual', 'one_off')),
	status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'measuring', 'realised', 'shortfall', 'retired')),
	evidence TEXT,
	finance_validator_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	validation_status TEXT NOT NULL DEFAULT 'not_required' CHECK (validation_status IN ('not_required', 'pending', 'validated')),
	capability_ref TEXT,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	CHECK ((project_id IS NOT NULL AND programme_id IS NULL) OR (project_id IS NULL AND programme_id IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS p3m_benefit_realizations (
	id SERIAL PRIMARY KEY,
	benefit_id INTEGER NOT NULL REFERENCES p3m_benefits(id) ON DELETE CASCADE,
	recorded_date DATE NOT NULL,
	realized_value REAL NOT NULL DEFAULT 0,
	notes TEXT DEFAULT '',
	recorded_by INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Configurable lifecycle (BRD §5.2): admin-managed stages replace the legacy
-- four hard-coded stages. stage_key is immutable; sort_order defines path
-- position. At least one enabled stage and one enabled entry stage must
-- always exist (enforced by the config handlers).
CREATE TABLE IF NOT EXISTS p3m_lifecycle_stages (
	id SERIAL PRIMARY KEY,
	stage_key TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	description TEXT DEFAULT '',
	sort_order INTEGER NOT NULL UNIQUE,
	is_entry INTEGER NOT NULL DEFAULT 0,
	is_exit INTEGER NOT NULL DEFAULT 0,
	gate_required INTEGER NOT NULL DEFAULT 1,
	skippable INTEGER NOT NULL DEFAULT 1,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Gate templates: mandatory/optional governance gates per transition.
-- from_stage_key NULL = "from anywhere"; work_type NULL = all work types;
-- required_artifacts is JSON [{name, required, description}] pre-populating
-- the artifact checklist of reviews created for this template.
CREATE TABLE IF NOT EXISTS p3m_stage_gate_templates (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	from_stage_key TEXT,
	to_stage_key TEXT NOT NULL REFERENCES p3m_lifecycle_stages(stage_key) ON DELETE RESTRICT,
	mandatory INTEGER NOT NULL DEFAULT 1,
	work_type TEXT,
	required_artifacts TEXT DEFAULT '[]',
	description TEXT DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_stage_gate_reviews (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	from_stage TEXT NOT NULL,
	to_stage TEXT NOT NULL,
	-- Template the review was created from (artifact checklist pre-populated);
	-- NULL for ad-hoc gates.
	template_id INTEGER REFERENCES p3m_stage_gate_templates(id) ON DELETE SET NULL,
	passed BOOLEAN NOT NULL DEFAULT false,
	reviewer_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	review_date DATE,
	comments TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_gate_artifacts (
	id SERIAL PRIMARY KEY,
	review_id INTEGER NOT NULL REFERENCES p3m_stage_gate_reviews(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	required BOOLEAN NOT NULL DEFAULT true,
	completed BOOLEAN NOT NULL DEFAULT false,
	link_or_note TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_raci_assignments (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('responsible', 'accountable', 'consulted', 'informed')),
	UNIQUE(entity_type, entity_id, user_id)
);

CREATE TABLE IF NOT EXISTS p3m_authorization_thresholds (
	id SERIAL PRIMARY KEY,
	level TEXT NOT NULL UNIQUE CHECK (level IN ('portfolio', 'programme', 'project')),
	threshold_amount REAL NOT NULL DEFAULT 0,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Default spend-authorization thresholds: project managers approve increases
-- up to 10k, programme managers up to 100k; larger increases route to the
-- portfolio board.
INSERT INTO p3m_authorization_thresholds (level, threshold_amount) VALUES
	('project', 10000),
	('programme', 100000),
	('portfolio', 1000000)
ON CONFLICT (level) DO NOTHING;

CREATE TABLE IF NOT EXISTS p3m_budget_approvals (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	current_budget REAL NOT NULL DEFAULT 0,
	requested_amount REAL NOT NULL,
	routed_to_level TEXT NOT NULL CHECK (routed_to_level IN ('project', 'programme', 'portfolio')),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
	requested_by INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	decision_notes TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_baselines (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	label TEXT NOT NULL,
	snapshot TEXT NOT NULL,
	created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_raid_items (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	item_type TEXT NOT NULL CHECK (item_type IN ('risk', 'assumption', 'issue', 'dependency', 'decision')),
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	impact_score INTEGER NOT NULL DEFAULT 1 CHECK (impact_score BETWEEN 1 AND 5),
	probability_score INTEGER NOT NULL DEFAULT 1 CHECK (probability_score BETWEEN 1 AND 5),
	mitigation_plan TEXT DEFAULT '',
	owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'mitigating', 'closed', 'escalated')),
	due_date DATE,
	item_subtype TEXT CHECK (item_subtype IN ('change_risk', 'adoption_risk')),
	decision_subtype TEXT CHECK (decision_subtype IN ('governance', 'funding', 'procurement', 'plan', 'closure')),
	srm_contract_id INTEGER, -- srm_contracts.id for procurement decisions; no FK (srm_postgres.sql loads after p3m_postgres.sql)
	item_id INTEGER REFERENCES items(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Architecture Review Board tracking: per-project architecture sign-off. A
-- review moves wip -> in_review -> approved | needs_work; approval requires a
-- linked architecture page, rejection writes a decision-register entry.
CREATE TABLE IF NOT EXISTS p3m_architecture_reviews (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'wip' CHECK (status IN ('wip', 'in_review', 'needs_work', 'approved')),
	reviewer_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	decided_at TIMESTAMPTZ,
	comments TEXT DEFAULT '',
	architecture_page_id INTEGER REFERENCES pages(id) ON DELETE SET NULL,
	decision_item_id INTEGER REFERENCES p3m_raid_items(id) ON DELETE SET NULL,
	created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_stakeholders (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	role_title TEXT DEFAULT '',
	power_score INTEGER NOT NULL DEFAULT 3 CHECK (power_score BETWEEN 1 AND 5),
	interest_score INTEGER NOT NULL DEFAULT 3 CHECK (interest_score BETWEEN 1 AND 5),
	sms_stakeholder_id INTEGER REFERENCES sms_stakeholders(id) ON DELETE SET NULL,  -- identity resolves to the SMS register (system of record)
	notes TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_p3m_stakeholders_sms ON p3m_stakeholders(sms_stakeholder_id);

CREATE TABLE IF NOT EXISTS p3m_communications (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	date DATE NOT NULL,
	audience TEXT NOT NULL,
	channel TEXT DEFAULT '',
	summary TEXT NOT NULL,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_resources (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	skills TEXT NOT NULL DEFAULT '[]',
	cost_rate REAL NOT NULL DEFAULT 0,
	capacity_hours_per_week INTEGER NOT NULL DEFAULT 40,
	pool TEXT, -- specialist pool, e.g. digital_services | change | general (RES-005)
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_resource_allocations (
	id SERIAL PRIMARY KEY,
	resource_id INTEGER NOT NULL REFERENCES p3m_resources(id) ON DELETE CASCADE,
	project_id INTEGER REFERENCES p3m_projects(id) ON DELETE SET NULL,
	start_date DATE NOT NULL,
	end_date DATE NOT NULL,
	hours_per_week INTEGER NOT NULL DEFAULT 0,
	booking_type TEXT NOT NULL DEFAULT 'soft' CHECK (booking_type IN ('soft', 'hard')),
	allocation_class TEXT NOT NULL DEFAULT 'project' CHECK (allocation_class IN ('project', 'bau')), -- bau rows count against capacity but not any project (RES-004)
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_budget_tranches (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	title TEXT NOT NULL,
	amount REAL NOT NULL DEFAULT 0,
	expenditure_type TEXT NOT NULL DEFAULT 'opex' CHECK (expenditure_type IN ('capex', 'opex')),
	-- one_off | ongoing: complements capex/opex for FIN-002 cost classification.
	expenditure_class TEXT CHECK (expenditure_class IN ('one_off', 'ongoing')),
	planned_date DATE NOT NULL,
	status TEXT NOT NULL DEFAULT 'approved' CHECK (status IN ('proposed', 'approved', 'released')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Financial close-out record for a project at its close stage (FIN-010):
-- final budget position, outstanding commitments and ongoing obligations
-- carried into BAU. One per project; validated by the finance reviewer.
CREATE TABLE IF NOT EXISTS p3m_financial_close_outs (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL UNIQUE REFERENCES p3m_projects(id) ON DELETE CASCADE,
	final_budget REAL NOT NULL DEFAULT 0,
	actual_to_date REAL NOT NULL DEFAULT 0,
	outstanding_commitments REAL NOT NULL DEFAULT 0,
	ongoing_obligations TEXT DEFAULT '',
	finance_reviewer_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'validated')),
	notes TEXT DEFAULT '',
	closed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Investment cases: one per project, carrying the prioritisation decision
-- pipeline and risk/benefit/return scores for portfolio balancing.
CREATE TABLE IF NOT EXISTS p3m_investment_cases (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL UNIQUE REFERENCES p3m_projects(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'under_review', 'approved', 'rejected', 'deferred')),
	benefit_score INTEGER NOT NULL DEFAULT 3 CHECK (benefit_score BETWEEN 1 AND 5),
	risk_score INTEGER NOT NULL DEFAULT 3 CHECK (risk_score BETWEEN 1 AND 5),
	roi_percent REAL,
	payback_months INTEGER,
	business_case TEXT NOT NULL DEFAULT '',
	page_id INTEGER REFERENCES pages(id) ON DELETE SET NULL,
	decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	decision_date DATE,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_programmes_portfolio ON p3m_programmes(portfolio_id);
CREATE INDEX IF NOT EXISTS idx_p3m_projects_portfolio ON p3m_projects(portfolio_id);
CREATE INDEX IF NOT EXISTS idx_p3m_projects_programme ON p3m_projects(programme_id);
CREATE INDEX IF NOT EXISTS idx_p3m_benefits_project ON p3m_benefits(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_benefits_programme ON p3m_benefits(programme_id);
CREATE INDEX IF NOT EXISTS idx_p3m_realizations_benefit ON p3m_benefit_realizations(benefit_id);
CREATE INDEX IF NOT EXISTS idx_p3m_stage_gates_project ON p3m_stage_gate_reviews(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_gate_artifacts_review ON p3m_gate_artifacts(review_id);
CREATE INDEX IF NOT EXISTS idx_p3m_raci_entity ON p3m_raci_assignments(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_budget_approvals_project ON p3m_budget_approvals(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_baselines_project ON p3m_baselines(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_raid_entity ON p3m_raid_items(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_raid_item ON p3m_raid_items(item_id);
CREATE INDEX IF NOT EXISTS idx_p3m_arch_reviews_project ON p3m_architecture_reviews(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_stakeholders_entity ON p3m_stakeholders(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_communications_entity ON p3m_communications(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_allocations_resource ON p3m_resource_allocations(resource_id);
CREATE INDEX IF NOT EXISTS idx_p3m_tranches_entity ON p3m_budget_tranches(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_projects_time_project_id ON p3m_projects(time_project_id);

-- Workspace bridge for P3M projects: workspaces relate to the module through
-- their linked projects, so per-project financials and stakeholders roll up
-- into workspace views without scoping every module row.
CREATE TABLE IF NOT EXISTS workspace_p3m_projects (
	id SERIAL PRIMARY KEY,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	p3m_project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(workspace_id, p3m_project_id)
);

CREATE INDEX IF NOT EXISTS idx_workspace_p3m_projects_workspace_id ON workspace_p3m_projects(workspace_id);
CREATE INDEX IF NOT EXISTS idx_workspace_p3m_projects_project_id ON workspace_p3m_projects(p3m_project_id);

-- Per-project proportional skips: the recorded decision that unlocks shortcut
-- transitions past skipped (skippable) stages. Defined at file end because it
-- references p3m_projects, which Postgres resolves at creation time.
CREATE TABLE IF NOT EXISTS p3m_project_stage_skips (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	stage_key TEXT NOT NULL REFERENCES p3m_lifecycle_stages(stage_key) ON DELETE RESTRICT,
	reason TEXT NOT NULL,
	decided_by INTEGER NOT NULL,
	decided_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(project_id, stage_key)
);

CREATE INDEX IF NOT EXISTS idx_p3m_stage_skips_project ON p3m_project_stage_skips(project_id);
CREATE INDEX IF NOT EXISTS idx_p3m_gate_templates_to ON p3m_stage_gate_templates(to_stage_key, enabled);

-- Default lifecycle seed (BRD §5.2/§5.6): the six proportional stages and the
-- mandatory gate templates between them. Stage rows are idempotent via the
-- stage_key unique index; template rows carry no natural unique key, so each
-- is guarded by NOT EXISTS on its identifying columns. Admins may then extend
-- or disable rows freely.
INSERT INTO p3m_lifecycle_stages (stage_key, name, description, sort_order, is_entry, is_exit, gate_required, skippable, enabled) VALUES
	('front_door', 'Idea / Front Door', 'Intake and initial assessment of the idea or request.', 0, 1, 0, 0, 0, 1),
	('proposal', 'Proposal', 'Business proposal with indicative cost and strategic alignment.', 1, 0, 0, 1, 1, 1),
	('define_planning', 'Define / Planning', 'Detailed definition, planning and baseline approval.', 2, 0, 0, 1, 0, 1),
	('implement_deliver', 'Implement / Deliver & Monitor', 'Delivery execution with monitoring against the baseline.', 3, 0, 0, 1, 0, 1),
	('close', 'Close', 'Delivery acceptance and financial close-out.', 4, 0, 0, 1, 0, 1),
	('benefits_realisation', 'Benefits Realisation', 'Post-implementation review and benefit tracking to target.', 5, 0, 1, 1, 1, 1)
ON CONFLICT (stage_key) DO NOTHING;

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Front Door assessment', 'front_door', 'proposal', 1, NULL, '[{"name":"Classification","required":true},{"name":"Initial assessment","required":true}]', 'Mandatory intake gate: classify the idea and record the initial assessment.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Front Door assessment' AND from_stage_key IS 'front_door' AND to_stage_key = 'proposal');

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Governance go/no-go', 'proposal', 'define_planning', 1, NULL, '[{"name":"Governance go/no-go decision","required":true},{"name":"Financial endorsement","required":true}]', 'Governance go/no-go with financial endorsement before detailed planning.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Governance go/no-go' AND from_stage_key IS 'proposal' AND to_stage_key IS 'define_planning');

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Baseline & sponsor approval', 'define_planning', 'implement_deliver', 1, NULL, '[{"name":"Approved baseline","required":true},{"name":"Sponsor approval","required":true}]', 'Baseline (scope/schedule/cost) approved by the sponsor before delivery starts.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Baseline & sponsor approval' AND from_stage_key IS 'define_planning' AND to_stage_key = 'implement_deliver');

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Delivery acceptance & financial close-out', 'implement_deliver', 'close', 1, NULL, '[{"name":"Delivery acceptance","required":true},{"name":"Financial close-out","required":true}]', 'Delivered scope accepted and finances closed out before closure.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Delivery acceptance & financial close-out' AND from_stage_key IS 'implement_deliver' AND to_stage_key = 'close');

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Post-implementation review (PIR)', 'close', 'benefits_realisation', 1, NULL, '[{"name":"PIR report","required":true}]', 'Mandatory-but-skippable PIR gate into benefits realisation; projects may remain at Close when the skip is recorded.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Post-implementation review (PIR)' AND from_stage_key IS 'close' AND to_stage_key = 'benefits_realisation');

INSERT INTO p3m_stage_gate_templates (name, from_stage_key, to_stage_key, mandatory, work_type, required_artifacts, description)
SELECT 'Return for information', 'proposal', 'front_door', 0, NULL, '[]', 'Optional return-to-intake path when a proposal needs more information.'
WHERE NOT EXISTS (SELECT 1 FROM p3m_stage_gate_templates WHERE name = 'Return for information' AND from_stage_key IS 'proposal' AND to_stage_key IS 'front_door');

-- Front Door intake pack (FD-001..005, FD-008/010/011): the single entry point
-- for new work into the P3M lifecycle. A "proceed" decision creates a project
-- pre-filled from this record so details are captured once.
CREATE TABLE IF NOT EXISTS p3m_intakes (
	id SERIAL PRIMARY KEY,
	title TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT 'standard',   -- standard | exceptional_path (FD-011 retrospective registration)
	description TEXT NOT NULL DEFAULT '',
	problem_opportunity TEXT NOT NULL DEFAULT '',
	strategic_alignment TEXT NOT NULL DEFAULT '',
	intended_outcomes TEXT NOT NULL DEFAULT '',
	indicative_cost REAL,
	resource_requirements TEXT NOT NULL DEFAULT '',
	risk_notes TEXT NOT NULL DEFAULT '',
	change_impact_notes TEXT NOT NULL DEFAULT '',
	business_group TEXT NOT NULL DEFAULT '',
	work_type TEXT,                             -- programme | project | continuous_improvement | digital_product | operational
	complexity TEXT NOT NULL DEFAULT 'med' CHECK (complexity IN ('low', 'med', 'high')),
	status TEXT NOT NULL DEFAULT 'captured' CHECK (status IN ('captured', 'assessing', 'decided')),
	decision TEXT CHECK (decision IS NULL OR decision IN ('proceed', 'redirect', 'defer', 'decline', 'return_info')),
	decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	decision_date TIMESTAMPTZ,
	conditions TEXT NOT NULL DEFAULT '',        -- copied to the first gate comments on proceed (FD-008)
	rationale TEXT NOT NULL DEFAULT '',         -- mandatory with every decision
	target_project_id INTEGER REFERENCES p3m_projects(id) ON DELETE SET NULL,
	lifecycle_position TEXT,                    -- FD-011: where an exceptional-path item sits in the lifecycle
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_intakes_status ON p3m_intakes(status);

-- Per-complexity required-field rules for Front Door assessment (FD-003/005):
-- every listed field must be completed before an intake of that complexity can
-- be decided. Admin-configurable via PUT /p3m/front-door/screens.
CREATE TABLE IF NOT EXISTS p3m_front_door_screens (
	id SERIAL PRIMARY KEY,
	complexity TEXT NOT NULL CHECK (complexity IN ('low', 'med', 'high')),
	field_key TEXT NOT NULL,
	required BOOLEAN NOT NULL DEFAULT false,
	UNIQUE (complexity, field_key)
);

-- Default assessment screens: low complexity skips change-impact detail and
-- cost; medium adds resourcing/risk; high requires the full picture.
INSERT INTO p3m_front_door_screens (complexity, field_key, required) VALUES
	('low', 'title', true), ('low', 'source', true), ('low', 'description', true),
	('low', 'problem_opportunity', true), ('low', 'strategic_alignment', true),
	('low', 'intended_outcomes', true), ('low', 'work_type', true), ('low', 'business_group', true),
	('med', 'title', true), ('med', 'source', true), ('med', 'description', true),
	('med', 'problem_opportunity', true), ('med', 'strategic_alignment', true),
	('med', 'intended_outcomes', true), ('med', 'work_type', true), ('med', 'business_group', true),
	('med', 'resource_requirements', true), ('med', 'risk_notes', true),
	('high', 'title', true), ('high', 'source', true), ('high', 'description', true),
	('high', 'problem_opportunity', true), ('high', 'strategic_alignment', true),
	('high', 'intended_outcomes', true), ('high', 'work_type', true), ('high', 'business_group', true),
	('high', 'resource_requirements', true), ('high', 'risk_notes', true),
	('high', 'change_impact_notes', true), ('high', 'indicative_cost', true)
ON CONFLICT (complexity, field_key) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Financial system sync (FIN-007): scheduled CSV import of actuals /
-- commitments / forecast from the external finance system, reconciled against
-- P3M budgets. Runs are append-only; the effective value for a
-- (project, period, line_type) key comes from the latest run that matched it.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS p3m_finance_sync_configs (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	source_type TEXT NOT NULL CHECK (source_type IN ('file_path', 's3')),
	source_ref TEXT NOT NULL,
	schedule_minutes INTEGER NOT NULL DEFAULT 60,
	period_format TEXT NOT NULL DEFAULT 'yyyy-mm',
	default_currency TEXT NOT NULL DEFAULT 'NZD',
	match_mode TEXT NOT NULL DEFAULT 'ref_then_title' CHECK (match_mode IN ('ref_only', 'ref_then_title')),
	last_run_at TIMESTAMPTZ,
	last_run_status TEXT,
	last_file_hash TEXT,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_fin_sync_configs_enabled ON p3m_finance_sync_configs(enabled);

CREATE TABLE IF NOT EXISTS p3m_finance_sync_runs (
	id SERIAL PRIMARY KEY,
	config_id INTEGER NOT NULL REFERENCES p3m_finance_sync_configs(id) ON DELETE CASCADE,
	trigger TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual', 'upload')),
	started_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ,
	status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'partial', 'failed')),
	file_hash TEXT,
	rows_total INTEGER NOT NULL DEFAULT 0,
	rows_matched INTEGER NOT NULL DEFAULT 0,
	rows_unmatched INTEGER NOT NULL DEFAULT 0,
	rows_failed INTEGER NOT NULL DEFAULT 0,
	error_summary TEXT
);

CREATE INDEX IF NOT EXISTS idx_p3m_fin_runs_config ON p3m_finance_sync_runs(config_id, started_at DESC);

CREATE TABLE IF NOT EXISTS p3m_finance_actuals (
	id SERIAL PRIMARY KEY,
	config_id INTEGER NOT NULL REFERENCES p3m_finance_sync_configs(id) ON DELETE CASCADE,
	run_id INTEGER REFERENCES p3m_finance_sync_runs(id) ON DELETE SET NULL,
	project_id INTEGER REFERENCES p3m_projects(id) ON DELETE SET NULL,
	external_ref TEXT NOT NULL,
	cost_center TEXT,
	work_order TEXT,
	period TEXT NOT NULL,
	line_type TEXT NOT NULL CHECK (line_type IN ('actual', 'commitment', 'forecast')),
	amount REAL NOT NULL,
	currency TEXT NOT NULL DEFAULT 'NZD',
	description TEXT,
	match_status TEXT NOT NULL DEFAULT 'unmatched' CHECK (match_status IN ('matched', 'unmatched', 'rejected')),
	raw_row TEXT,
	imported_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_fin_actuals_proj_period ON p3m_finance_actuals(project_id, period, line_type);
CREATE INDEX IF NOT EXISTS idx_p3m_fin_actuals_ref ON p3m_finance_actuals(external_ref, period);
CREATE INDEX IF NOT EXISTS idx_p3m_fin_actuals_unmatched ON p3m_finance_actuals(match_status) WHERE match_status = 'unmatched';
CREATE INDEX IF NOT EXISTS idx_p3m_fin_actuals_run ON p3m_finance_actuals(run_id);

CREATE TABLE IF NOT EXISTS p3m_finance_sync_rejections (
	id SERIAL PRIMARY KEY,
	external_ref TEXT NOT NULL,
	period TEXT NOT NULL,
	line_type TEXT NOT NULL CHECK (line_type IN ('actual', 'commitment', 'forecast')),
	reason TEXT NOT NULL,
	rejected_by INTEGER NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_p3m_fin_rejections ON p3m_finance_sync_rejections(external_ref, period, line_type);

-- ---------------------------------------------------------------------------
-- Phase 2 gap closure (BRD OHP F6-F15)
-- ---------------------------------------------------------------------------

-- Change calendar & cumulative change view (CHG-008/009). Weeks are ISO week
-- strings such as "2026-W45".
CREATE TABLE IF NOT EXISTS p3m_change_calendar_entries (
	id SERIAL PRIMARY KEY,
	entity_type TEXT CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER,
	activity TEXT NOT NULL,
	business_unit TEXT,
	start_week TEXT NOT NULL,
	end_week TEXT NOT NULL,
	impact_level INTEGER NOT NULL DEFAULT 3 CHECK (impact_level BETWEEN 1 AND 5),
	source TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('corporate_project', 'corporate_activity', 'bau', 'local')),
	notes TEXT,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_change_cal_weeks ON p3m_change_calendar_entries(start_week, end_week);
CREATE INDEX IF NOT EXISTS idx_p3m_change_cal_bu ON p3m_change_calendar_entries(business_unit);

-- Change impact assessment (CHG-002/003): one active per entity.
CREATE TABLE IF NOT EXISTS p3m_change_assessments (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('project', 'programme')),
	entity_id INTEGER NOT NULL,
	impacted_groups TEXT, -- JSON [string]
	impact_by_characteristic TEXT, -- JSON [{characteristic, nature, severity}]
	overall_rating INTEGER NOT NULL DEFAULT 3 CHECK (overall_rating BETWEEN 1 AND 5),
	support_level TEXT NOT NULL DEFAULT 'none' CHECK (support_level IN ('none', 'light', 'full')),
	approach TEXT,
	deliverables TEXT, -- JSON [string]
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_p3m_change_assessments ON p3m_change_assessments(entity_type, entity_id);

-- Readiness assessment (CHG-006): latest per entity gates implement_deliver.
CREATE TABLE IF NOT EXISTS p3m_readiness_assessments (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('project', 'programme')),
	entity_id INTEGER NOT NULL,
	questions TEXT NOT NULL, -- JSON [{id, text, required}]
	answers TEXT, -- JSON [{question_id, answer: yes|no, gap_action}]
	assessed_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	outcome TEXT NOT NULL CHECK (outcome IN ('go', 'no_go', 'reassess')),
	reassessment_of INTEGER REFERENCES p3m_readiness_assessments(id) ON DELETE SET NULL,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_readiness_entity ON p3m_readiness_assessments(entity_type, entity_id, assessed_at DESC);

-- Benefit measurement schedules (BEN-007): post-closure reminders.
CREATE TABLE IF NOT EXISTS p3m_benefit_measurements (
	id SERIAL PRIMARY KEY,
	benefit_id INTEGER NOT NULL REFERENCES p3m_benefits(id) ON DELETE CASCADE,
	due_date DATE NOT NULL,
	completed_at TIMESTAMPTZ,
	value REAL,
	evidence TEXT,
	recorded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_benefit_measurements_due ON p3m_benefit_measurements(due_date) WHERE completed_at IS NULL;

-- Report snapshots + RAG discipline (RPT-005/006/007/009).
CREATE TABLE IF NOT EXISTS p3m_report_snapshots (
	id SERIAL PRIMARY KEY,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('project', 'programme', 'portfolio')),
	entity_id INTEGER NOT NULL,
	period TEXT NOT NULL,
	overall_rag TEXT NOT NULL CHECK (overall_rag IN ('green', 'amber', 'red')),
	category_rags TEXT, -- JSON {schedule,budget,scope,risk,benefits,change}
	suggested_category_rags TEXT, -- tolerance-computed suggestions (RPT-007)
	rag_override_reasons TEXT, -- JSON {category: reason} where manual differs from suggested
	commentary TEXT,
	escalations TEXT, -- JSON [string]
	metrics TEXT, -- auto-captured at capture time
	captured_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	captured_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_snapshots_entity ON p3m_report_snapshots(entity_type, entity_id, captured_at DESC);

-- Tolerance profiles for RAG computation (RPT-007).
CREATE TABLE IF NOT EXISTS p3m_tolerance_profiles (
	id SERIAL PRIMARY KEY,
	scope TEXT NOT NULL CHECK (scope IN ('portfolio', 'project')),
	entity_id INTEGER NOT NULL, -- portfolio or project id per scope
	schedule_days INTEGER NOT NULL DEFAULT 14,
	budget_pct REAL NOT NULL DEFAULT 10.0,
	risk_score INTEGER NOT NULL DEFAULT 3 CHECK (risk_score BETWEEN 1 AND 5),
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_p3m_tolerance_profiles ON p3m_tolerance_profiles(scope, entity_id);

-- Portfolio prioritisation scoring (PO-003/004/008).
CREATE TABLE IF NOT EXISTS p3m_scoring_criteria (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	weight REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0),
	direction TEXT NOT NULL DEFAULT 'higher_better' CHECK (direction IN ('higher_better', 'lower_better')),
	description TEXT,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS p3m_investment_case_scores (
	id SERIAL PRIMARY KEY,
	ic_id INTEGER NOT NULL REFERENCES p3m_investment_cases(id) ON DELETE CASCADE,
	criterion_id INTEGER NOT NULL REFERENCES p3m_scoring_criteria(id) ON DELETE CASCADE,
	score REAL NOT NULL CHECK (score BETWEEN 1 AND 5),
	rationale TEXT,
	scored_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_p3m_ic_scores ON p3m_investment_case_scores(ic_id, criterion_id);

-- Financial exception alert rules (FIN-009).
CREATE TABLE IF NOT EXISTS p3m_finance_alert_rules (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	scope TEXT NOT NULL CHECK (scope IN ('portfolio', 'project')),
	portfolio_id INTEGER REFERENCES p3m_portfolios(id) ON DELETE CASCADE,
	project_id INTEGER REFERENCES p3m_projects(id) ON DELETE CASCADE,
	metric TEXT NOT NULL CHECK (metric IN ('forecast_overrun_pct', 'variance_vs_budget', 'commitment_vs_remaining')),
	comparator TEXT NOT NULL DEFAULT '>' CHECK (comparator IN ('>', '<', '>=', '<=')),
	threshold REAL NOT NULL,
	notify_email TEXT, -- comma list; empty = no email
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_p3m_finance_alert_rules_enabled ON p3m_finance_alert_rules(enabled);

-- Closure & handover records (CLS-001/002).
CREATE TABLE IF NOT EXISTS p3m_closure_records (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL UNIQUE REFERENCES p3m_projects(id) ON DELETE CASCADE,
	closure_report_page_id TEXT,
	acceptance_signoff_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	acceptance_signoff_at TIMESTAMPTZ,
	outstanding_items TEXT, -- JSON [string], auto from open RAID at capture
	lessons_learned TEXT,
	cost_ownership TEXT, -- owner ref + ongoing cost ref
	handover_to TEXT, -- user/team ref
	status TEXT NOT NULL DEFAULT 'recommended' CHECK (status IN ('recommended', 'approved')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
