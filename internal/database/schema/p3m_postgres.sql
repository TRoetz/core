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
	current_stage TEXT NOT NULL DEFAULT 'initiation' CHECK (current_stage IN ('initiation', 'planning', 'execution', 'closure')),
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

CREATE TABLE IF NOT EXISTS p3m_stage_gate_reviews (
	id SERIAL PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES p3m_projects(id) ON DELETE CASCADE,
	from_stage TEXT NOT NULL CHECK (from_stage IN ('initiation', 'planning', 'execution', 'closure')),
	to_stage TEXT NOT NULL CHECK (to_stage IN ('initiation', 'planning', 'execution', 'closure')),
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
	item_type TEXT NOT NULL CHECK (item_type IN ('risk', 'assumption', 'issue', 'dependency')),
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	impact_score INTEGER NOT NULL DEFAULT 1 CHECK (impact_score BETWEEN 1 AND 5),
	probability_score INTEGER NOT NULL DEFAULT 1 CHECK (probability_score BETWEEN 1 AND 5),
	mitigation_plan TEXT DEFAULT '',
	owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'mitigating', 'closed', 'escalated')),
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
	notes TEXT DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

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
	planned_date DATE NOT NULL,
	status TEXT NOT NULL DEFAULT 'approved' CHECK (status IN ('proposed', 'approved', 'released')),
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
CREATE INDEX IF NOT EXISTS idx_p3m_stakeholders_entity ON p3m_stakeholders(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_communications_entity ON p3m_communications(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_p3m_allocations_resource ON p3m_resource_allocations(resource_id);
CREATE INDEX IF NOT EXISTS idx_p3m_tranches_entity ON p3m_budget_tranches(entity_type, entity_id);
