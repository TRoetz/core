package models

import "time"

// Plan statuses.
const (
	PlanStatusDraft     = "draft"
	PlanStatusActive    = "active"
	PlanStatusCompleted = "completed"
	PlanStatusArchived  = "archived"
)

// P3M entity types attachable to a plan (mirrors sms_stakeholder_entities).
const (
	PlanEntityPortfolio = "portfolio"
	PlanEntityProgramme = "programme"
	PlanEntityProject   = "project"
)

// Plan is a first-class planning container that groups multiple
// workspaces/projects together and can additionally reference P3M entities
// and SMS stakeholders for cross-workspace coordination.
type Plan struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Color       string     `json:"color"`
	OwnerID     *int       `json:"owner_id,omitempty"`
	StartDate   *time.Time `json:"start_date,omitempty"`
	EndDate     *time.Time `json:"end_date,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Populated by list queries.
	WorkspaceCount  int `json:"workspace_count,omitempty"`
	P3MEntityCount  int `json:"p3m_entity_count,omitempty"`
	StakeholderCount int `json:"stakeholder_count,omitempty"`
}

// PlanWorkspace links a plan to a workspace (the core grouping).
type PlanWorkspace struct {
	PlanID      int       `json:"plan_id"`
	WorkspaceID int       `json:"workspace_id"`
	CreatedAt   time.Time `json:"created_at"`

	// Joined details (populated by list queries).
	WorkspaceName string `json:"workspace_name,omitempty"`
	WorkspaceKey  string `json:"workspace_key,omitempty"`
	ItemCount     int    `json:"item_count,omitempty"`
}

// PlanP3MEntity links a plan to a P3M portfolio, programme, or project.
type PlanP3MEntity struct {
	PlanID     int       `json:"plan_id"`
	EntityType string    `json:"entity_type"`
	EntityID   int       `json:"entity_id"`
	CreatedAt  time.Time `json:"created_at"`

	// Joined details (populated by list queries).
	EntityTitle string `json:"entity_title,omitempty"`
}

// PlanStakeholder links a plan to an SMS stakeholder.
type PlanStakeholder struct {
	PlanID        int       `json:"plan_id"`
	StakeholderID int       `json:"stakeholder_id"`
	CreatedAt     time.Time `json:"created_at"`

	// Joined details (populated by list queries).
	Name               string `json:"name,omitempty"`
	RoleClassification string `json:"role_classification,omitempty"`
	Organization       string `json:"organization,omitempty"`
}

// PlanDetail is the aggregate payload of a single plan with its links.
type PlanDetail struct {
	Plan         Plan              `json:"plan"`
	Workspaces   []PlanWorkspace   `json:"workspaces"`
	P3MEntities  []PlanP3MEntity   `json:"p3m_entities"`
	Stakeholders []PlanStakeholder `json:"stakeholders"`
}
