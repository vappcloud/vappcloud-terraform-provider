---
page_title: "Authentication - VAppCloud"
subcategory: "Guides"
description: |-
  Configure short-lived, SigV4-signed role credentials without storing them in state.
---

# Authentication

VAppCloud provider v2 accepts only temporary role credentials. Generate a
credential set from the **Access Portal → Command line or programmatic access**
screen and expose all three one-time values to the provider:

```shell
VAPPCLOUD_ACCESS_KEY_ID=...
VAPPCLOUD_SECRET_ACCESS_KEY=...
VAPPCLOUD_SESSION_TOKEN=...
```

For interactive automation, let `vappctl` obtain and renew those credentials:

```hcl
provider "vappcloud" {
  credential_process = "vappctl access credential-process --account-id acc_example --role-arn arn:vapp:iam::123:role/AccountEditor"
}
```

For GitHub Actions or another configured OIDC provider, configure
`web_identity_token_file` and `role_arn`. The provider re-reads the token file
whenever it refreshes the role session. These three modes are mutually
exclusive.

Every post-exchange API request is signed using `AWS4-HMAC-SHA256` with region
`global` and service `vappcloud`. The session token is never sent as a Bearer
token. Temporary credentials stay in memory and are never written to Terraform
state.

IAM policy attachments are the authorization source of truth. A human or
federated identity may assume a role only when its identity policy allows the
exact role ARN and the role trust policy accepts that identity. Evaluation is
deny-first: an explicit deny overrides every allow. Revoking a session, changing
the trust or permission policy, or disabling the source principal invalidates
the temporary credentials.

Federated automation sessions may provision VMMs, but the VMM SSH, exec, and
managed-tunnel APIs always require a recently authenticated human principal.
Terraform credentials therefore cannot be reused to open an interactive or
remote-exec shell.

VAppCloud resources accept references such as `secret_ids`, not secret values.
Never place temporary credentials in resource arguments, outputs, variable
defaults, logs, or checked-in configuration. Prefer environment variables,
`credential_process`, or a mode-`0600` web identity token file.
