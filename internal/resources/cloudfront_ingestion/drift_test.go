package cloudfront_ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func newTestResource(apiURL, apiKey, org, project string) *CloudfrontIngestionResource {
	return &CloudfrontIngestionResource{
		client: client.New(apiKey, apiURL, org, project),
	}
}

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
			"organization":            tftypes.String,
			"project":                 tftypes.String,
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

func testStateValues(apiURL, apiKeyHash, org, project, table, endpointURL string) map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"id":                      tftypes.NewValue(tftypes.String, "test-id"),
		"table":                   tftypes.NewValue(tftypes.String, table),
		"distribution_id":         tftypes.NewValue(tftypes.String, "E1234567890ABC"),
		"region":                  tftypes.NewValue(tftypes.String, "us-east-1"),
		"sampling_rate":           tftypes.NewValue(tftypes.Number, 100),
		"fields":                  testFieldsList(),
		"buffering_size":          tftypes.NewValue(tftypes.Number, 5),
		"buffering_interval":      tftypes.NewValue(tftypes.Number, 300),
		"s3_backup_mode":          tftypes.NewValue(tftypes.String, "FailedDataOnly"),
		"api_url":                 tftypes.NewValue(tftypes.String, apiURL),
		"api_key_hash":            tftypes.NewValue(tftypes.String, apiKeyHash),
		"organization":            tftypes.NewValue(tftypes.String, org),
		"project":                 tftypes.NewValue(tftypes.String, project),
		"endpoint_url":            tftypes.NewValue(tftypes.String, endpointURL),
		"kinesis_stream_arn":      tftypes.NewValue(tftypes.String, "arn:aws:kinesis:us-east-1:123456789012:stream/test"),
		"kinesis_stream_name":     tftypes.NewValue(tftypes.String, "rawtree-cf-test"),
		"firehose_arn":            tftypes.NewValue(tftypes.String, "arn:aws:firehose:us-east-1:123456789012:deliverystream/test"),
		"firehose_name":           tftypes.NewValue(tftypes.String, "rawtree-cf-test"),
		"backup_bucket_name":      tftypes.NewValue(tftypes.String, "rawtree-cf-backup-test"),
		"realtime_log_config_arn": tftypes.NewValue(tftypes.String, "arn:aws:cloudfront::123456789012:realtime-log-config/test"),
	}
}

func buildTFState(vals map[string]tftypes.Value) tfsdk.State {
	s := resourceSchema()
	objType := testSchemaType()
	raw := tftypes.NewValue(objType, vals)
	return tfsdk.State{Schema: s, Raw: raw}
}

func buildTFPlan(vals map[string]tftypes.Value) tfsdk.Plan {
	s := resourceSchema()
	objType := testSchemaType()
	raw := tftypes.NewValue(objType, vals)
	return tfsdk.Plan{Schema: s, Raw: raw}
}

func readPlanModel(t *testing.T, plan tfsdk.Plan) CloudfrontIngestionModel {
	t.Helper()
	var m CloudfrontIngestionModel
	diags := plan.Get(context.Background(), &m)
	if diags.HasError() {
		t.Fatalf("failed to read plan model: %s", diags.Errors())
	}
	return m
}

func TestModifyPlan_APIURLDrift(t *testing.T) {
	t.Parallel()

	oldURL := "https://old-api.example.com"
	newURL := "https://new-api.example.com"
	org := "myorg"
	project := "myproject"
	table := "cf_logs"
	oldEndpoint := oldURL + "/v1/" + org + "/" + project + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	oldKeyHash := util.HashString("old-key")

	r := newTestResource(newURL, "old-key", org, project)

	stateVals := testStateValues(oldURL, oldKeyHash, org, project, table, oldEndpoint)
	planVals := testStateValues(oldURL, oldKeyHash, org, project, table, oldEndpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if m.APIURL.ValueString() != newURL {
		t.Errorf("expected api_url %q, got %q", newURL, m.APIURL.ValueString())
	}

	if !strings.Contains(m.EndpointURL.ValueString(), newURL+"/v1/"+org+"/"+project+"/tables/"+table+"?transform=firehose&columns=") {
		t.Errorf("expected endpoint_url to contain new URL with columns param, got %q", m.EndpointURL.ValueString())
	}
}

func TestModifyPlan_APIKeyDrift(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.example.com"
	org := "myorg"
	project := "myproject"
	table := "cf_logs"
	endpoint := apiURL + "/v1/" + org + "/" + project + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	oldKeyHash := util.HashString("old-key")
	newKeyHash := util.HashString("new-key")

	r := newTestResource(apiURL, "new-key", org, project)

	stateVals := testStateValues(apiURL, oldKeyHash, org, project, table, endpoint)
	planVals := testStateValues(apiURL, oldKeyHash, org, project, table, endpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if m.APIKeyHash.ValueString() != newKeyHash {
		t.Errorf("expected api_key_hash %q, got %q", newKeyHash, m.APIKeyHash.ValueString())
	}

	if m.APIKeyHash.ValueString() == oldKeyHash {
		t.Error("api_key_hash should have changed but still matches old value")
	}
}

func TestModifyPlan_OrganizationDrift(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.example.com"
	oldOrg := "old-org"
	newOrg := "new-org"
	project := "myproject"
	table := "cf_logs"
	oldEndpoint := apiURL + "/v1/" + oldOrg + "/" + project + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	keyHash := util.HashString("key")

	r := newTestResource(apiURL, "key", newOrg, project)

	stateVals := testStateValues(apiURL, keyHash, oldOrg, project, table, oldEndpoint)
	planVals := testStateValues(apiURL, keyHash, "", project, table, oldEndpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if !strings.Contains(m.EndpointURL.ValueString(), apiURL+"/v1/"+newOrg+"/"+project+"/tables/"+table+"?transform=firehose&columns=") {
		t.Errorf("expected endpoint_url to contain new org with columns param, got %q", m.EndpointURL.ValueString())
	}
}

func TestModifyPlan_ProjectDrift(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.example.com"
	org := "myorg"
	oldProject := "old-project"
	newProject := "new-project"
	table := "cf_logs"
	oldEndpoint := apiURL + "/v1/" + org + "/" + oldProject + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	keyHash := util.HashString("key")

	r := newTestResource(apiURL, "key", org, newProject)

	stateVals := testStateValues(apiURL, keyHash, org, oldProject, table, oldEndpoint)
	planVals := testStateValues(apiURL, keyHash, org, "", table, oldEndpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if !strings.Contains(m.EndpointURL.ValueString(), apiURL+"/v1/"+org+"/"+newProject+"/tables/"+table+"?transform=firehose&columns=") {
		t.Errorf("expected endpoint_url to contain new project with columns param, got %q", m.EndpointURL.ValueString())
	}
}

func TestModifyPlan_NoDrift(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.example.com"
	org := "myorg"
	project := "myproject"
	table := "cf_logs"
	endpoint := apiURL + "/v1/" + org + "/" + project + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	key := "my-key"
	keyHash := util.HashString(key)

	r := newTestResource(apiURL, key, org, project)

	stateVals := testStateValues(apiURL, keyHash, org, project, table, endpoint)
	planVals := testStateValues(apiURL, keyHash, org, project, table, endpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if m.APIURL.ValueString() != apiURL {
		t.Errorf("api_url should not change: expected %q, got %q", apiURL, m.APIURL.ValueString())
	}
	if !strings.Contains(m.EndpointURL.ValueString(), apiURL+"/v1/"+org+"/"+project+"/tables/"+table+"?transform=firehose&columns=") {
		t.Errorf("endpoint_url unexpected: got %q", m.EndpointURL.ValueString())
	}
	if m.APIKeyHash.ValueString() != keyHash {
		t.Errorf("api_key_hash should not change: expected %q, got %q", keyHash, m.APIKeyHash.ValueString())
	}
}

func TestModifyPlan_MultipleFieldDrift(t *testing.T) {
	t.Parallel()

	oldURL := "https://old-api.example.com"
	newURL := "https://new-api.example.com"
	oldOrg := "old-org"
	newOrg := "new-org"
	project := "myproject"
	table := "cf_logs"
	oldEndpoint := oldURL + "/v1/" + oldOrg + "/" + project + "/tables/" + table + "?transform=firehose&columns=c-ip,sc-status,timestamp"
	oldKey := "old-key"
	newKey := "new-key"
	oldKeyHash := util.HashString(oldKey)

	r := newTestResource(newURL, newKey, newOrg, project)

	stateVals := testStateValues(oldURL, oldKeyHash, oldOrg, project, table, oldEndpoint)
	planVals := testStateValues(oldURL, oldKeyHash, "", project, table, oldEndpoint)

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	m := readPlanModel(t, resp.Plan)

	if m.APIURL.ValueString() != newURL {
		t.Errorf("expected api_url %q, got %q", newURL, m.APIURL.ValueString())
	}

	if !strings.Contains(m.EndpointURL.ValueString(), newURL+"/v1/"+newOrg+"/"+project+"/tables/"+table+"?transform=firehose&columns=") {
		t.Errorf("expected endpoint_url to contain new URL/org with columns param, got %q", m.EndpointURL.ValueString())
	}

	newKeyHash := util.HashString(newKey)
	if m.APIKeyHash.ValueString() != newKeyHash {
		t.Errorf("expected api_key_hash %q, got %q", newKeyHash, m.APIKeyHash.ValueString())
	}
}

func TestModifyPlan_SkipsOnCreate(t *testing.T) {
	t.Parallel()

	r := newTestResource("https://api.example.com", "key", "org", "project")

	planVals := testStateValues("https://api.example.com", util.HashString("key"), "org", "project", "cf_logs",
		"https://api.example.com/v1/org/project/tables/cf_logs?transform=firehose&columns=c-ip,sc-status,timestamp")

	req := resource.ModifyPlanRequest{
		State: tfsdk.State{Schema: resourceSchema(), Raw: tftypes.NewValue(testSchemaType(), nil)},
		Plan:  buildTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics on create: %s", resp.Diagnostics.Errors())
	}
}

func TestModifyPlan_SkipsOnDestroy(t *testing.T) {
	t.Parallel()

	r := newTestResource("https://api.example.com", "key", "org", "project")

	stateVals := testStateValues("https://api.example.com", util.HashString("key"), "org", "project", "cf_logs",
		"https://api.example.com/v1/org/project/tables/cf_logs?transform=firehose&columns=c-ip,sc-status,timestamp")

	req := resource.ModifyPlanRequest{
		State: buildTFState(stateVals),
		Plan:  tfsdk.Plan{Schema: resourceSchema(), Raw: tftypes.NewValue(testSchemaType(), nil)},
	}
	resp := &resource.ModifyPlanResponse{
		Plan: tfsdk.Plan{Schema: resourceSchema(), Raw: tftypes.NewValue(testSchemaType(), nil)},
	}

	r.ModifyPlan(context.Background(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics on destroy: %s", resp.Diagnostics.Errors())
	}
}

// Verify the unused import is used.
var _ attr.Value = types.StringValue("")
