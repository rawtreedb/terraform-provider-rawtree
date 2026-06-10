package cloudfront_ingestion

import (
	"testing"
)

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	s := resourceSchema()

	requiredAttrs := []string{"table", "distribution_id", "region"}
	for _, name := range requiredAttrs {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Errorf("missing required attribute %q", name)
			continue
		}
		sa, ok := attr.(interface{ IsRequired() bool })
		if !ok || !sa.IsRequired() {
			t.Errorf("attribute %q should be required", name)
		}
	}

	computedAttrs := []string{
		"id", "api_url", "api_key_hash", "endpoint_url",
		"kinesis_stream_arn", "kinesis_stream_name",
		"firehose_arn", "firehose_name",
		"backup_bucket_name", "realtime_log_config_arn",
	}
	for _, name := range computedAttrs {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Errorf("missing computed attribute %q", name)
			continue
		}
		sa, ok := attr.(interface{ IsComputed() bool })
		if !ok || !sa.IsComputed() {
			t.Errorf("attribute %q should be computed", name)
		}
	}

	optionalAttrs := []string{
		"sampling_rate", "fields", "buffering_size",
		"buffering_interval", "s3_backup_mode",
		"organization", "project",
	}
	for _, name := range optionalAttrs {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Errorf("missing optional attribute %q", name)
			continue
		}
		sa, ok := attr.(interface{ IsOptional() bool })
		if !ok || !sa.IsOptional() {
			t.Errorf("attribute %q should be optional", name)
		}
	}
}
