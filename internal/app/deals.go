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
	"strings"
	"time"
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

func fetchDeals(ctx context.Context, c *Client, storeID string) ([]any, error) {
	products := []any{}
	seen := map[string]bool{}
	pages, total := 1, -1
	for page := 0; page < pages; page++ {
		value, err := c.Get(ctx, "/search/campaigns/online", url.Values{"q": {storeID}, "type": {"PERSONAL_GENERAL"}, "page": {strconv.Itoa(page)}, "size": {"100"}})
		if err != nil {
			return nil, fmt.Errorf("cannot load online offers page %d: %w", page, err)
		}
		data := obj(value)
		pagination := obj(data["pagination"])
		current, err := paginationInt(pagination, "currentPage")
		if err != nil {
			return nil, err
		}
		count, err := paginationInt(pagination, "numberOfPages")
		if err != nil {
			return nil, err
		}
		hits, err := paginationInt(pagination, "totalNumberOfResults")
		if err != nil {
			return nil, err
		}
		if current != page || (page > 0 && (count != pages || hits != total)) {
			return nil, errors.New("online offers changed during pagination; run deals again")
		}
		if page == 0 {
			pages, total = count, hits
		}
		rows, ok := data["results"].([]any)
		if !ok || (len(rows) == 0 && hits > 0) || (hits > 0 && (count == 0 || count > hits)) || (hits == 0 && count > 1) {
			return nil, errors.New("online offers returned incomplete pagination")
		}
		for _, row := range rows {
			code := text(obj(row)["code"])
			if code == "" || seen[code] {
				return nil, errors.New("online offers returned missing or duplicate product codes; run deals again")
			}
			seen[code] = true
			products = append(products, row)
		}
	}
	if len(products) != total {
		return nil, errors.New("online offers returned an incomplete catalog; run deals again")
	}
	return products, nil
}

func dealView(product Object, details bool) Object {
	view := ProductView(product, nil, details)
	view["regularPrice"] = product["price"]
	delete(view, "price")
	if text(view["url"]) == "" {
		slug := strings.ReplaceAll(text(product["name"]), " ", "-") + "-" + text(product["code"])
		view["url"] = "https://www.willys.se/produkt/" + url.PathEscape(slug)
	}
	promotions := []any{}
	for _, raw := range list(product["potentialPromotions"]) {
		p := obj(raw)
		var member any
		switch text(p["campaignType"]) {
		case "LOYALTY":
			member = true
		case "GENERAL":
			member = false
		}
		var expires any
		if timestamp := number(p["validUntil"]); timestamp > 0 {
			expires = time.UnixMilli(int64(timestamp)).UTC().Format(time.RFC3339)
		}
		promotion := Object{"code": p["code"], "campaignType": p["campaignType"], "requiresMembership": member, "offerPrice": obj(p["price"])["formattedValue"], "offerComparisonPrice": p["comparePrice"], "condition": first(p["conditionLabel"], p["conditionLabelFormatted"]), "reward": p["rewardLabel"], "qualifyingQuantity": p["qualifyingCount"], "redemptionLimit": p["redeemLimitLabel"], "validUntil": expires, "mixAndMatch": p["realMixAndMatch"], "percentage": p["promotionPercentage"]}
		if number(p["threshold"]) > 0 {
			promotion["minimumSpend"] = p["threshold"]
		}
		promotions = append(promotions, promotion)
	}
	view["offers"] = promotions
	return view
}

func (a *App) Deals(ctx context.Context, c *Client, o Options) (any, error) {
	if err := o.Arity(0, 1); err != nil {
		return nil, err
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
	terms := []string{""}
	if len(o.Positionals) > 0 {
		terms = strings.Split(o.Positionals[0], ",")
		for i, term := range terms {
			terms[i] = strings.TrimSpace(term)
			if terms[i] == "" {
				return nil, errors.New("each comma-separated deal search term must contain text")
			}
		}
	}
	client, store, cleanup, err := dealStoreClient(ctx, c, o.Values["store"])
	defer cleanup()
	if err != nil {
		return nil, err
	}
	products, err := fetchDeals(ctx, client, text(store["storeId"]))
	if err != nil {
		return nil, err
	}
	groups := []any{}
	for _, term := range terms {
		matches := []any{}
		words := strings.Fields(strings.ToLower(term))
		for _, raw := range products {
			product := obj(raw)
			haystack := strings.ToLower(strings.Join([]string{text(product["code"]), text(product["name"]), text(product["manufacturer"]), text(product["displayVolume"]), text(product["productLine2"])}, " "))
			match := true
			for _, word := range words {
				if !strings.Contains(haystack, word) {
					match = false
					break
				}
			}
			if match {
				matches = append(matches, raw)
			}
		}
		start := len(matches)
		if page <= len(matches)/limit {
			start = min(page*limit, len(matches))
		}
		end := min(start+limit, len(matches))
		rows := []any{}
		for _, raw := range matches[start:end] {
			rows = append(rows, dealView(obj(raw), o.Bools["details"]))
		}
		query := term
		if query == "" {
			query = "All online offers"
		}
		groups = append(groups, Object{"query": query, "totalMatches": len(matches), "page": page, "hasMore": end < len(matches), "products": rows})
	}
	pricingSession := "current profile"
	if o.Values["store"] != "" {
		pricingSession = "temporary guest preview; your profile is unchanged"
	}
	return Object{"storeId": store["storeId"], "storeName": store["name"], "pricingSession": pricingSession, "totalOffers": len(products), "note": "Advertised offers. Conditions and membership apply. Check the cart for applied prices and fulfillment-date eligibility.", "searches": groups}, nil
}
