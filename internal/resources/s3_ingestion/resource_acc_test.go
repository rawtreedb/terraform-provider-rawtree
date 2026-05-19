package s3_ingestion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/provider"
)

const testRegion = "us-east-1"

// testAccPreCheck validates required env vars are set before running acc tests.
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
	if v := os.Getenv("RAWTREE_TEST_S3_BUCKET"); v == "" {
		t.Fatal("RAWTREE_TEST_S3_BUCKET must be set for acceptance tests (an existing S3 bucket with test data)")
	}
}

func TestAccS3Ingestion_basic(t *testing.T) {
	bucket := os.Getenv("RAWTREE_TEST_S3_BUCKET")
	tableName := fmt.Sprintf("acc_test_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckS3IngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccS3IngestionConfig(tableName, bucket, "json", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckS3IngestionExists("rawtree_s3_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "table", tableName),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "bucket", bucket),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "format", "json"),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "region", testRegion),
					resource.TestCheckResourceAttrSet("rawtree_s3_ingestion.test", "id"),
					resource.TestCheckResourceAttrSet("rawtree_s3_ingestion.test", "glue_job_name"),
					resource.TestCheckResourceAttrSet("rawtree_s3_ingestion.test", "glue_job_run_id"),
					resource.TestCheckResourceAttrSet("rawtree_s3_ingestion.test", "lambda_function_arn"),
					resource.TestCheckResourceAttrSet("rawtree_s3_ingestion.test", "eventbridge_rule_arn"),
					// Wait for Glue job to complete and verify data landed in Rawtree.
					testAccCheckGlueJobSucceeded("rawtree_s3_ingestion.test"),
					testAccCheckRawtreeHasData(tableName, 10), // events.json (5) + logs.jsonl (5)
				),
			},
		},
	})
}

func TestAccS3Ingestion_withPrefix(t *testing.T) {
	bucket := os.Getenv("RAWTREE_TEST_S3_BUCKET")
	tableName := fmt.Sprintf("acc_test_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckS3IngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccS3IngestionConfig(tableName, bucket, "json", "data/json/", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckS3IngestionExists("rawtree_s3_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "prefix", "data/json/"),
					testAccCheckGlueJobSucceeded("rawtree_s3_ingestion.test"),
					testAccCheckRawtreeHasData(tableName, 10), // events.json (5) + logs.jsonl (5)
				),
			},
		},
	})
}

func TestAccS3Ingestion_withFilePattern(t *testing.T) {
	bucket := os.Getenv("RAWTREE_TEST_S3_BUCKET")
	tableName := fmt.Sprintf("acc_test_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckS3IngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccS3IngestionConfig(tableName, bucket, "csv", "", `.*\.csv$`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckS3IngestionExists("rawtree_s3_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "format", "csv"),
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "file_pattern", `.*\.csv$`),
					testAccCheckGlueJobSucceeded("rawtree_s3_ingestion.test"),
					testAccCheckRawtreeHasData(tableName, 5), // metrics.csv (5 data rows)
				),
			},
		},
	})
}

func TestAccS3Ingestion_updateTable(t *testing.T) {
	bucket := os.Getenv("RAWTREE_TEST_S3_BUCKET")
	tableName1 := fmt.Sprintf("acc_test_%s", acctest.RandString(8))
	tableName2 := fmt.Sprintf("acc_test_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckS3IngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccS3IngestionConfig(tableName1, bucket, "json", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "table", tableName1),
					testAccCheckGlueJobSucceeded("rawtree_s3_ingestion.test"),
					testAccCheckRawtreeHasData(tableName1, 10),
				),
			},
			// Step 2: Update table name — only Lambda env vars change, no new Glue job.
			{
				Config: testAccS3IngestionConfig(tableName2, bucket, "json", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_s3_ingestion.test", "table", tableName2),
				),
			},
		},
	})
}

func TestAccS3Ingestion_changeBucketForcesReplace(t *testing.T) {
	bucket1 := os.Getenv("RAWTREE_TEST_S3_BUCKET")
	bucket2 := os.Getenv("RAWTREE_TEST_S3_BUCKET_2")
	if bucket2 == "" {
		t.Skip("RAWTREE_TEST_S3_BUCKET_2 not set, skipping replacement test")
	}
	tableName := fmt.Sprintf("acc_test_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckS3IngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccS3IngestionConfig(tableName, bucket1, "json", "", ""),
				Check:  testAccCheckS3IngestionExists("rawtree_s3_ingestion.test"),
			},
			{
				Config: testAccS3IngestionConfig(tableName, bucket2, "json", "", ""),
				Check:  testAccCheckS3IngestionExists("rawtree_s3_ingestion.test"),
			},
		},
	})
}

// testAccCheckS3IngestionExists verifies the AWS resources were actually created.
func testAccCheckS3IngestionExists(resourceName string) resource.TestCheckFunc {
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

		// Verify Glue job exists.
		glueJobName := rs.Primary.Attributes["glue_job_name"]
		if glueJobName != "" {
			glueClient := glue.NewFromConfig(awsCfg)
			_, err := glueClient.GetJob(ctx, &glue.GetJobInput{
				JobName: &glueJobName,
			})
			if err != nil {
				return fmt.Errorf("Glue job %s not found: %w", glueJobName, err)
			}
		}

		// Verify Lambda function exists.
		lambdaARN := rs.Primary.Attributes["lambda_function_arn"]
		if lambdaARN != "" {
			lambdaClient := lambda.NewFromConfig(awsCfg)
			_, err := lambdaClient.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: &lambdaARN,
			})
			if err != nil {
				return fmt.Errorf("Lambda function %s not found: %w", lambdaARN, err)
			}
		}

		return nil
	}
}

// testAccCheckGlueJobSucceeded waits for the Glue job run to complete successfully.
func testAccCheckGlueJobSucceeded(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		glueJobName := rs.Primary.Attributes["glue_job_name"]
		runID := rs.Primary.Attributes["glue_job_run_id"]
		if glueJobName == "" || runID == "" {
			return fmt.Errorf("glue_job_name or glue_job_run_id not set")
		}

		ctx := context.Background()
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
		if err != nil {
			return fmt.Errorf("loading AWS config: %w", err)
		}

		glueClient := glue.NewFromConfig(awsCfg)

		// Poll for up to 5 minutes.
		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			out, err := glueClient.GetJobRun(ctx, &glue.GetJobRunInput{
				JobName: aws.String(glueJobName),
				RunId:   aws.String(runID),
			})
			if err != nil {
				return fmt.Errorf("getting Glue job run: %w", err)
			}

			state := out.JobRun.JobRunState
			switch state {
			case gluetypes.JobRunStateSucceeded:
				return nil
			case gluetypes.JobRunStateFailed, gluetypes.JobRunStateStopped, gluetypes.JobRunStateTimeout, gluetypes.JobRunStateError:
				return fmt.Errorf("Glue job run %s ended with state %s: %s",
					runID, state, aws.ToString(out.JobRun.ErrorMessage))
			}

			time.Sleep(10 * time.Second)
		}

		return fmt.Errorf("Glue job run %s did not complete within 5 minutes", runID)
	}
}

// testAccCheckRawtreeHasData queries Rawtree and verifies the table has the expected number of rows.
func testAccCheckRawtreeHasData(tableName string, expectedRows int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		apiURL := os.Getenv("RAWTREE_URL")
		if apiURL == "" {
			apiURL = "https://api.us-east-1.aws.rawtree.dev"
		}
		apiKey := os.Getenv("RAWTREE_API_KEY")

		query := fmt.Sprintf(`SELECT count() as cnt FROM %s`, tableName)
		body := fmt.Sprintf(`{"sql":%q}`, query)

		url := fmt.Sprintf("%s/v1/query", apiURL)
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating query request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("querying Rawtree: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			var buf [1024]byte
			n, _ := resp.Body.Read(buf[:])
			return fmt.Errorf("Rawtree query returned %d: %s", resp.StatusCode, string(buf[:n]))
		}

		var result struct {
			Data []map[string]interface{} `json:"data"`
			Rows int                      `json:"rows"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("decoding query response: %w", err)
		}

		if len(result.Data) == 0 {
			return fmt.Errorf("query returned no data for table %s", tableName)
		}

		// Extract count from first row.
		cnt, ok := result.Data[0]["cnt"]
		if !ok {
			return fmt.Errorf("query response missing 'cnt' column: %v", result.Data[0])
		}

		// JSON numbers decode as float64.
		var rowCount int
		switch v := cnt.(type) {
		case float64:
			rowCount = int(v)
		case string:
			fmt.Sscanf(v, "%d", &rowCount)
		default:
			return fmt.Errorf("unexpected count type %T: %v", cnt, cnt)
		}

		if rowCount < expectedRows {
			return fmt.Errorf("table %s has %d rows, expected at least %d", tableName, rowCount, expectedRows)
		}

		return nil
	}
}

// testAccCheckS3IngestionDestroy verifies all AWS resources are cleaned up.
func testAccCheckS3IngestionDestroy(s *terraform.State) error {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rawtree_s3_ingestion" {
			continue
		}

		// Check Glue job is gone.
		glueJobName := rs.Primary.Attributes["glue_job_name"]
		if glueJobName != "" {
			glueClient := glue.NewFromConfig(awsCfg)
			_, err := glueClient.GetJob(ctx, &glue.GetJobInput{
				JobName: &glueJobName,
			})
			if err == nil {
				return fmt.Errorf("Glue job %s still exists after destroy", glueJobName)
			}
		}

		// Check Lambda function is gone.
		lambdaARN := rs.Primary.Attributes["lambda_function_arn"]
		if lambdaARN != "" {
			lambdaClient := lambda.NewFromConfig(awsCfg)
			_, err := lambdaClient.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: &lambdaARN,
			})
			if err == nil {
				return fmt.Errorf("Lambda function %s still exists after destroy", lambdaARN)
			}
		}

		// Check EventBridge rule is gone.
		ebRuleARN := rs.Primary.Attributes["eventbridge_rule_arn"]
		if ebRuleARN != "" {
			ebClient := eventbridge.NewFromConfig(awsCfg)
			ruleName := fmt.Sprintf("rawtree-s3-%s", rs.Primary.ID)
			_, err := ebClient.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
				Name: &ruleName,
			})
			if err == nil {
				return fmt.Errorf("EventBridge rule %s still exists after destroy", ruleName)
			}
		}

		// Check IAM roles are gone.
		iamClient := iam.NewFromConfig(awsCfg)
		for _, roleSuffix := range []string{"glue", "lambda"} {
			roleName := fmt.Sprintf("rawtree-%s-%s", roleSuffix, rs.Primary.ID)
			_, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
				RoleName: &roleName,
			})
			if err == nil {
				return fmt.Errorf("IAM role %s still exists after destroy", roleName)
			}
		}

		// Check Glue script object is gone from source bucket.
		s3Client := s3.NewFromConfig(awsCfg)
		scriptKey := fmt.Sprintf(".rawtree/glue-scripts/%s/glue_job.py", rs.Primary.ID)
		bucket := rs.Primary.Attributes["bucket"]
		_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: &bucket,
			Key:    &scriptKey,
		})
		if err == nil {
			return fmt.Errorf("Glue script %s still exists in bucket %s after destroy", scriptKey, bucket)
		}
	}

	return nil
}

// testAccS3IngestionConfig generates Terraform config for acceptance tests.
func testAccS3IngestionConfig(table, bucket, format, prefix, filePattern string) string {
	cfg := fmt.Sprintf(`
resource "rawtree_s3_ingestion" "test" {
  table  = %q
  bucket = %q
  format = %q
  region = %q
`, table, bucket, format, testRegion)

	if prefix != "" {
		cfg += fmt.Sprintf("  prefix = %q\n", prefix)
	}
	if filePattern != "" {
		cfg += fmt.Sprintf("  file_pattern = %q\n", filePattern)
	}

	cfg += "}\n"
	return cfg
}
