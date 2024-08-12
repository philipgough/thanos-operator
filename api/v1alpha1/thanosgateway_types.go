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

// AuthProvidersConfig defines the authentication providers configuration.
type AuthProvidersConfig struct {
	// JWTProviders is the list of JWT providers.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems:=10
	// +listType=map
	// +listMapKey=name
	JWTProviders []JWTProviderConfig `json:"jwtProviders,omitempty"`
	// MTLSConfig is the mTLS configuration.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems:=10
	// +listType=map
	// +listMapKey=name
	MTLSConfigs []MTLSConfig `json:"mTLSConfigs"`
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

// MTLSConfig defines the mTLS configuration.
type MTLSConfig struct {
	// Name of the JWT provider.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// MetricsBackendConfig defines the configuration for the metrics backend.
type MetricsBackendConfig struct {
	// MetricsReadConfig is the configuration for the metrics read backend.
	// +kubebuilder:validation:Required
	MetricsReadConfig BackendConfig `json:"metricsReadConfig"`
	// MetricsWriteConfig is the configuration for the metrics write backend.
	// +kubebuilder:validation:Required
	MetricsWriteConfig BackendConfig `json:"metricsWriteConfig"`
}

// BackendConfig defines the backend configuration.
// One of FQDN or DiscoveryConfig must be provided.
type BackendConfig struct {
	// DiscoveryConfig is the label selector to discover the resource.
	// +kubebuilder:validation:Optional
	DiscoveryConfig *metav1.LabelSelector `json:"labelSelector,omitempty"`
	// FQDN is the FQDN of the backend.
	// +kubebuilder:validation:Optional
	FQDN *string `json:"fqdn,omitempty"`
}

// Tenant defines the tenants configuration.
type Tenant struct {
	// Name of the tenant.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// MetricsReadConfig is the auth config for the metrics read backend.
	// +kubebuilder:validation:Optional
	MetricsReadConfig *AuthConfig `json:"metricsReadConfig,omitempty"`
	// MetricsWriteConfig is the auth config for the metrics write backend.
	// +kubebuilder:validation:Optional
	MetricsWriteConfig *AuthConfig `json:"metricsWriteConfig,omitempty"`
}

// TenantAuthConfig defines the tenant auth configuration for the metrics resource for a specific permission.
type TenantAuthConfig struct {
	// MetricsReadConfig is the auth config for the metrics read backend.
	// +kubebuilder:validation:Optional
	MetricsReadConfig *AuthConfig `json:"metricsReadConfig,omitempty"`
	// MetricsWriteConfig is the auth config for the metrics read backend.
	// +kubebuilder:validation:Optional
	MetricsWriteConfig *AuthConfig `json:"metricsReadConfig,omitempty"`
}

// AuthConfig defines the authentication and authorization configuration for a resource.
type AuthConfig struct {
	// TokenAuthConfig is a map of named JWTProviderConfig that are used to authenticate access to a resource.
	// The value is a list of TokenRBAC which authorizes the principal.
	// Authorization is granted if any of the TokenRBAC matches.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxProperties=5
	TokenAuthConfig map[string][]TokenRBAC `json:"tokenAuthConfig,omitempty"`
	// CertAutConfig is a map of named MTLSConfig that are used to authenticate access to a resource.
	// The value is a list of CertRBAC which authorizes the principal.
	// Authorization is granted if any of the CertRBAC matches.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxProperties=5
	CertAutConfig map[string][]CertRBAC `json:"certAuthConfig,omitempty"`
}

// TokenRBAC defines the token RBAC configuration.
type TokenRBAC struct {
	// Principal is the path on the token to identify the principal.
	// This should be provided as a JSON path seperated by dots.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="sub"
	Principal *string `json:"principal,omitempty"`
	// Matcher is the matcher to use to match the principal.
	// +kubebuilder:validation:Required
	Matcher Matcher `json:"matcher"`
}

// CertRBAC defines the cert RBAC configuration.
type CertRBAC struct {
	// Matcher is the matcher to use to match the principal.
	// In the case of a client certificate, the principal is set in the following precedence order:
	// 1. URI SAN
	// 2. DNS SAN
	// 3. The subject. The Common Name (CN) of the certificate.
	// +kubebuilder:validation:Required
	Matcher Matcher `json:"matcher"`
}

// Matcher defines the matcher configuration.
type Matcher struct {
	// IsRegex is true if the Value is a regular expression.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	IsRegex bool `json:"isRegex,omitempty"`
	// Value is the value to match.
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// ThanosGatewaySpec defines the desired state of ThanosGateway
// +kubebuilder:validation:XValidation:rule="self.tenants.all(tenant, tenant.metricsReadConfig.tokenAuthConfig.all(name, self.authProvidersConfig.jwtProviders.exists(provider, provider.name == name)))", message="jwt provider must exist for read"
// +kubebuilder:validation:XValidation:rule="self.tenants.all(tenant, tenant.metricsReadConfig.certAuthConfig.all(name, self.authProvidersConfig.mTLSConfigs.exists(provider, provider.name == name)))", message="cert provider must exist for read"
// +kubebuilder:validation:XValidation:rule="self.tenants.all(tenant, tenant.metricsWriteConfig.tokenAuthConfig.all(name, self.authProvidersConfig.jwtProviders.exists(provider, provider.name == name)))", message="jwt provider must exist for write"
// +kubebuilder:validation:XValidation:rule="self.tenants.all(tenant, tenant.metricsWriteConfig.certAuthConfig.all(name, self.authProvidersConfig.mTLSConfigs.exists(provider, provider.name == name)))", message="cert provider must exist for write"
type ThanosGatewaySpec struct {
	// GatewayConfig is the gateway configuration.
	// +kubebuilder:validation:Optional
	GatewayConfig *GatewayConfig `json:"gatewayConfig,omitempty"`
	// AuthProvidersConfig is the authentication providers configuration.
	// +kubebuilder:validation:Optional
	AuthProvidersConfig *AuthProvidersConfig `json:"authProvidersConfig,omitempty"`
	// BackendConfig is the configuration for the metrics backend.
	// +kubebuilder:validation:Required
	BackendConfig MetricsBackendConfig `json:"backendConfig"`
	// Tenants is the list of tenants.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Tenants []Tenant `json:"tenants,omitempty"`
}

// GatewayConfig defines the gateway configuration.
type GatewayConfig struct {
	// TenantIdentifierConfig is the tenant identifier configuration.
	// +kubebuilder:validation:Optional
	TenantIdentifierConfig *TenantIdentifierConfig `json:"tenantIdentifierConfig,omitempty"`
}

// TenantIdentifierConfig defines the tenant identifier configuration.
type TenantIdentifierConfig struct {
	// HeaderIdentifierConfig is the header identifier configuration.
	// This is the default configuration.
	// +kubebuilder:validation:Optional
	HeaderIdentifierConfig *HeaderIdentifierConfig `json:"headerIdentifierConfig,omitempty"`
}

// HeaderIdentifierConfig defines the header identifier configuration.
type HeaderIdentifierConfig struct {
	// ExternalHeaderValue is the header to use to identify the tenant.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="X-THANOS-TENANT"
	ExternalHeaderValue *string `json:"externalHeader,omitempty"`
	// InternalHeaderValue is the header to use to identify the tenant internally.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="X-THANOS-TENANT"
	InternalHeaderValue *string `json:"internalHeader,omitempty"`
}

// ThanosGatewayStatus defines the observed state of ThanosGateway
type ThanosGatewayStatus struct{}

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
