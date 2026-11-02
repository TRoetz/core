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
	Filters      []PlanFilter      `json:"filters,omitempty"`
}

// PlanFilter is a named CQL query contributing items to a plan in addition to
// (or instead of) linked workspaces.
type PlanFilter struct {
	ID        int       `json:"id"`
	PlanID    int       `json:"plan_id"`
	Name      string    `json:"name"`
	CQLQuery  string    `json:"cql_query"`
	CreatedAt time.Time `json:"created_at"`
}

// Item dependency types.
const (
	ItemDepBlocks  = "blocks"
	ItemDepRelated = "related"
)

// ItemDependency links two items: SourceItemID blocks TargetItemID (or is
// merely related to it). Links are global to items; plans expose the subset
// where both ends belong to the plan.
type ItemDependency struct {
	ID           int       `json:"id"`
	SourceItemID int       `json:"source_item_id"`
	TargetItemID int       `json:"target_item_id"`
	DepType      string    `json:"dep_type"`
	CreatedBy    *int      `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`

	// Joined item details (populated by scoped reads).
	SourceItemKey   string `json:"source_item_key,omitempty"`
	SourceItemTitle string `json:"source_item_title,omitempty"`
	TargetItemKey   string `json:"target_item_key,omitempty"`
	TargetItemTitle string `json:"target_item_title,omitempty"`
}

// Plan scenario statuses.
const (
	PlanScenarioDraft     = "draft"
	PlanScenarioActive    = "active"
	PlanScenarioArchived  = "archived"
)

// PlanScenario is a "what if" snapshot of a plan: per-item date overrides plus
// computed downstream impact through item_dependencies.
type PlanScenario struct {
	ID          int       `json:"id"`
	PlanID      int       `json:"plan_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	ItemCount int `json:"item_count,omitempty"`
}

// PlanScenarioItem is one item's override inside a scenario. NULL date fields
// fall back to the item's current values; ShiftDays and DownstreamCount are
// computed on read, never stored.
type PlanScenarioItem struct {
	ID         int        `json:"id"`
	ScenarioID int        `json:"scenario_id"`
	ItemID     int        `json:"item_id"`
	StartDate  *time.Time `json:"start_date,omitempty"`
	EndDate    *time.Time `json:"end_date,omitempty"`
	DueDate    *time.Time `json:"due_date,omitempty"`
	Notes      string     `json:"notes,omitempty"`

	// Joined item details.
	ItemKey   string `json:"item_key,omitempty"`
	ItemTitle string `json:"item_title,omitempty"`

	// Computed impact: effective (override-or-base) dates and how far the
	// end date moved relative to base, plus how many downstream dependents
	// inherit the shift.
	BaseStartDate *time.Time `json:"base_start_date,omitempty"`
	BaseEndDate   *time.Time `json:"base_end_date,omitempty"`
	EffectiveEnd  *time.Time `json:"effective_end_date,omitempty"`
	ShiftDays     int        `json:"shift_days,omitempty"`
	Downstream    int        `json:"downstream_count,omitempty"`
}

// PlanItem is one aggregated work item row for the plan views (list, board,
// timeline, calendar). It carries everything a view needs without extra
// round trips.
type PlanItem struct {
	ID              int        `json:"id"`
	Key             string     `json:"key"`
	Title           string     `json:"title"`
	ItemTypeName    string     `json:"item_type_name,omitempty"`
	StatusID        int        `json:"status_id"`
	StatusName      string     `json:"status_name"`
	StatusCategory  string     `json:"status_category,omitempty"` // status_categories.builtin_key
	IsCompleted     bool       `json:"is_completed"`              // status category is_completed flag
	WorkspaceID     int        `json:"workspace_id"`
	WorkspaceKey    string     `json:"workspace_key"`
	WorkspaceName   string     `json:"workspace_name"`
	ParentID        *int       `json:"parent_id,omitempty"`
	TeamID          *int       `json:"team_id,omitempty"`
	TeamName        string     `json:"team_name,omitempty"`
	AssigneeID      *int       `json:"assignee_id,omitempty"`
	AssigneeName      string     `json:"assignee_name,omitempty"`
	AssigneeAvatarURL string     `json:"assignee_avatar_url,omitempty"`
	StartDate       *time.Time `json:"start_date,omitempty"`
	EndDate         *time.Time `json:"end_date,omitempty"`
	DueDate         *time.Time `json:"due_date,omitempty"`
	StoryPoints     *float64   `json:"story_points,omitempty"`
	EstimateMinutes *int       `json:"estimate_minutes,omitempty"`
}

// PlanCount is a name/count pair for summary breakdowns.
type PlanCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Color string `json:"color,omitempty"`
}

// PlanWorkspaceStat is per-workspace progress inside a plan.
type PlanWorkspaceStat struct {
	WorkspaceID   int     `json:"workspace_id"`
	WorkspaceKey  string  `json:"workspace_key"`
	WorkspaceName string  `json:"workspace_name"`
	ItemCount     int     `json:"item_count"`
	DoneCount     int     `json:"done_count"`
	ProgressPct   float64 `json:"progress_pct"`
}

// PlanSummary is the aggregate payload for the plan Summary view.
type PlanSummary struct {
	TotalItems  int                 `json:"total_items"`
	DoneItems   int                 `json:"done_items"`
	ProgressPct float64             `json:"progress_pct"`
	ByStatus    []PlanCount         `json:"by_status"`
	ByType      []PlanCount         `json:"by_type"`
	ByWorkspace []PlanWorkspaceStat `json:"by_workspace"`
	Upcoming    []PlanItem          `json:"upcoming"` // open items due within 14 days, soonest first
}

// PlanTeamMember is one member's plan workload.
type PlanTeamMember struct {
	UserID    int    `json:"user_id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	ItemCount int    `json:"item_count"`
}

// PlanTeamStat is one team's plan workload and capacity for the Teams view.
type PlanTeamStat struct {
	TeamID               int              `json:"team_id"`
	Name                 string           `json:"name"`
	Color                string           `json:"color,omitempty"`
	MemberCount          int              `json:"member_count"`
	Members              []PlanTeamMember `json:"members,omitempty"`
	ItemCount            int              `json:"item_count"`             // items in the plan on this team (direct or via assignee)
	OpenItemCount        int              `json:"open_item_count"`        // of those, not completed
	TotalEstimateMinutes int64            `json:"total_estimate_minutes"` // open items only
	TotalStoryPoints     float64          `json:"total_story_points,omitempty"`
	CapacityUnit         string           `json:"capacity_unit,omitempty"`  // points|days|hours; "" = unset
	WeeklyCapacity       *float64         `json:"weekly_capacity,omitempty"`
	Overloaded           bool             `json:"overloaded"`
}

// PlanTeams is the payload for GET /plans/{id}/teams.
type PlanTeams struct {
	Teams []PlanTeamStat `json:"teams"`
}

// PlanScenarioImpact summarizes how far a scenario pushes the plan out.
type PlanScenarioImpact struct {
	AffectedItemCount int `json:"affected_item_count"` // items with any non-zero shift after propagation
	MaxShiftDays      int `json:"max_shift_days"`      // largest end-date slip in days (negative = pulled earlier)
}

// PlanScenarioDetail is the payload for GET /plans/{id}/scenarios/{sid}: the
// scenario, its overrides plus every item shifted by dependency propagation,
// and aggregate impact.
type PlanScenarioDetail struct {
	Scenario PlanScenario        `json:"scenario"`
	Items    []PlanScenarioItem  `json:"items"`
	Impact   PlanScenarioImpact  `json:"impact"`
}
