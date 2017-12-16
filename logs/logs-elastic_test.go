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
	"testing"
	"time"
)

func testElasticDoLog(t *testing.T) {
	logs := genLogs(LogsEntry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: time.Now(),
		LogTSec:     0,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	err := elasticTestLogger.postLogs(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}
}

func testElasticDoGetLogs(t *testing.T) {
	logs := genLogs(LogsEntry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: time.Now(),
		LogTSec:     100,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	err := elasticTestLogger.postLogs(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &LogsEntry{}
	sort := LogsSort{}
	pager, err := elasticTestLogger.getLogs(0, 3, filter, sort)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		t.Errorf("pager.Count should be 3, not %d", pager.Count)
		t.Fail()
	}

	pager, err = elasticTestLogger.getLogs(1, 3, filter, sort)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	pager, err = elasticTestLogger.getLogs(1, 1, filter, sort)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 1 {
		t.Errorf("pager.Count should be 1, not %d", pager.Count)
		t.Fail()
	}
}

func TestElastic(t *testing.T) {
	//subRunSetupTeardownElastic("A=1", t, testElasticDoLog)
	subRunSetupTeardownElastic("A=2", t, testElasticDoGetLogs)
}
