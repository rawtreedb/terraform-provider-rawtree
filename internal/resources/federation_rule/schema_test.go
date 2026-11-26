package federation_rule

import (
	"context"
	"testing"
)

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	s := resourceSchema()
	if diags := s.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema: %v", diags)
	}
	for _, attr := range []string{"name", "issuer_id", "allowed_audiences", "claim_conditions", "principal_type", "principal_id", "database"} {
		if !s.Attributes[attr].IsRequired() {
			t.Errorf("%s should be required", attr)
		}
	}
	for _, attr := range []string{"cluster", "description", "permission", "database_roles", "enabled"} {
		if !s.Attributes[attr].IsOptional() {
			t.Errorf("%s should be optional", attr)
		}
	}
	for _, attr := range []string{"id", "cluster", "organization_id", "cluster_id", "enabled", "created_at", "updated_at"} {
		if !s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be computed", attr)
		}
	}
}
