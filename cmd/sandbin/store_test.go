package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := openStore(filepath.Join(t.TempDir(), "sandbin.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateBinHash(t *testing.T) {
	store := newTestStore(t)

	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(hash) {
		t.Errorf("hash %q is not 8 lowercase hex chars", hash)
	}

	other, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if other == hash {
		t.Errorf("two bins got the same hash %q", hash)
	}
}

func TestGetBin(t *testing.T) {
	store := newTestStore(t)

	hash, err := store.CreateBin("hashed-password", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	bin, ok, err := store.GetBin(hash)
	if err != nil {
		t.Fatalf("GetBin: %v", err)
	}
	if !ok {
		t.Fatalf("GetBin(%q) not found", hash)
	}
	if bin.PasswordHash != "hashed-password" {
		t.Errorf("PasswordHash = %q, want %q", bin.PasswordHash, "hashed-password")
	}

	if _, ok, err := store.GetBin("deadbeef"); err != nil || ok {
		t.Errorf("GetBin(unknown) = ok %v, err %v; want not found, no error", ok, err)
	}
}

func TestCreateBinStoresMetadata(t *testing.T) {
	store := newTestStore(t)

	hash, err := store.CreateBin("hashed-password", "Test report", "findings of the audit")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	bin, ok, err := store.GetBin(hash)
	if err != nil {
		t.Fatalf("GetBin: %v", err)
	}
	if !ok {
		t.Fatalf("GetBin(%q) not found", hash)
	}
	if bin.Title != "Test report" {
		t.Errorf("Title = %q, want %q", bin.Title, "Test report")
	}
	if bin.Description != "findings of the audit" {
		t.Errorf("Description = %q, want %q", bin.Description, "findings of the audit")
	}

	// A bin made without metadata is a bin with empty metadata, not a bin missing fields.
	plain, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	bin, ok, err = store.GetBin(plain)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) = ok %v, err %v", plain, ok, err)
	}
	if bin.Title != "" || bin.Description != "" {
		t.Errorf("metadata of a plain bin = %q / %q, want both empty", bin.Title, bin.Description)
	}
}

// TestUpdateBinMeta walks the keep / replace / clear cases one field at a time, which is
// what COALESCE in one UPDATE is for, and reports a missing bin as false.
func TestUpdateBinMeta(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "first title", "first description")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	other, err := store.CreateBin("", "keep me", "keep me too")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	// A NULL argument is "leave that field alone", an empty string is "clear it".
	for i, tc := range []struct {
		title, description         sql.NullString
		wantTitle, wantDescription string
	}{
		{sql.NullString{}, sql.NullString{}, "first title", "first description"},
		{sql.NullString{String: "second title", Valid: true}, sql.NullString{}, "second title", "first description"},
		{sql.NullString{}, sql.NullString{String: "second description", Valid: true}, "second title", "second description"},
		{sql.NullString{String: "", Valid: true}, sql.NullString{}, "", "second description"},
		{sql.NullString{}, sql.NullString{String: "", Valid: true}, "", ""},
		{sql.NullString{String: "third title", Valid: true}, sql.NullString{String: "third description", Valid: true}, "third title", "third description"},
	} {
		found, err := store.UpdateBinMeta(hash, tc.title, tc.description)
		if err != nil {
			t.Fatalf("UpdateBinMeta #%d: %v", i+1, err)
		}
		if !found {
			t.Fatalf("UpdateBinMeta #%d = false, want the bin found", i+1)
		}
		bin, ok, err := store.GetBin(hash)
		if err != nil || !ok {
			t.Fatalf("GetBin after update #%d: ok %v, err %v", i+1, ok, err)
		}
		if bin.Title != tc.wantTitle || bin.Description != tc.wantDescription {
			t.Errorf("after update #%d: metadata = %q / %q, want %q / %q",
				i+1, bin.Title, bin.Description, tc.wantTitle, tc.wantDescription)
		}
	}

	// An unknown bin is reported as absent rather than a silent no-op, and no other row moves.
	found, err := store.UpdateBinMeta("deadbeef", sql.NullString{String: "x", Valid: true}, sql.NullString{String: "y", Valid: true})
	if err != nil {
		t.Fatalf("UpdateBinMeta(unknown): %v", err)
	}
	if found {
		t.Errorf("UpdateBinMeta(unknown) = true, want false")
	}
	bin, ok, err := store.GetBin(other)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q): ok %v, err %v", other, ok, err)
	}
	if bin.Title != "keep me" || bin.Description != "keep me too" {
		t.Errorf("the other bin's metadata = %q / %q, want it untouched", bin.Title, bin.Description)
	}
}

func TestPutGetFile(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	replaced, err := store.PutFile(hash, "notes.md", []byte("# first"))
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if replaced {
		t.Errorf("first PutFile reported a replacement")
	}

	content, ok, err := store.GetFile(hash, "notes.md")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !ok {
		t.Fatalf("GetFile returned not found")
	}
	if string(content) != "# first" {
		t.Errorf("content = %q, want %q", content, "# first")
	}

	replaced, err = store.PutFile(hash, "notes.md", []byte("# second"))
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if !replaced {
		t.Errorf("second PutFile did not report a replacement")
	}
	content, ok, err = store.GetFile(hash, "notes.md")
	if err != nil || !ok {
		t.Fatalf("GetFile after replace: ok %v, err %v", ok, err)
	}
	if string(content) != "# second" {
		t.Errorf("content = %q, want %q", content, "# second")
	}

	if _, ok, err := store.GetFile(hash, "missing.txt"); err != nil || ok {
		t.Errorf("GetFile(missing) = ok %v, err %v; want not found, no error", ok, err)
	}
}

func TestPutFiles(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	// One path in the batch already exists, so created and replaced must count apart.
	if replaced, err := store.PutFile(hash, "a.txt", []byte("the old a")); err != nil || replaced {
		t.Fatalf("PutFile seed: replaced %v, err %v", replaced, err)
	}
	if replaced, err := store.PutFile(hash, "keep.txt", []byte("leave me alone")); err != nil || replaced {
		t.Fatalf("PutFile seed: replaced %v, err %v", replaced, err)
	}

	created, replaced, err := store.PutFiles(hash, []FilePut{
		{Path: "a.txt", Content: []byte("the new a")},
		{Path: "sub/new.txt", Content: []byte("new")},
	})
	if err != nil {
		t.Fatalf("PutFiles: %v", err)
	}
	if created != 1 || replaced != 1 {
		t.Errorf("PutFiles counts = created %d, replaced %d; want 1, 1", created, replaced)
	}
	for _, want := range []struct{ path, content string }{
		{"a.txt", "the new a"},
		{"sub/new.txt", "new"},
		{"keep.txt", "leave me alone"}, // untouched by the batch
	} {
		content, ok, err := store.GetFile(hash, want.path)
		if err != nil || !ok {
			t.Fatalf("GetFile(%q): ok %v, err %v", want.path, ok, err)
		}
		if string(content) != want.content {
			t.Errorf("%q = %q, want %q", want.path, content, want.content)
		}
	}

	// The same batch again: every path is now a replacement.
	if created, replaced, err := store.PutFiles(hash, []FilePut{
		{Path: "a.txt", Content: []byte("third")},
		{Path: "sub/new.txt", Content: []byte("third")},
	}); err != nil || created != 0 || replaced != 2 {
		t.Errorf("second PutFiles = created %d, replaced %d, err %v; want 0, 2, nil", created, replaced, err)
	}

	// An empty batch is a no-op.
	if created, replaced, err := store.PutFiles(hash, nil); err != nil || created != 0 || replaced != 0 {
		t.Errorf("empty PutFiles = created %d, replaced %d, err %v; want 0, 0, nil", created, replaced, err)
	}
}

// TestPutFilesRollback is the all-or-nothing rule the extractor depends on: a
// failure on the second of three files leaves the bin exactly as it was.
func TestPutFilesRollback(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if _, err := store.PutFile(hash, "seed.txt", []byte("seed")); err != nil {
		t.Fatalf("PutFile seed: %v", err)
	}

	// files.content is NOT NULL, so a nil Content fails the INSERT mid-batch.
	created, replaced, err := store.PutFiles(hash, []FilePut{
		{Path: "one.txt", Content: []byte("one")},
		{Path: "bad.txt", Content: nil},
		{Path: "two.txt", Content: []byte("two")},
	})
	if err == nil {
		t.Fatalf("PutFiles with a nil Content succeeded, want an error")
	}
	if created != 0 || replaced != 0 {
		t.Errorf("failed PutFiles counts = created %d, replaced %d; want 0, 0", created, replaced)
	}
	for _, path := range []string{"one.txt", "bad.txt", "two.txt"} {
		if _, ok, err := store.GetFile(hash, path); err != nil || ok {
			t.Errorf("GetFile(%q) after rollback = ok %v, err %v; want rolled back", path, ok, err)
		}
	}
	if content, ok, err := store.GetFile(hash, "seed.txt"); err != nil || !ok || string(content) != "seed" {
		t.Errorf("seed.txt after rollback = ok %v, err %v, content %q; want untouched", ok, err, content)
	}
}

// TestPutFilesOnClosedStore is the Begin failure answer: no counts, an error.
func TestPutFilesOnClosedStore(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	created, replaced, err := store.PutFiles(hash, []FilePut{{Path: "a.txt", Content: []byte("a")}})
	if err == nil {
		t.Fatalf("PutFiles on a closed store succeeded, want an error")
	}
	if created != 0 || replaced != 0 {
		t.Errorf("counts = created %d, replaced %d; want 0, 0", created, replaced)
	}
}

// TestDeleteFile removes exactly one file: the row goes, its GetFile lookup fails, every
// other file and another bin's file of the same name survive, the bin row and its metadata
// are untouched, and a second delete of the same path reports nothing gone.
func TestDeleteFile(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "kept title", "kept description")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	other, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	for _, seed := range []struct{ bin, path string }{
		{hash, "notes.md"}, {hash, "hello/x.txt"}, {hash, "other.txt"},
		{other, "notes.md"}, // the same name in another bin
	} {
		if _, err := store.PutFile(seed.bin, seed.path, []byte("body of "+seed.path)); err != nil {
			t.Fatalf("PutFile(%q in %q): %v", seed.path, seed.bin, seed.path)
		}
	}

	gone, err := store.DeleteFile(hash, "notes.md")
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if !gone {
		t.Errorf("DeleteFile(notes.md) = false, want true")
	}
	if _, ok, err := store.GetFile(hash, "notes.md"); err != nil || ok {
		t.Errorf("GetFile(notes.md) after the delete = ok %v, err %v; want gone, no error", ok, err)
	}
	for _, path := range []string{"hello/x.txt", "other.txt"} {
		content, ok, err := store.GetFile(hash, path)
		if err != nil || !ok || string(content) != "body of "+path {
			t.Errorf("%q after the delete = ok %v, err %v, content %q; want untouched", path, ok, err, content)
		}
	}
	if content, ok, err := store.GetFile(other, "notes.md"); err != nil || !ok || string(content) != "body of notes.md" {
		t.Errorf("the other bin's notes.md = ok %v, err %v, content %q; want untouched", ok, err, content)
	}

	// A second delete of the same path removes nothing and says so.
	if gone, err := store.DeleteFile(hash, "notes.md"); err != nil || gone {
		t.Errorf("second DeleteFile = gone %v, err %v; want false, no error", gone, err)
	}

	// The bin row and its metadata are untouched by the delete.
	bin, ok, err := store.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin after the delete = ok %v, err %v", ok, err)
	}
	if bin.Title != "kept title" || bin.Description != "kept description" {
		t.Errorf("bin metadata after the delete = %q / %q, want it untouched", bin.Title, bin.Description)
	}
}

// TestDeleteFolder walks the recursion and the boundary: every file under the folder goes,
// the returned list is sorted, a sibling whose name merely starts with the folder name
// stays, a file named exactly like the folder stays, an empty match deletes nothing, and a
// second delete of the same folder returns empty.
func TestDeleteFolder(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "kept title", "kept description")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	for _, path := range []string{"hello/b.txt", "hello/deep/x", "hello", "helloworld/x", "other.txt"} {
		if _, err := store.PutFile(hash, path, []byte("body of "+path)); err != nil {
			t.Fatalf("PutFile(%q): %v", path, err)
		}
	}

	deleted, err := store.DeleteFolder(hash, "hello")
	if err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	want := []string{"hello/b.txt", "hello/deep/x"}
	if len(deleted) != len(want) {
		t.Fatalf("DeleteFolder deleted %v, want %v", deleted, want)
	}
	for i := range want {
		if deleted[i] != want[i] {
			t.Errorf("deleted[%d] = %q, want %q", i, deleted[i], want[i])
		}
	}
	for _, path := range want {
		if _, ok, err := store.GetFile(hash, path); err != nil || ok {
			t.Errorf("GetFile(%q) after the folder delete = ok %v, err %v; want gone", path, ok, err)
		}
	}
	// helloworld/x is never caught by DeleteFolder(hello), and a file named exactly like
	// the folder survives it.
	for _, path := range []string{"hello", "helloworld/x", "other.txt"} {
		content, ok, err := store.GetFile(hash, path)
		if err != nil || !ok || string(content) != "body of "+path {
			t.Errorf("%q after the folder delete = ok %v, err %v, content %q; want untouched", path, ok, err, content)
		}
	}

	// A second delete of the now-empty folder returns empty and touches nothing.
	if deleted, err := store.DeleteFolder(hash, "hello"); err != nil || len(deleted) != 0 {
		t.Errorf("second DeleteFolder = %v, err %v; want empty, no error", deleted, err)
	}
	// A folder name nothing lives under deletes nothing at all.
	if deleted, err := store.DeleteFolder(hash, "nope"); err != nil || len(deleted) != 0 {
		t.Errorf("DeleteFolder(nope) = %v, err %v; want empty, no error", deleted, err)
	}
	if files, err := store.ListFiles(hash); err != nil || len(files) != 3 {
		t.Errorf("the bin holds %v files after the refusals, want the 3 survivors", files)
	}

	// The folder delete does not disturb the bin row or its metadata.
	bin, ok, err := store.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin after the folder delete = ok %v, err %v", ok, err)
	}
	if bin.Title != "kept title" || bin.Description != "kept description" {
		t.Errorf("bin metadata after the folder delete = %q / %q, want it untouched", bin.Title, bin.Description)
	}
}

func TestListFiles(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("", "", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	for _, path := range []string{"b.txt", "sub/a.txt", "a.txt"} {
		if _, err := store.PutFile(hash, path, []byte(path)); err != nil {
			t.Fatalf("PutFile(%q): %v", path, err)
		}
	}

	files, err := store.ListFiles(hash)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	want := []string{"a.txt", "b.txt", "sub/a.txt"}
	if len(files) != len(want) {
		t.Fatalf("ListFiles returned %d files, want %d", len(files), len(want))
	}
	for i, f := range files {
		if f.Path != want[i] {
			t.Errorf("files[%d].Path = %q, want %q", i, f.Path, want[i])
		}
		if f.Size != int64(len(want[i])) {
			t.Errorf("files[%d].Size = %d, want %d", i, f.Size, len(want[i]))
		}
		if f.UpdatedAt == "" {
			t.Errorf("files[%d].UpdatedAt is empty", i)
		}
	}
}

// ipBlockRow reads one ip_blocks row for a test and reports whether it exists.
func ipBlockRow(t *testing.T, store *Store, ip string) (int, bool) {
	t.Helper()
	var failures int
	err := store.db.QueryRow(`SELECT failures FROM ip_blocks WHERE ip=?`, ip).Scan(&failures)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read ip_blocks row for %q: %v", ip, err)
	}
	return failures, true
}

func TestRecordFailureEscalates(t *testing.T) {
	store := newTestStore(t)
	// One fixed now for every failure: the count must stack 30, 60, 90 minutes
	// and stop growing at blockMax while the count itself is never reset.
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	for i := range 49 {
		want := time.Duration(i+1) * blockStep
		if want > blockMax {
			want = blockMax
		}
		until, err := store.RecordFailure("203.0.113.7", now)
		if err != nil {
			t.Fatalf("RecordFailure #%d: %v", i+1, err)
		}
		if got := until.Sub(now); got != want {
			t.Errorf("failure #%d blocks for %v, want %v", i+1, got, want)
		}
	}

	if failures, ok := ipBlockRow(t, store, "203.0.113.7"); !ok || failures != 49 {
		t.Errorf("row after 49 failures = failures %d, ok %v; want 49 failures, present", failures, ok)
	}
}

// TestRecordFailureParallel hammers one address from 50 goroutines at once. The old
// SELECT-then-UPSERT lost counts under this load (a reviewer measured 34 of 50, 12
// under -race) and leaked SQLITE_BUSY, so every failure must be charged and no call
// may return an error.
func TestRecordFailureParallel(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	const calls = 50
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = store.RecordFailure("203.0.113.7", now)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("RecordFailure #%d: %v", i+1, err)
		}
	}
	if failures, ok := ipBlockRow(t, store, "203.0.113.7"); !ok || failures != calls {
		t.Fatalf("after %d parallel failures = failures %d, ok %v; want %d failures, present", calls, failures, ok, calls)
	}

	want := time.Duration(calls) * blockStep
	if want > blockMax {
		want = blockMax
	}
	var untilRaw, updatedRaw string
	if err := store.db.QueryRow(`SELECT blocked_until, updated_at FROM ip_blocks WHERE ip=?`, "203.0.113.7").
		Scan(&untilRaw, &updatedRaw); err != nil {
		t.Fatalf("read the stored block: %v", err)
	}
	until, err := time.Parse(time.RFC3339Nano, untilRaw)
	if err != nil {
		t.Fatalf("parse blocked_until %q: %v", untilRaw, err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedRaw)
	if err != nil {
		t.Fatalf("parse updated_at %q: %v", updatedRaw, err)
	}
	if got := until.Sub(updated); got != want {
		t.Errorf("stored block after %d parallel failures = %v, want %v", calls, got, want)
	}
}

// TestRecordFailureRetriesALockedWrite plays the reviewer's second-writer case: a
// management verb (here a second store on the same file) holds the write lock, and
// sqlite answers SQLITE_BUSY immediately to this deferred read transaction's upgrade
// instead of queueing it. The charge must survive that as a retry, exactly once.
func TestRecordFailureRetriesALockedWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	holder, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()

	now := time.Now().UTC()
	tx, err := holder.db.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)`,
		"198.51.100.1", 1, now.Add(blockStep).Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		tx.Rollback()
		t.Fatalf("holder write: %v", err)
	}
	// The lock is what an unblock or clear holds; it goes away while the retries are running.
	time.AfterFunc(40*time.Millisecond, func() { tx.Commit() })

	if _, err := store.RecordFailure("203.0.113.60", now); err != nil {
		t.Fatalf("RecordFailure while the write lock was held: %v", err)
	}
	if failures, ok := ipBlockRow(t, store, "203.0.113.60"); !ok || failures != 1 {
		t.Errorf("charge through the lock = failures %d, ok %v; want exactly 1", failures, ok)
	}
}

// TestRecordFailureCapsOverflow seeds the row a long-lived hammerer eventually
// reaches. A reviewer stored failures=6000000 and got duration -2124095h4m33s,
// blocked_until 1784-07-18, and IsBlocked false: the multiplication overflowed
// int64 nanoseconds, so the penalty arithmetic must be capped before it runs.
func TestRecordFailureCapsOverflow(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	if _, err := store.db.Exec(`
INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)`,
		"203.0.113.7", 6000000,
		now.Add(blockMax).UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed failures=6000000: %v", err)
	}

	until, err := store.RecordFailure("203.0.113.7", now)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if got := until.Sub(now); got != blockMax {
		t.Errorf("block after 6000001 failures = %v, want exactly %v", got, blockMax)
	}
	if failures, ok := ipBlockRow(t, store, "203.0.113.7"); !ok || failures != 6000001 {
		t.Errorf("row after the failure = failures %d, ok %v; want 6000001, present", failures, ok)
	}
	if got, blocked := store.IsBlocked("203.0.113.7", now); !blocked {
		t.Errorf("IsBlocked after the overflow-capped failure = until %v, blocked %v; want blocked", got, blocked)
	}
}

func TestIsBlockedEndsAtBlockedUntil(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	if until, blocked := store.IsBlocked("203.0.113.7", now); blocked || !until.IsZero() {
		t.Fatalf("unknown address = until %v, blocked %v; want zero time, not blocked", until, blocked)
	}

	until, err := store.RecordFailure("203.0.113.7", now)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	want := now.Add(blockStep)
	if !until.Equal(want) {
		t.Fatalf("RecordFailure returned until %v, want %v", until, want)
	}

	if got, blocked := store.IsBlocked("203.0.113.7", now); !blocked || !got.Equal(want) {
		t.Errorf("at the failure = until %v, blocked %v; want %v, blocked", got, blocked, want)
	}
	if _, blocked := store.IsBlocked("203.0.113.7", now.Add(29*time.Minute)); !blocked {
		t.Errorf("29 minutes in is not blocked")
	}
	if _, blocked := store.IsBlocked("203.0.113.7", want); blocked {
		t.Errorf("at exactly blocked_until is still blocked")
	}
	if _, blocked := store.IsBlocked("203.0.113.7", now.Add(31*time.Minute)); blocked {
		t.Errorf("past blocked_until is still blocked")
	}
}

func TestRecordFailurePrunesIdleRows(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	if _, err := store.RecordFailure("10.0.0.1", now); err != nil {
		t.Fatalf("RecordFailure(10.0.0.1): %v", err)
	}
	if _, err := store.RecordFailure("10.0.0.2", now.Add(29*24*time.Hour)); err != nil {
		t.Fatalf("RecordFailure(10.0.0.2): %v", err)
	}
	// 29 days of idle is inside the window, so 10.0.0.1 must still be there.
	if failures, ok := ipBlockRow(t, store, "10.0.0.1"); !ok || failures != 1 {
		t.Errorf("10.0.0.1 after 29 days idle = failures %d, ok %v; want 1, present", failures, ok)
	}

	// This call is 31 days past 10.0.0.1's update, so the prune drops it.
	if _, err := store.RecordFailure("10.0.0.3", now.Add(31*24*time.Hour)); err != nil {
		t.Fatalf("RecordFailure(10.0.0.3): %v", err)
	}
	if _, ok := ipBlockRow(t, store, "10.0.0.1"); ok {
		t.Errorf("10.0.0.1 survived a 31 day idle prune")
	}
	if failures, ok := ipBlockRow(t, store, "10.0.0.2"); !ok || failures != 1 {
		t.Errorf("10.0.0.2 was pruned or changed = failures %d, ok %v; want 1, present", failures, ok)
	}
	if failures, ok := ipBlockRow(t, store, "10.0.0.3"); !ok || failures != 1 {
		t.Errorf("10.0.0.3 = failures %d, ok %v; want 1, present", failures, ok)
	}
}

func TestIpBlocksPersistAcrossStores(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	until, err := store.RecordFailure("203.0.113.7", now)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })

	if got, blocked := reopened.IsBlocked("203.0.113.7", now); !blocked || !got.Equal(until) {
		t.Errorf("after reopen = until %v, blocked %v; want %v, blocked", got, blocked, until)
	}
}

// oldBinsSchema is the whole schema as the binary from before bin metadata wrote it: a bins
// table with no title or description column.
const oldBinsSchema = `
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

// writeOldSchemaDB writes a db exactly as the pre-metadata binary left one, holding one bin
// and one file in it, and returns the bin's hash.
func writeOldSchemaDB(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()

	if _, err := db.Exec(oldBinsSchema); err != nil {
		t.Fatalf("write the old schema: %v", err)
	}
	const hash = "abcd1234"
	if _, err := db.Exec(`INSERT INTO bins (hash, password_hash, created_at) VALUES (?, '', ?)`,
		hash, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)); err != nil {
		t.Fatalf("insert the old bin: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO files (bin_hash, path, content, size, updated_at) VALUES (?, ?, ?, ?, ?)`,
		hash, "notes.md", []byte("# old"), int64(5), time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)); err != nil {
		t.Fatalf("insert the old file: %v", err)
	}
	return hash
}

// binColumns lists the columns of bins in table order, which is how these tests say which
// columns an open added.
func binColumns(t *testing.T, store *Store) []string {
	t.Helper()
	rows, err := store.db.Query(`SELECT name FROM pragma_table_info('bins') ORDER BY cid`)
	if err != nil {
		t.Fatalf("read table_info: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan a column name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read table_info: %v", err)
	}
	return names
}

// TestOpenStoreMigratesAnOldDb is the live-volume case: a db written by the binary from
// before bin metadata opens, gains the two columns, and its old bin reads as empty metadata
// with its files intact. Opening it a second time adds nothing further.
func TestOpenStoreMigratesAnOldDb(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	hash := writeOldSchemaDB(t, dbPath)

	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open the old db: %v", err)
	}

	want := []string{"hash", "password_hash", "created_at", "title", "description"}
	got := binColumns(t, store)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("columns after the migration = %v, want %v", got, want)
	}

	bin, ok, err := store.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin(%q) after the migration = ok %v, err %v", hash, ok, err)
	}
	if bin.Title != "" || bin.Description != "" {
		t.Errorf("the old bin's metadata = %q / %q, want both empty", bin.Title, bin.Description)
	}
	content, ok, err := store.GetFile(hash, "notes.md")
	if err != nil || !ok {
		t.Fatalf("the old bin's file = ok %v, err %v; want it still there", ok, err)
	}
	if string(content) != "# old" {
		t.Errorf("the old file = %q, want its stored bytes back", content)
	}

	// The migration is what an open does once: a second open adds nothing and loses nothing.
	if _, err := store.UpdateBinMeta(hash, sql.NullString{String: "added later", Valid: true}, sql.NullString{}); err != nil {
		t.Fatalf("UpdateBinMeta on the migrated row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("reopen the migrated db: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })

	if got := binColumns(t, reopened); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("columns after reopening = %v, want %v", got, want)
	}
	bin, ok, err = reopened.GetBin(hash)
	if err != nil || !ok {
		t.Fatalf("GetBin after reopening = ok %v, err %v", ok, err)
	}
	if bin.Title != "added later" || bin.Description != "" {
		t.Errorf("metadata after reopening = %q / %q, want the written title and an empty description",
			bin.Title, bin.Description)
	}
}

// TestOpenStoreCompletesAHalfMigratedTable is the table that only one column reached: the
// next open adds the missing description and leaves the title column and its rows as they are.
func TestOpenStoreCompletesAHalfMigratedTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sandbin.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	if _, err := db.Exec(`
CREATE TABLE bins (
	hash TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT ''
)`); err != nil {
		t.Fatalf("write the half-migrated schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO bins (hash, password_hash, created_at, title) VALUES (?, '', ?, ?)`,
		"abcd1234", time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC).Format(time.RFC3339), "already here"); err != nil {
		t.Fatalf("insert the bin: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the half-migrated db: %v", err)
	}

	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open the half-migrated db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	want := []string{"hash", "password_hash", "created_at", "title", "description"}
	got := binColumns(t, store)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("columns after the migration = %v, want %v", got, want)
	}
	bin, ok, err := store.GetBin("abcd1234")
	if err != nil || !ok {
		t.Fatalf("GetBin after the migration = ok %v, err %v", ok, err)
	}
	if bin.Title != "already here" || bin.Description != "" {
		t.Errorf("metadata = %q / %q, want the stored title kept and the new column empty", bin.Title, bin.Description)
	}
}

// seedIPBlock writes one ip_blocks row exactly as the test wants it, which is how these
// tests get a row with a blocked_until already in the past.
func seedIPBlock(t *testing.T, store *Store, ip string, failures int, blockedUntil, updatedAt time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`
INSERT INTO ip_blocks (ip, failures, blocked_until, updated_at) VALUES (?, ?, ?, ?)`,
		ip, failures, blockedUntil.UTC().Format(time.RFC3339Nano), updatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed %s: %v", ip, err)
	}
}

func TestListBlocks(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks on an empty table: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("ListBlocks on an empty table returned %d rows, want 0", len(blocks))
	}

	// Inserted newest ip first: ListBlocks orders by ip.
	seedIPBlock(t, store, "203.0.113.7", 2, now.Add(blockStep), now)
	seedIPBlock(t, store, "10.0.0.1", 1, now.Add(-time.Minute), now.Add(-time.Hour))

	blocks, err = store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	want := []IPBlock{
		{IP: "10.0.0.1", Failures: 1, BlockedUntil: now.Add(-time.Minute).Format(time.RFC3339Nano), UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)},
		{IP: "203.0.113.7", Failures: 2, BlockedUntil: now.Add(blockStep).Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	if len(blocks) != len(want) {
		t.Fatalf("ListBlocks returned %d rows, want %d", len(blocks), len(want))
	}
	for i, b := range blocks {
		if b != want[i] {
			t.Errorf("blocks[%d] = %+v, want %+v", i, b, want[i])
		}
	}
}

func TestGetBlock(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	seedIPBlock(t, store, "203.0.113.7", 2, now.Add(blockStep), now)
	seedIPBlock(t, store, "10.0.0.1", 1, now.Add(-time.Minute), now.Add(-time.Hour))

	block, ok, err := store.GetBlock("203.0.113.7")
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if !ok {
		t.Fatalf("GetBlock(203.0.113.7) not found")
	}
	want := IPBlock{IP: "203.0.113.7", Failures: 2, BlockedUntil: now.Add(blockStep).Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if block != want {
		t.Errorf("GetBlock(203.0.113.7) = %+v, want %+v", block, want)
	}

	// A key that is not there is not found rather than an error, and it reads as a zero row.
	if missing, ok, err := store.GetBlock("198.51.100.4"); err != nil || ok || missing != (IPBlock{}) {
		t.Errorf("GetBlock(unknown) = %+v, ok %v, err %v; want a zero row, not found, no error", missing, ok, err)
	}

	// GetBlock only reads: both seeded rows are still in the table.
	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 2 {
		t.Errorf("after GetBlock the table holds %d rows, want 2", len(blocks))
	}
}

func TestUnblockIP(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	seedIPBlock(t, store, "203.0.113.7", 3, now.Add(blockStep), now)
	seedIPBlock(t, store, "198.51.100.4", 1, now.Add(blockStep), now)

	gone, err := store.UnblockIP("203.0.113.7")
	if err != nil {
		t.Fatalf("UnblockIP: %v", err)
	}
	if !gone {
		t.Errorf("UnblockIP(203.0.113.7) = %v, want true", gone)
	}
	if _, ok := ipBlockRow(t, store, "203.0.113.7"); ok {
		t.Errorf("203.0.113.7 survived UnblockIP")
	}
	if failures, ok := ipBlockRow(t, store, "198.51.100.4"); !ok || failures != 1 {
		t.Errorf("198.51.100.4 = failures %d, ok %v; want 1 failure, present", failures, ok)
	}

	gone, err = store.UnblockIP("203.0.113.7")
	if err != nil || gone {
		t.Errorf("UnblockIP of a missing row = %v, err %v; want false, no error", gone, err)
	}
}

func TestClearBlocks(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	for _, ip := range []string{"10.0.0.1", "203.0.113.7", "198.51.100.4"} {
		seedIPBlock(t, store, ip, 1, now.Add(blockStep), now)
	}

	cleared, err := store.ClearBlocks()
	if err != nil {
		t.Fatalf("ClearBlocks: %v", err)
	}
	if cleared != 3 {
		t.Errorf("ClearBlocks cleared %d rows, want 3", cleared)
	}
	blocks, err := store.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks after clear: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("after ClearBlocks the table holds %d rows, want 0", len(blocks))
	}

	cleared, err = store.ClearBlocks()
	if err != nil || cleared != 0 {
		t.Errorf("ClearBlocks on an empty table = %d, err %v; want 0, no error", cleared, err)
	}
}
