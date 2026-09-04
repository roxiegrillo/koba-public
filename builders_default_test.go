package koba

import "testing"

func TestDefaultAppConstructorsInitializeGinRuntime(t *testing.T) {
	constructors := []struct {
		name string
		make func() KobaApp
	}{
		{name: "compatibility", make: MakeDefaultKobaApp},
		{name: "options", make: func() KobaApp { return MakeDefaultKobaAppWithOptions() }},
	}

	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			app := constructor.make()
			if app == nil {
				t.Fatal("constructor returned a nil app")
			}
			ginApp, ok := app.(WithRouter)
			if !ok || ginApp.Router() == nil {
				t.Fatalf("constructed app %T has no initialized Gin runtime", app)
			}
		})
	}
}
