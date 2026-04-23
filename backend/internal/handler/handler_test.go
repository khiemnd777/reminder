package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"

	"reminder/backend/internal/model"
	"reminder/backend/internal/provider"
	"reminder/backend/internal/repository"
	"reminder/backend/internal/service"
	"reminder/backend/pkg/db"
)

type fakeGoogleAuth struct {
	authURL string
	token   *oauth2.Token
	err     error
}

func (f *fakeGoogleAuth) AuthCodeURL(_ string) string { return f.authURL }
func (f *fakeGoogleAuth) ExchangeCode(_ context.Context, _ string) (*oauth2.Token, error) {
	return f.token, f.err
}

type fakeCalendarProvider struct {
	listEvents  []model.Event
	createEvent *model.Event
	createInput *model.CreateEventInput
	deleteID    string
}

func (p *fakeCalendarProvider) ListEvents(_ context.Context, _ model.Account, _, _ time.Time) ([]model.Event, error) {
	return append([]model.Event(nil), p.listEvents...), nil
}

func (p *fakeCalendarProvider) CreateEvent(_ context.Context, _ model.Account, input model.CreateEventInput) (*model.Event, error) {
	p.createInput = &input
	event := *p.createEvent
	return &event, nil
}

func (p *fakeCalendarProvider) DeleteEvent(_ context.Context, _ model.Account, eventID string) error {
	p.deleteID = eventID
	return nil
}

func newTestApp(t *testing.T) (*fiber.App, *repository.AccountRepository) {
	t.Helper()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "handler.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repo := repository.NewAccountRepository(database)
	appointmentRepo := repository.NewAppointmentRepository(database)
	appointmentService := service.NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: &fakeCalendarProvider{
			listEvents:  []model.Event{{ID: "g1", Source: model.ProviderGoogle, Title: "Sync", StartAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)}},
			createEvent: &model.Event{ID: "g-created", Source: model.ProviderGoogle, Title: "Created", StartAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)},
		},
	})

	app := fiber.New()
	RegisterHealthRoutes(app)
	NewAuthHandler(repo, &fakeGoogleAuth{
		authURL: "https://accounts.google.test/auth",
		token: &oauth2.Token{
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			Expiry:       time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		},
	}).Register(app)
	NewAppointmentHandler(appointmentService).Register(app)
	return app, repo
}

func TestGoogleCallbackStoresAccount(t *testing.T) {
	t.Parallel()

	app, repo := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}

	account, err := repo.GetByProvider(context.Background(), model.ProviderGoogle)
	if err != nil {
		t.Fatalf("load google account: %v", err)
	}
	if account == nil || account.AccessToken != "access-1" || account.RefreshToken != "refresh-1" {
		t.Fatalf("unexpected stored account: %+v", account)
	}
}

func TestGoogleCallbackRequiresCode(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAppointmentsEndpointReturnsUnifiedShape(t *testing.T) {
	t.Parallel()

	app, repo := newTestApp(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/appointments?from=2026-04-22T00:00:00Z&to=2026-04-23T00:00:00Z", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Events []model.Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(payload.Events))
	}
}

func TestCreateAppointmentRequiresConnectedGoogleAccount(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(`{"title":"Sync","startAt":"2026-04-22T09:00:00Z","endAt":"2026-04-22T10:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

func TestCreateAppointmentDefaultsEndAtWhenOmitted(t *testing.T) {
	t.Parallel()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "handler-default-end.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repo := repository.NewAccountRepository(database)
	if err := repo.Upsert(context.Background(), model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	googleProvider := &fakeCalendarProvider{
		createEvent: &model.Event{
			ID:      "g-created",
			Source:  model.ProviderGoogle,
			Title:   "Created",
			StartAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		},
	}
	appointmentRepo := repository.NewAppointmentRepository(database)
	appointmentService := service.NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	app := fiber.New()
	NewAppointmentHandler(appointmentService).Register(app)

	req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(`{"title":"Sync","startAt":"2026-04-22T09:00:00Z","syncGoogle":true}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if googleProvider.createInput == nil {
		t.Fatal("expected create input to be captured")
	}
	if got, want := googleProvider.createInput.EndAt, time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("expected default endAt %s, got %s", want, got)
	}
}

func TestConnectionsEndpointReturnsProviderStates(t *testing.T) {
	t.Parallel()

	app, repo := newTestApp(t)
	if err := repo.Upsert(context.Background(), model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}
	NewConnectionHandler(repo).Register(app)

	req := httptest.NewRequest(http.MethodGet, "/connections", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Google struct {
			Connected bool `json:"connected"`
		} `json:"google"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Google.Connected {
		t.Fatalf("unexpected connection payload: %+v", payload)
	}
}

func TestDeleteAppointmentEndpointReturnsNoContent(t *testing.T) {
	t.Parallel()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "handler-delete.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repo := repository.NewAccountRepository(database)
	if err := repo.Upsert(context.Background(), model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	googleProvider := &fakeCalendarProvider{}
	appointmentRepo := repository.NewAppointmentRepository(database)
	appointmentService := service.NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	app := fiber.New()
	NewAppointmentHandler(appointmentService).Register(app)

	req := httptest.NewRequest(http.MethodDelete, "/appointments/event-123?source=google", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if googleProvider.deleteID != "event-123" {
		t.Fatalf("unexpected deleted id: %s", googleProvider.deleteID)
	}
}

func TestSyncAppointmentsEndpointImportsGoogleIntoSystem(t *testing.T) {
	t.Parallel()

	app, repo := newTestApp(t)
	if err := repo.Upsert(context.Background(), model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/appointments/sync?source=google&from=2026-04-22T00:00:00Z&to=2026-04-23T00:00:00Z", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Source  string        `json:"source"`
		Created int           `json:"created"`
		Updated int           `json:"updated"`
		Events  []model.Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Source != model.ProviderGoogle || payload.Created != 1 || payload.Updated != 0 || len(payload.Events) != 1 {
		t.Fatalf("unexpected sync payload: %+v", payload)
	}
	if payload.Events[0].Source != model.ProviderSystem {
		t.Fatalf("expected synced event in system, got %+v", payload.Events[0])
	}
}
