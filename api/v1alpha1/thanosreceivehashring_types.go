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
	"encoding/json"

	"github.com/prometheus/prometheus/model/labels"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConfigMapTarget struct {
	// Name is the name of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Key is the key of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// ThanosReceiveHashringSpec defines the desired state of ThanosReceiveHashring
type ThanosReceiveHashringSpec struct {
	// Replicas is the number of replicas/members of the hashring to add to the Thanos Receive StatefulSet.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Required
	Replicas *int32 `json:"replicas,omitempty"`
	// Retention is the duration for which the Thanos Receive StatefulSet will retain data.
	// +kubebuilder:default="2h"
	// +kubebuilder:validation:Required
	Retention Duration `json:"retention,omitempty"`
	// ObjectStorageConfig is the secret's key that contains the object storage configuration.
	// The secret needs to be in the same namespace as the ReceiveHashring object.
	// +kubebuilder:validation:Required
	ObjectStorageConfig corev1.SecretKeySelector `json:"objectStorageConfig"`
	// StorageSize is the size of the storage to be used by the Thanos Receive StatefulSet.
	// If not set, ephemeral storage will be used.
	// +kubebuilder:validation:Optional
	StorageSize *string `json:"storageSize,omitempty"`
	// OutputHashringConfig is the ConfigMap's key that contains the hashring configuration.
	// The ConfigMap will be created in the same namespace as the ThanosReceiveHashring object.
	// If not set, the hashring will not be configured.
	// +kubebuilder:validation:Optional
	OutputHashringConfig *ConfigMapTarget `json:"outputHashringConfig,omitempty"`
	// Tenants is a list of tenants that should be matched by the hashring.
	// An empty list matches all tenants.
	// +kubebuilder:validation:Optional
	Tenants []string `json:"tenants,omitempty"`
	// TenantMatcherType is the type of tenant matching to use.
	// +kubebuilder:default:="exact"
	// +kubebuilder:validation:Enum=exact;glob
	TenantMatcherType TenantMatcher `json:"tenantMatcherType,omitempty"`
}

// ThanosReceiveHashringStatus defines the observed state of ThanosReceiveHashring
type ThanosReceiveHashringStatus struct {
	// Conditions represent the latest available observations of the state of the hashring.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ThanosReceiveHashring is the Schema for the thanosreceivehashrings API
type ThanosReceiveHashring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ThanosReceiveHashringSpec   `json:"spec,omitempty"`
	Status ThanosReceiveHashringStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ThanosReceiveHashringList contains a list of ThanosReceiveHashring
type ThanosReceiveHashringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ThanosReceiveHashring `json:"items"`
}

// GetServiceName returns the name of the ThanosReceiveHashring Service.
func (thr *ThanosReceiveHashring) GetServiceName() string {
	return thr.GetName()
}

func init() {
	SchemeBuilder.Register(&ThanosReceiveHashring{}, &ThanosReceiveHashringList{})
}

// Endpoint represents a single logical member of a hashring.
type Endpoint struct {
	// Address is the address of the endpoint.
	Address string `json:"address"`
	// AZ is the availability zone of the endpoint.
	AZ string `json:"az"`
}

// UnmarshalJSON unmarshals the endpoint from JSON.
func (e *Endpoint) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as a string.
	err := json.Unmarshal(data, &e.Address)
	if err == nil {
		return nil
	}

	// If that fails, try to unmarshal as an endpoint object.
	type endpointAlias Endpoint
	var configEndpoint endpointAlias
	err = json.Unmarshal(data, &configEndpoint)
	if err == nil {
		e.Address = configEndpoint.Address
		e.AZ = configEndpoint.AZ
	}
	return err
}

// HashringConfig represents the configuration for a hashring a receiver node knows about.
type HashringConfig struct {
	// Hashring is the name of the hashring.
	Hashring string `json:"hashring,omitempty"`
	// Tenants is a list of tenants that match on this hashring.
	Tenants []string `json:"tenants,omitempty"`
	// TenantMatcherType is the type of tenant matching to use.
	TenantMatcherType TenantMatcher `json:"tenant_matcher_type,omitempty"`
	// Endpoints is a list of endpoints that are part of this hashring.
	Endpoints []Endpoint `json:"endpoints"`
	// Algorithm is the hashing algorithm to use.
	Algorithm HashringAlgorithm `json:"algorithm,omitempty"`
	// ExternalLabels are the external labels to use for this hashring.
	ExternalLabels labels.Labels `json:"external_labels,omitempty"`
}

// TenantMatcher represents the type of tenant matching to use.
type TenantMatcher string

const (
	// TenantMatcherTypeExact matches tenants exactly. This is also the default one.
	TenantMatcherTypeExact TenantMatcher = "exact"
	// TenantMatcherGlob matches tenants using glob patterns.
	TenantMatcherGlob TenantMatcher = "glob"
)

// HashringAlgorithm represents the hashing algorithm to use.
type HashringAlgorithm string

const (
	// AlgorithmKetama is the ketama hashing algorithm.
	AlgorithmKetama HashringAlgorithm = "ketama"
)
