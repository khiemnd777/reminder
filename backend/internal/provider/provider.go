package provider

import (
	"context"
	"time"

	"golang.org/x/oauth2"

	"reminder/backend/internal/model"
)

type CalendarProvider interface {
	ListEvents(ctx context.Context, account model.Account, from, to time.Time) ([]model.Event, error)
	CreateEvent(ctx context.Context, account model.Account, input model.CreateEventInput) (*model.Event, error)
	DeleteEvent(ctx context.Context, account model.Account, eventID string) error
}

type GoogleOAuthClient interface {
	AuthCodeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*oauth2.Token, error)
}
