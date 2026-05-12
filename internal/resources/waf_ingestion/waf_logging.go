package waf_ingestion

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func putLoggingConfiguration(ctx context.Context, client *wafv2.Client, webACLARN, firehoseARN string) error {
	_, err := client.PutLoggingConfiguration(ctx, &wafv2.PutLoggingConfigurationInput{
		LoggingConfiguration: &wafv2types.LoggingConfiguration{
			ResourceArn:           aws.String(webACLARN),
			LogDestinationConfigs: []string{firehoseARN},
		},
	})
	if err != nil {
		return fmt.Errorf("putting WAF logging configuration for %s: %w", webACLARN, err)
	}
	return nil
}

func deleteLoggingConfiguration(ctx context.Context, client *wafv2.Client, webACLARN string) error {
	_, err := client.DeleteLoggingConfiguration(ctx, &wafv2.DeleteLoggingConfigurationInput{
		ResourceArn: aws.String(webACLARN),
	})
	if err != nil {
		return fmt.Errorf("deleting WAF logging configuration for %s: %w", webACLARN, err)
	}
	return nil
}
