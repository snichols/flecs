package componentindex_test

import (
	"testing"

	"github.com/snichols/flecs/internal/ids"
	"github.com/snichols/flecs/internal/storage/componentindex"
	"github.com/snichols/flecs/internal/storage/table"
)

func newTable() *table.Table {
	return table.New([]ids.ID{}, nil)
}

func id(v uint32) ids.ID { return ids.MakeEntity(v, 0) }

func TestNew_empty(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	if got := idx.Count(cid); got != 0 {
		t.Fatalf("Count on empty index: want 0, got %d", got)
	}
	if got := idx.TablesFor(cid); got == nil {
		t.Fatal("TablesFor on empty index: want non-nil empty slice, got nil")
	} else if len(got) != 0 {
		t.Fatalf("TablesFor on empty index: want len 0, got %d", len(got))
	}
	if got := idx.CountComponents(); got != 0 {
		t.Fatalf("CountComponents on empty index: want 0, got %d", got)
	}
}

func TestRegister_single(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	t1 := newTable()

	idx.Register(cid, t1)

	if got := idx.Count(cid); got != 1 {
		t.Fatalf("Count after one Register: want 1, got %d", got)
	}
	tables := idx.TablesFor(cid)
	if len(tables) != 1 || tables[0] != t1 {
		t.Fatalf("TablesFor after one Register: want [t1], got %v", tables)
	}
}

func TestRegister_idempotent(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	t1 := newTable()

	idx.Register(cid, t1)
	idx.Register(cid, t1)

	if got := idx.Count(cid); got != 1 {
		t.Fatalf("Count after duplicate Register: want 1, got %d", got)
	}
}

func TestRegister_two_tables_preserves_order(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	t1 := newTable()
	t2 := newTable()

	idx.Register(cid, t1)
	idx.Register(cid, t2)

	if got := idx.Count(cid); got != 2 {
		t.Fatalf("Count after two Registers: want 2, got %d", got)
	}
	tables := idx.TablesFor(cid)
	if len(tables) != 2 || tables[0] != t1 || tables[1] != t2 {
		t.Fatalf("TablesFor order: want [t1, t2], got %v", tables)
	}
}

func TestRegister_multiple_components(t *testing.T) {
	idx := componentindex.New()
	idA := id(1)
	idB := id(2)
	t1 := newTable()

	idx.Register(idA, t1)
	idx.Register(idB, t1)

	if got := idx.CountComponents(); got != 2 {
		t.Fatalf("CountComponents: want 2, got %d", got)
	}
	if tables := idx.TablesFor(idA); len(tables) != 1 || tables[0] != t1 {
		t.Fatalf("TablesFor(idA): want [t1], got %v", tables)
	}
	if tables := idx.TablesFor(idB); len(tables) != 1 || tables[0] != t1 {
		t.Fatalf("TablesFor(idB): want [t1], got %v", tables)
	}
}

func TestEach_early_stop(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	t1 := newTable()
	t2 := newTable()
	t3 := newTable()

	idx.Register(cid, t1)
	idx.Register(cid, t2)
	idx.Register(cid, t3)

	var visited []*table.Table
	idx.Each(cid, func(tbl *table.Table) bool {
		visited = append(visited, tbl)
		return tbl != t2 // stop after visiting t2
	})

	if len(visited) != 2 || visited[0] != t1 || visited[1] != t2 {
		t.Fatalf("Each early stop: want [t1, t2], got %v", visited)
	}
}

func TestTablesFor_returns_copy(t *testing.T) {
	idx := componentindex.New()
	cid := id(1)
	t1 := newTable()
	t2 := newTable()

	idx.Register(cid, t1)

	snap := idx.TablesFor(cid)
	snap[0] = t2 // mutate the returned slice

	snap2 := idx.TablesFor(cid)
	if len(snap2) != 1 || snap2[0] != t1 {
		t.Fatalf("TablesFor copy: mutating returned slice should not affect index; got %v", snap2)
	}
}
