package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

type WorkflowScope struct{ Organization, Cluster string }

func (s WorkflowScope) params() url.Values {
	return url.Values{"organization": {s.Organization}, "cluster": {s.Cluster}}
}

type WorkflowQuery struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
}

type WorkflowSchedule struct {
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
}

type WorkflowDefinition struct {
	Name        string        `json:"name"`
	Description *string       `json:"description"`
	Query       WorkflowQuery `json:"query"`
	// An explicit null disables automatic runs. Omitting this field on create schedules a run every second.
	Schedule *WorkflowSchedule `json:"schedule"`
	Sinks    *[]WorkflowSink   `json:"sinks,omitempty"`
}

// Sink settings are typed by the API discriminator. Header values are accepted only on writes.
type WorkflowSinkSettings struct {
	Database      string              `json:"database,omitempty"`
	Table         string              `json:"table,omitempty"`
	URL           *string             `json:"url,omitempty"`
	Headers       *map[string]*string `json:"headers,omitempty"`
	HeaderNames   []string            `json:"header_names,omitempty"`
	IntegrationID string              `json:"integration_id,omitempty"`
	ChannelID     string              `json:"channel_id,omitempty"`
}

type WorkflowSink struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type"`
	Settings WorkflowSinkSettings `json:"settings"`
}

type Workflow struct {
	ID string `json:"id"`
	WorkflowDefinition
}

var workflowUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidWorkflowID(id string) bool { return workflowUUID.MatchString(id) }

func (w Workflow) validate(expectedID string) error {
	if !ValidWorkflowID(w.ID) || (expectedID != "" && w.ID != expectedID) {
		return fmt.Errorf("unexpected workflow ID from Tinybird RawTree")
	}
	if w.Name == "" || w.Query.Database == "" || w.Query.SQL == "" ||
		w.Sinks == nil || (w.Schedule != nil && w.Schedule.Cron == "") {
		return fmt.Errorf("invalid workflow response from Tinybird RawTree")
	}
	for _, sink := range *w.Sinks {
		if !ValidWorkflowID(sink.ID) {
			return fmt.Errorf("invalid sink ID in workflow response")
		}
		switch sink.Type {
		case "table":
			if sink.Settings.Database == "" || sink.Settings.Table == "" {
				return fmt.Errorf("incomplete table sink in workflow response")
			}
		case "http":
			// A null URL is possible when the service cannot decrypt this sink.
			if sink.Settings.HeaderNames == nil {
				return fmt.Errorf("incomplete HTTP sink in workflow response")
			}
		case "slack":
			if sink.Settings.IntegrationID == "" || sink.Settings.ChannelID == "" {
				return fmt.Errorf("incomplete Slack sink in workflow response")
			}
		default:
			return fmt.Errorf("unsupported workflow sink type %q", sink.Type)
		}
	}
	return nil
}

func workflowMissing(err error) bool {
	var problem *platformError
	return errors.As(err, &problem) && problem.status == http.StatusNotFound && problem.message == "Workflow not found."
}

func (c *PlatformClient) workflowURL(id string) string {
	endpoint := c.baseURL + "/v1/workflows"
	if id != "" {
		endpoint += "/" + url.PathEscape(id)
	}
	return endpoint
}

func (c *PlatformClient) ListWorkflows(ctx context.Context, scope WorkflowScope) ([]Workflow, error) {
	var result struct {
		Workflows []Workflow `json:"workflows"`
	}
	if err := c.platformRequest(ctx, http.MethodGet, c.workflowURL(""), scope.params(), nil, http.StatusOK, &result); err != nil {
		return nil, err
	}
	if result.Workflows == nil {
		return nil, fmt.Errorf("incomplete workflow list from Tinybird RawTree")
	}
	for _, workflow := range result.Workflows {
		if err := workflow.validate(""); err != nil {
			return nil, err
		}
	}
	return result.Workflows, nil
}

func (c *PlatformClient) GetWorkflow(ctx context.Context, scope WorkflowScope, id string) (*Workflow, error) {
	var result Workflow
	err := c.platformRequest(ctx, http.MethodGet, c.workflowURL(id), scope.params(), nil, http.StatusOK, &result)
	if workflowMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, result.validate(id)
}

func (c *PlatformClient) CreateWorkflow(ctx context.Context, scope WorkflowScope, def WorkflowDefinition) (*Workflow, error) {
	return c.saveWorkflow(ctx, scope, "", http.MethodPost, http.StatusCreated, def)
}

func (c *PlatformClient) UpdateWorkflow(ctx context.Context, scope WorkflowScope, id string, def WorkflowDefinition) (*Workflow, error) {
	return c.saveWorkflow(ctx, scope, id, http.MethodPatch, http.StatusOK, def)
}

func (c *PlatformClient) saveWorkflow(ctx context.Context, scope WorkflowScope, id, method string, status int, def WorkflowDefinition) (*Workflow, error) {
	body, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	var result Workflow
	if err := c.platformRequest(ctx, method, c.workflowURL(id), scope.params(), body, status, &result); err != nil {
		return nil, err
	}
	return &result, result.validate(id)
}

func (c *PlatformClient) DeleteWorkflow(ctx context.Context, scope WorkflowScope, id string) error {
	err := c.platformRequest(ctx, http.MethodDelete, c.workflowURL(id), scope.params(), nil, http.StatusNoContent, nil)
	if workflowMissing(err) {
		return nil
	}
	return err
}
