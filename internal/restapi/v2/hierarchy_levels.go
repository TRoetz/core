package v2

import (
	"errors"
	"net/http"
	"time"

	"windshift/internal/models"
	"windshift/internal/services"
)

type hierarchyLevelDTO struct {
	ID                 int       `json:"id"`
	BuiltinKey         *string   `json:"builtin_key"`
	Level              int       `json:"level"`
	Name               string    `json:"name"`
	DisplayName        string    `json:"display_name,omitempty"`
	Description        string    `json:"description"`
	DisplayDescription string    `json:"display_description,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func hierarchyLevelFromEntity(entity services.EnumEntity) hierarchyLevelDTO {
	h, ok := entity.(*models.HierarchyLevel)
	if !ok {
		return hierarchyLevelDTO{}
	}
	builtinKey := nullableString(h.BuiltinKey)
	return hierarchyLevelDTO{
		ID:          h.ID,
		BuiltinKey:  builtinKey,
		Level:       h.Level,
		Name:        h.Name,
		Description: h.Description,
		CreatedAt:   h.CreatedAt,
		UpdatedAt:   h.UpdatedAt,
	}
}

type hierarchyLevelRequest struct {
	Level       int    `json:"level"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// hierarchyLevelMutationError maps shared service errors onto v2 error
// semantics. ServiceError values already carry the intended HTTP status.
func hierarchyLevelMutationError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *services.ServiceError
	if errors.As(err, &serviceErr) {
		return newError(serviceErr.StatusCode, catalogErrorCode(serviceErr.StatusCode), serviceErr.Message)
	}
	return internalError(err)
}

func registerHierarchyLevelRoutes(b *routeBuilder, deps Deps) {
	read := []string{"hierarchy-levels:read"}
	write := []string{"hierarchy-levels:write"}

	b.Read("/hierarchy-levels", AuthAuthenticated, read, func(r *http.Request) ([]hierarchyLevelDTO, error) {
		entities, err := deps.HierarchyLevels.GetAll()
		if err != nil {
			return nil, internalError(err)
		}
		items := make([]hierarchyLevelDTO, len(entities))
		for i, entity := range entities {
			items[i] = hierarchyLevelFromEntity(entity)
		}
		if err := localizeCatalog(r, deps.ObjectTranslations, "hierarchy_level", &items); err != nil {
			return nil, err
		}
		return items, nil
	})

	b.Read("/hierarchy-levels/{hierarchy_level_id}", AuthAuthenticated, read, func(r *http.Request) (hierarchyLevelDTO, error) {
		id, err := pathID(r, "hierarchy_level_id")
		if err != nil {
			return hierarchyLevelDTO{}, err
		}
		entity, err := deps.HierarchyLevels.GetByID(id)
		if err != nil {
			return hierarchyLevelDTO{}, hierarchyLevelMutationError(err)
		}
		item := hierarchyLevelFromEntity(entity)
		if err := localizeCatalog(r, deps.ObjectTranslations, "hierarchy_level", &item); err != nil {
			return hierarchyLevelDTO{}, err
		}
		return item, nil
	})

	b.JSON(http.MethodPost, "/hierarchy-levels", http.StatusCreated, false, AuthAuthenticated, write, func(r *http.Request, input hierarchyLevelRequest) (hierarchyLevelDTO, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return hierarchyLevelDTO{}, err
		}
		entity, err := deps.HierarchyLevels.Create(&models.HierarchyLevel{Level: input.Level, Name: input.Name, Description: input.Description}, r)
		if err != nil {
			return hierarchyLevelDTO{}, hierarchyLevelMutationError(err)
		}
		return hierarchyLevelFromEntity(entity), nil
	})

	b.JSON(http.MethodPut, "/hierarchy-levels/{hierarchy_level_id}", http.StatusOK, false, AuthAuthenticated, write, func(r *http.Request, input hierarchyLevelRequest) (hierarchyLevelDTO, error) {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return hierarchyLevelDTO{}, err
		}
		id, err := pathID(r, "hierarchy_level_id")
		if err != nil {
			return hierarchyLevelDTO{}, err
		}
		entity, err := deps.HierarchyLevels.Update(id, &models.HierarchyLevel{Level: input.Level, Name: input.Name, Description: input.Description}, r)
		if err != nil {
			return hierarchyLevelDTO{}, hierarchyLevelMutationError(err)
		}
		return hierarchyLevelFromEntity(entity), nil
	})

	b.Command(http.MethodDelete, "/hierarchy-levels/{hierarchy_level_id}", AuthAuthenticated, write, func(r *http.Request) error {
		if _, err := requireSystemAdmin(r, deps); err != nil {
			return err
		}
		id, err := pathID(r, "hierarchy_level_id")
		if err != nil {
			return err
		}
		return hierarchyLevelMutationError(deps.HierarchyLevels.Delete(id, r))
	})
}
