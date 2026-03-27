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

```go
// WRONG - breaks blocking I/O, causes timeouts
_ = handler(ctx, "error", Error{...})
return nil

// CORRECT - propagates response back through call chain
return handler(ctx, "error", Error{...})
```

**Only exceptions:**
- `_reconcile` port (internal system port, no response expected)
- True fire-and-forget async operations

When writing components, always ask: "Does this handler call need to return a response to an upstream blocker?" If yes (which is most cases), return the handler result.

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
