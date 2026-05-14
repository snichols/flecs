package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types for entity_scope tests.
type ESTag struct{}

// Test 1: Basic WithinScope — NewEntity inside scope auto-gets (ChildOf, parent).
func TestEntityScopeBasic(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			child = fw.NewEntity()
		})
	})
	got, ok := w.ParentOf(child)
	if !ok || got != parent {
		t.Fatalf("expected child parent=%v, got %v ok=%v", parent, got, ok)
	}
}

// Test 2: Nested WithinScope — inner scope wins; outer restored on return.
func TestEntityScopeNested(t *testing.T) {
	w := flecs.New()
	var parent1, parent2, childOuter, childInner flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent1 = fw.NewEntity()
		parent2 = fw.NewEntity()
		flecs.WithinScope(fw, parent1, func(fw *flecs.Writer) {
			flecs.WithinScope(fw, parent2, func(fw *flecs.Writer) {
				childInner = fw.NewEntity()
			})
			childOuter = fw.NewEntity()
		})
	})
	if got, ok := w.ParentOf(childInner); !ok || got != parent2 {
		t.Fatalf("childInner: expected parent2=%v, got %v ok=%v", parent2, got, ok)
	}
	if got, ok := w.ParentOf(childOuter); !ok || got != parent1 {
		t.Fatalf("childOuter: expected parent1=%v, got %v ok=%v", parent1, got, ok)
	}
}

// Test 3: Pop restores — NewEntity outside any scope has no auto-ChildOf.
func TestEntityScopePopRestores(t *testing.T) {
	w := flecs.New()
	var parent, before, after flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		before = fw.NewEntity() // outside scope
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			_ = fw.NewEntity() // inside scope (ignored here)
		})
		after = fw.NewEntity() // outside scope again
	})
	if _, ok := w.ParentOf(before); ok {
		t.Fatal("before: should have no parent")
	}
	if _, ok := w.ParentOf(after); ok {
		t.Fatal("after: should have no parent after scope exited")
	}
}

// Test 4: Explicit PushScope/PopScope works like WithinScope.
func TestEntityScopeExplicitPushPop(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		prev := flecs.PushScope(fw, parent)
		child = fw.NewEntity()
		flecs.PopScope(fw, prev)
	})
	if got, ok := w.ParentOf(child); !ok || got != parent {
		t.Fatalf("expected parent=%v, got %v ok=%v", parent, got, ok)
	}
}

// Test 5: Mismatched PopScope panics with a message.
func TestEntityScopePopMismatch(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()
		flecs.PushScope(fw, parent)
		// Pass wrong prev value (not what PushScope returned).
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on mismatched PopScope")
			}
		}()
		flecs.PopScope(fw, parent) // wrong: should pass 0 (the value PushScope returned)
	})
}

// Test 6: Panic inside fn — scope is still popped via defer.
func TestEntityScopePanicInFn(t *testing.T) {
	w := flecs.New()
	var parent, after flecs.ID
	func() {
		defer func() { recover() }() //nolint:errcheck
		w.Write(func(fw *flecs.Writer) {
			parent = fw.NewEntity()
			flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
				panic("deliberate test panic")
			})
		})
	}()
	// After the panic (recovered), scope must be empty.
	// A new top-level Write resets the stack; NewEntity must have no parent.
	w.Write(func(fw *flecs.Writer) {
		after = fw.NewEntity()
	})
	if _, ok := w.ParentOf(after); ok {
		t.Fatal("after panic recovery: entity should have no parent (scope must be reset)")
	}
}

// Test 7: GetScope returns current top inside, 0 outside; 0 on a Reader.
func TestEntityScopeGetScope(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	var insideScope, outsideScope flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		outsideScope = flecs.GetScope(fw)
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			insideScope = flecs.GetScope(fw)
		})
	})
	if outsideScope != 0 {
		t.Fatalf("GetScope outside scope: expected 0, got %v", outsideScope)
	}
	if insideScope != parent {
		t.Fatalf("GetScope inside scope: expected %v, got %v", parent, insideScope)
	}
	w.Read(func(r *flecs.Reader) {
		if got := flecs.GetScope(r); got != 0 {
			t.Fatalf("GetScope on Reader: expected 0, got %v", got)
		}
	})
}

// Test 8: MakeAlive ignores scope — explicit ID claim bypasses auto-ChildOf.
// MakeAlive panics in a deferred scope, so we use WriterForTest (deferDepth==0).
func TestEntityScopeMakeAliveIgnoresScope(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
	})
	fw := flecs.WriterForTest(w)
	prev := flecs.PushScope(fw, parent)
	claimed := flecs.MakeAlive(fw, flecs.ID(9999))
	flecs.PopScope(fw, prev)
	if _, ok := w.ParentOf(claimed); ok {
		t.Fatal("MakeAlive inside scope: entity must NOT auto-get (ChildOf, scope)")
	}
}

// Test 9: RangeNew respects scope — range-allocated entity gets auto-ChildOf.
// RangeNew panics in a deferred scope, so we use WriterForTest (deferDepth==0).
func TestEntityScopeRangeNewRespects(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
	})
	fw := flecs.WriterForTest(w)
	prev := flecs.PushScope(fw, parent)
	child := flecs.RangeNew(fw, 5000, 6000)
	flecs.PopScope(fw, prev)
	if got, ok := w.ParentOf(child); !ok || got != parent {
		t.Fatalf("RangeNew inside scope: expected parent=%v, got %v ok=%v", parent, got, ok)
	}
}

// Test 10: OrderedChildren + scope — scope-created children appear in insertion order.
func TestEntityScopeOrderedChildren(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			c1 = fw.NewEntity()
			c2 = fw.NewEntity()
			c3 = fw.NewEntity()
		})
	})
	var order []flecs.ID
	w.EachChild(parent, func(child flecs.ID) bool {
		order = append(order, child)
		return true
	})
	if len(order) != 3 || order[0] != c1 || order[1] != c2 || order[2] != c3 {
		t.Fatalf("OrderedChildren: expected [c1 c2 c3], got %v", order)
	}
}

// Test 11: Deferred path — scope-created entities get auto-ChildOf via the coalescer.
func TestEntityScopeDeferredPath(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	// All NewEntity calls inside w.Write are deferred (deferDepth > 0).
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			child = fw.NewEntity()
		})
	}) // flush happens here
	if got, ok := w.ParentOf(child); !ok || got != parent {
		t.Fatalf("deferred: expected parent=%v, got %v ok=%v", parent, got, ok)
	}
}

// Test 12: Stack reset across Write boundaries — leftover scope does not leak.
func TestEntityScopeStackResetAcrossWrite(t *testing.T) {
	w := flecs.New()
	var parent, leaked flecs.ID
	// First Write: push a scope but do NOT pop (simulating a leftover).
	// WithinScope pops on exit, so we test by using PushScope without PopScope.
	// Since Write resets the stack at top-level entry, any leftover is wiped.
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.PushScope(fw, parent) // intentionally not popped
	})
	// Second Write: scope stack should be clean.
	var scopeAtStart flecs.ID
	w.Write(func(fw *flecs.Writer) {
		scopeAtStart = flecs.GetScope(fw)
		leaked = fw.NewEntity()
	})
	if scopeAtStart != 0 {
		t.Fatalf("scope stack leaked across Write boundaries: GetScope=%v", scopeAtStart)
	}
	if _, ok := w.ParentOf(leaked); ok {
		t.Fatal("entity created in second Write got a parent from previous Write's scope")
	}
}

// Test 13: Recursive WithinScope with same parent — stack grows and pops correctly.
func TestEntityScopeRecursiveSameParent(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
			c1 = fw.NewEntity()
			flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
				c2 = fw.NewEntity()
			})
			// Scope should still be parent after inner WithinScope returns.
			if got := flecs.GetScope(fw); got != parent {
				t.Errorf("after inner WithinScope(parent): expected GetScope=%v, got %v", parent, got)
			}
		})
		if got := flecs.GetScope(fw); got != 0 {
			t.Errorf("after all WithinScope: expected GetScope=0, got %v", got)
		}
	})
	if got, ok := w.ParentOf(c1); !ok || got != parent {
		t.Fatalf("c1: expected parent=%v, got %v ok=%v", parent, got, ok)
	}
	if got, ok := w.ParentOf(c2); !ok || got != parent {
		t.Fatalf("c2: expected parent=%v, got %v ok=%v", parent, got, ok)
	}
}
