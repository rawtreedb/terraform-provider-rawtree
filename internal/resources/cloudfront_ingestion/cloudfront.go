package cloudfront_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func createRealtimeLogConfig(ctx context.Context, client *cloudfront.Client, name string, fields []string, samplingRate int64, kinesisStreamARN, cfRoleARN string) (string, error) {
	out, err := client.CreateRealtimeLogConfig(ctx, &cloudfront.CreateRealtimeLogConfigInput{
		Name:         aws.String(name),
		SamplingRate: aws.Int64(samplingRate),
		Fields:       fields,
		EndPoints: []cftypes.EndPoint{
			{
				StreamType: aws.String("Kinesis"),
				KinesisStreamConfig: &cftypes.KinesisStreamConfig{
					RoleARN:   aws.String(cfRoleARN),
					StreamARN: aws.String(kinesisStreamARN),
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating real-time log config %s: %w", name, err)
	}

	return aws.ToString(out.RealtimeLogConfig.ARN), nil
}

func getRealtimeLogConfig(ctx context.Context, client *cloudfront.Client, name string) (*cftypes.RealtimeLogConfig, error) {
	out, err := client.GetRealtimeLogConfig(ctx, &cloudfront.GetRealtimeLogConfigInput{
		Name: aws.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("getting real-time log config %s: %w", name, err)
	}
	return out.RealtimeLogConfig, nil
}

func updateRealtimeLogConfig(ctx context.Context, client *cloudfront.Client, arn string, fields []string, samplingRate int64, kinesisStreamARN, cfRoleARN string) error {
	_, err := client.UpdateRealtimeLogConfig(ctx, &cloudfront.UpdateRealtimeLogConfigInput{
		ARN:          aws.String(arn),
		SamplingRate: aws.Int64(samplingRate),
		Fields:       fields,
		EndPoints: []cftypes.EndPoint{
			{
				StreamType: aws.String("Kinesis"),
				KinesisStreamConfig: &cftypes.KinesisStreamConfig{
					RoleARN:   aws.String(cfRoleARN),
					StreamARN: aws.String(kinesisStreamARN),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("updating real-time log config %s: %w", arn, err)
	}
	return nil
}

func deleteRealtimeLogConfig(ctx context.Context, client *cloudfront.Client, arn string) error {
	_, err := client.DeleteRealtimeLogConfig(ctx, &cloudfront.DeleteRealtimeLogConfigInput{
		ARN: aws.String(arn),
	})
	if err != nil {
		return fmt.Errorf("deleting real-time log config: %w", err)
	}
	return nil
}

func deleteRealtimeLogConfigWithRetry(ctx context.Context, client *cloudfront.Client, arn string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := deleteRealtimeLogConfig(ctx, client, arn)
		if err == nil {
			return nil
		}
		// Config might still be attached to a distribution being updated; retry.
		time.Sleep(10 * time.Second)
	}
	return deleteRealtimeLogConfig(ctx, client, arn)
}

func attachToDistribution(ctx context.Context, client *cloudfront.Client, distributionID, realtimeLogConfigARN string) error {
	cfg, err := client.GetDistributionConfig(ctx, &cloudfront.GetDistributionConfigInput{
		Id: aws.String(distributionID),
	})
	if err != nil {
		return fmt.Errorf("getting distribution config %s: %w", distributionID, err)
	}

	cfg.DistributionConfig.DefaultCacheBehavior.RealtimeLogConfigArn = aws.String(realtimeLogConfigARN)

	_, err = client.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
		Id:                 aws.String(distributionID),
		DistributionConfig: cfg.DistributionConfig,
		IfMatch:            cfg.ETag,
	})
	if err != nil {
		return fmt.Errorf("attaching real-time log config to distribution %s: %w", distributionID, err)
	}

	return nil
}

func detachFromDistribution(ctx context.Context, client *cloudfront.Client, distributionID, expectedARN string) error {
	cfg, err := client.GetDistributionConfig(ctx, &cloudfront.GetDistributionConfigInput{
		Id: aws.String(distributionID),
	})
	if err != nil {
		return fmt.Errorf("getting distribution config %s: %w", distributionID, err)
	}

	currentARN := cfg.DistributionConfig.DefaultCacheBehavior.RealtimeLogConfigArn
	if currentARN == nil {
		return nil
	}

	// Only detach if the distribution still points at the config we own.
	if aws.ToString(currentARN) != expectedARN {
		return nil
	}

	cfg.DistributionConfig.DefaultCacheBehavior.RealtimeLogConfigArn = nil

	_, err = client.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
		Id:                 aws.String(distributionID),
		DistributionConfig: cfg.DistributionConfig,
		IfMatch:            cfg.ETag,
	})
	if err != nil {
		return fmt.Errorf("detaching real-time log config from distribution %s: %w", distributionID, err)
	}

	return nil
}
