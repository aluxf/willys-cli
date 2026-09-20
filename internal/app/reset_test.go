package app

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestResetCartClearsCartInOneRequest(t *testing.T) {
	p := profile(t)
	if err := p.Save("slots", Object{"created": float64(1)}); err != nil {
		t.Fatal(err)
	}
	products := []any{Object{"code": "123"}}
	var deletes int
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cart":
			if r.Method == http.MethodDelete {
				deletes++
				if r.URL.Query().Get("cancelPossibleContinueCart") != "false" {
					t.Error("missing safe cancellation flag")
				}
				products = nil
				writeJSON(w, Object{})
				return
			}
			writeJSON(w, Object{"code": "cart-test", "products": products})
		case "/csrf-token":
			writeJSON(w, "csrf")
		default:
			t.Error("unexpected request", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	a := NewApp(nil, nil, nil, false)
	result, err := a.ResetCart(context.Background(), c, p)
	if err != nil || obj(result)["products"] != 0 || deletes != 1 {
		t.Fatal(result, err, deletes)
	}
	if slots, err := p.Load("slots"); err != nil || len(slots) != 0 {
		t.Fatal(slots, err)
	}
}

func TestCookieSaveLockWaitIsBounded(t *testing.T) {
	p := profile(t)
	store, err := NewCookieStore(p)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://www.willys.se/")
	store.SetCookies(u, []*http.Cookie{{Name: "synthetic", Value: "value", Path: "/"}})
	lock := flock.New(filepath.Join(p.Path, "cookies.json.lock"))
	defer lock.Close()
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatal(ok, err)
	}
	start := time.Now()
	if err := store.Save(); err == nil {
		t.Fatal("save ignored a held lock")
	}
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Fatal("unexpected save wait", elapsed)
	}
}

func TestResetBlocksPaymentAndExistingOrder(t *testing.T) {
	for _, pending := range []bool{true, false} {
		p := profile(t)
		if pending {
			if e := p.Save("payment", Object{"state": "starting"}); e != nil {
				t.Fatal(e)
			}
		}
		c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
			if pending || r.Method != "GET" || r.URL.Path != "/cart" {
				t.Error("reset made an unsafe request", r.Method, r.URL.Path)
			}
			writeJSON(w, Object{"code": "old", "orderReference": "existing-order", "products": []any{}})
		})
		a := NewApp(nil, nil, nil, false)
		if _, e := a.ResetCart(context.Background(), c, p); e == nil {
			t.Fatal("reset should refuse protected state")
		}
	}
}
