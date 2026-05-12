package mongo_connector_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/provider"
)

const testRegion = "us-east-1"

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if v := os.Getenv("RAWTREE_API_KEY"); v == "" {
		t.Fatal("RAWTREE_API_KEY must be set for acceptance tests")
	}
	if v := os.Getenv("RAWTREE_ORG"); v == "" {
		t.Fatal("RAWTREE_ORG must be set for acceptance tests")
	}
	if v := os.Getenv("RAWTREE_PROJECT"); v == "" {
		t.Fatal("RAWTREE_PROJECT must be set for acceptance tests")
	}
	if v := os.Getenv("RAWTREE_TEST_MONGO_URI"); v == "" {
		t.Fatal("RAWTREE_TEST_MONGO_URI must be set for acceptance tests (a MongoDB replica set connection string)")
	}
}

func TestAccMongoConnector_basic(t *testing.T) {
	mongoURI := os.Getenv("RAWTREE_TEST_MONGO_URI")
	tableName := fmt.Sprintf("acc_test_mongo_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMongoConnectorDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccMongoConnectorConfig(tableName, mongoURI, "testdb", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckMongoConnectorExists("rawtree_mongo_connector.test"),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "table", tableName),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "mongo_database", "testdb"),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "region", testRegion),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "table_prefix", "mongo_"),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "snapshot_enabled", "true"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "id"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "ecs_cluster_arn"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "ecs_service_arn"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "task_definition_arn"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "log_group_name"),
					resource.TestCheckResourceAttrSet("rawtree_mongo_connector.test", "secret_arn"),
				),
			},
		},
	})
}

func TestAccMongoConnector_withCollections(t *testing.T) {
	mongoURI := os.Getenv("RAWTREE_TEST_MONGO_URI")
	tableName := fmt.Sprintf("acc_test_mongo_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMongoConnectorDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccMongoConnectorConfig(tableName, mongoURI, "testdb", "orders,users"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckMongoConnectorExists("rawtree_mongo_connector.test"),
					resource.TestCheckResourceAttr("rawtree_mongo_connector.test", "collections", "orders,users"),
				),
			},
		},
	})
}

func testAccCheckMongoConnectorExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no ID set for %s", resourceName)
		}

		ctx := context.Background()
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
		if err != nil {
			return fmt.Errorf("loading AWS config: %w", err)
		}

		// Verify ECS service exists.
		ecsClient := ecs.NewFromConfig(awsCfg)
		clusterARN := rs.Primary.Attributes["ecs_cluster_arn"]
		serviceARN := rs.Primary.Attributes["ecs_service_arn"]
		if clusterARN != "" && serviceARN != "" {
			out, err := ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
				Cluster:  &clusterARN,
				Services: []string{serviceARN},
			})
			if err != nil {
				return fmt.Errorf("ECS service not found: %w", err)
			}
			if len(out.Services) == 0 {
				return fmt.Errorf("ECS service %s not found", serviceARN)
			}
		}

		// Verify secret exists.
		secretARN := rs.Primary.Attributes["secret_arn"]
		if secretARN != "" {
			smClient := secretsmanager.NewFromConfig(awsCfg)
			_, err := smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
				SecretId: &secretARN,
			})
			if err != nil {
				return fmt.Errorf("secret %s not found: %w", secretARN, err)
			}
		}

		return nil
	}
}

func testAccCheckMongoConnectorDestroy(s *terraform.State) error {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rawtree_mongo_connector" {
			continue
		}

		// Check ECS cluster is gone.
		ecsClient := ecs.NewFromConfig(awsCfg)
		clusterARN := rs.Primary.Attributes["ecs_cluster_arn"]
		if clusterARN != "" {
			out, err := ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
				Clusters: []string{clusterARN},
			})
			if err == nil && len(out.Clusters) > 0 && *out.Clusters[0].Status != "INACTIVE" {
				return fmt.Errorf("ECS cluster %s still exists after destroy", clusterARN)
			}
		}

		// Check IAM role is gone.
		iamClient := iam.NewFromConfig(awsCfg)
		roleName := fmt.Sprintf("rawtree-mongo-task-%s", rs.Primary.ID)
		_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
			RoleName: &roleName,
		})
		if err == nil {
			return fmt.Errorf("IAM role %s still exists after destroy", roleName)
		}

		// Check secret is gone.
		secretARN := rs.Primary.Attributes["secret_arn"]
		if secretARN != "" {
			smClient := secretsmanager.NewFromConfig(awsCfg)
			_, err := smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
				SecretId: &secretARN,
			})
			if err == nil {
				return fmt.Errorf("secret %s still exists after destroy", secretARN)
			}
		}
	}

	return nil
}

func testAccMongoConnectorConfig(table, mongoURI, mongoDatabase, collections string) string {
	cfg := fmt.Sprintf(`
resource "rawtree_mongo_connector" "test" {
  table          = %q
  mongo_uri      = %q
  mongo_database = %q
  region         = %q
`, table, mongoURI, mongoDatabase, testRegion)

	if collections != "" {
		cfg += fmt.Sprintf("  collections = %q\n", collections)
	}

	cfg += "}\n"
	return cfg
}
