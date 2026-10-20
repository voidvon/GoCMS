// Package assetmin compresses theme CSS and JavaScript before it is served.
package assetmin

import (
	"bytes"
	"io"
	"sync"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/js"
)

var minifier = sync.OnceValue(func() *minify.M {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	keepNames := func(_ *minify.M, w io.Writer, r io.Reader, params map[string]string) error {
		return (&js.Minifier{KeepVarNames: true}).Minify(nil, w, r, params)
	}
	m.AddFunc("application/javascript", keepNames)
	m.AddFunc("text/javascript", keepNames)
	return m
})

func CSS(data []byte) ([]byte, error) {
	return minifyBytes("text/css", data)
}

func JS(data []byte) ([]byte, error) {
	return minifyBytes("application/javascript", data)
}

func minifyBytes(mediatype string, data []byte) ([]byte, error) {
	var output bytes.Buffer
	output.Grow(len(data))
	if err := minifier().Minify(mediatype, &output, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
