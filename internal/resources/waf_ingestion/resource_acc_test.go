package waf_ingestion_test

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
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
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
	if v := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN"); v == "" {
		t.Fatal("RAWTREE_TEST_WAF_WEB_ACL_ARN must be set for acceptance tests (an existing WAFv2 Web ACL ARN)")
	}
}

func TestAccWafIngestion_basic(t *testing.T) {
	webACLARN := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN")
	tableName := fmt.Sprintf("acc_test_waf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWafIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWafIngestionConfig(tableName, webACLARN, 0, 0, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckWafIngestionExists("rawtree_waf_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "table", tableName),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "web_acl_arn", webACLARN),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "region", testRegion),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "buffering_size", "5"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "buffering_interval", "300"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "s3_backup_mode", "FailedDataOnly"),
					resource.TestCheckResourceAttrSet("rawtree_waf_ingestion.test", "id"),
					resource.TestCheckResourceAttrSet("rawtree_waf_ingestion.test", "firehose_arn"),
					resource.TestCheckResourceAttrSet("rawtree_waf_ingestion.test", "firehose_name"),
					resource.TestCheckResourceAttrSet("rawtree_waf_ingestion.test", "backup_bucket_name"),
					resource.TestCheckResourceAttrSet("rawtree_waf_ingestion.test", "waf_logging_configuration_id"),
					testAccCheckFirehoseActive("rawtree_waf_ingestion.test"),
				),
			},
		},
	})
}

func TestAccWafIngestion_customBuffering(t *testing.T) {
	webACLARN := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN")
	tableName := fmt.Sprintf("acc_test_waf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWafIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWafIngestionConfig(tableName, webACLARN, 10, 120, "AllData"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckWafIngestionExists("rawtree_waf_ingestion.test"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "buffering_size", "10"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "buffering_interval", "120"),
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "s3_backup_mode", "AllData"),
					testAccCheckFirehoseActive("rawtree_waf_ingestion.test"),
				),
			},
		},
	})
}

func TestAccWafIngestion_updateTable(t *testing.T) {
	webACLARN := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN")
	tableName1 := fmt.Sprintf("acc_test_waf_%s", acctest.RandString(8))
	tableName2 := fmt.Sprintf("acc_test_waf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWafIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWafIngestionConfig(tableName1, webACLARN, 0, 0, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "table", tableName1),
					testAccCheckFirehoseActive("rawtree_waf_ingestion.test"),
				),
			},
			{
				Config: testAccWafIngestionConfig(tableName2, webACLARN, 0, 0, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("rawtree_waf_ingestion.test", "table", tableName2),
				),
			},
		},
	})
}

func TestAccWafIngestion_changeWebAclForcesReplace(t *testing.T) {
	webACLARN1 := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN")
	webACLARN2 := os.Getenv("RAWTREE_TEST_WAF_WEB_ACL_ARN_2")
	if webACLARN2 == "" {
		t.Skip("RAWTREE_TEST_WAF_WEB_ACL_ARN_2 not set, skipping replacement test")
	}
	tableName := fmt.Sprintf("acc_test_waf_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: provider.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckWafIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWafIngestionConfig(tableName, webACLARN1, 0, 0, ""),
				Check:  testAccCheckWafIngestionExists("rawtree_waf_ingestion.test"),
			},
			{
				Config: testAccWafIngestionConfig(tableName, webACLARN2, 0, 0, ""),
				Check:  testAccCheckWafIngestionExists("rawtree_waf_ingestion.test"),
			},
		},
	})
}

// testAccCheckWafIngestionExists verifies the AWS resources were actually created.
func testAccCheckWafIngestionExists(resourceName string) resource.TestCheckFunc {
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

		// Verify Firehose exists.
		firehoseName := rs.Primary.Attributes["firehose_name"]
		if firehoseName != "" {
			fhClient := firehose.NewFromConfig(awsCfg)
			_, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
				DeliveryStreamName: &firehoseName,
			})
			if err != nil {
				return fmt.Errorf("firehose %s not found: %w", firehoseName, err)
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
				return fmt.Errorf("s3 backup bucket %s not found: %w", bucketName, err)
			}
		}

		// Verify WAF logging configuration exists.
		webACLARN := rs.Primary.Attributes["web_acl_arn"]
		if webACLARN != "" {
			wafClient := wafv2.NewFromConfig(awsCfg)
			_, err := wafClient.GetLoggingConfiguration(ctx, &wafv2.GetLoggingConfigurationInput{
				ResourceArn: &webACLARN,
			})
			if err != nil {
				return fmt.Errorf("waf logging config for %s not found: %w", webACLARN, err)
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
			return fmt.Errorf("firehose %s is not ACTIVE, status: %s",
				firehoseName, out.DeliveryStreamDescription.DeliveryStreamStatus)
		}

		return nil
	}
}

// testAccCheckWafIngestionDestroy verifies all AWS resources are cleaned up.
func testAccCheckWafIngestionDestroy(s *terraform.State) error {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(testRegion))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rawtree_waf_ingestion" {
			continue
		}

		// Check Firehose is gone.
		firehoseName := rs.Primary.Attributes["firehose_name"]
		if firehoseName != "" {
			fhClient := firehose.NewFromConfig(awsCfg)
			_, err := fhClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
				DeliveryStreamName: &firehoseName,
			})
			if err == nil {
				return fmt.Errorf("firehose %s still exists after destroy", firehoseName)
			}
		}

		// Check WAF logging config is gone.
		webACLARN := rs.Primary.Attributes["web_acl_arn"]
		if webACLARN != "" {
			wafClient := wafv2.NewFromConfig(awsCfg)
			_, err := wafClient.GetLoggingConfiguration(ctx, &wafv2.GetLoggingConfigurationInput{
				ResourceArn: &webACLARN,
			})
			if err == nil {
				return fmt.Errorf("waf logging config for %s still exists after destroy", webACLARN)
			}
		}

		// Check IAM role is gone.
		iamClient := iam.NewFromConfig(awsCfg)
		roleName := fmt.Sprintf("rawtree-firehose-%s", rs.Primary.ID)
		_, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
			RoleName: &roleName,
		})
		if err == nil {
			return fmt.Errorf("iam role %s still exists after destroy", roleName)
		}

		// Check S3 backup bucket is gone.
		bucketName := rs.Primary.Attributes["backup_bucket_name"]
		if bucketName != "" {
			s3Client := s3.NewFromConfig(awsCfg)
			_, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
				Bucket: &bucketName,
			})
			if err == nil {
				return fmt.Errorf("s3 backup bucket %s still exists after destroy", bucketName)
			}
		}
	}

	return nil
}

func testAccWafIngestionConfig(table, webACLARN string, bufferingSize, bufferingInterval int, s3BackupMode string) string {
	cfg := fmt.Sprintf(`
resource "rawtree_waf_ingestion" "test" {
  table       = %q
  web_acl_arn = %q
  region      = %q
`, table, webACLARN, testRegion)

	if bufferingSize > 0 {
		cfg += fmt.Sprintf("  buffering_size = %d\n", bufferingSize)
	}
	if bufferingInterval > 0 {
		cfg += fmt.Sprintf("  buffering_interval = %d\n", bufferingInterval)
	}
	if s3BackupMode != "" {
		cfg += fmt.Sprintf("  s3_backup_mode = %q\n", s3BackupMode)
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

func TestAccWafIngestion_endToEndData(t *testing.T) {
	tableName := fmt.Sprintf("acc_test_waf_e2e_%s", acctest.RandString(8))
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
		CheckDestroy: testAccCheckWafIngestionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccWafIngestionE2EConfig(tableName, suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckWafIngestionExists("rawtree_waf_ingestion.test"),
					testAccCheckFirehoseActive("rawtree_waf_ingestion.test"),
					testAccE2EGenerateAndValidate(tableName, e2eNumRequests),
				),
			},
		},
	})
}

// testAccE2EGenerateAndValidate sends traffic to CloudFront and polls Rawtree
// until the expected row count appears or the timeout expires.
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

		if err := generateCloudFrontTraffic(domain, numRequests); err != nil {
			return fmt.Errorf("generating CloudFront traffic: %w", err)
		}

		return testAccWaitForRawtreeData(tableName, numRequests, 10*time.Minute)
	}
}

// generateCloudFrontTraffic sends numRequests HTTP GET requests to the
// CloudFront distribution. WAF evaluates every request regardless of origin
// response, so 403/404 from an empty S3 origin is expected and fine.
func generateCloudFrontTraffic(domain string, numRequests int) error {
	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < numRequests; i++ {
		url := fmt.Sprintf("https://%s/waf-test-path-%d?ts=%d", domain, i, time.Now().UnixNano())
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("creating request %d: %w", i, err)
		}
		req.Header.Set("User-Agent", fmt.Sprintf("rawtree-acc-test/%d", i))
		req.Header.Set("X-Test-Marker", "rawtree-e2e")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("request %d failed: %w", i, err)
		}
		if err := resp.Body.Close(); err != nil {
			return fmt.Errorf("closing response body for request %d: %w", i, err)
		}

		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// testAccWaitForRawtreeData polls Rawtree until the table has at least minRows
// or the timeout expires.
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

	var lastCount int
	for time.Now().Before(deadline) {
		count, err := queryRawtreeCount(queryURL, apiKey, body)
		if err != nil {
			// Table may not exist yet on first attempts; keep polling.
			time.Sleep(30 * time.Second)
			continue
		}
		lastCount = count
		if count >= minRows {
			return nil
		}
		time.Sleep(30 * time.Second)
	}

	return fmt.Errorf("table %s has %d rows after %s, expected at least %d",
		tableName, lastCount, timeout, minRows)
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

// testAccWafIngestionE2EConfig returns a Terraform config that creates a full
// CloudFront + WAF + rawtree_waf_ingestion pipeline for end-to-end testing.
func testAccWafIngestionE2EConfig(tableName, suffix string) string {
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
  bucket        = "rawtree-waf-e2e-origin-%[2]s"
  force_destroy = true

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "e2e-acceptance-testing"
  }
}

resource "aws_cloudfront_origin_access_control" "test" {
  name                              = "rawtree-waf-e2e-%[2]s"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_wafv2_web_acl" "test" {
  name        = "rawtree-waf-e2e-%[2]s"
  description = "E2E test Web ACL for terraform-provider-rawtree"
  scope       = "CLOUDFRONT"

  default_action {
    allow {}
  }

  visibility_config {
    cloudwatch_metrics_enabled = false
    metric_name                = "rawtree-waf-e2e-%[2]s"
    sampled_requests_enabled   = false
  }

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "e2e-acceptance-testing"
  }
}

resource "aws_cloudfront_distribution" "test" {
  enabled             = true
  is_ipv6_enabled     = true
  wait_for_deployment = true
  web_acl_id          = aws_wafv2_web_acl.test.arn

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
}

resource "rawtree_waf_ingestion" "test" {
  table              = %[3]q
  web_acl_arn        = aws_wafv2_web_acl.test.arn
  region             = %[1]q
  buffering_size     = 1
  buffering_interval = 60
}
`, testRegion, suffix, tableName)
}
