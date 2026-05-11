package table

import (
	"reflect"
	"runtime"
	"unsafe"
)

const initialCap = 8

// Column stores one component type for all rows in a Table using a typed
// reflect.Value (kind Slice). The GC traces pointer-containing components
// (e.g. struct{ S string }) correctly because the backing store is typed.
// Using []byte would hide GC pointers and silently corrupt such components.
//
// PtrAt returns a pointer to the element at row. The pointer is stable until
// the next appendZero that causes growth (capacity exceeded) or a removeSwap
// that may copy data. After such an operation, re-obtain via PtrAt or Get.
//
// Set and Get copy Size bytes using unsafe byte slices. This is safe because
// the bytes represent a typed value of the same Go type the slice was made with.
//
// Zero-size components (tags) do not get a Column; all methods on a nil
// *Column are no-ops (Len and Cap return 0).
//
// Implementation note: c.slice always has Len() == Cap() (the full backing
// array). Logical row count is tracked in c.n to avoid reflect.Value.Slice()
// calls in appendZero and removeSwap, each of which would allocate a heap
// object per call.
type Column struct {
	slice reflect.Value // kind Slice; Len() == Cap() == backing capacity
	size  uintptr       // unsafe.Sizeof of element
	n     int           // logical row count (0 ≤ n ≤ slice.Len())
}

func newColumn(elemType reflect.Type, size uintptr) *Column {
	// Allocate at full capacity so Len() == Cap() from the start.
	return &Column{
		slice: reflect.MakeSlice(reflect.SliceOf(elemType), initialCap, initialCap),
		size:  size,
	}
}

// Len returns the number of elements in the column.
func (c *Column) Len() int {
	if c == nil {
		return 0
	}
	return c.n
}

// Cap returns the capacity of the column.
func (c *Column) Cap() int {
	if c == nil {
		return 0
	}
	return c.slice.Len() // c.slice.Len() == c.slice.Cap() always
}

// PtrAt returns an unsafe pointer to the element at row.
// Stable until the next appendZero that grows the column or a removeSwap.
func (c *Column) PtrAt(row int) unsafe.Pointer {
	ptr := unsafe.Pointer(c.slice.Index(row).UnsafeAddr())
	runtime.KeepAlive(c.slice)
	return ptr
}

// Set copies size bytes from src into the column slot at row.
func (c *Column) Set(row int, src unsafe.Pointer) {
	if c == nil || c.size == 0 {
		return
	}
	dst := c.PtrAt(row)
	copy(unsafe.Slice((*byte)(dst), c.size), unsafe.Slice((*byte)(src), c.size))
}

// Get copies size bytes from the column slot at row into dst.
func (c *Column) Get(row int, dst unsafe.Pointer) {
	if c == nil || c.size == 0 {
		return
	}
	src := c.PtrAt(row)
	copy(unsafe.Slice((*byte)(dst), c.size), unsafe.Slice((*byte)(src), c.size))
}

// appendZero extends the column by one zero-initialized element.
// When n == cap, capacity is doubled (minimum initialCap).
// Hot path: never calls reflect.Value.Slice (which allocates a heap object).
func (c *Column) appendZero() {
	if c.n == c.slice.Len() { // n == cap → grow
		newCap := c.slice.Len() * 2
		if newCap < initialCap {
			newCap = initialCap
		}
		grown := reflect.MakeSlice(c.slice.Type(), newCap, newCap)
		reflect.Copy(grown, c.slice)
		c.slice = grown
	}
	// Zero the newly exposed slot; it may hold stale data from a prior removeSwap.
	c.slice.Index(c.n).Set(reflect.Zero(c.slice.Type().Elem()))
	c.n++
}

// BaseUnsafe returns an unsafe pointer to element 0 of the backing array and
// the element's reflect.Type. Returns (nil, nil) for a nil column; returns
// (nil, elemType) when the column is empty (no rows yet).
//
// The pointer is derived in the same expression as UnsafeAddr (rule 6 of the
// unsafe.Pointer rules) and is therefore safe to convert immediately to a typed
// *T via unsafe.Slice. The column's reflect.Value slice keeps the backing array
// alive as long as the Column itself is reachable; callers must not hold the
// returned pointer past any Append or RemoveSwap call.
func (c *Column) BaseUnsafe() (unsafe.Pointer, reflect.Type) {
	if c == nil {
		return nil, nil
	}
	elemType := c.slice.Type().Elem()
	if c.n == 0 {
		return nil, elemType
	}
	// Convert in one expression per the unsafe.Pointer rules for UnsafeAddr.
	ptr := unsafe.Pointer(c.slice.Index(0).UnsafeAddr()) //nolint:unsafeptr
	runtime.KeepAlive(c.slice)
	return ptr, elemType
}

// removeSwap overwrites slot row with the last element, then decrements n.
// If row == n-1, just decrements. Zeros the vacated last slot so the GC
// can collect any pointer-containing component values that were there.
// Hot path: never calls reflect.Value.Slice (which allocates a heap object).
func (c *Column) removeSwap(row int) {
	last := c.n - 1
	if row != last {
		c.slice.Index(row).Set(c.slice.Index(last))
	}
	// Zero before decrement so GC can reclaim pointers in the last slot.
	c.slice.Index(last).Set(reflect.Zero(c.slice.Type().Elem()))
	c.n--
}
