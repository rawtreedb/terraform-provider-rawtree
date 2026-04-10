package s3_ingestion

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

//go:embed scripts/glue_job.py
var glueJobScript []byte

// createScriptBucket creates an S3 bucket to store the Glue job script.
func createScriptBucket(ctx context.Context, s3Client *s3.Client, bucketName, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}

	// Only set LocationConstraint for non-us-east-1 regions.
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}

	_, err := s3Client.CreateBucket(ctx, input)
	if err != nil {
		return fmt.Errorf("creating script bucket: %w", err)
	}

	return nil
}

// uploadGlueScript uploads the embedded Glue job script to S3.
func uploadGlueScript(ctx context.Context, s3Client *s3.Client, bucket, key string) error {
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(glueJobScript),
	})
	if err != nil {
		return fmt.Errorf("uploading Glue script: %w", err)
	}
	return nil
}

// createGlueJob creates an AWS Glue job for batch S3 ingestion.
func createGlueJob(ctx context.Context, glueClient *glue.Client, jobName, roleARN, scriptBucket, scriptKey string, params map[string]string) error {
	defaultArgs := make(map[string]string)
	for k, v := range params {
		defaultArgs["--"+k] = v
	}

	_, err := glueClient.CreateJob(ctx, &glue.CreateJobInput{
		Name: aws.String(jobName),
		Role: aws.String(roleARN),
		Command: &gluetypes.JobCommand{
			Name:           aws.String("pythonshell"),
			PythonVersion:  aws.String("3.9"),
			ScriptLocation: aws.String(fmt.Sprintf("s3://%s/%s", scriptBucket, scriptKey)),
		},
		DefaultArguments: defaultArgs,
		GlueVersion:      aws.String("3.0"),
		MaxCapacity:      aws.Float64(0.0625), // 1/16 DPU for Python Shell.
		MaxRetries:       0,
		Timeout:          aws.Int32(480), // 8 hours max.
		Tags: map[string]string{
			"managed-by": "terraform-provider-rawtree",
		},
	})
	if err != nil {
		return fmt.Errorf("creating Glue job: %w", err)
	}
	return nil
}

// startGlueJobRun triggers a one-time run of the Glue job.
func startGlueJobRun(ctx context.Context, glueClient *glue.Client, jobName string) (string, error) {
	out, err := glueClient.StartJobRun(ctx, &glue.StartJobRunInput{
		JobName: aws.String(jobName),
	})
	if err != nil {
		return "", fmt.Errorf("starting Glue job run: %w", err)
	}
	return aws.ToString(out.JobRunId), nil
}

// deleteGlueJob deletes the Glue job.
func deleteGlueJob(ctx context.Context, glueClient *glue.Client, jobName string) error {
	_, err := glueClient.DeleteJob(ctx, &glue.DeleteJobInput{
		JobName: aws.String(jobName),
	})
	if err != nil {
		return fmt.Errorf("deleting Glue job: %w", err)
	}
	return nil
}

// deleteScriptBucket deletes the script object and bucket.
func deleteScriptBucket(ctx context.Context, s3Client *s3.Client, bucket, key string) error {
	_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	_, err := s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("deleting script bucket: %w", err)
	}
	return nil
}
