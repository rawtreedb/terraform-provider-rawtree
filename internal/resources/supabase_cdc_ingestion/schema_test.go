package supabase_cdc_ingestion

import "testing"

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	s := resourceSchema()

	requiredAttrs := []string{"name", "region", "publication", "subnet_ids"}
	for _, attr := range requiredAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("missing required attribute: %s", attr)
			continue
		}
		if !a.IsRequired() {
			t.Errorf("attribute %s should be required", attr)
		}
	}

	optionalAttrs := []string{
		"database_url",
		"database_url_secret_arn",
		"tls_root_cert_pem",
		"tls_root_cert_secret_arn",
		"security_group_ids",
		"environment",
		"organization",
		"project",
	}
	for _, attr := range optionalAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("missing optional attribute: %s", attr)
			continue
		}
		if !a.IsOptional() {
			t.Errorf("attribute %s should be optional", attr)
		}
	}

	computedAttrs := []string{
		"id",
		"pipeline_id",
		"image",
		"cpu",
		"memory",
		"assign_public_ip",
		"log_retention_days",
		"run_initialization_task",
		"api_url",
		"api_key_hash",
		"cluster_arn",
		"service_arn",
		"task_definition_arn",
		"log_group_name",
		"execution_role_arn",
		"rawtree_secret_arn",
	}
	for _, attr := range computedAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("missing computed attribute: %s", attr)
			continue
		}
		if !a.IsComputed() {
			t.Errorf("attribute %s should be computed", attr)
		}
	}
}
