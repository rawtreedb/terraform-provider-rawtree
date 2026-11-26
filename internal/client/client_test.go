package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTableURL(t *testing.T) {
	c := New("rt_x", "", "https://api.rawtree.com/", "", "", "", "")
	if got := c.TableURL("", "events", nil); got != "https://api.rawtree.com/v1/tables/events" {
		t.Errorf("no database: %s", got)
	}
	got := c.TableURL("analytics", "waf logs", url.Values{"transform": {"firehose"}})
	if got != "https://api.rawtree.com/v1/tables/waf%20logs?database=analytics&transform=firehose" {
		t.Errorf("with database: %s", got)
	}
}

func TestDeploymentURL(t *testing.T) {
	dep := &Deployment{ID: "765eb5f5", Region: "us-east-1"}
	c := New("", "jwt", "https://api.rawtree.com", "", "", "", "")
	if got := c.deploymentURL(dep); got != "https://api.765eb5f5.us-east-1.aws.rawtree.com" {
		t.Errorf("hosted: %s", got)
	}
	c.APIURL = "http://localhost:8080"
	if got := c.deploymentURL(dep); got != "http://localhost:8080" {
		t.Errorf("custom origin must be kept: %s", got)
	}
	if validRouting("765eb5f5", "us-east-01") || validRouting("xyz", "us-east-1") || !validRouting("765eb5f5", "eu-west-3") {
		t.Error("validRouting mismatch")
	}
}

// TestRouting checks the JWT path: organization -> regions -> cluster lookup
// per deployment -> routed call with selectors and routing headers.
func TestRouting(t *testing.T) {
	var routed *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt" {
			t.Errorf("auth header: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/organizations":
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"org-1","name":"acme","plan_id":"p"}]}`))
		case "/v1/regions":
			if r.URL.Query().Get("organization_id") != "org-1" {
				t.Errorf("regions query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"regions":[
				{"id":"r0","name":"off","cloud_region":"eu-west-1","deployment_id":"aaaaaaaa","enabled":false},
				{"id":"r2","name":"default","cloud_region":"us-east-1","deployment_id":null,"enabled":true},
				{"id":"r1","name":"use1","cloud_region":"us-east-1","deployment_id":"765eb5f5","enabled":true}]}`))
		case "/v1/clusters":
			if r.Header.Get("X-RawTree-Organization-Id") != "org-1" || r.URL.RawQuery != "" {
				t.Errorf("cluster list must select the organization by ID: %s %v", r.URL.RawQuery, r.Header)
			}
			// A region without a deployment ID is asked without routing headers.
			if r.Header.Get("X-RawTree-Deployment-Id") == "" {
				_, _ = w.Write([]byte(`{"clusters":[{"id":"c-0","name":"legacy","deployment_id":null,"cloud_region":null}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"clusters":[{"id":"c-1","name":"prod","deployment_id":"765eb5f5","cloud_region":"us-east-1"}]}`))
		case "/v1/connectors/x":
			routed = r
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found","message":"no connector","hint":""}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("", "jwt", srv.URL, "acme", "prod", "", "")
	err := c.InCluster(context.Background(), "prod", http.MethodGet, "/v1/connectors/x", nil, nil, nil)
	if !IsNotFound(err) {
		t.Fatalf("want 404 APIError, got %v", err)
	}
	if routed.URL.RawQuery != "" || routed.Header.Get("X-RawTree-Organization-Id") != "org-1" ||
		routed.Header.Get("X-RawTree-Cluster-Id") != "c-1" || routed.Header.Get("X-RawTree-Region") != "us-east-1" {
		t.Errorf("routed request: %s %v", routed.URL.RawQuery, routed.Header)
	}
	// The ID resolves from cache too.
	if dep, err := c.ClusterDeployment(context.Background(), "c-1"); err != nil || dep.ID != "765eb5f5" {
		t.Errorf("cache by id: %v %v", dep, err)
	}
	// A cluster on the default deployment resolves to no routing headers.
	if dep, err := c.ClusterDeployment(context.Background(), "legacy"); err != nil || dep != nil {
		t.Errorf("default deployment: %v %v", dep, err)
	}
}

func TestDataPrefersAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rt_key" || r.Header.Get("X-RawTree-Organization-Id") != "" {
			t.Errorf("api key call must be unrouted: %s %v", r.URL.RawQuery, r.Header)
		}
		_, _ = w.Write([]byte(`{"databases":[{"name":"default"}]}`))
	}))
	defer srv.Close()
	c := New("rt_key", "jwt", srv.URL, "acme", "prod", "", "")
	var out struct {
		Databases []struct{ Name string } `json:"databases"`
	}
	if err := c.Data(context.Background(), http.MethodGet, "/v1/databases", nil, nil, &out); err != nil || out.Databases[0].Name != "default" {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestControlPlaneNeedsAccessToken(t *testing.T) {
	c := New("rt_key", "", "https://api.rawtree.com", "acme", "prod", "", "")
	if err := c.Global(context.Background(), http.MethodGet, "/v1/organizations", nil, nil, nil); err == nil {
		t.Fatal("want access token error")
	}
}

func TestIsAPIError(t *testing.T) {
	err := fmt.Errorf("DELETE /v1/tables/x: %w", &APIError{Status: http.StatusBadRequest, Message: "Table not found."})
	if !IsAPIError(err, http.StatusBadRequest, "Table not found.") || IsAPIError(err, http.StatusNotFound, "Table not found.") || IsNotFound(err) {
		t.Error("IsAPIError must match the status and the message")
	}
}
