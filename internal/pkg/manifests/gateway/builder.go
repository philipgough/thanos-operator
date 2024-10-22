package gateway

import (
	"fmt"
	any2 "github.com/golang/protobuf/ptypes/any"
	"log"
	"net/url"
	"strconv"

	envoyconfigbootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoyconfigclusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoyconfigcorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyendpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoylistenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	envoyroutev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoyjwtauthnv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	envoyluav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	envoyrbacv3filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	envoyrouterv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	envoyconfigmanagerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoymatcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	pbduration "github.com/golang/protobuf/ptypes/duration"
	"github.com/google/cel-go/common"
	ast2 "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/parser"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"
	v1alpha1 "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	"sigs.k8s.io/yaml"
)

const (
	metricsReadClusterName  = "metrics_read"
	metricsWriteClusterName = "metrics_write"

	envoyListenerName    = "http_listener"
	envoyListenerAddress = "0.0.0.0"
	envoyListenerPort    = 8080

	envoyAdminAddress = envoyListenerAddress
	envoyAdminPort    = 9901

	statsPrefix = "ingress_http"

	tokenProviderName = "token_provider"
)

// Options is the configuration for the gateway.
type Options struct {
	manifests.Options
	HeaderManipulation  *HeaderManipulationConfig
	MetricsReadOptions  MetricsReadOptions
	MetricsWriteOptions MetricsWriteOptions
	TokenAuthConfig     *TokenAuthConfig
}

// MetricsReadOptions is the configuration for the metrics read backend.
type MetricsReadOptions struct {
	BackendConfig Backend
}

// MetricsWriteOptions is the configuration for the metrics write backend.
type MetricsWriteOptions struct {
	BackendConfig Backend
}

// Backend is the configuration for a backend.
// MatchRouteRegex is the regex to match the route.
type Backend struct {
	Address            string
	Port               int
	MatchRouteRegex    string
	HeaderModification HeaderModification
	HeaderMatcher      *HeaderMatcher
	TokenAuthConfig    *BackendTokenAuthConfig
}

func (b Backend) toTypedPerRouteFilters() map[string]*any2.Any {
	if b.TokenAuthConfig == nil {
		return nil
	}

	if b.TokenAuthConfig.JWTAuth != nil {

	}

	var matchRoutes []string
	if b.TokenAuthConfig.JWTAuth != nil {
		matchRoutes = append(matchRoutes, b.MatchRouteRegex)
	}

	if b.TokenAuthConfig.KubernetesAuth != nil {
		matchRoutes = append(matchRoutes, b.MatchRouteRegex)
	}

	if len(matchRoutes) == 0 {
		return nil
	}

	return map[string]*any2.Any{
		"envoy.filters.http.jwt_authn": b.TokenAuthConfig.toFilter(matchRoutes),
	}
}

func (bja *BackendJWTAuth) toTypedFilterValue() *any2.Any {
	if bja == nil {
		return nil
	}

	return &envoyconfigmanagerv3.HttpFilter{
		Name: "envoy.filters.http.lua",
		ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{
			TypedConfig: &anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua",
				Value:   luaPB.Value,
			},
		},
	}

	envoyjwtauthnv3.JwtAuthentication{
		Providers:           nil,
		Rules:               nil,
		FilterStateRules:    nil,
		BypassCorsPreflight: false,
		RequirementMap:      nil,
	}

	return nil
}

type BackendTokenAuthConfig struct {
	JWTAuth        *BackendJWTAuth
	KubernetesAuth *BackendKubernetesAuth
}

// BackendJWTAuth is the per-backend configuration for JWT authentication.
type BackendJWTAuth struct {
	// Audiences of the JWT provider.
	// If not specified, the audiences in JWT will not be checked.
	Audiences []string
	// CelTokenRBAC is a list of CEL expressions for RBAC.
	// If any of the expressions evaluate to true, the request is allowed.
	// If not specified, all requests that present a valid token are allowed.
	CelTokenRBAC CelTokenRBAC
}

type BackendKubernetesAuth struct {
	ListenAddress string
	ListenPort    int
	// CelTokenRBAC is a list of CEL expressions for RBAC.
	// If any of the expressions evaluate to true, the request is allowed.
	// If not specified, all requests that present a valid token are allowed.
	CelTokenRBAC CelTokenRBAC
}

// CelTokenRBAC is a list of CEL expressions for RBAC.
// If any of the expressions evaluate to true, the request is allowed.
type CelTokenRBAC []string

// HeaderModification is the configuration for header modification.
type HeaderModification struct {
	AddHeaders    map[string]string
	RemoveHeaders []string
}

// HeaderManipulationConfig is the configuration for header manipulation.
type HeaderManipulationConfig struct {
	ExternalHeader string
	InternalHeader string
}

type TokenAuthConfig struct {
	// JWTProvider is the JWT provider configuration.
	JWTProvider *JWTProvider
	// KubernetesProvider is the configuration for the Kubernetes token provider.
	KubernetesProvider *KubernetesTokenProvider
}

// KubernetesTokenProvider is the configuration for the Kubernetes token provider.
// This is used to authenticate requests to the Kubernetes API server.
// It is the configuration for an envoy ext_authz filter.
type KubernetesTokenProvider struct {
	ListenAddress string
	ListenPort    int
}

// JWTProvider defines the JWT provider configuration.
type JWTProvider struct {
	// Name of the JWT provider.
	Name string
	// Issuer URL of the JWT provider.
	Issuer string
	// Audiences of the JWT provider.
	// A list of JWT audiences allowed to access.
	// A JWT containing any of these audiences will be accepted.
	// If not specified, the audiences in JWT will not be checked.
	Audiences []string
	// RemoteJWKsURI is the URL of the JWKs endpoint
	RemoteJWKsURI RemoteJWKSURI
	// LocalJWK is the local JWKs.
	// If provided it is preferred over RemoteJWKsURI.
	LocalJWKs *string
}

type RemoteJWKSURI struct {
	Scheme string
	URL    string
	Port   int
}

// this could be a header manipulation filter?
func (hmc HeaderManipulationConfig) toLuaFilter() *envoyconfigmanagerv3.HttpFilter {
	lua := envoyluav3.Lua{
		DefaultSourceCode: &envoyconfigcorev3.DataSource{
			Specifier: &envoyconfigcorev3.DataSource_InlineString{
				InlineString: fmt.Sprintf(`
function envoy_on_request(request_handle)
	local headers = request_handle:headers()
	current = headers:get("%s")
	request_handle:headers():add("%s", current)
end
`, hmc.ExternalHeader, hmc.InternalHeader),
			},
		},
	}

	luaPB, err := anypb.New(&lua)
	if err != nil {
		panic(err)
	}

	return &envoyconfigmanagerv3.HttpFilter{
		Name: "envoy.filters.http.lua",
		ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{
			TypedConfig: &anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua",
				Value:   luaPB.Value,
			},
		},
	}
}

type HeaderMatcher struct {
	Name  string
	Regex string
}

func getRBAC() envoyconfigmanagerv3.HttpFilter {
	x := "some.test.replicas > 0"
	p, err := parser.NewParser()
	if err != nil {
		log.Fatalf("parse error: %s", err)
	}
	s := common.NewStringSource(x, "x")
	ast, issues := p.Parse(s)
	if issues != nil && len(issues.GetErrors()) > 0 {
		log.Fatalf("parse error")
	}
	a, err := ast2.ToProto(ast)
	if err != nil {
		log.Fatalf("parse error")
	}

	filter := envoyrbacv3filter.RBAC{
		Rules: &v3.RBAC{
			Action: v3.RBAC_ALLOW,
			Policies: map[string]*v3.Policy{
				"metrics_read": {
					Permissions: []*v3.Permission{
						{
							Rule: &v3.Permission_Any{
								Any: true,
							},
						},
					},
					Principals: []*v3.Principal{
						{
							Identifier: &v3.Principal_Any{
								Any: true,
							},
						},
					},
					Condition: &v1alpha1.Expr{
						Id:       1,
						ExprKind: a.Expr.ExprKind,
					},
				},
			},
		},
	}
	fmt.Println(filter)

	return envoyconfigmanagerv3.HttpFilter{
		Name: "envoy.filters.http.rbac",
		ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{
			TypedConfig: &anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
			},
		},
	}
}

// BuildRawOrDie returns raw JSON configuration for envoy proxy or panics if it fails.
func (opts Options) BuildRawOrDie() string {
	var httpFilters []*envoyconfigmanagerv3.HttpFilter
	if opts.HeaderManipulation != nil {
		httpFilters = append(httpFilters, opts.HeaderManipulation.toLuaFilter())
	}

	if opts.TokenAuthConfig != nil && opts.TokenAuthConfig.JWTProvider != nil {
		var matchRoutes []string
		if opts.MetricsReadOptions.BackendConfig.TokenAuthConfig != nil && opts.MetricsReadOptions.BackendConfig.TokenAuthConfig.JWTAuth != nil {
			matchRoutes = append(matchRoutes, opts.MetricsReadOptions.BackendConfig.MatchRouteRegex)
		}
		if opts.MetricsWriteOptions.BackendConfig.TokenAuthConfig != nil && opts.MetricsWriteOptions.BackendConfig.TokenAuthConfig.JWTAuth != nil {
			matchRoutes = append(matchRoutes, opts.MetricsWriteOptions.BackendConfig.MatchRouteRegex)
		}

		if len(matchRoutes) > 0 {
			httpFilters = append(httpFilters, opts.TokenAuthConfig.toFilter(matchRoutes))
		}
	}

	connManager := buildHTTPConnectionManager(opts, httpFilters)
	pbCM, err := anypb.New(connManager)
	if err != nil {
		panic(err)
	}

	filterChains := []*envoylistenerv3.FilterChain{
		{
			Filters: []*envoylistenerv3.Filter{
				{
					Name: "envoy.filters.network.http_connection_manager",
					ConfigType: &envoylistenerv3.Filter_TypedConfig{
						TypedConfig: pbCM,
					},
				},
			},
		},
	}

	listener, err := buildEnvoyListener(filterChains)
	if err != nil {
		panic(err)
	}

	bootstrap := &envoyconfigbootstrapv3.Bootstrap{
		Admin: buildEnvoyAdminConfig(),
		StaticResources: &envoyconfigbootstrapv3.Bootstrap_StaticResources{
			Listeners: []*envoylistenerv3.Listener{
				listener,
			},
			Clusters: buildClusters(opts),
		},
	}

	marshalOpts := protojson.MarshalOptions{Indent: "  "}
	b, err := marshalOpts.Marshal(bootstrap)
	if err != nil {
		panic(err)

	}
	y, err := yaml.JSONToYAML(b)
	if err != nil {
		panic(err)
	}

	return string(y)
}

// buildClusters returns the envoy clusters for the gateway.
func buildClusters(opts Options) []*envoyconfigclusterv3.Cluster {
	clusters := []*envoyconfigclusterv3.Cluster{
		opts.MetricsReadOptions.BackendConfig.toCluster(metricsReadClusterName),
		opts.MetricsWriteOptions.BackendConfig.toCluster(metricsWriteClusterName),
	}

	if opts.TokenAuthConfig != nil {
		clusters = opts.TokenAuthConfig.JWTProvider.appendCluster(clusters)
		clusters = opts.TokenAuthConfig.KubernetesProvider.appendCluster(clusters)
	}

	return clusters
}

// buildHTTPConnectionManager returns the HTTP connection manager for the gateway.
func buildHTTPConnectionManager(opts Options, httpFilters []*envoyconfigmanagerv3.HttpFilter) *envoyconfigmanagerv3.HttpConnectionManager {
	routerConfig, err := anypb.New(&envoyrouterv3.Router{})
	if err != nil {
		panic(err)
	}

	if len(httpFilters) == 0 {
		httpFilters = []*envoyconfigmanagerv3.HttpFilter{
			{
				Name:       "envoy.filters.http.router",
				ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{TypedConfig: routerConfig},
			},
		}
	} else {
		httpFilters = append(httpFilters, &envoyconfigmanagerv3.HttpFilter{
			Name:       "envoy.filters.http.router",
			ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{TypedConfig: routerConfig},
		})
	}

	routes := []*envoyroutev3.Route{
		opts.MetricsReadOptions.BackendConfig.toRoute(metricsReadClusterName),
		opts.MetricsWriteOptions.BackendConfig.toRoute(metricsWriteClusterName),
	}

	return &envoyconfigmanagerv3.HttpConnectionManager{
		CodecType:  envoyconfigmanagerv3.HttpConnectionManager_AUTO,
		StatPrefix: statsPrefix,

		RouteSpecifier: &envoyconfigmanagerv3.HttpConnectionManager_RouteConfig{
			RouteConfig: &envoyroutev3.RouteConfiguration{
				Name: "service",
				VirtualHosts: []*envoyroutev3.VirtualHost{
					{
						Name:    "service",
						Domains: []string{"*"},
						Routes:  routes,
					},
				},
			},
		},
		HttpFilters: httpFilters,
	}
}

// toCluster returns the envoy cluster for the backend.
func (b Backend) toCluster(name string) *envoyconfigclusterv3.Cluster {
	return buildEnvoyCluster(name, "http", b.Address, b.Port, envoyconfigclusterv3.Cluster_LOGICAL_DNS)
}

func (ktp *KubernetesTokenProvider) appendCluster(to []*envoyconfigclusterv3.Cluster) []*envoyconfigclusterv3.Cluster {
	if ktp == nil {
		return to
	}

	cluster := buildEnvoyCluster(tokenProviderName, "http", ktp.ListenAddress, ktp.ListenPort, envoyconfigclusterv3.Cluster_STRICT_DNS)
	return append(to, cluster)
}

// toCluster returns the envoy cluster for the backend.
func (j *JWTProvider) appendCluster(to []*envoyconfigclusterv3.Cluster) []*envoyconfigclusterv3.Cluster {
	if j == nil {
		return to
	}

	port := int(portToUInt32(j.RemoteJWKsURI))
	host := j.RemoteJWKsURI.Hostname()
	cluster := buildEnvoyCluster(tokenProviderName, j.RemoteJWKsURI.Scheme, host, port, envoyconfigclusterv3.Cluster_STRICT_DNS)
	return append(to, cluster)
}

func (tac *TokenAuthConfig) toFilter(matchRegex []string) *envoyconfigmanagerv3.HttpFilter {
	rt := &envoyjwtauthnv3.RequirementRule_Requires{
		Requires: &envoyjwtauthnv3.JwtRequirement{
			RequiresType: &envoyjwtauthnv3.JwtRequirement_ProviderName{
				ProviderName: tokenProviderName,
			},
		},
	}
	var rules []*envoyjwtauthnv3.RequirementRule
	for _, mr := range matchRegex {
		rules = append(rules, &envoyjwtauthnv3.RequirementRule{
			RequirementType: rt,
			Match: &envoyroutev3.RouteMatch{
				PathSpecifier: &envoyroutev3.RouteMatch_SafeRegex{SafeRegex: &envoymatcher.RegexMatcher{Regex: mr}},
			},
		})
	}

	auth := &envoyjwtauthnv3.JwtAuthentication{
		Providers: map[string]*envoyjwtauthnv3.JwtProvider{
			tokenProviderName: {
				Issuer:    tac.JWTProvider.Issuer,
				Audiences: tac.JWTProvider.Audiences,
				JwksSourceSpecifier: &envoyjwtauthnv3.JwtProvider_RemoteJwks{
					RemoteJwks: &envoyjwtauthnv3.RemoteJwks{
						HttpUri: &envoyconfigcorev3.HttpUri{
							Uri: tac.JWTProvider.RemoteJWKsURI.String(),
							HttpUpstreamType: &envoyconfigcorev3.HttpUri_Cluster{
								Cluster: tokenProviderName,
							},
							Timeout: &pbduration.Duration{
								Seconds: 5,
							},
						},
					},
				},
			},
		},
		Rules: rules,
	}

	authPB, err := anypb.New(auth)
	if err != nil {
		panic(err)
	}

	return &envoyconfigmanagerv3.HttpFilter{
		Name: "envoy.filters.http.jwt_authn",
		ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{
			TypedConfig: &anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.extensions.filters.http.jwt_authn.v3.JwtAuthentication",
				Value:   authPB.Value,
			},
		},
	}
}

// toRoute returns the envoy route for the backend.
func (b Backend) toRoute(cluster string) *envoyroutev3.Route {
	var requestHeaderToAdd []*envoyconfigcorev3.HeaderValueOption
	for header, value := range b.HeaderModification.AddHeaders {
		requestHeaderToAdd = append(requestHeaderToAdd, &envoyconfigcorev3.HeaderValueOption{
			Header: &envoyconfigcorev3.HeaderValue{
				Key:   header,
				Value: value,
			},
			AppendAction: envoyconfigcorev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	var headerMatch []*envoyroutev3.HeaderMatcher
	if b.HeaderMatcher != nil {
		headerMatch = []*envoyroutev3.HeaderMatcher{
			{
				Name: b.HeaderMatcher.Name,
				HeaderMatchSpecifier: &envoyroutev3.HeaderMatcher_StringMatch{
					StringMatch: &envoymatcher.StringMatcher{
						MatchPattern: &envoymatcher.StringMatcher_SafeRegex{
							SafeRegex: &envoymatcher.RegexMatcher{
								Regex: b.HeaderMatcher.Regex,
							},
						},
					},
				},
			},
		}
	}

	return &envoyroutev3.Route{
		Match: &envoyroutev3.RouteMatch{
			Headers: headerMatch,
			PathSpecifier: &envoyroutev3.RouteMatch_SafeRegex{
				SafeRegex: &envoymatcher.RegexMatcher{
					Regex: b.MatchRouteRegex,
				},
			},
		},
		Action: &envoyroutev3.Route_Route{
			Route: &envoyroutev3.RouteAction{
				ClusterSpecifier: &envoyroutev3.RouteAction_Cluster{Cluster: cluster},
			},
		},
		RequestHeadersToAdd:    requestHeaderToAdd,
		RequestHeadersToRemove: b.HeaderModification.RemoveHeaders,
	}
}

// buildEnvoyListener returns the envoy listener for the gateway.
func buildEnvoyListener(filterChains []*envoylistenerv3.FilterChain) (*envoylistenerv3.Listener, error) {
	listener := &envoylistenerv3.Listener{
		Name: envoyListenerName,
		Address: &envoyconfigcorev3.Address{
			Address: &envoyconfigcorev3.Address_SocketAddress{
				SocketAddress: &envoyconfigcorev3.SocketAddress{
					Address: envoyListenerAddress,
					PortSpecifier: &envoyconfigcorev3.SocketAddress_PortValue{
						PortValue: envoyListenerPort,
					},
				},
			},
		},
		FilterChains: filterChains,
	}
	return listener, nil
}

// buildEnvoyCluster returns the envoy cluster for the backend.
func buildEnvoyCluster(name string, scheme, address string, port int, discovery envoyconfigclusterv3.Cluster_DiscoveryType) *envoyconfigclusterv3.Cluster {
	cluster := &envoyconfigclusterv3.Cluster{
		Name:                 name,
		DnsLookupFamily:      envoyconfigclusterv3.Cluster_V4_ONLY,
		ClusterDiscoveryType: &envoyconfigclusterv3.Cluster_Type{Type: discovery},
		LoadAssignment: &envoyendpointv3.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*envoyendpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*envoyendpointv3.LbEndpoint{
						{
							HostIdentifier: &envoyendpointv3.LbEndpoint_Endpoint{
								Endpoint: &envoyendpointv3.Endpoint{
									Address: &envoyconfigcorev3.Address{
										Address: &envoyconfigcorev3.Address_SocketAddress{
											SocketAddress: &envoyconfigcorev3.SocketAddress{
												Address: address,
												PortSpecifier: &envoyconfigcorev3.SocketAddress_PortValue{
													PortValue: uint32(port),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if scheme == "https" {
		cluster.TransportSocket = &envoyconfigcorev3.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &envoyconfigcorev3.TransportSocket_TypedConfig{
				TypedConfig: &anypb.Any{
					TypeUrl: "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
				},
			},
		}
	}
	return cluster
}

// buildEnvoyAdminConfig returns the envoy admin configuration.
func buildEnvoyAdminConfig() *envoyconfigbootstrapv3.Admin {
	admin := &envoyconfigbootstrapv3.Admin{
		Address: &envoyconfigcorev3.Address{
			Address: &envoyconfigcorev3.Address_SocketAddress{
				SocketAddress: &envoyconfigcorev3.SocketAddress{
					Address: envoyAdminAddress,
					PortSpecifier: &envoyconfigcorev3.SocketAddress_PortValue{
						PortValue: envoyAdminPort,
					},
				},
			},
		},
	}
	return admin
}

func portToUInt32(url url.URL) uint32 {
	if url.Port() == "" {
		if url.Scheme == "https" {
			return 443
		}
		if url.Scheme == "http" {
			return 80
		}
	}
	p, _ := strconv.Atoi(url.Port())
	return uint32(p)
}
