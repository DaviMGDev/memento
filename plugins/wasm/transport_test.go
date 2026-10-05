package wasm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/runtime"
)

// recordingTransport answers every exchange with a canned response and keeps
// the request documents it was handed.
type recordingTransport struct {
	mu   sync.Mutex
	reqs []HTTPRequest
	resp HTTPResponse
	err  error
	hold bool
}

func (t *recordingTransport) Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	t.mu.Lock()
	t.reqs = append(t.reqs, req)
	resp, err, hold := t.resp, t.err, t.hold
	t.mu.Unlock()
	if hold {
		<-ctx.Done()
		return HTTPResponse{}, ctx.Err()
	}
	if err != nil {
		return HTTPResponse{}, err
	}
	return resp, nil
}

func (t *recordingTransport) calls() []HTTPRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]HTTPRequest(nil), t.reqs...)
}

// i32Const encodes an i32.const instruction.
func i32Const(v int32) []byte {
	return append([]byte{0x41}, encodeLEB128S(v)...)
}

// activeData encodes an active data segment at a fixed offset.
func activeData(offset int32, s string) []byte {
	b := append([]byte{0x00}, i32Const(offset)...)
	b = append(b, 0x0b)
	b = append(b, encodeLEB128U(uint32(len(s)))...)
	return append(b, []byte(s)...)
}

// httpImport encodes a memento host import of the given type index.
func httpImport(name string, typeIndex byte) []byte {
	return append(encodeString("memento"), append(encodeString(name), 0x00, typeIndex)...)
}

// buildHTTPGuest builds a guest whose memento_activate performs one HTTP
// exchange. The request JSON lives at offset 32; on success the guest reads
// the response document into a buffer at offset 4096 and logs at most respMax
// bytes of it; on failure it logs "FAIL". Memory: the response buffer.
func buildHTTPGuest(request string, respMax uint32) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f} // (i32,i32)->i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}             // ()->i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}       // (i32)->i32
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2}))

	// Imports 0..3: http_request, http_response_len, http_response, log.
	importSec := encodeSection(2, encodeVec([][]byte{
		httpImport("http_request", 0x00),
		httpImport("http_response_len", 0x01),
		httpImport("http_response", 0x00),
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

	body := []byte{0x01, 0x02, 0x7f} // two i32 locals
	body = append(body, i32Const(32)...)
	body = append(body, i32Const(int32(len(request)))...)
	body = append(body, 0x10, 0x00) // http_request(32, len)
	body = append(body, 0x45)       // i32.eqz
	body = append(body, 0x04, 0x40) // if
	body = append(body, 0x10, 0x01) // http_response_len()
	body = append(body, 0x21, 0x00) // local.set 0 (n)
	body = append(body, i32Const(4096)...)
	body = append(body, 0x20, 0x00)
	body = append(body, i32Const(int32(respMax))...)
	body = append(body, 0x20, 0x00)
	body = append(body, i32Const(int32(respMax))...)
	body = append(body, 0x49)       // i32.lt_u
	body = append(body, 0x1b)       // select -> min(n, respMax)
	body = append(body, 0x10, 0x02) // http_response(4096, min)
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

// buildHTTPDeclaringGuest builds a guest whose memento_declare performs an
// HTTP exchange, which must fail: declaration runs on a probe with no egress.
func buildHTTPDeclaringGuest(request string) []byte {
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}
	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2}))
	importSec := encodeSection(2, encodeVec([][]byte{httpImport("http_request", 0x00)}))

	// Defined funcs 1..3: declare (type1), activate (type1), revert (type2).
	funcSec := encodeSection(3, encodeVec([][]byte{{0x01}, {0x01}, {0x02}}))
	memSec := encodeSection(5, encodeVec([][]byte{{0x00, 0x01}}))
	exportSec := encodeSection(7, encodeVec([][]byte{
		append(encodeString("memory"), 0x02, 0x00),
		append(encodeString("memento_declare"), 0x00, 0x01),
		append(encodeString("memento_activate"), 0x00, 0x02),
		append(encodeString("memento_revert_effect"), 0x00, 0x03),
	}))

	declare := []byte{0x00}
	declare = append(declare, i32Const(32)...)
	declare = append(declare, i32Const(int32(len(request)))...)
	declare = append(declare, 0x10, 0x00) // http_request
	declare = append(declare, 0x0f, 0x0b) // return

	activate := []byte{0x00}
	activate = append(activate, i32Const(0)...)
	activate = append(activate, 0x0f, 0x0b)
	revert := append([]byte{}, activate...)

	codeSec := encodeSection(10, encodeVec([][]byte{
		append(encodeLEB128U(uint32(len(declare))), declare...),
		append(encodeLEB128U(uint32(len(activate))), activate...),
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

// activateHTTPGuest loads a guest, drives it to active, and returns what it
// logged. Activation is where the guest performs its exchange.
func activateHTTPGuest(t *testing.T, guest []byte, opts ...EngineOption) string {
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

func wantFailLog(t *testing.T, logged string) {
	t.Helper()
	if !strings.HasSuffix(logged, "FAIL") {
		t.Fatalf("guest log = %q, want it to end with FAIL", logged)
	}
}

func TestHTTPRequestPerformsExchange(t *testing.T) {
	req := `{"method":"POST","url":"https://api.example.test/v1/chat","headers":{"x-test":"1"},"body":"hi"}`
	ft := &recordingTransport{resp: HTTPResponse{
		Status:  200,
		Headers: map[string]string{"content-type": "application/json"},
		Body:    []byte(`{"ok":true}`),
	}}

	logged := activateHTTPGuest(t, buildHTTPGuest(req, 1024), WithHTTPTransport(ft))

	calls := ft.calls()
	if len(calls) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(calls))
	}
	got := calls[0]
	if got.Method != "POST" || got.URL != "https://api.example.test/v1/chat" || string(got.Body) != "hi" || got.Headers["x-test"] != "1" {
		t.Fatalf("transport request = %+v", got)
	}

	want, err := json.Marshal(httpResponseDoc{
		Status:  200,
		Headers: map[string]string{"content-type": "application/json"},
		Body:    `{"ok":true}`,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if logged != string(want) {
		t.Fatalf("guest log = %q, want %q", logged, want)
	}
}

func TestHTTPAllowHostsConstrainEgress(t *testing.T) {
	t.Run("admits a listed host", func(t *testing.T) {
		ft := &recordingTransport{resp: HTTPResponse{Status: 204}}
		logged := activateHTTPGuest(t, buildHTTPGuest(`{"url":"https://allowed.test/ping"}`, 1024),
			WithHTTPAllowHosts("allowed.test"), WithHTTPTransport(ft))
		if len(ft.calls()) != 1 {
			t.Fatalf("transport calls = %d, want 1", len(ft.calls()))
		}
		var doc httpResponseDoc
		if err := json.Unmarshal([]byte(logged), &doc); err != nil {
			t.Fatalf("guest log = %q, want a response document", logged)
		}
		if doc.Status != 204 {
			t.Fatalf("status = %d, want 204", doc.Status)
		}
	})

	t.Run("refuses an unlisted host", func(t *testing.T) {
		ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
		logged := activateHTTPGuest(t, buildHTTPGuest(`{"url":"https://blocked.test/ping"}`, 1024),
			WithHTTPAllowHosts("allowed.test"), WithHTTPTransport(ft))
		if len(ft.calls()) != 0 {
			t.Fatalf("transport calls = %d, want none for a denied host", len(ft.calls()))
		}
		wantFailLog(t, logged)
	})
}

func TestHTTPDefaultTransportTalksOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "pong")
	}))
	defer srv.Close()

	req, err := json.Marshal(httpRequestDoc{URL: srv.URL + "/x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	logged := activateHTTPGuest(t, buildHTTPGuest(string(req), 4096))

	var doc httpResponseDoc
	if err := json.Unmarshal([]byte(logged), &doc); err != nil {
		t.Fatalf("guest log = %q, want a response document", logged)
	}
	if doc.Status != http.StatusTeapot || doc.Body != "pong" {
		t.Fatalf("response = %+v, want status 418 and body pong", doc)
	}
}

func TestHTTPTimeoutFailsExchange(t *testing.T) {
	ft := &recordingTransport{hold: true}
	start := time.Now()
	logged := activateHTTPGuest(t, buildHTTPGuest(`{"url":"https://slow.test/"}`, 1024),
		WithHTTPTransport(ft), WithHTTPTimeout(50*time.Millisecond))

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("exchange took %v, want it bounded by the timeout", elapsed)
	}
	if len(ft.calls()) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(ft.calls()))
	}
	wantFailLog(t, logged)
}

func TestHTTPResponseTruncatesToBuffer(t *testing.T) {
	ft := &recordingTransport{resp: HTTPResponse{Status: 200, Body: []byte("0123456789")}}
	logged := activateHTTPGuest(t, buildHTTPGuest(`{"url":"https://t.test/"}`, 8), WithHTTPTransport(ft))

	want, err := json.Marshal(httpResponseDoc{Status: 200, Body: "0123456789"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if logged != string(want[:8]) {
		t.Fatalf("guest log = %q, want the first 8 bytes of %q", logged, want)
	}
}

func TestHTTPMalformedRequestFails(t *testing.T) {
	ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
	logged := activateHTTPGuest(t, buildHTTPGuest(`not json`, 64), WithHTTPTransport(ft))
	if len(ft.calls()) != 0 {
		t.Fatalf("transport calls = %d, want none for a malformed request", len(ft.calls()))
	}
	wantFailLog(t, logged)
}

func TestHTTPCredentialsAreResolvedHostSide(t *testing.T) {
	ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
	req := `{"url":"https://api.test/v1","headers":{"authorization":"Bearer env:TEST_KEY","x-api-key":"env:OTHER"}}`
	resolve := func(name string) (string, bool) {
		switch name {
		case "TEST_KEY":
			return "s3cret", true
		case "OTHER":
			return "other-secret", true
		}
		return "", false
	}

	logged := activateHTTPGuest(t, buildHTTPGuest(req, 1024),
		WithHTTPTransport(ft), WithHTTPCredentialResolver(resolve))

	calls := ft.calls()
	if len(calls) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(calls))
	}
	if got := calls[0].Headers["authorization"]; got != "Bearer s3cret" {
		t.Fatalf("authorization = %q, want a resolved bearer token", got)
	}
	if got := calls[0].Headers["x-api-key"]; got != "other-secret" {
		t.Fatalf("x-api-key = %q, want the resolved secret", got)
	}
	if !strings.HasPrefix(logged, `{"status":200`) {
		t.Fatalf("guest log = %q, want a completed exchange", logged)
	}
}

func TestHTTPMissingCredentialFailsExchange(t *testing.T) {
	ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
	req := `{"url":"https://api.test/v1","headers":{"authorization":"Bearer env:MISSING"}}`
	logged := activateHTTPGuest(t, buildHTTPGuest(req, 1024),
		WithHTTPTransport(ft), WithHTTPCredentialResolver(func(string) (string, bool) { return "", false }))

	if len(ft.calls()) != 0 {
		t.Fatalf("transport calls = %d, want none when a credential is unavailable", len(ft.calls()))
	}
	wantFailLog(t, logged)
}

func TestHTTPRequestDuringDeclarationFails(t *testing.T) {
	ctx := context.Background()
	ft := &recordingTransport{resp: HTTPResponse{Status: 200}}
	engine, err := NewEngine(ctx, WithHTTPTransport(ft))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	_, err = NewComponent(ctx, engine, buildHTTPDeclaringGuest(`{"url":"https://probe.test/"}`))
	if err == nil {
		t.Fatal("expected a declaration that performs an HTTP request to fail")
	}
	if !strings.Contains(err.Error(), "declaration") {
		t.Fatalf("error = %v, want it to name the declaration failure", err)
	}
	if len(ft.calls()) != 0 {
		t.Fatalf("transport calls = %d, want none during declaration", len(ft.calls()))
	}
}
