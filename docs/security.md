# Security model

- Provider/service tokens are sensitive configuration only and never enter state.
- Organization service tokens are exchanged at `/token`; only the short-lived JWT
  is sent to `/v1`.
- Enrollment/bootstrap material, cloud credentials, secret values, and application
  build credentials are neither modeled nor logged.
- Application resources accept secret IDs only.
- All mutations use an idempotency key. Only classified transient responses are
  retried.
- API request and correlation IDs are retained in diagnostics and operation state;
  credentials are redacted.
