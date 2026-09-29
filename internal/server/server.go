// Package server is the HTTP API that receives data from other devices.
package server

import (
	"encoding/json"
	"net/http"

	"dsync/internal/proto"
)

type Server struct {
	Self   func() proto.Device
	OnText func(m proto.TextMessage, remoteAddr string)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", s.info)
	mux.HandleFunc("POST /api/v1/text", s.text)
	return mux
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Self())
}

func (s *Server) text(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, proto.MaxTextBytes+4096)
	var m proto.TextMessage
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if m.Text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	if s.OnText != nil {
		s.OnText(m, r.RemoteAddr)
	}
	w.WriteHeader(http.StatusNoContent)
}
