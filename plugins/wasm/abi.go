package wasm

import (
	"context"
	"fmt"
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
			res, err := revertFn.Call(context.Background(), uint64(effectID))
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

func hostLog(ctx context.Context, m api.Module, ptr, length uint32) uint32 {
	// Debug logging hook; errors silently ignored.
	_, _ = readString(m, ptr, length)
	return 0
}
