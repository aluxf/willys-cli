package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSearchShowsOffersWithoutFilteringOrExtraRequests(t *testing.T) {
	for _, query := range []string{"pasta", "pasta, dill"} {
		t.Run(query, func(t *testing.T) {
			p := profile(t)
			c, _ := clientFor(t, p, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/search/clean" {
					t.Error("extra request", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				q := r.URL.Query()
				if q.Get("page") != "2" || q.Get("size") != "3" {
					t.Error(q)
				}
				plain := dealFixture("plain", "Ordinary pasta", "Example")
				delete(plain, "potentialPromotions")
				writeJSON(w, Object{"results": []any{plain, dealFixture("offer", "Offer pasta", "Garant")}})
			})
			result, err := setupApp().Search(context.Background(), c, p, query, 2, 3)
			if err != nil {
				t.Fatal(err)
			}
			groups := []any{Object{"products": result}}
			if strings.Contains(query, ",") {
				groups = list(result)
			}
			for _, raw := range groups {
				rows := list(obj(raw)["products"])
				if len(rows) != 2 || obj(rows[0])["code"] != "plain" || obj(rows[1])["code"] != "offer" {
					t.Fatal("search order or ordinary products changed", rows)
				}
				plain, promoted := obj(rows[0]), obj(rows[1])
				if len(list(plain["offers"])) != 0 {
					t.Fatal("ordinary product gained an offer")
				}
				if promoted["price"] != "12,20 kr" {
					t.Fatal("offer replaced regular price", promoted)
				}
				offer := obj(list(promoted["offers"])[0])
				if offer["offerPrice"] != "10,00 kr" || number(offer["qualifyingQuantity"]) != 2 || offer["requiresMembership"] != true || offer["reward"] != "20,00" {
					t.Fatal(offer)
				}
				if promoted["url"] == nil || promoted["image"] != nil {
					t.Fatal(promoted)
				}
			}
		})
	}
}
