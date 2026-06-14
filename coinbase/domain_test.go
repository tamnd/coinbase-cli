package coinbase

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network.
// The client's HTTP behaviour is covered in coinbase_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "coinbase" {
		t.Errorf("Scheme = %q, want coinbase", info.Scheme)
	}
	if len(info.Hosts) == 0 {
		t.Error("Hosts is empty")
	}
	if info.Identity.Binary != "coinbase" {
		t.Errorf("Identity.Binary = %q, want coinbase", info.Identity.Binary)
	}
}

func TestClassify_Pair(t *testing.T) {
	cases := []struct {
		in      string
		wantTyp string
		wantID  string
	}{
		{"BTC-USD", "pair", "BTC-USD"},
		{"eth-eur", "pair", "ETH-EUR"},
		{"BTC", "currency", "BTC"},
		{"eth", "currency", "ETH"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q) error: %v", tc.in, err)
			continue
		}
		if typ != tc.wantTyp || id != tc.wantID {
			t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)", tc.in, typ, id, tc.wantTyp, tc.wantID)
		}
	}
}

func TestClassify_Empty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") expected error, got nil")
	}
}

func TestLocate_Pair(t *testing.T) {
	got, err := Domain{}.Locate("pair", "BTC-USD")
	if err != nil {
		t.Fatalf("Locate pair: %v", err)
	}
	want := "https://www.coinbase.com/price/btc"
	if got != want {
		t.Errorf("Locate(pair, BTC-USD) = %q, want %q", got, want)
	}
}

func TestLocate_Currency(t *testing.T) {
	got, err := Domain{}.Locate("currency", "ETH")
	if err != nil {
		t.Fatalf("Locate currency: %v", err)
	}
	want := "https://www.coinbase.com/price/eth"
	if got != want {
		t.Errorf("Locate(currency, ETH) = %q, want %q", got, want)
	}
}

func TestLocate_Unknown(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "x")
	if err == nil {
		t.Error("Locate(unknown) expected error, got nil")
	}
}

func TestLooksLikePair(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"BTC-USD", true},
		{"eth-eur", true},
		{"BTC", false},
		{"X-Y", false},
		{"-USD", false},
		{"BTC-", false},
		{"", false},
	}
	for _, tc := range cases {
		got := looksLikePair(tc.in)
		if got != tc.want {
			t.Errorf("looksLikePair(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the coinbase domain is registered.
	if _, ok := h.Domain("coinbase"); !ok {
		t.Fatal("coinbase domain not registered")
	}
}
