package provider

import (
	"cmp"
	"context"
	"encoding/json"

	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/integrations/cloudfront_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/integrations/s3_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/integrations/supabase_cdc_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/integrations/waf_ingestion"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/platform/database"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/resources/platform/workflow"
)

const (
	defaultAPIURL = "https://api.rawtree.com"
	cliConfigPath = ".config/rtree/config.json"
)

var _ provider.ProviderWithListResources = &RawtreeProvider{}

type RawtreeProvider struct {
	version string
}

type RawtreeProviderModel struct {
	ManagementToken types.String `tfsdk:"management_token"`
	APIKey          types.String `tfsdk:"api_key"`
	APIURL          types.String `tfsdk:"api_url"`
	Organization    types.String `tfsdk:"organization"`
	Database        types.String `tfsdk:"database"`
	Project         types.String `tfsdk:"project"`
}

// cliConfig represents the structure of ~/.config/rtree/config.json.
type cliConfig struct {
	Token               string `json:"token"`
	URL                 string `json:"url"`
	Database            string `json:"database"`
	DefaultDatabase     string `json:"default_database"`
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
		Description: "Manage Tinybird RawTree resources and external ingestion integrations.",
		Attributes: map[string]schema.Attribute{
			"management_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bearer credential for native Tinybird RawTree resources. Can also be set with RAWTREE_MANAGEMENT_TOKEN. Integrations use their own API key.",
			},
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API key for ingestion integrations and a fallback for native resources. Set it with RAWTREE_API_KEY or the Tinybird RawTree CLI configuration.",
			},
			"api_url": schema.StringAttribute{
				Optional:    true,
				Description: "Tinybird RawTree API origin. Defaults to https://api.rawtree.com. Can also be set with RAWTREE_URL.",
			},
			"organization": schema.StringAttribute{
				Optional:    true,
				Description: "Default Tinybird RawTree organization used to name integration infrastructure. The ingestion key selects the organization and cluster for API requests. Can also be set with RAWTREE_ORG.",
			},
			"database": schema.StringAttribute{
				Optional: true, Description: "Default Tinybird RawTree destination database for ingestion integrations. Can also be set with RAWTREE_DATABASE.",
				Validators: []validator.String{stringvalidator.ConflictsWith(path.MatchRoot("project"))},
			},
			"project": schema.StringAttribute{
				DeprecationMessage: "Use database instead.",
				Optional:           true,
				Description:        "Legacy alias for database. RAWTREE_PROJECT is also supported.",
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

	cli := loadCLIConfig()

	apiKey := resolveString(config.APIKey, "RAWTREE_API_KEY", cli.Token)

	apiURL := resolveString(config.APIURL, "RAWTREE_URL", cli.URL)
	managementToken := resolveString(config.ManagementToken, "RAWTREE_MANAGEMENT_TOKEN", apiKey)
	org := resolveString(config.Organization, "RAWTREE_ORG", cli.DefaultOrganization)
	database := ingestionDatabase(config, cli)
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	if managementToken == "" {
		resp.Diagnostics.AddError("Missing Tinybird RawTree credential", "Set management_token or RAWTREE_MANAGEMENT_TOKEN for native resources, or configure an API key.")
		return
	}
	c := &client.Clients{
		Platform:     client.NewPlatformClient(apiURL, managementToken),
		Integrations: client.New(apiKey, apiURL, org, database),
	}

	resp.DataSourceData = c
	resp.ResourceData = c
	resp.ListResourceData = c
}

func (p *RawtreeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		database.NewResource,
		workflow.NewResource,
		cloudfront_ingestion.NewResource,
		s3_ingestion.NewResource,
		supabase_cdc_ingestion.NewResource,
		waf_ingestion.NewResource,
	}
}

func (p *RawtreeProvider) ListResources(_ context.Context) []func() list.ListResource {
	return []func() list.ListResource{database.NewListResource, workflow.NewListResource}
}

func (p *RawtreeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// At each source, database takes precedence over the legacy project spelling.
func ingestionDatabase(config RawtreeProviderModel, cli cliConfig) string {
	return cmp.Or(
		config.Database.ValueString(), config.Project.ValueString(),
		os.Getenv("RAWTREE_DATABASE"), os.Getenv("RAWTREE_PROJECT"),
		cli.Database, cli.DefaultDatabase, cli.DefaultProject,
	)
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
