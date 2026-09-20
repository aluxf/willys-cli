Parallel commands now share a profile safely.

- Search, product, store, and cart reads can run together.
- Different product updates can run together.
- Updates to the same product wait for each other.
- Checkout, setup, and slot commands hold an exclusive profile lock.
- Cookie saves merge changes without replacing another process's session data.
- Waiting commands support cancellation with Ctrl+C.

Close older CLI processes before using this version with the same profile.
Klarna authorization remains experimental. Payment completion remains outside the CLI.
