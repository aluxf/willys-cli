# Willys CLI

An unofficial CLI for shopping at Willys Sweden through its internal HTTP API.
The CLI stores sessions automatically. Shopping commands do not use a browser.
Payment opens in your normal browser.

Run `willys` or `willys --help` for the complete command guide and shopping workflow.
All command-level `--help` flags show the same global guide.

This is a beta release. Willys can change its internal API without notice.
This project has no affiliation with Willys, Axfood, Klarna, or Swedbank Pay.

## Install

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/aluxf/willys-cli/main/install.sh | sh
```

The installer downloads a standalone binary and verifies its SHA-256 checksum.
You do not need Python, Go, or a package manager.
It installs into `~/.local/bin`. Follow the printed PATH instruction if needed.
The installer defaults to the reviewed beta, `v0.2.0-beta.6`.
Set `WILLYS_INSTALL_DIR` to choose another directory, or `WILLYS_VERSION` to select a release.
Run the installer again to update.

Windows users can download a ZIP from [Releases](https://github.com/aluxf/willys-cli/releases).
Extract `willys.exe` into a directory on PATH.
Releases support macOS, Linux, and Windows on AMD64 and ARM64.

## Shop

```sh
willys search "penne"
willys search "penne, Pepsi Max, Heinz ketchup, ramen" --limit 3
willys set 101240218_ST 10
willys cart
willys cart --details
willys remove 101240218_ST
willys cart reset
```

Search returns brand, pack size, price, and product code. Verify the match, then use `set` directly.
`willys product CODE` is optional. Use it only when more information is needed.

`set` sets the final quantity. It does not add that quantity to the previous amount.
Use `--unit kilogram` for supported weight-based products. Weight-based updates remain unverified live.
Search returns Willys' ranking. Use `--page` and `--limit` to browse more results.
Separate search terms with commas to search up to four terms concurrently.
The CLI queues additional terms and groups results in the input order.
`--page` and `--limit` apply to each term. Failed terms show an error beside successful groups.
A single search retains its existing output format.
The CLI does not automatically match a shopping description to a product.

Cart output includes brand, pack size, prices, discounts, product links, and the payment reservation.
`--details` includes images, ingredients, nutrition, and full product data.
Missing API fields remain empty. They do not imply zero or unavailable stock.

`cart reset` empties the cart with one bulk-clear request and verifies the result.
It keeps the session and contact defaults, and invalidates cached slot choices.
It does not remove products individually or cancel an existing order.
The CLI refuses reset while a payment attempt remains unresolved.

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

`setup` prompts for contact details, fulfillment method, and customer address. Pickup also requires a store and customer address.
It remembers your inputs for the next setup. You do not need to create JSON files.
Press Ctrl+C or type `/cancel` to cancel a prompt.
Invalid interactive fields prompt for correction. Invalid flags fail without prompting.
Setup verifies the returned customer address and contact fields before reporting success. Pickup also verifies the active store.
A failure during saving identifies the affected step. Earlier server changes can remain saved.
Use flags such as `--first-name`, `--last-name`, `--phone`, `--email`, `--mode`, `--street`, `--postcode`, and `--town` for scripts.
For pickup, use `--mode pickup --store STORE_ID`.

`slots --choose` combines listing and interactive selection.
`slot NUMBER` uses the last list and rejects lists older than ten minutes.
The server remains the authority for availability and reservation expiry.

## Payment

```sh
willys checkout
willys payment
willys payment status
willys payment recover
```

`checkout` asks which available payment method to use.
It shows the cart total and the card reservation, including Willys' buffer.
It asks before starting a real payment.
A saved payment attempt prevents accidental repeat submission within the profile.
`payment` reopens the saved URL. It does not start another payment.
`payment --url` prints that URL instead. Saved payment URLs work without an API connection.
`payment status` shows the saved attempt and current cart without claiming payment completion.
`payment recover` archives a request only when the CLI proves it never reached the order endpoint.
It also checks that the current cart matches the attempt and has no order reference.
Sent, timed-out, and legacy attempts remain protected until their outcomes can be verified.
A missing URL, empty cart, or HTTP error does not prove that an order failed.

For scripts, use `checkout --method card --yes --expected-total "323,25 kr" --expected-reservation "339,78 kr"`.
The values must match the current cart. Use `--no-open` to receive the URL without opening a browser.
Do not reuse these example totals for another cart.

### Card

The earlier live probe created a hosted Swedbank Pay URL through Willys' order endpoint.
A fresh HTTP client loaded that page without Willys cookies.
The Go CLI tests verify the same request contract with a local HTTP server.
The shopper enters payment details on the provider page. The CLI does not collect card details.

### Klarna: experimental

Klarna is disabled by default during beta testing.
Use `checkout --experimental-klarna` to expose it when Willys offers it for the cart.
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

Payment completion and order cancellation remain unverified. BankID account login is under local testing.
Pickup setup and Klarna authorization remain unverified live.
The CLI retains failed or uncertain payment attempts to prevent duplicate orders.
Unsubmitted attempts have a recovery command. Other attempts require provider or Willys verification.
The CLI cannot automatically reconcile completed, cancelled, or expired hosted payment sessions.
Do not delete payment state files or reset cookies to bypass an uncertain outcome.
Do not operate on the same shopper through multiple profiles.

## Sessions and scripting

```sh
willys --profile groceries cart
willys --json cart
willys session
willys --profile imported session --import-cookies /path/to/cookies.txt
willys --help
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
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
scripts/release.sh 0.2.0-beta.1
```

Tests use synthetic data. They do not place orders or request live credit authorization.

## Parallel commands

Terminals share the same cart when they use the same profile and storage directory.
Search, product, store, and cart reads can run together.
Quantity changes for different product codes can also run together.
Changes to the same product wait for each other.
Checkout, setup, and slot commands hold an exclusive profile lock.
Conflicting commands wait until the lock is available. Press Ctrl+C to cancel a waiting command.
A cart read during updates can show an intermediate cart state.

```sh
willys search penne &
willys search havregryn &
wait
```

The CLI initializes a new session once before concurrent requests start.
Cookie saves merge individual changes under a short file lock.
Locks coordinate this CLI on one computer. They cannot coordinate browsers or other computers.
Close older CLI processes before using this version with the same profile.

## Errors and release checks

Normal output uses stdout. Prompts, progress, and errors use stderr.
With `--json`, command errors use a JSON object containing `error.code`, `error.message`, and `exitCode`.
Batch search preserves successful results on stdout when another term fails.
Exit status is 0 for success, 1 for failure, 2 for parsing errors, 3 for partial search failure, and 130 for cancellation.

Release publication waits for Linux, macOS, and Windows checks on the tagged commit.
These checks include race tests, vet, vulnerability scanning, and executable builds.
Unix runners also check the curl installer. Current releases publish as prereleases.

### Browser cart review

Run `willys cart --open` to open a minimal browser review. Use the same `--profile` as your cart.
The page shows product images, quantities, prices, ingredients, pickup or delivery details, and payment totals.
Refresh reads current data through the CLI session. The browser does not receive Willys cookies.
A saved payment link enables **Continue to payment**. A verified cancellation enables **Start new payment**. The review can start a new card payment only after the provider confirms that the previous payment was canceled.
If no link exists, run `willys checkout --no-open` in another terminal, then refresh the page.
Keep the review command running. Ctrl+C stops its local server, which also closes after one hour.

Payment recovery also archives card attempts that Swedbank Pay explicitly confirms as canceled. Unknown outcomes remain protected.

### BankID login

Run `willys auth login` and scan the QR code with BankID. Approve the Willys identification request.
The CLI verifies the account and stores session cookies in the selected profile.
Run `willys auth` to inspect account status, saved address availability, and cart setup.
Login does not place an order, select a slot, or silently merge carts.
The CLI saves a cart snapshot before login and reports changes after authentication.
An account address does not prove that the cart has an address. Run setup when required.
Unresolved payments block login. Use a separate profile for testing.
Live BankID login, session reuse, and an applied member discount passed local testing.
Saved account contact and address fields prefill setup. They do not automatically select fulfillment or a slot.

### Online deals

```sh
willys deals
willys deals "pasta, ost" --limit 5
willys deals "garant tortilla" --store 2351
willys deals "pasta" --page 1 --limit 5
willys deals "dill" --details
```

Deals use the profile's active store. `--store` selects a temporary guest preview without changing your profile.
The CLI fetches all online offer pages before filtering names, brands, sizes, and product codes.
Separate searches with commas. Words within each search must all match, without regard to case.
`--limit` applies to each search. `--page` starts at zero. Results include match counts and remaining-page indicators.
Results show regular prices, conditional offer prices, quantity conditions, membership requirements, limits, expiry, and product links.
`--details` includes the full offer product data, including images and promotion group codes.
Use `willys product CODE --details` when you need nutrition or ingredients that the offer endpoint omits.
Use `willys set CODE QUANTITY` directly after choosing a product.
The cart determines applied prices. A multibuy offer price applies only when its conditions are met.
These are general online offers, including Willys Plus offers. Targeted personal offers are not included.
Deal search reads fresh data on each invocation. A failed offer page stops the search without reporting incomplete matches.
