package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	"reminder/backend/internal/model"
)

const googleCalendarScope = "https://www.googleapis.com/auth/calendar"

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type GoogleProvider struct {
	config          *oauth2.Config
	calendarBaseURL string
	httpClient      *http.Client
}

func NewGoogleProvider(clientID, clientSecret, redirectURL string) *GoogleProvider {
	return NewGoogleProviderWithConfig(&oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{googleCalendarScope},
		Endpoint:     googleoauth.Endpoint,
	}, "https://www.googleapis.com/calendar/v3")
}

func NewGoogleProviderWithConfig(config *oauth2.Config, calendarBaseURL string) *GoogleProvider {
	baseURL := strings.TrimRight(calendarBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://www.googleapis.com/calendar/v3"
	}

	return &GoogleProvider{
		config:          config,
		calendarBaseURL: baseURL,
		httpClient:      http.DefaultClient,
	}
}

func (p *GoogleProvider) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.httpClient = client
	}
}

func (p *GoogleProvider) AuthCodeURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func (p *GoogleProvider) ExchangeCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *GoogleProvider) ListEvents(ctx context.Context, account model.Account, from, to time.Time) ([]model.Event, error) {
	values := url.Values{}
	values.Set("singleEvents", "true")
	values.Set("orderBy", "startTime")
	values.Set("timeMin", from.Format(time.RFC3339))
	values.Set("timeMax", to.Format(time.RFC3339))

	endpoint := fmt.Sprintf("%s/calendars/primary/events?%s", p.calendarBaseURL, values.Encode())
	resp, err := p.doAuthorizedJSON(ctx, account, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google list events failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Items []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
			Start   struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"start"`
			End struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"end"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode google list events response: %w", err)
	}

	events := make([]model.Event, 0, len(payload.Items))
	for _, item := range payload.Items {
		startAt, err := parseGoogleTime(item.Start.DateTime, item.Start.Date)
		if err != nil {
			return nil, err
		}
		endAt, err := parseGoogleTime(item.End.DateTime, item.End.Date)
		if err != nil {
			return nil, err
		}

		events = append(events, model.Event{
			ID:           item.ID,
			Source:       model.ProviderGoogle,
			SourceLabel:  "Google Calendar",
			SourceDetail: "Primary calendar",
			Title:        item.Summary,
			StartAt:      startAt,
			EndAt:        endAt,
		})
	}

	return events, nil
}

func (p *GoogleProvider) CreateEvent(ctx context.Context, account model.Account, input model.CreateEventInput) (*model.Event, error) {
	body, err := json.Marshal(map[string]any{
		"summary": input.Title,
		"start": map[string]string{
			"dateTime": input.StartAt.Format(time.RFC3339),
		},
		"end": map[string]string{
			"dateTime": input.EndAt.Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal google create payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/calendars/primary/events", p.calendarBaseURL)
	resp, err := p.doAuthorizedJSON(ctx, account, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google create event failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var item struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
		Start   struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"end"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("decode google create response: %w", err)
	}

	startAt, err := parseGoogleTime(item.Start.DateTime, item.Start.Date)
	if err != nil {
		return nil, err
	}
	endAt, err := parseGoogleTime(item.End.DateTime, item.End.Date)
	if err != nil {
		return nil, err
	}

	return &model.Event{
		ID:           item.ID,
		Source:       model.ProviderGoogle,
		SourceLabel:  "Google Calendar",
		SourceDetail: "Primary calendar",
		Title:        item.Summary,
		StartAt:      startAt,
		EndAt:        endAt,
	}, nil
}

func (p *GoogleProvider) DeleteEvent(ctx context.Context, account model.Account, eventID string) error {
	endpoint := fmt.Sprintf("%s/calendars/primary/events/%s", p.calendarBaseURL, url.PathEscape(eventID))
	resp, err := p.doAuthorizedJSON(ctx, account, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("google delete event failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	return nil
}

func (p *GoogleProvider) doAuthorizedJSON(ctx context.Context, account model.Account, method, endpoint string, body io.Reader) (*http.Response, error) {
	token := &oauth2.Token{
		AccessToken:  account.AccessToken,
		RefreshToken: account.RefreshToken,
	}
	var extra struct {
		Expiry string `json:"expiry"`
	}
	if len(account.Extra) > 0 {
		if err := json.Unmarshal(account.Extra, &extra); err == nil && extra.Expiry != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, extra.Expiry); parseErr == nil {
				token.Expiry = parsed
			}
		}
	}
	if account.AccessToken == "" && account.RefreshToken == "" {
		return nil, fmt.Errorf("google account is missing tokens")
	}

	client := p.config.Client(ctx, token)
	if p.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
		client = p.config.Client(ctx, token)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build google request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google request failed: %w", err)
	}
	return resp, nil
}

func parseGoogleTime(dateTime, date string) (time.Time, error) {
	switch {
	case dateTime != "":
		parsed, err := time.Parse(time.RFC3339, dateTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse google datetime %q: %w", dateTime, err)
		}
		return parsed, nil
	case date != "":
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse google date %q: %w", date, err)
		}
		return parsed, nil
	default:
		return time.Time{}, fmt.Errorf("google event missing time")
	}
}
