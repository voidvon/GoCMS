package legacyimport

import "testing"

func TestCustomLabelSettings(t *testing.T) {
	settings := customLabelSettings([]sourceRow{
		{"lname": "#BM_linkind#", "lcontent": `<a href="https://example.test">Example</a>`},
		{"lname": "#BM_indexfoot#", "lcontent": `Copyright &#169;2004-2015 <a href="#HOPE_WebUrl#">Example</a>`},
	})
	if got := settings["site_footer_links"]; got != `<a href="https://example.test">Example</a>` {
		t.Fatalf("footer links = %q", got)
	}
	if got := settings["site_copyright_years"]; got != "2004-2015" {
		t.Fatalf("copyright years = %q", got)
	}
}
