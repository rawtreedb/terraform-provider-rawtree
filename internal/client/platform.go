package client

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
)

// Clients keeps the platform credential separate from credentials deployed to AWS.
type Clients struct {
	Platform     *PlatformClient
	Integrations *RawtreeClient
}

type PlatformClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewPlatformClient(baseURL, token string) *PlatformClient {
	return &PlatformClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type platformError struct {
	method  string
	path    string
	status  int
	message string
	hint    string
}

func (e *platformError) Error() string {
	return strings.TrimSpace(fmt.Sprintf("request to Tinybird RawTree %s %s returned HTTP %d: %s %s", e.method, e.path, e.status, e.message, e.hint))
}

func (c *PlatformClient) platformRequest(ctx context.Context, method, endpoint string, scope url.Values, body []byte, status int, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"?"+scope.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "terraform-provider-rawtree")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != status {
		var problem struct{ Message, Hint string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&problem)
		return &platformError{method: method, path: req.URL.Path, status: resp.StatusCode, message: problem.Message, hint: problem.Hint}
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}
