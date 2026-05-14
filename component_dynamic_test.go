package flecs_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── helpers ───────────────────────────────────────────────────────────────────

type dynAuxPos struct{ X, Y float32 }

func getDynBytes(fr *flecs.Reader, e flecs.ID, id flecs.ID, size uintptr) []byte {
	ptr := flecs.GetIDPtr(fr, e, id)
	if ptr == nil {
		return nil
	}
	out := make([]byte, size)
	copy(out, unsafe.Slice((*byte)(ptr), size))
	return out
}

// ── Test 1: basic round-trip (size 16, align 8) ───────────────────────────────

func TestDynamicComponent_BasicRoundTrip(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/Basic16", 16, 8)
	})

	src := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		got := getDynBytes(fr, e, dynID, 16)
		if !bytes.Equal(got, src[:]) {
			t.Errorf("round-trip mismatch: got %v, want %v", got, src[:])
		}
	})
}

// ── Test 2: multiple dynamic components, distinct sizes, no cross-contamination ─

func TestDynamicComponent_MultipleDistinctSizes(t *testing.T) {
	w := flecs.New()
	var d8, d16, d32 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		d8 = flecs.RegisterDynamicComponent(fw, "test/Dyn8", 8, 8)
		d16 = flecs.RegisterDynamicComponent(fw, "test/Dyn16", 16, 8)
		d32 = flecs.RegisterDynamicComponent(fw, "test/Dyn32", 32, 8)
	})

	src8 := [8]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}
	src16 := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	var src32 [32]byte
	for i := range src32 {
		src32[i] = byte(i + 100)
	}

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, d8, unsafe.Pointer(&src8[0]))
		flecs.SetIDPtr(fw, e, d16, unsafe.Pointer(&src16[0]))
		flecs.SetIDPtr(fw, e, d32, unsafe.Pointer(&src32[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		got8 := getDynBytes(fr, e, d8, 8)
		got16 := getDynBytes(fr, e, d16, 16)
		got32 := getDynBytes(fr, e, d32, 32)
		if !bytes.Equal(got8, src8[:]) {
			t.Errorf("d8 mismatch: got %v, want %v", got8, src8[:])
		}
		if !bytes.Equal(got16, src16[:]) {
			t.Errorf("d16 mismatch")
		}
		if !bytes.Equal(got32, src32[:]) {
			t.Errorf("d32 mismatch")
		}
	})
}

// ── Test 3: dynamic + typed component coexist on same entity ─────────────────

type dynTypedPos struct{ X, Y float32 }

func TestDynamicComponent_CoexistWithTyped(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[dynTypedPos](w)
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/CoexistDyn", 8, 8)
	})

	dynSrc := [8]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, dynTypedPos{X: 1.5, Y: 2.5})
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&dynSrc[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		pos, ok := flecs.Get[dynTypedPos](fr, e)
		if !ok || pos != (dynTypedPos{X: 1.5, Y: 2.5}) {
			t.Errorf("typed component wrong: %v ok=%v", pos, ok)
		}
		got := getDynBytes(fr, e, dynID, 8)
		if !bytes.Equal(got, dynSrc[:]) {
			t.Errorf("dynamic component mismatch after coexist: got %v, want %v", got, dynSrc[:])
		}
	})
}

// ── Test 4: dynamic component on sparse storage ──────────────────────────────

func TestDynamicComponent_SparseStorage(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/SparseDyn", 8, 8)
	})
	flecs.SetSparse(w, dynID)

	src := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		got := getDynBytes(fr, e, dynID, 8)
		if !bytes.Equal(got, src[:]) {
			t.Errorf("sparse dynamic mismatch: got %v, want %v", got, src[:])
		}
	})
}

// ── Test 5: dynamic component on DontFragment storage ────────────────────────

func TestDynamicComponent_DontFragmentStorage(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/DFDyn", 8, 8)
	})
	flecs.SetDontFragment(w, dynID)

	src := [8]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		got := getDynBytes(fr, e, dynID, 8)
		if !bytes.Equal(got, src[:]) {
			t.Errorf("dontfragment dynamic mismatch: got %v, want %v", got, src[:])
		}
	})
}

// ── Test 6: marshal round-trip with default base64 serializer ────────────────

func TestDynamicComponent_MarshalRoundTrip_Base64(t *testing.T) {
	const compName = "test/MarshalDyn16"
	const size = uintptr(16)

	wA := flecs.New()
	var dynID flecs.ID
	wA.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})

	src := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	wA.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	data, err := wA.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// World B: re-register the same name+size+align, unmarshal, verify.
	wB := flecs.New()
	var dynIDB flecs.ID
	wB.Write(func(fw *flecs.Writer) {
		dynIDB = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})
	if err := wB.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	found := false
	wB.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynIDB, func(_ flecs.ID, ptr unsafe.Pointer) {
			found = true
			got := make([]byte, size)
			copy(got, unsafe.Slice((*byte)(ptr), size))
			if !bytes.Equal(got, src[:]) {
				t.Errorf("unmarshal round-trip mismatch: got %v, want %v", got, src[:])
			}
		})
	})
	if !found {
		t.Error("no entities found with dynamic component after unmarshal")
	}
}

// ── Test 7: marshal round-trip with custom serializer (JSON-encoded int32) ───

func TestDynamicComponent_MarshalRoundTrip_CustomSerializer(t *testing.T) {
	const compName = "test/MarshalDynCustom"
	const size = uintptr(4) // int32

	makeMarshaler := func() (func(unsafe.Pointer) (json.RawMessage, error), func(json.RawMessage, unsafe.Pointer) error) {
		m := func(ptr unsafe.Pointer) (json.RawMessage, error) {
			v := *(*int32)(ptr)
			return json.Marshal(v)
		}
		u := func(data json.RawMessage, ptr unsafe.Pointer) error {
			var v int32
			if err := json.Unmarshal(data, &v); err != nil {
				return err
			}
			*(*int32)(ptr) = v
			return nil
		}
		return m, u
	}

	wA := flecs.New()
	var dynID flecs.ID
	wA.Write(func(fw *flecs.Writer) {
		m, u := makeMarshaler()
		dynID = flecs.RegisterDynamicComponentWithMarshaler(fw, compName, size, 4, m, u)
	})

	val := int32(42)
	wA.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})

	data, err := wA.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	wB := flecs.New()
	var dynIDB flecs.ID
	wB.Write(func(fw *flecs.Writer) {
		m, u := makeMarshaler()
		dynIDB = flecs.RegisterDynamicComponentWithMarshaler(fw, compName, size, 4, m, u)
	})
	if err := wB.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	found := false
	wB.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynIDB, func(_ flecs.ID, ptr unsafe.Pointer) {
			found = true
			got := *(*int32)(ptr)
			if got != val {
				t.Errorf("custom serializer round-trip: got %d, want %d", got, val)
			}
		})
	})
	if !found {
		t.Error("no entities found with dynamic component after custom unmarshal")
	}
}

// ── Test 8: OnSet observer fires with correct pointer ─────────────────────────

func TestDynamicComponent_OnSetObserver(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/OnSetDyn", 8, 8)
	})

	src := [8]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	var observed []byte
	flecs.OnSetByID(w, dynID, func(_ *flecs.Writer, _ flecs.ID, ptr unsafe.Pointer) {
		if ptr != nil {
			b := make([]byte, 8)
			copy(b, unsafe.Slice((*byte)(ptr), 8))
			observed = b
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})
	_ = e

	if !bytes.Equal(observed, src[:]) {
		t.Errorf("OnSet observed bytes %v, want %v", observed, src[:])
	}
}

// ── Test 9: EachByID iterates every holder ───────────────────────────────────

func TestDynamicComponent_EachByID(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/EachByIDDyn", 4, 4)
	})

	const N = 5
	vals := [N]int32{10, 20, 30, 40, 50}
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < N; i++ {
			e := fw.NewEntity()
			flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&vals[i]))
		}
	})

	count := 0
	sum := int32(0)
	w.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynID, func(_ flecs.ID, ptr unsafe.Pointer) {
			count++
			sum += *(*int32)(ptr)
		})
	})

	if count != N {
		t.Errorf("EachByID count: got %d, want %d", count, N)
	}
	wantSum := int32(0)
	for _, v := range vals {
		wantSum += v
	}
	if sum != wantSum {
		t.Errorf("EachByID sum: got %d, want %d", sum, wantSum)
	}
}

// ── Test 10: pointer lifetime (stale after archetype migration) ───────────────

func TestDynamicComponent_PointerLifetime(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/LifetimeDyn", 8, 8)
	})
	flecs.RegisterComponent[dynAuxPos](w)

	src := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	// Obtain pointer before migration.
	var ptr1 unsafe.Pointer
	w.Read(func(fr *flecs.Reader) {
		ptr1 = flecs.GetIDPtr(fr, e, dynID)
	})
	if ptr1 == nil {
		t.Fatal("initial GetIDPtr returned nil")
	}

	// Trigger archetype migration by adding a typed component.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, dynAuxPos{X: 9, Y: 9})
	})

	// Re-obtain pointer after migration; must re-fetch after structural changes.
	// The prior ptr1 is stale and must not be dereferenced after migration.
	var ptr2 unsafe.Pointer
	w.Read(func(fr *flecs.Reader) {
		ptr2 = flecs.GetIDPtr(fr, e, dynID)
	})
	if ptr2 == nil {
		t.Fatal("second GetIDPtr returned nil after migration")
	}
	// The new pointer should read back the original bytes.
	got := make([]byte, 8)
	copy(got, unsafe.Slice((*byte)(ptr2), 8))
	if !bytes.Equal(got, src[:]) {
		t.Errorf("pointer after migration: got %v, want %v", got, src[:])
	}
	// ptr1 may now point into freed or reused memory; document but do NOT dereference.
	t.Logf("ptr1=%p ptr2=%p (ptr1 may be stale after migration)", ptr1, ptr2)
}

// ── Test 11: alignment correctness ───────────────────────────────────────────

func TestDynamicComponent_AlignmentCorrectness(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/AlignDyn", 16, 8)
	})

	src := [16]byte{0xAA, 0xBB}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetIDPtr(fr, e, dynID)
		if ptr == nil {
			t.Fatal("GetIDPtr returned nil")
		}
		if uintptr(ptr)%8 != 0 {
			t.Errorf("pointer alignment: addr=%d, mod 8 = %d (want 0)",
				uintptr(ptr), uintptr(ptr)%8)
		}
	})
}

// ── Test 12: re-registration with the same name panics ────────────────────────

func TestDynamicComponent_ReregistrationPanics(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterDynamicComponent(fw, "test/DupName", 8, 8)
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate dynamic component name, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterDynamicComponent(fw, "test/DupName", 8, 8)
	})
}

// ── Test 13: OnAddByID fires on first add ────────────────────────────────────

func TestDynamicComponent_OnAddByID(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/OnAddDyn", 4, 4)
	})

	addCount := 0
	flecs.OnAddByID(w, dynID, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCount++
	})

	val := int32(7)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})

	if addCount != 1 {
		t.Errorf("OnAdd fired %d times, want 1", addCount)
	}

	// Clear the hook.
	flecs.OnAddByID(w, dynID, nil)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})
	if addCount != 1 {
		t.Errorf("OnAdd should not fire after cleared; got %d", addCount)
	}
}

// ── Test 14: OnRemoveByID fires on removal ────────────────────────────────────

func TestDynamicComponent_OnRemoveByID(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/OnRemoveDyn", 4, 4)
	})

	removeCount := 0
	flecs.OnRemoveByID(w, dynID, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		removeCount++
	})

	val := int32(99)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})
	if removeCount != 0 {
		t.Errorf("OnRemove fired before remove: %d", removeCount)
	}

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, dynID)
	})
	if removeCount != 1 {
		t.Errorf("OnRemove fired %d times after remove, want 1", removeCount)
	}
}

// ── Test 15: GetIDPtr returns nil for unregistered/missing component ──────────

func TestDynamicComponent_GetIDPtr_Miss(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/GetMiss", 8, 8)
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		// Do NOT set the component.
	})

	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetIDPtr(fr, e, dynID)
		if ptr != nil {
			t.Errorf("expected nil for entity without component, got %p", ptr)
		}
	})
}

// ── helpers for nil/error paths ──────────────────────────────────────────────

// fakeID returns an entity ID that is not registered as a component.
func fakeID(w *flecs.World) flecs.ID {
	var fake flecs.ID
	w.Write(func(fw *flecs.Writer) {
		fake = fw.NewEntity()
	})
	return fake
}

// ── Test 16: EachByID on DontFragment (sparse) storage ───────────────────────

func TestDynamicComponent_EachByID_DontFragment(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/EachDF", 4, 4)
	})
	flecs.SetDontFragment(w, dynID)

	vals := []int32{1, 2, 3}
	w.Write(func(fw *flecs.Writer) {
		for _, v := range vals {
			v := v
			e := fw.NewEntity()
			flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&v))
		}
	})

	count := 0
	w.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynID, func(_ flecs.ID, _ unsafe.Pointer) {
			count++
		})
	})
	if count != len(vals) {
		t.Errorf("EachByID DontFragment count: got %d, want %d", count, len(vals))
	}
}

// ── Test 17: nil / miss coverage paths ───────────────────────────────────────

func TestDynamicComponent_NilAndMissPaths(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/NilMiss", 8, 8)
	})

	// GetIDPtr with unregistered ID returns nil.
	unregistered := fakeID(w)
	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetIDPtr(fr, unregistered, unregistered)
		if ptr != nil {
			t.Errorf("expected nil for unregistered component, got %p", ptr)
		}
	})

	// EachByID with unregistered ID is a no-op.
	called := false
	w.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, unregistered, func(_ flecs.ID, _ unsafe.Pointer) {
			called = true
		})
	})
	if called {
		t.Error("EachByID called fn for unregistered component")
	}

	// SetIDPtr with nil src exercises the immediate path (skips deferred queue).
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		// nil src: adds component to archetype without writing a value.
		flecs.SetIDPtr(fw, e, dynID, nil)
	})
	_ = e

	// OnAddByID with unregistered ID panics.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("OnAddByID with unregistered ID should panic")
			}
		}()
		flecs.OnAddByID(w, unregistered, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	}()

	// OnSetByID with unregistered ID panics.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("OnSetByID with unregistered ID should panic")
			}
		}()
		flecs.OnSetByID(w, unregistered, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	}()

	// OnRemoveByID with unregistered ID panics.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("OnRemoveByID with unregistered ID should panic")
			}
		}()
		flecs.OnRemoveByID(w, unregistered, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	}()
	_ = dynID
}

// ── Test 18 (was 17): marshal round-trip with Sparse dynamic component ────────

func TestDynamicComponent_MarshalRoundTrip_Sparse(t *testing.T) {
	const compName = "test/MarshalDynSparse"
	const size = uintptr(8)

	wA := flecs.New()
	var dynID flecs.ID
	wA.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})
	flecs.SetSparse(wA, dynID)

	src := [8]byte{0xFA, 0xCE, 0xB0, 0x0B, 0xDE, 0xAD, 0xC0, 0xDE}
	wA.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	data, err := wA.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	wB := flecs.New()
	var dynIDB flecs.ID
	wB.Write(func(fw *flecs.Writer) {
		dynIDB = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})
	flecs.SetSparse(wB, dynIDB)
	if err := wB.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	found := false
	wB.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynIDB, func(_ flecs.ID, ptr unsafe.Pointer) {
			found = true
			got := make([]byte, size)
			copy(got, unsafe.Slice((*byte)(ptr), size))
			if !bytes.Equal(got, src[:]) {
				t.Errorf("sparse marshal round-trip mismatch: got %v, want %v", got, src[:])
			}
		})
	})
	if !found {
		t.Error("no entities found after sparse marshal round-trip")
	}
}

// ── Test 19: logger path in RegisterDynamicComponent ─────────────────────────

func TestDynamicComponent_RegisterWithLogger(t *testing.T) {
	w := flecs.New()
	w.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/WithLogger", 8, 8)
	})
	if dynID == 0 {
		t.Error("RegisterDynamicComponent returned zero ID with logger set")
	}
}

// ── Test 20: GetIDPtr nil-rec path (dead entity) ──────────────────────────────

func TestDynamicComponent_GetIDPtr_DeadEntity(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/DeadEntityDyn", 8, 8)
	})
	src := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(e)
	})
	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetIDPtr(fr, e, dynID) // dead entity → nil
		if ptr != nil {
			t.Errorf("expected nil for dead entity, got %p", ptr)
		}
	})
}

// ── Test 21: EachByID sparse with no storage (ssOK==false path) ───────────────

func TestDynamicComponent_EachByID_SparseNoStorage(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/EachSparseNoStorage", 4, 4)
	})
	flecs.SetSparse(w, dynID) // sparse but no entity ever holds it
	called := false
	w.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynID, func(_ flecs.ID, _ unsafe.Pointer) {
			called = true
		})
	})
	if called {
		t.Error("EachByID called fn for sparse component with no storage")
	}
}

// ── Test 23: OnSetByID and OnRemoveByID nil clears hook ──────────────────────

func TestDynamicComponent_HookClear(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/HookClear", 4, 4)
	})

	setCalls := 0
	removeCalls := 0
	flecs.OnSetByID(w, dynID, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		setCalls++
	})
	flecs.OnRemoveByID(w, dynID, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		removeCalls++
	})

	val := int32(1)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})
	if setCalls != 1 {
		t.Fatalf("expected 1 OnSet call before clear, got %d", setCalls)
	}

	// Clear both hooks.
	flecs.OnSetByID(w, dynID, nil)
	flecs.OnRemoveByID(w, dynID, nil)

	// Set again — hook should not fire.
	val2 := int32(2)
	w.Write(func(fw *flecs.Writer) {
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val2))
	})
	if setCalls != 1 {
		t.Errorf("OnSet fired after clear: got %d calls, want 1", setCalls)
	}

	// Remove — hook should not fire.
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, dynID)
	})
	if removeCalls != 0 {
		t.Errorf("OnRemove fired after clear: got %d calls, want 0", removeCalls)
	}
}

// ── Test 24: DontFragment dynamic component marshal round-trip (base64) ───────

func TestDynamicComponent_MarshalRoundTrip_DontFragment(t *testing.T) {
	const compName = "test/MarshalDynDFBase64"
	const size = uintptr(8)

	wA := flecs.New()
	var dynID flecs.ID
	wA.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})
	flecs.SetDontFragment(wA, dynID)

	src := [8]byte{0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89}
	wA.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&src[0]))
	})

	data, err := wA.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	wB := flecs.New()
	var dynIDB flecs.ID
	wB.Write(func(fw *flecs.Writer) {
		dynIDB = flecs.RegisterDynamicComponent(fw, compName, size, 8)
	})
	flecs.SetDontFragment(wB, dynIDB)
	if err := wB.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	found := false
	wB.Read(func(fr *flecs.Reader) {
		flecs.EachByID(fr, dynIDB, func(_ flecs.ID, ptr unsafe.Pointer) {
			found = true
			got := make([]byte, size)
			copy(got, unsafe.Slice((*byte)(ptr), size))
			if !bytes.Equal(got, src[:]) {
				t.Errorf("DontFragment marshal round-trip mismatch: got %v, want %v", got, src[:])
			}
		})
	})
	if !found {
		t.Error("no entities found after DontFragment marshal round-trip")
	}
}

// ── Test 25: DontFragment dynamic marshal with custom hook (covers marshal 379-381)
//             and unmarshal error path (covers unmarshal 692-694) ─────────────

func TestDynamicComponent_MarshalRoundTrip_DontFragment_CustomError(t *testing.T) {
	const compName = "test/MarshalDynDFCustomErr"
	const size = uintptr(8)

	marshalFn := func(ptr unsafe.Pointer) (json.RawMessage, error) {
		return json.Marshal("placeholder")
	}
	errUnmarshalFn := func(data json.RawMessage, ptr unsafe.Pointer) error {
		return fmt.Errorf("intentional unmarshal hook error")
	}

	// wA: DontFragment + custom marshaler → produces SparseData with "placeholder".
	wA := flecs.New()
	var dynID flecs.ID
	wA.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponentWithMarshaler(fw, compName, size, 8, marshalFn, errUnmarshalFn)
	})
	flecs.SetDontFragment(wA, dynID)

	var buf [8]byte
	wA.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&buf[0]))
	})

	data, err := wA.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON with custom hook: %v", err)
	}

	// wB: same DontFragment + same error unmarshal hook → expect error from UnmarshalJSON.
	wB := flecs.New()
	var dynIDB flecs.ID
	wB.Write(func(fw *flecs.Writer) {
		dynIDB = flecs.RegisterDynamicComponentWithMarshaler(fw, compName, size, 8, marshalFn, errUnmarshalFn)
	})
	flecs.SetDontFragment(wB, dynIDB)
	if err := wB.UnmarshalJSON(data); err == nil {
		t.Error("expected error from DontFragment custom unmarshal hook, got nil")
	}
}

// ── Test 26: SetIDPtr panics for unregistered component ID ────────────────────

func TestDynamicComponent_SetIDPtr_PanicUnregistered(t *testing.T) {
	w := flecs.New()
	var e, badID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		badID = fw.NewEntity() // not registered as a component
	})
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		val := int32(0)
		w.Write(func(fw *flecs.Writer) {
			flecs.SetIDPtr(fw, e, badID, unsafe.Pointer(&val))
		})
	}()
	if !panicked {
		t.Error("expected panic for unregistered component ID, got none")
	}
}

// ── Test 27: GetByID on a dynamic component returns (nil, false) ──────────────

func TestDynamicComponent_GetByID_DynamicReturnsNil(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/GetByIDDyn", 8, 8)
	})
	var e flecs.ID
	var buf [8]byte
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&buf[0]))
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := fr.GetByID(e, dynID)
		if v != nil || ok {
			t.Errorf("GetByID on dynamic component: got (%v, %v), want (nil, false)", v, ok)
		}
	})
}

// ── Test 28: unmarshalDynamic error paths ─────────────────────────────────────

func TestDynamicComponent_UnmarshalErrors(t *testing.T) {
	const compName = "test/UnmarshalErrDyn"
	const size = uintptr(8)

	// Helper: build baseline JSON from a fresh world with one entity holding the component.
	buildBaseline := func() []byte {
		wA := flecs.New()
		var id flecs.ID
		wA.Write(func(fw *flecs.Writer) {
			id = flecs.RegisterDynamicComponent(fw, compName, size, 8)
		})
		var src [8]byte
		wA.Write(func(fw *flecs.Writer) {
			e := fw.NewEntity()
			flecs.SetIDPtr(fw, e, id, unsafe.Pointer(&src[0]))
		})
		data, err := wA.MarshalJSON()
		if err != nil {
			t.Fatalf("baseline MarshalJSON: %v", err)
		}
		return data
	}

	// Helper: patch the base64 value of compName inside the entity body JSON.
	patchCompValue := func(data []byte, newRaw json.RawMessage) []byte {
		var jw map[string]json.RawMessage
		if err := json.Unmarshal(data, &jw); err != nil {
			t.Fatalf("patchCompValue unmarshal outer: %v", err)
		}
		var entities []map[string]json.RawMessage
		if err := json.Unmarshal(jw["entities"], &entities); err != nil {
			t.Fatalf("patchCompValue unmarshal entities: %v", err)
		}
		for _, ent := range entities {
			var comps map[string]json.RawMessage
			if _, ok := ent["components"]; !ok {
				continue
			}
			if err := json.Unmarshal(ent["components"], &comps); err != nil {
				continue
			}
			if _, ok := comps[compName]; ok {
				comps[compName] = newRaw
				compsJSON, _ := json.Marshal(comps)
				ent["components"] = compsJSON
			}
		}
		entJSON, _ := json.Marshal(entities)
		jw["entities"] = entJSON
		out, _ := json.Marshal(jw)
		return out
	}

	makeWorld := func() *flecs.World {
		ww := flecs.New()
		ww.Write(func(fw *flecs.Writer) {
			flecs.RegisterDynamicComponent(fw, compName, size, 8)
		})
		return ww
	}

	data := buildBaseline()

	// 1. Component value is not a JSON string → json.Unmarshal into string fails (line 728-730).
	t.Run("notString", func(t *testing.T) {
		corrupt := patchCompValue(data, json.RawMessage(`123`))
		if err := makeWorld().UnmarshalJSON(corrupt); err == nil {
			t.Error("expected error for non-string component value, got nil")
		}
	})

	// 2. Component value is a valid JSON string but invalid base64 (line 732-734).
	t.Run("invalidBase64", func(t *testing.T) {
		corrupt := patchCompValue(data, json.RawMessage(`"!@#$%^&*()`+"`"+`"`))
		if err := makeWorld().UnmarshalJSON(corrupt); err == nil {
			t.Error("expected error for invalid base64, got nil")
		}
	})

	// 3. Component value is valid base64 but decodes to wrong byte count (line 735-737).
	t.Run("sizeMismatch", func(t *testing.T) {
		// 4 bytes encoded → "AAAAAA==" — component expects 8.
		wrongB64, _ := json.Marshal(base64.StdEncoding.EncodeToString(make([]byte, 4)))
		corrupt := patchCompValue(data, wrongB64)
		if err := makeWorld().UnmarshalJSON(corrupt); err == nil {
			t.Error("expected error for size mismatch, got nil")
		}
	})

	// 4. Custom unmarshal hook returns error (line 721-723).
	t.Run("customHookError", func(t *testing.T) {
		const customName = "test/UnmarshalCustomHookErr"
		errFn := func(raw json.RawMessage, ptr unsafe.Pointer) error {
			return fmt.Errorf("hook error")
		}
		okMarshal := func(ptr unsafe.Pointer) (json.RawMessage, error) {
			return json.Marshal("ok")
		}

		wA := flecs.New()
		var id flecs.ID
		wA.Write(func(fw *flecs.Writer) {
			id = flecs.RegisterDynamicComponentWithMarshaler(fw, customName, size, 8, okMarshal, errFn)
		})
		var src [8]byte
		wA.Write(func(fw *flecs.Writer) {
			e := fw.NewEntity()
			flecs.SetIDPtr(fw, e, id, unsafe.Pointer(&src[0]))
		})
		marshaledData, err := wA.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}

		wB := flecs.New()
		wB.Write(func(fw *flecs.Writer) {
			flecs.RegisterDynamicComponentWithMarshaler(fw, customName, size, 8, okMarshal, errFn)
		})
		if err := wB.UnmarshalJSON(marshaledData); err == nil {
			t.Error("expected error from custom unmarshal hook, got nil")
		}
	})
}
