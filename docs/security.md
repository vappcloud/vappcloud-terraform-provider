# Security model

- Temporary role credentials are sensitive configuration only and never enter state.
- Every API request is signed with SigV4. The temporary session token is a signed
  header and is never accepted directly as Bearer authentication.
- Enrollment/bootstrap material, cloud credentials, secret values, and application
  build credentials are neither modeled nor logged.
- Application resources accept secret IDs only.
- All mutations use an idempotency key. Only classified transient responses are
  retried.
- Remote API URLs must use HTTPS. Plain HTTP is accepted only for `localhost` and
  loopback addresses used by local development and acceptance tests.
- Trace logs include method, path, attempt, status, request ID, and operation state.
  Request bodies, authorization headers, temporary secrets, JWTs, and idempotency
  keys are never logged; reflected credentials are redacted from diagnostics.
