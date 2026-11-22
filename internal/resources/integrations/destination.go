package integrations

import (
	"context"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

// Destination reads configuration because an unconfigured Optional+Computed
// attribute in the proposed plan can still contain the previous state's value.
func Destination(ctx context.Context, config tfsdk.Config, c *client.RawtreeClient) (org, database types.String, diags diag.Diagnostics) {
	var project types.String
	diags.Append(config.GetAttribute(ctx, path.Root("organization"), &org)...)
	diags.Append(config.GetAttribute(ctx, path.Root("database"), &database)...)
	diags.Append(config.GetAttribute(ctx, path.Root("project"), &project)...)
	if diags.HasError() {
		return
	}
	if org.IsNull() {
		org = types.StringValue(c.Organization)
	}
	if database.IsNull() {
		database = project
	}
	if database.IsNull() {
		database = types.StringValue(c.Database)
	}
	if !org.IsUnknown() && org.ValueString() == "" {
		diags.AddError("Missing Tinybird RawTree organization", "Set organization on the integration or provider, or set RAWTREE_ORG.")
	}
	if !database.IsUnknown() && database.ValueString() == "" {
		diags.AddError("Missing Tinybird RawTree database", "Set database on the integration or provider, or set RAWTREE_DATABASE.")
	}
	return
}

func TableEndpoint(origin, database, table string, query url.Values) string {
	if query == nil {
		query = url.Values{}
	}
	query.Set("database", database)
	return strings.TrimRight(origin, "/") + "/v1/tables/" + url.PathEscape(table) + "?" + query.Encode()
}
