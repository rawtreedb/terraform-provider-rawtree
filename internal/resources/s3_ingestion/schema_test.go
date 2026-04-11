package s3_ingestion

import (
	"testing"
)

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	schema := resourceSchema()

	// Verify required attributes exist.
	requiredAttrs := []string{"table", "bucket", "format", "region"}
	for _, attr := range requiredAttrs {
		a, ok := schema.Attributes[attr]
		if !ok {
			t.Errorf("missing required attribute: %s", attr)
			continue
		}
		if !a.IsRequired() {
			t.Errorf("attribute %s should be required", attr)
		}
	}

	// Verify optional attributes exist.
	optionalAttrs := []string{"prefix", "file_pattern"}
	for _, attr := range optionalAttrs {
		a, ok := schema.Attributes[attr]
		if !ok {
			t.Errorf("missing optional attribute: %s", attr)
			continue
		}
		if !a.IsOptional() {
			t.Errorf("attribute %s should be optional", attr)
		}
	}

	// Verify computed attributes exist.
	computedAttrs := []string{"id", "glue_job_name", "glue_job_run_id", "lambda_function_arn", "eventbridge_rule_arn"}
	for _, attr := range computedAttrs {
		a, ok := schema.Attributes[attr]
		if !ok {
			t.Errorf("missing computed attribute: %s", attr)
			continue
		}
		if !a.IsComputed() {
			t.Errorf("attribute %s should be computed", attr)
		}
	}
}
