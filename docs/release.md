# Release policy

The canonical repository builds all release artifacts. The Registry discovery
repository receives the exact same immutable assets; it never builds provider
code.

Release candidates use `v1.0.0-rc.N` tags and remain private until API canary,
rollback rehearsal, Terraform/OpenTofu acceptance, signature verification, SBOM,
provenance, and documentation gates pass. The first public Registry release is
`v1.0.0`; no public `0.x` release is published.

Every release contains signed SHA-256 checksums, Linux/macOS/Windows archives for
amd64 and arm64, SPDX SBOMs, and GitHub build provenance. The provider protocol is
6 and the source address is `vappcloud/vappcloud`.
