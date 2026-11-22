package supabase_cdc_ingestion

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

func TestVerifyStockWorkerDefault(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"matches", `{"data":[{"database":"events"}]}`, 200, false},
		{"different database", `{"data":[{"database":"default"}]}`, 200, true},
		{"query denied", `{}`, 403, true},
		{"missing result", `{"data":[]}`, 200, true},
		{"invalid response", `other`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RequestURI() != "/v1/query" || r.Header.Get("Authorization") != "Bearer worker-key" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				var q struct{ SQL string }
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil || q.SQL != "SELECT currentDatabase() AS database" {
					t.Errorf("unexpected query: %v, %v", q, err)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := client.New("worker-key", server.URL, "org", "events")
			cfg := resolvedConfig{Image: defaultImage, APIURL: server.URL, Database: "events"}
			err := verifyWorkerDestination(t.Context(), c, cfg)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected verification result %v", err)
			}
		})
	}
}
