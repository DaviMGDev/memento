package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	spc "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	rt "github.com/DaviMGDev/memento/runtime"
	"github.com/cucumber/godog"
)

func registerLuaSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a Lua plugin "([^"]*)" providing key "([^"]*)"$`, stepLuaPluginRegistered)
	sc.Step(`^the Lua composition is applied with payload "([^"]*)"$`, stepLuaCompositionApplied)
	sc.Step(`^the reader observes "([^"]*)"$`, stepLuaReaderObserves)
	sc.Step(`^the Lua provider is disabled$`, stepLuaProviderDisabled)
	sc.Step(`^the Lua provider is unloaded and its reader deactivates$`, stepLuaProviderAndReaderUnloaded)
}

func stepLuaPluginRegistered(ctx context.Context, ref, keyName string) error {
	w := worldFrom(ctx)
	dir, err := os.MkdirTemp("", "memento-lua-*")
	if err != nil {
		return err
	}
	w.luaDir, w.luaKeyName, w.luaPluginRef = dir, keyName, ref
	w.luaKey = w.key("lua:" + keyName)
	keys := loader.NewLuaKeys()
	if err := loader.RegisterLuaKey(keys, keyName, w.luaKey, nil, func(value any) (string, error) {
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("expected string value, got %T", value)
		}
		return text, nil
	}); err != nil {
		return err
	}
	if err := w.reg.RegisterLuaDir(dir, keys); err != nil {
		return err
	}
	source := fmt.Sprintf(`return {
  provide = {%q},
  activate = function(ctx, payload) ctx:bind(%q, payload) end
}`, keyName, keyName)
	return os.WriteFile(filepath.Join(dir, ref), []byte(source), 0600)
}

func stepLuaCompositionApplied(ctx context.Context, value string) error {
	w := worldFrom(ctx)
	readerRef := "lua-reader"
	if err := w.reg.Register(readerRef, func(any) (rt.Component, error) {
		return &testComponent{
			decls: rt.Declarations{Inject: []spc.AnyKey{w.luaKey}},
			activate: func(inst *rt.Instance, _ any) error {
				observed, ok := rt.Get(inst, w.luaKey)
				if !ok {
					return fmt.Errorf("Lua binding %q was not readable", w.luaKeyName)
				}
				w.recordSet("lua-observed", observed)
				return nil
			},
		}, nil
	}); err != nil {
		return err
	}
	if strings.Contains(w.luaPluginRef, string(filepath.Separator)) {
		return fmt.Errorf("feature plugin ref must be a local filename")
	}
	tree, err := luaConfigTree(
		loader.Entry{ID: "lua-provider", Component: w.luaPluginRef, Payload: value, Enabled: true},
		loader.Entry{ID: "lua-reader", Component: readerRef, Enabled: true},
	)
	if err != nil {
		return err
	}
	return w.ld.Reconcile(tree)
}

func stepLuaReaderObserves(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	if got := w.recordSnapshot()["lua-observed"]; got != want {
		return fmt.Errorf("Lua reader observed %q, want %q", got, want)
	}
	return nil
}

func stepLuaProviderDisabled(ctx context.Context) error {
	w := worldFrom(ctx)
	tree, err := luaConfigTree(
		loader.Entry{ID: "lua-provider", Component: w.luaPluginRef, Payload: "ignored", Enabled: false},
		loader.Entry{ID: "lua-reader", Component: "lua-reader", Enabled: true},
	)
	if err != nil {
		return err
	}
	return w.ld.Reconcile(tree)
}

func stepLuaProviderAndReaderUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	if _, ok := w.ld.Fiber("lua-provider"); ok {
		return fmt.Errorf("Lua provider still has a fiber")
	}
	fiber, ok := w.ld.Fiber("lua-reader")
	if !ok {
		return fmt.Errorf("reader fiber is missing")
	}
	if state, ok := w.stateOf(fiber); !ok || state != rt.StateInactive {
		return fmt.Errorf("reader state = %v (present=%v), want inactive", state, ok)
	}
	return nil
}

func luaConfigTree(entries ...loader.Entry) (*loader.Tree, error) { return loader.NewTree(entries...) }
