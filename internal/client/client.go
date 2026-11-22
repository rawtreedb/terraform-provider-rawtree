package client

import (
	"net/http"
	"time"
)

// RawtreeClient is the API client shared between provider and resources.
type RawtreeClient struct {
	APIKey       string
	APIURL       string
	Organization string
	Database     string
	HTTPClient   *http.Client
}

// New creates a new RawtreeClient.
func New(apiKey, apiURL, org, database string) *RawtreeClient {
	return &RawtreeClient{
		APIKey:       apiKey,
		APIURL:       apiURL,
		Organization: org,
		Database:     database,
		HTTPClient: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}
