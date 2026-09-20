# Using Willys CLI

Use normal output by default. Add `--json` only for automated parsing.
Run `willys --help` or `willys COMMAND --help` for syntax.
Keep the same profile throughout an order. Omitting `--profile` uses `default`.

## Shopping workflow

1. Inspect the existing basket with `willys cart` before changing it.
2. Search several products with `willys search "penne, Pepsi Max, ketchup" --limit 3`.
3. Verify each candidate with `willys product CODE`. Check brand, pack size, price, and requested quantity.
4. Use `willys set CODE QUANTITY` to set the final quantity. It does not increment the quantity.
5. Use `willys setup`, `willys slots`, and `willys slot NUMBER` to configure fulfillment when needed.
6. Review `willys cart`, including delivery fees, discounts, deposits, and the payment reservation.
7. Start checkout only within the user's purchase instructions. Ask which payment method they want if unspecified.

Use Swedish catalog terms when English searches give poor results.
Use `--details` only when images, ingredients, nutrition, or full product data are needed.
Different-product updates can run concurrently. Conflicting commands wait automatically.
Use `willys remove CODE` for one product or `willys cart reset` to empty the basket.

## Payment and errors

- For scripted checkout, supply current `--expected-total` and `--expected-reservation` with `--yes`.
- Never retry an uncertain payment. Run `willys payment status` first.
- `willys payment` reopens a saved link. It does not submit another order.
- Use `payment recover` only when status reports an unsubmitted attempt.
- Never delete cookies or payment files to bypass payment protection.
- Leave payment approval and BankID to the user. A payment link does not prove purchase completion.
- Enable `--experimental-klarna` only when the user explicitly requests experimental Klarna testing.
- Cancel prompts with Ctrl+C or `/cancel`. Inspect reported partial changes before retrying.
- Batch search can return useful results and still exit nonzero. Check each failed term.
