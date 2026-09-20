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
			if target, err := PaymentURL(text(saved["location"])); err == nil {
				view["paymentURL"] = target
				view["paymentMessage"] = "Complete payment with Swedbank Pay or Klarna. Opening this link does not confirm an order."
			}
		}
	}
	return view, nil
}

func cartPageHandler(host, route string, load func(context.Context) (Object, error)) http.Handler {
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
	server := &http.Server{Handler: cartPageHandler(host, route, func(ctx context.Context) (Object, error) { return a.cartPageData(ctx, p, profile) }), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 30 * time.Second}
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
