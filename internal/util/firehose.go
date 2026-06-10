package util

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
)

func WaitForFirehoseActive(ctx context.Context, client *firehose.Client, name string, timeout time.Duration) error {
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
			return fmt.Errorf("firehose %s creation failed", name)
		}

		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("firehose %s did not become ACTIVE within %s", name, timeout)
}

func DeleteDeliveryStream(ctx context.Context, client *firehose.Client, name string) error {
	_, err := client.DeleteDeliveryStream(ctx, &firehose.DeleteDeliveryStreamInput{
		DeliveryStreamName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("deleting Firehose %s: %w", name, err)
	}
	return nil
}

func WaitForFirehoseDeleted(ctx context.Context, client *firehose.Client, name string, timeout time.Duration) error {
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
	return fmt.Errorf("firehose %s was not deleted within %s", name, timeout)
}
