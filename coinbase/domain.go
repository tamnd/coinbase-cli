package coinbase

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes coinbase as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/coinbase-cli/coinbase"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// coinbase:// URIs by routing to the operations Register installs.
func init() { kit.Register(Domain{}) }

// Domain is the coinbase driver.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "coinbase",
		Hosts:  []string{"coinbase.com", "www.coinbase.com", Host},
		Identity: kit.Identity{
			Binary: "coinbase",
			Short:  "A command line for Coinbase public data.",
			Long: `A command line for Coinbase public data.

coinbase reads public Coinbase API data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key, nothing to run alongside it.`,
			Site: "coinbase.com",
			Repo: "https://github.com/tamnd/coinbase-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "spot", Group: "read", Single: true,
		Summary: "Get spot price for a pair (e.g. BTC-USD)",
		Args:    []kit.Arg{{Name: "pair", Help: "currency pair e.g. BTC-USD"}}},
		spotOp)

	kit.Handle(app, kit.OpMeta{Name: "buy", Group: "read", Single: true,
		Summary: "Get buy price for a pair (e.g. BTC-USD)",
		Args:    []kit.Arg{{Name: "pair", Help: "currency pair e.g. BTC-USD"}}},
		buyOp)

	kit.Handle(app, kit.OpMeta{Name: "rates", Group: "read", List: true,
		Summary: "Get exchange rates for a base currency (e.g. BTC)",
		Args:    []kit.Arg{{Name: "currency", Help: "base currency e.g. BTC"}}},
		ratesOp)

	kit.Handle(app, kit.OpMeta{Name: "currencies", Group: "read", List: true,
		Summary: "List all supported fiat currencies", URIType: "currencies"},
		currenciesOp)
}

// newClient builds the Client from the host-resolved kit.Config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- input structs ---

type spotInput struct {
	Pair   string  `kit:"arg" help:"currency pair e.g. BTC-USD"`
	Client *Client `kit:"inject"`
}

type buyInput struct {
	Pair   string  `kit:"arg" help:"currency pair e.g. BTC-USD"`
	Client *Client `kit:"inject"`
}

type ratesInput struct {
	Currency string  `kit:"arg" help:"base currency e.g. BTC"`
	To       string  `kit:"flag" help:"comma-separated target currencies to filter (default: all)"`
	Client   *Client `kit:"inject"`
}

type currenciesInput struct {
	Crypto bool    `kit:"flag" help:"list crypto currencies instead of fiat"`
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func spotOp(ctx context.Context, in spotInput, emit func(*Price) error) error {
	result, err := in.Client.Spot(ctx, in.Pair)
	if err != nil {
		return err
	}
	return emit(result)
}

func buyOp(ctx context.Context, in buyInput, emit func(*Price) error) error {
	result, err := in.Client.Buy(ctx, in.Pair)
	if err != nil {
		return err
	}
	return emit(result)
}

func ratesOp(ctx context.Context, in ratesInput, emit func(*Rate) error) error {
	rates, err := in.Client.Rates(ctx, in.Currency)
	if err != nil {
		return err
	}

	// Build a filter set from --to if provided.
	var filter map[string]bool
	if in.To != "" {
		filter = make(map[string]bool)
		for _, t := range strings.Split(in.To, ",") {
			t = strings.TrimSpace(strings.ToUpper(t))
			if t != "" {
				filter[t] = true
			}
		}
	}

	for i := range rates {
		if filter != nil && !filter[rates[i].Target] {
			continue
		}
		if err := emit(&rates[i]); err != nil {
			return err
		}
	}
	return nil
}

func currenciesOp(ctx context.Context, in currenciesInput, emit func(*Currency) error) error {
	var currencies []Currency
	var err error
	if in.Crypto {
		currencies, err = in.Client.CryptoCurrencies(ctx)
	} else {
		currencies, err = in.Client.Currencies(ctx)
	}
	if err != nil {
		return err
	}
	for i := range currencies {
		if in.Limit > 0 && i >= in.Limit {
			break
		}
		if err := emit(&currencies[i]); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: URI driver string functions (no network) ---

// Classify turns any accepted input into the canonical (type, id).
// A pair like "BTC-USD" maps to ("pair", "BTC-USD");
// a bare currency like "BTC" maps to ("currency", "BTC").
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty coinbase reference")
	}
	if looksLikePair(input) {
		return "pair", strings.ToUpper(input), nil
	}
	return "currency", strings.ToUpper(input), nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "pair":
		base := id
		if idx := strings.Index(id, "-"); idx != -1 {
			base = id[:idx]
		}
		return "https://www.coinbase.com/price/" + strings.ToLower(base), nil
	case "currency":
		return "https://www.coinbase.com/price/" + strings.ToLower(id), nil
	case "currencies":
		return "https://www.coinbase.com/explore", nil
	default:
		return "", errs.Usage("coinbase has no resource type %q", uriType)
	}
}

// --- helpers ---

// looksLikePair returns true for inputs like "BTC-USD" or "eth-eur".
func looksLikePair(input string) bool {
	parts := strings.SplitN(input, "-", 2)
	return len(parts) == 2 && len(parts[0]) >= 2 && len(parts[1]) >= 2
}
