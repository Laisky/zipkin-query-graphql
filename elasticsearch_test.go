package zipkin_graphql

import (
	"context"
	"fmt"
	utils "github.com/Laisky/go-utils"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// esIntegrationConfig contains only explicitly supplied integration settings.
type esIntegrationConfig struct {
	endpoint                           *url.URL
	index, service, username, password string
}

// loadESIntegrationConfig leaves integration disabled unless the exact opt-in is set.
func loadESIntegrationConfig(lookup func(string) (string, bool)) (esIntegrationConfig, bool, error) {
	var cfg esIntegrationConfig
	enabled, _ := lookup("ZIPKIN_ES_INTEGRATION")
	if enabled != "1" {
		return cfg, false, nil
	}
	rawURL, _ := lookup("ZIPKIN_ES_URL")
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint == nil || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return cfg, true, fmt.Errorf("ZIPKIN_ES_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	cfg.endpoint = endpoint
	cfg.index, _ = lookup("ZIPKIN_ES_INDEX")
	cfg.service, _ = lookup("ZIPKIN_ES_SERVICE")
	if cfg.index == "" || cfg.service == "" {
		return cfg, true, fmt.Errorf("ZIPKIN_ES_INDEX and ZIPKIN_ES_SERVICE are required")
	}
	cfg.username, _ = lookup("ZIPKIN_ES_USERNAME")
	cfg.password, _ = lookup("ZIPKIN_ES_PASSWORD")
	if (cfg.username == "") != (cfg.password == "") {
		return cfg, true, fmt.Errorf("ZIPKIN_ES_USERNAME and ZIPKIN_ES_PASSWORD must be set together")
	}
	return cfg, true, nil
}

// esIntegrationTransport keeps optional authentication outside logged endpoint URLs.
type esIntegrationTransport struct {
	base          http.RoundTripper
	cfg           esIntegrationConfig
	cleaned       chan struct{}
	once          sync.Once
	cleanupFailed bool
}

// RoundTrip confines requests and credentials to the configured integration origin.
func (tr *esIntegrationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != tr.cfg.endpoint.Scheme || req.URL.Host != tr.cfg.endpoint.Host {
		return nil, fmt.Errorf("integration request outside the configured origin refused")
	}
	clone := new(http.Request)
	*clone = *req
	clone.Header = make(http.Header, len(req.Header))
	for key, values := range req.Header {
		clone.Header[key] = append([]string(nil), values...)
	}
	if tr.cfg.username != "" {
		clone.SetBasicAuth(tr.cfg.username, tr.cfg.password)
	}
	resp, err := tr.base.RoundTrip(clone)
	if err == nil && strings.EqualFold(req.Method, http.MethodDelete) && strings.HasSuffix(req.URL.Path, "/_search/scroll") {
		resp.Body = &esIntegrationBody{ReadCloser: resp.Body, complete: func() {
			tr.once.Do(func() {
				tr.cleanupFailed = resp.StatusCode/100 != 2
				close(tr.cleaned)
			})
		}}
	}
	return resp, err
}

// esIntegrationBody signals completion when the scroll cleanup body is closed.
type esIntegrationBody struct {
	io.ReadCloser
	complete func()
}

// Close closes the response body and records completed integration cleanup.
func (body *esIntegrationBody) Close() error {
	err := body.ReadCloser.Close()
	body.complete()
	return err
}

// TestESClient runs a bounded live scroll only with explicit environment opt-in.
func TestESClient(t *testing.T) {
	cfg, enabled, err := loadESIntegrationConfig(os.LookupEnv)
	if !enabled {
		t.Skip("live Elasticsearch integration disabled; see docs/integration-tests.md")
	}
	if err != nil {
		t.Fatal(err)
	}
	transport := &esIntegrationTransport{
		base: &http.Transport{MaxIdleConnsPerHost: 20}, cfg: cfg, cleaned: make(chan struct{}),
	}
	previous := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	defer func() {
		httpClient = previous
		transport.base.(*http.Transport).CloseIdleConnections()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli, err := NewESClient(cfg.endpoint.String())
	if err != nil {
		t.Fatal("Elasticsearch integration ping failed; response and configuration values suppressed")
	}
	now := utils.Clock.GetUTCNow()
	spans, err := cli.LoadSpansChan(ctx, 10, cfg.index, []string{cfg.service}, now.Add(-time.Minute), now)
	if err != nil {
		t.Fatal("Elasticsearch integration search failed; response and configuration values suppressed")
	}
	count := 0
	for {
		select {
		case span, open := <-spans:
			if open {
				if span == nil {
					t.Fatal("Elasticsearch integration returned a missing span")
				}
				count++
				continue
			}
			select {
			case <-transport.cleaned:
				if transport.cleanupFailed {
					t.Fatal("Elasticsearch integration scroll cleanup failed")
				}
				t.Logf("Elasticsearch integration completed (%d spans)", count)
				return
			case <-ctx.Done():
				t.Fatal("Elasticsearch integration cleanup exceeded its deadline")
			}
		case <-ctx.Done():
			t.Fatal("Elasticsearch integration exceeded its deadline")
		}
	}
}

// init configures test logging without enabling request debugging.
func init() { utils.SetupLogger("error") }
