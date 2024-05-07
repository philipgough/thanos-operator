//go:build !ignore_autogenerated

/*
Copyright 2024.

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
	timex "time"

	v1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CommonThanosFields) DeepCopyInto(out *CommonThanosFields) {
	*out = *in
	if in.Image != nil {
		in, out := &in.Image, &out.Image
		*out = new(string)
		**out = **in
	}
	if in.ImagePullSecrets != nil {
		in, out := &in.ImagePullSecrets, &out.ImagePullSecrets
		*out = make([]v1.LocalObjectReference, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CommonThanosFields.
func (in *CommonThanosFields) DeepCopy() *CommonThanosFields {
	if in == nil {
		return nil
	}
	out := new(CommonThanosFields)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosQuery) DeepCopyInto(out *ThanosQuery) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosQuery.
func (in *ThanosQuery) DeepCopy() *ThanosQuery {
	if in == nil {
		return nil
	}
	out := new(ThanosQuery)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ThanosQuery) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosQueryList) DeepCopyInto(out *ThanosQueryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ThanosQuery, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosQueryList.
func (in *ThanosQueryList) DeepCopy() *ThanosQueryList {
	if in == nil {
		return nil
	}
	out := new(ThanosQueryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ThanosQueryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosQuerySpec) DeepCopyInto(out *ThanosQuerySpec) {
	*out = *in
	in.CommonThanosFields.DeepCopyInto(&out.CommonThanosFields)
	if in.AlertQueryURL != nil {
		in, out := &in.AlertQueryURL, &out.AlertQueryURL
		*out = new(string)
		**out = **in
	}
	if in.Endpoint != nil {
		in, out := &in.Endpoint, &out.Endpoint
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.EndpointGroup != nil {
		in, out := &in.EndpointGroup, &out.EndpointGroup
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.EndpointStrict != nil {
		in, out := &in.EndpointStrict, &out.EndpointStrict
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.EndpointGroupStrict != nil {
		in, out := &in.EndpointGroupStrict, &out.EndpointGroupStrict
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ActiveQueryPath != nil {
		in, out := &in.ActiveQueryPath, &out.ActiveQueryPath
		*out = new(string)
		**out = **in
	}
	if in.DefaultEvaluationInterval != nil {
		in, out := &in.DefaultEvaluationInterval, &out.DefaultEvaluationInterval
		*out = new(timex.Duration)
		**out = **in
	}
	if in.DefaultStep != nil {
		in, out := &in.DefaultStep, &out.DefaultStep
		*out = new(timex.Duration)
		**out = **in
	}
	if in.LookbackDelta != nil {
		in, out := &in.LookbackDelta, &out.LookbackDelta
		*out = new(timex.Duration)
		**out = **in
	}
	if in.ReplicaLabels != nil {
		in, out := &in.ReplicaLabels, &out.ReplicaLabels
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosQuerySpec.
func (in *ThanosQuerySpec) DeepCopy() *ThanosQuerySpec {
	if in == nil {
		return nil
	}
	out := new(ThanosQuerySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosQueryStatus) DeepCopyInto(out *ThanosQueryStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosQueryStatus.
func (in *ThanosQueryStatus) DeepCopy() *ThanosQueryStatus {
	if in == nil {
		return nil
	}
	out := new(ThanosQueryStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosService) DeepCopyInto(out *ThanosService) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosService.
func (in *ThanosService) DeepCopy() *ThanosService {
	if in == nil {
		return nil
	}
	out := new(ThanosService)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ThanosService) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosServiceList) DeepCopyInto(out *ThanosServiceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ThanosService, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosServiceList.
func (in *ThanosServiceList) DeepCopy() *ThanosServiceList {
	if in == nil {
		return nil
	}
	out := new(ThanosServiceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ThanosServiceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosServiceSpec) DeepCopyInto(out *ThanosServiceSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosServiceSpec.
func (in *ThanosServiceSpec) DeepCopy() *ThanosServiceSpec {
	if in == nil {
		return nil
	}
	out := new(ThanosServiceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ThanosServiceStatus) DeepCopyInto(out *ThanosServiceStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ThanosServiceStatus.
func (in *ThanosServiceStatus) DeepCopy() *ThanosServiceStatus {
	if in == nil {
		return nil
	}
	out := new(ThanosServiceStatus)
	in.DeepCopyInto(out)
	return out
}
