package workflow

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

func TestWorkflowSinkUpdate(t *testing.T) {
	http := func(id, url, header string) sinkModel {
		return sinkModel{ID: types.StringValue(id), HTTP: &httpSink{URL: types.StringValue(url), Headers: map[string]types.String{"X-Fixture": types.StringValue(header)}}}
	}
	a := http("a", "https://example.com/", "one")
	b := http("b", "https://example.com/", "two")
	slack := sinkModel{ID: types.StringValue("c"), Slack: &slackSink{IntegrationID: types.StringValue("integration"), ChannelID: types.StringValue("channel")}}
	prior := []sinkModel{a, b, slack}
	if plannedSinks(prior, prior) != nil {
		t.Fatal("unchanged sinks must be omitted from PATCH")
	}
	reorder := plannedSinks([]sinkModel{slack, b, a}, prior)
	if reorder == nil || (*reorder)[0].Settings.ChannelID != "channel" || (*reorder)[1].ID != "b" || (*reorder)[2].ID != "a" {
		t.Fatal("reordering moved sink identities")
	}
	unknown := sinkModel{HTTP: &httpSink{URL: a.HTTP.URL}}
	if plannedSinks([]sinkModel{unknown}, []sinkModel{a}) != nil {
		t.Fatal("omitted headers should preserve remote credentials")
	}
	unknown.HTTP.URL = types.StringValue("https://other.example.com/")
	changed := plannedSinks([]sinkModel{unknown}, []sinkModel{a})
	if changed == nil || (*changed)[0].ID != "" || (*changed)[0].Settings.Headers != nil {
		t.Fatal("new URL inherited an old identity or credentials")
	}
	if remove := plannedSinks(nil, prior); remove == nil || len(*remove) != 0 {
		t.Fatal("removal must send an explicit empty list")
	}
}

func TestRefreshHTTPHeadersByID(t *testing.T) {
	url := "https://example.com/"
	prior := model{Sinks: []sinkModel{
		{ID: types.StringValue("a"), HTTP: &httpSink{URL: types.StringValue(url), Headers: map[string]types.String{"X-Example": types.StringValue("one")}}},
		{ID: types.StringValue("b"), HTTP: &httpSink{URL: types.StringValue(url), Headers: map[string]types.String{"X-Example": types.StringValue("two")}}},
	}}
	sinks := []client.WorkflowSink{
		{ID: "b", Type: "http", Settings: client.WorkflowSinkSettings{URL: &url, HeaderNames: []string{"x-example"}}},
		{ID: "c", Type: "http", Settings: client.WorkflowSinkSettings{URL: &url, HeaderNames: []string{"x-example"}}},
	}
	prior.readSinks(client.Workflow{WorkflowDefinition: client.WorkflowDefinition{Sinks: &sinks}}, true)
	if prior.Sinks[0].HTTP.Headers["X-Example"].ValueString() != "two" || prior.Sinks[1].HTTP.Headers != nil {
		t.Fatal("refresh associated a hidden header with the wrong sink")
	}
}
