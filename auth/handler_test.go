// Copyright (c) 2017-2026 Pantacor Ltd.
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

package auth

import (
	"net/http"
	"testing"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// newContractHandler serves auth the way base/init.go mounts it.
func newContractHandler(t *testing.T) http.Handler {
	t.Helper()
	mongoClient, err := utils.GetMongoClientTest()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &jwtauth.Config{
		Key:        []byte(contractKey),
		Realm:      contractRealm,
		Timeout:    time.Hour,
		MaxRefresh: 24 * time.Hour,
	}
	app := New(cfg, mongoClient)
	s := echoutil.NewServer("test")
	app.Mount(s)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/auth/", s.E)
	return mux
}
