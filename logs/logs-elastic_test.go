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
	"log"
	"os"
	"testing"
	"time"
)

var testLogger *elasticLogger

func TestDoLog(t *testing.T) {
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
	}

	err := testLogger.doLog(logs)

	if err != nil {
		t.Errorf("do Log fails: %s", err.Error())
		t.Fail()
	}
}

func TestMain(m *testing.M) {
	var err error

	testLogger, err = newElasticLogger()

	if err != nil {
		log.Println("error initiating testlogger " + err.Error())
		os.Exit(1)
	}

	testLogger.elasticIndexPrefix = "pantahub_testindex"
	err = testLogger.register()
	if err != nil {
		log.Println("error registering testlogger " + err.Error())
		os.Exit(2)
	}
	exitCode := m.Run()

	err = testLogger.unregister(true)
	if err != nil {
		log.Println("error unregistering testlogger " + err.Error())
		os.Exit(3)
	}

	os.Exit(exitCode)
}
