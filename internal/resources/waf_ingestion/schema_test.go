package waf_ingestion

import (
	"testing"
)

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	s := resourceSchema()

	requiredAttrs := []string{"table", "web_acl_arn", "region"}
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

	optionalAttrs := []string{"buffering_size", "buffering_interval", "s3_backup_mode", "organization", "project"}
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

	computedAttrs := []string{"id", "api_url", "api_key_hash", "firehose_arn", "firehose_name", "backup_bucket_name", "waf_logging_configuration_id"}
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

func TestResourceSchema_Defaults(t *testing.T) {
	t.Parallel()

	s := resourceSchema()

	// buffering_size, buffering_interval, s3_backup_mode should be computed (have defaults).
	for _, attr := range []string{"buffering_size", "buffering_interval", "s3_backup_mode"} {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Fatalf("missing attribute: %s", attr)
		}
		if !a.IsComputed() {
			t.Errorf("attribute %s should be computed (has default)", attr)
		}
	}
}
