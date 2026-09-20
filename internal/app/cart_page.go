package app

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed cart.html
var cartHTML string

//go:embed cart.js
var cartJS string

//go:embed cart.css
var cartCSS string

func (a *App) cartPageData(ctx context.Context, p *Profile, profile string) (Object, error) {
	// Hold the exclusive lock only while reading a consistent snapshot.
	release, err := commandLock(ctx, p, Options{Command: "checkout"})
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Run(ctx, p, Options{Command: "cart", Profile: profile, Bools: map[string]bool{"details": true}, Values: map[string]string{}})
	if err != nil {
		return nil, err
	}
	view := obj(result)
	view["profile"] = profile
	saved, err := p.Load("payment")
	if err != nil {
		return nil, err
	}
	view["paymentMessage"] = "Run willys checkout to create a payment link, then refresh this page."
	if saved != nil {
		view["paymentMessage"] = "A payment attempt is saved. Check willys payment status before continuing."
		if text(saved["cart"]) != "" && saved["cart"] == view["code"] {
			connect := a.Connect
			if connect == nil {
				connect = NewClient
			}
			client, err := connect(p)
			if err != nil {
				return nil, err
			}
			canceled, err := client.canceledPayment(ctx, saved)
			if err != nil {
				view["paymentMessage"] = "Cannot verify payment status. Try refreshing before continuing."
				return view, nil
			}
			if canceled {
				view["paymentCanceled"] = true
				view["paymentMessage"] = "Your previous payment was canceled. Start a new payment to continue."
				return view, nil
			}
			if target, err := PaymentURL(text(saved["location"])); err == nil {
				view["paymentURL"] = target
				view["paymentMessage"] = "Complete payment with Swedbank Pay or Klarna. Opening this link does not confirm an order."
			}
		}
	}
	return view, nil
}

func cartPageHandler(host, route string, load func(context.Context) (Object, error), restart ...func(context.Context, Object) (Object, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src https:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == route+"/payment" && r.Method == http.MethodPost && len(restart) > 0 {
			if r.Header.Get("Origin") != "http://"+host {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			defer r.Body.Close()
			var body Object
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			data, err := restart[0](r.Context(), body)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(Object{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(data)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var content, kind string
		switch r.URL.Path {
		case route + "/":
			content, kind = cartHTML, "text/html; charset=utf-8"
		case route + "/cart.js":
			content, kind = cartJS, "text/javascript; charset=utf-8"
		case route + "/cart.css":
			content, kind = cartCSS, "text/css; charset=utf-8"
		case route + "/data":
			ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
			defer cancel()
			data, err := load(ctx)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(Object{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(data)
			return
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", kind)
		_, _ = io.WriteString(w, content)
	})
}

func (a *App) ServeCartPage(ctx context.Context, p *Profile, profile string) error {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	route := "/" + hex.EncodeToString(nonce)
	host := listener.Addr().String()
	server := &http.Server{Handler: cartPageHandler(host, route, func(ctx context.Context) (Object, error) { return a.cartPageData(ctx, p, profile) }, func(ctx context.Context, body Object) (Object, error) { return a.restartCanceledPayment(ctx, p, body) }), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer server.Close()
	target := "http://" + host + route + "/"
	if err := a.Open(target); err != nil {
		return fmt.Errorf("cannot open cart page: %w", err)
	}
	fmt.Fprintf(a.Out, "Cart review: %s\n", target)
	fmt.Fprintln(a.Err, "Keep this command running. Press Ctrl+C to close the cart review. The server closes after one hour.")
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) || strings.Contains(text(err), "use of closed network connection") {
			return nil
		}
		return err
	}
}

func (a *App) restartCanceledPayment(ctx context.Context, p *Profile, body Object) (Object, error) {
	release, err := commandLock(ctx, p, Options{Command: "checkout"})
	if err != nil {
		return nil, err
	}
	defer release()
	connect := a.Connect
	if connect == nil {
		connect = NewClient
	}
	c, err := connect(p)
	if err != nil {
		return nil, err
	}
	saved, err := p.Load("payment")
	if err != nil {
		return nil, err
	}
	canceled, err := c.canceledPayment(ctx, saved)
	if err != nil {
		return nil, err
	}
	if !canceled {
		return nil, errors.New("the previous payment is not confirmed canceled; no new payment was started")
	}
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if text(body["total"]) == "" || body["total"] != cart["totalPrice"] || body["reservation"] != cart["reservedAmount"] {
		return nil, errors.New("cart totals changed; refresh before starting a new payment")
	}
	if err := validateCart(cart); err != nil {
		return nil, err
	}
	if text(saved["method"]) != "card" {
		return nil, errors.New("restart this payment through the CLI")
	}
	if _, err := a.PaymentStatus(ctx, c, p, true); err != nil {
		return nil, err
	}
	result, err := a.Checkout(ctx, c, p, Options{Values: map[string]string{"method": "card", "expected-total": text(body["total"]), "expected-reservation": text(body["reservation"])}, Bools: map[string]bool{"yes": true, "no-open": true}})
	return obj(result), err
}
