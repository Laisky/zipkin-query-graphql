package zipkin_graphql

import (
	stdjson "encoding/json"
	utils "github.com/Laisky/go-utils"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ginMiddlewares "github.com/Laisky/gin-middlewares"
	"github.com/gin-gonic/gin"
)

// TestConsumerGraphQLGinContract exercises the real schema and std-handler adapter without Elasticsearch.
func TestConsumerGraphQLGinContract(t *testing.T) {
	engine := gin.New()
	engine.Any("/query/", ginMiddlewares.FromStd(newGraphQLHandler()))
	for _, tc := range []struct {
		name, query string
		valid       bool
		contentType string
	}{
		{"schema typename", "{ __typename }", true, "application/json"},
		{"headerless JSON", "{ __typename }", true, ""},
		{"form content-type JSON", "{ __typename }", true, "application/x-www-form-urlencoded"},
		{"invalid field", "{ missingFixtureField }", false, "application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := stdjson.Marshal(map[string]string{"query": tc.query})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/query/", strings.NewReader(string(body)))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if request.Header.Get("Content-Type") != tc.contentType {
				t.Fatal("handler mutated the caller header")
			}
			var result map[string]interface{}
			if err := stdjson.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal("GraphQL response was not JSON")
			}
			if tc.valid {
				if response.Code != http.StatusOK {
					t.Fatal("valid GraphQL schema request failed")
				}
				data, ok := result["data"].(map[string]interface{})
				if !ok || data["__typename"] != "Query" {
					t.Fatal("GraphQL schema contract changed")
				}
			} else {
				errors, ok := result["errors"].([]interface{})
				if !ok || len(errors) == 0 {
					t.Fatal("unknown GraphQL field was accepted")
				}
			}
		})
	}
}

// TestConsumerGraphQLTracesContract exercises the generated schema, defaults, Date scalar and real resolver.
func TestConsumerGraphQLTracesContract(t *testing.T) {
	previousClient := esClient
	previousTimezone := utils.Settings.Get("settings.ui_timezone")
	previousPrefix := utils.Settings.Get("span-url-prefix")
	t.Cleanup(func() {
		esClient = previousClient
		utils.Settings.Set("settings.ui_timezone", previousTimezone)
		utils.Settings.Set("span-url-prefix", previousPrefix)
	})
	utils.Settings.Set("settings.ui_timezone", "+0000")
	utils.Settings.Set("span-url-prefix", "http://synthetic.invalid/traces/")
	cli, cleaned := consumerScrollClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.Write([]byte("{}"))
			return
		}
		if r.URL.Path != "/zipkin-prod-alias/span/_search" {
			t.Error("default environment or index changed")
		}
		consumerScrollReply(w, "graphql-fixture", "trace-1")
	})
	esClient = cli
	engine := gin.New()
	engine.Any("/query/", ginMiddlewares.FromStd(newGraphQLHandler()))
	query := `{traces(want:["fixture-api"],size:1,daterange:{from:"2023-11-14 22:12:00",to:"2023-11-14 22:14:00"}){url svcs duration_ms}}`
	body, err := stdjson.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/query/", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	select {
	case <-cleaned:
	case <-time.After(3 * time.Second):
		t.Fatal("GraphQL scroll cleanup did not finish")
	}
	if response.Code != http.StatusOK {
		t.Fatal("trace query failed")
	}
	var result struct {
		Data struct {
			Traces []Span `json:"traces"`
		} `json:"data"`
		Errors []interface{} `json:"errors"`
	}
	if err := stdjson.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal("trace result was not JSON")
	}
	if len(result.Errors) != 0 || len(result.Data.Traces) != 1 {
		t.Fatal("trace GraphQL result changed")
	}
	got := result.Data.Traces[0]
	if got.URL != "http://synthetic.invalid/traces/trace-1" || len(got.Svcs) != 1 || got.Svcs[0] != "fixture-api" || got.DurationMs != 0 {
		t.Fatal("trace GraphQL field contract changed")
	}
}
