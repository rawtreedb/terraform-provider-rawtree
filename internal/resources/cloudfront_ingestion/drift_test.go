package cloudfront_ingestion

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
			"id":                      tftypes.String,
			"table":                   tftypes.String,
			"distribution_id":         tftypes.String,
			"region":                  tftypes.String,
			"sampling_rate":           tftypes.Number,
			"fields":                  tftypes.List{ElementType: tftypes.String},
			"buffering_size":          tftypes.Number,
			"buffering_interval":      tftypes.Number,
			"s3_backup_mode":          tftypes.String,
			"api_url":                 tftypes.String,
			"api_key_hash":            tftypes.String,
			"database":                tftypes.String,
			"endpoint_url":            tftypes.String,
			"kinesis_stream_arn":      tftypes.String,
			"kinesis_stream_name":     tftypes.String,
			"firehose_arn":            tftypes.String,
			"firehose_name":           tftypes.String,
			"backup_bucket_name":      tftypes.String,
			"realtime_log_config_arn": tftypes.String,
		},
	}
}

func testFieldsList() tftypes.Value {
	return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
		tftypes.NewValue(tftypes.String, "timestamp"),
		tftypes.NewValue(tftypes.String, "c-ip"),
		tftypes.NewValue(tftypes.String, "sc-status"),
	})
}

// testValues builds a state/plan/config object. A nil database means unset.
func testValues(apiURL, apiKeyHash string, database *string, endpointURL string) tftypes.Value {
	return tftypes.NewValue(testSchemaType(), map[string]tftypes.Value{
		"id":                      tftypes.NewValue(tftypes.String, "test-id"),
		"table":                   tftypes.NewValue(tftypes.String, "cf_logs"),
		"distribution_id":         tftypes.NewValue(tftypes.String, "E1234567890ABC"),
		"region":                  tftypes.NewValue(tftypes.String, "us-east-1"),
		"sampling_rate":           tftypes.NewValue(tftypes.Number, 100),
		"fields":                  testFieldsList(),
		"buffering_size":          tftypes.NewValue(tftypes.Number, 5),
		"buffering_interval":      tftypes.NewValue(tftypes.Number, 300),
		"s3_backup_mode":          tftypes.NewValue(tftypes.String, "FailedDataOnly"),
		"api_url":                 tftypes.NewValue(tftypes.String, apiURL),
		"api_key_hash":            tftypes.NewValue(tftypes.String, apiKeyHash),
		"database":                tftypes.NewValue(tftypes.String, database),
		"endpoint_url":            tftypes.NewValue(tftypes.String, endpointURL),
		"kinesis_stream_arn":      tftypes.NewValue(tftypes.String, "arn:aws:kinesis:us-east-1:123456789012:stream/test"),
		"kinesis_stream_name":     tftypes.NewValue(tftypes.String, "rawtree-cf-test"),
		"firehose_arn":            tftypes.NewValue(tftypes.String, "arn:aws:firehose:us-east-1:123456789012:deliverystream/test"),
		"firehose_name":           tftypes.NewValue(tftypes.String, "rawtree-cf-test"),
		"backup_bucket_name":      tftypes.NewValue(tftypes.String, "rawtree-cf-backup-test"),
		"realtime_log_config_arn": tftypes.NewValue(tftypes.String, "arn:aws:cloudfront::123456789012:realtime-log-config/test"),
	})
}

func ptr(s string) *string { return &s }

func TestModifyPlanDrift(t *testing.T) {
	t.Parallel()

	const url = "https://api.rawtree.com"
	endpoint := func(base, query string) string { return base + "/v1/tables/cf_logs?" + query }

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
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose")),
			wantEndpoint: endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose"),
		},
		{
			name:         "api url changed",
			providerURL:  "https://api.rawtree.dev",
			providerKey:  "k",
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose")),
			wantEndpoint: endpoint("https://api.rawtree.dev", "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose"),
		},
		{
			name:           "api key changed",
			providerURL:    url,
			providerKey:    "new",
			state:          testValues(url, util.HashString("old"), ptr(""), endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose")),
			wantEndpoint:   endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose"),
			wantAPIKeyHash: util.HashString("new"),
		},
		{
			name:         "provider database changed",
			providerURL:  url,
			providerKey:  "k",
			providerDB:   "analytics",
			state:        testValues(url, util.HashString("k"), ptr(""), endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose")),
			wantEndpoint: endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&database=analytics&transform=firehose"),
			wantDatabase: "analytics",
		},
		{
			name:         "resource database overrides provider",
			providerURL:  url,
			providerKey:  "k",
			providerDB:   "analytics",
			configDB:     ptr("security"),
			state:        testValues(url, util.HashString("k"), ptr("analytics"), endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&database=analytics&transform=firehose")),
			wantEndpoint: endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&database=security&transform=firehose"),
			wantDatabase: "security",
		},
		{
			// State written by provider versions with organization/project.
			name:         "migration from org/project endpoint",
			providerURL:  url,
			providerKey:  "k",
			state:        testValues("https://api.us-east-1.aws.rawtree.com", util.HashString("k"), nil, "https://api.us-east-1.aws.rawtree.com/v1/acme/prod/tables/cf_logs?transform=firehose&columns=c-ip,sc-status,timestamp"),
			wantEndpoint: endpoint(url, "columns=timestamp%2Cc-ip%2Csc-status&transform=firehose"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &CloudfrontIngestionResource{client: client.New(tt.providerKey, "", tt.providerURL, "", "", tt.providerDB, "")}
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
			var m CloudfrontIngestionModel
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

	r := &CloudfrontIngestionResource{client: client.New("k", "", "https://api.rawtree.com", "", "", "", "")}
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
