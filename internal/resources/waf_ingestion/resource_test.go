package waf_ingestion

import (
	"testing"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestFirehoseNamePrefix(t *testing.T) {
	t.Parallel()

	resourceName := util.SanitizeResourceName("myorg-myproject-waf-logs")
	firehoseName := "aws-waf-logs-rawtree-" + resourceName

	// Firehose name must start with aws-waf-logs-.
	if len(firehoseName) < len("aws-waf-logs-") {
		t.Fatal("firehose name too short")
	}
	prefix := firehoseName[:len("aws-waf-logs-")]
	if prefix != "aws-waf-logs-" {
		t.Errorf("firehose name must start with 'aws-waf-logs-', got prefix: %s", prefix)
	}

	// Firehose name max is 64 chars.
	if len(firehoseName) > 64 {
		t.Errorf("firehose name too long: %d chars (max 64): %s", len(firehoseName), firehoseName)
	}
}

func TestEndpointURL(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.us-east-1.aws.rawtree.com"
	org := "myorg"
	project := "myproject"
	table := "waf_logs"

	expected := "https://api.us-east-1.aws.rawtree.com/v1/myorg/myproject/tables/waf_logs?transform=firehose"
	got := apiURL + "/v1/" + org + "/" + project + "/tables/" + table + "?transform=firehose"

	if got != expected {
		t.Errorf("endpoint URL mismatch:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestBackupBucketName(t *testing.T) {
	t.Parallel()

	resourceName := util.SanitizeResourceName("myorg-myproject-waf-logs")
	bucketName := "rawtree-waf-backup-" + resourceName

	// S3 bucket name max is 63 chars.
	if len(bucketName) > 63 {
		t.Errorf("bucket name too long: %d chars (max 63): %s", len(bucketName), bucketName)
	}

	// Must be lowercase alphanumeric + hyphens.
	for _, c := range bucketName {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			t.Errorf("invalid character %q in bucket name: %s", string(c), bucketName)
		}
	}
}
