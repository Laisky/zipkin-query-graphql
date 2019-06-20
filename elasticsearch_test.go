package zipkin_graphql_test

import (
	"context"
	"testing"
	"time"

	"github.com/Laisky/go-utils"

	zipkin_graphql "github.com/Laisky/zipkin-query-graphql"
)

func TestESClient(t *testing.T) {
	cli, err := zipkin_graphql.NewESClient("http://readonly:RrXLSWlEdld2y8f@172.16.7.41:8200")
	if err != nil {
		t.Fatalf("got error: %+v", err)
	}

	ctx := context.Background()
	docuChan, err := cli.LoadSpansChan(ctx, 10, "zipkin-prod-alias", []string{"dbdevice"}, utils.Clock.GetUTCNow().Add(-1*time.Minute), utils.Clock.GetUTCNow())
	if err != nil {
		t.Fatalf("got error: %+v", err)
	}

	var n int
	for _ = range docuChan {
		// t.Logf("got docu: %+v", docu)
		n++
		continue
	}

	t.Errorf("got %v docus", n)
}

func init() {
	utils.SetupLogger("info")
}
