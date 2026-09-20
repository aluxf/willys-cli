package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginVerifiesAccountAndPreservesCart(t *testing.T) {
	for _, authenticated := range []bool{true, false} {
		p := profile(t)
		approved := false
		posts := 0
		c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				posts++
			}
			switch r.URL.Path {
			case "/customer":
				if approved && authenticated {
					writeJSON(w, Object{"uid": "test-account", "firstName": "Test", "memberCreationMonthAndYear": "09/2026"})
				} else {
					writeJSON(w, Object{"uid": "anonymous"})
				}
			case "/cart":
				writeJSON(w, sampleCart())
			default:
				t.Error(r.URL.Path)
				http.NotFound(w, r)
			}
		})
		a := setupApp()
		a.BankID = func(context.Context, *Client) error { approved = true; return nil }
		result, err := a.Login(context.Background(), c, p)
		if authenticated {
			if err != nil || obj(result)["state"] != "signed in" || obj(result)["cartChanged"] != false {
				t.Fatal(result, err)
			}
		} else if err == nil {
			t.Fatal("claimed login without authenticated customer")
		}
		if posts != 0 {
			t.Fatal("login mutated the cart")
		}
		if _, err := os.Stat(filepath.Join(p.Path, "cart-before-login.json")); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLoginBlocksPendingPayment(t *testing.T) {
	p := profile(t)
	_ = p.Save("payment", Object{"state": "starting"})
	a := setupApp()
	a.BankID = func(context.Context, *Client) error { t.Fatal("started BankID"); return nil }
	if _, err := a.Login(context.Background(), nil, p); err == nil {
		t.Fatal("ignored pending payment")
	}
}
func TestBankIDPollLifecycle(t *testing.T) {
	for _, final := range []string{"COMPLETE", "FAILED", "CUSTOMERNOTFOUND", "UNEXPECTED"} {
		t.Run(final, func(t *testing.T) {
			p := profile(t)
			collects := 0
			qrCount := 0
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/csrf-token":
					writeJSON(w, "csrf")
				case "/checkout/bankid/collect-login":
					var body Object
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["orderRef"] != "test-order" || body["rememberMe"] != "false" {
						t.Error("wrong collect body")
					}
					collects++
					if collects == 1 {
						writeJSON(w, Object{"status": "PENDING", "hintCode": "outstandingTransaction"})
					} else {
						writeJSON(w, Object{"status": final, "ssn": "sensitive-value"})
					}
				case "/checkout/bankid/qr":
					qrCount++
					writeJSON(w, Object{"qrString": "synthetic-bankid-qr"})
				default:
					t.Error("unexpected", r.URL.Path)
				}
			})
			view := &loginView{State: "pending"}
			err := pollBankID(context.Background(), c, "test-order", view, time.Millisecond)
			if (err == nil) != (final == "COMPLETE") {
				t.Fatal(err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-value") {
				t.Fatal("identity leaked")
			}
			if collects != 2 || qrCount < 1 || len(view.PNG) == 0 {
				t.Fatal(collects, qrCount)
			}
		})
	}
}
func TestBankIDCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := profile(t)
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) { t.Error("request after cancellation") })
	if err := pollBankID(ctx, c, "reference", &loginView{}, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestBankIDPageBoundaries(t *testing.T) {
	view := &loginView{State: "pending", Message: "Scan the QR code", PNG: []byte("png")}
	cancels := 0
	h := bankIDHandler("127.0.0.1:1234", "/secret", view, func() { cancels++ })
	for _, test := range []struct {
		path, method, origin string
		status               int
	}{
		{"/secret/status", "GET", "", 200}, {"/secret/qr", "GET", "", 200}, {"/wrong/status", "GET", "", 404},
		{"/secret/cancel", "POST", "", 403}, {"/secret/cancel", "POST", "https://evil.example", 403}, {"/secret/cancel", "POST", "http://127.0.0.1:1234", 204},
	} {
		r := httptest.NewRequest(test.method, "http://127.0.0.1:1234"+test.path, nil)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatal(test, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache allowed")
		}
	}
	if cancels != 1 {
		t.Fatal(cancels)
	}
}
func TestAnonymousMembershipDoesNotClaimEligibility(t *testing.T) {
	out := membershipView(Object{"uid": "anonymous", "memberCreationDateFull": "date"}, sampleCart())
	if out["membership"] != "sign in required" {
		t.Fatal(out)
	}
	out = membershipView(Object{"uid": "test-user"}, sampleCart())
	if out["membership"] != "not verified" {
		t.Fatal(out)
	}
}

func TestAccountDefaultsPreserveUserValuesAndExcludeIdentity(t *testing.T) {
	p := profile(t)
	if err := p.Save("contact", Object{"email": "chosen@example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Save("address", Object{"town": "Chosen town"}); err != nil {
		t.Fatal(err)
	}
	customer := Object{"uid": "account", "firstName": "Test", "lastName": "User", "email": "account@example.test", "ssn": "private-identity", "defaultShippingAddress": Object{"line1": "Test street 1", "postalCode": "12345", "town": "Account town", "phone": "0701234567", "ssn": "private-identity"}}
	if err := cacheAccountDefaults(p, customer); err != nil {
		t.Fatal(err)
	}
	contact, _ := p.Load("contact")
	address, _ := p.Load("address")
	if contact["email"] != "chosen@example.test" || contact["firstName"] != "Test" || contact["cellphone"] != "0701234567" {
		t.Fatal(contact)
	}
	if address["town"] != "Chosen town" || address["addressLine1"] != "Test street 1" || address["postalCode"] != "12345" {
		t.Fatal(address)
	}
	for _, name := range []string{"contact", "address"} {
		data, err := os.ReadFile(filepath.Join(p.Path, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private-identity") {
			t.Fatal("saved identity number")
		}
	}
}

func TestBankIDStopsQRRefreshDuringApproval(t *testing.T) {
	p := profile(t)
	collects, qrCount := 0, 0
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "csrf")
		case "/checkout/bankid/collect-login":
			collects++
			if collects == 1 {
				writeJSON(w, Object{"status": "PENDING", "hintCode": "userSign"})
			} else {
				writeJSON(w, Object{"status": "COMPLETE"})
			}
		case "/checkout/bankid/qr":
			qrCount++
			http.Error(w, "QR expired", 410)
		default:
			t.Error("unexpected", r.URL.Path)
		}
	})
	view := &loginView{State: "pending", PNG: []byte("old QR")}
	if err := pollBankID(context.Background(), c, "reference", view, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if collects != 2 || qrCount != 0 || len(view.PNG) != 0 {
		t.Fatal(collects, qrCount, len(view.PNG))
	}
}

func TestAuthCommands(t *testing.T) {
	for _, args := range [][]string{{"auth"}, {"auth", "login"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p := profile(t)
			approved := false
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/customer":
					uid := "anonymous"
					if approved {
						uid = "test-account"
					}
					writeJSON(w, Object{"uid": uid})
				case "/cart":
					writeJSON(w, sampleCart())
				default:
					t.Error(r.URL.Path)
					http.NotFound(w, r)
				}
			})
			a := setupApp()
			a.Connect = func(*Profile) (*Client, error) { return c, nil }
			a.BankID = func(context.Context, *Client) error { approved = true; return nil }
			options, err := Parse(args)
			if err != nil {
				t.Fatal(err)
			}
			result, err := a.Run(context.Background(), p, options)
			if err != nil {
				t.Fatal(err)
			}
			want := "anonymous"
			if len(args) == 2 {
				want = "signed in"
			}
			if obj(result)["state"] != want {
				t.Fatal(result)
			}
		})
	}
	for _, args := range [][]string{{"auth", "wrong"}, {"auth", "login", "extra"}} {
		a := setupApp()
		a.Connect = func(*Profile) (*Client, error) { t.Fatal("connected for invalid auth command"); return nil, nil }
		options, err := Parse(args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Run(context.Background(), profile(t), options); err == nil {
			t.Fatal("accepted invalid auth arguments")
		}
	}
}

func TestAuthLoginExcludesOtherCommands(t *testing.T) {
	p := profile(t)
	release, err := commandLock(context.Background(), p, Options{Command: "auth", Positionals: []string{"login"}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	other, err := commandLock(ctx, p, Options{Command: "auth"})
	if err == nil {
		other()
		t.Fatal("auth read overlapped login")
	}
}
