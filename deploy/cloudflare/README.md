# Cloudflare Infrastructure (OpenTofu)

Manages Cloudflare DNS records, origin rules, R2 bucket, and Worker routing for:
- `bootstraps.opentela.ai` — round-robin HTTPS proxy to bootstrap nodes (port 8092)
- `docs.opentela.ai` — Worker custom domain + R2 cache for the docs site

Worker code deployment is handled by `wrangler deploy` from `docs/`, not by OpenTofu.

## Prerequisites

- [OpenTofu](https://opentofu.org/docs/intro/install/) installed
- Cloudflare API token with permissions: DNS:Edit, Zone Rulesets:Edit, R2:Edit, Workers:Edit
- Cloudflare account ID and zone ID for `opentela.ai`

## Setup

1. Create `terraform.tfvars` (this file is git-ignored):

   ```hcl
   cloudflare_api_token  = "your-api-token"
   cloudflare_account_id = "your-account-id"
   cloudflare_zone_id    = "your-zone-id"
   ```

2. Initialize:

   ```bash
   tofu init
   ```

3. If adopting existing Cloudflare resources, import them first:

   ```bash
   # Find record IDs via Cloudflare dashboard or API
   tofu import cloudflare_r2_bucket.docs_cache <account_id>/opentela-docs-opennext-cache
   tofu import cloudflare_workers_custom_domain.docs <account_id>/docs.opentela.ai
   ```

4. Review and apply:

   ```bash
   tofu plan
   tofu apply
   ```

5. Commit the lock file:

   ```bash
   git add .terraform.lock.hcl
   git commit -m "chore(cloudflare): add dependency lock file"
   ```

## Adding/Removing Bootstrap Nodes

Edit `terraform.tfvars`:

```hcl
node_ips = ["140.238.223.116", "152.67.64.117", "NEW.IP.HERE"]
```

Then: `tofu plan && tofu apply`
