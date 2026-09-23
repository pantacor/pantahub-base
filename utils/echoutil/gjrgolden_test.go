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

package echoutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// go-json-rest reference outputs, recorded before the framework was removed
// and replayed since, so the differential tests keep comparing echo against
// what go-json-rest actually answered.

var gjrGolden = struct {
	sync.Mutex
	loaded map[string]map[string]json.RawMessage
}{loaded: map[string]map[string]json.RawMessage{}}

func gjrFile(t *testing.T) string {
	top := strings.SplitN(t.Name(), "/", 2)[0]
	return filepath.Join("testdata", "gjr", top+".json")
}

// recorded returns the go-json-rest reference value for label within the
// current (sub)test: replay-only, produce is ignored (nil since recording).
func recorded[T any](t *testing.T, label string, produce func() T) T {
	t.Helper()
	key := t.Name() + "|" + label
	file := gjrFile(t)

	gjrGolden.Lock()
	defer gjrGolden.Unlock()
	m, ok := gjrGolden.loaded[file]
	if !ok {
		m = map[string]json.RawMessage{}
		if b, err := os.ReadFile(file); err == nil {
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("%s: %v", file, err)
			}
		}
		gjrGolden.loaded[file] = m
	}

	var v T
	raw, ok := m[key]
	if !ok {
		t.Fatalf("%s: no recorded go-json-rest reference in %s", key, file)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return v
}
