//
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
//

package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

// A name must never resolve outside the storage base. path.Clean on its own
// keeps a leading "..", which filepath.Join then resolves, so these inputs
// used to escape.
func TestMakeLocalS3PathForNameStaysInsideBase(t *testing.T) {
	base := PantahubS3Path()
	absBase, err := filepath.Abs(base)
	if err != nil {
		t.Fatalf("resolving base: %s", err)
	}

	for _, name := range []string{
		"../../etc/passwd",
		"../outside",
		"a/../../../etc/shadow",
		"./../../escape",
		"..",
		"/../../etc/passwd",
		"subdir/../../../../root/.ssh/authorized_keys",
	} {
		got, err := MakeLocalS3PathForName(name)
		if err != nil {
			// Refusing outright is an acceptable outcome too.
			continue
		}

		absGot, absErr := filepath.Abs(got)
		if absErr != nil {
			t.Fatalf("resolving %q: %s", got, absErr)
		}

		relative, relErr := filepath.Rel(absBase, absGot)
		if relErr != nil ||
			relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Errorf("name %q escaped the storage base: %q", name, got)
		}
	}
}

func TestMakeLocalS3PathForNameRejectsNul(t *testing.T) {
	if _, err := MakeLocalS3PathForName("obj\x00ect"); err == nil {
		t.Error("a NUL byte in the name was accepted")
	}
}

// The ordinary case still has to work: a storage id plus a unique suffix.
func TestMakeLocalS3PathForNameKeepsOrdinaryNames(t *testing.T) {
	got, err := MakeLocalS3PathForName("5f1a2b3c4d5e6f7a8b9c0d1e.6aa8f6d6cf81a60009704aee")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !strings.HasSuffix(got, "5f1a2b3c4d5e6f7a8b9c0d1e.6aa8f6d6cf81a60009704aee") {
		t.Errorf("name was mangled: %q", got)
	}
}
