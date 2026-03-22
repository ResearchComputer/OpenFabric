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
