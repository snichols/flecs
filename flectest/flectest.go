// Package flectest provides testing.TB-aware helpers that make ECS code easy to
// test. Assertion helpers produce richly-messaged failure output; lifecycle
// helpers manage world creation and cleanup; golden-snapshot helpers enable
// deterministic regression tests.
//
// # Design: why *flecs.World, not *Reader/*Writer
//
// The root flecs package exposes an unexported scope interface satisfied by
// *Reader and *Writer. External packages cannot name it, so flectest helpers
// cannot accept a scope parameter. Instead, every assertion helper accepts a
// *flecs.World and opens a Read scope internally via w.Read(func(*flecs.Reader)
// { … }). This gives callers the simplest possible call-site signature and
// mirrors the stdlib net/http/httptest and testing/fstest subpackage pattern.
//
// # Lifecycle
//
// [NewWorld] and [NewWorldWith] register a no-op tb.Cleanup closure. flecs.World
// has no Close/Free/Destroy method; the GC reclaims it. The Cleanup is a
// forward-compatibility placeholder: if a teardown method is added in the future,
// only this package needs to change.
//
// # Mock-TB pattern
//
// To test flectest helpers themselves (asserting that Fatalf is called on
// failure), embed testing.TB as an interface field left nil and override only
// the methods actually called (Helper, Fatalf, Errorf, Cleanup). The embedded
// nil satisfies the unexported private() method requirement at compile time;
// the overridden methods are the only ones ever called in tests.
//
// # Golden snapshots and the -update flag
//
// [AssertSnapshotGolden] compares the world's marshaled JSON against a file on
// disk. Pass -update on the test command line to rewrite golden files instead
// of comparing. [RequireRoundTrip] verifies that MarshalJSON → New() +
// UnmarshalJSON → MarshalJSON produces identical bytes, using the JSON path
// rather than the binary snapshot path. Binary snapshots are world-identity-
// bound (unsafe.Pointer token); RestoreSnapshot panics on cross-world restore,
// making them unsuitable for round-trip testing into a fresh world.
package flectest

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// update, when true, rewrites golden files instead of comparing against them.
// Activated with -update on the test command line.
var update = flag.Bool("update", false, "rewrite flectest golden files")

// ── Lifecycle helpers ─────────────────────────────────────────────────────────

// NewWorld creates a fresh *flecs.World bound to tb's lifetime.
// A no-op tb.Cleanup is registered as a forward-compatibility placeholder;
// the GC reclaims the world (flecs.World has no Close/Free/Destroy method).
func NewWorld(tb testing.TB) *flecs.World {
	tb.Helper()
	w := flecs.New()
	tb.Cleanup(func() {
		// no-op: flecs.World has no Close/Free/Destroy; GC reclaims it.
	})
	return w
}

// NewWorldWith creates a fresh *flecs.World, runs setup inside w.Write, and
// returns the populated world bound to tb's lifetime.
func NewWorldWith(tb testing.TB, setup func(fw *flecs.Writer)) *flecs.World {
	tb.Helper()
	w := NewWorld(tb)
	w.Write(setup)
	return w
}

// ── Assertion helpers ─────────────────────────────────────────────────────────

// AssertAlive fails tb if e is not alive in w.
func AssertAlive(tb testing.TB, w *flecs.World, e flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		if !fr.IsAlive(e) {
			tb.Fatalf("AssertAlive: entity %d: got not-alive, want alive", e)
		}
	})
}

// AssertNotAlive fails tb if e is alive in w.
func AssertNotAlive(tb testing.TB, w *flecs.World, e flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		if fr.IsAlive(e) {
			tb.Fatalf("AssertNotAlive: entity %d: got alive, want not-alive", e)
		}
	})
}

// AssertHasComponent fails tb if entity e does not have component T in w.
func AssertHasComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		if !flecs.Has[T](fr, e) {
			tb.Fatalf("AssertHasComponent[%T]: entity %d: got no component, want present", *new(T), e)
		}
	})
}

// AssertNoComponent fails tb if entity e has component T in w.
func AssertNoComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		if flecs.Has[T](fr, e) {
			tb.Fatalf("AssertNoComponent[%T]: entity %d: got component present, want absent", *new(T), e)
		}
	})
}

// AssertComponentValue fails tb if entity e does not have component T with
// value equal to want. The failure message includes both the got and want values.
func AssertComponentValue[T comparable](tb testing.TB, w *flecs.World, e flecs.ID, want T) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got, ok := flecs.Get[T](fr, e)
		if !ok {
			tb.Fatalf("AssertComponentValue[%T]: entity %d: component absent; want %+v", want, e, want)
			return
		}
		if got != want {
			tb.Fatalf("AssertComponentValue[%T]: entity %d: got %+v, want %+v", want, e, got, want)
		}
	})
}

// AssertComponentValueFunc fails tb if entity e does not have component T or
// check(value) returns false. The failure message includes desc and the actual
// component value.
func AssertComponentValueFunc[T any](tb testing.TB, w *flecs.World, e flecs.ID, check func(T) bool, desc string) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got, ok := flecs.Get[T](fr, e)
		if !ok {
			tb.Fatalf("AssertComponentValueFunc[%T]: entity %d: component absent (%s)", *new(T), e, desc)
			return
		}
		if !check(got) {
			tb.Fatalf("AssertComponentValueFunc[%T]: entity %d: check failed (%s); got %+v", *new(T), e, desc, got)
		}
	})
}

// AssertEntityCount fails tb if the number of alive entities in w differs from want.
func AssertEntityCount(tb testing.TB, w *flecs.World, want int) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got := fr.Count()
		if got != want {
			tb.Fatalf("AssertEntityCount: got %d alive, want %d", got, want)
		}
	})
}

// AssertQueryCount fails tb if the number of entities matched by q differs from want.
func AssertQueryCount(tb testing.TB, w *flecs.World, q *flecs.Query, want int) {
	tb.Helper()
	w.Read(func(_ *flecs.Reader) {
		it := q.Iter()
		got := 0
		for it.Next() {
			got += it.Count()
		}
		if got != want {
			tb.Fatalf("AssertQueryCount: got %d matched, want %d", got, want)
		}
	})
}

// AssertParent fails tb if the parent of child is not wantParent.
func AssertParent(tb testing.TB, w *flecs.World, child, wantParent flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got, ok := fr.ParentOf(child)
		if !ok {
			tb.Fatalf("AssertParent: entity %d: has no parent; want parent %d", child, wantParent)
			return
		}
		if got != wantParent {
			tb.Fatalf("AssertParent: entity %d: got parent %d, want parent %d", child, got, wantParent)
		}
	})
}

// AssertNoParent fails tb if child has a parent in w.
func AssertNoParent(tb testing.TB, w *flecs.World, child flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got, ok := fr.ParentOf(child)
		if ok {
			tb.Fatalf("AssertNoParent: entity %d: got parent %d, want no parent", child, got)
		}
	})
}

// AssertChildren fails tb if the set of direct children of parent does not
// equal wantChildren (order-independent set equality). The failure message lists
// missing and extra IDs.
func AssertChildren(tb testing.TB, w *flecs.World, parent flecs.ID, wantChildren ...flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got := make(map[flecs.ID]struct{})
		fr.EachChild(parent, func(c flecs.ID) bool {
			got[c] = struct{}{}
			return true
		})
		want := make(map[flecs.ID]struct{}, len(wantChildren))
		for _, c := range wantChildren {
			want[c] = struct{}{}
		}
		var missing, extra []flecs.ID
		for c := range want {
			if _, ok := got[c]; !ok {
				missing = append(missing, c)
			}
		}
		for c := range got {
			if _, ok := want[c]; !ok {
				extra = append(extra, c)
			}
		}
		if len(missing) == 0 && len(extra) == 0 {
			return
		}
		sortIDs(missing)
		sortIDs(extra)
		tb.Fatalf("AssertChildren: parent %d: missing=%v extra=%v", parent, missing, extra)
	})
}

// AssertHasPair fails tb if entity e does not have the pair (rel, target) in w.
func AssertHasPair(tb testing.TB, w *flecs.World, e, rel, target flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		pairID := flecs.MakePair(rel, target)
		if !flecs.HasID(fr, e, pairID) {
			tb.Fatalf("AssertHasPair: entity %d: missing pair (rel=%d, tgt=%d)", e, rel, target)
		}
	})
}

// AssertName fails tb if the name of e is not wantName.
func AssertName(tb testing.TB, w *flecs.World, e flecs.ID, wantName string) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		got, ok := fr.GetName(e)
		if !ok {
			tb.Fatalf("AssertName: entity %d: no name; want %q", e, wantName)
			return
		}
		if got != wantName {
			tb.Fatalf("AssertName: entity %d: got name %q, want %q", e, got, wantName)
		}
	})
}

// AssertTag fails tb if entity e does not have tag in w.
func AssertTag(tb testing.TB, w *flecs.World, e, tag flecs.ID) {
	tb.Helper()
	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, tag) {
			tb.Fatalf("AssertTag: entity %d: missing tag %d", e, tag)
		}
	})
}

// ── Golden snapshot helpers ───────────────────────────────────────────────────

// AssertSnapshotGolden compares w.MarshalJSON() against the file at goldenPath.
// When -update is passed on the test command line, it writes the current JSON
// to goldenPath (creating parent directories as needed) instead of comparing.
// The JSON is normalized (compact re-marshal) before writing or comparing to
// produce stable diffs regardless of key order.
func AssertSnapshotGolden(tb testing.TB, w *flecs.World, goldenPath string) {
	tb.Helper()
	raw, err := w.MarshalJSON()
	if err != nil {
		tb.Fatalf("AssertSnapshotGolden: MarshalJSON: %v", err)
		return
	}
	norm := mustNormalizeJSON(raw)
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			tb.Fatalf("AssertSnapshotGolden: mkdir: %v", err)
			return
		}
		if err := os.WriteFile(goldenPath, norm, 0o644); err != nil {
			tb.Fatalf("AssertSnapshotGolden: write golden: %v", err)
			return
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		tb.Fatalf("AssertSnapshotGolden: read golden %q: %v (run with -update to create)", goldenPath, err)
		return
	}
	if !bytes.Equal(norm, want) {
		tb.Fatalf("AssertSnapshotGolden: snapshot mismatch\ngot:\n%s\nwant:\n%s", norm, want)
	}
}

// RequireRoundTrip verifies that w survives a JSON round-trip:
// MarshalJSON → flecs.New() + UnmarshalJSON → MarshalJSON must produce
// identical bytes. The binary snapshot path (TakeSnapshot/RestoreSnapshot) is
// NOT used here: binary snapshots are bound to their origin world via an
// unsafe.Pointer identity token and RestoreSnapshot panics when the world
// differs, making cross-world round-trip testing impossible. The JSON path has
// no such restriction.
//
// Note: UnmarshalJSON requires component types to be pre-registered in the
// destination world. RequireRoundTrip copies component registrations from w to
// w2 using RegisterComponentByType before unmarshaling.
func RequireRoundTrip(tb testing.TB, w *flecs.World) {
	tb.Helper()
	b1, err := w.MarshalJSON()
	if err != nil {
		tb.Fatalf("RequireRoundTrip: MarshalJSON: %v", err)
		return
	}
	w2 := copyWorldRegistrations(w)
	if err := w2.UnmarshalJSON(b1); err != nil {
		tb.Fatalf("RequireRoundTrip: UnmarshalJSON: %v", err)
		return
	}
	b2, err := w2.MarshalJSON()
	if err != nil {
		tb.Fatalf("RequireRoundTrip: MarshalJSON (w2): %v", err)
		return
	}
	if !bytes.Equal(mustNormalizeJSON(b1), mustNormalizeJSON(b2)) {
		tb.Fatalf("RequireRoundTrip: round-trip mismatch\nb1:\n%s\nb2:\n%s", b1, b2)
	}
}

// copyWorldRegistrations creates a fresh world with all typed component
// registrations from src copied in. Used by RequireRoundTrip so that
// UnmarshalJSON can resolve component names in the JSON.
func copyWorldRegistrations(src *flecs.World) *flecs.World {
	var compTypes []reflect.Type
	src.Read(func(fr *flecs.Reader) {
		for _, cid := range fr.Components() {
			info, ok := fr.ComponentInfo(cid)
			if ok && info.Type != nil {
				compTypes = append(compTypes, info.Type)
			}
		}
	})
	dst := flecs.New()
	dst.Write(func(_ *flecs.Writer) {
		for _, t := range compTypes {
			flecs.RegisterComponentByType(dst, t)
		}
	})
	return dst
}

// ── Helper builders ───────────────────────────────────────────────────────────

// MustEntity creates a new named entity in w, writes each element of comps as
// a component (auto-registering its type if needed), and returns the entity ID.
// Callers do not need to pre-register component types; MustEntity does so
// via reflection.
func MustEntity(tb testing.TB, w *flecs.World, name string, comps ...any) flecs.ID {
	tb.Helper()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.SetName(e, name)
		for _, c := range comps {
			t := reflect.TypeOf(c)
			id := flecs.RegisterComponentByType(w, t)
			fw.SetByID(e, id, c)
		}
	})
	return e
}

// MustChild is like MustEntity but also establishes a (ChildOf, parent)
// relationship on the new entity.
func MustChild(tb testing.TB, w *flecs.World, parent flecs.ID, name string, comps ...any) flecs.ID {
	tb.Helper()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.SetName(e, name)
		flecs.AddID(fw, e, flecs.MakePair(w.ChildOf(), parent))
		for _, c := range comps {
			t := reflect.TypeOf(c)
			id := flecs.RegisterComponentByType(w, t)
			fw.SetByID(e, id, c)
		}
	})
	return e
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func sortIDs(ids []flecs.ID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

// normalizeJSON re-marshals raw JSON for stable comparison (compact, sorted keys).
// Returns an error only when raw is not valid JSON; the re-marshal step cannot
// fail because the intermediate value is always JSON-representable.
func normalizeJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	out, _ := json.Marshal(v) // can't fail: v was decoded from JSON
	return out, nil
}

// mustNormalizeJSON is like normalizeJSON but panics on error.
// Used where input is the output of World.MarshalJSON and therefore
// guaranteed to be valid JSON.
func mustNormalizeJSON(raw []byte) []byte {
	out, err := normalizeJSON(raw)
	if err != nil {
		panic(fmt.Sprintf("flectest: mustNormalizeJSON: %v", err))
	}
	return out
}
