package site

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"bilvie/internal/db"
)

func TestSessionSurvivesServerRestart(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	first, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := first.createSession("bilvie")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: "bilvie_admin", Value: token})
	if username, ok := first.adminUsername(request); !ok || username != "bilvie" {
		t.Fatalf("first server rejected session: %q, %v", username, ok)
	}

	second, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if username, ok := second.adminUsername(request); !ok || username != "bilvie" {
		t.Fatalf("session did not survive server restart: %q, %v", username, ok)
	}
}
