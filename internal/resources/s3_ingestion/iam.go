package s3_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

// createGlueRole creates an IAM role for the Glue job with S3 read access.
func createGlueRole(ctx context.Context, client *iam.Client, resourceName, bucket, prefix string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-glue-%s", resourceName)

	trustPolicy := util.TrustPolicy{
		Version: "2012-10-17",
		Statement: []util.TrustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "glue.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	}

	roleARN, err = util.CreateRole(ctx, client, roleName,
		"Rawtree Terraform provider - Glue job role for S3 ingestion", trustPolicy)
	if err != nil {
		return "", "", "", fmt.Errorf("creating Glue IAM role: %w", err)
	}

	if err := util.AttachManagedPolicy(ctx, client, roleName, "arn:aws:iam::aws:policy/service-role/AWSGlueServiceRole"); err != nil {
		return "", "", "", fmt.Errorf("attaching AWSGlueServiceRole policy: %w", err)
	}

	// Create inline S3 read policy.
	// Always include .rawtree/* for the Glue script stored in the source bucket.
	s3Resources := []string{
		fmt.Sprintf("arn:aws:s3:::%s", bucket),
		fmt.Sprintf("arn:aws:s3:::%s/.rawtree/*", bucket),
	}
	if prefix != "" {
		s3Resources = append(s3Resources, fmt.Sprintf("arn:aws:s3:::%s/%s*", bucket, prefix))
	} else {
		s3Resources = append(s3Resources, fmt.Sprintf("arn:aws:s3:::%s/*", bucket))
	}

	s3Policy := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:ListBucket", "s3:GetBucketLocation"},
				Resource: s3Resources,
			},
			{
				Effect:   "Allow",
				Action:   []string{"kms:Decrypt", "kms:GenerateDataKey"},
				Resource: []string{"*"},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-glue-s3-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"S3 read access for Rawtree Glue ingestion job", s3Policy)
	if err != nil {
		return "", "", "", fmt.Errorf("creating S3 policy for Glue: %w", err)
	}

	// Wait for role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}

// createLambdaRole creates an IAM role for the Lambda function with S3 read access.
func createLambdaRole(ctx context.Context, client *iam.Client, resourceName, bucket, prefix string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-lambda-%s", resourceName)

	trustPolicy := util.TrustPolicy{
		Version: "2012-10-17",
		Statement: []util.TrustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "lambda.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	}

	roleARN, err = util.CreateRole(ctx, client, roleName,
		"Rawtree Terraform provider - Lambda role for S3 ingestion", trustPolicy)
	if err != nil {
		return "", "", "", fmt.Errorf("creating Lambda IAM role: %w", err)
	}

	if err := util.AttachManagedPolicy(ctx, client, roleName, "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"); err != nil {
		return "", "", "", fmt.Errorf("attaching Lambda basic execution role: %w", err)
	}

	// Create S3 read policy.
	s3Resources := []string{
		fmt.Sprintf("arn:aws:s3:::%s", bucket),
	}
	if prefix != "" {
		s3Resources = append(s3Resources, fmt.Sprintf("arn:aws:s3:::%s/%s*", bucket, prefix))
	} else {
		s3Resources = append(s3Resources, fmt.Sprintf("arn:aws:s3:::%s/*", bucket))
	}

	s3Policy := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:GetBucketLocation"},
				Resource: s3Resources,
			},
			{
				Effect:   "Allow",
				Action:   []string{"kms:Decrypt", "kms:GenerateDataKey"},
				Resource: []string{"*"},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-lambda-s3-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"S3 read access for Rawtree Lambda ingestion function", s3Policy)
	if err != nil {
		return "", "", "", fmt.Errorf("creating S3 policy for Lambda: %w", err)
	}

	// Wait for role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}
