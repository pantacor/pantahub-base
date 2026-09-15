//
// Copyright 2026 Pantacor Ltd.
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

	"github.com/fluent/fluent-logger-golang/fluent"
	elastic "github.com/olivere/elastic/v7"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/resty.v1"
)

const defaultTimeoutSec = 30

// sortTieBreaker makes the sort order total. See buildSearchSource for why it
// is the ObjectID's keyword subfield and not the tsec/tnano event clock.
const sortTieBreaker = "id.keyword"

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

var logger *fluent.Fluent = nil

func getLogger() *fluent.Fluent {
	portStr := utils.GetEnv(utils.EnvFluentPort)

	if logger == nil && portStr != "" {
		portStr := utils.GetEnv(utils.EnvFluentPort)
		port, err := strconv.Atoi(portStr)
		if err != nil {
			log.Fatalln("FATAL: cannot read fluent logger settings: " + err.Error())
		}
		host := utils.GetEnv(utils.EnvFluentHost)
		logger, err = fluent.New(fluent.Config{FluentPort: port, FluentHost: host})
		if err != nil {
			log.Fatalln("FATAL: cannot initialize fluent logger: " + err.Error())
		}
	}

	return logger
}

func (s *elasticLogger) r(timeout int, debug bool) *resty.Request {
	if timeout == 0 {
		timeout = 15
	}
	request := utils.RT(timeout, debug)
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

	response, err := s.r(defaultTimeoutSec, false).SetBody(s.template).Put(registerTemplatesURL.String())

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

	response, err := s.r(defaultTimeoutSec, false).Delete(registerTemplatesURL.String())

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

	response, err = s.r(defaultTimeoutSec, false).Delete(allIndexURL.String())

	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		log.Println("Failed Delete all indexes returned: " + string(response.Body()))
		return errors.New("Unregistering: Delete all indexes failed with status: " + response.Status())
	}

	return nil
}

// buildSearchSource assembles the Elasticsearch query body for a log page:
// filters, paging (offset or search_after) and the sort tuple. It is kept
// free of I/O so the query shape can be asserted directly in tests.
func buildSearchSource(start int64, page int64, before *time.Time, after *time.Time,
	query Filters, sort Sorts, searchAfter []interface{}) (interface{}, error) {

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
		Size(int(page))

	// search_after continues from the previous page's sort values and cannot
	// be combined with a from offset.
	if len(searchAfter) > 0 {
		searchS = searchS.SearchAfter(searchAfter...)
	} else if start > 0 {
		searchS = searchS.From(int(start))
	}

	// Mirror the mongo backend's default rather than leaving the search
	// unsorted: every clause above is a Filter, so scores are constant and an
	// unsorted search comes back in index order.
	if len(sort) == 0 {
		sort = Sorts{"-time-created"}
	}

	// lets do the sort part
	sorted := map[string]bool{}
	primaryAsc := true
	for i, v := range sort {
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
		if i == 0 {
			primaryAsc = asc
		}
		sorted[v] = true
		searchS = searchS.Sort(v, asc)
	}

	// The sort tuple has to be unique. `time-created` is a millisecond-
	// precision date, so one device batch lands many entries on the same
	// value; ordering between them is then decided per shard and is not stable
	// between two requests. That breaks search_after -- it cannot tell where
	// the previous page ended -- and it silently drops every entry sharing the
	// boundary millisecond when the caller pages with an exclusive
	// before/after bound instead.
	//
	// `id` is the per-entry ObjectID, set unconditionally on ingest, and it is
	// the only unique field actually present in every index. tsec/tnano look
	// like the natural choice but are tagged omitempty and are absent from
	// most documents, so sorting on them throws
	// "No mapping found for [tsec] in order to sort on" -- and because the
	// search spans an index wildcard, that surfaces as a 200 with partially
	// failed shards, i.e. entries silently missing from every index that
	// lacks the field. unmapped_type keeps that from ever happening again.
	//
	// Note this relies on `id` keeping its dynamic text+keyword mapping; it
	// must not be pinned to `keyword` in the template, or the `.keyword`
	// subfield would stop existing on new indices.
	if !sorted[sortTieBreaker] {
		searchS = searchS.SortBy(
			elastic.NewFieldSort(sortTieBreaker).
				Order(primaryAsc).
				UnmappedType("keyword"),
		)
	}

	return searchS.Source()
}

func (s *elasticLogger) getLogs(pctx context.Context, start int64, page int64, before *time.Time,
	after *time.Time, query Filters, sort Sorts, searchAfter []interface{}, cursor bool) (*Pager, error) {
	queryFmt := fmt.Sprintf("%s-*/_search", s.elasticIndexPrefix)

	queryURL, err := url.Parse(queryFmt)

	if err != nil {
		return nil, err
	}

	queryURI := s.elasticURL.ResolveReference(queryURL)

	searchBody, err := buildSearchSource(start, page, before, after, query, sort, searchAfter)
	if err != nil {
		return nil, err
	}

	response, err := s.r(defaultTimeoutSec, false).SetContext(pctx).SetBody(searchBody).Post(queryURI.String())
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

	pagerResult.Start = start
	pagerResult.Page = int64(len(elasticResult.Hits.Hits))
	// Total matching entries, not the size of this page. This used to be
	// assigned from TotalHits and then immediately overwritten with the page
	// length, so no caller could tell how much was left.
	pagerResult.Count = elasticResult.TotalHits()

	prototype := Entry{}
	arr := elasticResult.Each(reflect.TypeOf(&prototype))

	for _, v := range arr {
		pagerResult.Entries = append(pagerResult.Entries, v.(*Entry))
	}

	// Hand back the last entry's sort values so the caller can continue with
	// search_after. This is emitted for a short page too: a caller tailing a
	// device needs to resume from exactly where it stopped, and re-issuing the
	// same cursor later simply returns whatever has arrived since. Callers
	// paging through a finite result set should stop when a page comes back
	// shorter than the one they asked for, not when the cursor runs out.
	if cursor && len(elasticResult.Hits.Hits) > 0 {
		lastHit := elasticResult.Hits.Hits[len(elasticResult.Hits.Hits)-1]
		if len(lastHit.Sort) > 0 {
			encoded, err := json.Marshal(lastHit.Sort)
			if err != nil {
				return nil, err
			}
			pagerResult.NextCursor = string(encoded)
		}
	}

	return &pagerResult, nil
}

func (s *elasticLogger) postLogs(parentCtx context.Context, e []Entry, debug bool) error {
	logsVersion := utils.GetEnv("PH_DEVICE_LOGS_VERSION")
	if debug {
		fmt.Printf("PH_DEVICE_LOGS_VERSION %s\n", logsVersion)
	}

	if logsVersion == "v2" {
		return s.postLogsv2(parentCtx, e, debug)
	}

	return s.postLogsv1(parentCtx, e, debug)
}

func (s *elasticLogger) postLogsv2(_ context.Context, e []Entry, _ bool) error {
	logger := getLogger()
	for _, v := range e {
		eventTime := time.Unix(v.LogTSec, v.LogTNano)
		ve := elasticLogEntry{
			Entry:      &v,
			TimeEvent:  eventTime,
			TimeRecord: v.TimeCreated,
		}

		datamap := map[string]interface{}{}
		data, err := json.Marshal(ve)
		if err != nil {
			return err
		}

		err = json.Unmarshal(data, &datamap)
		if err != nil {
			return err
		}

		err = logger.Post("com.pantahub-base.logs", datamap)
		if err != nil {
			return err
		}
	}

	return nil
}

// elasticBulkItemResult is the per-item result inside an Elasticsearch _bulk
// response. Status is the item's HTTP-style status and Error carries the
// failure type/reason when the item was not indexed.
type elasticBulkItemResult struct {
	Status int `json:"status"`
	Error  struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	} `json:"error"`
}

// elasticBulkResponse is the subset of the _bulk response we inspect to detect
// per-item indexing failures: the API returns HTTP 200 even when individual
// documents fail, and those failures live per item under Items.
type elasticBulkResponse struct {
	Errors bool                               `json:"errors"`
	Items  []map[string]elasticBulkItemResult `json:"items"`
}

func (s *elasticLogger) postLogsv1(parentCtx context.Context, e []Entry, debug bool) error {
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
	if debug {
		fmt.Printf("postURL %s\n", postURL)
	}

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

	payload := buf.String()
	if debug {
		fmt.Printf("request to elastic is %s\n", payload)
	}

	response, err := s.r(defaultTimeoutSec, debug).
		SetContext(parentCtx).
		SetBody(payload).
		SetHeader("Content-Type", "application/x-ndjson").
		Post(postURL.String())
	if err != nil {
		return err
	}

	if debug {
		fmt.Printf("elastic response %s\n", string(response.Body()))
	}

	if response.StatusCode() != http.StatusOK {
		return errors.New("WARNING: elasticsearch log entry failed " + response.Status() + "\nReturned Body: " + string(response.Body()))
	}

	// Elasticsearch _bulk returns HTTP 200 even when individual documents
	// fail to index (e.g. es_rejected_execution_exception write-queue
	// rejections on big batches, or max_bytes_length_exceeded_exception when
	// a msg field exceeds the keyword 32KB limit). Those per-item failures
	// live in the response body and must be inspected explicitly; otherwise
	// entries are dropped silently while the handler still replies ok.
	var bulkResp elasticBulkResponse
	if err := json.Unmarshal(response.Body(), &bulkResp); err != nil {
		return fmt.Errorf("WARNING: elasticsearch _bulk response could not be parsed: %s\nReturned Body: %s", err, string(response.Body()))
	}

	if bulkResp.Errors {
		failed, permanent, transient := 0, 0, 0
		firstError := ""
		for _, item := range bulkResp.Items {
			for _, result := range item {
				if result.Status < 300 && result.Error.Type == "" {
					continue
				}
				failed++
				if firstError == "" && result.Error.Type != "" {
					firstError = fmt.Sprintf("%s: %s", result.Error.Type, result.Error.Reason)
				}
				if isTransientBulkError(result.Status, result.Error.Type) {
					transient++
				} else {
					permanent++
				}
			}
		}
		const maxSample = 512
		if len(firstError) > maxSample {
			firstError = firstError[:maxSample]
		}
		msg := fmt.Sprintf("elasticsearch _bulk indexed with errors: %d of %d entries failed (%d permanent, %d transient); first error: %s",
			failed, len(bulkResp.Items), permanent, transient, firstError)

		// A permanent per-item failure (mapper_parsing_exception,
		// max_bytes_length_exceeded_exception, illegal_argument_exception, ...)
		// will never index no matter how often the batch is resent. Returning
		// an error here would wedge the device: ph_logger does not advance its
		// saved log-file position on a failed push, so it would resend the same
		// "poison" batch forever -- re-indexing the healthy items as duplicates
		// and never uploading newer logs. So we drop-and-log the permanent
		// failures and reply ok, letting the device advance past the batch.
		// Only when *every* failure is transient (es_rejected_execution_exception
		// / 429 write-queue pressure, unavailable shards, ...) do we return an
		// error, because resending the same batch can then still succeed.
		if permanent > 0 {
			log.Printf("WARNING: dropping %d unindexable log entr(ies); %s", permanent, msg)
			return nil
		}
		log.Printf("WARNING: %s", msg)
		return errors.New(msg)
	}

	return nil
}

// isTransientBulkError reports whether an Elasticsearch _bulk per-item failure
// is retryable (transient backpressure) versus a permanent rejection of the
// document's content. Transient failures clear on a resend; permanent ones do
// not, so the caller must not block the device's log stream on them.
func isTransientBulkError(status int, errType string) bool {
	switch errType {
	case "es_rejected_execution_exception",
		"unavailable_shards_exception",
		"circuit_breaking_exception",
		"cluster_block_exception",
		"no_shard_available_action_exception":
		return true
	}
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
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
				// The field queries sort on. It had no mapping and relied on
				// dynamic date detection from whichever document happened to
				// create each daily index; existing indices all resolved to
				// `date`, and pinning that keeps a stray document from ever
				// making it `text`, which would fail the sort outright.
				//
				// Deliberately not pinned here: `id`, which must keep its
				// dynamic text+keyword mapping so the `id.keyword` sort
				// tiebreaker keeps existing, and `rev`, which existing indices
				// map as text+keyword -- pinning it to keyword would diverge
				// the type across indices for no gain, as the filter uses
				// match_phrase either way.
				"time-created": bson.M{
					"type":   "date",
					"format": "strict_date_optional_time||epoch_millis",
				},
			},
		},
	}
	return defaultLogger, nil
}
