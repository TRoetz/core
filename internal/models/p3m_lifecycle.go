package models

import "time"

// Default seed stage keys for the configurable P3M lifecycle (BRD §5.2/§5.6).
// Runtime validation always reads p3m_lifecycle_stages, which is seeded with
// these keys and admin-configurable from there on.
const (
	P3MLifecycleStageFrontDoor           = "front_door"
	P3MLifecycleStageProposal            = "proposal"
	P3MLifecycleStageDefinePlanning      = "define_planning"
	P3MLifecycleStageImplementDeliver    = "implement_deliver"
	P3MLifecycleStageClose               = "close"
	P3MLifecycleStageBenefitsRealisation = "benefits_realisation"
)

// P3M work types (BRD §5.5). Gate templates and Front Door intakes filter on
// these; a project with no work type matches only NULL-work-type templates.
const (
	P3MWorkTypeProgramme             = "programme"
	P3MWorkTypeProject               = "project"
	P3MWorkTypeContinuousImprovement = "continuous_improvement"
	P3MWorkTypeDigitalProduct        = "digital_product"
	P3MWorkTypeOperational           = "operational"
)

// P3MLifecycleStage is one admin-configurable stage in the global P3M
// lifecycle. StageKey is a slug, immutable after creation; Name is display
// data (not i18n). SortOrder defines path position and must be unique.
type P3MLifecycleStage struct {
	ID           int       `json:"id"`
	StageKey     string    `json:"stage_key"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	SortOrder    int       `json:"sort_order"`
	IsEntry      bool      `json:"is_entry"`
	IsExit       bool      `json:"is_exit"`
	GateRequired bool      `json:"gate_required"`
	Skippable    bool      `json:"skippable"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// P3MStageGateTemplateArtifact is one checklist item on a gate template.
type P3MStageGateTemplateArtifact struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// P3MStageGateTemplate defines a mandatory or optional governance gate for a
// stage transition. FromStageKey nil means "from anywhere"; WorkType nil
// means the template applies to every work type. RequiredArtifacts is stored
// as JSON and pre-populates the artifact checklist of reviews created from
// this template.
type P3MStageGateTemplate struct {
	ID               int                            `json:"id"`
	Name             string                         `json:"name"`
	FromStageKey     *string                        `json:"from_stage_key,omitempty"`
	ToStageKey       string                         `json:"to_stage_key"`
	Mandatory        bool                           `json:"mandatory"`
	WorkType         *string                        `json:"work_type,omitempty"`
	RequiredArtifacts []P3MStageGateTemplateArtifact `json:"required_artifacts"`
	Description      string                         `json:"description"`
	Enabled          bool                           `json:"enabled"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
}

// P3MProjectStageSkip records a proportional skip of one stage for one
// project: the decision trail that unlocks shortcut transitions past the
// skipped (skippable) stages.
type P3MProjectStageSkip struct {
	ID        int       `json:"id"`
	ProjectID int       `json:"project_id"`
	StageKey  string    `json:"stage_key"`
	Reason    string    `json:"reason"`
	DecidedBy int       `json:"decided_by"`
	DecidedAt time.Time `json:"decided_at"`
}

// P3MProjectGateStatus is one gate template's status for a project, as
// returned by GET /p3m/projects/{id}/gates. Status values: not_started,
// in_progress (a review exists but has not passed), passed, skipped (the
// target stage carries a recorded skip for this project).
type P3MProjectGateStatus struct {
	Template *P3MStageGateTemplate `json:"template"`
	Status   string                `json:"status"`
}

// P3MLifecyclePathPreview is one work type's resulting stage path: the
// enabled stages in order plus the transitions that carry an applicable
// mandatory gate for that work type.
type P3MLifecyclePathPreview struct {
	WorkType         string   `json:"work_type"` // "" = all/default
	StageKeys        []string `json:"stage_keys"`
	MandatoryGates   []string `json:"mandatory_gates"` // "from->to" pairs
}

// P3MLifecycleConfig is the full lifecycle configuration returned by
// GET /p3m/config/lifecycle.
type P3MLifecycleConfig struct {
	Stages        []P3MLifecycleStage      `json:"stages"`
	GateTemplates []P3MStageGateTemplate   `json:"gate_templates"`
	PathPreviews  []P3MLifecyclePathPreview `json:"path_previews"`
}

// P3MProjectGates is the project-side gate view: current stage plus the
// pending/next gate templates for transitions out of it.
type P3MProjectGates struct {
	ProjectID    int                    `json:"project_id"`
	CurrentStage string                 `json:"current_stage"`
	Gates        []P3MProjectGateStatus `json:"gates"`
}
