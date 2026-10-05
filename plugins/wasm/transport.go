package wasm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tetratelabs/wazero/api"
)

// HTTPRequest is one exchange the host performs on a guest's behalf.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// HTTPResponse is the outcome of a completed exchange. Status is the HTTP
// status code: an exchange completed even when the status is an error, and
// only policy or transport failures are reported as errors.
type HTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// HTTPTransport performs one HTTP exchange. Hosts inject their own to
// constrain, record, or stub egress; the default talks to the network with
// net/http.
type HTTPTransport interface {
	Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
}

// defaultHTTPTimeout bounds one exchange when the policy sets no timeout.
const defaultHTTPTimeout = 30 * time.Second

// Egress is the host's HTTP policy for guests. The zero value is permissive:
// any absolute http(s) URL is reachable with the default timeout and the
// default net/http transport. A host that wants to constrain a guest sets
// AllowHosts.
type Egress struct {
	// Transport performs the exchange; nil means the default net/http transport.
	Transport HTTPTransport
	// AllowHosts, when non-empty, is the set of hostnames a guest may reach.
	// Comparison is case-insensitive and ignores the port. An empty list
	// permits every host.
	AllowHosts []string
	// Timeout bounds one exchange; zero or negative means defaultHTTPTimeout.
	Timeout time.Duration
	// Credentials resolves `env:NAME` references embedded in request header
	// values to the host's secret. Nil disables substitution: reference values
	// cross the wire literally. Hosts back it with an environment lookup or a
	// secret store, so a guest holds a reference and never a secret.
	Credentials func(name string) (string, bool)
}

// credentialRef matches an `env:NAME` reference inside a header value.
var credentialRef = regexp.MustCompile(`env:[A-Za-z_][A-Za-z0-9_]*`)

// substituteCredentials replaces `env:NAME` references in header values with
// the host's secret. It reports an error naming the first reference the
// resolver cannot satisfy, so a missing secret fails the exchange instead of
// crossing the wire as a literal.
func substituteCredentials(headers map[string]string, resolve func(string) (string, bool)) (map[string]string, error) {
	if len(headers) == 0 || resolve == nil {
		return headers, nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		for _, ref := range credentialRef.FindAllString(v, -1) {
			if _, ok := resolve(strings.TrimPrefix(ref, "env:")); !ok {
				return nil, fmt.Errorf("credential %q is not available", ref)
			}
		}
		out[k] = credentialRef.ReplaceAllStringFunc(v, func(ref string) string {
			secret, _ := resolve(strings.TrimPrefix(ref, "env:"))
			return secret
		})
	}
	return out, nil
}

// allowed reports whether the policy admits u.
func (e *Egress) allowed(u *url.URL) bool {
	if e == nil || len(e.AllowHosts) == 0 {
		return true
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range e.AllowHosts {
		if strings.ToLower(strings.TrimSpace(h)) == host {
			return true
		}
	}
	return false
}

// resolve returns the transport and timeout to use, applying defaults.
func (e *Egress) resolve() (HTTPTransport, time.Duration) {
	var (
		transport HTTPTransport
		timeout   time.Duration
	)
	if e != nil {
		transport = e.Transport
		timeout = e.Timeout
	}
	if transport == nil {
		transport = defaultTransport{}
	}
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return transport, timeout
}

// defaultTransport performs exchanges with net/http.
type defaultTransport struct{}

func (defaultTransport) Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return HTTPResponse{}, err
	}
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPResponse{}, err
	}
	headers := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	return HTTPResponse{Status: resp.StatusCode, Headers: headers, Body: rb}, nil
}

// httpRequestDoc is the JSON document a guest writes to request an exchange.
type httpRequestDoc struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// httpResponseDoc is the JSON document the host stashes for the guest to read
// back after a completed exchange.
type httpResponseDoc struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// hostHTTPRequest performs one HTTP exchange for the calling guest. The guest
// supplies a JSON request document; on success the host stashes a JSON
// response document the guest reads back with http_response_len and
// http_response. It returns 0 on a completed exchange and 1 on any policy or
// transport failure. Declarations run on a probe instance with no egress, so a
// request during memento_declare fails.
func hostHTTPRequest(ctx context.Context, m api.Module, reqPtr, reqLen uint32) uint32 {
	s := getExecState(ctx)
	if s == nil || s.instance == nil {
		return 1
	}
	return s.exchange(ctx, m, reqPtr, reqLen)
}

// exchange parses the request document, applies the policy, performs the
// exchange, and stashes the response document.
func (s *execState) exchange(ctx context.Context, m api.Module, reqPtr, reqLen uint32) uint32 {
	raw, err := readBytes(m, reqPtr, reqLen)
	if err != nil {
		s.warn("http_request: " + err.Error())
		return 1
	}
	var doc httpRequestDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		s.warn("http_request: malformed request document")
		return 1
	}
	method := strings.ToUpper(strings.TrimSpace(doc.Method))
	if method == "" {
		method = http.MethodGet
	}
	u, err := url.Parse(strings.TrimSpace(doc.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		s.warn("http_request: url must be an absolute http(s) URL")
		return 1
	}
	if !s.egress.allowed(u) {
		s.warn("http_request: host " + u.Hostname() + " is not permitted")
		return 1
	}

	transport, timeout := s.egress.resolve()
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	headers, err := substituteCredentials(doc.Headers, s.egress.Credentials)
	if err != nil {
		s.warn("http_request: " + err.Error())
		return 1
	}

	resp, err := transport.Do(reqCtx, HTTPRequest{
		Method:  method,
		URL:     u.String(),
		Headers: headers,
		Body:    []byte(doc.Body),
	})
	if err != nil {
		s.warn("http_request: " + err.Error())
		return 1
	}

	out, err := json.Marshal(httpResponseDoc{
		Status:  resp.Status,
		Headers: resp.Headers,
		Body:    string(resp.Body),
	})
	if err != nil {
		s.warn("http_request: encoding response: " + err.Error())
		return 1
	}
	s.mu.Lock()
	s.httpResp = out
	s.mu.Unlock()
	return 0
}

// hostHTTPResponseLen returns the length of the response document stashed by
// the last completed exchange, or 0 when there is none.
func hostHTTPResponseLen(ctx context.Context, m api.Module) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint32(len(s.httpResp))
}

// hostHTTPResponse copies the stashed response document into the caller's
// buffer, at most bufMax bytes, and returns the count copied.
func hostHTTPResponse(ctx context.Context, m api.Module, bufPtr, bufMax uint32) uint32 {
	s := getExecState(ctx)
	if s == nil {
		return 0
	}
	s.mu.Lock()
	resp := s.httpResp
	s.mu.Unlock()
	if len(resp) == 0 {
		return 0
	}
	mem := m.Memory()
	if mem == nil {
		return 0
	}
	toCopy := uint32(len(resp))
	if toCopy > bufMax {
		toCopy = bufMax
	}
	if !mem.Write(bufPtr, resp[:toCopy]) {
		return 0
	}
	return toCopy
}

// warn writes a diagnostic line to the instance's log writer, when one is
// configured. Transport failures stay visible without aborting the guest.
func (s *execState) warn(msg string) {
	if s == nil || s.logWriter == nil {
		return
	}
	_, _ = io.WriteString(s.logWriter, msg+"\n")
}
