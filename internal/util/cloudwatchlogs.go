package util

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func CreateLogGroup(ctx context.Context, client *cloudwatchlogs.Client, name string, retentionDays int64) error {
	_, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(name),
		Tags:         ManagedByTagMap(),
	})
	if err != nil {
		var exists *logstypes.ResourceAlreadyExistsException
		if !errors.As(err, &exists) {
			return fmt.Errorf("creating log group %s: %w", name, err)
		}
	}
	return PutLogRetention(ctx, client, name, retentionDays)
}

func PutLogRetention(ctx context.Context, client *cloudwatchlogs.Client, name string, retentionDays int64) error {
	_, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(name),
		RetentionInDays: aws.Int32(int32(retentionDays)),
	})
	if err != nil {
		return fmt.Errorf("setting retention for log group %s: %w", name, err)
	}
	return nil
}

func DeleteLogGroup(ctx context.Context, client *cloudwatchlogs.Client, name string) error {
	if name == "" {
		return nil
	}
	_, err := client.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(name),
	})
	if err != nil {
		var notFound *logstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("deleting log group %s: %w", name, err)
	}
	return nil
}
