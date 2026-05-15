package flecs

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"
)

// Interface assertion — *Snapshot satisfies io.WriterTo.
var _ io.WriterTo = (*Snapshot)(nil)

// WriteTo writes the snapshot to w as a self-contained byte stream in the same
// format as [(*Snapshot).Bytes]: 16-byte file header followed by the payload.
// It satisfies [io.WriterTo].
//
// WriteTo does not re-serialize the world; it streams the already-captured
// payload. For live streaming without materializing a *Snapshot first, use
// [(*World).TakeSnapshotTo].
func (s *Snapshot) WriteTo(w io.Writer) (n int64, err error) {
	var hdr [16]byte
	copy(hdr[:4], snapshotMagic[:])
	binary.BigEndian.PutUint32(hdr[4:8], snapshotFormatVersion)
	binary.LittleEndian.PutUint64(hdr[8:16], s.worldID)
	nn, err := w.Write(hdr[:])
	n = int64(nn)
	if err != nil {
		return
	}
	nn2, err := w.Write(s.blob)
	n += int64(nn2)
	return
}

// TakeSnapshotTo serializes w's current state directly to out without
// materializing an intermediate *Snapshot in memory. This is equivalent to
// TakeSnapshot followed by WriteTo, but avoids the extra allocation.
//
// TakeSnapshotTo holds w's read lock for the entire duration of the write.
// For very large worlds this can block writers for an extended period; in
// that case, consider TakeSnapshot (which captures state quickly) followed
// by (*Snapshot).WriteTo outside the lock.
//
// Panics if a Write block is currently in progress.
func (w *World) TakeSnapshotTo(out io.Writer) (n int64, err error) {
	return w.TakeSnapshotToContext(context.Background(), out)
}

// TakeSnapshotToContext serializes w's current state directly to out with
// cooperative context cancellation. It checks ctx between serialization
// stages; if cancelled, it returns the bytes written so far along with
// ctx.Err(). The stream written to out up to the point of cancellation is
// incomplete and must not be passed to ReadSnapshotFrom.
//
// TakeSnapshotToContext holds w's read lock for the entire write duration.
//
// Panics if a Write block is currently in progress.
func (w *World) TakeSnapshotToContext(ctx context.Context, out io.Writer) (n int64, err error) {
	if !w.mu.TryRLock() {
		panic("flecs: TakeSnapshotToContext: cannot take snapshot while a Write block is in progress")
	}
	defer w.mu.RUnlock()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	// Write 16-byte file header.
	worldID := uint64(uintptr(unsafe.Pointer(w)))
	var hdr [16]byte
	copy(hdr[:4], snapshotMagic[:])
	binary.BigEndian.PutUint32(hdr[4:8], snapshotFormatVersion)
	binary.LittleEndian.PutUint64(hdr[8:16], worldID)
	nn, werr := out.Write(hdr[:])
	n = int64(nn)
	if werr != nil {
		return n, werr
	}

	// Write payload sections directly.
	bw := &binWriter{w: out, n: n}
	partial := snapshotWritePayloadContext(ctx, bw, w)
	if bw.err != nil {
		return bw.n, bw.err
	}
	if partial {
		return bw.n, ctx.Err()
	}
	return bw.n, nil
}

// ReadSnapshotFrom reads a snapshot from r (as written by [(*Snapshot).WriteTo]
// or [(*World).TakeSnapshotTo]). The payload is materialized into memory;
// call [(*World).RestoreSnapshotFrom] to apply it to a world.
//
// Returns an error if the stream is truncated, has invalid magic, or reports
// an unsupported format version.
func ReadSnapshotFrom(r io.Reader) (*Snapshot, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("flecs: ReadSnapshotFrom: reading header: %w", err)
	}
	var magic [4]byte
	copy(magic[:], hdr[:4])
	if magic != snapshotMagic {
		return nil, fmt.Errorf("flecs: ReadSnapshotFrom: invalid magic bytes %x", hdr[:4])
	}
	ver := binary.BigEndian.Uint32(hdr[4:8])
	if ver != snapshotFormatVersion {
		return nil, fmt.Errorf("flecs: ReadSnapshotFrom: unsupported version %d (want %d)", ver, snapshotFormatVersion)
	}
	worldID := binary.LittleEndian.Uint64(hdr[8:16])
	blob, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("flecs: ReadSnapshotFrom: reading payload: %w", err)
	}
	return &Snapshot{blob: blob, worldID: worldID}, nil
}

// RestoreSnapshotFrom reads a snapshot from r and restores it into w. It is
// equivalent to ReadSnapshotFrom followed by RestoreSnapshot.
//
// Panics follow the same rules as [RestoreSnapshot].
func (w *World) RestoreSnapshotFrom(r io.Reader) error {
	return w.RestoreSnapshotFromContext(context.Background(), r)
}

// RestoreSnapshotFromContext reads a snapshot from r and restores it into w
// with cooperative context cancellation. It checks ctx before reading and
// between deserialization stages. If cancelled, the world may be left in a
// partially-restored state — callers that require atomicity should snapshot
// the world before calling this and re-restore on error.
//
// Panics follow the same rules as [RestoreSnapshot].
func (w *World) RestoreSnapshotFromContext(ctx context.Context, r io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	snap, err := ReadSnapshotFrom(r)
	if err != nil {
		return err
	}
	// Patch worldID so RestoreSnapshotContext's same-world check passes.
	snap.worldID = uint64(uintptr(unsafe.Pointer(w)))
	return w.RestoreSnapshotContext(ctx, snap)
}
