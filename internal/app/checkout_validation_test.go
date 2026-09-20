package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCheckoutRejectsIncompleteFulfillmentBeforeWrites(t *testing.T) {
	for _, field := range []string{"deliveryModeCode", "deliveryAddress", "line1", "town", "postalCode", "firstName", "lastName", "email", "cellphone"} {
		for _, mode := range []string{"homeDelivery", "pickUpInStore"} {
			t.Run(mode+"/"+field, func(t *testing.T) {
				p := profile(t)
				cart := sampleCart()
				cart["deliveryModeCode"] = mode
				if field == "deliveryModeCode" || field == "deliveryAddress" {
					delete(cart, field)
				} else {
					delete(obj(cart["deliveryAddress"]), field)
				}
				c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/cart" || r.Method != http.MethodGet {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					}
					writeJSON(w, cart)
				})
				if _, err := setupApp().Checkout(context.Background(), c, p, checkoutOptions()); err == nil || !strings.Contains(err.Error(), "setup") {
					t.Fatalf("expected setup guidance, got %v", err)
				}
				saved, err := p.Load("payment")
				if err != nil || saved != nil {
					t.Fatalf("validation created payment attempt: %v %v", saved, err)
				}
			})
		}
	}
}

func TestCartValidationRejectsMalformedFields(t *testing.T) {
	for _, field := range []string{"email", "cellphone", "postalCode"} {
		cart := sampleCart()
		obj(cart["deliveryAddress"])[field] = "invalid"
		if err := validateCart(cart); err == nil {
			t.Fatalf("accepted invalid %s", field)
		}
	}
	cart := sampleCart()
	cart["deliveryModeCode"] = "unknown"
	if err := validateCart(cart); err == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestCheckoutReportsServerRejectionWithoutSuggestingMissingLink(t *testing.T) {
	p := profile(t)
	fixture := &checkoutFixture{p: p, t: t}
	const reason = "Cart is missing delivery address for user (uid='anonymous')!"
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/singlestepcheckout/placeOrder" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, Object{"errorMessage": reason})
			return
		}
		fixture.handler(w, r)
	})
	a := setupApp()
	if _, err := a.Checkout(context.Background(), c, p, checkoutOptions()); err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("missing rejection: %v", err)
	}
	result, err := a.PaymentStatus(context.Background(), c, p, false)
	if err != nil {
		t.Fatal(err)
	}
	status := obj(result)
	if status["serverMessage"] != reason || !strings.Contains(text(status["nextAction"]), "No payment link") {
		t.Fatal(status)
	}
	if _, err := a.PaymentStatus(context.Background(), c, p, true); err == nil {
		t.Fatal("cleared a submitted attempt without verification")
	}
}
