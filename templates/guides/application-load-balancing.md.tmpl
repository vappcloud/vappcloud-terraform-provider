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

## Distributed service ingress

Application placement accepts zero replicas on any number of VMMs when at
least one other placement has a positive replica count. A zero-count placement
is a service-ingress member: it runs no application container and reserves no
workload capacity. The provider preserves `0` explicitly in the API payload.

One placement is a same-VMM deployment and must have at least one replica.
For distributed placement, every `replica_count` must be non-negative, VMM IDs
must be unique, and the sum must be greater than zero.

    placement = [
      { vmm_id = "vmm-ingress-a", replica_count = 0 },
      { vmm_id = "vmm-ingress-b", replica_count = 0 },
      { vmm_id = "vmm-workload",  replica_count = 2 },
    ]
