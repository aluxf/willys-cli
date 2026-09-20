package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const BaseURL = "https://www.willys.se/axfood/rest/v2"

type Response struct {
	Status   int    `json:"status"`
	Location string `json:"location"`
	Body     string `json:"body"`
}
type Client struct {
	OrderSubmitted bool
	Base           string
	HTTP           *http.Client
	Cookies        *CookieStore
}

func NewClient(p *Profile) (*Client, error) {
	cookies, err := NewCookieStore(p)
	if err != nil {
		return nil, err
	}
	return &Client{Base: BaseURL, Cookies: cookies, HTTP: &http.Client{Jar: cookies, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 9 {
			return errors.New("too many API redirects")
		}
		if len(via) > 0 && (req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme) {
			return errors.New("API redirect changed origin")
		}
		return nil
	}}}, nil
}
func (c *Client) request(ctx context.Context, method, endpoint string, q url.Values, data any, form, raw bool) (result any, err error) {
	if !strings.HasPrefix(endpoint, "/") || strings.ContainsAny(endpoint, "?#") || strings.Contains(endpoint, "://") {
		return nil, errors.New("invalid API endpoint")
	}
	if c.Cookies != nil {
		if err := c.Cookies.Refresh(ctx); err != nil {
			return nil, err
		}
	}
	target := c.Base + endpoint
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	headers := http.Header{"Accept": []string{"application/json"}, "User-Agent": []string{"willys-cli/" + Version}}
	var body io.Reader
	if method != http.MethodGet {
		token, e := c.Get(ctx, "/csrf-token", nil)
		if e != nil {
			return nil, e
		}
		if text(token) == "" {
			return nil, errors.New("Willys returned no CSRF token")
		}
		headers.Set("X-CSRF-Token", text(token))
		if form {
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
			v, ok := data.(url.Values)
			if !ok {
				return nil, errors.New("invalid form")
			}
			body = strings.NewReader(v.Encode())
		} else {
			headers.Set("Content-Type", "application/json")
			if data != nil {
				b, e := json.Marshal(data)
				if e != nil {
					return nil, e
				}
				body = bytes.NewReader(b)
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	client := *c.HTTP
	if raw {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	defer func() {
		if c.Cookies != nil {
			if e := c.Cookies.Save(); e != nil {
				err = errors.Join(err, fmt.Errorf("save session: %w", e))
			}
		}
	}()
	if endpoint == "/singlestepcheckout/placeOrder" {
		c.OrderSubmitted = true
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request did not finish; no automatic retry: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if raw {
		return Response{resp.StatusCode, resp.Header.Get("Location"), string(b)}, nil
	}
	if resp.StatusCode >= 400 {
		var message Object
		_ = json.Unmarshal(b, &message)
		detail := text(first(message["errorMessage"], message["error"], http.StatusText(resp.StatusCode)))
		return nil, failure("api_error", fmt.Sprintf("Willys returned HTTP %d: %s", resp.StatusCode, detail), 1, nil)
	}
	if len(b) == 0 {
		return nil, nil
	}
	var value any
	if err = json.Unmarshal(b, &value); err != nil {
		return nil, fmt.Errorf("Willys returned invalid JSON (HTTP %d)", resp.StatusCode)
	}
	return value, nil
}
func (c *Client) Get(ctx context.Context, p string, q url.Values) (any, error) {
	return c.request(ctx, http.MethodGet, p, q, nil, false, false)
}
func (c *Client) Post(ctx context.Context, p string, q url.Values, v any) (any, error) {
	return c.request(ctx, http.MethodPost, p, q, v, false, false)
}
func (c *Client) Place(ctx context.Context, form url.Values) (Response, error) {
	v, err := c.request(ctx, http.MethodPost, "/singlestepcheckout/placeOrder", nil, form, true, true)
	if err != nil {
		return Response{}, err
	}
	return v.(Response), nil
}
func (c *Client) Cart(ctx context.Context) (Object, error) {
	v, err := c.Get(ctx, "/cart", nil)
	return obj(v), err
}
func (c *Client) Product(ctx context.Context, code string) (Object, error) {
	v, err := c.Get(ctx, "/p/"+url.PathEscape(code), nil)
	return obj(v), err
}

func ProductView(p, detail Object, full bool) Object {
	if detail == nil {
		detail = p
	}
	crumbs := list(first(detail["breadcrumbs"], detail["breadCrumbs"]))
	var productURL any
	for i := len(crumbs) - 1; i >= 0; i-- {
		path := text(obj(crumbs[i])["url"])
		if strings.HasPrefix(path, "/produkt/") || strings.HasPrefix(path, "/produktdetalj/") {
			productURL = "https://www.willys.se" + path
			break
		}
	}
	view := Object{"code": p["code"], "name": p["name"], "brand": p["manufacturer"], "packSize": first(p["displayVolume"], p["productLine2"]), "price": p["price"], "quantity": p["pickQuantity"], "lineTotal": p["totalDiscountedPriceWithDeposit"], "discount": p["totalDiscount"], "deposit": p["depositPrice"], "comparisonPrice": p["comparePrice"], "comparisonUnit": p["comparePriceUnit"], "outOfStock": p["outOfStock"], "url": productURL}
	if full {
		out := clone(detail)
		for k, v := range p {
			out[k] = v
		}
		for k, v := range view {
			out[k] = v
		}
		return out
	}
	return view
}
func (c *Client) CartView(ctx context.Context, full bool) (Object, error) {
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	products := []any{}
	for _, raw := range list(cart["products"]) {
		p := obj(raw)
		d, e := c.Product(ctx, text(p["code"]))
		if e != nil {
			return nil, e
		}
		products = append(products, ProductView(p, d, full))
	}
	return Object{"code": cart["code"], "totalUnits": cart["totalUnitCount"], "total": cart["totalPrice"], "reservation": cart["reservedAmount"], "buffer": cart["bufferedAmount"], "deliveryFee": cart["serviceCost"], "slot": cart["slotFormattedDate"], "products": products}, nil
}
