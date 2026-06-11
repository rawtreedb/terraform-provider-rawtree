package supabase_cdc_ingestion

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestValidFargateSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cpu    int64
		memory int64
		valid  bool
	}{
		{name: "default", cpu: 512, memory: 1024, valid: true},
		{name: "smallest", cpu: 256, memory: 512, valid: true},
		{name: "invalid memory for cpu", cpu: 256, memory: 4096, valid: false},
		{name: "large valid step", cpu: 8192, memory: 32768, valid: true},
		{name: "large invalid step", cpu: 8192, memory: 32769, valid: false},
		{name: "unknown cpu", cpu: 123, memory: 512, valid: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validFargateSize(tt.cpu, tt.memory); got != tt.valid {
				t.Fatalf("validFargateSize(%d, %d) = %v, want %v", tt.cpu, tt.memory, got, tt.valid)
			}
		})
	}
}

func TestResolveConfigDefaultsAndProviderValues(t *testing.T) {
	t.Parallel()

	model := baseModel()
	model.DatabaseURLSecretARN = types.StringValue("arn:aws:secretsmanager:us-east-1:123456789012:secret:db")

	cfg, diags := resolveConfig(context.Background(), &model, client.New("rt_test", "https://api.example.com", "org", "project"))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %s", diags.Errors())
	}

	if cfg.ResourceName != util.SanitizeResourceName("org-project-orders") {
		t.Fatalf("unexpected resource name: %s", cfg.ResourceName)
	}
	if cfg.Organization != "org" || cfg.Project != "project" {
		t.Fatalf("expected provider org/project, got %s/%s", cfg.Organization, cfg.Project)
	}
	if cfg.APIKeyHash != util.HashString("rt_test") {
		t.Fatalf("unexpected api key hash")
	}
	if len(cfg.InitCommand) != 1 || cfg.InitCommand[0] != "init" {
		t.Fatalf("unexpected init command: %#v", cfg.InitCommand)
	}
	if len(cfg.WorkerCommand) != 1 || cfg.WorkerCommand[0] != "run" {
		t.Fatalf("unexpected worker command: %#v", cfg.WorkerCommand)
	}
}

func TestResolveConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*SupabaseCDCIngestionModel)
	}{
		{
			name: "missing database url",
			mutate: func(m *SupabaseCDCIngestionModel) {
				m.DatabaseURL = types.StringNull()
				m.DatabaseURLSecretARN = types.StringNull()
			},
		},
		{
			name: "conflicting database url inputs",
			mutate: func(m *SupabaseCDCIngestionModel) {
				m.DatabaseURL = types.StringValue("postgres://example")
				m.DatabaseURLSecretARN = types.StringValue("arn:aws:secretsmanager:us-east-1:123456789012:secret:db")
			},
		},
		{
			name: "conflicting tls inputs",
			mutate: func(m *SupabaseCDCIngestionModel) {
				m.DatabaseURLSecretARN = types.StringValue("arn:aws:secretsmanager:us-east-1:123456789012:secret:db")
				m.TLSRootCertPEM = types.StringValue("pem")
				m.TLSRootCertSecretARN = types.StringValue("arn:aws:secretsmanager:us-east-1:123456789012:secret:ca")
			},
		},
		{
			name: "invalid fargate size",
			mutate: func(m *SupabaseCDCIngestionModel) {
				m.DatabaseURLSecretARN = types.StringValue("arn:aws:secretsmanager:us-east-1:123456789012:secret:db")
				m.CPU = types.Int64Value(256)
				m.Memory = types.Int64Value(4096)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := baseModel()
			tt.mutate(&model)
			_, diags := resolveConfig(context.Background(), &model, client.New("rt_test", "https://api.example.com", "org", "project"))
			if !diags.HasError() {
				t.Fatal("expected diagnostics error")
			}
		})
	}
}

func TestBuildContainerDefinition(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig{
		Region:       "us-east-1",
		Image:        "example/image:sha",
		APIURL:       "https://api.example.com",
		Organization: "org",
		Project:      "project",
		Publication:  "rawtree_publication",
		PipelineID:   "7",
		Environment:  map[string]string{"EXTRA": "value"},
	}
	names := namesFor("org-project-orders")
	refs := ecsSecretRefs{
		APIKey:      "arn:config:RAWTREE_API_KEY::",
		DatabaseURL: "arn:config:DATABASE_URL::",
		TLSRootCert: "arn:config:POSTGRES_TLS_ROOT_CERT_PEM::",
	}

	def := buildContainerDefinition(cfg, names, refs, []string{"run"})

	if def.Image == nil || *def.Image != "example/image:sha" {
		t.Fatalf("unexpected image: %v", def.Image)
	}
	if len(def.Command) != 1 || def.Command[0] != "run" {
		t.Fatalf("unexpected command: %#v", def.Command)
	}
	if len(def.Secrets) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(def.Secrets))
	}
	env := map[string]string{}
	for _, kv := range def.Environment {
		env[*kv.Name] = *kv.Value
	}
	for k, want := range map[string]string{
		"RAWTREE_API_URL":      "https://api.example.com",
		"RAWTREE_ORG":          "org",
		"RAWTREE_PROJECT":      "project",
		"POSTGRES_PUBLICATION": "rawtree_publication",
		"PIPELINE_ID":          "7",
		"EXTRA":                "value",
	} {
		if env[k] != want {
			t.Fatalf("env %s = %q, want %q", k, env[k], want)
		}
	}
	if _, set := env["POSTGRES_TLS_ROOT_CERT_PATH"]; set {
		t.Fatal("POSTGRES_TLS_ROOT_CERT_PATH must not be set: the worker reads PEM from POSTGRES_TLS_ROOT_CERTS env var instead of mounting a file")
	}

	// The TLS secret must be injected under the env var name the worker
	// reads — POSTGRES_TLS_ROOT_CERTS (plural), not POSTGRES_TLS_ROOT_CERT_PEM.
	secretNames := map[string]string{}
	for _, s := range def.Secrets {
		secretNames[*s.Name] = *s.ValueFrom
	}
	if _, ok := secretNames["POSTGRES_TLS_ROOT_CERTS"]; !ok {
		t.Fatalf("expected POSTGRES_TLS_ROOT_CERTS secret, got %v", secretNames)
	}
	if _, ok := secretNames["POSTGRES_TLS_ROOT_CERT_PEM"]; ok {
		t.Fatal("POSTGRES_TLS_ROOT_CERT_PEM is the wrong env var name; the worker reads POSTGRES_TLS_ROOT_CERTS")
	}
	if def.LogConfiguration == nil || def.LogConfiguration.Options["awslogs-group"] != names.LogGroupName {
		t.Fatalf("missing awslogs config")
	}
}

func TestBuildNetworkConfiguration(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig{
		Subnets:        []string{"subnet-1", "subnet-2"},
		SecurityGroups: []string{"sg-1"},
		AssignPublicIP: true,
	}
	network := buildNetworkConfiguration(cfg)
	if network.AwsvpcConfiguration == nil {
		t.Fatal("missing awsvpc configuration")
	}
	if len(network.AwsvpcConfiguration.Subnets) != 2 {
		t.Fatalf("unexpected subnets: %#v", network.AwsvpcConfiguration.Subnets)
	}
	if string(network.AwsvpcConfiguration.AssignPublicIp) != "ENABLED" {
		t.Fatalf("expected public IP enabled, got %s", network.AwsvpcConfiguration.AssignPublicIp)
	}
}

func baseModel() SupabaseCDCIngestionModel {
	return SupabaseCDCIngestionModel{
		Name:                  types.StringValue("orders"),
		Region:                types.StringValue("us-east-1"),
		Publication:           types.StringValue("rawtree_publication"),
		PipelineID:            types.StringValue("1"),
		Image:                 types.StringValue(defaultImage),
		CPU:                   types.Int64Value(512),
		Memory:                types.Int64Value(1024),
		SubnetIDs:             types.ListValueMust(types.StringType, []attr.Value{types.StringValue("subnet-1")}),
		SecurityGroupIDs:      types.ListNull(types.StringType),
		AssignPublicIP:        types.BoolValue(false),
		DatabaseURL:           types.StringNull(),
		DatabaseURLSecretARN:  types.StringNull(),
		TLSRootCertPEM:        types.StringNull(),
		TLSRootCertSecretARN:  types.StringNull(),
		LogRetentionDays:      types.Int64Value(30),
		RunInitializationTask: types.BoolValue(true),
		InitializationCommand: types.ListNull(types.StringType),
		WorkerCommand:         types.ListNull(types.StringType),
		Environment:           types.MapNull(types.StringType),
		Organization:          types.StringNull(),
		Project:               types.StringNull(),
	}
}
