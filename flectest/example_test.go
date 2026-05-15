package flectest_test

import (
	"fmt"
	"testing"

	"github.com/snichols/flecs"
	"github.com/snichols/flecs/flectest"
)

type HP struct{ Value int }
type Speed struct{ V float32 }

// exampleTB is a minimal testing.TB for runnable Example functions.
// Embed testing.TB as a nil field to satisfy the unexported private() method;
// override only the methods actually invoked by flectest helpers.
type exampleTB struct {
	testing.TB // nil — satisfies unexported private()
}

func (e *exampleTB) Helper()                   {}
func (e *exampleTB) Fatalf(_ string, _ ...any) {}
func (e *exampleTB) Errorf(_ string, _ ...any) {}
func (e *exampleTB) Cleanup(fn func())         { fn() }

// ExampleNewWorld demonstrates creating a test world and asserting on it.
func ExampleNewWorld() {
	tb := &exampleTB{}

	w := flectest.NewWorld(tb)
	flecs.RegisterComponent[HP](w)

	var hero flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hero = fw.NewEntity()
		fw.SetName(hero, "hero")
		flecs.Set(fw, hero, HP{Value: 100})
	})

	flectest.AssertAlive(tb, w, hero)
	flectest.AssertName(tb, w, hero, "hero")
	flectest.AssertComponentValue[HP](tb, w, hero, HP{Value: 100})

	fmt.Println("assertions passed")
	// Output: assertions passed
}

// ExampleNewWorldWith demonstrates NewWorldWith for setup-heavy scenarios.
func ExampleNewWorldWith() {
	tb := &exampleTB{}

	w := flectest.NewWorldWith(tb, func(fw *flecs.Writer) {
		e := fw.NewEntity()
		fw.SetName(e, "soldier")
		flecs.Set(fw, e, Speed{V: 3.5})
	})

	var names []string
	w.Read(func(fr *flecs.Reader) {
		flecs.Each1[Speed](fr, func(e flecs.ID, _ *Speed) {
			name, _ := fr.GetName(e)
			names = append(names, name)
		})
	})

	fmt.Println(names)
	// Output: [soldier]
}

// ExampleMustEntity demonstrates auto-registering a component via MustEntity.
func ExampleMustEntity() {
	tb := &exampleTB{}
	w := flectest.NewWorld(tb)

	// HP is NOT pre-registered — MustEntity auto-registers it.
	e := flectest.MustEntity(tb, w, "warrior", HP{Value: 50})

	flectest.AssertHasComponent[HP](tb, w, e)
	fmt.Println("warrior has HP")
	// Output: warrior has HP
}

// ExampleMustChild demonstrates the MustChild builder.
func ExampleMustChild() {
	tb := &exampleTB{}
	w := flectest.NewWorld(tb)

	planet := flectest.MustEntity(tb, w, "earth")
	moon := flectest.MustChild(tb, w, planet, "moon")

	flectest.AssertParent(tb, w, moon, planet)
	flectest.AssertChildren(tb, w, planet, moon)
	fmt.Println("moon orbits earth")
	// Output: moon orbits earth
}
