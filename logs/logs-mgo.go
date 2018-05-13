//
// Copyright 2017, 2018  Pantacor Ltd.
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
	"errors"
	"log"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type mgoLogger struct {
	mgoSession    *mgo.Session
	mgoCollection string
}

func (s *mgoLogger) register() error {
	var err error

	index := mgo.Index{
		Key:        []string{"own"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"dev"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"time-created"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"tsec", "tnano"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"lvl"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"dev", "own", "time-created"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err = s.mgoSession.DB("").C(s.mgoCollection).EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for: " + s.mgoCollection + " ERROR: " + err.Error())
		return err
	}

	return nil
}

func (s *mgoLogger) unregister(delete bool) error {
	if delete {
		err := s.mgoSession.DB("").C(s.mgoCollection).DropCollection()
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *mgoLogger) getLogs(start int64, page int64, after *time.Time,
	query LogsFilter, sort LogsSort) (*LogsPager, error) {
	var result LogsPager
	var err error

	sortStr := strings.Join(sort, ",")
	collLogs := s.mgoSession.DB("").C(s.mgoCollection)

	if collLogs == nil {
		return nil, errors.New("Couldnt instantiate mgo connection for collection " + s.mgoCollection)
	}

	findFilter := bson.M{}

	if query.Owner != "" {
		findFilter["own"] = query.Owner
	}
	if query.LogLevel != "" {
		findFilter["lvl"] = query.LogLevel
	}
	if query.Device != "" {
		findFilter["dev"] = query.Device
	}
	if query.LogSource != "" {
		findFilter["src"] = query.LogSource
	}

	if after != nil {
		findFilter["time-created"] = bson.M{
			"$gt": after,
		}
	}

	// default sort by reverse time
	if sortStr == "" {
		sortStr =
			"-time-created"
	}

	q := collLogs.Find(findFilter).Sort(sortStr)

	if start > 0 {
		q = q.Skip(int(start))
	}
	if page > 0 {
		q = q.Limit(int(page))
	}

	count, err := q.Count()
	result.Count = int64(count)
	result.Start = start
	result.Page = page

	if err != nil {
		return nil, err
	}

	entries := []*LogsEntry{}
	err = q.Skip(int(start)).Limit(int(page)).All(&entries)

	if err != nil {
		return nil, err
	}

	result.Entries = entries
	return &result, nil
}

func (s *mgoLogger) postLogs(e []LogsEntry) error {
	collLogs := s.mgoSession.DB("").C(s.mgoCollection)

	if collLogs == nil {
		return errors.New("Error with Database connectivity")
	}

	arr := make([]interface{}, len(e))
	for i, v := range e {
		arr[i] = v
	}
	err := collLogs.Insert(arr...)
	if err != nil {
		return err
	}

	return nil
}

// NewMgoLogger instantiates an mgo logger backend. Expects an
// mgoSession configuration
func NewMgoLogger(mgoSession *mgo.Session) (LogsBackend, error) {
	return newMgoLogger(mgoSession)
}

func newMgoLogger(mgoSession *mgo.Session) (*mgoLogger, error) {
	self := &mgoLogger{}
	self.mgoCollection = utils.GetEnv(utils.ENV_PANTAHUB_PRODUCTNAME) + "_logs"
	self.mgoSession = mgoSession

	return self, nil
}
