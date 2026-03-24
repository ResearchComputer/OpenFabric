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
# bootstraps.opentela.ai — round-robin proxied A records (HTTP API)
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

# -----------------------------------------------------------------------------
# p2p.opentela.ai — round-robin proxied A records (libp2p WebSocket)
# Allows nodes behind restrictive firewalls (e.g., JSC/JUWELS) to connect
# to head nodes via WSS on port 443, which Cloudflare proxies to the origin
# WebSocket listener on port 43905.
# -----------------------------------------------------------------------------

resource "cloudflare_dns_record" "p2p" {
  for_each = toset(var.node_ips)
  zone_id  = var.cloudflare_zone_id
  name     = "p2p"
  content  = each.value
  type     = "A"
  proxied  = true
  ttl      = 1
}

# Origin Rules: override destination ports for proxied subdomains.
# One ruleset per phase per zone — all origin rules must be in one resource.
resource "cloudflare_ruleset" "origin_port" {
  zone_id = var.cloudflare_zone_id
  name    = "Override origin ports"
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
      description = "Route bootstraps.opentela.ai to origin port 8092 (HTTP API)"
      enabled     = true
    },
    {
      action = "route"
      action_parameters = {
        origin = {
          port = 43906
        }
      }
      expression  = "(http.host eq \"p2p.opentela.ai\")"
      description = "Route p2p.opentela.ai to origin port 43906 (libp2p WebSocket)"
      enabled     = true
    }
  ]
}

# SSL Configuration Rules: use Flexible SSL for proxied subdomains.
# Origins serve plain HTTP/WS, Cloudflare terminates TLS on the client side.
resource "cloudflare_ruleset" "ssl_flexible" {
  zone_id = var.cloudflare_zone_id
  name    = "Flexible SSL for proxied services"
  kind    = "zone"
  phase   = "http_config_settings"

  rules = [
    {
      action = "set_config"
      action_parameters = {
        ssl = "flexible"
      }
      expression  = "(http.host eq \"bootstraps.opentela.ai\" or http.host eq \"p2p.opentela.ai\")"
      description = "Use Flexible SSL for bootstraps and p2p (origins are plain HTTP/WS)"
      enabled     = true
    }
  ]
}

# Note: WebSocket support is enabled by default on Cloudflare.
# No explicit zone setting needed.

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
