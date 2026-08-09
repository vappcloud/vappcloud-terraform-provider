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
      version = "~> 2.0"
    }
  }
}

provider "vappcloud" {}
```

Use one short-lived role credential source:

- Generate temporary credentials in the Access Portal and set
  `VAPPCLOUD_ACCESS_KEY_ID`, `VAPPCLOUD_SECRET_ACCESS_KEY`, and
  `VAPPCLOUD_SESSION_TOKEN` together.
- Set `VAPPCLOUD_CREDENTIAL_PROCESS` to a command such as
  `vappctl access credential-process --account-id <account-id> --role-arn <role-arn>`.
- For CI, set `VAPPCLOUD_WEB_IDENTITY_TOKEN_FILE` and `VAPPCLOUD_ROLE_ARN`.

The provider re-reads web identity tokens when refreshing and signs every API
request with SigV4. Credentials are never written to Terraform state. Set
`VAPPCLOUD_SESSION_NAME` for web-identity audit records and optionally set
`VAPPCLOUD_API_URL` to override `https://api.4lock.net`.

IAM policy evaluation is deny-first. Human and federated identities can assume
only roles allowed by both their identity policies and each role's trust
policy; an explicit deny always overrides an allow. Federated STS sessions are
deliberately non-interactive and cannot create VMM SSH or exec sessions.
Register a human SSH key and use `vappctl vmm ssh` or `vappctl vmm exec` through
the recently authenticated human channel for audited operator access.
Transport behavior can be tuned with provider arguments for retries, request
timeouts, rate limiting, proxies, custom CAs, TLS verification, and
service-specific endpoint overrides.

See `docs/` and `examples/` for generated resource documentation and complete
configurations.

## Development

Run `task verify`, then both acceptance suites. `task registry:dry-run` builds
the exact snapshot package used by the Registry release workflow. See
`CONTRIBUTING.md` for the complete gate.

Credentialed development acceptance is opt-in and only manages the explicitly
configured QA project and device:

```text
VAPPCLOUD_API_URL
VAPPCLOUD_WEB_IDENTITY_TOKEN_FILE
VAPPCLOUD_ROLE_ARN
VAPPCLOUD_REAL_PROJECT_ID
VAPPCLOUD_REAL_DEVICE_ID
```

Run `task test:acceptance:live:terraform` or
`task test:acceptance:live:tofu`. The live VMM case creates a uniquely named
secondary VMM, repairs controlled drift, verifies import, and deletes only the
resource recorded by the test.
