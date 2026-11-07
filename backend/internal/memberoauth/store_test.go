package memberoauth

import (
	"encoding/base64"
	"testing"
)

func TestVaultSiteIsolation(t *testing.T) {
	v, e := NewVault(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := v.Seal(7, "client-secret")
	if e != nil {
		t.Fatal(e)
	}
	if got, e := v.Open(7, encrypted); e != nil || got != "client-secret" {
		t.Fatal("cannot decrypt own site")
	}
	if _, e = v.Open(8, encrypted); e == nil {
		t.Fatal("cross-site secret decrypted")
	}
	other := make([]byte, 32)
	other[0] = 1
	wrong, e := NewVault(base64.StdEncoding.EncodeToString(other))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wrong.Open(7, encrypted); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = v.Open(7, "bad"); e == nil {
		t.Fatal("invalid ciphertext accepted")
	}
	if _, e = NewVault("weak-key"); e == nil {
		t.Fatal("weak key accepted")
	}
}
func TestValidateConfig(t *testing.T) {
	base := Config{Enabled: true, ClientID: "client.apps.googleusercontent.com", Encrypted: "ciphertext", PublicURL: "https://example.com"}
	if e := Validate(base); e != nil {
		t.Fatal(e)
	}
	for _, origin := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?q=1", "https://example.com#fragment", "javascript:alert(1)", "https://example.com?", "https://example.com#"} {
		c := base
		c.PublicURL = origin
		if e := Validate(c); e == nil {
			t.Errorf("accepted %q", origin)
		}
	}
	for _, origin := range []string{"http://localhost:18080", "http://127.0.0.1:18080", "http://[::1]:18080"} {
		c := base
		c.PublicURL = origin
		if e := Validate(c); e != nil {
			t.Errorf("rejected development origin %q: %v", origin, e)
		}
	}
}
