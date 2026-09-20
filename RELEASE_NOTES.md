Browser cart review and checkout validation fixes.

- Run `willys cart --open` for a live browser review of the selected profile.
- View product images, quantities, ingredients, prices, fulfillment details, and totals.
- The minimal layout places pickup details and the order summary above a product grid.
- Continue to an existing payment portal without submitting another order.
- Keep the CLI running for refresh. Ctrl+C stops the local server; it closes after one hour.
- Pickup setup now saves and verifies the customer address.
- Checkout rejects missing address fields, invalid contact details, and unknown fulfillment modes.
- Payment errors show the server rejection and no longer recommend a missing payment link.

Live delivery and pickup setup, slot selection, checkout cancellation, and cart reset passed.
The card payment handoff returned a payment URL. Payment completion remains unverified.
Klarna remains behind the experimental flag.
