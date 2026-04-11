package s3_ingestion

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

//go:embed scripts/glue_job.py
var glueJobScript []byte

// uploadGlueScript uploads the embedded Glue job script to S3 and verifies it exists.
func uploadGlueScript(ctx context.Context, s3Client *s3.Client, bucket, key string) error {
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(glueJobScript),
	})
	if err != nil {
		return fmt.Errorf("uploading Glue script: %w", err)
	}

	// Verify the object is readable before Glue tries to download it.
	for i := 0; i < 5; i++ {
		_, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("script uploaded but HeadObject verification failed after 5 attempts for s3://%s/%s", bucket, key)
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

// stopRunningGlueJobRuns stops any active runs before deleting the job.
// In tests, destroy runs seconds after create so jobs are always still running.
// In production, jobs complete long before destroy and this is a no-op.
func stopRunningGlueJobRuns(ctx context.Context, glueClient *glue.Client, jobName string) {
	runs, err := glueClient.GetJobRuns(ctx, &glue.GetJobRunsInput{
		JobName:    aws.String(jobName),
		MaxResults: aws.Int32(10),
	})
	if err != nil {
		return
	}

	var activeIDs []string
	for _, run := range runs.JobRuns {
		s := run.JobRunState
		if s == gluetypes.JobRunStateRunning || s == gluetypes.JobRunStateStarting || s == gluetypes.JobRunStateWaiting {
			activeIDs = append(activeIDs, aws.ToString(run.Id))
		}
	}

	if len(activeIDs) == 0 {
		return
	}

	glueClient.BatchStopJobRun(ctx, &glue.BatchStopJobRunInput{
		JobName:   aws.String(jobName),
		JobRunIds: activeIDs,
	})

	// Wait for the stop to take effect before deleting the script.
	time.Sleep(5 * time.Second)
}

// deleteGlueJob stops active runs then deletes the Glue job.
func deleteGlueJob(ctx context.Context, glueClient *glue.Client, jobName string) error {
	stopRunningGlueJobRuns(ctx, glueClient, jobName)

	_, err := glueClient.DeleteJob(ctx, &glue.DeleteJobInput{
		JobName: aws.String(jobName),
	})
	if err != nil {
		return fmt.Errorf("deleting Glue job: %w", err)
	}
	return nil
}

// deleteGlueScriptObject deletes the Glue script from the source bucket.
func deleteGlueScriptObject(ctx context.Context, s3Client *s3.Client, bucket, key string) error {
	_, err := s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("deleting Glue script object: %w", err)
	}
	return nil
}
