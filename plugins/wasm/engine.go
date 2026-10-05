package wasm

import (
	"context"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Engine wraps the wazero runtime for executing WASM plugin modules.
type Engine struct {
	runtime  wazero.Runtime
	bindings *bindingTable
	egress   Egress
}

// EngineOption configures an Engine.
type EngineOption func(*engineConfig)

// engineConfig is the resolved configuration of one Engine.
type engineConfig struct {
	egress Egress
}

// WithHTTPTransport injects the transport used for guest HTTP exchanges. The
// default is a net/http client; hosts inject their own to record or stub
// egress in tests.
func WithHTTPTransport(t HTTPTransport) EngineOption {
	return func(c *engineConfig) { c.egress.Transport = t }
}

// WithHTTPAllowHosts constrains guest egress to the given hostnames. Passing
// no hosts leaves egress open, which is the default.
func WithHTTPAllowHosts(hosts ...string) EngineOption {
	return func(c *engineConfig) { c.egress.AllowHosts = append([]string(nil), hosts...) }
}

// WithHTTPTimeout bounds one guest HTTP exchange. Zero keeps the default.
func WithHTTPTimeout(d time.Duration) EngineOption {
	return func(c *engineConfig) { c.egress.Timeout = d }
}

// WithHTTPCredentialResolver resolves `env:NAME` references in guest request
// header values to the host's secret. Without it, reference values cross the
// wire literally, so a host that wants `env:` credentials names its resolver —
// typically an environment or secret-store lookup.
func WithHTTPCredentialResolver(resolve func(name string) (string, bool)) EngineOption {
	return func(c *engineConfig) { c.egress.Credentials = resolve }
}

// NewEngine creates a new WASM execution engine with default configuration.
//
// The WASI snapshot preview 1 host module is instantiated so guests built for
// wasi (notably Go's wasip1 port) can be loaded; core wasm guests that never
// import WASI are unaffected.
func NewEngine(ctx context.Context, opts ...EngineOption) (*Engine, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var cfg engineConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	r := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiating WASI: %w", err)
	}
	if err := RegisterHostModule(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm: registering host module: %w", err)
	}
	return &Engine{runtime: r, bindings: newBindingTable(), egress: cfg.egress}, nil
}

// Close releases resources associated with the WASM runtime.
func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return nil
	}
	if err := e.runtime.Close(ctx); err != nil {
		return fmt.Errorf("closing wasm runtime: %w", err)
	}
	return nil
}
