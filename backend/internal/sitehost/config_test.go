package sitehost

import "testing"

func TestFromURL(t *testing.T) {
	for input, expected := range map[string]string{
		"https://www.example.com/": "www.example.com",
		"www.example.com":          "www.example.com",
		"":                         "",
	} {
		if actual := FromURL(input); actual != expected {
			t.Fatalf("FromURL(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestMatches(t *testing.T) {
	if !Matches("WWW.EXAMPLE.COM", "www.example.com") {
		t.Fatal("host matching should be case insensitive")
	}
	if Matches("img.example.com", "www.example.com") {
		t.Fatal("different hosts must not match")
	}
}
