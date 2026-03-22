# Cloudflare DNS + Infrastructure with OpenTofu — Design Spec

## Goal

Manage Cloudflare infrastructure for the OpenTela project using OpenTofu:
1. `bootstraps.opentela.ai` — round-robin proxied DNS pointing to ocf-1 and ocf-2 on port 443, with an Origin Rule forwarding to port 8092
2. `docs.opentela.ai` — DNS record, R2 bucket, and Worker custom domain for the docs site (Worker code deployment stays with wrangler)

## Scope

- OpenTofu configuration for Cloudflare DNS records, origin rule, R2 bucket, and Worker routing
- Test mesh nodes (ocf-1: 140.238.223.116, ocf-2: 152.67.64.117)
- Docs site infrastructure (DNS, R2 bucket, Worker custom domain)
- No changes to Go code, systemd units, or Ansible playbooks
- Worker code deployment remains with `wrangler deploy`

## Architecture

### bootstraps.opentela.ai

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

**SSL/TLS mode:** A Configuration Rule scoped to `bootstraps.opentela.ai` sets SSL to "Flexible" (Cloudflare connects to origin over plain HTTP). This does not affect `docs.opentela.ai`, which can use "Full" or whatever the zone default is.

**Failover behavior:** With proxied round-robin A records, Cloudflare retries a different origin on 5xx errors, but does not proactively remove unhealthy origins. If a node is down, initial requests to that origin will experience a timeout before retry. This is acceptable for the bootstrap use case.

### docs.opentela.ai

OpenTofu manages the infrastructure layer:
- **R2 bucket** (`opentela-docs-opennext-cache`) for Next.js incremental cache
- **Worker custom domain** binding `docs.opentela.ai` to the `opentela-docs` Worker
- **DNS record** (CNAME, proxied) for `docs.opentela.ai`

Wrangler continues to handle Worker code and asset deployment. The split:

| Concern | Managed by |
|---------|-----------|
| DNS record for `docs.opentela.ai` | OpenTofu |
| R2 bucket creation | OpenTofu |
| Worker custom domain | OpenTofu |
| Worker script + assets upload | Wrangler (`wrangler deploy`) |
| Worker bindings (R2, images, etc.) | Wrangler (`wrangler.jsonc`) |

## File Structure

```
deploy/cloudflare/
├── main.tf              # Provider config, all resources
├── variables.tf         # Input variable declarations
├── terraform.tfvars     # Actual values (git-ignored, contains API token)
├── outputs.tf           # Output URLs after apply
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
| `cloudflare_api_token` | `string` (sensitive) | API token with DNS:Edit, Zone Rulesets:Edit, R2:Edit, Workers:Edit permissions |
| `cloudflare_account_id` | `string` | Cloudflare account ID (needed for R2 and Workers) |
| `cloudflare_zone_id` | `string` | Zone ID for `opentela.ai` |
| `node_ips` | `list(string)` | Origin IPs: `["140.238.223.116", "152.67.64.117"]` |

### Resources — bootstraps.opentela.ai

Two proxied A records, one per node IP:

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

Origin Rule to override the destination port to 8092:

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

### Resources — docs.opentela.ai

R2 bucket for Next.js incremental cache:

```hcl
resource "cloudflare_r2_bucket" "docs_cache" {
  account_id = var.cloudflare_account_id
  name       = "opentela-docs-opennext-cache"
}
```

DNS record for the docs site (CNAME to the Worker):

```hcl
resource "cloudflare_record" "docs" {
  zone_id = var.cloudflare_zone_id
  name    = "docs"
  content = "opentela-docs.workers.dev"
  type    = "CNAME"
  proxied = true
}
```

Worker custom domain:

```hcl
resource "cloudflare_workers_custom_domain" "docs" {
  account_id = var.cloudflare_account_id
  zone_id    = var.cloudflare_zone_id
  hostname   = "docs.opentela.ai"
  service    = "opentela-docs"
}
```

### Outputs

```hcl
output "bootstrap_url" {
  value = "https://bootstraps.opentela.ai/v1/dnt/bootstraps"
}

output "docs_url" {
  value = "https://docs.opentela.ai"
}
```

### State

Local state file, git-ignored. No remote backend.

## Import Existing Resources

The R2 bucket, docs DNS record, and Worker custom domain may already exist in Cloudflare. Before the first `tofu apply`, use `tofu import` to adopt them into state without recreating:

```bash
tofu import cloudflare_r2_bucket.docs_cache <account_id>/opentela-docs-opennext-cache
tofu import cloudflare_record.docs <zone_id>/<record_id>
tofu import cloudflare_workers_custom_domain.docs <account_id>/docs.opentela.ai
```

Record IDs can be found via the Cloudflare API or dashboard.

## What Changes After Deployment

- New nodes can use `https://bootstraps.opentela.ai/v1/dnt/bootstraps` as a bootstrap source instead of raw IP addresses. Existing raw-IP bootstrap sources continue to work.
- Docs infrastructure (DNS, R2, routing) is now managed as code and can be reproduced or modified via `tofu plan`/`tofu apply`.

**Future follow-up (not in scope):** Once DNS is proven stable, update the hardcoded bootstrap defaults in `src/entry/cmd/root.go` to use `https://bootstraps.opentela.ai/v1/dnt/bootstraps` as primary, with raw IPs as fallback.

## What Does NOT Change

- Go source code
- Systemd service units
- Ansible playbook and inventory
- Node listen port (stays 8092)
- Existing bootstrap resolution logic (already supports HTTP URLs)
- Worker code deployment workflow (`wrangler deploy` from `docs/`)
- Worker bindings in `wrangler.jsonc`

## Design Decisions

1. **Cloudflare proxied DNS over direct TLS on nodes**: Avoids cert management on nodes, provides DDoS protection and caching for free.
2. **Origin Rule for port override**: Cloudflare proxied mode does not support port 8092 natively. An Origin Rule (available on Free plan) overrides the destination port.
3. **Round-robin A records over Cloudflare Worker**: Simpler, no code to maintain. Acceptable failover latency for the bootstrap use case.
4. **Local state, git-ignored**: Appropriate for this setup size. No remote backend complexity.
5. **No individual node DNS names**: Only `bootstraps.opentela.ai` is needed. Individual node access continues via raw IPs for SSH and debugging.
6. **Provider version pinned to `~> 5.0`**: Prevents unexpected breaking changes from major version bumps.
7. **Split responsibility for docs Worker**: OpenTofu manages infrastructure (DNS, R2, routing); wrangler manages code deployment. This avoids fighting wrangler's asset upload pipeline while keeping infrastructure as code.
8. **Per-hostname SSL Configuration Rule**: Scoping "Flexible" SSL to `bootstraps.opentela.ai` avoids affecting the docs site's SSL settings.
