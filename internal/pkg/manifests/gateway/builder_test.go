package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

const (
	envoyImage = "envoyproxy/envoy"
	envoyTag   = "v1.30.6"

	httpbinName  = "httpbin.org"
	httpPort     = 80
	httpbinImage = "kennethreitz/httpbin"
	httpbinTag   = "latest"
)

const (
	readPath  = "/anything"
	writePath = "/anything/else"
)

var (
	pool    *dockertest.Pool
	network *dockertest.Network
)

func TestMain(m *testing.M) {
	var err error
	pool, err = dockertest.NewPool("")
	if err != nil {
		log.Fatalf("could not construct pool: %s", err)
	}

	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("could not connect to docker: %s", err)
	}

	network, err = pool.CreateNetwork("test-network")
	if err != nil {
		log.Fatalf("could not create network: %v", err)
	}

	options := dockertest.RunOptions{
		Name:         httpbinName,
		Hostname:     httpbinName,
		Repository:   httpbinImage,
		Tag:          httpbinTag,
		ExposedPorts: []string{"80"},
		Networks: []*dockertest.Network{
			network,
		},
	}

	resource, err := pool.RunWithOptions(&options, hostConfig)
	if err != nil {
		log.Fatalf("could not start resource: %s", err)
	}

	err = pool.Retry(func() error {
		probe := fmt.Sprintf("http://localhost:%s/", resource.GetPort("80/tcp"))
		resp, err := http.DefaultClient.Get(probe)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status code 200, got %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("could not connect to httpbin: %s", err)
	}

	cleanup := func() {
		pool.Purge(resource)
		if err != nil {
			log.Fatalf("could not purge resource: %s", err)
		}
		network.Close()
	}

	defer cleanup()

	m.Run()
}

func TestOpts_Routing(t *testing.T) {
	writeRegex := "(/anything/else|/anything/other)"
	opts := Options{
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writeRegex,
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s%s", port, readPath))
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}

	resp, err = http.Get(fmt.Sprintf("http://localhost:%s%s", port, writePath))
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}

	resp, err = http.Get(fmt.Sprintf("http://localhost:%s/anything/other", port))
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}

	resp, err = http.Get(fmt.Sprintf("http://localhost:%s/something/else", port))
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status code 404, got %d", resp.StatusCode)
	}
}

func TestOpts_HeaderManipulation(t *testing.T) {
	fromHeader := "X-Some-Test-Header"
	fromHeaderVal := "test"
	toHeader := "X-Thanos-Tenant"
	opts := Options{
		HeaderManipulation: &HeaderManipulationConfig{
			ExternalHeader: fromHeader,
			InternalHeader: toHeader,
		},
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writePath,
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, readPath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}
	req.Header.Add(fromHeader, fromHeaderVal)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}
	respBody := getAnythingResponseBody(t, resp.Body)
	if respBody.Headers[toHeader] != fromHeaderVal {
		t.Fatalf("expected header %s to be %s, got %s", toHeader, fromHeaderVal, respBody.Headers[toHeader])
	}
}

func TestOpts_HeaderModification(t *testing.T) {
	someHeaderToInitiallySend := "X-Some-Test-Header-To-Send"
	someHeaderToInitiallySendVal := "test-send"

	someHeaderToAddAtRouteMatch := "X-Some-Test-Header"
	someHeaderToAddAtRouteMatchVal := "test-add"
	opts := Options{
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
				HeaderModification: HeaderModification{
					AddHeaders: map[string]string{
						someHeaderToAddAtRouteMatch: someHeaderToAddAtRouteMatchVal,
					},
					RemoveHeaders: []string{someHeaderToInitiallySend},
				},
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writePath,
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, readPath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}
	req.Header.Add(someHeaderToInitiallySend, someHeaderToInitiallySendVal)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}
	respBody := getAnythingResponseBody(t, resp.Body)
	_, ok := respBody.Headers[someHeaderToInitiallySend]
	if ok {
		t.Fatalf("expected header %s to be removed", someHeaderToInitiallySend)
	}

	if respBody.Headers[someHeaderToAddAtRouteMatch] != someHeaderToAddAtRouteMatchVal {
		t.Fatalf("expected header %s to be %s, got %s", someHeaderToAddAtRouteMatch, someHeaderToAddAtRouteMatchVal, respBody.Headers[someHeaderToAddAtRouteMatch])
	}
}

func TestOpts_HeaderMatching(t *testing.T) {
	fromHeader := "X-Some-Test-Header"
	fromHeaderVal := "test"
	toHeader := "X-Thanos-Tenant"

	opts := Options{
		HeaderManipulation: &HeaderManipulationConfig{
			ExternalHeader: fromHeader,
			InternalHeader: toHeader,
		},
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writePath,
				HeaderMatcher: &HeaderMatcher{
					Name:  toHeader,
					Regex: "test.*",
				},
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, readPath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status code 404, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}
	req.Header.Add(fromHeader, fromHeaderVal)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}
}

func TestOpts_HeaderTransformMatching(t *testing.T) {
	someHeaderToInitiallySend := "X-Some-Test-Header-To-Send"
	someHeaderToInitiallySendVal := "test-send"
	toHeader := "X-Thanos-Tenant"

	opts := Options{
		HeaderManipulation: &HeaderManipulationConfig{
			ExternalHeader: someHeaderToInitiallySend,
			InternalHeader: toHeader,
		},
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writePath,
				HeaderMatcher: &HeaderMatcher{
					Name:  toHeader,
					Regex: "test.*",
				},
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, readPath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status code 404, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}
	req.Header.Add(someHeaderToInitiallySend, someHeaderToInitiallySendVal)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}
}

func TestOpts_HeaderMatchedAndDroppedUpstream(t *testing.T) {
	someHeaderToInitiallySend := "X-Thanos-Tenant"
	someHeaderToInitiallySendVal := "test-send"

	opts := Options{
		MetricsReadOptions: MetricsReadOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: readPath,
			},
		},
		MetricsWriteOptions: MetricsWriteOptions{
			BackendConfig: Backend{
				Address:         httpbinName,
				Port:            httpPort,
				MatchRouteRegex: writePath,
				HeaderMatcher: &HeaderMatcher{
					Name:  someHeaderToInitiallySend,
					Regex: "test.*",
				},
				HeaderModification: HeaderModification{
					RemoveHeaders: []string{someHeaderToInitiallySend},
				},
			},
		},
	}
	resource := runEnvoy(t, opts.BuildRaw())
	port := resource.GetPort(fmt.Sprintf("%d/tcp", envoyListenerPort))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, readPath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status code 404, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s%s", port, writePath), nil)
	if err != nil {
		t.Fatalf("could not create request: %s", err)
	}
	req.Header.Add(someHeaderToInitiallySend, someHeaderToInitiallySendVal)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("could not get response: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status code 200, got %d", resp.StatusCode)
	}

	respBody := getAnythingResponseBody(t, resp.Body)
	if _, ok := respBody.Headers[someHeaderToInitiallySend]; ok {
		t.Fatalf("expected header %s to be removed", someHeaderToInitiallySend)
	}
}

func getAnythingResponseBody(t *testing.T, closer io.ReadCloser) anythingResponse {
	t.Helper()
	var anyResp anythingResponse
	err := json.NewDecoder(closer).Decode(&anyResp)
	if err != nil {
		t.Fatalf("could not decode response: %s", err)
	}
	return anyResp
}

func runEnvoy(t *testing.T, withConfig string) *dockertest.Resource {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(dir+"/envoy.yaml", []byte(withConfig), 0644)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(withConfig)

	err = os.WriteFile("/tmp/envoy.yaml", []byte(withConfig), 0644)
	if err != nil {
		t.Fatal(err)
	}

	options := dockertest.RunOptions{
		Repository:   envoyImage,
		Tag:          envoyTag,
		Cmd:          []string{"envoy", "-c", "/etc/envoy/envoy.yaml", "--log-level", "debug"},
		ExposedPorts: []string{fmt.Sprintf("%d", envoyAdminPort), fmt.Sprintf("%d", envoyListenerPort)},
		Mounts: []string{
			dir + "/envoy.yaml:/etc/envoy/envoy.yaml",
		},
		Networks: []*dockertest.Network{
			network,
		},
	}

	resource, err := pool.RunWithOptions(&options, hostConfig)
	if err != nil {
		t.Fatalf("could not start resource: %s", err)
	}

	t.Cleanup(func() {
		err := pool.Purge(resource)
		if err != nil {
			t.Fatalf("could not purge resource: %s", err)
		}
	})

	err = pool.Retry(func() error {
		probe := fmt.Sprintf("http://localhost:%s/ready", resource.GetPort(fmt.Sprintf("%d/tcp", envoyAdminPort)))
		resp, err := http.DefaultClient.Get(probe)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status code 200, got %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not connect to envoy: %s", err)
	}
	return resource
}

var hostConfig = func(config *docker.HostConfig) {
	config.AutoRemove = true
	config.RestartPolicy = docker.RestartPolicy{Name: "no"}
}

type anythingResponse struct {
	Args struct {
	} `json:"args"`
	Data  string `json:"data"`
	Files struct {
	} `json:"files"`
	Form struct {
	} `json:"form"`
	Headers map[string]string `json:"headers"`
	JSON    any               `json:"json"`
	Method  string            `json:"method"`
	Origin  string            `json:"origin"`
	URL     string            `json:"url"`
}
