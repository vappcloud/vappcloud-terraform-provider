# Security policy

Do not open public issues for vulnerabilities. Report them privately through
GitHub Security Advisories in this repository. Include affected versions,
impact, reproduction steps, and any proposed mitigation.

Supported releases are the latest stable major release and the immediately
preceding major release for 90 days after a new major release.

Tokens, exchanged JWTs, custom CA material, and API responses containing
credentials must never be persisted in Terraform state or diagnostic output.

