package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func setupOptions(mode string) Options {
	return Options{Values: map[string]string{
		"first-name": " Test ", "last-name": " Shopper ", "phone": "+46700000000",
		"email": "shopper@example.com", "mode": mode,
		"street": " Example   1 ", "postcode": "111 11", "town": " Stockholm ",
	}, Bools: map[string]bool{}}
}

func setupApp() *App {
	return NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
}

func TestSetupDeliveryVerifiesNormalizedAddress(t *testing.T) {
	p := profile(t)
	cart := Object{}
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "csrf")
		case "/cart":
			writeJSON(w, cart)
		case "/cart/customer-contact-info", "/cart/postal-code":
			writeJSON(w, Object{})
		case "/cart/delivery-mode/homeDelivery":
			cart["deliveryModeCode"] = "homeDelivery"
			writeJSON(w, Object{})
		case "/cart/delivery-address":
			cart["deliveryAddress"] = Object{"line1": "Example 1", "postalCode": "11111", "town": "STOCKHOLM"}
			writeJSON(w, Object{})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	})
	if _, err := setupApp().Setup(context.Background(), c, p, setupOptions("delivery")); err != nil {
		t.Fatal(err)
	}
	saved, err := p.Load("address")
	if err != nil || text(saved["addressLine1"]) != "Example 1" || text(saved["postalCode"]) != "11111" || text(saved["town"]) != "Stockholm" {
		t.Fatal(saved, err)
	}
}

func TestSetupDeliveryRejectsDifferentReadback(t *testing.T) {
	p := profile(t)
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "csrf")
		case "/cart":
			writeJSON(w, Object{"deliveryModeCode": "homeDelivery", "deliveryAddress": Object{"line1": "Other 2", "postalCode": "11111", "town": "Stockholm"}})
		default:
			writeJSON(w, Object{})
		}
	})
	if _, err := setupApp().Setup(context.Background(), c, p, setupOptions("delivery")); err == nil || !strings.Contains(err.Error(), "differs from the requested address") {
		t.Fatal(err)
	}
}

func TestSetupPickupVerifiesActiveStore(t *testing.T) {
	for _, test := range []struct {
		name, active string
		wantError    bool
	}{
		{"success", "store-one", false},
		{"mismatch", "store-two", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := profile(t)
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/csrf-token":
					writeJSON(w, "csrf")
				case "/store":
					writeJSON(w, []any{Object{"storeId": "store-one", "name": "Store one", "clickAndCollect": true}})
				case "/cart":
					writeJSON(w, Object{"deliveryModeCode": "pickUpInStore"})
				case "/store/activate", "/cart/customer-contact-info", "/cart/delivery-mode/pickUpInStore":
					writeJSON(w, Object{})
				case "/store/active":
					writeJSON(w, Object{"storeId": test.active})
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
			})
			o := setupOptions("pickup")
			o.Values["store"] = "store-one"
			_, err := setupApp().Setup(context.Background(), c, p, o)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "different store") {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSetupReportsPartialDeliveryFailure(t *testing.T) {
	p := profile(t)
	var posts []string
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "csrf")
		case "/cart":
			writeJSON(w, Object{})
		case "/cart/customer-contact-info", "/cart/postal-code":
			posts = append(posts, r.URL.Path)
			writeJSON(w, Object{})
		case "/cart/delivery-mode/homeDelivery":
			posts = append(posts, r.URL.Path)
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, Object{"error": "temporary failure"})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	})
	_, err := setupApp().Setup(context.Background(), c, p, setupOptions("delivery"))
	if err == nil || !strings.Contains(err.Error(), "contact details and postcode are saved") {
		t.Fatal(err)
	}
	if strings.Join(posts, ",") != "/cart/customer-contact-info,/cart/postal-code,/cart/delivery-mode/homeDelivery" {
		t.Fatal(posts)
	}
}

func TestSetupRejectsInvalidFlagBeforeWrites(t *testing.T) {
	p := profile(t)
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Fatal("setup wrote invalid input")
		}
		if r.URL.Path != "/cart" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		writeJSON(w, Object{})
	})
	o := setupOptions("delivery")
	o.Values["phone"] = "not-a-phone"
	if _, err := setupApp().Setup(context.Background(), c, p, o); err == nil {
		t.Fatal("accepted invalid phone")
	}
}

func TestSetupFieldRepromptsInvalidInteractiveValue(t *testing.T) {
	var stderr bytes.Buffer
	a := NewApp(strings.NewReader("not-a-phone\n+46700000000\n"), io.Discard, &stderr, true)
	value, err := a.setupField(Options{Values: map[string]string{}, Bools: map[string]bool{}}, "phone", "Mobile number", nil, normalizePhone)
	if err != nil || value != "0700000000" {
		t.Fatal(value, err)
	}
	if !strings.Contains(stderr.String(), "Enter a valid value") {
		t.Fatal(stderr.String())
	}
}
