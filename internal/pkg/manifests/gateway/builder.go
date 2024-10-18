package gateway

import (
	"fmt"
	envoyconfigbootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoyconfigclusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoyconfigcorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	envoyconfigmanagerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
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
)

// Options is the configuration for the gateway.
type Options struct {
	manifests.Options
	HeaderManipulation  *HeaderManipulationConfig
	MetricsReadOptions  MetricsReadOptions
	MetricsWriteOptions MetricsWriteOptions
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
}

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

func (hmc HeaderManipulationConfig) toLuaFilter() *envoyconfigmanagerv3.HttpFilter {
	lua := luav3.Lua{
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

// BuildRaw returns raw JSON configuration for envoy proxy or panics if it fails.
func (opts Options) BuildRaw() string {
	var httpFilters []*envoyconfigmanagerv3.HttpFilter
	if opts.HeaderManipulation != nil {
		httpFilters = append(httpFilters, opts.HeaderManipulation.toLuaFilter())
	}

	connManager := buildHTTPConnectionManager(opts, httpFilters)
	pbCM, err := anypb.New(connManager)
	if err != nil {
		panic(err)
	}

	filterChains := []*listenerv3.FilterChain{
		{
			Filters: []*listenerv3.Filter{
				{
					Name: "envoy.filters.network.http_connection_manager",
					ConfigType: &listenerv3.Filter_TypedConfig{
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
			Listeners: []*listenerv3.Listener{
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
	return string(b)
}

// buildClusters returns the envoy clusters for the gateway.
func buildClusters(opts Options) []*envoyconfigclusterv3.Cluster {
	clusters := []*envoyconfigclusterv3.Cluster{
		opts.MetricsReadOptions.BackendConfig.toCluster(metricsReadClusterName),
		opts.MetricsWriteOptions.BackendConfig.toCluster(metricsWriteClusterName),
	}
	return clusters
}

// buildHTTPConnectionManager returns the HTTP connection manager for the gateway.
func buildHTTPConnectionManager(opts Options, httpFilters []*envoyconfigmanagerv3.HttpFilter) *envoyconfigmanagerv3.HttpConnectionManager {
	routerConfig, err := anypb.New(&routerv3.Router{})
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

	routes := []*routev3.Route{
		opts.MetricsReadOptions.BackendConfig.toRoute(metricsReadClusterName),
		opts.MetricsWriteOptions.BackendConfig.toRoute(metricsWriteClusterName),
	}

	return &envoyconfigmanagerv3.HttpConnectionManager{
		CodecType:  envoyconfigmanagerv3.HttpConnectionManager_AUTO,
		StatPrefix: statsPrefix,

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
		HttpFilters: httpFilters,
	}
}

// toCluster returns the envoy cluster for the backend.
func (t Backend) toCluster(name string) *envoyconfigclusterv3.Cluster {
	return buildEnvoyCluster(name, "http", t.Address, t.Port, envoyconfigclusterv3.Cluster_LOGICAL_DNS)
}

// toRoute returns the envoy route for the backend.
func (t Backend) toRoute(cluster string) *routev3.Route {
	var requestHeaderToAdd []*envoyconfigcorev3.HeaderValueOption
	for header, value := range t.HeaderModification.AddHeaders {
		requestHeaderToAdd = append(requestHeaderToAdd, &envoyconfigcorev3.HeaderValueOption{
			Header: &envoyconfigcorev3.HeaderValue{
				Key:   header,
				Value: value,
			},
			AppendAction: envoyconfigcorev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	var headerMatch []*routev3.HeaderMatcher
	if t.HeaderMatcher != nil {
		headerMatch = []*routev3.HeaderMatcher{
			{
				Name: t.HeaderMatcher.Name,
				HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
					StringMatch: &matcher.StringMatcher{
						MatchPattern: &matcher.StringMatcher_SafeRegex{
							SafeRegex: &matcher.RegexMatcher{
								Regex: t.HeaderMatcher.Regex,
							},
						},
					},
				},
			},
		}
	}

	return &routev3.Route{
		Match: &routev3.RouteMatch{
			Headers: headerMatch,
			PathSpecifier: &routev3.RouteMatch_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					Regex: t.MatchRouteRegex,
				},
			},
		},
		Action: &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{Cluster: cluster},
			},
		},
		RequestHeadersToAdd:    requestHeaderToAdd,
		RequestHeadersToRemove: t.HeaderModification.RemoveHeaders,
	}
}

// buildEnvoyListener returns the envoy listener for the gateway.
func buildEnvoyListener(filterChains []*listenerv3.FilterChain) (*listenerv3.Listener, error) {
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
		FilterChains: filterChains,
	}
	return listener, nil
}

// buildEnvoyCluster returns the envoy cluster for the backend.
func buildEnvoyCluster(name string, scheme, address string, port int, discovery envoyconfigclusterv3.Cluster_DiscoveryType) *envoyconfigclusterv3.Cluster {
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
