# VAppCloud Terraform/OpenTofu Provider

Production provider for managing VAppCloud projects, devices, compute instances,
secondary VMMs, and application instances.

The provider address is `vappcloud/vappcloud`. It supports Terraform 1.5+ and
OpenTofu 1.6+ using protocol 6.

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

See `docs/` and `examples/` for generated resource documentation and complete
configurations.
Terraform and OpenTofu provider for VAppCloud
