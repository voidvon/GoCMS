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

func TestFrontendDevProxy(t *testing.T) {
	devServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/" {
			_, _ = w.Write([]byte("vite dev index"))
			return
		}
		if r.URL.Path == "/admin/src/main.tsx" {
			_, _ = w.Write([]byte("console.log('dev tsx')"))
			return
		}
		http.NotFound(w, r)
	}))
	defer devServer.Close()

	server := &Server{}
	server.ConfigureEmbeddedFrontend(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("built static fallback")},
	})
	if err := server.ConfigureFrontendDev(devServer.URL); err != nil {
		t.Fatalf("configure frontend dev: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || resp.Body.String() != "vite dev index" {
		t.Fatalf("got %d: %q, want 'vite dev index'", resp.Code, resp.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/admin/src/main.tsx", nil)
	resp2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusOK || resp2.Body.String() != "console.log('dev tsx')" {
		t.Fatalf("got %d: %q, want tsx", resp2.Code, resp2.Body.String())
	}

	// Unreachable dev server should cleanly fall back to static/embedded files
	devServer.Close()
	req3 := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	resp3 := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp3, req3)
	if resp3.Code != http.StatusOK || resp3.Body.String() != "built static fallback" {
		t.Fatalf("got %d: %q, want fallback 'built static fallback'", resp3.Code, resp3.Body.String())
	}
}

