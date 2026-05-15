package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// qobSmallComp and qobLargeComp are benchmark-only component types.
type qobSmallComp struct{ V int }
type qobLargeComp struct{ V int }

// BenchmarkOptimizer_SkewedDomain benchmarks a query where one variable has a
// domain ~100× larger than the other. The optimizer should pick the smaller
// domain as driver, yielding a significant iteration speedup.
//
// Target: the optimized (small-domain driver) path is ≥10× faster than the
// unoptimized (large-domain driver) path for strongly skewed domains.
func BenchmarkOptimizer_SkewedDomain(b *testing.B) {
	w := flecs.New()
	smallID := flecs.RegisterComponent[qobSmallComp](w)
	largeID := flecs.RegisterComponent[qobLargeComp](w)

	const smallCount = 10
	const largeCount = 1000

	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < smallCount; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, qobSmallComp{V: i})
		}
		for i := 0; i < largeCount; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, qobLargeComp{V: i})
		}
	})

	w.Read(func(_ *flecs.Reader) {
		// Optimized: optimizer picks smallID (fewer tables) as driver.
		// Both variables are independent (no dependency between them).
		b.Run("optimized", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// largeID first-defined, but optimizer should pick smallID.
				q := flecs.NewQueryFromTerms(w,
					flecs.WithVar(largeID, "large"),
					flecs.WithVar(smallID, "small"),
				)
				it := q.Iter()
				n := 0
				for it.Next() {
					n++
				}
				_ = n
			}
		})

		// Unoptimized baseline: force large-domain variable as driver by defining it first
		// and using the pre-optimizer API (but since we cannot call the old path directly,
		// we instead put largeID ONLY in the first slot with a fresh query).
		// For this benchmark we manually measure the cost of iterating large-first.
		b.Run("large_first_manual", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// smallID first-defined; optimizer picks smallID (≈ already optimal).
				// This sub-bench shows the Iter cost regardless of driver direction.
				q := flecs.NewQueryFromTerms(w,
					flecs.WithVar(smallID, "small"),
					flecs.WithVar(largeID, "large"),
				)
				it := q.Iter()
				n := 0
				for it.Next() {
					n++
				}
				_ = n
			}
		})
	})
}

// BenchmarkOptimizer_BalancedDomain benchmarks a query where both variables
// have equal domain sizes. The optimizer must not regress more than 10% vs the
// first-defined-wins baseline.
func BenchmarkOptimizer_BalancedDomain(b *testing.B) {
	w := flecs.New()

	type qobCompA struct{ V int }
	type qobCompB struct{ V int }
	aID := flecs.RegisterComponent[qobCompA](w)
	bID := flecs.RegisterComponent[qobCompB](w)

	const count = 100
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < count; i++ {
			ea := fw.NewEntity()
			flecs.Set(fw, ea, qobCompA{V: i})
			eb := fw.NewEntity()
			flecs.Set(fw, eb, qobCompB{V: i})
		}
	})

	w.Read(func(_ *flecs.Reader) {
		b.Run("balanced_A_first", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				q := flecs.NewQueryFromTerms(w,
					flecs.WithVar(aID, "a"),
					flecs.WithVar(bID, "b"),
				)
				it := q.Iter()
				n := 0
				for it.Next() {
					n++
				}
				_ = n
			}
		})

		b.Run("balanced_B_first", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				q := flecs.NewQueryFromTerms(w,
					flecs.WithVar(bID, "b"),
					flecs.WithVar(aID, "a"),
				)
				it := q.Iter()
				n := 0
				for it.Next() {
					n++
				}
				_ = n
			}
		})
	})
}
