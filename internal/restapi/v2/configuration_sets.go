package v2

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"windshift/internal/models"
	"windshift/internal/repository"
	"windshift/internal/services"
)

type configurationSetDTO = models.ConfigurationSet

type configurationSetListMeta struct {
	Total int `json:"total"`
}

// configurationSetMutationResponse carries the persisted set plus the cache
// warnings the create flow can produce.
type configurationSetMutationResponse struct {
	ConfigurationSet *models.ConfigurationSet `json:"configuration_set"`
	Warnings         []models.APIWarning      `json:"warnings,omitempty"`
}

// requestInstance renders the request origin for export provenance metadata.
func requestInstance(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// configurationSetMutationError maps shared provisioning errors onto v2 error
// semantics. ServiceError values already carry the intended HTTP status.
func configurationSetMutationError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *services.ServiceError
	if errors.As(err, &serviceErr) {
		return newError(serviceErr.StatusCode, catalogErrorCode(serviceErr.StatusCode), serviceErr.Message)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return newError(http.StatusNotFound, "not_found", "Configuration set not found")
	}
	return internalError(err)
}

func registerConfigurationSetRoutes(b *routeBuilder, deps Deps) {
	read := []string{"configuration-sets:read"}
	write := []string{"configuration-sets:write"}

	b.PageMetadata("/configuration-sets", AuthAuthenticated, read, func(r *http.Request) ([]configurationSetDTO, Pagination, int, configurationSetListMeta, error) {
		page, limit := 1, 10
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
			page = v
		}
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
			limit = v
		}
		sets, total, err := deps.ConfigurationSetProvisioning.List(page, limit, r.URL.Query().Get("search"))
		if err != nil {
			return nil, Pagination{}, 0, configurationSetListMeta{}, internalError(err)
		}
		return sets, Pagination{Page: page, PageSize: limit}, total, configurationSetListMeta{Total: total}, nil
	})

	b.Read("/configuration-sets/{configuration_set_id}", AuthAuthenticated, read, func(r *http.Request) (configurationSetDTO, error) {
		id, err := pathID(r, "configuration_set_id")
		if err != nil {
			return configurationSetDTO{}, err
		}
		cs, err := deps.ConfigurationSetProvisioning.Get(id)
		if err != nil {
			return configurationSetDTO{}, configurationSetMutationError(err)
		}
		return *cs, nil
	})

	b.JSON(http.MethodPost, "/configuration-sets", http.StatusCreated, false, AuthAuthenticated, write, func(r *http.Request, input models.ConfigurationSet) (configurationSetMutationResponse, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return configurationSetMutationResponse{}, err
		}
		result, err := deps.ConfigurationSetProvisioning.Create(auditActorFromRequest(r), &input)
		if err != nil {
			return configurationSetMutationResponse{}, configurationSetMutationError(err)
		}
		return configurationSetMutationResponse{ConfigurationSet: result.Set, Warnings: result.Warnings}, nil
	})

	b.Command(http.MethodDelete, "/configuration-sets/{configuration_set_id}", AuthAuthenticated, write, func(r *http.Request) error {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return err
		}
		id, err := pathID(r, "configuration_set_id")
		if err != nil {
			return err
		}
		return configurationSetMutationError(deps.ConfigurationSetProvisioning.Delete(auditActorFromRequest(r), id))
	})

	// Export writes the portable template document directly — no v2 data
	// envelope — matching the browser download contract.
	b.RawResponse[*services.ConfigSetTemplate](http.MethodGet, "/configuration-sets/{configuration_set_id}/export", http.StatusOK, "application/json", AuthAuthenticated, read, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "configuration_set_id")
		if err != nil {
			return err
		}
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return err
		}
		user, err := principal(r)
		if err != nil {
			return err
		}
		tpl, err := deps.ConfigurationSetExport.Export(r.Context(), id, &services.ConfigSetExportBy{Username: user.Username, Instance: requestInstance(r)})
		if err != nil {
			if errors.Is(err, services.ErrCannotExportDefault) {
				deps.ConfigurationSetProvisioning.AuditExport(auditActor(r, user), id, false)
				return newError(http.StatusForbidden, "default_not_exportable",
					"The default configuration set cannot be exported; clone it first if you need a portable copy.")
			}
			return internalError(err)
		}
		deps.ConfigurationSetProvisioning.AuditExport(auditActor(r, user), id, true)
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(tpl)
	})
}
