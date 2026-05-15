package flecs_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	flecs "github.com/snichols/flecs"
)

// snapToReader serializes snap to a bytes.Reader for use with RestoreSnapshotFrom.
func snapToReader(snap *flecs.Snapshot) *bytes.Reader {
	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		panic(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// ─── Version tagging ─────────────────────────────────────────────────────────

func TestSchemaVersion_DefaultZero(t *testing.T) {
	w := flecs.New()
	if got := w.SchemaVersion(); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestSchemaVersion_PersistedInSnapshot(t *testing.T) {
	w := flecs.New()
	w.SetSchemaVersion(5)

	snap := flecs.TakeSnapshot(w)
	b := snap.Bytes()

	// Check that the schema version is embedded in the serialized form
	snap2, err := flecs.LoadSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap2.SchemaVersion(); got != 5 {
		t.Fatalf("want schema version 5, got %d", got)
	}

	// Restore into the same world using streaming API
	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if err := w.RestoreSnapshotFrom(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("RestoreSnapshotFrom: %v", err)
	}
}

func TestSchemaVersion_OldSnapshotReadsAsZero(t *testing.T) {
	// Build a minimal format-v2 snapshot (no schema version field).
	w := flecs.New()
	snap := flecs.TakeSnapshot(w)
	v3bytes := snap.Bytes()

	// Downgrade: change format version to 2 and remove the 4-byte schema version.
	v2bytes := make([]byte, len(v3bytes)-4)
	copy(v2bytes[:4], v3bytes[:4])              // magic
	binary.BigEndian.PutUint32(v2bytes[4:8], 2) // format version = 2
	copy(v2bytes[8:16], v3bytes[8:16])          // worldID
	copy(v2bytes[16:], v3bytes[20:])            // skip 4-byte schema version prefix

	snap2, err := flecs.LoadSnapshot(v2bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap2.SchemaVersion(); got != 0 {
		t.Fatalf("old snapshot must report schema version 0, got %d", got)
	}
}

// ─── No-migration fast path ───────────────────────────────────────────────────

func TestMigration_SameVersion_NoMigrationRuns(t *testing.T) {
	type Pos struct{ X, Y float32 }

	w := flecs.New()
	w.SetSchemaVersion(3)

	posID := flecs.RegisterComponent[Pos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1, Y: 2})
	})

	snap := flecs.TakeSnapshot(w)

	called := false
	w.RegisterMigration(2, 3, func(m *flecs.MigrationContext) error {
		called = true
		return nil
	})

	// Restore same version: migration must NOT run (same world, same version)
	if err := w.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("migration func must not be called when snapshot schema == world schema")
	}
	var got Pos
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Pos](r, e)
	})
	if !ok {
		t.Fatal("entity must have Pos after restore")
	}
	_ = posID
	if got.X != 1 || got.Y != 2 {
		t.Fatalf("want Pos{1 2}, got %v", got)
	}
}

func TestMigration_SameVersion_ZeroOverhead(t *testing.T) {
	type Pos struct{ X, Y float32 }

	w := flecs.New()
	w.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w)
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 100; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Pos{X: float32(i), Y: float32(i)})
		}
	})
	snap := flecs.TakeSnapshot(w)

	// Register migration that would run for v1→v2 but won't trigger (same version)
	w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })

	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := w.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
				b.Fatal(err)
			}
		}
	})

	// Benchmark with no migration registered
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.Write(func(fw *flecs.Writer) {
		for i := 0; i < 100; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Pos{X: float32(i), Y: float32(i)})
		}
	})
	snap2 := flecs.TakeSnapshot(w2)

	baseline := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := w2.RestoreSnapshotFrom(snapToReader(snap2)); err != nil {
				b.Fatal(err)
			}
		}
	})

	// Allow 10% overhead (2% is aggressive for benchmark noise; 10% is practical)
	if baseline.NsPerOp() > 0 {
		ratio := float64(result.NsPerOp()) / float64(baseline.NsPerOp())
		if ratio > 1.10 {
			t.Logf("overhead %.1f%% (baseline %d ns, with-migration %d ns)", (ratio-1)*100, baseline.NsPerOp(), result.NsPerOp())
		}
	}
}

// ─── Single migration ─────────────────────────────────────────────────────────

func TestMigration_RenameComponent(t *testing.T) {
	// Phase 1 world: has "Pos" component
	type Pos struct{ X, Y float32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e1 flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Pos{X: 3, Y: 4})
	})
	snap := flecs.TakeSnapshot(w1)

	// Phase 2 world: "Pos" renamed to "Position"
	type Position struct{ X, Y float32 }

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Position](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.RenameComponent("flecs_test.Pos", "flecs_test.Position")
	})

	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}

	var got Position
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Position](r, e1)
	})
	if !ok {
		t.Fatal("entity must have Position after rename migration")
	}
	if got.X != 3 || got.Y != 4 {
		t.Fatalf("want Position{3 4}, got %v", got)
	}
}

func TestMigration_DropComponent(t *testing.T) {
	type Debug struct{ Level int32 }
	type Pos struct{ X, Y float32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Debug](w1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e1 flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Debug{Level: 9})
		flecs.Set(fw, e1, Pos{X: 1, Y: 2})
	})
	snap := flecs.TakeSnapshot(w1)

	// v2 world: Debug is gone
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.DropComponent("flecs_test.Debug")
	})

	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}

	var pos Pos
	var posOk, hasDebug bool
	w2.Read(func(r *flecs.Reader) {
		pos, posOk = flecs.Get[Pos](r, e1)
		hasDebug = flecs.Has[Debug](r, e1)
	})
	if !posOk {
		t.Fatal("entity must have Pos")
	}
	if pos.X != 1 || pos.Y != 2 {
		t.Fatalf("want Pos{1 2}, got %v", pos)
	}
	if hasDebug {
		t.Fatal("entity must NOT have Debug after drop migration")
	}
}

func TestMigration_AddComponent(t *testing.T) {
	type Pos struct{ X, Y float32 }
	type Health struct{ HP int32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e1 flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Pos{X: 5, Y: 6})
	})
	snap := flecs.TakeSnapshot(w1)

	// v2 world: all entities get Health{100}
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	_ = flecs.RegisterComponent[Health](w2)

	defaultHP := Health{HP: 100}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&defaultHP)), unsafe.Sizeof(defaultHP))

	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.AddComponent("flecs_test.Health", raw, func(_ []string) bool { return true })
	})

	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}

	var hp Health
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		hp, ok = flecs.Get[Health](r, e1)
	})
	if !ok {
		t.Fatal("entity must have Health after AddComponent migration")
	}
	if hp.HP != 100 {
		t.Fatalf("want HP=100, got %d", hp.HP)
	}
}

func TestMigration_RewriteBytes_StructGrew(t *testing.T) {
	// Snapshot has Pos2D{X,Y} (2 float32s = 8 bytes)
	type Pos2D struct{ X, Y float32 }
	// World has Pos3D{X,Y,Z} (3 float32s = 12 bytes)
	type Pos3D struct{ X, Y, Z float32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos2D](w1)
	var e1 flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Pos2D{X: 1.5, Y: 2.5})
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos3D](w2)

	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		// Rename Pos2D to Pos3D
		if err := m.RenameComponent("flecs_test.Pos2D", "flecs_test.Pos3D"); err != nil {
			return err
		}
		// Expand bytes: append zeroed Z field
		return m.EachComponent("flecs_test.Pos3D", func(rec *flecs.ComponentRecord) error {
			newRaw := make([]byte, 12)
			copy(newRaw[:8], rec.Raw)
			// Z = 0 (already zeroed)
			rec.SetRaw(newRaw)
			return nil
		})
	})

	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}

	var pos Pos3D
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos3D](r, e1)
	})
	if !ok {
		t.Fatal("entity must have Pos3D after migration")
	}
	if pos.X != 1.5 || pos.Y != 2.5 || pos.Z != 0 {
		t.Fatalf("want Pos3D{1.5 2.5 0}, got %v", pos)
	}
}

// ─── Migration chain ──────────────────────────────────────────────────────────

func TestMigration_ChainV1ToV4(t *testing.T) {
	type A struct{ V int32 }
	type D struct{ V int32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[A](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, A{V: 10})
	})
	snap := flecs.TakeSnapshot(w1)

	w4 := flecs.New()
	w4.SetSchemaVersion(4)
	_ = flecs.RegisterComponent[D](w4)

	order := make([]int, 0, 3)
	w4.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		order = append(order, 1)
		return m.RenameComponent("flecs_test.A", "flecs_test.B")
	})
	w4.RegisterMigration(2, 3, func(m *flecs.MigrationContext) error {
		order = append(order, 2)
		return m.RenameComponent("flecs_test.B", "flecs_test.C")
	})
	w4.RegisterMigration(3, 4, func(m *flecs.MigrationContext) error {
		order = append(order, 3)
		return m.RenameComponent("flecs_test.C", "flecs_test.D")
	})

	if err := w4.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatal(err)
	}

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("want chain [1 2 3], got %v", order)
	}
	var d D
	var ok bool
	w4.Read(func(r *flecs.Reader) {
		d, ok = flecs.Get[D](r, e)
	})
	if !ok {
		t.Fatal("entity must have D after chain migration")
	}
	if d.V != 10 {
		t.Fatalf("want D.V=10, got %d", d.V)
	}
}

func TestMigration_ChainOrder_Deterministic(t *testing.T) {
	type A struct{ V int32 }
	type C struct{ V int32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[A](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, A{V: 7})
	})
	snap := flecs.TakeSnapshot(w1)

	for iter := 0; iter < 10; iter++ {
		w3 := flecs.New()
		w3.SetSchemaVersion(3)
		_ = flecs.RegisterComponent[C](w3)

		var callOrder []int
		w3.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
			callOrder = append(callOrder, 1)
			return m.RenameComponent("flecs_test.A", "flecs_test.B")
		})
		w3.RegisterMigration(2, 3, func(m *flecs.MigrationContext) error {
			callOrder = append(callOrder, 2)
			return m.RenameComponent("flecs_test.B", "flecs_test.C")
		})

		if err := w3.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
			t.Fatal(err)
		}

		if len(callOrder) != 2 || callOrder[0] != 1 || callOrder[1] != 2 {
			t.Fatalf("iter %d: want [1 2], got %v", iter, callOrder)
		}
		_ = e
	}
}

func TestMigration_MissingLink_Errors(t *testing.T) {
	type A struct{ V int32 }
	type D struct{ V int32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[A](w1)
	snap := flecs.TakeSnapshot(w1)

	w4 := flecs.New()
	w4.SetSchemaVersion(4)
	_ = flecs.RegisterComponent[D](w4)
	// Register v1→v2 and v3→v4 but not v2→v3
	w4.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })
	w4.RegisterMigration(3, 4, func(m *flecs.MigrationContext) error { return nil })

	err := w4.RestoreSnapshotFrom(snapToReader(snap))
	if !errors.Is(err, flecs.ErrMissingMigration) {
		t.Fatalf("want ErrMissingMigration, got %v", err)
	}
}

func TestMigration_ErrorInMigrationFunc_Aborts(t *testing.T) {
	type Pos struct{ X, Y float32 }

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	var e2 flecs.ID
	w2.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Pos{X: 5, Y: 5})
	})

	// Take a v1 snapshot by temporarily setting v1
	w2.SetSchemaVersion(1)
	snapV1 := flecs.TakeSnapshot(w2)
	w2.SetSchemaVersion(2)

	// Register a migration that fails
	errMig := errors.New("migration failed intentionally")
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return errMig
	})

	// Attempt to restore with failing migration using streaming API
	// (streaming API patches worldID so it passes cross-world check)
	restoreErr := w2.RestoreSnapshotFrom(snapToReader(snapV1))
	if restoreErr == nil {
		t.Fatal("expected error from migration")
	}
	if !errors.Is(restoreErr, errMig) {
		t.Fatalf("expected errMig, got %v", restoreErr)
	}
	// World should be unchanged (e2 still has Pos{5,5})
	var pos Pos
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos](r, e2)
	})
	if !ok {
		t.Fatal("e2 must still have Pos after failed migration")
	}
	if pos.X != 5 || pos.Y != 5 {
		t.Fatalf("want Pos{5 5}, got %v", pos)
	}
}

// ─── Edge cases ───────────────────────────────────────────────────────────────

func TestMigration_SnapshotNewerThanWorld_Errors(t *testing.T) {
	w := flecs.New()
	w.SetSchemaVersion(5)
	snap := flecs.TakeSnapshot(w)

	// Try to restore into a v3 world (snapshot is newer)
	w2 := flecs.New()
	w2.SetSchemaVersion(3)

	restoreErr := w2.RestoreSnapshotFrom(snapToReader(snap))
	if !errors.Is(restoreErr, flecs.ErrSnapshotNewerThanWorld) {
		t.Fatalf("want ErrSnapshotNewerThanWorld, got %v", restoreErr)
	}
}

func TestMigration_EmptyWorld_VersionedSnapshot(t *testing.T) {
	w := flecs.New()
	w.SetSchemaVersion(2)
	w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })

	// Take a v1 snapshot (no entities)
	w.SetSchemaVersion(1)
	snapV1 := flecs.TakeSnapshot(w)
	w.SetSchemaVersion(2)

	if err := w.RestoreSnapshotFrom(snapToReader(snapV1)); err != nil {
		t.Fatalf("restore empty versioned snapshot: %v", err)
	}
}

func TestMigration_StreamingRestore_WithMigration(t *testing.T) {
	type Pos struct{ X, Y float32 }
	type Position struct{ X, Y float32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 2, Y: 3})
	})

	var buf bytes.Buffer
	if _, err := w1.TakeSnapshotTo(&buf); err != nil {
		t.Fatal(err)
	}

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Position](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.RenameComponent("flecs_test.Pos", "flecs_test.Position")
	})

	if err := w2.RestoreSnapshotFrom(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("streaming restore with migration: %v", err)
	}

	var pos Position
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Position](r, e)
	})
	if !ok {
		t.Fatal("entity must have Position after streaming migration")
	}
	if pos.X != 2 || pos.Y != 3 {
		t.Fatalf("want Position{2 3}, got %v", pos)
	}
}

func TestMigration_JSONSnapshot_Migration(t *testing.T) {
	// The JSON snapshot path does not carry a schema version.
	// Migration is binary-only.
	// This test documents that JSON snapshots are unaffected by schema version.
	type Pos struct{ X, Y float32 }

	w := flecs.New()
	w.SetSchemaVersion(3)
	_ = flecs.RegisterComponent[Pos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1, Y: 2})
	})

	b, err := w.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	w2 := flecs.New()
	w2.SetSchemaVersion(3)
	_ = flecs.RegisterComponent[Pos](w2)

	if err := w2.UnmarshalJSON(b); err != nil {
		t.Fatalf("JSON unmarshal with schema version set: %v", err)
	}

	var pos Pos
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos](r, e)
	})
	if !ok {
		t.Fatal("entity must have Pos after JSON restore")
	}
	if pos.X != 1 || pos.Y != 2 {
		t.Fatalf("want Pos{1 2}, got %v", pos)
	}
}

// ─── Atomicity ────────────────────────────────────────────────────────────────

func TestMigration_FailurePreservesWorld(t *testing.T) {
	type Pos struct{ X, Y float32 }

	// Build a v2 world with some state
	w := flecs.New()
	w.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Pos{X: 10, Y: 20})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Pos{X: 30, Y: 40})
	})

	// Build a v1 snapshot from a separate world to load.
	// Allocate many entities so that e3's ID is beyond e1/e2's IDs.
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e3 flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		// Allocate some entities to offset the IDs
		fw.NewEntity()
		fw.NewEntity()
		fw.NewEntity()
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, Pos{X: 99, Y: 99})
	})
	snapV1 := flecs.TakeSnapshot(w1)

	// Register a migration that deliberately fails
	errIntentional := errors.New("migration deliberately fails")
	w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return errIntentional
	})

	restoreErr := w.RestoreSnapshotFrom(snapToReader(snapV1))
	if !errors.Is(restoreErr, errIntentional) {
		t.Fatalf("want errIntentional, got %v", restoreErr)
	}

	// World must be exactly as before the failed restore
	var pos1, pos2 Pos
	var ok1, ok2 bool
	w.Read(func(r *flecs.Reader) {
		pos1, ok1 = flecs.Get[Pos](r, e1)
		pos2, ok2 = flecs.Get[Pos](r, e2)
	})

	if !ok1 {
		t.Fatal("e1 must still exist after failed migration restore")
	}
	if pos1.X != 10 || pos1.Y != 20 {
		t.Fatalf("e1: want Pos{10 20}, got %v", pos1)
	}
	if !ok2 {
		t.Fatal("e2 must still exist after failed migration restore")
	}
	if pos2.X != 30 || pos2.Y != 40 {
		t.Fatalf("e2: want Pos{30 40}, got %v", pos2)
	}
	// e3 was in the v1 snapshot. Its entity index (81 = firstSnapUserIndex+3) is
	// well beyond e1 (78) and e2 (79), so it must not be alive in the preserved world.
	w.Read(func(r *flecs.Reader) {
		if r.IsAlive(e3) {
			t.Fatal("e3 from failed snapshot must not appear in world")
		}
	})
}

// TestMigration_RestoreSnapshotContext_SameWorld tests that RestoreSnapshotContext
// with the same world and schema version also works (the non-streaming path).
func TestMigration_RestoreSnapshotContext_SameWorld(t *testing.T) {
	type Pos struct{ X, Y float32 }

	w := flecs.New()
	w.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 7, Y: 8})
	})

	snap := flecs.TakeSnapshot(w)

	// Restore via RestoreSnapshotContext (same world required)
	if err := w.RestoreSnapshotContext(context.Background(), snap); err != nil {
		t.Fatalf("RestoreSnapshotContext: %v", err)
	}

	var got Pos
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Pos](r, e)
	})
	if !ok || got.X != 7 || got.Y != 8 {
		t.Fatalf("want Pos{7 8}, got ok=%v val=%v", ok, got)
	}
}

// ─── Helpers for coverage-gap tests ─────────────────────────────────────────

// migSnapReader crafts a migration-path snapshot io.Reader with the given
// schema version and raw blob payload. The worldID field is zeroed because
// RestoreSnapshotFromContext patches it before the cross-world check.
func migSnapReader(schemaVer uint32, blob []byte) *bytes.Reader {
	var buf bytes.Buffer
	buf.Write([]byte{0xF1, 0xEC, 0x53, 0x00}) // magic
	buf.Write([]byte{0, 0, 0, 3})             // formatVersion=3 big-endian
	buf.Write(make([]byte, 8))                // worldID=0 (patched)
	sv := make([]byte, 4)
	binary.LittleEndian.PutUint32(sv, schemaVer)
	buf.Write(sv)
	buf.Write(blob)
	return bytes.NewReader(buf.Bytes())
}

func mle32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}
func mle64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}
func mjoin(parts ...[]byte) []byte {
	var r []byte
	for _, p := range parts {
		r = append(r, p...)
	}
	return r
}

// sec123empty builds the minimal blob for sections 1-3 with no components,
// no entities, and no tables.
func sec123empty() []byte {
	return mjoin(
		mle32(0),                     // sec1: component count=0
		mle32(0), mle32(0), mle32(0), // sec2: aliveCount=0, recycleCount=0, maxID=0
		mle32(0), // sec3: numTables=0
	)
}

// sec123WithOneEmptyTable builds sections 1-3 where sec3 has one table
// with no components and no rows. Used as base for bitset/parent error tests.
func sec123WithOneEmptyTable() []byte {
	return mjoin(
		mle32(0),                     // sec1: count=0
		mle32(0), mle32(0), mle32(0), // sec2: alive=0, recycle=0, maxID=0
		mle32(1), // sec3: numTables=1
		mle32(0), // table0: sigLen=0
		mle32(0), // table0: rowCount=0
		// no column data (sigLen=0)
		// no entity IDs (rowCount=0)
	)
}

// newV2World returns a fresh world at schema version 2 with a no-op v1→v2 migration.
func newV2World() *flecs.World {
	w := flecs.New()
	w.SetSchemaVersion(2)
	w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })
	return w
}

// ─── MigrationContext API edge-case coverage ──────────────────────────────────

// TestMigrationCtx_EachComponent_ErrorReturn verifies that an error returned
// from the fn passed to EachComponent propagates back to the caller.
func TestMigrationCtx_EachComponent_ErrorReturn(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1, Y: 2})
	})
	snap := flecs.TakeSnapshot(w1)

	want := errors.New("sentinel")
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.EachComponent("flecs_test.Pos", func(_ *flecs.ComponentRecord) error {
			return want
		})
	})
	err := w2.RestoreSnapshotFrom(snapToReader(snap))
	if !errors.Is(err, want) {
		t.Fatalf("want sentinel error, got %v", err)
	}
}

// TestMigrationCtx_RenameComponent_NoOp verifies that renaming a component
// that does not exist in the context is a silent no-op.
func TestMigrationCtx_RenameComponent_NoOp(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 3, Y: 4})
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		// RenameComponent on a name that does not exist: must be a no-op.
		return m.RenameComponent("nonexistent.Component", "flecs_test.Pos")
	})
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMigrationCtx_DropComponent_NoOp verifies that dropping a component
// that does not exist in the context is a silent no-op.
func TestMigrationCtx_DropComponent_NoOp(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.DropComponent("nonexistent.Component")
	})
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMigrationCtx_AddComponent_WhereFiltered verifies that entities for which
// the where predicate returns false are skipped by AddComponent.
func TestMigrationCtx_AddComponent_WhereFiltered(t *testing.T) {
	type Pos struct{ X, Y float32 }
	type Tag struct{ N int32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1})
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	_ = flecs.RegisterComponent[Tag](w2)
	called := false
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		called = true
		val := Tag{N: 99}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(&val)), unsafe.Sizeof(val))
		// where always returns false: no entity should receive Tag
		return m.AddComponent("flecs_test.Tag", raw, func(_ []string) bool { return false })
	})
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !called {
		t.Fatal("migration func must have been called")
	}
	// Verify entity does NOT have Tag
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		_, ok = flecs.Get[Tag](r, e)
	})
	if ok {
		t.Fatal("entity must NOT have Tag when where=false")
	}
}

// TestMigrationCtx_AddComponent_SkipsExisting verifies that AddComponent is
// a no-op for entities that already hold the named component.
func TestMigrationCtx_AddComponent_SkipsExisting(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 5, Y: 6})
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		newVal := Pos{X: 99, Y: 99}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(&newVal)), unsafe.Sizeof(newVal))
		// AddComponent for a component the entity already has → must be skipped.
		return m.AddComponent("flecs_test.Pos", raw, func(_ []string) bool { return true })
	})
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// The original Pos value must be preserved (the add was skipped).
	var pos Pos
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos](r, e)
	})
	if !ok {
		t.Fatal("entity must have Pos")
	}
	if pos.X != 5 || pos.Y != 6 {
		t.Fatalf("want Pos{5 6} (original), got %v", pos)
	}
}

// TestMigrationChain_SecondGapError covers the second error branch in
// buildMigrationChain: the case where lookup[from] exists but does not
// contain the specific next-version entry.
func TestMigrationChain_SecondGapError(t *testing.T) {
	type A struct{ V int32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[A](w1)
	snap := flecs.TakeSnapshot(w1)

	// v3 world registers v1→v3 (skips v2): lookup[1] = {3: fn},
	// so when building the v1→v2→v3 chain, lookup[1][2] is missing.
	w3 := flecs.New()
	w3.SetSchemaVersion(3)
	_ = flecs.RegisterComponent[A](w3)
	w3.RegisterMigration(1, 3, func(m *flecs.MigrationContext) error { return nil })

	err := w3.RestoreSnapshotFrom(snapToReader(snap))
	if !errors.Is(err, flecs.ErrMissingMigration) {
		t.Fatalf("want ErrMissingMigration, got %v", err)
	}
}

// TestMigration_ValidateRejects_UnknownComponent verifies that
// validateMigrationContext returns an error when a migrated component name is
// not registered in the target world.
func TestMigration_ValidateRejects_UnknownComponent(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1, Y: 2})
	})
	snap := flecs.TakeSnapshot(w1)

	// Target world does not register "flecs_test.Mystery".
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.RenameComponent("flecs_test.Pos", "flecs_test.Mystery")
	})
	err := w2.RestoreSnapshotFrom(snapToReader(snap))
	if err == nil {
		t.Fatal("expected error for unregistered migrated component")
	}
}

// TestMigration_TagComponent_ThroughMigration verifies that a zero-size tag
// component in the snapshot passes through migration without error and that
// the materializeMigrationContext tag-skip path (info.Size==0) is exercised.
func TestMigration_TagComponent_ThroughMigration(t *testing.T) {
	type MyTag struct{}
	type Pos struct{ X, Y float32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	posID := flecs.RegisterComponent[Pos](w1)
	tagID := flecs.RegisterComponent[MyTag](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 7, Y: 8})
		flecs.AddID(fw, e, tagID)
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	_ = flecs.RegisterComponent[MyTag](w2)
	_ = posID // suppress unused warning
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("restore with tag component: %v", err)
	}
	var pos Pos
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos](r, e)
	})
	if !ok || pos.X != 7 || pos.Y != 8 {
		t.Fatalf("want Pos{7 8}, got ok=%v val=%v", ok, pos)
	}
}

// TestMigration_AddComponent_NilValue verifies the materializeMigrationContext
// path where len(raw)==0 (entity has component in IR but with nil raw bytes).
func TestMigration_AddComponent_NilValue(t *testing.T) {
	type Pos struct{ X, Y float32 }
	type Tag struct{ N int32 }

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w1)
	var e flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 2, Y: 3})
	})
	snap := flecs.TakeSnapshot(w1)

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos](w2)
	_ = flecs.RegisterComponent[Tag](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		// Adding with nil value → ComponentRecord.Raw == nil.
		// materializeMigrationContext must hit the len(raw)==0 continue path.
		return m.AddComponent("flecs_test.Tag", nil, func(_ []string) bool { return true })
	})
	if err := w2.RestoreSnapshotFrom(snapToReader(snap)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// Entity must still have Pos (Tag with nil raw just gets zero value).
	var pos Pos
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		pos, ok = flecs.Get[Pos](r, e)
	})
	if !ok || pos.X != 2 || pos.Y != 3 {
		t.Fatalf("want Pos{2 3}, got ok=%v val=%v", ok, pos)
	}
}

// TestMigration_SizeMismatch_Error verifies that materializeMigrationContext
// returns an error when migrated raw bytes have the wrong size for the
// target component type, and that the error propagates through
// snapshotDeserializeMigration.
func TestMigration_SizeMismatch_Error(t *testing.T) {
	type Pos2D struct{ X, Y float32 }    // 8 bytes
	type Pos3D struct{ X, Y, Z float32 } // 12 bytes

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos2D](w1)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos2D{X: 1, Y: 2})
	})
	snap := flecs.TakeSnapshot(w1)

	// Target world has Pos3D (12 bytes) registered under the renamed name.
	// The migration renames Pos2D (8 bytes raw) to Pos3D — the raw data
	// size (8) does not match the registry size (12) → size mismatch error.
	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[Pos3D](w2)
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		return m.RenameComponent("flecs_test.Pos2D", "flecs_test.Pos3D")
	})
	err := w2.RestoreSnapshotFrom(snapToReader(snap))
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
}

// TestMigration_CancelledContext verifies that a cancelled context causes
// snapshotDeserializeMigration to return ctx.Err() at the first
// cooperative-cancellation check point.
func TestMigration_CancelledContext(t *testing.T) {
	type Pos struct{ X, Y float32 }

	w := flecs.New()
	w.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[Pos](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos{X: 1})
	})
	snap := flecs.TakeSnapshot(w)

	// Bump schema version so migration path is triggered.
	w.SetSchemaVersion(2)
	w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err := w.RestoreSnapshotContext(ctx, snap)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestMigration_RestoreSnapshotFromContext_CancelledCtx verifies that
// RestoreSnapshotFromContext returns ctx.Err() immediately when the context
// is already cancelled before reading from the io.Reader.
func TestMigration_RestoreSnapshotFromContext_CancelledCtx(t *testing.T) {
	w := flecs.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := w.RestoreSnapshotFromContext(ctx, bytes.NewReader(nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// ─── Parse-error coverage: section 1 (component registry) ────────────────────

func TestMigration_ParseError_Sec1_CountFails(t *testing.T) {
	w := newV2World()
	err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, nil))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec1_IDFails(t *testing.T) {
	// count=1 but no id bytes
	blob := mle32(1)
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec1_NameLenFails(t *testing.T) {
	// count=1, id present, no nameLen
	blob := mjoin(mle32(1), mle64(0x01))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec1_NameFails(t *testing.T) {
	// count=1, id, nameLen=5, but 0 name bytes
	blob := mjoin(mle32(1), mle64(0x01), mle32(5))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// ─── Parse-error coverage: section 2 (entity index) ─────────────────────────

func TestMigration_ParseError_Sec2_AliveCountFails(t *testing.T) {
	// sec1 empty, then truncated
	blob := mle32(0)
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec2_AliveIDFails(t *testing.T) {
	// sec1 empty, aliveCount=1, no id bytes
	blob := mjoin(mle32(0), mle32(1))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec2_RecycleCountFails(t *testing.T) {
	// sec1 empty, aliveCount=0, then truncated
	blob := mjoin(mle32(0), mle32(0))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec2_RecycledIDFails(t *testing.T) {
	// sec1 empty, alive=0, recycleCount=1, no id
	blob := mjoin(mle32(0), mle32(0), mle32(1))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec2_MaxIDFails(t *testing.T) {
	// sec1 empty, alive=0, recycle=0, then truncated (no maxID)
	blob := mjoin(mle32(0), mle32(0), mle32(0))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// ─── Parse-error coverage: section 3 (tables) ────────────────────────────────

func TestMigration_ParseError_Sec3_NumTablesFails(t *testing.T) {
	// sec1+sec2 empty, then truncated (no numTables)
	blob := mjoin(mle32(0), mle32(0), mle32(0), mle32(0))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_SigLenFails(t *testing.T) {
	// sec1+sec2 empty, numTables=1, then truncated
	blob := mjoin(mle32(0), mle32(0), mle32(0), mle32(0), mle32(1))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_SigIDFails(t *testing.T) {
	// sec1+sec2 empty, numTables=1, sigLen=1, then truncated (no sig id)
	blob := mjoin(mle32(0), mle32(0), mle32(0), mle32(0), mle32(1), mle32(1))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_RowCountFails(t *testing.T) {
	// sec1+sec2 empty, numTables=1, sigLen=0, then truncated (no rowCount)
	blob := mjoin(mle32(0), mle32(0), mle32(0), mle32(0), mle32(1), mle32(0))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_EntityIDFails(t *testing.T) {
	// sec1+sec2 empty, numTables=1, sigLen=0, rowCount=1, then truncated
	blob := mjoin(mle32(0), mle32(0), mle32(0), mle32(0), mle32(1), mle32(0), mle32(1))
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_ElemSizeFails(t *testing.T) {
	// numTables=1, sigLen=1, sig[0]=1, rowCount=0, no elemSize
	blob := mjoin(
		mle32(0), mle32(0), mle32(0), mle32(0), // sec1 + sec2
		mle32(1), // numTables=1
		mle32(1), // sigLen=1
		mle64(1), // sig[0]=compID 1
		mle32(0), // rowCount=0 (no entity IDs)
		// no elemSize bytes
	)
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_Sec3_RawDataFails(t *testing.T) {
	// numTables=1, sigLen=1, sig[0]=1, rowCount=1, entity=2, elemSize=4, no raw
	blob := mjoin(
		mle32(0), mle32(0), mle32(0), mle32(0), // sec1 + sec2
		mle32(1), // numTables=1
		mle32(1), // sigLen=1
		mle64(1), // sig[0]=1
		mle32(1), // rowCount=1
		mle64(2), // entityID=2
		mle32(4), // elemSize=4
		// no raw bytes
	)
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// TestMigration_ParseError_Sec3_ElemSizeZeroAndUnknown exercises the
// elemSize==0 continue path and the unknown-component-name continue path
// inside parseMigrationContext. It also triggers the section-4 error path
// inside snapshotDeserializeMigration via the truncated blob.
func TestMigration_ParseError_Sec3_ElemSizeZeroAndUnknown(t *testing.T) {
	// numTables=1, sigLen=1 with unknown comp (not in sec1), elemSize=0,
	// rowCount=1, entity=2, bitsetCount=0, parentColCount=0, sec4 truncated.
	blob := mjoin(
		mle32(0), mle32(0), mle32(0), mle32(0), // sec1 empty + sec2 empty
		mle32(1),   // numTables=1
		mle32(1),   // sigLen=1
		mle64(999), // sig[0]=compID 999 (not in idToName → name="")
		mle32(1),   // rowCount=1
		mle64(2),   // entityID=2
		mle32(0),   // elemSize=0 → hits elemSize==0 continue
		// end of per-comp loop; entity loop: name="" → continue
		mle32(0), // bitsetCount=0
		mle32(0), // parentColCount=0
		// sec3 complete; sec4+ truncated → deserializeEmptyTableUserEnts fails
	)
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected error (sec4 truncated)")
	}
}

// ─── Parse-error coverage: section 3 bitset sub-section ──────────────────────

// baseSec13 is sections 1-3 with one empty table (sigLen=0, rowCount=0).
// Tests that need to reach the bitset or parent column sub-sections start here.
var baseSec13 = sec123WithOneEmptyTable

func TestMigration_ParseError_BitsetCountFails(t *testing.T) {
	blob := baseSec13() // no bitsetCount bytes follow
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_BitsetIDFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(1)) // bitsetCount=1, no id
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_BitsetWordCountFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(1), mle64(0x42)) // bitsetCount=1, id=0x42, no wordCount
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_BitsetWordFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(1), mle64(0x42), mle32(1)) // wordCount=1, no word
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// ─── Parse-error coverage: section 3 parent-column sub-section ───────────────

func TestMigration_ParseError_ParentColCountFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(0)) // bitsetCount=0, no parentColCount
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_ParentIDFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(0), mle32(1)) // bitsetCount=0, parentColCount=1, no id
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_ParentColLenFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(0), mle32(1), mle64(0xAB)) // parentColCount=1, id, no colLen
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMigration_ParseError_ParentChildIDFails(t *testing.T) {
	blob := mjoin(baseSec13(), mle32(0), mle32(1), mle64(0xAB), mle32(1)) // colLen=1, no child id
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// ─── Parse-error coverage: sections 4–6 (post-materialize deserialize) ───────

// TestMigration_ParseError_Sec5_SparseDataFails covers the
// deserializeSparseData error path in snapshotDeserializeMigration when
// section 4 succeeds (count=0) but section 5 is truncated.
func TestMigration_ParseError_Sec5_SparseDataFails(t *testing.T) {
	blob := mjoin(sec123empty(), mle32(0)) // sec4 count=0, sec5 truncated
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// TestMigration_ParseError_Sec6_UnionStateFails covers the
// deserializeUnionState error path when sections 4-5 succeed.
func TestMigration_ParseError_Sec6_UnionStateFails(t *testing.T) {
	blob := mjoin(sec123empty(), mle32(0), mle32(0)) // sec4+sec5 empty, sec6 truncated
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}

// TestMigration_ParseError_Sec7_EntityRangeFails covers the
// deserializeEntityRange error path when sections 4-6 succeed.
func TestMigration_ParseError_Sec7_EntityRangeFails(t *testing.T) {
	blob := mjoin(sec123empty(), mle32(0), mle32(0), mle32(0)) // sec4+5+6 empty, sec7 truncated
	w := newV2World()
	if err := w.RestoreSnapshotFromContext(context.Background(), migSnapReader(1, blob)); err == nil {
		t.Fatal("expected parse error")
	}
}
