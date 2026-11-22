package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDatabaseRequests(t *testing.T) {
	db := Database{Organization: "test-org", Cluster: "test-cluster", Name: "events"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("organization") != db.Organization || req.URL.Query().Get("cluster") != db.Cluster {
			t.Errorf("unexpected scope %v", req.URL.Query())
		}
		if req.Header.Get("Authorization") != "Bearer management" {
			t.Error("missing management credential")
		}
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/databases":
			_, _ = fmt.Fprint(w, `{"databases":[{"name":"events","s3_storage":null}]}`)
		case "POST /v1/databases":
			body, err := io.ReadAll(req.Body)
			if err != nil || string(body) != `{"name":"events"}` {
				t.Errorf("unexpected create request %q (%v)", body, err)
			}
			_, _ = fmt.Fprint(w, `{"database":{"name":"events"}}`)
		case "DELETE /v1/databases/events":
			_, _ = fmt.Fprint(w, `{"deleted":false}`)
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := NewPlatformClient(server.URL+"/", "management")
	ctx := context.Background()
	if ok, err := c.DatabaseExists(ctx, db); err != nil || !ok {
		t.Fatalf("expected database, got %v (%v)", ok, err)
	}
	if err := c.CreateDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseReadErrorsAreNotAbsence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		body    string
		wantErr bool
	}{
		{"missing", 200, `{"databases":[]}`, false},
		{"denied", 403, `{"message":"permission denied"}`, true},
		{"scope not found", 404, `{}`, true},
		{"incomplete", 200, `{}`, true},
		{"missing name", 200, `{"databases":[{}]}`, true},
		{"malformed", 200, `{"databases":`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			exists, err := NewPlatformClient(server.URL, "secret").DatabaseExists(context.Background(), Database{Name: "events"})
			if exists || (err != nil) != tc.wantErr {
				t.Fatalf("unexpected result %v, %v", exists, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("error contains the credential")
			}
		})
	}
}
