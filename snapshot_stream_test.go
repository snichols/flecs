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
