package client

import (
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	c := New("rw_testkey", "https://api.test.rawtree.com", "myorg", "myproject")

	if c.APIKey != "rw_testkey" {
		t.Errorf("expected API key rw_testkey, got %s", c.APIKey)
	}
	if c.APIURL != "https://api.test.rawtree.com" {
		t.Errorf("expected API URL https://api.test.rawtree.com, got %s", c.APIURL)
	}
	if c.Organization != "myorg" {
		t.Errorf("expected org myorg, got %s", c.Organization)
	}
	if c.Project != "myproject" {
		t.Errorf("expected project myproject, got %s", c.Project)
	}
	if c.HTTPClient == nil {
		t.Error("expected HTTP client to be initialized")
	}
}
