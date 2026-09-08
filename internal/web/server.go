// Package web serves the kander web UI: the requirements pool and the kanban
// task board in one browser page. It is deliberately separate from
// internal/tui and internal/reqtui so upstream syncs never conflict; it talks
// to the board exclusively through the exported board package APIs.
package web

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Server holds the board root and serves both the JSON API and the embedded
// single-page frontend.
type Server struct {
	Root string
}

// Handler builds the full HTTP mux: /api/* JSON endpoints plus the embedded
// frontend at /.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/requirements", s.listRequirements)
	mux.HandleFunc("GET /api/requirements/", s.getRequirement)
	mux.HandleFunc("POST /api/requirements", s.createRequirement)
	mux.HandleFunc("POST /api/requirements/import", s.importRequirements)
	mux.HandleFunc("POST /api/requirements/convert", s.convertRequirement)
	mux.HandleFunc("POST /api/requirements/complete", s.completeRequirement)
	mux.HandleFunc("GET /api/board", s.getBoard)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/tasks/", s.getTask)
	mux.HandleFunc("GET /", s.index)
	return mux
}

// writeJSON emits one JSON value with the given status code.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeError emits a plain-text error message with a status code.
func writeError(w http.ResponseWriter, status int, err error) {
	http.Error(w, err.Error(), status)
}

// idFromPath extracts the trailing path segment after the prefix.
func idFromPath(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// listenAddr renders the host:port bind string.
func listenAddr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
