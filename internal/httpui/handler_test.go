package httpui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Kshitijmishradev/cassette/internal/api"
	"github.com/Kshitijmishradev/cassette/internal/safety"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		Builder: &api.Builder{SuiteDir: t.TempDir(), Classifier: safety.New(safety.Config{}), Live: true},
		Assets: fstest.MapFS{
			"index.html":    &fstest.MapFile{Data: []byte("<main>cassette</main>")},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log('cassette')")},
		},
	}
}

func TestSuiteEndpointIsLive(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost"+api.PathSuite, nil)
	w := httptest.NewRecorder()
	testHandler(t).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"live":true`) {
		t.Fatalf("suite is not marked live: %s", w.Body.String())
	}
}

func TestMissingRunIsJSON404(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/runs/missing/run.json", nil)
	w := httptest.NewRecorder()
	testHandler(t).ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content type = %q", got)
	}
}

func TestAssetAndSPAFallback(t *testing.T) {
	h := testHandler(t)
	for _, path := range []string{"/assets/app.js", "/future/route"} {
		r := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, w.Code)
		}
	}
}

func TestLocalViewerRejectsRebindingAndCrossOriginReads(t *testing.T) {
	tests := []struct {
		host, origin, site string
		status             int
	}{
		{"localhost:7070", "", "", 200},
		{"127.0.0.1:7070", "http://127.0.0.1:7070", "same-origin", 200},
		{"[::1]:7070", "", "", 200},
		{"attacker.example:7070", "", "", 403},
		{"localhost.attacker.example:7070", "", "", 403},
		{"localhost:7070", "http://attacker.example", "", 403},
		{"localhost:7070", "null", "", 403},
		{"localhost:7070", "http://localhost:9999", "", 403},
		{"localhost:7070", "", "cross-site", 403},
	}
	for _, tc := range tests {
		r := httptest.NewRequest("GET", "http://localhost/api/suite.json", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		testHandler(t).ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%+v: status %d", tc, w.Code)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("security headers missing")
		}
	}
}
