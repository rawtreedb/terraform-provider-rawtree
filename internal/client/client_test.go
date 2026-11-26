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
	c := New("rt_x", "https://api.rawtree.com/", "", "", "")
	if got := c.TableURL("", "events", nil); got != "https://api.rawtree.com/v1/tables/events" {
		t.Errorf("no database: %s", got)
	}
	got := c.TableURL("analytics", "waf logs", url.Values{"transform": {"firehose"}})
	if got != "https://api.rawtree.com/v1/tables/waf%20logs?database=analytics&transform=firehose" {
		t.Errorf("with database: %s", got)
	}
}

func TestDataSendsAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rt_key" {
			t.Errorf("auth header: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"databases":[{"name":"default"}]}`))
	}))
	defer srv.Close()
	c := New("rt_key", srv.URL, "", "", "")
	var out struct {
		Databases []struct{ Name string } `json:"databases"`
	}
	if err := c.Data(context.Background(), http.MethodGet, "/v1/databases", nil, nil, &out); err != nil || out.Databases[0].Name != "default" {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestIsAPIError(t *testing.T) {
	err := fmt.Errorf("DELETE /v1/tables/x: %w", &APIError{Status: http.StatusBadRequest, Message: "Table not found."})
	if !IsAPIError(err, http.StatusBadRequest, "Table not found.") || IsAPIError(err, http.StatusNotFound, "Table not found.") || IsNotFound(err) {
		t.Error("IsAPIError must match the status and the message")
	}
}
