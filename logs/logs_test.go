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
)

var elasticTestLogger *elasticLogger

func subRunSetupTeardown(name string, t *testing.T, f func(t *testing.T)) {
	setupMongo(t)
	f(t)
	teardownMongo(t)
}

func subRunSetupTeardownElastic(name string, t *testing.T, f func(t *testing.T)) {
	setupElastic(t)
	f(t)
	teardownElastic(t)
}

func genLogs(proto LogsEntry, count int) []LogsEntry {

	logs := []LogsEntry{}

	for i := 0; i < count; i++ {
		instance := proto
		instance.LogTSec = 100 + int64(i)
		logs = append(logs, instance)
	}
	return logs
}

func TestMain(m *testing.M) {

	exitCode := m.Run()

	if exitCode != 0 {
		log.Printf("error running tests %d\n", exitCode)
	}

	os.Exit(exitCode)
}
