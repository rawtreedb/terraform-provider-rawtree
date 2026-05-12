package waf_ingestion

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func createBackupBucket(ctx context.Context, client *s3.Client, bucketName, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}
	// us-east-1 must not specify a LocationConstraint.
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}

	_, err := client.CreateBucket(ctx, input)
	if err != nil {
		return fmt.Errorf("creating S3 backup bucket %s: %w", bucketName, err)
	}

	// Add lifecycle rule to expire objects after 30 days.
	_, err = client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucketName),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{
			Rules: []s3types.LifecycleRule{
				{
					ID:     aws.String("rawtree-expire-failed-deliveries"),
					Status: s3types.ExpirationStatusEnabled,
					Filter: &s3types.LifecycleRuleFilter{
						Prefix: aws.String(""),
					},
					Expiration: &s3types.LifecycleExpiration{
						Days: aws.Int32(30),
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("setting lifecycle on bucket %s: %w", bucketName, err)
	}

	// Tag the bucket.
	_, err = client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bucketName),
		Tagging: &s3types.Tagging{
			TagSet: []s3types.Tag{
				{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
				{Key: aws.String("purpose"), Value: aws.String("waf-firehose-backup")},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("tagging bucket %s: %w", bucketName, err)
	}

	return nil
}

func deleteBackupBucket(ctx context.Context, client *s3.Client, bucketName string) error {
	// Empty the bucket first (required before deletion).
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			var nsk *s3types.NoSuchBucket
			if errors.As(err, &nsk) {
				return nil
			}
			return fmt.Errorf("listing objects in bucket %s: %w", bucketName, err)
		}

		if len(page.Contents) == 0 {
			continue
		}

		objects := make([]s3types.ObjectIdentifier, len(page.Contents))
		for i, obj := range page.Contents {
			objects[i] = s3types.ObjectIdentifier{Key: obj.Key}
		}

		_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucketName),
			Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("deleting objects in bucket %s: %w", bucketName, err)
		}
	}

	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var nsk *s3types.NoSuchBucket
		if errors.As(err, &nsk) {
			return nil
		}
		return fmt.Errorf("deleting bucket %s: %w", bucketName, err)
	}

	return nil
}
