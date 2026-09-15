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

package logs

import (
	"context"
	"log"
	"os"
	"testing"

	"gitlab.com/pantacor/pantahub-base/utils"
)

var mgoTestLogger *mgoLogger

func setupMongo(t *testing.T) error {

	var err error

	mongoClient, err := utils.GetMongoClientTest()

	if err != nil {
		log.Println("error initiating mongoClient " + err.Error())
		os.Exit(1)
	}

	mgoTestLogger, err = newMgoLogger(mongoClient)

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
	err := mgoTestLogger.postLogs(ctx, logs, false)
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

	filter := &Entry{}
	sort := Sorts{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pager, err := mgoTestLogger.getLogs(ctx, 0, -1, nil, nil, filter, sort, nil, false)

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
	err := mgoTestLogger.postLogs(ctx, logs, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &Entry{}
	sort := Sorts{}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err := mgoTestLogger.getLogs(ctx, 0, 3, nil, nil, filter, sort, nil, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		t.Errorf("pager.Count should be 3, not %d", pager.Count)
		t.Fail()
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err = mgoTestLogger.getLogs(ctx, 1, 3, nil, nil, filter, sort, nil, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		// Count is the TOTAL number of matching entries, not the size of this
		// page -- see the note in logs-elastic.go. This assertion used to read
		// 2, encoding the old behaviour where Count was overwritten with the
		// page length and no caller could tell how much was left.
		t.Errorf("pager.Count should be 3 (total matches), not %d", pager.Count)
		t.Fail()
	} else if len(pager.Entries) != 2 {
		// Paging itself is what this case exercises: from offset 1 of 3
		// entries, a page of 3 yields 2.
		t.Errorf("expected 2 entries from offset 1, got %d", len(pager.Entries))
		t.Fail()
	}

	pager, err = mgoTestLogger.getLogs(ctx, 1, 1, nil, nil, filter, sort, nil, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 3 {
		t.Errorf("pager.Count should be 3 (total matches), not %d", pager.Count)
		t.Fail()
	} else if len(pager.Entries) != 1 {
		t.Errorf("expected 1 entry for a page size of 1, got %d", len(pager.Entries))
		t.Fail()
	}
}

func testMongoDoGetLogsAfter(t *testing.T) {
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
	err := mgoTestLogger.postLogs(ctx, logs, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}

	filter := &Entry{}
	sort := Sorts{}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err := mgoTestLogger.getLogs(ctx, 0, 3, &timeBase, nil, filter, sort, nil, false)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	} else if pager.Count != 2 {
		t.Errorf("pager.Count should be 2, not %d", pager.Count)
		t.Fail()
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	pager, err = mgoTestLogger.getLogs(ctx, 1, 3, &timeBase, nil, filter, sort, nil, false)

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
