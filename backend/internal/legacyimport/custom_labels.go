package legacyimport

import (
	"regexp"
	"strings"
)

var copyrightYearsPattern = regexp.MustCompile(`(?i)copyright\s+(?:&#169;|©)?\s*(\d{4}(?:-\d{4})?)`)

// customLabelSettings converts legacy reusable fragments into generic site
// settings. The running CMS never reads the legacy custom-label table.
func customLabelSettings(rows []sourceRow) map[string]string {
	settings := make(map[string]string)
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.text("lname"))) {
		case "#bm_linkind#":
			if value := row.text("lcontent"); value != "" {
				settings["site_footer_links"] = value
			}
		case "#bm_indexfoot#":
			match := copyrightYearsPattern.FindStringSubmatch(row.text("lcontent"))
			if len(match) == 2 {
				settings["site_copyright_years"] = match[1]
			}
		}
	}
	return settings
}
