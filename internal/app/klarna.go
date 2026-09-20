package app

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed klarna.html
var klarnaPage string

func AuthorizeKlarna(ctx context.Context, session Object, open func(string) error) (string, error) {
	token := text(session["client_token"])
	if token == "" {
		return "", errors.New("Willys did not return a Klarna client token")
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	route := "/" + hex.EncodeToString(nonceBytes)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	host := listener.Addr().String()
	origin := "http://" + host
	var category any
	categories := list(session["payment_method_categories"])
	if len(categories) == 1 {
		category = obj(categories[0])["identifier"]
	}
	config, err := json.Marshal(Object{"clientToken": token, "category": category})
	if err != nil {
		return "", err
	}
	page := strings.Replace(klarnaPage, "CONFIG_PLACEHOLDER", string(config), 1)
	result := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != host || r.URL.Path != route {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, page)
		case http.MethodPost:
			if r.Header.Get("Origin") != origin {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			defer r.Body.Close()
			var body struct {
				Token string `json:"authorization_token"`
			}
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&body); err != nil || body.Token == "" || len(body.Token) > 4096 {
				http.Error(w, "Invalid authorization", http.StatusBadRequest)
				return
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				http.Error(w, "Invalid authorization", http.StatusBadRequest)
				return
			}
			select {
			case result <- body.Token:
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "OK")
			default:
				http.Error(w, "Already received", http.StatusConflict)
			}
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = open(origin + route); err != nil {
		return "", err
	}
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	select {
	case value := <-result:
		return value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("Klarna authorization timed out; this command did not submit an order")
	case err := <-serveError:
		return "", err
	}
}
