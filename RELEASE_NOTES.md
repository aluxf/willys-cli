Search several products with one comma-separated query:

```sh
willys search "penne, Pepsi Max, Heinz ketchup, ramen" --limit 3
```

The CLI runs up to four searches concurrently and groups results in input order.
The limit applies to each term. Failed searches do not discard successful results.
Single-term search output remains unchanged.

This release includes product-level concurrency locks from v0.1.1.
Klarna authorization remains experimental.
