package table

import (
	"reflect"
	"testing"
	"unsafe"
)

type colPos struct{ X, Y float32 }

func TestColumnInitial(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	if c.Len() != 0 {
		t.Fatalf("initial Len want 0, got %d", c.Len())
	}
	if c.Cap() < initialCap {
		t.Fatalf("initial Cap want >= %d, got %d", initialCap, c.Cap())
	}
}

func TestColumnNilLenCap(t *testing.T) {
	var c *Column
	if c.Len() != 0 {
		t.Fatal("nil Column.Len() should return 0")
	}
	if c.Cap() != 0 {
		t.Fatal("nil Column.Cap() should return 0")
	}
}

func TestColumnAppendZeroIsZero(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	c.appendZero()
	if c.Len() != 1 {
		t.Fatalf("Len after appendZero want 1, got %d", c.Len())
	}
	p := (*colPos)(c.PtrAt(0))
	if p.X != 0 || p.Y != 0 {
		t.Fatalf("zero element not zeroed: %+v", *p)
	}
}

func TestColumnSetGet(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	c.appendZero()
	val := colPos{X: 1.5, Y: 2.5}
	c.Set(0, unsafe.Pointer(&val))

	var out colPos
	c.Get(0, unsafe.Pointer(&out))
	if out != val {
		t.Fatalf("Get returned %+v, want %+v", out, val)
	}
}

func TestColumnGrowthDoubling(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	const n = 64
	for i := 0; i < n; i++ {
		c.appendZero()
		v := colPos{X: float32(i), Y: float32(i * 10)}
		c.Set(i, unsafe.Pointer(&v))
	}
	if c.Len() != n {
		t.Fatalf("Len want %d, got %d", n, c.Len())
	}
	for i := 0; i < n; i++ {
		var out colPos
		c.Get(i, unsafe.Pointer(&out))
		want := colPos{X: float32(i), Y: float32(i * 10)}
		if out != want {
			t.Fatalf("row %d: got %+v, want %+v", i, out, want)
		}
	}
}

func TestColumnRemoveSwapMiddle(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	for i := 0; i < 3; i++ {
		c.appendZero()
		v := colPos{X: float32(i + 1)}
		c.Set(i, unsafe.Pointer(&v))
	}
	// values: [{1},{2},{3}] → removeSwap(0) → [{3},{2}]
	c.removeSwap(0)
	if c.Len() != 2 {
		t.Fatalf("Len after removeSwap(0) want 2, got %d", c.Len())
	}
	var out colPos
	c.Get(0, unsafe.Pointer(&out))
	if out.X != 3 {
		t.Fatalf("after removeSwap(0), row 0 X want 3, got %g", out.X)
	}
	c.Get(1, unsafe.Pointer(&out))
	if out.X != 2 {
		t.Fatalf("after removeSwap(0), row 1 X want 2, got %g", out.X)
	}
}

func TestColumnRemoveSwapLast(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	for i := 0; i < 2; i++ {
		c.appendZero()
		v := colPos{X: float32(i + 1)}
		c.Set(i, unsafe.Pointer(&v))
	}
	c.removeSwap(1)
	if c.Len() != 1 {
		t.Fatalf("Len after removeSwap(last) want 1, got %d", c.Len())
	}
	var out colPos
	c.Get(0, unsafe.Pointer(&out))
	if out.X != 1 {
		t.Fatalf("row 0 X want 1, got %g", out.X)
	}
}

func TestColumnRemoveSwapOnly(t *testing.T) {
	c := newColumn(reflect.TypeFor[colPos](), unsafe.Sizeof(colPos{}))
	c.appendZero()
	v := colPos{X: 99}
	c.Set(0, unsafe.Pointer(&v))
	c.removeSwap(0)
	if c.Len() != 0 {
		t.Fatalf("Len after removing only element want 0, got %d", c.Len())
	}
}

func TestColumnStaleSlotZeroedAfterRemove(t *testing.T) {
	// Confirms that a slot beyond len is zeroed after removeSwap and stays zero
	// when reused by appendZero (important for GC pointer tracing).
	type withPtr struct{ S string }
	c := newColumn(reflect.TypeFor[withPtr](), unsafe.Sizeof(withPtr{}))
	c.appendZero()
	v := withPtr{S: "hello"}
	c.Set(0, unsafe.Pointer(&v))
	c.removeSwap(0)
	// Extend back into the same slot — should be zero, not "hello"
	c.appendZero()
	var out withPtr
	c.Get(0, unsafe.Pointer(&out))
	if out.S != "" {
		t.Fatalf("reused slot should be zero, got S=%q", out.S)
	}
}
