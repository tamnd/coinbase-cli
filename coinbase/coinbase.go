// Package coinbase is the library behind the coinbase command line:
// the HTTP client, request shaping, and the typed data models for the
// Coinbase public v2 API (api.coinbase.com/v2). No API key is required
// for the read-only endpoints this package uses.
package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to Coinbase.
const DefaultUserAgent = "coinbase-cli/dev (+https://github.com/tamnd/coinbase-cli)"

// Host is the Coinbase API host (used by the URI driver).
const Host = "api.coinbase.com"

// BaseURL is the Coinbase v2 API base.
const BaseURL = "https://api.coinbase.com/v2"

// Config holds optional overrides for NewClient.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Retries:   3,
		Timeout:   10 * time.Second,
	}
}

// Client talks to the Coinbase public v2 API over HTTPS.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	Rate      time.Duration
	Retries   int

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Get fetches url and returns the response body, pacing and retrying as configured.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- output types ---

// Currency is one entry from /v2/currencies.
type Currency struct {
	ID      string `kit:"id" json:"id"`
	Name    string `json:"name"`
	MinSize string `json:"min_size"`
}

// Price holds spot or buy price data from /v2/prices/<pair>/<type>.
type Price struct {
	Base     string `kit:"id" json:"base"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Type     string `json:"type"` // "spot" or "buy"
}

// Rate is one exchange-rate row from /v2/exchange-rates.
type Rate struct {
	Base   string `kit:"id" json:"base"`
	Target string `json:"target"`
	Rate   string `json:"rate"`
}

// --- wire envelopes ---

type currenciesEnv struct {
	Data []Currency `json:"data"`
}

type priceEnv struct {
	Data struct {
		Base     string `json:"base"`
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"data"`
}

type ratesEnv struct {
	Data struct {
		Currency string            `json:"currency"`
		Rates    map[string]string `json:"rates"`
	} `json:"data"`
}

// --- API methods ---

// Currencies returns all supported currencies from /v2/currencies.
func (c *Client) Currencies(ctx context.Context) ([]Currency, error) {
	body, err := c.Get(ctx, BaseURL+"/currencies")
	if err != nil {
		return nil, err
	}
	var env currenciesEnv
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("currencies: %w", err)
	}
	return env.Data, nil
}

// Spot returns the spot price for a currency pair such as "BTC-USD".
func (c *Client) Spot(ctx context.Context, pair string) (*Price, error) {
	return c.fetchPrice(ctx, pair, "spot")
}

// Buy returns the buy price for a currency pair such as "BTC-USD".
func (c *Client) Buy(ctx context.Context, pair string) (*Price, error) {
	return c.fetchPrice(ctx, pair, "buy")
}

func (c *Client) fetchPrice(ctx context.Context, pair, priceType string) (*Price, error) {
	url := fmt.Sprintf("%s/prices/%s/%s", BaseURL, pair, priceType)
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var env priceEnv
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("price %s/%s: %w", pair, priceType, err)
	}
	// The API returns base and currency in the response body.
	base := env.Data.Base
	currency := env.Data.Currency
	// Fall back to parsing the pair if the fields are missing.
	if base == "" || currency == "" {
		parts := splitPair(pair)
		if base == "" {
			base = parts[0]
		}
		if currency == "" && len(parts) > 1 {
			currency = parts[1]
		}
	}
	return &Price{
		Base:     base,
		Currency: currency,
		Amount:   env.Data.Amount,
		Type:     priceType,
	}, nil
}

// Rates returns exchange rates for a base currency such as "BTC".
// Each returned Rate has Base, Target, and Rate fields.
func (c *Client) Rates(ctx context.Context, currency string) ([]Rate, error) {
	url := fmt.Sprintf("%s/exchange-rates?currency=%s", BaseURL, currency)
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var env ratesEnv
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("exchange-rates %s: %w", currency, err)
	}
	rates := make([]Rate, 0, len(env.Data.Rates))
	for target, rate := range env.Data.Rates {
		rates = append(rates, Rate{
			Base:   env.Data.Currency,
			Target: target,
			Rate:   rate,
		})
	}
	return rates, nil
}

// splitPair splits "BTC-USD" into ["BTC", "USD"].
func splitPair(pair string) []string {
	for i, ch := range pair {
		if ch == '-' {
			return []string{pair[:i], pair[i+1:]}
		}
	}
	return []string{pair}
}
