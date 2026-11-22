package database_test

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

func TestAccDatabase(t *testing.T) {
	db := client.Database{
		Organization: os.Getenv("RAWTREE_TEST_ORGANIZATION"),
		Cluster:      os.Getenv("RAWTREE_TEST_CLUSTER"),
		Name:         "tfacc_" + acctest.RandStringFromCharSet(12, acctest.CharSetAlpha),
	}
	const address = "rawtree_database.test"
	config := fmt.Sprintf(`resource "rawtree_database" "test" {
		organization = %q
		cluster = %q
		name = %q
	}`, db.Organization, db.Cluster, db.Name)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			for _, key := range []string{"RAWTREE_URL", "RAWTREE_MANAGEMENT_TOKEN", "RAWTREE_TEST_ORGANIZATION", "RAWTREE_TEST_CLUSTER"} {
				if os.Getenv(key) == "" {
					t.Fatalf("Set %s to run the database acceptance test", key)
				}
			}
		},
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckResourceAttr(address, "id", db.ID())},
			{Config: config, PlanOnly: true},
			{ResourceName: address, ImportState: true, ImportStateVerify: true},
			{ResourceName: address, ImportState: true, ImportStateKind: resource.ImportBlockWithID},
		},
		CheckDestroy: func(_ *terraform.State) error {
			c := client.NewPlatformClient(os.Getenv("RAWTREE_URL"), os.Getenv("RAWTREE_MANAGEMENT_TOKEN"))
			found, err := c.DatabaseExists(context.Background(), db)
			if err != nil {
				return err
			}
			if found {
				return fmt.Errorf("database %s still exists", db.ID())
			}
			return nil
		},
	})
}
