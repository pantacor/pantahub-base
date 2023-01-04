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

package logs

import (
	"context"
	"log"
	"testing"
)

var elasticTestLogger *elasticLogger

func testElasticDoLog(t *testing.T) {
	logs := genLogs(Entry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     0,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := elasticTestLogger.postLogs(ctx, logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}
}

func testElasticDoGetLogs(t *testing.T) {
	logs := genLogs(Entry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     100,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := elasticTestLogger.postLogs(ctx, logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &Entry{}
	sort := Sorts{}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err := elasticTestLogger.getLogs(ctx, 0, 3, nil, nil, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		t.Errorf("pager.Count should be 3, not %d", pager.Count)
		t.Fail()
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err = elasticTestLogger.getLogs(ctx, 1, 3, nil, nil, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err = elasticTestLogger.getLogs(ctx, 1, 1, nil, nil, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 1 {
		t.Errorf("pager.Count should be 1, not %d", pager.Count)
		t.Fail()
	}
}

func testElasticDoGetLogsAfter(t *testing.T) {
	logs := genLogs(Entry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     100,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := elasticTestLogger.postLogs(ctx, logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &Entry{}
	sort := Sorts{}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err := elasticTestLogger.getLogs(ctx, 0, 3, &timeBase, nil, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err = elasticTestLogger.getLogs(ctx, 1, 3, &timeBase, nil, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 1 {
		t.Errorf("pager.Count should be 1, not %d", pager.Count)
		t.Fail()
	}
}

func setupElastic(t *testing.T) error {

	var err error

	elasticTestLogger, err = newElasticLogger()

	if err != nil {
		log.Println("error initiating elasticTestLogger " + err.Error())
		return err
	}

	elasticTestLogger.syncWrites = true

	err = elasticTestLogger.register()
	if err != nil {
		log.Println("error registering elasticTestLogger " + err.Error())

		return err
	}

	return nil
}

func teardownElastic(t *testing.T) error {
	var err error

	err = elasticTestLogger.unregister(true)
	if err != nil {
		log.Println("WARN: error unregistering elasticTestLogger " + err.Error())
	}

	elasticTestLogger = nil

	return nil
}

func TestElastic(t *testing.T) {
	subRunSetupTeardownElastic("A=1", t, testElasticDoLog)
	subRunSetupTeardownElastic("A=2", t, testElasticDoGetLogs)
	subRunSetupTeardownElastic("A=3", t, testElasticDoGetLogsAfter)
}
