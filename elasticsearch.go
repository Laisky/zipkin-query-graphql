package zipkin_graphql

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/Laisky/zap"

	"github.com/Laisky/go-utils"

	"github.com/pkg/errors"

	jsoniter "github.com/json-iterator/go"
)

var (
	json       = jsoniter.ConfigCompatibleWithStandardLibrary
	httpClient = &http.Client{ // default http client
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
		},
		Timeout: time.Duration(30) * time.Second,
	}
	esClient *ESClient
)

type ESClient struct {
	api string
}

func SetupESCli(esapi string) {
	var err error
	if esClient, err = NewESClient(esapi); err != nil {
		utils.Logger.Panic("try to create es client got error", zap.Error(err))
	}
}

func NewESClient(api string) (*ESClient, error) {
	if resp, err := httpClient.Get(api); err != nil {
		return nil, errors.Wrap(err, "try to ping es api got error")
	} else if resp.StatusCode/100 != 2 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "try to read err resp got error")
		}
		defer resp.Body.Close()
		return nil, errors.Wrap(fmt.Errorf("[%v] %v", resp.StatusCode, string(body)), "try to ping es api got error")
	}

	return &ESClient{api: api}, nil
}

const (
	uiTimeFormat = "2006-01-02 15:04:05-0700"
	esTimeFormat = "2006-01-02 15:04:05"
	esDateFormat = "yyyy-MM-dd HH:mm:ss"
)

func GenerateScrollQuery(index string, svcs []string, fr, to time.Time) map[string]interface{} {
	return map[string]interface{}{
		"size": 1000,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": map[string]interface{}{
					"terms": map[string]interface{}{
						"localEndpoint.serviceName": svcs,
					},
				},
				"must": []map[string]interface{}{
					map[string]interface{}{
						"range": map[string]interface{}{
							"timestamp_millis": map[string]interface{}{
								"gte":       fr.Format(esTimeFormat),
								"lte":       to.Format(esTimeFormat),
								"time_zone": "+00:00",
								"format":    esDateFormat,
							},
						},
					},
					map[string]interface{}{
						"match": map[string]interface{}{
							"_type": "span",
						},
					},
				},
			},
		},
		"sort": []map[string]interface{}{
			map[string]interface{}{
				"timestamp_millis": map[string]interface{}{
					"order": "desc",
				},
			},
		},
		// "_source": []string{
		// 	"traceId",
		// 	"localEndpoint.serviceName",
		// 	"timestamp_millis",
		// 	"kind",
		// 	"name",
		// 	"id",
		// },
	}
}

type ScrollResp struct {
	ScrollID string     `json:"_scroll_id"`
	Hits     *ScrollHit `json:"hits"`
}

type ScrollHit struct {
	Total int       `json:"total"`
	Hits  []*ESDocu `json:"hits"`
}

type ESDocu struct {
	Index  string    `json:"_index"`
	Type   string    `json:"_type"`
	Score  float64   `json:"_score"`
	Source *SpanDocu `json:"_source"`
}

type SpanDocu struct {
	LocalEndpoint   *SpanSvcInfo `json:"localEndpoint"`
	TimestampMillis int          `json:"timestamp_millis"`
	TraceID         string       `json:"traceId"`
	Kind            string       `json:"kind"`
	Name            string       `json:"name"`
	ID              string       `json:"id"`
	ParentID        string       `json:"parentId"`
}

type SpanSvcInfo struct {
	ServiceName string `json:"serviceName"`
}

func (es *ESClient) LoadSpansChan(ctx context.Context, maxN int, index string, svcs []string, fr, to time.Time) (outchan chan *SpanDocu, err error) {
	utils.Logger.Info("LoadSpansChan",
		zap.Int("maxn", maxN),
		zap.Strings("svcs", svcs),
		zap.String("index", index),
		zap.Time("from", fr),
		zap.Time("to", to))
	if fr.After(to) {
		return nil, fmt.Errorf("to must later than fr")
	}

	// create scroll
	query := GenerateScrollQuery(index, svcs, fr, to)
	resp := &ScrollResp{}
	if err = utils.RequestJSONWithClient(httpClient, "post", URLJoin(es.api, index, "/span/_search?scroll=2m"), &utils.RequestData{Data: query}, resp); err != nil {
		return nil, errors.Wrapf(err, "try to request es got error: %v", URLJoin(es.api, index, "/span/_search?scroll=2m"))
	}

	totalN := 0
	outchan = make(chan *SpanDocu, 1000)
	go func(resp *ScrollResp) {
		defer func(sid string) {
			close(outchan)
			// delete scroll
			if err := utils.RequestJSONWithClient(
				httpClient,
				"delete",
				URLJoin(es.api, "_search/scroll"),
				&utils.RequestData{Data: map[string]string{"scroll_id": sid}},
				new(interface{}),
			); err != nil {
				utils.Logger.Warn("try to delete scroll got error", zap.Error(err))
			}
		}(resp.ScrollID)

		var (
			gotn int
			err  error
			sid  = resp.ScrollID
		)
		for {
			for _, docu := range resp.Hits.Hits {
				select {
				case outchan <- docu.Source:
					totalN++
					if totalN > maxN {
						return
					}
				case <-ctx.Done():
					utils.Logger.Info("scroll end", zap.String("scroll_id", sid))
					return
				}
			}

			// continue scroll
			resp = &ScrollResp{}
			if err = utils.RequestJSONWithClient(
				httpClient, "post", URLJoin(es.api, "_search/scroll"),
				&utils.RequestData{Data: map[string]string{
					"scroll":    "2m",
					"scroll_id": sid,
				}},
				resp,
			); err != nil {
				utils.Logger.Error("try to request es got error", zap.Error(err), zap.String("url", URLJoin(es.api, "_search/scroll")))
				return
			}

			gotn = len(resp.Hits.Hits)
			utils.Logger.Info("got new spans", zap.Int("n", gotn))
			if gotn == 0 {
				utils.Logger.Info("scroll finished")
				return
			}
		}
	}(resp)

	return outchan, nil
}
