# VAppCloud Terraform/OpenTofu Provider

Production provider for managing VAppCloud projects, devices, compute instances,
secondary VMMs, and application instances.

The provider address is `vappcloud/vappcloud`. It supports Terraform 1.5+ and
OpenTofu 1.6+ using protocol 6.

Terraform/OpenTofu 1.12+ additionally persist first-class resource identity
metadata. Legacy string imports remain supported on every supported engine.

```hcl
terraform {
  required_providers {
    vappcloud = {
      source  = "vappcloud/vappcloud"
      version = "~> 1.0"
    }
  }
}

provider "vappcloud" {}
```

Create a service account and access key, then set the one-time credentials as
`VAPPCLOUD_ACCESS_KEY_ID` and `VAPPCLOUD_SECRET_ACCESS_KEY`. The provider
exchanges them for a short-lived STS session token in memory; neither the access
key secret nor the session token is written to Terraform state. Set
`VAPPCLOUD_ROLE_ARN` to assume a role and optionally set
`VAPPCLOUD_SESSION_NAME` for audit records. `VAPPCLOUD_TOKEN` remains available
for legacy short-lived bearer tokens, but it cannot be combined with an access
key pair. Optionally set `VAPPCLOUD_API_URL` to override
`https://api.4lock.net`.

IAM policy evaluation is deny-first. The effective permissions come from the
service account's direct, group, role, and instance-profile attachments; an
explicit deny always overrides an allow.
Service-account authorization is deliberately non-interactive: even a
service account with an editor or administrator role cannot create VMM SSH or
exec sessions. Register a human SSH key and use `vappctl vmm ssh` or
`vappctl vmm exec` for audited operator access.
Transport behavior can be tuned with provider arguments for retries, request
timeouts, rate limiting, proxies, custom CAs, TLS verification, and
service-specific endpoint overrides.

See `docs/` and `examples/` for generated resource documentation and complete
configurations.

## Development

Run `task verify`, then both acceptance suites. `task registry:dry-run` builds
the exact snapshot package used by the Registry release workflow. See
`CONTRIBUTING.md` for the complete gate.
Terraform and OpenTofu provider for VAppCloud
