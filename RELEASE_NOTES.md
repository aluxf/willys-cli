Beta readiness fixes:

- Ctrl+C and /cancel exit interactive prompts and release locks.
- Setup prompts again for invalid fields and verifies the saved destination.
- Errors support JSON output. Partial search failures return a nonzero exit status.
- Cart reset uses one bulk-clear request and verifies the empty cart. Contact defaults remain saved.
- Payment status explains unresolved attempts. Recovery supports requests that never reached the order endpoint.
- Saved payment URLs open without an API connection.
- Klarna requires --experimental-klarna.
- Go 1.26.6 resolves the reported standard-library vulnerability findings.
- Publication requires all platform, race, vulnerability, and installer checks.

Sent or uncertain payment attempts remain protected. Payment completion and provider reconciliation remain unverified.
Pickup and Klarna lack live end-to-end verification. This release is for controlled beta testing.
