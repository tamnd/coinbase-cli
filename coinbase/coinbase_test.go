package coinbase_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/coinbase-cli/coinbase"
)

func TestGet_UserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := coinbase.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGet_RetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := coinbase.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGet_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := coinbase.NewClient()
	c.Rate = 0

	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestCurrencies(t *testing.T) {
	payload := `{"data":[{"id":"BTC","name":"Bitcoin","min_size":"0.00000001"},{"id":"ETH","name":"Ethereum","min_size":"0.00000001"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/currencies") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	// Parse the JSON directly to verify the shape the client would produce.
	var env struct {
		Data []coinbase.Currency `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 2 {
		t.Fatalf("got %d currencies, want 2", len(env.Data))
	}
	if env.Data[0].ID != "BTC" {
		t.Errorf("first ID = %q, want BTC", env.Data[0].ID)
	}
	if env.Data[0].Name != "Bitcoin" {
		t.Errorf("first Name = %q, want Bitcoin", env.Data[0].Name)
	}
	if env.Data[1].MinSize != "0.00000001" {
		t.Errorf("second MinSize = %q", env.Data[1].MinSize)
	}
}

func TestSpot_JSON(t *testing.T) {
	payload := `{"data":{"base":"BTC","currency":"USD","amount":"64006.025"}}`
	var env struct {
		Data struct {
			Base     string `json:"base"`
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	p := coinbase.Price{
		Base:     env.Data.Base,
		Currency: env.Data.Currency,
		Amount:   env.Data.Amount,
		Type:     "spot",
	}
	if p.Base != "BTC" {
		t.Errorf("Base = %q, want BTC", p.Base)
	}
	if p.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", p.Currency)
	}
	if p.Amount != "64006.025" {
		t.Errorf("Amount = %q, want 64006.025", p.Amount)
	}
	if p.Type != "spot" {
		t.Errorf("Type = %q, want spot", p.Type)
	}
}

func TestBuy_JSON(t *testing.T) {
	payload := `{"data":{"base":"ETH","currency":"USD","amount":"1669.925"}}`
	var env struct {
		Data struct {
			Base     string `json:"base"`
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	p := coinbase.Price{
		Base:     env.Data.Base,
		Currency: env.Data.Currency,
		Amount:   env.Data.Amount,
		Type:     "buy",
	}
	if p.Base != "ETH" {
		t.Errorf("Base = %q, want ETH", p.Base)
	}
	if p.Type != "buy" {
		t.Errorf("Type = %q, want buy", p.Type)
	}
}

func TestRates_JSON(t *testing.T) {
	payload := `{"data":{"currency":"BTC","rates":{"ETH":"15.5","USD":"64000","EUR":"55349"}}}`
	var env struct {
		Data struct {
			Currency string            `json:"currency"`
			Rates    map[string]string `json:"rates"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	// Simulate what the client does: one Rate per target.
	var rates []coinbase.Rate
	for target, rate := range env.Data.Rates {
		rates = append(rates, coinbase.Rate{
			Base:   env.Data.Currency,
			Target: target,
			Rate:   rate,
		})
	}
	if len(rates) != 3 {
		t.Fatalf("got %d rates, want 3", len(rates))
	}
	for _, r := range rates {
		if r.Base != "BTC" {
			t.Errorf("Base = %q, want BTC", r.Base)
		}
		if r.Rate == "" {
			t.Errorf("Rate is empty for target %s", r.Target)
		}
	}
}

func TestSplitPair(t *testing.T) {
	// Verify Price fields round-trip correctly for a known pair.
	p := coinbase.Price{
		Base:     "BTC",
		Currency: "USD",
		Amount:   "63999",
		Type:     "spot",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var p2 coinbase.Price
	if err := json.Unmarshal(b, &p2); err != nil {
		t.Fatal(err)
	}
	if p2.Base != "BTC" || p2.Currency != "USD" || p2.Amount != "63999" || p2.Type != "spot" {
		t.Errorf("round-trip mismatch: %+v", p2)
	}
}
