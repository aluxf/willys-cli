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
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed login.html
var loginHTML string

//go:embed login.js
var loginJS string

type loginView struct {
	sync.RWMutex
	State   string
	Message string
	PNG     []byte
}

func (v *loginView) set(state, message string) {
	v.Lock()
	defer v.Unlock()
	v.State = state
	v.Message = message
	if state != "pending" {
		v.PNG = nil
	}
}
func signedIn(customer Object) bool {
	return text(customer["uid"]) != "" && text(customer["uid"]) != "anonymous" && !yes(customer["isAnonymous"])
}
func membershipView(customer, cart Object) Object {
	state := "anonymous"
	if signedIn(customer) {
		state = "signed in"
	}
	membership := "not verified"
	if !signedIn(customer) {
		membership = "sign in required"
	} else if text(customer["memberCreationDateFull"]) != "" || text(customer["memberCreationMonthAndYear"]) != "" {
		membership = "Willys Plus account"
	}
	return Object{"state": state, "membership": membership, "name": strings.TrimSpace(text(customer["firstName"]) + " " + text(customer["lastName"])), "savedAddressAvailable": len(obj(customer["defaultShippingAddress"])) > 0, "cartHasAddress": text(obj(cart["deliveryAddress"])["line1"]) != "", "fulfillment": cart["deliveryModeCode"], "slotSelected": text(cart["slotCode"]) != "", "cart": cart["code"], "total": cart["totalPrice"]}
}

// Save only setup defaults, never identity numbers or authentication responses.
func cacheAccountDefaults(p *Profile, customer Object) error {
	address := obj(customer["defaultShippingAddress"])
	contact, err := p.Load("contact")
	if err != nil {
		return err
	}
	if contact == nil {
		contact = Object{}
	}
	values := Object{"firstName": first(customer["firstName"], address["firstName"]), "lastName": first(customer["lastName"], address["lastName"]), "email": first(customer["email"], address["email"]), "cellphone": first(address["cellphone"], address["phone"])}
	for key, value := range values {
		if text(contact[key]) == "" && text(value) != "" {
			contact[key] = value
		}
	}
	if err := p.Save("contact", contact); err != nil {
		return err
	}
	saved, err := p.Load("address")
	if err != nil {
		return err
	}
	if saved == nil {
		saved = Object{}
	}
	defaults := Object{"addressLine1": first(address["line1"], address["addressLine1"]), "postalCode": address["postalCode"], "town": address["town"]}
	for key, value := range defaults {
		if text(saved[key]) == "" && text(value) != "" {
			saved[key] = value
		}
	}
	return p.Save("address", saved)
}

func (a *App) AuthStatus(ctx context.Context, c *Client) (any, error) {
	value, err := c.Get(ctx, "/customer", nil)
	if err != nil {
		return nil, err
	}
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	return membershipView(obj(value), cart), nil
}
func (a *App) Login(ctx context.Context, c *Client, p *Profile) (any, error) {
	if err := paymentPending(p); err != nil {
		return nil, err
	}
	value, err := c.Get(ctx, "/customer", nil)
	if err != nil {
		return nil, err
	}
	before, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if signedIn(obj(value)) {
		if err := cacheAccountDefaults(p, obj(value)); err != nil {
			return nil, err
		}
		return membershipView(obj(value), before), nil
	}
	if text(before["orderReference"]) != "" {
		return nil, errors.New("cannot change account while editing an order")
	}
	if err = p.Save("cart-before-login", before); err != nil {
		return nil, err
	}
	auth := a.BankID
	if auth == nil {
		auth = a.authorizeBankID
	}
	if err = auth(ctx, c); err != nil {
		return nil, err
	}
	value, err = c.Get(ctx, "/customer", nil)
	if err != nil {
		return nil, errors.New("BankID completed, but account verification failed; run willys auth")
	}
	customer := obj(value)
	if !signedIn(customer) {
		return nil, errors.New("BankID completed, but Willys did not authenticate this session")
	}
	after, err := c.Cart(ctx)
	if err != nil {
		return nil, errors.New("signed in, but cart verification failed; run willys cart")
	}
	if err := cacheAccountDefaults(p, customer); err != nil {
		return nil, fmt.Errorf("signed in, but could not save setup defaults: %w", err)
	}
	out := membershipView(customer, after)
	out["cartChanged"] = Fingerprint(before) != Fingerprint(after)
	out["nextAction"] = "Review willys cart. Run setup if the cart needs contact or address details, then select a slot."
	if Fingerprint(before) != Fingerprint(after) {
		out["notice"] = "Willys changed the cart during login. The previous cart is saved in this profile as cart-before-login.json. No carts were merged by the CLI."
	}
	return out, nil
}

func bankIDHandler(host, route string, view *loginView, cancel func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != host || r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host) {
			http.Error(w, "Forbidden", 403)
			return
		}
		if r.URL.Path == route+"/cancel" && r.Method == http.MethodPost {
			if r.Header.Get("Origin") != "http://"+host {
				http.Error(w, "Forbidden", 403)
				return
			}
			cancel()
			w.WriteHeader(204)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}
		switch r.URL.Path {
		case route + "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, loginHTML)
		case route + "/login.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = io.WriteString(w, loginJS)
		case route + "/cart.css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = io.WriteString(w, cartCSS)
		case route + "/status":
			view.RLock()
			defer view.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Object{"state": view.State, "message": view.Message, "qrReady": len(view.PNG) > 0})
		case route + "/qr":
			view.RLock()
			defer view.RUnlock()
			if len(view.PNG) == 0 {
				http.Error(w, "QR code unavailable", 404)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(view.PNG)
		default:
			http.NotFound(w, r)
		}
	})
}

func (a *App) authorizeBankID(ctx context.Context, c *Client) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
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
	view := &loginView{State: "pending", Message: "Open BankID on your phone and scan the QR code."}
	server := &http.Server{Handler: bankIDHandler(host, route, view, cancel), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	}()
	defer server.Close()
	response, err := c.Post(ctx, "/checkout/bankid/auth", nil, Object{"mobile": true, "generateQrData": true})
	if err != nil {
		return errors.New("Willys could not start BankID login; no login was confirmed")
	}
	order := text(obj(response)["orderRef"])
	if order == "" {
		return errors.New("Willys returned no BankID login reference")
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_, _ = c.Post(cleanup, "/checkout/bankid/cancel", nil, Object{"orderRef": order})
		}
	}()
	if err := a.Open("http://" + host + route + "/"); err != nil {
		return fmt.Errorf("cannot open BankID login: %w", err)
	}
	fmt.Fprintln(a.Err, "Scan the QR code in your browser with BankID. Approve only a Willys identification request. Press Ctrl+C to cancel.")
	err = pollBankID(ctx, c, order, view, time.Second)
	if err == nil {
		completed = true
		view.set("complete", "BankID approved. Check the CLI for account verification.")
	} else {
		view.set("failed", err.Error())
	}
	// Let the page receive the final status before the local server closes.
	if ctx.Err() == nil {
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
		}
	}
	return err
}

func pollBankID(ctx context.Context, c *Client, order string, view *loginView, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	waitingForApproval := false
	for tick := 0; ; tick++ {
		if tick%2 == 0 {
			value, err := c.Post(ctx, "/checkout/bankid/collect-login", nil, Object{"orderRef": order, "rememberMe": "false"})
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return errors.New("BankID login status could not be verified; run willys auth before trying again")
			}
			data := obj(value)
			switch text(data["status"]) {
			case "COMPLETE":
				return nil
			case "CUSTOMERNOTFOUND":
				return errors.New("no Willys account was found; join Willys Plus on willys.se, then run willys auth login again")
			case "FAILED":
				return errors.New("BankID login failed or was canceled; run willys auth login to try again")
			case "PENDING":
				if text(data["hintCode"]) == "userSign" {
					waitingForApproval = true
					view.set("pending", "Confirm the Willys identification request in BankID.")
					view.Lock()
					view.PNG = nil
					view.Unlock()
				}
			default:
				return errors.New("Willys returned an unknown BankID login status")
			}
		}
		if !waitingForApproval {
			qr, err := c.Post(ctx, "/checkout/bankid/qr", nil, nil)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return errors.New("BankID QR code expired or could not refresh; run willys auth login again")
			}
			value := text(obj(qr)["qrString"])
			if value != "" {
				png, err := qrcode.Encode(value, qrcode.Medium, 320)
				if err != nil {
					return errors.New("could not render BankID QR code")
				}
				view.Lock()
				view.PNG = png
				view.Unlock()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
