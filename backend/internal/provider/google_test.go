package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"reminder/backend/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestGoogleProviderRefreshesTokenAndMapsListResponse(t *testing.T) {
	t.Parallel()

	refreshed := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			refreshed = true
			return jsonResponse(http.StatusOK, `{"access_token":"fresh-token","token_type":"Bearer","refresh_token":"refresh-1","expires_in":3600}`), nil
		case strings.Contains(r.URL.Path, "/calendar/v3/calendars/primary/events"):
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Fatalf("expected refreshed bearer token, got %q", got)
			}
			return jsonResponse(http.StatusOK, `{"items":[{"id":"g1","summary":"Planning","start":{"dateTime":"2026-04-22T09:00:00Z"},"end":{"dateTime":"2026-04-22T10:00:00Z"}}]}`), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	config := &oauth2.Config{
		ClientID:     "client",
		ClientSecret: "secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.test/auth",
			TokenURL: "https://oauth.google.test/token",
		},
	}
	provider := NewGoogleProviderWithConfig(config, "https://calendar.google.test/calendar/v3")
	provider.SetHTTPClient(client)

	extra, _ := json.Marshal(map[string]string{
		"expiry": time.Now().Add(-time.Hour).Format(time.RFC3339),
	})
	events, err := provider.ListEvents(context.Background(), model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "expired-token",
		RefreshToken: "refresh-1",
		Extra:        extra,
	}, time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !refreshed {
		t.Fatal("expected token refresh")
	}
	if len(events) != 1 || events[0].Title != "Planning" || events[0].Source != model.ProviderGoogle {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestGoogleProviderCreateEventMapsResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		return jsonResponse(http.StatusOK, `{"id":"g1","summary":"Launch","start":{"dateTime":"2026-04-22T09:00:00Z"},"end":{"dateTime":"2026-04-22T10:00:00Z"}}`), nil
	})}

	config := &oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.test/auth", TokenURL: "https://oauth.google.test/token"}}
	provider := NewGoogleProviderWithConfig(config, "https://calendar.google.test")
	provider.SetHTTPClient(client)

	event, err := provider.CreateEvent(context.Background(), model.Account{
		ID:          "google",
		Provider:    model.ProviderGoogle,
		AccessToken: "access-1",
	}, model.CreateEventInput{
		Title:   "Launch",
		StartAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if event.ID != "g1" || event.Title != "Launch" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestGoogleProviderDeleteEventCallsDeleteEndpoint(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/calendars/primary/events/g1") {
			t.Fatalf("unexpected delete path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}

	config := &oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.test/auth", TokenURL: "https://oauth.google.test/token"}}
	provider := NewGoogleProviderWithConfig(config, "https://calendar.google.test")
	provider.SetHTTPClient(client)

	if err := provider.DeleteEvent(context.Background(), model.Account{
		ID:          "google",
		Provider:    model.ProviderGoogle,
		AccessToken: "access-1",
	}, "g1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
