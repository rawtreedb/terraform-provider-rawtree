package integrations

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

// Configure resolves the provider settings required by ingestion resources.
func Configure(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.RawtreeClient {
	if req.ProviderData == nil {
		return nil
	}
	clients, ok := req.ProviderData.(*client.Clients)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Clients, got %T.", req.ProviderData))
		return nil
	}
	c := clients.Integrations
	if c.APIKey == "" {
		resp.Diagnostics.AddError("Missing ingestion settings", "Configure an ingestion API key for this integration.")
		return nil
	}
	return c
}
