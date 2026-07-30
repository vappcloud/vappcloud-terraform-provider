# Contributing

Use a feature branch and keep changes focused. Install Go 1.25, Terraform and
OpenTofu, then run:

```shell
task verify
task test:acceptance:terraform
task test:acceptance:tofu
task registry:dry-run
```

Add a `.changelog/<issue>.<type>.md` fragment for user-visible changes. New
resources require unit tests, create/update/import/drift/empty-plan acceptance
coverage, a real destroy check, generated documentation, and an example.

Never commit credentials. Acceptance tests default to an in-process mock; real
API tests require explicitly configured repository secrets.

