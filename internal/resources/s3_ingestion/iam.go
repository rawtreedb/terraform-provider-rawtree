package s3_ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type trustPolicyStatement struct {
	Effect    string            `json:"Effect"`
	Principal map[string]string `json:"Principal"`
	Action    string            `json:"Action"`
}

type trustPolicy struct {
	Version   string                 `json:"Version"`
	Statement []trustPolicyStatement `json:"Statement"`
}

type policyStatement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

// createGlueRole creates an IAM role for the Glue job with S3 read access.
func createGlueRole(ctx context.Context, client *iam.Client, resourceName, bucket, prefix string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-glue-%s", resourceName)

	tp, _ := json.Marshal(trustPolicy{
		Version: "2012-10-17",
		Statement: []trustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "glue.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	})

	createRoleOut, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(tp)),
		Description:              aws.String("Rawtree Terraform provider - Glue job role for S3 ingestion"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", "", "", fmt.Errorf("creating Glue IAM role: %w", err)
	}
	roleARN = aws.ToString(createRoleOut.Role.Arn)

	// Attach AWSGlueServiceRole managed policy.
	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String("arn:aws:iam::aws:policy/service-role/AWSGlueServiceRole"),
	})
	if err != nil {
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

	s3Policy, _ := json.Marshal(policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
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
	})

	policyName := fmt.Sprintf("rawtree-glue-s3-%s", resourceName)
	createPolicyOut, err := client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(string(s3Policy)),
		Description:    aws.String("S3 read access for Rawtree Glue ingestion job"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", "", "", fmt.Errorf("creating S3 policy for Glue: %w", err)
	}
	policyARN = aws.ToString(createPolicyOut.Policy.Arn)

	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return "", "", "", fmt.Errorf("attaching S3 policy to Glue role: %w", err)
	}

	// Wait for role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}

// createLambdaRole creates an IAM role for the Lambda function with S3 read access.
func createLambdaRole(ctx context.Context, client *iam.Client, resourceName, bucket, prefix string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-lambda-%s", resourceName)

	tp, _ := json.Marshal(trustPolicy{
		Version: "2012-10-17",
		Statement: []trustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "lambda.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	})

	createRoleOut, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(tp)),
		Description:              aws.String("Rawtree Terraform provider - Lambda role for S3 ingestion"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", "", "", fmt.Errorf("creating Lambda IAM role: %w", err)
	}
	roleARN = aws.ToString(createRoleOut.Role.Arn)

	// Attach basic Lambda execution role.
	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	})
	if err != nil {
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

	s3Policy, _ := json.Marshal(policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
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
	})

	policyName := fmt.Sprintf("rawtree-lambda-s3-%s", resourceName)
	createPolicyOut, err := client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(string(s3Policy)),
		Description:    aws.String("S3 read access for Rawtree Lambda ingestion function"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", "", "", fmt.Errorf("creating S3 policy for Lambda: %w", err)
	}
	policyARN = aws.ToString(createPolicyOut.Policy.Arn)

	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return "", "", "", fmt.Errorf("attaching S3 policy to Lambda role: %w", err)
	}

	// Wait for role propagation.
	time.Sleep(10 * time.Second)

	return roleARN, roleName, policyARN, nil
}

// deleteRole detaches policies and deletes an IAM role.
func deleteRole(ctx context.Context, client *iam.Client, roleName, customPolicyARN string, managedPolicyARN string) error {
	// Detach managed policy.
	if managedPolicyARN != "" {
		_, _ = client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(managedPolicyARN),
		})
	}

	// Detach and delete custom policy.
	if customPolicyARN != "" {
		_, _ = client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(customPolicyARN),
		})
		_, _ = client.DeletePolicy(ctx, &iam.DeletePolicyInput{
			PolicyArn: aws.String(customPolicyARN),
		})
	}

	_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("deleting IAM role %s: %w", roleName, err)
	}

	return nil
}
