package v2

import (
	"errors"
	"net/http"
	"time"

	"windshift/internal/models"
	"windshift/internal/services"
)

type customFieldCreateRequest struct {
	Name                           string `json:"name"`
	FieldType                      string `json:"field_type"`
	Description                    string `json:"description"`
	Required                       bool   `json:"required"`
	Options                        string `json:"options"`
	DisplayOrder                   int    `json:"display_order"`
	AppliesToPortalCustomers       bool   `json:"applies_to_portal_customers"`
	AppliesToCustomerOrganisations bool   `json:"applies_to_customer_organisations"`
}

type customFieldPatchRequest struct {
	Name                           Optional[string]             `json:"name"`
	FieldType                      Optional[string]             `json:"field_type"`
	Description                    Optional[string]             `json:"description"`
	Required                       Optional[bool]               `json:"required"`
	Options                        Optional[string]             `json:"options"`
	DisplayOrder                   Optional[int]                `json:"display_order"`
	AppliesToPortalCustomers       Optional[bool]               `json:"applies_to_portal_customers"`
	AppliesToCustomerOrganisations Optional[bool]               `json:"applies_to_customer_organisations"`
	Indexed                        *models.CustomFieldIndexInfo `json:"indexed,omitempty"`
}

type customFieldSettingsRequest struct {
	MaxIndexesPerTable int `json:"max_indexes_per_table"`
}

type customFieldMutationDTO struct {
	ID                             int       `json:"id"`
	Name                           string    `json:"name"`
	FieldType                      string    `json:"field_type"`
	Description                    string    `json:"description,omitempty"`
	Required                       bool      `json:"required"`
	Options                        string    `json:"options,omitempty"`
	DisplayOrder                   int       `json:"display_order"`
	SystemDefault                  bool      `json:"system_default"`
	AppliesToPortalCustomers       bool      `json:"applies_to_portal_customers"`
	AppliesToCustomerOrganisations bool      `json:"applies_to_customer_organisations"`
	CreatedAt                      time.Time `json:"created_at"`
	UpdatedAt                      time.Time `json:"updated_at"`
}

func customFieldDTOFromModel(cf *models.CustomFieldDefinition) customFieldMutationDTO {
	return customFieldMutationDTO{
		ID:                             cf.ID,
		Name:                           cf.Name,
		FieldType:                      cf.FieldType,
		Description:                    cf.Description,
		Required:                       cf.Required,
		Options:                        cf.Options,
		DisplayOrder:                   cf.DisplayOrder,
		SystemDefault:                  cf.SystemDefault,
		AppliesToPortalCustomers:       cf.AppliesToPortalCustomers,
		AppliesToCustomerOrganisations: cf.AppliesToCustomerOrganisations,
		CreatedAt:                      cf.CreatedAt,
		UpdatedAt:                      cf.UpdatedAt,
	}
}

type customFieldMutationResponse struct {
	CustomField      customFieldMutationDTO       `json:"custom_field"`
	Warnings         []string                     `json:"warnings,omitempty"`
	IndexingDeferred *models.CustomFieldIndexInfo `json:"indexing_deferred,omitempty"`
}

// customFieldMutationError maps shared provisioning errors onto v2 error
// semantics. ServiceError values already carry the intended HTTP status.
func customFieldMutationError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *services.ServiceError
	if errors.As(err, &serviceErr) {
		return newError(serviceErr.StatusCode, catalogErrorCode(serviceErr.StatusCode), serviceErr.Message)
	}
	return internalError(err)
}

func registerCustomFieldMutationRoutes(b *routeBuilder, deps Deps) {
	write := []string{"custom-fields:write"}

	b.JSON(http.MethodPost, "/custom-fields", http.StatusCreated, false, AuthAuthenticated, write, func(r *http.Request, input customFieldCreateRequest) (customFieldMutationResponse, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return customFieldMutationResponse{}, err
		}
		result, err := deps.CustomFieldProvisioning.Create(auditActorFromRequest(r), &models.CustomFieldDefinition{
			Name:                           input.Name,
			FieldType:                      input.FieldType,
			Description:                    input.Description,
			Required:                       input.Required,
			Options:                        input.Options,
			DisplayOrder:                   input.DisplayOrder,
			AppliesToPortalCustomers:       input.AppliesToPortalCustomers,
			AppliesToCustomerOrganisations: input.AppliesToCustomerOrganisations,
		})
		if err != nil {
			return customFieldMutationResponse{}, customFieldMutationError(err)
		}
		return customFieldMutationResponse{CustomField: customFieldDTOFromModel(result.Field), Warnings: result.Warnings}, nil
	})

	b.JSON(http.MethodPatch, "/custom-fields/{custom_field_id}", http.StatusOK, true, AuthAuthenticated, write, func(r *http.Request, input customFieldPatchRequest) (customFieldMutationResponse, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return customFieldMutationResponse{}, err
		}
		id, err := pathID(r, "custom_field_id")
		if err != nil {
			return customFieldMutationResponse{}, err
		}
		current, err := deps.Configuration.GetCustomField(id)
		if err != nil {
			return customFieldMutationResponse{}, customFieldMutationError(err)
		}
		definition := customFieldDefinitionFromResult(current)
		applyCustomFieldPatch(&definition, input)
		result, err := deps.CustomFieldProvisioning.Update(auditActorFromRequest(r), id, services.CustomFieldUpdateInput{
			Definition: definition,
			Indexed:    input.Indexed,
		})
		if err != nil {
			return customFieldMutationResponse{}, customFieldMutationError(err)
		}
		return customFieldMutationResponse{
			CustomField:      customFieldDTOFromModel(result.Field),
			Warnings:         result.Warnings,
			IndexingDeferred: result.DeferredIndexes,
		}, nil
	})

	b.Command(http.MethodDelete, "/custom-fields/{custom_field_id}", AuthAuthenticated, write, func(r *http.Request) error {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return err
		}
		id, err := pathID(r, "custom_field_id")
		if err != nil {
			return err
		}
		return customFieldMutationError(deps.CustomFieldProvisioning.Delete(auditActorFromRequest(r), id))
	})

	b.JSON(http.MethodPut, "/custom-fields/settings", http.StatusOK, false, AuthAuthenticated, write, func(r *http.Request, input customFieldSettingsRequest) (customFieldSettingsRequest, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return customFieldSettingsRequest{}, err
		}
		if err := deps.CustomFieldProvisioning.UpdateSettings(auditActorFromRequest(r), input.MaxIndexesPerTable); err != nil {
			return customFieldSettingsRequest{}, customFieldMutationError(err)
		}
		return input, nil
	})
}

func customFieldDefinitionFromResult(result *services.CustomFieldResult) models.CustomFieldDefinition {
	return models.CustomFieldDefinition{
		ID:                             result.ID,
		Name:                           result.Name,
		FieldType:                      result.FieldType,
		Description:                    result.Description,
		Required:                       result.Required,
		Options:                        result.Options,
		DisplayOrder:                   result.DisplayOrder,
		SystemDefault:                  result.SystemDefault,
		AppliesToPortalCustomers:       result.AppliesToPortalCustomers,
		AppliesToCustomerOrganisations: result.AppliesToCustomerOrganisations,
	}
}

func auditActorFromRequest(r *http.Request) services.AuditActor {
	user, err := principal(r)
	if err != nil {
		return services.AuditActor{}
	}
	return auditActor(r, user)
}

func applyCustomFieldPatch(definition *models.CustomFieldDefinition, input customFieldPatchRequest) {
	if input.Name.Set {
		definition.Name = input.Name.Value
	}
	if input.FieldType.Set {
		definition.FieldType = input.FieldType.Value
	}
	if input.Description.Set {
		definition.Description = input.Description.Value
	}
	if input.Required.Set {
		definition.Required = input.Required.Value
	}
	if input.Options.Set {
		definition.Options = input.Options.Value
	}
	if input.DisplayOrder.Set {
		definition.DisplayOrder = input.DisplayOrder.Value
	}
	if input.AppliesToPortalCustomers.Set {
		definition.AppliesToPortalCustomers = input.AppliesToPortalCustomers.Value
	}
	if input.AppliesToCustomerOrganisations.Set {
		definition.AppliesToCustomerOrganisations = input.AppliesToCustomerOrganisations.Value
	}
}
