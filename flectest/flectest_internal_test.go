package flectest

import (
	"reflect"
	"testing"

	"github.com/snichols/flecs"
)

type innerPos struct{ X, Y float32 }

func TestNormalizeJSON_InvalidInput(t *testing.T) {
	_, err := normalizeJSON([]byte("not json"))
	if err == nil {
		t.Error("normalizeJSON: expected error for invalid JSON input")
	}
}

func TestNormalizeJSON_ValidInput(t *testing.T) {
	out, err := normalizeJSON([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("normalizeJSON: unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Error("normalizeJSON: empty output")
	}
}

func TestMustNormalizeJSON_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("mustNormalizeJSON: expected panic for invalid JSON")
		}
	}()
	mustNormalizeJSON([]byte("not json"))
}

func TestMustNormalizeJSON_Valid(t *testing.T) {
	out := mustNormalizeJSON([]byte(`{"x":1}`))
	if len(out) == 0 {
		t.Error("mustNormalizeJSON: empty output")
	}
}

func TestCopyWorldRegistrations_CopiesTypedComponents(t *testing.T) {
	src := flecs.New()
	flecs.RegisterComponent[innerPos](src)
	src.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, innerPos{X: 1, Y: 2})
	})

	dst := copyWorldRegistrations(src)
	// Verify innerPos is registered in dst with the same type.
	found := false
	dst.Read(func(fr *flecs.Reader) {
		for _, cid := range fr.Components() {
			info, ok := fr.ComponentInfo(cid)
			if ok && info.Type == reflect.TypeFor[innerPos]() {
				found = true
				return
			}
		}
	})
	if !found {
		t.Error("copyWorldRegistrations: innerPos not found in dst")
	}
}
