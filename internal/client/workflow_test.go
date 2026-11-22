package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testWorkflowID = "11111111-1111-4111-8111-111111111111"

func TestWorkflowReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		missing    bool
	}{
		{"absent", `{"message":"Workflow not found."}`, 404, true},
		{"unknown scope", `{"message":"Cluster not found."}`, 404, false},
		{"denied", `{"message":"Permission denied."}`, 403, false},
		{"disabled", `{"message":"Workflows are not available."}`, 503, false},
		{"missing definition", `{}`, 200, false},
		{"other workflow", `{"id":"22222222-2222-4222-8222-222222222222","name":"other","query":{"database":"default","sql":"SELECT 1"}}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("organization") != "org" || r.URL.Query().Get("cluster") != "cluster" || r.URL.Path != "/v1/workflows/"+testWorkflowID || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Errorf("unexpected request scope, path or credential")
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := NewPlatformClient(server.URL, "fixture")
			w, err := c.GetWorkflow(context.Background(), WorkflowScope{"org", "cluster"}, testWorkflowID)
			if tc.missing {
				if w != nil || err != nil {
					t.Fatalf("expected absence, got %v", err)
				}
			} else if err == nil {
				t.Fatal("expected a read error")
			}
			if tc.status == 404 {
				err = c.DeleteWorkflow(context.Background(), WorkflowScope{"org", "cluster"}, testWorkflowID)
				if (err == nil) != tc.missing {
					t.Fatalf("unexpected delete result %v", err)
				}
			}
		})
	}
}

func TestListWorkflows(t *testing.T) {
	const body = `{"workflows":[{"id":"11111111-1111-4111-8111-111111111111","name":"alerts","query":{"database":"analytics","sql":"SELECT 1"},"schedule":{"cron":"0 8 * * *","enabled":true},"sinks":[{"type":"slack","id":"22222222-2222-4222-8222-222222222222","settings":{"integration_id":"33333333-3333-4333-8333-333333333333","channel_id":"C0123456789","channel_name":"alerts"}}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/workflows" || r.URL.Query().Get("organization") != "org" || r.URL.Query().Get("cluster") != "cluster" {
			t.Error("unexpected list request")
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	items, err := NewPlatformClient(server.URL, "fixture").ListWorkflows(t.Context(), WorkflowScope{"org", "cluster"})
	if err != nil || len(items) != 1 {
		t.Fatalf("workflow list: %v", err)
	}
	w := items[0]
	if w.ID != testWorkflowID || w.Schedule == nil || !w.Schedule.Enabled || w.Sinks == nil || len(*w.Sinks) != 1 {
		t.Fatal("incomplete workflow")
	}
	s := (*w.Sinks)[0]
	if s.Type != "slack" || s.Settings.ChannelID != "C0123456789" {
		t.Fatal("incorrect Slack destination")
	}
}
