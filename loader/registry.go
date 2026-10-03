package loader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DaviMGDev/memento/runtime"
)

// Factory instantiates a component from an entry's configuration payload.
//
// Compiled Go cannot import modules by URL, so the registry replaces module
// resolution: it is populated at link time and maps each component
// reference an entry may name to the code that instantiates it.
type Factory func(payload any) (runtime.Component, error)

// Registry maps component references to factories.
//
// Registration is expected at initialization time, before reconciliation
// begins; the registry is not synchronized for concurrent writes.
type Registry struct {
	factories map[string]Factory
	luaDir    string
	luaKeys   *LuaKeys
}

// NewRegistry returns an empty component registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register binds ref to f. Empty references, nil factories, and duplicate
// references are refused.
func (r *Registry) Register(ref string, f Factory) error {
	if ref == "" {
		return errors.New("loader: cannot register an empty component reference")
	}
	if f == nil {
		return fmt.Errorf("loader: cannot register a nil factory for %q", ref)
	}
	if _, exists := r.factories[ref]; exists {
		return fmt.Errorf("loader: component reference %q is already registered", ref)
	}
	r.factories[ref] = f
	return nil
}

// Resolve returns the factory registered for ref, or a descriptive error
// when the reference is unknown.
func (r *Registry) Resolve(ref string) (Factory, error) {
	f, ok := r.factories[ref]
	if ok {
		return f, nil
	}
	if path, name, ok := r.luaPath(ref); ok {
		source, err := os.ReadFile(path)
		if err == nil {
			return luaFactory(name, source, r.luaKeys)
		}
	}
	return nil, fmt.Errorf("loader: unknown component reference %q", ref)
}

// RegisterLuaDir enables the Lua fallback for component references ending in
// .lua. References are resolved relative to dir and may not escape it. Go
// factories registered with Register always take precedence. Scripts are
// trusted host code; this does not provide a sandbox. The module chunk runs
// during resolution and activation, so top-level code should only construct
// and return its module; lifecycle effects belong in activate or ctx:effect.
func (r *Registry) RegisterLuaDir(dir string, keys *LuaKeys) error {
	if dir == "" {
		return errors.New("loader: Lua plugin directory cannot be empty")
	}
	if keys == nil {
		return errors.New("loader: Lua key registry cannot be nil")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("loader: cannot access Lua plugin directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("loader: Lua plugin path is not a directory")
	}
	if r.luaDir != "" {
		return errors.New("loader: Lua plugin directory is already registered")
	}
	r.luaDir = dir
	r.luaKeys = keys
	return nil
}

// Has reports whether ref is registered.
func (r *Registry) Has(ref string) bool {
	if _, ok := r.factories[ref]; ok {
		return true
	}
	path, _, ok := r.luaPath(ref)
	if !ok {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (r *Registry) luaPath(ref string) (path, name string, ok bool) {
	if r.luaDir == "" || r.luaKeys == nil {
		return "", "", false
	}
	name = filepath.Clean(ref)
	if !filepath.IsLocal(name) || filepath.Ext(name) != ".lua" {
		return "", "", false
	}
	root, err := filepath.EvalSymlinks(r.luaDir)
	if err != nil {
		return "", "", false
	}
	path, err = filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return path, name, true
}
