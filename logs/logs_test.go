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

func TestUnmarshalBodySingle(t *testing.T) {

	singleString := `
		{ "tsec": 5, "tnano": 730159, "lvl": "DEBUG", "src": "controller", "msg": "c->storage.path = '/dev/mmcblk0p2'" }
	`

	entries, err := unmarshalBody([]byte(singleString))

	if err != nil {
		t.Errorf("Must not fail to parse logs body 1: %s", err.Error())
		t.Fail()
	}

	if len(entries) != 1 {
		t.Errorf("Must have exactly two log entries parsed, but have %d", len(entries))
		t.Fail()
	}

	if entries[0].LogTSec != 5 {
		t.Errorf("tsec != 5, but %d", entries[0].LogTSec)
		t.Fail()
	}

	if entries[0].LogTNano != 730159 {
		t.Errorf("tsec != 730159, but %d", entries[0].LogTNano)
		t.Fail()
	}

	if entries[0].LogLevel != "DEBUG" {
		t.Errorf("LogLevel != \"DEBUG\", but %s", entries[0].LogLevel)
		t.Fail()
	}

	if entries[0].LogSource != "controller" {
		t.Errorf("LogSource != \"controller\", but %s", entries[0].LogSource)
		t.Fail()
	}

	if entries[0].LogText != "c->storage.path = '/dev/mmcblk0p2'" {
		t.Errorf("LogText != \"c->storage.path = '/dev/mmcblk0p2'\", but %s", entries[0].LogText)
		t.Fail()
	}
}

func TestUnmarshalBodyArray(t *testing.T) {

	arrayString := `[
		{ "tsec": 5, "tnano": 730159, "lvl": "DEBUG", "src": "controller", "msg": "c->storage.path = '/dev/mmcblk0p2' " },
		{ "tsec": 5, "tnano": 730234, "lvl": "DEBUG", "src": "controller", "msg": "c->storage.fstype = 'ext4' " },
		{ "tsec": 5, "tnano": 730279, "lvl": "DEBUG", "src": "controller", "msg": "c->storage.opts = '' " },
		{ "tsec": 5, "tnano": 730321, "lvl": "DEBUG", "src": "controller", "msg": "c->storage.mntpoint = '/storage' " },
		{ "tsec": 5, "tnano": 730364, "lvl": "DEBUG", "src": "controller", "msg": "c->creds.host = 'api2.pantahub.com' " }
	]`

	entries, err := unmarshalBody([]byte(arrayString))

	if err != nil {
		t.Errorf("Must not fail to parse logs body 1: %s", err.Error())
		t.Fail()
	}

	if len(entries) != 5 {
		t.Errorf("Must have exactly two log entries parsed, but have %d", len(entries))
		t.Fail()
	}
}

func TestUnmarshalBodyArrayEmpty(t *testing.T) {

	arrayString := `[]`

	entries, err := unmarshalBody([]byte(arrayString))

	if err != nil {
		t.Errorf("Must not fail to parse logs body 1: %s", err.Error())
		t.Fail()
	}

	if len(entries) != 0 {
		t.Errorf("Must have exactly two log entries parsed, but have %d", len(entries))
		t.Fail()
	}
}

func TestMain(m *testing.M) {

	exitCode := m.Run()

	if exitCode != 0 {
		log.Printf("error running tests %d\n", exitCode)
	}

	os.Exit(exitCode)
}
