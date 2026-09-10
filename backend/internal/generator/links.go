package generator

import (
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var resourceAttribute = regexp.MustCompile(`(?i)\b(href|src|action)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)

// Normalize theme and content resource references to the public URL space and
// match the actual case of files on disk.
func (c *content) normalizeLinks(assets, theme string) error {
	index := map[string]string{}
	basenames := map[string]string{}
	addTree := func(root, publicPrefix string) error {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("资源目录中不允许符号链接: %s", p)
			}
			rel, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			if rel == "." || d.IsDir() {
				return nil
			}
			rel = filepath.ToSlash(rel)
			publicPath := path.Join(publicPrefix, rel)
			key := strings.ToLower(publicPath)
			if _, exists := index[key]; !exists {
				index[key] = publicPath
			}
			if publicPrefix == "images" {
				name := strings.ToLower(path.Base(rel))
				if previous, exists := basenames[name]; exists && previous != publicPath {
					basenames[name] = ""
				} else {
					basenames[name] = publicPath
				}
			}
			return nil
		})
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if assets != "" {
		if err := addTree(filepath.Join(assets, "images"), "images"); err != nil {
			return err
		}
	}
	if theme != "" {
		for _, tree := range []struct {
			directory string
			public    string
		}{
			{directory: "images", public: "images"},
			{directory: "css", public: "css"},
			{directory: "js", public: "js"},
			{directory: "skin", public: "skin"},
		} {
			if err := addTree(filepath.Join(theme, tree.directory), tree.public); err != nil {
				return err
			}
		}
	}
	for p := range c.pages {
		index[strings.ToLower(p)] = p
	}
	for name, b := range c.pages {
		c.pages[name] = []byte(resourceAttribute.ReplaceAllStringFunc(string(b), func(attr string) string {
			m := resourceAttribute.FindStringSubmatch(attr)
			value := html.UnescapeString(strings.Trim(m[2], `"'`))
			u, e := url.Parse(value)
			if e != nil || u.Path == "" {
				return attr
			}
			external := u.IsAbs() || u.Host != ""
			if external {
				return attr
			}
			target := path.Clean(path.Join("/", path.Dir(name), u.Path))
			if strings.HasPrefix(u.Path, "/") {
				target = path.Clean(u.Path)
			}
			resolved, ok := index[strings.ToLower(strings.TrimPrefix(target, "/"))]
			if !ok {
				resolved = basenames[strings.ToLower(path.Base(target))]
				ok = resolved != ""
			}
			if !ok {
				return attr
			}
			u.Path = "/" + resolved
			if strings.HasSuffix(value, "/") {
				u.Path += "/"
			}
			return m[1] + `="` + esc(u.String()) + `"`
		}))
	}
	return nil
}
