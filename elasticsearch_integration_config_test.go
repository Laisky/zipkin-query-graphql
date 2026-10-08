package zipkin_graphql

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// TestESIntegrationRequiresExactOptIn proves ambient settings cannot enable I/O.
func TestESIntegrationRequiresExactOptIn(t *testing.T) {
	for _, flag := range []string{"", "0", "true", "1 "} {
		t.Run("flag="+flag, func(t *testing.T) {
			_, enabled, err := loadESIntegrationConfig(func(key string) (string, bool) {
				if key != "ZIPKIN_ES_INTEGRATION" {
					t.Fatal("disabled integration read another setting")
				}
				return flag, true
			})
			if err != nil || enabled {
				t.Fatal("integration enabled without exact opt-in")
			}
		})
	}
}

// TestESIntegrationConfigurationRejectsUnsafeInputs validates without printing values.
func TestESIntegrationConfigurationRejectsUnsafeInputs(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing-url", "ZIPKIN_ES_URL", ""},
		{"relative-url", "ZIPKIN_ES_URL", "/relative"},
		{"wrong-scheme", "ZIPKIN_ES_URL", "file:///unused"},
		{"inline-credentials", "ZIPKIN_ES_URL", "http://synthetic-user:synthetic-password@127.0.0.1:1"},
		{"query", "ZIPKIN_ES_URL", "http://127.0.0.1:1/?token=synthetic"},
		{"fragment", "ZIPKIN_ES_URL", "http://127.0.0.1:1/#synthetic"},
		{"missing-index", "ZIPKIN_ES_INDEX", ""},
		{"missing-service", "ZIPKIN_ES_SERVICE", ""},
		{"username-only", "ZIPKIN_ES_USERNAME", "synthetic-user"},
		{"password-only", "ZIPKIN_ES_PASSWORD", "synthetic-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]string{
				"ZIPKIN_ES_INTEGRATION": "1", "ZIPKIN_ES_URL": "http://127.0.0.1:1",
				"ZIPKIN_ES_INDEX": "fixture-index", "ZIPKIN_ES_SERVICE": "fixture-service",
			}
			settings[tc.key] = tc.value
			_, enabled, err := loadESIntegrationConfig(func(key string) (string, bool) { v, ok := settings[key]; return v, ok })
			if !enabled || err == nil {
				t.Fatal("invalid explicit configuration accepted")
			}
			if tc.value != "" && strings.Contains(err.Error(), tc.value) {
				t.Fatal("validation error included supplied value")
			}
		})
	}
}

// runESIntegrationChild executes only the real test with synthetic environment settings.
func runESIntegrationChild(t *testing.T, endpoint, flag string) ([]byte, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("test executable unavailable")
	}
	cmd := exec.Command(executable, "-test.run=^TestESClient$", "-test.v", "-test.timeout=40s")
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "ZIPKIN_ES_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "ZIPKIN_ES_URL="+endpoint, "ZIPKIN_ES_INDEX=fixture-index",
		"ZIPKIN_ES_SERVICE=fixture-service", "ZIPKIN_ES_USERNAME=synthetic-user", "ZIPKIN_ES_PASSWORD=synthetic-password")
	if flag != "" {
		cmd.Env = append(cmd.Env, "ZIPKIN_ES_INTEGRATION="+flag)
	}
	return cmd.CombinedOutput()
}

// TestESIntegrationDefaultMakesNoRequests checks the actual test with an ambient endpoint.
func TestESIntegrationDefaultMakesNoRequests(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "unexpected default integration request", http.StatusInternalServerError)
	}))
	defer server.Close()
	output, err := runESIntegrationChild(t, server.URL, "")
	if err != nil || !bytes.Contains(output, []byte("--- SKIP: TestESClient")) {
		t.Error("default integration did not exit successfully with explicit skip")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatal("default integration contacted synthetic endpoint")
	}
}

// TestESIntegrationExplicitOptInContactsConfiguredEndpoint checks the actual test gate.
func TestESIntegrationExplicitOptInContactsConfiguredEndpoint(t *testing.T) {
	var requests int32
	var bad int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		username, password, ok := r.BasicAuth()
		if r.Method != http.MethodGet || r.URL.Path != "/" || !ok ||
			username != "synthetic-user" || password != "synthetic-password" ||
			r.URL.User != nil {
			atomic.StoreInt32(&bad, 1)
		}
		// Stop before the unchanged legacy JSON library: no real service is contacted.
		http.Error(w, "synthetic rejected endpoint", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	output, err := runESIntegrationChild(t, server.URL, "1")
	if err == nil || !bytes.Contains(output, []byte("Elasticsearch integration ping failed; response and configuration values suppressed")) {
		t.Fatal("explicit integration did not report the synthetic endpoint failure")
	}
	if atomic.LoadInt32(&requests) != 1 || atomic.LoadInt32(&bad) != 0 {
		t.Fatal("explicit integration endpoint or authentication contract failed")
	}
	for _, forbidden := range []string{"synthetic-user", "synthetic-password", "synthetic rejected endpoint"} {
		if bytes.Contains(output, []byte(forbidden)) {
			t.Fatal("integration failure output included supplied configuration or response")
		}
	}
}

// TestESIntegrationTransportAuthenticatesAndCompletesCleanup checks the HTTP boundary.
func TestESIntegrationTransportAuthenticatesAndCompletesCleanup(t *testing.T) {
	var requests int32
	var bad int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		username, password, ok := r.BasicAuth()
		if !ok || username != "synthetic-user" || password != "synthetic-password" ||
			r.URL.User != nil || r.URL.Path != "/_search/scroll" {
			atomic.StoreInt32(&bad, 1)
		}
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("synthetic endpoint parse failed")
	}
	transport := &esIntegrationTransport{
		base: &http.Transport{}, cfg: esIntegrationConfig{endpoint: endpoint,
			username: "synthetic-user", password: "synthetic-password"}, cleaned: make(chan struct{}),
	}
	defer transport.base.(*http.Transport).CloseIdleConnections()
	request, err := http.NewRequest("delete", server.URL+"/_search/scroll", nil)
	if err != nil {
		t.Fatal("synthetic request creation failed")
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal("synthetic transport request failed")
	}
	if request.Header.Get("Authorization") != "" {
		t.Fatal("transport modified caller headers")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal("synthetic body close failed")
	}
	select {
	case <-transport.cleaned:
		if transport.cleanupFailed {
			t.Fatal("successful synthetic cleanup reported failure")
		}
	default:
		t.Fatal("synthetic cleanup was not completed")
	}
	if atomic.LoadInt32(&requests) != 1 || atomic.LoadInt32(&bad) != 0 {
		t.Fatal("synthetic authentication boundary failed")
	}
}

// TestESIntegrationCredentialsDoNotFollowRedirects refuses a different synthetic origin.
func TestESIntegrationCredentialsDoNotFollowRedirects(t *testing.T) {
	var destinationRequests int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&destinationRequests, 1)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer origin.Close()
	endpoint, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal("synthetic endpoint parse failed")
	}
	transport := &esIntegrationTransport{
		base: &http.Transport{}, cfg: esIntegrationConfig{endpoint: endpoint,
			username: "synthetic-user", password: "synthetic-password"}, cleaned: make(chan struct{}),
	}
	defer transport.base.(*http.Transport).CloseIdleConnections()
	client := &http.Client{Transport: transport}
	resp, err := client.Get(origin.URL)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin redirect was accepted")
	}
	if atomic.LoadInt32(&destinationRequests) != 0 {
		t.Fatal("credentials reached another synthetic origin")
	}
}
