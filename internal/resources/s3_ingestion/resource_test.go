package s3_ingestion

import (
	"testing"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestSanitizeResourceName_Compat(t *testing.T) {
	t.Parallel()

	result := util.SanitizeResourceName("myorg-myproject-events")
	if len(result) > 40 {
		t.Errorf("result too long: %d chars (%s)", len(result), result)
	}
	if len(result) < 10 {
		t.Errorf("result too short: %d chars (%s)", len(result), result)
	}
}

func TestBuildIngestEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apiURL  string
		org     string
		project string
		table   string
		want    string
	}{
		{
			name:    "standard",
			apiURL:  "https://api.rawtree.com",
			org:     "myorg",
			project: "myproject",
			table:   "events",
			want:    "https://api.rawtree.com/v1/myorg/myproject/tables/events",
		},
		{
			name:    "regional URL",
			apiURL:  "https://api.us-east-1.aws.rawtree.com",
			org:     "acme",
			project: "prod",
			table:   "logs",
			want:    "https://api.us-east-1.aws.rawtree.com/v1/acme/prod/tables/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildIngestEndpoint(tt.apiURL, tt.org, tt.project, tt.table)
			if got != tt.want {
				t.Errorf("buildIngestEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}
