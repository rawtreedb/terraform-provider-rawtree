package workflow

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type workflowModel struct {
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	Description types.String   `tfsdk:"description"`
	Database    types.String   `tfsdk:"database"`
	SQL         types.String   `tfsdk:"sql"`
	Schedule    *scheduleModel `tfsdk:"schedule"`
	Sinks       []sinkModel    `tfsdk:"sinks"`
}

type scheduleModel struct {
	Cron    types.String `tfsdk:"cron"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

type sinkModel struct {
	ID    types.String    `tfsdk:"id"`
	Table *tableSinkModel `tfsdk:"table"`
	HTTP  *httpSinkModel  `tfsdk:"http"`
	Slack *slackSinkModel `tfsdk:"slack"`
}

type tableSinkModel struct {
	Database types.String `tfsdk:"database"`
	Table    types.String `tfsdk:"table"`
}

type httpSinkModel struct {
	URL     types.String `tfsdk:"url"`
	Headers types.Map    `tfsdk:"headers"`
}

type slackSinkModel struct {
	IntegrationID types.String `tfsdk:"integration_id"`
	ChannelID     types.String `tfsdk:"channel_id"`
	ChannelName   types.String `tfsdk:"channel_name"`
}

// apiWorkflow is the WorkflowItem response.
type apiWorkflow struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description *string      `json:"description"`
	Query       apiQuery     `json:"query"`
	Schedule    *apiSchedule `json:"schedule"`
	Sinks       []apiSink    `json:"sinks"`
}

type apiQuery struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
}

type apiSchedule struct {
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
}

// apiSink is a SinkView; settings holds the fields of every sink type.
type apiSink struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Settings apiSinkSettings `json:"settings"`
}

type apiSinkSettings struct {
	Database      string   `json:"database"`
	Table         string   `json:"table"`
	URL           *string  `json:"url"`
	HeaderNames   []string `json:"header_names"`
	IntegrationID string   `json:"integration_id"`
	ChannelID     string   `json:"channel_id"`
	ChannelName   string   `json:"channel_name"`
}

func (s sinkModel) kind() string {
	switch {
	case s.Table != nil:
		return "table"
	case s.HTTP != nil:
		return "http"
	case s.Slack != nil:
		return "slack"
	}
	return ""
}
