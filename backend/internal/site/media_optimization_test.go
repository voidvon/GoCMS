package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocms/internal/db"
	"gocms/internal/imageopt"

	"github.com/gen2brain/webp"
)

func TestMediaOptimizationStorageAndSiteSettings(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	s.assetsRoot = t.TempDir()
	if _, err := database.Exec(`INSERT INTO gocms_site (id,name,code) VALUES (2,'Other','other')`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(t, s, token, http.MethodPut, "/api/admin/site-settings?site_id=2", `{"image_optimization_enabled":"false","image_optimization_quality":"90"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", response.Code, response.Body.String())
	}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	seed := uint32(1)
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			seed = seed*1664525 + 1013904223
			n := int(seed >> 28)
			img.SetNRGBA(x, y, color.NRGBA{uint8(x*2 + n), uint8(y*2 + n), uint8(x + y + n), 255})
		}
	}
	var source bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	for _, siteID := range []string{"1", "2"} {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/media?site_id="+siteID, bytes.NewReader(multipartUpload(t, "cover.png", source.Bytes())))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=testboundary")
		req.AddCookie(&http.Cookie{Name: "gocms_admin", Value: token})
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", res.Code, res.Body.String())
		}
		var result struct {
			Asset MediaAsset `json:"asset"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		asset := result.Asset
		stored, err := os.ReadFile(filepath.Join(s.assetsRoot, filepath.FromSlash(strings.TrimPrefix(asset.URL, "/assets/"))))
		if err != nil {
			t.Fatal(err)
		}
		if siteID == "1" && (asset.MimeType != "image/webp" || !strings.HasSuffix(asset.URL, ".webp") || len(stored) >= source.Len()) {
			t.Fatalf("not optimized: %+v", asset)
		}
		if siteID == "2" && (asset.MimeType != "image/png" || !bytes.Equal(stored, source.Bytes())) {
			t.Fatal("another site's settings were not applied")
		}
		hash := sha256.Sum256(stored)
		cfg, _, err := image.DecodeConfig(bytes.NewReader(stored))
		if err != nil || asset.SizeBytes != int64(len(stored)) || asset.SHA256 != hex.EncodeToString(hash[:]) || asset.Width != cfg.Width || asset.Height != cfg.Height {
			t.Fatalf("stored metadata mismatch: %+v %v", asset, err)
		}
		persisted, err := s.readMedia(context.Background(), asset.ID)
		if err != nil || persisted != asset {
			t.Fatalf("database metadata mismatch: %+v %v", persisted, err)
		}
		public := httptest.NewRecorder()
		s.Handler().ServeHTTP(public, httptest.NewRequest(http.MethodGet, asset.URL, nil))
		if public.Code != http.StatusOK || public.Header().Get("Content-Type") != asset.MimeType || !bytes.Equal(public.Body.Bytes(), stored) {
			t.Fatal("public response differs from stored image")
		}
	}
}

func TestMediaWebPUploadAndImageLimits(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	s.assetsRoot = t.TempDir()
	var b bytes.Buffer
	if err := webp.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3, 2)), webp.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	// Type is detected from file content, not the submitted extension.
	res := mediaUploadRequest(t, s, token, "cover.jpg", b.Bytes())
	if res.Code != http.StatusCreated {
		t.Fatalf("WebP upload failed: %d %s", res.Code, res.Body.String())
	}
	var result struct {
		Asset MediaAsset `json:"asset"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Asset.MimeType != "image/webp" || !strings.HasSuffix(result.Asset.URL, ".webp") {
		t.Fatalf("wrong format: %+v", result.Asset)
	}
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewNRGBA(image.Rect(0, 0, 12001, 1))); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"oversized.webp": make([]byte, maxMediaUploadSize+1), "wide.png": wide.Bytes(), "fake.webp": []byte("RIFF0000WEBP")} {
		res := mediaUploadRequest(t, s, token, name, data)
		if res.Code != http.StatusBadRequest && res.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("invalid image accepted: %s %d", name, res.Code)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_media`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rejected uploads created media records: %d %v", count, err)
	}
}

func TestImageSettingsValidationIsAtomic(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	for _, body := range []string{`{"image_optimization_enabled":"yes","site_name":"invalid"}`, `{"image_optimization_quality":"101","site_name":"invalid"}`, `{"image_optimization_quality":"0"}`, `{"image_optimization_quality":"80.5"}`} {
		res := adminRequest(t, s, token, http.MethodPut, "/api/admin/site-settings", body)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid settings accepted: %d %s", res.Code, res.Body.String())
		}
	}
	settings, err := db.LoadSiteSettingsForSite(context.Background(), database, 1)
	if err != nil || settings["site_name"] == "invalid" || settings[imageopt.QualityKey] != "" {
		t.Fatalf("invalid settings partially saved: %v %v", settings, err)
	}
	res := adminRequest(t, s, "", http.MethodPut, "/api/admin/site-settings", `{"image_optimization_enabled":"false"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized settings write: %d", res.Code)
	}
}

func TestHEIFUploadConvertsEvenWhenOptimizationDisabled(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	s.assetsRoot = t.TempDir()
	if err := db.SaveSiteSettingsForSite(context.Background(), database, 1, map[string]string{imageopt.EnabledKey: "false"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../imageopt/testdata/gradient.heic")
	if err != nil {
		t.Fatal(err)
	}
	// The HEIF generic major brand must work too; compatible HEIC brand remains.
	copy(data[8:12], "mif1")
	res := mediaUploadRequest(t, s, token, "photo.heif", data)
	if res.Code != http.StatusCreated {
		t.Fatalf("HEIF upload failed: %d %s", res.Code, res.Body.String())
	}
	var result struct {
		Asset MediaAsset `json:"asset"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	asset := result.Asset
	if asset.OriginalName != "photo.heif" || asset.MimeType == "image/heic" || asset.Width != 64 || asset.Height != 48 {
		t.Fatalf("invalid conversion metadata: %+v", asset)
	}
	stored, err := os.ReadFile(filepath.Join(s.assetsRoot, filepath.FromSlash(strings.TrimPrefix(asset.URL, "/assets/"))))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.Decode(bytes.NewReader(stored)); err != nil {
		t.Fatal(err)
	}
}

func TestDamagedMediaUploadsLeaveNoFilesOrRecords(t *testing.T) {
	s, database, token := newCategoryTestServer(t)
	s.assetsRoot = t.TempDir()
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewNRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []string{"true", "false"} {
		if err := db.SaveSiteSettingsForSite(context.Background(), database, 1, map[string]string{imageopt.EnabledKey: enabled}); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{
			"broken.png": testPNG(t)[:33],
			"broken.jpg": jpegBytes.Bytes()[:jpegBytes.Len()-20],
		} {
			res := mediaUploadRequest(t, s, token, name, data)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("damaged upload with optimization=%s: %d %s", enabled, res.Code, res.Body.String())
			}
		}
	}
	// A still-image major brand must not conceal sequence data from validation.
	data, err := os.ReadFile("../imageopt/testdata/gradient.heic")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, 0, 0, 0, 8, 'm', 'o', 'o', 'v')
	res := mediaUploadRequest(t, s, token, "sequence.heic", data)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "图片序列") {
		t.Fatalf("concealed sequence: %d %s", res.Code, res.Body.String())
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM gocms_media`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected uploads created records: %d %v", count, err)
	}
	if entries, err := os.ReadDir(s.assetsRoot); err != nil || len(entries) != 0 {
		t.Fatalf("rejected uploads created files: %v %v", entries, err)
	}
	// Rejection must not leave the worker slot occupied.
	if res := mediaUploadRequest(t, s, token, "valid.png", testPNG(t)); res.Code != http.StatusCreated {
		t.Fatalf("valid upload after rejection: %d %s", res.Code, res.Body.String())
	}
}
