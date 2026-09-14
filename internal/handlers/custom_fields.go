package handlers

import (
	"log/slog"
	"net/http"

	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/scheduler"
	"windshift/internal/services"
)

type CustomFieldHandler struct {
	db           database.Database
	provisioning *services.CustomFieldProvisioningService
}

func NewCustomFieldHandler(db database.Database) *CustomFieldHandler {
	return &CustomFieldHandler{
		db: db,
		provisioning: services.NewCustomFieldProvisioningService(db, services.CustomFieldProvisioningHooks{
			EnqueueOptionRemoval:     scheduler.EnqueueOptionRemoval,
			EnqueueFieldCleanup:      scheduler.EnqueueFieldCleanup,
			EnqueueIndexBuild:        scheduler.EnqueueIndexBuild,
			CancelPendingIndexBuilds: scheduler.CancelPendingIndexBuilds,
		}),
	}
}

// logAndRespondDatabaseError is referenced by core-tests overlay code via
// the shared handler type; nolint keeps the production linter from flagging
// the cross-repository usage it cannot see.
//
//nolint:unused // used by core-tests overlay GetAll/Get
func (h *CustomFieldHandler) logAndRespondDatabaseError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("database error in custom field handler", slog.String("component", "custom_fields"), slog.Any("error", err))
	respondInternalError(w, r, err)
}

// updateRequest extends the custom field definition with optional indexing control
type updateRequest struct {
	models.CustomFieldDefinition
	Indexed *models.CustomFieldIndexInfo `json:"indexed,omitempty"`
}

// updateResponse returns the updated custom field plus any index builds that
// were deferred. SQLite cannot build indexes concurrently, so enabling an index
// records the desired state and the physical index is created on next restart.
type updateResponse struct {
	models.CustomFieldDefinition
	IndexingDeferred *models.CustomFieldIndexInfo `json:"indexing_deferred,omitempty"`
}

type customFieldSettings struct {
	MaxIndexesPerTable int `json:"max_indexes_per_table"`
}

// Create handles POST requests to create a new custom field.
func (h *CustomFieldHandler) Create(w http.ResponseWriter, r *http.Request) {
	cf, ok := decodeJSON[models.CustomFieldDefinition](w, r)
	if !ok {
		return
	}

	result, err := h.provisioning.Create(serviceActor(r), &cf)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSONCreated(w, struct {
		*models.CustomFieldDefinition
		Warnings []string `json:"warnings,omitempty"`
	}{result.Field, result.Warnings})
}

// Update handles PUT requests to update an existing custom field.
func (h *CustomFieldHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[updateRequest](w, r)
	if !ok {
		return
	}

	result, err := h.provisioning.Update(serviceActor(r), id, services.CustomFieldUpdateInput{
		Definition: req.CustomFieldDefinition,
		Indexed:    req.Indexed,
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if result.IndexingDeferred {
		respondJSONOK(w, struct {
			updateResponse
			Warnings []string `json:"warnings,omitempty"`
		}{updateResponse{
			CustomFieldDefinition: *result.Field,
			IndexingDeferred:      result.DeferredIndexes,
		}, result.Warnings})
		return
	}

	respondJSONOK(w, struct {
		*models.CustomFieldDefinition
		Warnings []string `json:"warnings,omitempty"`
	}{result.Field, result.Warnings})
}

// Delete handles DELETE requests to remove a custom field.
func (h *CustomFieldHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	if err := h.provisioning.Delete(serviceActor(r), id); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateSettings handles PUT requests for custom-field indexing settings.
func (h *CustomFieldHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	settings, ok := decodeJSON[customFieldSettings](w, r)
	if !ok {
		return
	}

	if err := h.provisioning.UpdateSettings(serviceActor(r), settings.MaxIndexesPerTable); err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSONOK(w, settings)
}
