-- Plans module tables.

CREATE TABLE IF NOT EXISTS plans (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'completed', 'archived')),
	color TEXT NOT NULL DEFAULT '',
	owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	start_date DATE,
	end_date DATE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS plan_workspaces (
	id SERIAL PRIMARY KEY,
	plan_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(plan_id, workspace_id)
);

CREATE INDEX IF NOT EXISTS idx_plan_workspaces_plan ON plan_workspaces(plan_id);
CREATE INDEX IF NOT EXISTS idx_plan_workspaces_workspace ON plan_workspaces(workspace_id);

CREATE TABLE IF NOT EXISTS plan_p3m_entities (
	id SERIAL PRIMARY KEY,
	plan_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio', 'programme', 'project')),
	entity_id INTEGER NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(plan_id, entity_type, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_plan_p3m_entities_plan ON plan_p3m_entities(plan_id);
CREATE INDEX IF NOT EXISTS idx_plan_p3m_entities_entity ON plan_p3m_entities(entity_type, entity_id);

CREATE TABLE IF NOT EXISTS plan_stakeholders (
	id SERIAL PRIMARY KEY,
	plan_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(plan_id, stakeholder_id)
);

CREATE INDEX IF NOT EXISTS idx_plan_stakeholders_plan ON plan_stakeholders(plan_id);
CREATE INDEX IF NOT EXISTS idx_plan_stakeholders_stakeholder ON plan_stakeholders(stakeholder_id);
