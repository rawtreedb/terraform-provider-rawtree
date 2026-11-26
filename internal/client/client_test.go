package client

import (
	"net/url"
	"testing"
)

func TestTableURL(t *testing.T) {
	c := New("rt_x", "https://api.rawtree.com/", "", "", "")
	if got := c.TableURL("", "events", nil); got != "https://api.rawtree.com/v1/tables/events" {
		t.Errorf("no database: %s", got)
	}
	got := c.TableURL("analytics", "waf logs", url.Values{"transform": {"firehose"}})
	if got != "https://api.rawtree.com/v1/tables/waf%20logs?database=analytics&transform=firehose" {
		t.Errorf("with database: %s", got)
	}
}
