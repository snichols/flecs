package flecs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"unsafe"
)

// internalPos is a component type used only by internal REST query tests.
type internalPos struct{ X, Y float32 }

func TestRestQuery_HandlerBadMethod(t *testing.T) {
	w := New()
	posID := RegisterComponent[internalPos](w)
	w.SetName(posID, "IPos")
	handler := restQuery(w)
	req := httptest.NewRequest(http.MethodPost, "/query?expr=IPos", nil)
	rw := httptest.NewRecorder()
	handler(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405 for POST, got %d", rw.Code)
	}
}

func TestMarshalQuery_EntityDead(t *testing.T) {
	w := New()
	posID := RegisterComponent[internalPos](w)
	var dead ID
	w.Write(func(fw *Writer) {
		e := fw.NewEntity()
		Set(fw, e, internalPos{X: 1})
		dead = e
		fw.Delete(e)
	})
	w.Read(func(fr *Reader) {
		raw, ok := marshalComponentForQuery(w, fr, dead, posID)
		if ok || raw != nil {
			t.Errorf("expected (nil,false) for dead entity, got ok=%v raw=%v", ok, raw)
		}
	})
}

func TestMarshalQuery_IsAInheritedTyped(t *testing.T) {
	w := New()
	posID := RegisterComponent[internalPos](w)
	var childID ID
	w.Write(func(fw *Writer) {
		base := fw.NewEntity()
		Set(fw, base, internalPos{X: 10, Y: 20})
		child := fw.NewEntity()
		AddID(fw, child, MakePair(w.IsA(), base))
		childID = child
	})
	w.Read(func(fr *Reader) {
		// child HasID via IsA, GetIDPtr returns nil → GetByID fallback path
		raw, ok := marshalComponentForQuery(w, fr, childID, posID)
		if !ok || raw == nil {
			t.Errorf("expected (raw, true) for IsA-inherited typed component, got ok=%v", ok)
		}
		var v internalPos
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Errorf("unmarshal inherited component: %v", err)
		}
		if v.X != 10 || v.Y != 20 {
			t.Errorf("expected {10 20}, got %v", v)
		}
	})
}

func TestMarshalQuery_IsAInheritedDynamic(t *testing.T) {
	w := New()
	var dynID ID
	w.Write(func(fw *Writer) {
		dynID = RegisterDynamicComponent(fw, "DynISA", 8, 8)
	})
	var childID ID
	w.Write(func(fw *Writer) {
		base := fw.NewEntity()
		val := uint64(55)
		SetIDPtr(fw, base, dynID, unsafe.Pointer(&val))
		child := fw.NewEntity()
		AddID(fw, child, MakePair(w.IsA(), base))
		childID = child
	})
	w.Read(func(fr *Reader) {
		// child HasID via IsA, GetIDPtr returns nil for dynamic → return (nil,false)
		raw, ok := marshalComponentForQuery(w, fr, childID, dynID)
		if ok || raw != nil {
			t.Errorf("expected (nil,false) for IsA-inherited dynamic component, got ok=%v", ok)
		}
	})
}

func TestMarshalQuery_MarshalerError(t *testing.T) {
	w := New()
	var dynID ID
	w.Write(func(fw *Writer) {
		dynID = RegisterDynamicComponentWithMarshaler(fw, "ErrComp", 8, 8,
			func(_ unsafe.Pointer) (json.RawMessage, error) {
				return nil, fmt.Errorf("marshal always fails")
			},
			nil,
		)
	})
	var e ID
	w.Write(func(fw *Writer) {
		e = fw.NewEntity()
		val := uint64(1)
		SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})
	w.Read(func(fr *Reader) {
		raw, ok := marshalComponentForQuery(w, fr, e, dynID)
		if ok || raw != nil {
			t.Errorf("expected (nil,false) when marshaler errors, got ok=%v", ok)
		}
	})
}
