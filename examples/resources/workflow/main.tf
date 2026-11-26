terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}

provider "rawtree" {
  # Uses RAWTREE_API_KEY and RAWTREE_DATABASE, or an access token with
  # RAWTREE_ORG and RAWTREE_CLUSTER.
}

variable "webhook_token" {
  type      = string
  sensitive = true
}

# Every 5 minutes, copy error rows to a table, POST them to a webhook and
# post them to Slack.
resource "rawtree_workflow" "errors" {
  name        = "errors"
  description = "Errors in the last 5 minutes"
  database    = "analytics"
  sql         = "SELECT * FROM events WHERE level = 'error' AND ts > now() - INTERVAL 5 MINUTE"

  schedule = {
    cron = "*/5 * * * *"
  }

  sinks = [
    {
      table = {
        database = "analytics"
        table    = "errors"
      }
    },
    {
      http = {
        url = "https://example.com/hooks/errors"
        headers = {
          Authorization = "Bearer ${var.webhook_token}"
        }
      }
    },
    {
      slack = {
        integration_id = "00000000-0000-0000-0000-000000000000"
        channel_name   = "#alerts"
      }
    },
  ]
}

# Manual-only workflow without sinks.
resource "rawtree_workflow" "adhoc" {
  name = "adhoc"
  sql  = "SELECT count() FROM events"
}
