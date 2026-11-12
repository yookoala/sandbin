package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

const (
	maxArchiveEntries = 1000
	maxExtractTotal   = 32 << 20
)

type archiveKind int

const (
	archiveNone archiveKind = iota
	archiveZip
	archiveTar
	archiveTarGz
)

// detectArchive reports whether a PUT name and body describe an archive: the
// extension decides whether the body is sniffed at all, the magic decides the
// format, so a .tgz name holding a zip is unzipped and a lying extension cannot
// smuggle a format mismatch. An archive-looking name holding no magic is the one
// refusal; an ordinary name is archiveNone, never sniffed.
func detectArchive(sub string, body []byte) (archiveKind, error) {
	lower := strings.ToLower(sub)
	if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".tar") &&
		!strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
		return archiveNone, nil
	}
	switch {
	case bytes.HasPrefix(body, []byte("PK\x03\x04")), bytes.HasPrefix(body, []byte("PK\x05\x06")):
		return archiveZip, nil
	case bytes.HasPrefix(body, []byte{0x1f, 0x8b}):
		return archiveTarGz, nil
	case len(body) >= 257+len("ustar\x00") &&
		(bytes.Equal(body[257:257+6], []byte("ustar\x00")) || bytes.Equal(body[257:257+6], []byte("ustar "))):
		return archiveTar, nil
	case len(body) >= 2*512 && isZeroBlock(body[:512]):
		return archiveTar, nil // a tar written without files is nothing but zero blocks
	}
	return archiveNone, errors.New("not a zip, tar or tar.gz archive")
}

// isZeroBlock reports whether a 512-byte tar block is entirely zero, which is how a
// tar file says it has no more entries.
func isZeroBlock(block []byte) bool {
	for _, b := range block {
		if b != 0 {
			return false
		}
	}
	return true
}

// archiveErr is a refusal to extract an archive: the status to answer and the exact
// words the decisions settled on.
type archiveErr struct {
	status  int
	message string
}

func (e *archiveErr) Error() string { return e.message }

func refusal(format string, args ...any) error {
	return &archiveErr{status: http.StatusBadRequest, message: fmt.Sprintf(format, args...)}
}

func sizeRefusal(format string, args ...any) error {
	return &archiveErr{status: http.StatusRequestEntityTooLarge, message: fmt.Sprintf(format, args...)}
}

// archiveErrStatus reports the status an extraction error must be answered with.
func archiveErrStatus(err error) int {
	var ae *archiveErr
	if errors.As(err, &ae) {
		return ae.status
	}
	return http.StatusInternalServerError
}

// extractArchive parses an archive whole, validates every entry against the rules a
// normal upload name obeys, and returns the files the bin would gain: nothing is
// stored until the caller runs PutFiles on the full batch, so a refusal mid-parse
// leaves the bin exactly as it was. Entries land under the upload name's parent
// directory and the archive name itself is never among them.
func extractArchive(kind archiveKind, sub string, body []byte) ([]FilePut, error) {
	collector := &extractCollector{dir: parentOfUploadName(sub), seen: map[string]bool{}}
	var err error
	switch kind {
	case archiveZip:
		err = collector.fromZip(body)
	case archiveTar:
		err = collector.fromTar(bytes.NewReader(body), "corrupt tar archive")
	case archiveTarGz:
		gz, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr != nil {
			return nil, &archiveErr{status: http.StatusBadRequest, message: "corrupt tar.gz archive"}
		}
		defer gz.Close()
		err = collector.fromTar(gz, "corrupt tar.gz archive")
	default:
		return nil, errors.New("not a zip, tar or tar.gz archive")
	}
	if err != nil {
		return nil, err
	}
	if len(collector.files) == 0 {
		return nil, refusal("archive contains no files")
	}
	return collector.files, nil
}

// parentOfUploadName is the directory an archive's entries land under: the upload
// name's own directory, or the bin root ("") when the archive sits there.
func parentOfUploadName(sub string) string {
	if dir := path.Dir(sub); dir != "." {
		return dir
	}
	return ""
}

type extractCollector struct {
	dir      string
	seen     map[string]bool
	files    []FilePut
	total    int64
	zipEntry bool // zip entry names get their backslashes converted before combining
}

// pathOf combines one entry name with the destination directory and runs the
// result through the same cleanPath every upload name obeys, so zip slip is
// refused by the guard the repo already proves against adversarial input.
func (c *extractCollector) pathOf(name string) (string, error) {
	if c.zipEntry {
		name = strings.ReplaceAll(name, "\\", "/")
	}
	combined, err := cleanPath(path.Join(c.dir, name))
	if err != nil {
		// cleanPath only ever refuses with its own validation words; they are the
		// refusal the client sees, verbatim, at 400.
		return "", &archiveErr{status: http.StatusBadRequest, message: err.Error()}
	}
	return combined, nil
}

// add validates one regular-file entry, reads it under the caps while streaming
// (so a 1 MiB body claiming 4 GiB stops at the caps, not after inflation), and
// appends it to the batch.
func (c *extractCollector) add(p string, r io.Reader) error {
	if c.seen[p] {
		return refusal("archive contains duplicate path %s", p)
	}
	if len(c.files) >= maxArchiveEntries {
		return sizeRefusal("archive has over 1000 files")
	}
	content, err := io.ReadAll(io.LimitReader(r, maxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(content) > maxBodyBytes {
		return sizeRefusal("extracted file over 1 MiB")
	}
	c.total += int64(len(content))
	if c.total > maxExtractTotal {
		return sizeRefusal("archive unpacks over 32 MiB")
	}
	c.seen[p] = true
	c.files = append(c.files, FilePut{Path: p, Content: content})
	return nil
}

func (c *extractCollector) fromZip(body []byte) error {
	c.zipEntry = true
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return &archiveErr{status: http.StatusBadRequest, message: "corrupt zip archive"}
	}
	for _, f := range zr.File {
		p, err := c.pathOf(f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		if mode.IsDir() || strings.HasSuffix(f.Name, "/") {
			continue // directories exist only as path prefixes in a bin
		}
		if f.Flags&0x1 != 0 {
			return refusal("archive contains an encrypted entry %s", p)
		}
		if !mode.IsRegular() {
			return refusal("archive contains a %s %s", archiveEntryType(mode), p)
		}
		r, err := f.Open()
		if err != nil {
			return &archiveErr{status: http.StatusBadRequest, message: "corrupt zip archive"}
		}
		if err := c.add(p, r); err != nil {
			r.Close()
			if isArchiveCorruption(err) {
				return &archiveErr{status: http.StatusBadRequest, message: "corrupt zip archive"}
			}
			return err
		}
		r.Close()
	}
	return nil
}

func (c *extractCollector) fromTar(r io.Reader, corruptMessage string) error {
	corrupt := &archiveErr{status: http.StatusBadRequest, message: corruptMessage}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return corrupt
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			continue // pax global headers describe the archive, not a file
		case tar.TypeDir:
			if _, err := c.pathOf(hdr.Name); err != nil {
				return err
			}
			continue
		case tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			p, err := c.pathOf(hdr.Name)
			if err != nil {
				return err
			}
			return refusal("archive contains a %s %s", tarEntryType(hdr.Typeflag), p)
		case tar.TypeReg, tar.TypeRegA:
			p, err := c.pathOf(hdr.Name)
			if err != nil {
				return err
			}
			if err := c.add(p, tr); err != nil {
				if isArchiveCorruption(err) {
					return corrupt
				}
				return err
			}
		default:
			return corrupt
		}
	}
}

// isArchiveCorruption reports a read failure that came from the archive's own
// bytes, as opposed to a cap refusal this collector raised.
func isArchiveCorruption(err error) bool {
	var ae *archiveErr
	return !errors.As(err, &ae)
}

func archiveEntryType(mode os.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeDevice != 0, mode&os.ModeCharDevice != 0:
		return "device"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	default:
		return "device" // any other non-regular entry the zip format managed to name
	}
}

func tarEntryType(typeflag byte) string {
	switch typeflag {
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeLink:
		return "hardlink"
	case tar.TypeChar, tar.TypeBlock:
		return "device"
	case tar.TypeFifo:
		return "fifo"
	}
	return "device"
}
