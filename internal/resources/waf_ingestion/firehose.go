package waf_ingestion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
)

type firehoseConfig struct {
	Name             string
	EndpointURL      string
	AccessKey        string
	RoleARN          string
	BucketARN        string
	BufferingSizeMB  int32
	BufferingSeconds int32
	S3BackupMode     string
}

func createDeliveryStream(ctx context.Context, client *firehose.Client, cfg firehoseConfig) (string, error) {
	backupMode := fhtypes.HttpEndpointS3BackupModeFailedDataOnly
	if cfg.S3BackupMode == "AllData" {
		backupMode = fhtypes.HttpEndpointS3BackupModeAllData
	}

	input := &firehose.CreateDeliveryStreamInput{
		DeliveryStreamName: aws.String(cfg.Name),
		DeliveryStreamType: fhtypes.DeliveryStreamTypeDirectPut,
		HttpEndpointDestinationConfiguration: &fhtypes.HttpEndpointDestinationConfiguration{
			EndpointConfiguration: &fhtypes.HttpEndpointConfiguration{
				Url:       aws.String(cfg.EndpointURL),
				Name:      aws.String("Rawtree"),
				AccessKey: aws.String(cfg.AccessKey),
			},
			BufferingHints: &fhtypes.HttpEndpointBufferingHints{
				SizeInMBs:         aws.Int32(cfg.BufferingSizeMB),
				IntervalInSeconds: aws.Int32(cfg.BufferingSeconds),
			},
			RoleARN:      aws.String(cfg.RoleARN),
			S3BackupMode: backupMode,
			S3Configuration: &fhtypes.S3DestinationConfiguration{
				BucketARN: aws.String(cfg.BucketARN),
				RoleARN:   aws.String(cfg.RoleARN),
				BufferingHints: &fhtypes.BufferingHints{
					SizeInMBs:         aws.Int32(5),
					IntervalInSeconds: aws.Int32(300),
				},
				CompressionFormat: fhtypes.CompressionFormatGzip,
			},
			RetryOptions: &fhtypes.HttpEndpointRetryOptions{
				DurationInSeconds: aws.Int32(300),
			},
		},
		Tags: []fhtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	}

	out, err := client.CreateDeliveryStream(ctx, input)
	if err != nil {
		return "", fmt.Errorf("creating Firehose delivery stream %s: %w", cfg.Name, err)
	}

	return aws.ToString(out.DeliveryStreamARN), nil
}

func waitForFirehoseActive(ctx context.Context, client *firehose.Client, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(name),
		})
		if err != nil {
			return fmt.Errorf("describing Firehose %s: %w", name, err)
		}

		status := out.DeliveryStreamDescription.DeliveryStreamStatus
		switch status {
		case fhtypes.DeliveryStreamStatusActive:
			return nil
		case fhtypes.DeliveryStreamStatusCreatingFailed:
			return fmt.Errorf("Firehose %s creation failed", name)
		}

		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("Firehose %s did not become ACTIVE within %s", name, timeout)
}

func updateDeliveryStream(ctx context.Context, client *firehose.Client, name string, cfg firehoseConfig) error {
	// Get current version ID.
	desc, err := client.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
		DeliveryStreamName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("describing Firehose %s for update: %w", name, err)
	}

	versionID := desc.DeliveryStreamDescription.VersionId

	backupMode := fhtypes.HttpEndpointS3BackupModeFailedDataOnly
	if cfg.S3BackupMode == "AllData" {
		backupMode = fhtypes.HttpEndpointS3BackupModeAllData
	}

	_, err = client.UpdateDestination(ctx, &firehose.UpdateDestinationInput{
		DeliveryStreamName:             aws.String(name),
		CurrentDeliveryStreamVersionId: versionID,
		DestinationId:                  desc.DeliveryStreamDescription.Destinations[0].DestinationId,
		HttpEndpointDestinationUpdate: &fhtypes.HttpEndpointDestinationUpdate{
			EndpointConfiguration: &fhtypes.HttpEndpointConfiguration{
				Url:       aws.String(cfg.EndpointURL),
				Name:      aws.String("Rawtree"),
				AccessKey: aws.String(cfg.AccessKey),
			},
			BufferingHints: &fhtypes.HttpEndpointBufferingHints{
				SizeInMBs:         aws.Int32(cfg.BufferingSizeMB),
				IntervalInSeconds: aws.Int32(cfg.BufferingSeconds),
			},
			RoleARN:      aws.String(cfg.RoleARN),
			S3BackupMode: backupMode,
			S3Update: &fhtypes.S3DestinationUpdate{
				BucketARN: aws.String(cfg.BucketARN),
				RoleARN:   aws.String(cfg.RoleARN),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("updating Firehose %s: %w", name, err)
	}

	return nil
}

func deleteDeliveryStream(ctx context.Context, client *firehose.Client, name string) error {
	_, err := client.DeleteDeliveryStream(ctx, &firehose.DeleteDeliveryStreamInput{
		DeliveryStreamName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("deleting Firehose %s: %w", name, err)
	}
	return nil
}

func waitForFirehoseDeleted(ctx context.Context, client *firehose.Client, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := client.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(name),
		})
		if err != nil {
			var notFound *fhtypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				return nil
			}
			return fmt.Errorf("error checking Firehose %s deletion status: %w", name, err)
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("Firehose %s was not deleted within %s", name, timeout)
}
