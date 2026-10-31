-- SMS (Stakeholder Management System) module tables.

-- Core stakeholder profile
CREATE TABLE IF NOT EXISTS sms_stakeholders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',              -- job title / organization role
	organization TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	role_classification TEXT NOT NULL DEFAULT 'support' CHECK (role_classification IN ('key','decision_maker','support','influencer','observer')),
	influence_level INTEGER NOT NULL DEFAULT 3 CHECK (influence_level BETWEEN 1 AND 5),
	interest_level INTEGER NOT NULL DEFAULT 3 CHECK (interest_level BETWEEN 1 AND 5),
	interests TEXT NOT NULL DEFAULT '',          -- free text / comma list
	engagement_status TEXT NOT NULL DEFAULT 'passive' CHECK (engagement_status IN ('passive','engaged','active','disengaged')),
	lifecycle_stage TEXT NOT NULL DEFAULT 'onboarding' CHECK (lifecycle_stage IN ('onboarding','active','retained','exited')),
	relationship_status TEXT NOT NULL DEFAULT 'neutral' CHECK (relationship_status IN ('positive','neutral','tense','conflict')),
	communication_preferences TEXT NOT NULL DEFAULT '',  -- e.g. "email,quarterly"
	owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,   -- team ownership
	custom_fields TEXT NOT NULL DEFAULT '{}',    -- JSON object of custom attributes
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_classification ON sms_stakeholders(role_classification);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_owner ON sms_stakeholders(owner_user_id);

-- Polymorphic link to shared P3M entities (project sharing)
CREATE TABLE IF NOT EXISTS sms_stakeholder_entities (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio','programme','project')),
	entity_id INTEGER NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (stakeholder_id, entity_type, entity_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholder_entities_entity ON sms_stakeholder_entities(entity_type, entity_id);

-- Interaction log
CREATE TABLE IF NOT EXISTS sms_communications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','other')),
	direction TEXT NOT NULL DEFAULT 'outbound' CHECK (direction IN ('outbound','inbound')),
	subject TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	sentiment TEXT CHECK (sentiment IS NULL OR sentiment IN ('positive','neutral','negative')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_communications_stakeholder ON sms_communications(stakeholder_id, created_at DESC);

-- Communication strategy / engagement plan
CREATE TABLE IF NOT EXISTS sms_engagement_plans (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	strategy TEXT NOT NULL DEFAULT '',
	frequency TEXT NOT NULL DEFAULT 'monthly' CHECK (frequency IN ('daily','weekly','biweekly','monthly','quarterly','annual','as_needed')),
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','other')),
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','paused','completed')),
	start_date DATETIME,
	end_date DATETIME,
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_engagement_plans_stakeholder ON sms_engagement_plans(stakeholder_id);

-- Issues + risk register with escalation
CREATE TABLE IF NOT EXISTS sms_issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT 'issue' CHECK (kind IN ('issue','risk')),
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
	impact INTEGER NOT NULL DEFAULT 3 CHECK (impact BETWEEN 1 AND 5),        -- risk matrix axes
	likelihood INTEGER NOT NULL DEFAULT 3 CHECK (likelihood BETWEEN 1 AND 5),
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','escalated','resolved','closed')),
	escalation_level INTEGER NOT NULL DEFAULT 0,
	mitigation TEXT NOT NULL DEFAULT '',
	resolution_notes TEXT NOT NULL DEFAULT '',
	resolved_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_issues_stakeholder ON sms_issues(stakeholder_id);

-- Task assignment & tracking
CREATE TABLE IF NOT EXISTS sms_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	assigned_to_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	due_date DATETIME,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','done','cancelled')),
	completed_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_tasks_stakeholder ON sms_tasks(stakeholder_id);

-- Feedback & resolution tracking
CREATE TABLE IF NOT EXISTS sms_feedback (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	source TEXT NOT NULL DEFAULT 'form' CHECK (source IN ('form','survey','email','meeting','other')),
	category TEXT NOT NULL DEFAULT '',
	rating INTEGER CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),
	body TEXT NOT NULL DEFAULT '',
	resolution_status TEXT NOT NULL DEFAULT 'open' CHECK (resolution_status IN ('open','addressed','resolved')),
	resolution_notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_feedback_stakeholder ON sms_feedback(stakeholder_id);

-- Relationship goals / KPIs
CREATE TABLE IF NOT EXISTS sms_relationship_goals (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	goal TEXT NOT NULL,
	metric TEXT NOT NULL DEFAULT '',
	target_value REAL,
	current_value REAL,
	status TEXT NOT NULL DEFAULT 'on_track' CHECK (status IN ('on_track','at_risk','missed','achieved')),
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_relationship_goals_stakeholder ON sms_relationship_goals(stakeholder_id);
