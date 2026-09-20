package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func (a *App) field(o Options, key, label string, saved any) (string, error) {
	if value := strings.TrimSpace(o.Values[key]); value != "" {
		return value, nil
	}
	return a.Ask(label, text(saved))
}

func normalizeText(value string) (string, error) {
	return strings.Join(strings.Fields(value), " "), nil
}

func normalizePhone(value string) (string, error) {
	value = strings.ReplaceAll(value, " ", "")
	if strings.HasPrefix(value, "+46") {
		value = "0" + strings.TrimPrefix(value, "+46")
	}
	if !regexp.MustCompile(`^07[0-9]{8}$`).MatchString(value) {
		return "", errors.New("use a Swedish mobile number such as 07XXXXXXXX or +467XXXXXXXX")
	}
	return value, nil
}

func normalizeEmail(value string) (string, error) {
	email, err := mail.ParseAddress(value)
	if err != nil || email.Address != value {
		return "", errors.New("enter a valid email address")
	}
	return value, nil
}

func normalizePostcode(value string) (string, error) {
	value = strings.ReplaceAll(value, " ", "")
	if !regexp.MustCompile(`^[0-9]{5}$`).MatchString(value) {
		return "", errors.New("postcode must have five digits")
	}
	return value, nil
}

func (a *App) setupField(o Options, key, label string, saved any, normalize func(string) (string, error)) (string, error) {
	if value, present := o.Values[key]; present && strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	for {
		value, err := a.field(o, key, label, saved)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			err = fmt.Errorf("%s is required", label)
		} else {
			value, err = normalize(value)
		}
		if err == nil {
			return value, nil
		}
		if _, present := o.Values[key]; present || !a.Interactive {
			return "", err
		}
		fmt.Fprintf(a.Err, "%s. Enter a valid value, or type /cancel.\n", err)
	}
}

func sameAddress(actual, expected Object) bool {
	line := text(first(actual["line1"], actual["addressLine1"]))
	postcode := text(first(actual["postalCode"], actual["postcode"]))
	town := text(actual["town"])
	line, _ = normalizeText(line)
	postcode, _ = normalizePostcode(postcode)
	town, _ = normalizeText(town)
	return strings.EqualFold(line, text(expected["addressLine1"])) && postcode == text(expected["postalCode"]) && strings.EqualFold(town, text(expected["town"]))
}

func (a *App) Setup(ctx context.Context, c *Client, p *Profile, o Options) (any, error) {
	current, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if text(current["orderReference"]) != "" {
		return nil, errors.New("editing existing orders is not supported")
	}
	saved, err := p.Load("contact")
	if err != nil {
		return nil, err
	}
	fields := Object{}
	for _, f := range []struct {
		key, saved, label string
		normalize         func(string) (string, error)
	}{
		{"first-name", "firstName", "First name", normalizeText},
		{"last-name", "lastName", "Last name", normalizeText},
		{"phone", "cellphone", "Mobile number", normalizePhone},
		{"email", "email", "Email", normalizeEmail},
	} {
		value, e := a.setupField(o, f.key, f.label, saved[f.saved], f.normalize)
		if e != nil {
			return nil, e
		}
		fields[f.saved] = value
	}
	mode := o.Values["mode"]
	if mode == "" {
		mode, err = a.Choose([]Choice{{"Home delivery", "delivery"}, {"Store pickup", "pickup"}}, "Fulfillment")
		if err != nil {
			return nil, err
		}
	}
	var address Object
	storeID := ""
	switch mode {
	case "delivery":
		prior, e := p.Load("address")
		if e != nil {
			return nil, e
		}
		address = clone(fields)
		for _, f := range []struct {
			key, saved, label string
			normalize         func(string) (string, error)
		}{
			{"street", "addressLine1", "Street address", normalizeText},
			{"postcode", "postalCode", "Postcode", normalizePostcode},
			{"town", "town", "Town", normalizeText},
		} {
			v, e := a.setupField(o, f.key, f.label, prior[f.saved], f.normalize)
			if e != nil {
				return nil, e
			}
			address[f.saved] = v
		}
	case "pickup":
		value, e := c.Get(ctx, "/store", url.Values{"clickAndCollect": {"true"}})
		if e != nil {
			return nil, e
		}
		choices := []Choice{}
		valid := map[string]bool{}
		for _, raw := range list(value) {
			s := obj(raw)
			if text(s["name"]) != "" && yes(s["clickAndCollect"]) {
				id := text(s["storeId"])
				choices = append(choices, Choice{text(s["name"]), id})
				valid[id] = true
			}
		}
		storeID = o.Values["store"]
		if storeID == "" {
			storeID, err = a.Choose(choices, "Pickup store")
			if err != nil {
				return nil, err
			}
		}
		if !valid[storeID] {
			return nil, errors.New("this store does not offer pickup")
		}
	default:
		return nil, errors.New("mode must be delivery or pickup")
	}
	// Collect and validate all inputs before changing the checkout.
	if _, err = c.Post(ctx, "/cart/customer-contact-info", nil, fields); err != nil {
		return nil, fmt.Errorf("setup did not confirm contact details; check the cart: %w", err)
	}
	if err = p.Save("contact", fields); err != nil {
		return nil, fmt.Errorf("contact details are saved in the cart but not in this profile: %w", err)
	}
	if mode == "delivery" {
		if _, err = c.Post(ctx, "/cart/postal-code", url.Values{"postalCode": {text(address["postalCode"])}}, nil); err != nil {
			return nil, fmt.Errorf("contact details are saved; setup did not confirm the postcode: %w", err)
		}
		if _, err = c.Post(ctx, "/cart/delivery-mode/homeDelivery", nil, nil); err != nil {
			return nil, fmt.Errorf("contact details and postcode are saved; setup did not confirm home delivery: %w", err)
		}
		if _, err = c.Post(ctx, "/cart/delivery-address", nil, address); err != nil {
			return nil, fmt.Errorf("contact details, postcode, and home delivery are saved; setup did not confirm the address: %w", err)
		}
		if err = p.Save("address", address); err != nil {
			return nil, fmt.Errorf("delivery address is saved in the cart but not in this profile: %w", err)
		}
	} else {
		if _, err = c.Post(ctx, "/store/activate", url.Values{"storeId": {storeID}, "activelySelected": {"true"}, "forceAsPickingStore": {"true"}}, nil); err != nil {
			return nil, fmt.Errorf("contact details are saved; setup did not confirm pickup-store activation: %w", err)
		}
		if _, err = c.Post(ctx, "/cart/delivery-mode/pickUpInStore", url.Values{"newSuggestedStoreId": {storeID}}, nil); err != nil {
			return nil, fmt.Errorf("contact details and pickup-store activation are saved; setup did not confirm pickup mode: %w", err)
		}
	}
	after, err := c.Cart(ctx)
	if err != nil {
		return nil, fmt.Errorf("setup changed fulfillment but could not verify it; check the cart: %w", err)
	}
	if mode == "delivery" && (text(after["deliveryModeCode"]) != "homeDelivery" || !sameAddress(obj(after["deliveryAddress"]), address)) {
		return nil, errors.New("delivery setup differs from the requested address; check the cart")
	}
	if mode == "pickup" {
		if !isPickup(text(after["deliveryModeCode"])) {
			return nil, errors.New("pickup setup was not saved; check the cart")
		}
		store, e := c.Get(ctx, "/store/active", nil)
		if e != nil {
			return nil, fmt.Errorf("pickup mode is saved but the active store was not verified: %w", e)
		}
		if text(obj(store)["storeId"]) != storeID {
			return nil, errors.New("pickup setup selected a different store; check the cart")
		}
	}
	if err = p.Save("slots", Object{}); err != nil {
		return nil, err
	}
	return "Setup saved. Run willys slots --choose to select a time.", nil
}
func isPickup(mode string) bool { return mode == "pickUpInStore" || mode == "pickupInStore" }
func (c *Client) availableSlots(ctx context.Context) (Object, string, error) {
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, "", err
	}
	mode := text(cart["deliveryModeCode"])
	var data any
	var scope string
	if mode == "homeDelivery" {
		postcode := text(first(cart["postalCode"], obj(cart["deliveryAddress"])["postalCode"]))
		if postcode == "" {
			return nil, "", errors.New("run willys setup first")
		}
		scope = mode + ":" + postcode + ":" + text(obj(cart["deliveryAddress"])["line1"])
		data, err = c.Get(ctx, "/slot/homeDelivery", url.Values{"postalCode": {postcode}, "b2b": {"false"}})
	} else if isPickup(mode) {
		store, e := c.Get(ctx, "/store/active", nil)
		if e != nil {
			return nil, "", e
		}
		id := text(obj(store)["storeId"])
		scope = "pickup:" + id
		data, err = c.Get(ctx, "/slot/pickInStore", url.Values{"storeId": {id}, "b2b": {"false"}})
	} else {
		return nil, "", errors.New("run willys setup first")
	}
	if err != nil {
		return nil, "", err
	}
	d := obj(data)
	available := []any{}
	for _, s := range list(d["slots"]) {
		if yes(obj(s)["available"]) {
			available = append(available, s)
		}
	}
	d["slots"] = available
	return d, scope, nil
}
func (a *App) Slots(ctx context.Context, c *Client, p *Profile, o Options) (any, error) {
	data, scope, err := c.availableSlots(ctx)
	if err != nil {
		return nil, err
	}
	slots := list(data["slots"])
	if err = p.Save("slots", Object{"created": a.Now().Unix(), "scope": scope, "tms": yes(data["tmsSlots"]), "slots": slots}); err != nil {
		return nil, err
	}
	out := []any{}
	choices := []Choice{}
	for i, raw := range slots {
		s := obj(raw)
		fee := obj(s["totalCost"])["formattedValue"]
		out = append(out, Object{"number": i + 1, "time": s["formattedTime"], "fee": fee})
		choices = append(choices, Choice{text(s["formattedTime"]) + " — " + text(fee), strconv.Itoa(i + 1)})
	}
	if o.Bools["choose"] {
		n, e := a.Choose(choices, "Delivery time")
		if e != nil {
			return nil, e
		}
		return a.SelectSlot(ctx, c, p, n)
	}
	return out, nil
}
func (a *App) SelectSlot(ctx context.Context, c *Client, p *Profile, selection string) (any, error) {
	cached, err := p.Load("slots")
	if err != nil {
		return nil, err
	}
	age := float64(a.Now().Unix()) - number(cached["created"])
	if cached == nil || age < 0 || age > 600 || text(cached["scope"]) == "" {
		return nil, errors.New("run willys slots to refresh the time options")
	}
	n, err := strconv.Atoi(selection)
	slots := list(cached["slots"])
	if err != nil || n < 1 || n > len(slots) {
		return nil, errors.New("choose a number from willys slots")
	}
	selected := obj(slots[n-1])
	fresh, scope, err := c.availableSlots(ctx)
	if err != nil {
		return nil, err
	}
	if scope != text(cached["scope"]) {
		return nil, errors.New("fulfillment changed; run willys slots again")
	}
	for _, raw := range list(fresh["slots"]) {
		s := obj(raw)
		if text(s["code"]) == text(selected["code"]) {
			if text(obj(s["totalCost"])["formattedValue"]) != text(obj(selected["totalCost"])["formattedValue"]) {
				return nil, errors.New("slot fee changed; run willys slots again")
			}
			_, err = c.Post(ctx, "/slot/slotInCart/"+url.PathEscape(text(s["code"])), url.Values{"isTmsSlot": {strconv.FormatBool(yes(fresh["tmsSlots"]))}}, s["tmsDeliveryWindowReference"])
			if err != nil {
				return nil, err
			}
			cart, e := c.Cart(ctx)
			if e != nil {
				return nil, e
			}
			if text(cart["slotCode"]) != text(s["code"]) {
				return nil, errors.New("the requested slot was not saved")
			}
			return Object{"time": s["formattedTime"], "total": cart["totalPrice"], "reservedUntil": cart["slotReservedTo"]}, nil
		}
	}
	return nil, errors.New("that slot is no longer available; run willys slots again")
}

func PaymentURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "ecom.payex.com" && u.Hostname() != "payments.klarna.com") {
		return "", errors.New("the provider returned an unsupported payment URL; no browser was opened")
	}
	return u.String(), nil
}
func Fingerprint(cart Object) string {
	products := []string{}
	for _, raw := range list(cart["products"]) {
		p := obj(raw)
		b, _ := json.Marshal(Object{"code": p["code"], "quantity": p["pickQuantity"], "unit": p["pickUnit"], "substitution": p["replacement"]})
		products = append(products, string(b))
	}
	sort.Strings(products)
	value := Object{"cart": cart["code"], "total": cart["totalPrice"], "reservation": cart["reservedAmount"], "slot": cart["slotCode"], "products": products, "deliveryAddress": cart["deliveryAddress"], "deliveryMode": cart["deliveryModeCode"]}
	b, _ := json.Marshal(value)
	return string(b)
}
func validateCart(cart Object) error {
	if len(list(cart["products"])) == 0 || text(cart["slotCode"]) == "" {
		return errors.New("add products and select a slot before checkout")
	}
	if text(cart["orderReference"]) != "" {
		return errors.New("editing existing orders is not supported")
	}
	if text(cart["deliveryModeCode"]) == "homeDelivery" && text(obj(cart["deliveryAddress"])["line1"]) == "" {
		return errors.New("run willys setup to save the delivery address")
	}
	return nil
}
func (c *Client) checkStock(ctx context.Context, cart Object) error {
	status, err := c.Get(ctx, "/cart/status", url.Values{"slotCode": {text(cart["slotCode"])}, "checkStock": {"true"}})
	if err != nil {
		return err
	}
	s := obj(status)
	if text(s["reason"]) != "" || yes(s["emptyCart"]) {
		return errors.New("checkout reports a cart problem; review the cart")
	}
	for _, raw := range list(s["cartStatus"]) {
		x := obj(raw)
		for _, key := range []string{"outOfStock", "partialOutOfStock", "priceAbscent", "showSalableOnline", "showUpdatedPrice", "notAllowedAnonymous"} {
			if yes(x[key]) {
				return errors.New("checkout reports stock or price changes; review the cart")
			}
		}
	}
	return nil
}
func (a *App) Checkout(ctx context.Context, c *Client, p *Profile, o Options) (any, error) {
	if err := paymentPending(p); err != nil {
		return nil, err
	}
	cart, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateCart(cart); err != nil {
		return nil, err
	}
	if err = c.checkStock(ctx, cart); err != nil {
		return nil, err
	}
	available, err := c.Get(ctx, "/checkout/paymentmodes", nil)
	if err != nil {
		return nil, err
	}
	choices := []Choice{}
	valid := map[string]bool{}
	for _, m := range list(available) {
		switch text(m) {
		case "PspPayexAll":
			choices = append(choices, Choice{"Card — hosted Swedbank Pay page", "card"})
			valid["card"] = true
		case "Klarna":
			if !o.Bools["experimental-klarna"] {
				continue
			}
			choices = append(choices, Choice{"Klarna — experimental browser authorization", "klarna"})
			valid["klarna"] = true
		}
	}
	method := o.Values["method"]
	if method == "" {
		method, err = a.Choose(choices, "Payment method")
		if err != nil {
			return nil, err
		}
	}
	if method == "klarna" && !o.Bools["experimental-klarna"] {
		return nil, errors.New("Klarna is experimental; pass --experimental-klarna to enable it")
	}
	if !valid[method] {
		return nil, errors.New("this payment method is unavailable")
	}

	if o.Bools["yes"] {
		if o.Values["expected-total"] == "" || o.Values["expected-reservation"] == "" || o.Values["expected-total"] != text(cart["totalPrice"]) || o.Values["expected-reservation"] != text(cart["reservedAmount"]) {
			return nil, errors.New("--yes requires matching --expected-total and --expected-reservation")
		}
	} else {
		_ = Emit(a.Err, Object{"cartTotal": cart["totalPrice"], "paymentReservation": cart["reservedAmount"], "buffer": cart["bufferedAmount"], "slot": cart["slotCode"], "method": method}, false)
		answer, e := a.Ask("Start this payment? Type yes", "")
		if e != nil {
			return nil, e
		}
		if answer != "yes" {
			return "Payment was not started.", nil
		}
	}
	expected := Fingerprint(cart)
	mode := "PspPayexAll"
	if method == "klarna" {
		mode = "Klarna"
	}
	if _, err = c.Post(ctx, "/checkout/paymentmode", nil, Object{"mode": mode}); err != nil {
		return nil, err
	}
	token := ""
	if method == "klarna" {
		fmt.Fprintln(a.Err, "Klarna opens a local browser page. Live authorization remains experimental.")
		session, e := c.Get(ctx, "/klarna/payment-session", nil)
		if e != nil {
			return nil, e
		}
		token, err = a.Authorize(ctx, obj(session))
		if err != nil {
			return nil, err
		}
	}
	current, err := c.Cart(ctx)
	if err != nil {
		return nil, err
	}
	if Fingerprint(current) != expected {
		return nil, errors.New("the cart changed during checkout; review it before continuing")
	}
	if err = c.checkStock(ctx, current); err != nil {
		return nil, err
	}
	attempt := Object{"state": "starting", "created": a.Now().Unix(), "cart": cart["code"], "method": method}
	// Keep uncertain attempts. Never retry an order submission automatically.
	if err = p.Save("payment", attempt); err != nil {
		return nil, err
	}
	response, err := c.Place(ctx, url.Values{"saveCard": {"false"}, "selectedCard": {""}, "orderRef": {"null"}, "klarnaAuthorizationToken": {token}, "userClickedContinue": {"false"}})
	if err != nil {
		if !c.OrderSubmitted {
			attempt["state"] = "not_submitted"
			if saveErr := p.Save("payment", attempt); saveErr != nil {
				return nil, errors.Join(err, saveErr)
			}
			return nil, fmt.Errorf("payment was not submitted; run willys payment recover: %w", err)
		}
		return nil, fmt.Errorf("payment outcome is unknown; do not retry checkout; run willys payment status: %w", err)
	}
	attempt["state"] = "response"
	attempt["status"] = response.Status
	attempt["location"] = response.Location
	attempt["body"] = response.Body
	if err = p.Save("payment", attempt); err != nil {
		return nil, err
	}
	if response.Location == "" {
		return nil, fmt.Errorf("payment returned no URL (HTTP %d); the attempt remains saved", response.Status)
	}
	target, err := PaymentURL(response.Location)
	if err != nil {
		return nil, err
	}
	if !o.Bools["no-open"] {
		if err = a.Open(target); err != nil {
			return nil, fmt.Errorf("payment link is saved; run willys payment --url: %w", err)
		}
	}
	return Object{"state": "payment pending", "url": target, "reservation": current["reservedAmount"]}, nil
}
