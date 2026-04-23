package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reminder/backend/internal/model"
	"reminder/backend/internal/provider"
	"reminder/backend/internal/repository"
	"reminder/backend/pkg/db"
)

type fakeProvider struct {
	listEvents   []model.Event
	listErr      error
	createEvent  *model.Event
	createErr    error
	createCalled int
	deleteErr    error
	deletedID    string
}

func (p *fakeProvider) ListEvents(_ context.Context, _ model.Account, _, _ time.Time) ([]model.Event, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return append([]model.Event(nil), p.listEvents...), nil
}

func (p *fakeProvider) CreateEvent(_ context.Context, _ model.Account, _ model.CreateEventInput) (*model.Event, error) {
	p.createCalled++
	if p.createErr != nil {
		return nil, p.createErr
	}
	event := *p.createEvent
	return &event, nil
}

func (p *fakeProvider) DeleteEvent(_ context.Context, _ model.Account, eventID string) error {
	p.deletedID = eventID
	return p.deleteErr
}

func newServiceRepos(t *testing.T) (*repository.AccountRepository, *repository.AppointmentRepository) {
	t.Helper()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return repository.NewAccountRepository(database), repository.NewAppointmentRepository(database)
}

func TestListAppointmentsWithNoAccounts(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{})

	events, err := service.ListAppointments(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list appointments: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected empty events, got %d", len(events))
	}
}

func TestListAppointmentsMergesAndSorts(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	start := time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC)
	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: &fakeProvider{listEvents: []model.Event{
			{ID: "g2", Source: model.ProviderGoogle, Title: "Later", StartAt: start.Add(2 * time.Hour), EndAt: start.Add(3 * time.Hour)},
			{ID: "g1", Source: model.ProviderGoogle, Title: "Sooner", StartAt: start, EndAt: start.Add(time.Hour)},
		}},
	})

	events, err := service.ListAppointments(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("list appointments: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "g1" || events[1].ID != "g2" {
		t.Fatalf("events not sorted as expected: %+v", events)
	}
}

func TestCreateAppointmentsUsesSelectedProvidersOnly(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	googleProvider := &fakeProvider{createEvent: &model.Event{ID: "g1", Source: model.ProviderGoogle}}

	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	events, err := service.CreateAppointments(ctx, model.CreateEventInput{
		Title:   "Standup",
		StartAt: time.Now(),
		EndAt:   time.Now().Add(time.Hour),
	}, true)
	if err != nil {
		t.Fatalf("create appointments: %v", err)
	}
	if len(events) != 1 || events[0].Source != model.ProviderGoogle {
		t.Fatalf("unexpected create result: %+v", events)
	}
	if googleProvider.createCalled != 1 {
		t.Fatalf("unexpected google create calls=%d", googleProvider.createCalled)
	}
}

func TestCreateAppointmentsFailsClosedOnProviderError(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: &fakeProvider{createErr: errors.New("provider down")},
	})

	_, err := service.CreateAppointments(ctx, model.CreateEventInput{
		Title:   "Standup",
		StartAt: time.Now(),
		EndAt:   time.Now().Add(time.Hour),
	}, true)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteAppointmentUsesProviderDelete(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	googleProvider := &fakeProvider{}
	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	if err := service.DeleteAppointment(ctx, model.ProviderGoogle, "event-123"); err != nil {
		t.Fatalf("delete appointment: %v", err)
	}
	if googleProvider.deletedID != "event-123" {
		t.Fatalf("unexpected deleted id: %s", googleProvider.deletedID)
	}
}

func TestSyncAppointmentsImportsIntoSystemAndHidesDuplicates(t *testing.T) {
	t.Parallel()

	repo, appointmentRepo := newServiceRepos(t)
	ctx := context.Background()
	if err := repo.Upsert(ctx, model.Account{ID: "google", Provider: model.ProviderGoogle}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	start := time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC)
	googleProvider := &fakeProvider{listEvents: []model.Event{
		{ID: "g1", Source: model.ProviderGoogle, SourceLabel: "Google Calendar", Title: "Imported", StartAt: start, EndAt: start.Add(time.Hour)},
	}}
	service := NewAppointmentService(repo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	result, err := service.SyncAppointmentsFromProvider(ctx, model.ProviderGoogle, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("sync appointments: %v", err)
	}
	if result.Created != 1 || result.Updated != 0 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	events, err := service.ListAppointments(ctx, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("list appointments: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(events))
	}
	if events[0].Source != model.ProviderSystem {
		t.Fatalf("expected system event after sync, got %+v", events[0])
	}
}
