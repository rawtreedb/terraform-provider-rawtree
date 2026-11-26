package table

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
	for _, attr := range []string{"database", "sorting_key", "replacing_enable"} {
		if !s.Attributes[attr].IsOptional() || !s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be optional and computed", attr)
		}
	}
	for _, attr := range []string{"ttl", "settings_json"} {
		if !s.Attributes[attr].IsOptional() || s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be optional only", attr)
		}
	}
}

type call struct {
	Method, Path, Query string
	Body                map[string]any
}

type fakeAPI struct {
	calls      []call
	sortingKey string
	ttl        any
	replacing  any
	missing    bool
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		_ = json.NewDecoder(r.Body).Decode(&c.Body)
		f.calls = append(f.calls, c)
		db := r.URL.Query().Get("database")
		if db == "" {
			db = "key_db"
		}
		switch {
		case f.missing:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found","message":"table not found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tables":
			// The engine rewrites the TTL expression.
			f.sortingKey = "region, timestamp"
			f.ttl = "timestamp + toIntervalDay(30)"
			f.replacing = false
			_ = json.NewEncoder(w).Encode(map[string]any{"database": db, "table": "events", "sorting_key": f.sortingKey, "ttl": f.ttl, "settings": []any{}, "storage": map[string]any{"type": "default"}})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/tables/events":
			// The API accepts one field per request, and reads a null ttl as absent.
			if len(c.Body) != 1 || c.Body["ttl"] == nil && c.Body["sorting_key"] == nil && c.Body["settings"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad_request","message":"Update sorting_key, ttl, and settings in separate requests."}`))
				return
			}
			if v, ok := c.Body["sorting_key"]; ok {
				f.sortingKey = v.(string)
			}
			if v, ok := c.Body["ttl"]; ok {
				f.ttl = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"database": db, "table": "events", "sorting_key": f.sortingKey, "ttl": f.ttl, "settings": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tables/events":
			_ = json.NewEncoder(w).Encode(map[string]any{"table": map[string]any{"name": "events", "created_at": "2026-01-01", "total_rows": 0, "total_bytes": 0, "columns": []any{}, "sorting_key": f.sortingKey, "ttl": f.ttl, "settings": []any{}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/tables/events/settings":
			f.replacing = c.Body["replacing_enable"]
			_ = json.NewEncoder(w).Encode(c.Body)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tables/events/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{"settings": []any{}, "sorting_key": f.sortingKey, "replacing_enable": f.replacing, "can_update": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/tables/events":
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func setup(t *testing.T, providerDB string) (*TableResource, *fakeAPI, fwschema.Schema) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	return &TableResource{client: client.New("rt_test", "", srv.URL, "", "", providerDB, "")}, api, resourceSchema()
}

func nullRaw(s fwschema.Schema) tftypes.Value {
	return tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)
}

func createTable(t *testing.T, r *TableResource, s fwschema.Schema, m TableModel) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	if diags := plan.Set(ctx, &m); diags.HasError() {
		t.Fatal(diags)
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.State
}

func newPlan() TableModel {
	return TableModel{
		ID:               types.StringUnknown(),
		Database:         types.StringUnknown(),
		Name:             types.StringValue("events"),
		SortingKey:       types.StringValue("region,timestamp"),
		TTL:              types.StringValue("timestamp + INTERVAL 30 DAY"),
		SettingsJSON:     types.StringValue(`{"index_granularity": 8192}`),
		ReplacingEnable:  types.BoolValue(true),
		StoredSortingKey: types.StringUnknown(),
		StoredTTL:        types.StringUnknown(),
	}
}

func TestCreateUsesProviderDatabase(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "analytics")
	state := createTable(t, r, s, newPlan())

	create := api.calls[0]
	if create.Query != "database=analytics" || create.Body["sorting_key"] != "region,timestamp" || create.Body["ttl"] != "timestamp + INTERVAL 30 DAY" {
		t.Errorf("unexpected create call: %+v", create)
	}
	if create.Body["settings"].(map[string]any)["index_granularity"] != float64(8192) {
		t.Errorf("settings not sent: %+v", create.Body)
	}
	if api.calls[1].Path != "/v1/tables/events/settings" || api.calls[1].Body["replacing_enable"] != true {
		t.Errorf("replacing_enable not sent: %+v", api.calls[1])
	}

	var got TableModel
	state.Get(ctx, &got)
	// Configured values are kept; the stored form is recorded separately.
	if got.ID.ValueString() != "analytics/events" || got.TTL.ValueString() != "timestamp + INTERVAL 30 DAY" || got.StoredTTL.ValueString() != "timestamp + toIntervalDay(30)" || !got.ReplacingEnable.ValueBool() {
		t.Errorf("unexpected state: %+v", got)
	}

	// Read keeps the configured expressions while the stored form is unchanged.
	readResp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, readResp)
	readResp.State.Get(ctx, &got)
	if got.SortingKey.ValueString() != "region,timestamp" || got.TTL.ValueString() != "timestamp + INTERVAL 30 DAY" {
		t.Errorf("read replaced configured expressions: %+v", got)
	}

	// A change outside Terraform shows up as drift.
	api.sortingKey = "region"
	api.ttl = nil
	r.Read(ctx, resource.ReadRequest{State: state}, readResp)
	readResp.State.Get(ctx, &got)
	if got.SortingKey.ValueString() != "region" || !got.TTL.IsNull() {
		t.Errorf("read missed drift: %+v", got)
	}
}

func TestCreateWithoutDatabaseUsesKeyDatabase(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "")
	m := newPlan()
	m.SortingKey = types.StringUnknown()
	m.TTL = types.StringNull()
	m.SettingsJSON = types.StringNull()
	m.ReplacingEnable = types.BoolUnknown()
	state := createTable(t, r, s, m)

	if api.calls[0].Query != "" || api.calls[0].Body["sorting_key"] != nil {
		t.Errorf("unexpected create call: %+v", api.calls[0])
	}
	if api.calls[1].Method != http.MethodGet {
		t.Errorf("replacing_enable should only be read: %+v", api.calls[1])
	}
	var got TableModel
	state.Get(ctx, &got)
	if got.Database.ValueString() != "key_db" || got.ID.ValueString() != "key_db/events" || got.SortingKey.ValueString() != "region, timestamp" || got.ReplacingEnable.ValueBool() {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "analytics")
	state := createTable(t, r, s, newPlan())
	api.calls = nil

	var m TableModel
	state.Get(ctx, &m)
	m.SortingKey = types.StringValue("region")
	m.TTL = types.StringValue("timestamp + INTERVAL 7 DAY")
	m.SettingsJSON = types.StringValue(`{"merge_with_ttl_timeout": 3600}`)
	m.ReplacingEnable = types.BoolValue(false)
	m.StoredSortingKey = types.StringUnknown()
	m.StoredTTL = types.StringUnknown()
	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	plan.Set(ctx, &m)

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Errorf("expected a warning for the removed setting, got %v", resp.Diagnostics)
	}
	// One PATCH per field, then the replacing_enable call.
	for i, field := range []string{"sorting_key", "ttl", "settings"} {
		patch := api.calls[i]
		if patch.Method != http.MethodPatch || patch.Path != "/v1/tables/events" || patch.Query != "database=analytics" || len(patch.Body) != 1 || patch.Body[field] == nil {
			t.Errorf("call %d should patch only %s: %+v", i, field, patch)
		}
	}
	if api.calls[3].Body["replacing_enable"] != false {
		t.Errorf("unexpected settings call: %+v", api.calls[3])
	}
	var got TableModel
	resp.State.Get(ctx, &got)
	if got.StoredSortingKey.ValueString() != "region" || got.StoredTTL.ValueString() != "timestamp + INTERVAL 7 DAY" || got.ReplacingEnable.ValueBool() {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestUpdateRejectsTTLRemoval(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "analytics")
	state := createTable(t, r, s, newPlan())
	api.calls = nil

	var m TableModel
	state.Get(ctx, &m)
	m.SortingKey = types.StringValue("region")
	m.TTL = types.StringNull()
	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	plan.Set(ctx, &m)

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)
	if !resp.Diagnostics.HasError() || len(api.calls) != 0 {
		t.Errorf("ttl removal must fail before any API call: %v, calls %+v", resp.Diagnostics, api.calls)
	}
}

func TestUpdateRejectsEmptySortingKey(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "analytics")
	state := createTable(t, r, s, newPlan())
	api.calls = nil

	var m TableModel
	state.Get(ctx, &m)
	m.SortingKey = types.StringValue("")
	plan := tfsdk.Plan{Schema: s, Raw: nullRaw(s)}
	plan.Set(ctx, &m)

	resp := &resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)
	if !resp.Diagnostics.HasError() || len(api.calls) != 0 {
		t.Errorf("an empty sorting key must fail before any API call: %v, calls %+v", resp.Diagnostics, api.calls)
	}
}

func TestReadRemovesMissingTable(t *testing.T) {
	ctx := context.Background()
	r, api, s := setup(t, "analytics")
	state := createTable(t, r, s, newPlan())
	api.missing = true

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("missing table should be removed from state")
	}
}

func TestImportState(t *testing.T) {
	ctx := context.Background()
	r, _, s := setup(t, "fallback")
	for id, want := range map[string][2]string{
		"analytics/events": {"analytics", "analytics/events"},
		"events":           {"fallback", "fallback/events"},
	} {
		resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: nullRaw(s)}}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, resp)
		var got TableModel
		resp.State.Get(ctx, &got)
		if got.Database.ValueString() != want[0] || got.ID.ValueString() != want[1] || got.Name.ValueString() != "events" {
			t.Errorf("import %q: %+v", id, got)
		}
	}
}

func TestSettingsSemanticEqual(t *testing.T) {
	req := planmodifier.StringRequest{
		StateValue: types.StringValue(`{"a":1,"b":"x"}`),
		PlanValue:  types.StringValue(`{ "b": "x", "a": 1 }`),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	settingsSemanticEqual{}.PlanModifyString(context.Background(), req, resp)
	if !resp.PlanValue.Equal(req.StateValue) {
		t.Error("equal settings should keep the state value")
	}
	if _, err := parseSettings(`{"a":{"nested":true}}`); err == nil {
		t.Error("nested settings should be rejected")
	}
}
