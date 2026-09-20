package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Provider checks never follow completion or cancellation callbacks.
func (c *Client) canceledPayment(ctx context.Context, saved Object) (bool, error) {
	target, err := PaymentURL(text(saved["location"]))
	if err != nil {
		return false, nil
	}
	u, _ := url.Parse(target)
	if u.Hostname() != "ecom.payex.com" || !regexp.MustCompile(`^/checkout/[a-fA-F0-9]{64}$`).MatchString(u.Path) {
		return false, nil
	}
	u.Path = strings.Replace(u.Path, "/checkout/", "/checkout/core/", 1)
	client := c.ProviderHTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copyClient.Jar = nil
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}
	response, err := copyClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("cannot verify provider payment state: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("cannot verify provider payment state (HTTP %d)", response.StatusCode)
	}
	return canceledProviderData(io.LimitReader(response.Body, 2<<20), text(saved["cart"])), nil
}

func canceledProviderData(reader io.Reader, cart string) bool {
	if cart == "" {
		return false
	}
	tokenizer := html.NewTokenizer(reader)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			for _, attr := range token.Attr {
				if attr.Key != "data-paymentdata-message" {
					continue
				}
				var data Object
				if json.Unmarshal([]byte(attr.Val), &data) != nil {
					return false
				}
				action, body := obj(data["action"]), obj(data["body"])
				return data["status"] == "Success" && action["actionType"] == "OnPaymentCanceled" && action["state"] == "Aborted" && text(action["id"]) != "" && action["id"] == body["id"] && regexp.MustCompile(`^`+regexp.QuoteMeta(BaseURL)+`/singlestepcheckout/cancelPayment/[0-9]+$`).MatchString(text(action["redirectUrl"]))
			}
		}
	}
}
