package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS bins (
	hash TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS files (
	bin_hash TEXT NOT NULL,
	path TEXT NOT NULL,
	content BLOB NOT NULL,
	size INTEGER NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (bin_hash, path)
);
CREATE TABLE IF NOT EXISTS ip_blocks (
	ip TEXT PRIMARY KEY,
	failures INTEGER NOT NULL,
	blocked_until TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

type Store struct {
	db *sql.DB
}

type Bin struct {
	Hash         string
	PasswordHash string
}

type FileEntry struct {
	Path      string
	Size      int64
	UpdatedAt string
}

// openStore opens the single sqlite file every bin lives in. busy_timeout keeps
// parallel PUTs from failing with SQLITE_BUSY.
func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateBin(passwordHash string) (string, error) {
	for range 8 {
		hash, err := newBinHash()
		if err != nil {
			return "", err
		}
		var taken bool
		if err := s.db.QueryRow(`SELECT 1 FROM bins WHERE hash=?`, hash).Scan(&taken); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if taken {
			continue
		}
		_, err = s.db.Exec(`INSERT INTO bins (hash, password_hash, created_at) VALUES (?, ?, ?)`,
			hash, passwordHash, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return "", err
		}
		return hash, nil
	}
	return "", errors.New("no free bin hash")
}

func newBinHash() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Store) GetBin(hash string) (Bin, bool, error) {
	var b Bin
	err := s.db.QueryRow(`SELECT hash, password_hash FROM bins WHERE hash=?`, hash).Scan(&b.Hash, &b.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Bin{}, false, nil
	}
	if err != nil {
		return Bin{}, false, err
	}
	return b, true, nil
}

// PutFile upserts a file and reports whether it replaced an existing one.
func (s *Store) PutFile(binHash, path string, content []byte) (bool, error) {
	var existing int
	err := s.db.QueryRow(`SELECT 1 FROM files WHERE bin_hash=? AND path=?`, binHash, path).Scan(&existing)
	replaced := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = s.db.Exec(`
INSERT INTO files (bin_hash, path, content, size, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (bin_hash, path) DO UPDATE SET content=excluded.content, size=excluded.size, updated_at=excluded.updated_at`,
		binHash, path, content, int64(len(content)), time.Now().UTC().Format(time.RFC3339))
	return replaced, err
}

func (s *Store) GetFile(binHash, path string) ([]byte, bool, error) {
	var content []byte
	err := s.db.QueryRow(`SELECT content FROM files WHERE bin_hash=? AND path=?`, binHash, path).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func (s *Store) ListFiles(binHash string) ([]FileEntry, error) {
	rows, err := s.db.Query(`SELECT path, size, updated_at FROM files WHERE bin_hash=? ORDER BY path`, binHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileEntry
	for rows.Next() {
		var f FileEntry
		if err := rows.Scan(&f.Path, &f.Size, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// IsBlocked reports whether ip is kept away from key-checked writes right now,
// and when that block ends. A missing, unreadable or unparsable row reads as
// not blocked: the block path must never fail an ordinary request.
func (s *Store) IsBlocked(ip string, now time.Time) (time.Time, bool) {
	var stored string
	if err := s.db.QueryRow(`SELECT blocked_until FROM ip_blocks WHERE ip=?`, ip).Scan(&stored); err != nil {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339Nano, stored)
	if err != nil {
		return time.Time{}, false
	}
	return until, until.After(now)
}

// RecordFailure charges ip one API key failure and reports when the new block
// ends. The stored count is never reset, so blocked_until walks from blockStep
// up to blockMax. Rows idle past blockIdlePrune are deleted in the same call
// so the table cannot grow without bound.
func (s *Store) RecordFailure(ip string, now time.Time) (time.Time, error) {
	var failures int
	err := s.db.QueryRow(`SELECT failures FROM ip_blocks WHERE ip=?`, ip).Scan(&failures)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	failures++
	blockedUntil := now.Add(blockStep * time.Duration(failures))
	if max := now.Add(blockMax); blockedUntil.After(max) {
		blockedUntil = max
	}
	_, err = s.db.Exec(`
INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (ip) DO UPDATE SET failures=excluded.failures, blocked_until=excluded.blocked_until, updated_at=excluded.updated_at`,
		ip, failures, blockedUntil.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	if err != nil {
		return time.Time{}, err
	}
	// RFC3339 stamps in one zone sort as strings, so the prune needs no date arithmetic in SQL.
	if _, err := s.db.Exec(`DELETE FROM ip_blocks WHERE updated_at < ?`, now.Add(-blockIdlePrune).UTC().Format(time.RFC3339)); err != nil {
		return blockedUntil, err
	}
	return blockedUntil, nil
}
