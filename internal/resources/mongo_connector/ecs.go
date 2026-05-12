package mongo_connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const containerName = "rawtree-mongo-connector"
const imageRepo = "ghcr.io/rawtree/rawtree-mongo-connector"

type ecsConfig struct {
	ClusterName  string
	ServiceName  string
	TaskFamily   string
	ImageTag     string
	LogGroupName string
	Region       string

	ExecutionRoleARN string
	TaskRoleARN      string

	MongoSecretARN   string
	RawtreeSecretARN string

	MongoDatabase  string
	Collections    string
	TablePrefix    string
	FullDocument   string
	SnapshotEnable bool
	BatchMaxRows   int32
	FlushInterval  string

	RawtreeEndpoint string
	RawtreeOrg      string
	RawtreeProject  string
}

func createCluster(ctx context.Context, client *ecs.Client, name string) (string, error) {
	out, err := client.CreateCluster(ctx, &ecs.CreateClusterInput{
		ClusterName: &name,
		Tags: []ecstypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create ECS cluster: %w", err)
	}
	return *out.Cluster.ClusterArn, nil
}

func registerTaskDefinition(ctx context.Context, client *ecs.Client, cfg ecsConfig) (string, error) {
	snapshotEnabled := "true"
	if !cfg.SnapshotEnable {
		snapshotEnabled = "false"
	}

	env := []ecstypes.KeyValuePair{
		{Name: aws.String("RAWTREE_MONGO_MONGODB_DATABASE"), Value: aws.String(cfg.MongoDatabase)},
		{Name: aws.String("RAWTREE_MONGO_RAWTREE_ENDPOINT"), Value: aws.String(cfg.RawtreeEndpoint)},
		{Name: aws.String("RAWTREE_MONGO_RAWTREE_ORGANIZATION"), Value: aws.String(cfg.RawtreeOrg)},
		{Name: aws.String("RAWTREE_MONGO_RAWTREE_PROJECT"), Value: aws.String(cfg.RawtreeProject)},
		{Name: aws.String("RAWTREE_MONGO_RAWTREE_TABLE_PREFIX"), Value: aws.String(cfg.TablePrefix)},
	}

	if cfg.Collections != "" {
		env = append(env, ecstypes.KeyValuePair{
			Name: aws.String("RAWTREE_MONGO_COLLECTIONS"), Value: aws.String(cfg.Collections),
		})
	}

	secrets := []ecstypes.Secret{
		{Name: aws.String("RAWTREE_MONGO_MONGODB_URI"), ValueFrom: aws.String(cfg.MongoSecretARN + ":mongo_uri::")},
		{Name: aws.String("RAWTREE_MONGO_RAWTREE_API_KEY"), ValueFrom: aws.String(cfg.RawtreeSecretARN + ":rawtree_api_key::")},
	}

	image := fmt.Sprintf("%s:%s", imageRepo, cfg.ImageTag)

	containerDef := ecstypes.ContainerDefinition{
		Name:      aws.String(containerName),
		Image:     aws.String(image),
		Essential: aws.Bool(true),
		Command: []string{
			"--config", "/dev/null",
		},
		Environment: env,
		Secrets:     secrets,
		LogConfiguration: &ecstypes.LogConfiguration{
			LogDriver: ecstypes.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":         cfg.LogGroupName,
				"awslogs-region":        cfg.Region,
				"awslogs-stream-prefix": "connector",
			},
		},
		PortMappings: []ecstypes.PortMapping{
			{ContainerPort: aws.Int32(9090), Protocol: ecstypes.TransportProtocolTcp},
		},
	}

	_ = snapshotEnabled
	_ = cfg.FullDocument
	_ = cfg.BatchMaxRows
	_ = cfg.FlushInterval

	out, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  &cfg.TaskFamily,
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		Cpu:                     aws.String("256"),
		Memory:                  aws.String("512"),
		ExecutionRoleArn:        &cfg.ExecutionRoleARN,
		TaskRoleArn:             &cfg.TaskRoleARN,
		ContainerDefinitions:    []ecstypes.ContainerDefinition{containerDef},
		Tags: []ecstypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("register task definition: %w", err)
	}
	return *out.TaskDefinition.TaskDefinitionArn, nil
}

func createService(ctx context.Context, client *ecs.Client, clusterARN, serviceName, taskDefARN string) (string, error) {
	out, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        &clusterARN,
		ServiceName:    &serviceName,
		TaskDefinition: &taskDefARN,
		DesiredCount:   aws.Int32(1),
		LaunchType:     ecstypes.LaunchTypeFargate,
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				AssignPublicIp: ecstypes.AssignPublicIpEnabled,
				Subnets:        []string{},
			},
		},
		Tags: []ecstypes.Tag{
			{Key: aws.String("managed-by"), Value: aws.String("terraform-provider-rawtree")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create ECS service: %w", err)
	}
	return *out.Service.ServiceArn, nil
}

func updateService(ctx context.Context, client *ecs.Client, clusterARN, serviceName, taskDefARN string) error {
	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:            &clusterARN,
		Service:            &serviceName,
		TaskDefinition:     &taskDefARN,
		ForceNewDeployment: true,
	})
	if err != nil {
		return fmt.Errorf("update ECS service: %w", err)
	}
	return nil
}

func deleteService(ctx context.Context, client *ecs.Client, clusterARN, serviceName string) error {
	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      &clusterARN,
		Service:      &serviceName,
		DesiredCount: aws.Int32(0),
	})
	if err != nil {
		var notFound *ecstypes.ServiceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("scale down service: %w", err)
	}

	time.Sleep(5 * time.Second)

	_, err = client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Cluster: &clusterARN,
		Service: &serviceName,
		Force:   aws.Bool(true),
	})
	if err != nil {
		var notFound *ecstypes.ServiceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("delete ECS service: %w", err)
	}
	return nil
}

func deleteCluster(ctx context.Context, client *ecs.Client, clusterName string) error {
	_, err := client.DeleteCluster(ctx, &ecs.DeleteClusterInput{
		Cluster: &clusterName,
	})
	if err != nil {
		if strings.Contains(err.Error(), "ClusterNotFoundException") {
			return nil
		}
		return fmt.Errorf("delete ECS cluster: %w", err)
	}
	return nil
}

func deregisterTaskDefinitions(ctx context.Context, client *ecs.Client, family string) {
	out, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: &family,
	})
	if err != nil {
		return
	}
	for _, arn := range out.TaskDefinitionArns {
		client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
			TaskDefinition: &arn,
		})
	}
}
