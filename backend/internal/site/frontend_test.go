package site

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServeEmbeddedAdminApp(t *testing.T) {
	server := &Server{}
	server.ConfigureEmbeddedFrontend(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("admin app")},
		"assets/app.js": &fstest.MapFile{Data: []byte("app")},
	})

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/admin/", want: "admin app"},
		{path: "/admin/settings", want: "admin app"},
		{path: "/admin/assets/app.js", want: "app"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, location = %q, body = %s", response.Code, response.Header().Get("Location"), response.Body.String())
			}
			if response.Body.String() != test.want {
				t.Fatalf("body = %q, want %q", response.Body.String(), test.want)
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/../secret", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d", response.Code)
	}
}
