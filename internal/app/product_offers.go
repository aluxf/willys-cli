package app

import (
	"net/url"
	"strings"
	"time"
)

func catalogProductView(product Object, details bool) Object {
	view := ProductView(product, nil, details)
	if text(view["url"]) == "" && text(product["code"]) != "" {
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
	if len(promotions) > 0 {
		view["offers"] = promotions
	}
	return view
}

func dealView(product Object, details bool) Object {
	view := catalogProductView(product, details)
	view["regularPrice"] = product["price"]
	delete(view, "price")
	return view
}
