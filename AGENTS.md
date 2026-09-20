# Project guidelines

- Keep the CLI standalone in Go. Users must not need a language runtime.
- Put command syntax and usage guidance in the single global help page. Keep README examples consistent.
- Use normal text by default. Keep JSON results on stdout and errors or progress on stderr.
- Preserve profile compatibility, product locks, and payment protections. Never retry uncertain order submissions.
- Test changes with synthetic data or isolated profiles. Do not use real shopper profiles or submit live payments without authorization.
- Run relevant tests and `go vet ./...`. For release changes, also run race tests, govulncheck, and installer checks.
- Keep releases gated by CI. Report unverified payment or fulfillment behavior explicitly.
- Write short, clear English. Keep changes focused.
