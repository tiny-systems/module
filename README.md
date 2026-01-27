# TinySystems Module SDK

A Kubebuilder-based Kubernetes operator SDK for building flow-based workflow engines. This SDK provides the complete infrastructure for developing modular operators that can be composed into visual workflows.

## Overview

TinySystems Module SDK enables developers to create **module operators** (like `common-module`, `http-module`, `grpc-module`) that bring specific functionality into a Kubernetes-native flow engine. Each module provides reusable components that can be connected through a port-based architecture to create complex workflows.

### Key Features

- **Port-Based Component Architecture**: Visual programming model with input/output ports
- **JSON Schema-Driven Configuration**: Automatic schema generation with UI hints
- **Message Routing & Retry Logic**: Intelligent routing with exponential backoff
- **Multi-Module Communication**: gRPC-based inter-module communication
- **Expression-Based Data Transformation**: Mustache-style `{{expression}}` syntax with JSONPath for flexible data mapping
- **Kubernetes-Native**: Everything is a CRD with standard controller patterns
- **OpenTelemetry Integration**: Built-in observability with tracing and metrics
- **CLI Tools**: Complete tooling for running and building modules

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
  - [Core Concepts](#core-concepts)
  - [SDK Components](#sdk-components)
  - [Communication Patterns](#communication-patterns)
  - [Design Patterns](#design-patterns)
  - [Scalability](#scalability)
  - [Project Structure](#project-structure)
- [Getting Started](#getting-started)
- [Helm Charts](#helm-charts)
- [Development](#development)
  - [Running on the Cluster](#running-on-the-cluster)
  - [Testing](#test-it-out)
  - [Modifying the API](#modifying-the-api-definitions)
- [Module Development](#module-development)
  - [Quick Start](#quick-start)
  - [Component Interface](#component-interface-deep-dive)
  - [Configuration Schemas](#configuration-schemas)
  - [System Ports](#system-ports)
  - [Error Handling](#error-handling)
  - [Resource Manager](#using-the-resource-manager)
  - [Observability](#observability)
  - [Testing](#testing-components)
  - [Best Practices](#best-practices)
- [Contributing](#contributing)
- [License](#license)

## Architecture

### Core Concepts

The SDK is built around several key abstractions:

#### Custom Resource Definitions (CRDs)

- **TinyNode**: Core execution unit representing a component instance in a flow
  - Contains module name, version, component type, port configurations, and edges
  - Status includes module/component info, port schemas, and error state

- **TinyModule**: Registry of available modules for service discovery
  - References container image
  - Status contains module address (gRPC), version, and list of components

- **TinyFlow**: Logical grouping of nodes representing a workflow
  - Uses labels to associate nodes together

- **TinyProject**: Top-level organizational unit grouping multiple flows

- **TinySignal**: External trigger mechanism for node execution
  - Specifies target node, port, and data payload
  - One-off: deleted immediately after successful delivery
  - Works like webhooks or manual triggers

- **TinyTracker**: Execution monitoring for detailed tracing

- **TinyWidgetPage**: Custom UI dashboard pages for visualization

#### Component System

Components are the building blocks of workflows. Each component:

- Implements the `Component` interface (`module/component.go`)
- Defines **ports** for input/output with positions (Top/Right/Bottom/Left)
- Handles messages through the `Handle()` method
- Provides JSON Schema configuration for each port

#### Message Flow

**Standard message flow:**
```
External Trigger (TinySignal)
  → TinySignal Controller
    → Scheduler.Handle()
      → Runner.MsgHandler()
        → Component.Handle()
          → output() callback
            → Next node via edges
  → TinySignal deleted (one-off)
```

#### Port System

Ports enable component communication:
- **Source Ports**: Output ports that send data
- **Target Ports**: Input ports that receive data
- **System Ports**:
  - `_reconcile`: Triggers node reconciliation
  - `_client`: Receives Kubernetes client for resource operations
  - `_settings`: Configuration port
  - `_control`: Dashboard control port

Each port can have:
- **Configuration**: JSON schema defining expected input structure
- **Response Configuration**: Schema for output data structure

#### Edge Configuration

Edges connect ports between nodes and support data transformation using mustache-style expressions:

**Expression Syntax**: Use `{{expression}}` to evaluate JSONPath expressions against incoming data.

```json
{
  "body": "{{$.request.body}}",
  "statusCode": 200,
  "greeting": "Hello {{$.user.name}}!",
  "isAdmin": "{{$.user.role == 'admin'}}"
}
```

**Expression Types**:
- **Pure expression** (`"{{$.field}}"`) - Returns the actual type (string, number, boolean, object)
- **Literal value** (`"hello"`, `200`, `true`) - Static values passed through as-is
- **String interpolation** (`"Hello {{$.name}}!"`) - Embeds expression results in strings
- **JSONPath with operators** (`"{{$.count + 1}}"`, `"{{$.method == 'GET'}}"`) - Supports arithmetic and comparison

**Key Features**:
- Type preservation: `"{{$.count}}"` returns a number, not a string
- Graceful error handling: If source data is unavailable, expressions return `nil`
- Full JSONPath support via [ajson](https://github.com/tiny-systems/ajson) library

**Built-in Functions**:

The expression engine includes many built-in functions:

| Category | Functions |
|----------|-----------|
| **Array** | `length()`, `first()`, `last()`, `avg()`, `sum()`, `size()` |
| **String** | `upper()`, `lower()`, `trim()`, `reverse()`, `b64encode()`, `b64decode()` |
| **String (multi-arg)** | `split(str, sep)`, `join(arr, sep)`, `contains(str, sub)`, `hasprefix(str, prefix)`, `hassuffix(str, suffix)`, `replace(str, old, new)`, `substr(str, start[, len])`, `index(str, sub)` |
| **Math** | `abs()`, `ceil()`, `floor()`, `round()`, `sqrt()`, `pow10()` |
| **Logic** | `not()`, ternary `? :` |

**String Function Examples**:
```json
{
  "kind": "{{first(split($.target, '/'))}}",
  "name": "{{last(split($.target, '/'))}}",
  "hasNamespace": "{{contains($.args, '-n ')}}",
  "upperName": "{{upper($.name)}}",
  "label": "{{replace($.label, '=', ': ')}}",
  "domain": "{{first(split(last(split($.url, '//')), '/'))}}",
  "message": "{{hasprefix($.error, 'NotFound') ? 'Resource not found' : $.error}}"
}
```

### SDK Components

The SDK provides several packages for module developers:

#### `/module/` - Core Interfaces
- `Component`: Main interface for component implementation
- `Port`: Port definitions and configuration
- `Handler`: Callback function type for message routing

#### `/pkg/resource/` - Resource Manager
Unified Kubernetes client providing:
- Node/Module/Flow/Project CRUD operations
- Signal creation for triggering nodes
- Ingress/Service management for exposing ports
- Helm release management

#### `/pkg/schema/` - JSON Schema Generator
- Automatic schema generation from Go structs
- Custom struct tags for UI hints: `configurable`, `shared`, `propertyOrder`, `tab`, `align`
- Definition sharing and override mechanism

#### `/pkg/evaluator/` - Expression Evaluator
- Mustache-style `{{expression}}` syntax processing
- JSONPath evaluation via ajson library
- Type-preserving evaluation (numbers stay numbers, booleans stay booleans)
- Graceful error handling when source data is unavailable

#### `/pkg/metrics/` - Observability
- OpenTelemetry integration with spans
- Metrics (gauges, counters)
- Tracker system for flow execution monitoring

#### `/internal/scheduler/` - Message Routing
- Manages component instances
- Routes messages between nodes
- Handles cross-module communication via gRPC

#### `/internal/client/` - Client Pool
- Manages gRPC connections to other modules
- Connection pooling and lifecycle management

#### `/cli/` - Command-Line Tools
- `run`: Complete operator runtime
- `build`: Module building and publishing

### Communication Patterns

#### Same-Module Communication
When nodes belong to the same module, messages are routed directly through the scheduler for optimal performance.

#### Cross-Module Communication
When nodes belong to different modules:
1. Scheduler identifies the target module via TinyModule CRD
2. Client pool establishes/reuses gRPC connection
3. Message is sent to target module's gRPC server
4. Target module's scheduler routes to the appropriate component

#### Retry Mechanism
- **Transient Errors**: Exponential backoff (1s → 30s max)
- **Permanent Errors**: Marked via `PermanentError` wrapper to stop retries
- Context cancellation stops all retries

### Design Patterns

1. **Eventual Consistency with Reconciliation**: Periodic reconciliation (every 5 minutes) plus signal-based immediate updates
2. **Leader Election**: Leader handles control operations and blocking handlers for multi-pod coordination
3. **Schema-Driven Configuration**: Go structs automatically generate JSON schemas for UI integration
4. **Expression-Based Transformation**: Mustache-style `{{expression}}` syntax with JSONPath enables flexible data mapping without code changes
5. **Definition Sharing**: Components mark fields as `shared:true` or `configurable:true` for cross-node type safety
6. **One-off Signals**: TinySignals are deleted after delivery for clean trigger semantics

### Scalability

The SDK supports horizontal scaling of module operators with leader election for coordination.

#### Leader Election

- Uses Kubernetes native leader election via `k8s.io/client-go/tools/leaderelection` with Lease resources
- Configuration: 15s lease duration, 10s renew deadline, 2s retry period
- Each pod identifies itself using the `HOSTNAME` environment variable
- Leadership state tracked via `isLeader` atomic boolean
- **Purpose**: Only leader runs blocking handlers (e.g., HTTP servers, long-running processes)

#### One-off TinySignals

- TinySignals are deleted immediately after successful delivery
- Clean trigger semantics without signal accumulation

### Project Structure

```
.
├── api/v1alpha1/          # CRD definitions (TinyNode, TinyModule, TinyFlow, etc.)
├── module/                # Core SDK interfaces for component developers
│   ├── component.go       # Component interface
│   ├── node.go           # Port definitions
│   └── handler.go        # Handler function type
├── pkg/                   # Reusable SDK packages
│   ├── resource/         # Kubernetes resource manager
│   ├── schema/           # JSON schema generator
│   ├── evaluator/        # JSONPath expression evaluator
│   ├── errors/           # Error handling utilities
│   └── metrics/          # OpenTelemetry integration
├── internal/              # Internal operator implementation
│   ├── controller/       # Kubernetes controllers
│   ├── scheduler/        # Message routing and execution
│   ├── server/           # gRPC server
│   └── client/           # gRPC client pool
├── cli/                   # Command-line tools (run, build)
├── registry/              # Component registration system
├── config/                # Kubernetes manifests and CRD definitions
│   ├── crd/              # CRD YAML files
│   ├── samples/          # Example resources
│   └── rbac/             # RBAC configurations
└── charts/                # Helm charts for deployment
```

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

## Helm charts
```shell
helm repo add tinysystems https://tiny-systems.github.io/module/
helm repo update # if you already added repo before
helm install my-corp-data-processing-tools --set controllerManager.manager.image.repository=registry.mycorp/tools/data-processing  tinysystems/tinysystems-operator
```

### Running on the cluster
1. Install Instances of Custom Resources:

```shell
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```shell
make docker-build docker-push IMG=<some-registry>/operator:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```shell
make deploy IMG=<some-registry>/operator:tag
```


### Undeploy controller
UnDeploy the controller from the cluster:

```shell
make undeploy
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```shell
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```shell
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```shell
make manifests
```

### Create new api
```shell
kubebuilder create api --group operator --version v1alpha1 --kind TinySignal
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

### Module Development

This SDK provides everything you need to build custom module operators. Here's a complete guide to developing your own module.

#### Quick Start

1. **Create a New Go Project**

```bash
mkdir my-module
cd my-module
go mod init github.com/myorg/my-module
```

2. **Add SDK Dependency**

```bash
go get github.com/tiny-systems/module
```

3. **Implement a Component**

Create `components/hello.go`:

```go
package components

import (
    "context"
    "github.com/tiny-systems/module/module"
)

type Hello struct{}

// Configuration for the input port
type HelloInput struct {
    Name string `json:"name" configurable:"true"`
}

// Configuration for the output
type HelloOutput struct {
    Greeting string `json:"greeting" shared:"true"`
}

func (h *Hello) Instance() module.Component {
    return &Hello{}
}

func (h *Hello) GetInfo() module.ComponentInfo {
    return module.ComponentInfo{
        Name:        "hello",
        Description: "Greets a person by name",
        Info:        "Simple greeting component example",
        Tags:        []string{"example", "greeting"},
    }
}

func (h *Hello) Ports() []module.Port {
    return []module.Port{
        {
            Name:          "input",
            Label:         "Input",
            Source:        false, // This is an input port
            Position:      module.Left,
            Configuration: &HelloInput{},
        },
        {
            Name:     "output",
            Label:    "Output",
            Source:   true, // This is an output port
            Position: module.Right,
            Configuration: &HelloOutput{},
        },
        {
            Name:     "error",
            Label:    "Error",
            Source:   true,
            Position: module.Bottom,
        },
    }
}

func (h *Hello) Handle(ctx context.Context, output module.Handler, port string, message any) any {
    if port == "input" {
        // Parse input configuration
        input := message.(*HelloInput)

        // Create greeting
        greeting := "Hello, " + input.Name + "!"

        // Send to output port
        output(ctx, "output", &HelloOutput{
            Greeting: greeting,
        })
    }
    return nil
}
```

4. **Register Component and Create Main**

Create `main.go`:

```go
package main

import (
    "github.com/myorg/my-module/components"
    "github.com/tiny-systems/module/cli"
    "github.com/tiny-systems/module/registry"
)

func main() {
    // Register all components
    registry.Register(&components.Hello{})

    // Run the operator
    cli.Run()
}
```

5. **Run Your Module**

```bash
# Build
go build -o my-module

# Run locally (connects to your current kubectl context)
./my-module run --name=my-module --version=1.0.0 --namespace=tinysystems
```

6. **Deploy to Kubernetes**

```bash
# Build and push Docker image
docker build -t myregistry/my-module:1.0.0 .
docker push myregistry/my-module:1.0.0

# Install using Helm
helm repo add tinysystems https://tiny-systems.github.io/module/
helm install my-module \
  --set controllerManager.manager.image.repository=myregistry/my-module \
  --set controllerManager.manager.image.tag=1.0.0 \
  tinysystems/tinysystems-operator
```

#### Component Interface Deep Dive

##### GetInfo() - Component Metadata

```go
func (c *MyComponent) GetInfo() module.ComponentInfo {
    return module.ComponentInfo{
        Name:        "my-component",      // Unique identifier
        Description: "Does something",     // Short description
        Info:        "Detailed info...",   // Long description
        Tags:        []string{"tag1"},     // Searchable tags
    }
}
```

##### Ports() - Define Component Ports

Ports define how components connect to each other:

```go
func (c *MyComponent) Ports() []module.Port {
    return []module.Port{
        {
            Name:          "input",           // Unique port name
            Label:         "Input Data",      // Display label
            Source:        false,             // Input port
            Position:      module.Left,       // Visual position
            Configuration: &InputConfig{},    // Expected data structure
        },
        {
            Name:              "output",
            Label:             "Output Data",
            Source:            true,          // Output port
            Position:          module.Right,
            Configuration:     &OutputConfig{}, // Output data structure
        },
    }
}
```

**Port Positions**: `module.Top`, `module.Right`, `module.Bottom`, `module.Left`

##### Handle() - Process Messages

The `Handle` method is called when a message arrives on a port:

```go
func (c *MyComponent) Handle(
    ctx context.Context,
    output module.Handler,
    port string,
    message any,
) any {
    switch port {
    case "input":
        // Type assert the message
        input := message.(*InputConfig)

        // Do work
        result := processData(input)

        // Send to output port
        output(ctx, "output", &OutputConfig{
            Result: result,
        })

    case "_reconcile":
        // Handle reconciliation (called periodically)
        // Use this for cleanup, state sync, etc.
    }

    return nil
}
```

**Key Points**:
- `ctx`: Context with tracing span and cancellation
- `output`: Callback function to send data to other ports
- `port`: Name of the port that received the message
- `message`: The actual data (type assert to your config struct)
- Return value is currently unused

#### Configuration Schemas

The SDK automatically generates JSON Schemas from your Go structs. Use struct tags to control the UI:

```go
type Config struct {
    // Basic field
    Name string `json:"name"`

    // Configurable in UI (can reference other node outputs)
    UserID string `json:"userId" configurable:"true"`

    // Shared definition (other nodes can reference this)
    Result string `json:"result" shared:"true"`

    // Control UI layout
    APIKey string `json:"apiKey" propertyOrder:"1" tab:"auth"`

    // Nested object
    Settings struct {
        Timeout int `json:"timeout" configurable:"true"`
    } `json:"settings"`

    // Array
    Items []string `json:"items" configurable:"true"`
}
```

**Struct Tags**:
- `configurable:"true"`: Field can accept values from other nodes via `{{expression}}` syntax
- `shared:"true"`: Field definition is available to other nodes for type-safe mapping
- `propertyOrder:"N"`: Controls field order in UI
- `tab:"name"`: Groups field under a tab in UI
- `align:"horizontal"`: Layout hint for UI

#### System Ports

Special ports available to all components:

##### `_reconcile` Port
Called periodically (every 5 minutes) and on node changes:

```go
case "_reconcile":
    // Clean up resources
    // Sync state
    // Check for drift
```

##### `_client` Port
Provides Kubernetes client for resource operations:

```go
case "_client":
    client := message.(resource.Manager)

    // Create a signal
    client.CreateSignal(ctx, resource.CreateSignalRequest{
        Node: "target-node",
        Port: "input",
        Data: map[string]any{"key": "value"},
    })
    
    // Get node information
    node, err := client.GetNode(ctx, "node-name")
```

##### `_settings` Port
Receives initial configuration (no "from" connection required):

```go
case "_settings":
    settings := message.(*MyConfig)
    // Store settings for later use
```

#### Error Handling

##### Transient Errors
Return regular errors for automatic retry with exponential backoff:

```go
func (c *MyComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) any {
    data, err := fetchFromAPI()
    if err != nil {
        // Will retry automatically
        return err
    }
    // ...
}
```

##### Permanent Errors
Use `PermanentError` to stop retries:

```go
import "github.com/tiny-systems/module/pkg/errors"

func (c *MyComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) any {
    if !isValid(msg) {
        // Won't retry - send to error port instead
        output(ctx, "error", errors.PermanentError{
            Err: fmt.Errorf("invalid input"),
        })
        return nil
    }
    // ...
}
```

#### Using the Resource Manager

Access Kubernetes resources from your component:

```go
import "github.com/tiny-systems/module/pkg/resource"

func (c *MyComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) any {
    if port == "_client" {
        c.client = msg.(resource.Manager)
        return nil
    }

    if port == "create-flow" {
        // Create a new flow
        flow, err := c.client.CreateFlow(ctx, resource.CreateFlowRequest{
            Name:    "dynamic-flow",
            Project: "my-project",
        })

        // Create nodes in the flow
        node, err := c.client.CreateNode(ctx, resource.CreateNodeRequest{
            Name:      "node-1",
            Flow:      flow.Name,
            Module:    "http-module",
            Component: "request",
            Settings: map[string]any{
                "url": "https://api.example.com",
            },
        })

        // Trigger the node
        c.client.CreateSignal(ctx, resource.CreateSignalRequest{
            Node: node.Name,
            Port: "trigger",
            Data: map[string]any{},
        })
    }

    return nil
}
```

#### Observability

OpenTelemetry is built-in. The context includes a span:

```go
import "go.opentelemetry.io/otel"

func (c *MyComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) any {
    // Get tracer
    tracer := otel.Tracer("my-module")

    // Create child span
    ctx, span := tracer.Start(ctx, "processing")
    defer span.End()

    // Add attributes
    span.SetAttributes(
        attribute.String("input.size", "large"),
    )

    // Do work...
    result := doWork(ctx)

    output(ctx, "output", result)
    return nil
}
```

#### Testing Components

```go
package components_test

import (
    "context"
    "testing"
    "github.com/myorg/my-module/components"
)

func TestHello(t *testing.T) {
    component := &components.Hello{}

    var outputData *components.HelloOutput
    outputHandler := func(ctx context.Context, port string, data any) any {
        if port == "output" {
            outputData = data.(*components.HelloOutput)
        }
        return nil
    }

    // Send message to input port
    component.Handle(context.Background(), outputHandler, "input", &components.HelloInput{
        Name: "World",
    })

    // Verify output
    if outputData.Greeting != "Hello, World!" {
        t.Errorf("Expected 'Hello, World!', got '%s'", outputData.Greeting)
    }
}
```

#### Best Practices

1. **Keep Components Focused**: Each component should do one thing well
2. **Use System Ports**: Implement `_reconcile` for cleanup
3. **Handle Context Cancellation**: Respect `ctx.Done()` for graceful shutdown
4. **Leverage Schemas**: Use struct tags to create great UI experiences
5. **Share Definitions**: Mark output fields as `shared:true` for type-safe flows
6. **Use Permanent Errors**: Don't retry validation errors or user mistakes
7. **Add Observability**: Create spans for long operations
8. **Document with Tags**: Use meaningful tags in `GetInfo()` for discoverability
9. **Always Return Handler Results**: See critical section below

#### CRITICAL: Handler Response Propagation

**ALWAYS return the result of `handler()` calls. Never ignore the return value.**

The TinySystems SDK uses blocking I/O for request-response patterns. When a component like HTTP Server sends a request, it **blocks** waiting for a response to flow back through the same handler chain. If any component in the chain ignores the handler return value, the response is lost and the original caller times out.

**BAD - Breaks blocking I/O:**
```go
func (c *Component) handleError(ctx context.Context, handler module.Handler, req Request, errMsg string) any {
    if c.settings.EnableErrorPort {
        _ = handler(ctx, "error", Error{...})  // WRONG: ignores return value!
        return nil  // Response is lost, HTTP Server times out
    }
    return errors.New(errMsg)
}
```

**GOOD - Propagates response correctly:**
```go
func (c *Component) handleError(ctx context.Context, handler module.Handler, req Request, errMsg string) any {
    if c.settings.EnableErrorPort {
        return handler(ctx, "error", Error{...})  // CORRECT: returns handler result
    }
    return errors.New(errMsg)
}
```

**Why this matters:**

When HTTP Server → Slack Command → Router → HTTP Server:Response:

1. HTTP Server blocks on `handler(ctx, "request", req)`
2. Message flows to Slack Command via gRPC
3. Slack Command processes and calls `handler(ctx, "error", error)`
4. Edge transforms Error → Response and sends to HTTP Server's response port
5. HTTP Server's `handleResponse()` returns the Response
6. **Response must flow back through the entire chain** to unblock HTTP Server

If Slack Command does `_ = handler(...)` and returns `nil`, the Response is lost.

**Rules:**
- Always `return handler(ctx, port, data)` for output ports
- Exception: `_reconcile` port calls can ignore returns (internal system port)
- Exception: Fire-and-forget async operations where no response is expected

#### Component Naming Convention

All component names must follow a consistent naming pattern using `snake_case`:

**Pattern:** `[technology_][resource]_[action]` or `[resource]_[action]`

**Rules:**
1. Always use `snake_case` (lowercase with underscores)
2. Resource/noun comes before action/verb
3. Include technology prefix when multiple implementations exist
4. Keep names concise (2-3 words max)

**Examples by category:**

| Category | Name | Description |
|----------|------|-------------|
| **HTTP** | `http_server` | HTTP server |
| | `http_request` | Make HTTP request |
| | `http_auth_parse` | Parse auth header |
| **Encoding** | `json_encode` | Encode to JSON |
| | `json_decode` | Decode from JSON |
| | `go_template` | Render Go template |
| **Email** | `smtp_send` | Send email via SMTP |
| | `sendgrid_send` | Send email via SendGrid API |
| **Messaging** | `slack_send` | Send Slack message |
| | `slack_command` | Receive Slack command |
| **Kubernetes** | `pod_status_get` | Get pod status |
| | `pod_logs_get` | Get pod logs |
| | `deployment_restart` | Restart deployment |
| | `resource_watch` | Watch K8s resources |
| **Utilities** | `transform` | Transform/passthrough data |
| | `delay` | Delay execution |
| | `router` | Route messages |

**When to include technology prefix:**
- Multiple implementations of same concept (e.g., `smtp_send` vs `sendgrid_send`)
- Technology-specific behavior (e.g., `go_template` vs `handlebars_template`)
- Clarity about protocol/API used (e.g., `grpc_call` vs `http_request`)

#### Code Style

Follow idiomatic Go patterns:

1. **Early returns** - Avoid nested ifs, return early on errors
2. **Flat structure** - Extract logic into small, focused functions
3. **Error handling** - Use `if err != nil { return }` pattern
4. **No deep nesting** - Maximum 2-3 levels of indentation

```go
// Good - early return
func (c *Component) Handle(ctx context.Context, handler module.Handler, port string, msg any) error {
    if port != "request" {
        return fmt.Errorf("unknown port: %s", port)
    }

    req, ok := msg.(Request)
    if !ok {
        return errors.New("invalid request")
    }

    result, err := c.process(ctx, req)
    if err != nil {
        return c.handleError(ctx, handler, req, err)
    }

    return handler(ctx, "output", result)
}

// Bad - nested ifs
func (c *Component) Handle(ctx context.Context, handler module.Handler, port string, msg any) error {
    if port == "request" {
        if req, ok := msg.(Request); ok {
            if result, err := c.process(ctx, req); err == nil {
                return handler(ctx, "output", result)
            } else {
                return c.handleError(ctx, handler, req, err)
            }
        }
    }
    return fmt.Errorf("unknown port: %s", port)
}
```

#### Component Design Principles

**1. Minimize Settings**

Settings should ONLY contain:
- **Port flags** - `EnableErrorPort`, `EnableStatusPort` (affect port visibility)
- **Precompiled resources** - Go templates, JS code (compiled once on change)
- **Defaults** - Rarely-changing default values

Settings should NOT contain:
- Credentials (API keys, tokens, signing secrets)
- Runtime URLs/endpoints
- Per-request parameters
- Anything that varies between executions

```go
// Good - minimal settings
type Settings struct {
    EnableErrorPort bool  `json:"enableErrorPort" title:"Enable Error Port"`
    DefaultLines    int64 `json:"defaultLines" title:"Default Lines"`
}

// Bad - credentials and runtime config in settings
type Settings struct {
    APIKey      string `json:"apiKey"`       // Should be in request
    Endpoint    string `json:"endpoint"`     // Should be in request
    Namespace   string `json:"namespace"`    // Should be in request
}
```

**2. Credentials via Input Ports**

All credentials and runtime configuration come through input ports:

```go
type Request struct {
    Context   any    `json:"context,omitempty" configurable:"true"`

    // Credentials - from upstream (e.g., secret manager)
    APIKey    string `json:"apiKey" required:"true" configurable:"true"`

    // Runtime config - varies per execution
    Endpoint  string `json:"endpoint" required:"true" configurable:"true"`
    Namespace string `json:"namespace" required:"true" configurable:"true"`
}
```

**Why:** Settings are spread across flows, not programmatically configurable, and storing credentials in settings is a security anti-pattern.

**3. Context Passthrough**

Every component MUST pass context through for correlation:

```go
type Request struct {
    Context any `json:"context,omitempty" configurable:"true" title:"Context" description:"Arbitrary data passed through to output"`
    // ... other fields
}

type Response struct {
    Context any `json:"context,omitempty" title:"Context"`
    // ... other fields
}

func (c *Component) Handle(ctx context.Context, handler module.Handler, port string, msg any) error {
    req := msg.(Request)

    result := c.process(req)

    // Always pass context through
    handler(ctx, "output", Response{
        Context: req.Context,  // Pass through!
        Data:    result,
    })
    return nil
}
```

**4. Flow-Driven Configuration**

Everything should be configurable via edges/signals, not the settings panel:
- Credentials mapped from upstream components (secret managers, vaults)
- Runtime parameters from user input
- Makes flows self-contained, portable, and version-controllable

#### Example Modules

Check out these example modules for reference:
- **common-module**: Basic utilities (delay, switch, merge, signal)
- **http-module**: HTTP client/server components
- **grpc-module**: gRPC service components

#### CLI Reference

The SDK includes a CLI for running and building modules:

```bash
# Run module locally
./my-module run --name=my-module --version=1.0.0 --namespace=tinysystems

# Build (if custom build logic is needed)
./my-module build

# Get help
./my-module --help
```

## License

Copyright 2026 Tiny Systems Limited. All rights reserved.

This project is licensed under the [Business Source License 1.1](LICENSE).

**Key terms:**
- Free for production and non-production use
- Not permitted: offering this software as a managed service, cloud service, or SaaS
- Converts to Apache License 2.0 on January 11, 2031

For commercial licensing inquiries, contact Tiny Systems Limited.
