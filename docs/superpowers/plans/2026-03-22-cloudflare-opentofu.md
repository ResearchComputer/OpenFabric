# Cloudflare DNS + Infrastructure with OpenTofu — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Manage Cloudflare DNS records, origin rules, R2 bucket, and Worker routing for `bootstraps.opentela.ai` and `docs.opentela.ai` using OpenTofu.

**Architecture:** A single OpenTofu configuration in `deploy/cloudflare/` manages all Cloudflare infrastructure. Bootstraps uses proxied round-robin A records with an Origin Rule for port 8092 and a Configuration Rule for Flexible SSL. Docs uses a CNAME + Worker custom domain + R2 bucket, with wrangler handling code deployment.

**Tech Stack:** OpenTofu, Cloudflare provider `~> 5.0`, HCL

**Spec:** `docs/superpowers/specs/2026-03-22-cloudflare-dns-opentofu-design.md`

---

## File Structure

All files are created in `deploy/cloudflare/`:

| File | Responsibility |
|------|---------------|
| `.gitignore` | Ignore state files, tfvars, .terraform/ |
| `variables.tf` | Input variable declarations |
| `main.tf` | Provider config + all resources |
| `outputs.tf` | Output URLs after apply |
| `README.md` | Setup and usage instructions |

---

### Task 1: Create directory and .gitignore

**Files:**
- Create: `deploy/cloudflare/.gitignore`

- [ ] **Step 1: Create the .gitignore**

```gitignore
# OpenTofu / Terraform
.terraform/
*.tfstate
*.tfstate.backup

# Secrets
terraform.tfvars
```

Note: `.terraform.lock.hcl` is NOT ignored — it is committed for reproducibility.

- [ ] **Step 2: Verify**

```bash
ls deploy/cloudflare/.gitignore
```

- [ ] **Step 3: Commit**

```bash
git add deploy/cloudflare/.gitignore
git commit -m "chore(cloudflare): add .gitignore for OpenTofu state and secrets"
```

---

### Task 2: Create variables.tf

**Files:**
- Create: `deploy/cloudflare/variables.tf`

- [ ] **Step 1: Write variables.tf**

```hcl
variable "cloudflare_api_token" {
  description = "Cloudflare API token with DNS:Edit, Zone Rulesets:Edit, R2:Edit, Workers:Edit permissions"
  type        = string
  sensitive   = true
}

variable "cloudflare_account_id" {
  description = "Cloudflare account ID (needed for R2 and Workers)"
  type        = string
}

variable "cloudflare_zone_id" {
  description = "Cloudflare zone ID for opentela.ai"
  type        = string
}

variable "node_ips" {
  description = "Origin IPs for bootstrap nodes"
  type        = list(string)
  default     = ["140.238.223.116", "152.67.64.117"]
}
```

- [ ] **Step 2: Commit**

```bash
git add deploy/cloudflare/variables.tf
git commit -m "chore(cloudflare): add variable declarations"
```

---

### Task 3: Create main.tf with provider and all resources

**Files:**
- Create: `deploy/cloudflare/main.tf`

**Important:** Cloudflare provider v5 renamed `cloudflare_record` to `cloudflare_dns_record` and uses list-of-objects syntax for ruleset `rules` (not nested blocks).

- [ ] **Step 1: Write main.tf**

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

# DNS record for the docs site
resource "cloudflare_dns_record" "docs" {
  zone_id = var.cloudflare_zone_id
  name    = "docs"
  content = "opentela-docs.workers.dev"
  type    = "CNAME"
  proxied = true
}

# Bind docs.opentela.ai to the opentela-docs Worker
resource "cloudflare_workers_custom_domain" "docs" {
  account_id = var.cloudflare_account_id
  zone_id    = var.cloudflare_zone_id
  hostname   = "docs.opentela.ai"
  service    = "opentela-docs"
}
```

- [ ] **Step 2: Commit**

```bash
git add deploy/cloudflare/main.tf
git commit -m "feat(cloudflare): add provider, bootstraps DNS/origin rule/SSL, and docs infrastructure"
```

---

### Task 4: Create outputs.tf

**Files:**
- Create: `deploy/cloudflare/outputs.tf`

- [ ] **Step 1: Write outputs.tf**

```hcl
output "bootstrap_url" {
  description = "HTTPS URL for the bootstrap endpoint"
  value       = "https://bootstraps.opentela.ai/v1/dnt/bootstraps"
}

output "docs_url" {
  description = "HTTPS URL for the documentation site"
  value       = "https://docs.opentela.ai"
}
```

- [ ] **Step 2: Commit**

```bash
git add deploy/cloudflare/outputs.tf
git commit -m "chore(cloudflare): add output definitions"
```

---

### Task 5: Create README.md

**Files:**
- Create: `deploy/cloudflare/README.md`

- [ ] **Step 1: Write README.md**

````markdown
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
   tofu import cloudflare_dns_record.docs <zone_id>/<record_id>
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
````

- [ ] **Step 2: Commit**

```bash
git add deploy/cloudflare/README.md
git commit -m "docs(cloudflare): add README with setup instructions"
```

---

### Task 6: Validate and deploy

This task requires the Cloudflare API token, account ID, and zone ID. Ask the user for these values.

- [ ] **Step 1: Create terraform.tfvars**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare
cat > terraform.tfvars <<'EOF'
cloudflare_api_token  = "<YOUR_TOKEN>"
cloudflare_account_id = "<YOUR_ACCOUNT_ID>"
cloudflare_zone_id    = "<YOUR_ZONE_ID>"
EOF
```

- [ ] **Step 2: Initialize OpenTofu**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare && tofu init
```

Expected: downloads Cloudflare provider, prints "OpenTofu has been successfully initialized!"

- [ ] **Step 3: Validate configuration**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare && tofu validate
```

Expected: "Success! The configuration is valid."

- [ ] **Step 4: Commit lock file**

```bash
git add deploy/cloudflare/.terraform.lock.hcl
git commit -m "chore(cloudflare): add dependency lock file"
```

- [ ] **Step 5: Import existing docs resources (if they exist)**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare
tofu import cloudflare_r2_bucket.docs_cache <account_id>/opentela-docs-opennext-cache
tofu import cloudflare_dns_record.docs <zone_id>/<record_id>
tofu import cloudflare_workers_custom_domain.docs <account_id>/docs.opentela.ai
```

Skip any import that fails with "not found" — those resources will be created fresh. Bootstraps resources are new and don't need import.

- [ ] **Step 6: Plan**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare && tofu plan
```

Review the plan output. Expected changes:
- **Create:** 2x `cloudflare_dns_record.bootstraps` (A records), 1x `cloudflare_ruleset.origin_port`, 1x `cloudflare_ruleset.ssl_flexible_bootstraps`
- **No change** or **Create:** docs resources (depending on imports)

- [ ] **Step 7: Apply**

```bash
cd /home/xiayao/Documents/projects/opentela/OpenTela/deploy/cloudflare && tofu apply
```

Type `yes` when prompted.

- [ ] **Step 8: Verify bootstraps endpoint**

```bash
curl -s https://bootstraps.opentela.ai/v1/dnt/bootstraps
```

Expected: JSON response with `{"bootstraps": [...]}` (same as `curl http://140.238.223.116:8092/v1/dnt/bootstraps`).

- [ ] **Step 9: Verify docs site**

```bash
curl -sI https://docs.opentela.ai
```

Expected: HTTP 200 response.
