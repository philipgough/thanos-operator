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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ThanosGatewaySpec defines the desired state of ThanosGateway
type ThanosGatewaySpec struct {
	// Replicas is the number of proxy replicas.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +kubebuilder:validation:Required
	Replicas int32 `json:"replicas,omitempty"`
	// LogLevel is the log level for the server.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=info
	LogLevel string `json:"log_level"`
	// HeaderManipulation defines the configuration for header manipulation.
	// Header manipulations happens at a global level and applies to all requests.
	// +kubebuilder:validation:Optional
	HeaderManipulation *HeaderManipulation `json:"headerManipulations,omitempty"`
	// MetricsReadSpec defines the configuration for reading metrics.
	// +kubebuilder:validation:Required
	MetricsReadSpec MetricsReadSpec `json:"metricsReadSpec"`
	// MetricsWriteSpec defines the configuration for writing metrics.
	// +kubebuilder:validation:Required
	MetricsWriteSpec MetricsWriteSpec `json:"metricsWriteSpec"`
}

// HeaderManipulation defines the configuration for header manipulation.
// It enables copying of a header value at request time to another header.
// This process runs before the request is sent to the backend and is matched against any requirements
type HeaderManipulation struct {
	// FromHeader is the header to copy the value from.
	// +kubebuilder:validation:Required
	FromHeader string `json:"fromHeader"`
	// ToHeader is the header to copy the value to.
	// +kubebuilder:validation:Required
	ToHeader string `json:"toHeader"`
}

// HeaderModification defines the configuration for header modification.
// This allows adding and removing headers from the request.
// This process runs before the request is sent to the backend after a route is matched.
type HeaderModification struct {
	// AddHeaders is a list of headers to add to the request.
	// +kubebuilder:validation:Optional
	AddHeaders map[string]string `json:"addHeaders,omitempty"`
	// RemoveHeaders is a list of headers to remove from the request.
	// +kubebuilder:validation:Optional
	RemoveHeaders []string `json:"removeHeaders,omitempty"`
}

// BackendConfig defines the configuration for the backend.
type BackendConfig struct {
	// Address is the address of the backend.
	// +kubebuilder:validation:Required
	Address string `json:"address"`
	// Port is the port of the backend.
	// +kubebuilder:validation:Required
	Port int32 `json:"port"`
}

// MetricsReadSpec defines the configuration for reading metrics.
type MetricsReadSpec struct {
	// BackendConfig defines the configuration for the backend.
	// +kubebuilder:validation:Required
	BackendConfig BackendConfig `json:"backendConfig"`
	// HeaderModification defines the configuration for header modification.
	// +kubebuilder:validation:Optional
	HeaderModification *HeaderModification `json:"headerModification,omitempty"`
}

// MetricsWriteSpec defines the configuration for writing metrics.
type MetricsWriteSpec struct {
	// BackendConfig defines the configuration for the backend.
	// +kubebuilder:validation:Required
	BackendConfig BackendConfig `json:"backendConfig"`
	// HeaderModification defines the configuration for header modification.
	// +kubebuilder:validation:Optional
	HeaderModification *HeaderModification `json:"headerModification,omitempty"`
}

// ThanosGatewayStatus defines the observed state of ThanosGateway
type ThanosGatewayStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ThanosGateway is the Schema for the thanosgateways API
type ThanosGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ThanosGatewaySpec   `json:"spec,omitempty"`
	Status ThanosGatewayStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ThanosGatewayList contains a list of ThanosGateway
type ThanosGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ThanosGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ThanosGateway{}, &ThanosGatewayList{})
}
