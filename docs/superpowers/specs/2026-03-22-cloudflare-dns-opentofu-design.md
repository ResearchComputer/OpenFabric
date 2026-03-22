# Cloudflare DNS with OpenTofu — Design Spec

## Goal

Manage Cloudflare DNS records for the OpenTela test mesh using OpenTofu, putting ocf-1 and ocf-2 behind Cloudflare's HTTPS proxy on port 443 and creating a `bootstraps.opentela.ai` round-robin endpoint.

## Scope

- OpenTofu configuration for Cloudflare DNS records and origin rule
- Test mesh nodes (ocf-1: 140.238.223.116, ocf-2: 152.67.64.117) only
- No changes to Go code, systemd units, or Ansible playbooks

## Architecture

Cloudflare acts as a reverse proxy. Clients connect to `https://bootstraps.opentela.ai` on port 443. Cloudflare terminates TLS at the edge and forwards requests to the origin nodes on port 8092 via HTTP. Two proxied A records provide round-robin load distribution.

An Origin Rule is required because Cloudflare's proxied mode only connects to a fixed set of origin ports (80, 8080, etc.) — port 8092 is not in the allowlist. The Origin Rule overrides the destination port to 8092 for requests matching `bootstraps.opentela.ai`.

```
Client
  │
  │ HTTPS :443
  ▼
Cloudflare Edge (TLS termination, round-robin)
  │
  │ HTTP :8092 (via Origin Rule port override)
  ▼
ocf-1 (140.238.223.116)  or  ocf-2 (152.67.64.117)
```

Nodes continue listening on port 8092. No TLS certificates are needed on the nodes.

**SSL/TLS mode:** The zone must use "Flexible" SSL mode for `bootstraps.opentela.ai` traffic (Cloudflare connects to origin over plain HTTP). If the docs site requires "Full" SSL, a Configuration Rule scoped to `bootstraps.opentela.ai` can override the SSL mode for just this hostname.

**Failover behavior:** With proxied round-robin A records, Cloudflare retries a different origin on 5xx errors, but does not proactively remove unhealthy origins. If a node is down, initial requests to that origin will experience a timeout before retry. This is acceptable for the bootstrap use case.

## File Structure

```
deploy/cloudflare/
├── main.tf              # Provider config, DNS records, origin rule
├── variables.tf         # Input variable declarations
├── terraform.tfvars     # Actual values (git-ignored, contains API token)
├── outputs.tf           # Output the bootstrap URL after apply
├── .terraform.lock.hcl  # Dependency lock (committed)
├── .gitignore           # Ignores *.tfstate*, terraform.tfvars, .terraform/
└── README.md            # Setup instructions
```

State files (`*.tfstate`, `*.tfstate.backup`) and `terraform.tfvars` (contains Cloudflare API token) are git-ignored. The `.gitignore` must be created **before** running `tofu init`. The lock file is committed for reproducibility.

## OpenTofu Configuration

### Provider

```hcl
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
```

### Variables

| Variable | Type | Description |
|----------|------|-------------|
| `cloudflare_api_token` | `string` (sensitive) | API token with DNS:Edit and Zone Rulesets:Edit permissions |
| `cloudflare_zone_id` | `string` | Zone ID for `opentela.ai` |
| `node_ips` | `list(string)` | Origin IPs: `["140.238.223.116", "152.67.64.117"]` |

### Resources

Two proxied A records for `bootstraps.opentela.ai`, one per node IP:

```hcl
resource "cloudflare_record" "bootstraps" {
  for_each = toset(var.node_ips)
  zone_id  = var.cloudflare_zone_id
  name     = "bootstraps"
  content  = each.value
  type     = "A"
  proxied  = true
}
```

TTL is automatically managed by Cloudflare when `proxied = true`.

An Origin Rule to override the destination port to 8092:

```hcl
resource "cloudflare_ruleset" "origin_port" {
  zone_id = var.cloudflare_zone_id
  name    = "Override origin port for bootstraps"
  kind    = "zone"
  phase   = "http_request_origin"

  rules {
    action = "route"
    action_parameters {
      origin {
        port = 8092
      }
    }
    expression  = "(http.host eq \"bootstraps.opentela.ai\")"
    description = "Route bootstraps.opentela.ai to origin port 8092"
    enabled     = true
  }
}
```

Adding or removing entries in `var.node_ips` will only affect the corresponding DNS record without disrupting others (benefit of `for_each` over `count`).

No individual DNS names for the nodes (e.g., no `ocf-1.opentela.ai`).

### Outputs

```hcl
output "bootstrap_url" {
  value = "https://bootstraps.opentela.ai/v1/dnt/bootstraps"
}
```

### State

Local state file, git-ignored. No remote backend.

## What Changes After Deployment

New nodes can use `https://bootstraps.opentela.ai/v1/dnt/bootstraps` as a bootstrap source instead of raw IP addresses. Existing raw-IP bootstrap sources continue to work.

**Future follow-up (not in scope):** Once DNS is proven stable, update the hardcoded bootstrap defaults in `src/entry/cmd/root.go` to use `https://bootstraps.opentela.ai/v1/dnt/bootstraps` as primary, with raw IPs as fallback.

## What Does NOT Change

- Go source code
- Systemd service units
- Ansible playbook and inventory
- Node listen port (stays 8092)
- Existing bootstrap resolution logic (already supports HTTP URLs)

## Design Decisions

1. **Cloudflare proxied DNS over direct TLS on nodes**: Avoids cert management on nodes, provides DDoS protection and caching for free.
2. **Origin Rule for port override**: Cloudflare proxied mode does not support port 8092 natively. An Origin Rule (available on Free plan) overrides the destination port.
3. **Round-robin A records over Cloudflare Worker**: Simpler, no code to maintain. Acceptable failover latency for the bootstrap use case.
4. **Local state, git-ignored**: Appropriate for a small two-node setup. No remote backend complexity.
5. **No individual node DNS names**: Only `bootstraps.opentela.ai` is needed. Individual node access continues via raw IPs for SSH and debugging.
6. **Provider version pinned to `~> 5.0`**: Prevents unexpected breaking changes from major version bumps.
