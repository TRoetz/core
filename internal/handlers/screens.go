package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/objecttranslation"
	"windshift/internal/repository"
	"windshift/internal/services"
	"windshift/internal/utils"
)

type ScreenHandler struct {
	db           database.Database
	provisioning *services.ScreenProvisioningService
	translations *objecttranslation.Service
}

func (h *ScreenHandler) WithObjectTranslations(service *objecttranslation.Service) *ScreenHandler {
	h.translations = service
	return h
}

func NewScreenHandler(db database.Database) *ScreenHandler {
	return &ScreenHandler{db: db, provisioning: services.NewScreenProvisioningService(db)}
}

func (h *ScreenHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	screens, err := h.provisioning.ListScreens(r.URL.Query().Get("include_fields") == "true")
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !localizeObjectResponse(w, r, h.translations, "screen", screens) {
		return
	}
	respondJSONOK(w, screens)
}

func (h *ScreenHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	screen, err := h.provisioning.GetScreen(id)
	if err != nil {
		respondScreenError(w, r, err)
		return
	}
	if !localizeObjectResponse(w, r, h.translations, "screen", screen) {
		return
	}
	respondJSONOK(w, screen)
}

func (h *ScreenHandler) loadScreen(id int) (*models.Screen, error) {
	return h.provisioning.GetScreen(id)
}

// LoadScreen exposes screen loading to other handlers (request-type flows).
func (h *ScreenHandler) LoadScreen(id int) (*models.Screen, error) {
	return h.loadScreen(id)
}

func (h *ScreenHandler) Create(w http.ResponseWriter, r *http.Request) {
	screen, ok := decodeJSON[models.Screen](w, r)
	if !ok {
		return
	}

	created, warnings, err := h.provisioning.Create(serviceActor(r), &screen)
	if err != nil {
		respondScreenError(w, r, err)
		return
	}

	if !localizeObjectResponse(w, r, h.translations, "screen", created) {
		return
	}
	respondJSONCreated(w, struct {
		models.Screen
		Warnings []string `json:"warnings,omitempty"`
	}{*created, warnings})
}

func (h *ScreenHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	screen, ok := decodeJSON[models.Screen](w, r)
	if !ok {
		return
	}

	updated, warnings, err := h.provisioning.Update(serviceActor(r), id, &screen)
	if err != nil {
		respondScreenError(w, r, err)
		return
	}

	if !localizeObjectResponse(w, r, h.translations, "screen", updated) {
		return
	}
	respondJSONOK(w, struct {
		models.Screen
		Warnings []string `json:"warnings,omitempty"`
	}{*updated, warnings})
}

func (h *ScreenHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	if err := h.provisioning.Delete(serviceActor(r), id); err != nil {
		respondScreenError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetFields returns the fields configured for a screen.
func (h *ScreenHandler) GetFields(w http.ResponseWriter, r *http.Request) {
	screenID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	fields, err := h.provisioning.GetScreenFields(screenID)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if fields == nil {
		fields = []models.ScreenField{}
	}
	respondJSONOK(w, fields)
}

// UpdateFields replaces the screen's field configuration.
func (h *ScreenHandler) UpdateFields(w http.ResponseWriter, r *http.Request) {
	screenID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	fields, ok := decodeJSON[[]models.ScreenField](w, r)
	if !ok {
		return
	}

	updatedFields, err := h.provisioning.ReplaceScreenFields(serviceActor(r), screenID, fields)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, updatedFields)
}

// UpdateSystemFields replaces the screen's system-field visibility.
func (h *ScreenHandler) UpdateSystemFields(w http.ResponseWriter, r *http.Request) {
	screenID, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	systemFields, ok := decodeJSON[[]string](w, r)
	if !ok {
		return
	}

	updatedSystemFields, err := h.provisioning.ReplaceScreenSystemFields(serviceActor(r), screenID, systemFields)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	respondJSONOK(w, updatedSystemFields)
}

// respondScreenError maps shared screen provisioning errors onto the v1
// response shapes: validation failures as 400, not-found as 404.
func respondScreenError(w http.ResponseWriter, r *http.Request, err error) {
	if se, ok := err.(*services.ServiceError); ok {
		switch se.StatusCode {
		case 404:
			respondNotFound(w, r, "screen")
		default:
			respondValidationError(w, r, se.Message)
		}
		return
	}
	respondInternalError(w, r, err)
}

var (
	_ = errors.Is
	_ = sql.ErrNoRows
	_ = json.Marshal
	_ = repository.ErrNotFound
	_ = utils.GetCurrentUser
)
