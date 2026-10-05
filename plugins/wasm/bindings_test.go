package wasm

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/runtime"
)

// safeBuffer is a concurrency-safe log sink.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// buildProviderGuest builds a guest that declares provide "cache" and
// optionally binds it to the given value during activation.
func buildProviderGuest(bindValue string) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}             // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                         // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                   // (i32)->i32
	type3 := []byte{0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32,i32,i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2, type3}))

	importProvide := append(encodeString("memento"), append(encodeString("declare_provide"), 0x00, 0x00)...)
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x02)...)
	importBind := append(encodeString("memento"), append(encodeString("bind"), 0x00, 0x03)...)
	importSec := encodeSection(2, encodeVec([][]byte{importProvide, importEffect, importBind}))

	// Funcs 3, 4, 5: declare (type1), activate (type1), revert (type2).
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x01}, {0x02}}))

	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))

	expMem := append(encodeString("memory"), 0x02, 0x00)
	expDeclare := append(encodeString("memento_declare"), 0x00, 0x03)
	expActivate := append(encodeString("memento_activate"), 0x00, 0x04)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x05)
	exportSec := encodeSection(7, encodeVec([][]byte{expMem, expDeclare, expActivate, expRevert}))

	// declare: declare_provide("cache" at 16)
	body3 := []byte{
		0x00,
		0x41, 0x10, // i32.const 16
		0x41, 0x05, // i32.const 5
		0x10, 0x00, // call declare_provide
		0x1a, // drop
		0x41, 0x00, 0x0f, 0x0b,
	}
	// activate: register_effect(7); optionally bind("cache", value)
	body4 := []byte{
		0x00,
		0x41, 0x07, // i32.const 7
		0x10, 0x01, // call register_effect
		0x1a, // drop
	}
	if bindValue != "" {
		body4 = append(body4,
			0x41, 0x10, // key ptr 16
			0x41, 0x05, // key len 5
			0x41, 0x20, // value ptr 32
			0x41, byte(len(bindValue)), // value len
			0x10, 0x02, // call bind
			0x1a, // drop
		)
	}
	body4 = append(body4, 0x41, 0x00, 0x0f, 0x0b)

	body5 := []byte{0x00, 0x41, 0x00, 0x0f, 0x0b}
	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body3))), body3...),
		append(encodeLEB128U(uint32(len(body4))), body4...),
		append(encodeLEB128U(uint32(len(body5))), body5...),
	}))

	data1 := append([]byte{0x00, 0x41, 0x10, 0x0b}, append(encodeLEB128U(5), []byte("cache")...)...)
	dataItems := [][]byte{data1}
	if bindValue != "" {
		data2 := append([]byte{0x00, 0x41, 0x20, 0x0b}, append(encodeLEB128U(uint32(len(bindValue))), []byte(bindValue)...)...)
		dataItems = append(dataItems, data2)
	}
	dataSec := encodeSection(11, encodeVec(dataItems))

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

// buildDependentGuest builds a guest that declares inject "cache", reads its
// value during activation, and logs it.
func buildDependentGuest() []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}             // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                         // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                   // (i32)->i32
	type3 := []byte{0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32,i32,i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2, type3}))

	importInject := append(encodeString("memento"), append(encodeString("declare_inject"), 0x00, 0x00)...)
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x02)...)
	importGetLen := append(encodeString("memento"), append(encodeString("get_len"), 0x00, 0x00)...)
	importGet := append(encodeString("memento"), append(encodeString("get"), 0x00, 0x03)...)
	importLog := append(encodeString("memento"), append(encodeString("log"), 0x00, 0x00)...)
	importSec := encodeSection(2, encodeVec([][]byte{importInject, importEffect, importGetLen, importGet, importLog}))

	// Funcs 5, 6, 7: declare (type1), activate (type1), revert (type2).
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x01}, {0x02}}))

	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))

	expMem := append(encodeString("memory"), 0x02, 0x00)
	expDeclare := append(encodeString("memento_declare"), 0x00, 0x05)
	expActivate := append(encodeString("memento_activate"), 0x00, 0x06)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x07)
	exportSec := encodeSection(7, encodeVec([][]byte{expMem, expDeclare, expActivate, expRevert}))

	body5 := []byte{
		0x00,
		0x41, 0x10, // i32.const 16
		0x41, 0x05, // i32.const 5
		0x10, 0x00, // call declare_inject
		0x1a,
		0x41, 0x00, 0x0f, 0x0b,
	}
	// activate: register_effect(9); n = get_len("cache"); get -> 48; log(48, n)
	body6 := []byte{
		0x01, 0x01, 0x7f, // 1 local i32
		0x41, 0x09, // i32.const 9
		0x10, 0x01, // call register_effect
		0x1a,
		0x41, 0x10, // i32.const 16
		0x41, 0x05, // i32.const 5
		0x10, 0x02, // call get_len
		0x21, 0x00, // local.set 0
		0x41, 0x10, 0x41, 0x05, // key
		0x41, 0x30, // buf ptr 48
		0x20, 0x00, // local.get 0
		0x10, 0x03, // call get
		0x1a,
		0x41, 0x30, // buf ptr 48
		0x20, 0x00, // len
		0x10, 0x04, // call log
		0x1a,
		0x41, 0x00, 0x0f, 0x0b,
	}
	body7 := []byte{0x00, 0x41, 0x00, 0x0f, 0x0b}
	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body5))), body5...),
		append(encodeLEB128U(uint32(len(body6))), body6...),
		append(encodeLEB128U(uint32(len(body7))), body7...),
	}))

	data1 := append([]byte{0x00, 0x41, 0x10, 0x0b}, append(encodeLEB128U(5), []byte("cache")...)...)
	dataSec := encodeSection(11, encodeVec([][]byte{data1}))

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

func TestDeclaredProvideMustBeBound(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	comp, err := NewComponent(ctx, engine, buildProviderGuest(""), WithKeyRegistry(NewKeyRegistry()))
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}

	s := runtime.New()
	defer s.Close()

	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		info, ok := s.Inspect(id)
		if !ok {
			t.Fatal("fiber disappeared")
		}
		if info.State == runtime.StateFailed {
			if info.Err == nil || !strings.Contains(info.Err.Error(), "did not bind") {
				t.Fatalf("failure = %v, want a did-not-bind error", info.Err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fiber did not fail; last = %+v", info)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestBindAdvertisesAndDependentsReadValues(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	keys := NewKeyRegistry()

	provider, err := NewComponent(ctx, engine, buildProviderGuest("v1"), WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("provider NewComponent: %v", err)
	}

	var logged safeBuffer
	dependent, err := NewComponent(ctx, engine, buildDependentGuest(), WithKeyRegistry(keys), WithLogWriter(&logged))
	if err != nil {
		t.Fatalf("dependent NewComponent: %v", err)
	}

	s := runtime.New()
	defer s.Close()

	pid, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider): %v", err)
	}
	did, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent): %v", err)
	}

	waitForState(t, s, pid, runtime.StateActive)
	waitForState(t, s, did, runtime.StateActive)

	if got := logged.String(); !strings.Contains(got, "v1") {
		t.Fatalf("dependent log = %q, want the bound value v1", got)
	}

	// Withdrawing the provider deactivates the dependent first; a fresh
	// dependent cannot activate after the registration is gone.
	if err := s.Retire(pid); err != nil {
		t.Fatalf("Retire(provider): %v", err)
	}
	waitForState(t, s, did, runtime.StateInactive)

	dependent2, err := NewComponent(ctx, engine, buildDependentGuest(), WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("second dependent: %v", err)
	}
	did2, err := s.Insert(dependent2, nil)
	if err != nil {
		t.Fatalf("Insert(second dependent): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	info, ok := s.Inspect(did2)
	if !ok {
		t.Fatal("second dependent disappeared")
	}
	if info.State != runtime.StateInactive {
		t.Fatalf("second dependent state = %q, want inactive after withdrawal", info.State)
	}
}
