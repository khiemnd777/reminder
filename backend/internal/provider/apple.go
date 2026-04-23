package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"reminder/backend/internal/model"
)

type AppleProvider struct {
	httpClient *http.Client
}

func NewAppleProvider() *AppleProvider {
	return &AppleProvider{httpClient: http.DefaultClient}
}

func (p *AppleProvider) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.httpClient = client
	}
}

func (p *AppleProvider) ListEvents(ctx context.Context, account model.Account, from, to time.Time) ([]model.Event, error) {
	creds, err := parseAppleCredentials(account)
	if err != nil {
		return nil, err
	}

	reportBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:getetag />
    <c:calendar-data />
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="%s" end="%s" />
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`, toCalDAVTime(from), toCalDAVTime(to))

	req, err := http.NewRequestWithContext(ctx, "REPORT", creds.BaseURL, strings.NewReader(reportBody))
	if err != nil {
		return nil, fmt.Errorf("build apple list request: %w", err)
	}
	req.SetBasicAuth(creds.Username, creds.Password)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple list request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("apple list events failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read apple list response: %w", err)
	}

	var payload struct {
		Responses []struct {
			PropStat struct {
				Prop struct {
					CalendarData string `xml:"calendar-data"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode apple list response: %w", err)
	}

	var events []model.Event
	for _, response := range payload.Responses {
		if strings.TrimSpace(response.PropStat.Prop.CalendarData) == "" {
			continue
		}
		parsed, err := parseICS(response.PropStat.Prop.CalendarData)
		if err != nil {
			return nil, err
		}
		events = append(events, parsed...)
	}

	return events, nil
}

func (p *AppleProvider) CreateEvent(ctx context.Context, account model.Account, input model.CreateEventInput) (*model.Event, error) {
	creds, err := parseAppleCredentials(account)
	if err != nil {
		return nil, err
	}

	eventID := fmt.Sprintf("reminder-%d", time.Now().UnixNano())
	payload := buildICS(eventID, input)

	targetURL := strings.TrimRight(creds.BaseURL, "/") + "/" + eventID + ".ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL, bytes.NewBufferString(payload))
	if err != nil {
		return nil, fmt.Errorf("build apple create request: %w", err)
	}
	req.SetBasicAuth(creds.Username, creds.Password)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple create request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("apple create event failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return &model.Event{
		ID:           eventID,
		Source:       model.ProviderApple,
		SourceLabel:  "Apple Calendar",
		SourceDetail: "Configured CalDAV calendar",
		Title:        input.Title,
		StartAt:      input.StartAt,
		EndAt:        input.EndAt,
	}, nil
}

func parseAppleCredentials(account model.Account) (model.AppleCredentials, error) {
	var creds model.AppleCredentials
	if err := json.Unmarshal(account.Extra, &creds); err != nil {
		return creds, fmt.Errorf("decode apple credentials: %w", err)
	}
	if creds.BaseURL == "" || creds.Username == "" || creds.Password == "" {
		return creds, fmt.Errorf("apple credentials are incomplete")
	}
	return creds, nil
}

func parseICS(payload string) ([]model.Event, error) {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	var (
		events   []model.Event
		current  model.Event
		inEvent  bool
		haveID   bool
		haveName bool
		haveFrom bool
		haveTo   bool
	)

	flush := func() error {
		if !inEvent {
			return nil
		}
		if !haveID || !haveName || !haveFrom || !haveTo {
			return fmt.Errorf("apple calendar-data missing VEVENT fields")
		}
		current.Source = model.ProviderApple
		current.SourceLabel = "Apple Calendar"
		current.SourceDetail = "Configured CalDAV calendar"
		events = append(events, current)
		current = model.Event{}
		inEvent, haveID, haveName, haveFrom, haveTo = false, false, false, false, false
		return nil
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "BEGIN:VEVENT":
			inEvent = true
		case line == "END:VEVENT":
			if err := flush(); err != nil {
				return nil, err
			}
		case strings.HasPrefix(line, "UID:") && inEvent:
			current.ID = strings.TrimPrefix(line, "UID:")
			haveID = true
		case strings.HasPrefix(line, "SUMMARY:") && inEvent:
			current.Title = strings.TrimPrefix(line, "SUMMARY:")
			haveName = true
		case strings.HasPrefix(line, "DTSTART") && inEvent:
			value := line[strings.Index(line, ":")+1:]
			startAt, err := parseICSTime(value)
			if err != nil {
				return nil, err
			}
			current.StartAt = startAt
			haveFrom = true
		case strings.HasPrefix(line, "DTEND") && inEvent:
			value := line[strings.Index(line, ":")+1:]
			endAt, err := parseICSTime(value)
			if err != nil {
				return nil, err
			}
			current.EndAt = endAt
			haveTo = true
		}
	}

	return events, nil
}

func parseICSTime(value string) (time.Time, error) {
	if strings.Contains(value, "T") {
		if strings.HasSuffix(value, "Z") {
			return time.Parse("20060102T150405Z", value)
		}
		return time.Parse("20060102T150405", value)
	}
	return time.Parse("20060102", value)
}

func toCalDAVTime(value time.Time) string {
	return value.UTC().Format("20060102T150405Z")
}

func buildICS(id string, input model.CreateEventInput) string {
	return strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Reminder//Calendar Aggregator//EN",
		"BEGIN:VEVENT",
		"UID:" + id,
		"SUMMARY:" + escapeICS(input.Title),
		"DTSTART:" + toCalDAVTime(input.StartAt),
		"DTEND:" + toCalDAVTime(input.EndAt),
		"END:VEVENT",
		"END:VCALENDAR",
		"",
	}, "\r\n")
}

func escapeICS(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n")
	return replacer.Replace(value)
}
