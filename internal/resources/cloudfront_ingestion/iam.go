package cloudfront_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func createCloudFrontRole(ctx context.Context, client *iam.Client, resourceName, kinesisStreamARN string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-cf-source-%s", resourceName)

	trustPolicy := util.TrustPolicy{
		Version: "2012-10-17",
		Statement: []util.TrustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "cloudfront.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	}

	roleARN, err = util.CreateRole(ctx, client, roleName,
		"Rawtree Terraform provider - CloudFront role for real-time log delivery to Kinesis", trustPolicy)
	if err != nil {
		return "", "", "", err
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect: "Allow",
				Action: []string{
					"kinesis:DescribeStreamSummary",
					"kinesis:DescribeStream",
					"kinesis:PutRecord",
					"kinesis:PutRecords",
				},
				Resource: []string{kinesisStreamARN},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-cf-source-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"Kinesis write access for CloudFront real-time logs", policyDoc)
	if err != nil {
		return "", "", "", err
	}

	return roleARN, roleName, policyARN, nil
}

func createFirehoseRole(ctx context.Context, client *iam.Client, resourceName, kinesisStreamARN, bucketName, region string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-cf-firehose-%s", resourceName)

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
		"Rawtree Terraform provider - Firehose role for CloudFront log ingestion", trustPolicy)
	if err != nil {
		return "", "", "", err
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect: "Allow",
				Action: []string{
					"kinesis:DescribeStream",
					"kinesis:GetShardIterator",
					"kinesis:GetRecords",
					"kinesis:ListShards",
				},
				Resource: []string{kinesisStreamARN},
			},
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
					fmt.Sprintf("arn:aws:logs:%s:*:log-group:/aws/firehose/rawtree-*", region),
				},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-cf-firehose-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"Kinesis read, S3 backup, and CloudWatch access for Rawtree CloudFront Firehose", policyDoc)
	if err != nil {
		return "", "", "", err
	}

	// Wait for IAM role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}
