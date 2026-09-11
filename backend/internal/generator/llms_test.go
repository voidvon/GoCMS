package generator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLMSPublishedPages(t *testing.T) {
	web, data := t.TempDir(), t.TempDir()
	writeTemplate(t, web, "index.html", `<html><head><title>Example &amp; Company</title><meta content="Public &quot;guide&quot;" name="description"></head><body>private body</body></html>`)
	writeTemplate(t, web, "en/arbitrary/a (1).html", `<head><title>English [title]</title><meta NAME="DESCRIPTION" content="Line&#10;two"></head>`)
	writeTemplate(t, web, "sitemap.html", "ignored")
	writeTemplate(t, web, "secret.txt", "ignored")
	p := Publisher{Web: web, Data: data}
	name, err := p.GenerateLLMS(context.Background())
	if err != nil || name != "llms.txt" {
		t.Fatalf("%s: %v", name, err)
	}
	got := readGenerated(t, web, name)
	for _, want := range []string{"# Example & Company", `> Public "guide"`, `[English \[title\]](</en/arbitrary/a%20%281%29.html>): Line two`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"private body", "sitemap.html", "secret.txt"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("unexpected %q", forbidden)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GenerateLLMS(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if readGenerated(t, web, name) != got {
		t.Fatal("failed generation changed guide")
	}
	lock, err := os.OpenFile(filepath.Join(data, "publish.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unlock, err := acquirePublishLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := p.GenerateLLMS(context.Background()); !errors.Is(err, ErrPublishBusy) {
		t.Fatalf("lock: %v", err)
	}
}

func TestLLMSMetadataAndURL(t *testing.T) {
	open := func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`<title>A</title><body><title>Wrong</title>`)), nil
	}
	body, err := renderLLMS(context.Background(), "https://example.test/", []string{"index.html"}, open)
	if err != nil || !strings.Contains(string(body), "[A](<https://example.test/index.html>)") {
		t.Fatalf("%s: %v", body, err)
	}
	if _, err := renderLLMS(context.Background(), "javascript:alert(1)", nil, open); err == nil {
		t.Fatal("invalid origin accepted")
	}
}
