package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS bins (
	hash TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT ''
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
	Title        string
	Description  string
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
	// sqlite has one writer, so one pooled connection is how goroutines are kept from failing each other with SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := addMissingBinColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// addMissingBinColumns completes a bins table written before title and description existed:
// CREATE TABLE IF NOT EXISTS never adds a column to a table that is already there, and live
// dbs exist. An old bin reads back as empty metadata.
func addMissingBinColumns(db *sql.DB) error {
	present := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('bins')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, column := range []string{"title", "description"} {
		if present[column] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE bins ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// CreateBin makes a bin carrying title and description; empty ones leave the bin
// without metadata.
func (s *Store) CreateBin(passwordHash, title, description string) (string, error) {
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
		_, err = s.db.Exec(`INSERT INTO bins (hash, password_hash, created_at, title, description) VALUES (?, ?, ?, ?, ?)`,
			hash, passwordHash, time.Now().UTC().Format(time.RFC3339), title, description)
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
	err := s.db.QueryRow(`SELECT hash, password_hash, title, description FROM bins WHERE hash=?`, hash).
		Scan(&b.Hash, &b.PasswordHash, &b.Title, &b.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return Bin{}, false, nil
	}
	if err != nil {
		return Bin{}, false, err
	}
	return b, true, nil
}

// UpdateBinMeta writes the bin's title and description in one UPDATE: a NULL argument
// leaves that field as it is, so a partial update cannot race with itself. It reports
// whether the bin exists at all.
func (s *Store) UpdateBinMeta(hash string, title, description sql.NullString) (bool, error) {
	res, err := s.db.Exec(`UPDATE bins SET title=COALESCE(?,title), description=COALESCE(?,description) WHERE hash=?`,
		title, description, hash)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
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

// FilePut is one file of a PutFiles batch: a validated path and its bytes.
type FilePut struct {
	Path    string
	Content []byte
}

// PutFiles stores an archive's whole batch in one transaction, each path with the
// same SELECT-then-upsert PutFile does so created and replaced are exact. Any
// error rolls the whole batch back: extraction stores all entries or none.
func (s *Store) PutFiles(binHash string, files []FilePut) (created, replaced int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	for _, f := range files {
		var existing int
		err := tx.QueryRow(`SELECT 1 FROM files WHERE bin_hash=? AND path=?`, binHash, f.Path).Scan(&existing)
		switch {
		case err == nil:
			replaced++
		case errors.Is(err, sql.ErrNoRows):
			created++
		default:
			return 0, 0, err
		}
		if _, err := tx.Exec(`
INSERT INTO files (bin_hash, path, content, size, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (bin_hash, path) DO UPDATE SET content=excluded.content, size=excluded.size, updated_at=excluded.updated_at`,
			binHash, f.Path, f.Content, int64(len(f.Content)), time.Now().UTC().Format(time.RFC3339)); err != nil {
			return 0, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return created, replaced, nil
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

// DeleteFile removes one file from its bin and reports whether it removed anything.
func (s *Store) DeleteFile(binHash, path string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM files WHERE bin_hash=? AND path=?`, binHash, path)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// DeleteFolder removes every file sitting under folder at any depth, in one transaction,
// and returns the removed paths in listing order. The folder is the paths starting with
// folder followed by a slash, so a file named exactly like the folder and a sibling whose
// name merely starts with it both stay. Nothing under it deletes nothing, and the answer
// is empty.
func (s *Store) DeleteFolder(binHash, folder string) ([]string, error) {
	rows, err := s.db.Query(`SELECT path FROM files WHERE bin_hash=? ORDER BY path`, binHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deleted []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		if strings.HasPrefix(path, folder+"/") {
			deleted = append(deleted, path)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(deleted) == 0 {
		return nil, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, path := range deleted {
		if _, err := tx.Exec(`DELETE FROM files WHERE bin_hash=? AND path=?`, binHash, path); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deleted, nil
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
// so the table cannot grow without bound. The read, the upsert and the prune
// run in one transaction so parallel failures cannot interleave and drop counts.
// A second writer (an old binary's direct-db management, a stray sqlite client) makes
// sqlite answer SQLITE_BUSY immediately when this deferred read transaction tries to
// upgrade, so a refused charge is retried a bounded few times: dropping it silently
// would under-count the penalty. The ip verbs no longer open the db file.
func (s *Store) RecordFailure(ip string, now time.Time) (time.Time, error) {
	until, err := s.recordFailureOnce(ip, now)
	for attempt := 2; err != nil && isBusy(err) && attempt <= 4; attempt++ {
		time.Sleep(time.Duration(attempt-1) * 25 * time.Millisecond)
		until, err = s.recordFailureOnce(ip, now)
	}
	return until, err
}

func isBusy(err error) bool {
	var dbErr *sqlite.Error
	return errors.As(err, &dbErr) && dbErr.Code() == 5 // sqlite3's SQLITE_BUSY
}

func (s *Store) recordFailureOnce(ip string, now time.Time) (time.Time, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()

	var failures int
	err = tx.QueryRow(`SELECT failures FROM ip_blocks WHERE ip=?`, ip).Scan(&failures)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	failures++
	// 48 failures already reach blockMax, and multiplying a bigger count would
	// overflow int64 nanoseconds and flip blocked_until into the past.
	var blockedUntil time.Time
	if failures >= 48 {
		blockedUntil = now.Add(blockMax)
	} else {
		blockedUntil = now.Add(blockStep * time.Duration(failures))
		if max := now.Add(blockMax); blockedUntil.After(max) {
			blockedUntil = max
		}
	}
	_, err = tx.Exec(`
INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (ip) DO UPDATE SET failures=excluded.failures, blocked_until=excluded.blocked_until, updated_at=excluded.updated_at`,
		ip, failures, blockedUntil.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	if err != nil {
		return time.Time{}, err
	}
	// RFC3339 stamps in one zone sort as strings, so the prune needs no date arithmetic in SQL.
	if _, err := tx.Exec(`DELETE FROM ip_blocks WHERE updated_at < ?`, now.Add(-blockIdlePrune).UTC().Format(time.RFC3339)); err != nil {
		return blockedUntil, err
	}
	if err := tx.Commit(); err != nil {
		return blockedUntil, err
	}
	return blockedUntil, nil
}

// IPBlock is one row of ip_blocks as an operator sees it.
type IPBlock struct {
	IP           string
	Failures     int
	BlockedUntil string
	UpdatedAt    string
}

// ListBlocks returns every row of ip_blocks, ordered by ip. Rows whose blocked_until is
// past are included on purpose: their failure count still shapes the next penalty.
func (s *Store) ListBlocks() ([]IPBlock, error) {
	rows, err := s.db.Query(`SELECT ip, failures, blocked_until, updated_at FROM ip_blocks ORDER BY ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []IPBlock
	for rows.Next() {
		var b IPBlock
		if err := rows.Scan(&b.IP, &b.Failures, &b.BlockedUntil, &b.UpdatedAt); err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

// GetBlock returns the one row of ip_blocks whose ip is exactly ip, or reports that the
// table has no such row. It reads only; the control socket's GET /ip/{ip} answers from it.
func (s *Store) GetBlock(ip string) (IPBlock, bool, error) {
	var b IPBlock
	err := s.db.QueryRow(`SELECT ip, failures, blocked_until, updated_at FROM ip_blocks WHERE ip=?`, ip).
		Scan(&b.IP, &b.Failures, &b.BlockedUntil, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IPBlock{}, false, nil
	}
	if err != nil {
		return IPBlock{}, false, err
	}
	return b, true, nil
}

// UnblockIP deletes one row and reports whether a row went. The failure count goes with
// it, so the next bad key from that address starts again at blockStep.
func (s *Store) UnblockIP(ip string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM ip_blocks WHERE ip=?`, ip)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ClearBlocks deletes every row and reports how many went.
func (s *Store) ClearBlocks() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM ip_blocks`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
