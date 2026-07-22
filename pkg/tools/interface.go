package tools

import (
	"context"
	"encoding/json"
	"time"
)

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Success bool
	Output  interface{}
	Error   string
}

// FlowSaver is an interface for saving flow changes
type FlowSaver interface {
	// SaveFlowElements saves the flow elements and returns an error if it fails
	SaveFlowElements(ctx context.Context, projectName, flowName string, elements []map[string]interface{}) error
}

// ProjectElements contains all project data for LLM context
type ProjectElements struct {
	Flows    []FlowInfo               `json:"flows"`
	Elements []map[string]interface{} `json:"elements"`
}

// FlowInfo contains basic flow information
type FlowInfo struct {
	ResourceName string `json:"resource_name"`
	DisplayName  string `json:"display_name"`
}

// ProjectReader is an interface for reading entire project state
type ProjectReader interface {
	// ReadProjectElements reads all elements from the project with flow ownership info
	ReadProjectElements(ctx context.Context, projectName string) (*ProjectElements, error)
}

// ProjectInfo is the basic info returned for each project by ProjectLister.
type ProjectInfo struct {
	ResourceName string `json:"resource_name"`
	DisplayName  string `json:"display_name"`
}

// ProjectLister enumerates projects accessible in the current context. The
// MCP server reads TinyProject CRDs from the cluster namespace; the
// platform-hosted version reads from the workspace database. The "scope"
// (which projects are visible) is implicit in the adapter.
type ProjectLister interface {
	ListProjects(ctx context.Context) ([]ProjectInfo, error)
}

// FlowModifier is an interface for applying flow changes (used by edit_flow's delete_node and delete_edge actions)
type FlowModifier interface {
	// ApplyFlowChanges applies operations to the flow and returns results per operation
	ApplyFlowChanges(ctx context.Context, projectName, flowName string, operations []FlowOperation) ([]OperationResult, error)
}

// FlowOperation represents a single operation to apply to the flow
type FlowOperation struct {
	Op      string                 `json:"op"`      // "add", "update", "delete"
	ID      string                 `json:"id"`      // Element ID (for update/delete)
	Element map[string]interface{} `json:"element"` // Element data (for add/update)
}

// OperationResult represents the result of a single operation
type OperationResult struct {
	Op      string `json:"op"`
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	// For add operations, the generated ID
	GeneratedID string `json:"generated_id,omitempty"`
	// Hint provides actionable guidance for fixing the error
	Hint string `json:"hint,omitempty"`
}

// EdgeValidation represents edge validation results
type EdgeValidation struct {
	EdgeID string `json:"edge_id"`
	Valid  bool   `json:"valid"`
	Error  string `json:"error,omitempty"`
}

// SolutionSearcher is an interface for searching and retrieving solutions.
// Platform implementations hit a DB; public MCP implementations hit the
// public REST catalog on tinysystems.io. Keep the interface backend-agnostic.
type SolutionSearcher interface {
	// SearchSolutions searches for solutions by keyword/tags
	SearchSolutions(ctx context.Context, keyword string, tags []string, limit int) ([]SolutionSummary, error)
	// GetSolution gets full solution details including flows and nodes
	GetSolution(ctx context.Context, uuid string) (*SolutionDetails, error)
}

// PortInspectResult contains the result of inspecting a port
type PortInspectResult struct {
	NodeID       string                 `json:"node_id"`
	PortName     string                 `json:"port_name"`
	PortType     string                 `json:"port_type"` // "source" or "target"
	Schema       map[string]interface{} `json:"schema"`
	ExampleData  interface{}            `json:"example_data"`
	HasRealData  bool                   `json:"has_real_data,omitempty"` // true if example_data is from trace
	Configurable bool                   `json:"configurable,omitempty"`  // true if schema can be customized
	Description  string                 `json:"description,omitempty"`
}

// PortInspector is an interface for inspecting port schemas and data
// This uses recursive simulation through the flow graph, not static schema lookup
type PortInspector interface {
	// InspectPort returns the actual schema and simulated/real data for a port
	// If traceID is provided, uses real trace data instead of mocks
	InspectPort(ctx context.Context, projectName, nodeID, portName, traceID string) (*PortInspectResult, error)
}

// PortDetail contains port metadata including schema for system prompt injection
type PortDetail struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Example     json.RawMessage `json:"example,omitempty"`
}

// ComponentInfo contains component metadata for validation and discovery
type ComponentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Info carries the component's behavior notes (blocking semantics,
	// gotchas, when to use it). Populated when the module operator
	// publishes TinyModuleComponentStatus.Info; LLM callers should read
	// this before wiring.
	Info        string   `json:"info,omitempty"`
	InputPorts  []string `json:"input_ports"`
	OutputPorts []string `json:"output_ports"`
	// Tags propagate component-level tags set at module publish time.
	// The SDK build helper auto-adds AgentToolTag for components that
	// implement module.AgentTool; the MCP layer filters on this to
	// dynamically expose components as LLM tools.
	Tags []string `json:"tags,omitempty"`
	// Detailed port info with schemas — populated by GetModule, optional for ListModules
	InputPortDetails  []PortDetail `json:"input_port_details,omitempty"`
	OutputPortDetails []PortDetail `json:"output_port_details,omitempty"`
	// SettingsSchema is the component's _settings port schema — it names
	// the configurable setting fields (their exact paths + types) so
	// callers set them by the real names instead of guessing. The
	// _settings port itself is not wireable; this is config metadata.
	SettingsSchema json.RawMessage `json:"settings_schema,omitempty"`
}

// ModuleInfo contains module metadata with its components
type ModuleInfo struct {
	Name        string          `json:"name"`
	Version     string          `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Components  []ComponentInfo `json:"components"`
}

// ModuleCatalog provides discovery of modules and their components.
// Platform implementations read from the workspace DB; public MCP
// implementations read from TinyModule CRDs in the current namespace.
// Scope (workspace vs namespace) is implicit in the implementation.
type ModuleCatalog interface {
	// ListModules returns all available modules with basic component info
	// (names and port names, no schemas). Used by list_modules tool.
	ListModules(ctx context.Context) ([]ModuleInfo, error)
	// GetModule returns a specific module with full component details
	// including port schemas and examples. Used by get_component_info tool.
	// Module name may be a workspace-qualified name (e.g. "tinysystems/http-module-v0")
	// or a bare name — implementations should match both.
	// Returns nil if module not found (no error).
	GetModule(ctx context.Context, moduleName string) (*ModuleInfo, error)
}

// InstallProgress is one progress event emitted by ModuleInstaller
// during a streaming install. The platform-mode implementation maps
// each gRPC InstallProgress event to one of these; the local helm
// path can emit at the granularity it wants.
type InstallProgress struct {
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message"`
	LogType string `json:"log_type,omitempty"` // "info" | "warning" | "error" | "success"
}

// InstallResult carries the terminal state of an install attempt.
// Success=true means the module is deployed and reconciling; the
// caller can immediately list_modules / get_component_info to see
// it. Success=false means the install never completed; Error has
// the failure message.
type InstallResult struct {
	Success         bool   `json:"success"`
	ModuleVersionID string `json:"module_version_id,omitempty"`
	ReleaseName     string `json:"release_name,omitempty"`
	Error           string `json:"error,omitempty"`
}

// ModuleInstaller installs a module into the active cluster /
// workspace. Hosted-platform mode (mcp-server with --platform-token)
// implements this by streaming platform.mcp.v1.MCPService.InstallModule
// and surfacing the progress events through onProgress. Kubeconfig
// mode currently leaves this nil — users run `helm install` from
// their own shell via the command surfaced by get_module_info.
//
// onProgress is invoked for every progress event before the call
// returns. It may be nil if the caller doesn't want streaming.
//
// bundles semantics — same as the platform RPC:
//
//	nil           → use module-default bundle selection
//	[]string{}    → install with no bundles
//	["a", "b"]    → install with exactly those bundles
type ModuleInstaller interface {
	InstallModule(ctx context.Context, moduleName, version string, bundles []string, onProgress func(InstallProgress)) (*InstallResult, error)
}

// ModuleUninstaller removes a module's helm release from the active
// cluster / workspace. Platform-mode implementation calls the
// matching MCPService.UninstallModule unary RPC. Kubeconfig mode
// leaves this nil; users uninstall via `helm uninstall <release>`
// from their own shell.
//
// The platform refuses when any node still references the module —
// the InUse field on the result lists the offending node IDs and
// Success is false. Callers should delete those flows / nodes
// first, then retry.
type ModuleUninstaller interface {
	UninstallModule(ctx context.Context, moduleName string) (*UninstallResult, error)
}

// UninstallResult carries the terminal state of an uninstall call.
// On Success=true the module's helm release is gone and the
// deployment row is cleaned up. On Success=false either the module
// wasn't installed (caller's error) or the in-use guard rejected
// the call — InUse names the blocking nodes when that's the cause.
type UninstallResult struct {
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
	InUse   []string `json:"in_use,omitempty"`
}

// PublicModuleCatalog provides discovery of modules in the public Tiny
// Systems catalog — the catalog counterpart to ModuleCatalog. Where
// ModuleCatalog answers "what is installed in this cluster/workspace",
// PublicModuleCatalog answers "what is available to install". LLMs
// reach for it when list_modules returns empty or when a solution
// being cloned references a module that is not yet installed.
//
// Public-mode mcp-server implements this against the platform REST API
// at /v1/modules/search and /v1/modules/{name}. Hosted-platform
// implementations can back it with the same gRPC ModuleService the UI
// uses.
type PublicModuleCatalog interface {
	// SearchModules searches the public catalog by keyword.
	// Returns summaries only — call GetPublicModule for full details.
	SearchModules(ctx context.Context, keyword string, limit int) ([]PublicModuleSummary, error)
	// GetPublicModule returns the full catalog entry for a module,
	// including components with port schemas, RBAC permissions, and
	// helm install instructions. Returns nil if not found (no error).
	GetPublicModule(ctx context.Context, moduleName string) (*PublicModuleDetails, error)
}

// PublicModuleSummary is a brief overview of a module in the public
// catalog, returned by search_modules.
type PublicModuleSummary struct {
	Name                     string `json:"name"`
	FullName                 string `json:"full_name,omitempty"`
	Description              string `json:"description"`
	Verified                 bool   `json:"verified"`
	LatestVersion            string `json:"latest_version,omitempty"`
	SDKVersion               string `json:"sdk_version,omitempty"`
	RequiresKubernetesAccess bool   `json:"requires_kubernetes_access,omitempty"`
}

// PublicModuleDetails is the full catalog payload returned by
// get_module_info: summary + components with port schemas + RBAC
// permissions + helm install configuration.
type PublicModuleDetails struct {
	Name                     string                   `json:"name"`
	FullName                 string                   `json:"full_name,omitempty"`
	Description              string                   `json:"description"`
	Verified                 bool                     `json:"verified"`
	LatestVersion            string                   `json:"latest_version,omitempty"`
	SDKVersion               string                   `json:"sdk_version,omitempty"`
	ReleaseNotes             string                   `json:"release_notes,omitempty"`
	Repo                     string                   `json:"repo,omitempty"`
	Tag                      string                   `json:"tag,omitempty"`
	RequiresKubernetesAccess bool                     `json:"requires_kubernetes_access,omitempty"`
	Components               []PublicModuleComponent  `json:"components,omitempty"`
	Permissions              []PublicModulePermission `json:"permissions,omitempty"`
	HelmInstall              *PublicModuleHelmInstall `json:"helm_install,omitempty"`
}

// PublicModuleComponent describes one component inside a public module:
// name + author-written description/info + tags + its typed ports.
type PublicModuleComponent struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Info        string             `json:"info,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
	Ports       []PublicModulePort `json:"ports,omitempty"`
}

// PublicModulePort is the typed port metadata published alongside a
// component in the public catalog.
type PublicModulePort struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Type        string                 `json:"type"` // "source" or "target"
	Schema      map[string]interface{} `json:"schema,omitempty"`
	DefaultData map[string]interface{} `json:"default_data,omitempty"`
}

// PublicModulePermission is an RBAC rule the module needs to install.
type PublicModulePermission struct {
	APIGroups []string `json:"api_groups,omitempty"`
	Resources []string `json:"resources,omitempty"`
	Verbs     []string `json:"verbs,omitempty"`
}

// PublicModuleHelmInstall carries the helm install configuration used
// by local installers — command template, user-fillable fields,
// prerequisites, warnings, and the storage/ingress facet flags.
type PublicModuleHelmInstall struct {
	Command         string                  `json:"command,omitempty"`
	ChartRepo       string                  `json:"chart_repo,omitempty"`
	ChartName       string                  `json:"chart_name,omitempty"`
	Prerequisites   []string                `json:"prerequisites,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
	RequiresIngress bool                    `json:"requires_ingress,omitempty"`
	RequiresStorage bool                    `json:"requires_storage,omitempty"`
	Fields          []PublicModuleHelmField `json:"fields,omitempty"`
}

// PublicModuleHelmField is one user-fillable field in the helm install
// template.
type PublicModuleHelmField struct {
	Name         string   `json:"name"`
	Label        string   `json:"label,omitempty"`
	Description  string   `json:"description,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	Placeholder  string   `json:"placeholder,omitempty"`
	Required     bool     `json:"required,omitempty"`
	Type         string   `json:"type,omitempty"`
	Options      []string `json:"options,omitempty"`
}

// DashboardReader reads project dashboard widgets and their current
// runtime values from TinyWidgetPage CRDs + TinyNode status ports.
type DashboardReader interface {
	ReadDashboard(ctx context.Context, projectName string) (*DashboardData, error)
}

// DashboardWriter pins a node's port to the project dashboard as a widget, or
// unpins it. This is the write half of DashboardReader.
//
// It exists because a credential a user must type has nowhere else to go: the
// canonical pattern is a widget with a secret:true field that the user fills in
// at start-up. Without a write path an agent can build the node and declare the
// schema but cannot surface the form, so the flow dead-ends in "now go add a
// widget by hand" — which is why agents reach for cluster Secrets instead.
type DashboardWriter interface {
	// SetNodeWidget pins nodeID's port as a widget when enabled, and removes it
	// when not. Returns the dashboard page the widget landed on. Pinning an
	// already-pinned port, or unpinning an absent one, is not an error.
	SetNodeWidget(ctx context.Context, projectName, nodeID, portName string, enabled bool) (page string, err error)
}

// DashboardData is the full dashboard payload for a project.
type DashboardData struct {
	ProjectName string            `json:"project_name"`
	Widgets     []DashboardWidget `json:"widgets"`
}

// DashboardWidget represents one widget on the dashboard with its
// current runtime data resolved from the corresponding TinyNode
// status port.
type DashboardWidget struct {
	Name     string                 `json:"name"`
	NodeName string                 `json:"node_name"`
	PortName string                 `json:"port_name"`
	Data     map[string]interface{} `json:"data,omitempty"`
	GridX    int                    `json:"grid_x"`
	GridY    int                    `json:"grid_y"`
	GridW    int                    `json:"grid_w"`
	GridH    int                    `json:"grid_h"`
}

// NodeAdder is an interface for adding nodes with semantic operations
type NodeAdder interface {
	// AddNode adds a node and returns its generated ID and ports
	// tracker is used to check positions of nodes added in current session (not yet in cluster)
	AddNode(ctx context.Context, projectName, flowName, component, module string, tracker PositionTracker) (*AddNodeResult, error)
}

// AddNodeResult contains the result of adding a node
type AddNodeResult struct {
	NodeID string   `json:"node_id"`
	Ports  []string `json:"ports"`
	PosX   int      `json:"pos_x"` // X position for tracking
	PosY   int      `json:"pos_y"` // Y position for tracking
}

// EdgeAdder is an interface for adding edges with semantic operations
type EdgeAdder interface {
	// AddEdge connects two ports and returns edge info
	AddEdge(ctx context.Context, projectName, flowName string, fromNode, fromPort, toNode, toPort string) (*AddEdgeResult, error)
}

// AddEdgeResult contains the result of adding an edge
type AddEdgeResult struct {
	EdgeID             string `json:"edge_id"`
	NeedsConfiguration bool   `json:"needs_configuration"`
}

// EdgeConfigurer is an interface for configuring edges
type EdgeConfigurer interface {
	// ConfigureEdge sets edge configuration and validates it
	// If traceID is provided, validates against real trace data
	// schema is optional - if provided, it extends the schema for configurable fields on the target port
	ConfigureEdge(ctx context.Context, projectName, flowName, edgeID string, config map[string]interface{}, schema map[string]interface{}, traceID string) (*ConfigureEdgeResult, error)
}

// ConfigureEdgeResult contains the result of configuring an edge
type ConfigureEdgeResult struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// NodeSettingsConfigurer is an interface for configuring node settings
type NodeSettingsConfigurer interface {
	// ConfigureNodeSettings sets node settings (the _settings port configuration)
	// schema is optional - if provided, it extends the schema for configurable fields (e.g., context)
	ConfigureNodeSettings(ctx context.Context, projectName, flowName, nodeID string, settings map[string]interface{}, schema map[string]interface{}) (*ConfigureNodeSettingsResult, error)
}

// ConfigureNodeSettingsResult contains the result of configuring node settings
type ConfigureNodeSettingsResult struct {
	Valid bool     `json:"valid"`
	Error string   `json:"error,omitempty"`
	Hint  string   `json:"hint,omitempty"`
	Ports []string `json:"ports,omitempty"` // Updated ports after settings change (e.g., router ports change based on routes)
}

// SolutionSummary is a brief overview of a solution for search results
type SolutionSummary struct {
	UUID        string   `json:"uuid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// SolutionDetails contains full solution structure
type SolutionDetails struct {
	UUID        string           `json:"uuid"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Tags        []string         `json:"tags"`
	Flows       []SolutionFlow   `json:"flows"`
	Variables   []map[string]any `json:"variables,omitempty"`
}

// SolutionFlow represents a flow within a solution
type SolutionFlow struct {
	Title string         `json:"title"`
	Nodes []SolutionNode `json:"nodes"`
	Edges []SolutionEdge `json:"edges"`
}

// SolutionNode represents a node in a solution flow
type SolutionNode struct {
	ID        string         `json:"id"`
	Component string         `json:"component"`
	Module    string         `json:"module"`
	Settings  map[string]any `json:"settings,omitempty"`
	Position  map[string]any `json:"position,omitempty"`
}

// SolutionEdge represents an edge in a solution flow
type SolutionEdge struct {
	Source        string         `json:"source"`
	SourceHandle  string         `json:"source_handle"`
	Target        string         `json:"target"`
	TargetHandle  string         `json:"target_handle"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

// TraceSummary contains summary info for a single trace
type TraceSummary struct {
	ID       string `json:"id"`
	Spans    int    `json:"spans"`
	Errors   int    `json:"errors"`
	Data     int    `json:"data"`
	Duration int64  `json:"duration_ns"`
	Start    int64  `json:"start_unix_micro"`
	End      int64  `json:"end_unix_micro"`
}

// TraceSpanInfo contains a single span's details
type TraceSpanInfo struct {
	SpanID       string           `json:"span_id"`
	ParentSpanID string           `json:"parent_span_id,omitempty"`
	Name         string           `json:"name"`
	From         string           `json:"from,omitempty"`
	To           string           `json:"to,omitempty"`
	Port         string           `json:"port,omitempty"`
	DurationMs   float64          `json:"duration_ms"`
	Status       string           `json:"status,omitempty"`
	Events       []TraceEventInfo `json:"events,omitempty"`
}

// TraceEventInfo contains a single span event
type TraceEventInfo struct {
	Name string            `json:"name"`
	Data map[string]string `json:"data,omitempty"`
}

// SignalSender sends a signal (TinySignal CRD) to a node's input port to trigger execution
type SignalSender interface {
	SendSignal(ctx context.Context, projectName, nodeID, portName string, data []byte, traceID string) error
}

// FlowCreator creates new flows within a project
type FlowCreator interface {
	CreateFlow(ctx context.Context, projectName, flowName string) (string, error)
}

// FlowDeleter deletes flows from a project
type FlowDeleter interface {
	DeleteFlow(ctx context.Context, projectName, flowName string) error
}

// ScenarioItem contains basic scenario info returned by list
type ScenarioItem struct {
	ResourceName string `json:"resource_name"`
	Name         string `json:"name"`
	PortCount    int    `json:"port_count"`
}

// ScenarioManager manages scenarios (sample data snapshots for edge validation).
// Backed by TinyScenario CRDs — works identically in hosted and local modes.
type ScenarioManager interface {
	// CreateScenarioFromTrace creates a scenario from a trace's runtime data
	CreateScenarioFromTrace(ctx context.Context, projectName, name, traceID string) (*ScenarioItem, error)
	// CreateEmptyScenario creates an empty scenario for manual population via UpdateScenarioPort
	CreateEmptyScenario(ctx context.Context, projectName, name string) (*ScenarioItem, error)
	// DeleteScenario deletes a scenario by resource name
	DeleteScenario(ctx context.Context, projectName, resourceName string) error
	// ListScenarios lists all scenarios for a project
	ListScenarios(ctx context.Context, projectName string) ([]ScenarioItem, error)
	// UpdateScenarioPort sets or replaces the sample data for a specific port in a scenario
	UpdateScenarioPort(ctx context.Context, projectName, resourceName, port string, data []byte) error
}

// TraceReader provides access to execution traces for observability.
// Both hosted and local implementations talk to the same otel-server
// (hosted: in-cluster direct; local: via kubectl port-forward).
type TraceReader interface {
	// ReadTraces returns recent traces for a project/flow within the given time window
	ReadTraces(ctx context.Context, projectName, flowName string, lookback time.Duration, offset, limit int) ([]TraceSummary, error)
	// ReadTraceDetail returns full span details for a specific trace
	ReadTraceDetail(ctx context.Context, projectName, traceID string) ([]TraceSpanInfo, error)
}

// NodePosition tracks position of a node added during session
type NodePosition struct {
	NodeID   string
	FlowName string
	X        int
	Y        int
}

// PositionTracker tracks node positions during a session to avoid overlap
// when Kubernetes hasn't reconciled yet
type PositionTracker interface {
	// RecordPosition records a node's position
	RecordPosition(flowName, nodeID string, x, y int)
	// GetMaxX returns the maximum X position for nodes in a flow
	GetMaxX(flowName string) int
	// GetNextY returns the next available Y position for a given X column
	// This spreads nodes vertically to avoid stacking at same Y
	GetNextY(flowName string, targetX int, columnWidth int) int
}

// ExecutionContext contains contextual information for tool execution.
// Fields here are the minimum needed for flow-building tools that work
// identically in hosted and local modes. Platform adds its own
// extended context for hosted-only concerns (workspaces, deployments, jobs, etc.).
type ExecutionContext struct {
	// Current project/flow (resource names, not DB IDs)
	ProjectName string
	FlowName    string
	FlowTitle   string // Human-readable flow title, optional

	// Flow state
	ProjectReader ProjectReader
	ProjectLister ProjectLister
	FlowSaver     FlowSaver
	FlowModifier  FlowModifier

	// Discovery
	ModuleCatalog       ModuleCatalog       // installed modules (cluster-scoped)
	PublicModuleCatalog PublicModuleCatalog // catalog of modules available to install
	ModuleInstaller     ModuleInstaller     // platform-mode install path; nil in kubeconfig mode
	ModuleUninstaller   ModuleUninstaller   // platform-mode uninstall path; nil in kubeconfig mode
	PortInspector       PortInspector

	// Flow mutation
	NodeAdder              NodeAdder
	EdgeAdder              EdgeAdder
	EdgeConfigurer         EdgeConfigurer
	NodeSettingsConfigurer NodeSettingsConfigurer
	FlowCreator            FlowCreator
	FlowDeleter            FlowDeleter

	// Execution and observability
	SignalSender SignalSender
	TraceReader  TraceReader

	// Scenarios (TinyScenario CRD)
	ScenarioManager ScenarioManager

	// Solutions catalog (pluggable: DB or public REST)
	SolutionSearcher SolutionSearcher

	// Dashboard
	DashboardReader DashboardReader
	DashboardWriter DashboardWriter

	// Session-scoped utility
	PositionTracker PositionTracker
}

// Tool is the interface that all LLM tools must implement
type Tool interface {
	// Name returns the unique identifier for this tool
	Name() string
	// Description returns a human-readable description of what this tool does
	Description() string
	// Schema returns the JSON Schema for the tool's input parameters
	Schema() map[string]interface{}
	// Execute runs the tool with the given input and returns the result
	Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult
}
