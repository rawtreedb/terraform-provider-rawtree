package s3_ingestion

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

//go:embed scripts/lambda_handler.py
var lambdaHandlerScript []byte

// buildLambdaZip creates an in-memory zip archive containing the Lambda handler.
func buildLambdaZip() ([]byte, error) {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f, err := w.Create("lambda_handler.py")
	if err != nil {
		return nil, fmt.Errorf("creating zip entry: %w", err)
	}

	_, err = f.Write(lambdaHandlerScript)
	if err != nil {
		return nil, fmt.Errorf("writing zip entry: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing zip: %w", err)
	}

	return buf.Bytes(), nil
}

// createLambdaFunction creates the Lambda function for ongoing S3 ingestion.
func createLambdaFunction(ctx context.Context, client *lambda.Client, functionName, roleARN string, envVars map[string]string) (string, error) {
	zipData, err := buildLambdaZip()
	if err != nil {
		return "", err
	}

	out, err := client.CreateFunction(ctx, &lambda.CreateFunctionInput{
		FunctionName: aws.String(functionName),
		Role:         aws.String(roleARN),
		Runtime:      lambdatypes.RuntimePython312,
		Handler:      aws.String("lambda_handler.handler"),
		Code: &lambdatypes.FunctionCode{
			ZipFile: zipData,
		},
		Environment: &lambdatypes.Environment{
			Variables: envVars,
		},
		Timeout:    aws.Int32(300), // 5 minutes.
		MemorySize: aws.Int32(256),
		Tags: map[string]string{
			"managed-by": "terraform-provider-rawtree",
		},
		Description: aws.String("Rawtree S3 ingestion - processes new objects via presigned URLs"),
	})
	if err != nil {
		return "", fmt.Errorf("creating Lambda function: %w", err)
	}

	return aws.ToString(out.FunctionArn), nil
}

// updateLambdaEnvVars updates the Lambda function's environment variables.
func updateLambdaEnvVars(ctx context.Context, client *lambda.Client, functionName string, envVars map[string]string) error {
	_, err := client.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
		FunctionName: aws.String(functionName),
		Environment: &lambdatypes.Environment{
			Variables: envVars,
		},
	})
	if err != nil {
		return fmt.Errorf("updating Lambda environment: %w", err)
	}
	return nil
}

// deleteLambdaFunction deletes the Lambda function.
func deleteLambdaFunction(ctx context.Context, client *lambda.Client, functionName string) error {
	_, err := client.DeleteFunction(ctx, &lambda.DeleteFunctionInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		return fmt.Errorf("deleting Lambda function: %w", err)
	}
	return nil
}

// addLambdaPermission allows EventBridge to invoke the Lambda function.
func addLambdaPermission(ctx context.Context, client *lambda.Client, functionName, ruleARN string) error {
	_, err := client.AddPermission(ctx, &lambda.AddPermissionInput{
		FunctionName: aws.String(functionName),
		StatementId:  aws.String("rawtree-eventbridge-invoke"),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String("events.amazonaws.com"),
		SourceArn:    aws.String(ruleARN),
	})
	if err != nil {
		return fmt.Errorf("adding Lambda permission for EventBridge: %w", err)
	}
	return nil
}
