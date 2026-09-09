package site

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"bilvie/internal/generator"
)

type publication struct {
	mu        sync.Mutex
	publisher generator.Publisher
	report    generator.Report
	pending   bool
}

func (s *Server) ConfigurePublishing(templates, data, frontend, assets, theme string) {
	s.frontendRoot = frontend
	s.assetsRoot = assets
	s.themeRoot = theme
	s.templateRoot = templates
	p := &publication{publisher: generator.Publisher{DB: s.database, Web: s.siteRoot, Templates: templates, Data: data, Assets: assets, Theme: theme}, report: generator.Report{State: "idle"}}
	if b, e := os.ReadFile(filepath.Join(data, "publish.json")); e == nil {
		_ = json.Unmarshal(b, &p.report)
	}
	s.publication = p
}
func (s *Server) startPublish(queue bool) (generator.Report, bool) {
	p := s.publication
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.report.State == "running" {
		if queue {
			p.pending = true
		}
		return p.report, false
	}
	p.report = generator.Report{State: "running", Started: time.Now()}
	go func() {
		for {
			r, e := p.publisher.Generate(context.Background())
			if e != nil {
				r.State = "failed"
				r.Error = e.Error()
			}
			p.mu.Lock()
			if p.pending {
				p.pending = false
				p.report = generator.Report{State: "running", Started: time.Now()}
				p.mu.Unlock()
				continue
			}
			p.report = r
			p.mu.Unlock()
			return
		}
	}()

	return p.report, true
}
func (s *Server) adminPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if s.publication == nil {
		http.Error(w, "publishing is not configured", 503)
		return
	}
	if r.Method == http.MethodPost {
		report, started := s.startPublish(false)
		code := http.StatusAccepted
		if !started {
			code = http.StatusConflict
		}
		writeJSON(w, code, report)
		return
	}
	s.publication.mu.Lock()
	report := s.publication.report
	s.publication.mu.Unlock()
	writeJSON(w, 200, report)
}
func (s *Server) contentSaved(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("publish") == "1" && s.publication != nil {
		report, started := s.startPublish(true)
		// Never silently lose a publish request made while another snapshot is being generated.
		writeJSON(w, 200, map[string]any{"ok": true, "publication": report, "publish_started": started})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
