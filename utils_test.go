package zipkin_graphql_test

import (
	"testing"

	zipkin_graphql "github.com/Laisky/zipkin-query-graphql"
)

func TestURLJoin(t *testing.T) {
	a := "ab/"
	b := "/c/d/e/"
	ret := zipkin_graphql.URLJoin(a, b)
	if ret != "ab/c/d/e/" {
		t.Errorf("URLJoin error, got %v", ret)
	}
}
