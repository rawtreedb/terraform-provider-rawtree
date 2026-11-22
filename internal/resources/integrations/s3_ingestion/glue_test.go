package s3_ingestion

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"testing"
)

func TestWithGlueArgsPreservesExistingJob(t *testing.T) {
	job := &gluetypes.Job{
		Role: aws.String("existing-role"), Command: &gluetypes.JobCommand{ScriptLocation: aws.String("s3://bucket/script.py")},
		DefaultArguments: map[string]string{"--INGEST_ENDPOINT": "old", "--custom": "keep"},
		MaxCapacity:      aws.Float64(0.0625), Timeout: aws.Int32(75),
	}
	update := withGlueArgs(job, map[string]string{"INGEST_ENDPOINT": "new"})
	if update.Role != job.Role || update.Command != job.Command || update.MaxCapacity != job.MaxCapacity || update.Timeout != job.Timeout || update.DefaultArguments["--custom"] != "keep" || update.DefaultArguments["--INGEST_ENDPOINT"] != "new" || job.DefaultArguments["--INGEST_ENDPOINT"] != "old" {
		t.Fatalf("unexpected Glue job update: %#v", update)
	}
}
