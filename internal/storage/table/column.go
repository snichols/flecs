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
type Column struct {
	slice reflect.Value // kind Slice; element type == component type
	size  uintptr       // unsafe.Sizeof of element
}

func newColumn(elemType reflect.Type, size uintptr) *Column {
	return &Column{
		slice: reflect.MakeSlice(reflect.SliceOf(elemType), 0, initialCap),
		size:  size,
	}
}

// Len returns the number of elements in the column.
func (c *Column) Len() int {
	if c == nil {
		return 0
	}
	return c.slice.Len()
}

// Cap returns the capacity of the column.
func (c *Column) Cap() int {
	if c == nil {
		return 0
	}
	return c.slice.Cap()
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
// When len == cap, capacity is doubled (minimum initialCap).
func (c *Column) appendZero() {
	n := c.slice.Len()
	if n == c.slice.Cap() {
		newCap := c.slice.Cap() * 2
		if newCap < initialCap {
			newCap = initialCap
		}
		grown := reflect.MakeSlice(c.slice.Type(), n+1, newCap)
		reflect.Copy(grown, c.slice)
		c.slice = grown
		return
	}
	c.slice = c.slice.Slice(0, n+1)
	// Zero the newly exposed slot; it may hold stale data from a prior removeSwap.
	c.slice.Index(n).Set(reflect.Zero(c.slice.Type().Elem()))
}

// removeSwap overwrites slot row with the last element, then truncates by one.
// If row == Len()-1, just truncates. Zeros the vacated last slot so the GC
// can collect any pointer-containing component values that were there.
func (c *Column) removeSwap(row int) {
	last := c.slice.Len() - 1
	if row != last {
		c.slice.Index(row).Set(c.slice.Index(last))
	}
	// Zero before truncation so GC can reclaim pointers in the last slot.
	c.slice.Index(last).Set(reflect.Zero(c.slice.Type().Elem()))
	c.slice = c.slice.Slice(0, last)
}
