package generator

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	html "golang.org/x/net/html"
)

const publishR2ImageURLs = "publish_r2_image_urls"

type publishedImageLinks map[string]url.URL

func loadPublishedImageLinks(ctx context.Context, tx *sql.Tx, siteID int64) (publishedImageLinks, error) {
	legacySiteID := siteID
	if siteID == 1 {
		legacySiteID = 0
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT m.public_path, m.storage_path,
		       COALESCE(CASE WHEN m.shared_storage=1 THEN platform.public_url ELSE site.public_url END, '')
		FROM gocms_media m
		LEFT JOIN gocms_platform_media_storage platform ON platform.id=1
		LEFT JOIN gocms_media_storage site ON site.site_id=m.site_id
		WHERE m.site_id IN (?, ?) AND m.kind='image' AND m.status='active' AND m.storage_provider='r2'`,
		siteID, legacySiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := make(publishedImageLinks)
	uploadsPrefix := fmt.Sprintf("/assets/%d/uploads/", siteID)
	for rows.Next() {
		var publicPath, storagePath, domain string
		if err := rows.Scan(&publicPath, &storagePath, &domain); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(publicPath, "/") || strings.HasPrefix(publicPath, "//") ||
			path.Clean(publicPath) != publicPath || !validPublicImageKey(storagePath) {
			continue
		}
		origin, err := url.Parse(domain)
		if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil ||
			(origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
			continue
		}
		origin.Path = "/" + storagePath
		origin.RawPath = ""
		links[publicPath] = *origin
		if suffix, ok := strings.CutPrefix(publicPath, uploadsPrefix); ok {
			links["/uploads/"+suffix] = *origin
		}
	}
	return links, rows.Err()
}

func validPublicImageKey(key string) bool {
	return key != "" && key != "." && key != ".." && path.Clean(key) == key &&
		!strings.HasPrefix(key, "/") && !strings.HasPrefix(key, "../") && !strings.ContainsAny(key, "\\\x00")
}

func rememberImageOrigin(hosts map[string]bool, origin string) {
	if parsed, err := url.Parse(origin); err == nil && parsed.Host != "" {
		hosts[strings.ToLower(parsed.Host)] = true
	}
}

func (links publishedImageLinks) rewritePages(pages map[string][]byte, sameSiteHosts map[string]bool) {
	if len(links) == 0 {
		return
	}
	for name, source := range pages {
		if !isHTMLPage(name) {
			continue
		}
		document := url.URL{Path: "/" + name}
		resolve := func(value string) string {
			ref, err := url.Parse(strings.TrimSpace(value))
			if err != nil || ref.Path == "" || ref.User != nil ||
				(ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") ||
				(ref.Host != "" && !sameSiteHosts[strings.ToLower(ref.Host)]) {
				return value
			}
			target, ok := links[document.ResolveReference(ref).Path]
			if !ok {
				return value
			}
			target.RawQuery, target.ForceQuery, target.Fragment = ref.RawQuery, ref.ForceQuery, ref.Fragment
			return target.String()
		}
		pages[name] = rewriteHTMLImageLinks(source, resolve)
	}
}

var (
	imageCSSURL = regexp.MustCompile(`(?i)\burl\(\s*("[^"]*"|'[^']*'|[^\s)'"]+)\s*\)`)
	jsonString  = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

func rewriteHTMLImageLinks(source []byte, resolve func(string) string) []byte {
	tokenizer := html.NewTokenizer(bytes.NewReader(source))
	var result bytes.Buffer
	result.Grow(len(source))
	var textType string
	offset := 0
	for {
		kind := tokenizer.Next()
		// Parsing a token can unescape attributes in the tokenizer's buffer.
		end := offset + len(tokenizer.Raw())
		raw := source[offset:end]
		offset = end
		switch kind {
		case html.ErrorToken:
			result.Write(raw)
			return result.Bytes()
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			changed := false
			for i := range token.Attr {
				attr := &token.Attr[i]
				var value string
				switch attr.Key {
				case "src", "href", "poster", "data-src", "data-original", "data-lazy-src", "data-image", "data-background":
					value = resolve(attr.Val)
				case "srcset", "data-srcset":
					value = rewriteImageSrcset(attr.Val, resolve)
				case "style":
					value = rewriteImageCSS(attr.Val, resolve)
				case "content":
					if token.Data != "meta" {
						continue
					}
					value = resolve(attr.Val)
				default:
					continue
				}
				if value != attr.Val {
					attr.Val = value
					changed = true
				}
			}
			if kind == html.StartTagToken && token.Data == "style" {
				textType = "css"
			}
			if kind == html.StartTagToken && token.Data == "script" {
				for _, attr := range token.Attr {
					if attr.Key == "type" && strings.EqualFold(strings.TrimSpace(attr.Val), "application/ld+json") {
						textType = "jsonld"
						break
					}
				}
			}
			if changed {
				result.WriteString(token.String())
				continue
			}
		case html.EndTagToken:
			textType = ""
		case html.TextToken:
			if textType == "css" {
				result.WriteString(rewriteImageCSS(string(raw), resolve))
				continue
			}
			if textType == "jsonld" {
				result.Write(rewriteJSONLDImageLinks(raw, resolve))
				continue
			}
		}
		result.Write(raw)
	}
}

func rewriteImageSrcset(source string, resolve func(string) string) string {
	// A comma is part of an inline data URL. Preserve such srcsets intact.
	if strings.Contains(strings.ToLower(source), "data:") {
		return source
	}
	items := strings.Split(source, ",")
	for i, item := range items {
		if fields := strings.Fields(item); len(fields) > 0 {
			items[i] = strings.Replace(item, fields[0], resolve(fields[0]), 1)
		}
	}
	return strings.Join(items, ",")
}

func rewriteImageCSS(source string, resolve func(string) string) string {
	return imageCSSURL.ReplaceAllStringFunc(source, func(raw string) string {
		match := imageCSSURL.FindStringSubmatch(raw)
		value := strings.Trim(match[1], `"'`)
		if target := resolve(value); target != value {
			return `url("` + strings.ReplaceAll(target, `"`, `%22`) + `")`
		}
		return raw
	})
}

func rewriteJSONLDImageLinks(source []byte, resolve func(string) string) []byte {
	if !json.Valid(source) {
		return source
	}
	return jsonString.ReplaceAllFunc(source, func(raw []byte) []byte {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			if target := resolve(value); target != value {
				encoded, _ := json.Marshal(target)
				return encoded
			}
		}
		return raw
	})
}
