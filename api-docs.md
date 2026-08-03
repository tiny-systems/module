# API Reference

## Packages
- [operator.tinysystems.io/v1alpha1](#operatortinysystemsiov1alpha1)


## operator.tinysystems.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the operator v1alpha1 API group

### Resource Types
- [TinyFlow](#tinyflow)
- [TinyFlowList](#tinyflowlist)
- [TinyModule](#tinymodule)
- [TinyModuleList](#tinymodulelist)
- [TinyNode](#tinynode)
- [TinyNodeList](#tinynodelist)
- [TinyProject](#tinyproject)
- [TinyProjectList](#tinyprojectlist)
- [TinyScenario](#tinyscenario)
- [TinyScenarioList](#tinyscenariolist)
- [TinyWidgetPage](#tinywidgetpage)
- [TinyWidgetPageList](#tinywidgetpagelist)



#### EdgeRetryPolicy



EdgeRetryPolicy controls how the scheduler re-dispatches a single
edge on handler failure. Matches Temporal-style ActivityOptions in
spirit, scoped to one edge in a TinyFlow.


On error, the scheduler:
 1. checks if the error's code is in NonRetryableErrorCodes — if so,
    surface immediately, no retry.
 2. otherwise increments the attempt counter and re-dispatches after
    the policy's backoff, up to MaxAttempts total tries.

_Appears in:_
- [TinyNodeEdge](#tinynodeedge)

| Field | Description |
| --- | --- |
| `maxAttempts` _integer_ | Max total dispatch attempts (1 = no retry, the default). |
| `initialDelayMs` _integer_ | Initial backoff between attempts. Default 1s. |
| `backoffCoefficient` _string_ | Multiplier applied to the delay each attempt. Default 2.0<br /><br />(exponential backoff). |
| `maxDelayMs` _integer_ | Cap on a single attempt's delay. Default 30s. |
| `nonRetryableErrorCodes` _string array_ | Error codes that skip retry. Components signal these via<br /><br />module.NonRetryable(code, err) — typically "quota_exceeded",<br /><br />"unauthorized", "content_filter", "validation". The transport<br /><br />stamps `x-error-code` on the reply; the scheduler reads it and<br /><br />short-circuits the retry loop when matched. |
| `timeoutMs` _integer_ | Per-attempt handler timeout in milliseconds. Caps how long a<br /><br />single dispatch attempt can run before the scheduler cancels<br /><br />its context and (if MaxAttempts permits) retries the next one.<br /><br />0 = use the transport's default (currently 5 minutes for the<br /><br />NATS transports). Bump this on edges that legitimately need<br /><br />long-running handlers — agent planning loops, batch LLM calls,<br /><br />or HTTP probes against slow upstreams. |






#### Position

_Underlying type:_ _integer_



_Appears in:_
- [TinyModuleComponentPort](#tinymodulecomponentport)
- [TinyNodePortStatus](#tinynodeportstatus)



#### ScenarioPortData



ScenarioPortData stores the sample data for a single port

_Appears in:_
- [TinyScenarioSpec](#tinyscenariospec)

| Field | Description |
| --- | --- |
| `port` _string_ | Port is the full port name (e.g., "flowid.module.component-suffix:portname") |
| `data` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ | Data is the JSON-encoded sample payload for this port |


#### TinyFlow



TinyFlow is the Schema for the tinyflows API

_Appears in:_
- [TinyFlowList](#tinyflowlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyFlow`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyFlowSpec](#tinyflowspec)_ |  |
| `status` _[TinyFlowStatus](#tinyflowstatus)_ |  |


#### TinyFlowList



TinyFlowList contains a list of TinyFlow



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyFlowList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyFlow](#tinyflow) array_ |  |


#### TinyFlowSpec



TinyFlowSpec defines the desired state of TinyFlow

_Appears in:_
- [TinyFlow](#tinyflow)



#### TinyFlowStatus



TinyFlowStatus defines the observed state of TinyFlow

_Appears in:_
- [TinyFlow](#tinyflow)



#### TinyModule



TinyModule is the Schema for the tinymodules API

_Appears in:_
- [TinyModuleList](#tinymodulelist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyModule`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyModuleSpec](#tinymodulespec)_ |  |
| `status` _[TinyModuleStatus](#tinymodulestatus)_ |  |


#### TinyModuleComponentPort



TinyModuleComponentPort describes a single port on a component as
published by the module operator in TinyModule status. This is the
static, component-level view (independent of any placed TinyNode)
that lets MCP/LLM tooling inspect port schemas before building flows.

_Appears in:_
- [TinyModuleComponentStatus](#tinymodulecomponentstatus)

| Field | Description |
| --- | --- |
| `name` _string_ | Name is the port identifier (e.g. "request", "response", "out"). |
| `label` _string_ | Label is the human-readable port name. |
| `source` _boolean_ | Source is true for output ports, false for input ports. |
| `position` _[Position](#position)_ | Position is the visual port placement hint (Top/Right/Bottom/Left). |
| `schema` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ | Schema is the JSON schema describing the port's data structure. |


#### TinyModuleComponentStatus





_Appears in:_
- [TinyModuleStatus](#tinymodulestatus)

| Field | Description |
| --- | --- |
| `name` _string_ |  |
| `description` _string_ |  |
| `info` _string_ |  |
| `tags` _string array_ |  |
| `ports` _[TinyModuleComponentPort](#tinymodulecomponentport) array_ | Ports carries component-level port metadata (name, direction,<br /><br />JSON schema) so tooling can discover what a component looks like<br /><br />without placing a TinyNode first. |


#### TinyModuleList



TinyModuleList contains a list of TinyModule



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyModuleList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyModule](#tinymodule) array_ |  |


#### TinyModuleSpec



TinyModuleSpec defines the desired state of TinyModule

_Appears in:_
- [TinyModule](#tinymodule)

| Field | Description |
| --- | --- |
| `image` _string_ | Foo is an example field of TinyModule. Edit tinymodule_types.go to remove/update |


#### TinyModuleStatus



TinyModuleStatus defines the observed state of TinyModule

_Appears in:_
- [TinyModule](#tinymodule)

| Field | Description |
| --- | --- |
| `addr` _string_ | INSERT ADDITIONAL STATUS FIELD - define observed state of cluster<br /><br />Important: Run "make" to regenerate code after modifying this file |
| `name` _string_ |  |
| `version` _string_ |  |
| `sdkVersion` _string_ |  |
| `components` _[TinyModuleComponentStatus](#tinymodulecomponentstatus) array_ |  |


#### TinyNode



TinyNode is the Schema for the tinynodes API

_Appears in:_
- [TinyNodeList](#tinynodelist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyNode`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyNodeSpec](#tinynodespec)_ |  |
| `status` _[TinyNodeStatus](#tinynodestatus)_ |  |


#### TinyNodeComponentStatus





_Appears in:_
- [TinyNodeStatus](#tinynodestatus)

| Field | Description |
| --- | --- |
| `description` _string_ |  |
| `info` _string_ |  |
| `tags` _string array_ |  |


#### TinyNodeEdge





_Appears in:_
- [TinyNodeSpec](#tinynodespec)

| Field | Description |
| --- | --- |
| `id` _string_ | Edge id |
| `port` _string_ | Current node's port name<br /><br />Source port |
| `to` _string_ | Other node's full port name |
| `flowID` _string_ |  |
| `retryPolicy` _[EdgeRetryPolicy](#edgeretrypolicy)_ | Retry policy for this edge. Default (MaxAttempts == 0 or 1) =<br /><br />single-shot: the scheduler dispatches once, surface the error.<br /><br />Authors opt into retry per-edge for transient-failure-safe<br /><br />targets (webhooks, idempotent writes). The runtime never silently<br /><br />retries against paid LLM APIs by default — see<br /><br />feedback_no_implicit_retries.md. |


#### TinyNodeList



TinyNodeList contains a list of TinyNode



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyNodeList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyNode](#tinynode) array_ |  |


#### TinyNodeModuleStatus





_Appears in:_
- [TinyNodeStatus](#tinynodestatus)

| Field | Description |
| --- | --- |
| `name` _string_ |  |
| `version` _string_ |  |
| `sdkVersion` _string_ |  |


#### TinyNodePortConfig





_Appears in:_
- [TinyNodeSpec](#tinynodespec)

| Field | Description |
| --- | --- |
| `from` _string_ | Settings depend on a sender |
| `port` _string_ |  |
| `schema` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ | Schema JSON schema of the port |
| `configuration` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ | Configuration JSON data of the port's configuration |
| `flowID` _string_ |  |


#### TinyNodePortStatus





_Appears in:_
- [TinyNodeStatus](#tinynodestatus)

| Field | Description |
| --- | --- |
| `name` _string_ |  |
| `label` _string_ |  |
| `position` _[Position](#position)_ |  |
| `source` _boolean_ |  |
| `schema` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ |  |
| `configuration` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ |  |


#### TinyNodeSpec



TinyNodeSpec defines the desired state of TinyNode

_Appears in:_
- [TinyNode](#tinynode)

| Field | Description |
| --- | --- |
| `module` _string_ | Module name - container image repo + tag |
| `component` _string_ | Component name within a module |
| `ports` _[TinyNodePortConfig](#tinynodeportconfig) array_ | Port configurations |
| `edges` _[TinyNodeEdge](#tinynodeedge) array_ | Edges to send message next |


#### TinyNodeStatus



TinyNodeStatus defines the observed state of TinyNode

_Appears in:_
- [TinyNode](#tinynode)

| Field | Description |
| --- | --- |
| `observedGeneration` _integer_ | ObservedGeneration is the most recent generation observed by the controller.<br /><br />It corresponds to metadata.generation, which is updated on mutation by the API Server. |
| `module` _[TinyNodeModuleStatus](#tinynodemodulestatus)_ |  |
| `component` _[TinyNodeComponentStatus](#tinynodecomponentstatus)_ |  |
| `ports` _[TinyNodePortStatus](#tinynodeportstatus) array_ |  |
| `status` _string_ |  |
| `metadata` _object (keys:string, values:string)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `error` _boolean_ |  |
| `lastUpdateTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#time-v1-meta)_ |  |


#### TinyProject



TinyProject is the Schema for the tinyprojects API

_Appears in:_
- [TinyProjectList](#tinyprojectlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyProject`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyProjectSpec](#tinyprojectspec)_ |  |
| `status` _[TinyProjectStatus](#tinyprojectstatus)_ |  |


#### TinyProjectList



TinyProjectList contains a list of TinyProject



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyProjectList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyProject](#tinyproject) array_ |  |


#### TinyProjectSpec



TinyProjectSpec defines the desired state of TinyProject

_Appears in:_
- [TinyProject](#tinyproject)

| Field | Description |
| --- | --- |
| `description` _string_ | Description is a markdown description of the project |


#### TinyProjectStatus



TinyProjectStatus defines the observed state of TinyProject

_Appears in:_
- [TinyProject](#tinyproject)



#### TinyScenario



TinyScenario is the Schema for the tinyscenarios API

_Appears in:_
- [TinyScenarioList](#tinyscenariolist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyScenario`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyScenarioSpec](#tinyscenariospec)_ |  |
| `status` _[TinyScenarioStatus](#tinyscenariostatus)_ |  |


#### TinyScenarioList



TinyScenarioList contains a list of TinyScenario



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyScenarioList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyScenario](#tinyscenario) array_ |  |


#### TinyScenarioSpec



TinyScenarioSpec defines the desired state of TinyScenario

_Appears in:_
- [TinyScenario](#tinyscenario)

| Field | Description |
| --- | --- |
| `ports` _[ScenarioPortData](#scenarioportdata) array_ | Ports contains per-port sample data entries |


#### TinyScenarioStatus



TinyScenarioStatus defines the observed state of TinyScenario

_Appears in:_
- [TinyScenario](#tinyscenario)



#### TinyWidget





_Appears in:_
- [TinyWidgetPageSpec](#tinywidgetpagespec)

| Field | Description |
| --- | --- |
| `port` _string_ |  |
| `name` _string_ |  |
| `schemaPatch` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ |  |
| `gridX` _integer_ |  |
| `gridY` _integer_ |  |
| `gridW` _integer_ |  |
| `gridH` _integer_ |  |


#### TinyWidgetPage



TinyWidgetPage is the Schema for the tinywidgetpages API

_Appears in:_
- [TinyWidgetPageList](#tinywidgetpagelist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyWidgetPage`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyWidgetPageSpec](#tinywidgetpagespec)_ |  |
| `status` _[TinyWidgetPageStatus](#tinywidgetpagestatus)_ |  |


#### TinyWidgetPageList



TinyWidgetPageList contains a list of TinyWidgetPage



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyWidgetPageList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyWidgetPage](#tinywidgetpage) array_ |  |


#### TinyWidgetPageSpec



TinyWidgetPageSpec defines the desired state of TinyWidgetPage

_Appears in:_
- [TinyWidgetPage](#tinywidgetpage)

| Field | Description |
| --- | --- |
| `widgets` _[TinyWidget](#tinywidget) array_ | Foo is an example field of TinyWidgetPage. Edit tinywidgetpage_types.go to remove/update |


#### TinyWidgetPageStatus



TinyWidgetPageStatus defines the observed state of TinyWidgetPage

_Appears in:_
- [TinyWidgetPage](#tinywidgetpage)



