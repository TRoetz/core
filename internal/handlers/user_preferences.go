package handlers

import "windshift/internal/services"

type UserPreferencesHandler struct {
	service *services.UserPreferencesService
}

func NewUserPreferencesHandler(service *services.UserPreferencesService) *UserPreferencesHandler {
	return &UserPreferencesHandler{service: service}
}
