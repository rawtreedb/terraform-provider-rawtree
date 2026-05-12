package mongo_connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type connectorSecrets struct {
	MongoURI      string `json:"mongo_uri"`
	RawtreeAPIKey string `json:"rawtree_api_key"`
}

func createSecret(ctx context.Context, client *secretsmanager.Client, name, mongoURI, apiKey string) (string, error) {
	payload := connectorSecrets{
		MongoURI:      mongoURI,
		RawtreeAPIKey: apiKey,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	out, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         &name,
		SecretString: aws.String(string(data)),
		Tags: []smtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create secret: %w", err)
	}
	return *out.ARN, nil
}

func updateSecret(ctx context.Context, client *secretsmanager.Client, secretARN, mongoURI, apiKey string) error {
	payload := connectorSecrets{
		MongoURI:      mongoURI,
		RawtreeAPIKey: apiKey,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = client.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
		SecretId:     &secretARN,
		SecretString: aws.String(string(data)),
	})
	if err != nil {
		return fmt.Errorf("update secret: %w", err)
	}
	return nil
}

func deleteSecret(ctx context.Context, client *secretsmanager.Client, secretARN string) error {
	_, err := client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   &secretARN,
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if err != nil {
		var notFound *smtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}
