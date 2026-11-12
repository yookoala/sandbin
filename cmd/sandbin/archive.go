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
	"strconv"
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

// tarRawEntryNames reads the tar block layout just far enough to look at every entry's
// name field as the raw bytes carry it. archive/tar stops reading a name at the first
// NUL, so a header whose name field goes on past that terminator names a file no
// filesystem can hold, and the entry would land under the shortened name the archive
// never claimed. Anything this walk cannot trust — a block whose ustar magic or checksum
// does not hold, a GNU sparse entry with its extra blocks, a walk past the 32 MiB cap, a
// size field spelling a negative or unholdable number — ends the walk and leaves the
// archive to the tar reader itself, so a genuine archive is never refused over a miscount.
// A size field too big for octal digits is read the POSIX way rather than given up on
// (tarSize), because giving up used to let one base-256 size hide the name of every entry
// after it. Whatever the walk concludes, fromTar judges every entry the reader surfaces
// through pathOf, so no name is ever stored without the upload rules having been run on it.
func tarRawEntryNames(r io.Reader) error {
	blk := make([]byte, 512)
	var walked int64
	for {
		if _, err := io.ReadFull(r, blk); err != nil {
			return nil // a short stream is the tar reader's "corrupt" to name, not a name problem
		}
		if isZeroBlock(blk) {
			return nil
		}
		if !tarBlockTrustworthy(blk) {
			return nil
		}
		if field := blk[:100]; rawNameRunsPastItsTerminator(field) {
			return refusal("archive contains an entry name with a NUL byte %q",
				strings.TrimRight(string(field), "\x00"))
		}
		size, ok := tarSize(blk[124:136])
		if !ok || size < 0 || blk[156] == tar.TypeGNUSparse {
			return nil // a size this walk cannot trust, or a sparse map: the reader's own verdict
		}
		if size > maxBodyBytes {
			// A header claiming more than a bin file may hold, named exactly the way the
			// streaming caps name the same breach once the bytes are read.
			return sizeRefusal("extracted file over 1 MiB")
		}
		padded := (size + 511) / 512 * 512
		if walked += 512 + padded; walked > maxExtractTotal {
			return nil // the unpack cap is the tar reader's own refusal to make
		}
		if padded > 0 {
			if _, err := io.CopyN(io.Discard, r, padded); err != nil {
				return nil
			}
		}
	}
}

// tarRawEntryNamesGz runs the same walk over a second, independent read of a gzipped
// tar, so the streaming pass that validates and reads the entries stays exactly as it is.
func tarRawEntryNamesGz(body []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil // the archive pass is the one that names corrupt bytes
	}
	defer gz.Close()
	return tarRawEntryNames(gz)
}

// tarBlockTrustworthy reports a header block whose ustar magic and stored checksum hold,
// which is all this walk needs before it believes the block is a header at all.
func tarBlockTrustworthy(blk []byte) bool {
	if !bytes.Equal(blk[257:263], []byte("ustar\x00")) && !bytes.Equal(blk[257:263], []byte("ustar ")) {
		return false
	}
	var sum int
	for i, b := range blk {
		if i >= 148 && i < 156 {
			sum += ' ' // the checksum field is spelled as spaces while the sum is taken
			continue
		}
		sum += int(b)
	}
	stored, ok := tarOctal(blk[148:156])
	return ok && stored == int64(sum)
}

// tarOctal reads one tar octal field, which ends at a NUL or a space. A field this
// function cannot read (a base-2 GNU number, junk) is "not ok", which stops the walk.
// The size field has its own reader, tarSize, because POSIX lets a size be spelled some
// other way; every other field the walk looks at stays octal.
func tarOctal(field []byte) (int64, bool) {
	s := string(field)
	if i := strings.IndexAny(s, "\x00 "); i >= 0 {
		s = s[:i]
	}
	value, err := strconv.ParseInt(strings.TrimSpace(s), 8, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// tarSize reads a size field the way POSIX says one may be spelled: plain octal, or
// base-256 when the first byte carries the 0x80 bit — the remaining bytes being a
// big-endian two's complement number, exactly the decoding archive/tar itself applies. A
// number too wide to hold answers "not ok"; a negative one comes back as it decoded, and
// it is the walk that reads a negative size as a header lying rather than an entry.
func tarSize(field []byte) (int64, bool) {
	if len(field) == 0 || field[0]&0x80 == 0 {
		return tarOctal(field)
	}
	inv := byte(0x00) // 0x00 for a positive number, 0xff to read a negative one as unsigned
	if field[0]&0x40 != 0 {
		inv = 0xff
	}
	var x uint64
	for i, b := range field {
		b ^= inv
		if i == 0 {
			b &= 0x7f // the top bit says "base-256", the next one says "negative"
		}
		if x>>56 > 0 {
			return 0, false // the number is too wide for an int64
		}
		x = x<<8 | uint64(b)
	}
	if x>>63 > 0 {
		return 0, false
	}
	if inv == 0xff {
		return ^int64(x), true
	}
	return int64(x), true
}

// rawNameRunsPastItsTerminator reports name bytes that keep going after the NUL that
// ends the name.
func rawNameRunsPastItsTerminator(field []byte) bool {
	i := bytes.IndexByte(field, 0)
	if i < 0 {
		return false // the field is full of name, which is how tar writes a 100-byte one
	}
	for _, b := range field[i+1:] {
		if b != 0 {
			return true
		}
	}
	return false
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

// extractArchive parses an archive whole, judges every raw entry name against the rules
// a normal upload name obeys before the entry is placed anywhere, and returns the files
// the bin would gain: nothing is stored until the caller runs PutFiles on the full batch,
// so a refusal mid-parse leaves the bin exactly as it was. Entries land under the upload
// name's parent directory and the archive name itself is never among them.
func extractArchive(kind archiveKind, sub string, body []byte) ([]FilePut, error) {
	collector := &extractCollector{dir: parentOfUploadName(sub), seen: map[string]bool{}}
	var err error
	switch kind {
	case archiveZip:
		err = collector.fromZip(body)
	case archiveTar:
		if err := tarRawEntryNames(bytes.NewReader(body)); err != nil {
			return nil, err
		}
		err = collector.fromTar(bytes.NewReader(body), "corrupt tar archive")
	case archiveTarGz:
		if err := tarRawEntryNamesGz(body); err != nil {
			return nil, err
		}
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

// pathOf judges one archive entry name and answers the bin path it lands at. The name
// is judged as the archive wrote it, before path.Join gets a chance to clean it: joined
// first, "sub/../up.txt" would quietly become "up.txt" and "/etc/passwd" would lose its
// leading slash, so an entry could move a file out of the folder the archive lands in.
// The format's own conventions come first — a zip's backslashes become slashes, one
// trailing "/" is how an archive marks a directory, and one leading "./" is the
// `tar czf - .` idiom — and what is left obeys the same cleanPath every upload name
// obeys, which refuses a "." or ".." segment anywhere, an absolute name, a control
// character, a backslash (tar keeps them), and over-long or over-deep names. The joined
// path is cleaned once more as the belt-and-braces check on the caps of the path the
// entry actually lands at.
func (c *extractCollector) pathOf(raw string, isFile bool) (string, error) {
	name := raw
	if c.zipEntry {
		name = strings.ReplaceAll(name, "\\", "/")
	}
	name = strings.TrimSuffix(name, "/")
	name = strings.TrimPrefix(name, "./")
	if name == "" || name == "." {
		if isFile {
			return "", unnamedEntry(raw)
		}
		return "", nil // the folder the archive was made of: validated by being the destination
	}
	entered, err := cleanPath(name)
	if err != nil {
		// cleanPath only ever refuses with its own validation words; they are the
		// refusal the client sees, verbatim, at 400.
		return "", &archiveErr{status: http.StatusBadRequest, message: err.Error()}
	}
	landed, err := cleanPath(path.Join(c.dir, entered))
	if err != nil {
		return "", &archiveErr{status: http.StatusBadRequest, message: err.Error()}
	}
	return landed, nil
}

// unnamedEntry names an entry that claims to be a file and was given no name at all.
func unnamedEntry(raw string) error {
	if raw == "" {
		return refusal("archive contains an unnamed entry")
	}
	return refusal("archive contains an unnamed entry %s", raw)
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
		mode := f.Mode()
		isDir := mode.IsDir() || strings.HasSuffix(f.Name, "/")
		p, err := c.pathOf(f.Name, !isDir)
		if err != nil {
			return err
		}
		if isDir {
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
			if _, err := c.pathOf(hdr.Name, false); err != nil {
				return err
			}
			continue
		case tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			p, err := c.pathOf(hdr.Name, true)
			if err != nil {
				return err
			}
			return refusal("archive contains a %s %s", tarEntryType(hdr.Typeflag), p)
		case tar.TypeReg, tar.TypeRegA, tar.TypeGNUSparse:
			// A GNU sparse entry (typeflag 'S', the old in-header map and its extended
			// map blocks alike) is a regular file with holes, and the reader already
			// materializes those holes as zero bytes: the caps below count the bytes it
			// answers exactly as they count any other file's. The pax-based GNU sparse
			// variants and the GNU long name and long link records never appear here —
			// the reader folds them into the entry they describe.
			p, err := c.pathOf(hdr.Name, true)
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
