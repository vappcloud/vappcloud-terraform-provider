# Changelog

All notable user-facing changes are documented here. This project follows
[Semantic Versioning](https://semver.org/) and maintains unreleased fragments in
`.changelog/`.

## [Unreleased]

### Breaking

- Provider v2 removes `token`, `VAPPCLOUD_TOKEN`, static service-account keys,
  and service-token exchange. Configure a complete temporary credential triple,
  `credential_process`, or `web_identity_token_file` with `role_arn`.

### Added

- First-class Terraform/OpenTofu resources for projects, devices, compute
  instances, VMMs, and application instances.
- Resource identity support for Terraform and OpenTofu 1.12+.
- Configurable retries, timeouts, rate limiting, proxies, custom CAs, TLS
  verification, endpoint overrides, and complete user-agent attribution.

### Changed

- Every API request is SigV4-signed with short-lived assumed-role credentials;
  web identity token files are re-read before refresh and no STS session token
  is accepted as Bearer authentication.
- Create idempotency keys are unique per resource invocation while HTTP retries
  reuse the same key.
- RFC3339 timestamps use framework semantic time types.
