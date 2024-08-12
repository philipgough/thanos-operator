package gateway

import (
	"fmt"

	"net/url"
	"strconv"

	envoyconfigbootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoyconfigclusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoyconfigcorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	envoyconfigmanagerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	metricsReadClusterName  = "metrics_read"
	metricsWriteClusterName = "metrics_write"

	envoyAdminAddress = "127.0.0.1"
	envoyAdminPort    = 9901

	envoyListenerName    = "http_listener"
	envoyListenerAddress = "0.0.0.0"
	envoyListenerPort    = 8080

	statsPrefix = "ingress_http"
)

type Backend struct {
	Name          string
	URL           url.URL
	TargetCluster string
	RouteMappings []PathSpecifier
	IsHeadlessSvc bool
}

func DefaultMetricsWriteBackend(u url.URL) Backend {
	return Backend{
		Name:          metricsWriteClusterName,
		URL:           u,
		TargetCluster: metricsWriteClusterName,
		RouteMappings: []PathSpecifier{
			{Value: "/api/v1/receive", IsRegex: false},
		},
	}
}

func DefaultMetricsReadBackend(u url.URL) Backend {
	return Backend{
		Name:          metricsReadClusterName,
		URL:           u,
		TargetCluster: metricsReadClusterName,
		RouteMappings: []PathSpecifier{
			{Value: "/api/v1/query", IsRegex: false},
			{Value: "/api/v1/query_range", IsRegex: false},
			{Value: "/api/v1/series", IsRegex: false},
			{Value: "/api/v1/label", IsRegex: false},
			{Value: "/api/v1/labels", IsRegex: false},
			{Value: "/api/v1/query_exemplars", IsRegex: false},
			{Value: "/api/v1/targets", IsRegex: false},
			{Value: "/api/v1/rules", IsRegex: false},
			{Value: "/api/v1/metadata", IsRegex: false},
		},
	}
}

type PathSpecifier struct {
	IsRegex bool
	Value   string
}

// Tenant defines the tenant configuration.
type Tenant struct {
	// HeaderMatcher is the header matcher configuration.
	HeaderMatcher *HeaderMatcher
}

// HeaderMatcher defines the header matcher configuration.
type HeaderMatcher struct {
	// HeaderMatcherKey is the header matcher to match the tenant against.
	HeaderMatcherKey string
	// HeaderMatcherValue is the header matcher value to match the tenant against.
	HeaderMatcherValue string
}

// JWTProvider defines the JWT provider configuration.
type JWTProvider struct {
	// Name of the JWT provider.
	Name string `json:"name"`
	// Issuer URL of the JWT provider.
	Issuer string `json:"issuer"`
	// Audiences of the JWT provider.
	// A list of JWT audiences allowed to access.
	// A JWT containing any of these audiences will be accepted.
	// If not specified, the audiences in JWT will not be checked.
	Audiences []string `json:"audiences"`
	// RemoteJWKsURI is the URL of the JWKs endpoint
	RemoteJWKsURI url.URL `json:"remoteJWKsURI"`
	// LocalJWK is the local JWKs.
	// If provided it is preferred over RemoteJWKsURI.
	LocalJWKs *string `json:"localJWKs"`
}

type HeaderManipulationConfig struct {
	ExternalHeader string
	InternalHeader string
}

// Options defines the options for the gateway builder.
type Options struct {
	Backends           []Backend
	JWTProviders       []JWTProvider
	HeaderManipulation *HeaderManipulationConfig
}

func BuildGatewayConfig(opts Options) (string, error) {
	bootstrap, err := buildEnvoyConfig(opts)
	if err != nil {
		return "", err
	}

	marshalOpts := protojson.MarshalOptions{Indent: "  "}
	b, err := marshalOpts.Marshal(bootstrap)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(b), nil
}

func buildEnvoyConfig(opts Options) (*envoyconfigbootstrapv3.Bootstrap, error) {
	listener, err := buildEnvoyListener(opts)
	if err != nil {
		return nil, err
	}
	bootstrap := &envoyconfigbootstrapv3.Bootstrap{
		Admin: buildEnvoyAdminConfig(),
		StaticResources: &envoyconfigbootstrapv3.Bootstrap_StaticResources{
			Listeners: []*listenerv3.Listener{
				listener,
			},
			Clusters: buildEnvoyClusters(opts),
		},
	}

	return bootstrap, nil
}

func buildEnvoyListener(opts Options) (*listenerv3.Listener, error) {
	var matchers []routeMatcher
	for _, backend := range opts.Backends {
		for _, route := range backend.RouteMappings {
			matchers = append(matchers, routeMatcher{
				routes:    []string{route.Value},
				toCluster: backend.TargetCluster,
			})
		}
	}

	filter, err := buildEnvoyFilter(statsPrefix, opts)
	if err != nil {
		return nil, err
	}

	listener := &listenerv3.Listener{
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
		FilterChains: []*listenerv3.FilterChain{
			{
				Filters: []*listenerv3.Filter{
					filter,
				},
			},
		},
	}
	return listener, nil
}

func buildEnvoyClusters(opts Options) []*envoyconfigclusterv3.Cluster {
	var clusters []*envoyconfigclusterv3.Cluster
	for _, jwtP := range opts.JWTProviders {
		if jwtP.LocalJWKs != nil {
			continue
		}

		buildEnvoyCluster(jwtP.Name, jwtP.RemoteJWKsURI, envoyconfigclusterv3.Cluster_LOGICAL_DNS)
		clusters = append(clusters, buildEnvoyCluster(jwtP.Name, jwtP.RemoteJWKsURI, envoyconfigclusterv3.Cluster_LOGICAL_DNS))
	}

	for _, backend := range opts.Backends {
		discovery := envoyconfigclusterv3.Cluster_LOGICAL_DNS
		if backend.IsHeadlessSvc {
			discovery = envoyconfigclusterv3.Cluster_STRICT_DNS
		}
		clusters = append(clusters, buildEnvoyCluster(backend.TargetCluster, backend.URL, discovery))
	}

	return clusters
}

func buildEnvoyCluster(name string, url url.URL, discovery envoyconfigclusterv3.Cluster_DiscoveryType) *envoyconfigclusterv3.Cluster {
	cluster := &envoyconfigclusterv3.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &envoyconfigclusterv3.Cluster_Type{Type: discovery},
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: &envoyconfigcorev3.Address{
										Address: &envoyconfigcorev3.Address_SocketAddress{
											SocketAddress: &envoyconfigcorev3.SocketAddress{
												Address: url.Hostname(),
												PortSpecifier: &envoyconfigcorev3.SocketAddress_PortValue{
													PortValue: portToUInt32(url),
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
	if url.Scheme == "https" {
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

func buildEnvoyRoutesTo(opts Options) []*routev3.Route {
	var hvOpts []*envoyconfigcorev3.HeaderValueOption

	if opts.HeaderManipulation != nil {
		hvOpts = append(hvOpts, &envoyconfigcorev3.HeaderValueOption{
			Header: &envoyconfigcorev3.HeaderValue{
				Key:   opts.HeaderManipulation.InternalHeader,
				Value: fmt.Sprintf(`%%REQ(%s)%%`, opts.HeaderManipulation.ExternalHeader),
			},
		})
	}

	var routes []*routev3.Route
	for _, backend := range opts.Backends {
		for _, route := range backend.RouteMappings {
			envoyRoute := &routev3.Route{
				RequestHeadersToAdd: hvOpts,
				Action: &routev3.Route_Route{
					Route: &routev3.RouteAction{
						ClusterSpecifier: &routev3.RouteAction_Cluster{Cluster: backend.TargetCluster},
						PrefixRewrite:    "/",
					},
				},
			}
			if route.IsRegex {
				envoyRoute.Match = &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_SafeRegex{
						SafeRegex: &matcher.RegexMatcher{
							Regex: route.Value,
						},
					},
				}
			} else {
				envoyRoute.Match = &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_PathSeparatedPrefix{PathSeparatedPrefix: route.Value},
				}
			}
			routes = append(routes, envoyRoute)
		}
	}

	return routes
}

type routeMatcher struct {
	routes    []string
	toCluster string
}

func buildEnvoyFilter(statPrefix string, opts Options) (*listenerv3.Filter, error) {
	routes := buildEnvoyRoutesTo(opts)

	routerConfig, _ := anypb.New(&routerv3.Router{})
	manager := &envoyconfigmanagerv3.HttpConnectionManager{
		CodecType:  envoyconfigmanagerv3.HttpConnectionManager_AUTO,
		StatPrefix: statPrefix,

		RouteSpecifier: &envoyconfigmanagerv3.HttpConnectionManager_RouteConfig{
			RouteConfig: &routev3.RouteConfiguration{
				Name: "service",
				VirtualHosts: []*routev3.VirtualHost{
					{
						Name:    "service",
						Domains: []string{"*"},
						Routes:  routes,
					},
				},
			},
		},
		HttpFilters: []*envoyconfigmanagerv3.HttpFilter{
			{
				Name:       "http-router",
				ConfigType: &envoyconfigmanagerv3.HttpFilter_TypedConfig{TypedConfig: routerConfig},
			},
		},
	}
	connMgrTypedConf, err := anypb.New(manager)
	if err != nil {
		return nil, err
	}

	return &listenerv3.Filter{
		Name: "envoy.filters.network.http_connection_manager",
		ConfigType: &listenerv3.Filter_TypedConfig{
			TypedConfig: connMgrTypedConf,
		},
	}, nil
}

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
