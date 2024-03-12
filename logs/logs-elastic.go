//
// Copyright 2017, 2018  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

// Package logs provides the abstract logging infrastructure for pantahub
// logging endpoint as well as backends for elastic and mgo.
//
// Logs offers a simple logging service for Pantahub powered devices and apps.
// To post new log entries use the POST method on the main endpoint
// To page through log entries and sort etc. check the GET method
package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	elastic "github.com/olivere/elastic/v7"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/resty.v1"
)

type elasticLogEntry struct {
	*Entry

	TimeEvent  time.Time `json:"timeevent"`
	TimeRecord time.Time `json:"timerecord"`
}

type elasticLogger struct {
	elasticBaseURL       string
	elasticURL           *url.URL
	elasticBasicAuthUser string
	elasticBasicAuthPass string
	elasticBearerToken   string
	elasticIndexPrefix   string
	elasticIndexShards   int
	elasticIndexReplicas int
	works                bool
	template             bson.M
	syncWrites           bool
}

func (s *elasticLogger) r(timeout int) *resty.Request {
	if timeout == 0 {
		timeout = 15
	}
	request := utils.RT(timeout)
	if s.elasticBasicAuthUser != "" {
		request.SetBasicAuth(s.elasticBasicAuthUser, s.elasticBasicAuthPass)
	}
	if s.elasticBearerToken != "" {
		request.SetAuthToken(s.elasticBearerToken)
	}

	return request
}

func (s *elasticLogger) getTemplateURL() (*url.URL, error) {
	templateURLRef, err := url.Parse("_template/" + s.elasticIndexPrefix)

	if err != nil {
		return nil, err
	}

	return s.elasticURL.ResolveReference(templateURLRef), nil
}

func (s *elasticLogger) getAllIndexURL() (*url.URL, error) {
	templateURLRef, err := url.Parse(s.elasticIndexPrefix + "-*")

	if err != nil {
		return nil, err
	}

	return s.elasticURL.ResolveReference(templateURLRef), nil
}

func (s *elasticLogger) register() error {

	registerTemplatesURL, err := s.getTemplateURL()

	if err != nil {
		return err
	}

	response, err := s.r().SetBody(s.template).Put(registerTemplatesURL.String())

	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		log.Println("Failed Request returned: " + string(response.Body()))
		panic("Registering template failed with status: " + response.Status())
	}
	s.works = true

	return nil
}

func (s *elasticLogger) unregister(deleteIndex bool) error {
	registerTemplatesURL, err := s.getTemplateURL()

	if err != nil {
		return err
	}

	response, err := s.r().Delete(registerTemplatesURL.String())

	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		log.Println("Failed Delete template returned: " + string(response.Body()))
		return errors.New("Unregistering delete template failed with status: " + response.Status())
	}

	if !deleteIndex {
		return nil
	}

	allIndexURL, err := s.getAllIndexURL()

	if err != nil {
		return err
	}

	response, err = s.r().Delete(allIndexURL.String())

	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		log.Println("Failed Delete all indexes returned: " + string(response.Body()))
		return errors.New("Unregistering: Delete all indexes failed with status: " + response.Status())
	}

	return nil
}

func (s *elasticLogger) getLogs(pctx context.Context, start int64, page int64, before *time.Time,
	after *time.Time, query Filters, sort Sorts, cursor bool) (*Pager, error) {
	queryFmt := fmt.Sprintf(s.elasticIndexPrefix + "-*/_search")

	queryURL, err := url.Parse(queryFmt)

	if err != nil {
		return nil, err
	}

	queryURI := s.elasticURL.ResolveReference(queryURL)

	// build query part
	q := elastic.NewBoolQuery()
	if query.Owner != "" {
		q = q.Filter(elastic.NewMatchPhraseQuery("own", query.Owner))
	}
	if query.Device != "" {
		components := strings.Split(query.Device, ",")
		queryBool := elastic.NewBoolQuery()
		for _, device := range components {
			queryBool.Should(elastic.NewMatchPhraseQuery("dev", device))
		}
		q = q.Filter(queryBool)
	}
	if query.LogRev != "" {
		components := strings.Split(query.LogRev, ",")
		queryBool := elastic.NewBoolQuery()
		for _, rev := range components {
			queryBool.Should(elastic.NewMatchPhraseQuery("rev", rev))
		}
		q = q.Filter(queryBool)
	}
	if query.LogPlat != "" {
		components := strings.Split(query.LogPlat, ",")
		queryBool := elastic.NewBoolQuery()
		for _, plat := range components {
			queryBool.Should(elastic.NewMatchPhraseQuery("plat", plat))
		}
		q = q.Filter(queryBool)
	}
	if query.LogSource != "" {
		components := strings.Split(query.LogSource, ",")
		queryBool := elastic.NewBoolQuery()
		for _, source := range components {
			queryBool.Should(elastic.NewMatchPhraseQuery("src", source))
		}
		q = q.Filter(queryBool)
	}
	if query.LogLevel != "" {
		components := strings.Split(query.LogLevel, ",")
		queryBool := elastic.NewBoolQuery()
		for _, level := range components {
			queryBool.Should(elastic.NewMatchPhraseQuery("lvl", level))
		}
		q = q.Filter(queryBool)
	}
	if before != nil {
		q = q.Filter(elastic.NewRangeQuery("time-created").Lt(*before))
	}
	if after != nil {
		q = q.Filter(elastic.NewRangeQuery("time-created").Gt(*after))
	}

	// build search
	searchS := elastic.NewSearchSource().
		Query(q).
		From(int(start)).
		Size(int(page))

		// lets do the sort part
	for _, v := range sort {
		var asc bool
		if v[0] == '-' {
			asc = false
		} else {
			asc = true
		}
		// always strip the + and -
		if v[0] == '+' || v[0] == '-' {
			v = v[1:]
		}
		searchS = searchS.Sort(v, asc)
	}

	searchBody, err := searchS.Source()
	if err != nil {
		return nil, err
	}

	// add scroll to query; XXX: we need limits here for
	if cursor {
		q1 := queryURI.Query()
		q1.Add("scroll", "1m")
		queryURI.RawQuery = q1.Encode()
	}

	response, err := s.r().SetContext(pctx).SetBody(searchBody).Post(queryURI.String())
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusOK {
		errStr := fmt.Sprintf("WARN: getLogs call failed: %d - %s\n", response.StatusCode(), response.Body())
		return nil, errors.New(errStr)
	}

	var elasticResult elastic.SearchResult

	body := response.Body()
	err = json.Unmarshal(body, &elasticResult)

	if err != nil {
		return nil, err
	}

	var pagerResult Pager

	pagerResult.Count = elasticResult.TotalHits()
	pagerResult.Start = start
	pagerResult.Page = int64(len(elasticResult.Hits.Hits))
	pagerResult.NextCursor = elasticResult.ScrollId

	prototype := Entry{}
	arr := elasticResult.Each(reflect.TypeOf(&prototype))

	for _, v := range arr {
		pagerResult.Entries = append(pagerResult.Entries, v.(*Entry))
	}
	pagerResult.Count = int64(len(arr))

	return &pagerResult, nil
}

func (s *elasticLogger) scrollBuildNextURL(pretty bool) (string, url.Values, error) {
	path := "/_search/scroll"

	// Add query string parameters
	params := url.Values{}

	if pretty {
		params.Set("pretty", "1")
	}

	return path, params, nil
}

func (s *elasticLogger) scrollBuildBodyNext(keepAlive string, scrollID string) (interface{}, error) {
	body := struct {
		Scroll   string `json:"scroll"`
		ScrollID string `json:"scroll_id,omitempty"`
	}{
		Scroll:   keepAlive,
		ScrollID: scrollID,
	}
	return body, nil
}

func (s *elasticLogger) getLogsByCursor(pctx context.Context, nextCursor string) (*Pager, error) {
	queryFmt, values, err := s.scrollBuildNextURL(false)
	if err != nil {
		return nil, err
	}

	queryURL, err := url.Parse(queryFmt)
	if err != nil {
		return nil, err
	}

	queryURL.RawQuery = values.Encode()
	queryURI := s.elasticURL.ResolveReference(queryURL)

	searchBody, err := s.scrollBuildBodyNext("1m", nextCursor)
	if err != nil {
		return nil, err
	}

	response, err := s.r().SetContext(pctx).SetBody(searchBody).Post(queryURI.String())
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusOK {
		errStr := fmt.Sprintf("WARN: getLogs call failed: %d - %s\n", response.StatusCode(), response.Body())
		return nil, errors.New(errStr)
	}

	var elasticResult elastic.SearchResult

	body := response.Body()
	err = json.Unmarshal(body, &elasticResult)

	if err != nil {
		return nil, err
	}

	var pagerResult Pager

	pagerResult.Count = elasticResult.TotalHits()
	pagerResult.Start = 0
	pagerResult.Page = int64(len(elasticResult.Hits.Hits))
	pagerResult.NextCursor = elasticResult.ScrollId

	prototype := Entry{}
	arr := elasticResult.Each(reflect.TypeOf(&prototype))

	for _, v := range arr {
		pagerResult.Entries = append(pagerResult.Entries, v.(*Entry))
	}
	pagerResult.Count = int64(len(arr))

	return &pagerResult, nil
}

func (s *elasticLogger) postLogs(parentCtx context.Context, e []Entry) error {
	if !s.works {
		return errors.New("logger not initialized/works")
	}

	var buf bytes.Buffer

	timeRecv := time.Now()
	index := fmt.Sprintf(s.elasticIndexPrefix+"-%.4d%.2d%.2d", timeRecv.Year(), timeRecv.Month(), timeRecv.Day())
	buildURLStr := "_bulk"

	if s.syncWrites {
		buildURLStr = buildURLStr + "?refresh=wait_for"
	}
	bulkPostURL, err := url.Parse(buildURLStr)
	if err != nil {
		return err
	}

	postURL := s.elasticURL.ResolveReference(bulkPostURL)

	for _, v := range e {
		// write the bulkd op)
		m := bson.M{"index": bson.M{"_index": index}}
		data, err := json.Marshal(&m)
		if err != nil {
			return err
		}
		_, err = buf.Write(data)
		if err != nil {
			return err
		}
		err = buf.WriteByte(byte('\n'))
		if err != nil {
			return err
		}

		eventTime := time.Unix(v.LogTSec, v.LogTNano)
		ve := elasticLogEntry{
			Entry:      &v,
			TimeEvent:  eventTime,
			TimeRecord: v.TimeCreated,
		}
		// write the entry to insert
		data, err = json.Marshal(ve)
		if err != nil {
			return err
		}
		_, err = buf.Write(data)
		if err != nil {
			return err
		}
		err = buf.WriteByte(byte('\n'))
		if err != nil {
			return err
		}
	}

	response, err := s.r(60).
		SetContext(parentCtx).
		SetBody(buf.String()).
		SetHeader("Content-Type", "application/x-ndjson").
		Post(postURL.String())

	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		return errors.New("WARNING: elasticsearch log entry failed " + response.Status() + "\nReturned Body: " + string(response.Body()))
	}

	return nil
}

// NewElasticLogger uses environment settings to
// to initialize the elastic logger.
//
// You need to call register() afterwards.
func NewElasticLogger() (Backend, error) {
	return newElasticLogger()
}

func newElasticLogger() (*elasticLogger, error) {
	var err error

	defaultLogger := &elasticLogger{}
	defaultLogger.works = false

	defaultLogger.elasticBaseURL = utils.GetEnv(utils.EnvElasticURL)
	defaultLogger.elasticBasicAuthUser = utils.GetEnv(utils.EnvElasticUsername)
	defaultLogger.elasticBasicAuthPass = utils.GetEnv(utils.EnvElasticPassword)
	defaultLogger.elasticBearerToken = utils.GetEnv(utils.EnvElasticBearer)
	defaultLogger.elasticIndexPrefix = utils.GetEnv(utils.EnvPantahubProductName)

	defaultLogger.elasticIndexShards, err = strconv.Atoi(utils.GetEnv(utils.EnvPantahubElasticShards))
	if err != nil {
		log.Fatal("Elastic logger failed; bad config (must be integer) for " + utils.EnvPantahubElasticShards)
	}

	defaultLogger.elasticIndexReplicas, err = strconv.Atoi(utils.GetEnv(utils.EnvPantahubElasticReplicas))
	if err != nil {
		log.Fatal("Elastic logger failed; bad config (must be integer) for " + utils.EnvPantahubElasticReplicas)
	}

	if defaultLogger.elasticBaseURL == "" {
		defaultLogger.works = false
		log.Println("Elasic Logging disabled.")
		return nil, nil
	}

	defaultLogger.elasticURL, err = url.Parse(defaultLogger.elasticBaseURL)
	if err != nil {
		return nil, err
	}

	defaultLogger.template = bson.M{
		"index_patterns": defaultLogger.elasticIndexPrefix + "-*",
		"settings": bson.M{
			"number_of_shards":   defaultLogger.elasticIndexShards,
			"number_of_replicas": defaultLogger.elasticIndexReplicas,
		},
		"mappings": bson.M{
			"properties": bson.M{
				"host": bson.M{
					"type": "keyword",
				},
				"lvl": bson.M{
					"type": "keyword",
				},
				"plat": bson.M{
					"type": "keyword",
				},
				"src": bson.M{
					"type": "keyword",
				},
				"msg": bson.M{
					"type": "text",
				},
				"timeevent": bson.M{
					"type":   "date",
					"format": "strict_date_optional_time||epoch_millis",
				},
				"timerecord": bson.M{
					"type":   "date",
					"format": "strict_date_optional_time||epoch_millis",
				},
				"own": bson.M{
					"type": "keyword",
				},
				"dev": bson.M{
					"type": "keyword",
				},
			},
		},
	}
	return defaultLogger, nil
}
