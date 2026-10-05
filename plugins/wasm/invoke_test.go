package wasm

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DaviMGDev/memento/runtime"
)

// buildHandlerProviderGuest builds a provider that declares provide "cache",
// binds it to "cap" during activation, and exports memento_alloc plus a
// memento_handle that answers every request with "pong".
func buildHandlerProviderGuest() []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}             // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                         // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                   // (i32)->i32
	type3 := []byte{0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32,i32,i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2, type3}))

	importProvide := append(encodeString("memento"), append(encodeString("declare_provide"), 0x00, 0x00)...)
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x02)...)
	importBind := append(encodeString("memento"), append(encodeString("bind"), 0x00, 0x03)...)
	importSec := encodeSection(2, encodeVec([][]byte{importProvide, importEffect, importBind}))

	// Funcs 3..7: declare, activate, revert, alloc, handle.
	funcSec := encodeSection(3, encodeVec([][]byte{
		{0x01}, {0x01}, {0x02}, {0x02}, {0x03},
	}))

	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))

	expMem := append(encodeString("memory"), 0x02, 0x00)
	expDeclare := append(encodeString("memento_declare"), 0x00, 0x03)
	expActivate := append(encodeString("memento_activate"), 0x00, 0x04)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x05)
	expAlloc := append(encodeString("memento_alloc"), 0x00, 0x06)
	expHandle := append(encodeString("memento_handle"), 0x00, 0x07)
	exportSec := encodeSection(7, encodeVec([][]byte{expMem, expDeclare, expActivate, expRevert, expAlloc, expHandle}))

	body3 := []byte{ // declare_provide("cache" at 16)
		0x00,
		0x41, 0x10,
		0x41, 0x05,
		0x10, 0x00,
		0x1a,
		0x41, 0x00, 0x0f, 0x0b,
	}
	body4 := []byte{ // register_effect(7); bind("cache", "cap" at 24)
		0x00,
		0x41, 0x07,
		0x10, 0x01,
		0x1a,
		0x41, 0x10,
		0x41, 0x05,
		0x41, 0x18,
		0x41, 0x03,
		0x10, 0x02,
		0x1a,
		0x41, 0x00, 0x0f, 0x0b,
	}
	body5 := []byte{0x00, 0x41, 0x00, 0x0f, 0x0b}
	body6 := []byte{ // alloc: return 48
		0x00,
		0x41, 0x30,
		0x0f, 0x0b,
	}
	body7 := []byte{ // handle: memory.copy(respPtr, 32, 4); return 4
		0x00,
		0x20, 0x02, // local.get 2 (respPtr)
		0x41, 0x20, // i32.const 32 ("pong")
		0x41, 0x04, // i32.const 4
		0xfc, 0x0a, 0x00, 0x00, // memory.copy
		0x41, 0x04,
		0x0f, 0x0b,
	}
	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body3))), body3...),
		append(encodeLEB128U(uint32(len(body4))), body4...),
		append(encodeLEB128U(uint32(len(body5))), body5...),
		append(encodeLEB128U(uint32(len(body6))), body6...),
		append(encodeLEB128U(uint32(len(body7))), body7...),
	}))

	dataCache := append([]byte{0x00, 0x41, 0x10, 0x0b}, append(encodeLEB128U(5), []byte("cache")...)...)
	dataCap := append([]byte{0x00, 0x41, 0x18, 0x0b}, append(encodeLEB128U(3), []byte("cap")...)...)
	dataPong := append([]byte{0x00, 0x41, 0x20, 0x0b}, append(encodeLEB128U(4), []byte("pong")...)...)
	dataSec := encodeSection(11, encodeVec([][]byte{dataCache, dataCap, dataPong}))

	var wasm bytes.Buffer
	wasm.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	wasm.Write(typeSec)
	wasm.Write(importSec)
	wasm.Write(funcSec)
	wasm.Write(memSec)
	wasm.Write(exportSec)
	wasm.Write(codeSec)
	wasm.Write(dataSec)
	return wasm.Bytes()
}

// buildInvokingGuest builds a guest that optionally declares inject "cache"
// and invokes it during activation, logging the response.
func buildInvokingGuest(declareInject bool) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}                         // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                                     // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                               // (i32)->i32
	type4 := []byte{0x60, 0x06, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32*6)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2, type4}))

	importInject := append(encodeString("memento"), append(encodeString("declare_inject"), 0x00, 0x00)...)
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x02)...)
	importInvoke := append(encodeString("memento"), append(encodeString("invoke"), 0x00, 0x03)...)
	importLog := append(encodeString("memento"), append(encodeString("log"), 0x00, 0x00)...)
	importSec := encodeSection(2, encodeVec([][]byte{importInject, importEffect, importInvoke, importLog}))

	// Funcs 4..6: declare, activate, revert.
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x01}, {0x02}}))

	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))

	expMem := append(encodeString("memory"), 0x02, 0x00)
	expDeclare := append(encodeString("memento_declare"), 0x00, 0x04)
	expActivate := append(encodeString("memento_activate"), 0x00, 0x05)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x06)
	exportSec := encodeSection(7, encodeVec([][]byte{expMem, expDeclare, expActivate, expRevert}))

	body4 := []byte{0x00}
	if declareInject {
		body4 = append(body4,
			0x41, 0x10, // "cache" at 16
			0x41, 0x05,
			0x10, 0x00, // call declare_inject
			0x1a,
		)
	}
	body4 = append(body4, 0x41, 0x00, 0x0f, 0x0b)

	// activate: register_effect(9); n = invoke("cache", "ping" at 32, resp at 48, max 32); log(48, n)
	body5 := []byte{
		0x01, 0x01, 0x7f, // 1 local i32
		0x41, 0x09,
		0x10, 0x01, // call register_effect
		0x1a,
		0x41, 0x10, // key ptr 16
		0x41, 0x05, // key len 5
		0x41, 0x20, // req ptr 32
		0x41, 0x04, // req len 4
		0x41, 0x30, // resp ptr 48
		0x41, 0x20, // resp max 32
		0x10, 0x02, // call invoke
		0x21, 0x00, // local.set 0
		0x41, 0x30, // resp ptr 48
		0x20, 0x00, // len
		0x10, 0x03, // call log
		0x1a,
		0x41, 0x00, 0x0f, 0x0b,
	}
	body6 := []byte{0x00, 0x41, 0x00, 0x0f, 0x0b}
	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body4))), body4...),
		append(encodeLEB128U(uint32(len(body5))), body5...),
		append(encodeLEB128U(uint32(len(body6))), body6...),
	}))

	dataCache := append([]byte{0x00, 0x41, 0x10, 0x0b}, append(encodeLEB128U(5), []byte("cache")...)...)
	dataPing := append([]byte{0x00, 0x41, 0x20, 0x0b}, append(encodeLEB128U(4), []byte("ping")...)...)
	dataSec := encodeSection(11, encodeVec([][]byte{dataCache, dataPing}))

	var wasm bytes.Buffer
	wasm.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	wasm.Write(typeSec)
	wasm.Write(importSec)
	wasm.Write(funcSec)
	wasm.Write(memSec)
	wasm.Write(exportSec)
	wasm.Write(codeSec)
	wasm.Write(dataSec)
	return wasm.Bytes()
}

func TestInvokeRoutesToProviderHandler(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	keys := NewKeyRegistry()
	provider, err := NewComponent(ctx, engine, buildHandlerProviderGuest(), WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	var logged safeBuffer
	caller, err := NewComponent(ctx, engine, buildInvokingGuest(true), WithKeyRegistry(keys), WithLogWriter(&logged))
	if err != nil {
		t.Fatalf("caller: %v", err)
	}

	s := runtime.New()
	defer s.Close()
	pid, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider): %v", err)
	}
	did, err := s.Insert(caller, nil)
	if err != nil {
		t.Fatalf("Insert(caller): %v", err)
	}

	waitForState(t, s, pid, runtime.StateActive)
	waitForState(t, s, did, runtime.StateActive)

	if got := logged.String(); !strings.Contains(got, "pong") {
		t.Fatalf("caller log = %q, want the provider's pong response", got)
	}
}

func TestInvokeWithoutHandlerReturnsNothing(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	keys := NewKeyRegistry()
	provider, err := NewComponent(ctx, engine, buildProviderGuest("v1"), WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	var logged safeBuffer
	caller, err := NewComponent(ctx, engine, buildInvokingGuest(true), WithKeyRegistry(keys), WithLogWriter(&logged))
	if err != nil {
		t.Fatalf("caller: %v", err)
	}

	s := runtime.New()
	defer s.Close()
	if _, err := s.Insert(provider, nil); err != nil {
		t.Fatalf("Insert(provider): %v", err)
	}
	did, err := s.Insert(caller, nil)
	if err != nil {
		t.Fatalf("Insert(caller): %v", err)
	}
	waitForState(t, s, did, runtime.StateActive)

	if got := logged.String(); got != "" {
		t.Fatalf("caller log = %q, want no response from a provider without a handler", got)
	}
}

func TestNonDependentCannotInvoke(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	keys := NewKeyRegistry()
	provider, err := NewComponent(ctx, engine, buildHandlerProviderGuest(), WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	var logged safeBuffer
	caller, err := NewComponent(ctx, engine, buildInvokingGuest(false), WithKeyRegistry(keys), WithLogWriter(&logged))
	if err != nil {
		t.Fatalf("caller: %v", err)
	}

	s := runtime.New()
	defer s.Close()
	if _, err := s.Insert(provider, nil); err != nil {
		t.Fatalf("Insert(provider): %v", err)
	}
	did, err := s.Insert(caller, nil)
	if err != nil {
		t.Fatalf("Insert(caller): %v", err)
	}
	waitForState(t, s, did, runtime.StateActive)

	if got := logged.String(); got != "" {
		t.Fatalf("non-dependent log = %q, want refusal", got)
	}
}
