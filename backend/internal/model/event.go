package model

import "time"

type Event struct {
	ID             string    `json:"id"`
	Source         string    `json:"source"`
	SourceLabel    string    `json:"sourceLabel"`
	SourceDetail   string    `json:"sourceDetail"`
	ExternalID     string    `json:"externalId,omitempty"`
	ExternalSource string    `json:"externalSource,omitempty"`
	Title          string    `json:"title"`
	StartAt        time.Time `json:"startAt"`
	EndAt          time.Time `json:"endAt"`
}

type CreateEventInput struct {
	Title   string    `json:"title"`
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

type SyncResult struct {
	Source  string  `json:"source"`
	Created int     `json:"created"`
	Updated int     `json:"updated"`
	Events  []Event `json:"events"`
}
