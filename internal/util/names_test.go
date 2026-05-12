package util

import (
	"testing"
)

func TestSanitizeResourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple lowercase",
			input: "myorg-myproject-events",
		},
		{
			name:  "underscores converted to hyphens",
			input: "my_org-my_project-my_table",
		},
		{
			name:  "uppercase converted",
			input: "MyOrg-MyProject-Events",
		},
		{
			name:  "special characters removed",
			input: "org@name-proj!ect-tab.le",
		},
		{
			name:  "very long name truncated",
			input: "this-is-a-very-long-organization-name-with-a-very-long-project-name-and-table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SanitizeResourceName(tt.input)

			if len(result) > 40 {
				t.Errorf("result too long: %d chars (%s)", len(result), result)
			}

			if len(result) < 10 {
				t.Errorf("result too short: %d chars (%s)", len(result), result)
			}

			for _, c := range result {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
					t.Errorf("invalid character %q in result: %s", string(c), result)
				}
			}

			if result[0] == '-' || result[len(result)-1] == '-' {
				t.Errorf("result starts or ends with hyphen: %s", result)
			}

			result2 := SanitizeResourceName(tt.input)
			if result != result2 {
				t.Errorf("not deterministic: %s != %s", result, result2)
			}
		})
	}

	t.Run("uniqueness", func(t *testing.T) {
		t.Parallel()
		a := SanitizeResourceName("org-project-table1")
		b := SanitizeResourceName("org-project-table2")
		if a == b {
			t.Errorf("different inputs produced same result: %s", a)
		}
	})
}

func TestHashString(t *testing.T) {
	t.Parallel()

	h := HashString("test-key")
	if len(h) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h))
	}

	h2 := HashString("test-key")
	if h != h2 {
		t.Errorf("not deterministic")
	}

	h3 := HashString("different-key")
	if h == h3 {
		t.Errorf("different inputs produced same hash")
	}
}
