package waf_ingestion

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func testSchemaType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"id":                           tftypes.String,
			"table":                        tftypes.String,
			"web_acl_arn":                  tftypes.String,
			"region":                       tftypes.String,
			"buffering_size":               tftypes.Number,
			"buffering_interval":           tftypes.Number,
			"s3_backup_mode":               tftypes.String,
			"api_url":                      tftypes.String,
			"api_key_hash":                 tftypes.String,
			"database":                     tftypes.String,
			"endpoint_url":                 tftypes.String,
			"firehose_arn":                 tftypes.String,
			"firehose_name":                tftypes.String,
			"backup_bucket_name":           tftypes.String,
			"waf_logging_configuration_id": tftypes.String,
		},
	}
}

// testValues builds a state/plan/config object. A nil database means unset.
func testValues(apiURL, apiKeyHash string, database *string, endpointURL string) tftypes.Value {
	return tftypes.NewValue(testSchemaType(), map[string]tftypes.Value{
		"id":                           tftypes.NewValue(tftypes.String, "test-id"),
		"table":                        tftypes.NewValue(tftypes.String, "waf_logs"),
		"web_acl_arn":                  tftypes.NewValue(tftypes.String, "arn:aws:wafv2:us-east-1:123456789012:global/webacl/test/abc123"),
		"region":                       tftypes.NewValue(tftypes.String, "us-east-1"),
		"buffering_size":               tftypes.NewValue(tftypes.Number, 5),
		"buffering_interval":           tftypes.NewValue(tftypes.Number, 300),
		"s3_backup_mode":               tftypes.NewValue(tftypes.String, "FailedDataOnly"),
		"api_url":                      tftypes.NewValue(tftypes.String, apiURL),
		"api_key_hash":                 tftypes.NewValue(tftypes.String, apiKeyHash),
		"database":                     tftypes.NewValue(tftypes.String, database),
		"endpoint_url":                 tftypes.NewValue(tftypes.String, endpointURL),
		"firehose_arn":                 tftypes.NewValue(tftypes.String, "arn:aws:firehose:us-east-1:123456789012:deliverystream/test"),
		"firehose_name":                tftypes.NewValue(tftypes.String, "aws-waf-logs-rawtree-test"),
		"backup_bucket_name":           tftypes.NewValue(tftypes.String, "rawtree-waf-backup-test"),
		"waf_logging_configuration_id": tftypes.NewValue(tftypes.String, "arn:aws:wafv2:us-east-1:123456789012:global/webacl/test/abc123"),
	})
}

func ptr(s string) *string { return &s }

func TestModifyPlanDrift(t *testing.T) {
	t.Parallel()

	const url = "https://api.rawtree.com"
	endpoint := func(base, query string) string { return base + "/v1/tables/waf_logs?" + query }

	tests := []struct {
		name           string
		providerURL    string
		providerKey    string
		providerDB     string
		state          tftypes.Value
		configDB       *string
		wantEndpoint   string
		wantDatabase   string
		wantAPIKeyHash string
	}{
		{
			name:         "no drift",
			providerURL:  url,
			providerKey:  "k",
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "transform=firehose")),
			wantEndpoint: endpoint(url, "transform=firehose"),
		},
		{
			name:         "api url changed",
			providerURL:  "https://api.rawtree.dev",
			providerKey:  "k",
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "transform=firehose")),
			wantEndpoint: endpoint("https://api.rawtree.dev", "transform=firehose"),
		},
		{
			name:           "api key changed",
			providerURL:    url,
			providerKey:    "new",
			state:          testValues(url, util.HashString("old"), ptr(""), endpoint(url, "transform=firehose")),
			wantEndpoint:   endpoint(url, "transform=firehose"),
			wantAPIKeyHash: util.HashString("new"),
		},
		{
			name:         "provider database changed",
			providerURL:  url,
			providerKey:  "k",
			providerDB:   "analytics",
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "transform=firehose")),
			wantEndpoint: endpoint(url, "database=analytics&transform=firehose"),
			wantDatabase: "analytics",
		},
		{
			name:         "resource database overrides provider",
			providerURL:  url,
			providerKey:  "k",
			providerDB:   "analytics",
			configDB:     ptr("security"),
			state:        testValues(url, util.HashString("k"), ptr("analytics"), endpoint(url, "database=analytics&transform=firehose")),
			wantEndpoint: endpoint(url, "database=security&transform=firehose"),
			wantDatabase: "security",
		},
		{
			// State written by provider versions with organization/project.
			name:         "migration from org/project endpoint",
			providerURL:  url,
			providerKey:  "k",
			state:        testValues("https://api.us-east-1.aws.rawtree.com", util.HashString("k"), nil, "https://api.us-east-1.aws.rawtree.com/v1/acme/prod/tables/waf_logs?transform=firehose"),
			wantEndpoint: endpoint(url, "transform=firehose"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &WafIngestionResource{client: client.New(tt.providerKey, tt.providerURL, "", tt.providerDB, "")}
			s := resourceSchema()
			// Config only matters for database; other values are ignored by ModifyPlan.
			config := testValues(url, "", tt.configDB, "")
			req := resource.ModifyPlanRequest{
				State:  tfsdk.State{Schema: s, Raw: tt.state},
				Plan:   tfsdk.Plan{Schema: s, Raw: tt.state},
				Config: tfsdk.Config{Schema: s, Raw: config},
			}
			resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: tt.state}}

			r.ModifyPlan(context.Background(), req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
			}
			var m WafIngestionModel
			if diags := resp.Plan.Get(context.Background(), &m); diags.HasError() {
				t.Fatal(diags.Errors())
			}
			if m.EndpointURL.ValueString() != tt.wantEndpoint {
				t.Errorf("endpoint_url = %q, want %q", m.EndpointURL.ValueString(), tt.wantEndpoint)
			}
			if m.Database.IsNull() || m.Database.ValueString() != tt.wantDatabase {
				t.Errorf("database = %v, want %q", m.Database, tt.wantDatabase)
			}
			if m.APIURL.ValueString() != tt.providerURL {
				t.Errorf("api_url = %q, want %q", m.APIURL.ValueString(), tt.providerURL)
			}
			if tt.wantAPIKeyHash != "" && m.APIKeyHash.ValueString() != tt.wantAPIKeyHash {
				t.Errorf("api_key_hash = %q, want %q", m.APIKeyHash.ValueString(), tt.wantAPIKeyHash)
			}
		})
	}
}

func TestModifyPlan_SkipsOnCreateAndDestroy(t *testing.T) {
	t.Parallel()

	r := &WafIngestionResource{client: client.New("k", "https://api.rawtree.com", "", "", "")}
	s := resourceSchema()
	null := tftypes.NewValue(testSchemaType(), nil)
	vals := testValues("https://api.rawtree.com", util.HashString("k"), nil, "x")

	for _, pair := range [][2]tftypes.Value{{null, vals}, {vals, null}} {
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s, Raw: pair[1]}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			State:  tfsdk.State{Schema: s, Raw: pair[0]},
			Plan:   tfsdk.Plan{Schema: s, Raw: pair[1]},
			Config: tfsdk.Config{Schema: s, Raw: pair[1]},
		}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
		}
	}
}
