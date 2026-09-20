Sign in with BankID, browse online deals, and review your cart in the browser.

- Run `willys auth` to inspect account and cart setup status.
- Run `willys auth login` to sign in with BankID. Sessions persist in the selected profile.
- Saved contact and address fields prefill setup. Fulfillment and slot selection remain separate.
- Normal product search shows available offers, including conditions, membership requirements, expiry, and product links.
- Run `willys deals` to browse online offers. Deal search terms are no longer accepted.
- Deals fetch only the requested page. `--page` starts at zero; `--limit` defaults to 20.
- Use `--store ID` for a temporary guest preview. Your cart and login remain unchanged.
- Use `--details` for product images and full promotion data. Targeted personal offers remain excluded.
- Run `willys cart --open` for product images, quantities, ingredients, fulfillment details, and payment totals.
- The browser review can restart card payment after verified provider cancellation. Unknown payment outcomes remain protected.
- Pickup setup saves and verifies the customer address. Checkout validates address, contact, and fulfillment details.
- Help includes the new authentication and deal commands.

Live BankID login, session reuse, member pricing, and multibuy pricing passed local tests.
The test cart charged 12,20 kr for one tortilla pack and 20,00 kr for two. Test products were removed.
Delivery and pickup setup, slot selection, cart reset, and a card payment handoff passed earlier live tests.
Payment completion remains unverified. Klarna remains behind the experimental flag.

This prerelease includes the cart review and payment recovery changes from the canceled beta.4 and beta.5 releases.
