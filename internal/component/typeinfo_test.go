package component_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/ids"
)

type hookTarget struct{ v int }

func TestNilMoveHookAllowed(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[hookTarget](r)
	if info.Hooks.Move != nil {
		t.Fatal("Move hook should be nil by default")
	}
}

type fooRegistered struct{ V int }

func TestTypeInfoTypeField(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[fooRegistered](r)
	want := reflect.TypeFor[fooRegistered]()
	if info.Type != want {
		t.Fatalf("TypeInfo.Type = %v, want %v", info.Type, want)
	}
}

func TestMoveHookInvocation(t *testing.T) {
	r := component.NewRegistry()
	counter := 0
	info := component.RegisterWithHooks[hookTarget](r, component.Hooks{
		Move: func(dst, src unsafe.Pointer) {
			*(*hookTarget)(dst) = *(*hookTarget)(src)
			counter++
		},
	})

	src := hookTarget{v: 42}
	dst := hookTarget{}
	info.Hooks.Move(unsafe.Pointer(&dst), unsafe.Pointer(&src))

	if counter != 1 {
		t.Errorf("Move hook fired %d times, want 1", counter)
	}
	if dst.v != 42 {
		t.Errorf("Move: dst.v = %d, want 42", dst.v)
	}
}

func TestEntityCallbackFieldsNilByDefault(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[hookTarget](r)
	if info.Hooks.OnAdd != nil {
		t.Fatal("OnAdd should be nil by default")
	}
	if info.Hooks.OnSet != nil {
		t.Fatal("OnSet should be nil by default")
	}
	if info.Hooks.OnRemove != nil {
		t.Fatal("OnRemove should be nil by default")
	}
}

func TestEntityCallbackFieldsCanBeSetAndInvoked(t *testing.T) {
	r := component.NewRegistry()
	var addCalled, setCalled, removeCalled bool
	h := component.Hooks{
		OnAdd: func(world any, entity ids.ID, ptr unsafe.Pointer) {
			addCalled = true
		},
		OnSet: func(world any, entity ids.ID, ptr unsafe.Pointer) {
			setCalled = true
		},
		OnRemove: func(world any, entity ids.ID, ptr unsafe.Pointer) {
			removeCalled = true
		},
	}
	info := component.RegisterWithHooks[hookTarget](r, h)

	if info.Hooks.OnAdd == nil {
		t.Fatal("OnAdd should not be nil after RegisterWithHooks")
	}
	if info.Hooks.OnSet == nil {
		t.Fatal("OnSet should not be nil after RegisterWithHooks")
	}
	if info.Hooks.OnRemove == nil {
		t.Fatal("OnRemove should not be nil after RegisterWithHooks")
	}

	// Invoke the callbacks manually to confirm they are callable.
	info.Hooks.OnAdd(nil, 1, nil)
	info.Hooks.OnSet(nil, 2, nil)
	info.Hooks.OnRemove(nil, 3, nil)

	if !addCalled {
		t.Fatal("OnAdd callback was not invoked")
	}
	if !setCalled {
		t.Fatal("OnSet callback was not invoked")
	}
	if !removeCalled {
		t.Fatal("OnRemove callback was not invoked")
	}
}

func TestComponentFieldZeroByDefault(t *testing.T) {
	r := component.NewRegistry()
	info := component.Register[hookTarget](r)
	if info.Component != 0 {
		t.Fatalf("Component field should be 0 by default, got %d", info.Component)
	}
}
