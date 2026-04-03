package health

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Server exposes /healthz and /readyz endpoints for Kubernetes probes.
type Server struct {
	connected atomic.Bool
	startedAt time.Time
}

// NewServer creates a health check server.
func NewServer() *Server {
	return &Server{startedAt: time.Now()}
}

// SetConnected updates the WebSocket connection status.
func (s *Server) SetConnected(v bool) {
	s.connected.Store(v)
}

// ListenAndServe starts the health endpoint on the given address (e.g. ":8081").
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"uptime":  time.Since(s.startedAt).String(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.connected.Load() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ready"})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"status": "not_connected"})
	}
}
