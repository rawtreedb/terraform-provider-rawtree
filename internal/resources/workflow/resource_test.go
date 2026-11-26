package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

type call struct {
	Method string
	Path   string
	Body   map[string]any
}

// fakeAPI records requests and answers each with the next canned response.
type fakeAPI struct {
	t         *testing.T
	calls     []call
	responses []fakeResponse
}

type fakeResponse struct {
	status int
	body   string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer rt_test" {
		f.t.Errorf("Authorization = %q", got)
	}
	c := call{Method: r.Method, Path: r.URL.Path}
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		if err := json.Unmarshal(data, &c.Body); err != nil {
			f.t.Fatalf("request body: %v", err)
		}
	}
	f.calls = append(f.calls, c)
	if len(f.responses) == 0 {
		f.t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	w.WriteHeader(resp.status)
	_, _ = io.WriteString(w, resp.body)
}

func newTest(t *testing.T, responses ...fakeResponse) (*WorkflowResource, *fakeAPI) {
	f := &fakeAPI{t: t, responses: responses}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &WorkflowResource{client: client.New("rt_test", srv.URL, "", "analytics", "")}, f
}

func emptyState() tfsdk.State {
	s := resourceSchema()
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func toPlan(t *testing.T, m workflowModel) tfsdk.Plan {
	t.Helper()
	s := resourceSchema()
	p := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	if d := p.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	return p
}

func toState(t *testing.T, m workflowModel) tfsdk.State {
	t.Helper()
	st := emptyState()
	if d := st.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return st
}

func fromState(t *testing.T, st tfsdk.State) workflowModel {
	t.Helper()
	var m workflowModel
	if d := st.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("get state: %v", d)
	}
	return m
}

func headers(kv map[string]string) types.Map {
	elems := map[string]attr.Value{}
	for k, v := range kv {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

// plannedModel is a config as Terraform plans it on create: computed values unknown.
func plannedModel() workflowModel {
	return workflowModel{
		ID:          types.StringUnknown(),
		Name:        types.StringValue("Alerts"),
		Description: types.StringNull(),
		Database:    types.StringUnknown(),
		SQL:         types.StringValue("SELECT 1 AS value"),
		Schedule:    &scheduleModel{Cron: types.StringValue("*/5  * * * *"), Enabled: types.BoolValue(true)},
		Sinks: []sinkModel{
			{ID: types.StringUnknown(), Table: &tableSinkModel{Database: types.StringValue("analytics"), Table: types.StringValue("alerts")}},
			{ID: types.StringUnknown(), HTTP: &httpSinkModel{URL: types.StringValue("https://Example.com:443/hook"), Headers: headers(map[string]string{"Authorization": "Bearer secret"})}},
			{ID: types.StringUnknown(), Slack: &slackSinkModel{IntegrationID: types.StringValue("int-1"), ChannelID: types.StringUnknown(), ChannelName: types.StringValue("#alerts")}},
		},
	}
}

// The API lowercases the name, collapses cron whitespace, normalises the sink
// URL, resolves channel names and hides header values.
const createdItem = `{
  "id": "wf1", "name": "alerts", "description": null, "revision": 1,
  "created_at": "2026-11-24T00:00:00Z", "updated_at": "2026-11-24T00:00:00Z",
  "query": {"database": "analytics", "sql": "SELECT 1 AS value"},
  "schedule": {"cron": "*/5 * * * *", "enabled": true, "next_run_at": "2026-11-24T00:05:00Z"},
  "sinks": [
    {"id": "s3", "type": "slack", "settings": {"integration_id": "int-1", "channel_id": "C1", "channel_name": "alerts"}},
    {"id": "s1", "type": "table", "settings": {"database": "analytics", "table": "alerts"}},
    {"id": "s2", "type": "http", "settings": {"url": "https://example.com/hook", "url_configured": true, "header_names": ["authorization"]}}
  ]
}`

func TestResourceSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := resourceSchema()
	if d := s.ValidateImplementation(ctx); d.HasError() {
		t.Fatalf("schema: %v", d)
	}
	for _, name := range []string{"name", "sql"} {
		if !s.Attributes[name].IsRequired() {
			t.Errorf("%s should be required", name)
		}
	}
	for _, name := range []string{"description", "database", "schedule", "sinks"} {
		if !s.Attributes[name].IsOptional() {
			t.Errorf("%s should be optional", name)
		}
	}
	for _, name := range []string{"id", "database"} {
		if !s.Attributes[name].IsComputed() {
			t.Errorf("%s should be computed", name)
		}
	}
	for _, p := range []path.Path{
		path.Root("sinks").AtListIndex(0).AtName("http").AtName("headers"),
		path.Root("sinks").AtListIndex(0).AtName("http").AtName("url"),
	} {
		a, d := s.AttributeAtPath(ctx, p)
		if d.HasError() {
			t.Fatalf("%s: %v", p, d)
		}
		if !a.IsSensitive() {
			t.Errorf("%s should be sensitive", p)
		}
	}
}

func TestCreate(t *testing.T) {
	r, f := newTest(t, fakeResponse{201, createdItem})
	ctx := context.Background()
	resp := resource.CreateResponse{State: emptyState()}
	r.Create(ctx, resource.CreateRequest{Plan: toPlan(t, plannedModel())}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}

	if len(f.calls) != 1 || f.calls[0].Method != "POST" || f.calls[0].Path != "/v1/workflows" {
		t.Fatalf("calls = %+v", f.calls)
	}
	want := map[string]any{
		"name":        "Alerts",
		"description": nil,
		"query":       map[string]any{"database": "analytics", "sql": "SELECT 1 AS value"},
		"schedule":    map[string]any{"cron": "*/5  * * * *", "enabled": true},
		"sinks": []any{
			map[string]any{"type": "table", "settings": map[string]any{"database": "analytics", "table": "alerts"}},
			map[string]any{"type": "http", "settings": map[string]any{"url": "https://Example.com:443/hook", "headers": map[string]any{"Authorization": "Bearer secret"}}},
			map[string]any{"type": "slack", "settings": map[string]any{"integration_id": "int-1", "channel_name": "#alerts"}},
		},
	}
	if !reflect.DeepEqual(f.calls[0].Body, want) {
		t.Errorf("body = %#v", f.calls[0].Body)
	}

	m := fromState(t, resp.State)
	if m.ID.ValueString() != "wf1" || m.Database.ValueString() != "analytics" {
		t.Errorf("id/database = %s/%s", m.ID, m.Database)
	}
	// The state keeps the configured spelling of the values that the API normalises.
	if m.Name.ValueString() != "Alerts" || m.Schedule.Cron.ValueString() != "*/5  * * * *" || m.Sinks[1].HTTP.URL.ValueString() != "https://Example.com:443/hook" {
		t.Errorf("name/cron/url = %s/%s/%s", m.Name, m.Schedule.Cron, m.Sinks[1].HTTP.URL)
	}
	if len(m.Sinks) != 3 {
		t.Fatalf("sinks = %+v", m.Sinks)
	}
	for i, id := range []string{"s1", "s2", "s3"} {
		if m.Sinks[i].ID.ValueString() != id {
			t.Errorf("sink %d id = %s, want %s", i, m.Sinks[i].ID, id)
		}
	}
	if !m.Sinks[1].HTTP.Headers.Equal(headers(map[string]string{"Authorization": "Bearer secret"})) {
		t.Errorf("headers = %s", m.Sinks[1].HTTP.Headers)
	}
	if sl := m.Sinks[2].Slack; sl.ChannelName.ValueString() != "#alerts" || sl.ChannelID.ValueString() != "C1" {
		t.Errorf("slack = %+v", sl)
	}
}

func createdState(t *testing.T) workflowModel {
	m, err := decode(createdItem)
	if err != nil {
		t.Fatal(err)
	}
	return toModel(m, plannedModel(), nil, true)
}

func decode(s string) (apiWorkflow, error) {
	var w apiWorkflow
	return w, json.Unmarshal([]byte(s), &w)
}

// The API can store the workflow and then fail the call. The provider must
// keep that workflow in state, or the retry fails on the name.
func TestCreateKeepsWorkflowWhenScheduleIsNotConfirmed(t *testing.T) {
	r, f := newTest(t,
		fakeResponse{503, `{"error":"service_unavailable","message":"Could not confirm workflow creation.","hint":"Inspect workflow wf1."}`},
		fakeResponse{200, `{"workflows":[` + createdItem + `]}`})
	resp := resource.CreateResponse{State: emptyState()}
	r.Create(context.Background(), resource.CreateRequest{Plan: toPlan(t, plannedModel())}, &resp)
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("want one warning and no error: %v", resp.Diagnostics)
	}
	if len(f.calls) != 2 || f.calls[1].Method != "GET" || fromState(t, resp.State).ID.ValueString() != "wf1" {
		t.Errorf("calls = %+v, id = %s", f.calls, fromState(t, resp.State).ID)
	}
}

func TestUpdate(t *testing.T) {
	prior := createdState(t)
	plan := createdState(t)
	plan.Description = types.StringValue("pages oncall")
	plan.Schedule = nil
	plan.Sinks[0].Table.Table = types.StringValue("alerts_v2")
	plan.Sinks[1].HTTP.Headers = types.MapNull(types.StringType)
	plan.Sinks[2].Slack.ChannelName = types.StringUnknown()
	plan.Sinks[2].Slack.ChannelID = types.StringValue("C1")

	r, f := newTest(t, fakeResponse{200, `{
	  "id": "wf1", "name": "alerts", "description": "pages oncall", "revision": 2,
	  "created_at": "x", "updated_at": "y",
	  "query": {"database": "analytics", "sql": "SELECT 1 AS value"},
	  "schedule": null,
	  "sinks": [
	    {"id": "s1", "type": "table", "settings": {"database": "analytics", "table": "alerts_v2"}},
	    {"id": "s2", "type": "http", "settings": {"url": "https://example.com/hook", "url_configured": true, "header_names": []}},
	    {"id": "s3", "type": "slack", "settings": {"integration_id": "int-1", "channel_id": "C1", "channel_name": "alerts"}}
	  ]}`})
	ctx := context.Background()
	resp := resource.UpdateResponse{State: emptyState()}
	r.Update(ctx, resource.UpdateRequest{Plan: toPlan(t, plan), State: toState(t, prior)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if len(f.calls) != 1 || f.calls[0].Method != "PATCH" || f.calls[0].Path != "/v1/workflows/wf1" {
		t.Fatalf("calls = %+v", f.calls)
	}
	body := f.calls[0].Body
	if v, ok := body["schedule"]; !ok || v != nil {
		t.Errorf("schedule = %#v, want explicit null", v)
	}
	if body["description"] != "pages oncall" {
		t.Errorf("description = %#v", body["description"])
	}
	wantSinks := []any{
		map[string]any{"id": "s1", "type": "table", "settings": map[string]any{"database": "analytics", "table": "alerts_v2"}},
		map[string]any{"id": "s2", "type": "http", "settings": map[string]any{"url": "https://Example.com:443/hook", "headers": map[string]any{}}},
		map[string]any{"id": "s3", "type": "slack", "settings": map[string]any{"integration_id": "int-1", "channel_id": "C1"}},
	}
	if !reflect.DeepEqual(body["sinks"], wantSinks) {
		t.Errorf("sinks = %#v", body["sinks"])
	}
	m := fromState(t, resp.State)
	if m.Schedule != nil || m.Description.ValueString() != "pages oncall" {
		t.Errorf("state = %+v", m)
	}
	if !m.Sinks[1].HTTP.Headers.IsNull() {
		t.Errorf("headers = %s", m.Sinks[1].HTTP.Headers)
	}
}

func TestReadDetectsHeaderDriftAndRemoval(t *testing.T) {
	drifted := `{
	  "id": "wf1", "name": "alerts", "revision": 3, "created_at": "x", "updated_at": "y",
	  "query": {"database": "analytics", "sql": "SELECT 1 AS value"},
	  "schedule": {"cron": "*/5 * * * *", "enabled": false},
	  "sinks": [
	    {"id": "s2", "type": "http", "settings": {"url": "https://example.com/hook", "url_configured": true, "header_names": ["X-Region"]}},
	    {"id": "s1", "type": "table", "settings": {"database": "analytics", "table": "alerts"}}
	  ]}`
	r, f := newTest(t, fakeResponse{200, drifted}, fakeResponse{404, `{"error":"not_found","message":"Workflow not found"}`})
	ctx := context.Background()

	resp := resource.ReadResponse{State: toState(t, createdState(t))}
	r.Read(ctx, resource.ReadRequest{State: toState(t, createdState(t))}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	m := fromState(t, resp.State)
	if m.Schedule.Enabled.ValueBool() || m.Schedule.Cron.ValueString() != "*/5  * * * *" {
		t.Errorf("schedule = %+v", m.Schedule)
	}
	if len(m.Sinks) != 2 || m.Sinks[0].ID.ValueString() != "s1" || m.Sinks[1].ID.ValueString() != "s2" {
		t.Fatalf("sinks = %+v", m.Sinks)
	}
	if !m.Sinks[1].HTTP.Headers.Equal(headers(map[string]string{"X-Region": ""})) {
		t.Errorf("headers = %s", m.Sinks[1].HTTP.Headers)
	}

	resp = resource.ReadResponse{State: toState(t, createdState(t))}
	r.Read(ctx, resource.ReadRequest{State: toState(t, createdState(t))}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Errorf("404 should remove the resource: %v", resp.Diagnostics)
	}
	if f.calls[0].Path != "/v1/workflows/wf1" || f.calls[0].Method != "GET" {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestDelete(t *testing.T) {
	r, f := newTest(t, fakeResponse{204, ""}, fakeResponse{404, `{"error":"not_found"}`})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		resp := resource.DeleteResponse{State: toState(t, createdState(t))}
		r.Delete(ctx, resource.DeleteRequest{State: toState(t, createdState(t))}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("delete %d: %v", i, resp.Diagnostics)
		}
	}
	if f.calls[0].Method != "DELETE" || f.calls[0].Path != "/v1/workflows/wf1" {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestSinkIDModifier(t *testing.T) {
	ctx := context.Background()
	prior := createdState(t)
	state := toState(t, prior)

	unknownIDs := func(m workflowModel) workflowModel {
		for i := range m.Sinks {
			m.Sinks[i].ID = types.StringUnknown()
		}
		return m
	}
	// Same kinds at each index: the table sink keeps its ID.
	same := unknownIDs(createdState(t))
	same.Sinks[0].Table.Table = types.StringValue("alerts_v2")
	// A new sink inserted at index 0 shifts the kinds: no ID is reused.
	shifted := unknownIDs(createdState(t))
	shifted.Sinks = append([]sinkModel{shifted.Sinks[1]}, shifted.Sinks...)

	for _, tc := range []struct {
		name string
		plan workflowModel
		want types.String
	}{
		{"same kind", same, types.StringValue("s1")},
		{"kind changed", shifted, types.StringUnknown()},
	} {
		p := path.Root("sinks").AtListIndex(0).AtName("id")
		req := planmodifier.StringRequest{
			Path:       p,
			Plan:       toPlan(t, tc.plan),
			State:      state,
			PlanValue:  types.StringUnknown(),
			StateValue: types.StringValue("s1"),
		}
		resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
		sinkIDModifier{}.PlanModifyString(ctx, req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: %v", tc.name, resp.Diagnostics)
		}
		if !resp.PlanValue.Equal(tc.want) {
			t.Errorf("%s: id = %s, want %s", tc.name, resp.PlanValue, tc.want)
		}
	}
}

func TestRewrittenSinkURLIsNotDrift(t *testing.T) {
	// The API percent-encodes this URL in a way that normalizeURL does not mirror.
	configured, stored := "https://example.com/hook?q=a b", "https://example.com/hook?q=a%20b"
	prior := workflowModel{Sinks: []sinkModel{{ID: types.StringUnknown(), HTTP: &httpSinkModel{URL: types.StringValue(configured), Headers: types.MapNull(types.StringType)}}}}
	w := apiWorkflow{ID: "wf1", Sinks: []apiSink{{ID: "s1", Type: "http"}}}
	w.Sinks[0].Settings.URL = &stored
	url := func(m workflowModel) string { return m.Sinks[0].HTTP.URL.ValueString() }

	// After an apply the sink matches by position and keeps the configured URL.
	applied := toModel(w, prior, nil, true)
	if len(applied.Sinks) != 1 || url(applied) != configured {
		t.Fatalf("apply: %+v", applied.Sinks)
	}
	// A read keeps it while the API value is the one recorded at the last apply.
	if got := toModel(w, applied, map[string]string{"s1": stored}, false); url(got) != configured {
		t.Errorf("read without change: %s", url(got))
	}
	// A change made outside Terraform shows as drift.
	changed := "https://example.com/other"
	w.Sinks[0].Settings.URL = &changed
	if got := toModel(w, applied, map[string]string{"s1": stored}, false); url(got) != changed {
		t.Errorf("read after an outside change: %s", url(got))
	}
}
