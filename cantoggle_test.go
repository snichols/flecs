package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

type togglePos struct{ X, Y float32 }
type toggleVel struct{ DX, DY float32 }
type toggleTag struct{}

// Test 1a: EnableID panics when component is not marked CanToggle.
func TestCanToggle_EnableIDPanicsIfNotCanToggle(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{1, 2})
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected EnableID to panic for non-CanToggle component")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.EnableID(fw, e, posID)
	})
}

// Test 1: DisableID panics when component is not marked CanToggle.
func TestCanToggle_PanicIfNotCanToggle(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{1, 2})
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected DisableID to panic for non-CanToggle component")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})
}

// Test 2: Mark + Disable + Enable round-trip. Has returns true throughout;
// IsEnabledID reflects current state.
func TestCanToggle_MarkDisableEnableRoundTrip(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{1, 2})
	})
	flecs.SetCanToggle(w, posID)

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, posID) {
			t.Error("HasID should be true before disable")
		}
		if !flecs.IsEnabledID(fr, e, posID) {
			t.Error("IsEnabledID should be true before disable")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, posID) {
			t.Error("HasID should still be true after disable (component not removed)")
		}
		if flecs.IsEnabledID(fr, e, posID) {
			t.Error("IsEnabledID should be false after disable")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.EnableID(fw, e, posID)
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, posID) {
			t.Error("HasID should be true after re-enable")
		}
		if !flecs.IsEnabledID(fr, e, posID) {
			t.Error("IsEnabledID should be true after re-enable")
		}
	})
}

// Test 3: Each1[togglePos] skips rows where Position is disabled.
func TestCanToggle_Each1SkipsDisabledRows(t *testing.T) {
	w := flecs.New()
	var posID, e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		flecs.Set(fw, e1, togglePos{1, 0})
		flecs.Set(fw, e2, togglePos{2, 0})
		flecs.Set(fw, e3, togglePos{3, 0})
	})
	flecs.SetCanToggle(w, posID)

	// Disable e2's position.
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e2, posID)
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[togglePos](fr, func(e flecs.ID, _ *togglePos) {
			visited[e] = true
		})
	})

	if !visited[e1] {
		t.Error("Each1 should visit e1 (enabled)")
	}
	if visited[e2] {
		t.Error("Each1 should skip e2 (disabled)")
	}
	if !visited[e3] {
		t.Error("Each1 should visit e3 (enabled)")
	}
}

// Test 4: Re-enable restores query visibility on the same row in the same table.
func TestCanToggle_ReEnableRestoresVisibility(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{42, 0})
	})
	posID := flecs.RegisterComponent[togglePos](w)
	flecs.SetCanToggle(w, posID)

	w.Write(func(fw *flecs.Writer) { flecs.DisableID(fw, e, posID) })

	count := 0
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[togglePos](fr, func(_ flecs.ID, _ *togglePos) { count++ })
	})
	if count != 0 {
		t.Errorf("expected 0 visits after disable, got %d", count)
	}

	w.Write(func(fw *flecs.Writer) { flecs.EnableID(fw, e, posID) })

	count = 0
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[togglePos](fr, func(_ flecs.ID, _ *togglePos) { count++ })
	})
	if count != 1 {
		t.Errorf("expected 1 visit after re-enable, got %d", count)
	}
}

// Test 5: Multiple disabled components on one entity tracked independently.
func TestCanToggle_IndependentTracking(t *testing.T) {
	w := flecs.New()
	var posID, velID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		velID = flecs.RegisterComponent[toggleVel](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{1, 0})
		flecs.Set(fw, e, toggleVel{10, 0})
	})
	flecs.SetCanToggle(w, posID)
	flecs.SetCanToggle(w, velID)

	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})

	w.Read(func(fr *flecs.Reader) {
		if flecs.IsEnabledID(fr, e, posID) {
			t.Error("posID should be disabled")
		}
		if !flecs.IsEnabledID(fr, e, velID) {
			t.Error("velID should still be enabled")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, velID)
	})

	w.Read(func(fr *flecs.Reader) {
		if flecs.IsEnabledID(fr, e, posID) {
			t.Error("posID should still be disabled")
		}
		if flecs.IsEnabledID(fr, e, velID) {
			t.Error("velID should now be disabled")
		}
	})
}

// Test 6: Toggle survives table migration — disable Position, add unrelated tag,
// entity migrates to new archetype, Position remains disabled.
func TestCanToggle_SurvivesTableMigration(t *testing.T) {
	w := flecs.New()
	var posID, tagID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		tagID = flecs.RegisterComponent[toggleTag](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{7, 7})
	})
	flecs.SetCanToggle(w, posID)

	// Disable before migration.
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})

	// Force a table migration by adding a new component.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, tagID)
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, posID) {
			t.Error("entity should still have Position after migration")
		}
		if !fr.HasID(e, tagID) {
			t.Error("entity should have Tag after migration")
		}
		if flecs.IsEnabledID(fr, e, posID) {
			t.Error("Position should still be disabled after table migration")
		}
	})

	// Each1 should skip the entity even after migration.
	count := 0
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[togglePos](fr, func(_ flecs.ID, _ *togglePos) { count++ })
	})
	if count != 0 {
		t.Errorf("Each1 should skip migrated-but-disabled entity, got %d visits", count)
	}
}

// Test 7: Enabling or disabling bumps the table's change counter so cached
// queries can detect the modification.
func TestCanToggle_BumpsChangeCount(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{1, 2})
	})
	flecs.SetCanToggle(w, posID)

	tbl := flecs.TableOf(w, e)
	before := tbl.ChangeCount()

	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})
	after := tbl.ChangeCount()
	if after <= before {
		t.Error("DisableID should bump changeCount")
	}

	before = after
	w.Write(func(fw *flecs.Writer) {
		flecs.EnableID(fw, e, posID)
	})
	after = tbl.ChangeCount()
	if after <= before {
		t.Error("EnableID should bump changeCount")
	}
}

// Test 8: Multi-entity table — query visits exactly the enabled ones.
func TestCanToggle_MultiEntityTable(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	entities := make([]flecs.ID, 5)
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], togglePos{float32(i), 0})
		}
	})
	flecs.SetCanToggle(w, posID)

	// Disable entities at indices 1 and 3.
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, entities[1], posID)
		flecs.DisableID(fw, entities[3], posID)
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[togglePos](fr, func(e flecs.ID, _ *togglePos) {
			visited[e] = true
		})
	})

	for i, e := range entities {
		wantVisited := i != 1 && i != 3
		if visited[e] != wantVisited {
			t.Errorf("entity[%d]: visited=%v want=%v", i, visited[e], wantVisited)
		}
	}
}

// Test 9: IsCanToggle round-trip and bare-tag form fw.AddID(comp, w.CanToggle()).
func TestCanToggle_IsCanToggleRoundTrip(t *testing.T) {
	w := flecs.New()
	var posID, velID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		velID = flecs.RegisterComponent[toggleVel](w)
	})

	if flecs.IsCanToggle(w, posID) {
		t.Error("posID should not be CanToggle before SetCanToggle")
	}

	flecs.SetCanToggle(w, posID)
	if !flecs.IsCanToggle(w, posID) {
		t.Error("posID should be CanToggle after SetCanToggle")
	}
	if flecs.IsCanToggle(w, velID) {
		t.Error("velID should not be CanToggle")
	}

	// Bare-tag form.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(velID, w.CanToggle())
	})
	if !flecs.IsCanToggle(w, velID) {
		t.Error("velID should be CanToggle after fw.AddID(velID, w.CanToggle())")
	}
}

// Test 9b: Each2 with CanToggle components skips disabled rows.
func TestCanToggle_Each2SkipsDisabledRows(t *testing.T) {
	w := flecs.New()
	var posID, velID, e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		velID = flecs.RegisterComponent[toggleVel](w)
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		flecs.Set(fw, e1, togglePos{1, 0})
		flecs.Set(fw, e1, toggleVel{1, 0})
		flecs.Set(fw, e2, togglePos{2, 0})
		flecs.Set(fw, e2, toggleVel{2, 0})
		flecs.Set(fw, e3, togglePos{3, 0})
		flecs.Set(fw, e3, toggleVel{3, 0})
	})
	flecs.SetCanToggle(w, posID)
	flecs.SetCanToggle(w, velID)

	// Disable e2's position; disable e3's velocity.
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e2, posID)
		flecs.DisableID(fw, e3, velID)
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(fr *flecs.Reader) {
		flecs.Each2[togglePos, toggleVel](fr, func(e flecs.ID, _ *togglePos, _ *toggleVel) {
			visited[e] = true
		})
	})

	if !visited[e1] {
		t.Error("Each2 should visit e1 (both components enabled)")
	}
	if visited[e2] {
		t.Error("Each2 should skip e2 (Position disabled)")
	}
	if visited[e3] {
		t.Error("Each2 should skip e3 (Velocity disabled)")
	}
}

// Test 10b: EnableID panics when entity doesn't have the component.
func TestCanToggle_EnableIDPanicsNoComponent(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity() // no Position component
	})
	flecs.SetCanToggle(w, posID)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected EnableID to panic when entity does not have the component")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.EnableID(fw, e, posID)
	})
}

// Test 10c: DisableID panics when entity doesn't have the component.
func TestCanToggle_DisableIDPanicsNoComponent(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
	})
	flecs.SetCanToggle(w, posID)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected DisableID to panic when entity does not have the component")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})
}

// Test 10d: IsEnabledID returns false when entity doesn't have the component.
func TestCanToggle_IsEnabledIDFalseWhenNoComponent(t *testing.T) {
	w := flecs.New()
	var posID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
	})
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsEnabledID(fr, e, posID) {
			t.Error("IsEnabledID should return false when entity does not have the component")
		}
	})
}

// Test 10e: IsEnabled[T] returns false when type not registered.
func TestCanToggle_IsEnabledFalseWhenTypeNotRegistered(t *testing.T) {
	type neverRegistered struct{ Z int }
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsEnabled[neverRegistered](fr, e) {
			t.Error("IsEnabled[T] should return false for unregistered type")
		}
	})
}

// Test 10f: Enable[T] panics when type not registered.
func TestCanToggle_EnablePanicsTypeNotRegistered(t *testing.T) {
	type neverRegistered2 struct{ W int }
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("Enable[T] should panic for unregistered type")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Enable[neverRegistered2](fw, e)
	})
}

// Test 10g: Disable[T] panics when type not registered.
func TestCanToggle_DisablePanicsTypeNotRegistered(t *testing.T) {
	type neverRegistered3 struct{ V int }
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("Disable[T] should panic for unregistered type")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Disable[neverRegistered3](fw, e)
	})
}

// Test 10: Typed Enable[T] / Disable[T] / IsEnabled[T] generics.
func TestCanToggle_TypedGenericAPI(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[togglePos](w)
		e = fw.NewEntity()
		flecs.Set(fw, e, togglePos{5, 5})
	})
	posID := flecs.RegisterComponent[togglePos](w)
	flecs.SetCanToggle(w, posID)

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsEnabled[togglePos](fr, e) {
			t.Error("IsEnabled[togglePos] should be true before Disable")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Disable[togglePos](fw, e)
	})

	w.Read(func(fr *flecs.Reader) {
		if flecs.IsEnabled[togglePos](fr, e) {
			t.Error("IsEnabled[togglePos] should be false after Disable")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Enable[togglePos](fw, e)
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsEnabled[togglePos](fr, e) {
			t.Error("IsEnabled[togglePos] should be true after Enable")
		}
	})
}
