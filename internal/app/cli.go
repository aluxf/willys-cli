package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var Version = "dev"

const usage = `willys — shop at Willys through HTTP

Usage: willys [--profile NAME] [--json] COMMAND [OPTIONS]

Commands:
  search "TERM, TERM" [--page N] [--limit N]  Search products; limit applies per term
  deals ["TERM, TERM"] [--store ID] [--page N] [--limit N] [--details]
                                           Browse or search online offers
  product CODE [--details]                  Show brand, size, price, and product link
  cart [--details] [--open]                 Show the cart or open its browser review
  cart reset                               Empty the cart with one bulk request
  set CODE QUANTITY [--unit pieces|kilogram] Set the final quantity; never increments
  remove CODE [--unit pieces|kilogram]       Remove one product
  stores [QUERY] [--pickup] [--all] [--details]
  setup                                    Save contact and delivery or pickup details
  slots [--choose]                          List times, optionally choose interactively
  slot NUMBER                              Reserve a time from the latest slot list
  checkout                                 Choose a payment method and start payment
  payment [--url]                           Reopen or print the saved payment link
  payment status                           Inspect the saved attempt and current cart
  payment recover                          Recover a verified canceled or unsubmitted attempt
  auth                                     Show account and cart setup status
  auth login                               Sign in with BankID in this profile
  session [--import-cookies FILE]           Show storage or import into a new profile
  version                                  Show the installed version

Setup flags (omit them for interactive prompts):
  --first-name NAME --last-name NAME --phone NUMBER --email EMAIL
  --mode delivery --street ADDRESS --postcode CODE --town TOWN
  --mode pickup --store STORE_ID
  Both modes require --street, --postcode, and --town (or interactive input).

Checkout flags:
  --method card|klarna   Select the user's chosen method; omit to ask
  --no-open             Do not open the final payment link
  --yes --expected-total "AMOUNT kr" --expected-reservation "AMOUNT kr"
                        Start without a prompt; both amounts must match the current cart
  --experimental-klarna Enable the unverified Klarna browser flow

Example workflow:
  willys cart
  willys search "penne, Pepsi Max" --limit 3
  willys set CODE 2
  willys setup
  willys slots
  willys slot 1
  willys cart
  willys checkout

Usage notes:
  Search returns brand, pack size, price, and code. Verify the match, then use set directly.
  product CODE is optional; use it only when more information is needed.
  cart --open serves a live browser review. Keep the command running; Ctrl+C closes the local server.
  The review reopens a saved payment link. Use checkout --no-open, then refresh the review.
  It uses the selected profile and closes after one hour. A verified canceled card payment can be restarted from the page.
  deals searches all online offer pages locally; --page is zero-based and --limit applies per term.
  Deals use the active store. --store previews another store in a temporary guest session.
  Offer prices require the displayed conditions. Targeted personal offers are not included.
  Swedish catalog terms work best. --details adds images, ingredients, and nutrition to products.
  auth login opens a BankID QR page. Approve identification on your phone; the CLI verifies the saved session.
  Saved account contact and address fields prefill setup. Existing local values stay unchanged.
  Login does not select a store or slot, or silently merge carts. Check auth and cart afterward.
  Use the same profile throughout an order. Default terminals share the same saved cart.
  Different-product updates can run together. Conflicting commands wait automatically.
  Review delivery fees and the reservation buffer before starting an authorized purchase.
  Never retry an uncertain payment. Check payment status; never delete state to bypass protection.
  The user completes payment and BankID. A payment link does not prove purchase completion.
  Cancel prompts with Ctrl+C or /cancel. Failed batch searches retain successful results.

Global options:
  --profile NAME  Use a separate saved session and cart; default: default
  --json          Structured output for automated parsing; normal text is the default
  --help, -h      Show this global guide without contacting Willys
  --version       Show the installed version
`

var commandOptions = map[string]map[string]bool{
	"search": {"page": false, "limit": false}, "product": {"details": true}, "cart": {"details": true, "open": true},
	"set": {"unit": false}, "remove": {"unit": false}, "stores": {"pickup": true, "all": true, "details": true},
	"setup": {"first-name": false, "last-name": false, "phone": false, "email": false, "street": false, "postcode": false, "town": false, "store": false, "mode": false},
	"slots": {"choose": true}, "slot": {}, "checkout": {"method": false, "no-open": true, "yes": true, "expected-total": false, "expected-reservation": false, "experimental-klarna": true},
	"deals": {"store": false, "page": false, "limit": false, "details": true}, "auth": {}, "payment": {"url": true}, "session": {"import-cookies": false}, "version": {},
}

type Options struct {
	Command     string
	Positionals []string
	Values      map[string]string
	Bools       map[string]bool
	Profile     string
	JSON, Help  bool
}

func Parse(args []string) (Options, error) {
	o := Options{Values: map[string]string{}, Bools: map[string]bool{}, Profile: "default"}
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			o.Positionals = append(o.Positionals, args[i+1:]...)
			break
		}
		if token == "-h" {
			token = "--help"
		}
		if strings.HasPrefix(token, "--") {
			pair := strings.SplitN(strings.TrimPrefix(token, "--"), "=", 2)
			name := pair[0]
			if name == "help" {
				o.Help = true
				continue
			}
			if name == "version" {
				o.Command = "version"
				continue
			}
			isBool, known := false, false
			if name == "profile" {
				known = true
			} else if name == "json" {
				known = true
				isBool = true
			} else if m, ok := commandOptions[o.Command]; ok {
				isBool, known = m[name]
			}
			if !known {
				return o, fmt.Errorf("unknown option --%s", name)
			}
			if isBool {
				if len(pair) > 1 {
					return o, fmt.Errorf("--%s does not take a value", name)
				}
				if name == "json" {
					o.JSON = true
				} else {
					o.Bools[name] = true
				}
				continue
			}
			var value string
			if len(pair) == 2 {
				value = pair[1]
			} else {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "--") {
					return o, fmt.Errorf("--%s needs a value", name)
				}
				value = args[i]
			}
			if name == "profile" {
				o.Profile = value
			} else {
				o.Values[name] = value
			}
			continue
		}
		if o.Command == "" {
			if _, ok := commandOptions[token]; !ok {
				return o, fmt.Errorf("unknown command %q", token)
			}
			o.Command = token
		} else {
			o.Positionals = append(o.Positionals, token)
		}
	}
	return o, nil
}
func (o Options) Arity(min, max int) error {
	if len(o.Positionals) < min || len(o.Positionals) > max {
		return fmt.Errorf("%s needs %d–%d arguments; use --help", o.Command, min, max)
	}
	return nil
}
func (o Options) Int(name string, def int) (int, error) {
	s := o.Values[name]
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("--%s needs an integer", name)
	}
	return n, nil
}

type App struct {
	ctx         context.Context
	Connect     func(*Profile) (*Client, error)
	In          *bufio.Reader
	Out, Err    io.Writer
	Interactive bool
	Open        func(string) error
	Now         func() time.Time
	BankID      func(context.Context, *Client) error
	Authorize   func(context.Context, Object) (string, error)
}

func NewApp(in io.Reader, out, errOut io.Writer, interactive bool) *App {
	a := &App{In: bufio.NewReader(in), Out: out, Err: errOut, Interactive: interactive, Open: OpenBrowser, Now: time.Now}
	a.Authorize = func(ctx context.Context, s Object) (string, error) { return AuthorizeKlarna(ctx, s, a.Open) }
	return a
}
func (a *App) Ask(label, def string) (string, error) {
	if !a.Interactive {
		return "", fmt.Errorf("%s is required; pass its flag or use an interactive terminal", label)
	}
	fmt.Fprint(a.Err, label)
	if def != "" {
		fmt.Fprintf(a.Err, " [%s]", def)
	}
	fmt.Fprint(a.Err, ": ")
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type input struct {
		value string
		err   error
	}
	ready := make(chan input, 1)
	go func() { value, err := a.In.ReadString('\n'); ready <- input{value, err} }()
	var value string
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-ready:
		value = result.value
		if result.err != nil && !(errors.Is(result.err, io.EOF) && value != "") {
			if errors.Is(result.err, io.EOF) {
				return "", failure("input_closed", "Input closed. Command cancelled.", 130, nil)
			}
			return "", result.err
		}
	}
	if strings.TrimSpace(value) == "/cancel" {
		return "", context.Canceled
	}

	value = strings.TrimSpace(value)
	if value == "" {
		value = def
	}
	return value, nil
}

type Choice struct{ Label, Value string }

func (a *App) Choose(choices []Choice, label string) (string, error) {
	if len(choices) == 0 {
		return "", errors.New("no options are available")
	}
	for i, c := range choices {
		fmt.Fprintf(a.Err, "%d. %s\n", i+1, c.Label)
	}
	for {
		s, err := a.Ask(label, "")
		if err != nil {
			return "", err
		}
		n, err := strconv.Atoi(s)
		if err == nil && n >= 1 && n <= len(choices) {
			return choices[n-1].Value, nil
		}
		fmt.Fprintln(a.Err, "Choose one of the displayed numbers, or type /cancel.")
	}

}
func Emit(w io.Writer, value any, raw bool) error {
	if raw {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		e.SetEscapeHTML(false)
		return e.Encode(value)
	}
	switch v := value.(type) {
	case Object:
		if query, ok := v["query"]; ok {
			fmt.Fprintf(w, "Search: %s\n", query)
			if message, ok := v["error"]; ok {
				fmt.Fprintf(w, "Error: %s\n\n", message)
				return nil
			}
			if total, ok := v["totalMatches"]; ok {
				fmt.Fprintf(w, "Matches: %v; page: %v; more: %v\n", total, v["page"], v["hasMore"])
			}
			return Emit(w, v["products"], false)
		}
		keys := []string{}
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v[k] == nil || text(v[k]) == "" {
				continue
			}
			switch v[k].(type) {
			case []any, Object:
				fmt.Fprintln(w, k+":")
				if err := Emit(w, v[k], false); err != nil {
					return err
				}
			default:
				fmt.Fprintf(w, "%s: %v\n", k, v[k])
			}
		}
		fmt.Fprintln(w)
	case []any:
		for _, item := range v {
			if err := Emit(w, item, false); err != nil {
				return err
			}
		}
	default:
		fmt.Fprintln(w, value)
	}
	return nil
}
func (a *App) Execute(ctx context.Context, args []string) error {
	ctx = context.WithValue(ctx, lockNoticeKey{}, a.Err)
	a.ctx = ctx
	o, err := Parse(args)
	if err != nil {
		return failure("invalid_arguments", err.Error(), 2, err)
	}
	if o.Help || o.Command == "" {
		fmt.Fprint(a.Out, usage)
		return nil
	}
	if o.Command == "version" {
		fmt.Fprintf(a.Out, "willys %s\n", Version)
		return nil
	}

	root, err := DataRoot()
	if err != nil {
		return err
	}
	p, err := NewProfile(root, o.Profile)
	if err != nil {
		return err
	}
	if o.Command == "cart" && o.Bools["open"] {
		if err := o.Arity(0, 0); err != nil {
			return err
		}
		if o.JSON {
			return errors.New("--open cannot be combined with --json")
		}
		return a.ServeCartPage(ctx, p, o.Profile)
	}
	release, err := commandLock(ctx, p, o)
	if err != nil {
		return err
	}
	defer release()
	if a.Interactive && (o.Command == "setup" || o.Command == "checkout" || (o.Command == "slots" && o.Bools["choose"])) {
		fmt.Fprintln(a.Err, "Press Ctrl+C or type /cancel to cancel.")
	}
	result, err := a.Run(ctx, p, o)
	if result != nil {
		if emitErr := Emit(a.Out, result, o.JSON); emitErr != nil {
			return emitErr
		}
	}
	return err
}
func paymentPending(p *Profile) error {
	v, err := p.Load("payment")
	if err != nil {
		return err
	}
	if v != nil {
		return failure("payment_unresolved", "This profile has a payment attempt. Run willys payment status before changing checkout.", 1, nil)
	}
	return nil
}
func (a *App) Run(ctx context.Context, p *Profile, o Options) (any, error) {
	if o.Command == "auth" && (len(o.Positionals) > 1 || (len(o.Positionals) == 1 && o.Positionals[0] != "login")) {
		return nil, errors.New("use willys auth to inspect your session, or willys auth login to sign in")
	}
	if o.Command == "session" {
		if err := o.Arity(0, 0); err != nil {
			return nil, err
		}
		if src := o.Values["import-cookies"]; src != "" {
			for _, name := range []string{"cookies.json", "cookies.txt"} {
				if _, err := os.Stat(filepath.Join(p.Path, name)); err == nil {
					return nil, errors.New("this profile already has cookies; import into a new --profile")
				}
			}
			store, err := NewCookieStore(p)
			if err != nil {
				return nil, err
			}
			if err = store.Import(src); err != nil {
				return nil, err
			}
			if err = store.Save(); err != nil {
				return nil, err
			}
		}
		return Object{"profile": o.Profile, "storage": p.Path}, nil
	}
	if o.Command == "payment" && len(o.Positionals) == 0 {
		return a.OpenSavedPayment(p, o)
	}
	// Establish one shared server session before concurrent requests start.
	initRelease, err := acquire(ctx, filepath.Join(p.Path, "session-init.lock"), false)
	if err != nil {
		return nil, err
	}
	connect := a.Connect
	if connect == nil {
		connect = NewClient
	}
	c, err := connect(p)
	if err == nil && a.Connect == nil {
		u, _ := url.Parse(BaseURL)
		hasCart := false
		for _, cookie := range c.Cookies.Cookies(u) {
			if cookie.Name == "willys-cart" {
				hasCart = true
			}
		}
		if !hasCart {
			_, err = c.Cart(ctx)
		}
	}
	initRelease()
	if err != nil {
		return nil, err
	}
	switch o.Command {
	case "auth":
		if len(o.Positionals) == 1 {
			return a.Login(ctx, c, p)
		}
		return a.AuthStatus(ctx, c)
	case "deals":
		return a.Deals(ctx, c, o)
	case "search":
		if err = o.Arity(1, 1); err != nil {
			return nil, err
		}
		page, e := o.Int("page", 0)
		if e != nil {
			return nil, e
		}
		limit, e := o.Int("limit", 20)
		if e != nil {
			return nil, e
		}
		if page < 0 || limit < 1 || limit > 100 {
			return nil, errors.New("page must be nonnegative; limit must be 1–100")
		}
		return a.Search(ctx, c, p, o.Positionals[0], page, limit)
	case "product":
		if err = o.Arity(1, 1); err != nil {
			return nil, err
		}
		d, e := c.Product(ctx, o.Positionals[0])
		if e != nil {
			return nil, e
		}
		return ProductView(d, nil, o.Bools["details"]), nil
	case "cart":
		if len(o.Positionals) == 1 && o.Positionals[0] == "reset" {
			return a.ResetCart(ctx, c, p)
		}
		if err = o.Arity(0, 0); err != nil {
			return nil, err
		}
		return c.CartView(ctx, o.Bools["details"])
	case "set", "remove":
		n := 1
		if o.Command == "set" {
			n = 2
		}
		if err = o.Arity(n, n); err != nil {
			return nil, err
		}
		quantity := 0.0
		if n == 2 {
			quantity, err = strconv.ParseFloat(o.Positionals[1], 64)
			if err != nil {
				return nil, errors.New("quantity must be a number")
			}
		}
		unit := o.Values["unit"]
		if unit == "" {
			unit = "pieces"
		}
		if unit != "pieces" && unit != "kilogram" {
			return nil, errors.New("unit must be pieces or kilogram")
		}
		if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 || (unit == "pieces" && math.Trunc(quantity) != quantity) {
			return nil, errors.New("use a finite nonnegative quantity; pieces must be whole numbers")
		}
		if err = paymentPending(p); err != nil {
			return nil, err
		}
		before, e := c.Cart(ctx)
		if e != nil {
			return nil, e
		}
		if text(before["orderReference"]) != "" {
			return nil, errors.New("editing existing orders is not supported")
		}
		_, err = c.Post(ctx, "/cart/addProducts", nil, Object{"products": []any{Object{"productCodePost": o.Positionals[0], "qty": quantity, "pickUnit": unit, "noReplacementFlag": false, "hideDiscountToolTip": false}}})
		if err != nil {
			return nil, err
		}
		view, e := c.CartView(ctx, o.Bools["details"])
		if e != nil {
			return nil, e
		}
		actual := 0.0
		for _, raw := range list(view["products"]) {
			row := obj(raw)
			if text(row["code"]) == o.Positionals[0] {
				actual = number(row["quantity"])
			}
		}
		if actual != quantity {
			return nil, fmt.Errorf("Willys saved quantity %g instead of %g; check the cart", actual, quantity)
		}
		return view, nil
	case "stores":
		if err = o.Arity(0, 1); err != nil {
			return nil, err
		}
		v, e := c.Get(ctx, "/store", nil)
		if e != nil {
			return nil, e
		}
		out := []any{}
		query := ""
		if len(o.Positionals) > 0 {
			query = strings.ToLower(o.Positionals[0])
		}
		for _, raw := range list(v) {
			s := obj(raw)
			address := obj(s["address"])
			if !o.Bools["all"] && text(s["name"]) == "" {
				continue
			}
			if o.Bools["pickup"] && !yes(s["clickAndCollect"]) {
				continue
			}
			hay := strings.ToLower(text(s["name"]) + " " + text(address["town"]) + " " + text(address["postalCode"]))
			if !strings.Contains(hay, query) {
				continue
			}
			if o.Bools["details"] {
				out = append(out, s)
			} else {
				out = append(out, Object{"id": s["storeId"], "name": s["name"], "town": address["town"], "address": address["formattedAddress"], "pickup": s["clickAndCollect"], "openNow": s["open"]})
			}
		}
		return out, nil
	case "setup", "slots", "slot":
		if err = paymentPending(p); err != nil {
			return nil, err
		}
		switch o.Command {
		case "setup":
			if err = o.Arity(0, 0); err != nil {
				return nil, err
			}
			return a.Setup(ctx, c, p, o)
		case "slots":
			if err = o.Arity(0, 0); err != nil {
				return nil, err
			}
			return a.Slots(ctx, c, p, o)
		default:
			if err = o.Arity(1, 1); err != nil {
				return nil, err
			}
			return a.SelectSlot(ctx, c, p, o.Positionals[0])
		}
	case "checkout":
		if err = o.Arity(0, 0); err != nil {
			return nil, err
		}
		return a.Checkout(ctx, c, p, o)
	case "payment":
		if len(o.Positionals) == 1 && (o.Positionals[0] == "status" || o.Positionals[0] == "recover") {
			if o.Bools["url"] {
				return nil, failure("invalid_arguments", "--url applies only to willys payment, without status or recover.", 2, nil)
			}
			return a.PaymentStatus(ctx, c, p, o.Positionals[0] == "recover")
		}
		if err = o.Arity(0, 0); err != nil {
			return nil, err
		}
		return a.OpenSavedPayment(p, o)
	}
	return nil, errors.New("unknown command")
}

func (a *App) OpenSavedPayment(p *Profile, o Options) (any, error) {
	saved, e := p.Load("payment")
	if e != nil {
		return nil, e
	}
	if saved == nil {
		return nil, errors.New("no saved payment; run willys checkout first")
	}
	u, e := PaymentURL(text(saved["location"]))
	if e != nil {
		return nil, errors.New("saved payment has no supported URL; resolve the attempt before retrying")
	}
	connect := a.Connect
	if connect == nil {
		connect = NewClient
	}
	client, err := connect(p)
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	canceled, err := client.canceledPayment(ctx, saved)
	if err != nil {
		return nil, err
	}
	if canceled {
		return nil, errors.New("this payment was canceled; run willys payment recover, then checkout to start a new payment")
	}
	if !o.Bools["url"] {
		if e = a.Open(u); e != nil {
			return nil, fmt.Errorf("payment URL is saved; use willys payment --url: %w", e)
		}
	}
	return Object{"url": u, "state": "payment completion is not verified by this CLI"}, nil
}
