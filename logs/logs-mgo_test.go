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
	"log"
	"os"
	"testing"

	"gitlab.com/pantacor/pantahub-base/utils"
)

var mgoTestLogger *mgoLogger

func setupMongo(t *testing.T) error {

	var err error

	mgoSession, err := utils.GetMongoSession()

	if err != nil {
		log.Println("error initiating mgoSession " + err.Error())
		os.Exit(1)
	}

	mgoTestLogger, err = newMgoLogger(mgoSession)

	if err != nil {
		log.Println("error initiating mgoTestLogger " + err.Error())
		return err
	}

	mgoTestLogger.mgoCollection = "pantahub_testindex_log"

	return nil
}

func teardownMongo(t *testing.T) error {
	var err error

	err = mgoTestLogger.unregister(true)
	if err != nil {
		log.Println("WARN: error unregistering mgoTestLogger " + err.Error())
	}

	mgoTestLogger = nil

	return nil
}

func doLog() error {

	logs := genLogs(LogsEntry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     0,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	err := mgoTestLogger.postLogs(logs)
	return err
}

func testMongoDoLog(t *testing.T) {
	err := doLog()
	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}
}

func testMongoGetLogs(t *testing.T) {

	err := doLog()
	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &LogsEntry{}
	sort := LogsSort{}
	pager, err := mgoTestLogger.getLogs(0, -1, nil, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	if pager.Count != 3 {
		t.Errorf("pager.Count should be 3, not %d", pager.Count)
		t.Fail()
	}
}

func testMongoDoGetLogs(t *testing.T) {
	logs := genLogs(LogsEntry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     100,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	err := mgoTestLogger.postLogs(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &LogsEntry{}
	sort := LogsSort{}
	pager, err := mgoTestLogger.getLogs(0, 3, nil, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		t.Errorf("pager.Count should be 3, not %d", pager.Count)
		t.Fail()
	}

	pager, err = mgoTestLogger.getLogs(1, 3, nil, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	pager, err = mgoTestLogger.getLogs(1, 1, nil, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 1 {
		t.Errorf("pager.Count should be 1, not %d", pager.Count)
		t.Fail()
	}
}

func testMongoDoGetLogsAfter(t *testing.T) {
	logs := genLogs(LogsEntry{
		Device:      "testdevice",
		Owner:       "testowner",
		TimeCreated: timeBase,
		LogTSec:     100,
		LogTNano:    0,
		LogSource:   "testsource",
		LogLevel:    "TESTLEVEL",
		LogText:     "Test Log Text",
	}, 3)

	err := mgoTestLogger.postLogs(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &LogsEntry{}
	sort := LogsSort{}
	pager, err := mgoTestLogger.getLogs(0, 3, &timeBase, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	pager, err = mgoTestLogger.getLogs(1, 3, &timeBase, false, filter, sort, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 1 {
		t.Errorf("pager.Count should be 1, not %d", pager.Count)
		t.Fail()
	}
}

func TestMgo(t *testing.T) {
	subRunSetupTeardown("A=1", t, testMongoDoLog)
	subRunSetupTeardown("A=2", t, testMongoGetLogs)
	subRunSetupTeardown("A=3", t, testMongoDoGetLogs)
}
