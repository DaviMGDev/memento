package wasm

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/DaviMGDev/memento/runtime"
)

// stubServices is a HostServices test double: it records what the imports
// handed it and answers from fixed state.
type stubServices struct {
	mu          sync.Mutex
	startReq    []byte
	startDoc    []byte
	startErr    error
	peepReq     []byte
	peepDoc     []byte
	killReq     []byte
	killDoc     []byte
	published   []pubEvent
	publishErr  error
	cancel      bool
	caller      *runtime.Instance
	cancelCalls int
}

type pubEvent struct {
	topic   string
	payload []byte
}

func (s *stubServices) StartJob(caller *runtime.Instance, req []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
	s.startReq = append([]byte(nil), req...)
	return s.startDoc, s.startErr
}

func (s *stubServices) PeepJob(caller *runtime.Instance, req []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
	s.peepReq = append([]byte(nil), req...)
	return s.peepDoc, nil
}

func (s *stubServices) KillJob(caller *runtime.Instance, req []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
	s.killReq = append([]byte(nil), req...)
	return s.killDoc, nil
}

func (s *stubServices) Publish(topic string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publishErr != nil {
		return s.publishErr
	}
	s.published = append(s.published, pubEvent{topic: topic, payload: append([]byte(nil), payload...)})
	return nil
}

func (s *stubServices) Cancelled(caller *runtime.Instance) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
	s.cancelCalls++
	return s.cancel
}

func (s *stubServices) state() (caller *runtime.Instance, cancelCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caller, s.cancelCalls
}

func (s *stubServices) events() []pubEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pubEvent(nil), s.published...)
}

// activateGuest loads a guest, drives it to active, and returns what it
// logged. The guest's activation performs the ABI calls under test.
func activateGuest(t *testing.T, guest []byte, opts ...EngineOption) string {
	t.Helper()
	ctx := context.Background()
	engine, err := NewEngine(ctx, opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close(ctx) })

	var logged safeBuffer
	comp, err := NewComponent(ctx, engine, guest, WithLogWriter(&logged))
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}

	s := runtime.New()
	t.Cleanup(func() { _ = s.Close() })
	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForState(t, s, id, runtime.StateActive)
	return logged.String()
}

// buildJobGuest builds a guest whose memento_activate starts a job with the
// request document at offset 32 and reads the stashed result document back
// into offset 4096, logging it; on failure it logs "FAIL".
func buildJobGuest(request string, respMax uint32) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}             // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}       // (i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2}))

	// Imports 0..3: job_start, job_result_len, job_result, log.
	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("job_start", 0x00),
		httpImport("job_result_len", 0x01),
		httpImport("job_result", 0x00),
		httpImport("log", 0x00),
	}))

	// Defined funcs 4..5: activate (type1), revert (type2).
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x04),
		append(encodeString("memento_revert_effect"), 0x00, 0x05),
	}))

	body := []byte{0x01, 0x02, 0x7f} // two i32 locals: n, got
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x00) // job_start(32, len)
	body = append(body, 0x45)       // i32.eqz
	body = append(body, 0x04, 0x40) // if
	body = append(body, 0x10, 0x01) // job_result_len()
	body = append(body, 0x21, 0x00) // local.set 0 (n)
	body = append(body, i32Const(4096)...)
	body = append(body, 0x20, 0x00)
	body = append(body, i32Const(int32(respMax))...)
	body = append(body, 0x20, 0x00)
	body = append(body, i32Const(int32(respMax))...)
	body = append(body, 0x49)       // i32.lt_u
	body = append(body, 0x1b)       // select -> min(n, respMax)
	body = append(body, 0x10, 0x02) // job_result(4096, min)
	body = append(body, 0x21, 0x01) // local.set 1 (got)
	body = append(body, i32Const(4096)...)
	body = append(body, 0x20, 0x01)
	body = append(body, 0x10, 0x03) // log(4096, got)
	body = append(body, 0x1a)       // drop
	body = append(body, 0x05)       // else
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(4)...)
	body = append(body, 0x10, 0x03) // log(16, 4) -> "FAIL"
	body = append(body, 0x1a)       // drop
	body = append(body, 0x0b)       // end if
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	dataSec := encodeSection(11, encodeVec([][]byte{
		activeData(16, "FAIL"),
		activeData(32, request),
	}))

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

// buildPollGuest builds a guest whose activation calls cancel_poll and logs
// the returned code as one ASCII digit ("0" or "2").
func buildPollGuest() []byte {
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}             // ()->i32
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32)->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}       // (i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type1, type0, type2}))

	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("cancel_poll", 0x00),
		httpImport("log", 0x01),
	}))

	funcSec := encodeSection(3, encodeVec([][]byte{{0x00}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x02),
		append(encodeString("memento_revert_effect"), 0x00, 0x03),
	}))

	body := []byte{0x00}
	body = append(body, i32Const(16)...)
	body = append(body, 0x10, 0x00) // cancel_poll()
	body = append(body, i32Const(48)...)
	body = append(body, 0x6a)             // i32.add -> code + '0'
	body = append(body, 0x3a, 0x00, 0x00) // i32.store8 at 16
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(1)...)
	body = append(body, 0x10, 0x01) // log(16, 1)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	var wasm bytes.Buffer
	wasm.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	wasm.Write(typeSec)
	wasm.Write(importSec)
	wasm.Write(funcSec)
	wasm.Write(memSec)
	wasm.Write(exportSec)
	wasm.Write(codeSec)
	return wasm.Bytes()
}

// buildCancelThenStartGuest builds a guest whose activation polls for
// cancellation first and then attempts a job_start, logging the job_start
// code as one ASCII digit.
func buildCancelThenStartGuest(request string) []byte {
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}             // ()->i32
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32)->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}       // (i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type1, type0, type2}))

	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("cancel_poll", 0x00),
		httpImport("job_start", 0x01),
		httpImport("log", 0x01),
	}))

	funcSec := encodeSection(3, encodeVec([][]byte{{0x00}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x03),
		append(encodeString("memento_revert_effect"), 0x00, 0x04),
	}))

	body := []byte{0x00}
	body = append(body, 0x10, 0x00) // cancel_poll()
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x01) // job_start(32, len)
	body = append(body, i32Const(48)...)
	body = append(body, 0x6a)             // i32.add
	body = append(body, 0x3a, 0x00, 0x00) // i32.store8 at 16
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(1)...)
	body = append(body, 0x10, 0x02) // log(16, 1)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	dataSec := encodeSection(11, encodeVec([][]byte{activeData(32, request)}))

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

// buildPublishGuest builds a guest whose activation publishes one event and
// logs "OK" or "FAIL".
func buildPublishGuest(topic, payload string) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}             // (i32,i32)->i32
	type3 := []byte{0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32,i32,i32)->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                   // (i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                         // ()->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type3, type2, type1}))

	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("publish", 0x01),
		httpImport("log", 0x00),
	}))

	funcSec := encodeSection(3, encodeVec([][]byte{{0x03}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x02),
		append(encodeString("memento_revert_effect"), 0x00, 0x03),
	}))

	body := []byte{0x00}
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(topic)))...)
	body = append(body, i32Const(64)...)
	body = append(body, i32Const(int32(len(payload)))...)
	body = append(body, 0x10, 0x00) // publish(32, topicLen, 64, payloadLen)
	body = append(body, 0x45)       // i32.eqz
	body = append(body, 0x04, 0x40) // if
	body = append(body, i32Const(96)...)
	body = append(body, i32Const(2)...)
	body = append(body, 0x10, 0x01) // log(96, 2) -> "OK"
	body = append(body, 0x1a)       // drop
	body = append(body, 0x05)       // else
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(4)...)
	body = append(body, 0x10, 0x01) // log(16, 4) -> "FAIL"
	body = append(body, 0x1a)       // drop
	body = append(body, 0x0b)       // end if
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	dataSec := encodeSection(11, encodeVec([][]byte{
		activeData(16, "FAIL"),
		activeData(32, topic),
		activeData(64, payload),
		activeData(96, "OK"),
	}))

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

func TestWithHostServicesInjectsContract(t *testing.T) {
	ctx := context.Background()
	services := &stubServices{}

	engine, err := NewEngine(ctx, WithHostServices(services))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)
	if engine.services != services {
		t.Fatal("engine.services was not the injected contract")
	}

	plain, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer plain.Close(ctx)
	if plain.services != nil {
		t.Fatal("engine.services should be nil without WithHostServices")
	}
}

// buildAllServiceImportsGuest builds a guest whose activation calls every
// host-services import once, proving the whole surface resolves and
// dispatches. The request document lives at offset 32, the publish topic at
// 64, and the publish payload at 96.
func buildAllServiceImportsGuest(request, topic, payload string) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}             // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}                         // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}                   // (i32)->i32
	type3 := []byte{0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32,i32,i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2, type3}))

	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("job_start", 0x00),
		httpImport("job_peep", 0x00),
		httpImport("job_kill", 0x00),
		httpImport("job_result_len", 0x01),
		httpImport("job_result", 0x00),
		httpImport("publish", 0x03),
		httpImport("cancel_poll", 0x01),
	}))

	// Defined funcs 7..8: activate (type1), revert (type2).
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x07),
		append(encodeString("memento_revert_effect"), 0x00, 0x08),
	}))

	body := []byte{0x00}
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x00) // job_start(32, len)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x01) // job_peep(32, len)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x02) // job_kill(32, len)
	body = append(body, 0x1a)       // drop
	body = append(body, 0x10, 0x03) // job_result_len()
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(4096)...)
	body = append(body, i32Const(1024)...)
	body = append(body, 0x10, 0x04) // job_result(4096, 1024)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(64)...)
	body = append(body, i32Const(int32(len(topic)))...)
	body = append(body, i32Const(96)...)
	body = append(body, i32Const(int32(len(payload)))...)
	body = append(body, 0x10, 0x05) // publish(64, topicLen, 96, payloadLen)
	body = append(body, 0x1a)       // drop
	body = append(body, 0x10, 0x06) // cancel_poll()
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	dataSec := encodeSection(11, encodeVec([][]byte{
		activeData(32, request),
		activeData(64, topic),
		activeData(96, payload),
	}))

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

func TestServiceImportsResolveAndDispatch(t *testing.T) {
	request := `{"job":"j-1"}`
	services := &stubServices{
		startDoc: []byte(`{"job":"j-1"}`),
		peepDoc:  []byte(`{"state":"running"}`),
		killDoc:  []byte(`{"killed":true}`),
	}
	activateGuest(t, buildAllServiceImportsGuest(request, "chat.message", `{"text":"hi"}`), WithHostServices(services))

	services.mu.Lock()
	defer services.mu.Unlock()
	if string(services.startReq) != request || string(services.peepReq) != request || string(services.killReq) != request {
		t.Fatalf("job requests = start %q, peep %q, kill %q; want %q each",
			services.startReq, services.peepReq, services.killReq, request)
	}
	if len(services.published) != 1 || services.published[0].topic != "chat.message" || string(services.published[0].payload) != `{"text":"hi"}` {
		t.Fatalf("published events = %+v", services.published)
	}
	if services.cancelCalls == 0 {
		t.Fatal("cancel_poll did not consult the service")
	}
}

func TestJobStartStashesResultAndReadsItBack(t *testing.T) {
	request := `{"tool":"read","args":{"path":"/tmp/x"}}`
	result := `{"job":"j-1"}`
	services := &stubServices{startDoc: []byte(result)}

	logged := activateGuest(t, buildJobGuest(request, 1024), WithHostServices(services))

	if logged != result {
		t.Fatalf("guest log = %q, want %q", logged, result)
	}
	if string(services.startReq) != request {
		t.Fatalf("service request = %q, want %q", services.startReq, request)
	}
	caller, _ := services.state()
	if caller == nil {
		t.Fatal("service did not receive the calling instance")
	}
}

func TestJobStartDeclinesWithoutServices(t *testing.T) {
	logged := activateGuest(t, buildJobGuest(`{"tool":"read"}`, 1024))
	if logged != "FAIL" {
		t.Fatalf("guest log = %q, want FAIL", logged)
	}
}

func TestCancelPollAnswersForCallerOnly(t *testing.T) {
	t.Run("not canceled", func(t *testing.T) {
		services := &stubServices{}
		logged := activateGuest(t, buildPollGuest(), WithHostServices(services))
		if logged != "0" {
			t.Fatalf("guest log = %q, want 0", logged)
		}
		if caller, calls := services.state(); caller == nil || calls == 0 {
			t.Fatal("cancel_poll did not consult the service with the caller")
		}
	})

	t.Run("canceled", func(t *testing.T) {
		services := &stubServices{cancel: true}
		logged := activateGuest(t, buildPollGuest(), WithHostServices(services))
		if logged != "2" {
			t.Fatalf("guest log = %q, want 2", logged)
		}
	})
}

func TestCanceledCallFailsActionImports(t *testing.T) {
	services := &stubServices{cancel: true}
	logged := activateGuest(t, buildCancelThenStartGuest(`{"tool":"read"}`), WithHostServices(services))
	if logged != "2" {
		t.Fatalf("job_start code = %q, want 2 (canceled)", logged)
	}
	if string(services.startReq) != "" {
		t.Fatal("job_start should not reach the service once the caller is canceled")
	}
}

// buildCancelThenHTTPGuest builds a guest whose activation polls for
// cancellation first and then attempts an http_request, logging the request
// code as one ASCII digit.
func buildCancelThenHTTPGuest(request string) []byte {
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}             // ()->i32
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32)->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}       // (i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type1, type0, type2}))

	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("cancel_poll", 0x00),
		httpImport("http_request", 0x01),
		httpImport("log", 0x01),
	}))

	funcSec := encodeSection(3, encodeVec([][]byte{{0x00}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_activate"), 0x00, 0x03),
		append(encodeString("memento_revert_effect"), 0x00, 0x04),
	}))

	body := []byte{0x00}
	body = append(body, 0x10, 0x00) // cancel_poll()
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x01) // http_request(32, len)
	body = append(body, i32Const(48)...)
	body = append(body, 0x6a)             // i32.add
	body = append(body, 0x3a, 0x00, 0x00) // i32.store8 at 16
	body = append(body, i32Const(16)...)
	body = append(body, i32Const(1)...)
	body = append(body, 0x10, 0x02) // log(16, 1)
	body = append(body, 0x1a)       // drop
	body = append(body, i32Const(0)...)
	body = append(body, 0x0f, 0x0b) // return

	revert := []byte{0x00}
	revert = append(revert, i32Const(0)...)
	revert = append(revert, 0x0f, 0x0b)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(body))), body...),
		append(encodeLEB128U(uint32(len(revert))), revert...),
	}))

	dataSec := encodeSection(11, encodeVec([][]byte{activeData(32, request)}))

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

func TestCanceledCallFailsHTTPImport(t *testing.T) {
	services := &stubServices{cancel: true}
	ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
	logged := activateGuest(t, buildCancelThenHTTPGuest(`{"url":"https://example.test/x"}`),
		WithHostServices(services), WithHTTPTransport(ft))
	if logged != "2" {
		t.Fatalf("http_request code = %q, want 2 (canceled)", logged)
	}
	if calls := ft.calls(); len(calls) != 0 {
		t.Fatalf("transport calls = %d, want none for a canceled caller", len(calls))
	}
}

func TestPublishReachesHostServices(t *testing.T) {
	t.Run("delivered", func(t *testing.T) {
		services := &stubServices{}
		logged := activateGuest(t, buildPublishGuest("chat.message", `{"text":"hi"}`), WithHostServices(services))
		if logged != "OK" {
			t.Fatalf("guest log = %q, want OK", logged)
		}
		events := services.events()
		if len(events) != 1 || events[0].topic != "chat.message" || string(events[0].payload) != `{"text":"hi"}` {
			t.Fatalf("published events = %+v", events)
		}
	})

	t.Run("service failure fails the call", func(t *testing.T) {
		services := &stubServices{publishErr: errors.New("bus down")}
		logged := activateGuest(t, buildPublishGuest("chat.message", "hi"), WithHostServices(services))
		if logged != "FAIL" {
			t.Fatalf("guest log = %q, want FAIL", logged)
		}
	})
}
