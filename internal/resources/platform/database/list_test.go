package database

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

func TestDatabaseList(t *testing.T) {
	ctx := t.Context()
	for _, tc := range []struct {
		name, body string
		status     int
		limit      int64
		full       bool
		want       []string
	}{
		{"all", `{"databases":[{"name":"default"},{"name":"events"},{"name":"invalid-name"},{"name":"metrics"},{"name":"system"}]}`, 200, 100, true, []string{"events", "metrics"}},
		{"limit after exclusions", `{"databases":[{"name":"system"},{"name":"events"},{"name":"metrics"}]}`, 200, 1, false, []string{"events"}},
		{"built-ins only", `{"databases":[{"name":"default"},{"name":"system"},{"name":"information_schema"},{"name":"INFORMATION_SCHEMA"},{"name":"rawtree"}]}`, 200, 100, true, nil},
		{"permission denied", `{"message":"permission denied"}`, 403, 100, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != "GET" || r.URL.Path != "/v1/databases" || r.URL.Query().Get("organization") != "org" || r.URL.Query().Get("cluster") != "cluster" || r.Header.Get("Authorization") != "Bearer test" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			r := databaseResource{client: client.NewPlatformClient(server.URL, "test")}
			var cfg list.ListResourceSchemaResponse
			var managed resource.SchemaResponse
			var identity resource.IdentitySchemaResponse
			r.ListResourceConfigSchema(ctx, list.ListResourceSchemaRequest{}, &cfg)
			r.Schema(ctx, resource.SchemaRequest{}, &managed)
			r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &identity)
			raw := tftypes.NewValue(cfg.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{
				"organization": tftypes.NewValue(tftypes.String, "org"), "cluster": tftypes.NewValue(tftypes.String, "cluster"),
			})
			req := list.ListRequest{Config: tfsdk.Config{Raw: raw, Schema: cfg.Schema}, Limit: tc.limit, IncludeResource: tc.full, ResourceSchema: managed.Schema, ResourceIdentitySchema: identity.IdentitySchema}
			var stream list.ListResultsStream
			r.List(ctx, req, &stream)
			var names []string
			var sawError bool
			for result := range stream.Results {
				if result.Diagnostics.HasError() {
					sawError = true
					break
				}
				names = append(names, result.DisplayName)
				var id types.String
				diags := result.Identity.GetAttribute(ctx, path.Root("id"), &id)
				if diags.HasError() || id.ValueString() != "org/cluster/"+result.DisplayName {
					t.Fatalf("unexpected identity %s, %v", id, diags)
				}
				if tc.full {
					var state databaseModel
					if diags := result.Resource.Get(ctx, &state); diags.HasError() || !state.ID.Equal(id) || state.database().ID() != id.ValueString() {
						t.Fatalf("unexpected generated resource %#v, %v", state, diags)
					}
				} else if !result.Resource.Raw.IsNull() {
					t.Error("unexpected resource data on identity-only query")
				}
			}
			if !slices.Equal(names, tc.want) || sawError != (tc.status != 200) || requests.Load() != 1 {
				t.Fatalf("got names %v, error %t, requests %d", names, sawError, requests.Load())
			}
		})
	}
}
