package handler

import (
	"github.com/gofiber/fiber/v2"

	"reminder/backend/internal/model"
	"reminder/backend/internal/repository"
)

type ConnectionHandler struct {
	accounts *repository.AccountRepository
}

func NewConnectionHandler(accounts *repository.AccountRepository) *ConnectionHandler {
	return &ConnectionHandler{accounts: accounts}
}

func (h *ConnectionHandler) Register(app *fiber.App) {
	app.Get("/connections", h.listConnections)
}

func (h *ConnectionHandler) listConnections(c *fiber.Ctx) error {
	accounts, err := h.accounts.List(c.UserContext())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	response := fiber.Map{
		"google": fiber.Map{
			"provider":  model.ProviderGoogle,
			"label":     "Google Calendar",
			"detail":    "Primary calendar",
			"connected": false,
		},
	}

	for _, account := range accounts {
		switch account.Provider {
		case model.ProviderGoogle:
			response["google"] = fiber.Map{
				"provider":  model.ProviderGoogle,
				"label":     "Google Calendar",
				"detail":    "Primary calendar",
				"connected": account.AccessToken != "" || account.RefreshToken != "",
			}
		}
	}

	return c.JSON(response)
}
