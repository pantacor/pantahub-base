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

package webhooks

import (
	"net/http"
	"testing"

	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// newHandler serves the webhooks app the way base/init.go mounts it.
func newHandler(t *testing.T) http.Handler {
	t.Helper()
	app := New(&jwtauth.Config{Realm: testRealm, Key: []byte(testKey)})
	s := echoutil.NewServer("test")
	app.Mount(s)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/webhooks/", s.E)
	return mux
}
