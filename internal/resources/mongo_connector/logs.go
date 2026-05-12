package mongo_connector

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func createLogGroup(ctx context.Context, client *cloudwatchlogs.Client, name string) error {
	_, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: &name,
		Tags: map[string]string{
			"managed-by": "terraform-provider-rawtree",
		},
	})
	if err != nil {
		var exists *cwltypes.ResourceAlreadyExistsException
		if errors.As(err, &exists) {
			return nil
		}
		return fmt.Errorf("create log group: %w", err)
	}

	_, err = client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    &name,
		RetentionInDays: aws.Int32(30),
	})
	if err != nil {
		return fmt.Errorf("set log retention: %w", err)
	}

	return nil
}

func deleteLogGroup(ctx context.Context, client *cloudwatchlogs.Client, name string) error {
	_, err := client.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: &name,
	})
	if err != nil {
		var notFound *cwltypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("delete log group: %w", err)
	}
	return nil
}
