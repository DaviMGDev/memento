package wasm

import (
	"context"
	"testing"

	"github.com/DaviMGDev/memento/runtime"
)

func TestHandleInvokesActiveGuest(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	comp, err := NewComponent(ctx, engine, buildHandlerProviderGuest())
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}

	s := runtime.New()
	defer s.Close()
	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForState(t, s, id, runtime.StateActive)

	resp, err := comp.Handle(ctx, []byte("ping"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if string(resp) != "pong" {
		t.Fatalf("Handle response = %q, want pong", resp)
	}
}

func TestHandleRequiresAnActiveInstance(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	comp, err := NewComponent(ctx, engine, buildHandlerProviderGuest())
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	if _, err := comp.Handle(ctx, nil); err == nil {
		t.Fatal("Handle on an inactive component should fail")
	}
}
