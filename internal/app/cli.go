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

	"github.com/gofrs/flock"
)

var Version = "dev"

const usage = `willys — shop at Willys through HTTP

Usage: willys [--profile NAME] [--json] COMMAND [OPTIONS]

Commands:
  search QUERY [--page N] [--limit N]    Search products
  product CODE [--details]              Show a product
  cart [--details]                      Show the cart and payment reservation
  set CODE QUANTITY [--unit UNIT]       Set the final quantity
  remove CODE [--unit UNIT]             Remove a product
  stores [QUERY] [--pickup] [--all] [--details]
  setup                                Save contact and delivery details
  slots [--choose]                      List available delivery or pickup times
  slot NUMBER                          Reserve a time from the latest list
  checkout                             Choose a payment method and start payment
  payment [--url]                       Reopen the existing payment URL
  session [--import-cookies FILE]       Show storage or import Netscape cookies
  version                              Show the version

Global options: --profile NAME, --json, --help, --version
Use willys COMMAND --help for command options.
`

var commandOptions = map[string]map[string]bool{
	"search": {"page": false, "limit": false}, "product": {"details": true}, "cart": {"details": true},
	"set": {"unit": false}, "remove": {"unit": false}, "stores": {"pickup": true, "all": true, "details": true},
	"setup": {"first-name": false, "last-name": false, "phone": false, "email": false, "street": false, "postcode": false, "town": false, "store": false, "mode": false},
	"slots": {"choose": true}, "slot": {}, "checkout": {"method": false, "no-open": true, "yes": true, "expected-total": false, "expected-reservation": false},
	"payment": {"url": true}, "session": {"import-cookies": false}, "version": {},
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
	Connect     func(*Profile) (*Client, error)
	In          *bufio.Reader
	Out, Err    io.Writer
	Interactive bool
	Open        func(string) error
	Now         func() time.Time
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
	value, err := a.In.ReadString('\n')
	if err != nil {
		return "", err
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
	s, err := a.Ask(label, "")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > len(choices) {
		return "", errors.New("choose one of the displayed numbers")
	}
	return choices[n-1].Value, nil
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
	o, err := Parse(args)
	if err != nil {
		return err
	}
	if o.Command == "version" {
		fmt.Fprintf(a.Out, "willys %s\n", Version)
		return nil
	}
	if o.Help || o.Command == "" {
		fmt.Fprint(a.Out, usage)
		if m, ok := commandOptions[o.Command]; ok {
			fmt.Fprintf(a.Out, "\n%s options:\n", o.Command)
			keys := []string{}
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				tail := " VALUE"
				if m[k] {
					tail = ""
				}
				fmt.Fprintf(a.Out, "  --%s%s\n", k, tail)
			}
		}
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
	lock := flock.New(filepath.Join(p.Path, "profile.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("another command is using this profile; wait for it to finish")
	}
	defer lock.Close()
	if err = os.Chmod(filepath.Join(p.Path, "profile.lock"), 0600); err != nil {
		return err
	}
	result, err := a.Run(ctx, p, o)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	return Emit(a.Out, result, o.JSON)
}
func paymentPending(p *Profile) error {
	v, err := p.Load("payment")
	if err != nil {
		return err
	}
	if v != nil {
		return errors.New("this profile has a payment attempt; use willys payment and resolve it before changing checkout")
	}
	return nil
}
func (a *App) Run(ctx context.Context, p *Profile, o Options) (any, error) {
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
	connect := a.Connect
	if connect == nil {
		connect = NewClient
	}
	c, err := connect(p)
	if err != nil {
		return nil, err
	}
	switch o.Command {
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
		v, e := c.Get(ctx, "/search/clean", url.Values{"q": {o.Positionals[0]}, "page": {strconv.Itoa(page)}, "size": {strconv.Itoa(limit)}})
		if e != nil {
			return nil, e
		}
		out := []any{}
		for _, raw := range list(obj(v)["results"]) {
			out = append(out, ProductView(obj(raw), nil, false))
		}
		return out, nil
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
		if err = o.Arity(0, 0); err != nil {
			return nil, err
		}
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
		if !o.Bools["url"] {
			if e = a.Open(u); e != nil {
				return nil, fmt.Errorf("payment URL is saved; use willys payment --url: %w", e)
			}
		}
		return Object{"url": u, "state": "payment completion is not verified by this CLI"}, nil
	}
	return nil, errors.New("unknown command")
}
