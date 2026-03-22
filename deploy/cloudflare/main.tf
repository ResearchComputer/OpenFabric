terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }
}

provider "cloudflare" {
  api_token = var.cloudflare_api_token
}

# -----------------------------------------------------------------------------
# bootstraps.opentela.ai — round-robin proxied A records
# -----------------------------------------------------------------------------

resource "cloudflare_dns_record" "bootstraps" {
  for_each = toset(var.node_ips)
  zone_id  = var.cloudflare_zone_id
  name     = "bootstraps"
  content  = each.value
  type     = "A"
  proxied  = true
  ttl      = 1
}

# Origin Rule: override destination port to 8092 for bootstraps.opentela.ai
# Required because Cloudflare proxied mode does not support port 8092 natively.
resource "cloudflare_ruleset" "origin_port" {
  zone_id = var.cloudflare_zone_id
  name    = "Override origin port for bootstraps"
  kind    = "zone"
  phase   = "http_request_origin"

  rules = [
    {
      action = "route"
      action_parameters = {
        origin = {
          port = 8092
        }
      }
      expression  = "(http.host eq \"bootstraps.opentela.ai\")"
      description = "Route bootstraps.opentela.ai to origin port 8092"
      enabled     = true
    }
  ]
}

# SSL Configuration Rule: use Flexible SSL for bootstraps.opentela.ai
# The origin nodes serve plain HTTP on port 8092, so Cloudflare must connect
# over HTTP, not HTTPS. This does not affect docs.opentela.ai or other hostnames.
resource "cloudflare_ruleset" "ssl_flexible_bootstraps" {
  zone_id = var.cloudflare_zone_id
  name    = "Flexible SSL for bootstraps"
  kind    = "zone"
  phase   = "http_config_settings"

  rules = [
    {
      action = "set_config"
      action_parameters = {
        ssl = "flexible"
      }
      expression  = "(http.host eq \"bootstraps.opentela.ai\")"
      description = "Use Flexible SSL for bootstraps.opentela.ai (origin is plain HTTP)"
      enabled     = true
    }
  ]
}

# -----------------------------------------------------------------------------
# docs.opentela.ai — Worker infrastructure (code deployed via wrangler)
# -----------------------------------------------------------------------------

# R2 bucket for Next.js incremental cache
resource "cloudflare_r2_bucket" "docs_cache" {
  account_id = var.cloudflare_account_id
  name       = "opentela-docs-opennext-cache"
}

# Bind docs.opentela.ai to the opentela-docs Worker
# Note: This auto-creates a DNS record (AAAA 100::) — no separate DNS record needed.
resource "cloudflare_workers_custom_domain" "docs" {
  account_id = var.cloudflare_account_id
  zone_id    = var.cloudflare_zone_id
  hostname   = "docs.opentela.ai"
  service    = "opentela-docs"
}
