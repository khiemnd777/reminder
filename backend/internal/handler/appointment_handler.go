package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"reminder/backend/internal/model"
	"reminder/backend/internal/service"
)

const defaultAppointmentDuration = time.Hour

type AppointmentHandler struct {
	service *service.AppointmentService
}

func NewAppointmentHandler(service *service.AppointmentService) *AppointmentHandler {
	return &AppointmentHandler{service: service}
}

func (h *AppointmentHandler) Register(app *fiber.App) {
	app.Get("/appointments", h.listAppointments)
	app.Post("/appointments", h.createAppointment)
	app.Post("/appointments/sync", h.syncAppointments)
	app.Delete("/appointments/:id", h.deleteAppointment)
}

func (h *AppointmentHandler) listAppointments(c *fiber.Ctx) error {
	from, to, err := parseWindow(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	events, err := h.service.ListAppointments(c.UserContext(), from, to)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}

	return c.JSON(fiber.Map{"events": events})
}

func (h *AppointmentHandler) createAppointment(c *fiber.Ctx) error {
	var payload struct {
		Title   string     `json:"title"`
		StartAt time.Time  `json:"startAt"`
		EndAt   *time.Time `json:"endAt"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if payload.Title == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title is required")
	}
	if payload.StartAt.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "startAt is required")
	}

	endAt := payload.StartAt.Add(defaultAppointmentDuration)
	if payload.EndAt != nil {
		endAt = *payload.EndAt
	}

	if !endAt.After(payload.StartAt) {
		return fiber.NewError(fiber.StatusBadRequest, "endAt must be after startAt")
	}

	events, err := h.service.CreateAppointments(c.UserContext(), model.CreateEventInput{
		Title:   payload.Title,
		StartAt: payload.StartAt,
		EndAt:   endAt,
	}, true)
	if err != nil {
		status := fiber.StatusBadGateway
		if err == service.ErrNoProvidersSelected {
			status = fiber.StatusBadRequest
		}
		return fiber.NewError(status, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"events": events})
}

func (h *AppointmentHandler) deleteAppointment(c *fiber.Ctx) error {
	eventID := c.Params("id")
	if eventID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "appointment id is required")
	}

	source := c.Query("source")
	if err := h.service.DeleteAppointment(c.UserContext(), source, eventID); err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AppointmentHandler) syncAppointments(c *fiber.Ctx) error {
	from, to, err := parseWindow(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	source := c.Query("source")
	result, err := h.service.SyncAppointmentsFromProvider(c.UserContext(), source, from, to)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}

	return c.JSON(result)
}

func parseWindow(c *fiber.Ctx) (time.Time, time.Time, error) {
	fromRaw := c.Query("from")
	toRaw := c.Query("to")

	if fromRaw == "" {
		fromRaw = time.Now().UTC().Format(time.RFC3339)
	}
	if toRaw == "" {
		toRaw = time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	}

	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fiber.NewError(fiber.StatusBadRequest, "from must be RFC3339")
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fiber.NewError(fiber.StatusBadRequest, "to must be RFC3339")
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fiber.NewError(fiber.StatusBadRequest, "to must be after from")
	}

	return from, to, nil
}
