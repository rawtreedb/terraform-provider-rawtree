package supabase_cdc_ingestion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func createCluster(ctx context.Context, client *ecs.Client, name string) (string, error) {
	desc, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{name},
	})
	if err != nil {
		return "", fmt.Errorf("describing ECS cluster %s: %w", name, err)
	}
	for _, c := range desc.Clusters {
		if c.Status != nil && aws.ToString(c.Status) == "ACTIVE" {
			return aws.ToString(c.ClusterArn), nil
		}
	}

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

func registerTaskDefinition(ctx context.Context, client *ecs.Client, cfg resolvedConfig, names ecsNames, refs ecsSecretRefs, executionRoleARN string, command []string) (string, error) {
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
			buildContainerDefinition(cfg, names, refs, command),
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
		if !isServiceAlreadyExists(err) {
			return "", fmt.Errorf("creating ECS service %s: %w", names.ServiceName, err)
		}
		if err := updateService(ctx, client, cfg, clusterARN, names.ServiceName, taskDefinitionARN); err != nil {
			return "", fmt.Errorf("adopting existing ECS service %s: %w", names.ServiceName, err)
		}
		desc, descErr := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  aws.String(clusterARN),
			Services: []string{names.ServiceName},
		})
		if descErr != nil || len(desc.Services) == 0 {
			return "", fmt.Errorf("ECS service %s exists but could not be described: %w", names.ServiceName, descErr)
		}
		return aws.ToString(desc.Services[0].ServiceArn), nil
	}
	return aws.ToString(out.Service.ServiceArn), nil
}

func isServiceAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ServiceAlreadyExists" {
		return true
	}
	return strings.Contains(err.Error(), "Creation of service was not idempotent")
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
	var lastTask ecstypes.Task
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
		lastTask = task
		if task.LastStatus != nil && aws.ToString(task.LastStatus) == "STOPPED" {
			if task.StopCode == ecstypes.TaskStopCodeEssentialContainerExited {
				allExitedZero := len(task.Containers) > 0
				exitCodesAvailable := true
				for _, container := range task.Containers {
					if container.ExitCode == nil {
						allExitedZero = false
						exitCodesAvailable = false
					} else if aws.ToInt32(container.ExitCode) != 0 {
						return fmt.Errorf("initialization ECS task exited with code %d: %s",
							aws.ToInt32(container.ExitCode), aws.ToString(task.StoppedReason))
					}
				}
				if allExitedZero {
					return nil
				}
				if !exitCodesAvailable {
					// Exit codes not yet populated; poll again.
					time.Sleep(10 * time.Second)
					continue
				}
			}
			return fmt.Errorf("initialization ECS task stopped: %s", aws.ToString(task.StoppedReason))
		}
		time.Sleep(10 * time.Second)
	}

	// Timed out. Best-effort: stop the task so it doesn't keep blocking the
	// cluster delete on cleanup, then report the most recent task state so the
	// caller has something to act on (PROVISIONING vs PENDING vs RUNNING means
	// very different root causes).
	_, _ = client.StopTask(ctx, &ecs.StopTaskInput{
		Cluster: aws.String(clusterARN),
		Task:    aws.String(taskARN),
		Reason:  aws.String("terraform-provider-rawtree: initialization task exceeded timeout"),
	})
	return fmt.Errorf("initialization ECS task did not finish within %s: %s",
		timeout, formatTaskState(lastTask))
}

// formatTaskState renders the task's most recent observable state for an
// error message: lastStatus, desiredStatus, stop code/reason, and each
// container's lastStatus + reason. Helps distinguish "stuck in PROVISIONING"
// (ENI / Fargate capacity issue) from "RUNNING but slow" (container is
// actually executing and just hasn't finished) from "STOPPED with
// CannotPullContainerError" (image-pull failure).
func formatTaskState(task ecstypes.Task) string {
	if task.TaskArn == nil {
		return "no task state observed"
	}
	parts := []string{
		fmt.Sprintf("lastStatus=%s", aws.ToString(task.LastStatus)),
		fmt.Sprintf("desiredStatus=%s", aws.ToString(task.DesiredStatus)),
	}
	if task.StopCode != "" {
		parts = append(parts, fmt.Sprintf("stopCode=%s", task.StopCode))
	}
	if r := aws.ToString(task.StoppedReason); r != "" {
		parts = append(parts, fmt.Sprintf("stoppedReason=%q", r))
	}
	for _, c := range task.Containers {
		cparts := []string{fmt.Sprintf("name=%s", aws.ToString(c.Name)),
			fmt.Sprintf("lastStatus=%s", aws.ToString(c.LastStatus))}
		if c.ExitCode != nil {
			cparts = append(cparts, fmt.Sprintf("exitCode=%d", aws.ToInt32(c.ExitCode)))
		}
		if r := aws.ToString(c.Reason); r != "" {
			cparts = append(cparts, fmt.Sprintf("reason=%q", r))
		}
		parts = append(parts, fmt.Sprintf("container{%s}", strings.Join(cparts, " ")))
	}
	return strings.Join(parts, " ")
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
