package waf_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

// createFirehoseRole creates an IAM role for the Firehose delivery stream with
// permissions to write to the S3 backup bucket and CloudWatch Logs.
func createFirehoseRole(ctx context.Context, client *iam.Client, resourceName, bucketName, region string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-firehose-%s", resourceName)

	trustPolicy := util.TrustPolicy{
		Version: "2012-10-17",
		Statement: []util.TrustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "firehose.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	}

	roleARN, err = util.CreateRole(ctx, client, roleName,
		"Rawtree Terraform provider - Firehose role for WAF log ingestion", trustPolicy)
	if err != nil {
		return "", "", "", err
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect: "Allow",
				Action: []string{
					"s3:AbortMultipartUpload",
					"s3:GetBucketLocation",
					"s3:GetObject",
					"s3:ListBucket",
					"s3:ListBucketMultipartUploads",
					"s3:PutObject",
				},
				Resource: []string{
					fmt.Sprintf("arn:aws:s3:::%s", bucketName),
					fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
				},
			},
			{
				Effect: "Allow",
				Action: []string{
					"logs:PutLogEvents",
					"logs:CreateLogStream",
					"logs:CreateLogGroup",
				},
				Resource: []string{
					fmt.Sprintf("arn:aws:logs:%s:*:log-group:/aws/firehose/aws-waf-logs-rawtree-*", region),
				},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-firehose-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"S3 backup and CloudWatch Logs access for Rawtree WAF Firehose", policyDoc)
	if err != nil {
		return "", "", "", err
	}

	// Wait for IAM role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}
