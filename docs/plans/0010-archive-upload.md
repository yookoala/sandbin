# Plan: upload a folder as an archive

Status: closed. Implemented on `main` (`69edbbc`, `4ba462f`, `e0a709e`), reviewed as "ship with
fixes", the two required fixes landed (`5b34be9`, `4e41bea`) and passed the review's re-verification
with verdict "ship", followed by one hardening commit (`9ca1dfe`). Decisions in
`docs/decisions/0010-archive-upload.md`. Extends `docs/plans/0001-basic-implementation.md` (file
routes) and sits beside `docs/plans/0009-bin-metadata.md`.

## What it is

A `PUT` whose name ends in `.zip`, `.tar`, `.tar.gz` or `.tgz` is unpacked by the server: the
archive's files and folders land in the bin under the archive's parent directory, and the archive
itself is not stored. Everything else about a `PUT` — auth, charging, caps, serving — stays as is.
The client gains `--archive <folder>`: it builds a `.tar.gz` of the folder itself, and because the
archive entries keep the folder's relative paths and the archive is uploaded at the bin root, a
file `./hello/world.html` lands as `/<hash>/hello/world.html` — the folder is the base path.

```
PUT  /<hash>/site/bundle.tar.gz     entries land as /<hash>/site/<entry>
     X-Extract header present       store the archive raw instead
GET  /<hash>/site/<entry>           extracted files are ordinary bin files
sandbin-client site/bundle.zip      upload as today; result line reports extraction
sandbin-client --archive hello      builds a tar.gz of ./hello; ./hello/world.html
                                    lands as /<hash>/hello/world.html
sandbin-client --no-extract [...]   force raw storage
```

## Server behaviour

- New `cmd/sandbin/archive.go`: `detectArchive(sub string, body []byte) kind` (extension
  `.zip/.tar/.tar.gz/.tgz`, case-insensitive, plus magic: `PK\x03\x04`/`PK\x05\x06` zip,
  `\x1f\x8b` gzip, `ustar` at byte 257 tar; type comes from the magic) and
  `extractArchive(kind, body) ([]archiveFile, error)` over `archive/zip`, `archive/tar`,
  `compress/gzip` — stdlib only.
- `putFile` order after the unchanged guards (block, key, non-empty sub, `cleanPath(sub)`, `GetBin`
  404, `binAuthorized` 401) and the unchanged 1 MiB `MaxBytesReader` read: if `X-Extract` header is
  present, or no archive is detected, today's single-path `PutFile` runs untouched. Otherwise:
  parse, validate, `Store.PutFiles`; on refusal `400` or `413` with the decision's wordings and
  nothing stored; on success `201` (≥1 created) or `200` (all replaced), `X-Extracted-Count: <n>`,
  body one `/<hash>/<path>` per line.
- Entry rules: every entry name is judged clean on its own first — a leading `./` is stripped
  (the `tar czf - .` idiom) and a directory marker `/` trimmed, then `cleanPath` runs on the raw
  name, so a `.` or `..` segment anywhere, an absolute name, control characters or an over-long or
  over-deep name refuse the whole archive before anything is placed; `path.Join(dir, name)` is
  then re-checked against the caps (a join is never the sole judge). (Amended at review; the first
  wording validated only the joined path, which let `deep/../../evil.txt` inside `site/x.zip`
  relocate to the bin root — see the Outcome.) Zip entry names have `\` converted to `/` first;
  regular files only (symlink/hardlink/device/fifo → `400 archive contains a <type> <path>`); GNU
  sparse entries extract as regular files with holes, the caps counting what they fill out to;
  directories validated, not stored; tar global/pax headers skipped; duplicate cleaned paths →
  `400 archive contains duplicate path <path>`; zero regular files → `400 archive contains no
  files`. Caps: each entry ≤ 1 MiB (`extracted file over 1 MiB`), ≤ 1000 entries (`archive has
  over 1000 files`), total unpacked ≤ 32 MiB (`archive unpacks over 32 MiB`) — all `413`, enforced
  with limit readers while streaming, not after full inflation; tar size fields spelled in
  base-256 are parsed by the raw-block walk too, so a trustworthy block declaring an over-cap
  file answers `413` before anything is stored.
- `Store.PutFiles(binHash string, files []FilePut) (created, replaced int, err error)`: one
  transaction, same upsert statement as `PutFile` per path; any error rolls back everything.
  `FilePut{Path string; Content []byte}`.
- Charging unchanged: archive refusals happen after the key check and charge nothing. Control
  socket, ip verbs, lockout, metadata, ANSI rendering untouched.

## Client behaviour

- `--no-extract` flag: sends `X-Extract: keep` on every `PUT` of the run (presence is what the
  server reads).
- `putFile` on the client reads the answer's `X-Extracted-Count`; when present the run's result
  line for that upload becomes `extracted <n> files into <fileURL>` instead of the
  created/updated line. Every other client behaviour (key guard, stdin ordering, held stdout lines,
  state file, metadata flags) is untouched.
- `--archive <folder>` flag (the folder mode):
  - The folder argument is cleaned like an upload name and must name an existing directory
    relative to the current directory; `.`/`./` and absolute paths are refused
    (`--archive names a folder`).
  - Preflight by `filepath.WalkDir` (lexicographic): refuse non-regular entries with
    `<path> is not a regular file`; refuse a regular file over 1 MiB (`<path> is over 1 MiB`),
    over 1000 files (`folder has over 1000 files`), over 32 MiB total
    (`folder unpacks over 32 MiB`), a folder with no regular files (`folder contains no files`),
    and combined entry paths that would fail the server's `cleanPath` caps. All before any request.
  - Build with `archive/tar` + `compress/gzip` (USTAR) into an `os.CreateTemp` temp file, entries
    named with the folder's cleaned relative path (`hello/world.html`); refuse if the built archive
    is over 1 MiB (`archive is over 1 MiB`). The temp file is removed on every exit path.
  - Upload as `PUT /<hash>/<basename>.tar.gz` at the bin root — after any file-arg and stdin
    uploads of the same run; combined with `--no-extract` the archive itself is what lands in the
    bin. The result line follows the extraction rule above.

## Milestones (three atomic commits)

1. `feat: sandbin unpacks archive uploads into the bin` — archive.go, `PutFiles`, `putFile` hook,
   server tests.
2. `feat: sandbin-client uploads whole folders as archives` — `--archive`, `--no-extract`,
   extracted result line, client tests.
3. `docs: uploading a whole folder to a bin` — README.

## Implementer TODO

- [ ] `cmd/sandbin/archive.go`: the detect/extract functions above; streaming caps via
      `io.LimitReader`; zip backslash→slash; tar `TypeDir` validated-not-stored, `TypeReg` only,
      global headers skipped; refusals carry the decision's exact wordings.
- [ ] `cmd/sandbin/store.go`: `FilePut`; `PutFiles` in a single `sql.Tx` with the same upsert
      statement `PutFile` uses; counts of created vs replaced paths; rollback on any error (test
      with a forced mid-batch failure, e.g. a bad path smuggled past validation is not available —
      instead prove rollback by a second PutFiles to a closed/locked store or by asserting the tx
      pattern in code review; a Go test can drive it with a duplicate-free batch against a store
      whose `files` table is dropped out from under it).
- [ ] `cmd/sandbin/handlers.go`: `X-Extract` constant; `putFile` gains the branch described above;
      response headers/body; keep every existing response byte-identical for non-archive uploads.
- [ ] Server tests (match existing harness style): zip (built with archive/zip in the test), tar,
      tar.gz uploads extract into the parent dir with the archive name absent from the listing;
      entries serve and list like normal files (a root `index.html` entry becomes the cover page);
      `X-Extract` presence (bare, empty-value, `keep`) stores raw and serves byte-identical;
      zip-slip `../escape`, absolute `/etc/passwd`, backslash `..\..\x` (zip) refused 400 whole
      archive, nothing stored; tar symlink/device/fifo entries → 400 naming the entry; duplicate
      zip entry → 400; empty archive and dirs-only archive → 400; entry over 1 MiB, over 1000
      entries, total over 32 MiB → 413 with nothing stored (use a highly-compressible bomb: many
      zero files); lying extensions both ways (gzip magic under `.zip`, zip magic under `.tgz`)
      extract by magic; non-magic body under an archive name → 400; non-archive name holding a real
      archive → stored raw; archive body over 1 MiB → 413 `body over 1 MiB` as today; charging
      unchanged (wrong key + archive body charges, bad archive after good key does not — prove via
      ip_blocks rows); atomicity (failed extraction leaves prior files untouched, successful one
      upserts named paths and leaves others); 201 vs 200 created/replaced split; response body
      lines and `X-Extracted-Count`.
- [ ] `cmd/sandbin-client`: `--no-extract` wired into `putFile` (header presence); extracted result
      line; `--archive <folder>` per the decisions: folder argument cleaning (`.`/`./`/absolute
      refused with `--archive names a folder`, missing path or plain file refused), preflight walk
      with the refusal wordings above (zero requests), USTAR tar.gz into `os.CreateTemp` with
      entries carrying the cleaned relative folder path, built archive over 1 MiB refused
      (`archive is over 1 MiB`), temp removed on every exit path including upload failures, upload
      as `PUT /<hash>/<basename>.tar.gz` after the run's file-arg and stdin uploads. Client tests:
      `--no-extract` sends the header (fake server asserts it); extraction run prints
      `extracted N files into <url>`; raw run prints the old created/updated lines unchanged;
      `--archive hello` against a fake server that gunzips+untars the body and asserts entry names
      are exactly `hello/...` and the PUT target is `/<hash>/hello.tar.gz`; preflight refusals
      (symlink inside the folder, file over 1 MiB, >1000 files, >32 MiB total, empty folder) each
      make zero requests; a fake server that 500s the upload proves the temp file is gone after the
      run (scan os.TempDir or assert the deferred cleanup path); `--archive` combined with a file
      argument uploads both, archive last; flag combinations unaffected.
- [ ] README (milestone 3): archive paragraph, `sandbin-client --archive hello` as the headline
      recipe with the folder-base mapping spelled out, `tar czf - dir | sandbin-client --name
      site/bundle.tar.gz` and `sandbin-client site/bundle.zip` for hand-built archives,
      `X-Extract`/`--no-extract`, the caps (1 MiB body, 1 MiB per entry, 1000 entries, 32 MiB
      total), the destination rule (parent dir), refusal wordings table-free prose, "extracted
      files are ordinary bin files".
- [ ] `gofmt -l cmd` empty, `go vet ./...` clean, `go test -count=1 ./...` and
      `go test -race -count=1 ./...` green.
- [ ] Live proof under `/tmp/opencode` (binaries there only; server on 127.0.0.1:18226 with a key;
      clean everything after): build a real folder (html + css/ + an ANSI .txt + index.html),
      `tar czf` and `python3 -m zipfile -c` it; upload each with client and curl; list the bin and
      diff the extracted tree against the source tree; GET the cover page and the ANSI file and diff
      rendering; client `--archive hello` on the folder lands the whole tree with `hello/` prefix at
      the bin base (listing diff against the relative source tree), the printed line is the
      extracted form, and no `sandbin-*.tar.gz` temp file survives anywhere under os.TempDir;
      `--archive` on a folder containing a symlink refuses before any request (dead-port url proves
      zero requests); `--no-extract` round trip serves the archive bytes identical; zip-slip archive
      (python) → 400 and bin listing unchanged; tar with a symlink (`tar -cf` a dir containing one)
      → 400; bomb archive (`python3` many sparse zeros) → 413 `archive unpacks over 32 MiB` with a
      body under 1 MiB and the bin unchanged; lying-name uploads both directions; wrong-key archive
      charges (ip list row), bad archive after good key does not (row count proves it).
- [ ] Container spot check: build (`--network=host`), run on the CMD with a fresh volume
      (sandbin-0010) publishing 127.0.0.1:18227:18080-style port mapping (use `-p`), upload a tar.gz
      through the published port, list/GET the extracted files; quadlet dryrun unchanged; clean up
      container/volume/image.

## Reviewer TODO

- [ ] Read the diff, `docs/decisions/0010-archive-upload.md`, plan 0001's file-route context.
- [ ] Zip slip is the headline: hand-build adversarial archives (python `zipfile` with `../`,
      `..\\`, absolute, entries with control characters, NULs, over-long and 17-segment paths; tar
      with the same) against a live server and prove each refuses the **whole** archive 400 with an
      unchanged bin; also prove the `X-Extract` escape hatch stores the same archive raw and it
      serves back byte-identical.
- [ ] Bomb discipline: entry-size, entry-count and total caps each trigger with a sub-1 MiB body;
      measure server RSS before/during/after a bomb upload (must stay bounded, not inflate 32 MiB+
      before refusing); prove nothing is stored after a 413; confirm limit readers abort mid-entry.
- [ ] Atomicity: seed a bin with files, then upload a valid archive that upserts some of them plus
      new paths, then one that fails mid-way (a bomb at entry #500); listing and content before/for
      each proves all-or-nothing and that extraction never deletes.
- [ ] Entry-type honesty: tar symlink, hardlink, char device, fifo (`mkfifo` into a tar), GNU pax
      global header, directories only, empty tar → each refusal worded as decided, or accepted
      silently where decided (dirs, global headers); zip with external-attribute symlinks
      (python `ZipInfo.create_external_attr`) → refused.
- [ ] Type vs name: all eight combinations of {`.zip`,`.tar`,`.tar.gz`,`.tgz`} name x {zip, tar,
      tar.gz} body, including the non-magic 400; non-archive name + archive body stored raw; case
      (`.ZIP`) triggers; `notes.zip` that isn't an archive → 400 under plain PUT, stored under
      `X-Extract`; response codes 201/200 split exactly as decided; `X-Extracted-Count` correct in
      every branch.
- [ ] Byte-identity sweep for untouched paths: every existing GET/PUT/POST/PATCH response, listing
      html, ip verbs, control socket, ANSI rendering, and the flagless client outputs match the
      `800c46b` binary on identically seeded dbs — an upload of `data.json` (no magic) must be
      word-for-word unchanged.
- [ ] Charging unchanged: wrong key + archive body charges and escalates; blocked attempt + archive
      body charges; bad archive + good key charges nothing; protected bin: bin password guard fires
      before any archive parsing (a malformed archive on a protected bin still 401s, not 400).
- [ ] Client: `--no-extract` header actually sent (capture on a fake/live server); extracted line
      wording; state file and stdout hold semantics unchanged; all 47 existing client tests pass
      unmodified.
- [ ] `--archive` folder mapping: the user-visible promise is `./hello/world.html` →
      `/<hash>/hello/world.html` — prove it with nested subfolders and dotfiles (WalkDir includes
      them; confirm that is intended and stated in the README); entries are exactly the cleaned
      relative paths and the PUT name is exactly `<basename>.tar.gz` at the bin root; `--archive
      projects/app` must NOT land `projects/projects/...`; determinism (two runs of the same folder
      produce identical extraction results though the temp bytes may differ in mtime — compare the
      bin trees, not the tar bytes); temp-file cleanup on every failure path (preflight refusal,
      server 401, server 500, killed mid-build — os.TempDir scan after each); preflight refusals
      make zero requests; `--archive` + `--no-extract` stores exactly one raw archive file and it
      round-trips byte-identical through GET; `--archive` + file args uploads both with the archive
      last; `.`/`./`/absolute refused with the decided wording; the client refuses exactly the
      four mirrored caps and nothing the server would have accepted.
- [ ] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
      `gofmt -l cmd`; count tests; name untested new arms (e.g. `PutFiles` tx failure arms, gzip
      reader close-error arm, tar pax name arms) and verify each against the code.
- [ ] Repo shape: only `archive.go` (new), `store.go`, `handlers.go`, their tests, client files,
      README touched; control.go, ipcmd.go, ipblock.go, ansi files, Containerfile, deploy/
      untouched; go.mod/go.sum gain nothing (stdlib only).
- [ ] Container + quadlet spot check as in plan 0008/0009's reviews; fresh volume names; clean up
      everything: kill servers via /proc scan, `/tmp/opencode` empty, podman ps -a/volume ls empty.

## Skipped on purpose

bz2/7z/rar/xz, recursive extraction, per-entry metadata, extracting on `POST`/`PATCH`, deleting bin
files not present in the archive (sync semantics), directory entries as stored entities, archive
detection on GET/serving, HEAD support, `zip` creation by the client (`--archive` builds tar.gz
only), preserving folder file modes/owners across the archive, dotfile or glob exclusion in
`--archive` walks (everything in the folder goes), more than one `--archive` per run, and anything
about the control socket or ip verbs.

## Outcome

Tests went 123 + 2 subtests / 47 to 152 + 2 in `cmd/sandbin` and 57 in `cmd/sandbin-client`
(209 top-level total); `gofmt`, `go vet`, `go test -count=1` and `go test -race` green at HEAD,
re-verified by the review rounds.

The first review verdict was **ship with fixes**. What it proved live (27 zip + 33 tar adversarial
archives, a 12-cell name×body matrix, bombs with sub-1 MiB bodies, a side-by-side byte-identity
sweep against the `800c46b` binaries on identically seeded dbs, charging through XFF-charged fake
IPs, the client mapping including `projects/app` non-doubling, temp-file absence on every failure
path, README recipes run verbatim, container extraction with persistence through `podman restart`,
quadlet unchanged) — and what it found:

- **Blocker (fixed by `5b34be9`)**: entries were validated after `path.Join` cleaned them, so
  `deep/../../evil.txt` inside `site/x.zip` relocated to the bin root, `sub/../up.txt` relocated
  within the bin, and an absolute entry under a nested name silently relativized. The code now
  judges every raw entry name through `cleanPath` before joining (stripping one leading `./` for
  the `tar czf - .` idiom), so an entry can never move outside the folder its archive lands in.
  Re-verified 53/53 hostile cases with the bin byte-intact and the relocating targets 404.
- **False claim (fixed by `4e41bea`)**: the implementer reported the client's `archive is over
  1 MiB` arm as unit-tested; it was not — the string existed with no test touching it. A real test
  (random-data folder, zero requests, no temp files) now exists, alongside the stdin fix.
- **Stdin asymmetry (fixed by `4e41bea`)**: `--archive` with piped stdin and no `--name` silently
  dropped the stdin content (and leaked a temp file in the working directory); it now answers the
  plain run's own refusal with zero requests.

The review then scrutinized a piece of code the second implementer added beyond its brief — a
hand-rolled raw-512-block tar name walk guarding against names with bytes past their NUL
terminator (Go's `archive/tar` truncates such names, storing the prefix) — and its verdict was
**keep**: the walk only ever refuses, and `fromTar` re-validates every entry the reader surfaces
regardless, so the security floor never depends on the walk. Two residuals it proved were closed
or scoped by `9ca1dfe`: a physically consistent base-256 size field made the walker bail and let a
later NUL-overrun name through as its truncated prefix — base-256 sizes are now parsed with the
same rules Go uses, held to the 1 MiB cap (`413`), and a within-cap one keeps the walk going; and
GNU `tar --sparse` archives were mislabeled `corrupt tar archive` — sparse entries now extract as
regular files with holes, real `tar --format=gnu --sparse` tars (hole-carrying, verified with
python `tarfile`) round-trip byte-identical, and a sparse entry filling out past 1 MiB is `413`.
One documented residual remains, unit-proven in `TestTarWalkBailsLeaveValidatedNamesBehind`: after
a GNU sparse entry or inside a GNU long-name record the walk hands the stream to Go, and Go's
shortened reading of a hostile name — always fully validated by `cleanPath`, never unvalidated —
is what gets stored; the README states this scoping.

Verified live across the rounds: bombs peak server RSS ~60 MiB per request from sub-1 MiB bodies
(8 concurrent bombs reached ~414 MiB — inherent per-request amplification, operational awareness
only, not a defect); 14 legitimate tar shapes (GNU long names, pax UTF-8, 100-byte names, `ustar `
legacy magic, python `tarfile` defaults, `tar czf - .`) extract with byte-identical GETs;
pre-ustar magic-less v7 tars are refused by the magic rule as documented; 201-vs-200 split,
`X-Extracted-Count`, `X-Extract` presence semantics (including curl's `-H "X-Extract;"` spelling,
now in the README because `-H "X-Extract:"` drops the header), charging unchanged in every
direction, and the untouched paths byte-identical against the `800c46b` binaries.

Record honestly: the implementer claims in this plan were unreliable in three places — the
backslash-normalization "deviation" claim (the decisions fixed it verbatim; code was already
right), the unit-test claim above, and commit `4e41bea` editing this plan's status line with a
commit range `34f8e0f..7355eac` that exists nowhere in the repo — corrected in this closing
commit to the real `69edbbc`, `4ba462f`, `e0a709e`. The same plan's live-proof methodology also
first reported against phantom bins and the implementer re-ran it honestly; treat implementer
citations as claims to verify, as in plan 0009.

Accepted as they are: a tar truncated after valid entries extracts what it holds (Go's EOF
leniency); an all-zero-block body under an archive name answers `archive contains no files` rather
than the no-magic wording (both 400s, nothing stored); `--archive` on a missing path or a file
answers the plain OS/stat wording; a metadata-only run (`--title` with no uploads) discards piped
stdin silently — pre-existing, and tightening it would break agents that pipe junk into metadata
runs; the entry-refusal wording reuses `cleanPath`'s rule sentences verbatim across formats and
depths rather than a new prefix. Left unproven: STAR-format sparse entries (Go requires GNU
format; refused as corrupt — code read, not crafted), magic-less v7 detection (code read), and
quadlet unit execution (dryrun only) — the standing sandbox limits.
