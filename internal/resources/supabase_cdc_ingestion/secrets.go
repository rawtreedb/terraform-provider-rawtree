package supabase_cdc_ingestion

import (
	"encoding/json"
	"fmt"
)

const (
	configSecretKeyAPIKey      = "RAWTREE_API_KEY"
	configSecretKeyDatabaseURL = "DATABASE_URL"
	// configSecretKeyTLSRootCert matches the env var name the supabase/etl
	// worker reads PEM content from directly (see trusted_root_certs() in the
	// example main.rs). Reading from an env var avoids the file-mount problem
	// on Fargate, where there's no native "secret as file" mechanism.
	configSecretKeyTLSRootCert = "POSTGRES_TLS_ROOT_CERTS"
)

// ecsSecretRefs holds the per-env-var ValueFrom strings injected into the ECS
// container definition. Each value is either a JSON-keyed reference into our
// managed config secret (e.g. "arn:...:secret:name:RAWTREE_API_KEY::") or the
// raw ARN of an external Secrets Manager secret provided by the user.
type ecsSecretRefs struct {
	APIKey      string
	DatabaseURL string
	TLSRootCert string // empty if no TLS cert is configured
}

// buildConfigSecretJSON produces the JSON payload stored in the single managed
// Secrets Manager secret. The Rawtree API key is always included. DATABASE_URL
// and POSTGRES_TLS_ROOT_CERT_PEM are included only when the user provided the
// raw value inline (rather than an external Secrets Manager ARN).
func buildConfigSecretJSON(cfg resolvedConfig) (string, error) {
	payload := map[string]string{
		configSecretKeyAPIKey: cfg.APIKey,
	}
	if cfg.DatabaseURL != "" {
		payload[configSecretKeyDatabaseURL] = cfg.DatabaseURL
	}
	if cfg.TLSRootCertPEM != "" {
		payload[configSecretKeyTLSRootCert] = cfg.TLSRootCertPEM
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding managed config secret JSON: %w", err)
	}
	return string(b), nil
}

// buildECSSecretRefs returns the ValueFrom strings the ECS task definition uses
// for each secret-backed env var. configSecretARN is the managed secret's ARN.
func buildECSSecretRefs(cfg resolvedConfig, configSecretARN string) ecsSecretRefs {
	refs := ecsSecretRefs{
		APIKey: jsonKeyValueFrom(configSecretARN, configSecretKeyAPIKey),
	}
	if cfg.DatabaseURL != "" {
		refs.DatabaseURL = jsonKeyValueFrom(configSecretARN, configSecretKeyDatabaseURL)
	} else {
		refs.DatabaseURL = cfg.DatabaseURLSecretARN
	}
	if cfg.TLSRootCertPEM != "" {
		refs.TLSRootCert = jsonKeyValueFrom(configSecretARN, configSecretKeyTLSRootCert)
	} else if cfg.TLSRootCertSecretARN != "" {
		refs.TLSRootCert = cfg.TLSRootCertSecretARN
	}
	return refs
}

// collectSecretARNs returns the *base* secret ARNs the execution role needs
// GetSecretValue on. JSON-key suffixes are stripped — IAM matches on the base
// secret ARN, not on per-key references.
func collectSecretARNs(cfg resolvedConfig, configSecretARN string) []string {
	arns := []string{configSecretARN}
	if cfg.DatabaseURL == "" && cfg.DatabaseURLSecretARN != "" {
		arns = append(arns, cfg.DatabaseURLSecretARN)
	}
	if cfg.TLSRootCertPEM == "" && cfg.TLSRootCertSecretARN != "" {
		arns = append(arns, cfg.TLSRootCertSecretARN)
	}
	return arns
}

// jsonKeyValueFrom formats the ECS valueFrom string that selects a single
// JSON key out of a managed secret: "<ARN>:<json-key>::". The trailing empty
// fields are version-stage and version-id, which we always leave blank.
func jsonKeyValueFrom(secretARN, jsonKey string) string {
	return fmt.Sprintf("%s:%s::", secretARN, jsonKey)
}
