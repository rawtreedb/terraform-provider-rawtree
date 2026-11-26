package federation_rule

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

const (
	orgID     = "11111111-1111-1111-1111-111111111111"
	clusterID = "33333333-3333-3333-3333-333333333333"
	ruleID    = "44444444-4444-4444-4444-444444444444"
	issuerID  = "22222222-2222-2222-2222-222222222222"
	saID      = "55555555-5555-5555-5555-555555555555"
)

func ruleJSON(enabled bool) string {
	e := "true"
	if !enabled {
		e = "false"
	}
	return `{"id":"` + ruleID + `","organization_id":"` + orgID + `","cluster_id":"` + clusterID + `",
		"name":"deploy","description":null,"issuer_id":"` + issuerID + `",
		"allowed_audiences":["https://github.com/acme"],
		"claim_conditions":{"repository_id":"123","ref":"refs/heads/main"},
		"principal_type":"service_account","principal_id":"` + saID + `",
		"permission":null,"database_roles":["deployer"],"database":"analytics","enabled":` + e + `,
		"created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}`
}

// newServer serves the cluster routing lookups and passes rule calls to handler.
func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *FederationRuleResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/organizations":
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"` + orgID + `","name":"acme"}]}`))
		case "/v1/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"r1","name":"us-east-1","cloud_region":"us-east-1","deployment_id":"abcdef12","enabled":true}]}`))
		case "/v1/clusters":
			_, _ = w.Write([]byte(`{"clusters":[{"id":"` + clusterID + `","name":"prod"}]}`))
		default:
			if r.Header.Get("X-RawTree-Organization-Id") != orgID || r.Header.Get("X-RawTree-Cluster-Id") != clusterID || r.URL.RawQuery != "" {
				t.Errorf("missing cluster ID selectors: %s %v", r.URL.RawQuery, r.Header)
			}
			if r.Header.Get("X-RawTree-Deployment-Id") != "abcdef12" {
				t.Errorf("request not routed to the cluster deployment")
			}
			handler(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &FederationRuleResource{client: client.New("", "token", srv.URL, "acme", "prod", "", "")}
}

func objectType() tftypes.Object {
	return resourceSchema().Type().TerraformType(context.Background()).(tftypes.Object)
}

// rawObject fills unset attributes with null.
func rawObject(vals map[string]tftypes.Value) tftypes.Value {
	typ := objectType()
	full := map[string]tftypes.Value{}
	for name, t := range typ.AttributeTypes {
		if v, ok := vals[name]; ok {
			full[name] = v
		} else {
			full[name] = tftypes.NewValue(t, nil)
		}
	}
	return tftypes.NewValue(typ, full)
}

func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

func unknown() tftypes.Value { return tftypes.NewValue(tftypes.String, tftypes.UnknownValue) }

func planValues() map[string]tftypes.Value {
	strSet := tftypes.Set{ElementType: tftypes.String}
	return map[string]tftypes.Value{
		"id": unknown(), "organization_id": unknown(), "cluster_id": unknown(), "created_at": unknown(), "updated_at": unknown(),
		"cluster":           str("prod"),
		"name":              str("deploy"),
		"issuer_id":         str(issuerID),
		"allowed_audiences": tftypes.NewValue(strSet, []tftypes.Value{str("https://github.com/acme")}),
		"claim_conditions": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"ref": str("refs/heads/main"), "repository_id": str("123"),
		}),
		"principal_type": str("service_account"),
		"principal_id":   str(saID),
		"database_roles": tftypes.NewValue(strSet, []tftypes.Value{str("deployer")}),
		"database":       str("analytics"),
		"enabled":        tftypes.NewValue(tftypes.Bool, true),
	}
}

func TestCreate(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/federation-rules" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		want := map[string]any{
			"name": "deploy", "description": nil, "issuer_id": issuerID,
			"allowed_audiences": []any{"https://github.com/acme"},
			"claim_conditions":  map[string]any{"repository_id": "123", "ref": "refs/heads/main"},
			"principal_type":    "service_account", "principal_id": saID,
			"database_roles": []any{"deployer"}, "database": "analytics", "enabled": true,
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("body = %v\nwant %v", body, want)
		}
		_, _ = w.Write([]byte(ruleJSON(true)))
	})
	ctx := context.Background()
	plan := tfsdk.Plan{Schema: resourceSchema(), Raw: rawObject(planValues())}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(objectType(), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	var got FederationRuleModel
	resp.State.Get(ctx, &got)
	if got.ID.ValueString() != ruleID || got.ClusterID.ValueString() != clusterID || got.Cluster.ValueString() != "prod" || !got.Permission.IsNull() {
		t.Errorf("unexpected state %+v", got)
	}
	// State must equal the plan for configured attributes, or Terraform reports an inconsistent result.
	var planned FederationRuleModel
	plan.Get(ctx, &planned)
	if !got.ClaimConditions.Equal(planned.ClaimConditions) || !got.DatabaseRoles.Equal(planned.DatabaseRoles) || !got.AllowedAudiences.Equal(planned.AllowedAudiences) {
		t.Errorf("state drifted from plan: %+v", got)
	}
}

func TestUpdateAndReadAfterImport(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/federation-rules/"+ruleID {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		switch req.Method {
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["enabled"] != false {
				t.Errorf("enabled not sent: %v", body)
			}
			_, _ = w.Write([]byte(ruleJSON(false)))
		case http.MethodGet:
			_, _ = w.Write([]byte(ruleJSON(true)))
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	})
	ctx := context.Background()

	// Import, then Read fills everything from the API.
	iresp := &resource.ImportStateResponse{State: tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(objectType(), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "prod/" + ruleID}, iresp)
	if iresp.Diagnostics.HasError() {
		t.Fatalf("import: %v", iresp.Diagnostics)
	}
	rresp := &resource.ReadResponse{State: iresp.State}
	r.Read(ctx, resource.ReadRequest{State: iresp.State}, rresp)
	if rresp.Diagnostics.HasError() {
		t.Fatalf("read: %v", rresp.Diagnostics)
	}
	var got FederationRuleModel
	rresp.State.Get(ctx, &got)
	if got.Name.ValueString() != "deploy" || got.Cluster.ValueString() != "prod" || got.ClaimConditions.IsNull() {
		t.Errorf("unexpected state %+v", got)
	}

	vals := planValues()
	vals["id"] = str(ruleID)
	vals["enabled"] = tftypes.NewValue(tftypes.Bool, false)
	plan := tfsdk.Plan{Schema: resourceSchema(), Raw: rawObject(vals)}
	uresp := &resource.UpdateResponse{State: rresp.State}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: rresp.State}, uresp)
	if uresp.Diagnostics.HasError() {
		t.Fatalf("update: %v", uresp.Diagnostics)
	}
	uresp.State.Get(ctx, &got)
	if got.Enabled.ValueBool() {
		t.Error("enabled should be false after update")
	}
}

func TestReadRemovesOnNotFoundAndDelete(t *testing.T) {
	deleted := false
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/federation-rules/"+ruleID {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		if req.Method == http.MethodDelete {
			deleted = true
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	ctx := context.Background()
	state := tfsdk.State{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{"id": str(ruleID), "cluster": str("prod")})}
	rresp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, rresp)
	if rresp.Diagnostics.HasError() || !rresp.State.Raw.IsNull() {
		t.Fatalf("read should remove the resource: %v", rresp.Diagnostics)
	}
	dresp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, dresp)
	if dresp.Diagnostics.HasError() || !deleted {
		t.Fatalf("delete: deleted=%v %v", deleted, dresp.Diagnostics)
	}
}

func TestImportRejectsBareID(t *testing.T) {
	r := &FederationRuleResource{}
	resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(objectType(), nil)}}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: ruleID}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for an ID without cluster")
	}
}
