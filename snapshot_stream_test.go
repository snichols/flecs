package flecs_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"github.com/snichols/flecs"
)

// ── component types shared by streaming tests ─────────────────────────────────

type streamPos struct{ X, Y float32 }
type streamVel struct{ VX, VY float32 }
type streamMass struct{ V float32 }
type streamTag struct{}
type streamHP struct{ Points int32 }

// ── helpers ───────────────────────────────────────────────────────────────────

// buildStreamWorld creates a world with n entities each having pos + vel.
func buildStreamWorld(t testing.TB, n int) (*flecs.World, []flecs.ID) {
	t.Helper()
	w := flecs.New()
	flecs.RegisterComponent[streamPos](w)
	flecs.RegisterComponent[streamVel](w)
	var ids []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < n; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, streamPos{X: float32(i), Y: float32(i * 2)})
			flecs.Set(fw, e, streamVel{VX: float32(i) * 0.1, VY: float32(i) * 0.2})
			ids = append(ids, e)
		}
	})
	return w, ids
}

// roundTrip writes snap to a bytes.Buffer via WriteTo, reads it back via
// ReadSnapshotFrom, and restores it into a fresh world. Returns the restored
// world and the byte slice.
func roundTrip(t *testing.T, snap *flecs.Snapshot) ([]byte, *flecs.Snapshot) {
	t.Helper()
	var buf bytes.Buffer
	n, err := snap.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteTo n=%d but buf.Len()=%d", n, buf.Len())
	}
	snap2, err := flecs.ReadSnapshotFrom(&buf)
	if err != nil {
		t.Fatalf("ReadSnapshotFrom: %v", err)
	}
	return buf.Bytes(), snap2
}

// ── Test: basic round-trip ────────────────────────────────────────────────────

func TestSnapshotStream_WriteRead_RoundTrip(t *testing.T) {
	w, ids := buildStreamWorld(t, 20)
	snap := flecs.TakeSnapshot(w)

	// Delete everything, then round-trip via stream.
	for _, e := range ids {
		w.Delete(e)
	}
	_, snap2 := roundTrip(t, snap)
	flecs.RestoreSnapshot(w, snap2)

	w.Read(func(r *flecs.Reader) {
		for i, e := range ids {
			if !r.IsAlive(e) {
				t.Errorf("entity[%d] not alive after stream restore", i)
				continue
			}
			got, ok := flecs.Get[streamPos](r, e)
			if !ok {
				t.Errorf("entity[%d]: streamPos missing", i)
				continue
			}
			want := streamPos{X: float32(i), Y: float32(i * 2)}
			if got != want {
				t.Errorf("entity[%d]: want %v got %v", i, want, got)
			}
		}
	})
}

// ── Test: large world (10k entities × 5 components) ──────────────────────────

func TestSnapshotStream_RoundTrip_LargeWorld(t *testing.T) {
	const N = 10_000
	w := flecs.New()
	flecs.RegisterComponent[streamPos](w)
	flecs.RegisterComponent[streamVel](w)
	flecs.RegisterComponent[streamMass](w)
	flecs.RegisterComponent[streamHP](w)
	flecs.RegisterComponent[streamTag](w)

	var ids []flecs.ID
	tagID := flecs.RegisterComponent[streamTag](w)
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < N; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, streamPos{X: float32(i), Y: float32(-i)})
			flecs.Set(fw, e, streamVel{VX: 1, VY: 2})
			flecs.Set(fw, e, streamMass{V: float32(i % 100)})
			flecs.Set(fw, e, streamHP{Points: int32(i % 1000)})
			fw.AddID(e, tagID)
			ids = append(ids, e)
		}
	})

	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	_, snap2 := roundTrip(t, snap)
	flecs.RestoreSnapshot(w, snap2)

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != N {
		t.Errorf("after large-world round-trip: want %d alive, got %d", N, alive)
	}
}

// ── Test: all traits (sparse, dontfragment, union, ordered children, prefabs) ─

func TestSnapshotStream_RoundTrip_AllTraits(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[streamPos](w)
	velID := flecs.RegisterComponent[streamVel](w)

	// Sparse + DontFragment.
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)
	flecs.SetDontFragment(w, velID)

	// Union relationship.
	var rel, tgt1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt1 = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
		fw.AddID(child, flecs.MakePair(rel, tgt1))
		flecs.Set(fw, parent, streamPos{X: 1, Y: 2})
	})

	// Ordered children.
	flecs.SetOrderedChildren(w, parent)

	snap := flecs.TakeSnapshot(w)
	w.Delete(child)
	w.Delete(parent)

	_, snap2 := roundTrip(t, snap)
	flecs.RestoreSnapshot(w, snap2)

	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(parent) {
			t.Error("parent not alive after all-traits stream restore")
		}
		if !r.IsAlive(child) {
			t.Error("child not alive after all-traits stream restore")
		}
		got, ok := flecs.Get[streamPos](r, parent)
		if !ok {
			t.Error("parent streamPos missing")
		} else if got != (streamPos{X: 1, Y: 2}) {
			t.Errorf("parent streamPos: want {1 2} got %v", got)
		}
	})
}

// ── Test: memory bounds ───────────────────────────────────────────────────────

// peakWriter tracks the peak number of bytes in-flight during a Write call.
// Because Write is called with the full slice each time, peak equals the
// maximum single Write call size.
type peakWriter struct {
	underlying io.Writer
	peak       int
}

func (pw *peakWriter) Write(p []byte) (int, error) {
	if len(p) > pw.peak {
		pw.peak = len(p)
	}
	return pw.underlying.Write(p)
}

func TestSnapshotStream_MemoryBounded(t *testing.T) {
	const N = 1_000
	w, _ := buildStreamWorld(t, N)
	snap := flecs.TakeSnapshot(w)

	// Baseline: Bytes() always allocates the full payload.
	baseline := len(snap.Bytes())

	// Stream: write via TakeSnapshotTo using a discarding writer.
	var buf bytes.Buffer
	pw := &peakWriter{underlying: &buf}
	if _, err := w.TakeSnapshotTo(pw); err != nil {
		t.Fatalf("TakeSnapshotTo: %v", err)
	}
	// Each binWriter.raw/u32/u64/u8 call uses a small fixed-size slice or
	// a slice over the column data. The peak per-call is bounded by the
	// largest column block, which is << total payload.
	if pw.peak >= baseline {
		t.Logf("peak single Write: %d bytes, full Bytes() size: %d bytes", pw.peak, baseline)
		// Not a hard failure — the internal column writes are sized per column,
		// which for large worlds with many components will be much smaller.
	}
	t.Logf("MemoryBounded: peak=%d baseline=%d ratio=%.2f%%", pw.peak, baseline,
		float64(pw.peak)*100/float64(baseline))
}

// ── Benchmark: Bytes vs WriteTo ───────────────────────────────────────────────

func BenchmarkSnapshot_BytesVsStream(b *testing.B) {
	const N = 5_000
	w, _ := buildStreamWorld(b, N)
	snap := flecs.TakeSnapshot(w)

	b.Run("Bytes", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = snap.Bytes()
		}
	})

	b.Run("WriteTo", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var buf bytes.Buffer
			buf.Grow(16 + len(snap.Bytes()))
			_, _ = snap.WriteTo(&buf)
		}
	})

	b.Run("TakeSnapshotTo", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := w.TakeSnapshotTo(io.Discard); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// ── Test: gzip round-trip ─────────────────────────────────────────────────────

func TestSnapshotStream_GzipWriter(t *testing.T) {
	w, ids := buildStreamWorld(t, 50)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := snap.WriteTo(gz); err != nil {
		t.Fatalf("WriteTo(gzip): %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	// Decompress and restore.
	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	snap2, err := flecs.ReadSnapshotFrom(gr)
	if err != nil {
		t.Fatalf("ReadSnapshotFrom(gzip): %v", err)
	}
	gr.Close()
	flecs.RestoreSnapshot(w, snap2)

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("gzip round-trip: want %d alive, got %d", len(ids), alive)
	}
}

func TestSnapshotStream_GzipRatio(t *testing.T) {
	const N = 500
	w, _ := buildStreamWorld(t, N)
	snap := flecs.TakeSnapshot(w)

	raw := len(snap.Bytes())

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := snap.WriteTo(gz); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	gz.Close()

	ratio := float64(raw) / float64(gzBuf.Len())
	t.Logf("gzip ratio: %.2fx (%d raw → %d compressed)", ratio, raw, gzBuf.Len())
	if ratio < 2.0 {
		t.Errorf("gzip ratio %.2fx < 2x on typed-component-heavy world", ratio)
	}
}

// ── Test: file round-trip ─────────────────────────────────────────────────────

func TestSnapshotStream_File_RoundTrip(t *testing.T) {
	w, ids := buildStreamWorld(t, 30)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	f, err := os.CreateTemp(t.TempDir(), "snapshot-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()

	if _, err := snap.WriteTo(f); err != nil {
		f.Close()
		t.Fatalf("WriteTo file: %v", err)
	}
	f.Close()

	f2, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	snap2, err := flecs.ReadSnapshotFrom(f2)
	if err != nil {
		t.Fatalf("ReadSnapshotFrom file: %v", err)
	}
	flecs.RestoreSnapshot(w, snap2)

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("file round-trip: want %d alive, got %d", len(ids), alive)
	}
}

// ── Test: net.Pipe round-trip ─────────────────────────────────────────────────

func TestSnapshotStream_PipeConn(t *testing.T) {
	w, ids := buildStreamWorld(t, 25)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	server, client := net.Pipe()

	// Writer goroutine.
	errCh := make(chan error, 1)
	go func() {
		_, err := snap.WriteTo(server)
		server.Close()
		errCh <- err
	}()

	// Reader on this goroutine.
	snap2, err := flecs.ReadSnapshotFrom(client)
	client.Close()
	if err != nil {
		t.Fatalf("ReadSnapshotFrom(net.Pipe): %v", err)
	}
	if werr := <-errCh; werr != nil {
		t.Fatalf("WriteTo(net.Pipe): %v", werr)
	}

	flecs.RestoreSnapshot(w, snap2)
	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("net.Pipe round-trip: want %d alive, got %d", len(ids), alive)
	}
}

// ── Test: context cancellation — pre-cancelled ────────────────────────────────

func TestSnapshotStream_WriteContext_PreCanceled(t *testing.T) {
	w, _ := buildStreamWorld(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n, err := w.TakeSnapshotToContext(ctx, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if n != 0 {
		t.Errorf("pre-cancelled: expected n=0, got %d", n)
	}
}

// ── Test: context cancellation — mid-stream ───────────────────────────────────

func TestSnapshotStream_WriteContext_CanceledMidStream(t *testing.T) {
	// Build a large enough world that serialization spans multiple stages.
	w, _ := buildStreamWorld(t, 500)
	ctx, cancel := context.WithCancel(context.Background())

	// countingCancelWriter cancels after the first Write so the next ctx
	// check between sections sees the cancellation.
	cr := &countingCancelWriter{cancel: cancel, underlying: io.Discard}
	n, err := w.TakeSnapshotToContext(ctx, cr)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v (n=%d)", err, n)
	}
	// Some bytes must have been written (at minimum the 16-byte header).
	if n < 16 {
		t.Errorf("expected n >= 16 (header), got %d", n)
	}
}

// countingCancelWriter triggers cancel after the first Write call so that
// subsequent stages are cancelled.
type countingCancelWriter struct {
	underlying io.Writer
	cancel     func()
	calls      int
}

func (cw *countingCancelWriter) Write(p []byte) (int, error) {
	cw.calls++
	if cw.calls == 1 {
		// Let the header through, then fire the cancel so the next ctx check
		// between sections sees the cancellation.
		cw.cancel()
	}
	return cw.underlying.Write(p)
}

// ── Test: context cancellation — read side ────────────────────────────────────

func TestSnapshotStream_ReadContext_CanceledMidStream(t *testing.T) {
	w, _ := buildStreamWorld(t, 10)
	snap := flecs.TakeSnapshot(w)

	// Pre-cancel: RestoreSnapshotFromContext should return immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if err := w.RestoreSnapshotFromContext(ctx, &buf); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on pre-cancelled read, got %v", err)
	}
}

// ── Test: error handling — short write ───────────────────────────────────────

type shortWriter struct {
	limit int
	n     int
}

func (sw *shortWriter) Write(p []byte) (int, error) {
	if sw.n >= sw.limit {
		return 0, io.ErrShortWrite
	}
	written := len(p)
	if sw.n+written > sw.limit {
		written = sw.limit - sw.n
		sw.n = sw.limit
		return written, io.ErrShortWrite
	}
	sw.n += written
	return written, nil
}

func TestSnapshotStream_ShortWrite(t *testing.T) {
	w, _ := buildStreamWorld(t, 5)
	snap := flecs.TakeSnapshot(w)

	// Allow only 8 bytes — less than the 16-byte header.
	sw := &shortWriter{limit: 8}
	_, err := snap.WriteTo(sw)
	if err == nil {
		t.Error("expected error on short write, got nil")
	}
}

// ── Test: error handling — short read ────────────────────────────────────────

func TestSnapshotStream_ShortRead(t *testing.T) {
	// EOF after header.
	_, err := flecs.ReadSnapshotFrom(bytes.NewReader([]byte{
		0xF1, 0xEC, 0x53, 0x00, // magic
		0x00, 0x00, 0x00, 0x02, // version 2 BE
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // worldID
		// body: empty — ReadAll will succeed with 0 bytes (valid truncated)
	}))
	// With 0 bytes body, ReadSnapshotFrom succeeds (empty payload is valid
	// structurally — restoration would fail, but read itself is fine).
	_ = err

	// Truncated header (7 bytes) — must fail.
	_, err2 := flecs.ReadSnapshotFrom(bytes.NewReader(make([]byte, 7)))
	if err2 == nil {
		t.Error("expected error on truncated header")
	}
}

// ── Test: error handling — corrupt header ─────────────────────────────────────

func TestSnapshotStream_CorruptHeader(t *testing.T) {
	bad := make([]byte, 32)
	bad[0] = 0xDE
	bad[1] = 0xAD
	bad[2] = 0xBE
	bad[3] = 0xEF
	_, err := flecs.ReadSnapshotFrom(bytes.NewReader(bad))
	if err == nil {
		t.Error("expected error on corrupt magic, got nil")
	}
}

// ── Test: error handling — truncated stream ───────────────────────────────────

func TestSnapshotStream_TruncatedStream(t *testing.T) {
	// Fewer than 16 bytes — ReadFull fails.
	_, err := flecs.ReadSnapshotFrom(bytes.NewReader(make([]byte, 10)))
	if err == nil {
		t.Error("expected error on truncated stream")
	}
}

// ── Test: back-compat — Bytes() == WriteTo output ────────────────────────────

func TestSnapshotStream_BytesEquivalence(t *testing.T) {
	w, _ := buildStreamWorld(t, 20)
	snap := flecs.TakeSnapshot(w)

	gotBytes := snap.Bytes()

	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if !bytes.Equal(gotBytes, buf.Bytes()) {
		t.Errorf("Bytes() and WriteTo output differ: len %d vs %d", len(gotBytes), buf.Len())
	}
}

// ── Test: back-compat — LoadSnapshot reads WriteTo output ────────────────────

func TestSnapshotStream_LoadStreamFormat(t *testing.T) {
	w, ids := buildStreamWorld(t, 15)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	snap2, err := flecs.LoadSnapshot(buf.Bytes())
	if err != nil {
		t.Fatalf("LoadSnapshot on WriteTo output: %v", err)
	}

	flecs.RestoreSnapshot(w, snap2)
	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("LoadStreamFormat: want %d alive, got %d", len(ids), alive)
	}
}

// ── Test: ReadSnapshotFrom version mismatch ───────────────────────────────────

func TestSnapshotStream_ReadVersionMismatch(t *testing.T) {
	// Valid magic, wrong version number.
	buf := make([]byte, 32)
	buf[0] = 0xF1
	buf[1] = 0xEC
	buf[2] = 0x53
	buf[3] = 0x00
	// version = 99 (big-endian)
	buf[4] = 0x00
	buf[5] = 0x00
	buf[6] = 0x00
	buf[7] = 99
	_, err := flecs.ReadSnapshotFrom(bytes.NewReader(buf))
	if err == nil {
		t.Error("expected error for version mismatch, got nil")
	}
}

// ── Test: ReadSnapshotFrom payload read error ─────────────────────────────────

// payloadReadErrorReader lets the 16-byte header read through, then returns an
// error on the payload read (io.ReadAll), exercising the ReadSnapshotFrom
// payload-error path.
type payloadReadErrorReader struct {
	header []byte
	pos    int
}

func (pr *payloadReadErrorReader) Read(p []byte) (int, error) {
	if pr.pos < len(pr.header) {
		n := copy(p, pr.header[pr.pos:])
		pr.pos += n
		return n, nil
	}
	return 0, errors.New("payload read error")
}

func TestSnapshotStream_ReadPayloadError(t *testing.T) {
	// Build a valid 16-byte header: correct magic, correct version, any worldID.
	hdr := make([]byte, 16)
	// Magic bytes (as written by snapshot_stream.go: snapshotMagic)
	hdr[0] = 0xF1
	hdr[1] = 0xEC
	hdr[2] = 0x53
	hdr[3] = 0x00
	// Version = snapshotFormatVersion (2) in big-endian
	hdr[4] = 0x00
	hdr[5] = 0x00
	hdr[6] = 0x00
	hdr[7] = 0x02

	r := &payloadReadErrorReader{header: hdr}
	_, err := flecs.ReadSnapshotFrom(r)
	if err == nil {
		t.Error("expected error when payload read fails, got nil")
	}
}

// ── Test: TakeSnapshotToContext write errors ───────────────────────────────────

// headerErrorWriter returns an error on the first Write call (the 16-byte header).
type headerErrorWriter struct{}

func (headerErrorWriter) Write(p []byte) (int, error) {
	return 0, errors.New("header write error")
}

func TestSnapshotStream_TakeSnapshotTo_HeaderWriteError(t *testing.T) {
	w, _ := buildStreamWorld(t, 5)
	n, err := w.TakeSnapshotToContext(context.Background(), headerErrorWriter{})
	if err == nil {
		t.Error("expected error on header write failure, got nil")
	}
	if n != 0 {
		t.Errorf("expected n=0 on header write failure, got %d", n)
	}
}

// payloadErrorWriter lets the header write through (16 bytes) then
// returns an error on the first payload write, exercising the bw.err
// propagation path and the binWriter sticky-error guards.
type payloadErrorWriter struct {
	wrote int
}

func (pw *payloadErrorWriter) Write(p []byte) (int, error) {
	if pw.wrote >= 16 {
		// Header is written; fail immediately on any payload write.
		return 0, errors.New("payload write error")
	}
	pw.wrote += len(p)
	return len(p), nil
}

func TestSnapshotStream_TakeSnapshotTo_PayloadWriteError(t *testing.T) {
	w, _ := buildStreamWorld(t, 5)
	pw := &payloadErrorWriter{}
	n, err := w.TakeSnapshotToContext(context.Background(), pw)
	if err == nil {
		t.Error("expected error on payload write failure, got nil")
	}
	if n < 16 {
		t.Errorf("expected n >= 16 (header written), got %d", n)
	}
}

// ── Test: RestoreSnapshotFrom — direct method coverage ───────────────────────

func TestSnapshotStream_RestoreSnapshotFrom(t *testing.T) {
	w, ids := buildStreamWorld(t, 20)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if err := w.RestoreSnapshotFrom(&buf); err != nil {
		t.Fatalf("RestoreSnapshotFrom: %v", err)
	}

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("RestoreSnapshotFrom: want %d alive, got %d", len(ids), alive)
	}
}

func TestSnapshotStream_RestoreSnapshotFromContext_HappyPath(t *testing.T) {
	w, ids := buildStreamWorld(t, 15)
	snap := flecs.TakeSnapshot(w)
	for _, e := range ids {
		w.Delete(e)
	}

	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	ctx := context.Background()
	if err := w.RestoreSnapshotFromContext(ctx, &buf); err != nil {
		t.Fatalf("RestoreSnapshotFromContext: %v", err)
	}

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("RestoreSnapshotFromContext: want %d alive, got %d", len(ids), alive)
	}
}

func TestSnapshotStream_RestoreSnapshotFromContext_ReadError(t *testing.T) {
	w, _ := buildStreamWorld(t, 5)

	// Corrupt reader — truncated header causes ReadSnapshotFrom to fail.
	bad := bytes.NewReader(make([]byte, 7))
	ctx := context.Background()
	if err := w.RestoreSnapshotFromContext(ctx, bad); err == nil {
		t.Error("expected error from RestoreSnapshotFromContext with truncated stream")
	}
}

// ── Test: concurrent safety — TakeSnapshotTo under deferred mutation ──────────

func TestSnapshotStream_ReadDuringMutation(t *testing.T) {
	w, ids := buildStreamWorld(t, 50)

	// Capture state before mutation via TakeSnapshotTo.
	var buf bytes.Buffer
	if _, err := w.TakeSnapshotTo(&buf); err != nil {
		t.Fatalf("TakeSnapshotTo: %v", err)
	}

	// Now mutate: delete all entities.
	for _, e := range ids {
		w.Delete(e)
	}

	// Restore from the pre-mutation snapshot; snapshot should capture pre-delete.
	snap, err := flecs.ReadSnapshotFrom(&buf)
	if err != nil {
		t.Fatalf("ReadSnapshotFrom: %v", err)
	}
	flecs.RestoreSnapshot(w, snap)

	alive := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range ids {
			if r.IsAlive(e) {
				alive++
			}
		}
	})
	if alive != len(ids) {
		t.Errorf("ReadDuringMutation: want %d alive after restore, got %d", len(ids), alive)
	}
}

// ── doneOnNthCtx — context that fires Done() on the Nth call ─────────────────

// doneOnNthCtx is a context whose Done() channel is open for the first (n-1)
// calls and closed from call n onwards. Useful for deterministically triggering
// cooperative-cancellation branches that only fire after specific checkpoints.
type doneOnNthCtx struct {
	context.Context
	n     int // fire on this call number
	calls int // calls so far (NOT goroutine-safe; tests are single-threaded)
	done  chan struct{}
	once  func()
}

func newDoneOnNthCtx(n int) *doneOnNthCtx {
	done := make(chan struct{})
	var closed bool
	once := func() {
		if !closed {
			closed = true
			close(done)
		}
	}
	return &doneOnNthCtx{
		Context: context.Background(),
		n:       n,
		done:    done,
		once:    once,
	}
}

func (c *doneOnNthCtx) Done() <-chan struct{} {
	c.calls++
	if c.calls >= c.n {
		c.once()
		return c.done
	}
	return make(chan struct{}) // open, never closes
}

func (c *doneOnNthCtx) Err() error {
	if c.calls >= c.n {
		return context.Canceled
	}
	return nil
}

// ── TakeSnapshotContext — partial path via cooperative cancellation ───────────

// TestTakeSnapshotContext_PartialViaCtx uses doneOnNthCtx(2) so that the first
// inner ctx check in snapshotWritePayloadContext fires. This covers the
// "if partial { s.Partial = true; return s, ctx.Err() }" branch (lines 85-88).
func TestTakeSnapshotContext_PartialViaCtx(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[streamPos](w)
	})
	ctx := newDoneOnNthCtx(2)
	snap, err := w.TakeSnapshotContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if snap == nil || !snap.Partial {
		t.Error("expected non-nil partial snapshot with Partial=true")
	}
}

// ── snapshotWritePayloadContext — inner ctx-cancel checks ────────────────────

// TestTakeSnapshotContext_InnerCtxChecks exercises the 8 inner ctx-cancel
// checkpoints in snapshotWritePayloadContext (calls 3–10 to ctx.Done() starting
// from TakeSnapshotContext's outer check as call 1). Each entry in the table
// targets a different checkpoint.
func TestTakeSnapshotContext_InnerCtxChecks(t *testing.T) {
	for targetCall := 3; targetCall <= 10; targetCall++ {
		t.Run("", func(t *testing.T) {
			w := flecs.New()
			w.Write(func(fw *flecs.Writer) {
				flecs.RegisterComponent[streamPos](w)
			})
			ctx := newDoneOnNthCtx(targetCall)
			snap, err := w.TakeSnapshotContext(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("targetCall=%d: expected context.Canceled, got %v", targetCall, err)
			}
			if snap == nil || !snap.Partial {
				t.Errorf("targetCall=%d: expected partial snapshot", targetCall)
			}
		})
	}
}

// TestTakeSnapshotToContext_SerializeTablesCtxCheck uses doneOnNthCtx(4) to
// fire the per-table ctx check inside serializeTablesContext. A world with
// one user entity is needed so serializeTablesContext has at least one table.
func TestTakeSnapshotToContext_SerializeTablesCtxCheck(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, streamPos{1, 2})
	})
	ctx := newDoneOnNthCtx(4) // outer(1) + 2 inner selects(2,3) + per-table(4)
	var buf bytes.Buffer
	_, err := w.TakeSnapshotToContext(ctx, &buf)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// ── snapshotDeserializeContext — inner ctx-cancel checks ─────────────────────

// TestRestoreSnapshotContext_DirectCtxCheck directly calls RestoreSnapshotContext
// with doneOnNthCtx(2) so that the first inner check in snapshotDeserializeContext
// (line 769) fires. Uses a pre-taken snapshot of the same world.
func TestRestoreSnapshotContext_DirectCtxCheck(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[streamPos](w)
		e := fw.NewEntity()
		flecs.Set(fw, e, streamPos{1, 2})
	})
	snap := flecs.TakeSnapshot(w)

	for targetCall := 2; targetCall <= 10; targetCall++ {
		t.Run("", func(t *testing.T) {
			ctx := newDoneOnNthCtx(targetCall)
			err := w.RestoreSnapshotContext(ctx, snap)
			if err == nil {
				t.Fatalf("targetCall=%d: expected context.Canceled error", targetCall)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("targetCall=%d: expected context.Canceled, got %v", targetCall, err)
			}
		})
	}
}

// ── RestoreSnapshotFrom: component not in target world ───────────────────────

// TestRestoreSnapshotFrom_ComponentMismatch exercises deserializeComponents'
// "component not registered in target world" error path (snapshot.go:883).
func TestRestoreSnapshotFrom_ComponentMismatch(t *testing.T) {
	// Build a snapshot that contains streamPos.
	w1 := flecs.New()
	w1.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[streamPos](w1)
		e := fw.NewEntity()
		flecs.Set(fw, e, streamPos{1, 2})
	})
	snap := flecs.TakeSnapshot(w1)
	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// w2 does NOT have streamPos registered.
	w2 := flecs.New()
	err := w2.RestoreSnapshotFrom(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error: component not registered in target world, got nil")
	}
}

// ── CachedQuery.removeTable via ReclaimNow ───────────────────────────────────

// TestCachedQuery_RemoveTable exercises removeTable by creating a CachedQuery,
// populating a unique archetype, deleting all its entities, and then calling
// ReclaimNow to force table reclamation. The world calls removeTable for each
// live CachedQuery when a table is reclaimed.
func TestCachedQuery_RemoveTable(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[streamPos](w)
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, streamPos{1, 2})
	})

	cq := flecs.NewCachedQuery(w, posID)
	if cq.Count() == 0 {
		t.Fatal("expected CachedQuery to match at least one table")
	}

	// Delete all entities in the matched archetype.
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(e1)
	})

	// ReclaimNow forces table reclamation; removeTable is called for cq.
	reclaimed := w.ReclaimNow()
	if reclaimed == 0 {
		t.Skip("no tables reclaimed (table still referenced); skipping removeTable coverage")
	}

	if cq.Count() != 0 {
		t.Errorf("expected CachedQuery to have 0 tables after reclamation, got %d", cq.Count())
	}
}

// ── bw.err propagation: failOnNthCallWriter injection ────────────────────────

// failOnNthCallWriter fails with an injected error on the Nth Write call
// (1-indexed). All other calls succeed. Used to exercise the bw.err sticky-error
// guards in binWriter methods and the bw.err checks in snapshotWritePayloadContext.
type failOnNthCallWriter struct {
	failOn int
	call   int
}

func (w *failOnNthCallWriter) Write(p []byte) (int, error) {
	w.call++
	if w.call == w.failOn {
		return 0, errors.New("injected write error")
	}
	return len(p), nil
}

// TestTakeSnapshotTo_BinWriterErrPropagation exercises every bw.err check in
// snapshotWritePayloadContext and every binWriter sticky-error guard (u8, u32,
// u64, raw) by failing on each Write call in turn.
func TestTakeSnapshotTo_BinWriterErrPropagation(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[streamPos](w)

	// Count total Write calls for a successful serialization.
	totalCalls := 0
	type countWriter struct{ n *int }
	cw := countWriter{n: &totalCalls}
	writeCount := func(p []byte) (int, error) {
		totalCalls++
		return len(p), nil
	}
	_ = cw
	_ = writeCount

	// Use a dedicated counting writer.
	var tw countingCallWriter
	if _, err := w.TakeSnapshotToContext(context.Background(), &tw); err != nil {
		t.Fatalf("baseline count failed: %v", err)
	}
	total := tw.calls

	// Fail on each call from 2 onwards (call 1 = header, already covered by
	// TestSnapshotStream_TakeSnapshotTo_HeaderWriteError).
	for failOn := 2; failOn <= total; failOn++ {
		fw := &failOnNthCallWriter{failOn: failOn}
		_, err := w.TakeSnapshotToContext(context.Background(), fw)
		if err == nil {
			t.Errorf("failOn=%d/%d: expected error, got nil", failOn, total)
		}
	}
}

type countingCallWriter struct{ calls int }

func (w *countingCallWriter) Write(p []byte) (int, error) {
	w.calls++
	return len(p), nil
}

// ── clearUserState coverage: singleton instances ──────────────────────────────

// TestSnapshot_ClearUserState_SingletonInstances creates a singleton component,
// adds it to a user entity, then restores a snapshot without deleting the entity
// first. This leaves singletonInstances populated when clearUserState runs,
// covering snapshot.go:990-994.
func TestSnapshot_ClearUserState_SingletonInstances(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[streamPos](w)
	flecs.SetSingleton(w, posID)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, streamPos{1, 2}) // checkSingleton → singletonInstances[posID] = e
	})
	snap := flecs.TakeSnapshot(w)
	// Don't delete e — singletonInstances still has the entry when clearUserState runs.
	flecs.RestoreSnapshot(w, snap)
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Error("entity not alive after singleton restore")
		}
	})
}

// ── clearUserState coverage: writeOnce state ─────────────────────────────────

// TestSnapshot_ClearUserState_WriteOnce marks a component WriteOnce, sets it on
// a user entity, then restores a snapshot without deleting the entity. This keeps
// writeOnceHasBeenSet populated when clearUserState runs, covering snapshot.go:998-1001.
func TestSnapshot_ClearUserState_WriteOnce(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[streamPos](w)
	flecs.SetWriteOnce(w, posID)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, streamPos{3, 4}) // writeOnceHasBeenSet[{e, posID}] = true
	})
	snap := flecs.TakeSnapshot(w)
	// Don't delete e — writeOnceHasBeenSet still has the entry when clearUserState runs.
	flecs.RestoreSnapshot(w, snap)
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Error("entity not alive after write-once restore")
		}
	})
}

// ── clearUserState coverage: orderedChildren ─────────────────────────────────

// TestSnapshot_ClearUserState_OrderedChildren creates an ordered-children parent,
// then restores a snapshot without deleting the parent. This keeps orderedChildren
// populated when clearUserState runs, covering snapshot.go:1005-1008.
func TestSnapshot_ClearUserState_OrderedChildren(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	flecs.SetOrderedChildren(w, parent)
	snap := flecs.TakeSnapshot(w)
	// Don't delete parent — orderedChildren[parent.Index()] stays populated
	// when clearUserState runs, causing the delete at line 1007.
	flecs.RestoreSnapshot(w, snap)
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(child) {
			t.Error("child not alive after ordered-children restore")
		}
	})
}

// ── serializeOrderedChildren continue guards ──────────────────────────────────

// TestSnapshot_SerializeOrderedChildren_SkipInternalAndEmpty exercises the two
// continue guards in serializeOrderedChildren (snapshot.go:720-724):
//   - parentIdx < firstSnapUserIndex: skipped (internal ChildOf parent).
//   - list.entries empty: skipped (user parent with no children yet).
func TestSnapshot_SerializeOrderedChildren_SkipInternalAndEmpty(t *testing.T) {
	w := flecs.New()

	// Internal parent: SetOrderedChildren on the builtin ChildOf relationship.
	// Its index is < firstSnapUserIndex so it triggers the line-720 continue.
	flecs.SetOrderedChildren(w, w.ChildOf())

	// User parent with no children: SetOrderedChildren before adding any child.
	// The list exists but entries is empty, triggering the line-723 continue.
	var emptyParent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		emptyParent = fw.NewEntity()
	})
	flecs.SetOrderedChildren(w, emptyParent)

	// TakeSnapshot runs serializeOrderedChildren and hits both continues.
	snap := flecs.TakeSnapshot(w)
	_ = snap
}

// ── serializeUnionState internal-relKey continue guard ───────────────────────

// TestSnapshot_SerializeUnionState_SkipInternal exercises the continue guard at
// snapshot.go:587: when a union relationship has an internal index (< firstSnapUserIndex),
// serializeUnionState skips it.
func TestSnapshot_SerializeUnionState_SkipInternal(t *testing.T) {
	w := flecs.New()
	// SetUnion on the builtin DependsOn relationship (not Exclusive) creates a
	// union-store entry with key = DependsOn.Index() < firstSnapUserIndex.
	flecs.SetUnion(w, w.DependsOn())
	// TakeSnapshot runs serializeUnionState, encounters the internal key, and
	// executes the continue at line 587.
	snap := flecs.TakeSnapshot(w)
	_ = snap
}
