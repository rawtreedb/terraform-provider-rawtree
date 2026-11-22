package workflow_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/provider"
)

func TestAccWorkflow(t *testing.T) {
	scope := client.WorkflowScope{Organization: os.Getenv("RAWTREE_TEST_ORGANIZATION"), Cluster: os.Getenv("RAWTREE_TEST_CLUSTER")}
	name := "tfacc-" + acctest.RandStringFromCharSet(12, acctest.CharSetAlpha)
	const address = "rawtree_workflow.test"
	config := func(name, sql, attributes string) string {
		return fmt.Sprintf(`resource "rawtree_workflow" "test" {
        organization = %q
        cluster = %q
        name = %q
        query = { database = "default", sql = %q }
        %s
    }`, scope.Organization, scope.Cluster, name, sql, attributes)
	}
	manual := config(name, "SELECT 1 AS value", "")
	paused := config(name+"-renamed", "SELECT 2 AS value", `description = "Acceptance test"
        schedule = { cron = "0 0 1 1 *", enabled = false }`)
	edited := config(name+"-renamed", "SELECT 2 AS value", "")
	scheduled := config(name+"-renamed", "SELECT 2 AS value", `schedule = { cron = "0 0 1 1 *" }`)
	withSinks := config(name+"-renamed", "SELECT 2 AS value", `sinks = [
        { http = { url = "https://example.com", headers = { "X-Fixture" = "terraform-test" } } },
        { table = { database = "default", table = "tfacc_workflow_result" } }
    ]`)
	reordered := config(name+"-renamed", "SELECT 2 AS value", `sinks = [
        { table = { database = "default", table = "tfacc_workflow_result" } },
        { http = { url = "https://example.com", headers = { "X-Fixture" = "terraform-updated" } } }
    ]`)
	var httpID, tableID string
	var id string
	c := client.NewPlatformClient(os.Getenv("RAWTREE_URL"), os.Getenv("RAWTREE_MANAGEMENT_TOKEN"))
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			for _, k := range []string{"RAWTREE_URL", "RAWTREE_MANAGEMENT_TOKEN", "RAWTREE_TEST_ORGANIZATION", "RAWTREE_TEST_CLUSTER"} {
				if os.Getenv(k) == "" {
					t.Fatalf("Set %s to run the workflow acceptance test", k)
				}
			}
		},
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: manual, Check: func(state *terraform.State) error {
				id = state.RootModule().Resources[address].Primary.Attributes["workflow_id"]
				w, err := c.GetWorkflow(context.Background(), scope, id)
				if err != nil {
					return err
				}
				if w == nil || w.Name != name || w.Query.SQL != "SELECT 1 AS value" || w.Schedule != nil {
					return fmt.Errorf("unexpected manual workflow")
				}
				return resource.TestCheckResourceAttr(address, "id", scope.Organization+"/"+scope.Cluster+"/"+id)(state)
			}},
			{Config: paused, Check: func(state *terraform.State) error {
				return resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "workflow_id", id), resource.TestCheckResourceAttr(address, "schedule.enabled", "false"), resource.TestCheckResourceAttr(address, "name", name+"-renamed"))(state)
			}},
			{Config: paused, PlanOnly: true},
			{ResourceName: address, ImportState: true, ImportStateVerify: true},
			{Config: scheduled, Check: resource.TestCheckResourceAttr(address, "schedule.enabled", "true")},
			{Config: scheduled, PlanOnly: true},
			{Config: edited, Check: func(_ *terraform.State) error {
				w, err := c.GetWorkflow(context.Background(), scope, id)
				if err != nil {
					return err
				}
				if w == nil || w.Schedule != nil || w.Description != nil || w.Query.SQL != "SELECT 2 AS value" {
					return fmt.Errorf("manual update did not clear the schedule and description")
				}
				return nil
			}},
			{Config: edited, PlanOnly: true},
			{Config: edited, PreConfig: func() {
				_, err := c.UpdateWorkflow(context.Background(), scope, id, client.WorkflowDefinition{
					Name: name + "-drift", Query: client.WorkflowQuery{Database: "default", SQL: "SELECT 99"},
				})
				if err != nil {
					t.Fatal(err)
				}
			}, Check: func(_ *terraform.State) error {
				w, err := c.GetWorkflow(context.Background(), scope, id)
				if err != nil {
					return err
				}
				if w == nil || w.Name != name+"-renamed" || w.Query.SQL != "SELECT 2 AS value" {
					return fmt.Errorf("external change was not reconciled")
				}
				return nil
			}},
			{Config: withSinks, Check: func(state *terraform.State) error {
				attrs := state.RootModule().Resources[address].Primary.Attributes
				httpID, tableID = attrs["sinks.0.id"], attrs["sinks.1.id"]
				if !client.ValidWorkflowID(httpID) || !client.ValidWorkflowID(tableID) {
					return fmt.Errorf("expected two sink UUIDs")
				}
				return nil
			}},
			{Config: reordered, Check: func(state *terraform.State) error {
				return resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "sinks.0.id", tableID), resource.TestCheckResourceAttr(address, "sinks.1.id", httpID), resource.TestCheckResourceAttr(address, "sinks.1.http.headers.X-Fixture", "terraform-updated"))(state)
			}},
			{Config: reordered, PlanOnly: true},
			{Config: edited, Check: func(_ *terraform.State) error {
				w, err := c.GetWorkflow(context.Background(), scope, id)
				if err != nil {
					return err
				}
				if w == nil || w.Sinks == nil || len(*w.Sinks) != 0 {
					return fmt.Errorf("expected all sinks removed")
				}
				return nil
			}},
		},
		CheckDestroy: func(_ *terraform.State) error {
			w, err := c.GetWorkflow(context.Background(), scope, id)
			if err != nil {
				return err
			}
			if w != nil {
				return fmt.Errorf("workflow %s still exists", id)
			}
			return nil
		},
	})
}
