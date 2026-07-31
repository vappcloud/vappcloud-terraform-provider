---
page_title: "Authentication - VAppCloud"
subcategory: "Guides"
description: |-
  Configure service-token authentication without storing credentials in state.
---

# Authentication

In **Settings → Identity & Access → Service Accounts**, create a service account
with the organization-scoped `vapp-editor` role and a named API key. Set the
one-time key as `VAPPCLOUD_TOKEN`. The provider exchanges it for short-lived
JWTs in memory and never stores either credential in state. Prefer environment
variables or your automation platform's secret store over inline provider
configuration.

Role bindings are the authorization source of truth. An organization-scoped
`vapp-editor` can create projects and manage platform resources in every current
and future project. A project-scoped role cannot create projects and can access
only the selected projects. Revoking one named key leaves the service account's
other keys active; disabling the service account denies every key.

Service accounts are automation principals. Their roles may authorize VMM
provisioning, but the VMM SSH, exec, and managed-tunnel APIs always require an
active human principal. Terraform credentials therefore cannot be reused to
open an interactive or remote-exec shell.

VAppCloud resources accept references such as `secret_ids`, not secret values.
Any future value-carrying secret input must use Terraform's write-only attribute
support so it cannot enter plan or state. Short-lived credentials will be
exposed through an ephemeral resource rather than a persisted data source.
