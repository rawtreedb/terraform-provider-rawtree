package supabase_cdc_ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

// The stock worker sends unqualified table requests. Check the same credential
// and origin before an ECS rollout so it cannot silently write to another database.
func verifyWorkerDestination(ctx context.Context, c *client.RawtreeClient, cfg resolvedConfig) error {
	const stockRepository = "ghcr.io/rawtreedb/supabase-etl"
	if cfg.Image != stockRepository && !strings.HasPrefix(cfg.Image, stockRepository+":") && !strings.HasPrefix(cfg.Image, stockRepository+"@") {
		return nil
	}
	if override, ok := cfg.Environment["RAWTREE_API_URL"]; ok && override != cfg.APIURL {
		return fmt.Errorf("set api_url on the provider to the stock worker's API origin")
	}
	if _, ok := cfg.Environment["RAWTREE_API_KEY"]; ok {
		return fmt.Errorf("set the stock worker's ingestion key with the provider api_key")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIURL, "/")+"/v1/query", strings.NewReader(`{"sql":"SELECT currentDatabase() AS database"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received HTTP %d from Tinybird RawTree when checking the stock worker's default database. Use an ingestion key that can query its default database", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			Database string `json:"database"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if len(result.Data) != 1 || result.Data[0].Database == "" {
		return fmt.Errorf("no default database for the ingestion key in the Tinybird RawTree response")
	}
	if result.Data[0].Database != cfg.Database {
		return fmt.Errorf("the stock worker's key selects %q, but this integration targets %q. Use a key issued with %q as its default database", result.Data[0].Database, cfg.Database, cfg.Database)
	}
	return nil
}
