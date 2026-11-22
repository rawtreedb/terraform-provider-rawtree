package database

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

func TestCreateRejectsExistingDatabase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Errorf("unexpected %s for a database that already exists", req.Method)
		}
		_, _ = fmt.Fprint(w, `{"databases":[{"name":"events"}]}`)
	}))
	defer server.Close()

	ctx := context.Background()
	r := &databaseResource{client: client.NewPlatformClient(server.URL, "test")}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	plan := tfsdk.Plan{Schema: schema.Schema}
	diags := plan.Set(ctx, databaseModel{
		ID: types.StringUnknown(), Organization: types.StringValue("org"), Cluster: types.StringValue("cluster"), Name: types.StringValue("events"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Database already exists" {
		t.Fatalf("expected an import diagnostic, got %v", resp.Diagnostics)
	}
}

func TestImportDatabaseIdentity(t *testing.T) {
	ctx := context.Background()
	r := databaseResource{}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	for _, tc := range []struct {
		id    string
		valid bool
	}{
		{"org/cluster/events", true}, {"events", false}, {"org//events", false},
		{"org/cluster/default", false}, {"org/cluster/system", false}, {"org/cluster/invalid-name", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
			r.ImportState(ctx, resource.ImportStateRequest{ID: tc.id}, &resp)
			if resp.Diagnostics.HasError() == tc.valid {
				t.Fatalf("unexpected import result: %v", resp.Diagnostics)
			}
			if tc.valid {
				var state databaseModel
				if diags := resp.State.Get(ctx, &state); diags.HasError() {
					t.Fatal(diags.Errors())
				}
				if state.ID.ValueString() != tc.id || state.database().ID() != tc.id {
					t.Fatalf("unexpected import state: %+v", state)
				}
			}
		})
	}
}

func TestImportDiscoveredIdentity(t *testing.T) {
	ctx := t.Context()
	var r databaseResource
	var schema resource.SchemaResponse
	var ids resource.IdentitySchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &ids)
	identity := &tfsdk.ResourceIdentity{Schema: ids.IdentitySchema}
	id := "org/cluster/events"
	if diags := identity.Set(ctx, struct {
		ID string `tfsdk:"id"`
	}{ID: id}); diags.HasError() {
		t.Fatal(diags)
	}
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}, Identity: identity}
	r.ImportState(ctx, resource.ImportStateRequest{Identity: identity}, &resp)
	var state databaseModel
	var imported types.String
	resp.Diagnostics.Append(resp.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.GetAttribute(ctx, path.Root("id"), &imported)...)
	if resp.Diagnostics.HasError() || state.database().ID() != id || state.ID.ValueString() != id || !state.ID.Equal(imported) {
		t.Fatalf("unexpected imported state %#v, identity %s, diagnostics %v", state, imported, resp.Diagnostics)
	}
}
