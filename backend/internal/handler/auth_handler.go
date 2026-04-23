package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"reminder/backend/internal/model"
	"reminder/backend/internal/provider"
	"reminder/backend/internal/repository"
)

type AuthHandler struct {
	accounts   *repository.AccountRepository
	googleAuth provider.GoogleOAuthClient
}

func NewAuthHandler(accounts *repository.AccountRepository, googleAuth provider.GoogleOAuthClient) *AuthHandler {
	return &AuthHandler{
		accounts:   accounts,
		googleAuth: googleAuth,
	}
}

func (h *AuthHandler) Register(app *fiber.App) {
	app.Get("/auth/google/login", h.googleLogin)
	app.Get("/auth/google/callback", h.googleCallback)
}

func (h *AuthHandler) googleLogin(c *fiber.Ctx) error {
	return c.Redirect(h.googleAuth.AuthCodeURL("calendar-sync"), fiber.StatusTemporaryRedirect)
}

func (h *AuthHandler) googleCallback(c *fiber.Ctx) error {
	receivedErr := c.Query("error")
	if receivedErr != "" {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("google oauth error: %s", receivedErr))
	}

	code := c.Query("code")
	if code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing oauth code")
	}

	token, err := h.googleAuth.ExchangeCode(context.Background(), code)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, fmt.Sprintf("exchange google oauth code: %v", err))
	}

	account := model.Account{
		ID:           model.ProviderGoogle,
		Provider:     model.ProviderGoogle,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}
	if !token.Expiry.IsZero() {
		extra, marshalErr := json.Marshal(map[string]string{
			"expiry": token.Expiry.Format(time.RFC3339),
		})
		if marshalErr != nil {
			return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("marshal google token metadata: %v", marshalErr))
		}
		account.Extra = extra
	}
	if err := h.accounts.Upsert(c.UserContext(), account); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.Redirect("/", fiber.StatusTemporaryRedirect)
}
