package util

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

func CreateSecret(ctx context.Context, client *secretsmanager.Client, name, description, value string) (string, error) {
	out, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(name),
		Description:  aws.String(description),
		SecretString: aws.String(value),
		Tags: []secretsmanagertypes.Tag{
			{Key: aws.String(ManagedByTagKey), Value: aws.String(ManagedByTagValue)},
		},
	})
	if err != nil {
		var exists *secretsmanagertypes.ResourceExistsException
		if !errors.As(err, &exists) {
			return "", fmt.Errorf("creating secret %s: %w", name, err)
		}
		desc, descErr := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: aws.String(name),
		})
		if descErr != nil {
			return "", fmt.Errorf("secret %s already exists and failed to describe: %w", name, descErr)
		}
		if err := PutSecretValue(ctx, client, name, value); err != nil {
			return "", err
		}
		return aws.ToString(desc.ARN), nil
	}
	return aws.ToString(out.ARN), nil
}

func PutSecretValue(ctx context.Context, client *secretsmanager.Client, secretID, value string) error {
	_, err := client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(secretID),
		SecretString: aws.String(value),
	})
	if err != nil {
		return fmt.Errorf("updating secret %s: %w", secretID, err)
	}
	return nil
}

func DeleteSecret(ctx context.Context, client *secretsmanager.Client, secretID string) error {
	if secretID == "" {
		return nil
	}
	_, err := client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(secretID),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if err != nil {
		var notFound *secretsmanagertypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("deleting secret %s: %w", secretID, err)
	}
	return nil
}
