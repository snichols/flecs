package component_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
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
