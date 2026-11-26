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
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/supabase_cdc_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/waf_ingestion"
)

const (
	defaultAPIURL = "https://api.rawtree.com"
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
	Database     types.String `tfsdk:"database"`
	Project      types.String `tfsdk:"project"`
}

// cliConfig represents the structure of ~/.config/rtree/config.json.
type cliConfig struct {
	Token               string `json:"token"`
	URL                 string `json:"url"`
	Database            string `json:"database"`
	DefaultDatabase     string `json:"default_database"`
	DefaultOrganization string `json:"default_organization"`
	DefaultProject      string `json:"default_project"`
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
		Description: "The RawTree provider builds AWS pipelines that ingest S3, WAF, CloudFront and Supabase data into RawTree.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "RawTree API key (rt_...). The key selects its organization, cluster and default database. " +
					"Required by the ingestion resources. Can also be set via RAWTREE_API_KEY, or read from the rtree CLI config.",
			},
			"api_url": schema.StringAttribute{
				Optional:    true,
				Description: fmt.Sprintf("RawTree API URL. Defaults to %s. Can also be set via RAWTREE_API_URL.", defaultAPIURL),
			},
			"organization": schema.StringAttribute{
				Optional:    true,
				Description: "RawTree organization name. Only passed to the rawtree_supabase_cdc_ingestion worker. Can also be set via RAWTREE_ORG.",
			},
			"database": schema.StringAttribute{
				Optional:    true,
				Description: "Default RawTree database for ingestion. Defaults to the API key's database. Can also be set via RAWTREE_DATABASE.",
			},
			"project": schema.StringAttribute{
				Optional:           true,
				DeprecationMessage: "RawTree no longer has projects. Only rawtree_supabase_cdc_ingestion reads this value.",
				Description:        "Deprecated. Only passed to the rawtree_supabase_cdc_ingestion worker. Can also be set via RAWTREE_PROJECT.",
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

	// Each value resolves in this order: provider block, environment, rtree CLI config.
	cli := loadCLIConfig()

	apiKey := resolveString(config.APIKey, "RAWTREE_API_KEY", cli.Token)
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing RawTree API key",
			"Set the provider 'api_key' attribute or RAWTREE_API_KEY, or log in with the rtree CLI.",
		)
		return
	}

	// RAWTREE_URL is the old variable name; the rtree CLI uses RAWTREE_API_URL.
	apiURL := resolveString(config.APIURL, "RAWTREE_API_URL", firstNonEmpty(os.Getenv("RAWTREE_URL"), cli.URL, defaultAPIURL))

	c := client.New(
		apiKey,
		apiURL,
		resolveString(config.Organization, "RAWTREE_ORG", cli.DefaultOrganization),
		resolveString(config.Database, "RAWTREE_DATABASE", firstNonEmpty(cli.Database, cli.DefaultDatabase)),
		resolveString(config.Project, "RAWTREE_PROJECT", cli.DefaultProject),
	)

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *RawtreeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		cloudfront_ingestion.NewResource,
		s3_ingestion.NewResource,
		supabase_cdc_ingestion.NewResource,
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
