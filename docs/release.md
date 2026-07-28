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
6 and the source address is `vappcloud/vappcloud`. The protocol declaration is
also shipped as `terraform-registry-manifest.json`.

The mirror publisher downloads both releases and refuses completion unless their
asset names and every asset byte match. It then verifies the detached GPG
signature over the checksum file and validates all archive checksums in both
repositories. A failed comparison or signature check leaves the release train
failed rather than silently accepting a divergent mirror.
