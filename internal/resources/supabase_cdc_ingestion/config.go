package supabase_cdc_ingestion

import (
	"context"
	"fmt"
	"strconv"

	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var fargateMemoryByCPU = map[int64][]int64{
	256:   {512, 1024, 2048},
	512:   {1024, 2048, 3072, 4096},
	1024:  {2048, 3072, 4096, 5120, 6144, 7168, 8192},
	2048:  sequence(4096, 16384, 1024),
	4096:  sequence(8192, 30720, 1024),
	8192:  sequence(16384, 61440, 4096),
	16384: sequence(32768, 122880, 8192),
}

type resolvedConfig struct {
	ResourceName          string
	Name                  string
	Region                string
	Publication           string
	PipelineID            string
	Image                 string
	CPU                   int64
	Memory                int64
	Subnets               []string
	SecurityGroups        []string
	AssignPublicIP        bool
	DatabaseURL           string
	DatabaseURLSecretARN  string
	TLSRootCertPEM        string
	TLSRootCertSecretARN  string
	LogRetentionDays      int64
	RunInitializationTask bool
	InitCommand           []string
	WorkerCommand         []string
	Environment           map[string]string
	Organization          string
	Project               string
	APIURL                string
	APIKey                string
	APIKeyHash            string
}

type ecsNames struct {
	ClusterName           string
	ServiceName           string
	TaskDefinitionFamily  string
	ContainerName         string
	LogGroupName          string
	ExecutionRoleName     string
	RawtreeSecretName     string
	DatabaseURLSecretName string
	TLSRootCertSecretName string
}

func resolveConfig(ctx context.Context, plan *SupabaseCDCIngestionModel, c *client.RawtreeClient) (resolvedConfig, diag.Diagnostics) {
	var diags diag.Diagnostics

	subnets, subnetDiags := listToStrings(ctx, plan.SubnetIDs)
	diags.Append(subnetDiags...)
	securityGroups, sgDiags := listToStrings(ctx, plan.SecurityGroupIDs)
	diags.Append(sgDiags...)
	initCommand, initDiags := listToStrings(ctx, plan.InitializationCommand)
	diags.Append(initDiags...)
	workerCommand, workerDiags := listToStrings(ctx, plan.WorkerCommand)
	diags.Append(workerDiags...)
	env, envDiags := mapToStrings(ctx, plan.Environment)
	diags.Append(envDiags...)
	if diags.HasError() {
		return resolvedConfig{}, diags
	}

	if len(initCommand) == 0 {
		initCommand = []string{"init"}
	}
	if len(workerCommand) == 0 {
		workerCommand = []string{"run"}
	}

	org := c.Organization
	if stringSet(plan.Organization) {
		org = plan.Organization.ValueString()
	}
	project := c.Project
	if stringSet(plan.Project) {
		project = plan.Project.ValueString()
	}

	cfg := resolvedConfig{
		Name:                  plan.Name.ValueString(),
		Region:                plan.Region.ValueString(),
		Publication:           plan.Publication.ValueString(),
		PipelineID:            plan.PipelineID.ValueString(),
		Image:                 plan.Image.ValueString(),
		CPU:                   plan.CPU.ValueInt64(),
		Memory:                plan.Memory.ValueInt64(),
		Subnets:               subnets,
		SecurityGroups:        securityGroups,
		AssignPublicIP:        boolValue(plan.AssignPublicIP),
		DatabaseURL:           stringValue(plan.DatabaseURL),
		DatabaseURLSecretARN:  stringValue(plan.DatabaseURLSecretARN),
		TLSRootCertPEM:        stringValue(plan.TLSRootCertPEM),
		TLSRootCertSecretARN:  stringValue(plan.TLSRootCertSecretARN),
		LogRetentionDays:      plan.LogRetentionDays.ValueInt64(),
		RunInitializationTask: boolValue(plan.RunInitializationTask),
		InitCommand:           initCommand,
		WorkerCommand:         workerCommand,
		Environment:           env,
		Organization:          org,
		Project:               project,
		APIURL:                c.APIURL,
		APIKey:                c.APIKey,
		APIKeyHash:            util.HashString(c.APIKey),
	}
	cfg.ResourceName = util.SanitizeResourceName(fmt.Sprintf("%s-%s-%s", org, project, cfg.Name))

	validateResolvedConfig(cfg, &diags)
	return cfg, diags
}

func validateResolvedConfig(cfg resolvedConfig, diags *diag.Diagnostics) {
	if cfg.DatabaseURL == "" && cfg.DatabaseURLSecretARN == "" {
		diags.AddError(
			"Missing Supabase Database URL",
			"Set either database_url or database_url_secret_arn. database_url_secret_arn is recommended for production.",
		)
	}
	if cfg.DatabaseURL != "" && cfg.DatabaseURLSecretARN != "" {
		diags.AddError(
			"Conflicting Supabase Database URL Inputs",
			"Set only one of database_url or database_url_secret_arn.",
		)
	}
	if cfg.TLSRootCertPEM != "" && cfg.TLSRootCertSecretARN != "" {
		diags.AddError(
			"Conflicting TLS Certificate Inputs",
			"Set only one of tls_root_cert_pem or tls_root_cert_secret_arn.",
		)
	}
	if len(cfg.Subnets) == 0 {
		diags.AddError("Missing Subnets", "At least one subnet_id is required.")
	}
	if !validFargateSize(cfg.CPU, cfg.Memory) {
		diags.AddError(
			"Invalid Fargate CPU/Memory Combination",
			fmt.Sprintf("CPU %d does not support memory %d MiB.", cfg.CPU, cfg.Memory),
		)
	}
}

func namesFor(resourceName string) ecsNames {
	return ecsNames{
		ClusterName:           fmt.Sprintf("rawtree-supabase-cdc-%s", resourceName),
		ServiceName:           fmt.Sprintf("rawtree-supabase-cdc-%s", resourceName),
		TaskDefinitionFamily:  fmt.Sprintf("rawtree-supabase-cdc-%s", resourceName),
		ContainerName:         "rawtree-supabase-cdc",
		LogGroupName:          fmt.Sprintf("/aws/ecs/rawtree/supabase-cdc/%s", resourceName),
		ExecutionRoleName:     fmt.Sprintf("rawtree-ecs-%s", resourceName),
		RawtreeSecretName:     fmt.Sprintf("rawtree/supabase-cdc/%s/rawtree-api-key", resourceName),
		DatabaseURLSecretName: fmt.Sprintf("rawtree/supabase-cdc/%s/database-url", resourceName),
		TLSRootCertSecretName: fmt.Sprintf("rawtree/supabase-cdc/%s/tls-root-cert", resourceName),
	}
}

func buildContainerDefinition(cfg resolvedConfig, names ecsNames, secretARNs secretARNs, command []string) ecstypes.ContainerDefinition {
	env := []ecstypes.KeyValuePair{
		{Name: strptr("RAWTREE_API_URL"), Value: strptr(cfg.APIURL)},
		{Name: strptr("RAWTREE_ORG"), Value: strptr(cfg.Organization)},
		{Name: strptr("RAWTREE_PROJECT"), Value: strptr(cfg.Project)},
		{Name: strptr("POSTGRES_PUBLICATION"), Value: strptr(cfg.Publication)},
		{Name: strptr("PIPELINE_ID"), Value: strptr(cfg.PipelineID)},
	}
	if secretARNs.TLSRootCertARN != "" {
		env = append(env, ecstypes.KeyValuePair{
			Name:  strptr("POSTGRES_TLS_ROOT_CERT_PATH"),
			Value: strptr("/tmp/supabase-ca.pem"),
		})
	}
	for k, v := range cfg.Environment {
		env = append(env, ecstypes.KeyValuePair{Name: strptr(k), Value: strptr(v)})
	}

	secrets := []ecstypes.Secret{
		{Name: strptr("RAWTREE_API_KEY"), ValueFrom: strptr(secretARNs.RawtreeAPIKeyARN)},
		{Name: strptr("DATABASE_URL"), ValueFrom: strptr(secretARNs.DatabaseURLARN)},
	}
	if secretARNs.TLSRootCertARN != "" {
		secrets = append(secrets, ecstypes.Secret{
			Name:      strptr("POSTGRES_TLS_ROOT_CERT_PEM"),
			ValueFrom: strptr(secretARNs.TLSRootCertARN),
		})
	}

	return ecstypes.ContainerDefinition{
		Name:        strptr(names.ContainerName),
		Image:       strptr(cfg.Image),
		Essential:   boolptr(true),
		Command:     command,
		Environment: env,
		Secrets:     secrets,
		LogConfiguration: &ecstypes.LogConfiguration{
			LogDriver: ecstypes.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":         names.LogGroupName,
				"awslogs-region":        cfg.Region,
				"awslogs-stream-prefix": "worker",
			},
		},
	}
}

func buildNetworkConfiguration(cfg resolvedConfig) *ecstypes.NetworkConfiguration {
	assignPublicIP := ecstypes.AssignPublicIpDisabled
	if cfg.AssignPublicIP {
		assignPublicIP = ecstypes.AssignPublicIpEnabled
	}

	return &ecstypes.NetworkConfiguration{
		AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
			Subnets:        cfg.Subnets,
			SecurityGroups: cfg.SecurityGroups,
			AssignPublicIp: assignPublicIP,
		},
	}
}

func validFargateSize(cpu, memory int64) bool {
	for _, allowed := range fargateMemoryByCPU[cpu] {
		if allowed == memory {
			return true
		}
	}
	return false
}

func sequence(start, end, step int64) []int64 {
	var out []int64
	for v := start; v <= end; v += step {
		out = append(out, v)
	}
	return out
}

func listToStrings(ctx context.Context, value types.List) ([]string, diag.Diagnostics) {
	var out []string
	if value.IsNull() || value.IsUnknown() {
		return out, nil
	}
	diags := value.ElementsAs(ctx, &out, false)
	return out, diags
}

func mapToStrings(ctx context.Context, value types.Map) (map[string]string, diag.Diagnostics) {
	out := map[string]string{}
	if value.IsNull() || value.IsUnknown() {
		return out, nil
	}
	diags := value.ElementsAs(ctx, &out, false)
	return out, diags
}

func stringSet(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueString() != ""
}

func stringValue(value types.String) string {
	if stringSet(value) {
		return value.ValueString()
	}
	return ""
}

func boolValue(value types.Bool) bool {
	if value.IsNull() || value.IsUnknown() {
		return false
	}
	return value.ValueBool()
}

func int64String(v int64) *string {
	return strptr(strconv.FormatInt(v, 10))
}

func strptr(v string) *string {
	return &v
}

func boolptr(v bool) *bool {
	return &v
}
