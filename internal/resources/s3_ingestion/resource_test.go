package s3_ingestion

import (
	"testing"
)

func TestSanitizeResourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantLen  bool // just check length constraints
		contains string
	}{
		{
			name:    "simple lowercase",
			input:   "myorg-myproject-events",
			wantLen: true,
		},
		{
			name:    "underscores converted to hyphens",
			input:   "my_org-my_project-my_table",
			wantLen: true,
		},
		{
			name:    "uppercase converted",
			input:   "MyOrg-MyProject-Events",
			wantLen: true,
		},
		{
			name:    "special characters removed",
			input:   "org@name-proj!ect-tab.le",
			wantLen: true,
		},
		{
			name:    "very long name truncated",
			input:   "this-is-a-very-long-organization-name-with-a-very-long-project-name-and-table",
			wantLen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeResourceName(tt.input)

			// Must be <= 40 chars.
			if len(result) > 40 {
				t.Errorf("result too long: %d chars (%s)", len(result), result)
			}

			// Must be >= 9 chars (at minimum: 1 char + hyphen + 8 char hash).
			if len(result) < 10 {
				t.Errorf("result too short: %d chars (%s)", len(result), result)
			}

			// Must not contain invalid S3 bucket name chars.
			for _, c := range result {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
					t.Errorf("invalid character %q in result: %s", string(c), result)
				}
			}

			// Must not start or end with hyphen.
			if result[0] == '-' || result[len(result)-1] == '-' {
				t.Errorf("result starts or ends with hyphen: %s", result)
			}

			// Same input must produce same output (deterministic).
			result2 := sanitizeResourceName(tt.input)
			if result != result2 {
				t.Errorf("not deterministic: %s != %s", result, result2)
			}
		})
	}

	// Different inputs must produce different outputs.
	t.Run("uniqueness", func(t *testing.T) {
		t.Parallel()
		a := sanitizeResourceName("org-project-table1")
		b := sanitizeResourceName("org-project-table2")
		if a == b {
			t.Errorf("different inputs produced same result: %s", a)
		}
	})
}
