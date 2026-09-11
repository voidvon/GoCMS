package generator

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// GenerateLLMS refreshes the guide from published HTML, never from saved drafts.
func (p Publisher) GenerateLLMS(ctx context.Context) (string, error) {
	return p.generateIndex(ctx, "llms")
}

// Only read the head: large article bodies do not need to be parsed for an index.
func pageMetadata(r io.Reader) (title, description string, err error) {
	z := html.NewTokenizer(io.LimitReader(r, 128*1024))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() != io.EOF {
				err = z.Err()
			}
			return
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "body":
				return
			case "title":
				inTitle = true
			case "meta":
				var name, value string
				for _, a := range t.Attr {
					if a.Key == "name" {
						name = a.Val
					}
					if a.Key == "content" {
						value = a.Val
					}
				}
				if strings.EqualFold(name, "description") {
					description = value
				}
			}
		case html.EndTagToken:
			t := z.Token()
			if t.Data == "head" {
				return
			}
			if t.Data == "title" {
				inTitle = false
			}
		case html.TextToken:
			if inTitle {
				title += string(z.Text())
			}
		}
	}
}

func markdownText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > limit {
		s = string(runes[:limit]) + "…"
	}
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "<", "&lt;", ">", "&gt;", "#", "\\#").Replace(s)
}

func renderLLMS(ctx context.Context, base string, paths []string, open func(string) (io.ReadCloser, error)) ([]byte, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base != "" {
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("请在站点设置中配置有效的 HTTP(S) 网站地址")
		}
	}
	var entries strings.Builder
	siteTitle, siteDescription := "Website", "Public website pages and their source links."
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := open(path)
		if err != nil {
			return nil, err
		}
		title, description, readErr := pageMetadata(f)
		closeErr := f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		title = markdownText(title, 200)
		description = markdownText(description, 300)
		if path == "index.html" {
			if title != "" {
				siteTitle = title
			}
			if description != "" {
				siteDescription = description
			}
		}
		if title == "" {
			title = markdownText(path, 200)
		}
		source := base + (&url.URL{Path: "/" + path}).EscapedPath()
		fmt.Fprintf(&entries, "- [%s](<%s>)", title, source)
		if description != "" {
			fmt.Fprintf(&entries, ": %s", description)
		}
		entries.WriteByte('\n')
	}
	return []byte("# " + siteTitle + "\n\n> " + siteDescription + "\n\n## Pages\n\n" + entries.String()), nil
}
