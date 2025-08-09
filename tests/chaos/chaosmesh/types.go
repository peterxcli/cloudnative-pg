/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package chaosmesh provides integration with Chaos Mesh for chaos testing
package chaosmesh

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion for Chaos Mesh API
var (
	GroupVersion = schema.GroupVersion{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
	}
	SchemeGroupVersion = GroupVersion
)

// Resource names
const (
	ResourcePodChaos     = "podchaos"
	ResourceNetworkChaos = "networkchaos"
	ResourceIOChaos      = "iochaos"
	ResourceStressChaos  = "stresschaos"
	ResourceTimeChaos    = "timechaos"
)

// PodChaos represents a Chaos Mesh PodChaos resource
type PodChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PodChaosSpec   `json:"spec"`
	Status            PodChaosStatus `json:"status,omitempty"`
}

// PodChaosSpec defines the specification for PodChaos
type PodChaosSpec struct {
	// Action defines the chaos action
	Action PodChaosAction `json:"action"`
	// Mode defines the mode to select pods
	Mode SelectorMode `json:"mode"`
	// Value is the value for the mode
	Value string `json:"value,omitempty"`
	// Selector defines how to select pods
	Selector PodSelectorSpec `json:"selector"`
	// Duration defines how long the chaos lasts
	Duration *string `json:"duration,omitempty"`
	// GracePeriod for pod termination
	GracePeriod int64 `json:"gracePeriod,omitempty"`
}

// PodChaosStatus represents the status of PodChaos
type PodChaosStatus struct {
	// Phase represents the phase of chaos
	Phase string `json:"phase,omitempty"`
	// FailedMessage when chaos failed
	FailedMessage string `json:"failedMessage,omitempty"`
}

// PodChaosAction defines actions for PodChaos
type PodChaosAction string

const (
	// PodKillAction kills pods
	PodKillAction PodChaosAction = "pod-kill"
	// PodFailureAction injects failures into pods
	PodFailureAction PodChaosAction = "pod-failure"
	// ContainerKillAction kills containers
	ContainerKillAction PodChaosAction = "container-kill"
)

// NetworkChaos represents network chaos experiments
type NetworkChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NetworkChaosSpec   `json:"spec"`
	Status            NetworkChaosStatus `json:"status,omitempty"`
}

// NetworkChaosSpec defines network chaos specification
type NetworkChaosSpec struct {
	// Action defines the network chaos action
	Action NetworkChaosAction `json:"action"`
	// Mode defines the mode to select pods
	Mode SelectorMode `json:"mode"`
	// Value for the mode
	Value string `json:"value,omitempty"`
	// Selector defines how to select pods
	Selector PodSelectorSpec `json:"selector"`
	// Duration of the chaos
	Duration *string `json:"duration,omitempty"`
	// TcParameter for traffic control
	TcParameter *TcParameter `json:"tc,omitempty"`
	// Direction of network traffic
	Direction Direction `json:"direction,omitempty"`
	// Target defines the network target
	Target *PodSelectorSpec `json:"target,omitempty"`
}

// NetworkChaosStatus represents the status
type NetworkChaosStatus struct {
	Phase         string `json:"phase,omitempty"`
	FailedMessage string `json:"failedMessage,omitempty"`
}

// NetworkChaosAction defines network chaos actions
type NetworkChaosAction string

const (
	// NetworkDelayAction adds network delay
	NetworkDelayAction NetworkChaosAction = "delay"
	// NetworkLossAction causes packet loss
	NetworkLossAction NetworkChaosAction = "loss"
	// NetworkDuplicateAction duplicates packets
	NetworkDuplicateAction NetworkChaosAction = "duplicate"
	// NetworkCorruptAction corrupts packets
	NetworkCorruptAction NetworkChaosAction = "corrupt"
	// NetworkPartitionAction creates network partition
	NetworkPartitionAction NetworkChaosAction = "partition"
)

// Direction represents traffic direction
type Direction string

const (
	// To affects outgoing traffic
	To Direction = "to"
	// From affects incoming traffic
	From Direction = "from"
	// Both affects both directions
	Both Direction = "both"
)

// TcParameter defines traffic control parameters
type TcParameter struct {
	// Delay configuration
	Delay *DelaySpec `json:"delay,omitempty"`
	// Loss configuration
	Loss *LossSpec `json:"loss,omitempty"`
}

// DelaySpec defines delay parameters
type DelaySpec struct {
	// Latency is the delay time
	Latency string `json:"latency"`
	// Jitter is the jitter of latency
	Jitter string `json:"jitter,omitempty"`
	// Correlation is the correlation of latency
	Correlation string `json:"correlation,omitempty"`
}

// LossSpec defines packet loss parameters
type LossSpec struct {
	// Loss percentage
	Loss string `json:"loss"`
	// Correlation
	Correlation string `json:"correlation,omitempty"`
}

// IOChaos represents I/O chaos experiments
type IOChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              IOChaosSpec   `json:"spec"`
	Status            IOChaosStatus `json:"status,omitempty"`
}

// IOChaosSpec defines IO chaos specification
type IOChaosSpec struct {
	// Action defines the IO chaos action
	Action IOChaosAction `json:"action"`
	// Mode defines the mode to select pods
	Mode SelectorMode `json:"mode"`
	// Value for the mode
	Value string `json:"value,omitempty"`
	// Selector defines how to select pods
	Selector PodSelectorSpec `json:"selector"`
	// Duration of the chaos
	Duration *string `json:"duration,omitempty"`
	// Delay defines the IO delay
	Delay string `json:"delay,omitempty"`
	// Path defines the path to inject failures
	Path string `json:"path,omitempty"`
	// Percent defines the percentage of IO operations to inject
	Percent int `json:"percent,omitempty"`
	// Methods defines the IO methods to inject
	Methods []string `json:"methods,omitempty"`
}

// IOChaosStatus represents the status
type IOChaosStatus struct {
	Phase         string `json:"phase,omitempty"`
	FailedMessage string `json:"failedMessage,omitempty"`
}

// IOChaosAction defines IO chaos actions
type IOChaosAction string

const (
	// IODelayAction adds IO delay
	IODelayAction IOChaosAction = "delay"
	// IOFaultAction injects IO faults
	IOFaultAction IOChaosAction = "fault"
	// IOAttrOverrideAction overrides file attributes
	IOAttrOverrideAction IOChaosAction = "attrOverride"
)

// StressChaos represents stress chaos experiments
type StressChaos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StressChaosSpec   `json:"spec"`
	Status            StressChaosStatus `json:"status,omitempty"`
}

// StressChaosSpec defines stress chaos specification
type StressChaosSpec struct {
	// Mode defines the mode to select pods
	Mode SelectorMode `json:"mode"`
	// Value for the mode
	Value string `json:"value,omitempty"`
	// Selector defines how to select pods
	Selector PodSelectorSpec `json:"selector"`
	// Duration of the chaos
	Duration *string `json:"duration,omitempty"`
	// Stressors defines the stress load
	Stressors *Stressors `json:"stressors,omitempty"`
}

// StressChaosStatus represents the status
type StressChaosStatus struct {
	Phase         string `json:"phase,omitempty"`
	FailedMessage string `json:"failedMessage,omitempty"`
}

// Stressors defines stress parameters
type Stressors struct {
	// CPUStressor for CPU stress
	CPU *CPUStressor `json:"cpu,omitempty"`
	// MemoryStressor for memory stress
	Memory *MemoryStressor `json:"memory,omitempty"`
}

// CPUStressor defines CPU stress parameters
type CPUStressor struct {
	// Workers is the number of CPU workers
	Workers int `json:"workers"`
	// Load is the CPU load percentage (0-100)
	Load int `json:"load,omitempty"`
}

// MemoryStressor defines memory stress parameters
type MemoryStressor struct {
	// Workers is the number of memory workers
	Workers int `json:"workers"`
	// Size is the size of memory to stress
	Size string `json:"size,omitempty"`
}

// PodSelectorSpec defines how to select pods
type PodSelectorSpec struct {
	// Namespaces is the namespace list
	Namespaces []string `json:"namespaces,omitempty"`
	// LabelSelectors is the label selector
	LabelSelectors map[string]string `json:"labelSelectors,omitempty"`
	// FieldSelectors is the field selector
	FieldSelectors map[string]string `json:"fieldSelectors,omitempty"`
	// PodPhaseSelectors is the pod phase list
	PodPhaseSelectors []string `json:"podPhaseSelectors,omitempty"`
	// NodeSelectors is the node selector
	NodeSelectors map[string]string `json:"nodeSelectors,omitempty"`
	// Nodes is the node list
	Nodes []string `json:"nodes,omitempty"`
	// Pods is a map of pod names
	Pods map[string][]string `json:"pods,omitempty"`
}

// SelectorMode defines the mode to select resources
type SelectorMode string

const (
	// OneMode selects one resource
	OneMode SelectorMode = "one"
	// AllMode selects all resources
	AllMode SelectorMode = "all"
	// FixedMode selects a fixed number of resources
	FixedMode SelectorMode = "fixed"
	// FixedPercentMode selects a fixed percentage of resources
	FixedPercentMode SelectorMode = "fixed-percent"
	// RandomMaxPercentMode selects a random percentage of resources
	RandomMaxPercentMode SelectorMode = "random-max-percent"
)

// Helper functions

// GetDuration returns the duration as time.Duration
func GetDuration(duration *string) (time.Duration, error) {
	if duration == nil || *duration == "" {
		return 0, nil
	}
	return time.ParseDuration(*duration)
}

// SetDuration sets the duration from time.Duration
func SetDuration(d time.Duration) *string {
	s := d.String()
	return &s
}

// DeepCopyInto is required for Kubernetes resources
func (in *PodChaos) DeepCopyInto(out *PodChaos) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy creates a deep copy
func (in *PodChaos) DeepCopy() *PodChaos {
	if in == nil {
		return nil
	}
	out := new(PodChaos)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a deep copy as runtime.Object
func (in *PodChaos) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto for PodChaosSpec
func (in *PodChaosSpec) DeepCopyInto(out *PodChaosSpec) {
	*out = *in
	if in.Duration != nil {
		in, out := &in.Duration, &out.Duration
		*out = new(string)
		**out = **in
	}
	in.Selector.DeepCopyInto(&out.Selector)
}

// DeepCopyInto for PodSelectorSpec
func (in *PodSelectorSpec) DeepCopyInto(out *PodSelectorSpec) {
	*out = *in
	if in.Namespaces != nil {
		in, out := &in.Namespaces, &out.Namespaces
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.LabelSelectors != nil {
		in, out := &in.LabelSelectors, &out.LabelSelectors
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}