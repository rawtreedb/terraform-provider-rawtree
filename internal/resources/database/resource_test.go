package database

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

func TestResourceSchema(t *testing.T) {
	t.Parallel()
	s := resourceSchema()
	if diags := s.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags)
	}
	if !s.Attributes["name"].IsRequired() {
		t.Error("name should be required")
	}
	if !s.Attributes["s3_storage"].IsOptional() {
		t.Error("s3_storage should be optional")
	}
	if !s.Attributes["id"].IsComputed() {
		t.Error("id should be computed")
	}
}

type fakeAPI struct {
	databases []map[string]any
	created   map[string]any
	deleted   string
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rt_test" {
			t.Errorf("missing API key, got %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/databases":
			_ = json.NewDecoder(r.Body).Decode(&f.created)
			item := map[string]any{"name": f.created["name"], "s3_storage": nil}
			if s, ok := f.created["s3_storage"].(map[string]any); ok {
				// The API returns data and backups without role_arn.
				item["s3_storage"] = map[string]any{"data": s["data"], "backups": s["backups"]}
			}
			f.databases = append(f.databases, item)
			_ = json.NewEncoder(w).Encode(map[string]any{"database": map[string]any{"name": f.created["name"]}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/databases":
			_ = json.NewEncoder(w).Encode(map[string]any{"databases": f.databases})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/databases/analytics":
			f.deleted = "analytics"
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func setup(t *testing.T) (*DatabaseResource, *fakeAPI, fwschema.Schema) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	return &DatabaseResource{client: client.New("rt_test", "", srv.URL, "", "", "", "")}, api, resourceSchema()
}

func nullRaw(s fwschema.Schema) tftypes.Value {
	return tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)
}

func TestCreateReadDelete(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t)

	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	diags := plan.Set(ctx, &DatabaseModel{
		ID:   types.StringUnknown(),
		Name: types.StringValue("analytics"),
		S3Storage: &S3StorageModel{
			DataBucket:    types.StringValue("data-bucket"),
			DataPath:      types.StringValue("tables/"),
			BackupsBucket: types.StringValue("backup-bucket"),
			BackupsPath:   types.StringUnknown(),
			RoleARN:       types.StringValue("arn:aws:iam::123456789012:role/rawtree"),
		},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	createResp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatal(createResp.Diagnostics)
	}
	storage := api.created["s3_storage"].(map[string]any)
	if storage["role_arn"] != "arn:aws:iam::123456789012:role/rawtree" || storage["data"].(map[string]any)["path"] != "tables/" {
		t.Errorf("unexpected s3_storage body: %v", storage)
	}

	var got DatabaseModel
	createResp.State.Get(ctx, &got)
	if got.ID.ValueString() != "analytics" || got.S3Storage.BackupsPath.ValueString() != "" {
		t.Errorf("unexpected state after create: %+v %+v", got, got.S3Storage)
	}

	// The API strips the trailing slash: the configured path is kept.
	api.databases[0]["s3_storage"].(map[string]any)["data"] = map[string]any{"bucket": "data-bucket", "path": "tables"}
	readResp := &resource.ReadResponse{State: createResp.State}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatal(readResp.Diagnostics)
	}
	readResp.State.Get(ctx, &got)
	if got.S3Storage.DataPath.ValueString() != "tables/" || got.S3Storage.RoleARN.ValueString() == "" {
		t.Errorf("read lost configured values: %+v", got.S3Storage)
	}

	r.Delete(ctx, resource.DeleteRequest{State: readResp.State}, &resource.DeleteResponse{})
	if api.deleted != "analytics" {
		t.Error("database was not deleted")
	}
}

func TestReadRemovesMissingDatabase(t *testing.T) {
	ctx := context.Background()
	r, _, s := setup(t)

	state := tfsdk.State{Schema: s, Raw: nullRaw(s)}
	state.Set(ctx, &DatabaseModel{ID: types.StringValue("gone"), Name: types.StringValue("gone")})
	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("missing database should be removed from state")
	}
}

func TestImportReadsStorage(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t)
	api.databases = []map[string]any{{"name": "analytics", "s3_storage": map[string]any{
		"data":    map[string]any{"bucket": "d", "path": "p"},
		"backups": map[string]any{"bucket": "b", "path": ""},
	}}}

	importResp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "analytics"}, importResp)
	readResp := &resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatal(readResp.Diagnostics)
	}
	var got DatabaseModel
	readResp.State.Get(ctx, &got)
	if got.Name.ValueString() != "analytics" || got.S3Storage == nil || got.S3Storage.DataBucket.ValueString() != "d" || !got.S3Storage.RoleARN.IsNull() {
		t.Errorf("unexpected imported state: %+v %+v", got, got.S3Storage)
	}
}

func TestCreateRejectsExistingDatabase(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t)
	api.databases = []map[string]any{{"name": "analytics", "s3_storage": nil}}

	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	plan.Set(ctx, &DatabaseModel{ID: types.StringUnknown(), Name: types.StringValue("analytics")})
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	if !resp.Diagnostics.HasError() || api.created != nil {
		t.Errorf("create must fail without a POST for an existing database: %v, created %+v", resp.Diagnostics, api.created)
	}
}
