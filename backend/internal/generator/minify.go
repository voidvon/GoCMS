package generator

import (
	"bytes"
	"path"
	"strings"
	"sync"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/html"
)

var htmlMinifier = sync.OnceValue(func() *minify.M {
	m := minify.New()
	// 只压缩 HTML 结构：内联 script/style 保持原样，避免改变主题脚本语义。
	m.AddFunc("text/html", html.Minify)
	return m
})

// htmlMinifyEnabled 返回站点是否启用生成期 HTML 压缩。
// 默认启用，html_minify 设为 0/false/off 时关闭。
func htmlMinifyEnabled(settings map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(settings["html_minify"])) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func isHTMLPage(relative string) bool {
	switch strings.ToLower(path.Ext(relative)) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func minifyHTMLDocument(data []byte) ([]byte, error) {
	var output bytes.Buffer
	output.Grow(len(data))
	if err := htmlMinifier().Minify("text/html", &output, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
