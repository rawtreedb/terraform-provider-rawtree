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
	Project      string
	HTTPClient   *http.Client
}

// New creates a new RawtreeClient.
func New(apiKey, apiURL, org, project string) *RawtreeClient {
	return &RawtreeClient{
		APIKey:       apiKey,
		APIURL:       apiURL,
		Organization: org,
		Project:      project,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}
