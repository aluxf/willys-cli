package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func profile(t *testing.T) *Profile {
	t.Helper()
	p, e := NewProfile(t.TempDir(), "default")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func clientFor(t *testing.T, p *Profile, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, e := NewClient(p)
	if e != nil {
		t.Fatal(e)
	}
	c.Base = server.URL
	return c, server
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func sampleCart() Object {
	return Object{"code": "cart-test", "products": []any{Object{"code": "123_ST", "name": "Pasta", "pickQuantity": float64(2), "manufacturer": "Example", "displayVolume": "500g", "price": "10,00 kr", "replacement": true}}, "totalPrice": "100,00 kr", "reservedAmount": "110,00 kr", "bufferedAmount": "10,00 kr", "slotCode": "slot-test", "deliveryModeCode": "homeDelivery", "deliveryAddress": Object{"line1": "Example 1", "postalCode": "11111", "town": "Stockholm", "firstName": "Test", "lastName": "Shopper", "email": "shopper@example.com", "cellphone": "0700000000"}}
}
func checkoutOptions() Options {
	return Options{Values: map[string]string{"method": "card", "expected-total": "100,00 kr", "expected-reservation": "110,00 kr"}, Bools: map[string]bool{"yes": true, "no-open": true}}
}

func TestParseInterspersedFlags(t *testing.T) {
	o, e := Parse([]string{"--json", "--profile", "test", "search", "Pepsi Max", "--limit", "5"})
	if e != nil || o.Command != "search" || o.Profile != "test" || !o.JSON || o.Values["limit"] != "5" || o.Positionals[0] != "Pepsi Max" {
		t.Fatalf("%+v %v", o, e)
	}
	for _, args := range [][]string{{"cart", "--bogus"}, {"--profile"}, {"search", "a", "--details"}, {"cart", "--details=true"}} {
		if _, e := Parse(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestProfilePersistenceAndLock(t *testing.T) {
	p := profile(t)
	if e := p.Save("contact", Object{"name": "Example"}); e != nil {
		t.Fatal(e)
	}
	v, e := p.Load("contact")
	if e != nil || v["name"] != "Example" {
		t.Fatal(v, e)
	}
	if _, e := NewProfile(t.TempDir(), "../bad"); e == nil {
		t.Fatal("accepted path traversal")
	}
	a := flock.New(filepath.Join(p.Path, "profile.lock"))
	b := flock.New(filepath.Join(p.Path, "profile.lock"))
	defer a.Close()
	defer b.Close()
	ok, e := a.TryLock()
	if !ok || e != nil {
		t.Fatal(ok, e)
	}
	ok, e = b.TryLock()
	if ok || e != nil {
		t.Fatal("second lock acquired", ok, e)
	}
}
func TestMalformedProfileNotOverwritten(t *testing.T) {
	p := profile(t)
	_ = os.WriteFile(filepath.Join(p.Path, "payment.json"), []byte("broken"), 0600)
	if e := paymentPending(p); e == nil {
		t.Fatal("ignored invalid payment state")
	}
}
func TestDetailsOptIn(t *testing.T) {
	p := Object{"code": "123_ST", "name": "Pasta", "price": "10 kr"}
	d := Object{"ingredients": "Wheat", "image": Object{"url": "https://example.com/image"}, "breadcrumbs": []any{Object{"url": "/produkt/Pasta-123_ST"}}}
	basic := ProductView(p, d, false)
	full := ProductView(p, d, true)
	if basic["url"] != "https://www.willys.se/produkt/Pasta-123_ST" || basic["ingredients"] != nil || basic["image"] != nil || full["ingredients"] != "Wheat" {
		t.Fatal(basic, full)
	}
}

func TestCookiePersistenceAndDeletion(t *testing.T) {
	p := profile(t)
	s, e := NewCookieStore(p)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse("https://www.willys.se/axfood/rest/v2/cart")
	s.SetCookies(u, []*http.Cookie{{Name: "session", Value: "synthetic", Path: "/", Secure: true, HttpOnly: true}, {Name: "short", Value: "v", Path: "/", MaxAge: 60}})
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	again, e := NewCookieStore(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(again.Cookies(u)) != 2 {
		t.Fatal(again.Cookies(u))
	}
	other, _ := url.Parse("https://other.willys.se/")
	if len(again.Cookies(other)) != 0 {
		t.Fatal("host-only cookie leaked")
	}
	plain, _ := url.Parse("http://www.willys.se/")
	for _, c := range again.Cookies(plain) {
		if c.Name == "session" {
			t.Fatal("secure cookie leaked")
		}
	}
	again.SetCookies(u, []*http.Cookie{{Name: "session", Path: "/", MaxAge: -1}})
	_ = again.Save()
	third, e := NewCookieStore(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range third.Cookies(u) {
		if c.Name == "session" {
			t.Fatal("deleted cookie returned")
		}
	}
	for _, r := range third.records {
		r.Cookie.Expires = time.Now().Add(-time.Hour)
		third.SetCookies(u, []*http.Cookie{&r.Cookie})
	}
	_ = third.Save()
	last, _ := NewCookieStore(p)
	if len(last.Cookies(u)) != 0 {
		t.Fatal("expired cookies restored")
	}
}
func TestNetscapeImportPreservesScope(t *testing.T) {
	p := profile(t)
	input := "# Netscape HTTP Cookie File\n#HttpOnly_www.willys.se\tFALSE\t/\tTRUE\t\twillys-cart\tsynthetic\n"
	file := filepath.Join(p.Path, "cookies.txt")
	_ = os.WriteFile(file, []byte(input), 0600)
	s, e := NewCookieStore(p)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse("https://www.willys.se/")
	if len(s.Cookies(u)) != 1 {
		t.Fatal(s.Cookies(u))
	}
	if _, e = os.Stat(filepath.Join(p.Path, "cookies.json")); e != nil {
		t.Fatal("migration not saved", e)
	}
	for _, r := range s.records {
		if !r.Cookie.HttpOnly {
			t.Fatal("lost HttpOnly")
		}
	}
}
func TestCookiePathDefaults(t *testing.T) {
	p := profile(t)
	s, _ := NewCookieStore(p)
	u, _ := url.Parse("https://www.willys.se/one/cart")
	s.SetCookies(u, []*http.Cookie{{Name: "a", Value: "b"}})
	_ = s.Save()
	s, _ = NewCookieStore(p)
	other, _ := url.Parse("https://www.willys.se/two/cart")
	if len(s.Cookies(other)) != 0 {
		t.Fatal("lost cookie path")
	}
	if len(s.Cookies(u)) != 1 {
		t.Fatal("cookie missing")
	}
}

func TestHTTPContractAndFreshProcess(t *testing.T) {
	p := profile(t)
	calls := 0
	c, s := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "synthetic", Path: "/"})
			writeJSON(w, "token")
		case "/cart/addProducts":
			calls++
			if r.Method != "POST" || r.Header.Get("X-CSRF-Token") != "token" {
				t.Error("missing CSRF")
			}
			cookie, e := r.Cookie("sid")
			if e != nil || cookie.Value != "synthetic" {
				t.Error("cookie missing")
			}
			writeJSON(w, Object{"ok": true})
		case "/cart":
			if _, e := r.Cookie("sid"); e != nil {
				t.Error("cookie not resumed")
			}
			writeJSON(w, sampleCart())
		}
	})
	if _, e := c.Post(context.Background(), "/cart/addProducts", nil, Object{"products": []any{}}); e != nil {
		t.Fatal(e)
	}
	c2, e := NewClient(p)
	if e != nil {
		t.Fatal(e)
	}
	c2.Base = s.URL
	if _, e = c2.Cart(context.Background()); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("duplicate write")
	}
}
func TestRawPaymentDoesNotFollowRedirect(t *testing.T) {
	p := profile(t)
	var followed atomic.Int32
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "token")
		case "/singlestepcheckout/placeOrder":
			w.Header().Set("Location", "/next")
			w.WriteHeader(302)
		case "/next":
			followed.Add(1)
		}
	})
	v, e := c.Place(context.Background(), url.Values{"saveCard": {"false"}})
	if e != nil || v.Status != 302 || v.Location != "/next" || followed.Load() != 0 {
		t.Fatal(v, e, followed.Load())
	}
}
func TestHTTPFailureKeepsSession(t *testing.T) {
	p := profile(t)
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "synthetic", Path: "/"})
		w.WriteHeader(503)
		writeJSON(w, Object{"error": "temporarily unavailable"})
	})
	if _, e := c.Get(context.Background(), "/cart", nil); e == nil {
		t.Fatal("accepted 503")
	}
	if _, e := os.Stat(filepath.Join(p.Path, "cookies.json")); e != nil {
		t.Fatal(e)
	}
}

type checkoutFixture struct {
	mu                               sync.Mutex
	cartReads, placeCalls, modeCalls int
	changed, timeout, conflict       bool
	token                            string
	timeoutStarted                   chan struct{}
	timeoutRelease                   chan struct{}
	timeoutDone                      chan struct{}
	p                                *Profile
	t                                *testing.T
}

func (f *checkoutFixture) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/cart":
		f.mu.Lock()
		f.cartReads++
		cartReads := f.cartReads
		changed := f.changed
		f.mu.Unlock()
		cart := sampleCart()
		if changed && cartReads > 1 {
			cart["totalPrice"] = "101,00 kr"
		}
		writeJSON(w, cart)
	case "/cart/status":
		status := Object{"cartStatus": []any{}}
		if f.conflict {
			status["cartStatus"] = []any{Object{"partialOutOfStock": true}}
		}
		writeJSON(w, status)
	case "/checkout/paymentmodes":
		writeJSON(w, []any{"PspPayexAll", "Klarna"})
	case "/csrf-token":
		writeJSON(w, "csrf")
	case "/checkout/paymentmode":
		f.mu.Lock()
		f.modeCalls++
		f.mu.Unlock()
		writeJSON(w, Object{})
	case "/klarna/payment-session":
		writeJSON(w, Object{"client_token": "synthetic"})
	case "/singlestepcheckout/placeOrder":
		f.mu.Lock()
		f.placeCalls++
		timeout := f.timeout
		timeoutStarted := f.timeoutStarted
		timeoutRelease := f.timeoutRelease
		timeoutDone := f.timeoutDone
		f.mu.Unlock()
		if timeoutDone != nil {
			defer close(timeoutDone)
		}
		saved, e := f.p.Load("payment")
		if e != nil || saved["state"] != "starting" {
			f.t.Error("attempt was not saved before submission")
		}
		if timeout {
			close(timeoutStarted)
			<-timeoutRelease
			return
		}
		if e := r.ParseForm(); e != nil {
			f.t.Error(e)
		}
		if r.Form.Get("saveCard") != "false" || r.Form.Get("selectedCard") != "" {
			f.t.Error("saved card selected")
		}
		f.mu.Lock()
		f.token = r.Form.Get("klarnaAuthorizationToken")
		f.mu.Unlock()
		w.Header().Set("Location", "https://ecom.payex.com/checkout/synthetic")
		w.WriteHeader(402)
	default:
		f.t.Error("unexpected endpoint", r.URL.Path)
		w.WriteHeader(404)
	}
}
func (f *checkoutFixture) calls() (place, mode int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.placeCalls, f.modeCalls
}
func (f *checkoutFixture) authorizationToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}
func TestCheckoutPersistsAndBlocksDuplicate(t *testing.T) {
	p := profile(t)
	f := &checkoutFixture{p: p, t: t}
	c, _ := clientFor(t, p, f.handler)
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	result, e := a.Checkout(context.Background(), c, p, checkoutOptions())
	if e != nil || obj(result)["state"] != "payment pending" {
		t.Fatal(result, e)
	}
	if _, e = a.Checkout(context.Background(), c, p, checkoutOptions()); e == nil {
		t.Fatal("duplicate allowed")
	}
	if place, _ := f.calls(); place != 1 {
		t.Fatal(place)
	}
	saved, _ := p.Load("payment")
	if number(saved["status"]) != 402 {
		t.Fatal(saved)
	}
}
func TestCheckoutStopsBeforeSideEffects(t *testing.T) {
	for _, kind := range []string{"totals", "stock", "changed"} {
		t.Run(kind, func(t *testing.T) {
			p := profile(t)
			f := &checkoutFixture{p: p, t: t, conflict: kind == "stock", changed: kind == "changed"}
			c, _ := clientFor(t, p, f.handler)
			a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
			o := checkoutOptions()
			if kind == "totals" {
				o.Values["expected-reservation"] = "100,00 kr"
			}
			if _, e := a.Checkout(context.Background(), c, p, o); e == nil {
				t.Fatal("unsafe checkout passed")
			}
			place, mode := f.calls()
			if place != 0 {
				t.Fatal("submitted changed order")
			}
			if kind != "changed" && mode != 0 {
				t.Fatal("payment mode changed before validation")
			}
			saved, _ := p.Load("payment")
			if saved != nil {
				t.Fatal(saved)
			}
		})
	}
}
func TestTimeoutNeverRetriesPayment(t *testing.T) {
	p := profile(t)
	f := &checkoutFixture{
		p: p, t: t, timeout: true,
		timeoutStarted: make(chan struct{}), timeoutRelease: make(chan struct{}), timeoutDone: make(chan struct{}),
	}
	c, _ := clientFor(t, p, f.handler)
	c.HTTP.Timeout = 20 * time.Millisecond
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	checkoutDone := make(chan error, 1)
	go func() {
		_, err := a.Checkout(context.Background(), c, p, checkoutOptions())
		checkoutDone <- err
	}()
	select {
	case <-f.timeoutStarted:
	case <-time.After(time.Second):
		t.Fatal("payment request did not start")
	}
	if e := <-checkoutDone; e == nil {
		t.Fatal("timeout ignored")
	}
	close(f.timeoutRelease)
	saved, _ := p.Load("payment")
	select {
	case <-f.timeoutDone:
	case <-time.After(time.Second):
		t.Fatal("timed-out request handler did not finish")
	}
	if place, _ := f.calls(); saved["state"] != "starting" || place != 1 {
		t.Fatal(saved, place)
	}
	if _, e := a.Checkout(context.Background(), c, p, checkoutOptions()); e == nil {
		t.Fatal("duplicate allowed")
	}
}
func TestKlarnaTokenPassedToWillys(t *testing.T) {
	p := profile(t)
	f := &checkoutFixture{p: p, t: t}
	c, _ := clientFor(t, p, f.handler)
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	a.Authorize = func(context.Context, Object) (string, error) { return "synthetic-token", nil }
	o := checkoutOptions()
	o.Values["method"] = "klarna"
	o.Bools["experimental-klarna"] = true
	if _, e := a.Checkout(context.Background(), c, p, o); e != nil {
		t.Fatal(e)
	}
	if token := f.authorizationToken(); token != "synthetic-token" {
		t.Fatal(token)
	}
}
func TestKlarnaFailureDoesNotSubmit(t *testing.T) {
	p := profile(t)
	f := &checkoutFixture{p: p, t: t}
	c, _ := clientFor(t, p, f.handler)
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	a.Authorize = func(context.Context, Object) (string, error) { return "", errors.New("declined") }
	o := checkoutOptions()
	o.Values["method"] = "klarna"
	o.Bools["experimental-klarna"] = true
	if _, e := a.Checkout(context.Background(), c, p, o); e == nil {
		t.Fatal(e)
	}
	if place, _ := f.calls(); place != 0 {
		t.Fatal(place)
	}
}
func TestPaymentURLValidation(t *testing.T) {
	for _, u := range []string{"http://ecom.payex.com/a", "https://ecom.payex.com.evil.test/a", "https://user@ecom.payex.com/a", "https://ecom.payex.com:9999/a", "file:///a"} {
		if _, e := PaymentURL(u); e == nil {
			t.Fatal(u)
		}
	}
}
func TestFingerprintDetectsAddressChange(t *testing.T) {
	a := sampleCart()
	b := sampleCart()
	obj(b["deliveryAddress"])["line1"] = "Another 2"
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("address change ignored")
	}
}
func TestNonInteractiveSetupDoesNotWrite(t *testing.T) {
	p := profile(t)
	writes := 0
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
		}
		writeJSON(w, sampleCart())
	})
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	if _, e := a.Setup(context.Background(), c, p, Options{Values: map[string]string{}}); e == nil {
		t.Fatal("missing fields accepted")
	}
	if writes != 0 {
		t.Fatal("wrote before collecting inputs")
	}
}
func TestStaleSlots(t *testing.T) {
	p := profile(t)
	_ = p.Save("slots", Object{"created": 0, "scope": "homeDelivery:11111"})
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	if _, e := a.SelectSlot(context.Background(), nil, p, "1"); e == nil {
		t.Fatal("stale cache accepted")
	}
}
func TestSlotContextChange(t *testing.T) {
	p := profile(t)
	_ = p.Save("slots", Object{"created": time.Now().Unix(), "scope": "homeDelivery:99999:Old", "slots": []any{Object{"code": "s"}}})
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cart" {
			writeJSON(w, sampleCart())
			return
		}
		writeJSON(w, Object{"slots": []any{}})
	})
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	if _, e := a.SelectSlot(context.Background(), c, p, "1"); e == nil || !strings.Contains(e.Error(), "fulfillment changed") {
		t.Fatal(e)
	}
}
func TestJSONOutputContainsOnlyJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WILLYS_DATA_DIR", root)
	var out, errs bytes.Buffer
	a := NewApp(strings.NewReader(""), &out, &errs, false)
	a.Connect = func(p *Profile) (*Client, error) {
		c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, Object{"results": []any{Object{"code": "123", "name": "Pasta"}}})
		})
		return c, nil
	}
	if e := a.Execute(context.Background(), []string{"--json", "search", "pasta"}); e != nil {
		t.Fatal(e)
	}
	var v []any
	if e := json.Unmarshal(out.Bytes(), &v); e != nil || len(v) != 1 {
		t.Fatal(out.String(), e)
	}
}
func TestInvalidQuantitiesNeverWrite(t *testing.T) {
	for _, n := range []string{"nan", "+Inf", "-1", "1.5"} {
		t.Run(n, func(t *testing.T) {
			p := profile(t)
			a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
			o, _ := Parse([]string{"set", "123_ST", n})
			if _, e := a.Run(context.Background(), p, o); e == nil {
				t.Fatal("accepted quantity", n)
			}
		})
	}
}
func TestKlarnaCallbackBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var statuses []int
	value, err := AuthorizeKlarna(ctx, Object{"client_token": "synthetic</script>"}, func(target string) error {
		r, e := http.Get(target)
		if e != nil {
			return e
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.Header.Get("Cache-Control") != "no-store" || bytes.Contains(b, []byte("synthetic</script>")) {
			t.Error("unsafe page")
		}
		u, _ := url.Parse(target)
		origin := u.Scheme + "://" + u.Host
		for _, source := range []string{"https://evil.example", origin} {
			req, _ := http.NewRequest("POST", target, strings.NewReader(`{"authorization_token":"synthetic-result"}`))
			req.Header.Set("Origin", source)
			resp, e := http.DefaultClient.Do(req)
			if e != nil {
				return e
			}
			statuses = append(statuses, resp.StatusCode)
			resp.Body.Close()
		}
		return nil
	})
	if err != nil || value != "synthetic-result" || fmt.Sprint(statuses) != "[403 200]" {
		t.Fatal(value, err, statuses)
	}
}

func TestCLISetupSlotsAndPaymentAcrossInvocations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WILLYS_DATA_DIR", root)
	p, err := NewProfile(root, "default")
	if err != nil {
		t.Fatal(err)
	}
	cart := sampleCart()
	delete(cart, "deliveryModeCode")
	delete(cart, "deliveryAddress")
	delete(cart, "slotCode")
	f := &checkoutFixture{p: p, t: t}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cart":
			writeJSON(w, cart)
		case "/cart/customer-contact-info", "/cart/postal-code":
			writeJSON(w, Object{})
		case "/cart/delivery-mode/homeDelivery":
			cart["deliveryModeCode"] = "homeDelivery"
			writeJSON(w, Object{})
		case "/cart/delivery-address":
			var address Object
			if err := json.NewDecoder(r.Body).Decode(&address); err != nil {
				t.Error(err)
			}
			address["line1"] = address["addressLine1"]
			cart["deliveryAddress"] = address
			writeJSON(w, Object{})
		case "/slot/homeDelivery":
			if r.URL.Query().Get("postalCode") != "11111" {
				t.Error("wrong postcode")
			}
			writeJSON(w, Object{"tmsSlots": true, "slots": []any{Object{"available": true, "code": "slot-test", "formattedTime": "Monday 12-14", "totalCost": Object{"formattedValue": "10,00 kr"}, "tmsDeliveryWindowReference": Object{"id": "window-test"}}}})
		case "/slot/slotInCart/slot-test":
			var window Object
			_ = json.NewDecoder(r.Body).Decode(&window)
			if window["id"] != "window-test" || r.URL.Query().Get("isTmsSlot") != "true" {
				t.Error("wrong reservation payload")
			}
			cart["slotCode"] = "slot-test"
			writeJSON(w, Object{})
		default:
			f.handler(w, r)
		}
	}))
	defer server.Close()
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		a := NewApp(strings.NewReader(""), &out, io.Discard, false)
		a.Connect = func(p *Profile) (*Client, error) {
			c, e := NewClient(p)
			if e == nil {
				c.Base = server.URL
			}
			return c, e
		}
		if e := a.Execute(context.Background(), args); e != nil {
			t.Fatal(args, e)
		}
		return out.String()
	}
	run("setup", "--first-name", "Test", "--last-name", "Shopper", "--phone", "0700000000", "--email", "shopper@example.com", "--mode", "delivery", "--street", "Example 1", "--postcode", "11111", "--town", "Stockholm")
	if out := run("--json", "slots"); !strings.Contains(out, "Monday 12-14") {
		t.Fatal(out)
	}
	run("slot", "1")
	run("checkout", "--method", "card", "--yes", "--expected-total", "100,00 kr", "--expected-reservation", "110,00 kr", "--no-open")
	if out := run("payment", "--url"); !strings.Contains(out, "https://ecom.payex.com/checkout/synthetic") {
		t.Fatal(out)
	}
	if place, _ := f.calls(); place != 1 {
		t.Fatal("unexpected order count", place)
	}
}

func TestProductLocksAndCheckout(t *testing.T) {
	p := profile(t)
	ctx := context.Background()
	lock := func(command, code string) (func(), error) {
		return commandLock(ctx, p, Options{Command: command, Positionals: []string{code}})
	}
	a, e := lock("set", "a")
	if e != nil {
		t.Fatal(e)
	}
	b, e := lock("remove", "b")
	if e != nil {
		t.Fatal(e)
	}
	read, e := lock("search", "")
	if e != nil {
		t.Fatal(e)
	}
	read()
	for _, command := range []string{"set", "checkout", "setup", "slot"} {
		timeout, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
		release, e := commandLock(timeout, p, Options{Command: command, Positionals: []string{"a"}})
		cancel()
		if e == nil {
			release()
			t.Fatal("conflicting command did not wait", command)
		}
	}
	a()
	b()
	checkout, e := lock("checkout", "")
	if e != nil {
		t.Fatal(e)
	}
	timeout, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if release, e := commandLock(timeout, p, Options{Command: "set", Positionals: []string{"b"}}); e == nil {
		release()
		t.Fatal("write passed checkout")
	}
	checkout()
	release, e := lock("set", "a")
	if e != nil {
		t.Fatal(e)
	}
	release()
}

func TestConcurrentCookieMergeAndStaleDeletion(t *testing.T) {
	p := profile(t)
	u, _ := url.Parse("https://www.willys.se/")
	a, _ := NewCookieStore(p)
	b, _ := NewCookieStore(p)
	a.SetCookies(u, []*http.Cookie{{Name: "first", Value: "a", Path: "/"}})
	b.SetCookies(u, []*http.Cookie{{Name: "second", Value: "b", Path: "/"}})
	if e := a.Save(); e != nil {
		t.Fatal(e)
	}
	if e := b.Save(); e != nil {
		t.Fatal(e)
	}
	c, _ := NewCookieStore(p)
	if len(c.Cookies(u)) != 2 {
		t.Fatal("lost concurrent cookie")
	}
	stale, _ := NewCookieStore(p)
	c.SetCookies(u, []*http.Cookie{{Name: "first", MaxAge: -1, Path: "/"}})
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	stale.SetCookies(u, []*http.Cookie{{Name: "first", Value: "a", Path: "/"}})
	if e := stale.Save(); e != nil {
		t.Fatal(e)
	}
	final, _ := NewCookieStore(p)
	if len(final.Cookies(u)) != 1 || final.Cookies(u)[0].Name != "second" {
		t.Fatal("stale response restored deleted cookie")
	}
}

func TestCommaSearchParallelAndPartialFailure(t *testing.T) {
	p := profile(t)
	var active, peak atomic.Int32
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		if r.URL.Query().Get("size") != "3" {
			t.Error("limit was not applied to each term")
		}
		term := r.URL.Query().Get("q")
		if term == "bad" {
			w.WriteHeader(503)
			return
		}
		writeJSON(w, Object{"results": []any{Object{"name": term}}})
	})
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	result, e := a.Search(context.Background(), c, p, " pasta, oats, bad, milk, rice ", 0, 3)
	if e == nil {
		t.Fatal("accepted partial search failure")
	}
	groups := list(result)
	if len(groups) != 5 || obj(groups[0])["query"] != "pasta" || obj(groups[2])["error"] == nil || len(list(obj(groups[4])["products"])) != 1 {
		t.Fatal(result)
	}
	if peak.Load() < 2 || peak.Load() > 4 {
		t.Fatal("unexpected concurrency", peak.Load())
	}
	if _, e = a.Search(context.Background(), c, p, "pasta,,oats", 0, 3); e == nil {
		t.Fatal("accepted empty term")
	}
}
