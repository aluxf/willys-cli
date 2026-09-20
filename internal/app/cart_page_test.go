package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCartPageAccessBoundaries(t *testing.T) {
	calls := 0
	h := cartPageHandler("127.0.0.1:1234", "/secret", func(context.Context) (Object, error) {
		calls++
		return Object{"products": []any{}, "total": "100 kr"}, nil
	})
	for _, test := range []struct {
		name, path, host, method, origin string
		want                             int
	}{
		{"page", "/secret/", "127.0.0.1:1234", "GET", "", 200},
		{"data", "/secret/data", "127.0.0.1:1234", "GET", "", 200},
		{"wrong token", "/wrong/data", "127.0.0.1:1234", "GET", "", 404},
		{"wrong host", "/secret/data", "evil.example", "GET", "", 403},
		{"foreign origin", "/secret/data", "127.0.0.1:1234", "GET", "https://evil.example", 403},
		{"write", "/secret/data", "127.0.0.1:1234", "POST", "", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "http://"+test.host+test.path, nil)
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response can be cached")
			}
		})
	}
	if calls != 1 {
		t.Fatalf("unauthorized requests fetched cart: %d", calls)
	}
}

func TestCartPageUsesDetailsAndMatchingPayment(t *testing.T) {
	for _, matching := range []bool{true, false} {
		p := profile(t)
		cartCode := "other-cart"
		if matching {
			cartCode = "cart-test"
		}
		if err := p.Save("payment", Object{"cart": cartCode, "location": "https://ecom.payex.com/checkout/synthetic"}); err != nil {
			t.Fatal(err)
		}
		c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/cart":
				cart := sampleCart()
				cart["deliveryModeCode"] = "pickUpInStore"
				writeJSON(w, cart)
			case "/p/123_ST":
				writeJSON(w, Object{"image": Object{"url": "https://assets.axfood.se/test.jpg"}, "ingredients": "Pasta"})
			case "/store/active":
				writeJSON(w, Object{"name": "Test store", "address": Object{"line1": "Test street"}})
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
				http.NotFound(w, r)
			}
		})
		a := setupApp()
		a.Connect = func(*Profile) (*Client, error) { return c, nil }
		data, err := a.cartPageData(context.Background(), p, "test")
		if err != nil {
			t.Fatal(err)
		}
		if (text(data["paymentURL"]) != "") != matching {
			t.Fatal("wrong cart payment link", data)
		}
		if obj(data["store"])["name"] != "Test store" || obj(list(data["products"])[0])["image"] == nil {
			t.Fatal("missing details", data)
		}
	}
}

func TestPaymentResponseMessageWithSavedLink(t *testing.T) {
	message := paymentResponseMessage(Object{"location": "https://ecom.payex.com/checkout/synthetic"})
	if strings.Contains(message, "no payment link") {
		t.Fatal(message)
	}
}
