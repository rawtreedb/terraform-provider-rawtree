package supabase_cdc_ingestion_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/jackc/pgx/v5"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/provider"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

const testRegion = "us-east-1"

// testAccPreCheck validates required env vars are set before running acc tests.
//
// Required:
//
//	RAWTREE_API_KEY, RAWTREE_ORG, RAWTREE_PROJECT
//	RAWTREE_TEST_SUPABASE_DATABASE_URL   Postgres connection string (any reachable
//	                                     value works — the worker will try to
//	                                     connect but the resource itself completes
//	                                     as soon as the ECS service is created).
//	RAWTREE_TEST_SUPABASE_SUBNETS        Comma-separated subnet IDs in the test
//	                                     region. Must allow ECS Fargate ENIs.
//
// Optional:
//
//	RAWTREE_TEST_SUPABASE_SECURITY_GROUPS  Comma-separated security group IDs.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	required := []string{
		"RAWTREE_API_KEY",
		"RAWTREE_ORG",
		"RAWTREE_PROJECT",
		"RAWTREE_TEST_SUPABASE_DATABASE_URL",
		"RAWTREE_TEST_SUPABASE_SUBNETS",
	}
	for _, name := range required {
		if v := os.Getenv(name); v == "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}

	if err := ensureECSServiceLinkedRole(context.Background()); err != nil {
		t.Fatalf("ensuring ECS service-linked role: %s", err)
	}
}

// ensureECSServiceLinkedRole makes sure the AWSServiceRoleForECS service-linked
// role exists in the account. ECS Fargate's RunTask and CreateService cannot
// assume it if it's missing, which surfaces as a confusing
// "Unable to assume the service linked role" error. The SLR is an account-wide
// singleton — creating it is idempotent and a no-op on subsequent runs.
//
// AWS normally auto-creates this role the first time you provision an ECS
// service via the console, but accounts that have only ever used the API can
// be missing it. We create it explicitly to avoid making test runs depend on
// out-of-band setup.
func ensureECSServiceLinkedRole(ctx context.Context) error {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	iamClient := iam.NewFromConfig(awsCfg)

	_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String("AWSServiceRoleForECS"),
	})
	if err == nil {
		return nil
	}
	var notFound *iamtypes.NoSuchEntityException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("checking AWSServiceRoleForECS: %w", err)
	}

	_, err = iamClient.CreateServiceLinkedRole(ctx, &iam.CreateServiceLinkedRoleInput{
		AWSServiceName: aws.String("ecs.amazonaws.com"),
	})
	if err != nil {
		return fmt.Errorf("creating AWSServiceRoleForECS: %w", err)
	}
	// IAM propagation: ECS can fail to assume the role for a few seconds after
	// CreateServiceLinkedRole returns.
	time.Sleep(15 * time.Second)
	return nil
}

func TestAccSupabaseCDCIngestion_basic(t *testing.T) {
	name := fmt.Sprintf("acc-%s", strings.ToLower(acctest.RandString(8)))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSupabaseCDCIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSupabaseCDCIngestionConfig(name, 512, 1024, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "name", name),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "region", testRegion),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "cpu", "512"),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "memory", "1024"),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "run_initialization_task", "false"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "id"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "cluster_arn"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "service_arn"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "task_definition_arn"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "log_group_name"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "execution_role_arn"),
					resource.TestCheckResourceAttrSet("rawtree_supabase_cdc_ingestion.test", "config_secret_arn"),
					testAccCheckSupabaseCDCIngestionExists("rawtree_supabase_cdc_ingestion.test"),
				),
			},
		},
	})
}

// TestAccSupabaseCDCIngestion_updateRegistersNewTaskDef verifies that changing
// an in-place attribute (cpu/memory) registers a new task definition revision
// and updates the service.
func TestAccSupabaseCDCIngestion_updateRegistersNewTaskDef(t *testing.T) {
	name := fmt.Sprintf("acc-%s", strings.ToLower(acctest.RandString(8)))

	var firstTaskDefARN string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSupabaseCDCIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSupabaseCDCIngestionConfig(name, 512, 1024, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSupabaseCDCIngestionExists("rawtree_supabase_cdc_ingestion.test"),
					captureAttr("rawtree_supabase_cdc_ingestion.test", "task_definition_arn", &firstTaskDefARN),
				),
			},
			{
				Config: testAccSupabaseCDCIngestionConfig(name, 1024, 2048, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "cpu", "1024"),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "memory", "2048"),
					assertAttrChanged("rawtree_supabase_cdc_ingestion.test", "task_definition_arn", &firstTaskDefARN),
				),
			},
		},
	})
}

// TestAccSupabaseCDCIngestion_adoptsExistingCluster pre-creates an ECS cluster
// with the name the resource will compute, then runs Create. This exercises the
// idempotency fix in createCluster: rather than failing on the existing cluster
// (or duplicating it), the resource should adopt it and store its ARN in state.
// On destroy, the adopted cluster should still be cleaned up.
func TestAccSupabaseCDCIngestion_adoptsExistingCluster(t *testing.T) {
	// resource.Test gates on TF_ACC internally, but this test does AWS work
	// (preCreateECSCluster) before entering it, so we have to gate here too.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	name := fmt.Sprintf("acc-%s", strings.ToLower(acctest.RandString(8)))

	clusterName := computeClusterName(t, name)
	preCreatedARN := preCreateECSCluster(t, clusterName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSupabaseCDCIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSupabaseCDCIngestionConfig(name, 512, 1024, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSupabaseCDCIngestionExists("rawtree_supabase_cdc_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_supabase_cdc_ingestion.test", "cluster_arn", preCreatedARN),
				),
			},
		},
	})
}

// testAccCheckSupabaseCDCIngestionExists verifies the AWS resources backing the
// Terraform resource were actually created.
func testAccCheckSupabaseCDCIngestionExists(resourceName string) resource.TestCheckFunc {
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

		clusterARN := rs.Primary.Attributes["cluster_arn"]
		serviceARN := rs.Primary.Attributes["service_arn"]
		if clusterARN == "" || serviceARN == "" {
			return fmt.Errorf("cluster_arn or service_arn not set in state")
		}

		ecsClient := ecs.NewFromConfig(awsCfg)
		clusters, err := ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: []string{clusterARN},
		})
		if err != nil || len(clusters.Clusters) == 0 || aws.ToString(clusters.Clusters[0].Status) != "ACTIVE" {
			return fmt.Errorf("ECS cluster %s not ACTIVE: %v", clusterARN, err)
		}

		services, err := ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  aws.String(clusterARN),
			Services: []string{serviceARN},
		})
		if err != nil || len(services.Services) == 0 || aws.ToString(services.Services[0].Status) != "ACTIVE" {
			return fmt.Errorf("ECS service %s not ACTIVE: %v", serviceARN, err)
		}

		taskDefARN := rs.Primary.Attributes["task_definition_arn"]
		if taskDefARN != "" {
			if _, err := ecsClient.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
				TaskDefinition: aws.String(taskDefARN),
			}); err != nil {
				return fmt.Errorf("task definition %s not found: %w", taskDefARN, err)
			}
		}

		logGroupName := rs.Primary.Attributes["log_group_name"]
		if logGroupName != "" {
			logsClient := cloudwatchlogs.NewFromConfig(awsCfg)
			out, err := logsClient.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
				LogGroupNamePrefix: aws.String(logGroupName),
			})
			if err != nil {
				return fmt.Errorf("describing log group %s: %w", logGroupName, err)
			}
			found := false
			for _, lg := range out.LogGroups {
				if aws.ToString(lg.LogGroupName) == logGroupName {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("log group %s not found", logGroupName)
			}
		}

		execRoleARN := rs.Primary.Attributes["execution_role_arn"]
		if execRoleARN != "" {
			iamClient := iam.NewFromConfig(awsCfg)
			roleName := arnLastSegment(execRoleARN)
			if _, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
				RoleName: aws.String(roleName),
			}); err != nil {
				return fmt.Errorf("execution role %s not found: %w", roleName, err)
			}
		}

		secretARN := rs.Primary.Attributes["config_secret_arn"]
		if secretARN != "" {
			smClient := secretsmanager.NewFromConfig(awsCfg)
			if _, err := smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
				SecretId: aws.String(secretARN),
			}); err != nil {
				return fmt.Errorf("managed config secret %s not found: %w", secretARN, err)
			}
		}

		return nil
	}
}

// testAccCheckSupabaseCDCIngestionDestroy verifies AWS resources were cleaned
// up. Secrets Manager keeps deleted secrets in a pending-deletion state by
// default, but our provider deletes with ForceDeleteWithoutRecovery=true, so
// DescribeSecret should return ResourceNotFoundException.
func testAccCheckSupabaseCDCIngestionDestroy(s *terraform.State) error {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	iamClient := iam.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)
	smClient := secretsmanager.NewFromConfig(awsCfg)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rawtree_supabase_cdc_ingestion" {
			continue
		}

		if clusterARN := rs.Primary.Attributes["cluster_arn"]; clusterARN != "" {
			out, err := ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
				Clusters: []string{clusterARN},
			})
			if err == nil {
				for _, c := range out.Clusters {
					if c.Status != nil && aws.ToString(c.Status) == "ACTIVE" {
						return fmt.Errorf("ECS cluster %s still ACTIVE after destroy", clusterARN)
					}
				}
			}
		}

		if logGroupName := rs.Primary.Attributes["log_group_name"]; logGroupName != "" {
			out, err := logsClient.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
				LogGroupNamePrefix: aws.String(logGroupName),
			})
			if err != nil {
				var notFound *logstypes.ResourceNotFoundException
				if !errors.As(err, &notFound) {
					return fmt.Errorf("describing log group %s: %w", logGroupName, err)
				}
			} else {
				for _, lg := range out.LogGroups {
					if aws.ToString(lg.LogGroupName) == logGroupName {
						return fmt.Errorf("log group %s still exists after destroy", logGroupName)
					}
				}
			}
		}

		if execRoleARN := rs.Primary.Attributes["execution_role_arn"]; execRoleARN != "" {
			roleName := arnLastSegment(execRoleARN)
			_, err := iamClient.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
			if err == nil {
				return fmt.Errorf("execution role %s still exists after destroy", roleName)
			}
			var notFound *iamtypes.NoSuchEntityException
			if !errors.As(err, &notFound) {
				return fmt.Errorf("unexpected error checking execution role %s: %w", roleName, err)
			}
		}

		if secretARN := rs.Primary.Attributes["config_secret_arn"]; secretARN != "" {
			// ForceDeleteWithoutRecovery returns from DeleteSecret immediately
			// but DescribeSecret can still see the secret for a few seconds.
			// Poll briefly so we don't false-positive on the race.
			if err := waitForSecretGone(ctx, smClient, secretARN, 30*time.Second); err != nil {
				return err
			}
		}
	}

	return nil
}

// waitForSecretGone polls DescribeSecret until the secret returns
// ResourceNotFoundException or the timeout expires. ForceDeleteWithoutRecovery
// on DeleteSecret is documented to take "a few seconds" to fully propagate, so
// the destroy check needs a small grace window or it false-positives.
func waitForSecretGone(ctx context.Context, client *secretsmanager.Client, secretARN string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: aws.String(secretARN),
		})
		if err != nil {
			var notFound *secretsmanagertypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				return nil
			}
			return fmt.Errorf("unexpected error checking config secret %s: %w", secretARN, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("managed config secret %s still exists %s after destroy", secretARN, timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

// captureAttr stores a resource attribute value into dst for later comparison.
func captureAttr(resourceName, attr string, dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		*dst = rs.Primary.Attributes[attr]
		return nil
	}
}

// assertAttrChanged fails if the named attribute equals the captured value.
func assertAttrChanged(resourceName, attr string, prev *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		current := rs.Primary.Attributes[attr]
		if *prev == "" {
			return fmt.Errorf("previous value for %s.%s was not captured", resourceName, attr)
		}
		if current == *prev {
			return fmt.Errorf("expected %s.%s to change but stayed %s", resourceName, attr, current)
		}
		return nil
	}
}

// preCreateECSCluster creates an ECS cluster out-of-band and registers a
// t.Cleanup to delete it if the test fails before terraform destroy runs.
func preCreateECSCluster(t *testing.T, name string) string {
	t.Helper()

	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		t.Fatalf("loading AWS config: %s", err)
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	out, err := ecsClient.CreateCluster(ctx, &ecs.CreateClusterInput{
		ClusterName: aws.String(name),
		Tags: []ecstypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("rawtree-acc-test")},
		},
	})
	if err != nil {
		t.Fatalf("pre-creating ECS cluster %s: %s", name, err)
	}

	arn := aws.ToString(out.Cluster.ClusterArn)
	t.Cleanup(func() {
		// Safety net: if the test bailed before terraform destroy, clean the
		// cluster up so we don't leak it. Ignore errors — the test's own
		// destroy may already have deleted it.
		_, _ = ecsClient.DeleteCluster(context.Background(), &ecs.DeleteClusterInput{
			Cluster: aws.String(arn),
		})
	})
	return arn
}

// computeClusterName reproduces the cluster naming logic from namesFor() +
// util.SanitizeResourceName so the test can pre-create the exact cluster the
// resource will look for.
func computeClusterName(t *testing.T, name string) string {
	t.Helper()
	org := os.Getenv("RAWTREE_ORG")
	project := os.Getenv("RAWTREE_PROJECT")
	resourceName := util.SanitizeResourceName(fmt.Sprintf("%s-%s-%s", org, project, name))
	return fmt.Sprintf("rawtree-supabase-cdc-%s", resourceName)
}

func arnLastSegment(arn string) string {
	// IAM role ARN format: arn:aws:iam::123456789012:role/role-name
	if idx := strings.LastIndex(arn, "/"); idx >= 0 {
		return arn[idx+1:]
	}
	return arn
}

// testAccSupabaseCDCIngestionConfig builds a Terraform config block for the
// resource. extra is appended verbatim before the closing brace, allowing
// individual tests to inject attributes (e.g. environment maps).
func testAccSupabaseCDCIngestionConfig(name string, cpu, memory int, extra string) string {
	subnets := splitCSV(os.Getenv("RAWTREE_TEST_SUPABASE_SUBNETS"))
	securityGroups := splitCSV(os.Getenv("RAWTREE_TEST_SUPABASE_SECURITY_GROUPS"))
	databaseURL := os.Getenv("RAWTREE_TEST_SUPABASE_DATABASE_URL")

	// Basic tests run with run_initialization_task=false so the worker never
	// actually opens a replication connection — the publication name is just
	// a string passed through to the task definition. Any placeholder works.
	cfg := fmt.Sprintf(`
resource "rawtree_supabase_cdc_ingestion" "test" {
  name                    = %q
  region                  = %q
  publication             = "rawtree_publication"
  cpu                     = %d
  memory                  = %d
  run_initialization_task = false
  subnet_ids              = %s
  database_url            = %q
`, name, testRegion, cpu, memory, terraformList(subnets), databaseURL)

	if len(securityGroups) > 0 {
		cfg += fmt.Sprintf("  security_group_ids = %s\n", terraformList(securityGroups))
	}

	cfg += extra
	cfg += "}\n"
	return cfg
}

func terraformList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// End-to-end data validation test
//
// This test exercises the full CDC pipeline against a fixed, well-known
// dataset: the "Superstore Sales" CSV in test/supabase/. The setup steps
// (create Supabase project, create table, import CSV, create publication)
// are documented in test/supabase/README.md and prescribed exactly — the
// test code references the schema by name and the INSERT statement is
// hand-tuned for those columns, so the docs and the test must stay in sync.
//
// Verification uses a baseline + delta row count: the test reads the
// current Rawtree row count, inserts numRows new rows into Postgres, and
// polls until the count grows by ≥ numRows. This avoids requiring any
// marker column on the source table.
//
// Required env vars (beyond RAWTREE_API_KEY/ORG/PROJECT/URL and AWS creds):
//
//	RAWTREE_TEST_SUPABASE_DATABASE_URL
//	    Postgres URL the CDC worker connects to. For Supabase, this is the
//	    direct (IPv6) URL — typically:
//	        postgres://postgres:<pw>@db.<ref>.supabase.co:5432/postgres?sslmode=require
//	    See test/supabase/README.md for the exact value.
//
// Optional:
//
//	RAWTREE_TEST_SUPABASE_INSERT_URL   IPv4-reachable PG URL used by the
//	    test process to INSERT the marker rows. Use Supabase's session
//	    pooler URL when your laptop has no IPv6 route. Falls back to
//	    DATABASE_URL.
//
//	RAWTREE_TEST_SUPABASE_TLS_ROOT_CERT_PATH   path to the Supabase CA PEM
//	    (download from the Supabase dashboard). Required in practice
//	    because Supabase's CA isn't in Mozilla's bundle.
//
//	RAWTREE_E2E_TIMEOUT   how long to wait for rows to appear in Rawtree.
//	    Default "15m".
// ---------------------------------------------------------------------------

// Schema constants. The setup at test/supabase/README.md walks through
// importing the canonical Superstore Sales CSV into a Supabase project with
// these names; the test code below references them directly.
const (
	testPGSchema      = "public"
	testPGTable       = "superstore_sales_data"
	testPGPublication = "rawtree_superstore_sales_data_publication"
	// supabase/etl destination table naming: "<schema>_<table>" with each
	// source underscore doubled to disambiguate the boundary, so
	// "public.superstore_sales_data" → "public_superstore__sales__data".
	testRawtreeTable = "public_superstore__sales__data"

	e2eInsertRows = 5
)

func testAccPreCheckE2E(t *testing.T) {
	t.Helper()

	// E2E doesn't reuse testAccPreCheck — that one requires
	// RAWTREE_TEST_SUPABASE_SUBNETS for the basic-lifecycle tests, but the
	// E2E test provisions its own IPv6-enabled VPC + subnet via the AWS
	// provider, so the env var isn't needed (and would be confusing if set).
	required := []string{
		"RAWTREE_API_KEY",
		"RAWTREE_ORG",
		"RAWTREE_PROJECT",
		"RAWTREE_TEST_SUPABASE_DATABASE_URL",
	}
	for _, name := range required {
		if v := os.Getenv(name); v == "" {
			t.Fatalf("%s must be set for the E2E acceptance test", name)
		}
	}
	if err := ensureECSServiceLinkedRole(context.Background()); err != nil {
		t.Fatalf("ensuring ECS service-linked role: %s", err)
	}
}

func TestAccSupabaseCDCIngestion_endToEndData(t *testing.T) {
	name := fmt.Sprintf("acc-e2e-%s", strings.ToLower(acctest.RandString(8)))

	// pipeline_id is intentionally NOT randomized. It determines the Postgres
	// logical replication slot name (supabase_etl_apply_<id>), and our
	// provider's Delete does not drop the slot — so a unique id per run would
	// leak a slot in Supabase every time, pinning WAL forever. Re-using the
	// schema default ("1") means successive runs share a single slot and the
	// worker resumes from the prior LSN; concurrent runs would conflict on
	// the exclusive slot, which is what we want (test serialization).

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckE2E(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		ExternalProviders: map[string]resource.ExternalProvider{
			"aws": {
				Source:            "hashicorp/aws",
				VersionConstraint: "~> 5.0",
			},
		},
		CheckDestroy: testAccCheckSupabaseCDCIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccSupabaseCDCIngestionE2EConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSupabaseCDCIngestionExists("rawtree_supabase_cdc_ingestion.test"),
					testAccCheckSupabaseE2EInsertAndValidate(e2eInsertRows),
				),
			},
		},
	})
}

// testAccCheckSupabaseE2EInsertAndValidate validates the streaming CDC path:
//  1. Reads the current row count in the Rawtree destination table (baseline).
//     If the destination doesn't exist yet, treats baseline as 0.
//  2. Inserts numRows synthetic Superstore-Sales rows into Postgres. Row IDs
//     are picked from a high range to avoid PK conflicts with the imported
//     dataset (9.8k rows) and with prior test runs.
//  3. Polls Rawtree until the count grows by ≥ numRows or e2eTimeout expires.
//
// Caveat: baseline-delta assumes nothing else is writing to the table during
// the test. For the prescribed Supabase test project (no live workload) this
// holds. Don't point this at a production database.
func testAccCheckSupabaseE2EInsertAndValidate(numRows int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout())
		defer cancel()

		// The test process (your laptop) may not have an IPv6 route to
		// Supabase's direct PG host. RAWTREE_TEST_SUPABASE_INSERT_URL lets
		// you point the test's INSERT connection at a dual-stack pooler URL
		// while the Fargate worker keeps using the IPv6-only direct URL for
		// logical replication. If unset, falls back to DATABASE_URL.
		pgURL := os.Getenv("RAWTREE_TEST_SUPABASE_INSERT_URL")
		if pgURL == "" {
			pgURL = os.Getenv("RAWTREE_TEST_SUPABASE_DATABASE_URL")
		}

		baseline, err := readRawtreeCount(ctx, testRawtreeTable)
		if err != nil {
			// Destination table may not exist yet (the worker creates it on
			// first sync). Treat as baseline=0; once the worker brings the
			// table over and our inserts land, count will pass numRows.
			baseline = 0
		}

		baseRowID := acctest.RandIntRange(1_000_000_000, 1_900_000_000)
		runTag := strings.ToLower(acctest.RandString(8))
		if err := insertSuperstoreRows(ctx, pgURL, baseRowID, runTag, numRows); err != nil {
			return fmt.Errorf("inserting test rows into %s.%s: %w", testPGSchema, testPGTable, err)
		}

		return waitForRawtreeRowsDelta(ctx, testRawtreeTable, baseline, numRows)
	}
}

// insertSuperstoreRows inserts numRows synthetic rows into the canonical
// Superstore-Sales table. The schema is fixed by test/supabase/README.md:
// column names with spaces, "Row ID" as the BIGINT primary key.
func insertSuperstoreRows(ctx context.Context, pgURL string, baseRowID int, runTag string, numRows int) error {
	conn, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		return fmt.Errorf("connecting to Postgres: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const stmt = `INSERT INTO "public"."superstore_sales_data" (
		"Row ID", "Order ID", "Order Date", "Ship Date", "Ship Mode",
		"Customer ID", "Customer Name", "Segment", "Country", "City",
		"State", "Postal Code", "Region", "Product ID", "Category",
		"Sub-Category", "Product Name", "Sales"
	) VALUES (
		$1, $2, '01/01/2026', '05/01/2026', 'Test Mode',
		'AC-00000', 'Acc Test Customer', 'Test Segment', 'United States', 'Test City',
		'Test State', 99999, 'Test Region', 'TEST-00000', 'Test Category',
		'Test Sub', 'Acceptance Test Row', 0.00
	)`

	for i := 0; i < numRows; i++ {
		rowID := baseRowID + i
		orderID := fmt.Sprintf("TEST-%s-%d", runTag, i)
		if _, err := conn.Exec(ctx, stmt, rowID, orderID); err != nil {
			return fmt.Errorf("insert row %d (Row ID=%d, Order ID=%s): %w", i, rowID, orderID, err)
		}
	}
	return nil
}

// readRawtreeCount issues a single `SELECT count() FROM <table>` against the
// Rawtree query API and returns the count.
func readRawtreeCount(ctx context.Context, rawtreeTable string) (int, error) {
	queryURL, apiKey := rawtreeQueryEndpoint()
	query := fmt.Sprintf(`SELECT count() AS cnt FROM %s`, quoteIdent(rawtreeTable))
	body := fmt.Sprintf(`{"sql":%q}`, query)
	return queryRawtreeCount(ctx, queryURL, apiKey, body)
}

// waitForRawtreeRowsDelta polls the Rawtree query API until the table's row
// count has grown by at least delta from baseline, or the context expires.
func waitForRawtreeRowsDelta(ctx context.Context, rawtreeTable string, baseline, delta int) error {
	queryURL, apiKey := rawtreeQueryEndpoint()
	query := fmt.Sprintf(`SELECT count() AS cnt FROM %s`, quoteIdent(rawtreeTable))
	body := fmt.Sprintf(`{"sql":%q}`, query)

	var lastCount int
	var lastErr error
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		count, err := queryRawtreeCount(ctx, queryURL, apiKey, body)
		if err == nil {
			lastCount = count
			if count >= baseline+delta {
				return nil
			}
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("table %s grew from %d to %d (wanted ≥ %d) when deadline hit (last error: %v)",
					rawtreeTable, baseline, lastCount, baseline+delta, lastErr)
			}
			return fmt.Errorf("table %s grew from %d to %d (wanted ≥ %d) when deadline hit",
				rawtreeTable, baseline, lastCount, baseline+delta)
		case <-ticker.C:
		}
	}
}

// rawtreeQueryEndpoint returns the query URL and API key, picking up
// RAWTREE_URL (or the production default) and the API key from env.
func rawtreeQueryEndpoint() (queryURL, apiKey string) {
	// Must match the host the CDC worker writes to — i.e. whatever value the
	// provider resolved for r.client.APIURL and injected into the container as
	// RAWTREE_API_URL. The provider falls back to api.us-east-1.aws.rawtree.com
	// (production) when RAWTREE_URL is unset, so we use the same default here.
	apiURL := os.Getenv("RAWTREE_URL")
	if apiURL == "" {
		apiURL = "https://api.us-east-1.aws.rawtree.com"
	}
	apiKey = os.Getenv("RAWTREE_API_KEY")
	org := os.Getenv("RAWTREE_ORG")
	project := os.Getenv("RAWTREE_PROJECT")
	queryURL = fmt.Sprintf("%s/v1/%s/%s/query", apiURL, org, project)
	return queryURL, apiKey
}

func queryRawtreeCount(ctx context.Context, url, apiKey, body string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("query returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if len(result.Data) == 0 {
		return 0, fmt.Errorf("no data returned")
	}

	cnt, ok := result.Data[0]["cnt"]
	if !ok {
		return 0, fmt.Errorf("missing cnt column")
	}
	switch v := cnt.(type) {
	case float64:
		return int(v), nil
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return 0, fmt.Errorf("parsing cnt %q: %w", v, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unexpected cnt type %T", cnt)
	}
}

// testAccSupabaseCDCIngestionE2EConfig builds a Terraform config that:
//
//   - provisions a throwaway dual-stack VPC + public subnet + IGW + routes so
//     the Fargate task can reach Supabase direct Postgres over IPv6 (Supabase
//     direct Postgres is typically IPv6-only) and ghcr.io over IPv4 (public
//     IPv4 is assigned per-task via assign_public_ip = true on the rawtree
//     resource);
//   - opts into running the initialization task so PG connectivity errors
//     surface during apply instead of in a CloudWatch log nobody reads;
//   - omits pipeline_id so the schema default ("1") applies — see the
//     comment in TestAccSupabaseCDCIngestion_endToEndData for the rationale.
//
// The framework's destroy phase tears the VPC stack down at the end of the
// test, so nothing persists in AWS between runs.
func testAccSupabaseCDCIngestionE2EConfig(name string) string {
	databaseURL := os.Getenv("RAWTREE_TEST_SUPABASE_DATABASE_URL")
	suffix := strings.ToLower(acctest.RandString(6))

	return fmt.Sprintf(`
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = %[1]q

  default_tags {
    tags = {
      CostGroup   = "e2e"
      Environment = "terraform-provider-rawtree"
      Purpose     = "supabase-cdc-acc-test"
    }
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

# Dual-stack VPC: IPv4 for the container image pull from ghcr.io (via IGW +
# per-task public IP), IPv6 for the Supabase direct Postgres endpoint.
resource "aws_vpc" "test" {
  cidr_block                       = "10.99.0.0/16"
  assign_generated_ipv6_cidr_block = true
  enable_dns_hostnames             = true
  enable_dns_support               = true

  tags = { Name = "rawtree-cdc-e2e-%[2]s" }
}

resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id

  tags = { Name = "rawtree-cdc-e2e-%[2]s" }
}

resource "aws_subnet" "test" {
  vpc_id                          = aws_vpc.test.id
  cidr_block                      = cidrsubnet(aws_vpc.test.cidr_block, 8, 0)
  ipv6_cidr_block                 = cidrsubnet(aws_vpc.test.ipv6_cidr_block, 8, 0)
  assign_ipv6_address_on_creation = true
  availability_zone               = data.aws_availability_zones.available.names[0]

  tags = { Name = "rawtree-cdc-e2e-%[2]s" }
}

resource "aws_route_table" "test" {
  vpc_id = aws_vpc.test.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.test.id
  }

  # IPv6 egress through the same IGW works because the task ENI gets a
  # globally routable IPv6 address from the subnet's /64. No NAT needed.
  route {
    ipv6_cidr_block = "::/0"
    gateway_id      = aws_internet_gateway.test.id
  }

  tags = { Name = "rawtree-cdc-e2e-%[2]s" }
}

resource "aws_route_table_association" "test" {
  subnet_id      = aws_subnet.test.id
  route_table_id = aws_route_table.test.id
}

resource "rawtree_supabase_cdc_ingestion" "test" {
  name        = %[3]q
  region      = %[1]q
  publication = %[4]q
  cpu         = 512
  memory      = 1024

  # Skip the standalone init task. The worker creates the replication slot
  # itself on first run (the supabase-etl binary's entrypoint doesn't take
  # an "init" subcommand — passing it as argv just runs the full pipeline,
  # which never exits and trips our 10-min timeout). The E2E test validates
  # the worker via the long-running service instead.
  run_initialization_task = false

  assign_public_ip = true
  subnet_ids       = [aws_subnet.test.id]
  database_url     = %[5]q
%[6]s}
`, testRegion, suffix, name, testPGPublication, databaseURL, tlsRootCertHCL())
}

// tlsRootCertHCL returns either an empty string (no inline CA cert — relies
// on the container's system CA bundle) or a `  tls_root_cert_pem = "..."`
// HCL fragment, depending on whether RAWTREE_TEST_SUPABASE_TLS_ROOT_CERT_PATH
// is set. Supabase direct Postgres uses a private CA that isn't in Mozilla's
// bundle, so most runs will need this set.
func tlsRootCertHCL() string {
	path := os.Getenv("RAWTREE_TEST_SUPABASE_TLS_ROOT_CERT_PATH")
	if path == "" {
		return ""
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		// Surfaces at apply time as a more useful error than a TLS handshake
		// failure later in the container.
		return fmt.Sprintf("  # failed to read RAWTREE_TEST_SUPABASE_TLS_ROOT_CERT_PATH=%q: %s\n", path, err)
	}
	// HCL heredoc keeps PEM newlines intact and avoids quoting headaches.
	return fmt.Sprintf("  tls_root_cert_pem = <<-EOT\n%s\nEOT\n", strings.TrimRight(string(pem), "\n"))
}

func e2eTimeout() time.Duration {
	if v := os.Getenv("RAWTREE_E2E_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 15 * time.Minute
}

// quoteIdent double-quotes a SQL identifier, escaping embedded double quotes.
// Safe enough for trusted-but-defensive identifier use in our test paths.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
