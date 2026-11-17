package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// apiRoutes dispatches everything under /api. "api" is no eight-hex hash, so the prefix can
// never name or shadow a bin, same reasoning as the static routes below it. The skeleton
// answers POST /api/key and GET|HEAD /api/health today; /api/bins is the name Tasks 3–7 fill
// in, so that path is known but answers no method yet. A spelling the dispatch does not name
// — trailing-slash variants included — is the plain 404 unknown bin, and a known path on a
// wrong method is the plain 405.
func (s *server) apiRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/key":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiKeyCheck(w, r)
	case "/api/health":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiHealth(w, r)
	case "/api/bins":
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.Error(w, "unknown bin", http.StatusNotFound)
	}
}

// apiKeyCheck answers the key check: its guard is requireKey's verbatim, so a wrong key
// through it charges exactly like a wrong key through a write, and a match creates nothing,
// charges nothing and answers 204 with an empty body.
func (s *server) apiKeyCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireKey(w, r) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// healthBody is the /api/health answer.
type healthBody struct {
	Status string `json:"status"`
}

// apiHealth is the container probe's route: it says nothing about bins, needs no key and
// charges nothing, so it answers even while its address is blocked.
func (s *server) apiHealth(w http.ResponseWriter, r *http.Request) {
	if err := writeJSON(w, http.StatusOK, healthBody{Status: "ok"}); err != nil {
		log.Printf("encode the health answer: %v", err)
		http.Error(w, "could not answer health", http.StatusInternalServerError)
	}
}

// writeJSON answers status with v as JSON plus a trailing newline, the shape every JSON
// success on /api carries. Refusals stay http.Error plain text; only success bodies are JSON.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	out, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(append(out, '\n'))
	return err
}
