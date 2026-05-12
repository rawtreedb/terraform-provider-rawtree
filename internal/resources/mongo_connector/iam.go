package mongo_connector

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var ecsTrustPolicy = util.TrustPolicy{
	Version: "2012-10-17",
	Statement: []util.TrustPolicyStatement{
		{
			Effect:    "Allow",
			Principal: map[string]string{"Service": "ecs-tasks.amazonaws.com"},
			Action:    "sts:AssumeRole",
		},
	},
}

func createExecutionRole(ctx context.Context, client *iam.Client, resourceName, secretARN, logGroupName, region string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-mongo-exec-%s", resourceName)
	roleARN, err = util.CreateRole(ctx, client, roleName, "Rawtree MongoDB connector ECS execution role", ecsTrustPolicy)
	if err != nil {
		return "", "", "", fmt.Errorf("create execution role: %w", err)
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect:   "Allow",
				Action:   []string{"secretsmanager:GetSecretValue"},
				Resource: []string{secretARN},
			},
			{
				Effect:   "Allow",
				Action:   []string{"logs:CreateLogStream", "logs:PutLogEvents"},
				Resource: []string{fmt.Sprintf("arn:aws:logs:%s:*:log-group:%s:*", region, logGroupName)},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-mongo-exec-policy-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName, "Rawtree MongoDB connector execution policy", policyDoc)
	if err != nil {
		return "", "", "", fmt.Errorf("create execution policy: %w", err)
	}

	return roleARN, roleName, policyARN, nil
}

func createTaskRole(ctx context.Context, client *iam.Client, resourceName string) (roleARN, roleName, policyARN string, err error) {
	roleName = fmt.Sprintf("rawtree-mongo-task-%s", resourceName)
	roleARN, err = util.CreateRole(ctx, client, roleName, "Rawtree MongoDB connector ECS task role", ecsTrustPolicy)
	if err != nil {
		return "", "", "", fmt.Errorf("create task role: %w", err)
	}

	policyDoc := util.PolicyDocument{
		Version: "2012-10-17",
		Statement: []util.PolicyStatement{
			{
				Effect:   "Allow",
				Action:   []string{"logs:CreateLogStream", "logs:PutLogEvents"},
				Resource: []string{"*"},
			},
		},
	}

	policyName := fmt.Sprintf("rawtree-mongo-task-policy-%s", resourceName)
	policyARN, err = util.CreateAndAttachPolicy(ctx, client, policyName, roleName, "Rawtree MongoDB connector task policy", policyDoc)
	if err != nil {
		return "", "", "", fmt.Errorf("create task policy: %w", err)
	}

	return roleARN, roleName, policyARN, nil
}
