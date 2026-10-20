package assetmin

import (
	"testing"
)

func TestCSS(t *testing.T) {
	output, err := CSS([]byte("body {\n  color: red;\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "body{color:red}" {
		t.Fatalf("unexpected CSS: %s", output)
	}
}

func TestJS(t *testing.T) {
	output, err := JS([]byte("function add (a, b) {\n  return a + b;\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(output) >= len("function add (a, b) {\n  return a + b;\n}") {
		t.Fatalf("JS was not compressed: %s", output)
	}
	if string(output) != "function add(a,b){return a+b}" {
		t.Fatalf("unexpected JS: %s", output)
	}
}
