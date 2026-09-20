package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPromptCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	a := NewApp(reader, io.Discard, io.Discard, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.ctx = ctx
	done := make(chan error, 1)
	go func() { _, err := a.Ask("First name", ""); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt ignored cancellation")
	}
}
func TestPromptEOFAndCorrection(t *testing.T) {
	a := NewApp(strings.NewReader("yes"), io.Discard, io.Discard, true)
	if v, e := a.Ask("Confirm", ""); e != nil || v != "yes" {
		t.Fatal(v, e)
	}
	a = NewApp(strings.NewReader(""), io.Discard, io.Discard, true)
	if _, e := a.Ask("Confirm", ""); e == nil {
		t.Fatal("empty EOF accepted")
	} else if ReportError(io.Discard, e, false) != 130 {
		t.Fatal(e)
	}
	a = NewApp(strings.NewReader("bad\n2\n"), io.Discard, io.Discard, true)
	if v, e := a.Choose([]Choice{{"One", "one"}, {"Two", "two"}}, "Mode"); e != nil || v != "two" {
		t.Fatal(v, e)
	}
	a = NewApp(strings.NewReader("/cancel\n"), io.Discard, io.Discard, true)
	if _, e := a.Ask("Name", ""); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestStructuredErrors(t *testing.T) {
	var out bytes.Buffer
	if code := ReportError(&out, context.Canceled, true); code != 130 {
		t.Fatal(code)
	}
	var value Object
	if err := json.Unmarshal(out.Bytes(), &value); err != nil || obj(value["error"])["code"] != "cancelled" {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	if code := ReportError(&out, failure("partial_failure", "Some searches failed", 3, nil), true); code != 3 {
		t.Fatal(code)
	}
	if !JSONRequested([]string{"search", "a", "--json"}) || JSONRequested([]string{"search", "--", "--json"}) {
		t.Fatal("JSON flag detection")
	}
}
func TestRecoveryPreservesUncertainAttempt(t *testing.T) {
	for _, state := range []string{"starting", "response", "not_submitted"} {
		t.Run(state, func(t *testing.T) {
			p := profile(t)
			saved := Object{"state": state, "cart": "cart-test"}
			if e := p.Save("payment", saved); e != nil {
				t.Fatal(e)
			}
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cart" {
					t.Error("unexpected request", r.URL.Path)
				}
				writeJSON(w, sampleCart())
			})
			a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
			_, err := a.PaymentStatus(context.Background(), c, p, true)
			_, stat := os.Stat(filepath.Join(p.Path, "payment.json"))
			if state == "not_submitted" {
				if err != nil || !os.IsNotExist(stat) {
					t.Fatal(err, stat)
				}
			} else if err == nil || stat != nil {
				t.Fatal("uncertain attempt was cleared", err, stat)
			}
		})
	}
}
func TestPreflightPaymentFailureCanRecover(t *testing.T) {
	p := profile(t)
	fixture := &checkoutFixture{p: p, t: t}
	csrf := 0
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csrf-token" {
			csrf++
			if csrf == 2 {
				w.WriteHeader(503)
				return
			}
		}
		fixture.handler(w, r)
	})
	a := NewApp(strings.NewReader(""), io.Discard, io.Discard, false)
	if _, e := a.Checkout(context.Background(), c, p, checkoutOptions()); e == nil {
		t.Fatal("preflight should fail")
	}
	saved, e := p.Load("payment")
	if e != nil || saved["state"] != "not_submitted" || c.OrderSubmitted {
		t.Fatal(saved, e)
	}
	if _, e = a.PaymentStatus(context.Background(), c, p, true); e != nil {
		t.Fatal(e)
	}
}
