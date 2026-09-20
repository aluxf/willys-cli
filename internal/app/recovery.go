package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Payment recovery never treats a timeout or a missing URL as proof of failure.
func (a *App) PaymentStatus(ctx context.Context, c *Client, p *Profile, recover bool) (any, error) {
	saved, err := p.Load("payment")
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return Object{"state": "no saved payment attempt"}, nil
	}
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot verify the cart; payment attempt remains protected: %w", err)
	}
	out := Object{"state": "unresolved", "attemptState": saved["state"], "cart": saved["cart"], "currentCart": cart["code"], "orderReference": cart["orderReference"], "paymentVerified": false, "nextAction": "Open willys payment to inspect the provider. Do not repeat checkout while the outcome is unknown."}
	if saved["state"] == "not_submitted" {
		out["state"] = "not submitted"
		out["nextAction"] = "Run willys payment recover to archive this unsubmitted attempt."
	}
	if !recover {
		return out, nil
	}
	if saved["state"] != "not_submitted" || text(saved["cart"]) == "" || saved["cart"] != cart["code"] || text(cart["orderReference"]) != "" {
		return out, failure("payment_unresolved", "The payment outcome is not verified. Recovery did not clear the attempt. Check the provider or Willys before retrying.", 1, nil)
	}
	archive := fmt.Sprintf("payment-archive-%d", time.Now().UnixNano())
	if err = p.Save(archive, saved); err != nil {
		return nil, err
	}
	if err = os.Remove(filepath.Join(p.Path, "payment.json")); err != nil {
		return nil, err
	}
	return Object{"state": "recovered", "archive": archive, "message": "The request was never submitted. You can retry checkout."}, nil
}
