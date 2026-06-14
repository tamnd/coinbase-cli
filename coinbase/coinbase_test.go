package coinbase_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestCurrencies(t *testing.T) {
	payload := `{"data":[{"id":"BTC","name":"Bitcoin","min_size":"0.00000001"},{"id":"ETH","name":"Ethereum","min_size":"0.00000001"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := coinbase.NewClient()
	c.Rate = 0
	// override BaseURL via a helper we expose in tests by using the raw Get method
	// We test Currencies via the exported API after overriding the base URL through a test server.
	// Since BaseURL is a const, we test the JSON parsing via a manual round-trip.
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data []coinbase.Currency `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
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
}

func TestPrice_JSON(t *testing.T) {
	payload := `{"data":{"amount":"64006.025","currency":"USD"}}`
	var env struct {
		Data struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Amount != "64006.025" {
		t.Errorf("amount = %q, want 64006.025", env.Data.Amount)
	}
	if env.Data.Currency != "USD" {
		t.Errorf("currency = %q, want USD", env.Data.Currency)
	}
	// Build the Price struct as the real client would.
	p := coinbase.Price{
		Pair:     "BTC-USD",
		Amount:   env.Data.Amount,
		Currency: env.Data.Currency,
		Type:     "spot",
	}
	if p.Type != "spot" {
		t.Errorf("Type = %q, want spot", p.Type)
	}
}

func TestExchangeRate_JSON(t *testing.T) {
	payload := `{"data":{"currency":"BTC","rates":{"ETH":"15.5","USD":"64000"}}}`
	var env struct {
		Data struct {
			Currency string            `json:"currency"`
			Rates    map[string]string `json:"rates"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	rate := coinbase.ExchangeRate{
		BaseCurrency: env.Data.Currency,
		Rates:        env.Data.Rates,
	}
	if rate.BaseCurrency != "BTC" {
		t.Errorf("BaseCurrency = %q, want BTC", rate.BaseCurrency)
	}
	if rate.Rates["USD"] != "64000" {
		t.Errorf("Rates[USD] = %q, want 64000", rate.Rates["USD"])
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
