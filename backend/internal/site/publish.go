package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
	"gocms/internal/generator"
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
	s.themeMu.Lock()
	s.themeRoot = theme
	s.templateRoot = templates
	homeTpl := s.activeTheme.HomeTemplate()
	s.themeMu.Unlock()
	p := &publication{
		publisher: generator.Publisher{
			DB:           s.database,
			Web:          s.siteRoot,
			Templates:    templates,
			Data:         data,
			Assets:       assets,
			Theme:        theme,
			HomeTemplate: homeTpl,
			SiteID:       1,
		},
		report: generator.Report{State: "idle"},
	}
	if b, e := os.ReadFile(filepath.Join(data, "publish.json")); e == nil {
		_ = json.Unmarshal(b, &p.report)
	}
	s.publication = p
}

func (s *Server) publicationForSite(ctx context.Context, siteID int64) *publication {
	if s.publication == nil {
		return nil
	}
	if siteID <= 1 {
		if s.database != nil {
			if s1, err := db.GetSiteByID(ctx, s.database, 1); err == nil && s1 != nil {
				s.publication.publisher.OutputDir = s1.OutputDir
			}
		}
		return s.publication
	}
	s.sitePubMu.Lock()
	defer s.sitePubMu.Unlock()
	if s.sitePublications == nil {
		s.sitePublications = make(map[int64]*publication)
	}
	if pub, ok := s.sitePublications[siteID]; ok {
		return pub
	}

	targetSite, err := db.GetSiteByID(ctx, s.database, siteID)
	if err != nil || targetSite == nil {
		return s.publication
	}

	tplRoot := s.templateRoot
	assetRoot := s.assetsRoot
	themeRoot := s.themeRoot
	homeTpl := s.activeTheme.HomeTemplate()

	themeID := targetSite.ThemeID
	if themeID == "" && s.activeTheme.Manifest.ID != "" {
		themeID = s.activeTheme.Manifest.ID
	}
	if themeID != "" {
		siteIDStr := strconv.FormatInt(siteID, 10)
		siteThemeDir := filepath.Join(s.assetsRoot, siteIDStr, "themes", themeID)
		if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) {
			siteThemeDir = filepath.Join(s.assetsRoot, "1", "themes", themeID)
		}
		if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) && s.themeBase != "" {
			siteThemeDir = filepath.Join(s.themeBase, themeID)
		}
		themeAssets := filepath.Join(siteThemeDir, "assets")
		themeTemplates := filepath.Join(siteThemeDir, "templates")
		if stat, err := os.Stat(themeTemplates); err == nil && stat.IsDir() {
			tplRoot = themeTemplates
		}
		if stat, err := os.Stat(themeAssets); err == nil && stat.IsDir() {
			themeRoot = themeAssets
		}
	}

	pub := &publication{
		publisher: generator.Publisher{
			DB:           s.database,
			Web:          s.siteRoot,
			Templates:    tplRoot,
			Data:         s.publication.publisher.Data,
			Assets:       assetRoot,
			Theme:        themeRoot,
			HomeTemplate: homeTpl,
			SiteID:       siteID,
			OutputDir:    targetSite.OutputDir,
		},
		report: generator.Report{State: "idle"},
	}
	reportFile := filepath.Join(pub.publisher.Data, fmt.Sprintf("publish_%d.json", siteID))
	if b, e := os.ReadFile(reportFile); e == nil {
		_ = json.Unmarshal(b, &pub.report)
	}
	s.sitePublications[siteID] = pub
	return pub
}

func (s *Server) invalidateSitePublication(siteID int64) {
	s.sitePubMu.Lock()
	defer s.sitePubMu.Unlock()
	if s.sitePublications != nil {
		delete(s.sitePublications, siteID)
	}
}

func (s *Server) startPublish(queue bool) (generator.Report, bool) {

	return s.startPublishPub(s.publication, queue)
}

func (s *Server) startPublishPub(p *publication, queue bool) (generator.Report, bool) {
	if p == nil {
		return generator.Report{State: "failed", Error: "publishing is not configured"}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return s.startPublishLocked(p, queue)
}

func (s *Server) startPublishLocked(p *publication, queue bool) (generator.Report, bool) {
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

func (s *Server) publicationReport(p *publication) generator.Report {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.report.State != "running" {
		var persisted generator.Report
		reportFile := filepath.Join(p.publisher.Data, "publish.json")
		if p.publisher.SiteID > 1 {
			reportFile = filepath.Join(p.publisher.Data, fmt.Sprintf("publish_%d.json", p.publisher.SiteID))
		}
		if data, err := os.ReadFile(reportFile); err == nil && json.Unmarshal(data, &persisted) == nil && persisted.Finished.After(p.report.Finished) {
			p.report = persisted
		}
	}
	return p.report
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
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	user := s.currentAdmin(r)
	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	pub := s.publicationForSite(r.Context(), siteID)
	if pub == nil {
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPost {
		report, started := s.startPublishPub(pub, false)
		code := http.StatusAccepted
		if !started {
			code = http.StatusConflict
		}
		writeJSON(w, code, report)
		return
	}
	report := s.publicationReport(pub)
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) adminSitemap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if s.publication == nil {
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	user := s.currentAdmin(r)
	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	pub := s.publicationForSite(r.Context(), siteID)
	if pub == nil {
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}

	var payload struct {
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid sitemap request", http.StatusBadRequest)
		return
	}
	format := strings.ToLower(strings.TrimSpace(payload.Format))
	filename, err := pub.publisher.GenerateSitemap(r.Context(), format)
	if err != nil {
		if errors.Is(err, generator.ErrPublishBusy) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"format": format,
		"path":   "/" + filename,
	})
}

func (s *Server) contentSaved(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("publish") == "1" && s.publication != nil {
		user := s.currentAdmin(r)
		siteID, err := s.resolveSiteID(r, user)
		if err == nil {
			pub := s.publicationForSite(r.Context(), siteID)
			if pub != nil {
				report, started := s.startPublishPub(pub, true)
				// Never silently lose a publish request made while another snapshot is being generated.
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "publication": report, "publish_started": started})
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminLLMS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if s.publication == nil {
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}
	user := s.currentAdmin(r)
	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	pub := s.publicationForSite(r.Context(), siteID)
	if pub == nil {
		http.Error(w, "publishing is not configured", http.StatusServiceUnavailable)
		return
	}

	filename, err := pub.publisher.GenerateLLMS(r.Context())
	if err != nil {
		if errors.Is(err, generator.ErrPublishBusy) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": "/" + filename})
}
