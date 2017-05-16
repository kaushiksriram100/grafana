package kairosdb

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"golang.org/x/net/context/ctxhttp"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/grafana/grafana/pkg/components/null"
	"github.com/grafana/grafana/pkg/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/tsdb"
)

type KairosdbExecutor struct {
	*models.DataSource
	httpClient *http.Client
}

func NewKairosdbExecutor(datasource *models.DataSource) (tsdb.Executor, error) {
	plog.Info("Inside NewKairosdbExecutor - 0")
	httpClient, err := datasource.GetHttpClient()

	plog.Info("Inside NewKairosdbExecutor - 1")

	if err != nil {
		return nil, err
	}
	plog.Info("Inside NewKairosdbExecutor - 2")
	return &KairosdbExecutor{
		DataSource: datasource,
		httpClient: httpClient,
	}, nil
}

var (
	plog log.Logger
)

func init() {
	plog = log.New("tsdb.kairosdb1")
	var exec = NewKairosdbExecutor
	plog.Info("Exec value is ", exec)
	tsdb.RegisterExecutor("grafana-kairosdb-datasource", exec)
}

func (e *KairosdbExecutor) Execute(ctx context.Context, queries tsdb.QuerySlice, queryContext *tsdb.QueryContext) *tsdb.BatchResult {

	plog.Info("Inside execute")
	result := &tsdb.BatchResult{}

	var kairosdbQuery KairosdbQuery

	kairosdbQuery.StartAbsolute = queryContext.TimeRange.GetFromAsMsEpoch()
	plog.Debug("kairosdbQuery.Start", "start", kairosdbQuery.StartAbsolute)
	kairosdbQuery.EndAbsolute = queryContext.TimeRange.GetToAsMsEpoch()
	plog.Debug("kairosdbQuery.End", "end", kairosdbQuery.EndAbsolute)

	for _, query := range queries {
		metric := e.buildMetric(query)
		kairosdbQuery.Queries = append(kairosdbQuery.Queries, metric)
	}

	if setting.Env == setting.DEV {
		plog.Debug("KairosDb request", "params", kairosdbQuery)
	}

	req, err := e.createRequest(kairosdbQuery)
	if err != nil {
		result.Error = err
		return result
	}

	res, err := ctxhttp.Do(ctx, e.httpClient, req)
	if err != nil {
		result.Error = err
		return result
	}

	queryResult, err := e.parseResponse(kairosdbQuery, res)
	if err != nil {
		return result.WithError(err)
	}

	result.QueryResults = queryResult
	return result
}

func (e *KairosdbExecutor) createRequest(data KairosdbQuery) (*http.Request, error) {
	u, _ := url.Parse(e.Url)
	u.Path = path.Join(u.Path, "api/v1/datapoints/query/")

	postData, err := json.Marshal(data)
	plog.Info("postdata value -- ", data.Queries)
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(string(postData)))
	if err != nil {
		plog.Info("Failed to create request", "error", err)
		return nil, fmt.Errorf("Failed to create request. error: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if e.BasicAuth {
		req.SetBasicAuth(e.BasicAuthUser, e.BasicAuthPassword)
	}

	return req, err
}

func (e *KairosdbExecutor) parseResponse(query KairosdbQuery, res *http.Response) (map[string]*tsdb.QueryResult, error) {

	queryResults := make(map[string]*tsdb.QueryResult)
	queryRes := tsdb.NewQueryResult()

	body, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		return nil, err
	}

	if res.StatusCode/100 != 2 {
		plog.Info("Request failed", "status", res.Status, "body", string(body))
		return nil, fmt.Errorf("Request failed status: %v", res.Status)
	}

	var data []KairosdbResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		plog.Info("Failed to unmarshal opentsdb response", "error", err, "status", res.Status, "body", string(body))
		return nil, err
	}

	for _, val := range data {
		series := tsdb.TimeSeries{
			Name: val.Metric,
		}

		for timeString, value := range val.DataPoints {
			timestamp, err := strconv.ParseFloat(timeString, 64)
			if err != nil {
				plog.Info("Failed to unmarshal opentsdb timestamp", "timestamp", timeString)
				return nil, err
			}
			series.Points = append(series.Points, tsdb.NewTimePoint(null.FloatFrom(value), timestamp))
		}

		queryRes.Series = append(queryRes.Series, &series)
	}

	queryResults["A"] = queryRes
	return queryResults, nil
}

func (e *KairosdbExecutor) buildMetric(query *tsdb.Query) map[string]interface{} {
	plog.Info("query model - ", "query", query.Model)
	metric := make(map[string]interface{})

	// Setting metric
	metric["name"] = query.Model.Get("metric").MustString()
	plog.Info("query metric - ", "name", metric["name"])

	// Setting tags
	tags, tagsCheck := query.Model.CheckGet("tags")
	if tagsCheck && len(tags.MustMap()) > 0 {
		metric["tags"] = tags.MustMap()
	}
	plog.Info("query tags - ", "tags", metric["tags"])

	//setting aggregators

	aggregators, aggregatorsCheck := query.Model.CheckGet("horizontalAggregators")
	aggArray := aggregators.MustArray()
	if aggregatorsCheck && len(aggArray) > 0 {
		for _, aggArrayElem := range aggArray {
			aggName := aggArrayElem["name"]
			aggSamplingRate := aggArrayElem["sampling_rate"]
			
		}
		 = aggregators.MustArray()
	}
	plog.Info("query aggregator - ", "aggregators", metric["aggregators"])

	return metric

}
