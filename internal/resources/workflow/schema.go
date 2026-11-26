package workflow

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var sinkKinds = []string{"table", "http", "slack"}

func resourceSchema() schema.Schema {
	oneSinkKind := objectvalidator.ExactlyOneOf(
		path.MatchRelative().AtParent().AtName("table"),
		path.MatchRelative().AtParent().AtName("http"),
		path.MatchRelative().AtParent().AtName("slack"),
	)
	return schema.Schema{
		Description: "Manages a RawTree workflow: a SQL query that runs on a schedule (or manually) and delivers its rows to sinks.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The workflow ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The workflow name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form description, up to 1000 characters.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 1000)},
			},
			"database": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "The database the SQL runs in. Defaults to the provider database.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"sql": schema.StringAttribute{
				Required:    true,
				Description: "The SQL query the workflow runs.",
			},
			"schedule": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Automatic execution. Omit for a manual-only workflow.",
				Attributes: map[string]schema.Attribute{
					"cron": schema.StringAttribute{
						Required:    true,
						Description: "UTC cron expression, 5 fields or 6 with seconds (for example `*/30 * * * * *`).",
					},
					"enabled": schema.BoolAttribute{
						Optional:    true,
						Computed:    true,
						Default:     booldefault.StaticBool(true),
						Description: "Set to false to pause scheduled runs. Default: true.",
					},
				},
			},
			"sinks": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Where the workflow delivers its rows, at most five. Each sink sets exactly one of `table`, `http` or `slack`.",
				Validators:  []validator.List{listvalidator.SizeAtMost(5)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:      true,
							Description:   "The sink ID.",
							PlanModifiers: []planmodifier.String{sinkIDModifier{}},
						},
						"table": schema.SingleNestedAttribute{
							Optional:    true,
							Description: "Insert the rows into a RawTree table.",
							Validators:  []validator.Object{oneSinkKind},
							Attributes: map[string]schema.Attribute{
								"database": schema.StringAttribute{Required: true, Description: "Target database."},
								"table":    schema.StringAttribute{Required: true, Description: "Target table."},
							},
						},
						"http": schema.SingleNestedAttribute{
							Optional:    true,
							Description: "POST the rows to an HTTP endpoint.",
							Attributes: map[string]schema.Attribute{
								"url": schema.StringAttribute{
									Required:    true,
									Sensitive:   true,
									Description: "HTTP or HTTPS URL.",
								},
								"headers": schema.MapAttribute{
									Optional:    true,
									Sensitive:   true,
									ElementType: types.StringType,
									Description: "Request headers. The API never returns header values, so only header name changes are detected as drift.",
									Validators:  []validator.Map{mapvalidator.SizeAtMost(20)},
								},
							},
						},
						"slack": schema.SingleNestedAttribute{
							Optional:    true,
							Description: "Post the rows to a Slack channel.",
							Attributes: map[string]schema.Attribute{
								"integration_id": schema.StringAttribute{
									Required:    true,
									Description: "ID of a Slack integration connected to the organization (`rtree integration list`).",
								},
								"channel_id": schema.StringAttribute{
									Optional:    true,
									Computed:    true,
									Description: "Slack channel ID, such as `C0123456789`. Set this or `channel_name`.",
									Validators: []validator.String{stringvalidator.ExactlyOneOf(
										path.MatchRelative().AtParent().AtName("channel_name"),
									)},
								},
								"channel_name": schema.StringAttribute{
									Optional:    true,
									Computed:    true,
									Description: "Slack channel name, such as `alerts` or `#alerts`. The API resolves it to an ID when the sink is saved.",
								},
							},
						},
					},
				},
			},
		},
	}
}

// sinkIDModifier keeps the sink ID of the state element at the same index
// when it has the same kind, so the API updates that sink in place.
type sinkIDModifier struct{}

func (sinkIDModifier) Description(context.Context) string {
	return "Keeps the sink ID when the sink at this position keeps its kind."
}

func (m sinkIDModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (sinkIDModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}
	parent := req.Path.ParentPath()
	for _, kind := range sinkKinds {
		var planned, prior types.Object
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, parent.AtName(kind), &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, parent.AtName(kind), &prior)...)
		if resp.Diagnostics.HasError() || planned.IsNull() != prior.IsNull() {
			return
		}
	}
	resp.PlanValue = req.StateValue
}
