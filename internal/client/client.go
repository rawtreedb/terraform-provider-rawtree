package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RawtreeClient is the API client shared between provider and resources.
//
// The API key (rt_...) is bound to one organization, cluster and default
// database. It is the credential that AWS ingestion pipelines send data with.
type RawtreeClient struct {
	APIKey       string
	APIURL       string
	Organization string
	Database     string
	// Project is only used by rawtree_supabase_cdc_ingestion, which passes it
	// to the supabase-etl worker image unchanged.
	Project    string
	HTTPClient *http.Client
}

// New creates a new RawtreeClient.
func New(apiKey, apiURL, org, database, project string) *RawtreeClient {
	return &RawtreeClient{
		APIKey:       apiKey,
		APIURL:       strings.TrimRight(apiURL, "/"),
		Organization: org,
		Database:     database,
		Project:      project,
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

// DatabaseOr returns database, else the provider default. An empty result
// means the API key's own database.
func (c *RawtreeClient) DatabaseOr(database string) string {
	if database != "" {
		return database
	}
	return c.Database
}

// TableURL returns the ingest URL for a table, with the database and extra
// query parameters (for example transform=firehose) set.
func (c *RawtreeClient) TableURL(database, table string, extra url.Values) string {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	if database != "" {
		q.Set("database", database)
	}
	u := fmt.Sprintf("%s/v1/tables/%s", c.APIURL, url.PathEscape(table))
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}
