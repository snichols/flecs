package entityindex

import (
	"testing"

	"github.com/snichols/flecs/internal/ids"
	"github.com/snichols/flecs/internal/storage/table"
)

// fastForwardGen is a test helper that modifies the dense vector and page
// record to simulate an entity that has been freed many times (advancing its
// generation to a target value without actually cycling through all frees).
// Used only to test the generation-overflow panic.
func fastForwardGen(idx *Index, id ids.ID, targetGen uint32) ids.ID {
	rawIdx := id.Index()
	newID := ids.MakeEntity(rawIdx, targetGen)
	// Update the dense vector entry (find it by raw index in the alive range).
	for i := 1; i < idx.aliveCount; i++ {
		if idx.dense[i].Index() == rawIdx {
			idx.dense[i] = newID
			break
		}
	}
	return newID
}

func TestAllocBasic(t *testing.T) {
	idx := New()

	a := idx.Alloc()
	if a.Index() != 1 {
		t.Fatalf("first alloc index want 1, got %d", a.Index())
	}
	if a.Generation() != 0 {
		t.Fatalf("first alloc generation want 0, got %d", a.Generation())
	}

	b := idx.Alloc()
	if b.Index() != 2 {
		t.Fatalf("second alloc index want 2, got %d", b.Index())
	}
	if b == a {
		t.Fatal("consecutive allocs returned same ID")
	}
}

func TestIsAliveAfterAllocAndFree(t *testing.T) {
	idx := New()
	id := idx.Alloc()

	if !idx.IsAlive(id) {
		t.Fatal("IsAlive false immediately after Alloc")
	}
	if !idx.Free(id) {
		t.Fatal("Free returned false for alive entity")
	}
	if idx.IsAlive(id) {
		t.Fatal("IsAlive true after Free")
	}
}

func TestIsAliveNeverAllocated(t *testing.T) {
	idx := New()
	if idx.IsAlive(ids.MakeEntity(42, 0)) {
		t.Fatal("IsAlive true for never-allocated entity")
	}
	if idx.IsAlive(ids.ID(0)) {
		t.Fatal("IsAlive true for null ID")
	}
}

func TestIsAliveStaleHandle(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	idx.Free(id)
	// old generation
	if idx.IsAlive(id) {
		t.Fatal("IsAlive true for stale handle after free")
	}
}

func TestFreeUnknown(t *testing.T) {
	idx := New()
	if idx.Free(ids.MakeEntity(99, 0)) {
		t.Fatal("Free returned true for unknown entity")
	}
}

func TestFreeAlreadyFreed(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	idx.Free(id)
	if idx.Free(id) {
		t.Fatal("Free returned true for already-freed entity")
	}
}

func TestGenerationRecycling(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	rawIdx := id.Index()

	var prev ids.ID = id
	for i := 0; i < 5; i++ {
		if !idx.Free(prev) {
			t.Fatalf("iteration %d: Free returned false", i)
		}
		next := idx.Alloc()
		if next.Index() != rawIdx {
			t.Fatalf("iteration %d: recycled index want %d, got %d", i, rawIdx, next.Index())
		}
		if next.Generation() != prev.Generation()+1 {
			t.Fatalf("iteration %d: generation want %d, got %d", i, prev.Generation()+1, next.Generation())
		}
		prev = next
	}
}

func TestRecycleOrdering(t *testing.T) {
	idx := New()
	a := idx.Alloc()
	b := idx.Alloc()

	idx.Free(a)
	idx.Free(b)

	// FIFO: first freed (a) must be first recycled.
	r1 := idx.Alloc()
	r2 := idx.Alloc()

	if r1.Index() != a.Index() {
		t.Fatalf("first recycle: want index %d, got %d", a.Index(), r1.Index())
	}
	if r2.Index() != b.Index() {
		t.Fatalf("second recycle: want index %d, got %d", b.Index(), r2.Index())
	}
}

func TestPageGrowth(t *testing.T) {
	idx := New()
	const n = 200

	allocated := make([]ids.ID, n)
	for i := range allocated {
		allocated[i] = idx.Alloc()
	}

	for _, id := range allocated {
		if !idx.IsAlive(id) {
			t.Fatalf("entity %v should be alive", id)
		}
		if idx.Get(id) == nil {
			t.Fatalf("Get returned nil for alive entity %v", id)
		}
	}

	// Free every other one.
	freed := 0
	for i, id := range allocated {
		if i%2 == 0 {
			idx.Free(id)
			freed++
		}
	}

	// Reallocate — should come from recycled pool before new indices.
	newIDs := make([]ids.ID, freed)
	maxBeforeRealloc := idx.maxID
	for i := range newIDs {
		newIDs[i] = idx.Alloc()
	}

	// All new allocs should have been recycled (no new maxID growth).
	if idx.maxID != maxBeforeRealloc {
		t.Fatalf("expected recycled IDs only, but maxID grew from %d to %d",
			maxBeforeRealloc, idx.maxID)
	}

	for _, id := range newIDs {
		if !idx.IsAlive(id) {
			t.Fatalf("newly recycled entity %v not alive", id)
		}
	}
}

func TestRecordPointerStability(t *testing.T) {
	idx := New()
	first := idx.Alloc()
	rec := idx.Get(first)
	if rec == nil {
		t.Fatal("Get returned nil for first entity")
	}
	rec.Row = 42

	// Alloc 1000 more entities; this will grow pages but must not move
	// the existing page holding 'first'.
	for i := 0; i < 1000; i++ {
		idx.Alloc()
	}

	// The original pointer must still be valid and hold our written value.
	check := idx.Get(first)
	if check == nil {
		t.Fatal("Get returned nil after page growth")
	}
	if check != rec {
		t.Fatal("record pointer changed after page growth (stability violated)")
	}
	if check.Row != 42 {
		t.Fatalf("record content changed: want Row=42, got Row=%d", check.Row)
	}
}

func TestCountCorrectness(t *testing.T) {
	idx := New()
	if idx.Count() != 0 {
		t.Fatalf("initial count want 0, got %d", idx.Count())
	}

	allocated := make([]ids.ID, 5)
	for i := range allocated {
		allocated[i] = idx.Alloc()
	}
	if idx.Count() != 5 {
		t.Fatalf("after 5 allocs: want 5, got %d", idx.Count())
	}

	idx.Free(allocated[0])
	idx.Free(allocated[2])
	if idx.Count() != 3 {
		t.Fatalf("after 2 frees: want 3, got %d", idx.Count())
	}

	idx.Alloc()
	if idx.Count() != 4 {
		t.Fatalf("after re-alloc: want 4, got %d", idx.Count())
	}
}

func TestEachAliveset(t *testing.T) {
	idx := New()
	a := idx.Alloc()
	b := idx.Alloc()
	c := idx.Alloc()

	idx.Free(b) // b is dead

	seen := map[ids.ID]bool{}
	idx.Each(func(id ids.ID, _ *Record) {
		seen[id] = true
	})

	if len(seen) != 2 {
		t.Fatalf("Each visited %d entities, want 2", len(seen))
	}
	if !seen[a] {
		t.Fatal("Each missed entity a")
	}
	if !seen[c] {
		t.Fatal("Each missed entity c")
	}
	if seen[b] {
		t.Fatal("Each visited dead entity b")
	}
}

func TestGetNullID(t *testing.T) {
	idx := New()
	if idx.Get(ids.ID(0)) != nil {
		t.Fatal("Get(0) should return nil")
	}
	if idx.IsAlive(ids.ID(0)) {
		t.Fatal("IsAlive(0) should return false")
	}
}

func TestGenerationOverflowPanic(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	// Fast-forward the generation to max so the next Free would overflow.
	id = fastForwardGen(idx, id, ^uint32(0))

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on generation overflow, got none")
		}
	}()
	idx.Free(id)
}

func TestFreeStaleHandle(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	idx.Free(id)
	newID := idx.Alloc() // same index, gen+1

	// Trying to free the old generation handle must return false.
	if idx.Free(id) {
		t.Fatal("Free returned true for stale handle (old generation)")
	}
	// The new generation handle must still be alive.
	if !idx.IsAlive(newID) {
		t.Fatal("new-generation entity should still be alive")
	}
}

func TestGetDeadAndStaleEntity(t *testing.T) {
	idx := New()
	id := idx.Alloc()
	idx.Free(id)

	// Get on a freed entity must return nil.
	if idx.Get(id) != nil {
		t.Fatal("Get returned non-nil for freed entity")
	}

	// Get on a never-allocated entity must return nil.
	never := ids.MakeEntity(999, 0)
	if idx.Get(never) != nil {
		t.Fatal("Get returned non-nil for never-allocated entity")
	}

	// Get on a stale handle (entity re-allocated with new generation) must return nil.
	newID := idx.Alloc() // recycles the freed index
	if idx.Get(id) != nil {
		t.Fatal("Get returned non-nil for stale handle after re-allocation")
	}
	if idx.Get(newID) == nil {
		t.Fatal("Get returned nil for newly allocated entity")
	}
}

func TestRecordTableField(t *testing.T) {
	idx := New()
	e := idx.Alloc()

	rec := idx.Get(e)
	if rec == nil {
		t.Fatal("Get returned nil for allocated entity")
	}
	if rec.Table != nil {
		t.Fatal("Table should be nil before assignment")
	}

	// Assign a fake table pointer (zero-value pointer suffices for field storage).
	fakeTable := (*table.Table)(nil)
	rec.Table = fakeTable

	rec2 := idx.Get(e)
	if rec2.Table != fakeTable {
		t.Fatal("Record.Table not retained after Get")
	}

	// Free must zero the Table field.
	idx.Free(e)
	if rec.Table != nil {
		t.Fatal("Free did not zero Record.Table")
	}
}

func TestAllocNeverReturnsIndex0(t *testing.T) {
	idx := New()
	for i := 0; i < 100; i++ {
		id := idx.Alloc()
		if id.Index() == 0 {
			t.Fatal("Alloc returned index 0")
		}
	}
}

func TestEachDenseOrder(t *testing.T) {
	idx := New()
	var allocOrder []ids.ID
	for i := 0; i < 5; i++ {
		allocOrder = append(allocOrder, idx.Alloc())
	}
	// The dense vector [1:aliveCount] should be in allocation order.
	var eachOrder []ids.ID
	idx.Each(func(id ids.ID, _ *Record) {
		eachOrder = append(eachOrder, id)
	})
	if len(eachOrder) != len(allocOrder) {
		t.Fatalf("Each returned %d items, want %d", len(eachOrder), len(allocOrder))
	}
	for i, id := range allocOrder {
		if eachOrder[i] != id {
			t.Fatalf("dense order mismatch at %d: got %v, want %v", i, eachOrder[i], id)
		}
	}
}
