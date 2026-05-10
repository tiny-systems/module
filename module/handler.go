package module

import "context"

// Handler is the function components call to emit data to one of their
// own ports (or system ports). The framework injects an instance as the
// second argument of Component.Handle and again — for long-lived
// goroutines — via EmitterAware.OnEmitter.
//
// Returning Result rather than `any` is deliberate: the return must
// propagate up through every blocking-I/O caller (http-server is the
// canonical example), and an `any` return was trivial to silently drop
// with `_ = handler(...)` or a bare call. Result makes the shape explicit
// at every call site so review and tooling can flag drops.
//
// Construct Result via Ok / Fail / Pass — never zero-value it manually
// outside test code.
type Handler func(ctx context.Context, port string, data any) Result
