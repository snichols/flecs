package flecs_test

import (
	"bytes"
	"fmt"

	flecs "github.com/snichols/flecs"
)

// ExampleWorld_RegisterMigration demonstrates a two-release migration scenario.
//
// Release 1 has a "Player" component with a Name string and HP int32.
// Release 2 renames HP to Health and adds a Level int32 field (expanded struct).
// The migration function handles both the rename and the byte-layout expansion.
func ExampleWorld_RegisterMigration() {
	// ── Release 1 ────────────────────────────────────────────────────────────────
	// Define the v1 component layout.
	type PlayerV1 struct {
		HP int32
	}

	w1 := flecs.New()
	w1.SetSchemaVersion(1)
	_ = flecs.RegisterComponent[PlayerV1](w1)

	// Create some players in release 1.
	var warrior, mage flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		warrior = fw.NewEntity()
		flecs.Set(fw, warrior, PlayerV1{HP: 150})

		mage = fw.NewEntity()
		flecs.Set(fw, mage, PlayerV1{HP: 80})
	})

	// Serialize the release-1 world.
	var snapBuf bytes.Buffer
	if _, err := w1.TakeSnapshotTo(&snapBuf); err != nil {
		panic(err)
	}

	// ── Release 2 ────────────────────────────────────────────────────────────────
	// Define the v2 component layout. HP was renamed to Health; Level was added.
	type PlayerV2 struct {
		Health int32
		Level  int32
	}

	w2 := flecs.New()
	w2.SetSchemaVersion(2)
	_ = flecs.RegisterComponent[PlayerV2](w2)

	// Register a migration from schema v1 to v2.
	w2.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
		// Step 1: rename PlayerV1 to PlayerV2.
		if err := m.RenameComponent(
			"flecs_test.PlayerV1",
			"flecs_test.PlayerV2",
		); err != nil {
			return err
		}

		// Step 2: expand the raw bytes from 4 bytes (HP int32)
		// to 8 bytes (Health int32 + Level int32), setting Level = 1.
		return m.EachComponent("flecs_test.PlayerV2", func(rec *flecs.ComponentRecord) error {
			if len(rec.Raw) != 4 {
				return fmt.Errorf("unexpected raw size %d", len(rec.Raw))
			}
			newRaw := make([]byte, 8)
			copy(newRaw[:4], rec.Raw) // Health = old HP
			// Level defaults to 1 (little-endian int32).
			newRaw[4] = 1
			rec.SetRaw(newRaw)
			return nil
		})
	})

	// Load the release-1 snapshot and restore into the release-2 world.
	if err := w2.RestoreSnapshotFrom(&snapBuf); err != nil {
		panic(err)
	}

	// Verify the migrated data.
	w2.Read(func(r *flecs.Reader) {
		warriorV2, _ := flecs.Get[PlayerV2](r, warrior)
		mageV2, _ := flecs.Get[PlayerV2](r, mage)
		fmt.Printf("warrior: Health=%d Level=%d\n", warriorV2.Health, warriorV2.Level)
		fmt.Printf("mage:    Health=%d Level=%d\n", mageV2.Health, mageV2.Level)
	})

	// Output:
	// warrior: Health=150 Level=1
	// mage:    Health=80 Level=1
}
