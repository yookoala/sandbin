package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// zipEntry is one archive/zip entry for the test builders. header tweaks the
// FileHeader before it is written, which is how symlink external attributes and
// other shapes a plain zip tool would never name get built.
type zipEntry struct {
	name   string
	body   string
	header func(*zip.FileHeader)
}

// makeZip builds a zip from entries; a name ending in "/" is a directory entry.
func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.header != nil {
			e.header(hdr)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip CreateHeader %q: %v", e.name, err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatalf("zip write %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// makeZipWithEncryption flips general purpose bit 0 (encrypted) on every entry's local
// and central header. Go's zip writer cannot write an encrypted entry, so the flag is
// what a foreign zip would carry; the bytes behind it stay plain.
func makeZipWithEncryption(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	out := makeZip(t, entries...)
	for _, sig := range [][]byte{[]byte("PK\x03\x04"), []byte("PK\x01\x02")} {
		flagOffset := 6
		if sig[3] == 0x02 {
			flagOffset = 8 // the central directory header, which is what readers trust
		}
		for start := 0; start < len(out); {
			i := bytes.Index(out[start:], sig)
			if i < 0 {
				break
			}
			i += start
			out[i+flagOffset] |= 0x1
			start = i + 1
		}
	}
	return out
}

// tarEntry is one archive/tar entry for the test builders.
type tarEntry struct {
	hdr  tar.Header
	body string
}

// makeTar builds a USTAR tar, gzipped when gz is true. ModTime is fixed so runs are
// deterministic, and an entry may name its own Format when USTAR cannot hold it.
func makeTar(t *testing.T, gz bool, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	var sink io.Writer = &buf
	var gw *gzip.Writer
	if gz {
		gw = gzip.NewWriter(&buf)
		sink = gw
	}
	tw := tar.NewWriter(sink)
	for i := range entries {
		hdr := entries[i].hdr
		if hdr.Typeflag != tar.TypeXGlobalHeader {
			hdr.ModTime = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
			if hdr.Format == 0 {
				hdr.Format = tar.FormatUSTAR
			}
		}
		if hdr.Size == 0 && entries[i].body != "" {
			hdr.Size = int64(len(entries[i].body))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("tar WriteHeader %q: %v", hdr.Name, err)
		}
		if entries[i].body != "" {
			if _, err := io.WriteString(tw, entries[i].body); err != nil {
				t.Fatalf("tar write %q: %v", hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if gw != nil {
		if err := gw.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
	}
	return buf.Bytes()
}

func tarFile(name, body string) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(body))}, body: body}
}

func tarDir(name string) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0755}}
}

// rawTarEntry is one hand-written ustar header block, its name field spelled exactly as
// the bytes say. Go's own tar writer refuses a name that is empty or carries a NUL, and it
// has no way to spell a size the POSIX base-256 way or a GNU sparse map at all, so the
// shapes a crafted archive or a foreign tar tool can carry have to be laid out byte by
// byte here.
type rawTarEntry struct {
	name     string
	typ      byte
	body     string
	size     []byte     // the 12 size bytes spelled directly, which is how base-256 is spelled
	gnu      bool       // GNU magic and version, which is what a sparse map lives in
	apparent int64      // GNU sparse: the size the holes fill out to, kept in the real size field
	sparse   [][2]int64 // GNU sparse: the fragments the archive really carries, as offset and length
}

// makeRawTar writes those blocks, gzipped when gz is true, and closes the archive with
// the two zero blocks every tar ends with. The header is USTAR unless an entry asks for
// the GNU format — the one a sparse map or a long name record lives in — in which case
// the magic and version are GNU's.
func makeRawTar(t *testing.T, gz bool, entries ...rawTarEntry) []byte {
	t.Helper()
	var raw bytes.Buffer
	for _, e := range entries {
		blk := make([]byte, 512)
		copy(blk[0:100], e.name)
		copy(blk[100:108], fmt.Sprintf("%07o", 0644))
		if e.size != nil {
			copy(blk[124:136], e.size)
		} else {
			copy(blk[124:135], fmt.Sprintf("%011o", len(e.body)))
		}
		copy(blk[136:147], fmt.Sprintf("%011o", 1763000000))
		copy(blk[148:156], "        ")
		blk[156] = e.typ
		if e.gnu || len(e.sparse) > 0 {
			copy(blk[257:263], "ustar ") // GNU magic and version: the format a sparse map or a long name record belongs to
			copy(blk[263:265], " \x00")
			for i, frag := range e.sparse {
				if i >= 4 {
					t.Fatalf("the old GNU sparse map holds 4 fragments, got %d", len(e.sparse))
				}
				copy(blk[386+i*24:386+i*24+12], fmt.Sprintf("%011o\x00", frag[0]))
				copy(blk[386+i*24+12:386+i*24+24], fmt.Sprintf("%011o\x00", frag[1]))
			}
			if len(e.sparse) > 0 {
				copy(blk[483:494], fmt.Sprintf("%011o", e.apparent))
			}
		} else {
			copy(blk[257:263], "ustar\x00")
			copy(blk[263:265], "00")
		}
		var sum int
		for _, b := range blk {
			sum += int(b)
		}
		copy(blk[148:156], fmt.Sprintf("%07o\x00", sum))
		raw.Write(blk)
		if len(e.body) > 0 {
			data := make([]byte, ((len(e.body)+511)/512)*512)
			copy(data, e.body)
			raw.Write(data)
		}
	}
	raw.Write(make([]byte, 1024))

	var buf bytes.Buffer
	if !gz {
		return raw.Bytes()
	}
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// sparseRawEntry is the one shape the GNU sparse tests need: a regular file with holes,
// the bytes it really carries in body, the fragments that hold them in frag, and the size
// the holes fill out to in apparent. That is exactly what GNU tar --sparse writes, and the
// typeflag it writes is tar.TypeGNUSparse.
func sparseRawEntry(name string, apparent int64, frag [][2]int64, body string) rawTarEntry {
	return rawTarEntry{name: name, typ: tar.TypeGNUSparse, body: body, apparent: apparent, sparse: frag}
}

// base256 spells a tar size field the POSIX way for a number too big for octal digits: the
// top bit of the first byte marks base-256, and the 12 bytes carry a big-endian two's
// complement number, so a negative one keeps its sign bytes set.
func base256(t *testing.T, value int64) []byte {
	t.Helper()
	field := make([]byte, 12)
	rest := value
	for i := 11; i >= 1; i-- {
		field[i] = byte(rest)
		rest >>= 8
	}
	if rest != 0 && rest != -1 {
		t.Fatalf("base256(%d) does not fit the field", value)
	}
	field[0] = byte(rest) | 0x80
	return field
}

// bombZipBody builds a highly compressible bomb: count entries of 1 MiB of zeros each,
// a body far under 1 MiB that unpacks past the 32 MiB total cap.
func bombZipBody(t *testing.T, count int) []byte {
	t.Helper()
	zeros := strings.Repeat("\x00", 1<<20)
	var entries []zipEntry
	for i := range count {
		entries = append(entries, zipEntry{name: fmt.Sprintf("z%03d.txt", i), body: zeros})
	}
	return makeZip(t, entries...)
}

// listingPaths reads the bin's stored paths straight from the store, which is how
// these tests say exactly what an upload left behind.
func listingPaths(t *testing.T, s *server, hash string) []string {
	t.Helper()
	files, err := s.store.ListFiles(hash)
	if err != nil {
		t.Fatalf("ListFiles(%q): %v", hash, err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

func TestDetectArchive(t *testing.T) {
	zipBody := makeZip(t, zipEntry{name: "a.txt", body: "a"})
	tarBody := makeTar(t, false, tarFile("a.txt", "a"))
	tgzBody := makeTar(t, true, tarFile("a.txt", "a"))

	for _, tc := range []struct {
		name string
		body []byte
		want archiveKind
		err  string
	}{
		{"bundle.zip", zipBody, archiveZip, ""},
		{"BUNDLE.ZIP", zipBody, archiveZip, ""},
		{"bundle.tar", tarBody, archiveTar, ""},
		{"bundle.tar.gz", tgzBody, archiveTarGz, ""},
		{"bundle.tgz", tgzBody, archiveTarGz, ""},
		// The magic decides: a .tgz name holding a zip is unzipped, a .zip name
		// holding gzipped tar is gunzipped, a .tar.gz name holding a plain tar is untarred.
		{"bundle.tgz", zipBody, archiveZip, ""},
		{"bundle.zip", tgzBody, archiveTarGz, ""},
		{"bundle.tar.gz", tarBody, archiveTar, ""},
		// An empty zip is still a zip.
		{"bundle.zip", makeZip(t), archiveZip, ""},
		// Ordinary names are never sniffed, not even when they hold a real archive.
		{"notes.txt", zipBody, archiveNone, ""},
		{"bundle.bin", tarBody, archiveNone, ""},
		// An archive-looking name holding no magic is the one refusal.
		{"notes.zip", []byte("just text\n"), archiveNone, "not a zip, tar or tar.gz archive"},
		{"notes.tgz", zipBody[:3], archiveNone, "not a zip, tar or tar.gz archive"},
	} {
		kind, err := detectArchive(tc.name, tc.body)
		if kind != tc.want {
			t.Errorf("detectArchive(%q) = %d, want %d", tc.name, kind, tc.want)
		}
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("detectArchive(%q) err = %v, want none", tc.name, err)
		case tc.err != "" && (err == nil || err.Error() != tc.err):
			t.Errorf("detectArchive(%q) err = %v, want %q", tc.name, err, tc.err)
		}
	}
}

// TestZipUploadExtractsIntoParentDir is the destination rule end to end: the entries
// land under site/, the archive name itself is absent from the listing, and extracted
// entries serve and list like normal files.
func TestZipUploadExtractsIntoParentDir(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	body := makeZip(t,
		zipEntry{name: "index.html", body: "<h1>Bundle</h1>"},
		zipEntry{name: "css/", body: ""},
		zipEntry{name: "css/app.css", body: "body { color: red }\n"},
		zipEntry{name: "report.ansi", body: "\x1b[1mReport\x1b[0m done\n"},
	)
	rec := put(t, s, base+"site/bundle.zip", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT bundle.zip = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if got := rec.Header().Get("X-Extracted-Count"); got != "3" {
		t.Errorf("X-Extracted-Count = %q, want 3", got)
	}
	wantBody := "/" + hash + "/site/index.html\n/" + hash + "/site/css/app.css\n/" + hash + "/site/report.ansi"
	if rec.Body.String() != wantBody {
		t.Errorf("PUT body =\n%q\nwant\n%q", rec.Body.String(), wantBody)
	}

	got := listingPaths(t, s, hash)
	if strings.Join(got, ",") != "site/css/app.css,site/index.html,site/report.ansi" {
		t.Errorf("listing = %v, want the extracted paths with bundle.zip absent", got)
	}

	rec = get(t, s, base+"site/css/app.css")
	if rec.Code != http.StatusOK || rec.Body.String() != "body { color: red }\n" {
		t.Errorf("GET site/css/app.css = %d, body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", ct)
	}

	rec = get(t, s, base+"site/report.ansi")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET site/report.ansi = %d, want %d", rec.Code, http.StatusOK)
	}
	wantANSI := `<pre class="ansi"><span style="font-weight:bold">Report</span> done` + "\n</pre>"
	if rec.Body.String() != wantANSI {
		t.Errorf("rendered ANSI = %q, want %q", rec.Body.String(), wantANSI)
	}
}

// TestExtractedIndexHTMLBecomesCoverPage is the after-extraction rule: an index.html
// entry at the bin root takes over the bin base exactly like a plain PUT of it.
func TestExtractedIndexHTMLBecomesCoverPage(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	body := makeTar(t, true, tarFile("index.html", "<h1>From the archive</h1>"))
	if rec := put(t, s, base+"site.tar.gz", "test-key", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("PUT site.tar.gz = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec := get(t, s, base)
	if rec.Code != http.StatusOK || rec.Body.String() != "<h1>From the archive</h1>" {
		t.Errorf("GET bin base = %d, body %q, want the extracted index.html served raw", rec.Code, rec.Body.String())
	}
}

// TestTarAndTarGzUploadsExtract covers the two tar shapes, including the leading "./"
// entry names a `tar czf - .` gives its files and a pax global header record, which
// describes the archive and is skipped rather than stored or refused.
func TestTarAndTarGzUploadsExtract(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	body := makeTar(t, false,
		tarDir("site/"),
		tarFile("./site/index.html", "<p>dot slash</p>"),
		tarFile("site/css/app.css", "p { margin: 0 }\n"),
	)
	if rec := put(t, s, base+"plain.tar", "test-key", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("PUT plain.tar = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	got := listingPaths(t, s, hash)
	if strings.Join(got, ",") != "site/css/app.css,site/index.html" {
		t.Errorf("tar listing = %v, want the validated entries at the parent dir", got)
	}

	pax := makeTar(t, true,
		tarEntry{hdr: tar.Header{Typeflag: tar.TypeXGlobalHeader, Format: tar.FormatPAX,
			PAXRecords: map[string]string{"comment": "a global record"}}},
		tarFile("notes.md", "# Tgz\n"),
	)
	if rec := put(t, s, base+"gz.tgz", "test-key", string(pax)); rec.Code != http.StatusCreated {
		t.Fatalf("PUT gz.tgz = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if rec := get(t, s, base+"notes.md"); rec.Body.String() != "# Tgz\n" {
		t.Errorf("GET notes.md body = %q", rec.Body.String())
	}
	got = listingPaths(t, s, hash)
	if !strings.Contains(strings.Join(got, ","), "notes.md") || strings.Contains(strings.Join(got, ","), "PaxHeaders") {
		t.Errorf("listing = %v, want notes.md with the pax record left out", got)
	}
}

// TestXExtractStoresRaw is the opt-out in all three curl shapes: present at all, any
// value including empty, the archive body is stored and served byte for byte.
func TestXExtractStoresRaw(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")
	body := makeZip(t, zipEntry{name: "a.txt", body: "a"})

	for _, shape := range []struct {
		name  string
		file  string
		setup func(req *http.Request)
	}{
		{"bare", "notes-bare.zip", func(req *http.Request) { req.Header["X-Extract"] = []string{} }},
		{"empty value", "notes-empty.zip", func(req *http.Request) { req.Header.Set("X-Extract", "") }},
		{"keep", "notes-keep.zip", func(req *http.Request) { req.Header.Set("X-Extract", "keep") }},
	} {
		req := httptest.NewRequest(http.MethodPut, base+shape.file, strings.NewReader(string(body)))
		req.Header.Set("X-API-Key", "test-key")
		shape.setup(req)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("PUT X-Extract %s = %d, want %d; body %q", shape.name, rec.Code, http.StatusCreated, rec.Body.String())
		}
		if rec.Body.String() != "/"+hash+"/"+shape.file {
			t.Errorf("X-Extract %s body = %q, want the plain PUT line", shape.name, rec.Body.String())
		}
		if got := rec.Header().Get("X-Extracted-Count"); got != "" {
			t.Errorf("X-Extract %s carried X-Extracted-Count %q, want none", shape.name, got)
		}
		rec = get(t, s, base+shape.file)
		if rec.Body.String() != string(body) {
			t.Errorf("GET after X-Extract %s did not return the archive bytes unchanged", shape.name)
		}
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "notes-bare.zip,notes-empty.zip,notes-keep.zip" {
		t.Errorf("listing = %v, want the three raw archives", got)
	}
}

// TestZipSlipRefusedTheWholeArchive is the headline: every hostile entry name refuses
// the whole archive with cleanPath's own words and stores nothing at all.
func TestZipSlipRefusedTheWholeArchive(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	for _, bad := range []struct {
		entry string
		want  string
	}{
		{"../escape", "path must not escape the bin"},
		{"a/../../x", "path must not contain ., .., // or a trailing slash"}, // the name is judged before path.Join cleans it
		{"/etc/passwd", "path must be relative"},
		{strings.Repeat("a/", 16) + "x", "path has too many segments"},
		{"bad\nname.txt", "path may not contain control characters"},
		{"bad\x00name.txt", "path may not contain control characters"},
		{"..\x7f..\\x", "path may not contain control characters"},
		{"..\\..\\x", "path must not escape the bin"}, // backslashes become slashes first
	} {
		body := makeZip(t,
			zipEntry{name: "fine.txt", body: "fine"},
			zipEntry{name: bad.entry, body: "hostile"},
		)
		rec := put(t, s, base+"bundle.zip", "test-key", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT zip with entry %q = %d, want %d; body %q", bad.entry, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != bad.want {
			t.Errorf("zip entry %q body = %q, want %q", bad.entry, got, bad.want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("zip entry %q stored %v, want the whole archive refused", bad.entry, got)
		}
	}

	// A PC zip's ordinary backslash separator is converted and lands as a normal path.
	body := makeZip(t, zipEntry{name: `sub\page.html`, body: "<p>backslash</p>"})
	if rec := put(t, s, base+"pc.zip", "test-key", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("PUT pc.zip = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "sub/page.html" {
		t.Errorf("listing = %v, want sub/page.html", got)
	}
}

// TestTarHostileEntriesRefused keeps the same refusal for tar entry names, where
// backslashes stay backslashes and land on cleanPath's own rule.
func TestTarHostileEntriesRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	for _, bad := range []struct {
		entry string
		want  string
	}{
		{"../escape", "path must not escape the bin"},
		{"/etc/passwd", "path must be relative"},
		{`a\b`, "path may not contain backslashes"},
	} {
		body := makeTar(t, true, tarFile("fine.txt", "fine"), tarFile(bad.entry, "hostile"))
		rec := put(t, s, base+"bundle.tar.gz", "test-key", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT tar.gz with entry %q = %d, want %d; body %q", bad.entry, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != bad.want {
			t.Errorf("tar entry %q body = %q, want %q", bad.entry, got, bad.want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("tar entry %q stored %v, want the whole archive refused", bad.entry, got)
		}
	}
}

// TestNonRegularEntriesRefused names the entry: a bin holds blobs, not a filesystem.
func TestNonRegularEntriesRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	symlink := makeTar(t, true, tarFile("ok.txt", "ok"),
		tarEntry{hdr: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "ok.txt", Mode: 0777}})
	hardlink := makeTar(t, true, tarEntry{hdr: tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "ok.txt", Mode: 0644}})
	charDev := makeTar(t, true, tarEntry{hdr: tar.Header{Name: "tty", Typeflag: tar.TypeChar, Mode: 0660}})
	blockDev := makeTar(t, true, tarEntry{hdr: tar.Header{Name: "disk", Typeflag: tar.TypeBlock, Mode: 0660}})
	fifo := makeTar(t, true, tarEntry{hdr: tar.Header{Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0644}})

	for _, tc := range []struct {
		body []byte
		want string
	}{
		{symlink, "archive contains a symlink link"},
		{hardlink, "archive contains a hardlink hard"},
		{charDev, "archive contains a device tty"},
		{blockDev, "archive contains a device disk"},
		{fifo, "archive contains a fifo pipe"},
	} {
		rec := put(t, s, base+"bundle.tar.gz", "test-key", string(tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tc.want {
			t.Errorf("body = %q, want %q", got, tc.want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("refused tar stored %v, want nothing", got)
		}
	}

	// A zip names a symlink through its external attributes.
	zipSymlink := makeZip(t, zipEntry{name: "ok.txt", body: "ok"},
		zipEntry{name: "link", body: "ok.txt", header: func(h *zip.FileHeader) {
			h.CreatorVersion = 3<<8 | 20 // creatorUnix<<8 | zipVersion20
			h.ExternalAttrs = 0o120777 << 16
		}})
	rec := put(t, s, base+"bundle.zip", "test-key", string(zipSymlink))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT zip symlink = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive contains a symlink link" {
		t.Errorf("body = %q, want %q", got, "archive contains a symlink link")
	}

	// An entry zip tools claim to have encrypted is refused before its bytes are read.
	encrypted := makeZipWithEncryption(t, zipEntry{name: "secret.txt", body: "plain behind a claimed flag"})
	rec = put(t, s, base+"bundle.zip", "test-key", string(encrypted))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT encrypted zip = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive contains an encrypted entry secret.txt" {
		t.Errorf("body = %q, want %q", got, "archive contains an encrypted entry secret.txt")
	}
}

func TestDuplicateAndEmptyArchivesRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	dup := makeZip(t, zipEntry{name: "dup.txt", body: "first"}, zipEntry{name: "dup.txt", body: "second"})
	rec := put(t, s, base+"dup.zip", "test-key", string(dup))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT duplicate zip = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive contains duplicate path dup.txt" {
		t.Errorf("body = %q, want %q", got, "archive contains duplicate path dup.txt")
	}

	for _, empty := range [][]byte{
		makeZip(t), // an empty zip
		makeTar(t, true, tarDir("only/"), tarDir("only/dirs/")), // directories only
		makeTar(t, false), // an empty tar
	} {
		rec := put(t, s, base+"empty.zip", "test-key", string(empty))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT empty archive = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive contains no files" {
			t.Errorf("body = %q, want %q", got, "archive contains no files")
		}
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("refused archives left %v behind, want nothing", got)
	}
}

// TestArchiveCapRefusals proves each cap aborts while streaming: every body here is
// far under 1 MiB while the unpacked shape is over it, and nothing is stored.
func TestArchiveCapRefusals(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	overOne := makeZip(t, zipEntry{name: "big.txt", body: strings.Repeat("x", maxBodyBytes+1)})
	rec := put(t, s, base+"over.zip", "test-key", string(overOne))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT entry over 1 MiB = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "extracted file over 1 MiB" {
		t.Errorf("body = %q, want %q", got, "extracted file over 1 MiB")
	}

	var many []zipEntry
	for i := range maxArchiveEntries + 1 {
		many = append(many, zipEntry{name: fmt.Sprintf("e/%04d.txt", i), body: "x"})
	}
	manyBody := makeZip(t, many...)
	if len(manyBody) >= maxBodyBytes {
		t.Fatalf("the 1001-entry zip body is %d bytes, want it well under 1 MiB", len(manyBody))
	}
	rec = put(t, s, base+"many.zip", "test-key", string(manyBody))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT 1001-entry zip = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive has over 1000 files" {
		t.Errorf("body = %q, want %q", got, "archive has over 1000 files")
	}

	bombBody := bombZipBody(t, maxExtractTotal/(1<<20)+2)
	if len(bombBody) >= maxBodyBytes {
		t.Fatalf("the bomb body is %d bytes, want it under 1 MiB", len(bombBody))
	}
	rec = put(t, s, base+"bomb.zip", "test-key", string(bombBody))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT bomb = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive unpacks over 32 MiB" {
		t.Errorf("body = %q, want %q", got, "archive unpacks over 32 MiB")
	}

	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("cap refusals stored %v, want nothing", got)
	}
}

// TestBombTarStopsAtTheCap is the same discipline for a tar bomb read mid-stream.
func TestBombTarStopsAtTheCap(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	zeros := strings.Repeat("\x00", 1<<20)
	var entries []tarEntry
	for i := range maxExtractTotal/(1<<20) + 2 {
		entries = append(entries, tarFile(fmt.Sprintf("z%03d.txt", i), zeros))
	}
	body := makeTar(t, true, entries...)
	if len(body) >= maxBodyBytes {
		t.Fatalf("the tar bomb body is %d bytes, want it under 1 MiB", len(body))
	}
	rec := put(t, s, base+"bomb.tgz", "test-key", string(body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT tar bomb = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "archive unpacks over 32 MiB" {
		t.Errorf("body = %q, want %q", got, "archive unpacks over 32 MiB")
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("the tar bomb stored %v, want nothing", got)
	}
}

func TestCorruptArchivesRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	goodZip := makeZip(t, zipEntry{name: "a.txt", body: strings.Repeat("abcdefgh", 100)})
	goodTar := makeTar(t, false, tarFile("a.txt", strings.Repeat("abcdefgh", 200)))
	goodTgz := makeTar(t, true, tarFile("a.txt", strings.Repeat("abcdefgh", 400)))

	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"junk.zip", []byte("PK\x03\x04then nothing at all"), "corrupt zip archive"},
		{"cut.zip", goodZip[:len(goodZip)/2], "corrupt zip archive"},
		{"cut.tar", goodTar[:len(goodTar)/2], "corrupt tar archive"},
		{"junk.tar.gz", []byte("\x1f\x8bnot gzip at all"), "corrupt tar.gz archive"},
		{"cut.tar.gz", goodTgz[:len(goodTgz)/2], "corrupt tar.gz archive"},
	} {
		rec := put(t, s, base+tc.name, "test-key", string(tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want %d; body %q", tc.name, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tc.want {
			t.Errorf("PUT %s body = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("corrupt archives left %v behind, want nothing", got)
	}
}

// TestArchiveBodyUnderAPlainNameStoredRaw is the name rule: only archive names are
// looked at, so an archive under an ordinary name is just a file.
func TestArchiveBodyUnderAPlainNameStoredRaw(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	body := makeZip(t, zipEntry{name: "a.txt", body: "a"})
	rec := put(t, s, base+"bundle.bin", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT bundle.bin = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("X-Extracted-Count"); got != "" {
		t.Errorf("X-Extracted-Count = %q, want none for a non-archive name", got)
	}
	if rec.Body.String() != "/"+hash+"/bundle.bin" {
		t.Errorf("body = %q, want the plain PUT line", rec.Body.String())
	}
	got := listingPaths(t, s, hash)
	if strings.Join(got, ",") != "bundle.bin" {
		t.Errorf("listing = %v, want only bundle.bin", got)
	}
}

// TestArchiveBodyOverTheBodyCap keeps the unchanged 1 MiB MaxBytesReader answer for an
// archive-named upload too: the body cap comes before any extraction.
func TestArchiveBodyOverTheBodyCap(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	// Stored, not deflated: the 1 MiB entry body stays 1 MiB of request body.
	oversize := makeZip(t, zipEntry{name: "a.txt", body: strings.Repeat("x", maxBodyBytes),
		header: func(h *zip.FileHeader) { h.Method = zip.Store }})
	rec := put(t, s, base+"big.zip", "test-key", string(oversize))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT oversized .zip = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "body over 1 MiB") {
		t.Errorf("body = %q, want the plain body over 1 MiB refusal", rec.Body.String())
	}
}

// TestArchiveChargingUnchanged is the charging rule with archive bodies: only API key
// failures charge, a bad archive after a good key charges nothing.
func TestArchiveChargingUnchanged(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")

	slip := makeZip(t, zipEntry{name: "../escape", body: "hostile"})

	rec := putFrom(t, s, "198.51.100.41:1234", base+"slip.zip", "wrong", string(slip))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("archive with a wrong key = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if failures, dur := storedBlock(t, s, "198.51.100.41"); failures != 1 || dur != blockStep {
		t.Errorf("archive with a wrong key = failures %d, block %v; want 1 failure, %v", failures, dur, blockStep)
	}

	rec = putFrom(t, s, "198.51.100.42:1234", base+"slip.zip", "test-key", string(slip))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad archive with the good key = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.42"); ok {
		t.Errorf("a refused archive charged 198.51.100.42, want no row in ip_blocks")
	}

	body := makeZip(t, zipEntry{name: "a.txt", body: "a"})
	rec = putFrom(t, s, "198.51.100.43:1234", base+"bundle.zip", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("good archive = %d, want %d", rec.Code, http.StatusCreated)
	}
	if _, ok := ipBlockRow(t, s.store, "198.51.100.43"); ok {
		t.Errorf("a successful upload charged 198.51.100.43, want no row in ip_blocks")
	}
}

// TestExtractionUpsertsAtomically is all-or-nothing per upload and upsert-not-sync
// across uploads: a good archive replaces what it names and leaves every other file
// alone, a failing one afterwards changes nothing.
func TestExtractionUpsertsAtomically(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	for _, seed := range []struct{ path, body string }{
		{"a.txt", "the old a"},
		{"keep.txt", "leave me alone"},
	} {
		if rec := put(t, s, base+seed.path, "test-key", seed.body); rec.Code != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want %d", seed.path, rec.Code, http.StatusCreated)
		}
	}

	body := makeZip(t,
		zipEntry{name: "a.txt", body: "the new a"},
		zipEntry{name: "sub/new.txt", body: "new"},
	)
	rec := put(t, s, base+"update.zip", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT update.zip = %d, want 201 (a.txt replaced, sub/new.txt created); body %q", rec.Code, rec.Body.String())
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "a.txt,keep.txt,sub/new.txt" {
		t.Errorf("listing = %v, want a.txt,keep.txt,sub/new.txt", got)
	}
	if rec := get(t, s, base+"a.txt"); rec.Body.String() != "the new a" {
		t.Errorf("a.txt = %q, want the extracted replacement", rec.Body.String())
	}
	if rec := get(t, s, base+"keep.txt"); rec.Body.String() != "leave me alone" {
		t.Errorf("keep.txt = %q, want it untouched", rec.Body.String())
	}

	// The same archive again: every path is a replacement, so the answer is 200.
	rec = put(t, s, base+"update.zip", "test-key", string(body))
	if rec.Code != http.StatusOK {
		t.Errorf("second PUT update.zip = %d, want 200 (all replaced); body %q", rec.Code, rec.Body.String())
	}

	// A bomb mid-upload leaves the bin exactly as the good upload left it.
	rec = put(t, s, base+"bomb.zip", "test-key", string(bombZipBody(t, maxExtractTotal/(1<<20)+2)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT bomb = %d, want 413", rec.Code)
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "a.txt,keep.txt,sub/new.txt" {
		t.Errorf("listing after the bomb = %v, want the listing unchanged", got)
	}
	if rec := get(t, s, base+"a.txt"); rec.Body.String() != "the new a" {
		t.Errorf("a.txt after the bomb = %q, want it unchanged", rec.Body.String())
	}
}

// TestExtractionAnswersWithCountAndLines is the response contract itself.
func TestExtractionAnswersWithCountAndLines(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	body := makeTar(t, true, tarFile("one.txt", "1"), tarFile("dir/two.txt", "2"))
	rec := put(t, s, base+"two.tar.gz", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if got := rec.Header().Get("X-Extracted-Count"); got != "2" {
		t.Errorf("X-Extracted-Count = %q, want 2", got)
	}
	want := "/" + hash + "/one.txt\n/" + hash + "/dir/two.txt" // archive order
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

// TestEntryNamesJudgedBeforeTheyJoin is the relocation refusal: an entry is judged on the
// name the archive actually wrote, before path.Join can clean it. Joined first,
// "sub/../up.txt" would have landed as site/up.txt, "deep/../../evil.txt" as evil.txt at
// the bin root, and "/etc/passwd2" as site/etc/passwd2 — the same entry a root-level
// archive already refused.
func TestEntryNamesJudgedBeforeTheyJoin(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	for _, tc := range []struct {
		kind  string
		entry string
		want  string
	}{
		{"zip", "/etc/passwd2", "path must be relative"},
		{"zip", "sub/../up.txt", "path must not contain ., .., // or a trailing slash"},
		{"zip", "deep/../../evil.txt", "path must not contain ., .., // or a trailing slash"},
		{"zip", "..", "path must not escape the bin"},
		{"zip", "site/./index.html", "path must not contain ., .., // or a trailing slash"},
		{"zip", "././x.txt", "path must not contain ., .., // or a trailing slash"},
		{"tar.gz", "/etc/passwd2", "path must be relative"},
		{"tar.gz", "sub/../up.txt", "path must not contain ., .., // or a trailing slash"},
		{"tar.gz", "deep/../../evil.txt", "path must not contain ., .., // or a trailing slash"},
		{"tar.gz", "..", "path must not escape the bin"},
		{"tar", `a\..\b.txt`, "path may not contain backslashes"}, // tar keeps its backslashes verbatim
	} {
		var body []byte
		switch tc.kind {
		case "zip":
			body = makeZip(t, zipEntry{name: "fine.txt", body: "fine"}, zipEntry{name: tc.entry, body: "hostile"})
		case "tar":
			body = makeTar(t, false, tarFile("fine.txt", "fine"), tarFile(tc.entry, "hostile"))
		default:
			body = makeTar(t, true, tarFile("fine.txt", "fine"), tarFile(tc.entry, "hostile"))
		}
		name := "site/abs." + tc.kind
		rec := put(t, s, base+name, "test-key", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s with entry %q = %d, want %d; body %q", name, tc.entry, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tc.want {
			t.Errorf("%s entry %q body = %q, want %q", tc.kind, tc.entry, got, tc.want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("%s entry %q stored %v, want the whole archive refused", tc.kind, tc.entry, got)
		}
	}
}

// TestRefusedRelocatingEntryLeavesNothingAtTheBinRoot is the same refusal seen through the
// handler: deep/../../evil.txt under site/abs.zip never lands, the bin's own files are
// untouched, and no evil.txt appears at the root the entry was reaching for.
func TestRefusedRelocatingEntryLeavesNothingAtTheBinRoot(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	if rec := put(t, s, base+"keep.txt", "test-key", "leave me alone"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT keep.txt = %d, want %d", rec.Code, http.StatusCreated)
	}

	body := makeZip(t,
		zipEntry{name: "fine.txt", body: "fine"},
		zipEntry{name: "deep/../../evil.txt", body: "hostile"},
	)
	rec := put(t, s, base+"site/abs.zip", "test-key", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT site/abs.zip = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "keep.txt" {
		t.Errorf("listing after the refusal = %v, want only keep.txt", got)
	}
	for _, p := range []string{"evil.txt", "site/evil.txt", "site/fine.txt", "site/abs.zip"} {
		if rec := get(t, s, base+p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
	if rec := get(t, s, base+"keep.txt"); rec.Code != http.StatusOK || rec.Body.String() != "leave me alone" {
		t.Errorf("GET keep.txt = %d, body %q, want it untouched", rec.Code, rec.Body.String())
	}
}

// TestTarDotSlashEntriesLandInTheArchivesFolder is the `tar czf - .` idiom: one leading
// "./" is stripped, a root directory entry names the folder itself, and both land inside
// the folder the archive was uploaded under.
func TestTarDotSlashEntriesLandInTheArchivesFolder(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	body := makeTar(t, true,
		tarDir("./"),
		tarFile("./hello.txt", "hello\n"),
		tarDir("./sub/"),
		tarFile("./sub/app.css", "body {}\n"),
	)
	rec := put(t, s, base+"site/x.tar.gz", "test-key", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT site/x.tar.gz = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("X-Extracted-Count"); got != "2" {
		t.Errorf("X-Extracted-Count = %q, want 2", got)
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "site/hello.txt,site/sub/app.css" {
		t.Errorf("listing = %v, want site/hello.txt and site/sub/app.css", got)
	}
	if rec := get(t, s, base+"site/hello.txt"); rec.Body.String() != "hello\n" {
		t.Errorf("GET site/hello.txt = %q", rec.Body.String())
	}

	// The same shapes written byte for byte, as `tar czf - .` from inside the folder does,
	// including the bare "." block for the folder itself.
	root := makeRawTar(t, false,
		rawTarEntry{name: "./", typ: tar.TypeDir},
		rawTarEntry{name: "./root.txt", typ: tar.TypeReg, body: "root\n"},
	)
	if rec := put(t, s, base+"x.tar", "test-key", string(root)); rec.Code != http.StatusCreated {
		t.Fatalf("PUT x.tar = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "root.txt,site/hello.txt,site/sub/app.css" {
		t.Errorf("listing = %v, want root.txt beside the site/ files", got)
	}
}

// TestUnnamedEntryRefused names an archive that claims a file and gave it no name.
func TestUnnamedEntryRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	bodies := []struct {
		file string
		body []byte
		want string
	}{
		{"unnamed.zip", makeZip(t, zipEntry{name: "fine.txt", body: "fine"}, zipEntry{name: "", body: "hostile"}),
			"archive contains an unnamed entry"},
		{"unnamed.tar", makeRawTar(t, false,
			rawTarEntry{name: "fine.txt", typ: tar.TypeReg, body: "fine"},
			rawTarEntry{name: "", typ: tar.TypeReg, body: "hostile"}),
			"archive contains an unnamed entry"},
		{"unnamed.tar.gz", makeRawTar(t, true,
			rawTarEntry{name: "fine.txt", typ: tar.TypeReg, body: "fine"},
			rawTarEntry{name: "./", typ: tar.TypeReg, body: "hostile"}),
			"archive contains an unnamed entry ./"},
	}
	for _, tc := range bodies {
		rec := put(t, s, base+tc.file, "test-key", string(tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want %d; body %q", tc.file, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tc.want {
			t.Errorf("PUT %s body = %q, want %q", tc.file, got, tc.want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("PUT %s stored %v, want nothing", tc.file, got)
		}
	}
}

// TestNULInEntryNameRefused closes the truncation quirk: a name field that keeps going
// past its NUL terminator is refused instead of storing the entry under the shorter name
// archive/tar stops at.
func TestNULInEntryNameRefused(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	// A zip carries the name with the NUL in it, so cleanPath's control-character rule is
	// what answers.
	zipBody := makeZip(t, zipEntry{name: "fine.txt", body: "fine"}, zipEntry{name: "evil\x00.txt", body: "hostile"})
	rec := put(t, s, base+"nul.zip", "test-key", string(zipBody))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT nul.zip = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "path may not contain control characters" {
		t.Errorf("zip body = %q, want %q", got, "path may not contain control characters")
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("the NUL zip stored %v, want nothing", got)
	}

	// A tar cannot carry the NUL to the reader at all: archive/tar ends the name at it, so
	// the raw header block is what has to be refused.
	for _, tc := range []struct {
		file string
		body []byte
	}{
		{"nul.tar", makeRawTar(t, false,
			rawTarEntry{name: "fine.txt", typ: tar.TypeReg, body: "fine"},
			rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})},
		{"nul.tar.gz", makeRawTar(t, true,
			rawTarEntry{name: "./", typ: tar.TypeDir},
			rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})},
	} {
		rec := put(t, s, base+tc.file, "test-key", string(tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want %d; body %q", tc.file, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		want := `archive contains an entry name with a NUL byte "evil\x00.txt"`
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != want {
			t.Errorf("PUT %s body = %q, want %q", tc.file, got, want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("PUT %s stored %v, want nothing — the entry must not land as the truncated name", tc.file, got)
		}
	}
}

// TestTarBase256SizeFieldKeepsWalking closes the bypass the review found: a first header
// whose size is spelled the POSIX base-256 way used to stop the raw name walk before it
// reached the entry after it, and the entry waiting there was one whose name field ran on
// past its NUL terminator. The walk now reads that size the way archive/tar reads it, so
// the entry after it is named.
func TestTarBase256SizeFieldKeepsWalking(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	// Over the per-entry cap, and physically consistent: the 2 MiB of zeros are really in
	// the archive — gzip is what keeps the request body far under the 1 MiB body cap — so
	// this is an archive whose own declared size the caps refuse, named the way the
	// streaming refusal names the same breach. Nothing reaches the NUL-overrun entry, and
	// the reader would have refused this archive too.
	overBody := makeRawTar(t, true,
		rawTarEntry{name: "big.txt", typ: tar.TypeReg, body: strings.Repeat("\x00", 2<<20), size: base256(t, 2<<20)},
		rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})
	if len(overBody) >= maxBodyBytes {
		t.Fatalf("the gzipped 2 MiB archive is %d bytes, want it well under 1 MiB", len(overBody))
	}
	rec := put(t, s, base+"over.tar.gz", "test-key", string(overBody))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT over.tar.gz = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "extracted file over 1 MiB" {
		t.Errorf("over.tar.gz body = %q, want %q", got, "extracted file over 1 MiB")
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("the over-cap base-256 archive stored %v, want nothing", got)
	}

	// Within the cap: the walk reads the base-256 size, steps over the entry it counts,
	// and gets to the NUL-overrun name after it. This is the shape that used to land as a
	// 201 with the truncated name.
	for _, tc := range []struct {
		file string
		body []byte
	}{
		{"chain.tar", makeRawTar(t, false,
			rawTarEntry{name: "one.txt", typ: tar.TypeReg, body: strings.Repeat("a", 1024), size: base256(t, 1024)},
			rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})},
		{"chain.tar.gz", makeRawTar(t, true,
			rawTarEntry{name: "one.txt", typ: tar.TypeReg, body: strings.Repeat("a", 1024), size: base256(t, 1024)},
			rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})},
	} {
		rec := put(t, s, base+tc.file, "test-key", string(tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want %d; body %q", tc.file, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		want := `archive contains an entry name with a NUL byte "evil\x00.txt"`
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != want {
			t.Errorf("PUT %s body = %q, want %q", tc.file, got, want)
		}
		if got := listingPaths(t, s, hash); len(got) != 0 {
			t.Errorf("PUT %s stored %v, want nothing", tc.file, got)
		}
	}

	// A base-256 size that decodes negative says the header lies, so it is as untrustworthy
	// as junk and the walk hands the archive to archive/tar. Recorded here is what that
	// reader actually says about the archive, and nothing lands either way.
	negative := makeRawTar(t, false,
		rawTarEntry{name: "one.txt", typ: tar.TypeReg, body: "one", size: base256(t, -1)},
		rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})
	rec = put(t, s, base+"negative.tar", "test-key", string(negative))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT negative.tar = %d, want %d; body %q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "corrupt tar archive" {
		t.Errorf("negative.tar body = %q, want archive/tar's own verdict %q", got, "corrupt tar archive")
	}
	if got := listingPaths(t, s, hash); len(got) != 0 {
		t.Errorf("negative.tar stored %v, want nothing", got)
	}
}

// TestTarGNUSparseEntriesExtractAsFiles is the portability half: a sparse entry is a
// regular file with holes, archive/tar already materializes those holes, and GNU tar
// --sparse writes exactly that. The caps count the bytes such an entry materializes to.
func TestTarGNUSparseEntriesExtractAsFiles(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	// Two fragments of real data, holes between and after them, filling out to 200000
	// bytes. The fixture is checked to carry a typeflag 'S' header at all, because that is
	// the flag the server used to call corrupt.
	wantHoles := strings.Repeat("A", 1024) +
		strings.Repeat("\x00", 100000-1024) +
		strings.Repeat("B", 1024) +
		strings.Repeat("\x00", 200000-100000-1024)
	holes := makeRawTar(t, false,
		rawTarEntry{name: "fine.txt", typ: tar.TypeReg, body: "fine"},
		sparseRawEntry("holes.bin", 200000, [][2]int64{{0, 1024}, {100000, 1024}},
			strings.Repeat("A", 1024)+strings.Repeat("B", 1024)),
		rawTarEntry{name: "after.txt", typ: tar.TypeReg, body: "after"},
	)
	if !hasTypeflag(holes, tar.TypeGNUSparse) {
		t.Fatal("the sparse fixture carries no tar.TypeGNUSparse header block")
	}
	rec := put(t, s, base+"holes.tar", "test-key", string(holes))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT holes.tar = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("X-Extracted-Count"); got != "3" {
		t.Errorf("X-Extracted-Count = %q, want 3", got)
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "after.txt,fine.txt,holes.bin" {
		t.Errorf("listing = %v, want the sparse entry beside the ordinary ones", got)
	}
	if rec := get(t, s, base+"holes.bin"); rec.Code != http.StatusOK || rec.Body.String() != wantHoles {
		t.Errorf("GET holes.bin = %d with %d bytes, want %d bytes read byte-identical",
			rec.Code, len(rec.Body.String()), len(wantHoles))
	}
	if rec := get(t, s, base+"after.txt"); rec.Body.String() != "after" {
		t.Errorf("GET after.txt = %q, want the entry after a sparse map to extract too", rec.Body.String())
	}

	// The same discipline on a sparse file whose holes fill out past what a bin file may be.
	bigHoles := makeRawTar(t, false,
		sparseRawEntry("big.bin", 2<<20, [][2]int64{{0, 1024}}, strings.Repeat("A", 1024)))
	rec = put(t, s, base+"big.tar", "test-key", string(bigHoles))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT big.tar = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != "extracted file over 1 MiB" {
		t.Errorf("big.tar body = %q, want %q", got, "extracted file over 1 MiB")
	}

	// gzipped too, which is the shape a sparse folder upload takes, and under its own
	// directory so this case says 201 for a path it creates rather than 200 for one the
	// case above already made.
	holesGz := makeRawTar(t, true, sparseRawEntry("holes.bin", 200000,
		[][2]int64{{0, 1024}, {100000, 1024}}, strings.Repeat("A", 1024)+strings.Repeat("B", 1024)))
	rec = put(t, s, base+"gz/holes.tar.gz", "test-key", string(holesGz))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT gz/holes.tar.gz = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if rec := get(t, s, base+"gz/holes.bin"); rec.Body.String() != wantHoles {
		t.Errorf("GET gz/holes.bin from the gzipped archive = %d bytes, want %d bytes read byte-identical",
			len(rec.Body.String()), len(wantHoles))
	}

	if got := listingPaths(t, s, hash); len(got) == 0 {
		t.Errorf("the sparse archives stored nothing")
	}
}

// hasTypeflag reports whether a raw tar body carries a header block with that typeflag.
func hasTypeflag(body []byte, typeflag byte) bool {
	for start := 0; start+512 <= len(body); start += 512 {
		if body[start+156] == typeflag && !isZeroBlock(body[start:start+512]) {
			return true
		}
	}
	return false
}

// TestTarWalkBailsLeaveValidatedNamesBehind keeps the residual honest: the name walk gives
// up on a layout it cannot follow — a sparse entry's extra blocks, a GNU long name's data
// block — and there the entry lands under the name archive/tar itself read, which is a
// name the upload rules still judged. These are the shapes the README scopes.
func TestTarWalkBailsLeaveValidatedNamesBehind(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	// A NUL-overrun name after a sparse map: the walk stops at the sparse entry it cannot
	// count past, so the reader's truncated reading of the name is what lands.
	after := makeRawTar(t, false,
		sparseRawEntry("holes.bin", 2000, [][2]int64{{0, 512}}, strings.Repeat("A", 512)),
		rawTarEntry{name: "evil\x00.txt", typ: tar.TypeReg, body: "hostile"})
	rec := put(t, s, base+"after-sparse.tar", "test-key", string(after))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT after-sparse.tar = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != "evil,holes.bin" {
		t.Errorf("listing after a sparse map = %v, want the entry under the name archive/tar read", got)
	}
	if rec := get(t, s, base+"evil"); rec.Code != http.StatusOK || rec.Body.String() != "hostile" {
		t.Errorf("GET evil = %d with body %q, want the validated truncated name served", rec.Code, rec.Body.String())
	}

	// A name a GNU long name record carries is not in any 100-byte name field for the walk
	// to read at all, and the record's own bytes stop at a NUL for the reader too: the
	// entry lands under the truncated long name, not the name its own header spells.
	longBody := strings.Repeat("deep/", 8) + strings.Repeat("x", 60) + "evil\x00.txt"
	longName := makeRawTar(t, false,
		rawTarEntry{name: "././@LongLink", typ: tar.TypeGNULongName, body: longBody, gnu: true},
		rawTarEntry{name: "short.txt", typ: tar.TypeReg, body: "hostile"})
	rec = put(t, s, base+"longname.tar", "test-key", string(longName))
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT longname.tar = %d, want %d; body %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	wantLong := strings.Repeat("deep/", 8) + strings.Repeat("x", 60) + "evil"
	if got := listingPaths(t, s, hash); strings.Join(got, ",") != wantLong+",evil,holes.bin" {
		t.Errorf("listing after a long name record = %v, want the entry under %q", got, wantLong)
	}
	for _, p := range []string{"short.txt", wantLong + ".txt"} {
		if rec := get(t, s, base+p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 — neither the entry's own name nor the name past the NUL lands", p, rec.Code)
		}
	}
	if rec := get(t, s, base+wantLong); rec.Code != http.StatusOK || rec.Body.String() != "hostile" {
		t.Errorf("GET the truncated long name = %d with body %q, want it served", rec.Code, rec.Body.String())
	}
}

// TestEntryNamesWithinTheirOwnCaps keeps the caps honest on both sides of the join: a name
// the root accepts stays accepted, and the same name under a deep folder is refused by the
// length and depth of the path it lands at.
func TestEntryNamesWithinTheirOwnCaps(t *testing.T) {
	s := newTestServer(t, "test-key")
	base := createTestBin(t, s, "test-key")
	hash := strings.Trim(base, "/")

	long := strings.Repeat("n", maxPathLen)               // exactly the cap a name may have
	deep := strings.Repeat("s/", maxPathSegs-1) + "x.txt" // exactly the segments a name may have
	for _, name := range []string{long, deep} {
		body := makeZip(t, zipEntry{name: name, body: "fits"})
		rec := put(t, s, base+"root.zip", "test-key", string(body))
		if rec.Code != http.StatusCreated {
			t.Errorf("PUT root.zip with entry %q = %d, want %d; body %q", name, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}
	if got := listingPaths(t, s, hash); len(got) != 2 {
		t.Errorf("listing = %v, want the two accepted entries", got)
	}

	dir := strings.Repeat("d/", 10)
	for _, tc := range []struct {
		entry string
		want  string
	}{
		{strings.Repeat("n", 240), "path too long"},                                   // 20 bytes of folder + the name
		{strings.Repeat("s/", maxPathSegs-1) + "x.txt", "path has too many segments"}, // 10 folders + 15 + 1
	} {
		body := makeZip(t, zipEntry{name: tc.entry, body: "hostile"})
		rec := put(t, s, base+dir+"abs.zip", "test-key", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %sabs.zip with entry %q = %d, want %d; body %q", dir, tc.entry, rec.Code, http.StatusBadRequest, rec.Body.String())
			continue
		}
		if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tc.want {
			t.Errorf("entry %q under %s body = %q, want %q", tc.entry, dir, got, tc.want)
		}
	}
	if got := listingPaths(t, s, hash); len(got) != 2 {
		t.Errorf("listing = %v, want the refused joins to have stored nothing", got)
	}
}
