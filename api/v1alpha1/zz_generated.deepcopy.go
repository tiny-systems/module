//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyModule) DeepCopyInto(out *TinyModule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyModule.
func (in *TinyModule) DeepCopy() *TinyModule {
	if in == nil {
		return nil
	}
	out := new(TinyModule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyModule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyModuleList) DeepCopyInto(out *TinyModuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TinyModule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyModuleList.
func (in *TinyModuleList) DeepCopy() *TinyModuleList {
	if in == nil {
		return nil
	}
	out := new(TinyModuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyModuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyModuleSpec) DeepCopyInto(out *TinyModuleSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyModuleSpec.
func (in *TinyModuleSpec) DeepCopy() *TinyModuleSpec {
	if in == nil {
		return nil
	}
	out := new(TinyModuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyModuleStatus) DeepCopyInto(out *TinyModuleStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyModuleStatus.
func (in *TinyModuleStatus) DeepCopy() *TinyModuleStatus {
	if in == nil {
		return nil
	}
	out := new(TinyModuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNode) DeepCopyInto(out *TinyNode) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNode.
func (in *TinyNode) DeepCopy() *TinyNode {
	if in == nil {
		return nil
	}
	out := new(TinyNode)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyNode) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeComponentStatus) DeepCopyInto(out *TinyNodeComponentStatus) {
	*out = *in
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeComponentStatus.
func (in *TinyNodeComponentStatus) DeepCopy() *TinyNodeComponentStatus {
	if in == nil {
		return nil
	}
	out := new(TinyNodeComponentStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeEdge) DeepCopyInto(out *TinyNodeEdge) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeEdge.
func (in *TinyNodeEdge) DeepCopy() *TinyNodeEdge {
	if in == nil {
		return nil
	}
	out := new(TinyNodeEdge)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeList) DeepCopyInto(out *TinyNodeList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TinyNode, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeList.
func (in *TinyNodeList) DeepCopy() *TinyNodeList {
	if in == nil {
		return nil
	}
	out := new(TinyNodeList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyNodeList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeModuleStatus) DeepCopyInto(out *TinyNodeModuleStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeModuleStatus.
func (in *TinyNodeModuleStatus) DeepCopy() *TinyNodeModuleStatus {
	if in == nil {
		return nil
	}
	out := new(TinyNodeModuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodePortConfig) DeepCopyInto(out *TinyNodePortConfig) {
	*out = *in
	if in.Schema != nil {
		in, out := &in.Schema, &out.Schema
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
	if in.Configuration != nil {
		in, out := &in.Configuration, &out.Configuration
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodePortConfig.
func (in *TinyNodePortConfig) DeepCopy() *TinyNodePortConfig {
	if in == nil {
		return nil
	}
	out := new(TinyNodePortConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodePortStatus) DeepCopyInto(out *TinyNodePortStatus) {
	*out = *in
	if in.Schema != nil {
		in, out := &in.Schema, &out.Schema
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
	if in.Configuration != nil {
		in, out := &in.Configuration, &out.Configuration
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodePortStatus.
func (in *TinyNodePortStatus) DeepCopy() *TinyNodePortStatus {
	if in == nil {
		return nil
	}
	out := new(TinyNodePortStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeSpec) DeepCopyInto(out *TinyNodeSpec) {
	*out = *in
	if in.Ports != nil {
		in, out := &in.Ports, &out.Ports
		*out = make([]TinyNodePortConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Edges != nil {
		in, out := &in.Edges, &out.Edges
		*out = make([]TinyNodeEdge, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeSpec.
func (in *TinyNodeSpec) DeepCopy() *TinyNodeSpec {
	if in == nil {
		return nil
	}
	out := new(TinyNodeSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyNodeStatus) DeepCopyInto(out *TinyNodeStatus) {
	*out = *in
	out.Module = in.Module
	in.Component.DeepCopyInto(&out.Component)
	if in.Ports != nil {
		in, out := &in.Ports, &out.Ports
		*out = make([]TinyNodePortStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyNodeStatus.
func (in *TinyNodeStatus) DeepCopy() *TinyNodeStatus {
	if in == nil {
		return nil
	}
	out := new(TinyNodeStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinySignal) DeepCopyInto(out *TinySignal) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinySignal.
func (in *TinySignal) DeepCopy() *TinySignal {
	if in == nil {
		return nil
	}
	out := new(TinySignal)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinySignal) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinySignalList) DeepCopyInto(out *TinySignalList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TinySignal, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinySignalList.
func (in *TinySignalList) DeepCopy() *TinySignalList {
	if in == nil {
		return nil
	}
	out := new(TinySignalList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinySignalList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinySignalSpec) DeepCopyInto(out *TinySignalSpec) {
	*out = *in
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinySignalSpec.
func (in *TinySignalSpec) DeepCopy() *TinySignalSpec {
	if in == nil {
		return nil
	}
	out := new(TinySignalSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinySignalStatus) DeepCopyInto(out *TinySignalStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinySignalStatus.
func (in *TinySignalStatus) DeepCopy() *TinySignalStatus {
	if in == nil {
		return nil
	}
	out := new(TinySignalStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyTracker) DeepCopyInto(out *TinyTracker) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyTracker.
func (in *TinyTracker) DeepCopy() *TinyTracker {
	if in == nil {
		return nil
	}
	out := new(TinyTracker)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyTracker) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyTrackerList) DeepCopyInto(out *TinyTrackerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TinyTracker, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyTrackerList.
func (in *TinyTrackerList) DeepCopy() *TinyTrackerList {
	if in == nil {
		return nil
	}
	out := new(TinyTrackerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TinyTrackerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyTrackerSpec) DeepCopyInto(out *TinyTrackerSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyTrackerSpec.
func (in *TinyTrackerSpec) DeepCopy() *TinyTrackerSpec {
	if in == nil {
		return nil
	}
	out := new(TinyTrackerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TinyTrackerStatus) DeepCopyInto(out *TinyTrackerStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TinyTrackerStatus.
func (in *TinyTrackerStatus) DeepCopy() *TinyTrackerStatus {
	if in == nil {
		return nil
	}
	out := new(TinyTrackerStatus)
	in.DeepCopyInto(out)
	return out
}
