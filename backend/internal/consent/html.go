package consent

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

//go:embed runtime.js
var runtimeJS string

// Process blocks explicitly classified resources before a browser can discover
// them. Runtime-only blocking would be too late for the preload scanner.
func Process(page []byte, policy Policy) ([]byte, error) {
	z := html.NewTokenizer(bytes.NewReader(page))
	var output bytes.Buffer
	marked := false
	bodyEnd := -1
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, z.Err()
			}
			break
		}
		raw := append([]byte(nil), z.Raw()...)
		if kind == html.EndTagToken && strings.EqualFold(z.Token().Data, "body") {
			bodyEnd = output.Len()
		}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := z.Token()
			purpose := ""
			classified := false
			for _, attr := range token.Attr {
				if attr.Key == "data-consent" {
					purpose, classified = attr.Val, true
				}
			}
			if classified {
				if !ValidPurpose(purpose) {
					return nil, fmt.Errorf("invalid data-consent purpose %q", purpose)
				}
				if token.Data != "script" && token.Data != "iframe" && token.Data != "img" {
					return nil, fmt.Errorf("data-consent is only supported on script, iframe and img, got %s", token.Data)
				}
				marked = true
				attrs := make([]html.Attribute, 0, len(token.Attr)+1)
				for _, attr := range token.Attr {
					switch attr.Key {
					case "src", "srcset", "srcdoc":
						attr.Key = "data-consent-" + attr.Key
					case "type":
						if token.Data == "script" {
							if strings.EqualFold(attr.Val, "text/plain") {
								continue
							}
							attr.Key = "data-consent-type"
						}
					}
					// Duplicates can make the browser use a different URL or type.
					for _, existing := range attrs {
						if existing.Key == attr.Key {
							return nil, fmt.Errorf("duplicate consent attribute %s", attr.Key)
						}
					}
					attrs = append(attrs, attr)
				}
				if token.Data == "script" {
					attrs = append(attrs, html.Attribute{Key: "type", Val: "text/plain"})
				}
				token.Attr = attrs
				raw = []byte(token.String())
			}
		}
		output.Write(raw)
	}
	if !policy.Enabled && !marked {
		return page, nil
	}
	config, err := json.Marshal(policy) // EscapeHTML prevents closing the script element.
	if err != nil {
		return nil, err
	}
	bootstrap := []byte(`<script id="gocms-consent-config" type="application/json">` + string(config) + `</script><script data-gocms-consent-runtime>` + runtimeJS + `</script>`)
	data := output.Bytes()
	if bodyEnd < 0 {
		bodyEnd = len(data)
	}
	result := make([]byte, 0, len(data)+len(bootstrap))
	result = append(result, data[:bodyEnd]...)
	result = append(result, bootstrap...)
	return append(result, data[bodyEnd:]...), nil
}
