## Goal

Add streaming `io.Writer` / `io.Reader` interfaces to the snapshot subsystem (originally Phase 16.24 / v0.79.0, currently maintained as of Phase 16.51's context-aware refactor). The current `*Snapshot` API materializes the full payload as a `[]byte` via `Snapshot.Bytes()` — for large worlds (100MB+ snapshots) this is a memory-bounded concern. Streaming I/O eliminates the materialization step and composes naturally with `gzip.NewWriter`, `net.Conn`, `*os.File`, encrypted writers, etc.

This is the third post-port-completion Go-idiomatic value-add, following Phase 16.51 (context cancellation) and Phase 16.52 (iter.Seq / range-over-func for queries).

**Format compatibility:** verified that the current binary format (`@snapshot.go` lines 240–300) already writes every section with its own length prefix via `bw.u32(count)` before the payload (components, entity index, tables, sparse, union, range, policies, ordered children, unit defs — 10 sections, each independent and length-prefixed). The 16-byte file header (magic `0xF1 0xEC 0x53 0x00` + version BE + worldID LE) has **no total-size field**, so a single streaming format works. No format v2 is needed — `WriteTo` and `Bytes()` produce byte-for-byte identical output.

### API additions

Streaming write:
- `(*Snapshot).WriteTo(w io.Writer) (n int64, err error)` — satisfies `io.WriterTo`
- `(*World).TakeSnapshotTo(w io.Writer) (n int64, err error)` — take + write without materializing `*Snapshot`
- `(*World).TakeSnapshotToContext(ctx context.Context, w io.Writer) (n int64, err error)` — ctx-aware; checks ctx between stages

Streaming read:
- `ReadSnapshotFrom(r io.Reader) (*Snapshot, error)`
- `(*World).RestoreSnapshotFrom(r io.Reader) error`
- `(*World).RestoreSnapshotFromContext(ctx context.Context, r io.Reader) error`

Existing API retained: `Bytes()`, `LoadSnapshot([]byte)`, `TakeSnapshot()`, `RestoreSnapshot(*Snapshot)`, plus the `*Context` variants from Phase 16.51, all unchanged. Internal: `Bytes()` becomes a thin wrapper around `WriteTo(&bytes.Buffer{})`.

### Implementation notes

- The internal `binWriter` (`@snapshot.go:164`) wraps `bytes.Buffer`; refactor to accept any `io.Writer` (or introduce a parallel `binStreamWriter`). Each `u8`/`u32`/`u64`/`id`/`raw` write becomes a direct call on the underlying writer with an accumulated `n int64` and a sticky `err` field — once `err != nil`, subsequent writes become no-ops and the error is returned by `WriteTo`.
- `binReader` (`@snapshot.go:181`) currently cursors over a `[]byte`; for streaming reads, wrap an `io.Reader` (likely with `bufio.Reader`) and update the same `u8`/`u32`/`u64`/`raw` helpers. Variable-length sections work because every section is already length-prefixed.
- Streaming `WriteTo` holds `w.mu.RLock` for the duration of the write — document that long writes block other writers; for very large worlds, recommend snapshotting to a buffer first and streaming from the buffer.
- `ReadSnapshotFrom` materializes a `*Snapshot.blob` in memory after streaming reads (because `RestoreSnapshot` still consumes a complete blob). A future phase could stream the restore path as well; out of scope here.
- Interface assertions in the package: `var _ io.WriterTo = (*Snapshot)(nil)`.

### Required tests (new file `@snapshot_stream_test.go`)

Streaming round-trip:
- `TestSnapshotStream_WriteRead_RoundTrip`
- `TestSnapshotStream_RoundTrip_LargeWorld` (10k entities × 5 components)
- `TestSnapshotStream_RoundTrip_AllTraits` (sparse, dontfragment, union, ordered children, prefabs, hierarchies)

Memory bounds:
- `TestSnapshotStream_MemoryBounded` — custom writer tracking peak in-flight bytes
- `BenchmarkSnapshot_BytesVsStream` — allocs/op comparison

gzip integration:
- `TestSnapshotStream_GzipWriter` — gzip.NewWriter round-trip
- `TestSnapshotStream_GzipRatio` — verify reasonable ratio (≥2x) on typed-component-heavy world

File integration:
- `TestSnapshotStream_File_RoundTrip` — os.File write + read

Network integration:
- `TestSnapshotStream_PipeConn` — net.Pipe between two ends

Context cancellation:
- `TestSnapshotStream_WriteContext_PreCanceled`
- `TestSnapshotStream_WriteContext_CanceledMidStream` (verify `n` indicates bytes written)
- `TestSnapshotStream_ReadContext_CanceledMidStream`

Error handling:
- `TestSnapshotStream_ShortWrite` (writer returns `io.ErrShortWrite`)
- `TestSnapshotStream_ShortRead` (reader returns EOF mid-stream)
- `TestSnapshotStream_CorruptHeader` (wrong magic bytes)
- `TestSnapshotStream_TruncatedStream`

Back-compat:
- `TestSnapshotStream_BytesEquivalence` — `Bytes()` and `WriteTo(&bytes.Buffer{})` byte-for-byte identical
- `TestSnapshotStream_LoadStreamFormat` — `LoadSnapshot` reads `WriteTo` output

Concurrent safety:
- `TestSnapshotStream_ReadDuringMutation` — `TakeSnapshotTo` under read lock during deferred mutation; verify snapshot captures pre-mutation state

### Documentation update matrix

- `@docs/Snapshots.md` — new "Streaming snapshots" section with API surface, format compatibility, memory-bounded usage, gzip/file/network examples
- `@snapshot_stream_example_test.go` — new runnable Example combining `os.File` + `gzip.NewWriter` + `TakeSnapshotTo`
- `@CHANGELOG.md` — v0.108.0 entry
- `@ROADMAP.md` — add Phase 16.53 to Shipped (currently ends at Phase 16.52)
- `@README.md` — feature row mention

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All existing snapshot tests pass unchanged
- Interface assertion `var _ io.WriterTo = (*Snapshot)(nil)` compiles
- Memory benchmark: `WriteTo` peak memory < 10% of `Bytes()` peak memory on a 100MB snapshot

### Non-goals

- Replacing the binary format — existing format retained
- Snapshot diffing / incremental snapshots — separate phase
- Multi-format support (CBOR, protobuf) — binary only
- Built-in compression — users wrap with `gzip.Writer`
- Built-in encryption — users wrap with encrypted writer
- Versioned snapshot migration — separate phase

### Target

- Version: **v0.108.0**
- Phase: **16.53**

## Constraints

- @snapshot.go — Phase 16.24 binary snapshot implementation, extended through Phase 16.51 for context cancellation. Now 1594 LOC. Defines `Snapshot{blob, worldID, Partial}`, the 16-byte file header (magic + version BE + worldID LE), and the 10-section payload pipeline: components → entity index → tables → empty-table user ents → sparse → union → entity range → policies → ordered children → unit defs. Each section is length-prefixed via `bw.u32(count)` before payload — streaming requires no format change. `binWriter` wraps `bytes.Buffer`; refactor to write directly to an `io.Writer` with accumulated `n` and sticky `err`. `binReader` cursors a `[]byte`; refactor for `io.Reader` (likely wrapping `bufio.Reader`). `TakeSnapshotContext` (line 65) and `RestoreSnapshotContext` (line 110) already check ctx between stages — the streaming variants follow the same pattern.
- @snapshot_test.go — existing snapshot tests (951 LOC); all must continue to pass unchanged. The new `@snapshot_stream_test.go` parallels existing structure.
- @world.go — `(*World).mu` is the read/write lock guarding state; streaming writes acquire `mu.RLock()` for the entire stream duration. The package-level `TakeSnapshot`/`RestoreSnapshot` functions live in `snapshot.go` (not `world.go`) — verify the convention applies for the new `TakeSnapshotTo` / `RestoreSnapshotFrom` (likely also in `snapshot.go` as methods on `*World`).
- @marshal.go — separate JSON marshal path; not affected by binary streaming. Mentioned only to confirm scope: this phase is binary-format-only.
- @docs/Snapshots.md — user-facing snapshot manual; needs the new "Streaming snapshots" section covering API, format compatibility, memory bounds, gzip/file/network examples.
- @CLAUDE.md — repository conventions (commit style, test/lint discipline, doc update matrix). New phases ship with `go vet ./...` + `golangci-lint run ./...` + `go test ./... -race -count=3` clean, coverage ≥ 95%, and a CHANGELOG/ROADMAP/README update.
- @CHANGELOG.md — current head is v0.107.0 (Phase 16.52); next entry is v0.108.0 / Phase 16.53.
- @ROADMAP.md — current Shipped list ends at Phase 16.52; this phase is added as the next post-port-completion entry (Phase 16.51 context cancellation set the precedent for Go-idiomatic value-adds layered onto the completed port).
- The `io.WriteSeeker` two-pass approach (write size sentinel, patch at end) is **not needed** — the existing format has per-section length prefixes and no total-size field, so a uniform streaming write works.
- The headline use case is `gzip.NewWriter(file)` for compressed on-disk snapshots; test this explicitly.
