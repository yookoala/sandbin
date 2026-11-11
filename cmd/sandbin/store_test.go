package main

import (
	"path/filepath"
	"regexp"
	"testing"
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

	hash, err := store.CreateBin("")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(hash) {
		t.Errorf("hash %q is not 8 lowercase hex chars", hash)
	}

	other, err := store.CreateBin("")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if other == hash {
		t.Errorf("two bins got the same hash %q", hash)
	}
}

func TestGetBin(t *testing.T) {
	store := newTestStore(t)

	hash, err := store.CreateBin("hashed-password")
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

func TestPutGetFile(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("")
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

func TestListFiles(t *testing.T) {
	store := newTestStore(t)
	hash, err := store.CreateBin("")
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
