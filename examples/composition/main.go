// Command composition demonstrates instance-level dynamic composition: a
// declarative configuration loads two components, one of them reconfigures,
// and unloading reclaims every effect they installed.
package main

import (
	"fmt"
	"os"

	spc "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	rt "github.com/DaviMGDev/memento/runtime"
)

var storageKey = spc.NewKey[string]("storage")

// database provides the storage key, binding the DSN from its payload.
type database struct{}

func (database) Declarations() rt.Declarations {
	return rt.Declarations{Provide: []spc.AnyKey{storageKey}}
}

func (database) Activate(inst *rt.Instance, payload any) error {
	dsn, _ := payload.(string)
	fmt.Printf("database: bound storage to %s\n", dsn)
	return rt.Bind(inst, storageKey, dsn)
}

// console injects the storage key and reports the DSN it reads.
type console struct{}

func (console) Declarations() rt.Declarations {
	return rt.Declarations{Inject: []spc.AnyKey{storageKey}}
}

func (console) Activate(inst *rt.Instance, _ any) error {
	dsn, ok := rt.Get(inst, storageKey)
	if !ok {
		return fmt.Errorf("storage is not available")
	}
	fmt.Printf("console: connected to %s\n", dsn)
	return inst.Context().RegisterEffect(func() (func() error, error) {
		return func() error {
			fmt.Printf("console: disconnected from %s\n", dsn)
			return nil
		}, nil
	})
}

func main() {
	sched := rt.New()
	defer sched.Close()

	registry := loader.NewRegistry()
	must(registry.Register("database", func(payload any) (rt.Component, error) {
		return database{}, nil
	}))
	must(registry.Register("console", func(payload any) (rt.Component, error) {
		return console{}, nil
	}))
	ld := loader.New(sched, registry)

	apply := func(label string, entries ...loader.Entry) {
		fmt.Printf("-- %s\n", label)
		tree, err := loader.NewTree(entries...)
		must(err)
		must(ld.Reconcile(tree))
	}

	apply("apply: database and console",
		loader.Entry{ID: "database", Component: "database", Payload: "postgres://db", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)

	apply("reconfigure: database payload changed",
		loader.Entry{ID: "database", Component: "database", Payload: "postgres://new", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)

	apply("disable: console",
		loader.Entry{ID: "database", Component: "database", Payload: "postgres://new", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: false},
	)

	apply("unload: everything",
		loader.Entry{ID: "database", Component: "database", Payload: "postgres://new", Enabled: false},
	)

	fmt.Printf("fibers: %d\n", len(sched.Snapshot().Fibers))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "composition:", err)
		os.Exit(1)
	}
}
