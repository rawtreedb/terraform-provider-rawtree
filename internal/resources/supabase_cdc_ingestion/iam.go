package supabase_cdc_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

const ecsTaskExecutionManagedPolicyARN = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"

func createExecutionRole(ctx context.Context, client *iam.Client, resourceName string, secretARNs []string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-ecs-%s", resourceName)

	trustPolicy := util.TrustPolicy{
		Version: "2012-10-17",
		Statement: []util.TrustPolicyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"Service": "ecs-tasks.amazonaws.com"},
				Action:    "sts:AssumeRole",
			},
		},
	}

	roleARN, err = util.CreateRole(ctx, client, roleName,
		"Rawtree Terraform provider - ECS execution role for Supabase CDC ingestion", trustPolicy)
	if err != nil {
		return "", "", "", err
	}

	if err := util.AttachManagedPolicy(ctx, client, roleName, ecsTaskExecutionManagedPolicyARN); err != nil {
		return "", "", "", fmt.Errorf("attaching ECS execution managed policy: %w", err)
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect: "Allow",
				Action: []string{
					"secretsmanager:GetSecretValue",
				},
				Resource: secretARNs,
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-ecs-secrets-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName,
		"Secrets Manager access for Rawtree Supabase CDC ECS task", policyDoc)
	if err != nil {
		return "", "", "", err
	}

	time.Sleep(10 * time.Second)
	return roleARN, roleName, policyARN, nil
}
