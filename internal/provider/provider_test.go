package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
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

	for _, name := range []string{"rawtree_database", "rawtree_workflow", "rawtree_s3_ingestion", "rawtree_waf_ingestion", "rawtree_cloudfront_ingestion", "rawtree_supabase_cdc_ingestion"} {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("expected %s resource to be registered", name)
		}
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

func TestProviderSeparatesManagementAndIngestionCredentials(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer management-test" {
			t.Error("native request used the wrong credential")
		}
		_, _ = fmt.Fprint(w, `{"databases":[]}`)
	}))
	defer server.Close()

	p := New("test")()
	var schema tfprovider.SchemaResponse
	p.Schema(ctx, tfprovider.SchemaRequest{}, &schema)
	for _, ingestion := range []string{"", "ingestion-test"} {
		state := tfsdk.State{Schema: schema.Schema}
		diags := state.Set(ctx, RawtreeProviderModel{
			APIKey: types.StringValue(ingestion), APIURL: types.StringValue(server.URL),
			ManagementToken: types.StringValue("management-test"),
			Organization:    types.StringValue(""), Project: types.StringValue(""),
		})
		if diags.HasError() {
			t.Fatal(diags.Errors())
		}
		var resp tfprovider.ConfigureResponse
		p.Configure(ctx, tfprovider.ConfigureRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics.Errors())
		}
		clients := resp.ResourceData.(*client.Clients)
		if clients.Integrations.APIKey != ingestion {
			t.Fatal("integration inherited the management token")
		}
		if _, err := clients.Platform.DatabaseExists(ctx, client.Database{Organization: "org", Cluster: "cluster", Name: "db"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIngestionDatabasePrecedence(t *testing.T) {
	cli := cliConfig{Database: "saved", DefaultProject: "old-saved"}
	for _, tc := range []struct{ db, project, envDB, envProject, want string }{
		{"explicit", "", "env", "old-env", "explicit"},
		{"", "old-explicit", "env", "old-env", "old-explicit"},
		{"", "", "env", "old-env", "env"},
		{"", "", "", "old-env", "old-env"},
		{"", "", "", "", "saved"},
	} {
		t.Setenv("RAWTREE_DATABASE", tc.envDB)
		t.Setenv("RAWTREE_PROJECT", tc.envProject)
		cfg := RawtreeProviderModel{Database: types.StringValue(tc.db), Project: types.StringValue(tc.project)}
		if got := ingestionDatabase(cfg, cli); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	if got := ingestionDatabase(RawtreeProviderModel{}, cliConfig{DefaultProject: "legacy"}); got != "legacy" {
		t.Errorf("lost legacy saved default: %q", got)
	}
}
