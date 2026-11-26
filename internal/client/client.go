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
	"regexp"
	"strings"
	"sync"
	"time"
)

// RawtreeClient is the API client shared between provider and resources.
//
// RawTree has two credential types:
//   - APIKey (rt_...): bound to one organization, cluster and default database.
//     It can manage databases, tables, API keys and workflows, and it is the
//     credential that AWS ingestion pipelines send data with.
//   - AccessToken (user OAuth or session JWT): required for clusters,
//     connectors, service accounts and federation. Cluster-scoped calls are
//     routed to the regional deployment that hosts the cluster.
type RawtreeClient struct {
	APIKey       string
	AccessToken  string
	APIURL       string
	Organization string
	Cluster      string
	Database     string
	// Project is only used by rawtree_supabase_cdc_ingestion, which passes it
	// to the supabase-etl worker image unchanged.
	Project    string
	HTTPClient *http.Client

	mu     sync.Mutex
	orgIDs map[string]string
	routes map[string]clusterRoute
}

// clusterRoute is the ID of a cluster and the deployment that serves it.
type clusterRoute struct {
	id  string
	dep *Deployment
}

// Deployment identifies the regional RawTree deployment that serves a cluster.
type Deployment struct {
	ID     string
	Region string
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
func New(apiKey, accessToken, apiURL, org, cluster, database, project string) *RawtreeClient {
	return &RawtreeClient{
		APIKey:       apiKey,
		AccessToken:  accessToken,
		APIURL:       strings.TrimRight(apiURL, "/"),
		Organization: org,
		Cluster:      cluster,
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

// Data calls a data-plane endpoint (databases, tables, API keys, workflows).
// It uses the API key when one is configured. Otherwise it uses the access
// token and routes the call to the provider's organization and cluster.
func (c *RawtreeClient) Data(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.APIKey != "" {
		return c.do(ctx, c.APIKey, c.APIURL, nil, method, path, query, body, out)
	}
	return c.InCluster(ctx, c.Cluster, method, path, query, body, out)
}

// Global calls an organization-level endpoint on the global API with the
// access token (organizations, regions, service accounts, federation issuers).
func (c *RawtreeClient) Global(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if err := c.requireAccessToken(); err != nil {
		return err
	}
	return c.do(ctx, c.AccessToken, c.APIURL, nil, method, path, query, body, out)
}

// InCluster calls a cluster-scoped endpoint with the access token. It selects
// the organization and cluster by ID and routes the call to the deployment
// that hosts the cluster. The cluster can be a name or an ID.
func (c *RawtreeClient) InCluster(ctx context.Context, cluster, method, path string, query url.Values, body, out any) error {
	if err := c.requireAccessToken(); err != nil {
		return err
	}
	if c.Organization == "" || cluster == "" {
		return errors.New("this operation needs an organization and a cluster: set the provider 'organization' and 'cluster' attributes (or RAWTREE_ORG and RAWTREE_CLUSTER)")
	}
	route, err := c.clusterRoute(ctx, cluster)
	if err != nil {
		return err
	}
	headers, err := c.scopeHeaders(ctx, route.dep)
	if err != nil {
		return err
	}
	headers["X-RawTree-Cluster-Id"] = route.id
	return c.do(ctx, c.AccessToken, c.deploymentURL(route.dep), headers, method, path, query, body, out)
}

// Deployed calls an organization-scoped endpoint on a regional deployment
// with the access token. A nil deployment is the default deployment.
func (c *RawtreeClient) Deployed(ctx context.Context, dep *Deployment, method, path string, query url.Values, body, out any) error {
	if err := c.requireAccessToken(); err != nil {
		return err
	}
	headers, err := c.scopeHeaders(ctx, dep)
	if err != nil {
		return err
	}
	return c.do(ctx, c.AccessToken, c.deploymentURL(dep), headers, method, path, query, body, out)
}

// scopeHeaders selects the provider organization and the deployment. The
// organization and cluster name query parameters are deprecated in the API.
func (c *RawtreeClient) scopeHeaders(ctx context.Context, dep *Deployment) (map[string]string, error) {
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"X-RawTree-Organization-Id": orgID}
	if dep != nil {
		headers["X-RawTree-Deployment-Id"] = dep.ID
		headers["X-RawTree-Region"] = dep.Region
	}
	return headers, nil
}

// OrganizationID resolves the provider organization name to its ID.
func (c *RawtreeClient) OrganizationID(ctx context.Context) (string, error) {
	if c.Organization == "" {
		return "", errors.New("this operation needs an organization: set the provider 'organization' attribute or RAWTREE_ORG")
	}
	c.mu.Lock()
	id, ok := c.orgIDs[c.Organization]
	c.mu.Unlock()
	if ok {
		return id, nil
	}
	var resp struct {
		Organizations []struct {
			ID   string `json:"organization_id"`
			Name string `json:"name"`
		} `json:"organizations"`
	}
	if err := c.Global(ctx, http.MethodGet, "/v1/organizations", nil, nil, &resp); err != nil {
		return "", err
	}
	for _, o := range resp.Organizations {
		// Accept the name or the ID, like the rtree CLI.
		if o.Name == c.Organization || o.ID == c.Organization {
			c.mu.Lock()
			if c.orgIDs == nil {
				c.orgIDs = map[string]string{}
			}
			c.orgIDs[c.Organization] = o.ID
			c.mu.Unlock()
			return o.ID, nil
		}
	}
	return "", fmt.Errorf("organization %q not found for this access token", c.Organization)
}

// Region is an organization region from GET /v1/regions.
type Region struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	CloudRegion  string  `json:"cloud_region"`
	DeploymentID *string `json:"deployment_id"`
	Enabled      bool    `json:"enabled"`
}

// Deployment returns the routing target of the region, or nil.
func (r Region) Deployment() *Deployment {
	if r.DeploymentID == nil || !validRouting(*r.DeploymentID, r.CloudRegion) {
		return nil
	}
	return &Deployment{ID: *r.DeploymentID, Region: r.CloudRegion}
}

// Regions lists the regions of the provider organization.
func (c *RawtreeClient) Regions(ctx context.Context) ([]Region, error) {
	orgID, err := c.OrganizationID(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Regions []Region `json:"regions"`
	}
	if err := c.Global(ctx, http.MethodGet, "/v1/regions", url.Values{"organization_id": {orgID}}, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Regions, nil
}

// ClusterDeployment finds the deployment that hosts a cluster (name or ID) of
// the provider organization. Nil means the default deployment.
func (c *RawtreeClient) ClusterDeployment(ctx context.Context, cluster string) (*Deployment, error) {
	route, err := c.clusterRoute(ctx, cluster)
	return route.dep, err
}

// clusterRoute resolves a cluster name or ID to its ID and deployment. It asks
// each enabled region, like the rtree CLI.
// ponytail: cached for the provider run, a cluster does not move regions.
func (c *RawtreeClient) clusterRoute(ctx context.Context, cluster string) (clusterRoute, error) {
	c.mu.Lock()
	route, ok := c.routes[cluster]
	c.mu.Unlock()
	if ok {
		return route, nil
	}
	regions, err := c.Regions(ctx)
	if err != nil {
		return clusterRoute{}, err
	}
	seen := map[Deployment]bool{}
	var lastErr error
	for _, r := range regions {
		// A region without a deployment ID is served by the default deployment.
		d := r.Deployment()
		key := Deployment{}
		if d != nil {
			key = *d
		}
		if !r.Enabled || seen[key] {
			continue
		}
		seen[key] = true
		var resp struct {
			Clusters []struct {
				ID           string  `json:"id"`
				Name         string  `json:"name"`
				DeploymentID *string `json:"deployment_id"`
				CloudRegion  *string `json:"cloud_region"`
			} `json:"clusters"`
		}
		if err := c.Deployed(ctx, d, http.MethodGet, "/v1/clusters", nil, nil, &resp); err != nil {
			lastErr = err
			continue
		}
		for _, cl := range resp.Clusters {
			if cl.ID != cluster && cl.Name != cluster {
				continue
			}
			found := clusterRoute{id: cl.ID, dep: d}
			if cl.DeploymentID != nil && cl.CloudRegion != nil && validRouting(*cl.DeploymentID, *cl.CloudRegion) {
				found.dep = &Deployment{ID: *cl.DeploymentID, Region: *cl.CloudRegion}
			}
			c.mu.Lock()
			if c.routes == nil {
				c.routes = map[string]clusterRoute{}
			}
			c.routes[cl.ID] = found
			c.routes[cl.Name] = found
			c.mu.Unlock()
			return found, nil
		}
	}
	if lastErr != nil {
		return clusterRoute{}, fmt.Errorf("cluster %q not found in organization %q; a region lookup failed: %w", cluster, c.Organization, lastErr)
	}
	return clusterRoute{}, fmt.Errorf("cluster %q not found in organization %q", cluster, c.Organization)
}

// ForgetCluster drops a cached route, for example after a cluster is deleted.
func (c *RawtreeClient) ForgetCluster(id, name string) {
	c.mu.Lock()
	delete(c.routes, id)
	delete(c.routes, name)
	c.mu.Unlock()
}

func (c *RawtreeClient) requireAccessToken() error {
	if c.AccessToken == "" {
		return errors.New("this operation needs a RawTree user access token (OAuth or session JWT): set the provider 'access_token' attribute or RAWTREE_ACCESS_TOKEN. API keys (rt_...) cannot manage clusters, connectors, service accounts or federation")
	}
	return nil
}

var hexID = regexp.MustCompile(`^([0-9a-f]{8}|[0-9a-f]{32})$`)
var cloudRegion = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)+-[1-9][0-9]*$`)

// validRouting matches the rtree CLI check on deployment IDs and regions.
func validRouting(deployment, region string) bool {
	return hexID.MatchString(deployment) && len(region) <= 63 && cloudRegion.MatchString(region)
}

// deploymentURL rewrites the hosted API origin to the regional host, for
// example https://api.rawtree.com -> https://api.<id>.<region>.aws.rawtree.com.
// Other origins (local gateways, tests) are kept; the routing headers still apply.
func (c *RawtreeClient) deploymentURL(dep *Deployment) string {
	if dep == nil {
		return c.APIURL
	}
	for _, origin := range []string{"https://api.rawtree.com", "https://api.rawtree.dev"} {
		if c.APIURL == origin {
			return strings.Replace(origin, "api.", fmt.Sprintf("api.%s.%s.aws.", dep.ID, dep.Region), 1)
		}
	}
	return c.APIURL
}

func (c *RawtreeClient) do(ctx context.Context, token, base string, headers map[string]string, method, path string, query url.Values, body, out any) error {
	u := base + path
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
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-rawtree")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
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
