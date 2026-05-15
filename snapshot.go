package flecs

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"runtime"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/componentindex"
	"github.com/snichols/flecs/internal/storage/table"
)

// snapshotMagic is the 4-byte marker at the start of every persisted snapshot.
var snapshotMagic = [4]byte{0xF1, 0xEC, 0x53, 0x00}

// snapshotFormatVersion is the current binary-format version.
// v2: adds parentStoragePolicies (policy map 18) and per-table parent columns.
const snapshotFormatVersion = uint32(2)

// firstSnapUserIndex is the first raw entity index owned by user code.
// Built-in entities occupy indices 1–77 (47 non-unit + 25 unit entities + 5 table/delete events);
// user entities start at index 78.
const firstSnapUserIndex = uint32(78)

// Snapshot holds a binary in-memory snapshot of a World's state. The blob is
// opaque; use [Snapshot.Bytes] / [LoadSnapshot] for disk persistence. The
// binary format has no stability guarantee in v1 — version mismatches on
// LoadSnapshot return an error.
//
// Partial is true when the snapshot was produced by [(*World).TakeSnapshotContext]
// and the context was cancelled before the walk completed. A partial snapshot
// must not be passed to RestoreSnapshot or RestoreSnapshotContext; use it only
// to detect that serialization was interrupted.
type Snapshot struct {
	blob    []byte // serialized payload (everything after the 16-byte file header)
	worldID uint64 // world-identity token captured at take time
	Partial bool   // true if serialization was cancelled mid-walk
}

// ─── Public API ──────────────────────────────────────────────────────────────

// TakeSnapshot captures a full binary snapshot of w's current state.
//
// Panics if a Write block is currently in progress from another goroutine
// (detected via TryRLock).
func TakeSnapshot(w *World) *Snapshot {
	s, err := w.TakeSnapshotContext(context.Background())
	if err != nil {
		panic(fmt.Sprintf("flecs: TakeSnapshot: %v", err))
	}
	return s
}

// TakeSnapshotContext captures a binary snapshot of w's current state with
// cooperative context cancellation. It checks ctx between tables during the
// walk; if the context is cancelled, it returns a partial [Snapshot] (with
// Partial == true) along with ctx.Err(). A partial snapshot must not be
// restored; inspect Partial to detect interruption.
//
// Panics if a Write block is currently in progress.
func (w *World) TakeSnapshotContext(ctx context.Context) (*Snapshot, error) {
	if !w.mu.TryRLock() {
		panic("flecs: TakeSnapshotContext: cannot take snapshot while a Write block is in progress")
	}
	defer w.mu.RUnlock()
	select {
	case <-ctx.Done():
		return &Snapshot{worldID: uint64(uintptr(unsafe.Pointer(w))), Partial: true}, ctx.Err()
	default:
	}
	s := &Snapshot{worldID: uint64(uintptr(unsafe.Pointer(w)))}
	var buf bytes.Buffer
	bw := &binWriter{w: &buf}
	partial := snapshotWritePayloadContext(ctx, bw, w)
	if bw.err != nil {
		s.Partial = true
		return s, bw.err
	}
	s.blob = buf.Bytes()
	if partial {
		s.Partial = true
		return s, ctx.Err()
	}
	return s, nil
}

// RestoreSnapshot restores w to the state captured in s, fully replacing all
// current user-entity and component state with snapshot data.
//
// Panics if:
//   - s belongs to a different world (cross-world restore is not supported).
//   - A Read or Write block is currently in progress (concurrent-restore violation).
//
// Post-restore guarantees:
//   - Entity IDs and generations from the snapshot are preserved.
//   - Observers are NOT fired for restored entities.
//   - Archetype *Table pointers cached by user code are invalidated.
//   - Cached queries are cleared; systems retain their registrations.
func RestoreSnapshot(w *World, s *Snapshot) {
	if err := w.RestoreSnapshotContext(context.Background(), s); err != nil {
		panic(fmt.Sprintf("flecs: RestoreSnapshot: %v", err))
	}
}

// RestoreSnapshotContext restores w from s with cooperative context
// cancellation. It checks ctx between deserialization steps; if the context
// is cancelled mid-restore the world is left in a partial state — entity-index
// and component tables may be inconsistent. Callers that require atomicity
// should take a snapshot before calling this function and re-restore on error.
//
// Panics follow the same rules as [RestoreSnapshot].
func (w *World) RestoreSnapshotContext(ctx context.Context, s *Snapshot) error {
	if !w.mu.TryLock() {
		panic("flecs: RestoreSnapshotContext: cannot restore while a Read or Write block is in progress")
	}
	defer w.mu.Unlock()
	if s.worldID != uint64(uintptr(unsafe.Pointer(w))) {
		panic("flecs: RestoreSnapshotContext: snapshot belongs to a different world; cross-world restore is forbidden")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return snapshotDeserializeContext(ctx, w, s.blob)
}

// Bytes serializes s to a self-contained byte slice for disk storage.
// The first 16 bytes are the file header (magic + version BE + world-identity
// token LE); the remainder is the snapshot payload.
//
// For large snapshots, prefer [(*Snapshot).WriteTo] to avoid materializing a
// second copy of the payload.
func (s *Snapshot) Bytes() []byte {
	var buf bytes.Buffer
	buf.Grow(16 + len(s.blob))
	_, _ = s.WriteTo(&buf)
	return buf.Bytes()
}

// LoadSnapshot deserializes a [Snapshot] from b (as produced by
// [Snapshot.Bytes]).  Returns an error if b has invalid magic, an unsupported
// version, or is too short to contain a valid header.
func LoadSnapshot(b []byte) (*Snapshot, error) {
	if len(b) < 16 {
		return nil, fmt.Errorf("flecs: LoadSnapshot: buffer too short (%d bytes)", len(b))
	}
	var magic [4]byte
	copy(magic[:], b[:4])
	if magic != snapshotMagic {
		return nil, fmt.Errorf("flecs: LoadSnapshot: invalid magic bytes %x", b[:4])
	}
	ver := binary.BigEndian.Uint32(b[4:8])
	if ver != snapshotFormatVersion {
		return nil, fmt.Errorf("flecs: LoadSnapshot: unsupported version %d (want %d)", ver, snapshotFormatVersion)
	}
	worldID := binary.LittleEndian.Uint64(b[8:16])
	return &Snapshot{
		blob:    append([]byte(nil), b[16:]...),
		worldID: worldID,
	}, nil
}

// ─── Binary-format helpers ────────────────────────────────────────────────────

// binWriter wraps an io.Writer with convenience write methods (all LE).
// Writes accumulate in n; the first error is stored in err and subsequent
// writes become no-ops. This lets callers call many write methods and check
// bw.err once at the end.
type binWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (bw *binWriter) u8(v uint8) {
	if bw.err != nil {
		return
	}
	nn, err := bw.w.Write([]byte{v})
	bw.n += int64(nn)
	bw.err = err
}

func (bw *binWriter) u32(v uint32) {
	if bw.err != nil {
		return
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	nn, err := bw.w.Write(b[:])
	bw.n += int64(nn)
	bw.err = err
}

func (bw *binWriter) u64(v uint64) {
	if bw.err != nil {
		return
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	nn, err := bw.w.Write(b[:])
	bw.n += int64(nn)
	bw.err = err
}

func (bw *binWriter) id(v ID) { bw.u64(uint64(v)) }

func (bw *binWriter) raw(p []byte) {
	if bw.err != nil {
		return
	}
	nn, err := bw.w.Write(p)
	bw.n += int64(nn)
	bw.err = err
}

// binReader is a cursor over a []byte with convenience read methods (all LE).
type binReader struct {
	data []byte
	pos  int
}

func (br *binReader) remain() int { return len(br.data) - br.pos }

func (br *binReader) u8() (uint8, error) {
	if br.remain() < 1 {
		return 0, fmt.Errorf("snapshot: EOF reading uint8 at pos %d", br.pos)
	}
	v := br.data[br.pos]
	br.pos++
	return v, nil
}
func (br *binReader) u32() (uint32, error) {
	if br.remain() < 4 {
		return 0, fmt.Errorf("snapshot: EOF reading uint32 at pos %d", br.pos)
	}
	v := binary.LittleEndian.Uint32(br.data[br.pos:])
	br.pos += 4
	return v, nil
}
func (br *binReader) u64() (uint64, error) {
	if br.remain() < 8 {
		return 0, fmt.Errorf("snapshot: EOF reading uint64 at pos %d", br.pos)
	}
	v := binary.LittleEndian.Uint64(br.data[br.pos:])
	br.pos += 8
	return v, nil
}
func (br *binReader) id() (ID, error) {
	v, err := br.u64()
	return ID(v), err
}
func (br *binReader) raw(n int) ([]byte, error) {
	if br.remain() < n {
		return nil, fmt.Errorf("snapshot: EOF reading %d bytes at pos %d", n, br.pos)
	}
	out := br.data[br.pos : br.pos+n]
	br.pos += n
	return out, nil
}

// ─── Serialization ────────────────────────────────────────────────────────────

// Binary layout (all integers little-endian):
//
//  1. Component registry
//  2. Entity-index  (alive + recycle + maxID)
//  3. Tables        (non-empty user archetypes)
//  4. Empty-table user entities
//  5. Sparse/DontFragment storage
//  6. Union state
//  7. Entity range
//  8. Policies      (20 maps in fixed order)
//  9. Ordered children
// 10. Unit defs     (user-registered unit definitions)

// snapshotWritePayloadContext writes the 10-section snapshot payload to bw,
// checking ctx between each section. Returns true if cancelled mid-write.
// bw.err captures any underlying write error.
func snapshotWritePayloadContext(ctx context.Context, bw *binWriter, w *World) (partial bool) {
	serializeComponents(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeEntityIndex(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	if p := serializeTablesContext(ctx, bw, w); p {
		return true
	}
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeEmptyTableUserEnts(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeSparseData(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeUnionState(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeEntityRange(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializePolicies(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeOrderedChildren(bw, w)
	if bw.err != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
	}
	serializeUnitDefs(bw, w)
	return false
}

// ─── serializeComponents ──────────────────────────────────────────────────────

func serializeComponents(bw *binWriter, w *World) {
	allIDs := w.registry.IDs()
	var comps []struct {
		id   ID
		name string
	}
	for _, cid := range allIDs {
		info, ok := w.registry.LookupByID(cid)
		if !ok || info.Name == "" || info.Name == "tag" {
			continue
		}
		comps = append(comps, struct {
			id   ID
			name string
		}{cid, info.Name})
	}
	bw.u32(uint32(len(comps)))
	for _, c := range comps {
		bw.id(c.id)
		b := []byte(c.name)
		bw.u32(uint32(len(b)))
		bw.raw(b)
	}
}

// ─── serializeEntityIndex ────────────────────────────────────────────────────

func serializeEntityIndex(bw *binWriter, w *World) {
	all := w.index.DenseAlive()
	var alive []ID
	for _, id := range all {
		if id.Index() >= firstSnapUserIndex {
			alive = append(alive, id)
		}
	}
	bw.u32(uint32(len(alive)))
	for _, id := range alive {
		bw.id(id)
	}

	rec := w.index.Recycle()
	var userRec []ID
	for _, id := range rec {
		if id.Index() >= firstSnapUserIndex {
			userRec = append(userRec, id)
		}
	}
	bw.u32(uint32(len(userRec)))
	for _, id := range userRec {
		bw.id(id)
	}

	bw.u32(w.index.MaxID())
}

// ─── serializeTables ─────────────────────────────────────────────────────────

// serializeTablesContext serializes tables, checking ctx between each table.
// Returns true if serialization was cancelled.
func serializeTablesContext(ctx context.Context, bw *binWriter, w *World) (partial bool) {
	var userTables []*table.Table
	for _, t := range w.tables {
		if len(t.Type()) == 0 {
			continue
		}
		userTables = append(userTables, t)
	}
	bw.u32(uint32(len(userTables)))
	for _, t := range userTables {
		serializeTable(bw, w, t)
		select {
		case <-ctx.Done():
			return true
		default:
		}
	}
	return false
}

func serializeTable(bw *binWriter, w *World, t *table.Table) {
	sig := t.Type()
	bw.u32(uint32(len(sig)))
	for _, id := range sig {
		bw.id(id)
	}

	ents := t.Entities()
	n := len(ents)
	bw.u32(uint32(n))
	for _, e := range ents {
		bw.id(e)
	}

	// Column data: one entry per sig component (in order).
	for _, compID := range sig {
		info, ok := w.registry.LookupByID(compID)
		if !ok || info.Size == 0 {
			bw.u32(0)
			continue
		}
		bw.u32(uint32(info.Size))
		if n > 0 {
			base, _, count := t.ColumnBasePtr(compID)
			if base != nil && count > 0 {
				bw.raw(unsafe.Slice((*byte)(base), uintptr(count)*info.Size))
				runtime.KeepAlive(t)
			}
		}
	}

	// Bitsets.
	bs := t.GetBitsetsCopy()
	bw.u32(uint32(len(bs)))
	for compID, words := range bs {
		bw.id(compID)
		bw.u32(uint32(len(words)))
		for _, w64 := range words {
			bw.u64(w64)
		}
	}

	// Parent columns (non-fragmenting parent storage).
	relIDs := t.ParentColRelIDs()
	bw.u32(uint32(len(relIDs)))
	for _, relKey := range relIDs {
		bw.id(relKey)
		col := t.GetParentCol(relKey)
		bw.u32(uint32(len(col)))
		for _, parent := range col {
			bw.id(parent)
		}
	}
}

// ─── serializeEmptyTableUserEnts ─────────────────────────────────────────────

func serializeEmptyTableUserEnts(bw *binWriter, w *World) {
	var userEnts []ID
	for _, e := range w.empty.Entities() {
		if e.Index() >= firstSnapUserIndex {
			userEnts = append(userEnts, e)
		}
	}
	bw.u32(uint32(len(userEnts)))
	for _, e := range userEnts {
		bw.id(e)
	}
}

// ─── serializeSparseData ─────────────────────────────────────────────────────

func serializeSparseData(bw *binWriter, w *World) {
	type ssInfo struct {
		key        ID
		size       uintptr
		isSparse   bool
		isDontFrag bool
	}
	var infos []ssInfo
	for key, ss := range w.sparseStorage {
		if ss == nil || ss.typeInfo == nil || ss.typeInfo.Size == 0 {
			continue
		}
		infos = append(infos, ssInfo{
			key:        key,
			size:       ss.typeInfo.Size,
			isSparse:   w.sparsePolicies[key],
			isDontFrag: w.dontFragmentPolicies[key],
		})
	}
	bw.u32(uint32(len(infos)))
	for _, si := range infos {
		bw.id(si.key)
		bw.u32(uint32(si.size))
		flags := uint8(0)
		if si.isSparse {
			flags |= 1
		}
		if si.isDontFrag {
			flags |= 2
		}
		bw.u8(flags)

		ss := w.sparseStorage[si.key]
		var entries []sparseEntry
		for _, entry := range ss.dense {
			if entry.entity.Index() >= firstSnapUserIndex {
				entries = append(entries, entry)
			}
		}
		bw.u32(uint32(len(entries)))
		for _, entry := range entries {
			bw.id(entry.entity)
			bw.raw(unsafe.Slice((*byte)(entry.data), si.size))
		}
	}
}

// ─── serializeUnionState ─────────────────────────────────────────────────────

func serializeUnionState(bw *binWriter, w *World) {
	if w.unionStore == nil {
		bw.u32(0)
		return
	}
	type rel struct {
		relID   ID
		entries []unionEntry
	}
	var rels []rel
	for relKey, store := range w.unionStore {
		if uint64(relKey) < uint64(firstSnapUserIndex) {
			continue
		}
		var entries []unionEntry
		for _, e := range store.dense {
			if e.entity.Index() >= firstSnapUserIndex {
				entries = append(entries, e)
			}
		}
		if len(entries) > 0 {
			rels = append(rels, rel{relKey, entries})
		}
	}
	bw.u32(uint32(len(rels)))
	for _, r := range rels {
		bw.id(r.relID)
		bw.u32(uint32(len(r.entries)))
		for _, e := range r.entries {
			bw.id(e.entity)
			bw.id(e.target)
		}
	}
}

// ─── serializeEntityRange ────────────────────────────────────────────────────

func serializeEntityRange(bw *binWriter, w *World) {
	min, max, set := w.index.GetRange()
	bw.u32(min)
	bw.u32(max)
	if set {
		bw.u8(1)
	} else {
		bw.u8(0)
	}
}

// ─── serializePolicies ───────────────────────────────────────────────────────

func writeBoolMap(bw *binWriter, m map[ID]bool) {
	var keys []ID
	for k, v := range m {
		if v && uint64(k) >= uint64(firstSnapUserIndex) {
			keys = append(keys, k)
		}
	}
	bw.u32(uint32(len(keys)))
	for _, k := range keys {
		bw.id(k)
	}
}

func serializePolicies(bw *binWriter, w *World) {
	writeBoolMap(bw, w.sparsePolicies)
	writeBoolMap(bw, w.dontFragmentPolicies)
	writeBoolMap(bw, w.unionPolicies)
	writeBoolMap(bw, w.exclusivePolicies)
	writeBoolMap(bw, w.symmetricPolicies)
	writeBoolMap(bw, w.transitivePolicies)
	writeBoolMap(bw, w.reflexivePolicies)
	writeBoolMap(bw, w.acyclicPolicies)
	writeBoolMap(bw, w.finalPolicies)
	writeBoolMap(bw, w.singletonPolicies)
	writeBoolMap(bw, w.writeOncePolicies)
	writeBoolMap(bw, w.traversablePolicies)
	writeBoolMap(bw, w.relationshipPolicies)
	writeBoolMap(bw, w.targetPolicies)
	writeBoolMap(bw, w.traitPolicies)
	writeBoolMap(bw, w.pairIsTagPolicies)
	writeBoolMap(bw, w.canTogglePolicies)
	writeBoolMap(bw, w.parentStoragePolicies)

	// cleanupPolicies (uint8 value).
	var ckv []struct {
		k ID
		v uint8
	}
	for k, v := range w.cleanupPolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			ckv = append(ckv, struct {
				k ID
				v uint8
			}{k, uint8(v)})
		}
	}
	bw.u32(uint32(len(ckv)))
	for _, e := range ckv {
		bw.id(e.k)
		bw.u8(e.v)
	}

	// instantiatePolicies (uint8 value).
	var ikv []struct {
		k ID
		v uint8
	}
	for k, v := range w.instantiatePolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			ikv = append(ikv, struct {
				k ID
				v uint8
			}{k, uint8(v)})
		}
	}
	bw.u32(uint32(len(ikv)))
	for _, e := range ikv {
		bw.id(e.k)
		bw.u8(e.v)
	}

	// oneOfPolicies (uint64 value).
	var okv []struct{ k, v ID }
	for k, v := range w.oneOfPolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			okv = append(okv, struct{ k, v ID }{k, v})
		}
	}
	bw.u32(uint32(len(okv)))
	for _, e := range okv {
		bw.id(e.k)
		bw.id(e.v)
	}
}

// ─── serializeOrderedChildren ────────────────────────────────────────────────

func serializeOrderedChildren(bw *binWriter, w *World) {
	type entry struct {
		parentFull ID
		children   []ID
	}
	var entries []entry
	for parentIdx, list := range w.orderedChildren {
		if uint64(parentIdx) < uint64(firstSnapUserIndex) {
			continue
		}
		if list == nil || len(list.entries) == 0 {
			continue
		}
		parentFull, ok := w.index.GetCurrentByIndex(uint32(parentIdx))
		if !ok {
			continue
		}
		entries = append(entries, entry{parentFull, list.entries})
	}
	bw.u32(uint32(len(entries)))
	for _, e := range entries {
		bw.id(e.parentFull)
		bw.u32(uint32(len(e.children)))
		for _, ch := range e.children {
			bw.id(ch)
		}
	}
}

// ─── Deserialization ─────────────────────────────────────────────────────────

// Restore order — entity-index is applied first so that subsequent table
// and empty-table steps can set Record.Table/Row immediately.
//
//  1. Validate component registry
//  2. Clear user state
//  3. Restore entity-index (RestoreUserState)
//  4. Restore tables
//  5. Restore empty-table user entities
//  6. Restore sparse/DontFragment data
//  7. Restore union state
//  8. Restore entity range
//  9. Restore policies
// 10. Restore ordered children
// 11. Restore unit defs

// snapshotDeserializeContext restores world state from blob, checking ctx
// between major deserialization steps. If ctx is cancelled mid-restore, the
// world is left in a partial state — callers that require atomicity should
// snapshot the world before calling this function and re-restore on error.
func snapshotDeserializeContext(ctx context.Context, w *World, blob []byte) error {
	br := &binReader{data: blob}
	if err := deserializeComponents(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	clearUserState(w)
	if err := deserializeEntityIndex(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeTables(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeEmptyTableUserEnts(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeSparseData(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeUnionState(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeEntityRange(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializePolicies(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeOrderedChildren(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeUnitDefs(br, w); err != nil {
		return err
	}
	w.inheritorCache = nil
	w.pipelineDirty = true
	w.cachedQueries = nil
	// Reset emptyTicks for all tables so the restored world reclaims correctly
	// on subsequent Progress calls (counts restart from zero after restore).
	for _, t := range w.tables {
		t.ResetEmptyTicks()
	}
	return nil
}

// ─── deserializeComponents ───────────────────────────────────────────────────

func deserializeComponents(br *binReader, w *World) error {
	// Build name→TypeInfo index from the current registry.
	allIDs := w.registry.IDs()
	nameToInfo := make(map[string]*component.TypeInfo, len(allIDs))
	for _, cid := range allIDs {
		info, ok := w.registry.LookupByID(cid)
		if !ok || info.Name == "" || info.Name == "tag" {
			continue
		}
		nameToInfo[info.Name] = info
	}

	count, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		if _, err := br.id(); err != nil { // snapshot component ID (ignored)
			return err
		}
		nameLen, err := br.u32()
		if err != nil {
			return err
		}
		nameBytes, err := br.raw(int(nameLen))
		if err != nil {
			return err
		}
		name := string(nameBytes)
		if _, ok := nameToInfo[name]; !ok {
			return fmt.Errorf("component %q from snapshot is not registered in the target world", name)
		}
	}
	return nil
}

// ─── clearUserState ──────────────────────────────────────────────────────────

func clearUserState(w *World) {
	// Remove all non-empty tables.
	for key, t := range w.tables {
		if len(t.Type()) == 0 {
			continue
		}
		delete(w.tables, key)
	}

	// Remove user entities from the empty table and correct remaining rows.
	w.empty.PruneUserEntities(firstSnapUserIndex)
	for row, e := range w.empty.Entities() {
		if rec := w.index.Get(e); rec != nil {
			rec.Row = uint32(row)
		}
	}

	// Rebuild compIndex.
	w.compIndex = componentindex.New()

	// Clear user entries from sparse storage.
	for _, ss := range w.sparseStorage {
		if ss == nil {
			continue
		}
		write := 0
		for _, entry := range ss.dense {
			if entry.entity.Index() < firstSnapUserIndex {
				ss.dense[write] = entry
				write++
			}
		}
		ss.dense = ss.dense[:write]
		clear(ss.index)
		for i, entry := range ss.dense {
			ss.index[entry.entity.Index()] = i
		}
	}

	// Clear sparseHeld for user entities.
	for rawIdx := range w.sparseHeld {
		if rawIdx >= firstSnapUserIndex {
			delete(w.sparseHeld, rawIdx)
		}
	}

	// Clear union store user entries.
	for _, store := range w.unionStore {
		write := 0
		for _, entry := range store.dense {
			if entry.entity.Index() < firstSnapUserIndex {
				store.dense[write] = entry
				write++
			}
		}
		store.dense = store.dense[:write]
		clear(store.index)
		for i, entry := range store.dense {
			store.index[ID(entry.entity.Index())] = i
		}
	}

	// Clear user policy entries.
	clearBoolPolicyUser(w.sparsePolicies)
	clearBoolPolicyUser(w.dontFragmentPolicies)
	clearBoolPolicyUser(w.unionPolicies)
	clearBoolPolicyUser(w.exclusivePolicies)
	clearBoolPolicyUser(w.symmetricPolicies)
	clearBoolPolicyUser(w.transitivePolicies)
	clearBoolPolicyUser(w.reflexivePolicies)
	clearBoolPolicyUser(w.acyclicPolicies)
	clearBoolPolicyUser(w.finalPolicies)
	clearBoolPolicyUser(w.singletonPolicies)
	clearBoolPolicyUser(w.writeOncePolicies)
	clearBoolPolicyUser(w.traversablePolicies)
	clearBoolPolicyUser(w.relationshipPolicies)
	clearBoolPolicyUser(w.targetPolicies)
	clearBoolPolicyUser(w.traitPolicies)
	clearBoolPolicyUser(w.pairIsTagPolicies)
	clearBoolPolicyUser(w.canTogglePolicies)
	clearBoolPolicyUser(w.parentStoragePolicies)
	for k := range w.cleanupPolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			delete(w.cleanupPolicies, k)
		}
	}
	for k := range w.instantiatePolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			delete(w.instantiatePolicies, k)
		}
	}
	for k := range w.oneOfPolicies {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			delete(w.oneOfPolicies, k)
		}
	}

	// Clear singleton instance tracking for user components.
	for compIdx, holder := range w.singletonInstances {
		if uint64(compIdx) >= uint64(firstSnapUserIndex) ||
			holder.Index() >= firstSnapUserIndex {
			delete(w.singletonInstances, compIdx)
		}
	}

	// Clear writeOnce state for user entities.
	for key := range w.writeOnceHasBeenSet {
		if key.entity >= firstSnapUserIndex {
			delete(w.writeOnceHasBeenSet, key)
		}
	}

	// Clear orderedChildren for user parents.
	for parentIdx := range w.orderedChildren {
		if uint64(parentIdx) >= uint64(firstSnapUserIndex) {
			delete(w.orderedChildren, parentIdx)
		}
	}

	// Drop inheritor cache (may reference dead tables).
	w.inheritorCache = nil
}

func clearBoolPolicyUser(m map[ID]bool) {
	for k := range m {
		if uint64(k) >= uint64(firstSnapUserIndex) {
			delete(m, k)
		}
	}
}

// ─── deserializeEntityIndex ──────────────────────────────────────────────────

func deserializeEntityIndex(br *binReader, w *World) error {
	aliveCount, err := br.u32()
	if err != nil {
		return err
	}
	alive := make([]ID, aliveCount)
	for i := range alive {
		v, err := br.id()
		if err != nil {
			return err
		}
		alive[i] = v
	}

	recycleCount, err := br.u32()
	if err != nil {
		return err
	}
	recycled := make([]ID, recycleCount)
	for i := range recycled {
		v, err := br.id()
		if err != nil {
			return err
		}
		recycled[i] = v
	}

	maxID, err := br.u32()
	if err != nil {
		return err
	}

	w.index.RestoreUserState(firstSnapUserIndex, alive, recycled, maxID)
	return nil
}

// ─── deserializeTables ───────────────────────────────────────────────────────

func deserializeTables(br *binReader, w *World) error {
	numTables, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < numTables; i++ {
		if err := deserializeTable(br, w); err != nil {
			return err
		}
	}
	return nil
}

func deserializeTable(br *binReader, w *World) error {
	sigLen, err := br.u32()
	if err != nil {
		return err
	}
	sig := make([]ID, sigLen)
	for i := range sig {
		v, err := br.id()
		if err != nil {
			return err
		}
		sig[i] = v
	}

	rowCount, err := br.u32()
	if err != nil {
		return err
	}
	entityIDs := make([]ID, rowCount)
	for i := range entityIDs {
		v, err := br.id()
		if err != nil {
			return err
		}
		entityIDs[i] = v
	}

	// Build TypeInfo slice.
	types := make([]*component.TypeInfo, sigLen)
	for i, compID := range sig {
		info, ok := w.registry.LookupByID(compID)
		if !ok {
			info = w.registry.EnsureID(compID)
		}
		types[i] = info
	}

	// Create and register the table.
	key := sigKey(sig)
	t, exists := w.tables[key]
	if !exists {
		t = table.New(sig, types)
		w.tables[key] = t
		for _, id := range sig {
			w.compIndex.Register(id, t)
		}
	}

	// Append entities (zero-fills columns).
	for _, e := range entityIDs {
		t.Append(e)
	}

	// Column data: read one block per sig component.
	for ci, compID := range sig {
		elemSize, err := br.u32()
		if err != nil {
			return err
		}
		if elemSize == 0 || rowCount == 0 {
			continue
		}
		totalBytes := int(elemSize) * int(rowCount)
		colData, err := br.raw(totalBytes)
		if err != nil {
			return err
		}
		// Validate size matches current registry.
		info := types[ci]
		if info.Size == 0 {
			continue // tag in current registry; skip
		}
		if uintptr(elemSize) != info.Size {
			return fmt.Errorf("snapshot: component %v size mismatch: snapshot %d bytes, registry %d bytes",
				compID, elemSize, info.Size)
		}
		base, _, n := t.ColumnBasePtr(compID)
		if base != nil && n > 0 {
			dst := unsafe.Slice((*byte)(base), uintptr(n)*uintptr(elemSize))
			copy(dst, colData)
			runtime.KeepAlive(t)
		}
	}

	// Bitsets.
	bitsetCount, err := br.u32()
	if err != nil {
		return err
	}
	if bitsetCount > 0 {
		bsMap := make(map[ID][]uint64, bitsetCount)
		for i := uint32(0); i < bitsetCount; i++ {
			compID, err := br.id()
			if err != nil {
				return err
			}
			wordCount, err := br.u32()
			if err != nil {
				return err
			}
			words := make([]uint64, wordCount)
			for j := range words {
				w64, err := br.u64()
				if err != nil {
					return err
				}
				words[j] = w64
			}
			bsMap[compID] = words
		}
		t.SetBitsets(bsMap)
	}

	// Parent columns.
	parentColCount, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < parentColCount; i++ {
		relKey, err := br.id()
		if err != nil {
			return err
		}
		colLen, err := br.u32()
		if err != nil {
			return err
		}
		t.EnsureParentCol(relKey)
		for j := uint32(0); j < colLen; j++ {
			parent, err := br.id()
			if err != nil {
				return err
			}
			if int(j) < int(rowCount) {
				t.SetParentEntry(int(j), relKey, parent)
			}
		}
	}

	// Set Record.Table and Record.Row for each entity.
	for row, e := range entityIDs {
		if rec := w.index.Get(e); rec != nil {
			rec.Table = t
			rec.Row = uint32(row)
		}
	}
	return nil
}

// ─── deserializeEmptyTableUserEnts ───────────────────────────────────────────

func deserializeEmptyTableUserEnts(br *binReader, w *World) error {
	count, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		eID, err := br.id()
		if err != nil {
			return err
		}
		if rec := w.index.Get(eID); rec != nil {
			row := w.empty.Append(eID)
			rec.Table = w.empty
			rec.Row = uint32(row)
		}
	}
	return nil
}

// ─── deserializeSparseData ───────────────────────────────────────────────────

func deserializeSparseData(br *binReader, w *World) error {
	numComps, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < numComps; i++ {
		compID, err := br.id()
		if err != nil {
			return err
		}
		elemSize, err := br.u32()
		if err != nil {
			return err
		}
		flags, err := br.u8()
		if err != nil {
			return err
		}
		entryCount, err := br.u32()
		if err != nil {
			return err
		}

		if flags&1 != 0 {
			applySparsePolicy(w, compID)
		}
		if flags&2 != 0 {
			applyDontFragmentPolicy(w, compID)
		}

		for j := uint32(0); j < entryCount; j++ {
			entityID, err := br.id()
			if err != nil {
				return err
			}
			dataBytes, err := br.raw(int(elemSize))
			if err != nil {
				return err
			}
			sparseSetInsert(w, entityID, compID, unsafe.Pointer(&dataBytes[0]))
		}
	}
	return nil
}

// ─── deserializeUnionState ───────────────────────────────────────────────────

func deserializeUnionState(br *binReader, w *World) error {
	numRels, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < numRels; i++ {
		relID, err := br.id()
		if err != nil {
			return err
		}
		entryCount, err := br.u32()
		if err != nil {
			return err
		}
		applyUnionPolicy(w, relID)
		relKey := ID(relID.Index())
		store := w.unionStore[relKey]

		for j := uint32(0); j < entryCount; j++ {
			entityID, err := br.id()
			if err != nil {
				return err
			}
			targetID, err := br.id()
			if err != nil {
				return err
			}
			if store == nil {
				continue
			}
			eIdx := ID(entityID.Index())
			store.dense = append(store.dense, unionEntry{entity: entityID, target: targetID})
			store.index[eIdx] = len(store.dense) - 1
		}
	}
	return nil
}

// ─── deserializeEntityRange ──────────────────────────────────────────────────

func deserializeEntityRange(br *binReader, w *World) error {
	min, err := br.u32()
	if err != nil {
		return err
	}
	max, err := br.u32()
	if err != nil {
		return err
	}
	set, err := br.u8()
	if err != nil {
		return err
	}
	w.index.ClearRange()
	if set != 0 {
		w.index.SetRange(min, max)
	}
	return nil
}

// ─── deserializePolicies ─────────────────────────────────────────────────────

func readBoolMapUser(br *binReader, m *map[ID]bool) error {
	count, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		k, err := br.id()
		if err != nil {
			return err
		}
		(*m)[k] = true
	}
	return nil
}

func deserializePolicies(br *binReader, w *World) error {
	if err := readBoolMapUser(br, &w.sparsePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.dontFragmentPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.unionPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.exclusivePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.symmetricPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.transitivePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.reflexivePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.acyclicPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.finalPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.singletonPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.writeOncePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.traversablePolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.relationshipPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.targetPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.traitPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.pairIsTagPolicies); err != nil {
		return err
	}
	if err := readBoolMapUser(br, &w.canTogglePolicies); err != nil {
		return err
	}
	if w.parentStoragePolicies == nil {
		w.parentStoragePolicies = make(map[ID]bool)
	}
	if err := readBoolMapUser(br, &w.parentStoragePolicies); err != nil {
		return err
	}

	cleanupCount, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < cleanupCount; i++ {
		k, err := br.id()
		if err != nil {
			return err
		}
		v, err := br.u8()
		if err != nil {
			return err
		}
		w.cleanupPolicies[k] = cleanupPolicyFlags(v)
	}

	instCount, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < instCount; i++ {
		k, err := br.id()
		if err != nil {
			return err
		}
		v, err := br.u8()
		if err != nil {
			return err
		}
		w.instantiatePolicies[k] = instantiatePolicyFlags(v)
	}

	oneOfCount, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < oneOfCount; i++ {
		k, err := br.id()
		if err != nil {
			return err
		}
		v, err := br.id()
		if err != nil {
			return err
		}
		w.oneOfPolicies[k] = v
	}
	return nil
}

// ─── deserializeOrderedChildren ──────────────────────────────────────────────

func deserializeOrderedChildren(br *binReader, w *World) error {
	count, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		parentID, err := br.id()
		if err != nil {
			return err
		}
		childCount, err := br.u32()
		if err != nil {
			return err
		}
		children := make([]ID, childCount)
		for j := range children {
			ch, err := br.id()
			if err != nil {
				return err
			}
			children[j] = ch
		}
		parentIdx := ID(parentID.Index())
		if w.orderedChildren == nil {
			w.orderedChildren = make(map[ID]*orderedChildList)
		}
		w.orderedChildren[parentIdx] = &orderedChildList{entries: children}
	}
	return nil
}

// ─── serializeUnitDefs ───────────────────────────────────────────────────────
//
// Format per unit:
//   u64 entityID
//   u8  nameLen; nameLen bytes (UTF-8)
//   u8  symbolLen; symbolLen bytes
//   f64 factor (8 bytes LE IEEE-754)
//   u8  power (int8 as uint8)
//   u64 base (0 if none)
//   u64 over (0 if none)
//   u32 numCount; numCount × u64 (numerator factor IDs)
//   u32 denomCount; denomCount × u64 (denominator factor IDs)

func serializeUnitDefs(bw *binWriter, w *World) {
	var units []struct {
		id ID
		u  Unit
	}
	for id, u := range w.unitDefs {
		if isBuiltinUnit(id) || id.Index() < firstSnapUserIndex {
			continue
		}
		units = append(units, struct {
			id ID
			u  Unit
		}{id, u})
	}
	bw.u32(uint32(len(units)))
	for _, e := range units {
		bw.id(e.id)
		b := []byte(e.u.Name)
		bw.u8(uint8(len(b)))
		bw.raw(b)
		s := []byte(e.u.Symbol)
		bw.u8(uint8(len(s)))
		bw.raw(s)
		var f [8]byte
		binary.LittleEndian.PutUint64(f[:], math.Float64bits(e.u.Factor))
		bw.raw(f[:])
		bw.u8(uint8(e.u.Power))
		bw.id(e.u.Base)
		bw.id(e.u.Over)
		if cd, ok := w.compoundDefs[e.id]; ok {
			bw.u32(uint32(len(cd.numerators)))
			for _, fid := range cd.numerators {
				bw.id(fid)
			}
			bw.u32(uint32(len(cd.denominators)))
			for _, fid := range cd.denominators {
				bw.id(fid)
			}
		} else {
			bw.u32(0)
			bw.u32(0)
		}
	}
}

// deserializeUnitDefs restores user-registered unit definitions from the snapshot.
func deserializeUnitDefs(br *binReader, w *World) error {
	for id := range w.unitDefs {
		if !isBuiltinUnit(id) && id.Index() >= firstSnapUserIndex {
			delete(w.unitDefs, id)
		}
	}
	if w.compoundDefs != nil {
		for id := range w.compoundDefs {
			if !isBuiltinUnit(id) && id.Index() >= firstSnapUserIndex {
				delete(w.compoundDefs, id)
			}
		}
	}
	count, err := br.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		entityID, err := br.id()
		if err != nil {
			return err
		}
		nameLen, err := br.u8()
		if err != nil {
			return err
		}
		nameBytes, err := br.raw(int(nameLen))
		if err != nil {
			return err
		}
		symLen, err := br.u8()
		if err != nil {
			return err
		}
		symBytes, err := br.raw(int(symLen))
		if err != nil {
			return err
		}
		fBytes, err := br.raw(8)
		if err != nil {
			return err
		}
		factor := math.Float64frombits(binary.LittleEndian.Uint64(fBytes))
		powerByte, err := br.u8()
		if err != nil {
			return err
		}
		base, err := br.id()
		if err != nil {
			return err
		}
		over, err := br.id()
		if err != nil {
			return err
		}
		numCount, err := br.u32()
		if err != nil {
			return err
		}
		numIDs := make([]ID, numCount)
		for j := range numIDs {
			numIDs[j], err = br.id()
			if err != nil {
				return err
			}
		}
		denomCount, err := br.u32()
		if err != nil {
			return err
		}
		denomIDs := make([]ID, denomCount)
		for j := range denomIDs {
			denomIDs[j], err = br.id()
			if err != nil {
				return err
			}
		}
		w.unitDefs[entityID] = Unit{
			Name:   string(nameBytes),
			Symbol: string(symBytes),
			Factor: factor,
			Power:  int8(powerByte),
			Base:   base,
			Over:   over,
		}
		if numCount > 0 || denomCount > 0 {
			w.compoundDefs[entityID] = &compoundDef{
				numerators:   numIDs,
				denominators: denomIDs,
			}
		}
	}
	return nil
}
