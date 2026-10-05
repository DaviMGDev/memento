package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var instanceCounter uint64

// ComponentOption configures a WASM Component or Factory.
type ComponentOption func(*componentConfig)

type componentConfig struct {
	keys      *KeyRegistry
	name      string
	logWriter io.Writer
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

// WithKeyRegistry configures a custom KeyRegistry.
func WithKeyRegistry(keys *KeyRegistry) ComponentOption {
	return func(c *componentConfig) {
		c.keys = keys
	}
}

// WithModuleName sets a prefix name for module instantiations.
func WithModuleName(name string) ComponentOption {
	return func(c *componentConfig) {
		c.name = name
	}
}

// WithLogWriter forwards guest calls to the memento "log" import to w.
// By default guest log output is discarded.
func WithLogWriter(w io.Writer) ComponentOption {
	return func(c *componentConfig) {
		c.logWriter = w
	}
}

// WithStdin wires r to the guest's WASI standard input (fd 0). Guests that
// read stdin — interactive REPLs, for instance — block their activation
// until the reader is exhausted.
func WithStdin(r io.Reader) ComponentOption {
	return func(c *componentConfig) {
		c.stdin = r
	}
}

// WithStdout wires w to the guest's WASI standard output (fd 1).
func WithStdout(w io.Writer) ComponentOption {
	return func(c *componentConfig) {
		c.stdout = w
	}
}

// WithStderr wires w to the guest's WASI standard error (fd 2).
func WithStderr(w io.Writer) ComponentOption {
	return func(c *componentConfig) {
		c.stderr = w
	}
}

// WASMComponent wraps a compiled WASM module and implements runtime.Component.
type WASMComponent struct {
	engine   *Engine
	compiled wazero.CompiledModule
	decls    runtime.Declarations
	keys     *KeyRegistry
	cfg      componentConfig
}

// NewComponent compiles wasmBytes and returns a runtime.Component.
func NewComponent(ctx context.Context, engine *Engine, wasmBytes []byte, opts ...ComponentOption) (*WASMComponent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if engine == nil {
		return nil, fmt.Errorf("wasm: nil engine")
	}
	if len(wasmBytes) == 0 {
		return nil, fmt.Errorf("wasm: empty bytecode")
	}

	var cfg componentConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.keys == nil {
		cfg.keys = NewKeyRegistry()
	}
	if cfg.name == "" {
		cfg.name = "component"
	}

	// Ensure host module is registered in engine
	if err := RegisterHostModule(ctx, engine.runtime); err != nil {
		// Module may already be registered
	}

	compiled, err := engine.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm: compiling module: %w", err)
	}

	// Probe declarations by instantiating a temporary probe instance
	decls, err := probeDeclarations(ctx, engine.runtime, compiled, cfg.keys)
	if err != nil {
		return nil, fmt.Errorf("wasm: inspecting declarations: %w", err)
	}

	return &WASMComponent{
		engine:   engine,
		compiled: compiled,
		decls:    decls,
		keys:     cfg.keys,
		cfg:      cfg,
	}, nil
}

func probeDeclarations(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule, keys *KeyRegistry) (runtime.Declarations, error) {
	probeName := fmt.Sprintf("_probe_%d", atomic.AddUint64(&instanceCounter, 1))
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(probeName))
	if err != nil {
		return runtime.Declarations{}, fmt.Errorf("instantiating probe module: %w", err)
	}
	defer mod.Close(ctx)

	if err := initializeReactors(ctx, mod); err != nil {
		return runtime.Declarations{}, err
	}

	declFn := mod.ExportedFunction("memento_declare")
	if declFn == nil {
		return runtime.Declarations{}, nil
	}

	st := &execState{}
	execCtx := withExecState(ctx, st)
	res, err := declFn.Call(execCtx)
	if err != nil {
		return runtime.Declarations{}, fmt.Errorf("calling memento_declare: %w", err)
	}
	if len(res) > 0 && res[0] != 0 {
		return runtime.Declarations{}, fmt.Errorf("memento_declare failed with code %d", res[0])
	}

	var decls runtime.Declarations
	for _, name := range st.injected {
		decls.Inject = append(decls.Inject, keys.GetOrCreate(name))
	}
	for _, name := range st.provided {
		decls.Provide = append(decls.Provide, keys.GetOrCreate(name))
	}
	return decls, nil
}

// Declarations implements runtime.Component.
func (c *WASMComponent) Declarations() runtime.Declarations {
	return c.decls
}

// Activate implements runtime.Component.
func (c *WASMComponent) Activate(inst *runtime.Instance, payload any) error {
	ctx := context.Background()
	var payloadBytes []byte
	if payload != nil {
		switch p := payload.(type) {
		case []byte:
			payloadBytes = p
		case string:
			payloadBytes = []byte(p)
		default:
			data, err := json.Marshal(p)
			if err != nil {
				return fmt.Errorf("wasm: serializing payload: %w", err)
			}
			payloadBytes = data
		}
	}

	instID := atomic.AddUint64(&instanceCounter, 1)
	modName := fmt.Sprintf("%s_%d_%d", c.cfg.name, inst.FiberID(), instID)

	mod, err := c.engine.runtime.InstantiateModule(ctx, c.compiled, c.moduleConfig(modName))
	if err != nil {
		return fmt.Errorf("wasm: instantiating module instance: %w", err)
	}
	if err := initializeReactors(ctx, mod); err != nil {
		_ = mod.Close(ctx)
		return err
	}

	var modMu sync.Mutex

	st := &execState{
		instance:     inst,
		payloadBytes: payloadBytes,
		mod:          mod,
		modMu:        &modMu,
		logWriter:    c.cfg.logWriter,
		keys:         c.keys,
		decls:        c.decls,
		bindings:     c.engine.bindings,
	}

	// Push module cleanup as the bottom-most effect so it closes after all guest inverses run.
	if err := inst.Context().RegisterEffect(func() (func() error, error) {
		return func() error {
			modMu.Lock()
			defer modMu.Unlock()
			c.engine.bindings.forget(st)
			return mod.Close(context.Background())
		}, nil
	}); err != nil {
		_ = mod.Close(ctx)
		return fmt.Errorf("wasm: registering cleanup effect: %w", err)
	}

	if actFn := mod.ExportedFunction("memento_activate"); actFn != nil {
		execCtx := withExecState(ctx, st)
		modMu.Lock()
		res, err := actFn.Call(execCtx)
		modMu.Unlock()
		if err != nil {
			return fmt.Errorf("wasm: activate invocation failed: %w", err)
		}
		if len(res) > 0 && res[0] != 0 {
			return fmt.Errorf("wasm: activate returned error code %d", res[0])
		}
	}

	// Declaring a provide is a promise: activation must register it.
	for _, k := range c.decls.Provide {
		if owner, ok := inst.Context().LookupOwner(k.ID()); !ok || owner != inst.FiberID() {
			return fmt.Errorf("wasm: component %s declared provide %q but did not bind it", c.cfg.name, k.Name())
		}
	}

	return nil
}

// NewFactory returns a loader.Factory that yields a WASMComponent.
func NewFactory(engine *Engine, wasmBytes []byte, opts ...ComponentOption) loader.Factory {
	return func(payload any) (runtime.Component, error) {
		return NewComponent(context.Background(), engine, wasmBytes, opts...)
	}
}

// moduleConfig builds the wazero module configuration for one instance,
// wiring the configured stdio streams when present.
func (c *WASMComponent) moduleConfig(name string) wazero.ModuleConfig {
	cfg := wazero.NewModuleConfig().WithName(name)
	if c.cfg.stdin != nil {
		cfg = cfg.WithStdin(c.cfg.stdin)
	}
	if c.cfg.stdout != nil {
		cfg = cfg.WithStdout(c.cfg.stdout)
	}
	if c.cfg.stderr != nil {
		cfg = cfg.WithStderr(c.cfg.stderr)
	}
	return cfg
}

// initializeReactors runs the reactor initialization export of WASI-based
// guests when present: wasi-libc uses "_initialize" while Go's c-shared
// wasip1 runtime uses "_rt0_wasm_wasip1_lib". Core wasm guests — with
// neither export — are left untouched.
func initializeReactors(ctx context.Context, mod api.Module) error {
	fn := mod.ExportedFunction("_initialize")
	if fn == nil {
		fn = mod.ExportedFunction("_rt0_wasm_wasip1_lib")
	}
	if fn == nil {
		return nil
	}
	if _, err := fn.Call(ctx); err != nil {
		return fmt.Errorf("wasm: initializing reactor module: %w", err)
	}
	return nil
}
