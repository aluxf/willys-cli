package app

import (
	"context"
	"fmt"
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
func dealPage(page, pages, total int, rows []any) Object {
	return Object{"pagination": Object{"currentPage": page, "numberOfPages": pages, "totalNumberOfResults": total}, "results": rows}
}
func TestDealsSearchesEveryPageBeforeLimiting(t *testing.T) {
	p := profile(t)
	pagesRead := []int{}
	c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("mutated active profile")
		}
		switch r.URL.Path {
		case "/store/active":
			writeJSON(w, Object{"storeId": "2351", "name": "Test store"})
		case "/search/campaigns/online":
			if r.URL.Query().Get("q") != "2351" || r.URL.Query().Get("type") != "PERSONAL_GENERAL" {
				t.Error(r.URL.RawQuery)
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			pagesRead = append(pagesRead, page)
			rows := []any{dealFixture("1", "Mjölk", "Garant")}
			if page == 1 {
				rows = []any{dealFixture("2", "Pasta Penne", "Garant"), dealFixture("3", "Pasta Spaghetti", "Garant"), dealFixture("4", "Dill", "Other")}
			}
			writeJSON(w, dealPage(page, 2, 4, rows))
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := setupApp().Deals(context.Background(), c, Options{Positionals: []string{"GARANT pasta, dill, absent"}, Values: map[string]string{"limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(pagesRead) != "[0 1]" {
		t.Fatal(pagesRead)
	}
	groups := list(obj(result)["searches"])
	pasta := obj(groups[0])
	if pasta["totalMatches"] != 2 || pasta["hasMore"] != true || obj(list(pasta["products"])[0])["code"] != "2" {
		t.Fatal(pasta)
	}
	if obj(groups[1])["totalMatches"] != 1 || obj(groups[2])["totalMatches"] != 0 {
		t.Fatal(groups)
	}
	product := obj(list(pasta["products"])[0])
	if product["image"] != nil || product["regularPrice"] != "12,20 kr" || !strings.Contains(text(product["url"]), "Pasta-Penne-2") {
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
func TestDealsRejectsPartialOrChangingCatalog(t *testing.T) {
	for _, kind := range []string{"failed-page", "changed-total", "repeated-page", "duplicate", "missing-metadata", "short-catalog"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if kind == "missing-metadata" {
					writeJSON(w, Object{"results": []any{}})
					return
				}
				if page == 0 {
					writeJSON(w, dealPage(0, 2, 3, []any{dealFixture("1", "Pasta", "Garant")}))
					return
				}
				if kind == "failed-page" {
					http.Error(w, "unavailable", 503)
					return
				}
				total := 3
				if kind == "changed-total" {
					total = 4
				}
				if kind == "repeated-page" {
					page = 0
				}
				rows := []any{dealFixture("2", "Dill", "Garant"), dealFixture("3", "Milk", "Garant")}
				if kind == "duplicate" {
					rows[0] = dealFixture("1", "Pasta", "Garant")
				}
				if kind == "short-catalog" {
					rows = rows[:1]
				}
				writeJSON(w, dealPage(page, 2, total, rows))
			})
			if rows, err := fetchDeals(context.Background(), c, "2351"); err == nil || rows != nil {
				t.Fatal("reported partial catalog as complete", rows, err)
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
func TestDealsEmptyCatalogAndLargePage(t *testing.T) {
	for _, total := range []int{0, 1} {
		c, _ := clientFor(t, profile(t), func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/store/active" {
				writeJSON(w, Object{"storeId": "2351"})
				return
			}
			rows := []any{}
			if total == 1 {
				rows = append(rows, dealFixture("1", "Pasta", "Garant"))
			}
			writeJSON(w, dealPage(0, total, total, rows))
		})
		result, err := setupApp().Deals(context.Background(), c, Options{Values: map[string]string{"page": strconv.Itoa(int(^uint(0) >> 1))}})
		if err != nil {
			t.Fatal(err)
		}
		group := obj(list(obj(result)["searches"])[0])
		if len(list(group["products"])) != 0 || group["hasMore"] != false || group["totalMatches"] != total {
			t.Fatal(group)
		}
	}
}
