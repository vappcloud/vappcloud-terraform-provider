# Security model

- Provider/service tokens are sensitive configuration only and never enter state.
- Organization service tokens are exchanged at `/token`; only the short-lived JWT
  is sent to `/v1`.
- Enrollment/bootstrap material, cloud credentials, secret values, and application
  build credentials are neither modeled nor logged.
- Application resources accept secret IDs only.
- All mutations use an idempotency key. Only classified transient responses are
  retried.
- Remote API URLs must use HTTPS. Plain HTTP is accepted only for `localhost` and
  loopback addresses used by local development and acceptance tests.
- Trace logs include method, path, attempt, status, request ID, and operation state.
  Request bodies, authorization headers, service tokens, JWTs, and idempotency
  keys are never logged; reflected credentials are redacted from diagnostics.
