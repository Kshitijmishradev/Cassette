// Package httpui serves Cassette's JSON API and embedded React application.
package httpui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Kshitijmishradev/cassette/internal/api"
)

// Handler serves the same file-shaped API paths used by static exports.
type Handler struct {
	Builder *api.Builder
	Assets  fs.FS
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if !localHost(r.Host) {
		writeError(w, http.StatusForbidden, "non-local Host is not allowed")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || u.User != nil || !strings.EqualFold(u.Host, r.Host) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			writeError(w, http.StatusForbidden, "cross-origin requests are not allowed")
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusForbidden, "cross-site requests are not allowed")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "only GET is supported")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveAPI(w, r)
		return
	}
	h.serveAsset(w, r)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if r.URL.Path == api.PathSuite {
		v, err := h.Builder.BuildSuite()
		h.writeJSON(w, v, err)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, api.RunDir)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		writeError(w, http.StatusNotFound, "API endpoint not found")
		return
	}
	name := parts[0]

	switch {
	case len(parts) == 2 && parts[1] == "run.json":
		v, err := h.Builder.BuildRun(name)
		h.writeJSON(w, v, err)
	case len(parts) == 2 && parts[1] == "diff.json":
		v, err := h.Builder.BuildDiff(name)
		h.writeJSON(w, v, err)
	case len(parts) == 3 && parts[1] == "calls" && strings.HasSuffix(parts[2], ".json"):
		raw := strings.TrimSuffix(parts[2], ".json")
		index, err := strconv.Atoi(raw)
		if err != nil || index < 0 {
			writeError(w, http.StatusNotFound, "call not found")
			return
		}
		v, err := h.Builder.BuildCall(name, index)
		h.writeJSON(w, v, err)
	default:
		writeError(w, http.StatusNotFound, "API endpoint not found")
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, api.ErrDiffUnavailable) || os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		writeError(w, status, http.StatusText(status))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if info, err := fs.Stat(h.Assets, path); err == nil && !info.IsDir() {
		http.FileServer(http.FS(h.Assets)).ServeHTTP(w, r)
		return
	}

	// Hash navigation normally keeps routes out of the HTTP path. The index
	// fallback still makes copied links resilient if routing changes later.
	index, err := fs.ReadFile(h.Assets, "index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "embedded UI is missing index.html")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Error: message})
}

// Do not resolve arbitrary hostnames: DNS rebinding can point an attacker's
// domain at loopback while preserving that domain as the browser origin.
func localHost(authority string) bool {
	host := authority
	if strings.Contains(authority, ":") {
		var err error
		host, _, err = net.SplitHostPort(authority)
		if err != nil {
			return false
		}
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
