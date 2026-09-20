package app

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func dealFixture(code, name, brand string) Object {
	return Object{"code": code, "name": name, "manufacturer": brand, "displayVolume": "500g", "price": "12,20 kr", "image": Object{"url": "https://example.test/image"}, "potentialPromotions": []any{Object{"code": "offer-1", "campaignType": "LOYALTY", "price": Object{"formattedValue": "10,00 kr"}, "qualifyingCount": 2, "conditionLabel": "2 för", "rewardLabel": "20,00", "redeemLimitLabel": "Max 5 köp", "validUntil": float64(1789941599000), "realMixAndMatch": true, "productCodes": []any{code, "other"}}}}
}
func dealPage(page, size, pages, total int, rows []any) Object {
	return Object{"pagination": Object{"currentPage": page, "pageSize": size, "numberOfPages": pages, "totalNumberOfResults": total}, "results": rows}
}
func TestDealsFetchesOnlyRequestedPage(t *testing.T) {
	calls := 0
	c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("mutated active profile")
		}
		switch r.URL.Path {
		case "/store/active":
			writeJSON(w, Object{"storeId": "2351", "name": "Test store"})
		case "/search/campaigns/online":
			calls++
			q := r.URL.Query()
			if q.Get("q") != "2351" || q.Get("type") != "PERSONAL_GENERAL" || q.Get("page") != "1" || q.Get("size") != "1" {
				t.Error(q)
			}
			writeJSON(w, dealPage(1, 1, 4, 4, []any{dealFixture("2", "Pasta Penne", "Garant")}))
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := setupApp().Deals(context.Background(), c, Options{Values: map[string]string{"page": "1", "limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	out := obj(result)
	if calls != 1 || out["totalOffers"] != 4 || out["hasMore"] != true || out["page"] != 1 || out["searches"] != nil {
		t.Fatal(calls, out)
	}
	product := obj(list(out["products"])[0])
	if product["code"] != "2" || product["image"] != nil || product["regularPrice"] != "12,20 kr" || !strings.Contains(text(product["url"]), "Pasta-Penne-2") {
		t.Fatal(product)
	}
	offer := obj(list(product["offers"])[0])
	if offer["offerPrice"] != "10,00 kr" || number(offer["qualifyingQuantity"]) != 2 || offer["reward"] != "20,00" || offer["requiresMembership"] != true || offer["redemptionLimit"] != "Max 5 köp" {
		t.Fatal(offer)
	}
	if offer["validUntil"] != "2026-09-20T21:59:59Z" {
		t.Fatal(offer["validUntil"])
	}
}
func TestDealsRejectsIncompleteOrInvalidPage(t *testing.T) {
	for _, kind := range []string{"failed-page", "wrong-page", "wrong-size", "duplicate", "missing-metadata", "short-page"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
				if kind == "failed-page" {
					http.Error(w, "unavailable", 503)
					return
				}
				if kind == "missing-metadata" {
					writeJSON(w, Object{"results": []any{}})
					return
				}
				rows := []any{dealFixture("1", "Pasta", "Garant"), dealFixture("2", "Dill", "Garant")}
				if kind == "duplicate" {
					rows[1] = rows[0]
				}
				if kind == "short-page" {
					rows = rows[:1]
				}
				page := 0
				size := 2
				if kind == "wrong-page" {
					page = 1
				}
				if kind == "wrong-size" {
					size = 3
				}
				writeJSON(w, dealPage(page, size, 2, 4, rows))
			})
			if result, err := fetchDealPage(context.Background(), c, "2351", 0, 2); err == nil || result != nil {
				t.Fatal("accepted invalid page", result, err)
			}
		})
	}
}
func TestDealStoreOverrideUsesSeparateSession(t *testing.T) {
	p := profile(t)
	writes := 0
	c, server := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("account-secret"); err == nil {
			t.Errorf("account cookie reached guest session: %s", cookie.Name)
		}
		switch r.URL.Path {
		case "/csrf-token":
			writeJSON(w, "csrf")
		case "/store/activate":
			writes++
			if r.Method != "POST" || r.URL.Query().Get("storeId") != "2351" {
				t.Error(r.Method, r.URL.RawQuery)
			}
			writeJSON(w, Object{})
		case "/store/active":
			writeJSON(w, Object{"storeId": "2351"})
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	})
	u, _ := url.Parse(server.URL)
	c.HTTP.Jar.SetCookies(u, []*http.Cookie{{Name: "account-secret", Value: "synthetic"}})
	preview, store, cleanup, err := dealStoreClient(context.Background(), c, "2351")
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if preview == c || store["storeId"] != "2351" || writes != 1 {
		t.Fatal("wrong preview session")
	}
	path := filepath.Dir(preview.Cookies.file)
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preview session was not removed", err)
	}
	if len(c.HTTP.Jar.Cookies(u)) != 1 {
		t.Fatal("modified source cookies")
	}
}
func TestDealDetailsAndUnknownEligibility(t *testing.T) {
	p := dealFixture("1", "Dill", "Garant")
	obj(list(p["potentialPromotions"])[0])["campaignType"] = "UNKNOWN"
	view := dealView(p, true)
	if view["image"] == nil || view["potentialPromotions"] == nil {
		t.Fatal("missing details")
	}
	if obj(list(view["offers"])[0])["requiresMembership"] != nil {
		t.Fatal("claimed eligibility for unknown campaign")
	}
}
func TestDealsValidateArgumentsBeforeRequests(t *testing.T) {
	for _, args := range [][]string{{"deals", "pasta,"}, {"deals", "--store", "2351:bad"}, {"deals", "--page", "-1"}, {"deals", "--limit", "0"}, {"deals", "extra", "argument"}} {
		options, err := Parse(args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setupApp().Deals(context.Background(), nil, options); err == nil {
			t.Fatal(args)
		}
	}
}
func TestDealsEmptyCatalogAndOutOfRangePage(t *testing.T) {
	for _, total := range []int{0, 1} {
		c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/store/active" {
				writeJSON(w, Object{"storeId": "2351"})
				return
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			writeJSON(w, dealPage(page, 20, total, total, []any{}))
		})
		result, err := setupApp().Deals(context.Background(), c, Options{Values: map[string]string{"page": "1000000"}})
		if err != nil {
			t.Fatal(err)
		}
		out := obj(result)
		if len(list(out["products"])) != 0 || out["hasMore"] != false || out["totalOffers"] != total {
			t.Fatal(out)
		}
	}
}

func TestDealsIgnoresExtraEmptyAPIPage(t *testing.T) {
	c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, dealPage(1, 2, 3, 4, []any{dealFixture("3", "Pasta", "Garant"), dealFixture("4", "Dill", "Garant")}))
	})
	result, err := fetchDealPage(context.Background(), c, "2351", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result["hasMore"] != false || result["totalOffers"] != 4 {
		t.Fatal(result)
	}
}
