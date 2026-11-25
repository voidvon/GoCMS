package generator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestPublicationProgressThroughInstall(t *testing.T) {
	p := incrementalFixture(t)
	if _, err := p.DB.Exec(`INSERT INTO gocms_language (code, name, is_enabled, site_mode, domain) VALUES ('en', 'English', 1, 'standalone', 'en.example.test')`); err != nil {
		t.Fatal(err)
	}
	var stages []string
	var rendered, written, total int
	p.OnProgress = func(progress Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != progress.Stage {
			stages = append(stages, progress.Stage)
			live, err := p.LastReport()
			if err != nil || live.State != "running" || !live.Finished.IsZero() {
				t.Fatalf("stage %s reported premature completion: %+v %v", progress.Stage, live, err)
			}
		}
		switch progress.Stage {
		case "rendering":
			if progress.Completed < rendered {
				t.Fatal("render count reset between languages")
			}
			rendered = progress.Completed
		case "writing":
			if progress.Completed != 0 && progress.Completed != written+1 {
				t.Fatalf("file count jumped from %d to %d", written, progress.Completed)
			}
			written, total = progress.Completed, progress.Total
		}
	}
	report, err := p.Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stages, []string{"preparing", "rendering", "assembling", "writing", "installing"}) ||
		rendered == 0 || written != report.Files || total != report.Files || total == 0 {
		t.Fatalf("stages %v, rendered %d, wrote %d/%d, report %+v", stages, rendered, written, total, report)
	}
	stored, err := p.LastReport()
	if err != nil || stored.State != "success" || stored.Files != written || stored.Finished.IsZero() {
		t.Fatalf("completion report %+v, %v", stored, err)
	}
}

func TestInterruptedPublicationReport(t *testing.T) {
	p := Publisher{Data: t.TempDir(), SiteID: 4}
	if err := os.WriteFile(filepath.Join(p.Data, p.lockPath()), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.writeReport(Report{State: "running", Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := p.LastReport()
	if err != nil || got.State != "failed" || got.Error == "" || got.Finished.IsZero() {
		t.Fatalf("interrupted report %+v, %v", got, err)
	}
	stored, err := p.readReport()
	if err != nil || stored.State != got.State || !stored.Finished.Equal(got.Finished) {
		t.Fatalf("persisted report %+v, %v", stored, err)
	}
}
