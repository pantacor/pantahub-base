//
// Copyright 2017  Pantacor Ltd.
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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

var (
	defaultLogger *elasticLogger
)

type elasticLogger struct {
	elasticBaseURL       string
	elasticURL           *url.URL
	elasticBasicAuthUser string
	elasticBasicAuthPass string
	elasticBearerToken   string
	elasticIndexPrefix   string
	works                bool
	template             bson.M
}

func (s *elasticLogger) r() *resty.Request {
	request := utils.R()
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

func (s *elasticLogger) getLogs(start int, page int, query LogsFilter, sort LogsSort) (*LogsPager, error) {
	return nil, errors.New("WARNING: getLogs for elastic logger not yet implemented.")
}

func (s *elasticLogger) postLogs(e []*LogsEntry) error {
	if !s.works {
		return errors.New("logger not initialized/works")
	}

	var buf bytes.Buffer

	timeRecv := time.Now()
	index := fmt.Sprintf(s.elasticIndexPrefix+"-%.4d%.2d%.2d", timeRecv.Year(), timeRecv.Month(), timeRecv.Day())

	bulkPostURL, err := url.Parse("_bulk")
	if err != nil {
		return err
	}

	postURL := s.elasticURL.ResolveReference(bulkPostURL)

	for _, v := range e {
		// write the bulkd op)
		m := bson.M{"index": bson.M{"_index": index, "_type": "pv"}}
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

		// write the entry to insert
		data, err = json.Marshal(v)
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

	response, err := s.r().
		SetBody(string(buf.Bytes())).
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
func NewElasticLogger() (LogsBackend, error) {
	return newElasticLogger()
}

func newElasticLogger() (*elasticLogger, error) {
	var err error

	defaultLogger := &elasticLogger{}
	defaultLogger.works = false

	defaultLogger.elasticBaseURL = utils.GetEnv(utils.ENV_ELASTIC_URL)
	defaultLogger.elasticBasicAuthUser = utils.GetEnv(utils.ENV_ELASTIC_USERNAME)
	defaultLogger.elasticBasicAuthPass = utils.GetEnv(utils.ENV_ELASTIC_PASSWORD)
	defaultLogger.elasticBearerToken = utils.GetEnv(utils.ENV_ELASTIC_BEARER)
	defaultLogger.elasticIndexPrefix = utils.GetEnv(utils.ENV_PANTAHUB_PRODUCTNAME)

	if defaultLogger.elasticBaseURL == "" {
		defaultLogger.works = false
		log.Println("Elasic Logging disabled.")
		return nil, errors.New("cannot initiated logger without baseurl")
	}

	defaultLogger.elasticURL, err = url.Parse(defaultLogger.elasticBaseURL)
	if err != nil {
		return nil, err
	}

	defaultLogger.template = bson.M{
		"index_patterns": defaultLogger.elasticIndexPrefix + "-*",
		"settings": bson.M{
			"number_of_shards": 5,
		},
		"mappings": bson.M{
			"pv": bson.M{
				"_source": bson.M{
					"enabled": true,
				},
				"properties": bson.M{
					"hostname": bson.M{
						"type": "text",
					},
					"lvl": bson.M{
						"type": "keyword",
					},
					"plat": bson.M{
						"type": "keyword",
					},
					"source": bson.M{
						"type": "keyword",
					},
					"message": bson.M{
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
					"owner": bson.M{
						"type": "text",
					},
					"device": bson.M{
						"type": "text",
					},
				},
			},
		},
	}
	return defaultLogger, nil
}
