package main

import (
	"crypto/subtle"
	"io"
	"log"
	"net/http"
)

const apiKeyHeader = "X-API-Key"

type server struct {
	store  *Store
	apiKey []byte
}

func (s *server) mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /", s.createBin)
	return mux
}

// createBin makes a bin and answers with the path it lives under.
func (s *server) createBin(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(apiKeyHeader)), s.apiKey) != 1 {
		http.Error(w, "invalid API key", http.StatusForbidden)
		return
	}

	hash, err := s.store.CreateBin("")
	if err != nil {
		log.Printf("create bin: %v", err)
		http.Error(w, "could not create bin", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, "/"+hash+"/")
}
