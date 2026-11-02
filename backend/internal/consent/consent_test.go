package consent

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPolicyValidationAndLanguageOverrides(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{"enabled":true} {}`, `{"enable":true}`, `{"max_age_days":0}`, `{"version":" "}`,
		`{"purposes":["unknown"]}`, `{"purposes":["analytics","analytics"]}`, `{"languages":{"fr":{"mode":"auto"}}}`,
		`{"text":{"privacy_url":"javascript:alert(1)"}}`, `{"text":{"privacy_url":"//tracker.test"}}`,
		`{"text":{"cookie_url":"/\\tracker.test"}}`, `{"text":{"cookie_url":"relative.html"}}`,
	} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted invalid policy: %s", raw)
		}
	}
	c, err := Parse(`{"enabled":true,"text":{"title":"Shared","privacy_url":"/privacy"},"languages":{"de":{"mode":"off"},"fr":{"text":{"title":"Votre choix"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resolve(2, "de").Enabled || !c.Resolve(2, "fr").Enabled || !c.Resolve(2, "ar").Enabled {
		t.Fatal("language inheritance failed")
	}
	if got := c.Resolve(2, "fr"); got.Text["title"] != "Votre choix" || got.Text["privacy_url"] != "/privacy" || got.SiteID != 2 {
		t.Fatalf("incorrect override: %+v", got)
	}
	if c.Resolve(2, "fr").Revision != c.Resolve(2, "ar").Revision {
		t.Fatal("translations must share consent")
	}
	old := c.Resolve(2, "fr").Revision
	c.Version = "2"
	if old == c.Resolve(2, "fr").Revision {
		t.Fatal("policy version changes must invalidate consent")
	}
	old = c.Resolve(2, "fr").Revision
	c.Purposes = []string{"analytics"}
	if old == c.Resolve(2, "fr").Revision {
		t.Fatal("purpose changes must invalidate consent")
	}
	if empty, err := Parse(`{"purposes":null}`); err != nil || empty.Purposes == nil {
		t.Fatal("null purposes must normalize to an empty array")
	}
}

func TestHTMLBlocksResourcesBeforeBrowserDiscovery(t *testing.T) {
	c, _ := Parse(`{"enabled":true}`)
	page := []byte(`<!doctype html><html><head><script src="/essential.js"></script><script data-consent="analytics" type="module" src="https://tracker.test/a.js"></script></head><body><script data-consent="marketing">window.tracking = true</script><iframe data-consent="marketing" src="https://tracker.test/frame" srcdoc="&lt;p&gt;embed&lt;/p&gt;"></iframe><img data-consent="analytics" src="https://tracker.test/pixel" srcset="https://tracker.test/2x 2x"><script type="text/plain" data-consent="preferences" data-consent-src="/preferences.js"></script></body></html>`)
	out, err := Process(page, c.Resolve(3, "en"))
	if err != nil {
		t.Fatal(err)
	}
	z := html.NewTokenizer(bytes.NewReader(out))
	blocked := 0
	for z.Next() != html.ErrorToken {
		token := z.Token()
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		if attrs["data-consent"] == "" {
			continue
		}
		blocked++
		for _, key := range []string{"src", "srcdoc", "srcset"} {
			if _, ok := attrs[key]; ok {
				t.Fatalf("active %s on %s", key, token.Data)
			}
		}
		if token.Data == "script" && attrs["type"] != "text/plain" {
			t.Fatal("script is executable before consent")
		}
	}
	if blocked != 5 {
		t.Fatalf("blocked %d resources, want 5", blocked)
	}
	if !bytes.Contains(out, []byte(`<script src="/essential.js">`)) {
		t.Fatal("necessary scripts changed")
	}
	if !bytes.Contains(out, []byte(`data-consent-type="module"`)) {
		t.Fatal("module type lost")
	}
	if bytes.Index(out, []byte(`id="gocms-consent-config"`)) > bytes.Index(out, []byte(`</body>`)) {
		t.Fatal("bootstrap outside body")
	}
}

func TestHTMLDisabledPolicyStillBlocksMarkedResources(t *testing.T) {
	c, _ := Parse("")
	plain := []byte("<BODY>unchanged</BODY>")
	out, err := Process(plain, c.Resolve(1, "en"))
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatal("disabled policy changed unmarked page")
	}
	out, err = Process([]byte(`<script data-consent="analytics" src="/track.js"></script>`), c.Resolve(1, "en"))
	if err != nil || !bytes.Contains(out, []byte(`type="text/plain"`)) || !bytes.Contains(out, []byte(`"enabled":false`)) {
		t.Fatal("disabled interface must not imply consent")
	}
	for _, input := range []string{`<script data-consent="unknown"></script>`, `<link data-consent="analytics" href="/track">`, `<script data-consent="analytics" src="/a" data-consent-src="/b"></script>`} {
		if _, err := Process([]byte(input), c.Resolve(1, "en")); err == nil {
			t.Fatalf("invalid integration must fail publication: %s", input)
		}
	}
}

func TestPolicyTextCannotEscapeJSONScript(t *testing.T) {
	c, err := Parse(`{"enabled":true,"text":{"title":"</script><script>alert(1)</script>"}}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Process([]byte(`<body></body>`), c.Resolve(1, "en"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `</script><script>alert(1)`) {
		t.Fatal("text escaped JSON container")
	}
}
