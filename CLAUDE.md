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
