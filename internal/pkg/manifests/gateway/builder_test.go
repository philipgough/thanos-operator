package gateway

import (
	"net/url"
	"os"
	"testing"
)

func TestBuildGatewayConfig(t *testing.T) {
	u, err := url.Parse("https://httpbin.org/anything")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	opts := Options{
		Backends: []Backend{
			DefaultMetricsWriteBackend(*u),
			DefaultMetricsReadBackend(*u),
		},
		HeaderManipulation: &HeaderManipulationConfig{
			ExternalHeader: "X-Scope-OrgID",
			InternalHeader: "X-THANOS-TENANT",
		},
	}
	conf, err := BuildGatewayConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	os.WriteFile("/tmp/gateway-config.json", []byte(conf), 0644)
}
