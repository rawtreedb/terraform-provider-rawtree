package database

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ list.ListResourceWithConfigure = (*databaseResource)(nil)

func NewListResource() list.ListResource { return &databaseResource{} }

func (r *databaseResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List databases that Terraform can manage in one Tinybird RawTree cluster.",
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{Required: true, Description: "Organization name.", Validators: scopeValidators},
			"cluster":      schema.StringAttribute{Required: true, Description: "Cluster name within the organization.", Validators: scopeValidators},
		},
	}
}

func (r *databaseResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	var scope struct {
		Organization types.String `tfsdk:"organization"`
		Cluster      types.String `tfsdk:"cluster"`
	}
	diags := req.Config.Get(ctx, &scope)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	if scope.Organization.ValueString() == "" || scope.Cluster.ValueString() == "" {
		diags.AddError("Missing database query scope", "Set the organization and cluster to known names before running the query.")
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	names, err := r.client.ListDatabases(ctx, scope.Organization.ValueString(), scope.Cluster.ValueString())
	if err != nil {
		diags.AddError("Cannot list databases", err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	stream.Results = func(yield func(list.ListResult) bool) {
		var count int64
		for _, name := range names {
			if count >= req.Limit {
				return
			}
			if !manageableName(name) {
				continue
			}
			state := databaseModel{Organization: scope.Organization, Cluster: scope.Cluster, Name: types.StringValue(name)}
			state.ID = types.StringValue(state.database().ID())
			result := req.NewListResult(ctx)
			result.DisplayName = name
			result.Diagnostics.Append(state.setIdentity(ctx, result.Identity)...)
			if req.IncludeResource {
				result.Diagnostics.Append(result.Resource.Set(ctx, &state)...)
			}
			if !yield(result) || result.Diagnostics.HasError() {
				return
			}
			count++
		}
	}
}
