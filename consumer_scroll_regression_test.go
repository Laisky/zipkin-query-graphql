package zipkin_graphql

import (
	"context"
	stdjson "encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConsumerJSONRoundTrip protects the graph's JSON map encoder and decoder on supported Go.
func TestConsumerJSONRoundTrip(t *testing.T) {
	want := map[string]interface{}{"query": map[string]interface{}{"services": []interface{}{"fixture-api", "fixture-世界"}, "active": true, "limit": float64(2)}, "optional": nil}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("JSON round trip changed the request graph")
	}
}

// consumerScrollClient installs a synthetic HTTP endpoint and waits for actual cleanup-body closure.
func consumerScrollClient(t *testing.T, handler http.HandlerFunc) (*ESClient, <-chan struct{}) {
	t.Helper()
	server := httptest.NewServer(handler)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &esIntegrationTransport{base: &http.Transport{}, cfg: esIntegrationConfig{endpoint: endpoint}, cleaned: make(chan struct{})}
	previous := httpClient
	httpClient = &http.Client{Transport: transport, Timeout: 2 * time.Second}
	t.Cleanup(func() { httpClient = previous; transport.base.(*http.Transport).CloseIdleConnections(); server.Close() })
	return &ESClient{api: server.URL}, transport.cleaned
}

func consumerScrollReply(w http.ResponseWriter, id string, spans ...string) {
	hits := make([]interface{}, 0, len(spans))
	for _, span := range spans {
		hits = append(hits, map[string]interface{}{"_source": map[string]interface{}{"traceId": span, "id": span, "timestamp_millis": float64(1700000000000), "localEndpoint": map[string]string{"serviceName": "fixture-api"}}})
	}
	w.Header().Set("Content-Type", "application/json")
	stdjson.NewEncoder(w).Encode(map[string]interface{}{"_scroll_id": id, "hits": map[string]interface{}{"total": len(hits), "hits": hits}})
}

func consumerCollectSpans(t *testing.T, cli *ESClient, max int, cleaned <-chan struct{}) []*SpanDocu {
	t.Helper()
	now := time.Unix(1700000000, 0).UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spans, err := cli.LoadSpansChan(ctx, max, "fixture", []string{"fixture-api"}, now.Add(-time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	var got []*SpanDocu
	for {
		select {
		case span, ok := <-spans:
			if !ok {
				select {
				case <-cleaned:
					return got
				case <-ctx.Done():
					t.Fatal("scroll cleanup body was not closed")
				}
			}
			if span == nil {
				t.Fatal("scroll produced a missing span")
			}
			got = append(got, span)
		case <-ctx.Done():
			t.Fatal("synthetic scroll did not close")
		}
	}
}

// TestConsumerScrollTracksLatestID proves full pagination and cleanup follow rotating IDs.
func TestConsumerScrollTracksLatestID(t *testing.T) {
	var mu sync.Mutex
	var requests, deleted []string
	calls := 0
	cli, cleaned := consumerScrollClient(t, func(w http.ResponseWriter, r *http.Request) {
		method := strings.ToUpper(r.Method)
		var data map[string]interface{}
		if err := stdjson.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Error("scroll request was not JSON")
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/fixture/span/_search" {
			if method != "POST" || r.URL.Query().Get("scroll") != "2m" {
				t.Error("initial scroll request changed")
			}
			if _, ok := data["query"]; !ok {
				t.Error("initial query missing")
			}
			consumerScrollReply(w, "page-1", "trace-1")
			return
		}
		if r.URL.Path != "/_search/scroll" {
			t.Error("unexpected synthetic path")
			http.Error(w, "unexpected path", 500)
			return
		}
		sid, _ := data["scroll_id"].(string)
		if method == "DELETE" {
			deleted = append(deleted, sid)
			w.Write([]byte("{}"))
			return
		}
		if method != "POST" {
			t.Error("unexpected scroll method")
		}
		requests = append(requests, sid)
		calls++
		if calls == 1 {
			consumerScrollReply(w, "page-2", "trace-2")
			return
		}
		if calls == 2 {
			consumerScrollReply(w, "page-3")
			return
		}
		t.Error("scroll did not stop on empty page")
		consumerScrollReply(w, "page-3")
	})
	got := consumerCollectSpans(t, cli, 10, cleaned)
	if len(got) != 2 || got[0].TraceID != "trace-1" || got[1].TraceID != "trace-2" {
		t.Fatal("full scroll lost or duplicated spans")
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"page-1", "page-2"}) {
		t.Fatalf("continuation IDs=%v, want latest IDs", requests)
	}
	if !reflect.DeepEqual(deleted, []string{"page-3"}) {
		t.Fatalf("deleted IDs=%v, want last ID", deleted)
	}
}

// TestConsumerScrollHonorsLimit proves a page cannot overrun the requested span count.
func TestConsumerScrollHonorsLimit(t *testing.T) {
	for _, max := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("max=%d", max), func(t *testing.T) {
			var continuations int32
			cli, cleaned := consumerScrollClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.EqualFold(r.Method, "DELETE") {
					w.Write([]byte("{}"))
					return
				}
				if r.URL.Path == "/fixture/span/_search" {
					consumerScrollReply(w, "bounded", "trace-1", "trace-2")
					return
				}
				atomic.AddInt32(&continuations, 1)
				consumerScrollReply(w, "bounded")
			})
			got := consumerCollectSpans(t, cli, max, cleaned)
			if len(got) != max {
				t.Fatalf("got %d spans, want exactly %d", len(got), max)
			}
			for i, span := range got {
				if span.TraceID != fmt.Sprintf("trace-%d", i+1) {
					t.Fatal("bounded page changed span order")
				}
			}
			if atomic.LoadInt32(&continuations) != 0 {
				t.Fatal("scroll continued after reaching its limit")
			}
		})
	}
}

type consumerCloseBody struct {
	io.Reader
	closed *bool
}

func (b consumerCloseBody) Close() error { *b.closed = true; return nil }

type consumerTransport func(*http.Request) (*http.Response, error)

func (f consumerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestConsumerPingClosesBody proves successful pings release their HTTP response.
func TestConsumerPingClosesBody(t *testing.T) {
	closed := false
	previous := httpClient
	defer func() { httpClient = previous }()
	httpClient = &http.Client{Transport: consumerTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: consumerCloseBody{Reader: strings.NewReader("{}"), closed: &closed}}, nil
	})}
	if _, err := NewESClient("http://synthetic.invalid"); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("successful ping left its response body open")
	}
}
