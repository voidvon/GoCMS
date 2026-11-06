package payment

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
)

func TestMoneyPrecisionAndBounds(t *testing.T) {
	for _, tc := range []struct {
		value, currency string
		want            int64
	}{{"12.34", "USD", 1234}, {"12.340000", "USD", 1234}, {"0.01", "EUR", 1}, {"12.000000", "JPY", 12}, {"001.20", "GBP", 120}} {
		got, err := ParseAmount(tc.value, tc.currency)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %d %v", tc, got, err)
		}
		formatted, err := FormatAmount(got, tc.currency)
		if err != nil {
			t.Fatal(err)
		}
		round, err := ParseAmount(formatted, tc.currency)
		if err != nil || round != got {
			t.Fatal("money failed round trip")
		}
	}
	for _, value := range []string{"-1", "+1", "1e2", "NaN", "1.001", " 1", "9223372036854775808", "1000000000.00", "1.2.3"} {
		if _, err := ParseAmount(value, "USD"); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if _, err := ParseAmount("1.01", "JPY"); err == nil {
		t.Fatal("fractional JPY accepted")
	}
	if _, err := ParseAmount("1", "XXX"); err == nil {
		t.Fatal("unknown currency accepted")
	}
}

func TestCipherConcurrentInitializationAndUse(t *testing.T) {
	c := &Cipher{}
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Configured(); _, _ = c.Seal("salt", "1:account"); _ = c.Initialize(key) }()
	}
	wg.Wait()
	if !c.Configured() {
		t.Fatal("key not installed")
	}
	if err := c.Initialize(key); err == nil {
		t.Fatal("key overwritten")
	}
	raw, err := c.Seal("salt", "1:account")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := c.Open(raw, "1:account"); err != nil || value != "salt" {
		t.Fatal("initialized key not usable")
	}
}

func TestCredentialEncryptionBindsAccountAndKey(t *testing.T) {
	c, err := NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.Seal("secret-salt", "1:account")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Seal("secret-salt", "1:account")
	if err != nil || a == b {
		t.Fatal("nonce reused")
	}
	if strings.Contains(a, "secret-salt") {
		t.Fatal("plaintext credential")
	}
	if plain, err := c.Open(a, "1:account"); err != nil || plain != "secret-salt" {
		t.Fatal("cannot decrypt credential")
	}
	if _, err := c.Open(a, "2:account"); err == nil {
		t.Fatal("cross-site credential transplant accepted")
	}
	if _, err := NewCipher("weak-key"); err == nil {
		t.Fatal("weak key accepted")
	}
	var missing *Cipher
	if _, err := missing.Seal("x", "y"); err == nil {
		t.Fatal("missing key accepted")
	}
}
