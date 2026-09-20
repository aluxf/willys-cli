# Membership and online deals

Status: login, account inspection, and general online deal search are implemented locally. Registration and checkout membership display remain planned.
Release target: v0.2.0-beta.6. Publication requires the configured CI checks.

## Verified findings

- Online deals use GET /axfood/rest/v2/search/campaigns/online.
- The website supplies q=STORE_ID, type=PERSONAL_GENERAL, page, and size.
- An anonymous request for Bromma returned 246 results during inspection.
- Promotions include prices, limits, qualifying quantities, expiry times, and campaign types such as LOYALTY.
- q=2351:pasta returned the same total and first results as q=2351. Free-text filtering is not verified.
- The website uses PERSONAL_SEGMENTED for targeted offers after sign-in.
- GET /axfood/rest/v2/customer returns customer identity. Anonymous responses use uid=anonymous.
- The frontend exposes BankID auth, QR, collect-login, and cancel endpoints.
- The frontend exposes membership registration and completion endpoints.
- The frontend distinguishes merging carts, retaining the account cart, and retaining the session cart.

Official references:

- https://www.willys.se/artikel/kundservice/villkor-for-willys-plus
- https://www.willys.se/artikel/ehandelsguide

Willys describes account identification with BankID, personal identity number, or membership number.
Membership registration requires contact information and acceptance of membership terms.
CLI BankID login now passes live testing. Registration remains untested.

## Proposed user flow

1. Run willys auth login once for the selected profile.
2. Approve identification with BankID on the user's device.
3. Verify the resulting customer identity and membership eligibility through Willys.
4. Run willys deals to browse online offers for the selected store.
5. Search or filter deals, choose products, and use set directly.
6. Recheck eligibility and applied prices before starting checkout.

Proposed commands:

    willys auth login
    willys auth
    willys auth join
    willys auth logout
    willys deals
    willys deals "pasta, cheese"
    willys deals --store 2351

No identifiers, passwords, or tokens should be required as command-line flags.
Normal text remains the default. JSON remains available for programmatic use.

## Implementation order

### 1. Account and membership inspection

Add membership status with separate anonymous, signed-in, verified-member, and unknown states.
Identify the actual membership fields from a consenting member's response before implementing eligibility rules.
Do not equate a non-anonymous session with eligibility for every offer.
Do not infer membership from contact information, saved card details, or the existence of an advertised offer.

### 2. Existing-member login

Trace the website's BankID request bodies and cookie transition.
Use the CLI's own cookie jar for authentication and polling.
Show the rotating QR code in a small local page if needed.
The user completes BankID identification. The CLI verifies the resulting account before reporting success.
Handle cancellation, timeout, expired authentication, and missing membership explicitly.
Do not persist passwords, identity numbers, QR secrets, or BankID responses in logs.
Keep session files private and redact identifiers in normal output.

Login must hold the profile lock and refuse unresolved payment attempts.
Snapshot the current cart before authentication.
Verify products, quantities, fulfillment, and totals after authentication.
If the account has another cart, ask which cart to retain or merge. Never merge silently.
Browser login alone cannot authenticate the CLI because the cookie jars are separate.

### 3. Joining Willys Plus

Initially, membership join opens the official registration flow in the normal browser.
The user enters identity data and accepts membership terms there.
After registration, instruct the user to run willys auth login to authenticate the CLI session.
Do not create real memberships during automated tests.
Direct API registration is a later option after its consent and verification steps are understood.

### 4. Online deals

Default to the selected fulfillment store. Allow an explicit store override without changing the cart.
Return product codes, names, pack sizes, regular prices, offer prices, comparison units, and images with details.
Include membership requirements, minimum quantities, maximum redemptions, mix-and-match groups, and expiry.
Keep advertised prices separate from verified cart prices.
Read every page before applying local text filters. Do not silently filter only the first page.
Use a short cache keyed by store, account identity, and fulfillment context. Invalidate it after login or logout.
If the backend's documented filter syntax supports search, use it after a verified test.
Do not describe PERSONAL_GENERAL as personalized or guaranteed eligible.

### 5. Checkout eligibility

Check identity and membership before the payment attempt starts.
Re-fetch the cart after login or offer activation because prices and the cart can change.
Use Willys' applied cart promotions and totals as the final price evidence.
Do not calculate the payable total by subtracting advertised savings locally.
Check quantity conditions and offer validity for the fulfillment date when the API supports that determination.
If future-date eligibility cannot be verified, show that limitation and the current cart price.
If a selected member offer is unavailable, show the actual price and require renewed total confirmation.
Preserve existing payment protections. Never change an in-progress payment's amount or identity.
Add membership status and verified savings to the browser review without adding unnecessary controls.

## Tests and acceptance criteria

- Synthetic tests: anonymous, member, nonmember, unknown, expired session, login cancellation, timeout, and QR refresh.
- Synthetic tests: existing account cart conflicts, profile isolation, identity change, and unresolved payment blocks.
- Synthetic tests: loyalty prices, multibuy quantities, redemption limits, targeted offers, expiry, pagination, and local search.
- Synthetic tests: changed totals require confirmation; advertised savings never replace server totals.
- Live anonymous test: list online deals and verify ordinary prices in a disposable cart.
- Live member test: the user approves BankID, then compare the same qualifying product before and after login.
- Verify the member discount in the server cart and browser review. Stop before payment submission.
- Restart the CLI and confirm session reuse. Log out and verify that member eligibility disappears.
- Use an existing consenting member for live tests. Registration remains a user-operated flow.
- Run tests, race checks, vet, vulnerability checks, and installer checks before a separately authorized release.

## Remaining uncertainties

Existing account cart conflicts and future-slot offer pricing still need live verification.
Logout, registration, targeted offers, and checkout membership display remain unimplemented.

## Local login test results

- BankID login authenticated an existing Willys Plus account.
- A new CLI process reused the saved session.
- Willys returned membership creation fields and a saved shipping address.
- Contact and address fields now prefill setup without overwriting existing local values.
- Login did not attach an address to the empty cart or select a slot.
- Product 101412786_ST cost 29,90 kr anonymously and 14,90 kr after login.
- The authenticated cart reported a 15,00 kr discount.
- Both test carts were empty after cleanup. No payment was submitted.
- Race tests, vet, and vulnerability checks passed.
- Cart merging remains disabled. Login snapshots the current cart and reports changes.

## Online deal verification and implementation

- The general online endpoint returns 246 products for store 2351 and 219 for store 2583.
- Category and campaign-type query facets work. The tested free-text suffix does not filter results.
- The CLI filters all result pages locally before applying each term's page and limit.
- Query store IDs select campaigns, but session store context also affects returned prices.
- Store overrides therefore use a temporary guest session with the requested store activated and verified.
- The guest session is deleted after the command. The shopper's cart and account are unchanged.
- The tested authenticated account returns no targeted personal offers. Targeted search remains outside this implementation.
- General offers expose member requirements, multibuy conditions, limits, expiry, and promotion group codes.
- The CLI does not infer personal eligibility or subtract advertised savings from cart totals.
- The initial implementation reads fresh data rather than caching store and account context.

Local verification passed: complete pagination, text filtering, store isolation, missing-page failures, images, race tests, and vet.
A live member cart charged 12,20 kr for one tortilla pack and 20,00 kr for two.
The test cart was empty after cleanup. No payment was submitted.
