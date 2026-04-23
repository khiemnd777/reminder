package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reminder/backend/internal/model"
)

func TestAppleProviderListEventsMapsICSResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "REPORT" {
			t.Fatalf("expected REPORT, got %s", r.Method)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != "secret" {
			t.Fatalf("unexpected basic auth: %v %s %s", ok, user, pass)
		}
		return &http.Response{
			StatusCode: http.StatusMultiStatus,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="utf-8"?>
<multistatus xmlns="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <response>
    <propstat>
      <prop>
        <c:calendar-data>BEGIN:VCALENDAR
BEGIN:VEVENT
UID:apple-1
SUMMARY:Planning
DTSTART:20260422T090000Z
DTEND:20260422T100000Z
END:VEVENT
END:VCALENDAR</c:calendar-data>
      </prop>
    </propstat>
  </response>
</multistatus>`)),
		}, nil
	})}

	creds, _ := json.Marshal(model.AppleCredentials{BaseURL: "https://caldav.apple.test/calendar", Username: "user", Password: "secret"})
	provider := NewAppleProvider()
	provider.SetHTTPClient(client)

	events, err := provider.ListEvents(context.Background(), model.Account{
		ID:       "apple",
		Provider: model.ProviderApple,
		Extra:    creds,
	}, time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].ID != "apple-1" || events[0].Source != model.ProviderApple {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestAppleProviderCreateEventWritesICS(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := string(body)
		if !strings.Contains(payload, "SUMMARY:Planning") || !strings.Contains(payload, "BEGIN:VEVENT") {
			t.Fatalf("unexpected ics payload: %s", payload)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
		}, nil
	})}

	creds, _ := json.Marshal(model.AppleCredentials{BaseURL: "https://caldav.apple.test/calendar", Username: "user", Password: "secret"})
	provider := NewAppleProvider()
	provider.SetHTTPClient(client)

	event, err := provider.CreateEvent(context.Background(), model.Account{
		ID:       "apple",
		Provider: model.ProviderApple,
		Extra:    creds,
	}, model.CreateEventInput{
		Title:   "Planning",
		StartAt: time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if event.Source != model.ProviderApple || event.Title != "Planning" {
		t.Fatalf("unexpected event: %+v", event)
	}
}
