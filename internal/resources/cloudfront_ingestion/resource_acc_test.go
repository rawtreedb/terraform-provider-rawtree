package cloudfront_ingestion_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awscf "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	if v := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID"); v == "" {
		t.Fatal("RAWTREE_TEST_CF_DISTRIBUTION_ID must be set for acceptance tests (an existing CloudFront distribution ID)")
	}
}

func TestAccCloudfrontIngestion_basic(t *testing.T) {
	distributionID := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	tableName := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID, 0, 0, "", 0, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "table", tableName),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "distribution_id", distributionID),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "region", testRegion),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "sampling_rate", "100"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "buffering_size", "5"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "buffering_interval", "300"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "s3_backup_mode", "FailedDataOnly"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "id"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "kinesis_stream_arn"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "kinesis_stream_name"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "firehose_arn"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "firehose_name"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "backup_bucket_name"),
					resource.TestCheckResourceAttrSet("rawtree_cloudfront_ingestion.test", "realtime_log_config_arn"),
					testAccCheckFirehoseActive("rawtree_cloudfront_ingestion.test"),
				),
			},
		},
	})
}

func TestAccCloudfrontIngestion_customBuffering(t *testing.T) {
	distributionID := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	tableName := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID, 10, 120, "AllData", 0, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "buffering_size", "10"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "buffering_interval", "120"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "s3_backup_mode", "AllData"),
					testAccCheckFirehoseActive("rawtree_cloudfront_ingestion.test"),
				),
			},
		},
	})
}

func TestAccCloudfrontIngestion_customFields(t *testing.T) {
	distributionID := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	tableName := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	fields := []string{"timestamp", "c-ip", "sc-status", "cs-method", "cs-uri-stem"}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID, 0, 0, "", 50, fields),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "sampling_rate", "50"),
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "fields.#", "5"),
					testAccCheckFirehoseActive("rawtree_cloudfront_ingestion.test"),
				),
			},
		},
	})
}

func TestAccCloudfrontIngestion_updateTable(t *testing.T) {
	distributionID := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	tableName1 := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))
	tableName2 := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName1, distributionID, 0, 0, "", 0, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "table", tableName1),
					testAccCheckFirehoseActive("rawtree_cloudfront_ingestion.test"),
				),
			},
			{
				Config: testAccCloudfrontIngestionConfig(tableName2, distributionID, 0, 0, "", 0, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "table", tableName2),
				),
			},
		},
	})
}

func TestAccCloudfrontIngestion_updateSamplingRate(t *testing.T) {
	distributionID := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	tableName := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID, 0, 0, "", 100, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "sampling_rate", "100"),
				),
			},
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID, 0, 0, "", 50, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_cloudfront_ingestion.test", "sampling_rate", "50"),
				),
			},
		},
	})
}

func TestAccCloudfrontIngestion_changeDistributionForcesReplace(t *testing.T) {
	distributionID1 := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID")
	distributionID2 := os.Getenv("RAWTREE_TEST_CF_DISTRIBUTION_ID_2")
	if distributionID2 == "" {
		t.Skip("RAWTREE_TEST_CF_DISTRIBUTION_ID_2 not set, skipping replacement test")
	}
	tableName := fmt.Sprintf("acc_test_cf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID1, 0, 0, "", 0, nil),
				Check:  testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
			},
			{
				Config: testAccCloudfrontIngestionConfig(tableName, distributionID2, 0, 0, "", 0, nil),
				Check:  testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
			},
		},
	})
}

// testAccCheckCloudfrontIngestionExists verifies the AWS resources were actually created.
func testAccCheckCloudfrontIngestionExists(resourceName string) resource.TestCheckFunc {
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

		// Verify Kinesis stream exists.
		kinesisStreamName := rs.Primary.Attributes["kinesis_stream_name"]
		if kinesisStreamName != "" {
			kClient := kinesis.NewFromConfig(awsCfg)
			_, err := kClient.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
				StreamName: &kinesisStreamName,
			})
			if err != nil {
				return fmt.Errorf("Kinesis stream %s not found: %w", kinesisStreamName, err)
			}
		}

		// Verify Firehose exists.
		firehoseName := rs.Primary.Attributes["firehose_name"]
		if firehoseName != "" {
			fhClient := firehose.NewFromConfig(awsCfg)
			_, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
				DeliveryStreamName: &firehoseName,
			})
			if err != nil {
				return fmt.Errorf("Firehose %s not found: %w", firehoseName, err)
			}
		}

		// Verify backup bucket exists.
		bucketName := rs.Primary.Attributes["backup_bucket_name"]
		if bucketName != "" {
			s3Client := s3.NewFromConfig(awsCfg)
			_, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
				Bucket: &bucketName,
			})
			if err != nil {
				return fmt.Errorf("S3 backup bucket %s not found: %w", bucketName, err)
			}
		}

		// Verify real-time log config exists.
		logConfigARN := rs.Primary.Attributes["realtime_log_config_arn"]
		if logConfigARN != "" {
			cfClient := awscf.NewFromConfig(awsCfg)
			_, err := cfClient.GetRealtimeLogConfig(ctx, &awscf.GetRealtimeLogConfigInput{
				ARN: &logConfigARN,
			})
			if err != nil {
				return fmt.Errorf("real-time log config %s not found: %w", logConfigARN, err)
			}
		}

		return nil
	}
}

// testAccCheckFirehoseActive verifies the Firehose delivery stream is in ACTIVE state.
func testAccCheckFirehoseActive(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		firehoseName := rs.Primary.Attributes["firehose_name"]
		if firehoseName == "" {
			return fmt.Errorf("firehose_name not set")
		}

		ctx := context.Background()
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
		if err != nil {
			return fmt.Errorf("loading AWS config: %w", err)
		}

		fhClient := firehose.NewFromConfig(awsCfg)
		out, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(firehoseName),
		})
		if err != nil {
			return fmt.Errorf("describing Firehose %s: %w", firehoseName, err)
		}

		if out.DeliveryStreamDescription.DeliveryStreamStatus != fhtypes.DeliveryStreamStatusActive {
			return fmt.Errorf("Firehose %s is not ACTIVE, status: %s",
				firehoseName, out.DeliveryStreamDescription.DeliveryStreamStatus)
		}

		return nil
	}
}

// testAccCheckCloudfrontIngestionDestroy verifies all AWS resources are cleaned up.
// It retries for up to 60 seconds to account for async deletions (Kinesis, Firehose).
func testAccCheckCloudfrontIngestionDestroy(s *terraform.State) error {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rawtree_cloudfront_ingestion" {
			continue
		}

		kClient := kinesis.NewFromConfig(awsCfg)
		fhClient := firehose.NewFromConfig(awsCfg)
		iamClient := iam.NewFromConfig(awsCfg)
		s3Client := s3.NewFromConfig(awsCfg)

		kinesisStreamName := rs.Primary.Attributes["kinesis_stream_name"]
		firehoseName := rs.Primary.Attributes["firehose_name"]
		bucketName := rs.Primary.Attributes["backup_bucket_name"]

		for time.Now().Before(deadline) {
			allGone := true

			if kinesisStreamName != "" {
				_, err := kClient.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
					StreamName: &kinesisStreamName,
				})
				if err == nil {
					allGone = false
				}
			}

			if firehoseName != "" {
				_, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
					DeliveryStreamName: &firehoseName,
				})
				if err == nil {
					allGone = false
				}
			}

			if allGone {
				break
			}
			time.Sleep(5 * time.Second)
		}

		// Final checks after waiting.
		if kinesisStreamName != "" {
			if _, err := kClient.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
				StreamName: &kinesisStreamName,
			}); err == nil {
				return fmt.Errorf("Kinesis stream %s still exists after destroy", kinesisStreamName)
			}
		}

		if firehoseName != "" {
			if _, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
				DeliveryStreamName: &firehoseName,
			}); err == nil {
				return fmt.Errorf("Firehose %s still exists after destroy", firehoseName)
			}
		}

		for _, prefix := range []string{"rawtree-cf-source-", "rawtree-cf-firehose-"} {
			roleName := prefix + rs.Primary.ID
			if _, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
				RoleName: &roleName,
			}); err == nil {
				return fmt.Errorf("IAM role %s still exists after destroy", roleName)
			}
		}

		if bucketName != "" {
			if _, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
				Bucket: &bucketName,
			}); err == nil {
				return fmt.Errorf("S3 backup bucket %s still exists after destroy", bucketName)
			}
		}
	}

	return nil
}

func testAccCloudfrontIngestionConfig(table, distributionID string, bufferingSize, bufferingInterval int, s3BackupMode string, samplingRate int, fields []string) string {
	cfg := fmt.Sprintf(`
resource "rawtree_cloudfront_ingestion" "test" {
  table           = %q
  distribution_id = %q
  region          = %q
`, table, distributionID, testRegion)

	if bufferingSize > 0 {
		cfg += fmt.Sprintf("  buffering_size = %d\n", bufferingSize)
	}
	if bufferingInterval > 0 {
		cfg += fmt.Sprintf("  buffering_interval = %d\n", bufferingInterval)
	}
	if s3BackupMode != "" {
		cfg += fmt.Sprintf("  s3_backup_mode = %q\n", s3BackupMode)
	}
	if samplingRate > 0 {
		cfg += fmt.Sprintf("  sampling_rate = %d\n", samplingRate)
	}
	if fields != nil {
		quoted := make([]string, len(fields))
		for i, f := range fields {
			quoted[i] = fmt.Sprintf("%q", f)
		}
		cfg += fmt.Sprintf("  fields = [%s]\n", strings.Join(quoted, ", "))
	}

	cfg += "}\n"
	return cfg
}

// ---------------------------------------------------------------------------
// End-to-end data validation test
// ---------------------------------------------------------------------------

const e2eNumRequests = 10

func testAccPreCheckE2E(t *testing.T) {
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
}

func TestAccCloudfrontIngestion_endToEndData(t *testing.T) {
	tableName := fmt.Sprintf("acc_test_cf_e2e_%s", acctest.RandString(8))
	suffix := acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckE2E(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		ExternalProviders: map[string]resource.ExternalProvider{
			"aws": {
				Source:            "hashicorp/aws",
				VersionConstraint: "~> 5.0",
			},
		},
		CheckDestroy: testAccCheckCloudfrontIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCloudfrontIngestionE2EConfig(tableName, suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCloudfrontIngestionExists("rawtree_cloudfront_ingestion.test"),
					testAccCheckFirehoseActive("rawtree_cloudfront_ingestion.test"),
					testAccE2EGenerateAndValidate(tableName, e2eNumRequests),
				),
			},
		},
	})
}

func testAccE2EGenerateAndValidate(tableName string, numRequests int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources["aws_cloudfront_distribution.test"]
		if !ok {
			return fmt.Errorf("aws_cloudfront_distribution.test not found in state")
		}
		domain := rs.Primary.Attributes["domain_name"]
		if domain == "" {
			return fmt.Errorf("cloudfront distribution domain_name is empty")
		}
		distributionStatus := rs.Primary.Attributes["status"]

		ingestionRS, ok := s.RootModule().Resources["rawtree_cloudfront_ingestion.test"]
		if !ok {
			return fmt.Errorf("rawtree_cloudfront_ingestion.test not found in state")
		}

		fmt.Printf("[E2E] CloudFront domain: %s (status: %s)\n", domain, distributionStatus)
		fmt.Printf("[E2E] Kinesis stream: %s\n", ingestionRS.Primary.Attributes["kinesis_stream_name"])
		fmt.Printf("[E2E] Firehose: %s\n", ingestionRS.Primary.Attributes["firehose_name"])
		fmt.Printf("[E2E] Realtime log config ARN: %s\n", ingestionRS.Primary.Attributes["realtime_log_config_arn"])
		fmt.Printf("[E2E] Target table: %s\n", tableName)

		if err := generateCloudFrontTraffic(domain, numRequests); err != nil {
			return fmt.Errorf("generating CloudFront traffic: %w", err)
		}

		return testAccWaitForRawtreeData(tableName, numRequests, 10*time.Minute)
	}
}

func generateCloudFrontTraffic(domain string, numRequests int) error {
	client := &http.Client{Timeout: 10 * time.Second}

	fmt.Printf("[E2E] Sending %d requests to https://%s\n", numRequests, domain)
	for i := 0; i < numRequests; i++ {
		url := fmt.Sprintf("https://%s/cf-test-path-%d?ts=%d", domain, i, time.Now().UnixNano())
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("creating request %d: %w", i, err)
		}
		req.Header.Set("User-Agent", fmt.Sprintf("rawtree-acc-test/%d", i))
		req.Header.Set("X-Test-Marker", "rawtree-cf-e2e")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[E2E] Request %d FAILED: %v\n", i, err)
			return fmt.Errorf("request %d failed: %w", i, err)
		}
		fmt.Printf("[E2E] Request %d: HTTP %d\n", i, resp.StatusCode)
		_ = resp.Body.Close()

		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("[E2E] All %d requests sent successfully\n", numRequests)
	return nil
}

func testAccWaitForRawtreeData(tableName string, minRows int, timeout time.Duration) error {
	apiURL := os.Getenv("RAWTREE_URL")
	if apiURL == "" {
		apiURL = "https://api.us-east-1.aws.rawtree.dev"
	}
	apiKey := os.Getenv("RAWTREE_API_KEY")
	org := os.Getenv("RAWTREE_ORG")
	project := os.Getenv("RAWTREE_PROJECT")

	deadline := time.Now().Add(timeout)
	queryURL := fmt.Sprintf("%s/v1/%s/%s/query", apiURL, org, project)
	query := fmt.Sprintf(`SELECT count() as cnt FROM %s`, tableName)
	body := fmt.Sprintf(`{"sql":%q}`, query)

	fmt.Printf("[E2E] Polling Rawtree for data (table=%s, need>=%d rows, timeout=%s)\n", tableName, minRows, timeout)
	fmt.Printf("[E2E] Query URL: %s\n", queryURL)

	attempt := 0
	var lastCount int
	var lastErr error
	for time.Now().Before(deadline) {
		attempt++
		count, err := queryRawtreeCount(queryURL, apiKey, body)
		if err != nil {
			lastErr = err
			fmt.Printf("[E2E] Poll #%d: error: %v\n", attempt, err)
			time.Sleep(30 * time.Second)
			continue
		}
		lastErr = nil
		lastCount = count
		fmt.Printf("[E2E] Poll #%d: %d rows\n", attempt, count)
		if count >= minRows {
			fmt.Printf("[E2E] Success! Got %d rows (needed %d)\n", count, minRows)
			return nil
		}
		time.Sleep(30 * time.Second)
	}

	msg := fmt.Sprintf("table %s has %d rows after %s, expected at least %d", tableName, lastCount, timeout, minRows)
	if lastErr != nil {
		msg += fmt.Sprintf(" (last error: %v)", lastErr)
	}
	return errors.New(msg)
}

func queryRawtreeCount(url, apiKey, body string) (int, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var respBody []byte
	respBody, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("query returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(respBody))
	}
	if len(result.Data) == 0 {
		return 0, fmt.Errorf("no data returned (body: %s)", string(respBody))
	}

	cnt, ok := result.Data[0]["cnt"]
	if !ok {
		return 0, fmt.Errorf("missing cnt column (body: %s)", string(respBody))
	}

	switch v := cnt.(type) {
	case float64:
		return int(v), nil
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return 0, fmt.Errorf("parsing cnt %q: %w (body: %s)", v, err, string(respBody))
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unexpected cnt type %T (body: %s)", cnt, string(respBody))
	}
}

func testAccCloudfrontIngestionE2EConfig(tableName, suffix string) string {
	return fmt.Sprintf(`
provider "aws" {
  region = %[1]q

  default_tags {
    tags = {
      CostGroup   = "e2e"
      Environment = "terraform-provider-rawtree"
    }
  }
}

resource "aws_s3_bucket" "origin" {
  bucket        = "rawtree-cf-e2e-origin-%[2]s"
  force_destroy = true

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "e2e-acceptance-testing"
  }
}

resource "aws_cloudfront_origin_access_control" "test" {
  name                              = "rawtree-cf-e2e-%[2]s"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "test" {
  enabled             = true
  is_ipv6_enabled     = true
  wait_for_deployment = true

  origin {
    domain_name              = aws_s3_bucket.origin.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.test.id
    origin_id                = "s3origin"
  }

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "s3origin"
    viewer_protocol_policy = "allow-all"

    forwarded_values {
      query_string = true
      cookies {
        forward = "none"
      }
    }

    min_ttl     = 0
    default_ttl = 60
    max_ttl     = 60
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "e2e-acceptance-testing"
  }

  lifecycle {
    ignore_changes = [default_cache_behavior[0].realtime_log_config_arn]
  }
}

resource "rawtree_cloudfront_ingestion" "test" {
  table              = %[3]q
  distribution_id    = aws_cloudfront_distribution.test.id
  region             = %[1]q
  sampling_rate      = 100
  buffering_size     = 1
  buffering_interval = 60
  fields             = ["timestamp", "c-ip", "sc-status", "sc-bytes", "cs-method", "cs-uri-stem", "x-edge-location", "x-edge-result-type"]
}
`, testRegion, suffix, tableName)
}
