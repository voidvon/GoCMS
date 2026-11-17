package security

import (
	"net/http/httptest"
	"testing"
)

func TestResolveIPTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		remote, xff string
		proxies     []string
		want        string
	}{
		{"203.0.113.1:10", "1.1.1.1", nil, "203.0.113.1"},
		{"127.0.0.1:10", "1.1.1.1, 203.0.113.1", []string{"127.0.0.1/32"}, "203.0.113.1"},
		{"127.0.0.1:10", "203.0.113.1, 10.0.0.5", []string{"127.0.0.1/32", "10.0.0.0/8"}, "203.0.113.1"},
		{"127.0.0.1:10", "garbage, 203.0.113.1", []string{"127.0.0.1/32"}, "127.0.0.1"},
		{"[::1]:10", "2001:db8::1", []string{"::1/128"}, "2001:db8::1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.xff)
		if got := ResolveIP(r, tc.proxies).String(); got != tc.want {
			t.Fatalf("remote=%s xff=%s got=%s want=%s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

func TestProtectionInputValidation(t *testing.T) {
	policy := DefaultPolicy()
	if err := ValidatePolicy(&policy); err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{-1, 1, 59, 30*86400 + 1} {
		p := policy
		p.BanSeconds = seconds
		if ValidatePolicy(&p) == nil {
			t.Fatalf("invalid ban duration accepted: %d", seconds)
		}
	}
	invalidScope := policy
	invalidScope.BanScope = "unknown"
	if ValidatePolicy(&invalidScope) == nil {
		t.Fatal("invalid automatic ban scope accepted")
	}
	for _, v := range []string{"/server.key", "/.env.production", "/.git/config", "/directory/../server.key", "//SERVER.KEY/"} {
		if !Matches(policy, v) {
			t.Fatalf("missed %s", v)
		}
	}
	for _, v := range []string{"/document.php", "/ordinary-404", "/server.key/article"} {
		if Matches(policy, v) {
			t.Fatalf("unexpected match %s", v)
		}
	}
	p := DefaultPlatform()
	if err := ValidatePlatform(&p); err != nil {
		t.Fatal(err)
	}
	p.TrustedProxies = []string{"0.0.0.0/0"}
	if ValidatePlatform(&p) == nil {
		t.Fatal("trust-all proxy accepted")
	}
	for _, n := range []string{"1.1.1.1;deny all", "1.1.1.1\nallow all", "::ffff:1.1.1.1/32"} {
		if _, err := Network(n); err == nil {
			t.Fatalf("invalid network accepted %q", n)
		}
	}
}
