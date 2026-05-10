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

// EachTableForCount calls EachTableFor for componentID and returns the number
// of tables visited. If stopAfter > 0, iteration stops after that many visits.
// For tests only.
func EachTableForCount(w *World, componentID ID, stopAfter int) int {
	count := 0
	w.EachTableFor(componentID, func(_ *table.Table) bool {
		count++
		if stopAfter > 0 && count >= stopAfter {
			return false
		}
		return true
	})
	return count
}
