package loader_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

type luaValueReader struct {
	key    context.Key[string]
	mu     sync.Mutex
	values []string
}

func (c *luaValueReader) Declarations() runtime.Declarations {
	return runtime.Declarations{Inject: []context.AnyKey{c.key}}
}
func (c *luaValueReader) Activate(inst *runtime.Instance, _ any) error {
	value, ok := runtime.Get(inst, c.key)
	if !ok {
		return nil
	}
	c.mu.Lock()
	c.values = append(c.values, value)
	c.mu.Unlock()
	return nil
}
func (c *luaValueReader) observed() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.values...)
}

func TestLuaPluginLoadsReconfiguresAndUnloads(t *testing.T) {
	key := context.NewKey[string]("lua-value")
	keys := loader.NewLuaKeys()
	if err := loader.RegisterLuaKey(keys, "value", key, nil, func(v any) (string, error) {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want string, got %T", v)
		}
		return s, nil
	}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	script := `return {
  provide = {"value"},
  activate = function(ctx, payload)
    ctx:bind("value", payload.value)
    ctx:effect(function()
      local file = assert(io.open(payload.path, "w"))
      file:write(payload.value)
      file:close()
      return function() os.remove(payload.path) end
    end)
  end
}`
	if err := os.WriteFile(filepath.Join(dir, "provider.lua"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}

	registry := loader.NewRegistry()
	if err := registry.RegisterLuaDir(dir, keys); err != nil {
		t.Fatal(err)
	}
	reader := &luaValueReader{key: key}
	if err := registry.Register("reader", func(any) (runtime.Component, error) { return reader, nil }); err != nil {
		t.Fatal(err)
	}

	sched := runtime.New()
	defer sched.Close()
	entries := loader.New(sched, registry)
	effectPath := filepath.Join(t.TempDir(), "lua-effect")
	apply := func(value string, enabled bool) error {
		tree, err := loader.NewTree(
			loader.Entry{ID: "provider", Component: "provider.lua", Payload: map[string]any{"value": value, "path": effectPath}, Enabled: enabled},
			loader.Entry{ID: "reader", Component: "reader", Enabled: true},
		)
		if err != nil {
			return err
		}
		return entries.Reconcile(tree)
	}

	if err := apply("first", true); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if data, err := os.ReadFile(effectPath); err != nil || string(data) != "first" {
		t.Fatalf("Lua effect file = (%q, %v), want first", data, err)
	}
	if err := apply("second", true); err != nil {
		t.Fatalf("payload reconcile: %v", err)
	}
	if data, err := os.ReadFile(effectPath); err != nil || string(data) != "second" {
		t.Fatalf("Lua effect file after reload = (%q, %v), want second", data, err)
	}
	if got := reader.observed(); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("observed values = %v, want [first second]", got)
	}
	if err := apply("second", false); err != nil {
		t.Fatalf("disable reconcile: %v", err)
	}
	if id, ok := entries.Fiber("provider"); ok {
		t.Fatalf("disabled Lua provider still has fiber %d", id)
	}
	if _, err := os.Stat(effectPath); !os.IsNotExist(err) {
		t.Fatalf("Lua effect remains after unload: stat error = %v", err)
	}
	if err := apply("second", false); err != nil {
		t.Fatalf("repeated disabled reconcile: %v", err)
	}
}

func TestLuaPluginRollsBackFailedActivation(t *testing.T) {
	key := context.NewKey[string]("lua-failure")
	keys := loader.NewLuaKeys()
	if err := loader.RegisterLuaKey(keys, "value", key, nil, func(v any) (string, error) {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want string, got %T", v)
		}
		return s, nil
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := `return {
  provide = {"value"},
  activate = function(ctx, payload)
    ctx:bind("value", "installed")
    ctx:effect(function()
      local file = assert(io.open(payload.path, "w"))
      file:write("active")
      file:close()
      return function() os.remove(payload.path) end
    end)
    error("activation failed")
  end
}`
	if err := os.WriteFile(filepath.Join(dir, "broken.lua"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	registry := loader.NewRegistry()
	if err := registry.RegisterLuaDir(dir, keys); err != nil {
		t.Fatal(err)
	}
	sched := runtime.New()
	defer sched.Close()
	entries := loader.New(sched, registry)
	effectPath := filepath.Join(t.TempDir(), "failed-effect")
	tree, err := loader.NewTree(loader.Entry{ID: "broken", Component: "broken.lua", Payload: map[string]any{"path": effectPath}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := entries.Reconcile(tree); err != nil {
		t.Fatalf("reconcile failed before settling the failed Lua fiber: %v", err)
	}
	id, ok := entries.Fiber("broken")
	if !ok {
		t.Fatal("failed Lua entry has no inspectable fiber")
	}
	info, ok := sched.Inspect(id)
	if !ok || info.State != runtime.StateFailed {
		t.Fatalf("Lua fiber = (%+v, %v), want failed", info, ok)
	}
	if _, err := os.Stat(effectPath); !os.IsNotExist(err) {
		t.Fatalf("partial Lua effect remains after rollback: stat error = %v", err)
	}
}
