package site

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"gocms/internal/db"
	"gocms/internal/sitecontacts"
)

type contactCardQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadContactLibrary(ctx context.Context, query contactCardQueryer, siteID int64) (sitecontacts.Library, error) {
	raw, err := readContactCards(ctx, query, siteID)
	if err != nil {
		return nil, err
	}
	cards, err := sitecontacts.Parse(raw)
	if err != nil {
		return nil, err
	}
	return sitecontacts.NewLibrary(cards)
}

func readContactCards(ctx context.Context, query contactCardQueryer, siteID int64) (string, error) {
	var raw string
	err := query.QueryRowContext(ctx, `SELECT "value" FROM "gocms_site_setting" WHERE "site_id" = ? AND "key" = ?`, siteID, sitecontacts.SettingKey).Scan(&raw)
	if err == sql.ErrNoRows {
		err = nil
	}
	return raw, err
}

type contactCardsPayload struct {
	Cards    []sitecontacts.Card `json:"cards"`
	Revision string              `json:"revision"`
}

func contactCardsRevision(raw string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func (s *Server) adminContactCards(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	siteID, err := s.resolveSiteID(r, s.currentAdmin(r))
	if err != nil {
		contactCardsError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		raw, err := readContactCards(ctx, s.database, siteID)
		if err != nil {
			contactCardsError(w, http.StatusInternalServerError, "加载联系方式失败")
			return
		}
		cards, err := sitecontacts.Parse(raw)
		if err != nil {
			contactCardsError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, contactCardsPayload{Cards: cards, Revision: contactCardsRevision(raw)})
	case http.MethodPut:
		s.saveContactCards(w, r, siteID)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) saveContactCards(w http.ResponseWriter, r *http.Request, siteID int64) {
	var payload contactCardsPayload
	if err := decodeRequest(r, &payload); err != nil || payload.Cards == nil || payload.Revision == "" {
		contactCardsError(w, http.StatusBadRequest, "联系方式配置格式无效")
		return
	}
	library, err := sitecontacts.NewLibrary(payload.Cards)
	if err != nil {
		contactCardsError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		contactCardsError(w, http.StatusInternalServerError, "保存联系方式失败")
		return
	}
	defer tx.Rollback()
	current, err := readContactCards(ctx, tx, siteID)
	if err != nil {
		contactCardsError(w, http.StatusInternalServerError, "加载联系方式失败")
		return
	}
	if contactCardsRevision(current) != payload.Revision {
		contactCardsError(w, http.StatusConflict, "联系方式已被更新，请关闭弹窗后重新打开")
		return
	}
	conflict, err := contactCardSelectionConflict(ctx, tx, siteID, library)
	if err != nil {
		contactCardsError(w, http.StatusInternalServerError, "核对语言选择失败")
		return
	}
	if conflict != "" {
		contactCardsError(w, http.StatusConflict, fmt.Sprintf("「%s」仍使用这张联系卡片，请先取消选择", conflict))
		return
	}
	raw, err := json.Marshal(payload.Cards)
	if err == nil {
		err = db.UpsertSiteSettings(ctx, tx, siteID, map[string]string{sitecontacts.SettingKey: string(raw)})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		contactCardsError(w, http.StatusInternalServerError, "保存联系方式失败")
		return
	}
	s.invalidateSitePublication(siteID)
	payload.Revision = contactCardsRevision(string(raw))
	writeJSON(w, http.StatusOK, payload)
}

func contactCardSelectionConflict(ctx context.Context, tx *sql.Tx, siteID int64, library sitecontacts.Library) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "name", "contact_card_ids" FROM "`+db.LanguageTable+`" WHERE "site_id" = ? ORDER BY "id"`, siteID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return "", err
		}
		ids, err := sitecontacts.ParseSelection(raw)
		if err != nil {
			return "", err
		}
		if err := library.ValidateSelection(ids); err != nil {
			return name, nil
		}
	}
	return "", rows.Err()
}

func contactCardsError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
