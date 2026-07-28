# Changelog

All notable user-facing changes are documented here. This project follows
[Semantic Versioning](https://semver.org/) and maintains unreleased fragments in
`.changelog/`.

## [Unreleased]

### Added

- First-class Terraform/OpenTofu resources for projects, devices, compute
  instances, VMMs, and application instances.
- Resource identity support for Terraform and OpenTofu 1.12+.
- Configurable retries, timeouts, rate limiting, proxies, custom CAs, TLS
  verification, endpoint overrides, and complete user-agent attribution.

### Changed

- Create idempotency keys are unique per resource invocation while HTTP retries
  reuse the same key.
- RFC3339 timestamps use framework semantic time types.

