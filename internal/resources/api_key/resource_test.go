package api_key

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

const keyID = "8d6f0d3e-2f4e-4c55-9a51-0d9c2e6f1a11"

func TestResourceSchema(t *testing.T) {
	t.Parallel()
	s := resourceSchema()
	if diags := s.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
	if !s.Attributes["name"].IsRequired() {
		t.Error("name should be required")
	}
	token := s.Attributes["token"]
	if !token.IsComputed() || !token.IsSensitive() || token.IsOptional() {
		t.Error("token should be computed and sensitive")
	}
	for _, attr := range []string{"database", "principal_type", "principal_id"} {
		if !s.Attributes[attr].IsOptional() || !s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be optional and computed", attr)
		}
	}
}

type fakeAPI struct {
	query   string
	created map[string]any
	keys    []map[string]any
	deleted bool
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/api-keys":
			f.query = r.URL.RawQuery
			_ = json.NewDecoder(r.Body).Decode(&f.created)
			key := map[string]any{
				"id": keyID, "token": "rt_secret", "name": f.created["name"],
				"permission": f.created["permission"], "database_roles": nil,
				"expires_at":     "2027-01-01T00:00:00Z",
				"principal_type": "user", "principal_id": "0b7c1e7c-1111-4a5b-9c3d-222222222222",
				"database": map[string]any{"name": r.URL.Query().Get("database")},
			}
			_ = json.NewEncoder(w).Encode(key)
			listed := map[string]any{"created_at": "2026-11-24T10:00:00Z"}
			for k, v := range key {
				listed[k] = v
			}
			// The list does not return the secret.
			listed["token"] = "rt_****"
			f.keys = append(f.keys, listed)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/api-keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": f.keys})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/api-keys/"+keyID:
			f.deleted = true
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func setup(t *testing.T) (*APIKeyResource, *fakeAPI, fwschema.Schema) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	return &APIKeyResource{client: client.New("rt_test", "", srv.URL, "", "", "analytics", "")}, api, resourceSchema()
}

func nullRaw(s fwschema.Schema) tftypes.Value {
	return tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)
}

func TestCreateReadDelete(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t)

	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	plan.Set(ctx, &APIKeyModel{
		ID:            types.StringUnknown(),
		Name:          types.StringValue("ingest"),
		Database:      types.StringUnknown(),
		Permission:    types.StringValue("write_only"),
		DatabaseRoles: types.SetNull(types.StringType),
		ExpiresAt:     types.StringValue("2027-01-01T01:00:00+01:00"),
		PrincipalType: types.StringUnknown(),
		PrincipalID:   types.StringUnknown(),
		Token:         types.StringUnknown(),
		CreatedAt:     types.StringUnknown(),
	})
	createResp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatal(createResp.Diagnostics)
	}
	if api.query != "database=analytics" {
		t.Errorf("unexpected query %q", api.query)
	}
	if api.created["permission"] != "write_only" || api.created["expires_at"] != "2027-01-01T01:00:00+01:00" {
		t.Errorf("unexpected body: %v", api.created)
	}
	if _, ok := api.created["principal_type"]; ok {
		t.Errorf("unset principal should be omitted: %v", api.created)
	}

	var got APIKeyModel
	createResp.State.Get(ctx, &got)
	if got.ID.ValueString() != keyID || got.Token.ValueString() != "rt_secret" || got.Database.ValueString() != "analytics" ||
		got.PrincipalType.ValueString() != "user" || got.CreatedAt.ValueString() == "" ||
		got.ExpiresAt.ValueString() != "2027-01-01T01:00:00+01:00" {
		t.Errorf("unexpected state after create: %+v", got)
	}

	readResp := &resource.ReadResponse{State: createResp.State}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatal(readResp.Diagnostics)
	}
	readResp.State.Get(ctx, &got)
	if got.Token.ValueString() != "rt_secret" {
		t.Errorf("read overwrote the token: %q", got.Token.ValueString())
	}
	if got.ExpiresAt.ValueString() != "2027-01-01T01:00:00+01:00" {
		t.Errorf("same instant should keep the configured form: %q", got.ExpiresAt.ValueString())
	}

	r.Delete(ctx, resource.DeleteRequest{State: readResp.State}, &resource.DeleteResponse{})
	if !api.deleted {
		t.Error("key was not deleted")
	}
}

func TestImportAndMissing(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t)
	api.keys = []map[string]any{{
		"id": keyID, "token": "rt_****", "name": "reader", "permission": nil,
		"database_roles": []string{"b", "a"}, "expires_at": nil,
		"principal_type": "service_account", "principal_id": "0b7c1e7c-1111-4a5b-9c3d-222222222222",
		"database": map[string]any{"name": "default"}, "created_at": "2026-11-24T10:00:00Z",
	}}

	importResp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: keyID}, importResp)
	readResp := &resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatal(readResp.Diagnostics)
	}
	var got APIKeyModel
	readResp.State.Get(ctx, &got)
	if !got.Token.IsNull() || got.Name.ValueString() != "reader" || !got.Permission.IsNull() || len(got.DatabaseRoles.Elements()) != 2 || !got.ExpiresAt.IsNull() {
		t.Errorf("unexpected imported state: %+v", got)
	}

	api.keys = nil
	missing := &resource.ReadResponse{State: readResp.State}
	r.Read(ctx, resource.ReadRequest{State: readResp.State}, missing)
	if !missing.State.Raw.IsNull() {
		t.Error("missing key should be removed from state")
	}
}
