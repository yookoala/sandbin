package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNoExtractSendsTheHeaderOnEveryPut is the opt-out wiring: --no-extract puts
// X-Extract on every PUT of the run, and a plain run sends none.
func TestNoExtractSendsTheHeaderOnEveryPut(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "a.zip", "PK fake zip bytes")
	writeWorkDirFile(t, "b.tar.gz", "not gzip at all")

	if _, _, err := runClient(t, nil, "upload", "--no-extract", "a.zip", "b.tar.gz"); err != nil {
		t.Fatalf("--no-extract run: %v", err)
	}
	if got := f.extractValues(); !reflect.DeepEqual(got, []string{"keep", "keep"}) {
		t.Errorf("X-Extract values = %q, want keep on both PUTs", got)
	}
	if got := f.stored("bin0001/a.zip"); got != "PK fake zip bytes" {
		t.Errorf("stored a.zip = %q, want the body unchanged", got)
	}

	f2 := newFake(t, "test-key")
	writeWorkDirFile(t, "a.zip", "PK fake zip bytes")
	writeWorkDirFile(t, "b.tar.gz", "not gzip at all")
	if _, _, err := runClient(t, nil, "upload", "a.zip", "b.tar.gz"); err != nil {
		t.Fatalf("plain run: %v", err)
	}
	if got := f2.extractValues(); !reflect.DeepEqual(got, []string{"", ""}) {
		t.Errorf("X-Extract values = %q, want no header on a plain run", got)
	}
}

// TestArchiveExtractsIntoTheBin is the headline run: --archive hello builds a
// tar.gz whose entries are the folder's paths verbatim, the fake server unpacks
// them, and stdout says how many landed where. The archive name itself is not
// stored.
func TestArchiveExtractsIntoTheBin(t *testing.T) {
	f := newFake(t, "test-key")
	f.setUnpack(true)
	writeWorkDirFile(t, "hello/world.html", "<p>world</p>")
	writeWorkDirFile(t, "hello/sub/deep.html", "<p>deep</p>")

	stdout, stderr, err := runClient(t, nil, "upload", "--archive", "hello")
	if err != nil {
		t.Fatalf("--archive hello: %v; stderr %s", err, stderr)
	}

	base := os.Getenv("SANDBIN_URL")
	want := []string{
		"extracted 2 files into " + base + "/bin0001/hello.tar.gz",
		base + "/bin0001/",
	}
	got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := f.stored("bin0001/hello/world.html"); got != "<p>world</p>" {
		t.Errorf("stored hello/world.html = %q", got)
	}
	if got := f.stored("bin0001/hello/sub/deep.html"); got != "<p>deep</p>" {
		t.Errorf("stored hello/sub/deep.html = %q", got)
	}
	if got := f.stored("bin0001/hello.tar.gz"); got != "" {
		t.Errorf("stored hello.tar.gz = %q, want the archive name not stored", got)
	}
	if n := f.putsSeen(); n != 1 {
		t.Errorf("PUTs = %d, want exactly the one archive upload", n)
	}
}

// TestArchiveNestedFolderIsBinRelative is the mapping promise: --archive
// projects/app uploads app.tar.gz at the bin root, entries carry projects/app/
// verbatim, and the bin's paths come out exactly folder-relative — never a
// doubled projects/projects.
func TestArchiveNestedFolderIsBinRelative(t *testing.T) {
	f := newFake(t, "test-key")
	f.setUnpack(true)
	writeWorkDirFile(t, "projects/app/index.html", "the app")
	writeWorkDirFile(t, "projects/app/css/app.css", "body {}")

	stdout, stderr, err := runClient(t, nil, "upload", "--archive", "projects/app")
	if err != nil {
		t.Fatalf("--archive projects/app: %v; stderr %s", err, stderr)
	}

	if got := f.putOrder(); !reflect.DeepEqual(got, []string{"/bin0001/app.tar.gz"}) {
		t.Errorf("PUT paths = %q, want the archive at the bin root named app.tar.gz", got)
	}
	for _, want := range []struct{ key, body string }{
		{"bin0001/projects/app/css/app.css", "body {}"},
		{"bin0001/projects/app/index.html", "the app"},
	} {
		if got := f.stored(want.key); got != want.body {
			t.Errorf("stored %s = %q, want %q", want.key, got, want.body)
		}
	}
	for _, key := range []string{"bin0001/app.tar.gz", "bin0001/projects/projects/app/index.html"} {
		if got := f.stored(key); got != "" {
			t.Errorf("stored %s = %q, want nothing there", key, got)
		}
	}
	if !strings.Contains(stdout, "extracted 2 files into "+os.Getenv("SANDBIN_URL")+"/bin0001/app.tar.gz") {
		t.Errorf("stdout = %q, want the extracted line naming app.tar.gz", stdout)
	}
}

// TestArchiveCombinedWithFilesAndStdin keeps the flat client order: file
// arguments first, then named stdin, and the folder archive uploads last.
func TestArchiveCombinedWithFilesAndStdin(t *testing.T) {
	f := newFake(t, "test-key")
	f.setUnpack(true)
	writeWorkDirFile(t, "first.txt", "one")
	writeWorkDirFile(t, "hello/world.html", "two")

	stdout, stderr, err := runClient(t, strings.NewReader("piped"), "upload",
		"--archive", "hello", "--name", "piped.txt", "first.txt")
	if err != nil {
		t.Fatalf("combined run: %v; stderr %s", err, stderr)
	}
	wantOrder := []string{"/bin0001/first.txt", "/bin0001/piped.txt", "/bin0001/hello.tar.gz"}
	if got := f.putOrder(); !reflect.DeepEqual(got, wantOrder) {
		t.Errorf("PUT order = %q, want %q", got, wantOrder)
	}
	base := os.Getenv("SANDBIN_URL")
	wantLines := []string{
		"created " + base + "/bin0001/first.txt",
		"created " + base + "/bin0001/piped.txt",
		"extracted 1 files into " + base + "/bin0001/hello.tar.gz",
		base + "/bin0001/",
	}
	got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if !reflect.DeepEqual(got, wantLines) {
		t.Errorf("stdout = %q, want %q", got, wantLines)
	}
}

// TestArchiveRawModeWithNoExtract uploads the built tar.gz as one plain file:
// the opt-out header goes, the bin holds exactly the archive, and the result
// line is the old created line.
func TestArchiveRawModeWithNoExtract(t *testing.T) {
	f := newFake(t, "test-key")
	f.setUnpack(true) // on, so only the X-Extract header can keep it raw
	writeWorkDirFile(t, "hello/world.html", "two")

	stdout, stderr, err := runClient(t, nil, "upload", "--archive", "hello", "--no-extract")
	if err != nil {
		t.Fatalf("--archive hello --no-extract: %v; stderr %s", err, stderr)
	}
	if got := f.extractValues(); !reflect.DeepEqual(got, []string{"keep"}) {
		t.Errorf("X-Extract values = %q, want keep on the archive PUT", got)
	}
	body := f.stored("bin0001/hello.tar.gz")
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Fatalf("stored hello.tar.gz = %q..., want the gzip archive bytes", body[:min(20, len(body))])
	}
	if got := f.stored("bin0001/hello/world.html"); got != "" {
		t.Errorf("stored hello/world.html = %q, want nothing extracted", got)
	}
	if !strings.HasPrefix(stdout, "created "+os.Getenv("SANDBIN_URL")+"/bin0001/hello.tar.gz\n") {
		t.Errorf("stdout = %q, want the plain created line", stdout)
	}
}

// TestArchivePreflightRefusals makes every folder-shaped refusal land with zero
// requests and no bin created, the way the client refuses bad names today.
func TestArchivePreflightRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		args  []string
		want  string
	}{
		{"dot", nil, []string{"--archive", "."}, "--archive names a folder"},
		{"dot slash", nil, []string{"--archive", "./"}, "--archive names a folder"},
		{"absolute", nil, []string{"--archive", "/tmp"}, "--archive names a folder"},
		{"empty value", nil, []string{"--archive", ""}, "--archive names a folder"},
		{"missing", nil, []string{"--archive", "missing"}, "no such file"},
		{"not a directory", func(t *testing.T) { writeWorkDirFile(t, "hello.txt", "x") },
			[]string{"--archive", "hello.txt"}, "is not a directory"},
		{"symlink inside", func(t *testing.T) {
			writeWorkDirFile(t, "snafu/ok.txt", "x")
			if err := os.Symlink("ok.txt", "snafu/link"); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}, []string{"--archive", "snafu"}, "snafu/link is not a regular file"},
		{"file over 1 MiB", func(t *testing.T) {
			writeWorkDirFile(t, "fat/big.bin", strings.Repeat("x", maxBodyBytes+1))
		}, []string{"--archive", "fat"}, "fat/big.bin is over 1 MiB"},
		{"over 1000 files", func(t *testing.T) {
			for i := range maxArchiveEntries + 1 {
				writeWorkDirFile(t, fmt.Sprintf("many/f%04d.txt", i), "x")
			}
		}, []string{"--archive", "many"}, "folder has over 1000 files"},
		{"over 32 MiB total", func(t *testing.T) {
			for i := range maxExtractTotal/(1<<20) + 1 {
				name := fmt.Sprintf("huge/z%03d.bin", i)
				if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				fh, err := os.Create(name) // sparse: size without bytes
				if err != nil {
					t.Fatalf("create %s: %v", name, err)
				}
				if err := fh.Truncate(1 << 20); err != nil {
					t.Fatalf("truncate %s: %v", name, err)
				}
				fh.Close()
			}
		}, []string{"--archive", "huge"}, "folder unpacks over 32 MiB"},
		{"no regular files", func(t *testing.T) {
			if err := os.MkdirAll("empty/only/dirs", 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}, []string{"--archive", "empty"}, "folder contains no files"},
		{"entry too deep", func(t *testing.T) {
			writeWorkDirFile(t, "deep/"+strings.Repeat("d/", maxPathSegs)+"x.txt", "x")
		}, []string{"--archive", "deep"}, "path has too many segments"},
		{"entry too long", func(t *testing.T) {
			writeWorkDirFile(t, "l/"+strings.Repeat("n", 255), "x")
		}, []string{"--archive", "l"}, "path too long"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, "test-key")
			if tc.setup != nil {
				tc.setup(t)
			}
			stdout, stderr, err := runClient(t, nil, append([]string{"upload"}, tc.args...)...)
			if err == nil {
				t.Fatalf("run %q succeeded, want a refusal", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if !strings.Contains(stderr, "sandbin-client:") {
				t.Errorf("stderr = %q, want the refusal echoed", stderr)
			}
			if n := f.requestsSeen(); n != 0 {
				t.Errorf("requests = %d, want zero", n)
			}
			if n := f.binCount(); n != 0 {
				t.Errorf("bins = %d, want none created", n)
			}
		})
	}
}

// TestDescribeRefusedWithArchive keeps describe read-only when --archive joins: the flag
// is not even defined on describe, so joining it is a usage refusal with zero requests.
func TestDescribeRefusedWithArchive(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "hello/world.html", "x")
	_, stderr, err := runClient(t, nil, "describe", "--archive", "hello")
	if err == nil {
		t.Fatalf("describe --archive succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "flag provided but not defined: -archive") {
		t.Errorf("stderr = %q, want the usage refusal naming --archive", stderr)
	}
	if n := f.requestsSeen(); n != 0 {
		t.Errorf("requests = %d, want zero", n)
	}
}

// archiveTempLeftovers is the temp archive scan the cleanup tests use: nothing named
// sandbin-archive-* may survive a run, whatever the run did.
func archiveTempLeftovers(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "sandbin-archive-*"))
	if err != nil {
		t.Fatalf("scan temp dir: %v", err)
	}
	return matches
}

// TestArchiveRefusesPipedStdinWithoutAName keeps the stdin rule whole: --archive brings
// its own upload target, but what was piped in still needs a --name, and the refusal is
// the same sentence a run without --archive gives, with zero requests and no archive
// built.
func TestArchiveRefusesPipedStdinWithoutAName(t *testing.T) {
	f := newFake(t, "test-key")
	writeWorkDirFile(t, "hello/world.html", "x")

	stdout, stderr, err := runClient(t, strings.NewReader("piped in\n"), "upload", "--archive", "hello")
	if err == nil {
		t.Fatalf("--archive with piped stdin and no --name succeeded, want a refusal")
	}
	if !strings.Contains(stderr, "stdin needs --name to say what it uploads") {
		t.Errorf("stderr = %q, want the plain stdin refusal", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if n := f.requestsSeen(); n != 0 {
		t.Errorf("requests = %d, want zero", n)
	}
	if n := f.binCount(); n != 0 {
		t.Errorf("bins = %d, want none created", n)
	}
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Errorf("temp dir holds %v after the refusal, want nothing", got)
	}

	// The same run without --archive refuses with the same words, which is what
	// "regardless of --archive" has to mean.
	_, plainStderr, err := runClient(t, strings.NewReader("piped in\n"), "upload")
	if err == nil || plainStderr != stderr {
		t.Errorf("plain run stderr = %q, want the same refusal as the --archive run (%q)", plainStderr, stderr)
	}

	// With a --name, stdin and the folder both upload, stdin first.
	f2 := newFake(t, "test-key")
	f2.setUnpack(true)
	writeWorkDirFile(t, "hello/world.html", "x")
	if _, _, err := runClient(t, strings.NewReader("piped in\n"), "upload", "--archive", "hello", "--name", "piped.txt"); err != nil {
		t.Fatalf("--archive with --name: %v", err)
	}
	want := []string{"/bin0001/piped.txt", "/bin0001/hello.tar.gz"}
	if got := f2.putOrder(); !reflect.DeepEqual(got, want) {
		t.Errorf("PUT order = %q, want %q", got, want)
	}
	if got := f2.stored("bin0001/piped.txt"); got != "piped in\n" {
		t.Errorf("stored piped.txt = %q, want the piped bytes", got)
	}
	if got := f2.stored("bin0001/hello/world.html"); got != "x" {
		t.Errorf("stored hello/world.html = %q, want the folder file", got)
	}
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Errorf("temp dir holds %v after the run, want nothing", got)
	}
}

// TestArchiveOverOneMiBRefusesBeforeAnyRequest is the one client-side cap that can only be
// measured after the archive exists: three files of incompressible random data, each under
// the 1 MiB file cap and the folder under the 32 MiB total, so the preflight walk passes and
// only the built archive is over the cap every PUT body obeys.
func TestArchiveOverOneMiBRefusesBeforeAnyRequest(t *testing.T) {
	f := newFake(t, "test-key")
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Fatalf("temp dir already holds %v before the test", got)
	}

	if err := os.MkdirAll("fat", 0755); err != nil {
		t.Fatalf("mkdir fat: %v", err)
	}
	for i := range 3 {
		data := make([]byte, 600<<10) // under the 1 MiB per-file cap, and incompressible
		if _, err := rand.Read(data); err != nil {
			t.Fatalf("read random data: %v", err)
		}
		if err := os.WriteFile(filepath.Join("fat", fmt.Sprintf("f%d.bin", i)), data, 0644); err != nil {
			t.Fatalf("write fat/f%d.bin: %v", i, err)
		}
	}

	stdout, stderr, err := runClient(t, nil, "upload", "--archive", "fat")
	if err == nil {
		t.Fatalf("--archive fat succeeded, want the built archive refused")
	}
	if !strings.Contains(err.Error(), "archive is over 1 MiB") {
		t.Errorf("err = %v, want it to say the archive is over 1 MiB", err)
	}
	if !strings.Contains(stderr, "archive is over 1 MiB") {
		t.Errorf("stderr = %q, want the refusal echoed", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if n := f.requestsSeen(); n != 0 {
		t.Errorf("requests = %d, want zero", n)
	}
	if n := f.binCount(); n != 0 {
		t.Errorf("bins = %d, want none created", n)
	}
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Errorf("temp dir holds %v after the refusal, want no sandbin-archive-* left", got)
	}
}

// TestArchiveTempFileRemoved proves the temp archive never survives the run,
// through a success and through a server failure.
func TestArchiveTempFileRemoved(t *testing.T) {
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Fatalf("temp dir already holds %v before the test", got)
	}

	f := newFake(t, "test-key")
	f.setUnpack(true)
	writeWorkDirFile(t, "hello/world.html", "x")
	if _, _, err := runClient(t, nil, "upload", "--archive", "hello"); err != nil {
		t.Fatalf("--archive hello: %v", err)
	}
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Errorf("temp dir holds %v after a success, want nothing", got)
	}

	f2 := newFake(t, "test-key")
	writeWorkDirFile(t, "hello/world.html", "x")
	f2.setPutFail(500)
	if _, _, err := runClient(t, nil, "upload", "--archive", "hello"); err == nil {
		t.Fatalf("--archive hello against a 500 server succeeded, want the failure")
	}
	if got := archiveTempLeftovers(t); len(got) != 0 {
		t.Errorf("temp dir holds %v after an upload failure, want nothing", got)
	}
}
