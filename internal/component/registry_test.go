package component_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/ids"
)

type Position struct{ X, Y float32 }

type zeroSize struct{}

type big64 struct{ _ [64]byte }

type Velocity struct{ DX, DY float32 }

func TestRegisterBasicStruct(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[Position](r)

	if got, want := info.Size, uintptr(unsafe.Sizeof(Position{})); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
	if got, want := info.Align, uintptr(unsafe.Alignof(Position{})); got != want {
		t.Errorf("Align = %d, want %d", got, want)
	}
	if got, want := info.Name, "component_test.Position"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestRegisterPrimitives(t *testing.T) {
	r := component.NewRegistry()

	infoInt := component.Register[int](r)
	if got, want := infoInt.Size, uintptr(unsafe.Sizeof(int(0))); got != want {
		t.Errorf("int Size = %d, want %d", got, want)
	}
	if got, want := infoInt.Align, uintptr(unsafe.Alignof(int(0))); got != want {
		t.Errorf("int Align = %d, want %d", got, want)
	}
	if got, want := infoInt.Name, "int"; got != want {
		t.Errorf("int Name = %q, want %q", got, want)
	}

	infoF64 := component.Register[float64](r)
	if got, want := infoF64.Size, uintptr(unsafe.Sizeof(float64(0))); got != want {
		t.Errorf("float64 Size = %d, want %d", got, want)
	}
	if got, want := infoF64.Align, uintptr(unsafe.Alignof(float64(0))); got != want {
		t.Errorf("float64 Align = %d, want %d", got, want)
	}
	if got, want := infoF64.Name, "float64"; got != want {
		t.Errorf("float64 Name = %q, want %q", got, want)
	}

	infoBool := component.Register[bool](r)
	if got, want := infoBool.Size, uintptr(unsafe.Sizeof(false)); got != want {
		t.Errorf("bool Size = %d, want %d", got, want)
	}
	if got, want := infoBool.Align, uintptr(unsafe.Alignof(false)); got != want {
		t.Errorf("bool Align = %d, want %d", got, want)
	}
	if got, want := infoBool.Name, "bool"; got != want {
		t.Errorf("bool Name = %q, want %q", got, want)
	}
}

func TestRegisterZeroSize(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[zeroSize](r)

	if info.Size != 0 {
		t.Errorf("zeroSize Size = %d, want 0", info.Size)
	}
}

func TestRegister64Bytes(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[big64](r)

	if got, want := info.Size, uintptr(64); got != want {
		t.Errorf("big64 Size = %d, want %d", got, want)
	}
}

func TestRegisterIdempotent(t *testing.T) {
	r := component.NewRegistry()
	a := component.Register[Position](r)
	b := component.Register[Position](r)

	if a != b {
		t.Error("Register[Position] twice returned different pointers")
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d after two Register[Position] calls, want 1", r.Count())
	}
}

func TestRegisterWithHooksUpdatesExisting(t *testing.T) {
	r := component.NewRegistry()
	orig := component.Register[Position](r)
	if orig.Hooks.Move != nil {
		t.Fatal("Move hook should be nil after Register")
	}

	called := false
	updated := component.RegisterWithHooks[Position](r, component.Hooks{
		Move: func(dst, src unsafe.Pointer) { called = true },
	})

	if orig != updated {
		t.Error("RegisterWithHooks returned a different pointer than Register")
	}
	if orig.Hooks.Move == nil {
		t.Error("RegisterWithHooks did not update Move hook on existing TypeInfo")
	}
	orig.Hooks.Move(nil, nil)
	if !called {
		t.Error("updated Move hook did not fire")
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d after Register + RegisterWithHooks, want 1", r.Count())
	}
}

func TestLookupByTypeFound(t *testing.T) {
	r := component.NewRegistry()
	registered := component.Register[Position](r)

	found, ok := component.LookupByType[Position](r)
	if !ok {
		t.Fatal("LookupByType[Position]: ok = false, want true")
	}
	if found != registered {
		t.Error("LookupByType[Position] returned a different pointer than Register")
	}
}

func TestLookupByTypeNotFound(t *testing.T) {
	r := component.NewRegistry()
	component.Register[Position](r)

	info, ok := component.LookupByType[Velocity](r)
	if ok {
		t.Error("LookupByType[Velocity]: ok = true, want false")
	}
	if info != nil {
		t.Error("LookupByType[Velocity]: info != nil, want nil")
	}
}

func TestLookupByReflectTypeMatchesLookupByType(t *testing.T) {
	r := component.NewRegistry()
	component.Register[Position](r)

	byGeneric, _ := component.LookupByType[Position](r)
	byReflect, ok := r.LookupByReflectType(reflect.TypeFor[Position]())

	if !ok {
		t.Fatal("LookupByReflectType: ok = false, want true")
	}
	if byGeneric != byReflect {
		t.Error("LookupByReflectType returned a different pointer than LookupByType")
	}
}

func TestEachInsertionOrder(t *testing.T) {
	r := component.NewRegistry()
	component.Register[Position](r)
	component.Register[Velocity](r)
	component.Register[int](r)

	want := []reflect.Type{
		reflect.TypeFor[Position](),
		reflect.TypeFor[Velocity](),
		reflect.TypeFor[int](),
	}

	var got []reflect.Type
	r.Each(func(t reflect.Type, _ *component.TypeInfo) {
		got = append(got, t)
	})

	if len(got) != len(want) {
		t.Fatalf("Each visited %d types, want %d", len(got), len(want))
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("Each[%d] = %v, want %v", i, g, want[i])
		}
	}
}

func TestEachCountMatchesCount(t *testing.T) {
	r := component.NewRegistry()
	component.Register[Position](r)
	component.Register[Velocity](r)

	visited := 0
	r.Each(func(_ reflect.Type, _ *component.TypeInfo) {
		visited++
	})

	if visited != r.Count() {
		t.Errorf("Each visited %d types, Count = %d", visited, r.Count())
	}
}

func TestCount(t *testing.T) {
	r := component.NewRegistry()
	if r.Count() != 0 {
		t.Errorf("Count = %d on empty registry, want 0", r.Count())
	}
	component.Register[Position](r)
	if r.Count() != 1 {
		t.Errorf("Count = %d after one Register, want 1", r.Count())
	}
	component.Register[Velocity](r)
	if r.Count() != 2 {
		t.Errorf("Count = %d after two Registers, want 2", r.Count())
	}
}

func TestAssociateIDBasic(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[Position](r)

	const testID ids.ID = 42
	r.AssociateID(info, testID)

	if info.Component != testID {
		t.Fatalf("Component field want %d, got %d", testID, info.Component)
	}

	found, ok := r.LookupByID(testID)
	if !ok {
		t.Fatal("LookupByID returned false for associated ID")
	}
	if found != info {
		t.Fatal("LookupByID returned a different *TypeInfo")
	}
}

func TestAssociateIDIdempotent(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[Position](r)
	const testID ids.ID = 7
	r.AssociateID(info, testID)
	r.AssociateID(info, testID) // same call — must not panic
	if info.Component != testID {
		t.Fatalf("Component field changed after idempotent call: got %d", info.Component)
	}
}

func TestAssociateIDZeroPanics(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[Position](r)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero ID, got none")
		}
	}()
	r.AssociateID(info, 0)
}

func TestAssociateIDConflictPanics(t *testing.T) {
	r := component.NewRegistry()
	info1 := component.Register[Position](r)

	type Other struct{ Z float32 }
	info2 := component.Register[Other](r)

	const testID ids.ID = 5
	r.AssociateID(info1, testID)
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected panic for conflicting TypeInfo, got none")
		}
	}()
	r.AssociateID(info2, testID)
}

func TestLookupByIDNotFound(t *testing.T) {
	r := component.NewRegistry()
	found, ok := r.LookupByID(99)
	if ok {
		t.Fatal("LookupByID returned true for unknown ID")
	}
	if found != nil {
		t.Fatal("LookupByID returned non-nil for unknown ID")
	}
}

// ── EnsureID ──────────────────────────────────────────────────────────────────

func TestEnsureIDCreatesZeroSizeTypeInfo(t *testing.T) {
	r := component.NewRegistry()
	const rawID ids.ID = 42
	info := r.EnsureID(rawID)

	if info == nil {
		t.Fatal("EnsureID returned nil")
	}
	if info.Size != 0 {
		t.Errorf("EnsureID TypeInfo Size want 0, got %d", info.Size)
	}
	if info.Component != rawID {
		t.Errorf("EnsureID TypeInfo Component want %d, got %d", rawID, info.Component)
	}
	// LookupByID must find it.
	found, ok := r.LookupByID(rawID)
	if !ok {
		t.Fatal("LookupByID returned false for EnsureID'd id")
	}
	if found != info {
		t.Fatal("LookupByID returned a different *TypeInfo than EnsureID")
	}
}

func TestEnsureIDIdempotent(t *testing.T) {
	r := component.NewRegistry()
	const rawID ids.ID = 7
	a := r.EnsureID(rawID)
	b := r.EnsureID(rawID)
	if a != b {
		t.Fatal("EnsureID not idempotent: returned different pointers")
	}
}

func TestEnsureIDReturnsExistingTypeInfo(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[Position](r)
	const posID ids.ID = 55
	r.AssociateID(info, posID)

	got := r.EnsureID(posID)
	if got != info {
		t.Fatal("EnsureID did not return the existing TypeInfo for a registered ID")
	}
}

// ── RegisterPairData ──────────────────────────────────────────────────────────

func TestRegisterPairDataBasic(t *testing.T) {
	r := component.NewRegistry()
	const pairID ids.ID = 1000
	info := component.RegisterPairData[Position](r, pairID)

	if info == nil {
		t.Fatal("RegisterPairData returned nil")
	}
	if info.Size != unsafe.Sizeof(Position{}) {
		t.Errorf("RegisterPairData Size want %d, got %d", unsafe.Sizeof(Position{}), info.Size)
	}
	if info.Type != reflect.TypeFor[Position]() {
		t.Errorf("RegisterPairData Type mismatch")
	}
	if info.Component != pairID {
		t.Errorf("RegisterPairData Component want %d, got %d", pairID, info.Component)
	}
	if len(info.Name) == 0 {
		t.Error("RegisterPairData Name is empty")
	}

	// LookupByID must find it.
	found, ok := r.LookupByID(pairID)
	if !ok {
		t.Fatal("LookupByID returned false for pair-registered ID")
	}
	if found != info {
		t.Fatal("LookupByID returned a different *TypeInfo than RegisterPairData")
	}
}

func TestRegisterPairDataPointerDistinct(t *testing.T) {
	// The per-pair TypeInfo must be pointer-distinct from the base T TypeInfo.
	r := component.NewRegistry()
	base := component.Register[Position](r)
	const pairID ids.ID = 2000
	pairInfo := component.RegisterPairData[Position](r, pairID)
	if base == pairInfo {
		t.Fatal("RegisterPairData returned the same *TypeInfo as Register (must be pointer-distinct)")
	}
}

func TestRegisterPairDataIdempotent(t *testing.T) {
	r := component.NewRegistry()
	const pairID ids.ID = 3000
	a := component.RegisterPairData[Position](r, pairID)
	b := component.RegisterPairData[Position](r, pairID)
	if a != b {
		t.Fatal("RegisterPairData not idempotent: returned different pointers for same (T, pairID)")
	}
}

func TestRegisterPairDataConflictPanics(t *testing.T) {
	r := component.NewRegistry()
	const pairID ids.ID = 4000
	component.RegisterPairData[Position](r, pairID)

	type Other struct{ Z float64 }

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("RegisterPairData with conflicting type should panic, got none")
		}
	}()
	component.RegisterPairData[Other](r, pairID) // same pairID, different type → panic
}

func TestRegisterPairDataBaseTypeInfoUnmodified(t *testing.T) {
	// Registering pair data must not change the base T TypeInfo's Component field.
	r := component.NewRegistry()
	base := component.Register[Position](r)
	const compID ids.ID = 5000
	r.AssociateID(base, compID)

	const pairID ids.ID = 6000
	component.RegisterPairData[Position](r, pairID)

	if base.Component != compID {
		t.Errorf("base TypeInfo.Component changed: want %d, got %d", compID, base.Component)
	}
}
