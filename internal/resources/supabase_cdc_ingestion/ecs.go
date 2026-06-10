package supabase_cdc_ingestion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func createCluster(ctx context.Context, client *ecs.Client, name string) (string, error) {
	out, err := client.CreateCluster(ctx, &ecs.CreateClusterInput{
		ClusterName: aws.String(name),
		Tags: []ecstypes.Tag{
			{Key: aws.String(util.ManagedByTagKey), Value: aws.String(util.ManagedByTagValue)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating ECS cluster %s: %w", name, err)
	}
	return aws.ToString(out.Cluster.ClusterArn), nil
}

func registerTaskDefinition(ctx context.Context, client *ecs.Client, cfg resolvedConfig, names ecsNames, secretARNs secretARNs, executionRoleARN string, command []string) (string, error) {
	out, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(names.TaskDefinitionFamily),
		Cpu:                     int64String(cfg.CPU),
		Memory:                  int64String(cfg.Memory),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		ExecutionRoleArn:        aws.String(executionRoleARN),
		RuntimePlatform: &ecstypes.RuntimePlatform{
			OperatingSystemFamily: ecstypes.OSFamilyLinux,
			CpuArchitecture:       ecstypes.CPUArchitectureX8664,
		},
		ContainerDefinitions: []ecstypes.ContainerDefinition{
			buildContainerDefinition(cfg, names, secretARNs, command),
		},
		Tags: []ecstypes.Tag{
			{Key: aws.String(util.ManagedByTagKey), Value: aws.String(util.ManagedByTagValue)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("registering ECS task definition %s: %w", names.TaskDefinitionFamily, err)
	}
	return aws.ToString(out.TaskDefinition.TaskDefinitionArn), nil
}

func createService(ctx context.Context, client *ecs.Client, cfg resolvedConfig, names ecsNames, clusterARN, taskDefinitionARN string) (string, error) {
	minHealthy := int32(0)
	maxPercent := int32(100)
	desiredCount := int32(1)

	out, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:              aws.String(clusterARN),
		ServiceName:          aws.String(names.ServiceName),
		TaskDefinition:       aws.String(taskDefinitionARN),
		DesiredCount:         aws.Int32(desiredCount),
		LaunchType:           ecstypes.LaunchTypeFargate,
		NetworkConfiguration: buildNetworkConfiguration(cfg),
		DeploymentConfiguration: &ecstypes.DeploymentConfiguration{
			MinimumHealthyPercent: aws.Int32(minHealthy),
			MaximumPercent:        aws.Int32(maxPercent),
		},
		EnableECSManagedTags: true,
		PropagateTags:        ecstypes.PropagateTagsService,
		Tags: []ecstypes.Tag{
			{Key: aws.String(util.ManagedByTagKey), Value: aws.String(util.ManagedByTagValue)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating ECS service %s: %w", names.ServiceName, err)
	}
	return aws.ToString(out.Service.ServiceArn), nil
}

func updateService(ctx context.Context, client *ecs.Client, cfg resolvedConfig, clusterARN, serviceName, taskDefinitionARN string) error {
	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:              aws.String(clusterARN),
		Service:              aws.String(serviceName),
		TaskDefinition:       aws.String(taskDefinitionARN),
		DesiredCount:         aws.Int32(1),
		ForceNewDeployment:   true,
		NetworkConfiguration: buildNetworkConfiguration(cfg),
	})
	if err != nil {
		return fmt.Errorf("updating ECS service %s: %w", serviceName, err)
	}
	return nil
}

func runInitializationTask(ctx context.Context, client *ecs.Client, cfg resolvedConfig, clusterARN, taskDefinitionARN string) error {
	out, err := client.RunTask(ctx, &ecs.RunTaskInput{
		Cluster:              aws.String(clusterARN),
		TaskDefinition:       aws.String(taskDefinitionARN),
		LaunchType:           ecstypes.LaunchTypeFargate,
		NetworkConfiguration: buildNetworkConfiguration(cfg),
		Count:                aws.Int32(1),
	})
	if err != nil {
		return fmt.Errorf("starting initialization ECS task: %w", err)
	}
	if len(out.Failures) > 0 {
		return fmt.Errorf("starting initialization ECS task failed: %s", aws.ToString(out.Failures[0].Reason))
	}
	if len(out.Tasks) == 0 {
		return errors.New("starting initialization ECS task returned no tasks")
	}

	taskARN := aws.ToString(out.Tasks[0].TaskArn)
	return waitForTaskSuccess(ctx, client, clusterARN, taskARN, 10*time.Minute)
}

func waitForTaskSuccess(ctx context.Context, client *ecs.Client, clusterARN, taskARN string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: aws.String(clusterARN),
			Tasks:   []string{taskARN},
		})
		if err != nil {
			return fmt.Errorf("describing initialization ECS task: %w", err)
		}
		if len(out.Tasks) == 0 {
			return errors.New("initialization ECS task disappeared")
		}
		task := out.Tasks[0]
		if task.LastStatus != nil && aws.ToString(task.LastStatus) == "STOPPED" {
			if task.StopCode == ecstypes.TaskStopCodeEssentialContainerExited {
				for _, container := range task.Containers {
					if container.ExitCode != nil && aws.ToInt32(container.ExitCode) != 0 {
						return fmt.Errorf("initialization ECS task exited with code %d: %s", aws.ToInt32(container.ExitCode), aws.ToString(task.StoppedReason))
					}
				}
				return nil
			}
			return fmt.Errorf("initialization ECS task stopped: %s", aws.ToString(task.StoppedReason))
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("initialization ECS task did not finish within %s", timeout)
}

func serviceExists(ctx context.Context, client *ecs.Client, clusterARN, serviceName string) (bool, error) {
	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterARN),
		Services: []string{serviceName},
	})
	if err != nil {
		return false, fmt.Errorf("describing ECS service %s: %w", serviceName, err)
	}
	if len(out.Services) == 0 {
		return false, nil
	}
	return out.Services[0].Status != nil && aws.ToString(out.Services[0].Status) != "INACTIVE", nil
}

func deleteService(ctx context.Context, client *ecs.Client, clusterARN, serviceName string) error {
	if clusterARN == "" || serviceName == "" {
		return nil
	}
	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String(clusterARN),
		Service:      aws.String(serviceName),
		DesiredCount: aws.Int32(0),
	})
	if err != nil {
		var notFound *ecstypes.ServiceNotFoundException
		if !errors.As(err, &notFound) {
			return fmt.Errorf("scaling down ECS service %s: %w", serviceName, err)
		}
		return nil
	}
	_, err = client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Cluster: aws.String(clusterARN),
		Service: aws.String(serviceName),
		Force:   aws.Bool(true),
	})
	if err != nil {
		var notFound *ecstypes.ServiceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("deleting ECS service %s: %w", serviceName, err)
	}
	return waitForServiceDeleted(ctx, client, clusterARN, serviceName, 10*time.Minute)
}

func waitForServiceDeleted(ctx context.Context, client *ecs.Client, clusterARN, serviceName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  aws.String(clusterARN),
			Services: []string{serviceName},
		})
		if err != nil {
			var notFound *ecstypes.ServiceNotFoundException
			if errors.As(err, &notFound) {
				return nil
			}
			return fmt.Errorf("describing ECS service %s deletion status: %w", serviceName, err)
		}
		if len(out.Services) == 0 {
			return nil
		}
		if out.Services[0].Status != nil && aws.ToString(out.Services[0].Status) == "INACTIVE" {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("ecs service %s was not deleted within %s", serviceName, timeout)
}

func deregisterTaskDefinition(ctx context.Context, client *ecs.Client, taskDefinitionARN string) error {
	if taskDefinitionARN == "" {
		return nil
	}
	_, err := client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinitionARN),
	})
	if err != nil {
		return fmt.Errorf("deregistering ECS task definition %s: %w", taskDefinitionARN, err)
	}
	return nil
}

func deleteCluster(ctx context.Context, client *ecs.Client, clusterARN string) error {
	if clusterARN == "" {
		return nil
	}
	_, err := client.DeleteCluster(ctx, &ecs.DeleteClusterInput{
		Cluster: aws.String(clusterARN),
	})
	if err != nil {
		var notFound *ecstypes.ClusterNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("deleting ECS cluster %s: %w", clusterARN, err)
	}
	return nil
}
