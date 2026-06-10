package cloudfront_ingestion

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
)

type firehoseConfig struct {
	Name             string
	EndpointURL      string
	AccessKey        string
	FirehoseRoleARN  string
	KinesisStreamARN string
	BucketARN        string
	BufferingSizeMB  int32
	BufferingSeconds int32
	S3BackupMode     string
	Region           string
}

func createDeliveryStream(ctx context.Context, client *firehose.Client, logsClient *cloudwatchlogs.Client, cfg firehoseConfig) (string, error) {
	backupMode := fhtypes.HttpEndpointS3BackupModeFailedDataOnly
	if cfg.S3BackupMode == "AllData" {
		backupMode = fhtypes.HttpEndpointS3BackupModeAllData
	}

	logGroup := fmt.Sprintf("/aws/firehose/%s", cfg.Name)
	logStream := "HttpEndpointDelivery"

	// Pre-create log group + stream. Firehose won't create the stream itself.
	_, _ = logsClient.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(logGroup),
	})
	_, _ = logsClient.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(logGroup),
		RetentionInDays: aws.Int32(1),
	})
	_, _ = logsClient.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(logGroup),
		LogStreamName: aws.String(logStream),
	})

	input := &firehose.CreateDeliveryStreamInput{
		DeliveryStreamName: aws.String(cfg.Name),
		DeliveryStreamType: fhtypes.DeliveryStreamTypeKinesisStreamAsSource,
		KinesisStreamSourceConfiguration: &fhtypes.KinesisStreamSourceConfiguration{
			KinesisStreamARN: aws.String(cfg.KinesisStreamARN),
			RoleARN:          aws.String(cfg.FirehoseRoleARN),
		},
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
			RoleARN:      aws.String(cfg.FirehoseRoleARN),
			S3BackupMode: backupMode,
			S3Configuration: &fhtypes.S3DestinationConfiguration{
				BucketARN: aws.String(cfg.BucketARN),
				RoleARN:   aws.String(cfg.FirehoseRoleARN),
				BufferingHints: &fhtypes.BufferingHints{
					SizeInMBs:         aws.Int32(5),
					IntervalInSeconds: aws.Int32(300),
				},
				CompressionFormat: fhtypes.CompressionFormatGzip,
			},
			CloudWatchLoggingOptions: &fhtypes.CloudWatchLoggingOptions{
				Enabled:       aws.Bool(true),
				LogGroupName:  aws.String(logGroup),
				LogStreamName: aws.String(logStream),
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

func updateDeliveryStream(ctx context.Context, client *firehose.Client, name string, cfg firehoseConfig) error {
	desc, err := client.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
		DeliveryStreamName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("describing Firehose %s for update: %w", name, err)
	}

	versionID := desc.DeliveryStreamDescription.VersionId

	if len(desc.DeliveryStreamDescription.Destinations) == 0 {
		return fmt.Errorf("Firehose %s has no destinations configured", name)
	}
	destinationID := desc.DeliveryStreamDescription.Destinations[0].DestinationId

	backupMode := fhtypes.HttpEndpointS3BackupModeFailedDataOnly
	if cfg.S3BackupMode == "AllData" {
		backupMode = fhtypes.HttpEndpointS3BackupModeAllData
	}

	_, err = client.UpdateDestination(ctx, &firehose.UpdateDestinationInput{
		DeliveryStreamName:             aws.String(name),
		CurrentDeliveryStreamVersionId: versionID,
		DestinationId:                  destinationID,
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
			RoleARN:      aws.String(cfg.FirehoseRoleARN),
			S3BackupMode: backupMode,
			S3Update: &fhtypes.S3DestinationUpdate{
				BucketARN: aws.String(cfg.BucketARN),
				RoleARN:   aws.String(cfg.FirehoseRoleARN),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("updating Firehose %s: %w", name, err)
	}

	return nil
}
