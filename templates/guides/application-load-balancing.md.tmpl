---
page_title: "Application Load Balancing - VAppCloud"
subcategory: "Guides"
description: |-
  Select the immutable Maglev traffic policy for application instances.
---

# Application load balancing

`vappcloud_application_instance.load_balancing_policy` accepts exactly
`round_robin` or `connection_persistence`. The provider defaults the plan to
`round_robin` client-side and always sends that value explicitly during
creation. Changing it requires replacement.

`round_robin` is the product name for Maglev five-tuple flow-hash
distribution, not cyclic request rotation. `connection_persistence` uses
source-IP affinity through the same Maglev table, excluding the client source
port. It is not cookie-based HTTP session persistence.

    resource "vappcloud_application_instance" "web" {
      # ...
      load_balancing_policy = "connection_persistence"
    }
