package waf_ingestion

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func createBackupBucket(ctx context.Context, client *s3.Client, bucketName, region string) error {
	return util.CreateBackupBucket(ctx, client, bucketName, region, "waf-firehose-backup")
}

func deleteBackupBucket(ctx context.Context, client *s3.Client, bucketName string) error {
	return util.DeleteBackupBucket(ctx, client, bucketName)
}
