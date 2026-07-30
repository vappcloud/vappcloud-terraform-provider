# VAppCloud Terraform Provider

The canonical provider source for registry address `vappcloud/vappcloud`.

- Go 1.25+, Terraform Plugin Framework, protocol 6.
- Terraform 1.5+ and OpenTofu 1.6+.
- Feature branches only; default branch is `main`.
- Never log or persist provider tokens, enrollment/bootstrap material, cloud credentials, or secret values.
- Mutations must carry stable idempotency keys. Retry only classified transient failures.
- Default VMMs are read-only data; `vappcloud_vmm` manages secondary VMMs only.

Verify with `task verify`. Acceptance tests are `task test:acceptance:terraform`
and `task test:acceptance:tofu`; they use isolated test fixtures unless explicitly
configured for a real VAppCloud organization.
