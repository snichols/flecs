package component_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
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
