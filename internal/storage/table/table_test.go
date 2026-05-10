package table_test

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// Component types used across tests.
type Position struct{ X, Y float32 }
type Velocity struct{ X, Y float32 }
type WithStr struct{ S string }
type Marker struct{}

// IDs used for components (stable within test package; not issued by a World).
const (
	posID    flecs.ID = 1
	velID    flecs.ID = 2
	strID    flecs.ID = 3
	markerID flecs.ID = 4
)

func posInfo() *component.TypeInfo {
	return &component.TypeInfo{
		Size:  unsafe.Sizeof(Position{}),
		Align: unsafe.Alignof(Position{}),
		Name:  "Position",
		Type:  reflect.TypeFor[Position](),
	}
}

func velInfo() *component.TypeInfo {
	return &component.TypeInfo{
		Size:  unsafe.Sizeof(Velocity{}),
		Align: unsafe.Alignof(Velocity{}),
		Name:  "Velocity",
		Type:  reflect.TypeFor[Velocity](),
	}
}

func strInfo() *component.TypeInfo {
	return &component.TypeInfo{
		Size:  unsafe.Sizeof(WithStr{}),
		Align: unsafe.Alignof(WithStr{}),
		Name:  "WithStr",
		Type:  reflect.TypeFor[WithStr](),
	}
}

func markerInfo() *component.TypeInfo {
	return &component.TypeInfo{
		Size:  unsafe.Sizeof(Marker{}),
		Align: unsafe.Alignof(Marker{}),
		Name:  "Marker",
		Type:  reflect.TypeFor[Marker](),
	}
}

func TestTableConstructionAndType(t *testing.T) {
	tbl := table.New([]flecs.ID{posID, velID}, []*component.TypeInfo{posInfo(), velInfo()})
	ids := tbl.Type()
	if len(ids) != 2 || ids[0] != posID || ids[1] != velID {
		t.Fatalf("Type() = %v, want [%d %d]", ids, posID, velID)
	}
	if !tbl.HasComponent(posID) {
		t.Fatal("HasComponent(posID) = false, want true")
	}
	if !tbl.HasComponent(velID) {
		t.Fatal("HasComponent(velID) = false, want true")
	}
	if tbl.HasComponent(flecs.ID(99)) {
		t.Fatal("HasComponent(unknown) = true, want false")
	}
	if tbl.Count() != 0 {
		t.Fatalf("initial Count want 0, got %d", tbl.Count())
	}
}

func TestTableAppendEntities(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})

	e1 := flecs.MakeEntity(1, 0)
	e2 := flecs.MakeEntity(2, 0)
	e3 := flecs.MakeEntity(3, 0)

	r0 := tbl.Append(e1)
	r1 := tbl.Append(e2)
	r2 := tbl.Append(e3)

	if r0 != 0 || r1 != 1 || r2 != 2 {
		t.Fatalf("Append returned rows %d,%d,%d, want 0,1,2", r0, r1, r2)
	}
	if tbl.Count() != 3 {
		t.Fatalf("Count want 3, got %d", tbl.Count())
	}
	ents := tbl.Entities()
	if len(ents) != 3 || ents[0] != e1 || ents[1] != e2 || ents[2] != e3 {
		t.Fatalf("Entities() = %v, want [%v %v %v]", ents, e1, e2, e3)
	}
}

func TestTableSetGetRoundTrip(t *testing.T) {
	tbl := table.New([]flecs.ID{posID, velID}, []*component.TypeInfo{posInfo(), velInfo()})

	e := flecs.MakeEntity(1, 0)
	row := tbl.Append(e)

	p := Position{X: 1.5, Y: 2.5}
	v := Velocity{X: 3.0, Y: 4.0}
	tbl.Set(row, posID, unsafe.Pointer(&p))
	tbl.Set(row, velID, unsafe.Pointer(&v))

	pOut := (*Position)(tbl.Get(row, posID))
	vOut := (*Velocity)(tbl.Get(row, velID))
	if *pOut != p {
		t.Fatalf("Position round-trip: got %+v, want %+v", *pOut, p)
	}
	if *vOut != v {
		t.Fatalf("Velocity round-trip: got %+v, want %+v", *vOut, v)
	}
}

func TestTableGCPointerTracing(t *testing.T) {
	tbl := table.New([]flecs.ID{strID}, []*component.TypeInfo{strInfo()})

	e := flecs.MakeEntity(1, 0)
	row := tbl.Append(e)

	want := strings.Repeat("a", 1<<10)
	ws := WithStr{S: want}
	tbl.Set(row, strID, unsafe.Pointer(&ws))

	// Trigger GC twice to verify the column keeps the string alive.
	runtime.GC()
	runtime.GC()

	wsOut := (*WithStr)(tbl.Get(row, strID))
	if wsOut.S != want {
		t.Fatalf("string corrupted after GC: got len=%d, want len=%d", len(wsOut.S), len(want))
	}
}

func TestTableRemoveSwapMiddle(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})

	e1 := flecs.MakeEntity(1, 0)
	e2 := flecs.MakeEntity(2, 0)
	e3 := flecs.MakeEntity(3, 0)

	p1 := Position{X: 1}
	p2 := Position{X: 2}
	p3 := Position{X: 3}

	r0 := tbl.Append(e1)
	r1 := tbl.Append(e2)
	r2 := tbl.Append(e3)
	tbl.Set(r0, posID, unsafe.Pointer(&p1))
	tbl.Set(r1, posID, unsafe.Pointer(&p2))
	tbl.Set(r2, posID, unsafe.Pointer(&p3))

	moved, ok := tbl.RemoveSwap(0)
	if !ok || moved != e3 {
		t.Fatalf("RemoveSwap(0) returned (%v, %v), want (%v, true)", moved, ok, e3)
	}
	if tbl.Count() != 2 {
		t.Fatalf("Count want 2, got %d", tbl.Count())
	}
	ents := tbl.Entities()
	if len(ents) != 2 || ents[0] != e3 || ents[1] != e2 {
		t.Fatalf("Entities() = %v, want [%v %v]", ents, e3, e2)
	}
	pOut := (*Position)(tbl.Get(0, posID))
	if *pOut != p3 {
		t.Fatalf("row 0 Position want %+v (e3's), got %+v", p3, *pOut)
	}
}

func TestTableRemoveSwapLast(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})

	e1 := flecs.MakeEntity(1, 0)
	e2 := flecs.MakeEntity(2, 0)
	tbl.Append(e1)
	r1 := tbl.Append(e2)

	moved, ok := tbl.RemoveSwap(r1)
	if ok || moved != 0 {
		t.Fatalf("RemoveSwap(last) returned (%v, %v), want (0, false)", moved, ok)
	}
	if tbl.Count() != 1 {
		t.Fatalf("Count want 1, got %d", tbl.Count())
	}
	ents := tbl.Entities()
	if len(ents) != 1 || ents[0] != e1 {
		t.Fatalf("Entities() = %v, want [%v]", ents, e1)
	}
}

func TestTableRemoveSwapOnly(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	e1 := flecs.MakeEntity(1, 0)
	tbl.Append(e1)

	moved, ok := tbl.RemoveSwap(0)
	if ok || moved != 0 {
		t.Fatalf("RemoveSwap(only) returned (%v, %v), want (0, false)", moved, ok)
	}
	if tbl.Count() != 0 {
		t.Fatalf("Count want 0, got %d", tbl.Count())
	}
}

func TestTableGrowth(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	const n = 1024
	for i := 0; i < n; i++ {
		e := flecs.MakeEntity(uint32(i+1), 0)
		row := tbl.Append(e)
		p := Position{X: float32(i), Y: float32(i * 2)}
		tbl.Set(row, posID, unsafe.Pointer(&p))
	}
	if tbl.Count() != n {
		t.Fatalf("Count want %d, got %d", n, tbl.Count())
	}
	for i := 0; i < n; i++ {
		want := Position{X: float32(i), Y: float32(i * 2)}
		got := (*Position)(tbl.Get(i, posID))
		if *got != want {
			t.Fatalf("row %d: got %+v, want %+v", i, *got, want)
		}
	}
}

func TestTableTagComponent(t *testing.T) {
	tbl := table.New([]flecs.ID{posID, markerID}, []*component.TypeInfo{posInfo(), markerInfo()})

	e := flecs.MakeEntity(1, 0)
	row := tbl.Append(e)

	p := Position{X: 7, Y: 8}
	tbl.Set(row, posID, unsafe.Pointer(&p))
	tbl.Set(row, markerID, unsafe.Pointer(&p)) // no-op for tag

	pOut := (*Position)(tbl.Get(row, posID))
	if *pOut != p {
		t.Fatalf("Position round-trip with tag present: got %+v, want %+v", *pOut, p)
	}
	if tbl.Get(row, markerID) != nil {
		t.Fatal("Get on tag component should return nil")
	}
}

func TestTableEmptySignature(t *testing.T) {
	tbl := table.New([]flecs.ID{}, []*component.TypeInfo{})
	if tbl.Count() != 0 {
		t.Fatal("Count want 0 initially")
	}
	e := flecs.MakeEntity(1, 0)
	row := tbl.Append(e)
	if tbl.Count() != 1 {
		t.Fatal("Count want 1 after Append")
	}
	moved, ok := tbl.RemoveSwap(row)
	if ok || moved != 0 {
		t.Fatalf("RemoveSwap on empty-sig table: got (%v, %v), want (0, false)", moved, ok)
	}
	if tbl.Count() != 0 {
		t.Fatal("Count want 0 after RemoveSwap")
	}
}

func TestTableColumnIndex(t *testing.T) {
	tbl := table.New([]flecs.ID{posID, velID}, []*component.TypeInfo{posInfo(), velInfo()})
	if tbl.ColumnIndex(posID) != 0 {
		t.Fatalf("ColumnIndex(posID) want 0, got %d", tbl.ColumnIndex(posID))
	}
	if tbl.ColumnIndex(velID) != 1 {
		t.Fatalf("ColumnIndex(velID) want 1, got %d", tbl.ColumnIndex(velID))
	}
	if tbl.ColumnIndex(flecs.ID(99)) != -1 {
		t.Fatal("ColumnIndex(absent) want -1")
	}
}

func TestTableUnsortedPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unsorted ids, got none")
		}
	}()
	// Pass ids in descending order — must panic.
	table.New([]flecs.ID{velID, posID}, []*component.TypeInfo{velInfo(), posInfo()})
}

func TestTableGetOutOfRangePanic(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	tbl.Append(flecs.MakeEntity(1, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for out-of-range Get, got none")
		}
	}()
	tbl.Get(5, posID)
}

func TestTableSetOutOfRangePanic(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	tbl.Append(flecs.MakeEntity(1, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for out-of-range Set, got none")
		}
	}()
	p := Position{}
	tbl.Set(5, posID, unsafe.Pointer(&p))
}

func TestTableRemoveSwapOutOfRangePanic(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	tbl.Append(flecs.MakeEntity(1, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for out-of-range RemoveSwap, got none")
		}
	}()
	tbl.RemoveSwap(5)
}

func TestTableGetMissingIDPanic(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	tbl.Append(flecs.MakeEntity(1, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Get with absent id, got none")
		}
	}()
	tbl.Get(0, flecs.ID(99))
}

func TestTableSetMissingIDPanic(t *testing.T) {
	tbl := table.New([]flecs.ID{posID}, []*component.TypeInfo{posInfo()})
	tbl.Append(flecs.MakeEntity(1, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Set with absent id, got none")
		}
	}()
	p := Position{}
	tbl.Set(0, flecs.ID(99), unsafe.Pointer(&p))
}
