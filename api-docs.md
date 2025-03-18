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
- [TinySignal](#tinysignal)
- [TinySignalList](#tinysignallist)
- [TinyTracker](#tinytracker)
- [TinyTrackerList](#tinytrackerlist)



#### Position

_Underlying type:_ _integer_



_Appears in:_
- [TinyNodePortStatus](#tinynodeportstatus)



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


#### TinyModuleComponentStatus





_Appears in:_
- [TinyModuleStatus](#tinymodulestatus)

| Field | Description |
| --- | --- |
| `name` _string_ |  |
| `description` _string_ |  |
| `info` _string_ |  |
| `tags` _string array_ |  |


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
| `module_version` _string_ | Module version semver v2 compatible (without v prefix) |
| `component` _string_ | Component name within a module |
| `ports` _[TinyNodePortConfig](#tinynodeportconfig) array_ | Port configurations |
| `edges` _[TinyNodeEdge](#tinynodeedge) array_ | Edges to send message next |


#### TinyNodeStatus



TinyNodeStatus defines the observed state of TinyNode

_Appears in:_
- [TinyNode](#tinynode)

| Field | Description |
| --- | --- |
| `module` _[TinyNodeModuleStatus](#tinynodemodulestatus)_ |  |
| `component` _[TinyNodeComponentStatus](#tinynodecomponentstatus)_ |  |
| `ports` _[TinyNodePortStatus](#tinynodeportstatus) array_ |  |
| `status` _string_ |  |
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



#### TinyProjectStatus



TinyProjectStatus defines the observed state of TinyProject

_Appears in:_
- [TinyProject](#tinyproject)



#### TinySignal



TinySignal is the Schema for the tinysignals API

_Appears in:_
- [TinySignalList](#tinysignallist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinySignal`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinySignalSpec](#tinysignalspec)_ |  |
| `status` _[TinySignalStatus](#tinysignalstatus)_ |  |


#### TinySignalList



TinySignalList contains a list of TinySignal



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinySignalList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinySignal](#tinysignal) array_ |  |


#### TinySignalSpec



TinySignalSpec defines the desired state of TinySignal

_Appears in:_
- [TinySignal](#tinysignal)

| Field | Description |
| --- | --- |
| `node` _string_ | Foo is an example field of TinySignal. Edit tinysignal_types.go to remove/update |
| `port` _string_ |  |
| `data` _[byte](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#byte-v1-meta) array_ |  |


#### TinySignalStatus



TinySignalStatus defines the observed state of TinySignal

_Appears in:_
- [TinySignal](#tinysignal)



#### TinyTracker



TinyTracker is the Schema for the tinytrackers API

_Appears in:_
- [TinyTrackerList](#tinytrackerlist)

| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyTracker`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[TinyTrackerSpec](#tinytrackerspec)_ |  |
| `status` _[TinyTrackerStatus](#tinytrackerstatus)_ |  |


#### TinyTrackerList



TinyTrackerList contains a list of TinyTracker



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `operator.tinysystems.io/v1alpha1`
| `kind` _string_ | `TinyTrackerList`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br /><br />Servers may infer this from the endpoint the client submits requests to.<br /><br />Cannot be updated.<br /><br />In CamelCase.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br /><br />Servers should convert recognized schemas to the latest internal value, and<br /><br />may reject unrecognized values.<br /><br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `items` _[TinyTracker](#tinytracker) array_ |  |


#### TinyTrackerSpec



TinyTrackerSpec defines the desired state of Tracker

_Appears in:_
- [TinyTracker](#tinytracker)



#### TinyTrackerStatus



TinyTrackerStatus defines the observed state of TinyTracker

_Appears in:_
- [TinyTracker](#tinytracker)



