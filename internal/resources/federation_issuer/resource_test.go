package federation_issuer

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

const (
	orgID     = "11111111-1111-1111-1111-111111111111"
	issuerID  = "22222222-2222-2222-2222-222222222222"
	itemPath  = "/v1/organizations/" + orgID + "/federation-issuers/" + issuerID
	githubURL = "https://token.actions.githubusercontent.com"
)

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *FederationIssuerResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations" {
			_, _ = w.Write([]byte(`{"organizations":[{"organization_id":"` + orgID + `","name":"acme"}]}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &FederationIssuerResource{client: client.New("", "token", srv.URL, "acme", "", "", "")}
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

func issuerJSON(name string) string {
	return `{"id":"` + issuerID + `","organization_id":"` + orgID + `","name":"` + name + `","description":null,"issuer_url":"` + githubURL + `","jwks_source":"discovery","jwks_url":null,"created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}`
}

func decode(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCreateAndUpdate(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/v1/organizations/"+orgID+"/federation-issuers":
			body := decode(t, req)
			if body["name"] != "github-actions" || body["issuer_url"] != githubURL || body["jwks_source"] != "discovery" || body["jwks_url"] != nil {
				t.Errorf("unexpected create body %v", body)
			}
			_, _ = w.Write([]byte(issuerJSON("github-actions")))
		case req.Method == http.MethodPut && req.URL.Path == itemPath:
			body := decode(t, req)
			// PUT replaces the whole configuration: cleared fields are sent as null.
			if _, ok := body["description"]; !ok || body["name"] != "github" {
				t.Errorf("unexpected replace body %v", body)
			}
			_, _ = w.Write([]byte(issuerJSON("github")))
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
	})
	ctx := context.Background()
	plan := tfsdk.Plan{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{
		"id": unknown(), "organization_id": unknown(), "created_at": unknown(), "updated_at": unknown(),
		"name": str("github-actions"), "issuer_url": str(githubURL), "jwks_source": str("discovery"),
	})}
	cresp := &resource.CreateResponse{State: tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(objectType(), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, cresp)
	if cresp.Diagnostics.HasError() {
		t.Fatalf("create: %v", cresp.Diagnostics)
	}
	var got FederationIssuerModel
	cresp.State.Get(ctx, &got)
	if got.ID.ValueString() != issuerID || got.OrganizationID.ValueString() != orgID || !got.JwksURL.IsNull() {
		t.Errorf("unexpected state %+v", got)
	}

	plan = tfsdk.Plan{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{
		"id": str(issuerID), "organization_id": str(orgID), "created_at": str("2026-11-01T00:00:00Z"), "updated_at": unknown(),
		"name": str("github"), "issuer_url": str(githubURL), "jwks_source": str("discovery"),
	})}
	uresp := &resource.UpdateResponse{State: cresp.State}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: cresp.State}, uresp)
	if uresp.Diagnostics.HasError() {
		t.Fatalf("update: %v", uresp.Diagnostics)
	}
	uresp.State.Get(ctx, &got)
	if got.Name.ValueString() != "github" || got.UpdatedAt.ValueString() != "2026-11-02T00:00:00Z" {
		t.Errorf("unexpected state %+v", got)
	}
}

func TestReadRemovesOnNotFound(t *testing.T) {
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != itemPath {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	})
	// Import state: only the ID is set.
	state := tfsdk.State{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{"id": str(issuerID)})}
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("state should be removed")
	}
}

func TestDelete(t *testing.T) {
	called := false
	r := newServer(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodDelete || req.URL.Path != itemPath {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		called = true
	})
	state := tfsdk.State{Schema: resourceSchema(), Raw: rawObject(map[string]tftypes.Value{"id": str(issuerID), "organization_id": str(orgID)})}
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() || !called {
		t.Fatalf("delete: called=%v %v", called, resp.Diagnostics)
	}
}

func TestValidateConfigJwks(t *testing.T) {
	r := &FederationIssuerResource{}
	for _, tc := range []struct {
		source, url string
		wantErr     bool
	}{
		{"discovery", "", false},
		{"discovery", "https://example.com/jwks", true},
		{"explicit_url", "", true},
		{"explicit_url", "https://example.com/jwks", false},
	} {
		vals := map[string]tftypes.Value{"name": str("x"), "issuer_url": str(githubURL), "jwks_source": str(tc.source)}
		if tc.url != "" {
			vals["jwks_url"] = str(tc.url)
		}
		resp := &resource.ValidateConfigResponse{}
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: resourceSchema(), Raw: rawObject(vals)}}, resp)
		if resp.Diagnostics.HasError() != tc.wantErr {
			t.Errorf("%s/%q: error = %v, want %v", tc.source, tc.url, resp.Diagnostics, tc.wantErr)
		}
	}
}
