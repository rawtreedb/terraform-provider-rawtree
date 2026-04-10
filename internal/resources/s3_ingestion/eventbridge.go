package s3_ingestion

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// eventPattern represents an EventBridge event pattern for S3 object creation.
type eventPattern struct {
	Source     []string               `json:"source"`
	DetailType []string               `json:"detail-type"`
	Detail     map[string]interface{} `json:"detail"`
}

// enableS3EventBridge enables EventBridge notifications on the S3 bucket.
func enableS3EventBridge(ctx context.Context, s3Client *s3.Client, bucket string) error {
	_, err := s3Client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket: aws.String(bucket),
		NotificationConfiguration: &s3types.NotificationConfiguration{
			EventBridgeConfiguration: &s3types.EventBridgeConfiguration{},
		},
	})
	if err != nil {
		return fmt.Errorf("enabling EventBridge on bucket %s: %w", bucket, err)
	}
	return nil
}

// createEventBridgeRule creates a rule matching S3 ObjectCreated events for the specified bucket/prefix.
func createEventBridgeRule(ctx context.Context, ebClient *eventbridge.Client, ruleName, bucket, prefix string) (string, error) {
	detail := map[string]interface{}{
		"bucket": map[string]interface{}{
			"name": []string{bucket},
		},
	}

	// Add prefix filter if specified.
	if prefix != "" {
		detail["object"] = map[string]interface{}{
			"key": []map[string]interface{}{
				{"prefix": prefix},
			},
		}
	}

	pattern := eventPattern{
		Source:     []string{"aws.s3"},
		DetailType: []string{"Object Created"},
		Detail:     detail,
	}

	patternJSON, err := json.Marshal(pattern)
	if err != nil {
		return "", fmt.Errorf("marshaling event pattern: %w", err)
	}

	out, err := ebClient.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         aws.String(ruleName),
		EventPattern: aws.String(string(patternJSON)),
		State:        ebtypes.RuleStateEnabled,
		Description:  aws.String("Rawtree S3 ingestion - triggers on new object creation"),
		Tags: []ebtypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating EventBridge rule: %w", err)
	}

	return aws.ToString(out.RuleArn), nil
}

// addEventBridgeTarget adds a Lambda function as target for the EventBridge rule.
func addEventBridgeTarget(ctx context.Context, ebClient *eventbridge.Client, ruleName, targetID, lambdaARN string) error {
	_, err := ebClient.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule: aws.String(ruleName),
		Targets: []ebtypes.Target{
			{
				Id:  aws.String(targetID),
				Arn: aws.String(lambdaARN),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("adding EventBridge target: %w", err)
	}
	return nil
}

// deleteEventBridgeRule removes the target and deletes the rule.
func deleteEventBridgeRule(ctx context.Context, ebClient *eventbridge.Client, ruleName, targetID string) error {
	// Remove targets first.
	if targetID != "" {
		_, _ = ebClient.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
			Rule: aws.String(ruleName),
			Ids:  []string{targetID},
		})
	}

	_, err := ebClient.DeleteRule(ctx, &eventbridge.DeleteRuleInput{
		Name: aws.String(ruleName),
	})
	if err != nil {
		return fmt.Errorf("deleting EventBridge rule: %w", err)
	}
	return nil
}
