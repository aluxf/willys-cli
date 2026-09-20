package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ResetCart clears every product with Willys' cart-level operation.
func (a *App) ResetCart(ctx context.Context, c *Client, p *Profile) (any, error) {
	if err := paymentPending(p); err != nil {
		return nil, err
	}
	before, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if text(before["orderReference"]) != "" {
		return nil, errors.New("editing existing orders is not supported")
	}
	if text(before["code"]) == "" {
		return nil, errors.New("cannot identify the cart; reset was not started")
	}
	if err = p.Save("slots", Object{}); err != nil {
		return nil, err
	}
	if _, err = c.request(ctx, http.MethodDelete, "/cart", url.Values{"cancelPossibleContinueCart": {"false"}}, nil, false, false); err != nil {
		return nil, fmt.Errorf("cart reset outcome is unknown; run willys cart before retrying: %w", err)
	}
	after, err := c.Cart(ctx)
	if err != nil {
		return nil, fmt.Errorf("reset was requested but could not be verified; run willys cart: %w", err)
	}
	_, hasProducts := after["products"]
	if text(after["code"]) == "" || !hasProducts {
		return nil, errors.New("Willys returned an incomplete cart; reset could not be verified")
	}
	if len(list(after["products"])) != 0 {
		return nil, errors.New("Willys did not clear the cart; review it before trying again")
	}
	return Object{"cart": after["code"], "products": 0}, nil
}
