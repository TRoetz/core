package handlers

import (
	"net/http"
	"strconv"

	"windshift/internal/database"
	"windshift/internal/models"
	"windshift/internal/objecttranslation"
	"windshift/internal/repository"
	"windshift/internal/services"
)

type ConfigurationSetHandler struct {
	db                  database.Database
	repo                *repository.ConfigurationSetRepository
	notificationService interface {
		ForceRefreshCache() error
	} // Notification service for cache refresh (optional, can be nil)
	permissionService *services.PermissionService
	translations      *objecttranslation.Service
	provisioning      *services.ConfigurationSetProvisioningService
}

func (h *ConfigurationSetHandler) WithObjectTranslations(service *objecttranslation.Service) *ConfigurationSetHandler {
	h.translations = service
	return h
}

func NewConfigurationSetHandler(db database.Database, notificationService interface{ ForceRefreshCache() error }, permissionService *services.PermissionService) *ConfigurationSetHandler {
	repo := repository.NewConfigurationSetRepository(db)
	return &ConfigurationSetHandler{
		db:                  db,
		repo:                repo,
		notificationService: notificationService,
		permissionService:   permissionService,
		provisioning:        services.NewConfigurationSetProvisioningService(db, repo, permissionService, notificationService),
	}
}

func (h *ConfigurationSetHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	page := 1
	limit := 10 // Default to 10 configuration sets per page

	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Parse search parameter
	search := r.URL.Query().Get("search")

	// Use repository to fetch configuration sets with all relations
	configSets, totalCount, err := h.repo.List(page, limit, search)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	// Create paginated response
	response := models.PaginatedConfigurationSetsResponse{
		ConfigurationSets: configSets,
		Pagination: models.PaginationMeta{
			Page:       page,
			Limit:      limit,
			Total:      totalCount,
			TotalPages: (totalCount + limit - 1) / limit,
		},
	}
	if !localizeObjectResponse(w, r, h.translations, "configuration_set", response.ConfigurationSets) {
		return
	}

	respondJSONOK(w, response)
}

func (h *ConfigurationSetHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}

	// Use repository to fetch configuration set with all relations
	cs, err := h.repo.FindByID(id)
	if err == repository.ErrNotFound {
		respondNotFound(w, r, "configuration_set")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	if !localizeObjectResponse(w, r, h.translations, "configuration_set", cs) {
		return
	}

	respondJSONOK(w, cs)
}

func (h *ConfigurationSetHandler) Create(w http.ResponseWriter, r *http.Request) {
	cs, ok := decodeJSON[models.ConfigurationSet](w, r)
	if !ok {
		return
	}

	result, err := h.provisioning.Create(serviceActor(r), &cs)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if !localizeObjectResponse(w, r, h.translations, "configuration_set", result.Set) {
		return
	}
	respondJSONCreatedWithWarnings(w, result.Set, result.Warnings)
}

func (h *ConfigurationSetHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
