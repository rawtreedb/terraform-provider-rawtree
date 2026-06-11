package util

import (
	"context"
	"encoding/json"
	"errors"
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
// If the role already exists, it adopts the existing role and updates its trust policy.
func CreateRole(ctx context.Context, client *iam.Client, roleName, description string, trustPolicy TrustPolicy) (string, error) {
	tp, err := json.Marshal(trustPolicy)
	if err != nil {
		return "", fmt.Errorf("marshaling trust policy for role %s: %w", roleName, err)
	}

	out, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(string(tp)),
		Description:              aws.String(description),
		Tags: []iamtypes.Tag{
			ManagedByIAMTag(),
		},
	})
	if err != nil {
		var exists *iamtypes.EntityAlreadyExistsException
		if !errors.As(err, &exists) {
			return "", fmt.Errorf("creating IAM role %s: %w", roleName, err)
		}
		getOut, getErr := client.GetRole(ctx, &iam.GetRoleInput{
			RoleName: aws.String(roleName),
		})
		if getErr != nil {
			return "", fmt.Errorf("role %s already exists and failed to describe: %w", roleName, getErr)
		}
		if _, updateErr := client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
			RoleName:       aws.String(roleName),
			PolicyDocument: aws.String(string(tp)),
		}); updateErr != nil {
			return "", fmt.Errorf("updating assume-role policy for existing role %s: %w", roleName, updateErr)
		}
		return aws.ToString(getOut.Role.Arn), nil
	}
	return aws.ToString(out.Role.Arn), nil
}

// CreateAndAttachPolicy creates a customer-managed IAM policy and attaches it to a role.
// If the policy already exists, it adopts the existing policy and updates its document.
func CreateAndAttachPolicy(ctx context.Context, client *iam.Client, policyName, roleName, description string, doc PolicyDocument) (string, error) {
	policyJSON, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshaling policy %s: %w", policyName, err)
	}

	var policyARN string
	out, err := client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(string(policyJSON)),
		Description:    aws.String(description),
		Tags: []iamtypes.Tag{
			ManagedByIAMTag(),
		},
	})
	if err != nil {
		var exists *iamtypes.EntityAlreadyExistsException
		if !errors.As(err, &exists) {
			return "", fmt.Errorf("creating policy %s: %w", policyName, err)
		}
		policyARN, err = findPolicyARN(ctx, client, policyName)
		if err != nil {
			return "", err
		}
		if _, err := client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
			PolicyArn:      aws.String(policyARN),
			PolicyDocument: aws.String(string(policyJSON)),
			SetAsDefault:   true,
		}); err != nil {
			var limitErr *iamtypes.LimitExceededException
			if errors.As(err, &limitErr) {
				if delErr := pruneOldestPolicyVersion(ctx, client, policyARN); delErr != nil {
					return "", fmt.Errorf("pruning old policy version for %s: %w", policyName, delErr)
				}
				if _, err := client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
					PolicyArn:      aws.String(policyARN),
					PolicyDocument: aws.String(string(policyJSON)),
					SetAsDefault:   true,
				}); err != nil {
					return "", fmt.Errorf("updating policy %s after pruning: %w", policyName, err)
				}
			} else {
				return "", fmt.Errorf("updating policy %s: %w", policyName, err)
			}
		}
	} else {
		policyARN = aws.ToString(out.Policy.Arn)
	}

	if err := AttachManagedPolicy(ctx, client, roleName, policyARN); err != nil {
		return "", err
	}
	return policyARN, nil
}

// AttachManagedPolicy attaches an AWS managed or customer managed IAM policy to a role.
// This is idempotent — attaching an already-attached policy is a no-op in IAM.
func AttachManagedPolicy(ctx context.Context, client *iam.Client, roleName, policyARN string) error {
	_, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return fmt.Errorf("attaching policy %s to role %s: %w", policyARN, roleName, err)
	}
	return nil
}

func findPolicyARN(ctx context.Context, client *iam.Client, policyName string) (string, error) {
	paginator := iam.NewListPoliciesPaginator(client, &iam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("listing IAM policies to find %s: %w", policyName, err)
		}
		for _, p := range page.Policies {
			if aws.ToString(p.PolicyName) == policyName {
				return aws.ToString(p.Arn), nil
			}
		}
	}
	return "", fmt.Errorf("IAM policy %s exists but could not be found by listing", policyName)
}

func pruneOldestPolicyVersion(ctx context.Context, client *iam.Client, policyARN string) error {
	out, err := client.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return err
	}
	var oldest *iamtypes.PolicyVersion
	for i := range out.Versions {
		v := &out.Versions[i]
		if v.IsDefaultVersion {
			continue
		}
		if oldest == nil || v.CreateDate.Before(*oldest.CreateDate) {
			oldest = v
		}
	}
	if oldest == nil {
		return fmt.Errorf("no non-default policy versions to prune for %s", policyARN)
	}
	_, err = client.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: oldest.VersionId,
	})
	return err
}

// DeleteRole detaches policies and deletes an IAM role.
func DeleteRole(ctx context.Context, client *iam.Client, roleName, customPolicyARN, managedPolicyARN string) error {
	var cleanupErrs []error

	if managedPolicyARN != "" {
		_, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(managedPolicyARN),
		})
		if err != nil && !isIAMNotFound(err) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("detaching managed policy %s from role %s: %w", managedPolicyARN, roleName, err))
		}
	}

	if customPolicyARN != "" {
		_, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(customPolicyARN),
		})
		if err != nil && !isIAMNotFound(err) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("detaching custom policy %s from role %s: %w", customPolicyARN, roleName, err))
		}

		_, err = client.DeletePolicy(ctx, &iam.DeletePolicyInput{
			PolicyArn: aws.String(customPolicyARN),
		})
		if err != nil && !isIAMNotFound(err) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("deleting custom policy %s: %w", customPolicyARN, err))
		}
	}

	_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		if isIAMNotFound(err) {
			return errors.Join(cleanupErrs...)
		}
		cleanupErrs = append(cleanupErrs, fmt.Errorf("deleting IAM role %s: %w", roleName, err))
	}

	return errors.Join(cleanupErrs...)
}

func isIAMNotFound(err error) bool {
	var notFound *iamtypes.NoSuchEntityException
	return errors.As(err, &notFound)
}
