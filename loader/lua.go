package loader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/runtime"
	"github.com/yuin/gopher-lua"
)

// LuaKeys is the host-controlled set of typed keys visible to Lua plugins.
// Keys are addressed by their registered names in the plugin API.
type LuaKeys struct {
	bindings map[string]luaBinding
}

type luaBinding interface {
	anyKey() context.AnyKey
	get(*runtime.Instance, *lua.LState) (lua.LValue, error)
	bind(*runtime.Instance, *lua.LState, lua.LValue) error
}

type typedLuaBinding[T any] struct {
	key    context.Key[T]
	encode func(T) any
	decode func(any) (T, error)
}

func (b typedLuaBinding[T]) anyKey() context.AnyKey { return b.key }
func (b typedLuaBinding[T]) get(i *runtime.Instance, L *lua.LState) (lua.LValue, error) {
	v, ok := runtime.Get(i, b.key)
	if !ok {
		return lua.LNil, nil
	}
	return goToLua(L, b.encode(v))
}
func (b typedLuaBinding[T]) bind(i *runtime.Instance, L *lua.LState, value lua.LValue) error {
	decoded, err := luaToGo(value)
	if err != nil {
		return err
	}
	v, err := b.decode(decoded)
	if err != nil {
		return err
	}
	return runtime.Bind(i, b.key, v)
}

// NewLuaKeys returns an empty registry of host-exposed Lua keys.
func NewLuaKeys() *LuaKeys { return &LuaKeys{bindings: make(map[string]luaBinding)} }

// RegisterLuaKey exposes a typed runtime key to Lua under name. encode and
// decode define the boundary representation; nil encode uses JSON-compatible
// Go values. Names must be unique, and keys must be non-zero.
func RegisterLuaKey[T any](keys *LuaKeys, name string, key context.Key[T], encode func(T) any, decode func(any) (T, error)) error {
	if keys == nil {
		return errors.New("loader: Lua key registry cannot be nil")
	}
	if name == "" || key.IsZero() || decode == nil {
		return errors.New("loader: Lua key requires a name, non-zero key, and decoder")
	}
	if keys.bindings == nil {
		keys.bindings = make(map[string]luaBinding)
	}
	if _, exists := keys.bindings[name]; exists {
		return fmt.Errorf("loader: Lua key %q is already registered", name)
	}
	if encode == nil {
		encode = func(v T) any { return v }
	}
	keys.bindings[name] = typedLuaBinding[T]{key: key, encode: encode, decode: decode}
	return nil
}

type luaComponent struct {
	name    string
	source  []byte
	keys    *LuaKeys
	decls   runtime.Declarations
	inject  map[string]luaBinding
	provide map[string]luaBinding
}

func luaFactory(name string, source []byte, keys *LuaKeys) (Factory, error) {
	component := &luaComponent{name: name, source: append([]byte(nil), source...), keys: keys, inject: make(map[string]luaBinding), provide: make(map[string]luaBinding)}
	L := lua.NewState()
	defer L.Close()
	module, err := loadLuaModule(L, name, source)
	if err != nil {
		return nil, fmt.Errorf("loader: Lua plugin %q: %w", name, err)
	}
	if err := component.readDeclarations(module); err != nil {
		return nil, fmt.Errorf("loader: Lua plugin %q: %w", name, err)
	}
	return func(any) (runtime.Component, error) { return component, nil }, nil
}

func (c *luaComponent) Declarations() runtime.Declarations { return c.decls }

func (c *luaComponent) readDeclarations(module *lua.LTable) error {
	if module.RawGetString("activate").Type() != lua.LTFunction {
		return errors.New("module must return a table with an activate function")
	}
	for _, name := range []string{"inject", "provide"} {
		value := module.RawGetString(name)
		if value == lua.LNil {
			continue
		}
		list, ok := value.(*lua.LTable)
		if !ok {
			return fmt.Errorf("%s must be an array of registered key names", name)
		}
		seen := make(map[string]bool)
		for i := 1; i <= list.Len(); i++ {
			keyName, ok := list.RawGetInt(i).(lua.LString)
			if !ok || string(keyName) == "" {
				return fmt.Errorf("%s[%d] must be a key name", name, i)
			}
			keyNameString := string(keyName)
			binding, ok := c.keys.bindings[keyNameString]
			if !ok {
				return fmt.Errorf("%s references unregistered Lua key %q", name, keyNameString)
			}
			if seen[keyNameString] {
				return fmt.Errorf("%s declares Lua key %q more than once", name, keyNameString)
			}
			seen[keyNameString] = true
			if name == "inject" {
				c.inject[keyNameString] = binding
				c.decls.Inject = append(c.decls.Inject, binding.anyKey())
			} else {
				c.provide[keyNameString] = binding
				c.decls.Provide = append(c.decls.Provide, binding.anyKey())
			}
		}
	}
	for name := range c.inject {
		if _, ok := c.provide[name]; ok {
			return fmt.Errorf("Lua key %q cannot be both injected and provided", name)
		}
	}
	return nil
}

func (c *luaComponent) Activate(inst *runtime.Instance, payload any) error {
	L := lua.NewState()
	lease := &luaLease{state: L}
	defer func() {
		if lease.refs == 0 {
			lease.close()
		}
	}()
	module, err := loadLuaModule(L, c.name, c.source)
	if err != nil {
		return err
	}
	if err := c.validateDeclarations(module); err != nil {
		return err
	}
	activate, ok := module.RawGetString("activate").(*lua.LFunction)
	if !ok {
		return errors.New("module activate member is not a function")
	}
	ctx := L.NewTable()
	ctx.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(2)
		binding, ok := c.inject[name]
		if !ok {
			L.RaiseError("key %q is not declared as injected", name)
			return 0
		}
		value, err := binding.get(inst, L)
		if err != nil {
			L.RaiseError("read key %q: %v", name, err)
			return 0
		}
		L.Push(value)
		return 1
	}))
	ctx.RawSetString("bind", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(2)
		binding, ok := c.provide[name]
		if !ok {
			L.RaiseError("key %q is not declared as provided", name)
			return 0
		}
		if err := binding.bind(inst, L, L.Get(3)); err != nil {
			L.RaiseError("bind key %q: %v", name, err)
		}
		return 0
	}))
	ctx.RawSetString("effect", L.NewFunction(func(L *lua.LState) int {
		install := L.CheckFunction(2)
		err := inst.Context().RegisterEffect(func() (func() error, error) {
			if err := L.CallByParam(lua.P{Fn: install, NRet: 1, Protect: true}); err != nil {
				return nil, err
			}
			undo, ok := L.Get(-1).(*lua.LFunction)
			L.Pop(1)
			if !ok {
				return nil, errors.New("Lua effect must return an inverse function")
			}
			lease.refs++
			return func() error {
				defer lease.release()
				return L.CallByParam(lua.P{Fn: undo, NRet: 0, Protect: true})
			}, nil
		})
		if err != nil {
			L.RaiseError("register effect: %v", err)
		}
		return 0
	}))
	payloadValue, err := goToLua(L, payload)
	if err != nil {
		return fmt.Errorf("loader: Lua plugin %q payload: %w", c.name, err)
	}
	return L.CallByParam(lua.P{Fn: activate, NRet: 0, Protect: true}, ctx, payloadValue)
}

func (c *luaComponent) validateDeclarations(module *lua.LTable) error {
	probe := &luaComponent{keys: c.keys, inject: make(map[string]luaBinding), provide: make(map[string]luaBinding)}
	if err := probe.readDeclarations(module); err != nil {
		return err
	}
	if !sameLuaKeys(c.decls.Inject, probe.decls.Inject) || !sameLuaKeys(c.decls.Provide, probe.decls.Provide) {
		return errors.New("module declarations changed between resolution and activation")
	}
	return nil
}

func sameLuaKeys(a, b []context.AnyKey) bool {
	if len(a) != len(b) {
		return false
	}
	want := make(map[context.KeyID]bool, len(a))
	for _, key := range a {
		want[key.ID()] = true
	}
	for _, key := range b {
		if !want[key.ID()] {
			return false
		}
	}
	return true
}

type luaLease struct {
	state  *lua.LState
	refs   int
	closed bool
}

func (l *luaLease) release() {
	l.refs--
	if l.refs == 0 {
		l.close()
	}
}
func (l *luaLease) close() {
	if !l.closed {
		l.closed = true
		l.state.Close()
	}
}

func loadLuaModule(L *lua.LState, name string, source []byte) (*lua.LTable, error) {
	fn, err := L.Load(bytes.NewReader(source), "@"+name)
	if err != nil {
		return nil, err
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return nil, err
	}
	module, ok := L.Get(-1).(*lua.LTable)
	L.Pop(1)
	if !ok {
		return nil, errors.New("script must return a module table")
	}
	return module, nil
}

func goToLua(L *lua.LState, value any) (lua.LValue, error) {
	if value == nil {
		return lua.LNil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("value is not JSON-compatible: %w", err)
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	return jsonToLua(L, normalized)
}

func jsonToLua(L *lua.LState, value any) (lua.LValue, error) {
	switch v := value.(type) {
	case nil:
		return lua.LNil, nil
	case bool:
		return lua.LBool(v), nil
	case string:
		return lua.LString(v), nil
	case json.Number:
		f, err := v.Float64()
		return lua.LNumber(f), err
	case []any:
		t := L.NewTable()
		for i, item := range v {
			converted, err := jsonToLua(L, item)
			if err != nil {
				return nil, err
			}
			t.RawSetInt(i+1, converted)
		}
		return t, nil
	case map[string]any:
		t := L.NewTable()
		for key, item := range v {
			converted, err := jsonToLua(L, item)
			if err != nil {
				return nil, err
			}
			t.RawSetString(key, converted)
		}
		return t, nil
	default:
		return nil, fmt.Errorf("unsupported value %T", value)
	}
}

func luaToGo(value lua.LValue) (any, error) {
	switch v := value.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(v), nil
	case lua.LString:
		return string(v), nil
	case lua.LNumber:
		return float64(v), nil
	case *lua.LTable:
		if v.Len() > 0 {
			out := make([]any, v.Len())
			for i := 1; i <= v.Len(); i++ {
				item, err := luaToGo(v.RawGetInt(i))
				if err != nil {
					return nil, err
				}
				out[i-1] = item
			}
			return out, nil
		}
		out := make(map[string]any)
		var conversionErr error
		v.ForEach(func(key, item lua.LValue) {
			name, ok := key.(lua.LString)
			if !ok {
				conversionErr = errors.New("Lua tables used as values must have string keys or be arrays")
				return
			}
			converted, err := luaToGo(item)
			if err != nil {
				conversionErr = err
				return
			}
			out[string(name)] = converted
		})
		return out, conversionErr
	default:
		return nil, fmt.Errorf("unsupported Lua value %s", strings.ToLower(value.Type().String()))
	}
}
