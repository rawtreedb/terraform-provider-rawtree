package util

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type TrustPolicyStatement struct {
	Effect    string            `json:"Effect"`
	Principal map[string]string `json:"Principal"`
	Action    string            `json:"Action"`
}

type TrustPolicy struct {
	Version   string                 `json:"Version"`
	Statement []TrustPolicyStatement `json:"Statement"`
}

type PolicyStatement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

type PolicyDocument struct {
	Version   string            `json:"Version"`
	Statement []PolicyStatement `json:"Statement"`
}

// CreateRole creates an IAM role with the given trust policy and tags.
func CreateRole(ctx context.Context, client *iam.Client, roleName, description string, trustPolicy TrustPolicy) (string, error) {
	tp, _ := json.Marshal(trustPolicy)

	out, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(tp)),
		Description:              aws.String(description),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating IAM role %s: %w", roleName, err)
	}
	return aws.ToString(out.Role.Arn), nil
}

// CreateAndAttachPolicy creates a customer-managed IAM policy and attaches it to a role.
func CreateAndAttachPolicy(ctx context.Context, client *iam.Client, policyName, roleName, description string, doc PolicyDocument) (string, error) {
	policyJSON, _ := json.Marshal(doc)

	out, err := client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(string(policyJSON)),
		Description:    aws.String(description),
		Tags: []iamtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating policy %s: %w", policyName, err)
	}
	policyARN := aws.ToString(out.Policy.Arn)

	_, err = client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return "", fmt.Errorf("attaching policy %s to role %s: %w", policyName, roleName, err)
	}

	return policyARN, nil
}

// AttachManagedPolicy attaches an AWS-managed policy to a role.
func AttachManagedPolicy(ctx context.Context, client *iam.Client, roleName, policyARN string) error {
	_, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return fmt.Errorf("attaching managed policy %s to role %s: %w", policyARN, roleName, err)
	}
	return nil
}

// DeleteRole detaches policies and deletes an IAM role.
func DeleteRole(ctx context.Context, client *iam.Client, roleName, customPolicyARN, managedPolicyARN string) error {
	if managedPolicyARN != "" {
		_, _ = client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(managedPolicyARN),
		})
	}

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
