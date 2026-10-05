package wasm

import (
	"context"
	"fmt"
	"io"
	"sync"

	mcontext "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/runtime"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostModuleName is the import module name through which WASM modules access Memento ABI.
const HostModuleName = "memento"

type execState struct {
	mu           sync.Mutex
	instance     *runtime.Instance
	fiberCtx     *mcontext.Context
	payloadBytes []byte
	injected     []string
	provided     []string
	mod          api.Module
	modMu        *sync.Mutex
	logWriter    io.Writer
	keys         *KeyRegistry
	decls        runtime.Declarations
	bindings     *bindingTable
	bound        []mcontext.KeyID
}

type execCtxKey struct{}

func withExecState(ctx context.Context, s *execState) context.Context {
	return context.WithValue(ctx, execCtxKey{}, s)
}

func getExecState(ctx context.Context) *execState {
	if s, ok := ctx.Value(execCtxKey{}).(*execState); ok {
		return s
	}
	return nil
}

// RegisterHostModule exports Memento host ABI functions to the wazero runtime.
func RegisterHostModule(ctx context.Context, r wazero.Runtime) error {
	if r.Module(HostModuleName) != nil {
		return nil
	}
	_, err := r.NewHostModuleBuilder(HostModuleName).
		NewFunctionBuilder().
		WithFunc(hostDeclareInject).
		Export("declare_inject").
		NewFunctionBuilder().
		WithFunc(hostDeclareProvide).
		Export("declare_provide").
		NewFunctionBuilder().
		WithFunc(hostBind).
		Export("bind").
		NewFunctionBuilder().
		WithFunc(hostGetLen).
		Export("get_len").
		NewFunctionBuilder().
		WithFunc(hostGet).
		Export("get").
		NewFunctionBuilder().
		WithFunc(hostGetPayloadLen).
		Export("get_payload_len").
		NewFunctionBuilder().
		WithFunc(hostGetPayload).
		Export("get_payload").
		NewFunctionBuilder().
		WithFunc(hostRegisterEffect).
		Export("register_effect").
		NewFunctionBuilder().
		WithFunc(hostLog).
		Export("log").
		Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("wasm: instantiating host module %q: %w", HostModuleName, err)
	}
	return nil
}

func readString(m api.Module, ptr, length uint32) (string, error) {
	mem := m.Memory()
	if mem == nil {
		return "", fmt.Errorf("module memory not exported")
	}
	buf, ok := mem.Read(ptr, length)
	if !ok {
		return "", fmt.Errorf("memory read out of bounds at offset %d, len %d", ptr, length)
	}
	return string(buf), nil
}

func hostDeclareInject(ctx context.Context, m api.Module, ptr, length uint32) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 1
	}
	str, err := readString(m, ptr, length)
	if err != nil {
		return 1
	}
	s.mu.Lock()
	s.injected = append(s.injected, str)
	s.mu.Unlock()
	return 0
}

func hostDeclareProvide(ctx context.Context, m api.Module, ptr, length uint32) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 1
	}
	str, err := readString(m, ptr, length)
	if err != nil {
		return 1
	}
	s.mu.Lock()
	s.provided = append(s.provided, str)
	s.mu.Unlock()
	return 0
}

func hostGetPayloadLen(ctx context.Context, m api.Module) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 0
	}
	return uint32(len(s.payloadBytes))
}

func hostGetPayload(ctx context.Context, m api.Module, bufPtr, maxLen uint32) uint32 {
	s := getExecState(ctx)
	if s == nil || len(s.payloadBytes) == 0 {
		return 0
	}
	mem := m.Memory()
	if mem == nil {
		return 0
	}
	toCopy := uint32(len(s.payloadBytes))
	if toCopy > maxLen {
		toCopy = maxLen
	}
	if !mem.Write(bufPtr, s.payloadBytes[:toCopy]) {
		return 0
	}
	return toCopy
}

func hostRegisterEffect(ctx context.Context, m api.Module, effectID uint32) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 1
	}

	targetCtx := s.fiberCtx
	if s.instance != nil {
		targetCtx = s.instance.Context()
	}
	if targetCtx == nil {
		return 1
	}

	mod := s.mod
	modMu := s.modMu
	// The inverse runs after activation returns; carry the exec context so
	// host imports invoked from memento_revert_effect (e.g. log) still see
	// the module's execState.
	execCtx := ctx
	// Register effect on fiber context with inverse callback.
	err := targetCtx.RegisterEffect(func() (func() error, error) {
		inverse := func() error {
			if modMu != nil {
				modMu.Lock()
				defer modMu.Unlock()
			}
			revertFn := mod.ExportedFunction("memento_revert_effect")
			if revertFn == nil {
				return nil
			}
			res, err := revertFn.Call(execCtx, uint64(effectID))
			if err != nil {
				return fmt.Errorf("wasm revert effect %d failed: %w", effectID, err)
			}
			if len(res) > 0 && res[0] != 0 {
				return fmt.Errorf("wasm revert effect %d returned error code %d", effectID, res[0])
			}
			return nil
		}
		return inverse, nil
	})
	if err != nil {
		return 1
	}
	return 0
}

// hostLog forwards guest log output to the execState log writer, when one
// is configured. Logging is best-effort: errors are silently ignored.
func hostLog(ctx context.Context, m api.Module, ptr, length uint32) uint32 {
	s := getExecState(ctx)
	if s == nil || s.logWriter == nil {
		return 0
	}
	str, err := readString(m, ptr, length)
	if err != nil {
		return 0
	}
	_, _ = io.WriteString(s.logWriter, str)
	return 0
}

// hostBind registers the value of a declared provided key on the calling
// fiber. The kernel's Bind installs it as a revertible effect, so unloading
// withdraws the registration with the rest of the fiber's effects.
func hostBind(ctx context.Context, m api.Module, keyPtr, keyLen, valPtr, valLen uint32) uint32 {
	s := getExecState(ctx)
	if s == nil || s.instance == nil {
		return 1
	}
	name, err := readString(m, keyPtr, keyLen)
	if err != nil {
		return 1
	}
	key, ok := s.lookupKey(name)
	if !ok || !declaresKey(s.decls.Provide, key.ID()) {
		return 1
	}
	value, err := readBytes(m, valPtr, valLen)
	if err != nil {
		return 1
	}
	if err := runtime.Bind(s.instance, key, any(value)); err != nil {
		return 1
	}
	s.mu.Lock()
	s.bound = append(s.bound, key.ID())
	s.mu.Unlock()
	s.bindings.set(key.ID(), s)
	return 0
}

// hostGetLen returns the length of the value resolved for a declared
// injected key, or 0 when absent.
func hostGetLen(ctx context.Context, m api.Module, keyPtr, keyLen uint32) uint32 {
	value, ok := readValue(ctx, m, keyPtr, keyLen)
	if !ok {
		return 0
	}
	return uint32(len(value))
}

// hostGet copies the value resolved for a declared injected key into the
// caller's buffer, at most bufMax bytes, and returns the count copied.
func hostGet(ctx context.Context, m api.Module, keyPtr, keyLen, bufPtr, bufMax uint32) uint32 {
	value, ok := readValue(ctx, m, keyPtr, keyLen)
	if !ok {
		return 0
	}
	if uint32(len(value)) > bufMax {
		value = value[:bufMax]
	}
	mem := m.Memory()
	if mem == nil || !mem.Write(bufPtr, value) {
		return 0
	}
	return uint32(len(value))
}

// readValue resolves the bytes bound at a declared injected key through the
// calling instance's committed view.
func readValue(ctx context.Context, m api.Module, keyPtr, keyLen uint32) ([]byte, bool) {
	s := getExecState(ctx)
	if s == nil || s.instance == nil {
		return nil, false
	}
	name, err := readString(m, keyPtr, keyLen)
	if err != nil {
		return nil, false
	}
	key, ok := s.lookupKey(name)
	if !ok || !declaresKey(s.decls.Inject, key.ID()) {
		return nil, false
	}
	v, ok := runtime.Get(s.instance, key)
	if !ok {
		return nil, false
	}
	switch b := v.(type) {
	case []byte:
		return b, true
	case string:
		return []byte(b), true
	default:
		return nil, false
	}
}

// lookupKey resolves a guest-visible key name through the state's registry.
// The ABI value surface requires the registry's default Key[any]; a custom
// typed key cannot carry bytes and is reported as absent.
func (s *execState) lookupKey(name string) (mcontext.Key[any], bool) {
	if s.keys == nil {
		return mcontext.Key[any]{}, false
	}
	k, ok := s.keys.Get(name)
	if !ok {
		return mcontext.Key[any]{}, false
	}
	typed, ok := k.(mcontext.Key[any])
	return typed, ok
}

// declaresKey reports whether id is in a declaration set.
func declaresKey(keys []mcontext.AnyKey, id mcontext.KeyID) bool {
	for _, k := range keys {
		if k.ID() == id {
			return true
		}
	}
	return false
}

// readBytes copies length bytes of module memory, tolerating zero length.
func readBytes(m api.Module, ptr, length uint32) ([]byte, error) {
	mem := m.Memory()
	if mem == nil {
		return nil, fmt.Errorf("module memory not exported")
	}
	if length == 0 {
		return nil, nil
	}
	buf, ok := mem.Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("memory read out of bounds at offset %d, len %d", ptr, length)
	}
	return append([]byte(nil), buf...), nil
}
