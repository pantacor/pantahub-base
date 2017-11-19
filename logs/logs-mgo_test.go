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
	"testing"
	"time"
)

func TestMongoDoLog(t *testing.T) {
	logs := []*LogsEntry{
		&LogsEntry{
			Device:      "testdevice",
			Owner:       "testowner",
			TimeCreated: time.Now(),
			LogTSec:     100,
			LogTNano:    0,
			LogSource:   "testsource",
			LogLevel:    "TESTLEVEL",
			LogText:     "Test Log Text",
		},
		&LogsEntry{
			Device:      "testdevice",
			Owner:       "testowner",
			TimeCreated: time.Now(),
			LogTSec:     101,
			LogTNano:    0,
			LogSource:   "testsource",
			LogLevel:    "TESTLEVEL",
			LogText:     "Test Log Text 1",
		},
		&LogsEntry{
			Device:      "testdevice",
			Owner:       "testowner",
			TimeCreated: time.Now(),
			LogTSec:     101,
			LogTNano:    1,
			LogSource:   "testsource",
			LogLevel:    "TESTLEVEL",
			LogText:     "Test Log Text 2",
		},
	}

	err := mgoTestLogger.postLogs(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}
}
