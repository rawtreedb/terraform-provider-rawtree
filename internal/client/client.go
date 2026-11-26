package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RawtreeClient is the API client shared between provider and resources.
//
// The API key (rt_...) is bound to one organization, cluster and default
// database. It can manage databases, tables, API keys and workflows, and it is
// the credential that AWS ingestion pipelines send data with.
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

// APIError is a non-2xx response from the RawTree API.
type APIError struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
	Body    string `json:"-"`
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Body
	}
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return fmt.Sprintf("rawtree API returned %d: %s", e.Status, msg)
}

// IsNotFound reports whether err is a 404 from the RawTree API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// IsAPIError reports whether err is an API response with this status and message.
// Some endpoints return the same status for different missing objects.
func IsAPIError(err error, status int, message string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == status && apiErr.Message == message
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

// Data calls a data-plane endpoint (databases, tables, API keys, workflows)
// with the API key.
func (c *RawtreeClient) Data(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.do(ctx, method, path, query, body, out)
}

func (c *RawtreeClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.APIURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-rawtree")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
		_ = json.Unmarshal(data, apiErr)
		return fmt.Errorf("%s %s: %w", method, path, apiErr)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}
