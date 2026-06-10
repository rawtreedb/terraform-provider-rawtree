package cloudfront_ingestion

import (
	"testing"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestFirehoseNameLength(t *testing.T) {
	t.Parallel()

	resourceName := util.SanitizeResourceName("myorg-myproject-cloudfront-logs")
	firehoseName := "rawtree-cf-" + resourceName

	if len(firehoseName) > 64 {
		t.Errorf("firehose name too long: %d chars (max 64): %s", len(firehoseName), firehoseName)
	}
}

func TestKinesisStreamName(t *testing.T) {
	t.Parallel()

	resourceName := util.SanitizeResourceName("myorg-myproject-cloudfront-logs")
	streamName := "rawtree-cf-" + resourceName

	// Kinesis stream name max is 128 chars.
	if len(streamName) > 128 {
		t.Errorf("kinesis stream name too long: %d chars (max 128): %s", len(streamName), streamName)
	}

	// Must be alphanumeric, hyphens, underscores, periods.
	for _, c := range streamName {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			t.Errorf("invalid character %q in kinesis stream name: %s", string(c), streamName)
		}
	}
}

func TestEndpointURL(t *testing.T) {
	t.Parallel()

	apiURL := "https://api.us-east-1.aws.rawtree.com"
	org := "myorg"
	project := "myproject"
	table := "cf_logs"
	fields := []string{"timestamp", "c-ip", "sc-status"}

	expected := "https://api.us-east-1.aws.rawtree.com/v1/myorg/myproject/tables/cf_logs?transform=firehose&columns=timestamp,c-ip,sc-status"
	got := buildEndpointURL(apiURL, org, project, table, fields)

	if got != expected {
		t.Errorf("endpoint URL mismatch:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestExtractBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard endpoint with columns",
			input:    "https://api.us-east-1.aws.rawtree.com/v1/myorg/myproject/tables/cf_logs?transform=firehose&columns=timestamp,c-ip",
			expected: "https://api.us-east-1.aws.rawtree.com",
		},
		{
			name:     "localhost endpoint",
			input:    "http://localhost:9876/v1/myorg/myproject/tables/cf_logs?transform=firehose&columns=timestamp",
			expected: "http://localhost:9876",
		},
		{
			name:     "no path match returns full url",
			input:    "https://api.example.com/other/path",
			expected: "https://api.example.com/other/path",
		},
		{
			name:     "bare url",
			input:    "https://api.example.com",
			expected: "https://api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.ExtractBaseURL(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractBaseURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBackupBucketName(t *testing.T) {
	t.Parallel()

	resourceName := util.SanitizeResourceName("myorg-myproject-cloudfront-logs")
	bucketName := "rawtree-cf-backup-" + resourceName

	// S3 bucket name max is 63 chars.
	if len(bucketName) > 63 {
		t.Errorf("bucket name too long: %d chars (max 63): %s", len(bucketName), bucketName)
	}

	for _, c := range bucketName {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			t.Errorf("invalid character %q in bucket name: %s", string(c), bucketName)
		}
	}
}

func TestDefaultFields(t *testing.T) {
	t.Parallel()

	if len(defaultFields) == 0 {
		t.Fatal("defaultFields must not be empty")
	}

	seen := make(map[string]bool)
	for _, f := range defaultFields {
		if f == "" {
			t.Error("empty field name in defaultFields")
		}
		if seen[f] {
			t.Errorf("duplicate field in defaultFields: %s", f)
		}
		seen[f] = true
	}

	sorted := sortFieldsCanonical(defaultFields)
	for i, f := range defaultFields {
		if f != sorted[i] {
			t.Errorf("defaultFields not in canonical order: position %d is %q, expected %q", i, f, sorted[i])
		}
	}
}

func TestSortFieldsCanonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "already canonical",
			input:    []string{"timestamp", "c-ip", "sc-status"},
			expected: []string{"timestamp", "c-ip", "sc-status"},
		},
		{
			name:     "reversed",
			input:    []string{"sc-status", "c-ip", "timestamp"},
			expected: []string{"timestamp", "c-ip", "sc-status"},
		},
		{
			name:     "mixed subset",
			input:    []string{"cs-user-agent", "timestamp", "x-edge-location", "c-ip"},
			expected: []string{"timestamp", "c-ip", "x-edge-location", "cs-user-agent"},
		},
		{
			name:     "full default set shuffled",
			input:    []string{"c-country", "cs-bytes", "timestamp", "ssl-protocol", "cs-protocol-version", "sc-bytes", "cs-method", "cs-host", "x-edge-result-type", "c-ip", "time-to-first-byte", "sc-status", "cs-protocol", "cs-uri-stem", "x-edge-location", "time-taken", "cs-user-agent", "x-edge-response-result-type", "c-port", "x-edge-detailed-result-type"},
			expected: []string{"timestamp", "c-ip", "time-to-first-byte", "sc-status", "sc-bytes", "cs-method", "cs-protocol", "cs-host", "cs-uri-stem", "cs-bytes", "x-edge-location", "time-taken", "cs-protocol-version", "cs-user-agent", "x-edge-response-result-type", "ssl-protocol", "x-edge-result-type", "c-port", "x-edge-detailed-result-type", "c-country"},
		},
		{
			name:     "does not mutate input",
			input:    []string{"c-ip", "timestamp"},
			expected: []string{"timestamp", "c-ip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := make([]string, len(tt.input))
			copy(original, tt.input)

			got := sortFieldsCanonical(tt.input)

			if len(got) != len(tt.expected) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("position %d: got %q, want %q", i, got[i], tt.expected[i])
				}
			}

			if tt.name == "does not mutate input" {
				for i := range tt.input {
					if tt.input[i] != original[i] {
						t.Errorf("input was mutated at position %d: got %q, was %q", i, tt.input[i], original[i])
					}
				}
			}
		})
	}
}
