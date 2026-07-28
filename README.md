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

Set `VAPPCLOUD_TOKEN` to an organization service token and optionally
`VAPPCLOUD_API_URL` to override `https://api.4lock.net`. Service tokens are
exchanged for short-lived API JWTs and are never written to Terraform state.
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
