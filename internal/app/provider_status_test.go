package app

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type providerTransport func(*http.Request) (*http.Response, error)

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func providerFixture(state, cart string) string {
	b, _ := json.Marshal(Object{"status": "Success", "action": Object{"actionType": "OnPaymentCanceled", "state": state, "id": "provider-order", "redirectUrl": BaseURL + "/singlestepcheckout/cancelPayment/3070000001"}, "body": Object{"id": "provider-order"}})
	return `<div data-paymentdata-message="` + html.EscapeString(string(b)) + `"></div>`
}
func setProvider(c *Client, state, cart string) {
	c.ProviderHTTP = &http.Client{Transport: providerTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(providerFixture(state, cart))), Header: http.Header{}}, nil
	})}
}
func TestCanceledProviderDataRequiresExactProof(t *testing.T) {
	for _, state := range []string{"Aborted", "Paid", "Pending", "Failed", ""} {
		got := canceledProviderData(strings.NewReader(providerFixture(state, "cart-test")), "cart-test")
		if got != (state == "Aborted") {
			t.Fatal(state, got)
		}
	}
	if canceledProviderData(strings.NewReader(strings.ReplaceAll(providerFixture("Aborted", "cart-test"), "www.willys.se", "evil.example")), "cart-test") {
		t.Fatal("accepted foreign cancellation callback")
	}
	if canceledProviderData(strings.NewReader(`<p>Payment canceled</p>`), "cart-test") {
		t.Fatal("accepted display text")
	}
}
func TestCanceledPaymentRecovery(t *testing.T) {
	for _, state := range []string{"Aborted", "Paid", "Pending"} {
		t.Run(state, func(t *testing.T) {
			p := profile(t)
			_ = p.Save("payment", Object{"state": "response", "cart": "cart-test", "location": "https://ecom.payex.com/checkout/" + strings.Repeat("a", 64)})
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, sampleCart()) })
			setProvider(c, state, "cart-test")
			_, err := setupApp().PaymentStatus(context.Background(), c, p, true)
			_, stat := os.Stat(filepath.Join(p.Path, "payment.json"))
			if state == "Aborted" {
				if err != nil || !os.IsNotExist(stat) {
					t.Fatal(err, stat)
				}
			} else if err == nil || stat != nil {
				t.Fatal("cleared non-canceled payment", err, stat)
			}
		})
	}
}
func TestRestartCanceledPaymentSubmitsOnlyOnce(t *testing.T) {
	p := profile(t)
	_ = p.Save("payment", Object{"state": "response", "method": "card", "cart": "cart-test", "location": "https://ecom.payex.com/checkout/" + strings.Repeat("a", 64)})
	f := &checkoutFixture{p: p, t: t}
	c, _ := clientFor(t, p, f.handler)
	setProvider(c, "Aborted", "cart-test")
	a := setupApp()
	a.Connect = func(*Profile) (*Client, error) { return c, nil }
	body := Object{"total": "100,00 kr", "reservation": "110,00 kr"}
	if _, err := a.restartCanceledPayment(context.Background(), p, Object{"total": "wrong", "reservation": "110,00 kr"}); err == nil {
		t.Fatal("accepted stale total")
	}
	if _, err := a.restartCanceledPayment(context.Background(), p, body); err != nil {
		t.Fatal(err)
	}
	if _, err := a.restartCanceledPayment(context.Background(), p, body); err == nil {
		t.Fatal("submitted duplicate")
	}
	if place, _ := f.calls(); place != 1 {
		t.Fatal(place)
	}
}
func TestRestartEndpointRequiresOrigin(t *testing.T) {
	calls := 0
	h := cartPageHandler("127.0.0.1:1234", "/secret", nil, func(context.Context, Object) (Object, error) { calls++; return Object{}, nil })
	for _, origin := range []string{"", "https://evil.example", "http://127.0.0.1:1234"} {
		r, _ := http.NewRequest("POST", "http://127.0.0.1:1234/secret/payment", strings.NewReader(`{}`))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if origin == "http://127.0.0.1:1234" && w.Code != 200 {
			t.Fatal(w.Code)
		}
		if origin != "http://127.0.0.1:1234" && w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
