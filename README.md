# Willys CLI

An unofficial CLI for shopping at Willys Sweden through its internal HTTP API.
The CLI stores sessions automatically. Shopping commands do not use a browser.
Payment opens in your normal browser.

This is an alpha release. Willys can change its internal API without notice.
This project has no affiliation with Willys, Axfood, Klarna, or Swedbank Pay.

## Install

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/aluxf/willys-cli/main/install.sh | sh
```

The installer downloads a standalone binary and verifies its SHA-256 checksum.
You do not need Python, Go, or a package manager.
It installs into `~/.local/bin`. Follow the printed PATH instruction if needed.
Set `WILLYS_INSTALL_DIR` to choose another directory, or `WILLYS_VERSION=v0.1.0` to select a release.
Run the installer again to update.

Windows users can download a ZIP from [Releases](https://github.com/aluxf/willys-cli/releases).
Extract `willys.exe` into a directory on PATH.
Releases support macOS, Linux, and Windows on AMD64 and ARM64.

## Shop

```sh
willys search "penne"
willys product 101240218_ST
willys set 101240218_ST 10
willys cart
willys cart --details
willys remove 101240218_ST
```

`set` sets the final quantity. It does not add that quantity to the previous amount.
Use `--unit kilogram` for supported weight-based products. Weight-based updates remain unverified live.
Search returns Willys' ranking. Use `--page` and `--limit` to browse more results.
The CLI does not automatically match a shopping description to a product.

Cart output includes brand, pack size, prices, discounts, product links, and the payment reservation.
`--details` includes images, ingredients, nutrition, and full product data.
Missing API fields remain empty. They do not imply zero or unavailable stock.

## Stores and delivery

```sh
willys stores
willys stores Stockholm --pickup
willys stores --details
willys setup
willys slots
willys slot 1
```

`stores` fetches the full store list. The optional query filters names, towns, and postcodes locally.
It does not calculate the nearest store. `--all` includes unnamed API records.
The inspected endpoint returned 257 records: 256 named stores, including 182 with pickup support.
These counts are a snapshot from 20 September 2026.

`setup` prompts for contact details, fulfillment method, and address or pickup store.
It remembers your inputs for the next setup. You do not need to create JSON files.
Use flags such as `--first-name`, `--last-name`, `--phone`, `--email`, `--mode`, `--street`, `--postcode`, and `--town` for scripts.
For pickup, use `--mode pickup --store STORE_ID`.

`slots --choose` combines listing and interactive selection.
`slot NUMBER` uses the last list and rejects lists older than ten minutes.
The server remains the authority for availability and reservation expiry.

## Payment

```sh
willys checkout
willys payment
```

`checkout` asks which available payment method to use.
It shows the cart total and the card reservation, including Willys' buffer.
It asks before starting a real payment.
A saved payment attempt prevents accidental repeat submission within the profile.
`payment` reopens the saved URL. It does not start another payment.
`payment --url` prints that URL instead.

For scripts, use `checkout --method card --yes --expected-total "323,25 kr" --expected-reservation "339,78 kr"`.
The values must match the current cart. Use `--no-open` to receive the URL without opening a browser.
Do not reuse these example totals for another cart.

### Card

The earlier live probe created a hosted Swedbank Pay URL through Willys' order endpoint.
A fresh HTTP client loaded that page without Willys cookies.
The Go CLI tests verify the same request contract with a local HTTP server.
The shopper enters payment details on the provider page. The CLI does not collect card details.

### Klarna: experimental

Klarna is selectable when Willys offers it for the cart.
Willys uses Klarna's browser SDK, not a simple payment-mode toggle.
The CLI opens a temporary local page in your default browser.
That page requests authorization through Klarna's SDK and returns the token to the CLI.
The CLI checks that the cart has not changed before it submits the token to Willys.

This path has not passed a live Klarna authorization test.
Klarna can reject the local origin or require a different merchant integration.
Do not treat selectable Klarna as production-verified support.
The local authorization page expires after ten minutes.
See the [Klarna SDK reference](https://docs.klarna.com/acquirer/klarna/web-payments/additional-resources/klarna-payments-sdk-reference/) for the authorization contract.
The CLI does not automate credit decisions, payment approval, or BankID.

### Current limits

Payment completion, order cancellation, and account login are not implemented.
Pickup setup and Klarna authorization remain unverified live.
The CLI retains failed or uncertain payment attempts to prevent duplicate orders.
There is no automatic reset of those attempts. Resolve the provider/order status before retrying.
Do not operate on the same shopper through multiple profiles.

## Sessions and scripting

```sh
willys --profile groceries cart
willys --json cart
willys session
willys --profile imported session --import-cookies /path/to/cookies.txt
willys --help
willys checkout --help
```

Each profile has its own cookies, setup information, slot cache, and payment response.
The default profile works without any session arguments.
`session` shows the storage directory. Import is only permitted into a profile without cookies.

Storage uses `~/Library/Application Support/willys` on macOS.
Linux uses `$XDG_DATA_HOME/willys` or `~/.local/share/willys`.
Windows uses `%LOCALAPPDATA%/willys`.
`WILLYS_DATA_DIR` overrides the storage root.
Files use owner-only permissions where the operating system supports them.
The CLI automatically imports existing Netscape cookies from a profile's `cookies.txt` file.
It retains saved setup information and payment responses.

## Development

```sh
go test -race ./...
go vet ./...
go build -o willys ./cmd/willys
sh scripts/test-install.sh
scripts/release.sh 0.1.0
```

Tests use synthetic data. They do not place orders or request live credit authorization.
