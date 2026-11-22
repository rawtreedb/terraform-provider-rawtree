package cloudfront_ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	ktypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

func createKinesisStream(ctx context.Context, client *kinesis.Client, name string) error {
	_, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{
		StreamName: aws.String(name),
		StreamModeDetails: &ktypes.StreamModeDetails{
			StreamMode: ktypes.StreamModeOnDemand,
		},
		Tags: map[string]string{
			"managed-by": "terraform-provider-rawtree",
		},
	})
	if err != nil {
		return fmt.Errorf("creating Kinesis stream %s: %w", name, err)
	}
	return nil
}

func waitForKinesisActive(ctx context.Context, client *kinesis.Client, name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
			StreamName: aws.String(name),
		})
		if err != nil {
			return "", fmt.Errorf("describing Kinesis stream %s: %w", name, err)
		}

		status := out.StreamDescriptionSummary.StreamStatus
		switch status {
		case ktypes.StreamStatusActive:
			return aws.ToString(out.StreamDescriptionSummary.StreamARN), nil
		}

		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("kinesis stream %s did not become ACTIVE within %s", name, timeout)
}

func deleteKinesisStream(ctx context.Context, client *kinesis.Client, name string) error {
	_, err := client.DeleteStream(ctx, &kinesis.DeleteStreamInput{
		StreamName:              aws.String(name),
		EnforceConsumerDeletion: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("deleting Kinesis stream %s: %w", name, err)
	}
	return nil
}
