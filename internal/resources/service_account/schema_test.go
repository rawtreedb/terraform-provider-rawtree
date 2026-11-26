package service_account

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestResourceSchema(t *testing.T) {
	t.Parallel()

	s := resourceSchema()
	if diags := s.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema: %v", diags)
	}
	if !s.Attributes["name"].IsRequired() {
		t.Error("name should be required")
	}
	for _, attr := range []string{"id", "organization_id", "role"} {
		if !s.Attributes[attr].IsComputed() {
			t.Errorf("%s should be computed", attr)
		}
	}
	// No update endpoint: every configurable attribute forces replacement.
	for _, attr := range []string{"name", "description", "role"} {
		if len(s.Attributes[attr].(schema.StringAttribute).PlanModifiers) == 0 {
			t.Errorf("%s should require replacement", attr)
		}
	}
}
