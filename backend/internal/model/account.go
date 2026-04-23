package model

import "encoding/json"

const (
	ProviderSystem = "system"
	ProviderGoogle = "google"
	ProviderApple  = "apple"
)

type Account struct {
	ID           string          `json:"id"`
	Provider     string          `json:"provider"`
	AccessToken  string          `json:"-"`
	RefreshToken string          `json:"-"`
	Extra        json.RawMessage `json:"extra,omitempty"`
}

type AppleCredentials struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}
