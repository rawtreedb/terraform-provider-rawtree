package workflow

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var _ list.ListResourceWithConfigure = (*workflowResource)(nil)

func NewListResource() list.ListResource { return &workflowResource{} }

func (r *workflowResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List workflows that Terraform can manage in one Tinybird RawTree cluster.",
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{Required: true, Description: "Organization name.", Validators: scopeValidators},
			"cluster":      schema.StringAttribute{Required: true, Description: "Cluster name within the organization.", Validators: scopeValidators},
		},
	}
}

func (r *workflowResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
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
		diags.AddError("Missing workflow query scope", "Set the organization and cluster to known names before running the query.")
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	workflows, err := r.client.ListWorkflows(ctx, client.WorkflowScope{Organization: scope.Organization.ValueString(), Cluster: scope.Cluster.ValueString()})
	if err != nil {
		diags.AddError("Cannot list workflows", err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	stream.Results = func(yield func(list.ListResult) bool) {
		for i, w := range workflows {
			if int64(i) >= req.Limit {
				return
			}
			state := model{Organization: scope.Organization, Cluster: scope.Cluster}
			state.setRemote(w, true)
			result := req.NewListResult(ctx)
			result.DisplayName = w.Name
			result.Diagnostics.Append(result.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
			if req.IncludeResource {
				result.Diagnostics.Append(result.Resource.Set(ctx, &state)...)
			}
			if !yield(result) || result.Diagnostics.HasError() {
				return
			}
		}
	}
}
