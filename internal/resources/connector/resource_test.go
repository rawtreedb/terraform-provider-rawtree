package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

type call struct {
	key string
	// query is "<organization ID header>/<cluster ID header>?<raw query>".
	query string
	body  map[string]any
}

// apiItem is what the fake API returns: password stripped, defaults added.
const apiItem = `{
  "id": "conn-1", "name": "orders", "type": "postgres", "status": "%s",
  "destinations": [{"id": "dest-1", "database": "shop", "table": "orders", "postgres_tables": ["public.orders"], "initial_load": true}],
  "settings": {"host": "db.example.com", "port": 5432, "database": "shop", "user": "rawtree", "publication": "pub", "slot": "rawtree_slot"},
  "credentials_configured": true, "slot_owned": true, "revision": %d,
  "created_at": "2026-11-24T10:00:00Z", "updated_at": "2026-11-24T10:00:00Z"
}`

func newFakeAPI(t *testing.T, getStatus int) (*[]call, *httptest.Server) {
	calls := &[]call{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{key: r.Method + " " + r.URL.Path, query: r.Header.Get("X-RawTree-Organization-Id") + "/" + r.Header.Get("X-RawTree-Cluster-Id") + "?" + r.URL.RawQuery}
		_ = json.NewDecoder(r.Body).Decode(&c.body)
		switch c.key {
		case "GET /v1/organizations":
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"org-1","name":"acme"}]}`))
			return
		case "GET /v1/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"r-1","name":"Ireland","cloud_region":"eu-west-1","deployment_id":"abcd1234","enabled":true}]}`))
			return
		case "GET /v1/clusters":
			_, _ = w.Write([]byte(`{"clusters":[{"id":"c-1","name":"main"}]}`))
			return
		}
		*calls = append(*calls, c)
		status, revision := "active", 1
		switch c.key {
		case "POST /v1/connectors":
			w.WriteHeader(http.StatusCreated)
		case "PATCH /v1/connectors/conn-1":
			revision = 2
			if c.body["status"] == "paused" {
				status = "paused"
			}
		case "GET /v1/connectors/conn-1":
			if getStatus != http.StatusOK {
				w.WriteHeader(getStatus)
				_, _ = w.Write([]byte(`{"error":"not_found","message":"Connector not found."}`))
				return
			}
		case "DELETE /v1/connectors/conn-1":
			_, _ = w.Write([]byte(`{"deleted":true}`))
			return
		default:
			t.Errorf("unexpected request %s", c.key)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprintf(w, apiItem, status, revision)
	}))
	t.Cleanup(srv.Close)
	return calls, srv
}

func newTestResource(apiURL string) *ConnectorResource {
	return &ConnectorResource{client: client.New("", "jwt", apiURL, "acme", "main", "", "")}
}

func emptyState() tfsdk.State {
	s := resourceSchema()
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func stateFrom(t *testing.T, m ConnectorModel) tfsdk.State {
	t.Helper()
	s := emptyState()
	if d := s.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return s
}

// planAndConfig returns the plan and a config that also holds the write-only credentials.
func planAndConfig(t *testing.T, m ConnectorModel, credentials string) (tfsdk.Plan, tfsdk.Config) {
	t.Helper()
	p := tfsdk.Plan{Schema: resourceSchema()}
	if d := p.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	m.CredentialsJSON = types.StringValue(credentials)
	c := tfsdk.Plan{Schema: resourceSchema()}
	if d := c.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("config: %v", d)
	}
	return p, tfsdk.Config(c)
}

func stateModel(t *testing.T, s tfsdk.State) ConnectorModel {
	t.Helper()
	var m ConnectorModel
	if d := s.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return m
}

const configuredSettings = `{"host":"db.example.com","database":"shop","user":"rawtree","publication":"pub","slot":"rawtree_slot"}`

func basePlan() ConnectorModel {
	return ConnectorModel{
		ID:      types.StringUnknown(),
		Cluster: types.StringUnknown(),
		Name:    types.StringValue("orders"),
		Type:    types.StringValue("postgres"),
		Destinations: []DestinationModel{{
			ID:             types.StringUnknown(),
			Database:       types.StringValue("shop"),
			Table:          types.StringValue("orders"),
			PostgresTables: []types.String{types.StringValue("public.orders")},
			GroupID:        types.StringUnknown(),
			InitialLoad:    types.BoolUnknown(),
		}},
		SettingsJSON:          types.StringValue(configuredSettings),
		Status:                types.StringUnknown(),
		CredentialsConfigured: types.BoolUnknown(),
		SlotOwned:             types.BoolUnknown(),
		Revision:              types.Int64Unknown(),
		CreatedAt:             types.StringUnknown(),
		UpdatedAt:             types.StringUnknown(),
	}
}

func TestCreateMergesCredentialsAndKeepsThemOutOfState(t *testing.T) {
	calls, srv := newFakeAPI(t, http.StatusOK)
	r := newTestResource(srv.URL)
	plan, config := planAndConfig(t, basePlan(), `{"password":"s3cret"}`)

	resp := fwresource.CreateResponse{State: emptyState()}
	r.Create(context.Background(), fwresource.CreateRequest{Plan: plan, Config: config}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if len(*calls) != 1 || (*calls)[0].key != "POST /v1/connectors" || (*calls)[0].query != "org-1/c-1?" {
		t.Fatalf("unexpected calls %+v", *calls)
	}
	body := (*calls)[0].body
	settings := body["settings"].(map[string]any)
	if settings["password"] != "s3cret" || settings["host"] != "db.example.com" {
		t.Errorf("credentials not merged into settings: %v", settings)
	}
	dest := body["destinations"].([]any)[0].(map[string]any)
	if _, ok := dest["id"]; ok {
		t.Errorf("new destination should not send an id: %v", dest)
	}
	if dest["postgres_tables"].([]any)[0] != "public.orders" {
		t.Errorf("unexpected destination %v", dest)
	}

	m := stateModel(t, resp.State)
	if !m.CredentialsJSON.IsNull() {
		t.Error("credentials must not be stored in state")
	}
	if m.SettingsJSON.ValueString() != configuredSettings {
		t.Errorf("settings_json should keep the configured value, got %s", m.SettingsJSON.ValueString())
	}
	if m.ID.ValueString() != "conn-1" || m.Cluster.ValueString() != "main" || m.Status.ValueString() != "active" ||
		m.Destinations[0].ID.ValueString() != "dest-1" || !m.Destinations[0].InitialLoad.ValueBool() || !m.SlotOwned.ValueBool() {
		t.Errorf("unexpected state %+v", m)
	}
}

func TestCreatePausedPatchesStatus(t *testing.T) {
	calls, srv := newFakeAPI(t, http.StatusOK)
	r := newTestResource(srv.URL)
	p := basePlan()
	p.Status = types.StringValue("paused")
	plan, config := planAndConfig(t, p, `{"password":"x"}`)

	resp := fwresource.CreateResponse{State: emptyState()}
	r.Create(context.Background(), fwresource.CreateRequest{Plan: plan, Config: config}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	if len(*calls) != 2 || (*calls)[1].key != "PATCH /v1/connectors/conn-1" || (*calls)[1].body["status"] != "paused" {
		t.Fatalf("unexpected calls %+v", *calls)
	}
	if m := stateModel(t, resp.State); m.Status.ValueString() != "paused" {
		t.Errorf("unexpected status %s", m.Status.ValueString())
	}
}

func savedState() ConnectorModel {
	m := basePlan()
	m.ID = types.StringValue("conn-1")
	m.Cluster = types.StringValue("main")
	m.Destinations[0].ID = types.StringValue("dest-1")
	m.Destinations[0].GroupID = types.StringNull()
	m.Destinations[0].InitialLoad = types.BoolValue(true)
	m.Status = types.StringValue("active")
	m.CredentialsConfigured = types.BoolValue(true)
	m.SlotOwned = types.BoolValue(true)
	m.Revision = types.Int64Value(1)
	m.CreatedAt = types.StringValue("2026-11-24T10:00:00Z")
	m.UpdatedAt = types.StringValue("2026-11-24T10:00:00Z")
	return m
}

func TestUpdateRenameOnlySendsName(t *testing.T) {
	calls, srv := newFakeAPI(t, http.StatusOK)
	r := newTestResource(srv.URL)
	p := savedState()
	p.Name = types.StringValue("orders-v2")
	p.Destinations[0].ID = types.StringUnknown()
	p.Destinations[0].InitialLoad = types.BoolUnknown()
	plan, config := planAndConfig(t, p, `{"password":"x"}`)

	resp := fwresource.UpdateResponse{State: emptyState()}
	r.Update(context.Background(), fwresource.UpdateRequest{Plan: plan, Config: config, State: stateFrom(t, savedState())}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if len(*calls) != 1 || len((*calls)[0].body) != 1 || (*calls)[0].body["name"] != "orders-v2" {
		t.Fatalf("unexpected calls %+v", *calls)
	}
}

func TestUpdateSettingsResendsCredentialsAndKeepsDestinationIDs(t *testing.T) {
	calls, srv := newFakeAPI(t, http.StatusOK)
	r := newTestResource(srv.URL)
	p := savedState()
	p.SettingsJSON = types.StringValue(`{"host":"db2.example.com","database":"shop","user":"rawtree","publication":"pub","slot":"rawtree_slot"}`)
	p.Destinations = append(p.Destinations, DestinationModel{
		ID:             types.StringUnknown(),
		Database:       types.StringValue("shop"),
		Table:          types.StringValue("customers"),
		PostgresTables: []types.String{types.StringValue("public.customers")},
		GroupID:        types.StringUnknown(),
		InitialLoad:    types.BoolUnknown(),
	})
	p.Destinations[0].ID = types.StringUnknown()
	plan, config := planAndConfig(t, p, `{"password":"new"}`)

	resp := fwresource.UpdateResponse{State: emptyState()}
	r.Update(context.Background(), fwresource.UpdateRequest{Plan: plan, Config: config, State: stateFrom(t, savedState())}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	body := (*calls)[0].body
	settings := body["settings"].(map[string]any)
	if settings["password"] != "new" || settings["host"] != "db2.example.com" {
		t.Errorf("unexpected settings %v", settings)
	}
	dests := body["destinations"].([]any)
	first, second := dests[0].(map[string]any), dests[1].(map[string]any)
	if first["id"] != "dest-1" || first["initial_load"] != true {
		t.Errorf("existing destination should keep its id: %v", first)
	}
	if _, ok := second["id"]; ok {
		t.Errorf("new destination should not send an id: %v", second)
	}
	if _, ok := second["initial_load"]; ok {
		t.Errorf("a destination added later cannot ask for initial_load: %v", second)
	}
}

func TestReadDetectsSettingsDriftAndRemovesMissing(t *testing.T) {
	_, srv := newFakeAPI(t, http.StatusOK)
	r := newTestResource(srv.URL)
	saved := savedState()
	saved.SettingsJSON = types.StringValue(`{"host":"old.example.com"}`)
	resp := fwresource.ReadResponse{State: stateFrom(t, saved)}
	r.Read(context.Background(), fwresource.ReadRequest{State: stateFrom(t, saved)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	if got := stateModel(t, resp.State).SettingsJSON.ValueString(); !strings.Contains(got, `"host":"db.example.com"`) {
		t.Errorf("drift not detected, settings_json = %s", got)
	}

	_, srv404 := newFakeAPI(t, http.StatusNotFound)
	r = newTestResource(srv404.URL)
	resp = fwresource.ReadResponse{State: stateFrom(t, savedState())}
	r.Read(context.Background(), fwresource.ReadRequest{State: stateFrom(t, savedState())}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Errorf("expected removal, diags %v", resp.Diagnostics)
	}
}

func TestImportStateParsesClusterAndID(t *testing.T) {
	r := newTestResource("http://unused")
	resp := fwresource.ImportStateResponse{State: emptyState()}
	r.ImportState(context.Background(), fwresource.ImportStateRequest{ID: "main/conn-1"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import: %v", resp.Diagnostics)
	}
	var cluster, id types.String
	resp.State.GetAttribute(context.Background(), path.Root("cluster"), &cluster)
	resp.State.GetAttribute(context.Background(), path.Root("id"), &id)
	if cluster.ValueString() != "main" || id.ValueString() != "conn-1" {
		t.Errorf("got cluster %q id %q", cluster.ValueString(), id.ValueString())
	}

	resp = fwresource.ImportStateResponse{State: emptyState()}
	r.ImportState(context.Background(), fwresource.ImportStateRequest{ID: "conn-1"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("expected an error for an ID without a cluster")
	}
}

func TestSubsetIgnoresAPIDefaults(t *testing.T) {
	var local, remote any
	_ = json.Unmarshal([]byte(`{"a":1,"b":{"c":true}}`), &local)
	_ = json.Unmarshal([]byte(`{"b":{"c":true,"d":2},"a":1,"port":5432}`), &remote)
	if !subset(local, remote) {
		t.Error("expected subset")
	}
	_ = json.Unmarshal([]byte(`{"a":2,"b":{"c":true}}`), &remote)
	if subset(local, remote) {
		t.Error("expected a difference")
	}
}

func TestSchema(t *testing.T) {
	s := resourceSchema()
	if d := s.ValidateImplementation(context.Background()); d.HasError() {
		t.Fatalf("invalid schema: %v", d)
	}
	creds := s.Attributes["credentials_json_wo"]
	if !creds.IsWriteOnly() || !creds.IsSensitive() {
		t.Error("credentials_json_wo should be write-only and sensitive")
	}
	for _, a := range []string{"cluster", "status"} {
		if !s.Attributes[a].IsOptional() || !s.Attributes[a].IsComputed() {
			t.Errorf("%s should be optional and computed", a)
		}
	}
}

func TestRetargetedDestinationKeepsItsID(t *testing.T) {
	dest := func(id, table string) DestinationModel {
		return DestinationModel{ID: types.StringValue(id), Database: types.StringValue("shop"), Table: types.StringValue(table)}
	}
	saved := []DestinationModel{dest("d1", "orders"), dest("d2", "users")}
	// A renamed table keeps the ID at its position; the other matches by target.
	got := requestDestinations([]DestinationModel{dest("", "orders_v2"), dest("", "users")}, saved, false)
	if got[0].ID != "d1" || got[1].ID != "d2" {
		t.Errorf("unexpected IDs %q %q", got[0].ID, got[1].ID)
	}
	// A new destination after a removed one does not take the ID of another target.
	got = requestDestinations([]DestinationModel{dest("", "users"), dest("", "events")}, saved, false)
	if got[0].ID != "d2" || got[1].ID != "" {
		t.Errorf("unexpected IDs %q %q", got[0].ID, got[1].ID)
	}
}

func TestSamePostgresTables(t *testing.T) {
	conf := []types.String{types.StringValue("Orders"), types.StringValue("Sales.Items")}
	if !samePostgresTables(conf, []string{"public.orders", "sales.items"}) || samePostgresTables(conf, []string{"public.orders", "sales.other"}) {
		t.Error("the API form of a configured list must match, and a different table must not")
	}
}

func TestSubsetToleratesWhatTheAPIRewrites(t *testing.T) {
	parse := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	// The API strips sasl.username, lowercases host and forces slot creation.
	local := parse(`{"host":"DB.Example.com","create_slot_if_missing":false,"sasl":{"mechanism":"PLAIN","username":"app"}}`)
	remote := parse(`{"host":"db.example.com","create_slot_if_missing":true,"sasl":{"mechanism":"PLAIN"},"port":5432}`)
	if !subset(local, remote) {
		t.Error("values that the API rewrites must not count as drift")
	}
	if subset(parse(`{"sasl":{"mechanism":"PLAIN"}}`), parse(`{"sasl":{"mechanism":"SCRAM-SHA-256"}}`)) {
		t.Error("a real difference must count as drift")
	}
}
