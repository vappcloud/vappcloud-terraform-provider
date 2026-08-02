---
page_title: "Authentication - VAppCloud"
subcategory: "Guides"
description: |-
  Configure STS authentication without storing credentials in state.
---

# Authentication

In **Settings → Identity & Access → Service Accounts**, create a service
account and an access key. The secret is shown once. Store the credentials in
your automation platform's secret store and expose them to the provider as:

```shell
VAPPCLOUD_ACCESS_KEY_ID=...
VAPPCLOUD_SECRET_ACCESS_KEY=...
```

The provider exchanges the pair for a short-lived STS session token in memory.
Neither the secret nor the session token is written to Terraform state. To
assume a role, set `VAPPCLOUD_ROLE_ARN`; use `VAPPCLOUD_SESSION_NAME` to identify
the run in audit records. A legacy short-lived bearer token may be supplied as
`VAPPCLOUD_TOKEN`, but token and access-key authentication are mutually
exclusive.

IAM policy attachments are the authorization source of truth. Policies may be
attached directly or through groups, roles, and instance profiles. Evaluation is
deny-first: an explicit deny overrides every allow. Revoking one access key
leaves the service account's other keys active; disabling the service account
denies every key and prevents new STS sessions.

Service accounts are automation principals. Their roles may authorize VMM
provisioning, but the VMM SSH, exec, and managed-tunnel APIs always require an
active human principal. Terraform credentials therefore cannot be reused to
open an interactive or remote-exec shell.

VAppCloud resources accept references such as `secret_ids`, not secret values.
Never place an access-key secret in provider configuration, resource arguments,
outputs, or variable defaults because those values may enter plan or state.
