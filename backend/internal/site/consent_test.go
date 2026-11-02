package site

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"gocms/internal/db"
)

func TestConsentSettingsValidatedAndIsolatedBySite(t *testing.T) {
	s, database, token := newLanguageTestServer(t)
	if _, err := database.Exec(`INSERT INTO gocms_site (name, code, domain) VALUES ('Other','other','other.test')`); err != nil {
		t.Fatal(err)
	}
	var otherID int64
	if err := database.QueryRow(`SELECT id FROM gocms_site WHERE code='other'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	valid := `{"enabled":true,"languages":{"fr":{"mode":"off"}}}`
	body, _ := json.Marshal(map[string]string{"consent_config": valid})
	res := adminRequest(t, s, token, http.MethodPost, "/api/admin/site-settings?site_id=1", string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", res.Code, res.Body.String())
	}
	other, err := db.LoadSiteSettingsForSite(context.Background(), database, otherID)
	if err != nil || other["consent_config"] != "" {
		t.Fatal("policy leaked into another site")
	}
	body, _ = json.Marshal(map[string]string{"consent_config": `{"text":{"cookie_url":"javascript:alert(1)"}}`, "site_name": "Must not save"})
	res = adminRequest(t, s, token, http.MethodPost, "/api/admin/site-settings?site_id=1", string(body))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy accepted: %d %s", res.Code, res.Body.String())
	}
	stored, err := db.LoadSiteSettingsForSite(context.Background(), database, 1)
	if err != nil || stored["consent_config"] != valid || stored["site_name"] == "Must not save" {
		t.Fatal("invalid request partially saved settings")
	}
	res = adminRequest(t, s, "", http.MethodPost, "/api/admin/site-settings?site_id=1", string(body))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated write status: %d", res.Code)
	}
}
