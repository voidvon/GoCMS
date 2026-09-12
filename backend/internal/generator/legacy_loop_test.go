package generator

import "testing"

func TestExpandLegacyLoopSyntax(t *testing.T) {
	got, err := expandLegacyLoopSyntax(`[e:loop={71,10,2,0,,newstime desc}]{{.Index}} {{.Title}}[/e:loop]`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{{range contentItems 71 10 true false "newest"}}{{.Index}} {{.Title}}{{end}}`
	if got != want {
		t.Fatalf("expanded loop = %q, want %q", got, want)
	}
	got, err = expandLegacyLoopSyntax(`[e:loop={71,10,2,1,,newstime desc}]{{.Image}}[/e:loop]`)
	if err != nil || got != `{{range contentItemsWithImage 71 10 true false true "newest"}}{{.Image}}{{end}}` {
		t.Fatalf("image loop = %q: %v", got, err)
	}
	for _, source := range []string{
		`[e:loop={71,10,2,0,title='x',newstime desc}]{{.Title}}[/e:loop]`,
		`[e:loop={71,10,1,0,,newstime desc}]{{.Title}}[/e:loop]`,
		`[e:loop={71,10,2,3,,newstime desc}]{{.Title}}[/e:loop]`,
		`[e:loop={71,10,2,0,,newstime desc}]<?php echo $bqr['title']; ?>[/e:loop]`,
	} {
		if _, err := expandLegacyLoopSyntax(source); err == nil {
			t.Fatalf("unsupported loop accepted: %s", source)
		}
	}
	if got, err := expandLegacyLoopSyntax("plain html"); err != nil || got != "plain html" {
		t.Fatalf("plain template changed: %q %v", got, err)
	}
}
