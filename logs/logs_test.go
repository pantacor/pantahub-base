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

	"gitlab.com/pantacor/pantahub-base/utils"
)

var elasticTestLogger *elasticLogger
var mgoTestLogger *mgoLogger

func TestMain(m *testing.M) {
	var err error

	elasticTestLogger, err = newElasticLogger()

	if err != nil {
		log.Println("error initiating elasticTestLogger " + err.Error())
		os.Exit(1)
	}

	elasticTestLogger.elasticIndexPrefix = "pantahub_testindex"
	err = elasticTestLogger.register()
	if err != nil {
		log.Println("error registering elasticTestLogger " + err.Error())
		os.Exit(2)
	}

	mgoSession, err := utils.GetMongoSession()

	if err != nil {
		log.Println("error initiating mgoSession " + err.Error())
		os.Exit(1)
	}

	mgoTestLogger, err = newMgoLogger(mgoSession)

	if err != nil {
		log.Println("error initiating mgoTestLogger " + err.Error())
		os.Exit(1)
	}

	mgoTestLogger.mgoCollection = "pantahub_testindex_log"

	err = mgoTestLogger.register()
	if err != nil {
		log.Println("error registering mgoTestLogger " + err.Error())
		os.Exit(2)
	}

	exitCode := m.Run()

	err = mgoTestLogger.unregister(true)
	if err != nil {
		log.Println("error unregistering mgoTestLogger " + err.Error())
		os.Exit(3)
	}

	err = elasticTestLogger.unregister(true)
	if err != nil {
		log.Println("error unregistering elasticTestLogger " + err.Error())
		os.Exit(4)
	}

	if exitCode != 0 {
		log.Printf("error running tests %d\n", exitCode)
	}

	os.Exit(exitCode)
}
