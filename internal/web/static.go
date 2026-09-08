package web

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML string

// index serves the embedded single-page frontend.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}
