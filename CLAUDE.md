# Claude Code Rules for TinySystems

## Thinking

- Think through solutions completely before proposing
- Consider edge cases and flow implications
- Don't give half-baked answers that need revision when questioned

## Code Style

- Early returns, no nested ifs
- Extract logic into small, focused functions
- Flat structure over deep nesting
- Idiomatic Go - if err != nil { return } pattern

## CRITICAL: Handler Response Propagation

**NEVER ignore the return value of handler() calls. ALWAYS return it.**

TinySystems uses blocking I/O. HTTP Server blocks waiting for responses to flow back through the handler chain. If any component ignores the handler return, responses are lost and requests time out.

`module.Handler` and `Component.Handle` both return `module.Result`. Construct it via `module.Ok(value)` or `module.Fail(err)`. Chain handler calls back as the Handle return:

```go
// WRONG — breaks blocking I/O, causes timeouts
_ = handler(ctx, "error", Error{...})
return module.Result{}

// CORRECT — propagates response back through call chain
return handler(ctx, "error", Error{...})
```

When emitting fresh data, wrap with Ok / Fail:

```go
// Fresh success (no chained downstream): wrap the payload
return module.Ok(Response{StatusCode: 200, Body: body})

// Fresh failure
return module.Fail(fmt.Errorf("invalid request: %w", err))

// No-op success (system-port branches, ignored ports)
return module.Result{}
```

**Only exceptions to "always return":**
- `_reconcile` port (internal system port, no response expected) — use the `ReconcileHandler` capability interface, not the legacy port branch
- True fire-and-forget async operations launched from background goroutines (use `Base.Emit` and let the zero-Result no-op stand)

When writing components, always ask: "Does this handler call need to return a response to an upstream blocker?" If yes (which is most cases), return the handler result.

## Error Ports: the Recovery Boundary Pattern

Error ports are how flow authors define **self-healing zones**. They are the entire fault-tolerance story in TinySystems — there is no separate retry layer or durability primitive. A component author's job is to expose the right toggle and route failures consistently so the pattern composes across flows.

**The contract authors expect:**

- Every component that can fail in ways an upstream caller might want to handle differently exposes a settings flag — by convention `EnableErrorPort bool` — and a corresponding source output port `ErrorPort = "error"` that appears in `Ports()` only when the flag is on.
- When the flag is off and an error happens inside `Handle`, return `module.Fail(err)`. The error bubbles up via `Result.Err()` through every intermediate component (each of which is following the "always return handler result" rule above) until it hits an enabled error port or the top of the flow.
- When the flag is on and an error happens, route via the handler instead:

```go
func (c *Component) handleError(ctx context.Context, handler module.Handler, reqCtx Context, err error) module.Result {
    if !c.settings.EnableErrorPort {
        return module.Fail(err)
    }
    return handler(ctx, ErrorPort, Error{
        Context: reqCtx,
        Error:   err.Error(),
    })
}
```

The two branches are not duplicates — they encode "let it bubble" vs "catch here and let the author wire recovery downstream from the error port".

**The mental model is try/catch on the canvas.** A chain `A → B → C` where C fails:
- C returns `module.Fail` → B's `handler(...)` returns Result with `Err() != nil` → B returns that Result up (standard pattern) → A's `handler(...)` returns Result with `Err() != nil` → A decides.
- If A has `EnableErrorPort` on and routes via the pattern above, the error fires out A's error port to whatever recovery flow the author wired.
- If A doesn't, the error keeps bubbling up A's own caller.

Each enabled error port is a "catch" boundary. Everything between two error-port boundaries (or between an error port and the top of the flow) is a single transactional unit from the flow author's POV.

**Practical rules for component authors:**

- Expose `EnableErrorPort` on any component that does external side effects (HTTP calls, DB writes, paid APIs, sends, etc.). Authors need recovery options for these.
- Don't expose it on pure transforms (json encode/decode, template render). Failure in a pure transform is a programming error, not a runtime condition worth catching mid-flow.
- The Error payload carries Context (so the recovery flow can correlate back to the original work) and an Error string (the failure message). Don't pack large unrelated data into Error structs — recovery flows usually need lean correlation, not the whole request.
- Always return the handler result from the error-routing call — same propagation rule as success.

## CRITICAL: System Port Delivery Order

**System ports (`_settings`, `_control`, `_reconcile`, `_identity`) have NO guaranteed delivery order.**

On pod restart or leadership change, `_reconcile` may fire before `_settings`. Components that persist state to metadata must guard against reconcile overwriting fresh in-memory values with stale metadata.

## Identity Port

The `_identity` port delivers a `v1alpha1.NodeIdentity` struct with the node's resource name, namespace, flow, and project. Use it when a component needs to namespace local resources (e.g., filesystem paths on a shared PVC).

```go
case v1alpha1.IdentityPort:
    id, ok := msg.(v1alpha1.NodeIdentity)
    if !ok {
        return fmt.Errorf("invalid identity")
    }
    c.storagePath = filepath.Join(os.Getenv("STORAGE_PATH"), id.NodeName)
    return nil
```

`NodeIdentity` fields: `NodeName`, `Namespace`, `FlowName`, `ProjectName`. Delivered once during reconciliation, like `_client`.

**Pattern: use a guard flag to prevent stale overwrites:**

```go
type Component struct {
    settings         Settings
    settingsFromPort bool // prevents _reconcile from overwriting with stale metadata
}

// _settings handler — set the flag
case v1alpha1.SettingsPort:
    c.settings = in
    c.settingsFromPort = true
    // if component is active, also persist to metadata

// _reconcile handler — check the flag
func (c *Component) handleReconcile(...) {
    if c.settingsFromPort {
        return nil // don't overwrite user-provided settings
    }
    c.restoreFromMetadata(metadata)
}
```

**Also: when active state changes settings (e.g. running cron receives new settings), persist to metadata immediately** so subsequent reconciles don't clobber the update.

## SDK vs Module Responsibilities

- SDK handles serialization/deserialization
- SDK handles metadata cleanup on state deletion
- Components receive properly typed messages, not []byte
- Components don't know about other modules' metadata keys

## Workflow

- Build and test before claiming something works
- Tag SDK first, then update modules with new SDK version
- Push changes proactively - don't wait to be asked

## Communication

- Be direct, not verbose
- Run commands instead of asking user to run them
- Don't claim things work without verification
