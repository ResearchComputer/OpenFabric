# Cloudflare DNS with OpenTofu — Design Spec

## Goal

Manage Cloudflare DNS records for the OpenTela test mesh using OpenTofu, putting ocf-1 and ocf-2 behind Cloudflare's HTTPS proxy on port 443 and creating a `bootstraps.opentela.ai` round-robin endpoint.

## Scope

- OpenTofu configuration for Cloudflare DNS records only
- Test mesh nodes (ocf-1: 140.238.223.116, ocf-2: 152.67.64.117) only
- No changes to Go code, systemd units, or Ansible playbooks

## Architecture

Cloudflare acts as a reverse proxy. Clients connect to `https://bootstraps.opentela.ai` on port 443. Cloudflare terminates TLS at the edge and forwards requests to the origin nodes on port 8092 via HTTP. Two proxied A records provide round-robin load distribution.

```
Client
  │
  │ HTTPS :443
  ▼
Cloudflare Edge (TLS termination, round-robin)
  │
  │ HTTP :8092
  ▼
ocf-1 (140.238.223.116)  or  ocf-2 (152.67.64.117)
```

Nodes continue listening on port 8092. No TLS certificates are needed on the nodes.

## File Structure

```
deploy/cloudflare/
├── main.tf              # Provider config + DNS record resources
├── variables.tf         # Input variable declarations
├── terraform.tfvars     # Actual values (git-ignored, contains API token)
├── .terraform.lock.hcl  # Dependency lock (committed)
├── .gitignore           # Ignores *.tfstate*, terraform.tfvars, .terraform/
└── README.md            # Setup instructions
```

## OpenTofu Configuration

### Provider

```hcl
terraform {
  required_providers {
    cloudflare = {
      source = "cloudflare/cloudflare"
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
| `cloudflare_api_token` | `string` (sensitive) | API token with DNS:Edit permission for the zone |
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

No individual DNS names for the nodes (e.g., no `ocf-1.opentela.ai`).

### State

Local state file, git-ignored. No remote backend.

## What Changes After Deployment

New nodes can use `https://bootstraps.opentela.ai/v1/dnt/bootstraps` as a bootstrap source instead of raw IP addresses. Existing raw-IP bootstrap sources continue to work.

## What Does NOT Change

- Go source code
- Systemd service units
- Ansible playbook and inventory
- Node listen port (stays 8092)
- Existing bootstrap resolution logic (already supports HTTP URLs)

## Design Decisions

1. **Cloudflare proxied DNS over direct TLS on nodes**: Avoids cert management on nodes, provides DDoS protection and caching for free.
2. **Round-robin A records over Cloudflare Worker**: Simpler, no code to maintain. Cloudflare handles failover at the proxy layer.
3. **Local state, git-ignored**: Appropriate for a small two-node setup. No remote backend complexity.
4. **No individual node DNS names**: Only `bootstraps.opentela.ai` is needed. Individual node access continues via raw IPs for SSH and debugging.
