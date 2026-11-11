package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
)

// controlMux is the json API the control socket speaks: the block table as four requests,
// and nothing else. It is its own mux on its own listener, so no bin, file or healthz
// route is reachable through it, and the only authority it needs is the right to connect.
func controlMux(store *Store) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ip", func(w http.ResponseWriter, r *http.Request) {
		blocks, err := store.ListBlocks()
		if err != nil {
			controlStoreError(w, err)
			return
		}
		writeControlJSON(w, http.StatusOK, blocksAsRows(blocks))
	})

	mux.HandleFunc("GET /ip/{ip}", func(w http.ResponseWriter, r *http.Request) {
		ip := r.PathValue("ip")
		block, ok, err := store.GetBlock(ip)
		if err != nil {
			controlStoreError(w, err)
			return
		}
		if !ok {
			controlNoBlock(w, ip)
			return
		}
		writeControlJSON(w, http.StatusOK, blockAsRow(block))
	})

	mux.HandleFunc("DELETE /ip/{ip}", func(w http.ResponseWriter, r *http.Request) {
		ip := r.PathValue("ip")
		gone, err := store.UnblockIP(ip)
		if err != nil {
			controlStoreError(w, err)
			return
		}
		if !gone {
			controlNoBlock(w, ip)
			return
		}
		writeControlJSON(w, http.StatusOK, unblockedBody{Unblocked: ip})
	})

	mux.HandleFunc("DELETE /ip", func(w http.ResponseWriter, r *http.Request) {
		cleared, err := store.ClearBlocks()
		if err != nil {
			controlStoreError(w, err)
			return
		}
		writeControlJSON(w, http.StatusOK, clearedBody{Cleared: cleared})
	})

	return mux
}

// errorBody, unblockedBody and clearedBody are the answers that are not rows.
type errorBody struct {
	Error string `json:"error"`
}

type unblockedBody struct {
	Unblocked string `json:"unblocked"`
}

type clearedBody struct {
	Cleared int64 `json:"cleared"`
}

// blocksAsRows renders rows in the four-field json shape --json prints, and [] when the
// table has no row at all.
func blocksAsRows(blocks []IPBlock) []jsonIPBlock {
	rows := make([]jsonIPBlock, 0, len(blocks))
	for _, b := range blocks {
		rows = append(rows, blockAsRow(b))
	}
	return rows
}

func blockAsRow(b IPBlock) jsonIPBlock {
	return jsonIPBlock{IP: b.IP, Failures: b.Failures, BlockedUntil: b.BlockedUntil, UpdatedAt: b.UpdatedAt}
}

// writeControlJSON answers with a json body, which is the only shape this API speaks.
func writeControlJSON(w http.ResponseWriter, status int, body any) {
	out, err := json.Marshal(body)
	if err != nil {
		// Only a value json cannot encode gets here, and the answer is still a 500.
		log.Printf("control socket: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(out, '\n'))
}

// controlStoreError answers a store failure with 500 and the error's own words: the server
// can do no better than say what its read or write did.
func controlStoreError(w http.ResponseWriter, err error) {
	log.Printf("control socket: %v", err)
	writeControlJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
}

// controlNoBlock is the one answer for an address the table does not have. The server
// treats the path value as an opaque key, so it names back exactly what was asked for.
func controlNoBlock(w http.ResponseWriter, ip string) {
	writeControlJSON(w, http.StatusNotFound, errorBody{Error: fmt.Sprintf("no block for %s", ip)})
}

// listenControl binds the control socket and keeps it 0600, because connecting it is the
// whole authority the ip verbs need. Startup rules: a socket that answers a dial belongs to
// a live serve and is refused; a socket nobody answers on is stale and gets replaced; a path
// that is not a socket is refused and left exactly where it is.
func listenControl(socketPath string) (net.Listener, error) {
	info, err := os.Lstat(socketPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Nothing at the path yet: bind it.
	case info != nil && info.Mode()&os.ModeSocket == 0:
		return nil, fmt.Errorf("control socket path %s exists and is not a socket", socketPath)
	case err != nil:
		return nil, fmt.Errorf("control socket %s: %w", socketPath, err)
	default:
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr == nil {
			conn.Close()
			return nil, fmt.Errorf("another sandbin serve is already listening on %s", socketPath)
		}
		if errors.Is(dialErr, os.ErrPermission) {
			// A socket this run may not reach is not a stale one: refusing keeps a second
			// serve from unlinking another owner's live listener and taking its address.
			return nil, fmt.Errorf("another sandbin serve is already listening on %s: %v", socketPath, dialErr)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale control socket %s: %w", socketPath, err)
		}
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on control socket %s: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod control socket %s: %w", socketPath, err)
	}
	return ln, nil
}
