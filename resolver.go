package zipkin_graphql

import (
	"context"
	"fmt"
	"math"
	"time"

	mapset "github.com/deckarep/golang-set"

	"github.com/Laisky/go-utils"
	"github.com/Laisky/zap"
	"github.com/pkg/errors"
) // THIS CODE IS A STARTING POINT ONLY. IT WILL NOT BE UPDATED WITH SCHEMA CHANGES.

type Resolver struct{}

func (r *Resolver) Query() QueryResolver {
	return &queryResolver{r}
}

type queryResolver struct{ *Resolver }

func parseStr2Time(ts string) (t time.Time, err error) {
	if t, err = time.Parse(uiTimeFormat, ts+utils.Settings.GetString("settings.timezone")); err != nil {
		utils.Logger.Warn("try to parse ts got error", zap.Error(err), zap.String("ts", ts))
		return utils.Clock.GetUTCNow(), err
	}

	return t.UTC(), nil
}

func (r *queryResolver) Traces(ctx context.Context, want, exclude []string, size int, daterange DateRange) (spans []Span, err error) {
	var (
		ft, tt time.Time
	)
	if ft, err = parseStr2Time(daterange.From); err != nil {
		return nil, errors.Wrap(err, "invalidate from, time should be `2019-10-10 10:10:10`")
	}
	if daterange.To == "now" {
		tt = utils.Clock.GetUTCNow()
	} else if tt, err = parseStr2Time(daterange.To); err != nil {
		return nil, errors.Wrap(err, "invalidate to, time should be `2019-10-10 10:10:10`")
	}

	if len(want)+len(exclude) == 0 {
		return nil, fmt.Errorf("`want` and `exclude` should not all empty")
	}

	docuChan, err := esClient.LoadSpansChan(ctx, size, "zipkin-prod-alias", append(want, exclude...), ft, tt)
	if err != nil {
		utils.Logger.Warn("try to scroll got error", zap.Error(err))
		return nil, fmt.Errorf("try to scroll docus got error")
	}

	spanMap := generateSpanMap(docuChan)
	filterByWantAndExclude(spanMap, want, exclude)

	return generateSpans(spanMap), nil
}

func getSvcsFromSpan(spans []*SpanDocu) []string {
	svcset := mapset.NewSet()
	for _, span := range spans {
		svcset.Add(span.LocalEndpoint.ServiceName)
	}

	svcs := []string{}
	svcset.Each(func(v interface{}) bool {
		svcs = append(svcs, v.(string))
		return true
	})

	return svcs
}

func getSpanDuration(spans []*SpanDocu) time.Duration {
	var (
		maxTs = 0
		minTs = int(math.MaxInt64)
	)

	for _, span := range spans {
		if span.TimestampMillis > maxTs {
			maxTs = span.TimestampMillis
		}
		if span.TimestampMillis < minTs {
			minTs = span.TimestampMillis
		}
	}

	return time.Duration(maxTs-minTs) * time.Millisecond
}

func generateSpans(spanMap map[string][]*SpanDocu) (spans []Span) {
	spans = []Span{}
	for sid, sdocus := range spanMap {
		spans = append(spans, Span{
			URL:        utils.Settings.GetString("span-url-prefix") + sid,
			Svcs:       getSvcsFromSpan(sdocus),
			DurationMs: int(getSpanDuration(sdocus) / time.Millisecond),
		})
	}

	return spans
}

func generateSpanMap(spanChan chan *SpanDocu) (spanMap map[string][]*SpanDocu) {
	var ok bool
	spanMap = map[string][]*SpanDocu{}
	for span := range spanChan {
		if _, ok = spanMap[span.TraceID]; !ok {
			spanMap[span.TraceID] = []*SpanDocu{span}
		} else {
			spanMap[span.TraceID] = append(spanMap[span.TraceID], span)
		}
	}

	return spanMap
}

func getSvcSetBySpans(spans []*SpanDocu) mapset.Set {
	set := mapset.NewSet()
	for _, span := range spans {
		set.Add(span.LocalEndpoint.ServiceName)
	}
	return set
}

func filterByWantAndExclude(spanMap map[string][]*SpanDocu, want, exclude []string) {
	var (
		wantSet    = mapset.NewSet()
		excludeSet = mapset.NewSet()
		spanSet    mapset.Set
	)

	for _, v := range want {
		wantSet.Add(v)
	}
	for _, v := range exclude {
		excludeSet.Add(v)
	}

	for sid, spans := range spanMap {
		spanSet = getSvcSetBySpans(spans)
		if excludeSet.Intersect(spanSet).Cardinality() != 0 ||
			!wantSet.IsSubset(spanSet) {
			delete(spanMap, sid)
			continue
		}
	}
}
