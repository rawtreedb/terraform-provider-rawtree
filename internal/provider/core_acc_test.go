package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// accTTL reads a typed value: the API rejects a TTL on a bare Dynamic column.
const accTTL = "toDateTime(ifNull(dynamicElement(timestamp, 'Int64'), 0))"

// accCoreConfig declares the four resources that need only an API key.
func accCoreConfig(name, sortingKey, ttl, ttlTimeout, description string) string {
	if ttl != "" {
		ttl = fmt.Sprintf("ttl = %q", ttl)
	}
	return fmt.Sprintf(`
resource "rawtree_database" "test" {
  name = %[1]q
}

resource "rawtree_table" "test" {
  database      = rawtree_database.test.name
  name          = "events"
  sorting_key   = %[2]q
  %[3]s
  settings_json = jsonencode({ merge_with_ttl_timeout = %[4]s })
}

resource "rawtree_api_key" "test" {
  name       = %[1]q
  database   = rawtree_database.test.name
  permission = "read_only"
}

resource "rawtree_workflow" "test" {
  name        = %[1]q
  database    = rawtree_database.test.name
  description = %[5]q
  sql         = "SELECT 1 AS n"

  schedule = {
    cron    = "*/5 * * * *"
    enabled = false
  }

  sinks = [
    { table = { database = rawtree_database.test.name, table = rawtree_table.test.name } },
    { http = { url = "https://example.com", headers = { Authorization = "Bearer x" } } },
  ]
}
`, name, sortingKey, ttl, ttlTimeout, description)
}

func TestAccCoreResources(t *testing.T) {
	name := "acc_test_" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: accCoreConfig(name, "region, toStartOfHour(timestamp)", accTTL+" + INTERVAL 30 DAY", "14400", "first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_table.test", "id", name+"/events"),
					resource.TestCheckResourceAttrSet("rawtree_table.test", "stored_sorting_key"),
					resource.TestCheckResourceAttrSet("rawtree_api_key.test", "token"),
					resource.TestCheckResourceAttrSet("rawtree_workflow.test", "sinks.1.id"),
				),
			},
			// All three table fields change in one apply.
			{
				Config: accCoreConfig(name, "region", accTTL+" + INTERVAL 7 DAY", "7200", "second"),
				Check:  resource.TestCheckResourceAttr("rawtree_workflow.test", "description", "second"),
			},
			{ResourceName: "rawtree_database.test", ImportState: true, ImportStateVerify: true},
			{
				ResourceName:            "rawtree_table.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"settings_json", "sorting_key", "ttl"},
			},
			{
				Config:      accCoreConfig(name, "region", "", "7200", "second"),
				ExpectError: regexp.MustCompile("TTL cannot be removed"),
			},
		},
	})
}

func TestAccDatabaseDoesNotAdoptExisting(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `resource "rawtree_database" "test" { name = "default" }`,
			ExpectError: regexp.MustCompile("Database already exists"),
		}},
	})
}
