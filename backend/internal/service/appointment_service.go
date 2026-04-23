package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"reminder/backend/internal/model"
	"reminder/backend/internal/provider"
	"reminder/backend/internal/repository"
)

var ErrNoProvidersSelected = errors.New("at least one provider must be selected")

type AppointmentService struct {
	accounts     *repository.AccountRepository
	appointments *repository.AppointmentRepository
	providers    map[string]provider.CalendarProvider
}

func NewAppointmentService(accounts *repository.AccountRepository, appointments *repository.AppointmentRepository, providers map[string]provider.CalendarProvider) *AppointmentService {
	return &AppointmentService{
		accounts:     accounts,
		appointments: appointments,
		providers:    providers,
	}
}

func (s *AppointmentService) ListAppointments(ctx context.Context, from, to time.Time) ([]model.Event, error) {
	events, err := s.appointments.List(ctx, from, to)
	if err != nil {
		return nil, err
	}
	syncedRefs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if event.ExternalSource == "" || event.ExternalID == "" {
			continue
		}
		syncedRefs[event.ExternalSource+":"+event.ExternalID] = struct{}{}
	}

	accounts, err := s.accounts.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, account := range accounts {
		calendarProvider, ok := s.providers[account.Provider]
		if !ok {
			continue
		}

		providerEvents, err := calendarProvider.ListEvents(ctx, account, from, to)
		if err != nil {
			return nil, fmt.Errorf("list events from %s: %w", account.Provider, err)
		}
		for _, event := range providerEvents {
			ref := account.Provider + ":" + event.ID
			if _, exists := syncedRefs[ref]; exists {
				continue
			}
			events = append(events, event)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].StartAt.Equal(events[j].StartAt) {
			if events[i].EndAt.Equal(events[j].EndAt) {
				return events[i].ID < events[j].ID
			}
			return events[i].EndAt.Before(events[j].EndAt)
		}
		return events[i].StartAt.Before(events[j].StartAt)
	})

	return events, nil
}

func (s *AppointmentService) CreateAppointments(ctx context.Context, input model.CreateEventInput, syncGoogle bool) ([]model.Event, error) {
	targetProviders := make([]string, 0, 2)
	if syncGoogle {
		targetProviders = append(targetProviders, model.ProviderGoogle)
	}
	if len(targetProviders) == 0 {
		return nil, ErrNoProvidersSelected
	}

	created := make([]model.Event, 0, len(targetProviders))
	for _, providerName := range targetProviders {
		account, err := s.accounts.GetByProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return nil, fmt.Errorf("%s account is not connected", providerName)
		}

		calendarProvider, ok := s.providers[providerName]
		if !ok {
			return nil, fmt.Errorf("provider %s is not configured", providerName)
		}

		event, err := calendarProvider.CreateEvent(ctx, *account, input)
		if err != nil {
			return nil, fmt.Errorf("create event in %s: %w", providerName, err)
		}
		created = append(created, *event)
	}

	sort.Slice(created, func(i, j int) bool {
		return created[i].Source < created[j].Source
	})

	return created, nil
}

func (s *AppointmentService) SyncAppointmentsFromProvider(ctx context.Context, source string, from, to time.Time) (*model.SyncResult, error) {
	if source == "" {
		source = model.ProviderGoogle
	}

	account, err := s.accounts.GetByProvider(ctx, source)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("%s account is not connected", source)
	}

	calendarProvider, ok := s.providers[source]
	if !ok {
		return nil, fmt.Errorf("provider %s is not configured", source)
	}

	providerEvents, err := calendarProvider.ListEvents(ctx, *account, from, to)
	if err != nil {
		return nil, fmt.Errorf("list events from %s: %w", source, err)
	}

	result := &model.SyncResult{
		Source: source,
		Events: make([]model.Event, 0, len(providerEvents)),
	}

	for _, providerEvent := range providerEvents {
		providerEvent.ExternalSource = source
		providerEvent.ExternalID = providerEvent.ID

		savedEvent, created, err := s.appointments.UpsertImported(ctx, source, providerEvent)
		if err != nil {
			return nil, err
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
		result.Events = append(result.Events, savedEvent)
	}

	return result, nil
}

func (s *AppointmentService) DeleteAppointment(ctx context.Context, source, eventID string) error {
	if source == "" {
		source = model.ProviderGoogle
	}
	if source == model.ProviderSystem {
		return s.appointments.Delete(ctx, eventID)
	}

	account, err := s.accounts.GetByProvider(ctx, source)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("%s account is not connected", source)
	}

	calendarProvider, ok := s.providers[source]
	if !ok {
		return fmt.Errorf("provider %s is not configured", source)
	}

	if err := calendarProvider.DeleteEvent(ctx, *account, eventID); err != nil {
		return fmt.Errorf("delete event in %s: %w", source, err)
	}

	return nil
}
