package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
)

var dealStoreID = regexp.MustCompile(`^[0-9]+$`)

// Store overrides need a separate session because prices follow the active store.
func dealStoreClient(ctx context.Context, current *Client, storeID string) (*Client, Object, func(), error) {
	cleanup := func() {}
	client := current
	if storeID != "" {
		if !dealStoreID.MatchString(storeID) {
			return nil, nil, cleanup, errors.New("store must be a numeric store ID; use willys stores")
		}
		root, err := os.MkdirTemp("", "willys-deals-")
		if err != nil {
			return nil, nil, cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(root) }
		p, err := NewProfile(root, "preview")
		if err != nil {
			return nil, nil, cleanup, err
		}
		client, err = NewClient(p)
		if err != nil {
			return nil, nil, cleanup, err
		}
		client.Base = current.Base
		if _, err = client.Post(ctx, "/store/activate", url.Values{"storeId": {storeID}, "activelySelected": {"true"}, "forceAsPickingStore": {"true"}}, nil); err != nil {
			return nil, nil, cleanup, fmt.Errorf("cannot select preview store: %w", err)
		}
	}
	value, err := client.Get(ctx, "/store/active", nil)
	if err != nil {
		return nil, nil, cleanup, err
	}
	store := obj(value)
	actual := text(store["storeId"])
	if !dealStoreID.MatchString(actual) {
		return nil, nil, cleanup, errors.New("no active store found; use deals --store ID or run setup")
	}
	if storeID != "" && storeID != actual {
		return nil, nil, cleanup, errors.New("Willys did not select the requested preview store")
	}
	return client, store, cleanup, nil
}

func paginationInt(p Object, key string) (int, error) {
	value, ok := p[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("online offers returned no %s", key)
	}
	n := number(value)
	switch value.(type) {
	case int, int64, float64:
	default:
		return 0, fmt.Errorf("online offers returned invalid %s", key)
	}
	if n < 0 || n != math.Trunc(n) || n > float64(math.MaxInt32) {
		return 0, fmt.Errorf("online offers returned invalid %s", key)
	}
	return int(n), nil
}

func fetchDealPage(ctx context.Context, c *Client, storeID string, page, limit int) (Object, error) {
	value, err := c.Get(ctx, "/search/campaigns/online", url.Values{"q": {storeID}, "type": {"PERSONAL_GENERAL"}, "page": {strconv.Itoa(page)}, "size": {strconv.Itoa(limit)}})
	if err != nil {
		return nil, fmt.Errorf("cannot load online offers page %d: %w", page, err)
	}
	data := obj(value)
	pagination := obj(data["pagination"])
	current, err := paginationInt(pagination, "currentPage")
	if err != nil {
		return nil, err
	}
	pages, err := paginationInt(pagination, "numberOfPages")
	if err != nil {
		return nil, err
	}
	total, err := paginationInt(pagination, "totalNumberOfResults")
	if err != nil {
		return nil, err
	}
	size, err := paginationInt(pagination, "pageSize")
	if err != nil {
		return nil, err
	}
	// Willys reports an extra page when the total is divisible by the page size.
	actualPages := (total + limit - 1) / limit
	if current != page || size != limit || pages < actualPages || pages > total/limit+1 {
		return nil, errors.New("online offers returned inconsistent pagination; run deals again")
	}
	expected := 0
	if page <= total/limit {
		expected = min(limit, total-page*limit)
	}
	rows, ok := data["results"].([]any)
	if !ok || len(rows) != expected {
		return nil, errors.New("online offers returned an incomplete page; run deals again")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		code := text(obj(row)["code"])
		if code == "" || seen[code] {
			return nil, errors.New("online offers returned missing or duplicate product codes; run deals again")
		}
		seen[code] = true
	}
	return Object{"products": rows, "page": page, "limit": limit, "totalOffers": total, "hasMore": page < actualPages-1}, nil
}

func (a *App) Deals(ctx context.Context, c *Client, o Options) (any, error) {
	if len(o.Positionals) != 0 {
		return nil, errors.New("deals browses online offers; use willys search \"TERM\" to find products and their offers")
	}
	page, err := o.Int("page", 0)
	if err != nil {
		return nil, err
	}
	limit, err := o.Int("limit", 20)
	if err != nil {
		return nil, err
	}
	if page < 0 || limit < 1 || limit > 100 {
		return nil, errors.New("page must be nonnegative; limit must be 1–100")
	}
	client, store, cleanup, err := dealStoreClient(ctx, c, o.Values["store"])
	defer cleanup()
	if err != nil {
		return nil, err
	}
	result, err := fetchDealPage(ctx, client, text(store["storeId"]), page, limit)
	if err != nil {
		return nil, err
	}
	rows := []any{}
	for _, raw := range list(result["products"]) {
		rows = append(rows, dealView(obj(raw), o.Bools["details"]))
	}
	result["products"] = rows
	result["storeId"] = store["storeId"]
	result["storeName"] = store["name"]
	result["pricingSession"] = "current profile"
	if o.Values["store"] != "" {
		result["pricingSession"] = "temporary guest preview; your profile is unchanged"
	}
	result["note"] = "Advertised offers. Conditions and membership apply. Check the cart for applied prices and fulfillment-date eligibility."
	return result, nil
}
