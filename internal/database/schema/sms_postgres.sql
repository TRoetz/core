-- SMS (Stakeholder Management System) module tables.
-- Postgres variant of sms.sql; conventions follow srm_postgres.sql: SERIAL PKs,
-- TIMESTAMPTZ timestamps, TEXT + CHECK for enum columns, polymorphic
-- (entity_type, entity_id) pairs for P3M bindings (application-level FK checks).
--
-- sms_stakeholders is the authoritative stakeholder identity register for the
-- platform (system of record). P3M and SRM keep per-entity analysis scores in
-- their own tables but resolve person identity back to this register through
-- nullable sms_stakeholder_id links (see p3m_postgres.sql / srm_postgres.sql).

-- Core stakeholder profile
CREATE TABLE IF NOT EXISTS sms_stakeholders (
	id SERIAL PRIMARY KEY,
	external_id TEXT UNIQUE,                    -- stable business ID (SH-000123), preserved on export
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',              -- job title / organization role
	organization TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	stakeholder_type TEXT CHECK (stakeholder_type IS NULL OR stakeholder_type IN ('mana_whenua','elected_member','government_agency','regional_authority','regulatory_partner','infrastructure_partner','business','developer','business_association','strategic_supplier','utility_provider','community_organisation','industry_group','advocacy_group','educational_institution','major_landowner','project_stakeholder','other')),
	category TEXT NOT NULL DEFAULT '',          -- free text, e.g. "Central City developers"
	tier TEXT NOT NULL DEFAULT 'tier_3' CHECK (tier IN ('tier_1','tier_2','tier_3','tier_4')),  -- materiality tier (Std §8)
	business_unit TEXT NOT NULL DEFAULT '',
	role_classification TEXT NOT NULL DEFAULT 'support' CHECK (role_classification IN ('key','decision_maker','support','influencer','observer')),
	influence_level INTEGER NOT NULL DEFAULT 3 CHECK (influence_level BETWEEN 1 AND 5),
	interest_level INTEGER NOT NULL DEFAULT 3 CHECK (interest_level BETWEEN 1 AND 5),
	interests TEXT NOT NULL DEFAULT '',          -- free text / comma list
	engagement_status TEXT NOT NULL DEFAULT 'passive' CHECK (engagement_status IN ('passive','engaged','active','disengaged')),
	lifecycle_stage TEXT NOT NULL DEFAULT 'onboarding' CHECK (lifecycle_stage IN ('onboarding','active','retained','exited')),
	relationship_status TEXT NOT NULL DEFAULT 'neutral' CHECK (relationship_status IN ('advocate','supportive','neutral','concerned','opposed')),  -- HCC §13 scale
	trust_level TEXT CHECK (trust_level IS NULL OR trust_level IN ('very_high','high','moderate','low','critical')),
	risk_rating TEXT CHECK (risk_rating IS NULL OR risk_rating IN ('low','medium','high','critical')),
	communication_preferences TEXT NOT NULL DEFAULT '',  -- e.g. "email,quarterly"
	owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,   -- team ownership
	backup_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	executive_sponsor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	last_engagement_at TIMESTAMPTZ,             -- maintained on communication create
	next_engagement_at TIMESTAMPTZ,             -- planned next touchpoint
	info_classification TEXT NOT NULL DEFAULT 'internal' CHECK (info_classification IN ('public','internal','confidential','restricted')),
	consent_status TEXT NOT NULL DEFAULT 'not_required' CHECK (consent_status IN ('not_required','granted','declined','withdrawn','unknown')),
	retention_category TEXT NOT NULL DEFAULT '',
	last_reviewed_at TIMESTAMPTZ,
	review_cadence TEXT NOT NULL DEFAULT 'quarterly' CHECK (review_cadence IN ('monthly','quarterly','semi_annual','annual')),
	location_lat REAL,
	location_lon REAL,
	location_text TEXT NOT NULL DEFAULT '',
	custom_fields TEXT NOT NULL DEFAULT '{}',    -- JSON object of custom attributes
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_classification ON sms_stakeholders(role_classification);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_owner ON sms_stakeholders(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_tier ON sms_stakeholders(tier);

-- Polymorphic link to shared P3M entities (project sharing)
CREATE TABLE IF NOT EXISTS sms_stakeholder_entities (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio','programme','project')),
	entity_id INTEGER NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (stakeholder_id, entity_type, entity_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholder_entities_entity ON sms_stakeholder_entities(entity_type, entity_id);

-- Interaction log
CREATE TABLE IF NOT EXISTS sms_communications (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','other')),
	direction TEXT NOT NULL DEFAULT 'outbound' CHECK (direction IN ('outbound','inbound')),
	subject TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	sentiment TEXT CHECK (sentiment IS NULL OR sentiment IN ('positive','neutral','negative')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_communications_stakeholder ON sms_communications(stakeholder_id, created_at DESC);

-- Communication strategy / engagement plan (Std §14: per programme/project,
-- covering multiple stakeholders via sms_engagement_plan_stakeholders)
CREATE TABLE IF NOT EXISTS sms_engagement_plans (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,  -- lead stakeholder; additional members via sms_engagement_plan_stakeholders
	strategy TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	approach TEXT CHECK (approach IS NULL OR approach IN ('inform','consult','involve','collaborate','partner')),  -- IAP2 spectrum
	key_messages TEXT NOT NULL DEFAULT '',
	success_measures TEXT NOT NULL DEFAULT '',
	frequency TEXT NOT NULL DEFAULT 'monthly' CHECK (frequency IN ('daily','weekly','biweekly','monthly','quarterly','annual','as_needed')),
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','other')),
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','paused','completed')),
	entity_type TEXT,                          -- optional programme/project scope
	entity_id INTEGER,
	start_date TIMESTAMPTZ,
	end_date TIMESTAMPTZ,
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_engagement_plans_stakeholder ON sms_engagement_plans(stakeholder_id);

-- Plan ↔ stakeholder membership (multi-stakeholder plans; Std §14)
CREATE TABLE IF NOT EXISTS sms_engagement_plan_stakeholders (
	id SERIAL PRIMARY KEY,
	plan_id INTEGER NOT NULL REFERENCES sms_engagement_plans(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	UNIQUE (plan_id, stakeholder_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_plan_stakeholders_stakeholder ON sms_engagement_plan_stakeholders(stakeholder_id);

-- Issues + risk register with escalation. category follows the Std §15 risk
-- taxonomy; escalation_path records where an escalated item sits on the
-- project → programme → business unit → enterprise path (§25.7).
CREATE TABLE IF NOT EXISTS sms_issues (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT 'issue' CHECK (kind IN ('issue','risk')),
	category TEXT NOT NULL DEFAULT 'other' CHECK (category IN ('relationship','reputation','delivery','communication','dependency','knowledge_loss','commitment','other')),
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
	impact INTEGER NOT NULL DEFAULT 3 CHECK (impact BETWEEN 1 AND 5),        -- risk matrix axes
	likelihood INTEGER NOT NULL DEFAULT 3 CHECK (likelihood BETWEEN 1 AND 5),
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','escalated','resolved','closed')),
	escalation_level INTEGER NOT NULL DEFAULT 0,
	escalation_path TEXT NOT NULL DEFAULT '',   -- e.g. "project → programme"
	mitigation TEXT NOT NULL DEFAULT '',
	resolution_notes TEXT NOT NULL DEFAULT '',
	resolved_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_issues_stakeholder ON sms_issues(stakeholder_id);

-- Task assignment & tracking
CREATE TABLE IF NOT EXISTS sms_tasks (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	assigned_to_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	due_date TIMESTAMPTZ,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','done','cancelled')),
	completed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_tasks_stakeholder ON sms_tasks(stakeholder_id);

-- Feedback & resolution tracking
CREATE TABLE IF NOT EXISTS sms_feedback (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	source TEXT NOT NULL DEFAULT 'form' CHECK (source IN ('form','survey','email','meeting','other')),
	category TEXT NOT NULL DEFAULT '',
	rating INTEGER CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),
	body TEXT NOT NULL DEFAULT '',
	resolution_status TEXT NOT NULL DEFAULT 'open' CHECK (resolution_status IN ('open','addressed','resolved')),
	resolution_notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_feedback_stakeholder ON sms_feedback(stakeholder_id);

-- Relationship goals / KPIs
CREATE TABLE IF NOT EXISTS sms_relationship_goals (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	goal TEXT NOT NULL,
	metric TEXT NOT NULL DEFAULT '',
	target_value REAL,
	current_value REAL,
	status TEXT NOT NULL DEFAULT 'on_track' CHECK (status IN ('on_track','at_risk','missed','achieved')),
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_relationship_goals_stakeholder ON sms_relationship_goals(stakeholder_id);

-- Commitment register (SRM-1004, Std §21 template 7). direction records who
-- owes what: from_us / from_them / mutual.
CREATE TABLE IF NOT EXISTS sms_commitments (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	direction TEXT NOT NULL DEFAULT 'from_them' CHECK (direction IN ('from_us','from_them','mutual')),
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	due_date TIMESTAMPTZ,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','at_risk','completed','cancelled')),
	escalated BOOLEAN NOT NULL DEFAULT false,
	escalation_level INTEGER NOT NULL DEFAULT 0,
	source_communication_id INTEGER REFERENCES sms_communications(id) ON DELETE SET NULL,
	linked_issue_id INTEGER REFERENCES sms_issues(id) ON DELETE SET NULL,
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_commitments_stakeholder ON sms_commitments(stakeholder_id);
CREATE INDEX IF NOT EXISTS idx_sms_commitments_due ON sms_commitments(due_date);

-- Grievance management (R3; distinct from general feedback)
CREATE TABLE IF NOT EXISTS sms_grievances (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	severity TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high','critical')),
	status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received','acknowledged','investigating','responded','closed')),
	escalated BOOLEAN NOT NULL DEFAULT false,
	escalation_level INTEGER NOT NULL DEFAULT 0,
	acknowledged_at TIMESTAMPTZ,
	response_due_at TIMESTAMPTZ,
	response TEXT NOT NULL DEFAULT '',
	resolution TEXT NOT NULL DEFAULT '',
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_grievances_stakeholder ON sms_grievances(stakeholder_id);

-- Decisions log (Appendix A: decisions belong in relationship history)
CREATE TABLE IF NOT EXISTS sms_decisions (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	decision TEXT NOT NULL DEFAULT '',
	context TEXT NOT NULL DEFAULT '',
	decided_at TIMESTAMPTZ,
	decided_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	entity_type TEXT,                          -- optional P3M link (portfolio/programme/project)
	entity_id INTEGER,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_decisions_stakeholder ON sms_decisions(stakeholder_id);

-- Stakeholder groups (e.g. "Central City developers", "Mana whenua")
CREATE TABLE IF NOT EXISTS sms_stakeholder_groups (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sms_stakeholder_group_members (
	id SERIAL PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES sms_stakeholder_groups(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	UNIQUE (group_id, stakeholder_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_group_members_stakeholder ON sms_stakeholder_group_members(stakeholder_id);

-- Stakeholder network: undirected stakeholder↔stakeholder relationships.
-- Rows are stored once; handlers canonicalise (source_id < target_id).
CREATE TABLE IF NOT EXISTS sms_stakeholder_links (
	id SERIAL PRIMARY KEY,
	source_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	target_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	relation_type TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (source_id, target_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_links_target ON sms_stakeholder_links(target_id);

-- Relationship continuity: ownership transfer / handover records (Std §16).
-- The review checklist flags record that history, risks and commitments were
-- reviewed before the handover completed.
CREATE TABLE IF NOT EXISTS sms_ownership_transfers (
	id SERIAL PRIMARY KEY,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	from_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	to_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	reason TEXT NOT NULL DEFAULT '',
	history_reviewed BOOLEAN NOT NULL DEFAULT false,
	risks_reviewed BOOLEAN NOT NULL DEFAULT false,
	commitments_reviewed BOOLEAN NOT NULL DEFAULT false,
	notes TEXT NOT NULL DEFAULT '',
	completed_at TIMESTAMPTZ,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_transfers_stakeholder ON sms_ownership_transfers(stakeholder_id);

-- Pilot success-measure baselines (Doc 7): metric_key → baseline/target.
CREATE TABLE IF NOT EXISTS sms_pilot_baselines (
	metric_key TEXT PRIMARY KEY,
	baseline_value REAL,
	target_value REAL,
	set_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
