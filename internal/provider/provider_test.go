package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	resp, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatalf("failed to create provider server: %s", err)
	}
	if resp == nil {
		t.Fatal("provider server is nil")
	}
}

func TestProviderHasResources(t *testing.T) {
	t.Parallel()

	testProviderServer := providerserver.NewProtocol6WithError(New("test")())
	server, err := testProviderServer()
	if err != nil {
		t.Fatalf("failed to create provider server: %s", err)
	}

	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("failed to get provider schema: %s", err)
	}

	for _, name := range []string{"rawtree_api_key", "rawtree_cluster", "rawtree_connector", "rawtree_database", "rawtree_table", "rawtree_workflow", "rawtree_cloudfront_ingestion"} {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("expected %s resource to be registered", name)
		}
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("schema diagnostic: %s: %s", d.Summary, d.Detail)
	}
	if _, ok := resp.ResourceSchemas["rawtree_s3_ingestion"]; !ok {
		t.Error("expected rawtree_s3_ingestion resource to be registered")
	}
	if _, ok := resp.ResourceSchemas["rawtree_supabase_cdc_ingestion"]; !ok {
		t.Error("expected rawtree_supabase_cdc_ingestion resource to be registered")
	}
	if _, ok := resp.ResourceSchemas["rawtree_waf_ingestion"]; !ok {
		t.Error("expected rawtree_waf_ingestion resource to be registered")
	}
}

func TestResolveString(t *testing.T) {
	// Cannot use t.Parallel() because subtests use t.Setenv.

	tests := []struct {
		name     string
		tfValue  types.String
		envVar   string
		envVal   string
		fallback string
		expected string
	}{
		{
			name:     "tf value takes precedence",
			tfValue:  types.StringValue("from-tf"),
			envVar:   "TEST_RESOLVE_1",
			fallback: "fallback",
			expected: "from-tf",
		},
		{
			name:     "env var used when tf is null",
			tfValue:  types.StringNull(),
			envVar:   "TEST_RESOLVE_2",
			envVal:   "from-env",
			fallback: "fallback",
			expected: "from-env",
		},
		{
			name:     "fallback used when both empty",
			tfValue:  types.StringNull(),
			envVar:   "TEST_RESOLVE_3",
			fallback: "fallback",
			expected: "fallback",
		},
		{
			name:     "unknown tf value uses env",
			tfValue:  types.StringUnknown(),
			envVar:   "TEST_RESOLVE_4",
			envVal:   "from-env",
			fallback: "fallback",
			expected: "from-env",
		},
		{
			name:     "empty string returns empty",
			tfValue:  types.StringNull(),
			envVar:   "TEST_RESOLVE_5",
			fallback: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(tt.envVar, tt.envVal)
			}
			result := resolveString(tt.tfValue, tt.envVar, tt.fallback)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLoadCLIConfig(t *testing.T) {
	// Cannot use t.Parallel() because subtests use t.Setenv.

	t.Run("valid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configDir := filepath.Join(tmpDir, ".config", "rtree")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatal(err)
		}

		cfg := cliConfig{
			Token:               "rw_test123",
			URL:                 "https://api.test.rawtree.com",
			DefaultProject:      "myproject",
			DefaultOrganization: "myorg",
		}
		data, _ := json.Marshal(cfg)
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("HOME", tmpDir)

		result := loadCLIConfig()
		if result.Token != "rw_test123" {
			t.Errorf("expected token rw_test123, got %s", result.Token)
		}
		if result.URL != "https://api.test.rawtree.com" {
			t.Errorf("expected URL https://api.test.rawtree.com, got %s", result.URL)
		}
		if result.DefaultProject != "myproject" {
			t.Errorf("expected project myproject, got %s", result.DefaultProject)
		}
		if result.DefaultOrganization != "myorg" {
			t.Errorf("expected org myorg, got %s", result.DefaultOrganization)
		}
	})

	t.Run("missing config returns empty", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		result := loadCLIConfig()
		if result.Token != "" {
			t.Errorf("expected empty token, got %s", result.Token)
		}
	})

	t.Run("invalid json returns empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		configDir := filepath.Join(tmpDir, ".config", "rtree")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("failed to create config dir: %s", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("not json"), 0600); err != nil {
			t.Fatalf("failed to write config file: %s", err)
		}

		t.Setenv("HOME", tmpDir)

		result := loadCLIConfig()
		if result.Token != "" {
			t.Errorf("expected empty token, got %s", result.Token)
		}
	})
}

// State written before RawTree removed projects still has organization and
// project. The same-version upgrade must drop them instead of failing.
func TestUpgradeStateDropsOrganizationAndProject(t *testing.T) {
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]string{
		"rawtree_waf_ingestion":        `{"id":"x","table":"t","web_acl_arn":"a","region":"us-east-1","organization":"acme","project":"prod","endpoint_url":"https://api.us-east-1.aws.rawtree.com/v1/acme/prod/tables/t?transform=firehose"}`,
		"rawtree_cloudfront_ingestion": `{"id":"x","table":"t","distribution_id":"d","region":"us-east-1","organization":"acme","project":"prod"}`,
		"rawtree_s3_ingestion":         `{"id":"x","table":"t","bucket":"b","format":"json","region":"us-east-1","organization":"acme","project":"prod"}`,
	}
	for typeName, raw := range old {
		resp, err := server.UpgradeResourceState(context.Background(), &tfprotov6.UpgradeResourceStateRequest{
			TypeName: typeName,
			Version:  0,
			RawState: &tfprotov6.RawState{JSON: []byte(raw)},
		})
		if err != nil {
			t.Fatalf("%s: %v", typeName, err)
		}
		for _, d := range resp.Diagnostics {
			t.Errorf("%s: %s: %s", typeName, d.Summary, d.Detail)
		}
	}
}

func TestSplitCLIToken(t *testing.T) {
	if k, a := splitCLIToken("rt_abc"); k != "rt_abc" || a != "" {
		t.Errorf("API key: %q %q", k, a)
	}
	// A user JWT from `rtree login` must not be sent as an API key.
	if k, a := splitCLIToken("eyJhbGciOi.x.y"); k != "" || a != "eyJhbGciOi.x.y" {
		t.Errorf("user token: %q %q", k, a)
	}
}
