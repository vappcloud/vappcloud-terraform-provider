---
page_title: "Networking and TLS - VAppCloud"
subcategory: "Guides"
description: |-
  Configure endpoints, proxies, custom certificate authorities, and rate limits.
---

# Networking and TLS

`api_url` selects the API edge. `endpoint_overrides` can route an individual
service to a staged endpoint. `proxy_url`, `ca_certificate`,
`insecure_skip_verify`, `request_timeout`, `retry_max_wait`, `max_retries`, and
`rate_limit_per_second` control transport behavior. Keep TLS verification
enabled outside isolated development environments.

