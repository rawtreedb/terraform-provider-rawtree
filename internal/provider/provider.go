package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/cloudfront_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/s3_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/waf_ingestion"
)

const (
	defaultAPIURL = "https://api.us-east-1.aws.rawtree.com"
	cliConfigPath = ".config/rtree/config.json"
)

var _ provider.Provider = &RawtreeProvider{}

type RawtreeProvider struct {
	version string
}

type RawtreeProviderModel struct {
	APIKey       types.String `tfsdk:"api_key"`
	APIURL       types.String `tfsdk:"api_url"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`
}

// cliConfig represents the structure of ~/.config/rtree/config.json.
type cliConfig struct {
	Token               string `json:"token"`
	URL                 string `json:"url"`
	DefaultProject      string `json:"default_project"`
	DefaultOrganization string `json:"default_organization"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &RawtreeProvider{
			version: version,
		}
	}
}

func (p *RawtreeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "rawtree"
	resp.Version = p.version
}

func (p *RawtreeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Rawtree provider enables automated data ingestion into Rawtree from AWS sources (S3, WAF logs, CloudFront real-time logs).",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Rawtree API key (rw_...). Can also be set via RAWTREE_API_KEY env var, or auto-detected from rtree CLI config.",
			},
			"api_url": schema.StringAttribute{
				Optional:    true,
				Description: fmt.Sprintf("Rawtree API URL. Defaults to %s. Can also be set via RAWTREE_URL env var.", defaultAPIURL),
			},
			"organization": schema.StringAttribute{
				Optional:    true,
				Description: "Rawtree organization name. Can also be set via RAWTREE_ORG env var.",
			},
			"project": schema.StringAttribute{
				Optional:    true,
				Description: "Rawtree project name. Can also be set via RAWTREE_PROJECT env var.",
			},
		},
	}
}

func (p *RawtreeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config RawtreeProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Load CLI config as fallback.
	cli := loadCLIConfig()

	// Resolve API key: provider → env → CLI config
	apiKey := resolveString(config.APIKey, "RAWTREE_API_KEY", cli.Token)
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API Key",
			"A Rawtree API key must be provided via the provider 'api_key' attribute, "+
				"the RAWTREE_API_KEY environment variable, or by logging in with the rtree CLI.",
		)
		return
	}

	// Resolve API URL: provider → env → CLI config → default
	apiURL := resolveString(config.APIURL, "RAWTREE_URL", cli.URL)
	if apiURL == "" {
		apiURL = defaultAPIURL
	}

	// Resolve organization: provider → env → CLI config
	org := resolveString(config.Organization, "RAWTREE_ORG", cli.DefaultOrganization)
	if org == "" {
		resp.Diagnostics.AddError(
			"Missing Organization",
			"A Rawtree organization must be provided via the provider 'organization' attribute, "+
				"the RAWTREE_ORG environment variable, or by setting a default in the rtree CLI.",
		)
		return
	}

	// Resolve project: provider → env → CLI config
	project := resolveString(config.Project, "RAWTREE_PROJECT", cli.DefaultProject)
	if project == "" {
		resp.Diagnostics.AddError(
			"Missing Project",
			"A Rawtree project must be provided via the provider 'project' attribute, "+
				"the RAWTREE_PROJECT environment variable, or by setting a default in the rtree CLI.",
		)
		return
	}

	c := client.New(apiKey, apiURL, org, project)

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *RawtreeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		cloudfront_ingestion.NewResource,
		s3_ingestion.NewResource,
		waf_ingestion.NewResource,
	}
}

func (p *RawtreeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// resolveString returns the first non-empty value from: TF config, env var, fallback.
func resolveString(tfValue types.String, envVar string, fallback string) string {
	if !tfValue.IsNull() && !tfValue.IsUnknown() {
		return tfValue.ValueString()
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return fallback
}

// loadCLIConfig reads the rtree CLI configuration file.
func loadCLIConfig() cliConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return cliConfig{}
	}

	data, err := os.ReadFile(filepath.Join(home, cliConfigPath))
	if err != nil {
		return cliConfig{}
	}

	var cfg cliConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cliConfig{}
	}

	return cfg
}
