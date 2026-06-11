package supabase_cdc_ingestion

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestModifyPlanProviderDrift(t *testing.T) {
	t.Parallel()

	r := &SupabaseCDCIngestionResource{
		client: client.New("new-key", "https://new-api.example.com", "new-org", "new-project"),
	}

	stateVals := testSupabaseStateValues("https://old-api.example.com", util.HashString("old-key"), "old-org", "old-project")
	planVals := testSupabaseStateValues("https://old-api.example.com", util.HashString("old-key"), "", "")

	req := resource.ModifyPlanRequest{
		State: buildSupabaseTFState(stateVals),
		Plan:  buildSupabaseTFPlan(planVals),
	}
	resp := &resource.ModifyPlanResponse{
		Plan: buildSupabaseTFPlan(planVals),
	}

	r.ModifyPlan(context.Background(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %s", resp.Diagnostics.Errors())
	}

	var model SupabaseCDCIngestionModel
	diags := resp.Plan.Get(context.Background(), &model)
	if diags.HasError() {
		t.Fatalf("failed to read plan: %s", diags.Errors())
	}

	if model.APIURL.ValueString() != "https://new-api.example.com" {
		t.Fatalf("api_url = %q", model.APIURL.ValueString())
	}
	if model.APIKeyHash.ValueString() != util.HashString("new-key") {
		t.Fatalf("api_key_hash did not refresh")
	}
	if model.Organization.ValueString() != "new-org" {
		t.Fatalf("organization = %q", model.Organization.ValueString())
	}
	if model.Project.ValueString() != "new-project" {
		t.Fatalf("project = %q", model.Project.ValueString())
	}
}

func buildSupabaseTFState(vals map[string]tftypes.Value) tfsdk.State {
	s := resourceSchema()
	raw := tftypes.NewValue(testSupabaseSchemaType(), vals)
	return tfsdk.State{Schema: s, Raw: raw}
}

func buildSupabaseTFPlan(vals map[string]tftypes.Value) tfsdk.Plan {
	s := resourceSchema()
	raw := tftypes.NewValue(testSupabaseSchemaType(), vals)
	return tfsdk.Plan{Schema: s, Raw: raw}
}

func testSupabaseSchemaType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"id":                       tftypes.String,
			"name":                     tftypes.String,
			"region":                   tftypes.String,
			"publication":              tftypes.String,
			"pipeline_id":              tftypes.String,
			"image":                    tftypes.String,
			"cpu":                      tftypes.Number,
			"memory":                   tftypes.Number,
			"subnet_ids":               tftypes.List{ElementType: tftypes.String},
			"security_group_ids":       tftypes.List{ElementType: tftypes.String},
			"assign_public_ip":         tftypes.Bool,
			"database_url":             tftypes.String,
			"database_url_secret_arn":  tftypes.String,
			"tls_root_cert_pem":        tftypes.String,
			"tls_root_cert_secret_arn": tftypes.String,
			"log_retention_days":       tftypes.Number,
			"run_initialization_task":  tftypes.Bool,
			"initialization_command":   tftypes.List{ElementType: tftypes.String},
			"worker_command":           tftypes.List{ElementType: tftypes.String},
			"environment":              tftypes.Map{ElementType: tftypes.String},
			"organization":             tftypes.String,
			"project":                  tftypes.String,
			"api_url":                  tftypes.String,
			"api_key_hash":             tftypes.String,
			"cluster_arn":              tftypes.String,
			"service_arn":              tftypes.String,
			"task_definition_arn":      tftypes.String,
			"log_group_name":           tftypes.String,
			"execution_role_arn":       tftypes.String,
			"config_secret_arn":        tftypes.String,
		},
	}
}

func testSupabaseStateValues(apiURL, apiKeyHash, org, project string) map[string]tftypes.Value {
	listType := tftypes.List{ElementType: tftypes.String}
	mapType := tftypes.Map{ElementType: tftypes.String}

	return map[string]tftypes.Value{
		"id":                       tftypes.NewValue(tftypes.String, "org-project-orders"),
		"name":                     tftypes.NewValue(tftypes.String, "orders"),
		"region":                   tftypes.NewValue(tftypes.String, "us-east-1"),
		"publication":              tftypes.NewValue(tftypes.String, "rawtree_publication"),
		"pipeline_id":              tftypes.NewValue(tftypes.String, "1"),
		"image":                    tftypes.NewValue(tftypes.String, defaultImage),
		"cpu":                      tftypes.NewValue(tftypes.Number, 512),
		"memory":                   tftypes.NewValue(tftypes.Number, 1024),
		"subnet_ids":               tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(tftypes.String, "subnet-1")}),
		"security_group_ids":       tftypes.NewValue(listType, nil),
		"assign_public_ip":         tftypes.NewValue(tftypes.Bool, false),
		"database_url":             tftypes.NewValue(tftypes.String, nil),
		"database_url_secret_arn":  tftypes.NewValue(tftypes.String, "arn:aws:secretsmanager:us-east-1:123456789012:secret:db"),
		"tls_root_cert_pem":        tftypes.NewValue(tftypes.String, nil),
		"tls_root_cert_secret_arn": tftypes.NewValue(tftypes.String, nil),
		"log_retention_days":       tftypes.NewValue(tftypes.Number, 30),
		"run_initialization_task":  tftypes.NewValue(tftypes.Bool, true),
		"initialization_command":   tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(tftypes.String, "init")}),
		"worker_command":           tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(tftypes.String, "run")}),
		"environment":              tftypes.NewValue(mapType, nil),
		"organization":             tftypes.NewValue(tftypes.String, org),
		"project":                  tftypes.NewValue(tftypes.String, project),
		"api_url":                  tftypes.NewValue(tftypes.String, apiURL),
		"api_key_hash":             tftypes.NewValue(tftypes.String, apiKeyHash),
		"cluster_arn":              tftypes.NewValue(tftypes.String, "arn:aws:ecs:us-east-1:123456789012:cluster/rawtree"),
		"service_arn":              tftypes.NewValue(tftypes.String, "arn:aws:ecs:us-east-1:123456789012:service/rawtree/service"),
		"task_definition_arn":      tftypes.NewValue(tftypes.String, "arn:aws:ecs:us-east-1:123456789012:task-definition/rawtree:1"),
		"log_group_name":           tftypes.NewValue(tftypes.String, "/aws/ecs/rawtree/supabase-cdc/org-project-orders"),
		"execution_role_arn":       tftypes.NewValue(tftypes.String, "arn:aws:iam::123456789012:role/rawtree-ecs"),
		"config_secret_arn":        tftypes.NewValue(tftypes.String, "arn:aws:secretsmanager:us-east-1:123456789012:secret:rawtree-config"),
	}
}
