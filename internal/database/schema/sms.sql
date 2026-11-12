-- SMS (Stakeholder Management System) module tables.
--
-- sms_stakeholders is the authoritative stakeholder identity register for the
-- platform (system of record). P3M and SRM keep per-entity analysis scores in
-- their own tables but resolve person identity back to this register through
-- nullable sms_stakeholder_id links (see p3m.sql / srm.sql).

-- Core stakeholder profile
CREATE TABLE IF NOT EXISTS sms_stakeholders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	relationship_status TEXT NOT NULL DEFAULT 'neutral' CHECK (relationship_status IN ('advocate','supportive','neutral','concerned','opposed')),  -- five-point relationship scale
	trust_level TEXT CHECK (trust_level IS NULL OR trust_level IN ('very_high','high','moderate','low','critical')),
	risk_rating TEXT CHECK (risk_rating IS NULL OR risk_rating IN ('low','medium','high','critical')),
	communication_preferences TEXT NOT NULL DEFAULT '',  -- e.g. "email,quarterly"
	owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,   -- team ownership
	backup_owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	executive_sponsor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	last_engagement_at DATETIME,                -- maintained on communication create
	next_engagement_at DATETIME,                -- planned next touchpoint
	info_classification TEXT NOT NULL DEFAULT 'internal' CHECK (info_classification IN ('public','internal','confidential','restricted')),
	consent_status TEXT NOT NULL DEFAULT 'not_required' CHECK (consent_status IN ('not_required','granted','declined','withdrawn','unknown')),
	retention_category TEXT NOT NULL DEFAULT '',
	last_reviewed_at DATETIME,
	review_cadence TEXT NOT NULL DEFAULT 'quarterly' CHECK (review_cadence IN ('monthly','quarterly','semi_annual','annual')),
	location_lat REAL,
	location_lon REAL,
	location_text TEXT NOT NULL DEFAULT '',
	custom_fields TEXT NOT NULL DEFAULT '{}',    -- JSON object of custom attributes
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_classification ON sms_stakeholders(role_classification);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_owner ON sms_stakeholders(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_sms_stakeholders_tier ON sms_stakeholders(tier);

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
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','letter','sms_text','other')),
	direction TEXT NOT NULL DEFAULT 'outbound' CHECK (direction IN ('outbound','inbound')),
	subject TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	sentiment TEXT CHECK (sentiment IS NULL OR sentiment IN ('positive','neutral','negative')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_communications_stakeholder ON sms_communications(stakeholder_id, created_at DESC);

-- Communication strategy / engagement plan (Std §14: per programme/project,
-- covering multiple stakeholders via sms_engagement_plan_stakeholders)
CREATE TABLE IF NOT EXISTS sms_engagement_plans (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,  -- lead stakeholder; additional members via sms_engagement_plan_stakeholders
	strategy TEXT NOT NULL DEFAULT '',
	objective TEXT NOT NULL DEFAULT '',
	approach TEXT CHECK (approach IS NULL OR approach IN ('inform','consult','involve','collaborate','partner')),  -- IAP2 spectrum
	key_messages TEXT NOT NULL DEFAULT '',
	success_measures TEXT NOT NULL DEFAULT '',
	frequency TEXT NOT NULL DEFAULT 'monthly' CHECK (frequency IN ('daily','weekly','biweekly','monthly','quarterly','annual','as_needed')),
	channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','phone','meeting','in_person','slack','teams','letter','sms_text','other')),
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','paused','completed')),
	entity_type TEXT,                          -- optional programme/project scope
	entity_id INTEGER,
	start_date DATETIME,
	end_date DATETIME,
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_engagement_plans_stakeholder ON sms_engagement_plans(stakeholder_id);

-- Plan ↔ stakeholder membership (multi-stakeholder plans; Std §14)
CREATE TABLE IF NOT EXISTS sms_engagement_plan_stakeholders (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	plan_id INTEGER NOT NULL REFERENCES sms_engagement_plans(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	UNIQUE (plan_id, stakeholder_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_plan_stakeholders_stakeholder ON sms_engagement_plan_stakeholders(stakeholder_id);

-- Issues + risk register with escalation. category follows the Std §15 risk
-- taxonomy; escalation_path records where an escalated item sits on the
-- project → programme → business unit → enterprise path (§25.7).
CREATE TABLE IF NOT EXISTS sms_issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
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

-- Commitment register (SRM-1004, Std §21 template 7). direction records who
-- owes what: from_us / from_them / mutual.
CREATE TABLE IF NOT EXISTS sms_commitments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	direction TEXT NOT NULL DEFAULT 'from_them' CHECK (direction IN ('from_us','from_them','mutual')),
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	due_date DATETIME,
	status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','at_risk','completed','cancelled')),
	escalated BOOLEAN NOT NULL DEFAULT 0,
	escalation_level INTEGER NOT NULL DEFAULT 0,
	source_communication_id INTEGER REFERENCES sms_communications(id) ON DELETE SET NULL,
	linked_issue_id INTEGER REFERENCES sms_issues(id) ON DELETE SET NULL,
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_commitments_stakeholder ON sms_commitments(stakeholder_id);
CREATE INDEX IF NOT EXISTS idx_sms_commitments_due ON sms_commitments(due_date);

-- Grievance management (R3; distinct from general feedback)
CREATE TABLE IF NOT EXISTS sms_grievances (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	severity TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high','critical')),
	status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received','acknowledged','investigating','responded','closed')),
	escalated BOOLEAN NOT NULL DEFAULT 0,
	escalation_level INTEGER NOT NULL DEFAULT 0,
	acknowledged_at DATETIME,
	response_due_at DATETIME,
	response TEXT NOT NULL DEFAULT '',
	resolution TEXT NOT NULL DEFAULT '',
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_grievances_stakeholder ON sms_grievances(stakeholder_id);

-- Decisions log (Appendix A: decisions belong in relationship history)
CREATE TABLE IF NOT EXISTS sms_decisions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	decision TEXT NOT NULL DEFAULT '',
	context TEXT NOT NULL DEFAULT '',
	decided_at DATETIME,
	decided_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	entity_type TEXT,                          -- optional P3M link (portfolio/programme/project)
	entity_id INTEGER,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_decisions_stakeholder ON sms_decisions(stakeholder_id);

-- Stakeholder groups (e.g. "Central City developers", "Mana whenua")
CREATE TABLE IF NOT EXISTS sms_stakeholder_groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sms_stakeholder_group_members (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL REFERENCES sms_stakeholder_groups(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	UNIQUE (group_id, stakeholder_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_group_members_stakeholder ON sms_stakeholder_group_members(stakeholder_id);

-- Stakeholder network: undirected stakeholder↔stakeholder relationships.
-- Rows are stored once; handlers canonicalise (source_id < target_id).
CREATE TABLE IF NOT EXISTS sms_stakeholder_links (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	target_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	relation_type TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (source_id, target_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_links_target ON sms_stakeholder_links(target_id);

-- Relationship continuity: ownership transfer / handover records (Std §16).
-- The review checklist flags record that history, risks and commitments were
-- reviewed before the handover completed.
CREATE TABLE IF NOT EXISTS sms_ownership_transfers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	from_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	to_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
	reason TEXT NOT NULL DEFAULT '',
	history_reviewed BOOLEAN NOT NULL DEFAULT 0,
	risks_reviewed BOOLEAN NOT NULL DEFAULT 0,
	commitments_reviewed BOOLEAN NOT NULL DEFAULT 0,
	notes TEXT NOT NULL DEFAULT '',
	completed_at DATETIME,
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_transfers_stakeholder ON sms_ownership_transfers(stakeholder_id);

-- Pilot success-measure baselines (Doc 7): metric_key → baseline/target.
CREATE TABLE IF NOT EXISTS sms_pilot_baselines (
	metric_key TEXT PRIMARY KEY,
	baseline_value REAL,
	target_value REAL,
	set_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- Node taxonomy: admin-configurable kinds, a generic hierarchy of nodes, and
-- role-based stakeholder↔node membership. Kinds/roles are data, not schema —
-- new structures (wards, precincts, asset registers) need no migration.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sms_node_kinds (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	key TEXT NOT NULL UNIQUE,                 -- 'building', 'road_segment', 'area'…
	label TEXT NOT NULL,                      -- "Building"
	description TEXT NOT NULL DEFAULT '',
	allows_children BOOLEAN NOT NULL DEFAULT 1,
	child_kinds TEXT NOT NULL DEFAULT '',     -- comma list of allowed child kind keys; '' = any
	is_system BOOLEAN NOT NULL DEFAULT 0,     -- seeded kinds can't be deleted (only edited)
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Allowed membership roles per kind — the mapping vocabulary is configurable.
CREATE TABLE IF NOT EXISTS sms_node_kind_roles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind_id INTEGER NOT NULL REFERENCES sms_node_kinds(id) ON DELETE CASCADE,
	role_key TEXT NOT NULL,                   -- 'owner', 'tenant', 'lessee'…
	label TEXT NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	UNIQUE (kind_id, role_key)
);

CREATE TABLE IF NOT EXISTS sms_nodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	external_id TEXT UNIQUE,
	kind_id INTEGER NOT NULL REFERENCES sms_node_kinds(id),
	parent_id INTEGER REFERENCES sms_nodes(id) ON DELETE CASCADE,  -- hierarchy; NULL = root
	name TEXT NOT NULL,                       -- "42 Main St" / "Main St kerbside works"
	description TEXT NOT NULL DEFAULT '',
	location_lat REAL, location_lon REAL, location_text TEXT NOT NULL DEFAULT '',
	custom_fields TEXT NOT NULL DEFAULT '{}', -- kind-specific attributes (access constraints…)
	entity_type TEXT, entity_id INTEGER,      -- optional P3M scope link (same pattern as sms_stakeholder_entities)
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sms_nodes_parent ON sms_nodes(parent_id);
CREATE INDEX IF NOT EXISTS idx_sms_nodes_kind ON sms_nodes(kind_id);

-- Generic mapping mechanism: stakeholder ↔ node with a kind-validated role.
CREATE TABLE IF NOT EXISTS sms_node_members (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id INTEGER NOT NULL REFERENCES sms_nodes(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT '',            -- must be in the kind's roles ('' = unrestricted kind)
	start_date DATE, end_date DATE,           -- optional period (tenancy etc.)
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (node_id, stakeholder_id, role)
);
CREATE INDEX IF NOT EXISTS idx_sms_node_members_stakeholder ON sms_node_members(stakeholder_id);

-- Seeded system kinds: editable via the API, not deletable. Idempotent so
-- fresh installs and migration re-runs converge on the same rows.
INSERT OR IGNORE INTO sms_node_kinds (key, label, description, allows_children, child_kinds, is_system) VALUES
	('area', 'Area', 'Top-level area or precinct grouping nodes.', 1, '', 1),
	('road_segment', 'Road segment', 'A stretch of road affected by works.', 1, 'building', 1),
	('building', 'Building', 'A building with occupants (owners and tenants).', 0, '', 1);

INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'affected_business', 'Affected business', 1 FROM sms_node_kinds WHERE key = 'road_segment';
INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'resident_representative', 'Resident representative', 2 FROM sms_node_kinds WHERE key = 'road_segment';
INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'owner', 'Owner', 1 FROM sms_node_kinds WHERE key = 'building';
INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'tenant', 'Tenant', 2 FROM sms_node_kinds WHERE key = 'building';
INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'lessee', 'Lessee', 3 FROM sms_node_kinds WHERE key = 'building';
INSERT OR IGNORE INTO sms_node_kind_roles (kind_id, role_key, label, sort_order) SELECT id, 'body_corporate_manager', 'Body corporate manager', 4 FROM sms_node_kinds WHERE key = 'building';

-- ---------------------------------------------------------------------------
-- Notification campaigns: a notice to be delivered, its node targets, and the
-- per-stakeholder delivery ledger ("have they been informed, by when?").
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sms_notification_campaigns (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	entity_type TEXT, entity_id INTEGER,      -- optional P3M scope
	channel TEXT NOT NULL DEFAULT 'email',    -- preferred channel; per-row channel may differ
	scheduled_at DATETIME,
	due_by DATE,                              -- must be informed by
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','scheduled','in_progress','completed','cancelled')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Campaign → node targets. include_descendants pulls the whole subtree's members.
CREATE TABLE IF NOT EXISTS sms_campaign_targets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	campaign_id INTEGER NOT NULL REFERENCES sms_notification_campaigns(id) ON DELETE CASCADE,
	node_id INTEGER NOT NULL REFERENCES sms_nodes(id) ON DELETE CASCADE,
	include_descendants BOOLEAN NOT NULL DEFAULT 0,
	UNIQUE (campaign_id, node_id)
);

-- Per-stakeholder delivery ledger — the "have they been informed" table.
CREATE TABLE IF NOT EXISTS sms_notifications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	campaign_id INTEGER NOT NULL REFERENCES sms_notification_campaigns(id) ON DELETE CASCADE,
	stakeholder_id INTEGER NOT NULL REFERENCES sms_stakeholders(id) ON DELETE CASCADE,
	node_id INTEGER REFERENCES sms_nodes(id) ON DELETE SET NULL,  -- which node they're notified about
	required_by DATE,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','read','acknowledged','failed','exempt')),
	channel TEXT NOT NULL DEFAULT '',
	sent_at DATETIME, acknowledged_at DATETIME,
	communication_id INTEGER REFERENCES sms_communications(id) ON DELETE SET NULL,  -- linked log entry when delivered
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (campaign_id, stakeholder_id)
);
CREATE INDEX IF NOT EXISTS idx_sms_notifications_campaign ON sms_notifications(campaign_id, status);

-- ---------------------------------------------------------------------------
-- RACI + work packages: versioned R/A/C/I matrices scoped to P3M entities
-- (mirrors srm_racis; assignees are SMS stakeholders and/or internal users)
-- and a work-package register tying performing contractors to impacted nodes.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sms_racis (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	entity_type TEXT NOT NULL CHECK (entity_type IN ('portfolio','programme','project')),
	entity_id INTEGER NOT NULL,
	version INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','preliminary','under_discussion','agreed')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	agreed_at DATETIME, last_reviewed_at DATE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (entity_type, entity_id, version)
);

CREATE TABLE IF NOT EXISTS sms_raci_assignments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	raci_id INTEGER NOT NULL REFERENCES sms_racis(id) ON DELETE CASCADE,
	activity_code TEXT NOT NULL,              -- "Notify affected residents", "Relocate water main"…
	description TEXT NOT NULL DEFAULT '',
	stakeholder_id INTEGER REFERENCES sms_stakeholders(id) ON DELETE SET NULL,  -- contractor / owner / tenant…
	user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,                    -- internal role
	value TEXT NOT NULL CHECK (value IN ('r','a','c','i'))
);
CREATE INDEX IF NOT EXISTS idx_sms_raci_assign ON sms_raci_assignments(raci_id, activity_code);

-- Work packages: physical activities with a performing contractor and impacted nodes.
CREATE TABLE IF NOT EXISTS sms_work_packages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
	entity_type TEXT, entity_id INTEGER,      -- optional P3M scope
	contractor_stakeholder_id INTEGER REFERENCES sms_stakeholders(id) ON DELETE SET NULL,
	status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','in_progress','completed','cancelled')),
	planned_start DATE, planned_end DATE, actual_start DATETIME, completed_at DATETIME,
	notes TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sms_work_package_nodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	work_package_id INTEGER NOT NULL REFERENCES sms_work_packages(id) ON DELETE CASCADE,
	node_id INTEGER NOT NULL REFERENCES sms_nodes(id) ON DELETE CASCADE,
	impact_notes TEXT NOT NULL DEFAULT '',
	UNIQUE (work_package_id, node_id)
);
