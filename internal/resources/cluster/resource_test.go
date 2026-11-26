package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

const testRegionID = "6f1c1f7e-0000-4000-8000-000000000001"

type fakeAPI struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]map[string]any
	headers  map[string]string
	polls    int
	// snapshotLag makes the first status poll return the "snapshot not found" 404.
	snapshotLag bool
	deleted     bool
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{bodies: map[string]map[string]any{}, headers: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		key := r.Method + " " + r.URL.Path
		f.requests = append(f.requests, key+"?"+r.URL.RawQuery)
		f.headers[key] = r.Header.Get("X-RawTree-Deployment-Id")
		if r.Body != nil {
			var b map[string]any
			if json.NewDecoder(r.Body).Decode(&b) == nil {
				f.bodies[key] = b
			}
		}
		item := map[string]any{
			"id": "c-1", "name": "analytics", "created_at": "2026-11-24T10:00:00Z",
			"deployment_id": "abcd1234", "cloud_region": "eu-west-1", "rawtree_version": "1.2.3",
			"status":               map[string]any{"phase": "provisioning", "ready": false, "label": "Provisioning"},
			"can_pause":            false,
			"can_resume":           false,
			"idle_timeout_minutes": 60,
			"s3_storage_access":    map[string]any{"external_id": "ext-1", "resource_tag": "tag-1"},
			"resources": map[string]any{
				"shards": 1, "replicas": 2,
				"minimum_size": map[string]any{"cpu_cores": 2, "memory_bytes": 8 << 30},
				"maximum_size": map[string]any{"cpu_cores": 4, "memory_bytes": 16 << 30},
			},
		}
		switch key {
		case "GET /v1/organizations":
			_ = json.NewEncoder(w).Encode(map[string]any{"organizations": []any{map[string]any{"organization_id": "org-1", "name": "acme"}}})
		case "GET /v1/regions":
			_ = json.NewEncoder(w).Encode(map[string]any{"regions": []any{map[string]any{
				"id": testRegionID, "name": "Ireland", "cloud_region": "eu-west-1", "deployment_id": "abcd1234", "enabled": true,
			}}})
		case "GET /v1/clusters":
			_ = json.NewEncoder(w).Encode(map[string]any{"clusters": []any{item}})
		case "POST /v1/clusters":
			_ = json.NewEncoder(w).Encode(item)
		case "GET /v1/clusters/c-1":
			if f.deleted {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not_found","message":"Cluster not found."}`))
				return
			}
			f.polls++
			if f.snapshotLag && f.polls == 1 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not_found","message":"Cluster status snapshot not found."}`))
				return
			}
			if f.polls > 1 {
				item["status"] = map[string]any{"phase": "ready", "ready": true, "label": "Ready"}
			}
			_ = json.NewEncoder(w).Encode(item)
		case "PATCH /v1/clusters/c-1":
			item["name"] = "renamed"
			item["idle_timeout_minutes"] = 0
			item["status"] = map[string]any{"phase": "ready", "ready": true, "label": "Ready"}
			_ = json.NewEncoder(w).Encode(item)
		case "DELETE /v1/clusters/c-1":
			f.deleted = true
			_, _ = w.Write([]byte(`{"deleted":true}`))
		default:
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func newTestResource(apiURL string) *ClusterResource {
	return &ClusterResource{client: client.New("", "jwt", apiURL, "acme", "", "", "")}
}

func emptyState() tfsdk.State {
	s := resourceSchema()
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func planFrom(t *testing.T, m ClusterModel) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: resourceSchema()}
	if d := p.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	return p
}

func stateModel(t *testing.T, s tfsdk.State) ClusterModel {
	t.Helper()
	var m ClusterModel
	if d := s.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return m
}

func basePlan() ClusterModel {
	return ClusterModel{
		ID:                 types.StringUnknown(),
		Name:               types.StringValue("analytics"),
		RegionID:           types.StringValue(testRegionID),
		Replicas:           types.Int64Value(2),
		MinSize:            types.StringValue("2:8"),
		MaxSize:            types.StringValue("4:16"),
		IdleTimeoutMinutes: types.Int64Value(60),
		ExternalID:         types.StringUnknown(),
		ResourceTag:        types.StringUnknown(),
		Status:             types.StringUnknown(),
		DeploymentID:       types.StringUnknown(),
		CloudRegion:        types.StringUnknown(),
		RawtreeVersion:     types.StringUnknown(),
		CreatedAt:          types.StringUnknown(),
	}
}

func TestCreateWaitsUntilReady(t *testing.T) {
	pollInterval = time.Millisecond
	f, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	ctx := context.Background()

	resp := fwresource.CreateResponse{State: emptyState()}
	r.Create(ctx, fwresource.CreateRequest{Plan: planFrom(t, basePlan())}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}

	body := f.bodies["POST /v1/clusters"]
	if body["region_id"] != testRegionID || body["name"] != "analytics" || body["replicas"].(float64) != 2 {
		t.Errorf("unexpected create body %v", body)
	}
	if size := body["size"].(map[string]any); size["cpu_cores"].(float64) != 2 || size["memory_gib"].(float64) != 8 {
		t.Errorf("unexpected size %v", size)
	}
	if max := body["autoscaling"].(map[string]any)["max_size"].(map[string]any); max["cpu_cores"].(float64) != 4 {
		t.Errorf("unexpected autoscaling %v", body["autoscaling"])
	}
	if _, ok := body["s3_storage"]; ok {
		t.Errorf("s3_storage should be omitted: %v", body)
	}
	if f.headers["POST /v1/clusters"] != "abcd1234" {
		t.Errorf("create not routed to the region deployment: %q", f.headers["POST /v1/clusters"])
	}
	if f.polls != 2 {
		t.Errorf("expected 2 status polls, got %d", f.polls)
	}

	m := stateModel(t, resp.State)
	if m.ID.ValueString() != "c-1" || m.Status.ValueString() != "ready" || m.DeploymentID.ValueString() != "abcd1234" ||
		m.CloudRegion.ValueString() != "eu-west-1" || m.ExternalID.ValueString() != "ext-1" || m.RawtreeVersion.ValueString() != "1.2.3" {
		t.Errorf("unexpected state %+v", m)
	}
}

func TestCreateFailsOnErrorPhase(t *testing.T) {
	pollInterval = time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/organizations":
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"org-1","name":"acme"}]}`))
		case "/v1/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"` + testRegionID + `","name":"Ireland","cloud_region":"eu-west-1","deployment_id":"abcd1234","enabled":true}]}`))
		default:
			_, _ = w.Write([]byte(`{"id":"c-1","name":"analytics","created_at":"x","status":{"phase":"error","ready":false,"label":"Error","message":"quota"},"idle_timeout_minutes":60}`))
		}
	}))
	defer srv.Close()
	r := newTestResource(srv.URL)
	resp := fwresource.CreateResponse{State: emptyState()}
	r.Create(context.Background(), fwresource.CreateRequest{Plan: planFrom(t, basePlan())}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error")
	}
	// The cluster ID is kept so Terraform taints the resource instead of orphaning it.
	if m := stateModel(t, resp.State); m.ID.ValueString() != "c-1" {
		t.Errorf("expected ID in state, got %+v", m)
	}
}

func createdState() ClusterModel {
	m := basePlan()
	m.DeletionProtection = types.BoolValue(false)
	m.ID = types.StringValue("c-1")
	m.ExternalID = types.StringValue("ext-1")
	m.ResourceTag = types.StringValue("tag-1")
	m.Status = types.StringValue("ready")
	m.DeploymentID = types.StringValue("abcd1234")
	m.CloudRegion = types.StringValue("eu-west-1")
	m.RawtreeVersion = types.StringValue("1.2.3")
	m.CreatedAt = types.StringValue("2026-11-24T10:00:00Z")
	return m
}

func stateFrom(t *testing.T, m ClusterModel) tfsdk.State {
	t.Helper()
	s := emptyState()
	if d := s.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return s
}

func TestUpdateSendsOnlyChangedFields(t *testing.T) {
	f, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	plan := createdState()
	plan.Name = types.StringValue("renamed")
	plan.IdleTimeoutMinutes = types.Int64Value(0)
	plan.Status = types.StringUnknown()

	resp := fwresource.UpdateResponse{State: emptyState()}
	r.Update(context.Background(), fwresource.UpdateRequest{Plan: planFrom(t, plan), State: stateFrom(t, createdState())}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	body := f.bodies["PATCH /v1/clusters/c-1"]
	if len(body) != 2 || body["name"] != "renamed" || body["idle_timeout_minutes"].(float64) != 0 {
		t.Errorf("unexpected patch body %v", body)
	}
	if f.requests[1] != "PATCH /v1/clusters/c-1?" {
		t.Errorf("unexpected requests %v", f.requests)
	}
	if m := stateModel(t, resp.State); m.Name.ValueString() != "renamed" || m.IdleTimeoutMinutes.ValueInt64() != 0 {
		t.Errorf("unexpected state %+v", m)
	}
}

func TestDeleteThenReadRemoves(t *testing.T) {
	f, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	ctx := context.Background()

	dresp := fwresource.DeleteResponse{State: stateFrom(t, createdState())}
	r.Delete(ctx, fwresource.DeleteRequest{State: stateFrom(t, createdState())}, &dresp)
	if dresp.Diagnostics.HasError() {
		t.Fatalf("delete: %v", dresp.Diagnostics)
	}
	if f.requests[1] != "DELETE /v1/clusters/c-1?" {
		t.Errorf("unexpected requests %v", f.requests)
	}

	rresp := fwresource.ReadResponse{State: stateFrom(t, createdState())}
	r.Read(ctx, fwresource.ReadRequest{State: stateFrom(t, createdState())}, &rresp)
	if rresp.Diagnostics.HasError() {
		t.Fatalf("read: %v", rresp.Diagnostics)
	}
	if !rresp.State.Raw.IsNull() {
		t.Error("expected the resource to be removed from state")
	}
}

func TestImportFillsRegionAndSizes(t *testing.T) {
	_, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	ctx := context.Background()

	iresp := fwresource.ImportStateResponse{State: emptyState()}
	r.ImportState(ctx, fwresource.ImportStateRequest{ID: "c-1"}, &iresp)
	if iresp.Diagnostics.HasError() {
		t.Fatalf("import: %v", iresp.Diagnostics)
	}
	rresp := fwresource.ReadResponse{State: iresp.State}
	r.Read(ctx, fwresource.ReadRequest{State: iresp.State}, &rresp)
	if rresp.Diagnostics.HasError() {
		t.Fatalf("read: %v", rresp.Diagnostics)
	}
	m := stateModel(t, rresp.State)
	if m.ID.ValueString() != "c-1" || m.RegionID.ValueString() != testRegionID || m.Replicas.ValueInt64() != 2 ||
		m.MinSize.ValueString() != "2:8" || m.MaxSize.ValueString() != "4:16" {
		t.Errorf("unexpected imported state %+v", m)
	}
}

func TestSchema(t *testing.T) {
	s := resourceSchema()
	if d := s.ValidateImplementation(context.Background()); d.HasError() {
		t.Fatalf("invalid schema: %v", d)
	}
	for _, a := range []string{"name", "region_id", "replicas", "min_size"} {
		if !s.Attributes[a].IsRequired() {
			t.Errorf("%s should be required", a)
		}
	}
	for _, a := range []string{"id", "status", "deployment_id", "cloud_region", "rawtree_version", "created_at", "max_size", "idle_timeout_minutes", "external_id", "resource_tag"} {
		if !s.Attributes[a].IsComputed() {
			t.Errorf("%s should be computed", a)
		}
	}
}

func TestMissingStatusSnapshotIsNotADeletedCluster(t *testing.T) {
	pollInterval = time.Millisecond
	f, srv := newFakeAPI(t)
	f.snapshotLag = true
	r := newTestResource(srv.URL)
	ctx := context.Background()

	// Read keeps the cluster in state.
	rresp := fwresource.ReadResponse{State: stateFrom(t, createdState())}
	r.Read(ctx, fwresource.ReadRequest{State: stateFrom(t, createdState())}, &rresp)
	if rresp.Diagnostics.HasError() || rresp.Diagnostics.WarningsCount() != 1 || rresp.State.Raw.IsNull() {
		t.Fatalf("read must keep the state with a warning: %v", rresp.Diagnostics)
	}

	// Create continues to poll.
	f.polls = 0
	cresp := fwresource.CreateResponse{State: emptyState()}
	r.Create(ctx, fwresource.CreateRequest{Plan: planFrom(t, basePlan())}, &cresp)
	if cresp.Diagnostics.HasError() || stateModel(t, cresp.State).Status.ValueString() != "ready" {
		t.Fatalf("create must wait through the 404: %v", cresp.Diagnostics)
	}
}

func TestDeleteRefusesProtectedCluster(t *testing.T) {
	f, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	m := createdState()
	m.DeletionProtection = types.BoolValue(true)

	resp := fwresource.DeleteResponse{State: stateFrom(t, m)}
	r.Delete(context.Background(), fwresource.DeleteRequest{State: stateFrom(t, m)}, &resp)
	if !resp.Diagnostics.HasError() || len(f.requests) != 0 {
		t.Errorf("delete must fail without an API call: %v, requests %v", resp.Diagnostics, f.requests)
	}
}

func TestRegionAliasDoesNotReplace(t *testing.T) {
	_, srv := newFakeAPI(t)
	r := newTestResource(srv.URL)
	ctx := context.Background()

	for region, replace := range map[string]bool{"eu-west-1": false, "Ireland": false, "us-east-1": true} {
		plan := createdState()
		plan.RegionID = types.StringValue(region)
		resp := fwresource.ModifyPlanResponse{Plan: planFrom(t, plan)}
		r.ModifyPlan(ctx, fwresource.ModifyPlanRequest{Plan: planFrom(t, plan), State: stateFrom(t, createdState())}, &resp)
		if got := len(resp.RequiresReplace) > 0; got != replace {
			t.Errorf("region_id %q: replace = %v, want %v", region, got, replace)
		}
	}
}
