package federation_issuer

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
	for _, attr := range []string{"name", "issuer_url"} {
		if !s.Attributes[attr].IsRequired() {
			t.Errorf("%s should be required", attr)
		}
	}
	for _, attr := range []string{"description", "jwks_source", "jwks_url"} {
		if !s.Attributes[attr].IsOptional() {
			t.Errorf("%s should be optional", attr)
		}
	}
	for _, attr := range []string{"id", "organization_id", "jwks_source", "created_at", "updated_at"} {
		if !s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be computed", attr)
		}
	}
}
