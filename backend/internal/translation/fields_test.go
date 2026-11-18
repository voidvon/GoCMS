package translation

import (
	"reflect"
	"testing"
)

func TestExtraDataEmptyValuesAndSharedFields(t *testing.T) {
	base := map[string]any{"documents": "old.pdf", "shared": "shared"}
	fields := map[string]bool{"documents": true, "zero": true, "enabled": true}
	for _, empty := range []any{nil, "", " \t\n", []any{}, map[string]any{}} {
		target := map[string]any{"documents": empty, "zero": 0, "enabled": false, "shared": "translated"}
		got := ExtraData(base, target, map[string]any{"documents": []any{"english.pdf"}}, fields)
		want := map[string]any{"documents": []any{"english.pdf"}, "zero": 0, "enabled": false, "shared": "shared"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("target %v resolved to %v, want %v", empty, got, want)
		}
		got = ExtraData(base, target, map[string]any{"documents": empty}, fields)
		if _, exists := got["documents"]; exists {
			t.Fatalf("empty fallback retained documents: %v", got)
		}
	}
	if base["documents"] != "old.pdf" {
		t.Fatal("resolution mutated stored fields")
	}
}
