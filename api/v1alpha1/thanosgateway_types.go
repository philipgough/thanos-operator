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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ThanosGatewaySpec defines the desired state of ThanosGateway
type ThanosGatewaySpec struct {
	// CommonFields are the options available to all Thanos components.
	CommonFields `json:",inline"`
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
	// When a resource is paused, no actions except for deletion
	// will be performed on the underlying objects.
	// +kubebuilder:validation:Optional
	Paused *bool `json:"paused,omitempty"`
	// Additional configuration for the component. Allows you to add
	// additional args, containers, volumes, and volume mounts to Deployment.
	// +kubebuilder:validation:Optional
	Additional `json:",inline"`
}

// Policy is a list of CEL expressions and matchers.
// If all the CELExpressions evaluate to true the Selectors are injected into the request.
type Policy struct {
	// Name is a human-readable name for the policy.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// CELExpression is a CEL expression that must evaluate to true for the policy to be applied.
	// +kubebuilder:validation:Required
	CELExpression string `json:"expression"`
	// Selectors is a list of matchers to be injected into the request as part of the policy if the CELExpression evaluates to true.
	// +kubebuilder:validation:Required
	Selectors []Selector `json:"selectors"`
}

// Selector is a label selector that will be applied if the ConditionalSelector evaluates to true.
type Selector struct {
	// LabelSelector is the label selector that will be applied if all the selectors in the ConditionalSelector are true.
	// +kubebuilder:validation:Required
	LabelSelector string `json:"label_selector"`
	// ConditionalSelector is a list of selectors that must all evaluate to true for the Selector to be applied.
	// This is optional and if not present the Selector will be applied if the CELExpression evaluates to true.
	// +kubebuilder:validation:Optional
	ConditionalSelector *string `json:"conditional_selector,omitempty"`
}

// Backend defines the configuration for the backend.
type Backend struct {
	// Address is the address of the backend.
	// +kubebuilder:validation:Required
	Address string `json:"address"`
	// Port is the port of the backend.
	// +kubebuilder:validation:Required
	Port int32 `json:"port"`
}

// HeaderMutation represents a mutation to be applied to HTTP headers.
// It contains the header to be set and the value to set it to.
type HeaderMutation struct {
	// SetHeader is the name of the header to be set.
	SetHeader string
	// FromValue is the value to set the header to, implementing the fmt.Stringer interface.
	//FromValue fmt.Stringer
}

// BackendTokenAuthConfig defines the configuration for token authentication.
type BackendTokenAuthConfig struct {
	// EnableTokenReview enables token review.
	// If not specified, token review will not be enabled.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EnableKubernetesTokenReview bool `json:"enableKubernetesTokenReview,omitempty"`
}

// MTLSConfig is the configuration for mTLS.
type MTLSConfig struct {
	// TrustedCA is the path to the trusted CA certificate.
	TrustedCA string `json:"trustedCA"`
	// ServerCert is the path to the server certificate.
	ServerCert string `json:"serverCert"`
	// ServerKey is the path to the server key.
	ServerKey string `json:"serverKey"`
	// MatchSANs is the list of SANs to match.
	// If not specified, the SANs in the server certificate will not be checked.
	MatchSANs []string `json:"matchSANs,omitempty"`
}

// BackendSpec is the configuration for the backend service.
type BackendSpec struct {
	// BackendConfig is the configuration for the backend service.
	BackendConfig Backend `json:"backendConfig"`
	// TokenAuthConfig is the configuration for token authentication.
	// +kubebuilder:validation:Optional
	TokenAuthConfig BackendTokenAuthConfig `json:"tokenAuthConfig"`
	// MTLSConfig is the configuration for mTLS.
	// +kubebuilder:validation:Optional
	MTLSConfig *MTLSConfig `json:"mtlsConfig,omitempty"`
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

// MetricsReadSpec defines the configuration for reading metrics.
type MetricsReadSpec struct {
	// BackendSpec defines the configuration for the backend.
	// +kubebuilder:validation:Required
	BackendSpec BackendSpec `json:"backendSpec"`
	// RBACConfig is the configuration for RBAC.
	// +kubebuilder:validation:Optional`
	RBACConfig map[string]string `json:"rbacConfig,omitempty"`
	// Policies is a list of label based access control policies to be applied to the request.
	// +kubebuilder:validation:Optional
	Policies []Policy `json:"policies,omitempty"`
}

// MetricsWriteSpec defines the configuration for writing metrics.
type MetricsWriteSpec struct {
	// BackendSpec defines the configuration for the backend.
	// +kubebuilder:validation:Required
	BackendSpec BackendSpec `json:"backendSpec"`
}

// TokenAuthConfig defines the configuration for token authentication.
// This allows for the use of one of the following
// 1. Kubernetes TokenReview for authentication.
// 2. JWT token for authentication.
// It is invalid to have both JWT and TokenReview enabled.
type TokenAuthConfig struct {
	// EnableKubernetesTokenReview for authentication.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EnableKubernetesTokenReview bool `json:"enable,omitempty"`
	// JWTProvider enables and configures JWT token for authentication.
	// +kubebuilder:validation:Optional
	JWTProvider *JWTProviderConfig `json:"jwtProvider,omitempty"`
}

// JWTProviderConfig defines the JWT provider configuration.
// JWT will be validated against the JWT provider.
// JWT is extracted from the Authorization header and is expected to be in the form of Bearer token.
type JWTProviderConfig struct {
	// Name of the JWT provider.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Issuer is the principal that issues the JWT.
	// +kubebuilder:validation:Required
	Issuer string `json:"issuer"`
	// Audiences of the JWT provider.
	// A list of JWT audiences allowed to access.
	// A JWT containing any of these audiences will be accepted.
	// If not specified, the audiences in JWT will not be checked.
	Audiences []string `json:"audiences,omitempty"`
	// RemoteJWKsURI is the URL of the JWKs endpoint.
	// +kubebuilder:validation:Optional
	RemoteJWKsURI *string `json:"remoteJWKsURI,omitempty"`
	// LocalJWKS is the local JWKs.
	// If provided, it is preferred over RemoteJWKsURI.
	// +kubebuilder:validation:Optional
	LocalJWKS *v1.ConfigMapKeySelector `json:"localJWKS,omitempty"`
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
