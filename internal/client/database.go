package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Database identifies a database within its organization and cluster.
type Database struct {
	Organization string
	Cluster      string
	Name         string
}

func (d Database) ID() string {
	return strings.Join([]string{d.Organization, d.Cluster, d.Name}, "/")
}

func (c *PlatformClient) DatabaseExists(ctx context.Context, db Database) (bool, error) {
	names, err := c.ListDatabases(ctx, db.Organization, db.Cluster)
	return slices.Contains(names, db.Name), err
}

func (c *PlatformClient) ListDatabases(ctx context.Context, organization, cluster string) ([]string, error) {
	var result struct {
		Databases []struct{ Name string } `json:"databases"`
	}
	scope := Database{Organization: organization, Cluster: cluster}
	if err := c.databaseRequest(ctx, scope, http.MethodGet, nil, &result); err != nil {
		return nil, err
	}
	if result.Databases == nil {
		return nil, fmt.Errorf("incomplete database list from Tinybird RawTree")
	}
	names := make([]string, 0, len(result.Databases))
	for _, item := range result.Databases {
		if item.Name == "" {
			return nil, fmt.Errorf("database entry from Tinybird RawTree has no name")
		}
		names = append(names, item.Name)
	}
	return names, nil
}

func (c *PlatformClient) CreateDatabase(ctx context.Context, db Database) error {
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{db.Name})
	if err != nil {
		return err
	}
	var result struct{ Database struct{ Name string } }
	if err := c.databaseRequest(ctx, db, http.MethodPost, body, &result); err != nil {
		return err
	}
	if result.Database.Name != db.Name {
		return fmt.Errorf("could not confirm creation of database %q with Tinybird RawTree", db.Name)
	}
	return nil
}

func (c *PlatformClient) DeleteDatabase(ctx context.Context, db Database) error {
	var result struct{ Deleted *bool }
	if err := c.databaseRequest(ctx, db, http.MethodDelete, nil, &result); err != nil {
		return err
	}
	// The API returns deleted=false if the database is already absent.
	if result.Deleted == nil {
		return fmt.Errorf("could not confirm deletion of database %q with Tinybird RawTree", db.Name)
	}
	return nil
}

func (c *PlatformClient) databaseRequest(ctx context.Context, db Database, method string, body []byte, result any) error {
	endpoint := c.baseURL + "/v1/databases"
	if method == http.MethodDelete {
		endpoint += "/" + url.PathEscape(db.Name)
	}
	scope := url.Values{"organization": {db.Organization}, "cluster": {db.Cluster}}
	return c.platformRequest(ctx, method, endpoint, scope, body, http.StatusOK, result)
}
