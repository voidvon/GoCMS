package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gocms/internal/routing"
)

// Publication routing history lives beside the reports, never in the public
// directory. It lets a later refresh remove old routes and obsolete pagination.
type publishedPages map[string]*publishedLanguage

type publishedLanguage struct {
	Categories map[string]*publishedCategory `json:"categories"`
	Contents   map[string]publishedContent   `json:"contents"`
}

type publishedCategory struct {
	ParentID int      `json:"parent_id"`
	Paths    []string `json:"paths"`
}

type publishedContent struct {
	CategoryID int      `json:"category_id"`
	Paths      []string `json:"paths"`
}

func (p Publisher) publishedPagesPath() string {
	return filepath.Join(p.Data, strings.TrimSuffix(p.reportPath(), ".json")+"-pages.json")
}

func (p Publisher) readPublishedPages() (publishedPages, error) {
	data, err := os.ReadFile(p.publishedPagesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pages publishedPages
	if err := json.Unmarshal(data, &pages); err != nil {
		return nil, fmt.Errorf("读取发布路由记录失败，请先全站发布: %w", err)
	}
	return pages, nil
}

func (p Publisher) writePublishedPages(pages publishedPages) error {
	data, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	return atomicWrite(p.publishedPagesPath(), data)
}

func (c *content) beginPublication(id int64, previous *publishedLanguage) {
	c.partialID, c.previous = id, previous
	c.published = &publishedLanguage{
		Categories: map[string]*publishedCategory{},
		Contents:   map[string]publishedContent{},
	}
	c.removed = map[string]bool{}
	c.affected = map[int]bool{}
	if id == 0 {
		return
	}
	key := strconv.FormatInt(id, 10)
	if previous != nil {
		for k, v := range previous.Categories {
			c.published.Categories[k] = v
		}
		for k, v := range previous.Contents {
			c.published.Contents[k] = v
		}
		old := previous.Contents[key]
		for _, page := range old.Paths {
			c.removed[page] = true
		}
		c.affectAncestors(old.CategoryID)
	}
	for _, item := range c.tables["gocms_content"] {
		if item["id"] != key {
			continue
		}
		c.affectAncestors(item.n("category_id"))
		// Also handles a hidden entry on sites published before routing history
		// was introduced. Hidden content must never retain its current detail URL.
		c.removed[c.contentPagePath(item)] = true
		c.published.Contents[key] = publishedContent{CategoryID: item.n("category_id")}
	}
	for categoryID := range c.affected {
		categoryKey := strconv.Itoa(categoryID)
		if old := c.published.Categories[categoryKey]; old != nil {
			for _, page := range old.Paths {
				c.removed[page] = true
			}
		}
		delete(c.published.Categories, categoryKey)
	}
}

func (c *content) affectAncestors(id int) {
	// Walk both snapshots: moving an entry or reparenting its category also
	// invalidates the previously published parent chain.
	for _, historical := range []bool{false, true} {
		seen := map[int]bool{}
		for current := id; current > 0 && !seen[current]; {
			seen[current], c.affected[current] = true, true
			if historical {
				if c.previous == nil || c.previous.Categories[strconv.Itoa(current)] == nil {
					break
				}
				current = c.previous.Categories[strconv.Itoa(current)].ParentID
			} else {
				current = c.cat(current).n("parent_id")
			}
		}
	}
}

func (c *content) ownsDirectoryIndex(category Row, indexPath string) bool {
	for _, candidate := range c.tables["gocms_category"] {
		if candidate["id"] == category["id"] {
			return true
		}
		switch c.categoryPageType(candidate) {
		case routing.PageTypeLink:
			continue
		case routing.PageTypeCover:
			if c.categoryListPagePath(candidate, 1) == indexPath {
				return false
			}
		default:
			if c.categoryListDir(candidate) == c.categoryListDir(category) {
				return false
			}
		}
	}
	return false
}

func safePagePath(relative string) bool {
	return relative != "" && !filepath.IsAbs(relative) && filepath.IsLocal(filepath.FromSlash(relative)) &&
		filepath.ToSlash(filepath.Clean(relative)) == relative && relative != "." && !strings.Contains(relative, "\\")
}

// Hard links preserve unrelated pages and their timestamps without copying the
// whole site's bytes. All replacements use atomicWrite, so live inodes remain
// untouched until the staged directory is ready to replace the live directory.
func stagePublishedFiles(ctx context.Context, web, stage string) error {
	if _, err := os.Stat(web); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(web, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(web, source)
		if err != nil {
			return err
		}
		target := filepath.Join(stage, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("发布目录包含非常规文件: %s", relative)
		}
		if err := os.Link(source, target); err == nil {
			return nil
		}
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

type stagedPages struct {
	web, stage, backup string
	hadWeb, installed  bool
}

func (s *stagedPages) discard() { _ = os.RemoveAll(s.stage) }

func installPageUpdates(updates []*stagedPages, saveHistory func() error) (err error) {
	defer func() {
		if err == nil {
			for _, update := range updates {
				if update.hadWeb {
					_ = os.RemoveAll(update.backup)
				}
			}
			return
		}
		for i := len(updates) - 1; i >= 0; i-- {
			update := updates[i]
			if update.installed {
				if rollbackErr := os.RemoveAll(update.web); rollbackErr != nil {
					err = fmt.Errorf("%w; 清理失败目录: %v", err, rollbackErr)
					continue
				}
			}
			if update.hadWeb {
				if rollbackErr := os.Rename(update.backup, update.web); rollbackErr != nil {
					err = fmt.Errorf("%w; 恢复备份失败（%s）: %v", err, update.backup, rollbackErr)
				}
			}
		}
	}()
	for _, update := range updates {
		if _, statErr := os.Stat(update.web); statErr == nil {
			if err = os.Rename(update.web, update.backup); err != nil {
				return err
			}
			update.hadWeb = true
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err = os.Rename(update.stage, update.web); err != nil {
			return err
		}
		update.installed = true
	}
	if saveHistory != nil {
		return saveHistory()
	}
	return nil
}
