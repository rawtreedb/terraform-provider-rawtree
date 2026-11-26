package service_account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

const orgID = "11111111-1111-1111-1111-111111111111"

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *ServiceAccountResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/v1/organizations" {
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"` + orgID + `","name":"acme"}]}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &ServiceAccountResource{client: client.New("", "token", srv.URL, "acme", "", "", "")}
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

func emptyState() tfsdk.State {
	return tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(objectType(), nil)}
}

func TestCreate(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/organizations/"+orgID+"/service-accounts" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		want := map[string]any{"name": "ci-deployer", "description": "GitHub Actions", "role": "member"}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("body[%s] = %v, want %v", k, body[k], v)
			}
		}
		_, _ = w.Write([]byte(`{"id":"sa-1","organization_id":"` + orgID + `","name":"ci-deployer","description":"GitHub Actions","role":"member"}`))
	})
	plan := tfsdk.Plan{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"organization_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name":            str("ci-deployer"),
		"description":     str("GitHub Actions"),
		"role":            str("member"),
	})}
	resp := &resource.CreateResponse{State: emptyState()}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	var got ServiceAccountModel
	resp.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "sa-1" || got.OrganizationID.ValueString() != orgID || got.Role.ValueString() != "member" {
		t.Errorf("unexpected state %+v", got)
	}
}

func TestReadFindsByIDAndRemovesMissing(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/v1/organizations/"+orgID+"/service-accounts" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"sa-1","organization_id":"` + orgID + `","name":"ci-deployer","description":null,"role":"admin"}]`))
	})
	for _, tc := range []struct {
		id      string
		removed bool
	}{{"sa-1", false}, {"sa-2", true}} {
		// Import leaves organization_id null; Read resolves it from the provider.
		state := tfsdk.State{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{"id": str(tc.id)})}
		resp := &resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("read: %v", resp.Diagnostics)
		}
		if got := resp.State.Raw.IsNull(); got != tc.removed {
			t.Fatalf("id %s: removed = %v, want %v", tc.id, got, tc.removed)
		}
		if tc.removed {
			continue
		}
		var got ServiceAccountModel
		resp.State.Get(context.Background(), &got)
		if got.Name.ValueString() != "ci-deployer" || got.Role.ValueString() != "admin" || !got.Description.IsNull() || got.OrganizationID.ValueString() != orgID {
			t.Errorf("unexpected state %+v", got)
		}
	}
}

func TestDelete(t *testing.T) {
	called := false
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodDelete || req.URL.Path != "/v1/organizations/"+orgID+"/service-accounts/sa-1" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		called = true
		w.WriteHeader(http.StatusNotFound)
	})
	state := tfsdk.State{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{
		"id": str("sa-1"), "organization_id": str(orgID), "name": str("ci-deployer"), "role": str("member"),
	})}
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete should ignore 404: %v", resp.Diagnostics)
	}
	if !called {
		t.Fatal("DELETE not sent")
	}
}
