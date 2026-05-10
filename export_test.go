package flecs

import "github.com/snichols/flecs/internal/storage/table"

// TableOf returns the archetype table pointer for entity e. For tests only.
func TableOf(w *World, e ID) *table.Table {
	rec := w.index.Get(e)
	if rec == nil {
		return nil
	}
	return rec.Table
}
