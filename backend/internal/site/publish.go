package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocms/internal/db"
	"gocms/internal/generator"
	themepkg "gocms/internal/theme"
)

type publication struct {
	*publicationState
	publisher generator.Publisher
}

// Site settings can invalidate the publisher configuration during a run.
// The running report and queued updates belong to the site, not that cache entry.
type publicationState struct {
	mu      sync.Mutex
	report  generator.Report
	pending []publicationJob
}

type publicationJob struct {
	target    *publication
	contentID int64 // Zero requests a full site snapshot.
}

func (p *publication) publisherSnapshot() generator.Publisher {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.publisher
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
		publicationState: &publicationState{report: generator.Report{State: "idle"}},
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
				s.publication.mu.Lock()
				s.publication.publisher.OutputDir = s1.OutputDir
				s.publication.mu.Unlock()
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
		return nil
	}

	s.themeMu.RLock()
	tplRoot := s.templateRoot
	assetRoot := s.assetsRoot
	themeRoot := s.themeRoot
	activeTheme := s.activeTheme
	themeBase := s.themeBase
	s.themeMu.RUnlock()
	homeTpl := activeTheme.HomeTemplate()

	themeID := targetSite.ThemeID
	if themeID == "" && activeTheme.Manifest.ID != "" {
		themeID = activeTheme.Manifest.ID
	}
	if themeID != "" {
		siteIDStr := strconv.FormatInt(siteID, 10)
		siteThemeDir := filepath.Join(s.assetsRoot, siteIDStr, "themes", themeID)
		if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) {
			siteThemeDir = filepath.Join(s.assetsRoot, "1", "themes", themeID)
		}
		if _, err := os.Stat(siteThemeDir); os.IsNotExist(err) && themeBase != "" {
			siteThemeDir = filepath.Join(themeBase, themeID)
		}
		themeAssets := filepath.Join(siteThemeDir, "assets")
		themeTemplates := filepath.Join(siteThemeDir, "templates")
		if stat, err := os.Stat(themeTemplates); err == nil && stat.IsDir() {
			tplRoot = themeTemplates
		}
		if stat, err := os.Stat(themeAssets); err == nil && stat.IsDir() {
			themeRoot = themeAssets
		}
		if manifestBytes, err := os.ReadFile(filepath.Join(siteThemeDir, "theme.json")); err == nil {
			var m themepkg.Manifest
			if json.Unmarshal(manifestBytes, &m) == nil {
				homeTpl = themepkg.Definition{Manifest: m}.HomeTemplate()
			}
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
	}
	if s.sitePublicationStates == nil {
		s.sitePublicationStates = make(map[int64]*publicationState)
	}
	pub.publicationState = s.sitePublicationStates[siteID]
	if pub.publicationState == nil {
		pub.publicationState = &publicationState{report: generator.Report{State: "idle"}}
		s.sitePublicationStates[siteID] = pub.publicationState
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

func (s *Server) startPublishPub(p *publication, contentID int64, queue bool) (generator.Report, bool) {
	if p == nil {
		return generator.Report{State: "failed", Error: "publishing is not configured"}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return s.startPublishLocked(p, contentID, queue)
}

func (s *Server) startPublishLocked(p *publication, contentID int64, queue bool) (generator.Report, bool) {
	if p.report.State == "running" {
		if queue {
			p.enqueueLocked(contentID)
			p.report.Queued = true
		}
		return p.report, false
	}
	publisher := p.beginRunLocked(contentID)
	go func() {
		var report generator.Report
		var err error
		if contentID == 0 {
			report, err = publisher.Generate(context.Background())
		} else {
			report, err = publisher.GenerateContent(context.Background(), contentID)
			if err != nil {
				// A later queued success can replace this report between browser polls.
				log.Printf("publish site %d content %d: %v", publisher.SiteID, contentID, err)
				s.recordAdminNotification(context.Background(), db.AdminNotification{
					SiteID: publisher.SiteID, Kind: "content_publish_failed", Level: db.AdminNotificationLevelError,
					Title: "内容已保存，页面发布失败", Detail: "请核对本次内容更新。服务日志中已记录失败的内容编号和原因。", LinkView: "content",
				})
			}
		}
		s.finishPublish(p, report, err)
	}()
	return p.report, true
}

func (p *publication) enqueueLocked(contentID int64) {
	job := publicationJob{target: p, contentID: contentID}
	if contentID == 0 {
		// A waiting full build will read all committed changes when it starts.
		p.pending = []publicationJob{job}
		return
	}
	for i, waiting := range p.pending {
		if waiting.contentID == 0 || waiting.contentID == contentID {
			p.pending[i].target = p
			return
		}
	}
	p.pending = append(p.pending, job)
}

func (p *publication) beginRunLocked(contentID int64) generator.Publisher {
	p.report = generator.Report{State: "running", Started: time.Now(), ContentID: contentID, Queued: len(p.pending) > 0}
	publisher := p.publisher
	publisher.OnProgress = func(progress generator.Progress) {
		p.mu.Lock()
		p.report.Progress = &progress
		p.mu.Unlock()
	}
	return publisher
}

func (s *Server) finishPublish(p *publication, report generator.Report, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		report.State, report.Error = "failed", err.Error()
		if report.Finished.IsZero() {
			report.Finished = time.Now()
		}
	}
	p.report = report
	if len(p.pending) > 0 {
		next := p.pending[0]
		p.pending[0] = publicationJob{}
		p.pending = p.pending[1:]
		s.startPublishLocked(next.target, next.contentID, false)
	}
}

func (s *Server) publicationReport(p *publication) generator.Report {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.report.State == "running" {
		return p.report
	}
	if persisted, err := p.publisher.LastReport(); err == nil {
		if persisted.State == "running" {
			return persisted
		}
		if persisted.Finished.After(p.report.Finished) {
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
		report, started := s.startPublishPub(pub, 0, false)
		code := http.StatusAccepted
		if !started {
			code = http.StatusConflict
		}
		writeJSON(w, code, report)
		return
	}
	writeJSON(w, http.StatusOK, publicationForUser(s.publicationReport(pub), user, siteID))
}

func publicationForUser(report generator.Report, user *AdminUser, siteID int64) generator.Report {
	if !user.HasPermissionInSite(siteID, "publish") {
		report.ContentID = 0
		if report.Error != "" {
			report.Error = "网站发布失败，请联系有网站发布权限的管理员查看详情。"
		}
	}
	return report
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
	filename, err := pub.publisherSnapshot().GenerateSitemap(r.Context(), format)
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

func (s *Server) respondAfterSave(w http.ResponseWriter, r *http.Request, contentID int64) {
	if r.URL.Query().Get("publish") == "1" && s.publication != nil {
		user := s.currentAdmin(r)
		siteID, err := s.resolveSiteID(r, user)
		if err == nil {
			if pub := s.publicationForSite(r.Context(), siteID); pub != nil {
				report, started := s.startPublishPub(pub, contentID, true)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "publication": publicationForUser(report, user, siteID), "publish_started": started})
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

	filename, err := pub.publisherSnapshot().GenerateLLMS(r.Context())
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

// adminPublishContent completes a targeted refresh before reporting success.
// It shares both the in-process publication state and the generator's file lock
// with full publishing, including queued save-and-publish requests.
func (s *Server) adminPublishContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	user := s.currentAdmin(r)
	siteID, err := s.resolveSiteID(r, user)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := decodeRequest(r, &payload); err != nil || payload.ID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请指定有效的内容 ID"})
		return
	}
	var contentSiteID, categoryID int64
	err = s.database.QueryRowContext(r.Context(), `SELECT site_id, category_id FROM gocms_content WHERE id = ?`, payload.ID).Scan(&contentSiteID, &categoryID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取内容失败"})
		return
	}
	if siteID != contentSiteID || !user.CanManageSite(contentSiteID) || !user.canManageCategory(categoryID) {
		http.NotFound(w, r)
		return
	}
	pub := s.publicationForSite(r.Context(), siteID)
	if pub == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "尚未配置网站发布"})
		return
	}
	pub.mu.Lock()
	if pub.report.State == "running" {
		pub.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": generator.ErrPublishBusy.Error()})
		return
	}
	publisher := pub.beginRunLocked(payload.ID)
	pub.mu.Unlock()

	report, err := publisher.GenerateContent(r.Context(), payload.ID)
	s.finishPublish(pub, report, err)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, generator.ErrPublishBusy) {
			status = http.StatusConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
